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
3. **Testing**: [testing/README.md](testing/README.md)
4. **Client Integration**: [integration/client-integration-guide.md](integration/client-integration-guide.md)

## Quick Reference

### Essential Development Commands
```bash
make start-dev                 # Start development environment
make build-server              # Build the server
make test-unit                 # Run unit tests
make test-integration-new      # Run integration tests (server must be running)
make cats-fuzz                 # Run security fuzzing
make lint                      # Run code linting
```

### Development Workflow
1. Set up local environment with PostgreSQL and Redis containers
2. Configure OAuth providers or use test provider
3. Run tests to verify setup
4. Begin development with hot reloading

### Key Technologies
- **Backend**: Go with Gin framework
- **Database**: PostgreSQL with Redis for caching
- **Authentication**: OAuth 2.0 with JWT tokens
- **Real-time**: WebSockets for collaborative editing
- **API**: RESTful with OpenAPI 3.0 specification

## Documentation by Category

### Setup & Configuration
- [Development Environment Setup](setup/development-setup.md) - Local development setup
- [OAuth Integration Guide](setup/oauth-integration.md) - Authentication setup
- [Automatic Versioning](setup/automatic-versioning.md) - Version management

### Testing & Quality
- [Testing Guide](testing/README.md) - Comprehensive testing documentation
- [Coverage Reporting](testing/coverage-reporting.md) - Test coverage analysis
- [WebSocket Testing](testing/websocket-testing.md) - Real-time feature testing
- [CATS Public Endpoints](testing/cats-public-endpoints.md) - Security fuzzing configuration
- [CATS OAuth False Positives](testing/cats-oauth-false-positives.md) - OAuth testing guidance
- [CATS Test Data Setup](testing/cats-test-data-setup.md) - CATS configuration

### Client Integration
- [Client Integration Guide](integration/client-integration-guide.md) - Complete client integration
- [Client OAuth Integration](integration/client-oauth-integration.md) - OAuth client patterns
- [Client WebSocket Integration](integration/client-websocket-integration-guide.md) - WebSocket integration
- [Webhook Subscriptions](integration/webhook-subscriptions.md) - Webhook integration

## Development Principles

### Code Standards
- Go formatting with `gofmt`
- Comprehensive error handling
- Structured logging throughout (use `slogging` package)
- OpenAPI-first API design

### Testing Philosophy
- **Unit tests**: Fast, isolated, no external dependencies
- **Integration tests**: OpenAPI-driven, end-to-end workflows
- **Security fuzzing**: CATS for vulnerability testing
- **WebSocket tests**: Real-time collaboration testing

### Security Practices
- JWT-based authentication
- Role-based access control (RBAC)
- Input validation and sanitization
- OAuth 2.0 best practices with PKCE

## Testing Overview

TMI uses a multi-layered testing approach:

### Unit Tests
Located in-package (`api/*_test.go`, `auth/*_test.go`)
```bash
make test-unit
```

### Integration Tests
New framework in `test/integration/`
- OpenAPI-driven validation
- Automated OAuth authentication
- Workflow-oriented testing

```bash
# Prerequisites
make start-dev          # Terminal 1
make start-oauth-stub   # Terminal 2

# Run tests
make test-integration-new
```

See [test/integration/README.md](../../test/integration/README.md) for detailed guide.

### Security Fuzzing
CATS (Contract-driven Automatic Testing Suite)
```bash
make cats-fuzz
make cats-analyze
```

### WebSocket Testing
```bash
make wstest
```

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
4. Ensure all make targets pass (`lint`, `build-server`, `test-unit`)
5. Follow conventional commit message format
6. Run integration tests if API changes: `make test-integration-new`

For questions or issues, consult the documentation or create an issue in the project repository.
