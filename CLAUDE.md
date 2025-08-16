# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains API documentation and Go implementation for a Collaborative Threat Modeling Interface (TMI). It's a server-based web application enabling collaborative threat modeling with real-time diagram editing via WebSockets, role-based access control, OAuth authentication with JWT, and a RESTful API with OpenAPI 3.0 specification.

## Key Files

- docs/TMI-API-v1_0.md - API documentation in Markdown
- shared/api-specs/tmi-openapi.json - OpenAPI specification
- api/store.go - Generic typed map storage implementation
- api/server.go - Main API server with WebSocket support
- api/websocket.go - WebSocket hub for real-time collaboration
- cmd/server/main.go - Server entry point
- Makefile - Build automation with development targets

## Commands

- List targets: `make list-targets` (lists all available make targets)
- Build: `make build-server` (creates bin/server executable)
- Lint: `make run-lint` (runs golangci-lint)
- Generate API: `make generate-api` (uses oapi-codegen with config from oapi-codegen-config.yaml)
- Development: `make start-dev` (starts full dev environment with DB and Redis)
- Dev DB only: `make start-dev-db` (starts PostgreSQL container)
- Dev Redis only: `make start-dev-redis` (starts Redis container)

### OpenAPI Schema Management

- JSON Patcher Tool: `python3 scripts/patch-json.py` - Utility for making precise modifications to OpenAPI specification
  - Patch schema: `python3 scripts/patch-json.py -s shared/api-specs/tmi-openapi.json -p "$.components.schemas.SchemaName" -f patch.json`
  - Creates automatic backups and validates JSON structure
  - Useful for implementing Input/Output schema separation or other targeted schema modifications
- Validate OpenAPI: `make validate-openapi [file=path/to/spec.json]` (validates OpenAPI specification with comprehensive JSON syntax and detailed analysis)

### API Testing Tool

- **Ad-hoc API Testing**: `make test-api-script script=<script.txt>` - Human-readable API testing with simple script format
  - **Location**: `scripts/api_test.py` (executable Python script)
  - **Examples**: `test_examples/` directory contains sample test scripts
  - **Features**: OAuth authentication, variable substitution, JSON expectations, response validation
  - **Script Format**:
    ```
    server localhost    # Configure server (optional)
    port 8080          # Configure port (optional) 
    auth user1         # Authenticate user via OAuth
    request createtm post /threat_models $user1.jwt$ body={"name":"Test"}
    expect $createtm.status$ == 201
    expect $createtm.body.id$ exists
    ```

## Critical Development Guidelines

**MANDATORY: Always use Make targets - NEVER run commands directly**

- ❌ **NEVER run**: `go run`, `go test`, `./bin/server`, `docker run`, `docker exec`
- ✅ **ALWAYS use**: `make start-dev`, `make test-unit`, `make test-integration`, `make build-server`
- **Reason**: Make targets provide consistent, repeatable configurations with proper environment setup

**Examples of FORBIDDEN practices:**
```bash
# ❌ DON'T DO THESE:
go run cmd/server/main.go --config=config-development.yaml
go test ./api/...
./bin/server --config=config-development.yaml
docker exec tmi-postgresql psql -U postgres
docker run -d postgres:13

# ✅ DO THESE INSTEAD:
make start-dev
make test-unit
make test-integration
make start-dev-db
```

**Container Management**: Use `make start-dev-db`, `make start-dev-redis`, `make start-dev` for all container operations.

### Testing Commands

**IMPORTANT: Always use make targets for testing. Never run `go test` commands directly.**

#### Core Testing
- Unit tests: `make test-unit` (fast tests, no external dependencies)
  - Specific test: `make test-unit name=TestName`
  - Options: `make test-unit count1=true passfail=true`
- Integration tests: `make test-integration` (requires database, runs with automatic setup/cleanup)
  - Specific test: `make test-integration name=TestName` 
  - Cleanup only: `make test-integration-cleanup`
- Alias: `make run-tests` (same as `make test-unit` for backward compatibility)

#### Specialized Testing  
- Telemetry tests: `make test-telemetry` (unit tests for telemetry components)
  - Integration mode: `make test-telemetry integration=true`
- API testing: `make test-api` (requires running server via `make start-dev`)
  - Auth token only: `make test-api auth=only`
  - No auth test only: `make test-api noauth=true`
- Full API test: `make test-api-full` (automated setup: kills servers, starts DB/Redis, starts server, runs tests, cleans up)
- Development test: `make test-dev` (builds and tests API endpoints, requires running server)
- Full dev test: `make test-dev-full` (alias for `make test-api-full`)

#### Testing Examples
```bash
# Standard development workflow
make test-unit                    # Fast unit tests
make test-integration            # Full integration tests with database
make run-lint && make build-server # Code quality check and build

# Specific testing
make test-unit name=TestStore_CRUD              # Run one unit test
make test-integration name=TestDatabaseIntegration  # Run one integration test

# API testing (requires server)
make start-dev                   # Start server first
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

- After changing any file, run `make run-lint` and fix any issues caused by the change
- After changing any executable or test file, run `make build-server` and fix any issues, then run `make test-unit` and fix any issues
- Do not disable or skip failing tests, either diagnose to root cause and fix either the test issue or code issue, or ask the user what to do
- Always use make targets for testing - never run `go test` commands directly
- For database-dependent functionality, also run `make test-integration` to ensure full integration works

## Make Command Memories

- `make list-targets` is useful for quickly discovering and reviewing all available make targets in the project
- `make validate-asyncapi` validates the AsyncAPI specification for the project

## Test Execution Guidelines

**CRITICAL: Never run `go test` commands directly. Always use make targets.**

- Unit tests: Use `make test-unit` or `make test-unit name=TestName`
- Integration tests: Use `make test-integration` or `make test-integration name=TestName` 
- Never create ad hoc `go test` commands - they will miss configuration settings and dependencies
- Never create ad hoc commands to run the server - use `make start-dev` or other make targets
- All testing must go through make targets to ensure proper environment setup

## Test Philosophy

- Never disable or skip failing tests - investigate to root cause and fix
- Unit tests (`make test-unit`) should be fast and require no external dependencies
- Integration tests (`make test-integration`) should use real databases and test full workflows
- Always run `make run-lint` and `make build-server` after making changes

## Logging Requirements

**CRITICAL: Never use the standard `log` package. Always use structured logging.**

- **ALWAYS** use `github.com/ericfitz/tmi/internal/logging` for all logging operations
- **NEVER** import or use the standard `log` package (`"log"`) in any Go code
- Use `logging.Get()` for global logging or `logging.Get().WithContext(c)` for request-scoped logging
- Available log levels: `Debug()`, `Info()`, `Warn()`, `Error()`
- Structured logging provides request context (request ID, user, IP), consistent formatting, and log rotation
- For main functions that need to exit on fatal errors, use `logging.Get().Error()` followed by `os.Exit(1)` instead of `log.Fatalf()`

### OpenAPI Integration

- API code generated from shared/api-specs/tmi-openapi.json using oapi-codegen v2
- OpenAPI validation middleware clears security schemes (auth handled by JWT middleware)
- Generated types in api/api.go include Echo server handlers and embedded spec
- Config file: oapi-codegen-config.yaml

## Clean Architecture - Request Flow

**Current Architecture (Post-Cleanup)**:

The system now uses a clean, single-router architecture with OpenAPI-driven routing:

1. **Single Router Architecture**: All HTTP requests flow through the OpenAPI specification
2. **Request Tracing**: Comprehensive module-tagged debug logging for all requests
3. **Authentication Flow**: 
   - JWT middleware validates tokens and sets user context
   - ThreatModelMiddleware and DiagramMiddleware handle resource-specific authorization
   - Auth handlers integrate cleanly with OpenAPI endpoints
4. **No Route Conflicts**: Single source of truth for all routing eliminates duplicate route registration panics

**Request Flow**:
```
HTTP Request → OpenAPI Route Registration → ServerInterface Implementation → 
JWT Middleware → Auth Context → Resource Middleware → Endpoint Handlers
```

**Key Components**:
- `api/server.go`: Main OpenAPI server with single router
- `api/*_middleware.go`: Resource-specific authorization middleware
- `auth/handlers.go`: Authentication endpoints integrated via auth service adapter
- `api/request_tracing.go`: Module-tagged request logging for debugging

## Authentication Memories

- Always use a normal oauth login flow with the "test" provider when performing any development or testing task that requires authentication  
- Use `make test-api auth=only` to get JWT tokens for testing
- OAuth test provider generates JWT tokens with user claims (email, name, sub)
- For API testing, use `make test-api` (requires `make start-dev` first)

## Python Development Memories

- Run python scripts with uv. When creating python scripts, add uv toml to the script for automatic package management.