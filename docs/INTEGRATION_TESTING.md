# Integration Testing Guide

This document describes how to run integration tests for the TMI (Threat Modeling Interface) project.

## Overview

Integration tests validate that the API endpoints correctly persist data to actual PostgreSQL and Redis databases, rather than using in-memory mocks. These tests ensure that:

- API endpoints correctly create, read, update, and delete data in PostgreSQL
- Authentication and authorization work with real database-backed user sessions
- Database transactions and constraints are properly enforced
- Redis caching and session management work correctly

## Advantages of This Approach

The automated integration test setup provides several key benefits:

1. **Zero Configuration**: No manual database setup, environment variables, or cleanup required
2. **Isolated Testing**: Uses dedicated ports (5434/6381) to avoid conflicts with development databases
3. **Consistent Environment**: Same setup across all developers and CI/CD systems
4. **Automatic Cleanup**: Containers are automatically removed on success or failure
5. **Production-Like Testing**: Tests use real databases with actual constraints and transactions
6. **Developer Friendly**: Single command runs everything: `make test-integration`

## Quick Start

The easiest way to run integration tests is using the automated script:

```bash
# Run all integration tests (automatic setup and cleanup)
make test-integration

# Or run the script directly
./scripts/integration-test.sh
```

This will automatically:
1. Start PostgreSQL test container on port 5434
2. Start Redis test container on port 6381  
3. Run database migrations
4. Set up auth database schema
5. Run integration tests
6. Clean up containers when finished

## Manual Cleanup

If you need to clean up test containers manually:

```bash
make test-integration-cleanup
```

## Configuration

### Port Configuration
The integration tests use dedicated ports to avoid conflicts with development databases:

- **PostgreSQL**: Port 5434 (vs 5432 for development)
- **Redis**: Port 6381 (vs 6379 for development)

### Database Configuration
- **Database Name**: `tmi_integration_test`
- **Username**: `tmi_integration`
- **Password**: `integration_test_123`

### Container Names
- **PostgreSQL Container**: `tmi-integration-postgres`
- **Redis Container**: `tmi-integration-redis`

## Test Structure

Integration tests are located in:
- `api/sub_entities_integration_test.go` - Main integration test suite

### Test Coverage

Current integration tests cover:

1. **Threat Model CRUD Operations**
   - Creating threat models with database persistence
   - Retrieving threat models from database
   - Updating threat models and verifying persistence
   - Database constraint validation (required fields, counts, etc.)

2. **Authentication & Authorization**
   - JWT token creation and validation
   - Role-based access control (RBAC)
   - User creation and management

3. **Database Integration**
   - Real PostgreSQL transactions
   - Schema constraint enforcement
   - Count field management and validation

## Troubleshooting

### Docker Issues
If you encounter Docker-related errors:
```bash
# Check if Docker is running
docker info

# Clean up any stuck containers
make test-integration-cleanup
```

### Port Conflicts
If ports 5434 or 6381 are already in use:
```bash
# Check what's using the ports
lsof -i :5434
lsof -i :6381

# Stop conflicting processes or modify script configuration
```

### Database Migration Issues
If migrations fail:
```bash
# Check container logs
docker logs tmi-integration-postgres

# Manually run migrations
DATABASE_URL="postgresql://tmi_integration:integration_test_123@localhost:5434/tmi_integration_test" go run cmd/migrate/main.go
```

## Development

### Adding New Integration Tests

1. Add test functions to `sub_entities_integration_test.go`
2. Follow the pattern: `TestDatabase<Entity>Integration`
3. Use the `SubEntityIntegrationTestSuite` for database setup
4. Always test actual database persistence by creating and then retrieving data

### Running Specific Integration Tests

```bash
# Run only threat model integration tests
TEST_DB_HOST=localhost TEST_DB_PORT=5434 TEST_DB_USER=tmi_integration TEST_DB_PASSWORD=integration_test_123 TEST_DB_NAME=tmi_integration_test TEST_REDIS_HOST=localhost TEST_REDIS_PORT=6381 go test -v ./api -run TestDatabaseThreatModelIntegration

# Or modify the script to run specific tests
```

## CI/CD Integration

The integration test script is designed to work in CI/CD environments:

- Exits with proper error codes on failure
- Includes comprehensive logging with colors
- Automatically cleans up resources
- Uses consistent, non-conflicting ports
- Waits for services to be ready before proceeding

Example GitHub Actions usage:
```yaml
- name: Run Integration Tests
  run: make test-integration
```

## Writing Integration Tests

When writing integration tests, follow these patterns:

1. **Database Access**: Use the database stores directly instead of in-memory stores
2. **Authentication**: Create real users and tokens through the auth service
3. **HTTP Testing**: Use httptest.NewRecorder and real Gin routes
4. **Clean Setup/Teardown**: Always clean up test data after tests complete

### Test Structure

```go
func TestMyIntegration(t *testing.T) {
    suite := SetupSubEntityIntegrationTest(t)
    defer suite.TeardownSubEntityIntegrationTest(t)
    
    // Your test logic here
    req := suite.makeAuthenticatedRequest("GET", "/api/endpoint", nil)
    w := suite.executeRequest(req)
    suite.assertJSONResponse(t, w, http.StatusOK)
}
```

### Proper Object Creation Flow

When testing the API, follow the correct object hierarchy and creation flow:

#### 1. Threat Model Creation
```go
// First, create a threat model
threatModelData := map[string]interface{}{
    "name": "Test Threat Model",
    "owner": testUser.Email,
    "created_by": testUser.Email,
    "threat_model_framework": "STRIDE",
    // Include count fields with default values
    "document_count": 0,
    "source_count": 0,
    "diagram_count": 0,
    "threat_count": 0,
}

req := suite.makeAuthenticatedRequest("POST", "/threat_models", threatModelData)
w := suite.executeRequest(req)
response := suite.assertJSONResponse(t, w, http.StatusCreated)
threatModelID := response["id"].(string)
```

#### 2. Sub-Entity Creation (Diagrams, Documents, etc.)
```go
// Create diagrams using the threat model ID
diagramData := map[string]interface{}{
    "name": "Test Diagram",
    "description": "A test diagram",
}

// Use the proper sub-entity endpoint
path := fmt.Sprintf("/threat_models/%s/diagrams", threatModelID)
req := suite.makeAuthenticatedRequest("POST", path, diagramData)
w := suite.executeRequest(req)
response := suite.assertJSONResponse(t, w, http.StatusCreated)
diagramID := response["id"].(string)
```

#### 3. Sub-Entity Retrieval and Verification
```go
// Verify the diagram was created and is accessible via the threat model
getPath := fmt.Sprintf("/threat_models/%s/diagrams/%s", threatModelID, diagramID)
getReq := suite.makeAuthenticatedRequest("GET", getPath, nil)
getW := suite.executeRequest(getReq)
getResponse := suite.assertJSONResponse(t, getW, http.StatusOK)

// Verify the data matches what was created
assert.Equal(t, diagramID, getResponse["id"])
assert.Equal(t, diagramData["name"], getResponse["name"])
```

### Authentication Best Practices

**DO:**
- Use the built-in test OAuth provider ("test")
- Create users through the auth service with `CreateUser()`
- Generate tokens through `GenerateTokens()`
- Use the same authenticated user throughout related test operations
- Pass the Bearer token in the Authorization header

**DON'T:**
- Manually create authorization contexts or manipulate `TestFixtures`
- Try to bypass the authentication middleware
- Mix different user contexts within a single test flow
- Assume authentication state carries between different API calls

### Database Integration

The integration tests use real PostgreSQL and Redis instances running in Docker containers. This ensures:

- **True Integration**: Tests interact with the actual database schema and constraints
- **Data Persistence**: Verify that data survives across API calls
- **Concurrency Safety**: Test database locking and transaction behavior
- **Migration Validation**: Ensure database schema matches the application expectations

### Error Handling

When integration tests fail, check these common issues:

1. **Object Creation Order**: Ensure parent objects (threat models) exist before creating sub-entities
2. **Authentication Context**: Verify the same user context is used throughout the test
3. **Database State**: Check if previous test data wasn't properly cleaned up
4. **Endpoint URLs**: Ensure you're using the correct API endpoint paths
5. **Required Fields**: Include all required fields with appropriate default values