# Testing & Quality Assurance

This directory contains comprehensive testing strategies, tools, and quality assurance processes for TMI development.

## Testing Philosophy

TMI follows a multi-layered testing approach:

- **Unit Tests**: Fast, isolated business logic testing
- **Integration Tests**: Database and service integration testing  
- **API Tests**: Complete HTTP endpoint testing
- **WebSocket Tests**: Real-time collaboration testing
- **End-to-End Tests**: Full user workflow validation

## Files in this Directory

### Core Testing Documentation

#### [integration-testing.md](integration-testing.md)
**Primary integration testing guide** for TMI server testing with real databases.

**Content includes:**
- Automated PostgreSQL and Redis setup/cleanup
- Database verification patterns
- API endpoint testing with authentication
- Testing methodology and best practices
- CI/CD integration guidance
- Performance testing considerations

#### [coverage-reporting.md](coverage-reporting.md)
**Test coverage analysis** for comprehensive coverage reporting.

**Content includes:**
- Unit and integration test coverage
- Coverage threshold configuration
- HTML and text report generation
- CI/CD coverage integration
- Coverage analysis tools
- Testing quality metrics

#### [api-integration-tests.md](api-integration-tests.md)
**API-specific integration testing** focused on HTTP endpoint behavior.

**Content includes:**
- REST API endpoint testing
- Authentication flow testing
- Request/response validation
- Error handling verification
- Database state verification

#### [websocket-testing.md](websocket-testing.md)
**Real-time collaboration testing** for WebSocket features.

**Content includes:**
- WebSocket test harness usage
- Multi-user collaboration testing
- Authentication with WebSockets
- Message flow validation
- Performance and load testing
- Debugging collaborative features

### Postman Testing Suite

#### [postman-comprehensive-testing.md](postman-comprehensive-testing.md)
**Postman collection overview** for API testing with Postman.

**Content includes:**
- Postman collection structure
- Environment setup for testing
- Authentication configuration
- Automated test execution
- Test result analysis

#### [comprehensive-test-plan.md](comprehensive-test-plan.md)
**Overall testing strategy** covering all testing approaches.

**Content includes:**
- Testing scope and objectives
- Test case categorization
- Testing workflows and procedures
- Quality gates and criteria

#### [comprehensive-testing-strategy.md](comprehensive-testing-strategy.md)
**Strategic testing approach** for TMI quality assurance.

**Content includes:**
- Testing pyramid implementation
- Test automation strategy
- Performance testing approach
- Security testing considerations
- Release testing procedures

## Testing Commands

### Essential Testing Commands
```bash
# Unit tests (fast, no external dependencies)
make test-unit

# Integration tests (requires database)
make test-integration

# Specific tests
make test-unit name=TestName
make test-integration name=TestName

# Code quality
make lint
make build-server

# Coverage reporting
make coverage-report
```

### WebSocket Testing
```bash
# Build WebSocket test harness
make build-wstest

# Run multi-user collaboration test
make wstest

# Clean up test processes
make wstest-clean
```

### API Testing with Postman
```bash
# Run Postman test suite
cd postman && ./run-tests.sh

# Individual collection testing
newman run collection.json -e environment.json
```

## Testing Environment Setup

### Prerequisites
1. Development environment running (`make start-dev`)
2. Database containers active (PostgreSQL & Redis)
3. Valid authentication configuration

### Test Database
- Integration tests use isolated test databases
- Automatic setup and cleanup
- No impact on development data

### Authentication for Testing
- OAuth callback stub for automated testing
- Test provider for predictable user accounts
- JWT token management for API tests

## Test Categories

### Unit Tests
- **Location**: Alongside source code (`*_test.go`)
- **Purpose**: Business logic validation
- **Speed**: Very fast (< 1 second)
- **Dependencies**: None (mocked)

### Integration Tests
- **Location**: `api/` directory with `_integration_test.go` suffix
- **Purpose**: Database and service integration
- **Speed**: Fast (< 10 seconds)
- **Dependencies**: PostgreSQL, Redis

### API Tests
- **Location**: Postman collections and Go integration tests
- **Purpose**: HTTP endpoint behavior
- **Speed**: Medium (10-30 seconds)
- **Dependencies**: Full TMI server stack

### WebSocket Tests
- **Location**: `ws-test-harness/` directory
- **Purpose**: Real-time collaboration
- **Speed**: Medium (10-30 seconds)
- **Dependencies**: TMI server with WebSocket support

## Quality Gates

### Pre-Commit Requirements
1. All unit tests pass (`make test-unit`)
2. Code linting passes (`make lint`)
3. Server builds successfully (`make build-server`)

### Pre-Merge Requirements  
1. All integration tests pass (`make test-integration`)
2. API test suite passes (Postman collections)
3. Coverage thresholds met
4. WebSocket tests pass for collaboration features

### Release Requirements
1. Full test suite passes
2. Performance benchmarks met
3. Security scans pass
4. Documentation updated

## Test Data Management

### Test Fixtures
- Minimal, focused test data
- Predictable UUIDs and timestamps
- Isolated per test case

### Database State
- Each integration test starts with clean state
- Automatic cleanup after test completion
- No cross-test dependencies

### Authentication Test Data
- Test OAuth provider with known users
- Predictable JWT tokens for testing
- Role-based access testing scenarios

## Debugging Tests

### Common Issues
- **Database connection errors**: Check container status
- **Authentication failures**: Verify OAuth configuration
- **WebSocket connection issues**: Check server startup
- **Test timeouts**: Increase timeout values or check server performance

### Debugging Tools
- Verbose test output: `go test -v`
- Test-specific logging in TMI server
- Database query logging
- WebSocket message tracing

## Related Documentation

### Setup Requirements
- [Development Setup](../setup/development-setup.md) - Environment prerequisites
- [OAuth Integration](../setup/oauth-integration.md) - Authentication setup

### Integration Guidance  
- [Client Integration Guide](../integration/client-integration-guide.md) - Client testing patterns
- [WebSocket Integration](../integration/collaborative-editing-plan.md) - Real-time testing

### Operations
- [Database Operations](../../operator/database/postgresql-operations.md) - Database management
- [Deployment Guide](../../operator/deployment/deployment-guide.md) - Production testing

## Contributing to Tests

When adding new features:

1. **Write unit tests first** - Test business logic in isolation
2. **Add integration tests** - Verify database interactions
3. **Update API tests** - Include new endpoints in Postman collections
4. **Test WebSocket changes** - Use WebSocket test harness for real-time features
5. **Update documentation** - Keep test documentation current

Follow the established patterns and ensure all quality gates pass before submitting changes.