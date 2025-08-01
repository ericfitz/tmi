# TMI Granular API Enhancement - Implementation Plan & Tracking

## Project Overview

**Objective**: Implement granular API operations for TMI threat model sub-resources while maintaining authorization inheritance and backward compatibility, with integrated Redis caching for performance optimization.

**Duration Estimate**: 7-9 weeks  
**Complexity**: High  
**Priority**: High

## Redis Caching Integration

**Caching Strategy**: Based on existing Redis infrastructure and analysis from `context/redis.md`, we will implement a comprehensive caching layer to reduce database load by 60-80% for read operations.

### Cache Architecture
- **Write-through caching**: Update both PostgreSQL and Redis on modifications
- **Selective caching**: Cache frequently accessed threat models and sub-resources
- **TTL management**: 10-15 minutes for threat models, 2-3 minutes for diagrams, 5-10 minutes for sub-resources
- **Cache invalidation**: Clear related caches when data changes or permissions update

### Caching Targets
1. **Complete threat models** with all related sub-resources
2. **Individual sub-resources** for granular access
3. **Authorization data** for faster permission checks
4. **Metadata collections** by entity type
5. **Paginated list views** per user
6. **Diagram cells** for real-time collaboration  

## Implementation Phases

### Phase 1: Foundation & Infrastructure (Week 1-2)
**Goal**: Establish core infrastructure for authorization inheritance, database schema, and Redis caching layer

#### 1.1 Redis Caching Infrastructure
- [ ] **Task**: Extend Redis key builder for sub-resource caching
  - **File**: `auth/db/redis_keys.go`
  - **Estimated Time**: 4 hours
  - **Dependencies**: Existing Redis infrastructure
  - **Tests**: Key pattern validation tests
  - **New Keys**: 
    - `cache:threat:{threat_id}` - Individual threat caching
    - `cache:document:{doc_id}` - Document caching  
    - `cache:source:{source_id}` - Source code caching
    - `cache:metadata:{entity_type}:{entity_id}` - Metadata collections
    - `cache:cells:{diagram_id}` - Diagram cells collection
    - `cache:auth:{threat_model_id}` - Authorization data caching
    - `cache:list:threats:{threat_model_id}:{offset}:{limit}` - Paginated lists

- [ ] **Task**: Implement cache service layer for sub-resources
  - **File**: `api/cache_service.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: Redis key builder
  - **Tests**: Cache CRUD operation tests

- [ ] **Task**: Create cache invalidation utilities
  - **File**: `api/cache_invalidation.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Cache service layer
  - **Tests**: Invalidation strategy tests

#### 1.2 Authorization Infrastructure Enhancement
- [ ] **Task**: Implement `GetInheritedAuthData()` function
  - **File**: `api/auth_utils.go`
  - **Estimated Time**: 4 hours
  - **Dependencies**: None
  - **Tests**: Unit tests for authorization inheritance logic

- [ ] **Task**: Create `CheckSubResourceAccess()` utility function with caching
  - **File**: `api/auth_utils.go`  
  - **Estimated Time**: 5 hours
  - **Dependencies**: GetInheritedAuthData(), Cache service layer
  - **Tests**: Unit tests for sub-resource access validation and cache integration

- [ ] **Task**: Build `ValidateSubResourceAccess()` middleware
  - **File**: `api/middleware.go`
  - **Estimated Time**: 5 hours
  - **Dependencies**: CheckSubResourceAccess()
  - **Tests**: Integration tests with Gin context

#### 1.3 Database Schema Migration
- [ ] **Task**: Create database migration for normalized sub-resource tables
  - **File**: `auth/migrations/000016_create_sub_resource_tables.up.sql`
  - **Estimated Time**: 6 hours
  - **Dependencies**: None
  - **Tests**: Migration validation tests

- [ ] **Task**: Update metadata table schema for polymorphic associations
  - **File**: `auth/migrations/000017_enhance_metadata_table.up.sql`
  - **Estimated Time**: 3 hours
  - **Dependencies**: Previous migration
  - **Tests**: Schema validation tests

- [ ] **Task**: Add database indexes for performance optimization
  - **File**: `auth/migrations/000018_add_performance_indexes.up.sql`
  - **Estimated Time**: 2 hours
  - **Dependencies**: Schema migrations
  - **Tests**: Performance test validation

#### 1.4 Testing Infrastructure
- [ ] **Task**: Create test fixtures for sub-resource testing
  - **File**: `api/sub_resource_test_fixtures.go`
  - **Estimated Time**: 4 hours
  - **Dependencies**: None
  - **Tests**: Fixture validation tests

- [ ] **Task**: Set up integration test helpers for authorization and caching
  - **File**: `api/auth_test_helpers.go`
  - **Estimated Time**: 5 hours
  - **Dependencies**: Authorization infrastructure, Cache service layer
  - **Tests**: Helper function tests with cache validation

- [ ] **Task**: Create Redis cache testing utilities
  - **File**: `api/cache_test_helpers.go`
  - **Estimated Time**: 4 hours
  - **Dependencies**: Cache service layer
  - **Tests**: Cache state validation and cleanup utilities

**Phase 1 Milestone**: Authorization inheritance working, database schema migrated, Redis caching infrastructure ready, test infrastructure complete

### Phase 2: Core Store Implementations with Caching (Week 2-4)
**Goal**: Implement specialized stores for all sub-resource types with integrated Redis caching

#### 2.1 Threat Store Implementation with Caching
- [ ] **Task**: Implement ThreatStore interface with full CRUD + PATCH + Caching
  - **File**: `api/threat_store.go`
  - **Estimated Time**: 10 hours
  - **Dependencies**: Database schema, Cache service layer
  - **Tests**: Comprehensive unit tests for all CRUD operations and cache integration

- [ ] **Task**: Add database-backed ThreatStore implementation with write-through caching
  - **File**: `api/threat_database_store.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: ThreatStore interface, Cache service layer
  - **Tests**: Integration tests with PostgreSQL and Redis

- [ ] **Task**: Implement threat cache warming and invalidation strategies
  - **File**: `api/threat_cache_manager.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: ThreatStore, Cache service layer
  - **Tests**: Cache warming and invalidation tests

#### 2.2 Document & Source Store Implementation with Caching
- [ ] **Task**: Implement DocumentStore interface (CRUD only, no PATCH) with caching
  - **File**: `api/document_store.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: Database schema, Cache service layer
  - **Tests**: Unit tests for CRUD operations and cache integration

- [ ] **Task**: Implement SourceStore interface (CRUD only, no PATCH) with caching
  - **File**: `api/source_store.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: Database schema, Cache service layer
  - **Tests**: Unit tests for CRUD operations and cache integration

#### 2.3 Enhanced Metadata Store with Caching
- [ ] **Task**: Implement MetadataStore with POST operations, key-based access, and caching
  - **File**: `api/metadata_store.go`
  - **Estimated Time**: 10 hours
  - **Dependencies**: Database schema, Cache service layer
  - **Tests**: Comprehensive tests for all metadata operations and cache integration

- [ ] **Task**: Add polymorphic metadata association support with cache invalidation
  - **File**: `api/metadata_store.go` (enhancement)
  - **Estimated Time**: 6 hours
  - **Dependencies**: MetadataStore base implementation, Cache service layer
  - **Tests**: Tests for all entity type associations and cache consistency

#### 2.4 Enhanced Cell Store with Real-time Caching
- [ ] **Task**: Add PATCH support to existing CellStore with caching optimized for collaboration
  - **File**: `api/cell_store.go` (enhancement)
  - **Estimated Time**: 8 hours
  - **Dependencies**: Existing CellStore, Cache service layer
  - **Tests**: PATCH operation tests, batch operation tests, and collaborative caching tests

- [ ] **Task**: Implement cell-level cache invalidation for WebSocket updates
  - **File**: `api/cell_cache_manager.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Enhanced CellStore, WebSocket hub
  - **Tests**: Real-time cache invalidation tests

#### 2.5 Cache Performance Optimization
- [ ] **Task**: Implement cache warming strategies for frequently accessed data
  - **File**: `api/cache_warming.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: All store implementations
  - **Tests**: Cache warming performance tests

- [ ] **Task**: Add cache metrics and monitoring
  - **File**: `api/cache_metrics.go`
  - **Estimated Time**: 4 hours
  - **Dependencies**: Cache service layer
  - **Tests**: Metrics collection and reporting tests

**Phase 2 Milestone**: All store implementations complete with comprehensive Redis caching and test coverage

### Phase 3: API Handler Implementation with Cache Integration (Week 4-6)
**Goal**: Implement all sub-resource API handlers with proper authorization and cache-aware responses

#### 3.1 Threat Handlers
- [ ] **Task**: Implement threat sub-resource handlers
  - **File**: `api/threat_sub_resource_handlers.go`
  - **Estimated Time**: 10 hours
  - **Dependencies**: ThreatStore, authorization middleware
  - **Tests**: Full API endpoint tests with authorization scenarios

- [ ] **Task**: Add threat metadata handlers
  - **File**: `api/threat_metadata_handlers.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: MetadataStore, authorization middleware
  - **Tests**: Metadata CRUD tests with authorization

#### 3.2 Document Handlers
- [ ] **Task**: Implement document sub-resource handlers
  - **File**: `api/document_sub_resource_handlers.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: DocumentStore, authorization middleware
  - **Tests**: CRUD endpoint tests with authorization

- [ ] **Task**: Add document metadata handlers
  - **File**: `api/document_metadata_handlers.go`
  - **Estimated Time**: 5 hours
  - **Dependencies**: MetadataStore, authorization middleware
  - **Tests**: Document metadata operation tests

#### 3.3 Source Code Handlers
- [ ] **Task**: Implement source code sub-resource handlers
  - **File**: `api/source_sub_resource_handlers.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: SourceStore, authorization middleware
  - **Tests**: CRUD endpoint tests with authorization

- [ ] **Task**: Add source code metadata handlers
  - **File**: `api/source_metadata_handlers.go`
  - **Estimated Time**: 5 hours
  - **Dependencies**: MetadataStore, authorization middleware
  - **Tests**: Source metadata operation tests

#### 3.4 Enhanced Diagram & Cell Handlers
- [ ] **Task**: Add diagram metadata handlers
  - **File**: `api/diagram_metadata_handlers.go`
  - **Estimated Time**: 5 hours
  - **Dependencies**: MetadataStore, authorization middleware
  - **Tests**: Diagram metadata operation tests

- [ ] **Task**: Enhance cell handlers with PATCH support and metadata operations
  - **File**: `api/cell_handlers.go` (enhancement)
  - **Estimated Time**: 8 hours
  - **Dependencies**: Enhanced CellStore, MetadataStore
  - **Tests**: PATCH tests and cell metadata tests

#### 3.5 Batch Operation Handlers
- [ ] **Task**: Implement batch operation handlers for threats
  - **File**: `api/batch_handlers.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: All sub-resource stores
  - **Tests**: Batch operation tests with authorization

**Phase 3 Milestone**: All API handlers implemented with full test coverage

### Phase 4: Integration & Testing (Week 5-6)
**Goal**: Complete integration testing and ensure backward compatibility

#### 4.1 OpenAPI Specification Updates
- [ ] **Task**: Update OpenAPI spec with new endpoints
  - **File**: `tmi-openapi.json`
  - **Estimated Time**: 8 hours
  - **Dependencies**: All handlers implemented
  - **Tests**: API specification validation tests

- [ ] **Task**: Generate updated API client code
  - **File**: Auto-generated files
  - **Estimated Time**: 2 hours
  - **Dependencies**: Updated OpenAPI spec
  - **Tests**: Client code generation tests

#### 4.2 Route Registration
- [ ] **Task**: Register all new sub-resource routes
  - **File**: `cmd/server/main.go`, route registration
  - **Estimated Time**: 4 hours
  - **Dependencies**: All handlers
  - **Tests**: Route registration tests

- [ ] **Task**: Apply authorization middleware to all new routes
  - **File**: Route configuration
  - **Estimated Time**: 3 hours
  - **Dependencies**: Routes registered
  - **Tests**: Authorization middleware integration tests

#### 4.3 Integration Testing
- [ ] **Task**: Create comprehensive integration test suite
  - **File**: `api/integration_test.go`
  - **Estimated Time**: 12 hours
  - **Dependencies**: All components implemented
  - **Tests**: End-to-end workflow tests

- [ ] **Task**: Test authorization inheritance across all sub-resources
  - **File**: `api/authorization_integration_test.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: Integration test suite
  - **Tests**: Authorization flow validation

#### 4.4 Performance Testing
- [ ] **Task**: Create performance benchmarks for new endpoints
  - **File**: `api/performance_test.go`
  - **Estimated Time**: 6 hours
  - **Dependencies**: Integration tests
  - **Tests**: Performance benchmark validation

**Phase 4 Milestone**: Full integration testing complete, performance validated

### Phase 5: Documentation & Deployment (Week 6-7)
**Goal**: Complete documentation and prepare for deployment

#### 5.1 API Documentation
- [ ] **Task**: Update API documentation with new endpoints
  - **File**: `docs/TMI-API-v1_0.md`
  - **Estimated Time**: 6 hours
  - **Dependencies**: OpenAPI spec updated
  - **Tests**: Documentation accuracy validation

- [ ] **Task**: Create usage examples for sub-resource operations
  - **File**: `docs/SUB_RESOURCE_EXAMPLES.md`
  - **Estimated Time**: 4 hours
  - **Dependencies**: API documentation
  - **Tests**: Example validation tests

#### 5.2 Migration Documentation
- [ ] **Task**: Create database migration guide
  - **File**: `docs/DATABASE_MIGRATION_GUIDE.md`
  - **Estimated Time**: 3 hours
  - **Dependencies**: All migrations complete
  - **Tests**: Migration guide validation

#### 5.3 Backward Compatibility Testing
- [ ] **Task**: Test existing API endpoints for compatibility
  - **File**: `api/backward_compatibility_test.go`
  - **Estimated Time**: 8 hours
  - **Dependencies**: All new features implemented
  - **Tests**: Compatibility validation suite

**Phase 5 Milestone**: Documentation complete, backward compatibility verified

### Phase 6: Production Readiness (Week 7-8)
**Goal**: Final validation and production deployment preparation

#### 6.1 Security Review
- [ ] **Task**: Conduct security review of authorization implementation
  - **Estimated Time**: 6 hours
  - **Dependencies**: All features complete
  - **Tests**: Security vulnerability tests

- [ ] **Task**: Validate input sanitization and validation
  - **Estimated Time**: 4 hours
  - **Dependencies**: Security review
  - **Tests**: Input validation tests

#### 6.2 Load Testing
- [ ] **Task**: Conduct load testing on new endpoints
  - **Estimated Time**: 8 hours
  - **Dependencies**: Performance tests
  - **Tests**: Load test validation

#### 6.3 Production Deployment
- [ ] **Task**: Create deployment checklist
  - **File**: `docs/DEPLOYMENT_CHECKLIST.md`
  - **Estimated Time**: 2 hours
  - **Dependencies**: All testing complete
  - **Tests**: Deployment validation

- [ ] **Task**: Execute production deployment
  - **Estimated Time**: 4 hours
  - **Dependencies**: Deployment checklist
  - **Tests**: Post-deployment validation

**Phase 6 Milestone**: Production deployment complete

## Testing Strategy

### Unit Testing Requirements
Each component must have comprehensive unit tests covering:

#### Authorization Components
- [ ] **GetInheritedAuthData()** function tests
  - Valid threat model ID scenarios
  - Invalid threat model ID handling
  - Authorization data extraction accuracy
  - Error handling and edge cases

- [ ] **CheckSubResourceAccess()** function tests
  - All role levels (reader, writer, owner)
  - Authorization inheritance scenarios
  - Access denial cases
  - Error propagation

- [ ] **ValidateSubResourceAccess()** middleware tests
  - Gin context integration
  - Authentication validation
  - Authorization validation
  - Error response handling

#### Store Layer Tests with Redis Integration
- [ ] **ThreatStore** tests
  - CRUD operations for all methods
  - PATCH operation validation
  - Pagination and filtering
  - Error handling and edge cases
  - Database transaction handling
  - Redis cache integration
  - Cache invalidation scenarios
  - Cache miss/hit behavior
  - Cache warming strategies

- [ ] **DocumentStore & SourceStore** tests
  - CRUD operations (no PATCH)
  - Relationship integrity
  - Error handling
  - Data validation
  - Cache integration
  - Cache invalidation patterns

- [ ] **MetadataStore** tests
  - Polymorphic association handling
  - Key-based operations
  - POST operations
  - Entity type validation
  - Duplicate key handling
  - Cache consistency across entity types
  - Metadata collection caching
  - Cache invalidation for metadata changes

- [ ] **Enhanced CellStore** tests
  - PATCH operation validation
  - Batch operations
  - Cell data integrity
  - Diagram relationship validation
  - Real-time cache updates for WebSocket
  - Collaborative caching scenarios
  - Cell-level cache invalidation

#### Handler Layer Tests
- [ ] **Sub-Resource Handler** tests for each type
  - HTTP method handling
  - Request/response serialization
  - Authorization integration
  - Error response formatting
  - Input validation

- [ ] **Metadata Handler** tests
  - Individual key operations
  - Collection operations
  - POST operations for all entity types
  - Authorization validation

### Integration Testing Requirements

#### Authorization Integration Tests
- [ ] **End-to-End Authorization Flow**
  - User authentication through JWT
  - Threat model authorization extraction
  - Sub-resource access validation
  - Role-based access enforcement

- [ ] **Cross-Resource Authorization**
  - Threat model owner access to all sub-resources
  - Writer role access validation
  - Reader role access limitations
  - Access denial scenarios

#### API Integration Tests
- [ ] **Complete Workflow Tests**
  - Create threat model with sub-resources
  - CRUD operations on each sub-resource type
  - Metadata operations across all resource types
  - Batch operations validation

- [ ] **Error Handling Integration**
  - Invalid authentication scenarios
  - Insufficient permissions
  - Invalid input data
  - Database constraint violations

#### Redis Cache Integration Tests
- [ ] **Cache Service Layer Tests**
  - Cache key generation and validation
  - TTL management and expiration
  - Write-through caching operations
  - Cache invalidation strategies
  - Cache warming and preloading
  - Memory usage optimization

- [ ] **Cache Consistency Tests**
  - PostgreSQL and Redis data synchronization
  - Transaction rollback cache handling
  - Concurrent access scenarios
  - Cache lock mechanisms
  - Distributed cache invalidation

- [ ] **Performance Cache Tests**
  - Cache hit/miss ratio optimization
  - Response time improvements
  - Memory usage under load
  - Cache eviction policies
  - Redis connection pooling efficiency

#### Database Integration Tests
- [ ] **Schema Validation**
  - Migration execution
  - Foreign key constraints
  - Index performance
  - Data integrity

- [ ] **Transaction Handling**
  - Rollback scenarios
  - Concurrent access
  - Deadlock prevention
  - Data consistency

### Performance Testing Requirements

#### Load Testing Scenarios
- [ ] **High-Volume Operations**
  - Concurrent threat model access
  - Bulk metadata operations
  - Large result set pagination
  - WebSocket collaboration under load

- [ ] **Authorization Performance**
  - Authorization check latency
  - Cache efficiency
  - Database query optimization
  - Memory usage patterns

#### Benchmark Requirements
- [ ] **Response Time Benchmarks**
  - Sub-resource CRUD operations < 200ms (cached: < 50ms)
  - Metadata operations < 100ms (cached: < 10ms)
  - Authorization checks < 50ms (cached: < 5ms)
  - Batch operations linear scaling
  - Cache warming operations < 1s

- [ ] **Cache Performance Benchmarks**
  - Cache hit ratio > 85% for read operations
  - Cache miss penalty < 2x uncached response time
  - Redis memory usage < 1GB for 10K threat models
  - Cache invalidation propagation < 100ms
  - WebSocket cache updates < 50ms

- [ ] **Throughput Benchmarks**
  - Concurrent user capacity (with cache: 3x improvement)
  - Operations per second limits (with cache: 5x improvement)
  - Database connection pooling efficiency
  - Redis connection pooling optimization
  - Memory usage optimization

## Progress Tracking

### Completion Status Legend
- ⭕ Not Started
- 🔄 In Progress  
- ✅ Complete
- ❌ Blocked
- ⚠️ Needs Review

### Overall Progress Dashboard

| Phase | Status | Progress | Est. Completion |
|-------|--------|----------|----------------|
| Phase 1: Foundation + Redis | ⭕ | 0% | Week 2 |
| Phase 2: Stores + Caching | ⭕ | 0% | Week 4 |
| Phase 3: Handlers + Cache Integration | ⭕ | 0% | Week 6 |
| Phase 4: Integration + Cache Testing | ⭕ | 0% | Week 7 |
| Phase 5: Documentation + Cache Docs | ⭕ | 0% | Week 8 |
| Phase 6: Production + Cache Monitoring | ⭕ | 0% | Week 9 |

### Risk Management

#### High Risk Items
- [ ] **Database Migration Impact**
  - **Risk**: Data loss during schema migration
  - **Mitigation**: Comprehensive backup and rollback procedures
  - **Owner**: Database team

- [ ] **Authorization Security**
  - **Risk**: Authorization bypass vulnerabilities
  - **Mitigation**: Security review and penetration testing
  - **Owner**: Security team

- [ ] **Cache Consistency**
  - **Risk**: PostgreSQL and Redis data inconsistency
  - **Mitigation**: Write-through caching, transaction-aware invalidation
  - **Owner**: Engineering team

- [ ] **Redis Memory Overflow**
  - **Risk**: Redis memory exhaustion under high load
  - **Mitigation**: Memory monitoring, cache eviction policies, TTL optimization
  - **Owner**: Infrastructure team

- [ ] **Performance Degradation**
  - **Risk**: New endpoints impact existing performance
  - **Mitigation**: Performance monitoring, caching optimization
  - **Owner**: Engineering team

#### Medium Risk Items
- [ ] **Backward Compatibility**
  - **Risk**: Breaking changes to existing API clients
  - **Mitigation**: Comprehensive compatibility testing
  - **Owner**: API team

- [ ] **Test Coverage Gaps**
  - **Risk**: Insufficient test coverage for edge cases
  - **Mitigation**: Code coverage requirements and review
  - **Owner**: QA team

### Success Criteria

#### Functional Requirements
- [ ] All sub-resource endpoints implemented and functional
- [ ] Authorization inheritance working correctly
- [ ] Metadata operations available for all resource types
- [ ] PATCH support for cells and threats
- [ ] Backward compatibility maintained

#### Non-Functional Requirements
- [ ] >95% test coverage for all new code
- [ ] API response times < 200ms for CRUD operations (< 50ms with cache)
- [ ] Cache hit ratio > 85% for read operations
- [ ] Redis memory usage < 1GB for 10K threat models
- [ ] Zero security vulnerabilities in security review
- [ ] Zero breaking changes to existing API contracts
- [ ] Cache consistency maintained under all failure scenarios
- [ ] Documentation complete and accurate

#### Deployment Requirements
- [ ] Database migrations execute successfully
- [ ] No service downtime during deployment
- [ ] Monitoring and alerting configured
- [ ] Rollback procedures tested and documented

## Development Commands

### Testing Commands
```bash
# Run all tests
make test

# Run specific test suites
make test-one name=TestThreatStore
make test-one name=TestAuthorizationInheritance
make test-one name=TestSubResourceHandlers

# Run integration tests
make test-integration

# Run performance benchmarks
make benchmark

# Check test coverage
make coverage

# Redis-specific testing
make test-redis
make test-cache-integration
make benchmark-cache
```

### Development Commands
```bash
# Database migrations
make migrate

# Generate API code
make gen-api

# Lint code
make lint

# Build and test
make build && make test
```

### Deployment Commands
```bash
# Development environment (includes Redis)
make dev

# Redis-only development
make dev-redis

# Production build
make build-prod

# Database setup
make setup-db

# Cache management
make cache-clear
make cache-warm
make cache-stats
```

This implementation plan provides a comprehensive roadmap for implementing the granular API enhancements with detailed task breakdown, testing requirements, and progress tracking mechanisms.