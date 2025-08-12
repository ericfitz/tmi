# Integration Tests

This directory contains integration tests for the TMI API endpoints that test against actual database connections.

## Test Files

### `integration_test.go`

Contains full integration tests that require an actual PostgreSQL and Redis database connection. These tests:

- Create real database connections
- Use the test OAuth provider for authentication
- Test complete CRUD operations for threat models and diagrams
- Clean up test data after execution

### `integration_mock_test.go`

Contains integration-style tests that use mock authentication and the in-memory store. These tests:

- Run quickly without external dependencies
- Test all HTTP endpoints (POST, GET, PUT)
- Verify request/response formats
- Can run in CI/CD environments

## Running Integration Tests

### Option 1: Mock Integration Tests (Recommended for CI/CD)

```bash
# Run mock integration tests (no database required)
go test -v ./api -run TestEndpointIntegrationMock
```

### Option 2: Full Integration Tests (Requires Database)

```bash
# Run the integration test script that sets up databases automatically
./scripts/run-integration-tests.sh
```

### Option 3: Manual Database Setup

```bash
# Set up test databases manually
export TEST_DB_HOST=localhost
export TEST_DB_PORT=5433
export TEST_DB_USER=tmi_test
export TEST_DB_PASSWORD=test123
export TEST_DB_NAME=tmi_test
export TEST_REDIS_HOST=localhost
export TEST_REDIS_PORT=6380

# Run full integration tests
go test -v ./api -run TestIntegration
```

## Test Environment Variables

The integration tests support the following environment variables:

| Variable           | Default   | Description              |
| ------------------ | --------- | ------------------------ |
| `TEST_DB_HOST`     | localhost | PostgreSQL host          |
| `TEST_DB_PORT`     | 5433      | PostgreSQL port          |
| `TEST_DB_USER`     | tmi_test  | PostgreSQL username      |
| `TEST_DB_PASSWORD` | test123   | PostgreSQL password      |
| `TEST_DB_NAME`     | tmi_test  | PostgreSQL database name |
| `TEST_REDIS_HOST`  | localhost | Redis host               |
| `TEST_REDIS_PORT`  | 6380      | Redis port               |

## Test Coverage

The integration tests cover:

### Threat Models

- **POST /threat_models** - Create new threat models
- **GET /threat_models** - List all threat models
- **GET /threat_models/:threat_model_id** - Get specific threat model
- **PUT /threat_models/:threat_model_id** - Update threat model

### Diagrams

- **POST /diagrams** - Create new diagrams
- **GET /diagrams** - List all diagrams
- **GET /diagrams/:id** - Get specific diagram
- **PUT /diagrams/:id** - Update diagram

## Authentication

The integration tests use different authentication strategies:

### Full Integration Tests

- Use the "test" OAuth provider configured in the auth package
- Create real user records in the database
- Generate valid JWT tokens for authentication

### Mock Integration Tests

- Use mock middleware that sets authentication context
- Skip actual OAuth flows
- Suitable for testing API logic without authentication complexity

## Database Schema

The full integration tests expect the following:

- PostgreSQL database with TMI schema
- Redis instance for session storage
- Database migrations should be run before testing

## Cleanup

Both test suites properly clean up after themselves:

- Full integration tests clean up database records
- Mock tests reset the in-memory stores
- No persistent state between test runs

## Continuous Integration

For CI/CD pipelines, use the mock integration tests:

```bash
go test -short ./api -run TestEndpointIntegrationMock
```

The mock tests:

- Run quickly (< 1 second)
- Don't require external services
- Still validate API endpoint behavior
- Test JSON serialization/deserialization
- Verify HTTP status codes and response formats
