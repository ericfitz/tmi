# Developer Documentation

This directory contains everything developers need to build, test, and integrate with the TMI server.

## Purpose

Comprehensive development guidance covering environment setup, testing strategies, and client integration patterns for the TMI (Threat Modeling Interface) project.

## Directory Structure

### 🔧 [setup/](setup/) - Development Environment Setup
Initial setup and configuration for local development.

### 🧪 [testing/](testing/) - Testing & Quality Assurance  
Testing strategies, tools, and quality assurance processes.

### 🔗 [integration/](integration/) - Client Integration Guides
Patterns and guides for integrating client applications with TMI.

## Getting Started

1. **Start Here**: [setup/development-setup.md](setup/development-setup.md)
2. **Authentication**: [setup/oauth-integration.md](setup/oauth-integration.md)  
3. **Testing**: [testing/integration-testing.md](testing/integration-testing.md)
4. **Client Integration**: [integration/client-integration-guide.md](integration/client-integration-guide.md)

## Quick Reference

### Essential Development Commands
```bash
make start-dev          # Start development environment
make build-server       # Build the server
make test-unit          # Run unit tests
make test-integration   # Run integration tests
make lint               # Run code linting
```

### Development Workflow
1. Set up local environment with PostgreSQL and Redis containers
2. Configure OAuth providers or use test provider  
3. Run integration tests to verify setup
4. Begin development with hot reloading

### Key Technologies
- **Backend**: Go with Echo framework
- **Database**: PostgreSQL with Redis for caching
- **Authentication**: OAuth 2.0 with JWT tokens
- **Real-time**: WebSockets for collaborative editing
- **API**: RESTful with OpenAPI 3.0 specification

## Documentation by Category

### Setup & Configuration
- [Development Environment Setup](setup/development-setup.md) - Local development setup
- [OAuth Integration Guide](setup/oauth-integration.md) - Authentication setup

### Testing & Quality
- [Integration Testing](testing/integration-testing.md) - Full integration test suite
- [Coverage Reporting](testing/coverage-reporting.md) - Test coverage analysis
- [API Integration Tests](testing/api-integration-tests.md) - API-specific testing
- [WebSocket Testing](testing/websocket-testing.md) - Real-time feature testing
- [Postman Comprehensive Testing](testing/postman-comprehensive-testing.md) - API testing with Postman
- [Comprehensive Test Plan](testing/comprehensive-test-plan.md) - Overall testing strategy
- [Comprehensive Testing Strategy](testing/comprehensive-testing-strategy.md) - Testing approach
- [Endpoints Status Codes](testing/endpoints-status-codes.md) - API response reference

### Client Integration
- [Client Integration Guide](integration/client-integration-guide.md) - Complete client integration
- [Client OAuth Integration](integration/client-oauth-integration.md) - OAuth client patterns
- [Collaborative Editing Plan](integration/collaborative-editing-plan.md) - Real-time editing
- [Workflow Generation Prompt](integration/workflow-generation-prompt.md) - Workflow automation

## Development Principles

### Code Standards
- Go formatting with `gofmt`
- Comprehensive error handling
- Structured logging throughout
- OpenAPI-first API design

### Testing Philosophy  
- Unit tests for business logic
- Integration tests for full workflows
- API tests for endpoint behavior
- Load testing for performance validation

### Security Practices
- JWT-based authentication
- Role-based access control (RBAC)
- Input validation and sanitization
- OAuth 2.0 best practices

## Related Documentation

### For Operations
- [Deployment Guide](../operator/deployment/deployment-guide.md) - Production deployment
- [Database Operations](../operator/database/postgresql-operations.md) - Database management

### For Reference
- [Architecture Documentation](../reference/architecture/) - System architecture
- [API Specifications](../reference/apis/) - API reference materials

## Contributing

When adding new features:

1. Follow the existing code structure and patterns
2. Add comprehensive tests (unit + integration)
3. Update relevant documentation
4. Ensure all make targets pass
5. Follow the established commit message format

For questions or issues, consult the integration tests or create an issue in the project repository.