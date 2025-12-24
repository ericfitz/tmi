# TMI Testing Strategy

## Overview

All testing code, resources, and outputs are consolidated under the `test/` directory for improved organization and maintainability.

## Directory Structure

```
test/
├── integration/              # Integration test framework and tests
│   ├── framework/            # Core framework components
│   │   ├── client.go         # HTTP client with OpenAPI validation
│   │   ├── oauth.go          # OAuth flow helpers (uses oauth-stub)
│   │   ├── workflow.go       # Workflow execution engine
│   │   ├── assertions.go     # Common assertion helpers
│   │   └── fixtures.go       # Test data generators
│   ├── workflows/            # End-to-end workflow tests
│   │   ├── oauth_flow_test.go
│   │   ├── threat_model_crud_test.go
│   │   ├── diagram_collaboration_test.go
│   │   ├── metadata_operations_test.go
│   │   ├── bulk_operations_test.go
│   │   ├── webhook_workflow_test.go
│   │   └── addon_workflow_test.go
│   ├── spec/                 # OpenAPI validation
│   │   ├── openapi_validator.go
│   │   └── schema_loader.go
│   └── testdata/             # Test fixtures and data
│       ├── workflows/        # Workflow definitions
│       └── fixtures/         # Test fixture data
├── unit/                     # Unit tests (migrated from api/, auth/, etc.)
├── tools/                    # Test harnesses and utilities
│   ├── wstest/               # WebSocket test harness (moved from wstest/)
│   ├── oauth-stub/           # OAuth callback stub (moved from scripts/)
│   └── cats/                 # CATS fuzzing configuration
├── deprecated/               # Deprecated test resources (gitignored)
│   └── postman/              # Old Postman collections
├── outputs/                  # All test outputs (gitignored)
│   ├── integration/          # Integration test results
│   ├── unit/                 # Unit test coverage reports
│   ├── cats/                 # CATS fuzzing results
│   ├── newman/               # Newman test results (if needed)
│   └── security/             # Security scan reports (SBOM, etc.)
└── configs/                  # Test-specific configurations
    ├── config-unit.yml
    ├── config-integration.yml
    └── cats-test-data.yml
```

## Test Organization Principles

### 1. Consolidation

- **All test code** under `test/` (except inline unit tests in packages)
- **All test outputs** under `test/outputs/` (gitignored)
- **All test configs** under `test/configs/`

### 2. Deprecation Strategy

- Old/outdated test resources moved to `test/deprecated/` (gitignored)
- Postman collections (out of date) → `test/deprecated/postman/`
- Eventually delete `test/deprecated/` when no longer needed

### 3. Test Output Management

- Single output directory: `test/outputs/`
- Subdirectories by test type
- All outputs gitignored
- Scripts updated to write to appropriate subdirectory

## Test Types

### Unit Tests (`test/unit/`)

- Migrated from `api/*_test.go`, `auth/*_test.go`, etc.
- Fast, isolated tests with no external dependencies
- Run via: `make test-unit`
- Output: `test/outputs/unit/coverage.out`

### Integration Tests (`test/integration/`)

- OpenAPI-driven workflow tests
- Test complete user scenarios end-to-end
- Require running server + OAuth stub
- Run via: `make test-integration`
- Output: `test/outputs/integration/results.json`

### WebSocket Test Harness (`test/tools/wstest/`)

- Standalone tool for testing WebSocket collaboration
- Moved from `wstest/`
- Run via: `make wstest`
- Output: `test/outputs/wstest/session-*.log`

### CATS Fuzzing (`test/tools/cats/`)

- Security fuzzing configuration
- Run via: `make cats-fuzz`
- Output: `test/outputs/cats/*.db`, `test/outputs/cats/*.md`

## Integration Test Framework Architecture

### Philosophy

1. **OpenAPI-Driven**: Specification is source of truth
2. **User-Focused**: Test from client perspective (black-box)
3. **Workflow-Oriented**: Test realistic scenarios, not isolated endpoints

### Core Components

#### IntegrationClient (`framework/client.go`)

- HTTP client with automatic OpenAPI validation
- Bearer token management
- Request/response logging
- Workflow state management (store IDs between steps)

#### OAuth Integration (`framework/oauth.go`)

- Leverage `test/tools/oauth-stub/`
- Automatic authentication with test provider
- PKCE flow support
- Token refresh handling

#### Workflow Engine (`framework/workflow.go`)

- Load workflows from `api-workflows.json` or Arazzo specs
- Automatic prerequisite ordering
- State management (pass IDs between steps)
- Comprehensive error reporting

#### OpenAPI Validator (`spec/openapi_validator.go`)

- Runtime validation against OpenAPI schemas
- Request validation (ensure valid payloads)
- Response validation (ensure API compliance)
- Detailed error messages for schema violations

### Workflow Test Organization

#### Tier 1: Core Workflows (Run on every commit)

- OAuth flow (PKCE, token refresh, revocation)
- Threat Model CRUD (full lifecycle)
- Diagram Collaboration (multi-user WebSocket)

#### Tier 2: Feature Tests (Run nightly/pre-release)

- Metadata operations (all resource types)
- Bulk operations (create/update/delete)
- Webhook workflow (subscription → delivery)
- Addon workflow (register → invoke → status)

#### Tier 3: Edge Cases (Run on-demand)

- Authorization/RBAC tests
- Pagination tests
- Error handling tests (4xx, 5xx scenarios)

## Migration Plan

### Phase 1: Directory Structure ✓

1. Create `test/` directory structure
2. Update `.gitignore` for `test/outputs/` and `test/deprecated/`

### Phase 2: Move Existing Resources

1. Move `wstest/` → `test/tools/wstest/`
2. Move `scripts/oauth-client-callback-stub.py` → `test/tools/oauth-stub/`
3. Move `postman/` → `test/deprecated/postman/`
4. Move `cats-test-data.yml` → `test/configs/`
5. Update all scripts to use new paths

### Phase 3: Implement Integration Framework

1. Create framework core (`client.go`, `oauth.go`, `workflow.go`)
2. Implement OpenAPI validator
3. Create first workflow test (OAuth flow)
4. Validate approach

### Phase 4: Test Output Consolidation

1. Update CATS scripts → `test/outputs/cats/`
2. Update newman scripts → `test/outputs/newman/` (if kept)
3. Update coverage reports → `test/outputs/unit/`
4. Update security reports → `test/outputs/security/`

### Phase 5: Documentation

1. Update `docs/developer/testing/` to reference new structure
2. Update `CLAUDE.md` with new test commands
3. Update Makefile targets

## Make Commands

```makefile
# Unit tests
make test-unit                    # Run all unit tests
make test-unit-coverage          # Generate coverage report

# Integration tests
make test-integration            # Run all integration tests
make test-integration-workflow WORKFLOW=oauth_flow  # Run specific workflow

# Tools
make wstest                      # Run WebSocket test harness
make oauth-stub                  # Start OAuth callback stub

# CATS fuzzing
make cats-fuzz                   # Run CATS fuzzing
make cats-analyze                # Analyze CATS results

# Cleanup
make test-clean                  # Remove all test outputs
```

## Success Criteria

- ✅ Single test directory structure
- ✅ All test outputs gitignored and consolidated
- ✅ Deprecated resources isolated
- ✅ 90%+ OpenAPI endpoint coverage
- ✅ Tests run in < 5 minutes
- ✅ < 1% flake rate
- ✅ Clear documentation and examples
