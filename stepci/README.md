# StepCI Integration Tests for TMI API

This directory contains comprehensive integration tests for the TMI (Threat Modeling Interface) API using StepCI framework.

## Overview

The test suite covers all 87+ REST API endpoints with both success and failure scenarios, focusing on:

- **Authentication & Authorization**: Complete OAuth flow with JWT token management
- **CRUD Operations**: Full lifecycle testing for all entities (threat models, threats, diagrams, documents, sources)
- **Collaboration Features**: Real-time diagram collaboration session management
- **Input Validation**: Comprehensive failure testing to harden API security
- **Role-Based Access Control**: Multi-user permission testing
- **Edge Cases**: Boundary testing, Unicode support, large datasets

## Prerequisites

1. **StepCI installed**: `npm install -g stepci`
2. **TMI API server running**: `make dev-start` (on localhost:8080)
3. **Database and Redis**: Required for full functionality
4. **OAuth Callback Stub**: `python3 scripts/oauth-client-callback-stub.py --port 8079` (for OAuth tests)

## Test Structure

```
stepci/
├── auth/                    # Authentication & token management tests
│   ├── oauth-flow.yml       # Complete OAuth authentication flow
│   ├── token-management.yml # JWT token lifecycle management
│   └── user-operations.yml  # User info and logout operations
├── threat-models/           # Threat model management tests
│   ├── crud-operations.yml  # Standard CRUD lifecycle
│   ├── search-filtering.yml # Search, filtering, pagination
│   └── validation-failures.yml # Input validation error cases
├── threats/                 # Threat management tests
│   ├── crud-operations.yml  # Threat CRUD within threat models
│   └── bulk-operations.yml  # Bulk create/update operations
├── diagrams/                # Diagram and collaboration tests
│   └── collaboration.yml    # Real-time collaboration session management
├── integration/             # Cross-cutting integration tests
│   ├── full-workflow.yml    # Complete end-to-end user journey
│   └── rbac-permissions.yml # Role-based access control testing
└── utils/                   # Shared utilities and test data
    ├── common-variables.yml  # Shared configuration and variables
    └── test-data.yml         # Test fixtures and sample data
```

## Running Tests

### Individual Test Files
```bash
# Run specific test category
stepci run stepci/auth/oauth-flow.yml
stepci run stepci/threat-models/crud-operations.yml
stepci run stepci/diagrams/collaboration.yml

# Run with specific base URL
stepci run stepci/auth/oauth-flow.yml --env baseURL=http://localhost:8080
```

### Test Suites by Category
```bash
# Authentication tests
stepci run stepci/auth/oauth-flow.yml stepci/auth/token-management.yml stepci/auth/user-operations.yml

# Threat model tests (success cases)
stepci run stepci/threat-models/crud-operations.yml stepci/threat-models/search-filtering.yml

# Threat model tests (failure cases)
stepci run stepci/threat-models/validation-failures.yml

# Integration tests
stepci run stepci/integration/full-workflow.yml stepci/integration/rbac-permissions.yml
```

### Complete Test Suite
```bash
# Run all tests (warning: this will take significant time)
find stepci -name "*.yml" -not -path "*/utils/*" | xargs stepci run
```

## Test Features

### OAuth Authentication Flow
- **Test Provider Integration**: Uses TMI's "test" OAuth provider
- **OAuth Callback Stub**: `scripts/oauth-client-callback-stub.py` handles OAuth redirects
- **Dynamic User Generation**: Each OAuth flow creates a new user (email/ID cannot be predicted)
- **JWT Token Management**: Access token, refresh token lifecycle testing
- **Session Management**: Login, logout, token invalidation

#### OAuth Callback Stub (`scripts/oauth-client-callback-stub.py`)
The OAuth tests require a callback stub to capture authorization codes from the OAuth flow:

**Features:**
- **Route 1** (`GET /`): Receives OAuth callbacks, stores latest code/state
- **Route 2** (`GET /latest`): Returns stored credentials as JSON for StepCI consumption
- **Automatic Integration**: StepCI tests fetch real OAuth codes via `/latest` endpoint
- **Structured Logging**: All requests and events logged to `/tmp/oauth-stub.log` with RFC3339 timestamps
- **Magic Exit Code**: Send `GET /?code=exit` to gracefully shutdown the server via HTTP request

**Usage:**
```bash
# Start the callback stub (required for OAuth tests)
python3 scripts/oauth-client-callback-stub.py --port 8079

# In another terminal, run OAuth tests
stepci run stepci/auth/oauth-flow.yml

# Monitor logs (optional)
tail -f /tmp/oauth-stub.log

# Gracefully shutdown server (alternative to Ctrl+C)
curl "http://localhost:8079/?code=exit"
```

**API Response:**
```json
GET http://localhost:8079/latest
{"code": "test_auth_code_1234567890", "state": "AbCdEf123456"}
```

**Log Output:**
```
2025-08-16T16:57:29.8050Z Server listening on http://localhost:8079/...
2025-08-16T16:58:48.7159Z Received OAuth redirect: Code=test_auth_code_1234567890, State=AbCdEf123456
2025-08-16T16:58:48.7161Z API request: 127.0.0.1 GET /?code=test_auth_code_1234567890&state=AbCdEf123456 HTTP/1.1 200 "Redirect received. Check server logs for details."
2025-08-16T16:58:52.2411Z API request: 127.0.0.1 GET /latest HTTP/1.1 200 {"code": "test_auth_code_1234567890", "state": "AbCdEf123456"}
2025-08-16T16:59:06.9896Z Received 'exit' in code parameter, shutting down gracefully...
2025-08-16T16:59:06.9897Z Server has shut down.
```

This approach solves StepCI's variable substitution limitations by using real OAuth authorization codes captured from the actual OAuth flow. All activity is logged to `/tmp/oauth-stub.log` for debugging and monitoring purposes.

### CRUD Operations Testing
- **Complete Lifecycle**: Create → Read → Update → Delete for all entities
- **JSON Patch Support**: RFC 6902 JSON Patch partial updates
- **Bulk Operations**: Multi-entity creation and updates
- **Relationship Testing**: Parent-child entity relationships (TM → Threats, TM → Diagrams)

### Input Validation & API Hardening
- **Schema Violations**: Invalid types, missing fields, unknown fields
- **Read-Only Field Protection**: Attempting to set system-generated fields
- **Field Length Limits**: Oversized inputs and empty required fields
- **Format Validation**: Invalid UUIDs, URLs, dates, enums
- **Security Testing**: SQL injection patterns, XSS attempts (safely handled)

### Collaboration Testing
- **Session Lifecycle**: Create → Join → End collaboration sessions
- **Conflict Handling**: POST when session exists (409), PUT when none exists (404)
- **Multi-User Scenarios**: Multiple users joining same session
- **Permission Inheritance**: Collaboration permissions based on threat model access

### RBAC (Role-Based Access Control)
- **Dynamic Role Assignment**: Owner creates resource, grants roles to other users
- **Permission Testing**: Reader (read-only), Writer (read/write), Owner (full control)
- **Inheritance Testing**: Child entity permissions inherit from parent
- **Unauthorized Access**: Comprehensive 401/403 testing

## Key Testing Patterns

### OAuth User Management
Since the test OAuth provider generates random users:
```yaml
# Cannot hard-code users like this:
user_email: "test@example.com"  # ❌ Won't work

# Instead, capture dynamically:
captures:
  user_id:
    jsonpath: $.sub
  user_email:
    jsonpath: $.email
```

### RBAC Testing Strategy
```yaml
# 1. User1 authenticates and creates resource (becomes owner)
# 2. User2 authenticates (new random user)
# 3. User1 grants role to User2 (if permission system exists)
# 4. Test User2's access with assigned role
```

### Error Response Validation
All failure tests validate:
- Correct HTTP status codes (400, 401, 403, 404, 409, 422, 500)
- Consistent error response schema
- Helpful error messages
- No sensitive information leakage

## Environment Configuration

### Default Configuration
- **Base URL**: `http://localhost:8080`
- **OAuth Provider**: `test`
- **Authentication**: Bearer JWT tokens
- **Content Type**: `application/json`

### Customizing Configuration
Modify `stepci/utils/common-variables.yml` to change:
- Base URL for different environments
- OAuth provider configuration
- Common headers and validation patterns

## Expected Outcomes

### Success Scenarios
- All CRUD operations complete successfully
- OAuth flow generates valid JWT tokens  
- Collaboration sessions created and managed properly
- Proper HTTP status codes (200, 201, 204) returned
- Response schemas match OpenAPI specification

### Failure Scenarios  
- Invalid requests properly rejected with 400 Bad Request
- Unauthorized requests return 401 Unauthorized
- Forbidden operations return 403 Forbidden
- Non-existent resources return 404 Not Found
- Conflicts return 409 Conflict
- Malformed JSON Patch returns 422 Unprocessable Entity

## Debugging Tests

### Verbose Output
```bash
stepci run --verbose stepci/auth/oauth-flow.yml
```

### Individual Test Steps
```bash
# Run single test case within file
stepci run stepci/auth/oauth-flow.yml --test oauth_success_flow
```

### Common Issues
1. **Server Not Running**: Ensure `make dev-start` is running
2. **Database Not Ready**: Wait for PostgreSQL container to fully start
3. **OAuth Provider Issues**: Check test provider is configured correctly
4. **Token Expiration**: Tests create fresh tokens, shouldn't be an issue

## Contributing

When adding new test cases:

1. **Follow Existing Patterns**: Use established authentication and cleanup patterns
2. **Test Both Success and Failure**: Every endpoint should have positive and negative tests
3. **Validate Response Schemas**: Check both status codes and response structure
4. **Clean Up Resources**: Always delete created test resources
5. **Handle Dynamic Data**: Don't hard-code UUIDs or user information

## Integration with CI/CD

These tests are designed for integration into automated pipelines:

```bash
# Example GitHub Actions step
- name: Run API Integration Tests
  run: |
    make dev-start &
    sleep 30  # Wait for services to start
    stepci run stepci/integration/full-workflow.yml
    stepci run stepci/auth/oauth-flow.yml
    # Add more test runs as needed
```

The comprehensive test suite validates API correctness, security, and robustness across all supported operations and user scenarios.