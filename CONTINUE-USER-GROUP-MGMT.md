# Continue: User and Group Management API Implementation

## Quick Context

I'm implementing user and group management APIs for TMI. **Phase 1-2 are complete** (database stores and handlers). The code **builds and lints cleanly**. Now I need to integrate with OpenAPI and wire up the handlers.

## What's Done ✅

- Database stores for users and groups (full CRUD, filtering, pagination)
- Admin handlers for user/group management (list, get, update, delete, create)
- SAML user listing handler for UI autocomplete
- Provider authorization middleware (same-provider enforcement)
- **All code compiles and lints successfully (v0.219.1)**

## What's Next 🔄

### Priority 1: OpenAPI Integration

Update `docs/reference/apis/tmi-openapi.json` to add:

**New Endpoints**:
1. `GET /admin/users` - List users with filtering
2. `GET /admin/users/{internal_uuid}` - Get user details
3. `PATCH /admin/users/{internal_uuid}` - Update user
4. `DELETE /admin/users?provider={provider}&provider_id={provider_id}` - Delete user
5. `GET /admin/groups` - List groups with filtering
6. `GET /admin/groups/{internal_uuid}` - Get group details
7. `POST /admin/groups` - Create group
8. `PATCH /admin/groups/{internal_uuid}` - Update group
9. `DELETE /admin/groups?provider={provider}&group_name={group_name}` - Delete group (501)
10. `GET /saml/providers/{idp}/users` - List users for SAML provider

**Enhance Existing**:
- `GET /oauth2/providers/{idp}/groups` - Add same-provider authorization requirement

**New Schemas**:
- AdminUser, AdminUserListResponse
- AdminGroup, AdminGroupListResponse
- CreateAdminGroupRequest, UpdateAdminUserRequest, UpdateAdminGroupRequest
- SAMLUserListResponse, DeletionStats

**Security Markers**:
- `x-admin-only: true` for all `/admin/*` endpoints
- `x-same-provider-required: true` for SAML endpoints

### Priority 2: Wire Up Handlers

After generating code (`make generate-api`):

1. Initialize stores in server startup:
   ```go
   GlobalUserStore = NewUserDatabaseStore(db, authService)
   GlobalGroupStore = NewGroupDatabaseStore(db)
   ```

2. Implement ServerInterface methods (delegate to handlers)

3. Apply middleware to routes

### Priority 3: Testing

Write tests for stores and handlers (see plan for details).

## Key Files to Reference

- **Implementation Plan**: `docs/developer/planning/user-group-management-apis.md`
- **Design Decisions**: `docs/developer/planning/user-group-management-decisions.md`
- **Status Report**: `docs/developer/planning/user-group-management-implementation-status.md`
- **Implementation**: `api/admin_user_handlers.go`, `api/admin_group_handlers.go`, `api/provider_auth_middleware.go`

## Important Design Notes

- User deletion uses existing `auth.Service.DeleteUserAndData()` (same as DELETE /users/me)
- Group deletion returns 501 Not Implemented (placeholder)
- AdminUser type (not User) - renamed to avoid OpenAPI type conflict
- Identification: users by provider+provider_id, groups by provider+group_name
- SAML endpoints return active users only

## Verification Commands

```bash
make lint           # Should pass (0 issues)
make build-server   # Should succeed
make generate-api   # Run after OpenAPI changes
make test-unit      # After writing tests
```

## Next Steps

Please continue with **OpenAPI integration** (Priority 1 above). The OpenAPI spec update is the critical path - all handler code is ready and waiting to be wired in.
