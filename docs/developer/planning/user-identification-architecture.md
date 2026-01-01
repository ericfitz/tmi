# User Identification Architecture - Planning Document

**Status**: Approved for implementation
**Last Updated**: 2025-11-22
**Breaking Changes**: Yes - Complete database schema refactoring

## Executive Summary

This refactoring consolidates the user identification architecture to eliminate confusion between Internal UUIDs and Provider IDs. Key changes:

1. **Consolidate `user_providers` table into `users`** - Eliminates duplication
2. **Standardize naming** - `internal_uuid`, `provider`, `provider_user_id` throughout
3. **JWT profile updates** - Middleware updates email/name from JWT claims
4. **Single context object** - Store `UserCacheEntry` instead of separate keys
5. **Redis-backed user cache** - Reduce database lookups

**Business Logic**: Users from different OAuth providers are treated as separate users, even with the same email address.

## Key Decisions Made

### 1. Table Consolidation ✅

**Decision**: Eliminate `user_providers` table entirely and consolidate into `users` table.

**Rationale**:

- Business logic treats each provider account as separate user
- No account linking feature planned or implemented
- Eliminates unnecessary JOIN on every authenticated request
- Simplifies schema and aligns with actual usage

### 2. Naming Convention ✅

**Decision**: Use prefix-based naming throughout (Option C+)

**Standard**:

- `internal_uuid` - Database primary key (UUID type)
- `provider` - OAuth provider name ("test", "google", etc.)
- `provider_user_id` - Provider's identifier for user
- Foreign keys: `{relationship}_internal_uuid` (e.g., `owner_internal_uuid`)

### 3. Single Context Object ✅

**Decision**: Store `*UserCacheEntry` in context instead of separate keys

**Benefits**:

- Type-safe access to all user fields
- No risk of mismatched values
- Cleaner handler code: `user := MustGetAuthenticatedUser(c)`

### 4. JWT Profile Updates ✅

**Decision**: Middleware automatically updates email/name from JWT claims

**Implementation**:

- Extract `email` and `name` from JWT on every request
- Compare with cached values
- Update database if different
- Don't fail request on update errors (log warning only)

### 5. User Cache Design ✅

**Decision**: Redis-backed cache with 15-minute TTL

**Key Structure**:

- Primary storage: `user:cache:{internal_uuid}` → full `UserCacheEntry`
- Index for lookups: `user:provider:{provider}:{provider_user_id}` → `internal_uuid`
- Database fallback on cache miss or Redis unavailable

### 6. Migration Strategy ✅

**Decision**: Consolidate all migrations into two base files

**Approach**:

- Archive existing migrations to `docs/archive/migrations/`
- Consolidate into `000001_core_infrastructure.up.sql` and `000002_business_domain.up.sql`
- Drop and recreate all databases (dev + Heroku)
- No backwards compatibility needed (pre-launch)

## Current State Analysis

### User Identity Fields

The system needs to track FOUR distinct user identifiers:

1. **Internal UUID** (`uuid.UUID`) - Auto-generated primary key in `users` table

   - Purpose: Internal database relationships, foreign keys, indices
   - Never exposed in API responses
   - Used for: Rate limiting, quotas, database joins, caching keys

2. **Provider** (`string`) - OAuth provider name

   - Purpose: Identify which OAuth provider authenticated this user
   - Examples: "test", "google", "github", "azure"
   - Combined with Provider User ID for unique user identification

3. **Provider User ID** (`string`) - Opaque identifier from OAuth provider

   - Purpose: External user identification from identity provider
   - Used in: JWT `sub` claim, API responses (as `id` field)
   - Examples: "alice@tmi", "108234567890123456789" (Google)
   - Unique per provider (not globally unique)

4. **Email** (`string`) - User's email address

   - Purpose: User-visible identifier, communication
   - NOT unique across providers (alice@gmail.com from Google ≠ alice@gmail.com from GitHub)
   - Used in: API responses, JWT claims, display

5. **Name/Display Name** (`string`) - User's display name
   - Purpose: User-visible label
   - Used in: API responses, JWT claims, UI display

### Critical Rule: JWT `sub` Claim

**The JWT `sub` claim ALWAYS contains the Provider ID, NEVER the Internal UUID.**

Current implementation (auth/service.go:179):

```go
Subject: providerID,  // ✅ CORRECT
```

Any code comparing JWT `sub` to Internal UUID is a BUG.
Any code putting Internal UUID into JWT `sub` is a BUG.

## Problems Identified

### 1. Context Variable Confusion

**File**: `cmd/server/jwt_auth.go:141`

Current code:

```go
c.Set("userID", sub)  // sub is Provider ID from JWT
```

**Problem**: The variable name `userID` is misleading - it contains Provider ID, not Internal UUID.

**Impact**: Every handler that calls `c.Get("userID")` expects different things:

- Some treat it as a UUID (BUG)
- Some treat it as a string Provider ID (CORRECT)
- Inconsistent usage leads to type conversion errors

### 2. User Lookup Pattern

**Current flow**:

1. JWT middleware extracts `sub` (Provider ID)
2. Sets `c.Set("userID", sub)`
3. Handlers retrieve it with `c.Get("userID")`
4. Need to look up Internal UUID separately for database operations

**Missing**: No centralized user lookup/caching mechanism

### 3. Database Schema Issues

**Current Table**: `users`

```sql
- uuid (PRIMARY KEY, auto-generated) -- Internal UUID
- id (TEXT, UNIQUE)                  -- Provider ID
- email (TEXT)
- name (TEXT)
...
```

**Current Table**: `user_providers`

```sql
- user_uuid (FOREIGN KEY -> users.uuid)  -- Internal UUID
- provider (TEXT)                        -- "test", "google", "github"
- provider_user_id (TEXT)                -- Provider ID
- email (TEXT)
- access_token (TEXT)                    -- OAuth tokens
- refresh_token (TEXT)
- token_expiry (TIMESTAMP)
```

**Problems Identified**:

1. ❌ **Duplication**: `users.id` and `user_providers.provider_user_id` both store Provider ID
2. ❌ **Confusion**: Which table is source of truth for user identity?
3. ❌ **Unnecessary Join**: Every authenticated request requires join between tables
4. ❌ **Business Logic Mismatch**: Schema suggests multi-provider linking, but business logic treats each provider as separate user
5. ❌ **Token Storage Split**: OAuth tokens in separate table from user data

### 4. API Response Representation

**OpenAPI Generated**: `api.User`

```go
type User struct {
    Id    string  // Provider ID
    Email Email
    Name  string
}
```

**Auth Service**: `auth.User`

```go
type User struct {
    ID               string    // Provider ID (confusing name!)
    Email            string
    Name             string
    EmailVerified    bool
    GivenName        string
    FamilyName       string
    Picture          string
    Locale           string
    IdentityProvider string
    Groups           []string
    CreatedAt        time.Time
    ModifiedAt       time.Time
    LastLogin        time.Time
}
```

**Problem**: `auth.User.ID` looks like Internal UUID but actually holds Provider ID

### 5. Invocation Storage

**File**: `api/addon_invocation_store.go`

```go
type AddonInvocation struct {
    InvokedByUUID   uuid.UUID  // Internal UUID ✅
    InvokedByID     string     // Provider ID ✅
    InvokedByEmail  string     // Email ✅
    InvokedByName   string     // Display name ✅
}
```

**Status**: CORRECT architecture! Stores all needed identifiers.

**Problem**: No helper to populate these from context

### 6. Rate Limiting & Quotas

**Files**:

- `api/addon_rate_limiter.go`
- `api/addon_invocation_quota_store.go`

**Current**: Uses `uuid.UUID` for user identification ✅ CORRECT

**Problem**: Handlers need to provide Internal UUID, but context only has Provider ID

## Terminology Normalization Challenge

### The "id" Name Collision Problem

**Current Conflicting Usage**:

1. **Database**: `users.id` column = Provider ID (opaque string from OAuth provider)
2. **OpenAPI**: `User.id` field = Provider ID (matches database, but confusing name)
3. **Database**: `users.uuid` column = Internal UUID (actual primary key)
4. **Code**: `auth.User.ID` field = Provider ID (looks like it should be UUID!)
5. **Context**: `c.Get("userID")` = Provider ID (misleading name!)

**Why This Causes Bugs**:

- Developers see "ID" and assume it's the primary key UUID
- Variables named `userID` are sometimes UUID, sometimes string
- No way to tell from the name which ID we're talking about
- Type system doesn't help when both are stored as strings in different places

### Proposed Naming Standard

We need ONE consistent naming rubric across database, code, and API:

#### Option A: Rename Database Column (BREAKING CHANGE)

**Database Migration**:

```sql
ALTER TABLE users RENAME COLUMN id TO provider_id;
ALTER TABLE users RENAME COLUMN uuid TO id;  -- Make UUID the "id"
```

**Pros**:

- `id` becomes the actual primary key (standard convention)
- Clear distinction: `id` vs `provider_id`
- Aligns with Rails/Django conventions

**Cons**:

- Requires database migration
- Breaks existing queries
- Need to update all SQL statements
- Risky for production system

#### Option B: Standardize Code Names (NO DATABASE CHANGE)

**Keep database as-is, standardize variable names**:

| Concept       | Database Column | Go Struct Field      | Variable Name | JSON Field (API) |
| ------------- | --------------- | -------------------- | ------------- | ---------------- |
| Internal UUID | `users.uuid`    | `UserUUID uuid.UUID` | `userUUID`    | (never exposed)  |
| Provider ID   | `users.id`      | `ProviderID string`  | `providerID`  | `id`             |
| Email         | `users.email`   | `Email string`       | `userEmail`   | `email`          |
| Name          | `users.name`    | `Name string`        | `userName`    | `name`           |

**Pros**:

- No database changes
- Can implement incrementally
- Type system helps (uuid.UUID vs string)
- Clear naming in code

**Cons**:

- Database columns keep confusing names
- SQL queries still use `users.id` for provider ID
- Mismatch between database and code names

#### Option C: Prefix-Based Naming (COMPREHENSIVE)

**Add prefixes everywhere for clarity**:

| Concept       | Database Column       | Go Struct Field          | Variable Name  | Context Key    |
| ------------- | --------------------- | ------------------------ | -------------- | -------------- |
| Internal UUID | `users.internal_uuid` | `InternalUUID uuid.UUID` | `internalUUID` | `internalUUID` |
| Provider ID   | `users.provider_id`   | `ProviderID string`      | `providerID`   | `providerID`   |
| Email         | `users.email`         | `Email string`           | `email`        | `userEmail`    |
| Name          | `users.name`          | `Name string`            | `name`         | `userName`     |

**Pros**:

- Crystal clear everywhere
- Impossible to confuse
- Self-documenting code

**Cons**:

- Verbose
- Requires database migration
- Large refactoring effort

### Recommended Approach: Option C+ (Comprehensive Refactoring + Table Consolidation)

**Since we haven't launched yet, we can do a complete refactoring without backwards compatibility concerns.**

**RECOMMENDATION: Option C+ - Prefix-Based Naming with Database Migration AND Table Consolidation**

This eliminates ALL ambiguity and prevents future bugs by:

1. Using crystal-clear naming conventions
2. Consolidating `user_providers` into `users` table
3. Aligning schema with business logic (separate users per provider)

| Concept          | Database Column          | Go Struct Field          | Variable Name    | Context Storage                 |
| ---------------- | ------------------------ | ------------------------ | ---------------- | ------------------------------- |
| Internal UUID    | `users.internal_uuid`    | `InternalUUID uuid.UUID` | `internalUUID`   | Part of `UserCacheEntry` object |
| Provider         | `users.provider`         | `Provider string`        | `provider`       | Part of `UserCacheEntry` object |
| Provider User ID | `users.provider_user_id` | `ProviderUserID string`  | `providerUserID` | Part of `UserCacheEntry` object |
| Email            | `users.email`            | `Email string`           | `email`          | Part of `UserCacheEntry` object |
| Name             | `users.name`             | `Name string`            | `name`           | Part of `UserCacheEntry` object |

**Rationale for Table Consolidation**:

The current `user_providers` table exists to support linking multiple OAuth providers to a single user account (e.g., "Link your Google account" feature). However:

- ✅ **Business Logic**: We treat users from different providers as **separate users**
  - `alice@gmail.com` via Google = User A
  - `alice@gmail.com` via GitHub = User B (completely different user!)
- ✅ **No Account Linking**: We don't have (and don't plan) UI for linking provider accounts
- ❌ **Current Schema**: Suggests multi-provider support we don't implement
- ❌ **Performance**: Requires unnecessary join on every authenticated request

**Solution**: Consolidate all fields into single `users` table with `(provider, provider_user_id)` as unique identifier.

**Benefits of Full Refactoring (Pre-Launch)**:

- Crystal clear naming everywhere - impossible to confuse IDs
- Self-documenting code - no need to check which "id" is meant
- Type system helps prevent bugs (uuid.UUID vs string)
- No technical debt carried forward
- Clean foundation for future development
- Easier onboarding for new developers

**Strict Naming Conventions**:

1. **NEVER use generic "ID" anywhere** - always fully qualified:

   - ❌ `userID`, `id`, `ID` (ambiguous)
   - ✅ `internalUUID` (database primary key)
   - ✅ `providerID` (OAuth provider's identifier)

2. **Struct field naming**:

   ```go
   type User struct {
       InternalUUID uuid.UUID `db:"internal_uuid"` // Database primary key
       ProviderID   string    `db:"provider_id"`   // OAuth provider ID
       Email        string    `db:"email"`
       Name         string    `db:"name"`
   }
   ```

3. **Variable naming patterns**:

   ```go
   var internalUUID uuid.UUID   // Database primary key
   var providerID string         // OAuth provider ID
   var userEmail string          // Email address
   var userName string           // Display name
   ```

4. **Context keys** (use in `c.Set()`/`c.Get()`):

   ```go
   "internalUUID"  // Internal UUID (stored as string, parse to uuid.UUID)
   "providerID"    // Provider ID
   "userEmail"     // Email
   "userName"      // Display name
   ```

5. **Function parameters** - always use full names:

   ```go
   func GetUser(ctx context.Context, internalUUID uuid.UUID) (*User, error)
   func GetUserByProviderID(ctx context.Context, providerID string) (*User, error)
   ```

6. **SQL Queries** - now match code naming:
   ```sql
   SELECT internal_uuid, provider_id, email, name FROM users WHERE provider_id = $1
   ```

### Implementation Steps for Terminology Normalization

**Phase 1: Database Migration (FIRST)**

1. Create migration to rename columns:
   - `users.uuid` → `users.internal_uuid`
   - `users.id` → `users.provider_id`
2. Update all SQL queries in `auth/` package
3. Test database operations

**Phase 2: Update Struct Definitions**

1. Rename `auth.User.ID` to `auth.User.ProviderID`
2. Rename `auth.User.UUID` to `auth.User.InternalUUID` (if exists)
3. Update all struct tags to match new column names
4. Regenerate any code that depends on struct definitions

**Phase 3: Update All Code References**

1. Update all variable declarations to follow conventions
2. Update context keys in middleware
3. Update all handler code
4. Update all test code

**Phase 4: Verification**

1. Run all tests (unit + integration)
2. Verify no generic "ID" usage remains
3. Add linter rules to prevent future violations
4. Update code review guidelines

## Recommended Architecture

### Design Principles

1. **Internal UUID for all internal operations**

   - Database foreign keys
   - Rate limiting keys
   - Quota tracking
   - Cache keys
   - Redis keys
   - **Database column**: `internal_uuid`
   - **Variable name**: `internalUUID`
   - **Type**: `uuid.UUID`

2. **Provider ID for external identification**

   - JWT `sub` claim
   - API responses (as `id` field)
   - Provider lookups
   - **Database column**: `provider_id`
   - **Variable name**: `providerID`
   - **Type**: `string`

3. **Single source of truth**
   - No duplicate storage of same data
   - No unnecessary type conversions
   - Clear separation of concerns
   - **Consistent naming across ALL layers** (database, code, API)

### Proposed Solution

#### Step 1: Database Schema Migration - Consolidated Single Table Design

**Strategy: Consolidate ALL migrations + Merge user_providers into users table**

Since we're doing a breaking refactoring with no backwards compatibility, we will:

1. Consolidate all existing migrations into the original two base migrations
2. **ELIMINATE** `user_providers` table entirely
3. Move all provider-specific fields into `users` table
4. Use `(provider, provider_user_id)` as unique identifier

**File**: `auth/migrations/000001_core_infrastructure.up.sql`

```sql
-- Consolidated users table with provider information
CREATE TABLE users (
    internal_uuid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,                           -- NEW: "test", "google", "github", "azure"
    provider_user_id TEXT NOT NULL,                   -- RENAMED from 'id'
    email TEXT NOT NULL,
    name TEXT NOT NULL,
    email_verified BOOLEAN DEFAULT FALSE,
    given_name TEXT,
    family_name TEXT,
    picture TEXT,
    locale TEXT,
    access_token TEXT,                                -- MOVED from user_providers
    refresh_token TEXT,                               -- MOVED from user_providers
    token_expiry TIMESTAMP WITH TIME ZONE,            -- MOVED from user_providers
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMP WITH TIME ZONE,

    -- Unique constraint: one user per (provider, provider_user_id) combination
    UNIQUE(provider, provider_user_id)
);

-- Index for quick lookups by provider + provider_user_id
CREATE INDEX idx_users_provider_lookup ON users(provider, provider_user_id);

-- NO user_providers table - consolidated into users!
```

**File**: `auth/migrations/000002_business_domain.up.sql`

```sql
-- Update all foreign key references to use internal_uuid with descriptive names
CREATE TABLE threat_models (
    uuid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    owner_internal_uuid UUID NOT NULL REFERENCES users(internal_uuid) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    -- ... rest of columns
);

CREATE TABLE threat_model_permissions (
    threat_model_uuid UUID NOT NULL REFERENCES threat_models(uuid) ON DELETE CASCADE,
    user_internal_uuid UUID NOT NULL REFERENCES users(internal_uuid) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('reader', 'writer', 'owner')),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (threat_model_uuid, user_internal_uuid)
);

-- Similar updates for all other tables with user foreign keys:
-- - diagrams.owner_internal_uuid
-- - diagram_permissions.user_internal_uuid
-- - addon_invocations.invoked_by_internal_uuid
-- - addon_invocation_quotas.user_internal_uuid
-- Pattern: Use descriptive prefix + _internal_uuid for all foreign keys
```

**Migration Consolidation Steps**:

1. Archive existing migrations to `docs/archive/migrations/` for reference
2. Update `000001_core_infrastructure.up.sql` with consolidated schema
3. Update `000002_business_domain.up.sql` with new foreign key names
4. Create corresponding `.down.sql` files for both migrations
5. Document consolidation in `docs/archive/MIGRATION_CONSOLIDATION.md`
6. Drop and recreate development database
7. Drop and recreate Heroku database (using `make heroku-reset-db`)

**Result After Consolidation**:

- ✅ `users.internal_uuid`: Internal UUID (primary key)
- ✅ `users.provider`: OAuth provider name ("test", "google", etc.)
- ✅ `users.provider_user_id`: Provider's user ID (unique per provider)
- ✅ UNIQUE constraint on `(provider, provider_user_id)` enforces one user per provider account
- ✅ All foreign keys use descriptive names: `owner_internal_uuid`, `user_internal_uuid`
- ✅ OAuth tokens stored directly in users table
- ❌ NO `user_providers` table (eliminated entirely)

#### Step 2: Create User Lookup Cache

**New file**: `auth/user_cache.go`

```go
// UserCacheEntry represents cached user data for fast lookups
type UserCacheEntry struct {
    InternalUUID   uuid.UUID  // Internal database UUID (users.internal_uuid)
    Provider       string     // OAuth provider (users.provider)
    ProviderUserID string     // Provider-assigned user ID (users.provider_user_id)
    Email          string     // Email address (users.email)
    Name           string     // Display name (users.name)
}

type UserCache interface {
    // GetByProviderAndUserID looks up user by provider + provider user ID (from JWT)
    GetByProviderAndUserID(ctx context.Context, provider, providerUserID string) (*UserCacheEntry, error)

    // GetByInternalUUID looks up user by Internal UUID
    GetByInternalUUID(ctx context.Context, internalUUID uuid.UUID) (*UserCacheEntry, error)

    // Set caches a user entry
    Set(ctx context.Context, entry *UserCacheEntry) error

    // Invalidate removes cached user by provider + provider user ID
    Invalidate(ctx context.Context, provider, providerUserID string) error

    // InvalidateByUUID removes cached user by internal UUID
    InvalidateByUUID(ctx context.Context, internalUUID uuid.UUID) error
}

// Redis-backed implementation with TTL and database fallback
type redisUserCache struct {
    redis  RedisClient
    db     *sql.DB
    ttl    time.Duration  // Default: 15 minutes
}

// Redis keys use internal UUID for primary storage
func buildUserCacheKey(internalUUID uuid.UUID) string {
    return fmt.Sprintf("user:cache:%s", internalUUID.String())
}

// Index key for provider + provider_user_id lookups
func buildProviderIndexKey(provider, providerUserID string) string {
    return fmt.Sprintf("user:provider:%s:%s", provider, providerUserID)
}
```

**Cache Strategy**:

- **Primary Key**: Internal UUID (for user data storage)
- **Index Key**: `provider:provider_user_id` → Internal UUID (for lookups)
- **TTL**: 15 minutes (matches typical JWT refresh cycle)
- **Invalidation**: Manual on user profile updates
- **Fallback**: Direct database query if Redis unavailable or cache miss

#### Step 3: Update JWT Middleware with Profile Updates

**File**: `cmd/server/jwt_auth.go`

```go
// After JWT validation, extract all relevant claims
claims := token.Claims.(jwt.MapClaims)
providerUserID := claims["sub"].(string)  // Provider's user ID
jwtEmail := claims["email"].(string)      // Email from JWT
jwtName := claims["name"].(string)        // Name from JWT

// Determine provider from JWT issuer or custom claim
provider := determineProvider(claims)  // e.g., "test", "google", "github"

// Lookup user from cache/database
userEntry, err := userCache.GetByProviderAndUserID(ctx, provider, providerUserID)
if err != nil {
    c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
    return
}

// Check if email or name changed at provider - update database if so
if userEntry.Email != jwtEmail || userEntry.Name != jwtName {
    err = authService.UpdateUserProfile(ctx, userEntry.InternalUUID, jwtEmail, jwtName)
    if err != nil {
        slogging.Get().Warn("Failed to update user profile from JWT", "error", err)
        // Don't fail the request - continue with cached values
    } else {
        // Update cache entry with new values
        userEntry.Email = jwtEmail
        userEntry.Name = jwtName
        userCache.Set(ctx, userEntry)
    }
}

// Set user entry as single context object (cleaner than multiple keys)
c.Set("authenticatedUser", userEntry)

// REMOVED: Old separate context variables (userID, internalUUID, providerID, userEmail, userName)
```

**Benefits of Single Context Object**:

- ✅ Type-safe access to all user fields
- ✅ No risk of mismatched values between separate keys
- ✅ Cleaner handler code: `userEntry := c.MustGet("authenticatedUser").(*UserCacheEntry)`
- ✅ Automatic profile updates from JWT claims

#### Step 4: Update Handler Utilities

**File**: `api/auth_utils.go` or `api/request_utils.go`

```go
// GetAuthenticatedUser returns the cached user entry from context
func GetAuthenticatedUser(c *gin.Context) (*auth.UserCacheEntry, error) {
    userEntry, exists := c.Get("authenticatedUser")
    if !exists {
        return nil, errors.New("authentication required")
    }

    entry, ok := userEntry.(*auth.UserCacheEntry)
    if !ok {
        return nil, errors.New("invalid user context")
    }

    return entry, nil
}

// MustGetAuthenticatedUser returns the cached user entry or panics
// Use only in handlers where JWT middleware guarantees authentication
func MustGetAuthenticatedUser(c *gin.Context) *auth.UserCacheEntry {
    entry, err := GetAuthenticatedUser(c)
    if err != nil {
        panic(err)  // Should never happen after JWT middleware
    }
    return entry
}

// UserToAPIResponse converts UserCacheEntry to API User response
func UserToAPIResponse(entry *auth.UserCacheEntry) api.User {
    return api.User{
        Id:    entry.ProviderUserID,  // API exposes provider user ID
        Email: api.Email(entry.Email),
        Name:  entry.Name,
    }
}
```

**Usage in Handlers**:

```go
// Before: Multiple context lookups, type conversions, error handling
userID, _ := c.Get("userID").(string)
email, _ := c.Get("userEmail").(string)
// ... more lookups

// After: Single type-safe lookup
user := MustGetAuthenticatedUser(c)
// Access: user.InternalUUID, user.ProviderUserID, user.Email, user.Name, user.Provider
```

#### Step 5: Remove Duplicate Structures

**Remove or consolidate**:

- `authUserToAPIUser` converter - shouldn't be needed
- Multiple user representation structures

**Keep**:

- `api.User` (OpenAPI generated) - for API responses
- `auth.User` (full database model) - for auth service operations
- `UserCacheEntry` (new) - for efficient lookups

## Files Requiring Updates

### Critical Path (Must Fix)

1. **`cmd/server/jwt_auth.go`**

   - Change: Implement user lookup after JWT validation
   - Change: Set both `providerID` and `internalUUID` in context
   - Impact: All authenticated requests

2. **`auth/user_cache.go`** (NEW FILE)

   - Create: User cache implementation
   - Redis-backed with database fallback

3. **`api/request_utils.go`** or **`api/auth_utils.go`**

   - Change: Update `ValidateAuthenticatedUser` to return both IDs
   - Add: `GetAuthenticatedUser` helper
   - Add: `GetUserForAPI` helper

4. **`api/addon_invocation_handlers.go`**

   - Fix: Use `internalUUID` for rate limiting/quotas
   - Fix: Use `providerID`, `email`, `name` for invocation storage
   - Lines: ~42-85 (user ID extraction), ~146-164 (rate limiting), ~167-182 (invocation creation)

5. **`api/addon_rate_limiter.go`**

   - Verify: Already uses `uuid.UUID` correctly ✅
   - No changes needed (already correct)

6. **`api/addon_invocation_quota_store.go`**
   - Verify: Already uses `uuid.UUID` correctly ✅
   - No changes needed (already correct)

### Secondary Updates (Should Fix)

7. **`api/websocket.go`**

   - Fix: `toUser()` method (line 158-164)
   - Fix: Any user ID comparisons
   - Update: Use proper ID fields

8. **`api/asyncapi_types.go`**

   - Fix: User field references (`UserId` -> `Id`)
   - Fix: Validation methods
   - Lines: ~166, 184, 187, 204, 221, 408, 425, 554, 573

9. **`api/user_deletion_handlers.go`**

   - Review: User lookup patterns
   - Ensure: Uses Internal UUID for database operations

10. **`api/middleware.go`**
    - Review: Authorization checks
    - Ensure: Uses Internal UUID for database lookups

### Testing Files

11. **`api/*_test.go`**

    - Update: All test files that create users or mock authentication
    - Ensure: Tests use correct ID types

12. **`auth/*_test.go`**
    - Update: Auth service tests
    - Ensure: JWT tests verify Provider ID in `sub` claim

## Migration Strategy

**Since we haven't launched, we can do a complete atomic refactoring with no backwards compatibility needed.**

### Single-Phase Implementation (All-or-Nothing)

**IMPORTANT**: Since we're consolidating migrations AND eliminating the `user_providers` table, this is a fresh-start approach. All databases will be dropped and recreated.

1. **Migration Consolidation & Archive** (45 minutes)

   - Create `docs/archive/migrations/` directory
   - Copy all existing migrations to archive for reference
   - Create `docs/archive/MIGRATION_CONSOLIDATION.md` documenting what was consolidated
   - Review all schema changes from existing migrations
   - Consolidate into `000001_core_infrastructure.up.sql` and `000002_business_domain.up.sql`
   - **Key changes in 000001**:
     - Add `provider` column to `users` table
     - Rename `users.uuid` → `users.internal_uuid`
     - Rename `users.id` → `users.provider_user_id`
     - Add `users.identity_provider` field (REMOVED - redundant with `provider`)
     - Move `access_token`, `refresh_token`, `token_expiry` from `user_providers` to `users`
     - Add UNIQUE constraint on `(provider, provider_user_id)`
     - **ELIMINATE** `user_providers` table entirely
   - **Key changes in 000002**:
     - Rename all foreign keys: `*_uuid` → `*_internal_uuid` with descriptive prefixes
     - Examples: `owner_internal_uuid`, `user_internal_uuid`
   - Create corresponding `.down.sql` files
   - Delete migration files beyond 000002

2. **Database Recreation** (15 minutes)

   - Stop all local services: `make clean-everything`
   - Drop and recreate development database: `make start-dev`
   - Drop and recreate Heroku database: `make heroku-reset-db`
   - Verify schema with manual inspection

3. **Auth Package Update** (60 minutes)

   - Update `auth.User` struct with new fields:
     - `UUID` → `InternalUUID uuid.UUID` with tag `db:"internal_uuid"`
     - Add `Provider string` with tag `db:"provider"`
     - `ID` → `ProviderUserID string` with tag `db:"provider_user_id"`
     - Add `AccessToken`, `RefreshToken`, `TokenExpiry` (moved from user_providers)
     - **REMOVE** `IdentityProvider` field (redundant with `Provider`)
   - Update all SQL queries to:
     - Use new column names
     - Remove all JOINs with `user_providers` table
     - Use `(provider, provider_user_id)` for lookups instead of just `id`
   - Update `auth.Service` methods:
     - `GetUserByID()` → `GetUserByProviderAndUserID(provider, providerUserID)`
     - Update token storage to write directly to `users` table
     - Add `UpdateUserProfile(internalUUID, email, name)` for JWT updates
   - Remove all `user_providers` table queries
   - Run auth package unit tests: `make test-unit`

4. **Create User Cache** (60 minutes)

   - Implement `auth/user_cache.go` with `UserCacheEntry` struct
   - Fields: `InternalUUID`, `Provider`, `ProviderUserID`, `Email`, `Name`
   - Add Redis-backed cache with 15-minute TTL
   - Implement cache methods:
     - `GetByProviderAndUserID(provider, providerUserID)` - JWT lookups
     - `GetByInternalUUID(internalUUID)` - Internal lookups
     - `Set(entry)` - Cache storage
     - `Invalidate(provider, providerUserID)` - Clear by provider
     - `InvalidateByUUID(internalUUID)` - Clear by UUID
   - Redis key structure:
     - Primary: `user:cache:{internal_uuid}` (stores full UserCacheEntry)
     - Index: `user:provider:{provider}:{provider_user_id}` → `internal_uuid`
   - Database fallback for cache misses
   - Add cache initialization in server startup
   - Write cache unit tests

5. **Update JWT Middleware** (45 minutes)

   - Update `cmd/server/jwt_auth.go` to integrate user cache
   - Extract claims: `sub` (provider user ID), `email`, `name`
   - Determine provider from JWT (issuer or custom claim)
   - Lookup user: `userCache.GetByProviderAndUserID(provider, providerUserID)`
   - **Add profile update logic**:
     - Compare cached email/name with JWT email/name
     - If different, call `authService.UpdateUserProfile()`
     - Update cache with new values
     - Don't fail request if update fails (log warning only)
   - Set single context object: `c.Set("authenticatedUser", userEntry)`
   - **REMOVE** all old context variables: `userID`, `internalUUID`, `providerID`, etc.
   - Add error handling for user not found

6. **Create Handler Utilities** (30 minutes)

   - Create `api/auth_utils.go` with helper functions:
     - `GetAuthenticatedUser(c)` → `(*auth.UserCacheEntry, error)`
     - `MustGetAuthenticatedUser(c)` → `*auth.UserCacheEntry` (panic on error)
     - `UserToAPIResponse(entry)` → `api.User`
   - Remove old multi-value helper functions
   - Add comprehensive error handling and documentation

7. **Update All Handlers** (2-2.5 hours)

   - **Pattern**: Replace `c.Get("userID")` calls with `MustGetAuthenticatedUser(c)`
   - Update `api/addon_invocation_handlers.go`:
     - Use `user.InternalUUID` for rate limiting and quotas
     - Use `user.ProviderUserID`, `user.Email`, `user.Name` for invocation storage
     - Remove manual user ID extraction logic
   - Update `api/websocket.go`:
     - Fix `toUser()` method to use `ProviderUserID` in API responses
     - Update user comparisons to use correct ID types
   - Update `api/asyncapi_types.go`:
     - Fix all `UserId` → `Id` references (9 locations)
     - Update validation methods
   - Update `api/user_deletion_handlers.go`:
     - Use `user.InternalUUID` for database deletions
     - Use `user.Provider` and `user.ProviderUserID` for lookups
   - Update `api/middleware.go`:
     - Authorization checks use `user.InternalUUID` for database queries
     - Permission checks compare correct ID types
   - Update all other handlers that access user context
   - Fix all compilation errors as they arise

8. **Update All Tests** (1.5-2 hours)

   - Update test fixtures to use new `auth.User` field names:
     - `ID` → `ProviderUserID`
     - `UUID` → `InternalUUID`
     - Add `Provider` field to all test users
   - Update mock authentication:
     - Set `authenticatedUser` context with `*auth.UserCacheEntry`
     - Remove old context variable mocking
   - Fix test assertions:
     - Compare `user.ProviderUserID` for API responses (not `userID`)
     - Use `user.InternalUUID` for database queries
   - Update integration tests:
     - Verify new schema (no `user_providers` table)
     - Test `(provider, provider_user_id)` uniqueness constraint
     - Verify foreign keys use `*_internal_uuid` naming
   - Run test suite incrementally after each file update

9. **Update Type Converters** (20 minutes)

   - Update `authUserToAPIUser()`:
     - Use `u.ProviderUserID` instead of `u.ID` for API `id` field
     - Handle new struct field names
   - Verify `invocationToResponse()` uses correct field names
   - Remove any unnecessary converter functions
   - Add `UserCacheEntryToAPIUser()` converter

10. **Add Linter Rules** (15 minutes)

    - Add golangci-lint rules to prevent:
      - Variable names: `userID`, `userId` (enforce `internalUUID` or `providerUserID`)
      - Generic `id` variables without prefix
    - Document naming conventions in `CLAUDE.md`

11. **Final Verification** (60 minutes)
    - Run `make lint` and fix any issues
    - Run `make test-unit` - all tests must pass
    - Run `make test-integration` - all tests must pass
    - Verify Heroku database status (staging or production?)
    - Manual smoke testing:
      - OAuth login flow (test provider with login_hint)
      - Create threat model (verify owner_internal_uuid)
      - Invoke addon (verify rate limiting with InternalUUID)
      - Check rate limiting enforcement
      - Verify WebSocket collaboration
      - Test user profile updates via JWT claims
    - Deploy to Heroku:
      - Run `make heroku-reset-db` (with confirmation)
      - Deploy code changes
      - Test production OAuth flows
      - Verify cache hit rates in logs

**Total Estimated Time**: 7-8 hours for complete refactoring including:

- Migration consolidation and archival
- Table consolidation (eliminating user_providers)
- JWT profile update feature
- User cache implementation
- Complete code refactoring
- Comprehensive testing

**Rollback Plan**:

- Since databases are dropped/recreated, rollback means reverting code changes
- Keep git branch for rollback: `git checkout main` to undo
- No database rollback needed - just recreate from old migrations if needed
- Development and production are both fresh starts

## Validation Checklist

After implementation, verify:

### Database Schema

- [ ] `users` table has `internal_uuid`, `provider`, `provider_user_id` columns
- [ ] NO `user_providers` table exists
- [ ] UNIQUE constraint on `(provider, provider_user_id)` enforced
- [ ] All foreign keys use descriptive `*_internal_uuid` naming
- [ ] OAuth tokens stored in `users` table (`access_token`, `refresh_token`, `token_expiry`)

### JWT & Authentication

- [ ] JWT `sub` claim contains Provider User ID (never Internal UUID)
- [ ] JWT middleware extracts `email` and `name` claims
- [ ] Profile updates happen when JWT email/name differs from cached values
- [ ] Single `authenticatedUser` context object (no separate keys)
- [ ] User cache lookup uses `(provider, provider_user_id)` pair

### User Cache

- [ ] Cache hit rate > 90% (monitor in production)
- [ ] TTL set to 15 minutes
- [ ] Invalidation works for both provider lookup and UUID lookup
- [ ] Database fallback works when Redis unavailable
- [ ] Cache stores all 5 fields: `InternalUUID`, `Provider`, `ProviderUserID`, `Email`, `Name`

### Code Patterns

- [ ] All rate limiting uses `InternalUUID`
- [ ] All quota checks use `InternalUUID`
- [ ] All database foreign keys reference `internal_uuid`
- [ ] All API responses use `ProviderUserID` in `id` field
- [ ] All Redis keys use `InternalUUID` where appropriate
- [ ] Handlers use `MustGetAuthenticatedUser(c)` pattern
- [ ] No mixing of Provider User ID and Internal UUID
- [ ] No variables named `userID`, `userId`, or generic `id`

### Testing

- [ ] All unit tests pass (`make test-unit`)
- [ ] All integration tests pass (`make test-integration`)
- [ ] Tests verify `(provider, provider_user_id)` uniqueness
- [ ] Tests verify profile updates from JWT claims
- [ ] Tests cover all ID types correctly
- [ ] Mock authentication uses `UserCacheEntry` object

### Documentation & Code Quality

- [ ] Linter rules prevent ambiguous variable names
- [ ] `make lint` passes with no warnings
- [ ] Naming conventions documented in `CLAUDE.md`
- [ ] Migration consolidation documented in `docs/archive/MIGRATION_CONSOLIDATION.md`
- [ ] Old migrations archived in `docs/archive/migrations/`

## Known Issues to Fix

1. ❌ `api/addon_invocation_handlers.go:270` - Compares `InvokedBy` (removed field)
2. ❌ `api/addon_invocation_handlers.go:272` - References `InvokedBy` (removed field)
3. ❌ `api/asyncapi_types.go` - Multiple references to `UserId` (should be `Id`)
4. ❌ `api/websocket.go:160` - Returns `User{UserId: ...}` (should be `Id`)
5. ⚠️ Context variable `userID` is ambiguous throughout codebase

## Success Criteria

Implementation is complete when:

1. ✅ All compilation errors resolved
2. ✅ All unit tests pass (`make test-unit`)
3. ✅ All integration tests pass (`make test-integration`)
4. ✅ Linting passes (`make lint`)
5. ✅ Database schema matches specification (no `user_providers` table)
6. ✅ No mixing of Provider User ID and Internal UUID
7. ✅ User cache hit rate > 90% in production
8. ✅ No duplicate user lookups in request path
9. ✅ Clear naming conventions followed everywhere (no `userID` variables)
10. ✅ JWT profile updates working (email/name sync)
11. ✅ Single context object pattern (`authenticatedUser`) used throughout
12. ✅ All validation checklist items completed
13. ✅ Documentation updated and migrations archived
14. ✅ Heroku deployment successful with new schema

## Summary

This refactoring addresses fundamental architectural confusion around user identification by:

1. **Consolidating Tables**: Merging `user_providers` into `users` to match business logic
2. **Standardizing Naming**: Using clear, prefix-based names (`internal_uuid`, `provider`, `provider_user_id`)
3. **Improving Performance**: Redis-backed user cache reduces database lookups by >90%
4. **Adding Profile Updates**: JWT middleware keeps email/name synchronized with provider
5. **Simplifying Code**: Single `UserCacheEntry` context object replaces multiple keys
6. **Preventing Bugs**: Type-safe patterns and linter rules prevent ID confusion

**Estimated Effort**: 7-8 hours for complete implementation and testing

**Risk Level**: Low (pre-launch, can drop/recreate databases without user impact)

**Benefits**:

- Clearer, more maintainable codebase
- Better performance (no joins, cache hits)
- Automatic profile synchronization
- Foundation for future OAuth provider additions
- Eliminates entire class of ID confusion bugs
