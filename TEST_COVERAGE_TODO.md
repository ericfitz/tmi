# Test Coverage Improvement TODO List

This document tracks the remaining work to improve test coverage for the TMI (Collaborative Threat Modeling Interface) project.

## Current Status
- **Baseline Coverage**: 22.6% (original)
- **Current Coverage**: ~45-50% (estimated after WebSocket and store test fixes)
- **Target Coverage**: 80%
- **Files with Tests**: 62 test files (added websocket_test.go)
- **Files without Tests**: ~39 files remaining

## Completed Work ✅
- [x] Fixed Makefile configuration for coverage reporting
- [x] Created comprehensive tests for `api/utils.go` (100% coverage)
- [x] Created comprehensive tests for `api/cell_conversion.go` (100% coverage)
- [x] Created comprehensive tests for store implementations (document, threat, source, metadata stores)
- [x] Fixed type system issues and linting problems
- [x] Established baseline coverage measurement
- [x] Fixed store implementation test isolation issues
- [x] Implemented comprehensive WebSocket real-time collaboration tests

## High Priority Remaining Work

### 1. ✅ Fix Store Implementation Tests 
**Status**: COMPLETED
**Files**: `api/store_implementations_test.go`
**Issue**: Tests were sharing store instances causing failures in Update/Delete tests
**Solution Implemented**: 
- Created fresh store instances for each subtest
- Fixed pointer iteration issues in BulkCreate methods
- Fixed Create methods to update original pointers with generated IDs
- All store tests now passing

### 2. ✅ WebSocket Real-time Collaboration Tests
**Status**: COMPLETED 
**Files**: `api/websocket_test.go` (created)
**Priority**: High (core feature)
**Tests Implemented**: 
- WebSocketHub creation and session management (CreateSession, GetSession, GetOrCreateSession, CleanupSession)
- DiagramSession client management (AddConnection, RemoveConnection) 
- Message broadcasting functionality
- Session termination handling
- WebSocket connection authentication tests
- Mock auth service for testing
- All tests passing successfully

### 3. Authentication Middleware Tests 🔐
**Status**: Not started
**Files**: 
- `api/auth_middleware.go`
- `api/threat_model_middleware.go` 
- `api/diagram_middleware.go`
**Priority**: High (security-critical)
**Coverage Needed**:
- JWT token validation
- Role-based access control
- Authorization header parsing
- Error responses (401, 403)
- User context extraction

## Medium Priority Remaining Work

### 4. API Handler Tests 📡
**Status**: Not started
**Files**: `api/*_handlers.go`
**Priority**: Medium
**Coverage Needed**:
- HTTP request/response handling
- Input validation
- Error responses
- Business logic integration
- OpenAPI spec compliance

### 5. Database Store Integration Tests 🗄️
**Status**: Not started
**Files**: 
- `api/document_store.go`
- `api/threat_store.go`
- `api/source_store.go`
- `api/diagram_store.go`
**Priority**: Medium (in-memory stores already covered)
**Approach**: 
- Use `make test-integration` patterns
- Focus on caching logic
- Test database transaction handling
- Test error conditions

### 6. Cache System Tests 💾
**Status**: Not started
**Files**: 
- `api/cache_service.go`
- `api/cache_invalidation.go`
- `api/cache_metrics.go`
**Priority**: Medium
**Coverage Needed**:
- Redis cache operations
- Cache invalidation patterns
- Cache metrics collection
- Cache warming strategies

## Low Priority Remaining Work

### 7. Telemetry and Monitoring Tests 📊
**Status**: Some existing tests
**Files**: `api/telemetry*.go`
**Priority**: Low (some coverage exists)
**Note**: Use `make test-telemetry` as starting point

### 8. Configuration and Setup Tests ⚙️
**Status**: Not started
**Files**: Various config and startup files
**Priority**: Low
**Coverage Needed**:
- Server initialization
- Configuration validation
- Database connection setup

### 9. Integration Test Expansion 🔗
**Status**: Basic integration tests exist
**Priority**: Low
**Expansion Areas**:
- End-to-end API workflows
- Multi-user collaboration scenarios
- Performance under load

## Technical Notes and Guidelines

### Testing Standards
- Always use `make test-unit` and `make test-integration` (never run `go test` directly)
- Follow existing table-driven test patterns
- Use `github.com/stretchr/testify` for assertions
- Run `make lint` after any changes
- Achieve >90% line coverage for new test files

### Store Testing Patterns
```go
// Create fresh store instance per subtest
t.Run("test name", func(t *testing.T) {
    store := NewInMemoryDocumentStore()
    threatModelID := uuid.New().String()
    // ... test implementation
})
```

### WebSocket Testing Approach
1. Test hub logic separately from WebSocket connections
2. Use mock connections for unit tests
3. Test message serialization/deserialization independently
4. Reference existing `ws-test-harness/` for integration patterns

### Authentication Testing Requirements
- Test with valid/invalid JWT tokens
- Test role hierarchy (reader < writer < owner)  
- Test authorization for different endpoints
- Test error responses and status codes

## Coverage Analysis Commands

```bash
# Generate coverage report
make test-coverage

# Run specific test types
make test-unit
make test-integration
make test-telemetry

# Check for linting issues
make lint

# Build to check for compile errors
make build-server
```

## Success Metrics
- [ ] Overall coverage > 80%
- [x] All store implementation tests passing ✅
- [x] WebSocket hub tests implemented ✅
- [ ] Authentication middleware fully tested
- [ ] No linting errors
- [ ] All critical business logic covered

## Notes on Testing Philosophy
- **Unit tests** (`make test-unit`): Fast, no external dependencies
- **Integration tests** (`make test-integration`): Real databases, full workflows  
- **Never disable failing tests**: Investigate root cause and fix
- **Focus on business logic**: Prioritize core functionality over boilerplate
- **Test error conditions**: Don't just test the happy path

## Estimated Timeline
- **Store test fixes**: ✅ COMPLETED
- **WebSocket tests**: ✅ COMPLETED  
- **Auth middleware tests**: 2-3 hours
- **API handler tests**: 6-8 hours
- **Total estimated effort**: 8-11 hours remaining

---

**Last Updated**: 2025-09-05
**Current Coverage**: ~45-50% (estimated)
**Target Coverage**: 80%