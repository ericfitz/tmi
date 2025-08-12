# Integration Test Implementation Plan - 100% Endpoint Coverage

## Executive Summary

This plan outlines the work required to achieve 100% integration test coverage for all API endpoints in the TMI project, following the comprehensive natural API flow methodology documented in `INTEGRATION_TESTING.md`.

**Current Status:** ~23% coverage (7/32 endpoints)  
**Target:** 100% coverage (32/32 endpoints)  
**Approach:** Natural hierarchy testing with database verification and Redis consistency validation

## Phase 1: Foundation and Immediate Fixes (Priority: Critical)

### 1.1 Fix Misleading Comment ✅

**Status:** COMPLETED

- **File:** `api/sub_entities_integration_test.go:226-228`
- **Action:** Updated incorrect comment about sub-entity testing being inappropriate

### 1.2 Add Missing Database Verification Helpers

**Estimated Effort:** 2-3 days
**Files to Create/Modify:**

- `api/integration_test_helpers.go` (new file)

**Required Helpers:**

```go
// Database verification methods for each entity type
func (suite *SubEntityIntegrationTestSuite) verifyThreatModelInDatabase(t *testing.T, id string, expectedData map[string]interface{})
func (suite *SubEntityIntegrationTestSuite) verifyThreatInDatabase(t *testing.T, id, threatModelID string, expectedData map[string]interface{})
func (suite *SubEntityIntegrationTestSuite) verifyDocumentInDatabase(t *testing.T, id, threatModelID string, expectedData map[string]interface{})
func (suite *SubEntityIntegrationTestSuite) verifySourceInDatabase(t *testing.T, id, threatModelID string, expectedData map[string]interface{})
func (suite *SubEntityIntegrationTestSuite) verifyDiagramInDatabase(t *testing.T, id, threatModelID string, expectedData map[string]interface{})
func (suite *SubEntityIntegrationTestSuite) verifyMetadataInDatabase(t *testing.T, parentID, entityType string, expectedData map[string]interface{})
func (suite *SubEntityIntegrationTestSuite) verifyCellInDatabase(t *testing.T, diagramID, cellID string, expectedData map[string]interface{})

// Negative verification (deletion testing)
func (suite *SubEntityIntegrationTestSuite) verifyThreatModelNotInDatabase(t *testing.T, id string)
func (suite *SubEntityIntegrationTestSuite) verifyNoOrphanedSubEntitiesInDatabase(t *testing.T, threatModelID string)
// ... similar for all entity types

// Field-specific verification
func (suite *SubEntityIntegrationTestSuite) verifyFieldInDatabase(t *testing.T, entityID, fieldName string, expectedValue interface{})
func (suite *SubEntityIntegrationTestSuite) assertFieldsMatch(t *testing.T, response map[string]interface{}, expectedData map[string]interface{})
func (suite *SubEntityIntegrationTestSuite) assertContainsEntity(t *testing.T, list []interface{}, entityID string)
```

### 1.3 Redis Configuration Management

**Estimated Effort:** 1 day
**Files to Create/Modify:**

- `api/integration_redis_test.go` (new file)
- Update existing test setup to support Redis toggle

**Implementation:**

```go
// Test suite variants
func TestIntegrationWithRedisEnabled(t *testing.T) {
    runFullIntegrationSuite(t, true)
}

func TestIntegrationWithRedisDisabled(t *testing.T) {
    runFullIntegrationSuite(t, false)
}

func runFullIntegrationSuite(t *testing.T, redisEnabled bool) {
    // Set Redis configuration
    os.Setenv("REDIS_ENABLED", strconv.FormatBool(redisEnabled))
    defer os.Unsetenv("REDIS_ENABLED")

    // Run all test categories
    t.Run("RootEntities", TestRootEntities)
    t.Run("SubEntities", TestSubEntities)
    t.Run("Metadata", TestMetadata)
    t.Run("Collaboration", TestCollaboration)
    t.Run("BatchOperations", TestBatchOperations)
    t.Run("Deletion", TestDeletion)
}
```

## Phase 2: Missing Endpoint Coverage (Priority: High)

### 2.1 Root Entity Completion

**Estimated Effort:** REMOVED - No standalone diagram endpoints exist in OpenAPI spec

**Note:** According to tmi-openapi.json, there are NO standalone diagram endpoints (`/diagrams`). All diagram operations are nested under threat models (`/threat_models/{threat_model_id}/diagrams`). This section is removed from the plan.

### 2.2 Sub-Entity Completion

**Estimated Effort:** 1-2 days
**Current Gap:** PATCH operations for threat model diagrams

**Missing Endpoints:**

- `PATCH /threat_models/:threat_model_id/diagrams/:diagram_id` - Patch threat model diagram

**Implementation File:** Update existing `api/diagram_integration_test.go`

### 2.3 Metadata Completion

**Estimated Effort:** REMOVED - No metadata endpoints exist in OpenAPI spec

**Note:** According to tmi-openapi.json, there are NO metadata endpoints (no `/metadata` paths found). All metadata appears to be handled as properties within the main entities. This section is removed from the plan.

### 2.4 Cell Operations (New Category)

**Estimated Effort:** REMOVED - No cell endpoints exist in OpenAPI spec

**Note:** According to tmi-openapi.json, there are NO cell-specific endpoints (no `/cells` paths found). Cell operations appear to be handled through diagram content updates. This section is removed from the plan.

## Phase 3: Advanced Features (Priority: Medium)

### 3.1 Collaboration Features

**Estimated Effort:** 2-3 days
**Current Gap:** Complete WebSocket collaboration testing

**Missing Endpoints (confirmed in OpenAPI spec):**

- `GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate`
- `POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate`
- `DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate`

**Note:** No standalone `/diagrams/:id/collaborate` endpoints exist. Only nested under threat models.

**Implementation File:** `api/integration_collaboration_test.go` (new)

**Special Considerations:**

- Requires WebSocket testing setup
- Need to test real-time message broadcasting
- Test collaboration session lifecycle
- Test concurrent user scenarios

### 3.2 Batch Operations

**Estimated Effort:** REMOVED - No batch endpoints exist in OpenAPI spec

**Note:** According to tmi-openapi.json, there are NO batch operation endpoints (no `/batch` paths found). All operations are individual CRUD operations only. This section is removed from the plan.

## Phase 4: Comprehensive Deletion Testing (Priority: Medium)

### 4.1 Individual Deletion Testing

**Estimated Effort:** 3-4 days
**Current Gap:** Systematic deletion verification

**Requirements:**

- Test deletion in reverse hierarchy order (deepest → shallowest)
- Verify database cleanup for each deletion
- Test deletion permissions and authorization
- Test deletion of entities with relationships

**Implementation File:** `api/integration_deletion_test.go` (new)

**Test Categories:**

```go
func TestIndividualDeletion(t *testing.T) {
    // Test deleting each entity type individually
    // Verify proper cleanup and no orphaned references
}

func TestCascadingDeletion(t *testing.T) {
    // Delete parent entities and verify cascade behavior
    // Test: Threat Model → All sub-entities deleted
    // Test: Diagram → All cells and metadata deleted
}

func TestDeletionPermissions(t *testing.T) {
    // Test role-based deletion permissions
    // Test cross-user deletion prevention
}
```

### 4.2 Orphan Prevention Testing

**Estimated Effort:** 2-3 days

**Requirements:**

- Test that deleting parents properly handles sub-entities
- Verify foreign key constraints are working
- Test edge cases (concurrent deletion, interrupted operations)

## Phase 4: Authentication and System Features (Priority: High)

### 4.1 Authentication Endpoints

**Estimated Effort:** 3-4 days
**Current Gap:** No auth endpoint testing

**Missing Endpoints (confirmed in OpenAPI spec):**

- `GET /` - API info endpoint
- `GET /auth/providers` - Get available OAuth providers
- `GET /auth/authorize/{provider}` - Start OAuth authorization flow
- `GET /auth/login` - Login endpoint
- `POST /auth/token` - Exchange authorization code for tokens
- `POST /auth/exchange/{provider}` - Exchange provider token
- `POST /auth/refresh` - Refresh access token
- `GET /auth/me` - Get current user information
- `GET /auth/callback` - OAuth callback handler
- `POST /auth/logout` - Logout user

**Implementation File:** `api/integration_auth_test.go` (new)

**Special Considerations:**

- Mock OAuth provider interactions
- Test JWT token lifecycle
- Test session management
- Test authentication error scenarios

## Implementation Schedule

### Sprint 1 (Week 1-2): Foundation

- ✅ Fix misleading comment (COMPLETED)
- Complete Phase 1: Database helpers and Redis management
- Start Phase 2.1: Root entity completion

### Sprint 2 (Week 3-4): Core Coverage

- Complete Phase 2: Missing endpoint coverage
- Start Phase 4: Deletion testing

### Sprint 3 (Week 5-6): Advanced Features

- Complete Phase 3: Collaboration and batch operations
- Complete Phase 4: Comprehensive deletion testing

### Sprint 4 (Week 7-8): Final Coverage

- Complete Phase 5: Authentication endpoints
- Integration testing and bug fixes
- Documentation updates

## Success Metrics

### Coverage Tracking

- **Current:** 7/32 endpoints (~22%)
- **Sprint 1 Target:** 15/32 endpoints (~47%)
- **Sprint 2 Target:** 22/32 endpoints (~69%)
- **Sprint 3 Target:** 29/32 endpoints (~91%)
- **Sprint 4 Target:** 32/32 endpoints (100%)

### Quality Gates

1. **Database Verification:** Every test must verify data persistence
2. **Redis Consistency:** All tests pass with Redis enabled/disabled
3. **Authentication Coverage:** All permission scenarios tested
4. **Error Handling:** Invalid inputs and edge cases covered
5. **Performance:** Test execution time under 5 minutes total

## File Organization Plan

### New Test Files to Create:

```
api/
├── integration_test_helpers.go           # Database verification helpers
├── integration_collaboration_test.go     # WebSocket collaboration
├── integration_deletion_test.go          # Comprehensive deletion testing
├── integration_auth_test.go              # Authentication endpoints
└── integration_redis_test.go             # Redis consistency testing
```

### Files to Update:

```
api/
├── sub_entities_integration_test.go      # Update comment ✅, add helpers
├── diagram_integration_test.go           # Add PATCH operations
└── *_integration_test.go                 # Add Redis toggle support
```

## Risk Mitigation

### Technical Risks:

1. **WebSocket Testing Complexity:** Start with simple collaboration scenarios
2. **Database Performance:** Use test database isolation and cleanup
3. **Test Execution Time:** Parallelize independent test suites
4. **Flaky Tests:** Add proper wait conditions and retry logic

### Delivery Risks:

1. **Scope Creep:** Stick to endpoint coverage, avoid feature enhancements
2. **Integration Issues:** Test each phase incrementally
3. **Resource Constraints:** Prioritize critical gaps first (Phases 1-2)

## Conclusion

This implementation plan provides a systematic approach to achieving 100% integration test coverage while following the natural API flow methodology. The phased approach ensures critical gaps are addressed first while building a solid foundation for comprehensive testing.

**Expected Outcome:**

- 100% endpoint coverage (32/32 endpoints) with database verification
- Redis consistency validation
- Comprehensive deletion and cascade testing
- Production-ready integration test suite
- Clear documentation and maintainable test structure

**Total Estimated Effort:** 3-4 weeks with 1-2 developers (significantly reduced after removing non-existent endpoints)
**Key Dependencies:** Database helpers foundation (Phase 1) must be completed before other phases
