# User and Group Management APIs - Implementation Plan

## Executive Summary

This plan outlines the design and implementation of admin-level user and group management APIs for TMI, with SAML-specific UI-driving endpoints that respect provider boundaries.

## Current State Analysis

### Existing Infrastructure

**Users Table (auth/migrations/001_core_infrastructure.up.sql)**:
- `internal_uuid` (UUID, PK)
- `provider` (text) - OAuth/SAML provider
- `provider_user_id` (text) - Provider's user ID
- `email` (text)
- `name` (text) - Display name
- `email_verified` (boolean)
- `created_at`, `modified_at`, `last_login` (timestamps)

**Groups Table (auth/migrations/002_business_domain.up.sql)**:
- `internal_uuid` (UUID, PK)
- `provider` (text) - Identity provider or "*" for provider-independent
- `group_name` (text) - Group identifier
- `name` (text) - Display name
- `description` (text)
- `first_used`, `last_used` (timestamps)
- `usage_count` (integer)
- `UNIQUE(provider, group_name)`

**Existing Patterns**:
- Administrator management in `api/administrator_handlers.go`
- Database-backed stores with filtering (e.g., `AdministratorDatabaseStore`)
- Enrichment pattern for adding related data (emails, group names)
- Admin-only middleware (`administrator_middleware.go`)
- Provider-based filtering and scoping

### Existing Endpoints

**Admin APIs**:
- `GET /admin/administrators` - List admin grants
- `POST /admin/administrators` - Create admin grant
- `DELETE /admin/administrators/{id}` - Remove admin grant
- `GET/PUT /admin/quotas/users/{user_id}` - User quota management
- `GET/PUT /admin/quotas/webhooks/{user_id}` - Webhook quota management

**Provider Groups (UI-Driven)**:
- `GET /oauth2/providers/{idp}/groups` - Get groups from IdP (currently no auth check)

**User Endpoints**:
- `GET /me` - Current user profile
- `GET /oauth2/userinfo` - OIDC userinfo

## Requirements

### Admin-Only APIs (Cross-Provider)

1. **User Management**:
   - List all users across all providers with filtering/pagination
   - Get user by internal_uuid
   - Update user metadata (name, email_verified, etc.)
   - Delete/deactivate user
   - Filter by provider, email pattern, last login date

2. **Group Management**:
   - List all groups across all providers with filtering/pagination
   - Get group by internal_uuid
   - Create provider-independent groups (provider="*")
   - Update group metadata (name, description)
   - Delete groups (if not referenced in authorizations/admin grants)
   - Filter by provider, name pattern, usage

### SAML UI-Driven APIs (Provider-Scoped)

3. **SAML User Listing**:
   - List users for a specific SAML provider
   - Only accessible by users authenticated with that same SAML provider
   - Support filtering and pagination
   - Return user subset suitable for autocomplete/selection UIs

4. **SAML Group Listing** (Enhancement):
   - Enhance existing `GET /oauth2/providers/{idp}/groups`
   - Add authorization check: only users authenticated with the same provider
   - Return groups from the database (not just cache)
   - Include usage metadata

## Proposed API Design

### Admin User Management

#### List Users (Admin)
```
GET /admin/users
Query Parameters:
  - provider (string, optional) - Filter by provider
  - email (string, optional) - Filter by email pattern (case-insensitive contains)
  - created_after (ISO8601, optional) - Filter by creation date
  - created_before (ISO8601, optional)
  - last_login_after (ISO8601, optional)
  - last_login_before (ISO8601, optional)
  - limit (integer, default: 50, max: 200)
  - offset (integer, default: 0)
  - sort_by (string, optional) - Field to sort by: created_at, last_login, email
  - sort_order (string, optional) - asc or desc (default: desc)

Response 200:
{
  "users": [
    {
      "internal_uuid": "uuid",
      "provider": "saml_okta",
      "provider_user_id": "user@example.com",
      "email": "user@example.com",
      "name": "User Name",
      "email_verified": true,
      "created_at": "2025-01-15T12:00:00Z",
      "modified_at": "2025-01-15T12:00:00Z",
      "last_login": "2025-01-20T10:30:00Z",
      "is_admin": false
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}

Security: Admin only (x-admin-only: true)
```

#### Get User (Admin)
```
GET /admin/users/{internal_uuid}

Response 200:
{
  "internal_uuid": "uuid",
  "provider": "saml_okta",
  "provider_user_id": "user@example.com",
  "email": "user@example.com",
  "name": "User Name",
  "email_verified": true,
  "created_at": "2025-01-15T12:00:00Z",
  "modified_at": "2025-01-15T12:00:00Z",
  "last_login": "2025-01-20T10:30:00Z",
  "is_admin": false,
  "groups": ["engineering", "security"],
  "active_threat_models": 5,
  "administrator_grants": [
    {
      "id": "uuid",
      "provider": "saml_okta",
      "granted_at": "2025-01-15T12:00:00Z",
      "granted_by": "uuid"
    }
  ]
}

Response 404: User not found

Security: Admin only (x-admin-only: true)
```

#### Update User (Admin)
```
PATCH /admin/users/{internal_uuid}

Request Body:
{
  "email": "newemail@example.com",      // optional
  "name": "New Name",                   // optional
  "email_verified": true                // optional
}

Response 200: Updated user object (same as GET)
Response 400: Invalid request body
Response 404: User not found
Response 409: Email already exists (if changing email)

Security: Admin only (x-admin-only: true)
Notes:
  - Cannot change provider or provider_user_id (identity fields)
  - Email changes must maintain uniqueness per provider
```

#### Delete User (Admin)
```
DELETE /admin/users?provider={provider}&provider_id={provider_id}

Query Parameters:
  - provider (string, required) - Identity provider (e.g., "saml_okta", "google")
  - provider_id (string, required) - Provider's user ID

Response 204: User deleted successfully
Response 400: Missing or invalid parameters
Response 404: User not found

Security: Admin only (x-admin-only: true)

Deletion Algorithm (same as DELETE /me):
  1. Begin transaction
  2. For each owned threat model:
     - Find alternate owner (another user with 'owner' role)
     - If alternate owner exists: Transfer ownership + remove deleting user's permissions
     - If no alternate owner: Delete threat model (CASCADE deletes diagrams, threats, etc.)
  3. Delete remaining permissions (reader/writer on other threat models)
  4. Delete user record (CASCADE deletes user_providers, sessions, etc.)
  5. Commit transaction
  6. Audit log deletion with statistics (transferred count, deleted count)

Notes:
  - Hard deletion with transactional integrity
  - Automatic ownership transfer when possible
  - Uses provider + provider_id for identification (not internal_uuid)
  - Returns deletion statistics in audit logs
  - No challenge required (admin operation)
```

### Admin Group Management

#### List Groups (Admin)
```
GET /admin/groups
Query Parameters:
  - provider (string, optional) - Filter by provider ("*" for provider-independent)
  - name (string, optional) - Filter by name pattern (case-insensitive contains)
  - used_in_authorizations (boolean, optional) - Filter by usage
  - limit (integer, default: 50, max: 200)
  - offset (integer, default: 0)
  - sort_by (string, optional) - Field to sort by: group_name, first_used, last_used, usage_count
  - sort_order (string, optional) - asc or desc (default: desc)

Response 200:
{
  "groups": [
    {
      "internal_uuid": "uuid",
      "provider": "saml_okta",
      "group_name": "engineering",
      "name": "Engineering Team",
      "description": "Engineering team members",
      "first_used": "2025-01-15T12:00:00Z",
      "last_used": "2025-01-20T10:30:00Z",
      "usage_count": 42,
      "used_in_authorizations": true,
      "used_in_admin_grants": false
    }
  ],
  "total": 75,
  "limit": 50,
  "offset": 0
}

Security: Admin only (x-admin-only: true)
```

#### Get Group (Admin)
```
GET /admin/groups/{internal_uuid}

Response 200:
{
  "internal_uuid": "uuid",
  "provider": "saml_okta",
  "group_name": "engineering",
  "name": "Engineering Team",
  "description": "Engineering team members",
  "first_used": "2025-01-15T12:00:00Z",
  "last_used": "2025-01-20T10:30:00Z",
  "usage_count": 42,
  "used_in_authorizations": true,
  "used_in_admin_grants": false,
  "authorizations": [
    {
      "threat_model_id": "uuid",
      "threat_model_name": "Payment System Threat Model",
      "role": "reader"
    }
  ],
  "member_count": 15  // If available from IdP
}

Response 404: Group not found

Security: Admin only (x-admin-only: true)
```

#### Create Group (Admin)
```
POST /admin/groups

Request Body:
{
  "provider": "*",                      // required (use "*" for provider-independent)
  "group_name": "security-team",        // required
  "name": "Security Team",              // optional
  "description": "Security team members" // optional
}

Response 201: Created group object
Response 400: Invalid request body or duplicate group
Response 409: Group already exists for provider

Security: Admin only (x-admin-only: true)
Notes:
  - Primary use case: provider-independent groups (provider="*")
  - Provider-specific groups typically come from IdP claims
  - UNIQUE constraint: (provider, group_name)
```

#### Update Group (Admin)
```
PATCH /admin/groups/{internal_uuid}

Request Body:
{
  "name": "Security & Compliance Team",  // optional
  "description": "Updated description"   // optional
}

Response 200: Updated group object
Response 400: Invalid request body
Response 404: Group not found

Security: Admin only (x-admin-only: true)
Notes:
  - Cannot change provider or group_name (identity fields)
  - Primarily for updating metadata of provider-independent groups
```

#### Delete Group (Admin)
```
DELETE /admin/groups?provider={provider}&group_name={group_name}

Query Parameters:
  - provider (string, required) - Identity provider (e.g., "saml_okta", "*")
  - group_name (string, required) - Group identifier

Response 501: Not Implemented (placeholder)

Security: Admin only (x-admin-only: true)

Notes:
  - Placeholder endpoint - returns 501 Not Implemented
  - Future implementation will handle:
    - Check threat_model_access table (ON DELETE CASCADE would auto-remove)
    - Check administrators table (ON DELETE CASCADE would auto-remove)
    - Audit log the deletion
  - Uses provider + group_name for identification (not internal_uuid)
  - Deferred to later implementation phase
```

### SAML UI-Driven APIs (Provider-Scoped)

#### List Users for SAML Provider
```
GET /saml/providers/{idp}/users
Query Parameters:
  - email (string, optional) - Filter by email pattern
  - limit (integer, default: 100, max: 500)
  - offset (integer, default: 0)

Response 200:
{
  "idp": "saml_okta",
  "users": [
    {
      "internal_uuid": "uuid",
      "email": "user@example.com",
      "name": "User Name",
      "last_login": "2025-01-20T10:30:00Z"
    }
  ],
  "total": 42
}

Response 401: Unauthorized
Response 403: Forbidden - User not authenticated with this provider
Response 404: Provider not found

Security:
  - Requires authentication (bearerAuth)
  - User must be authenticated with the same SAML provider
  - Check: JWT idp claim == path parameter {idp}
  - Only SAML providers (not OAuth)

Notes:
  - Lightweight response for autocomplete/selection UIs
  - Only returns users from the same SAML provider
  - Only returns active users (no deactivated/deleted users)
  - No admin privileges required
```

#### List Groups for Provider (Enhanced)
```
GET /oauth2/providers/{idp}/groups
(Existing endpoint - add authorization)

Current Implementation:
  - No authorization check
  - Returns groups from cache only
  - Limited metadata

Proposed Enhancements:
  1. Add authorization check:
     - User must be authenticated with the same provider (OAuth or SAML)
     - Check: JWT idp claim == path parameter {idp}
  2. Query database groups table (not just cache)
  3. Include all metadata from groups table
  4. Add pagination support

Response 200: (Keep existing format, enhance data)
{
  "idp": "saml_okta",
  "groups": [
    {
      "name": "engineering",
      "display_name": "Engineering Team",
      "used_in_authorizations": true,
      "description": "Engineering team members",  // NEW
      "last_used": "2025-01-20T10:30:00Z"        // NEW
    }
  ]
}

Response 401: Unauthorized
Response 403: Forbidden - User not authenticated with this provider
Response 404: Provider not found

Security:
  - Requires authentication (bearerAuth)
  - User must be authenticated with the same provider
  - Check: JWT idp claim == path parameter {idp}
```

## Implementation Plan

### Phase 1: Database Layer (Week 1)

**Files to Create**:
1. `api/user_store.go` - User store interface
2. `api/user_database_store.go` - Database implementation with filtering
3. `api/group_store.go` - Group store interface
4. `api/group_database_store.go` - Database implementation with filtering

**Key Features**:
- Generic Store[T] pattern for consistency with existing code
- Filter structs for complex queries (UserFilter, GroupFilter)
- Enrichment methods for related data
- Transaction support for cascading operations
- Usage tracking (threat model counts, authorization counts)

**Example Filter Struct**:
```go
type UserFilter struct {
    Provider        string
    Email           string  // Case-insensitive ILIKE %email%
    CreatedAfter    *time.Time
    CreatedBefore   *time.Time
    LastLoginAfter  *time.Time
    LastLoginBefore *time.Time
    Limit           int
    Offset          int
    SortBy          string  // created_at, last_login, email
    SortOrder       string  // asc, desc
}

type GroupFilter struct {
    Provider              string
    GroupName             string  // Case-insensitive ILIKE %name%
    UsedInAuthorizations  *bool
    Limit                 int
    Offset                int
    SortBy                string  // group_name, first_used, last_used, usage_count
    SortOrder             string  // asc, desc
}
```

### Phase 2: Admin Handlers (Week 1-2)

**Files to Create**:
1. `api/admin_user_handlers.go` - Admin user CRUD handlers
2. `api/admin_group_handlers.go` - Admin group CRUD handlers

**Pattern to Follow**:
- Copy structure from `administrator_handlers.go`
- Use database store with filtering
- Enrichment for related data (groups, threat model counts)
- Comprehensive error handling with RequestError
- Audit logging for all mutations
- User deletion: Delegate to `auth.Service.DeleteUserAndData()` (same as /me)
  - Requires provider + provider_id lookup to get user email
  - Returns deletion statistics (transferred/deleted counts)
- Group deletion: Return 501 Not Implemented (placeholder)

**Validation**:
- Email format validation
- Provider validation (must exist)
- Uniqueness checks (email per provider, group_name per provider)
- Foreign key constraint checks

### Phase 3: SAML UI Handlers (Week 2)

**Files to Create/Modify**:
1. `api/saml_user_handlers.go` - SAML-scoped user listing
2. `api/server.go` - Enhance GetProviderGroups with auth check

**Authorization Middleware**:
```go
// SameProviderMiddleware ensures user is authenticated with the specified provider
func SameProviderMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Get IdP from path parameter
        idp := c.Param("idp")

        // Get user's IdP from JWT claims
        userIdP := c.GetString("identityProvider")

        if userIdP != idp {
            HandleRequestError(c, &RequestError{
                Status:  http.StatusForbidden,
                Code:    "provider_mismatch",
                Message: "Can only access resources for your own provider",
            })
            c.Abort()
            return
        }

        c.Next()
    }
}

// SAMLProviderOnlyMiddleware ensures the provider is a SAML provider
func SAMLProviderOnlyMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        idp := c.Param("idp")

        // Check if provider starts with "saml_"
        if !strings.HasPrefix(idp, "saml_") {
            HandleRequestError(c, &RequestError{
                Status:  http.StatusBadRequest,
                Code:    "invalid_provider_type",
                Message: "This endpoint only supports SAML providers",
            })
            c.Abort()
            return
        }

        c.Next()
    }
}
```

### Phase 4: OpenAPI Specification (Week 2)

**File to Modify**:
- `docs/reference/apis/tmi-openapi.json`

**New Paths**:
- `/admin/users` (GET, POST if needed)
- `/admin/users/{internal_uuid}` (GET, PATCH, DELETE)
- `/admin/groups` (GET, POST)
- `/admin/groups/{internal_uuid}` (GET, PATCH, DELETE)
- `/saml/providers/{idp}/users` (GET)

**Enhanced Paths**:
- `/oauth2/providers/{idp}/groups` (add security requirement and 403 response)

**New Schemas**:
- `User` - Complete user object
- `UserListResponse`
- `UserFilter` (as query parameters)
- `UpdateUserRequest`
- `Group` - Complete group object
- `GroupListResponse`
- `GroupFilter` (as query parameters)
- `CreateGroupRequest`
- `UpdateGroupRequest`
- `SAMLUserListResponse`

**Security Markers**:
- Admin endpoints: `x-admin-only: true`
- SAML endpoints: `x-same-provider-required: true`
- Rate limiting tier: `resource-operations`

### Phase 5: Code Generation (Week 2)

**Commands**:
```bash
make generate-api
make lint
make build-server
```

**Files Updated**:
- `api/api.go` - Generated types and interfaces

### Phase 6: Testing (Week 3)

**Unit Tests**:
1. `api/user_database_store_test.go`
2. `api/group_database_store_test.go`
3. `api/admin_user_handlers_test.go`
4. `api/admin_group_handlers_test.go`
5. `api/saml_user_handlers_test.go`

**Integration Tests**:
1. Test filtering and pagination
2. Test authorization checks (admin vs non-admin)
3. Test provider scoping (SAML endpoints)
4. Test cascading deletes
5. Test duplicate detection
6. Test enrichment queries

**Test Scenarios**:
- Admin lists all users across providers
- Admin filters users by provider, date ranges
- Admin updates user email (with validation)
- Admin attempts to delete user with owned threat models (should fail)
- SAML user lists users from their own provider (success)
- SAML user attempts to list users from different provider (403)
- OAuth user attempts to access SAML-only endpoint (403)
- Provider groups enhanced endpoint with same-provider check

### Phase 7: Documentation (Week 3)

**Files to Create/Update**:
1. `docs/developer/integration/admin-api-guide.md` - Admin API usage guide
2. `docs/developer/integration/saml-ui-integration.md` - SAML UI integration patterns
3. `docs/reference/apis/user-group-management.md` - Complete API reference
4. `CLAUDE.md` - Update with new commands and patterns

## Security Considerations

### Admin-Only Endpoints

1. **Authorization**: Use existing `AdministratorMiddleware`
2. **Audit Logging**: Log all admin operations with actor details
3. **Self-Protection**: Prevent admins from removing their own access
4. **Cascading Effects**: Warn or prevent deletions with dependencies

### SAML UI Endpoints

1. **Provider Scoping**: Strict JWT idp claim validation
2. **SAML-Only**: Reject OAuth providers for SAML-specific endpoints
3. **Rate Limiting**: Apply standard resource-operations tier limits
4. **Information Disclosure**: Only return minimal user data (no tokens)

### Data Protection

1. **PII Handling**: Never return tokens, sensitive auth data
2. **Filtering**: Sanitize email/name patterns to prevent SQL injection
3. **Pagination**: Enforce maximum limits to prevent DoS
4. **Enrichment**: Gracefully handle missing related data

## Migration Considerations

**Database Changes**: None required - all tables already exist

**Existing Endpoint Changes**:
- `GET /oauth2/providers/{idp}/groups` - Add authorization check
  - **Breaking Change**: Users without valid provider auth will get 403
  - **Mitigation**: Document in release notes, provide migration guide
  - **Timeline**: Give 1 release cycle warning

## Performance Considerations

1. **Indexing**: Verify indexes on users and groups tables
   - `users(provider, email)`
   - `users(created_at)`, `users(last_login)`
   - `groups(provider, group_name)` (already UNIQUE)
   - `groups(last_used)`

2. **Pagination**: Always enforce limit caps to prevent large result sets

3. **Enrichment**: Consider caching for frequently accessed data
   - Group usage counts
   - User threat model counts
   - Administrator grant lookups

4. **Database Queries**:
   - Use prepared statements for all filters
   - Optimize JOIN operations for enrichment
   - Consider using COUNT(*) OVER() for total counts

## Success Criteria

1. ✅ Admin can list, view, update, and delete users across all providers
2. ✅ Admin can list, view, create, update, and delete groups
3. ✅ SAML users can list users from their own provider for UI autocomplete
4. ✅ SAML/OAuth users can list groups from their own provider
5. ✅ Provider boundary enforcement prevents cross-provider access
6. ✅ All operations are properly audited
7. ✅ API documentation is complete with examples
8. ✅ Integration tests cover authorization scenarios
9. ✅ Performance is acceptable (< 500ms for list endpoints with 100 items)

## Design Decisions (Confirmed)

1. **User Deletion**: ✅ Hard delete using same algorithm as DELETE /me
   - Transactional ownership transfer where possible
   - Cascade delete threat models without alternate owners
   - Clean up all permissions and references
   - No challenge required (admin operation)
   - Identified by provider + provider_id (not internal_uuid)

2. **Group Deletion**: ✅ Placeholder returning 501 Not Implemented
   - Endpoint defined in OpenAPI but not implemented
   - Deferred to later phase
   - Identified by provider + group_name (not internal_uuid)

3. **Email Uniqueness**: ✅ Per-provider (current behavior)
   - UNIQUE constraint on (provider, provider_user_id)
   - Supports same user across multiple IdPs

4. **SAML User Listing**: ✅ Only active users
   - UI endpoints show active users only
   - Admin endpoints have full visibility

5. **Enhanced Groups Endpoint**: ✅ Add authorization check immediately
   - No backwards compatibility needed
   - Document in release notes

6. **Pagination Defaults**: ✅ Confirmed
   - Admin: Default 50, max 200
   - SAML UI: Default 100, max 500

7. **Provider-Independent Groups**: ✅ Admin-only management
   - Only admins can create/modify provider="*" groups
   - Global scope requires elevated privileges

## Timeline

- **Week 1**: Database layer (stores) + Admin user handlers
- **Week 2**: Admin group handlers + SAML UI handlers + OpenAPI spec + Code generation
- **Week 3**: Testing + Documentation + Integration validation

**Total Estimated Effort**: 3 weeks for complete implementation

## Next Steps

1. Review this plan with stakeholders
2. Answer open questions
3. Create detailed task breakdown in TodoWrite
4. Begin Phase 1 implementation
