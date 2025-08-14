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
./scripts/start-integration-tests.sh
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
   - **Input validation for calculated fields** (see below)

2. **Authentication & Authorization**

   - JWT token creation and validation
   - Role-based access control (RBAC)
   - User creation and management

3. **Database Integration**
   - Real PostgreSQL transactions
   - Schema constraint enforcement
   - Count field management and validation

#### Calculated Fields Validation

The threat model API now includes comprehensive input validation to prevent submission of calculated/read-only fields:

**Prohibited Fields:**

- Count fields: `document_count`, `source_count`, `diagram_count`, `threat_count` (calculated automatically from database)
- Server-controlled fields: `id`, `created_at`, `modified_at`, `created_by` (managed by server)
- Owner field: `owner` (set automatically for POST, only changeable by owners for PUT/PATCH)
- Sub-entity arrays: `diagrams`, `documents`, `threats`, `sourceCode` (managed via sub-entity endpoints)

**Validation Coverage:**

- POST `/threat_models`: Rejects all prohibited fields with descriptive error messages
- PUT `/threat_models/:threat_model_id`: Uses restricted request struct to prevent prohibited fields
- PATCH `/threat_models/:threat_model_id`: Validates JSON patch paths against prohibited field list

**Error Responses:**
All prohibited field submissions return HTTP 400 with descriptive error messages explaining why each field cannot be set directly.

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
// NOTE: Do NOT include count fields or other calculated/read-only fields
// These are now rejected by the API and will cause 400 Bad Request errors
threatModelData := map[string]interface{}{
    "name": "Test Threat Model",
    "description": "A test threat model for integration testing",
    // Do not include: owner, created_by, id, timestamps, or count fields
    // These are set automatically by the server
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

## Comprehensive Integration Testing Methodology

### Test Philosophy and Approach

The integration test suite follows a **natural API flow pattern** that mirrors real-world usage:

1. **Creation Flow**: Follow the natural hierarchy (Threat Model → Sub-entities → Sub-sub-entities)
2. **Database Verification**: At each step, verify data persistence and accuracy in the database
3. **Retrieval Validation**: Test GET endpoints after creation to ensure data integrity
4. **Mutation Testing**: Test PUT/PATCH operations and verify changes persist
5. **Deletion Testing**: Test deletion in reverse hierarchy order with cascade verification

### API Entity Hierarchy

The TMI API follows a clear hierarchical structure:

```
Root Entities:
├── Threat Models (/threat_models)
├── Standalone Diagrams (/diagrams)

Sub-Entities (under Threat Models):
├── Diagrams (/threat_models/:threat_model_id/diagrams)
├── Threats (/threat_models/:threat_model_id/threats)
├── Documents (/threat_models/:threat_model_id/documents)
├── Sources (/threat_models/:threat_model_id/sources)

Sub-Sub-Entities (Metadata):
├── Threat Model Metadata (/threat_models/:threat_model_id/metadata)
├── Diagram Metadata (/threat_models/:threat_model_id/diagrams/:diagram_id/metadata)
├── Threat Metadata (/threat_models/:threat_model_id/threats/:threat_id/metadata)
├── Document Metadata (/threat_models/:threat_model_id/documents/:document_id/metadata)
├── Source Metadata (/threat_models/:threat_model_id/sources/:source_id/metadata)

Sub-Sub-Sub-Entities:
├── Diagram Cells (/diagrams/:id/cells/:cell_id)
├── Cell Metadata (/diagrams/:id/cells/:cell_id/metadata)
```

### Comprehensive Test Patterns

#### 1. **Creation and Persistence Testing (POST)**

**Pattern**: Create → Verify Database → Cache IDs → Use for Sub-entities

```go
func TestComprehensiveEntityCreation(t *testing.T) {
    suite := SetupSubEntityIntegrationTest(t)
    defer suite.TeardownSubEntityIntegrationTest(t)

    // 1. Create root entity (threat model)
    threatModelData := map[string]interface{}{
        "name": "Test Threat Model",
        "description": "Test description",
        // Do not include calculated fields
    }

    req := suite.makeAuthenticatedRequest("POST", "/threat_models", threatModelData)
    w := suite.executeRequest(req)
    tmResponse := suite.assertJSONResponse(t, w, http.StatusCreated)
    threatModelID := tmResponse["id"].(string)

    // Verify database persistence
    suite.verifyThreatModelInDatabase(t, threatModelID, threatModelData)

    // 2. Create sub-entity (threat)
    threatData := map[string]interface{}{
        "name": "SQL Injection",
        "description": "Database injection attack",
    }

    threatPath := fmt.Sprintf("/threat_models/%s/threats", threatModelID)
    threatReq := suite.makeAuthenticatedRequest("POST", threatPath, threatData)
    threatW := suite.executeRequest(threatReq)
    threatResponse := suite.assertJSONResponse(t, threatW, http.StatusCreated)
    threatID := threatResponse["id"].(string)

    // Verify sub-entity database persistence
    suite.verifyThreatInDatabase(t, threatID, threatModelID, threatData)

    // 3. Create sub-sub-entity (threat metadata)
    metadataData := map[string]interface{}{
        "key": "priority",
        "value": "high",
    }

    metadataPath := fmt.Sprintf("/threat_models/%s/threats/%s/metadata", threatModelID, threatID)
    metadataReq := suite.makeAuthenticatedRequest("POST", metadataPath, metadataData)
    metadataW := suite.executeRequest(metadataReq)
    metadataResponse := suite.assertJSONResponse(t, metadataW, http.StatusCreated)

    // Verify metadata database persistence
    suite.verifyMetadataInDatabase(t, threatID, "threat", metadataData)
}
```

#### 2. **Retrieval Testing (GET)**

**Pattern**: After creation, test all GET endpoints to verify data integrity

```go
func TestComprehensiveEntityRetrieval(t *testing.T) {
    // Use previously created entities from creation test

    // Test individual retrieval
    getReq := suite.makeAuthenticatedRequest("GET", "/threat_models/" + threatModelID, nil)
    getW := suite.executeRequest(getReq)
    getResponse := suite.assertJSONResponse(t, getW, http.StatusOK)

    // Verify all fields match database
    suite.assertFieldsMatch(t, getResponse, threatModelData)

    // Test list retrieval
    listReq := suite.makeAuthenticatedRequest("GET", "/threat_models", nil)
    listW := suite.executeRequest(listReq)
    listResponse := suite.assertJSONArrayResponse(t, listW, http.StatusOK)

    // Verify our created item is in the list
    suite.assertContainsEntity(t, listResponse, threatModelID)
}
```

#### 3. **Mutation Testing (PUT/PATCH)**

**Pattern**: Modify → Verify Database → Verify GET returns updated data

```go
func TestComprehensiveEntityMutation(t *testing.T) {
    // Test PUT (complete replacement)
    modifiedAta := map[string]interface{}{
        "name": "Updated Threat Model",
        "description": "Updated description",
        // Include all required fields for PUT
    }

    putReq := suite.makeAuthenticatedRequest("PUT", "/threat_models/" + threatModelID, modifiedAta)
    putW := suite.executeRequest(putReq)
    putResponse := suite.assertJSONResponse(t, putW, http.StatusOK)

    // Verify database was updated
    suite.verifyThreatModelInDatabase(t, threatModelID, modifiedAta)

    // Test PATCH (partial update)
    patchData := []map[string]interface{}{
        {
            "op": "replace",
            "path": "/name",
            "value": "Patched Name",
        },
    }

    patchReq := suite.makeAuthenticatedRequest("PATCH", "/threat_models/" + threatModelID, patchData)
    patchW := suite.executeRequest(patchReq)
    suite.assertJSONResponse(t, patchW, http.StatusOK)

    // Verify PATCH was applied
    suite.verifyFieldInDatabase(t, threatModelID, "name", "Patched Name")
}
```

#### 4. **Redis Testing (Both Enabled and Disabled)**

**Pattern**: Run same test suite with Redis on/off to verify caching doesn't introduce bugs

```go
func TestWithRedisEnabled(t *testing.T) {
    // Set environment variable to enable Redis
    os.Setenv("REDIS_ENABLED", "true")
    defer os.Unsetenv("REDIS_ENABLED")

    // Run standard test suite
    runComprehensiveTestSuite(t)
}

func TestWithRedisDisabled(t *testing.T) {
    // Set environment variable to disable Redis
    os.Setenv("REDIS_ENABLED", "false")
    defer os.Unsetenv("REDIS_ENABLED")

    // Run same test suite - should get identical results
    runComprehensiveTestSuite(t)
}
```

#### 5. **Deletion Testing (DELETE)**

**Pattern**: Delete in reverse hierarchy order, test both individual and cascading deletion

```go
func TestComprehensiveDeletion(t *testing.T) {
    // Test individual deletion (deepest first)

    // 1. Delete sub-sub-entity (metadata)
    metadataDeleteReq := suite.makeAuthenticatedRequest("DELETE",
        fmt.Sprintf("/threat_models/%s/threats/%s/metadata/priority", threatModelID, threatID), nil)
    metadataDeleteW := suite.executeRequest(metadataDeleteReq)
    assert.Equal(t, http.StatusNoContent, metadataDeleteW.Code)

    // Verify metadata deleted from database
    suite.verifyMetadataNotInDatabase(t, threatID, "priority")

    // 2. Delete sub-entity (threat)
    threatDeleteReq := suite.makeAuthenticatedRequest("DELETE",
        fmt.Sprintf("/threat_models/%s/threats/%s", threatModelID, threatID), nil)
    threatDeleteW := suite.executeRequest(threatDeleteReq)
    assert.Equal(t, http.StatusNoContent, threatDeleteW.Code)

    // Verify threat deleted from database
    suite.verifyThreatNotInDatabase(t, threatID)

    // 3. Delete root entity (threat model)
    tmDeleteReq := suite.makeAuthenticatedRequest("DELETE", "/threat_models/" + threatModelID, nil)
    tmDeleteW := suite.executeRequest(tmDeleteReq)
    assert.Equal(t, http.StatusNoContent, tmDeleteW.Code)

    // Verify threat model deleted from database
    suite.verifyThreatModelNotInDatabase(t, threatModelID)
}

func TestCascadingDeletion(t *testing.T) {
    // Create full hierarchy
    threatModelID := suite.createThreatModelWithSubEntities(t)

    // Delete root entity - should cascade delete all sub-entities
    deleteReq := suite.makeAuthenticatedRequest("DELETE", "/threat_models/" + threatModelID, nil)
    deleteW := suite.executeRequest(deleteReq)
    assert.Equal(t, http.StatusNoContent, deleteW.Code)

    // Verify ALL related entities were cascade deleted
    suite.verifyThreatModelNotInDatabase(t, threatModelID)
    suite.verifyNoOrphanedSubEntitiesInDatabase(t, threatModelID)
}
```

### Database Verification Helpers

Every test must include database verification to ensure data persistence:

```go
func (suite *SubEntityIntegrationTestSuite) verifyThreatModelInDatabase(t *testing.T, id string, expectedData map[string]interface{}) {
    var tm ThreatModel
    err := suite.dbManager.DB.Where("id = ?", id).First(&tm).Error
    require.NoError(t, err, "Threat model should exist in database")

    // Verify each field matches
    assert.Equal(t, expectedData["name"], tm.Name)
    assert.Equal(t, expectedData["description"], *tm.Description)

    // Verify timestamps are set
    assert.NotZero(t, tm.CreatedAt)
    assert.NotZero(t, tm.ModifiedAt)

    // Verify calculated fields are correct
    // (count fields should be calculated from actual sub-entities)
}
```

### Bulk and Batch Operation Testing

Test bulk operations that create/modify multiple entities:

```go
func TestBulkOperations(t *testing.T) {
    // Test bulk creation
    bulkData := []map[string]interface{}{
        {"name": "Threat 1", "description": "First bulk threat"},
        {"name": "Threat 2", "description": "Second bulk threat"},
        {"name": "Threat 3", "description": "Third bulk threat"},
    }

    bulkReq := suite.makeAuthenticatedRequest("POST",
        fmt.Sprintf("/threat_models/%s/threats/bulk", threatModelID), bulkData)
    bulkW := suite.executeRequest(bulkReq)
    bulkResponse := suite.assertJSONArrayResponse(t, bulkW, http.StatusCreated)

    // Verify all items created in database
    assert.Len(t, bulkResponse, 3)
    for _, item := range bulkResponse {
        threatID := item["id"].(string)
        suite.verifyThreatInDatabase(t, threatID, threatModelID, map[string]interface{}{
            "name": item["name"].(string),
        })
    }
}
```

### Coverage Requirements

To achieve 100% endpoint coverage, every endpoint in `gin_adapter.go` must have integration tests covering:

1. **Happy Path**: Successful operation with valid data
2. **Error Cases**: Invalid data, missing authentication, insufficient permissions
3. **Edge Cases**: Empty data, maximum field lengths, special characters
4. **Database Persistence**: Data correctly stored and retrievable
5. **Redis Consistency**: Same behavior with/without Redis enabled

### Test Organization

Organize tests by entity hierarchy and operation type:

```
api/
├── integration_root_entities_test.go     # Threat models, standalone diagrams
├── integration_sub_entities_test.go      # Threats, documents, sources, diagrams
├── integration_metadata_test.go          # All metadata operations
├── integration_collaboration_test.go     # WebSocket collaboration features
├── integration_batch_operations_test.go  # Bulk/batch operations
├── integration_deletion_test.go          # Deletion and cascading
└── integration_redis_consistency_test.go # Redis enabled/disabled comparison
```

This comprehensive methodology ensures that the integration tests provide confidence that the entire API works correctly in production-like conditions while maintaining data integrity throughout the entity hierarchy.
