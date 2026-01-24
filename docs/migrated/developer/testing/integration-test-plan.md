# Integration Test Plan: 100% API Coverage

<!-- VERIFICATION SUMMARY:
- Total operations: 178 across 92 paths (verified via jq count of OpenAPI spec)
- Framework package path verified: github.com/ericfitz/tmi/test/integration/framework
- OAuth stub URL verified: http://localhost:8079 (OAuthStubURL constant in framework/oauth.go)
- CATS tool verified: https://github.com/Endava/cats (REST API Fuzzer)
- Tier directories exist but are mostly empty (tier2_features, tier3_edge_cases have only .gitkeep)
- Tier-specific make targets (test-integration-tier1/2/3) do NOT exist in Makefile
- scripts/check-integration-coverage.sh does NOT exist
- Verified 2025-01-24
-->

## Executive Summary

This document outlines the comprehensive plan to achieve 100% integration test coverage for the TMI API using the new OpenAPI-driven integration test framework.

### Current State
- **Total API Operations**: 178 operations across 92 endpoint paths <!-- Verified via OpenAPI spec -->
- **Current Coverage**: 7 test files implemented (Tier 1 partially complete)
  - `example_test.go` - Framework demonstration
  - `oauth_flow_test.go` - OAuth authentication tests
  - `threat_model_crud_test.go` - Threat model CRUD operations
  - `diagram_crud_test.go` - Diagram CRUD operations
  - `user_operations_test.go` - User management operations
  - `user_preferences_test.go` - User preferences tests
  - `admin_promotion_test.go` - Admin promotion tests
- **Coverage Goal**: 100% of all 178 operations

### API Resource Distribution
- **threat_models**: 105 operations (60% of API)
- **admin**: 27 operations (16% of API)
- **oauth2**: 9 operations (5% of API)
- **webhooks**: 7 operations (4% of API)
- **saml**: 7 operations (4% of API)
- **addons**: 5 operations (3% of API)
- **.well-known**: 4 operations (2% of API)
- **invocations**: 3 operations (2% of API)
- **client_credentials**: 3 operations (2% of API)
- **users**: 2 operations (1% of API)
- **collaboration**: 1 operation (1% of API)
- **root**: 1 operation (1% of API)

## Test Organization Strategy

### Three-Tier Test Structure

#### Tier 1: Core Workflows (CI/CD - Every Commit)
**Run Time Target**: < 2 minutes
**Purpose**: Critical path validation, smoke tests

Tests that exercise the most critical user workflows and API functionality:

1. **oauth_flow_test.go** - OAuth & Authentication (9 operations)
   - Authorization Code flow with PKCE
   - Token refresh
   - Token revocation
   - User info retrieval
   - Provider listing

2. **threat_model_crud_test.go** - Core CRUD Operations (15 operations)
   - Threat model create/read/update/patch/delete
   - Threat create/read/update/patch/delete
   - List operations with pagination
   - Basic validation and error cases

3. **diagram_crud_test.go** - Diagram Management (10 operations)
   - Diagram create/read/update/delete
   - Diagram model GET (multi-format)
   - Collaboration session management
   - Basic diagram operations

4. **user_operations_test.go** - User Management (2 operations)
   - Get current user
   - Delete current user account

**Total Tier 1 Coverage**: ~36 operations (21%)

#### Tier 2: Feature Tests (Nightly Builds)
**Run Time Target**: < 10 minutes
**Purpose**: Complete feature validation, integration scenarios

Tests organized by feature domain:

1. **metadata_operations_test.go** - Metadata CRUD (30 operations)
   - Metadata for all resource types (threat_models, threats, diagrams, assets, documents, notes, repositories)
   - Single key operations (GET/PUT/DELETE)
   - Bulk operations (POST/PUT)
   - Metadata inheritance and propagation

2. **asset_management_test.go** - Asset Operations (12 operations)
   - Asset CRUD with metadata
   - Bulk asset operations
   - Asset relationships to threat models

3. **document_management_test.go** - Document Operations (12 operations)
   - Document CRUD with metadata
   - Bulk document operations
   - Document versioning scenarios

4. **note_management_test.go** - Note Operations (12 operations)
   - Note CRUD with metadata
   - Note relationships to threat models
   - Bulk note operations

5. **repository_management_test.go** - Repository Operations (12 operations)
   - Repository CRUD with metadata
   - Bulk repository operations
   - Repository relationships

6. **webhook_workflow_test.go** - Webhook Lifecycle (7 operations)
   - Subscription create/read/update/delete
   - Test webhook delivery
   - Delivery tracking and retrieval
   - Event filtering

7. **addon_workflow_test.go** - Addon Lifecycle (5 operations)
   - Addon registration and listing
   - Addon invocation
   - Invocation status tracking
   - Error handling

8. **client_credentials_test.go** - Service Account Auth (3 operations)
   - Client credential creation
   - Client credential listing
   - Client credential deletion
   - Token exchange with client credentials

9. **collaboration_test.go** - Collaboration Features (1 operation)
   - List active collaboration sessions
   - Session state validation

10. **well_known_test.go** - Discovery Endpoints (4 operations)
    - OpenID configuration
    - OAuth authorization server metadata
    - JWKS retrieval
    - OAuth protected resource metadata

11. **saml_workflow_test.go** - SAML Authentication (7 operations)
    - SAML provider listing
    - SAML login flow
    - Metadata retrieval
    - Assertion consumer service (ACS)
    - Single logout (SLO)
    - User listing per provider

**Total Tier 2 Coverage**: ~105 operations (60%)

#### Tier 3: Edge Cases & Admin (On-Demand / Weekly)
**Run Time Target**: < 15 minutes
**Purpose**: Authorization, edge cases, admin operations, error conditions

1. **admin_users_test.go** - User Administration (7 operations)
   - User listing and retrieval
   - User creation and updates (PATCH)
   - User deletion
   - User search and filtering

2. **admin_groups_test.go** - Group Administration (10 operations)
   - Group CRUD operations
   - Group member management (add/remove)
   - Group membership listing
   - Group permissions

3. **admin_quotas_test.go** - Quota Management (10 operations)
   - Quota CRUD for users, addons, webhooks
   - Quota enforcement scenarios
   - Quota retrieval and updates
   - Default quota handling

4. **authorization_test.go** - RBAC & Permissions (Cross-cutting)
   - Reader/Writer/Owner role enforcement
   - Resource ownership transfer
   - Permission denial scenarios
   - Cross-user access attempts

5. **pagination_test.go** - Pagination & Filtering (Cross-cutting)
   - Limit/offset on all list endpoints
   - Large result set handling
   - Empty result sets
   - Boundary conditions

6. **error_handling_test.go** - Error Scenarios (Cross-cutting)
   - 400 Bad Request (invalid payloads)
   - 401 Unauthorized (missing/invalid tokens)
   - 403 Forbidden (insufficient permissions)
   - 404 Not Found (missing resources)
   - 409 Conflict (duplicate resources)
   - 422 Unprocessable Entity (validation errors)
   - 500 Internal Server Error scenarios

7. **bulk_operations_test.go** - Bulk Operation Patterns (Cross-cutting)
   - Bulk create operations
   - Bulk update operations (PUT/PATCH)
   - Bulk delete operations
   - Partial success handling
   - Transaction rollback scenarios

**Total Tier 3 Coverage**: ~33 operations (19%)

### Coverage Summary by Tier

| Tier | Operations | Percentage | Run Frequency | Time Budget |
|------|-----------|------------|---------------|-------------|
| Tier 1 | ~36 | 20% | Every commit | < 2 min |
| Tier 2 | ~105 | 59% | Nightly | < 10 min |
| Tier 3 | ~37 | 21% | Weekly | < 15 min |
| **Total** | **178** | **100%** | - | **< 27 min** |

## Test File Organization

### Current Structure (Actual)

```
test/integration/workflows/
├── example_test.go              # Framework demonstration
├── oauth_flow_test.go           # OAuth authentication (IMPLEMENTED)
├── threat_model_crud_test.go    # Threat model CRUD (IMPLEMENTED)
├── diagram_crud_test.go         # Diagram CRUD (IMPLEMENTED)
├── user_operations_test.go      # User operations (IMPLEMENTED)
├── user_preferences_test.go     # User preferences (IMPLEMENTED)
├── admin_promotion_test.go      # Admin promotion (IMPLEMENTED)
├── tier2_features/
│   └── .gitkeep                 # Placeholder - tests not yet implemented
└── tier3_edge_cases/
    └── .gitkeep                 # Placeholder - tests not yet implemented
```

### Planned Structure (Target)

<!-- NEEDS-REVIEW: tier1_core directory does not exist; current Tier 1 tests are in workflows root -->

```
test/integration/workflows/
├── tier1_core/                  # NOT YET CREATED - Tier 1 tests currently in root
│   ├── oauth_flow_test.go
│   ├── threat_model_crud_test.go
│   ├── diagram_crud_test.go
│   └── user_operations_test.go
├── tier2_features/              # EXISTS - empty except .gitkeep
│   ├── metadata_operations_test.go
│   ├── asset_management_test.go
│   ├── document_management_test.go
│   ├── note_management_test.go
│   ├── repository_management_test.go
│   ├── webhook_workflow_test.go
│   ├── addon_workflow_test.go
│   ├── client_credentials_test.go
│   ├── collaboration_test.go
│   ├── well_known_test.go
│   └── saml_workflow_test.go
├── tier3_edge_cases/            # EXISTS - empty except .gitkeep
│   ├── admin_users_test.go
│   ├── admin_groups_test.go
│   ├── admin_quotas_test.go
│   ├── authorization_test.go
│   ├── pagination_test.go
│   ├── error_handling_test.go
│   └── bulk_operations_test.go
└── example_test.go
```

## Implementation Roadmap

### Phase 1: Core Foundation (Weeks 1-2) - PARTIALLY COMPLETE
**Goal**: Establish Tier 1 tests - critical path coverage

1. **Week 1: OAuth & Threat Models** - COMPLETE
   - [x] oauth_flow_test.go (9 operations)
   - [x] threat_model_crud_test.go (15 operations)
   - [x] Validate CI/CD integration
   - [x] Establish test patterns and conventions

2. **Week 2: Diagrams & Users** - COMPLETE
   - [x] diagram_crud_test.go (10 operations)
   - [x] user_operations_test.go (2 operations)
   - [x] user_preferences_test.go (additional user tests)
   - [x] admin_promotion_test.go (admin promotion)
   - [ ] Tier 1 complete: 36/178 operations (20%)
   - [ ] CI/CD smoke test suite operational

**Milestone**: Critical path mostly covered, tests run via `make test-integration`

### Phase 2: Feature Coverage (Weeks 3-6)
**Goal**: Expand to Tier 2 - comprehensive feature validation

3. **Week 3: Metadata & Assets**
   - [ ] metadata_operations_test.go (30 operations)
   - [ ] asset_management_test.go (12 operations)
   - [ ] Running total: 78/174 operations (45%)

4. **Week 4: Documents, Notes, Repositories**
   - [ ] document_management_test.go (12 operations)
   - [ ] note_management_test.go (12 operations)
   - [ ] repository_management_test.go (12 operations)
   - [ ] Running total: 114/174 operations (66%)

5. **Week 5: Webhooks, Addons, Credentials**
   - [ ] webhook_workflow_test.go (7 operations)
   - [ ] addon_workflow_test.go (5 operations)
   - [ ] client_credentials_test.go (3 operations)
   - [ ] Running total: 129/174 operations (74%)

6. **Week 6: Discovery & SAML**
   - [ ] collaboration_test.go (1 operation)
   - [ ] well_known_test.go (4 operations)
   - [ ] saml_workflow_test.go (7 operations)
   - [ ] Tier 2 complete: 141/174 operations (81%)
   - [ ] Nightly build test suite operational

**Milestone**: All major features covered, nightly regression testing active

### Phase 3: Edge Cases & Admin (Weeks 7-8)
**Goal**: Complete Tier 3 - admin operations and edge cases

7. **Week 7: Admin Operations**
   - [ ] admin_users_test.go (7 operations)
   - [ ] admin_groups_test.go (10 operations)
   - [ ] admin_quotas_test.go (10 operations)
   - [ ] Running total: 168/174 operations (97%)

8. **Week 8: Cross-Cutting Concerns**
   - [ ] authorization_test.go (RBAC scenarios)
   - [ ] pagination_test.go (all list endpoints)
   - [ ] error_handling_test.go (error scenarios)
   - [ ] bulk_operations_test.go (bulk patterns)
   - [ ] **100% Coverage Achieved**: 174/174 operations

**Milestone**: Complete API coverage, full regression suite operational

### Phase 4: Optimization & Maintenance (Week 9+)
**Goal**: Optimize test performance and establish maintenance practices

9. **Week 9: Performance Optimization**
   - [ ] Parallelize independent tests
   - [ ] Optimize fixture creation
   - [ ] Reduce test execution time
   - [ ] Implement test result caching where appropriate

10. **Ongoing: Maintenance**
    - [ ] Monitor test stability and flakiness
    - [ ] Update tests as API evolves
    - [ ] Add new tests for new endpoints
    - [ ] Refactor common patterns into framework utilities

## Test Development Guidelines

### Standard Test Structure

Each test file should follow this pattern:

```go
package workflows

import (
    "os"
    "testing"
    "github.com/ericfitz/tmi/test/integration/framework"
)

// TestResourceCRUD tests complete CRUD lifecycle for Resource
func TestResourceCRUD(t *testing.T) {
    // 1. Setup
    if os.Getenv("INTEGRATION_TESTS") != "true" {
        t.Skip("Skipping integration test")
    }

    serverURL := os.Getenv("TMI_SERVER_URL")
    if serverURL == "" {
        serverURL = "http://localhost:8080"
    }

    // Ensure OAuth stub is running
    if err := framework.EnsureOAuthStubRunning(); err != nil {
        t.Fatalf("OAuth stub not running: %v", err)
    }

    // 2. Authenticate
    userID := framework.UniqueUserID()
    tokens, err := framework.AuthenticateUser(userID)
    framework.AssertNoError(t, err, "Authentication failed")

    // 3. Create client
    client, err := framework.NewClient(serverURL, tokens)
    framework.AssertNoError(t, err, "Client creation failed")

    // 4. Test workflow (subtests for each operation)
    t.Run("Create", func(t *testing.T) {
        // Test create operation
    })

    t.Run("Read", func(t *testing.T) {
        // Test read operation
    })

    t.Run("Update", func(t *testing.T) {
        // Test update operation
    })

    t.Run("Delete", func(t *testing.T) {
        // Test delete operation
    })
}
```

### Test Naming Conventions

- **Test files**: `<resource>_<operation>_test.go` (e.g., `threat_model_crud_test.go`)
- **Test functions**: `Test<Resource><Operation>` (e.g., `TestThreatModelCRUD`)
- **Subtests**: Descriptive names for each step (e.g., `t.Run("CreateWithValidData")`)

### Coverage Tracking

Track test coverage using operation mapping:

```go
// TestMetadataOperations covers the following OpenAPI operations:
// - GET /threat_models/{id}/metadata (listThreatModelMetadata)
// - POST /threat_models/{id}/metadata (createThreatModelMetadata)
// - GET /threat_models/{id}/metadata/{key} (getThreatModelMetadata)
// - PUT /threat_models/{id}/metadata/{key} (updateThreatModelMetadata)
// - DELETE /threat_models/{id}/metadata/{key} (deleteThreatModelMetadata)
// - POST /threat_models/{id}/metadata/bulk (bulkCreateThreatModelMetadata)
// - PUT /threat_models/{id}/metadata/bulk (bulkUpdateThreatModelMetadata)
func TestMetadataOperations(t *testing.T) {
    // ... test implementation
}
```

### Make Target Updates

<!-- NEEDS-REVIEW: These tier-specific make targets do NOT currently exist in the Makefile -->
<!-- Current integration tests run via: make test-integration (or test-integration-pg, test-integration-oci) -->

**Current Make Targets** (verified in Makefile):
- `make test-integration` - Runs all integration tests via `scripts/run-integration-tests-pg.sh`
- `make test-integration-pg` - PostgreSQL backend (default)
- `make test-integration-oci` - Oracle ADB backend

**Proposed Tier-Specific Targets** (NOT YET IMPLEMENTED):

```makefile
# Run Tier 1 tests (CI/CD)
test-integration-tier1:
	@echo "Running Tier 1 integration tests..."
	INTEGRATION_TESTS=true go test -v ./test/integration/workflows/tier1_core/...

# Run Tier 2 tests (Nightly)
test-integration-tier2:
	@echo "Running Tier 2 integration tests..."
	INTEGRATION_TESTS=true go test -v ./test/integration/workflows/tier2_features/...

# Run Tier 3 tests (Weekly)
test-integration-tier3:
	@echo "Running Tier 3 integration tests..."
	INTEGRATION_TESTS=true go test -v ./test/integration/workflows/tier3_edge_cases/...

# Run all integration tests
test-integration-all: test-integration-tier1 test-integration-tier2 test-integration-tier3

# Run integration tests with coverage report
test-integration-coverage:
	@echo "Running integration tests with coverage..."
	INTEGRATION_TESTS=true go test -v -coverprofile=coverage-integration.out ./test/integration/workflows/...
	go tool cover -html=coverage-integration.out -o coverage-integration.html
```

## Success Criteria

### Definition of Done for Each Test
- [ ] All operations for the resource/workflow are covered
- [ ] OpenAPI validation passes for all requests/responses
- [ ] Test includes happy path and at least 2 error scenarios
- [ ] Test cleanup properly deletes created resources
- [ ] Test uses unique IDs to avoid conflicts
- [ ] Test has descriptive subtests for each operation
- [ ] Test includes operation ID comments for traceability
- [ ] Test passes consistently (no flakiness)

### Definition of Done for Each Phase
- [ ] All tests in phase implemented and passing
- [ ] Make targets updated and documented
- [ ] Coverage tracking updated in this document
- [ ] CI/CD integration tested (for Tier 1)
- [ ] Code review completed
- [ ] Documentation updated

### Definition of Done for 100% Coverage
- [ ] All 178 operations have test coverage
- [ ] All tests pass in CI/CD environment
- [ ] Test execution time meets budget (< 27 minutes total)
- [ ] Coverage report shows 100% of OpenAPI operations tested
- [ ] Flaky test rate < 1%
- [ ] Documentation complete and up-to-date

## Monitoring & Maintenance

### Coverage Tracking Dashboard

<!-- NEEDS-REVIEW: scripts/check-integration-coverage.sh does NOT exist - this is a proposed feature -->

Create a simple tracking mechanism to monitor progress:

```bash
# Proposed script to check coverage progress (NOT YET IMPLEMENTED)
scripts/check-integration-coverage.sh

# Output example:
# Integration Test Coverage Report
# ================================
# Total Operations: 178
# Tested Operations: 141
# Coverage: 79%
#
# By Tier:
# Tier 1: 36/36 (100%)
# Tier 2: 105/105 (100%)
# Tier 3: 0/37 (0%)
#
# By Resource:
# threat_models: 85/105 (81%)
# admin: 0/27 (0%)
# ...
```

### Weekly Review Process
1. Review test execution times and optimize slow tests
2. Analyze test failures and fix root causes
3. Update coverage tracking document
4. Identify and eliminate flaky tests
5. Review and update test fixtures as API evolves

### Continuous Improvement
- Add new tests immediately when new endpoints are added
- Refactor common patterns into framework utilities
- Share learnings across test files
- Maintain test execution speed through optimization
- Keep OpenAPI spec synchronized with tests

## Risk Assessment & Mitigation

### Identified Risks

1. **Test Execution Time**
   - **Risk**: Full suite exceeds time budget
   - **Mitigation**: Parallelize tests, use tier system, optimize fixtures

2. **Test Flakiness**
   - **Risk**: Network/timing issues cause intermittent failures
   - **Mitigation**: Proper cleanup, retries for network calls, unique IDs

3. **Resource Cleanup**
   - **Risk**: Failed tests leave orphaned resources
   - **Mitigation**: Use defer for cleanup, cleanup hooks, unique test namespaces

4. **OAuth Dependency**
   - **Risk**: OAuth stub becomes a bottleneck
   - **Mitigation**: Token caching, parallel stub support, fallback mechanisms

5. **API Evolution**
   - **Risk**: Tests become stale as API changes
   - **Mitigation**: OpenAPI validation catches mismatches, regular reviews

## Next Steps

1. ~~**Review and approve this plan** with stakeholders~~ - Plan approved
2. ~~**Set up tier directory structure** in test/integration/workflows/~~ - Directories exist (tier2_features, tier3_edge_cases)
3. ~~**Begin Phase 1, Week 1** implementation~~ - COMPLETE (Tier 1 tests implemented)
4. **Create tier1_core directory** and move Tier 1 tests into it
5. **Add tier-specific make targets** to Makefile
6. **Establish coverage tracking** mechanism (create scripts/check-integration-coverage.sh)
7. **Begin Phase 2** - Tier 2 feature tests
8. **Schedule weekly progress reviews**

## Appendix: Operation Reference

### Complete Operation Inventory

See the API Coverage Analysis section for the complete breakdown of all 178 operations organized by resource type.

The OpenAPI specification (`docs/reference/apis/tmi-openapi.json`) contains:
- **92 endpoint paths**
- **178 total operations** (GET, POST, PUT, PATCH, DELETE methods)

### Operation Priority Matrix

| Priority | Resource Type | Operations | Rationale |
|----------|--------------|------------|-----------|
| P0 | OAuth | 9 | Required for all other tests |
| P0 | Threat Models (Core) | 15 | Primary resource, most used |
| P0 | Diagrams (Core) | 10 | Core collaboration feature |
| P1 | Metadata | 30 | Cross-cutting, affects all resources |
| P1 | Webhooks | 7 | Integration mechanism |
| P1 | Addons | 5 | Extension mechanism |
| P2 | Admin | 27 | Lower usage frequency |
| P2 | SAML | 7 | Alternative auth path |
| P2 | Documents/Notes/Repos | 36 | Supporting resources |
| P3 | Discovery | 4 | Informational endpoints |

---

**Document Version**: 1.1
**Last Updated**: 2025-01-24
**Owner**: Integration Testing Team
**Status**: In Progress (Phase 1 Complete)
