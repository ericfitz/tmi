# Developer Documentation

<!-- VERIFICATION SUMMARY:
     Verified: 2026-01-24
     - Directory structure: setup/, testing/, integration/ VERIFIED to exist
     - Make targets: start-dev, build-server, test-unit, test-integration, cats-fuzz, lint VERIFIED in Makefile
     - Unit test locations: api/*_test.go, auth/*_test.go VERIFIED (50+ test files exist)
     - Integration test framework: test/integration/ VERIFIED to exist
     - Logging package: internal/slogging VERIFIED to exist
     - CORRECTED: make test-integration-new -> make test-integration (target does not exist)
     - BROKEN LINKS NOTED: Several referenced docs don't exist (marked below)
-->

This directory contains everything developers need to build, test, and integrate with the TMI server.

## Purpose

Comprehensive development guidance covering environment setup, testing strategies, and client integration patterns for the TMI (Threat Modeling Interface) project.

## Directory Structure

### [setup/](setup/) - Development Environment Setup
Initial setup and configuration for local development.

### [testing/](testing/) - Testing & Quality Assurance
Testing strategies, tools, and quality assurance processes.

### [integration/](integration/) - Client Integration Guides
Patterns and guides for integrating client applications with TMI.

## Getting Started

<!-- NEEDS-REVIEW: Several files below do not exist. See wiki for consolidated documentation. -->

1. **Start Here**: See [Getting Started with Development](https://github.com/ericfitz/tmi/wiki/Getting-Started-with-Development) in wiki
2. **Authentication**: See [Setting Up Authentication](https://github.com/ericfitz/tmi/wiki/Setting-Up-Authentication) in wiki
3. **Testing**: [testing/README.md](testing/README.md)
4. **Client Integration**: [integration/client-oauth-integration.md](integration/client-oauth-integration.md)

## Quick Reference

### Essential Development Commands
```bash
make start-dev                 # Start development environment
make build-server              # Build the server
make test-unit                 # Run unit tests
make test-integration          # Run integration tests (automatic setup/teardown)
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
<!-- NEEDS-REVIEW: These specific files do not exist; content migrated to wiki -->
- See [Getting Started with Development](https://github.com/ericfitz/tmi/wiki/Getting-Started-with-Development) in wiki

### Testing & Quality
- [Testing Guide](testing/README.md) - Comprehensive testing documentation
- [WebSocket Testing](testing/websocket-testing.md) - Real-time feature testing
- [CATS Public Endpoints](testing/cats-public-endpoints.md) - Security fuzzing configuration
- [CATS OAuth False Positives](testing/cats-oauth-false-positives.md) - OAuth testing guidance
- [CATS Test Data Setup](testing/cats-test-data-setup.md) - CATS configuration
- See also [Testing](https://github.com/ericfitz/tmi/wiki/Testing) in wiki for coverage reporting

### Client Integration
- [Client OAuth Integration](integration/client-oauth-integration.md) - OAuth client patterns
- [Client WebSocket Integration](integration/client-websocket-integration-guide.md) - WebSocket integration
- [Webhook Subscriptions](integration/webhook-subscriptions.md) - Webhook integration

## Development Principles

### Code Standards
- Go formatting with `gofmt`
- Comprehensive error handling
- Structured logging throughout (use `internal/slogging` package)
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
Framework in `test/integration/`
- OpenAPI-driven validation
- Automated OAuth authentication
- Workflow-oriented testing

```bash
# Run integration tests (automatic setup and cleanup)
make test-integration

# Or leave server running after tests
make test-integration CLEANUP=false
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
6. Run integration tests if API changes: `make test-integration`

For questions or issues, consult the documentation or create an issue in the project repository.
