# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains API documentation and Go implementation for a Collaborative Threat Modeling Interface (TMI). It's a server-based web application enabling collaborative threat modeling with real-time diagram editing via WebSockets, role-based access control, OAuth authentication with JWT, and a RESTful API with OpenAPI 3.0 specification.

## Key Files

- docs/TMI-API-v1_0.md - API documentation in Markdown
- tmi-openapi.json - OpenAPI specification
- api/store.go - Generic typed map storage implementation
- api/server.go - Main API server with WebSocket support
- api/websocket.go - WebSocket hub for real-time collaboration
- cmd/server/main.go - Server entry point
- Makefile - Build automation with development targets

## Commands

- List targets: `make list` (lists all available make targets)
- Build: `make build` (creates bin/server executable)
- Lint: `make lint` (runs golangci-lint)
- Generate API: `make gen-api` (uses oapi-codegen with config from oapi-codegen-config.yaml)
- Development: `make dev` (starts full dev environment with DB and Redis)
- Dev DB only: `make dev-db` (starts PostgreSQL container)
- Dev Redis only: `make dev-redis` (starts Redis container)

### Testing Commands

**IMPORTANT: Always use make targets for testing. Never run `go test` commands directly.**

#### Core Testing
- Unit tests: `make test-unit` (fast tests, no external dependencies)
  - Specific test: `make test-unit name=TestName`
  - Options: `make test-unit count1=true passfail=true`
- Integration tests: `make test-integration` (requires database, runs with automatic setup/cleanup)
  - Specific test: `make test-integration name=TestName` 
  - Cleanup only: `make test-integration-cleanup`
- Alias: `make test` (same as `make test-unit` for backward compatibility)

#### Specialized Testing  
- Telemetry tests: `make test-telemetry` (unit tests for telemetry components)
  - Integration mode: `make test-telemetry integration=true`
- API testing: `make test-api` (requires running server via `make dev`)
  - Auth token only: `make test-api auth=only`
  - No auth test only: `make test-api noauth=true`
- Full API test: `make test-api-full` (automated setup: kills servers, starts DB/Redis, starts server, runs tests, cleans up)
- Development test: `make dev-test` (builds and tests API endpoints, requires running server)
- Full dev test: `make dev-test-full` (alias for `make test-api-full`)

#### Testing Examples
```bash
# Standard development workflow
make test-unit                    # Fast unit tests
make test-integration            # Full integration tests with database
make lint && make build          # Code quality check and build

# Specific testing
make test-unit name=TestStore_CRUD              # Run one unit test
make test-integration name=TestDatabaseIntegration  # Run one integration test

# API testing (requires server)
make dev                         # Start server first
make test-api                    # Test API endpoints with auth

# Automated API testing (no manual setup required)
make test-api-full               # Full automated API testing (setup + test + cleanup)
```

## Go Style Guidelines

- Format code with `gofmt`
- Group imports by standard lib, external libs, then internal packages
- Use camelCase for variables, PascalCase for exported functions/structs
- Error handling: check errors and return with context
- Prefer interfaces over concrete types for flexibility
- Document all exported functions with godoc comments
- Structure code by domain (auth, diagrams, threats)

## API Design Guidelines

- Follow OpenAPI 3.0.3 specification standards
- Use snake_case for API JSON properties
- Include descriptions for all properties and endpoints
- Document error responses (401, 403, 404)
- Use UUID format for IDs, ISO8601 for timestamps
- Role-based access with reader/writer/owner permissions
- Bearer token auth with JWT
- JSON Patch for partial updates
- WebSocket for real-time collaboration
- Pagination with limit/offset parameters

## Architecture & Code Structure

### Storage Pattern

- Use the generic Store[T] implementation from api/store.go
- Each entity type has its own store instance (DiagramStore, ThreatModelStore)
- Store provides CRUD operations with proper concurrency control
- Entity fields should be properly validated before storage
- Use WithTimestamps interface for entities with created_at/modified_at fields

### Project Structure

- `api/` - API handlers, server implementation, and storage
- `auth/` - Authentication service with OAuth, JWT, and RBAC
- `cmd/` - Command-line executables (server, migrate, check-db)
- `internal/` - Internal packages (logging, dbschema)
- `docs/` - API documentation and architectural diagrams
- `scripts/` - Development setup scripts

### WebSocket Architecture

- Real-time collaboration via WebSocket connections at `/ws/diagrams/{id}`
- WebSocketHub manages active connections and broadcasts updates
- Only diagrams support real-time collaboration, not threat models
- Uses Gorilla WebSocket library

### Database Integration

- PostgreSQL for persistent storage (configured via auth/ package)
- Redis for caching and session management
- Database migrations in auth/migrations/
- Development uses Docker containers
- Dual-mode storage: in-memory for tests, database-backed for dev/prod

## Development Environment

- Copy `.env.example` to `.env.dev` for local development
- Uses PostgreSQL and Redis Docker containers
- Development scripts handle container management automatically
- Server runs on port 8080 by default with configurable TLS support

## User Preferences

- After changing any file, run `make lint` and fix any issues caused by the change
- After changing any executable or test file, run `make build` and fix any issues, then run `make test-unit` and fix any issues
- Do not disable or skip failing tests, either diagnose to root cause and fix either the test issue or code issue, or ask the user what to do
- Always use make targets for testing - never run `go test` commands directly
- For database-dependent functionality, also run `make test-integration` to ensure full integration works

## Make Command Memories

- `make list` is useful for quickly discovering and reviewing all available make targets in the project
- `make validate-asyncapi` validates the AsyncAPI specification for the project

## Test Execution Guidelines

**CRITICAL: Never run `go test` commands directly. Always use make targets.**

- Unit tests: Use `make test-unit` or `make test-unit name=TestName`
- Integration tests: Use `make test-integration` or `make test-integration name=TestName` 
- Never create ad hoc `go test` commands - they will miss configuration settings and dependencies
- Never create ad hoc commands to run the server - use `make dev` or other make targets
- All testing must go through make targets to ensure proper environment setup

## Test Philosophy

- Never disable or skip failing tests - investigate to root cause and fix
- Unit tests (`make test-unit`) should be fast and require no external dependencies
- Integration tests (`make test-integration`) should use real databases and test full workflows
- Always run `make lint` and `make build` after making changes

### OpenAPI Integration

- API code generated from tmi-openapi.json using oapi-codegen v2
- OpenAPI validation middleware clears security schemes (auth handled by JWT middleware)
- Generated types in api/api.go include Echo server handlers and embedded spec
- Config file: oapi-codegen-config.yaml

## Authentication Memories

- Always use a normal oauth login flow with the "test" provider when performing any development or testing task that requires authentication
- Use `make test-api auth=only` to get JWT tokens for testing
- OAuth test provider generates JWT tokens with user claims (email, name, sub)
- For API testing, use `make test-api` (requires `make dev` first)