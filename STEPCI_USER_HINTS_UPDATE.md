# StepCI Tests Updated with User Hints

## Overview

All StepCI test files have been successfully updated to use the **user hint functionality** for predictable test users. This update enables consistent, automation-friendly testing with known user identities instead of random usernames.

## What Changed

### 1. **Common Variables Updated** (`stepci/utils/common-variables.yml`)
- ✅ Added user hint environment variables for different test scenarios
- ✅ Created reusable OAuth flow components that accept user hints
- ✅ Updated OAuth URLs to include `&user_hint={{ user_hint }}` parameter

**Key Variables Added:**
- `test_user_owner: "alice"` → Creates `alice@test.tmi`
- `test_user_writer: "bob"` → Creates `bob@test.tmi`  
- `test_user_reader: "charlie"` → Creates `charlie@test.tmi`
- `test_user_default: "qa-automation"` → Creates `qa-automation@test.tmi`
- `test_user_admin: "admin-user"` → Creates `admin-user@test.tmi`

### 2. **Authentication Tests Updated**

#### **OAuth Flow Tests** (`stepci/auth/oauth-flow.yml`)
- ✅ Added `test_user_hint: "qa-automation"`
- ✅ Updated OAuth login URL with user hint parameter
- ✅ Added validation for predictable email: `qa-automation@test.tmi`
- ✅ Added comprehensive **User Hint Validation Tests** section
- ✅ Tests for valid/invalid user hints, format validation, provider restrictions

#### **Token Management Tests** (`stepci/auth/token-management.yml`)
- ✅ Added `test_user_hint: "token-test-user"`
- ✅ Updated all OAuth flows to use user hints
- ✅ Creates predictable `token-test-user@test.tmi` for testing

#### **User Operations Tests** (`stepci/auth/user-operations.yml`)
- ✅ Added multiple user hints for different test scenarios:
  - `test_user_hint: "user-ops"`
  - `test_user_1_hint: "session-user1"`
  - `test_user_2_hint: "session-user2"`
- ✅ Updated user info validation to check predictable identities
- ✅ Multi-user session testing now uses distinct predictable users

### 3. **RBAC and Permissions Tests** (`stepci/integration/rbac-permissions.yml`)
- ✅ **Role-specific user hints** for comprehensive permission testing:
  - `owner_user_hint: "alice"` → `alice@test.tmi` (Owner role)
  - `writer_user_hint: "bob"` → `bob@test.tmi` (Writer role)  
  - `reader_user_hint: "charlie"` → `charlie@test.tmi` (Reader role)
- ✅ Each user authentication includes identity validation
- ✅ Predictable users enable consistent RBAC testing across runs

### 4. **Integration and CRUD Tests**

#### **Full Workflow Integration** (`stepci/integration/full-workflow.yml`)
- ✅ Added `workflow_user_hint: "workflow-user"`
- ✅ End-to-end testing with consistent user identity
- ✅ User profile validation for `workflow-user@test.tmi`

#### **Threat Model CRUD** (`stepci/threat-models/crud-operations.yml`)
- ✅ Added user hints: `crud_user_hint: "tm-crud-user"` and `edge_case_user_hint: "tm-edge-user"`
- ✅ Separate users for main CRUD operations vs edge case testing
- ✅ Predictable ownership for threat model lifecycle testing

#### **Threats CRUD** (`stepci/threats/crud-operations.yml`)
- ✅ Added `threat_crud_user_hint: "threat-user"`
- ✅ Consistent user for threat management operations within threat models

## User Hint Specifications

All user hints follow the TMI test provider validation rules:

- **Format**: 3-20 characters, alphanumeric + hyphens, case-insensitive
- **Pattern**: `^[a-zA-Z0-9-]{3,20}$`
- **Generated Email**: `{hint}@test.tmi` (e.g., `alice@test.tmi`)
- **Generated Name**: `{Hint} (Test User)` (e.g., `Alice (Test User)`)
- **Provider Restriction**: Only works with "test" provider (development/testing only)

## Benefits of This Update

### ✅ **Predictable Test Data**
- No more random `testuser-12345678@test.tmi` users
- Consistent user identities across test runs
- Easier debugging and test result analysis

### ✅ **Enhanced RBAC Testing**
- Alice, Bob, and Charlie represent distinct roles consistently
- Permission testing with known user relationships
- Multi-user collaboration scenarios are reproducible

### ✅ **Better Test Automation**
- Tests can assert specific user emails and names
- Reduced flakiness from random user generation
- CI/CD pipelines will have consistent results

### ✅ **Improved Debugging**
- Log files show recognizable user names instead of UUIDs
- Easier to trace specific user actions in test scenarios
- Clear separation between different test user roles

## Testing the Updates

### Prerequisites
1. Ensure TMI server is running with test OAuth provider enabled
2. Start OAuth callback stub: `make oauth-stub-start`
3. Verify user hint functionality is working in your TMI build

### Run Specific Updated Tests

```bash
# Test basic OAuth flow with user hints
stepci run stepci/auth/oauth-flow.yml

# Test user hint validation (new test section)
stepci run stepci/auth/oauth-flow.yml --grep "user_hint_validation"

# Test RBAC with predictable users (Alice, Bob, Charlie)
stepci run stepci/integration/rbac-permissions.yml

# Test multi-user sessions with different hint users
stepci run stepci/auth/user-operations.yml --grep "user_session_edge_cases"

# Test full workflow with predictable user
stepci run stepci/integration/full-workflow.yml
```

### Run All Updated Tests
```bash
# Full automated test suite with all user hint updates
make stepci-full

# Execute all StepCI tests directly
make stepci-execute
```

### Verify User Hint Functionality

You can test the user hint functionality directly:

```bash
# Test predictable user creation
curl "http://localhost:8080/auth/login/test?user_hint=alice&client_callback=http://localhost:8079/"

# Verify callback stub captured predictable credentials
curl http://localhost:8079/latest | jq '.'

# Should show:
# {
#   "flow_type": "implicit",
#   "access_token": "...",
#   "state": "...",
#   // User will be alice@test.tmi
# }
```

## Rollback Plan

If issues arise, you can temporarily disable user hints by removing the `user_hint` parameters from the test files. The TMI server gracefully handles missing user hint parameters by falling back to random user generation.

## Files Modified

- ✅ `stepci/utils/common-variables.yml` - Added user hint variables and reusable components
- ✅ `stepci/auth/oauth-flow.yml` - Added user hint testing and validation
- ✅ `stepci/auth/token-management.yml` - Updated for predictable token testing
- ✅ `stepci/auth/user-operations.yml` - Multi-user hint scenarios
- ✅ `stepci/integration/rbac-permissions.yml` - Role-specific predictable users
- ✅ `stepci/integration/full-workflow.yml` - End-to-end with consistent user
- ✅ `stepci/threat-models/crud-operations.yml` - CRUD testing with user hints
- ✅ `stepci/threats/crud-operations.yml` - Threat management with user hints

## Next Steps

1. **Test the updated configuration** using the commands above
2. **Verify predictable user creation** works as expected
3. **Monitor test results** for improved consistency
4. **Update any remaining test files** that weren't covered in this batch

The StepCI test suite now provides **predictable, automation-friendly testing** with consistent user identities across all test scenarios!