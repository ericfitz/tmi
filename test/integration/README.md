# TMI Integration Test Framework

## Overview

OpenAPI-driven integration test framework for testing TMI REST API workflows from a client perspective.

## Quick Start

```bash
# 1. Start development server
make start-dev

# 2. Start OAuth stub (in separate terminal)
make start-oauth-stub

# 3. Run integration tests
make test-integration
```

## Framework Components

### Client (`framework/client.go`)
HTTP client with automatic OpenAPI validation:
- Bearer token injection
- Request/response logging
- Schema validation on every request/response
- Workflow state management

### OAuth (`framework/oauth.go`)
Automated OAuth authentication helpers:
- `AuthenticateUser(userID)` - Automated PKCE flow
- `GetStoredCredentials(userID)` - Retrieve existing tokens
- `RefreshToken(refreshToken, userID)` - Token refresh

### Assertions (`framework/assertions.go`)
Common assertion helpers:
- `AssertStatusOK`, `AssertStatusCreated`, etc.
- `AssertJSONField`, `AssertValidUUID`, `AssertValidTimestamp`
- `ExtractID` - Extract resource IDs from responses

### Fixtures (`framework/fixtures.go`)
Test data builders:
- `NewThreatModelFixture()`, `NewDiagramFixture()`, etc.
- Fluent interface for customization
- Unique IDs generated automatically

### OpenAPI Validator (`spec/`)
Runtime OpenAPI validation:
- Validates requests match spec
- Validates responses match spec
- Detailed error messages for debugging

## Writing Tests

### Basic Test Structure

```go
func TestThreatModelCRUD(t *testing.T) {
    // 1. Setup
    if os.Getenv("INTEGRATION_TESTS") != "true" {
        t.Skip("Skipping integration test")
    }

    serverURL := os.Getenv("TMI_SERVER_URL")
    if serverURL == "" {
        serverURL = "http://localhost:8080"
    }

    // 2. Authenticate
    tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
    framework.AssertNoError(t, err, "Authentication failed")

    // 3. Create client
    client, err := framework.NewClient(serverURL, tokens)
    framework.AssertNoError(t, err, "Client creation failed")

    // 4. Test workflow
    // Create
    resp, err := client.Do(framework.Request{
        Method: "POST",
        Path:   "/threat_models",
        Body:   framework.NewThreatModelFixture(),
    })
    framework.AssertNoError(t, err, "Create failed")
    framework.AssertStatusCreated(t, resp)

    tmID := framework.ExtractID(t, resp, "id")

    // Read
    resp, err = client.Do(framework.Request{
        Method: "GET",
        Path:   "/threat_models/" + tmID,
    })
    framework.AssertStatusOK(t, resp)

    // Update
    resp, err = client.Do(framework.Request{
        Method: "PUT",
        Path:   "/threat_models/" + tmID,
        Body:   framework.NewThreatModelFixture().WithName("Updated"),
    })
    framework.AssertStatusOK(t, resp)

    // Delete
    resp, err = client.Do(framework.Request{
        Method: "DELETE",
        Path:   "/threat_models/" + tmID,
    })
    framework.AssertStatusNoContent(t, resp)
}
```

### Using Workflow State

```go
// Save IDs for multi-step workflows
client.SaveState("threat_model_id", tmID)
client.SaveState("diagram_id", diagramID)

// Retrieve later
tmID, err := client.GetStateString("threat_model_id")
framework.AssertNoError(t, err, "State retrieval failed")
```

### Custom Fixtures

```go
// Use builder pattern
tmFixture := framework.NewThreatModelFixture().
    WithName("Custom Threat Model").
    WithDescription("Detailed description").
    WithIssueURI("https://github.com/org/repo/issues/123")

// Create with unique identifiers
userID := framework.UniqueUserID()  // testuser-abc123
name := framework.UniqueName("tm")   // tm-xyz789
```

### Testing Error Scenarios

```go
// Test 404 Not Found
resp, err := client.Do(framework.Request{
    Method: "GET",
    Path:   "/threat_models/00000000-0000-0000-0000-000000000000",
})
framework.AssertStatusNotFound(t, resp)

// Test 400 Bad Request
resp, err = client.Do(framework.Request{
    Method: "POST",
    Path:   "/threat_models",
    Body:   map[string]string{}, // Missing required fields
})
framework.AssertStatusBadRequest(t, resp)
framework.AssertError(t, resp, "name is required")
```

## Test Organization

### Tier 1: Core Workflows (Run on every commit)
- `oauth_flow_test.go` - OAuth PKCE, token refresh, revocation
- `threat_model_crud_test.go` - Full CRUD lifecycle
- `diagram_collaboration_test.go` - Multi-user WebSocket

### Tier 2: Feature Tests (Run nightly)
- `metadata_operations_test.go` - All metadata endpoints
- `bulk_operations_test.go` - Bulk create/update/delete
- `webhook_workflow_test.go` - Subscription → delivery
- `addon_workflow_test.go` - Register → invoke → status

### Tier 3: Edge Cases (Run on-demand)
- `authorization_test.go` - RBAC, ownership, permissions
- `pagination_test.go` - Limit/offset on all list endpoints
- `error_handling_test.go` - 4xx, 5xx scenarios

## Environment Variables

- `INTEGRATION_TESTS=true` - Enable integration tests
- `TMI_SERVER_URL` - Server URL (default: http://localhost:8080)
- `OAUTH_STUB_URL` - OAuth stub URL (default: http://localhost:8079)
- `STRICT_VALIDATION=true` - Fail on OpenAPI validation warnings

## Running Tests

```bash
# Run all integration tests
make test-integration

# Run specific test
go test -v ./test/integration/workflows -run TestThreatModelCRUD

# Run with verbose logging
INTEGRATION_TESTS=true go test -v ./test/integration/workflows/...

# Run against different server
TMI_SERVER_URL=https://tmi-staging.herokuapp.com make test-integration
```

## Debugging

### Enable Request Logging
Already enabled by default. Check logs at `test/outputs/integration/`

### Disable OpenAPI Validation
```go
client, err := framework.NewClient(serverURL, tokens,
    framework.WithValidation(false))
```

### Pretty-Print Responses
```go
framework.PrettyPrintJSON(t, resp.Body)
```

### Check OpenAPI Operation ID
```go
validator, _ := spec.NewValidator()
opID, _ := validator.GetOperationID("POST", "/threat_models")
t.Logf("Operation ID: %s", opID)
```

## Best Practices

1. **Always authenticate** - Use `framework.AuthenticateUser()` for each test
2. **Use unique IDs** - Use `framework.UniqueUserID()` to avoid conflicts
3. **Clean up resources** - Delete created resources at test end
4. **Test one workflow per function** - Keep tests focused
5. **Use descriptive names** - `TestThreatModelCRUDWithDiagrams`, not `TestAPI`
6. **Validate timestamps** - Use `AssertValidTimestamp()` and `AssertTimestampOrder()`
7. **Check UUIDs** - Use `AssertValidUUID()` for all IDs
8. **Save state** - Use `client.SaveState()` for multi-step workflows

## Troubleshooting

### OAuth stub not running
```
Error: OAuth stub not running at http://localhost:8079
Solution: make start-oauth-stub
```

### Server not running
```
Error: connection refused
Solution: make start-dev
```

### OpenAPI validation errors
```
Error: Response validation failed: field 'xyz' not in spec
Solution: Check OpenAPI spec is up-to-date (make generate-api)
```

### Test timeouts
```
Error: flow timed out after 30 seconds
Solution: Check server logs, ensure DB/Redis are running
```
