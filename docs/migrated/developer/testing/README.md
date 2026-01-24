# TMI Testing Guide

## Overview

TMI uses a multi-layered testing strategy with unit tests, integration tests, and security fuzzing. All test resources are organized under the `test/` directory.

## Testing Layers

### 1. Unit Tests

**Purpose**: Fast, isolated tests with no external dependencies

**Location**: In-package tests (`api/*_test.go`, `auth/*_test.go`, etc.)

**Run**:

```bash
make test-unit
```

**Characteristics**:

- Run in milliseconds
- No database or external services required
- Mock external dependencies
- Test individual functions and methods
- Use `LOGGING_IS_TEST=true` to suppress logs

**Coverage**: Generate coverage reports with `make test-coverage`

### 2. Integration Tests (New Framework)

**Purpose**: End-to-end workflow testing from client perspective

**Location**: `test/integration/`

**Architecture**:

- **OpenAPI-driven**: Validates all requests/responses against spec
- **Black-box testing**: Tests as a client would use the API
- **Workflow-oriented**: Tests complete user scenarios
- **Automated OAuth**: Uses oauth-stub for authentication

**Components**:

- `framework/client.go` - HTTP client with OpenAPI validation
- `framework/oauth.go` - OAuth authentication helpers
- `framework/assertions.go` - Test assertion functions
- `framework/fixtures.go` - Test data builders
- `spec/` - OpenAPI validation

**Run**:

```bash
# Prerequisites
make start-dev          # Terminal 1: Start server
make start-oauth-stub   # Terminal 2: Start OAuth stub

# Run tests
make test-integration              # All integration tests (PostgreSQL)
make test-integration-pg           # Integration tests with PostgreSQL
make test-integration-oci          # Integration tests with Oracle ADB
```

<!-- NEEDS-REVIEW: make test-integration-new, make test-integration-quick, and make test-integration-workflow targets not found in Makefile -->

**Writing Tests**:
See `test/integration/README.md` for detailed guide and examples.

**Environment Variables**:

- `INTEGRATION_TESTS=true` - Enable integration tests
- `TMI_SERVER_URL` - Server URL (default: http://localhost:8080)
- `OAUTH_STUB_URL` - OAuth stub URL (default: http://localhost:8079)

### 3. CATS Security Fuzzing

**Purpose**: Security testing and API contract validation

**What is CATS**:
CATS (Contract-driven Automatic Testing Suite) is a security fuzzing tool that tests API endpoints for vulnerabilities and spec compliance.

**Features**:

- Boundary testing (very long strings, large numbers)
- Type confusion testing
- Required field validation
- Authentication bypass testing
- Malformed input handling

**Run**:

```bash
make cats-fuzz                # Full fuzzing with OAuth
make cats-fuzz-user USER=alice  # Fuzz with specific user
make analyze-cats-results     # Analyze results
```

**Public Endpoint Handling**:

- TMI has 17 public endpoints (OAuth, OIDC, SAML)
- Marked with `x-public-endpoint: true` in OpenAPI spec
- CATS skips `BypassAuthentication` fuzzer on these endpoints
- See `docs/developer/testing/cats-public-endpoints.md`

**Output**: `test/outputs/cats/`

**Documentation**:

- `docs/developer/testing/cats-public-endpoints.md`
- `docs/developer/testing/cats-oauth-false-positives.md`
- `docs/developer/testing/cats-test-data-setup.md`

### 4. WebSocket Testing

**Purpose**: Test real-time collaboration features

**Tool**: WebSocket Test Harness (`wstest/`)

**Run**:

```bash
make wstest              # Multi-user collaboration test
make wstest-clean        # Stop all test instances
```

**Features**:

- OAuth authentication with test provider
- Host mode: Creates sessions
- Participant mode: Joins sessions
- Comprehensive message logging
- 30-second timeout for safety

**Output**: Logs to console

<!-- NEEDS-REVIEW: test/outputs/wstest/ directory does not exist - WebSocket tests log to console only -->

## Test Organization

```
test/
+-- integration/          # Integration test framework
|   +-- framework/        # Core components (client.go, oauth.go, assertions.go, fixtures.go, database.go)
|   +-- spec/             # OpenAPI validation (schema_loader.go)
|   +-- workflows/        # Test workflows
|   +-- README.md         # Detailed guide
+-- postman/              # Postman/Newman API test collections
+-- outputs/              # Test outputs (gitignored)
|   +-- integration/      # Integration test logs
+-- TESTING_STRATEGY.md   # Testing strategy documentation
```

<!-- NEEDS-REVIEW: test/tools/, test/configs/ directories do not exist; test/outputs/unit/, test/outputs/cats/, test/outputs/wstest/ subdirectories not present -->

## Running All Tests

### CI/CD Pipeline

```bash
make test-unit           # Fast unit tests
make test-integration    # Integration tests (PostgreSQL)
make cats-fuzz           # Security fuzzing
```

<!-- NEEDS-REVIEW: make test-integration-full target not found in Makefile -->

### Local Development

```bash
# Quick validation
make test-unit
make lint
make build-server

# Full validation before commit
make test-integration    # With server running
```

## Test Data Management

### Integration Tests

- Each test uses unique user IDs: `framework.UniqueUserID()`
- Fixtures provide test data: `framework.NewThreatModelFixture()`
- Tests clean up resources after execution
- OAuth tokens obtained automatically via oauth-stub

### CATS Fuzzing

- Test user credentials managed by OAuth stub
- Rate limits automatically cleared before testing

<!-- NEEDS-REVIEW: test/configs/cats-test-data.yml does not exist -->

### WebSocket Tests

- Uses login hints with test provider
- Alice (host), Bob, Charlie (participants)
- Sessions automatically timeout after 30 seconds

## OAuth Testing

### Test Provider

TMI includes a test OAuth provider for development/testing:

**Features**:

- PKCE support (RFC 7636)
- Login hints for predictable users
- Token introspection
- Token refresh

**Usage**:

```go
// Integration tests (automated)
tokens, err := framework.AuthenticateUser("testuser-123")

// Manual testing
curl "http://localhost:8080/oauth2/authorize?idp=tmi&login_hint=alice&..."
```

### OAuth Callback Stub

**Purpose**: Receives OAuth callbacks and manages tokens for tests

**Location**: `scripts/oauth-client-callback-stub.py`

**Start**:

```bash
make start-oauth-stub  # Starts on port 8079
```

**API**:

- `POST /oauth/init` - Initialize OAuth flow
- `POST /flows/start` - Automated end-to-end flow
- `GET /flows/{id}` - Poll flow status
- `GET /creds?userid=X` - Retrieve stored credentials
- `POST /refresh` - Refresh tokens

**Output**: `/tmp/oauth-stub.log`

## Coverage Reporting

### Generate Coverage

```bash
make test-coverage  # Generates unit + integration coverage
```

**Output**:

- `coverage/` - Coverage data files
- `coverage_html/` - HTML reports

### View Coverage

```bash
# Open HTML report
open coverage_html/combined_coverage.html

# Terminal summary
go tool cover -func=coverage/combined_coverage.out | tail -1
```

## Debugging Tests

### Integration Tests

```go
// Pretty-print responses
framework.PrettyPrintJSON(t, resp.Body)

// Disable OpenAPI validation
client, _ := framework.NewClient(serverURL, tokens,
    framework.WithValidation(false))

// Check operation ID
validator, _ := spec.NewValidator()
opID, _ := validator.GetOperationID("POST", "/threat_models")
```

### Logs

- Unit tests: `logs/tmi.log` (project directory)
- Integration tests: `test/outputs/integration/*.log`
- OAuth stub: `/tmp/oauth-stub.log`
- WebSocket tests: Console output
- CATS: `test/outputs/cats/report/`

### Common Issues

**OAuth stub not running**:

```bash
make start-oauth-stub
```

**Server not running**:

```bash
make start-dev
```

**Integration test timeouts**:

- Check server logs: `tail -f logs/tmi.log`
- Verify database running: `docker ps | grep postgres`
- Verify Redis running: `docker ps | grep redis`

**OpenAPI validation errors**:

```bash
make generate-api  # Regenerate API from spec
```

## Best Practices

### Unit Tests

- Test one thing per test function
- Use table-driven tests for multiple scenarios
- Mock external dependencies
- Keep tests fast (< 100ms each)

### Integration Tests

- Use unique IDs for all resources
- Clean up created resources
- Test complete workflows, not individual endpoints
- Validate timestamps and UUIDs
- Use descriptive test names

### CATS Fuzzing

- Run before major releases
- Review non-success results carefully
- Filter OAuth false positives

### WebSocket Tests

- Test multi-user scenarios
- Validate message formats against AsyncAPI spec
- Test error conditions (invalid messages, denied participants)

## Test Coverage Goals

- **Unit Tests**: 80%+ code coverage
- **Integration Tests**: 90%+ endpoint coverage
- **CATS Fuzzing**: 100% endpoint coverage
- **WebSocket Tests**: All AsyncAPI message types

## Resources

### Documentation

- Integration Framework: `test/integration/README.md`
- Testing Strategy: `test/TESTING_STRATEGY.md`
- CATS Public Endpoints: `docs/developer/testing/cats-public-endpoints.md`
- CATS OAuth False Positives: `docs/developer/testing/cats-oauth-false-positives.md`
- WebSocket Testing: `docs/developer/testing/websocket-testing.md`

### Tools

- CATS: https://github.com/Endava/cats
- OpenAPI Generator: https://github.com/oapi-codegen/oapi-codegen
- OAuth Stub: `scripts/oauth-client-callback-stub.py`
- WS Test Harness: `wstest/`

### Make Commands

```bash
make list-targets  # See all available commands
```

<!-- NEEDS-REVIEW: make test-help target not found in Makefile -->

---

## Verification Summary

<!-- Added: 2025-01-24 -->

**Verified Items:**

1. Make targets: `test-unit`, `test-integration`, `test-integration-pg`, `test-integration-oci`, `cats-fuzz`, `cats-fuzz-user`, `analyze-cats-results`, `wstest`, `wstest-clean`, `start-oauth-stub`, `start-dev`, `generate-api`, `test-coverage`, `lint`, `build-server`, `list-targets` - all exist in Makefile
2. File paths:
   - `test/integration/` directory exists with framework/, spec/, workflows/ subdirectories
   - `test/integration/framework/client.go`, `oauth.go`, `assertions.go`, `fixtures.go` exist
   - `test/integration/spec/schema_loader.go` exists
   - `test/integration/README.md` exists
   - `test/TESTING_STRATEGY.md` exists
   - `wstest/` directory exists with main.go
   - `scripts/oauth-client-callback-stub.py` exists
   - `docs/developer/testing/cats-public-endpoints.md` exists
   - `docs/developer/testing/cats-oauth-false-positives.md` exists
   - `docs/developer/testing/cats-test-data-setup.md` exists
   - `docs/developer/testing/websocket-testing.md` exists
   - `api/*_test.go` and `auth/*_test.go` test files exist
   - `test/outputs/integration/` exists with log files
3. External tools:
   - CATS (https://github.com/Endava/cats) - verified as active REST API fuzzer
   - oapi-codegen (https://github.com/oapi-codegen/oapi-codegen) - verified as Go OpenAPI code generator

**Items Marked for Review:**

1. `make test-integration-new`, `make test-integration-quick`, `make test-integration-workflow` - targets not found in Makefile
2. `make test-integration-full` - target not found in Makefile
3. `make test-help` - target not found in Makefile
4. `test/tools/` directory - does not exist (oauth-stub is in scripts/)
5. `test/configs/` directory - does not exist
6. `test/outputs/unit/`, `test/outputs/cats/`, `test/outputs/wstest/` - directories not found
7. `test/configs/cats-test-data.yml` - file does not exist

**Corrections Made:**

1. Changed `make cats-analyze` to `make analyze-cats-results` (correct target name)
2. Changed OAuth provider from `idp=test` to `idp=tmi` (correct provider ID)
3. Updated oapi-codegen URL from deepmap to oapi-codegen organization
4. Updated OAuth stub location from `test/tools/oauth-stub/` to `scripts/oauth-client-callback-stub.py`
5. Updated test organization diagram to reflect actual directory structure
6. Updated coverage output paths to use `coverage/` instead of `test/outputs/unit/`
