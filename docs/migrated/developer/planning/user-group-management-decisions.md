# User and Group Management APIs - Design Decisions

## Date: 2025-12-01

## Overview

This document captures the confirmed design decisions for implementing user and group management APIs in TMI.

## Confirmed Decisions

### 1. User Deletion Strategy

**Decision**: Hard delete using the same algorithm as `DELETE /me`

**Implementation**:
- Endpoint: `DELETE /admin/users?provider={provider}&provider_id={provider_id}`
- Delegate to existing `auth.Service.DeleteUserAndData()` method
- No challenge required (admin operation, not self-service)

**Deletion Algorithm**:
1. Begin transaction
2. For each owned threat model:
   - Find alternate owner (another user with 'owner' role)
   - If alternate owner exists: Transfer ownership + remove deleting user's permissions
   - If no alternate owner: Delete threat model (CASCADE deletes diagrams, threats, etc.)
3. Delete remaining permissions (reader/writer on other threat models)
4. Delete user record (CASCADE deletes user_providers, sessions, etc.)
5. Commit transaction
6. Audit log deletion with statistics (transferred count, deleted count)

**Rationale**:
- Maintains data consistency with existing self-deletion flow
- Transactional integrity ensures no orphaned resources
- Automatic ownership transfer preserves collaborative work
- Hard deletion complies with data minimization principles

**Reference Code**: `auth/user_deletion.go` (DeleteUserAndData method at line 102)

### 2. Group Deletion Strategy

**Decision**: Placeholder endpoint returning 501 Not Implemented

**Implementation**:
- Endpoint: `DELETE /admin/groups?provider={provider}&group_name={group_name}`
- Returns HTTP 501 Not Implemented
- Defined in OpenAPI specification but not implemented
- Deferred to later implementation phase

**Rationale**:
- Group deletion has complex cascade implications (authorizations, admin grants)
- Defer until usage patterns are better understood
- Allows API surface to be defined now, implementation later
- No immediate user requirement for group deletion

### 3. User Identification in APIs

**Decision**: Use provider + provider_id (not internal_uuid)

**Endpoints**:
- `DELETE /admin/users?provider={provider}&provider_id={provider_id}`
- `DELETE /admin/groups?provider={provider}&group_name={group_name}`

**Rationale**:
- Provider + provider_id are the natural keys from identity providers
- Admins think in terms of "user@domain.com from Okta" not UUIDs
- Aligns with how users are referenced in external systems
- Internal UUIDs remain implementation detail

**Query Flow**:
1. Accept provider + provider_id in request
2. Query database to get internal_uuid and email
3. Perform operations using internal_uuid
4. Return results referencing provider + provider_id

### 4. Email Uniqueness

**Decision**: Per-provider (maintain current behavior)

**Database Constraint**: `UNIQUE(provider, provider_user_id)` and `UNIQUE(provider, email)`

**Rationale**:
- Same person can have accounts in multiple providers
- Example: alice@company.com in both Google OAuth and Okta SAML
- Each provider maintains its own user namespace
- Aligns with federated identity model

**Implications**:
- User listing must filter by provider for uniqueness
- Cross-provider user search requires explicit provider scoping
- Admin APIs return provider field in all user responses

### 5. SAML User Listing Scope

**Decision**: Only return active users

**Endpoint**: `GET /saml/providers/{idp}/users`

**Filtering**:
- Active users only (exclude deleted/deactivated)
- From specified SAML provider only
- Authenticated user must be from same provider

**Rationale**:
- UI autocomplete needs current, active users only
- Deleted users clutter selection interfaces
- Security: prevents information leakage about past users
- Admin endpoints have full visibility if needed

### 6. Enhanced Groups Endpoint Authorization

**Decision**: Add same-provider authorization check immediately

**Endpoint**: `GET /oauth2/providers/{idp}/groups` (existing)

**Enhancement**:
- Add authorization middleware: JWT idp claim must match path parameter
- Query database groups table (not just Redis cache)
- Include full metadata from groups table
- Add pagination support

**Breaking Change**: YES
- Users not authenticated with the provider will receive 403 Forbidden
- Previously: No authorization check (open to all authenticated users)
- Mitigation: Document in release notes, acceptable breaking change

**Rationale**:
- Prevents cross-provider information leakage
- Aligns with same-provider security model for SAML endpoints
- No backwards compatibility requirement confirmed by stakeholder
- Security improvement outweighs compatibility concerns

### 7. Pagination Limits

**Admin Endpoints**:
- Default: 50 items per page
- Maximum: 200 items per page

**SAML UI Endpoints**:
- Default: 100 items per page
- Maximum: 500 items per page

**Rationale**:
- Admin endpoints: Comprehensive views, moderate pagination
- UI endpoints: Responsive autocomplete, larger sets acceptable
- Prevents unbounded queries and DoS
- Balances performance with usability

### 8. Provider-Independent Groups

**Decision**: Admin-only creation and modification

**Implementation**:
- Only admins can create groups with `provider="*"`
- Admin middleware required for `POST /admin/groups`
- Provider-specific groups typically from IdP claims

**Rationale**:
- Provider-independent groups have global scope
- Cross-provider access requires elevated privileges
- Prevents privilege escalation through group creation
- Clear separation: IdP groups vs. system groups

## API Summary

### Admin APIs (Cross-Provider, Admin-Only)

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/admin/users` | GET | List all users with filtering | Implemented |
| `/admin/users/{internal_uuid}` | GET | Get user details | Implemented |
| `/admin/users/{internal_uuid}` | PATCH | Update user metadata | Implemented |
| `/admin/users` | DELETE | Delete user (by provider + provider_id) | Implemented |
| `/admin/groups` | GET | List all groups with filtering | Implemented |
| `/admin/groups/{internal_uuid}` | GET | Get group details | Implemented |
| `/admin/groups` | POST | Create provider-independent group | Implemented |
| `/admin/groups/{internal_uuid}` | PATCH | Update group metadata | Implemented |
| `/admin/groups` | DELETE | Delete group (501 placeholder) | Placeholder |

### SAML UI APIs (Provider-Scoped, Same-Provider Auth)

| Endpoint | Method | Purpose | Status |
|----------|--------|---------|--------|
| `/saml/providers/{idp}/users` | GET | List users from caller's SAML provider | Implemented |
| `/oauth2/providers/{idp}/groups` | GET | List groups from caller's provider (enhanced) | Implemented |

## Security Model

### Authorization Levels

1. **Admin-Only** (`x-admin-only: true`):
   - All `/admin/*` endpoints
   - Uses existing `AdministratorMiddleware`
   - Global scope across all providers

2. **Same-Provider** (`x-same-provider-required: true`):
   - `/saml/providers/{idp}/users`
   - `/oauth2/providers/{idp}/groups` (enhanced)
   - Uses `SameProviderMiddleware`
   - Validates JWT idp claim matches path parameter

3. **SAML-Only**:
   - `/saml/providers/{idp}/users`
   - Uses `SAMLProviderOnlyMiddleware`
   - Rejects OAuth providers (must start with "saml_")

### Audit Logging

All admin operations log:
- Operation type (create, update, delete)
- Target resource (user email, group name)
- Actor details (admin user email, internal_uuid)
- Timestamp and request ID
- For deletions: Statistics (threat models transferred/deleted)

## Database Schema

### Users Table (Existing)
```sql
CREATE TABLE users (
    internal_uuid UUID PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    email TEXT NOT NULL,
    name TEXT,
    email_verified BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMPTZ,
    UNIQUE NULLS NOT DISTINCT (provider, provider_user_id),
    UNIQUE(provider, email)
);
```

### Groups Table (Existing)
```sql
CREATE TABLE groups (
    internal_uuid UUID PRIMARY KEY,
    provider TEXT NOT NULL,           -- IdP or "*" for provider-independent
    group_name TEXT NOT NULL,         -- Group identifier
    name TEXT,                        -- Display name
    description TEXT,
    first_used TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    last_used TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    usage_count INTEGER DEFAULT 1,
    UNIQUE(provider, group_name)
);
```

**No schema changes required** - all tables exist and are ready for use.

## Implementation Status

All phases have been completed:

### Completed: Database Layer
- User store with filtering (`api/user_store.go`)
- Group store with filtering (`api/group_store.go`)
- Filter structs: `UserFilter`, `GroupFilter`
- Enrichment methods for related data

### Completed: Admin Handlers
- Admin user handlers (`api/admin_user_handlers.go`)
- Admin group handlers (`api/admin_group_handlers.go`)
- User deletion: Delegates to `auth.Service.DeleteUserAndData()`
- Group deletion: Returns 501 Not Implemented (placeholder)

### Completed: SAML UI Handlers
- SAML user listing (`api/saml_user_handlers.go`)
- Enhanced provider groups in server
- Authorization middleware (`SameProviderMiddleware`, `SAMLProviderOnlyMiddleware`) in `api/provider_auth_middleware.go`

### Completed: OpenAPI Specification
- All endpoints defined in `docs/reference/apis/tmi-openapi.json`
- New schemas: User, Group, ListResponses, Filters, Requests
- Security markers: `x-admin-only`, `x-same-provider-required`

### Completed: Code Generation
- Generated types in `api/api.go`

## References

- Main Plan: `docs/developer/planning/user-group-management-apis.md`
- Deletion Algorithm: `auth/user_deletion.go` (DeleteUserAndData method)
- Deletion Repository: `auth/repository/deletion_repository.go` (implementation details)
- Admin Pattern: `api/administrator_handlers.go`
- Middleware Pattern: `api/administrator_middleware.go`
- Provider Auth Middleware: `api/provider_auth_middleware.go`
- Migration Files: `docs/reference/legacy-migrations/001_core_infrastructure.up.sql`, `002_business_domain.up.sql`

---

## Verification Summary

**Document verified on 2026-01-24**

### File References Verified
- `auth/user_deletion.go` - EXISTS, DeleteUserAndData at line 102 (not 103 as originally stated)
- `docs/developer/planning/user-group-management-apis.md` - EXISTS
- `api/administrator_handlers.go` - EXISTS
- `api/administrator_middleware.go` - EXISTS
- `api/user_store.go` - EXISTS
- `api/group_store.go` - EXISTS
- `api/admin_user_handlers.go` - EXISTS
- `api/admin_group_handlers.go` - EXISTS
- `api/saml_user_handlers.go` - EXISTS
- `api/provider_auth_middleware.go` - EXISTS (contains SameProviderMiddleware and SAMLProviderOnlyMiddleware)

### Migration Files
- Original path `auth/migrations/001_core_infrastructure.up.sql` - NOT FOUND
- Correct path: `docs/reference/legacy-migrations/001_core_infrastructure.up.sql` - EXISTS
- Correct path: `docs/reference/legacy-migrations/002_business_domain.up.sql` - EXISTS

### Database Schema Verified
- Users table schema matches `docs/reference/legacy-migrations/001_core_infrastructure.up.sql`
- Groups table schema matches `docs/reference/legacy-migrations/002_business_domain.up.sql`
- Minor correction: Users table has two unique constraints: `UNIQUE NULLS NOT DISTINCT (provider, provider_user_id)` and `UNIQUE(provider, email)`

### Implementation Status Updated
- Changed from "Phase X (Week Y)" format to "Completed" status
- All implementation phases have been completed as verified by existence of handler files

### Corrections Made
1. Line reference for DeleteUserAndData changed from 103 to 102
2. Migration file paths corrected from `auth/migrations/` to `docs/reference/legacy-migrations/`
3. Database schema unique constraint updated to match actual schema
4. API Summary table updated from "Phase X" to "Implemented/Placeholder" status
5. Implementation Phases section rewritten to reflect completed status
6. Added provider_auth_middleware.go to references
