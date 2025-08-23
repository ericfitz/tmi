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
- Lint: `make lint` (runs golangci-lint)
- Generate API: `make generate-api` (uses oapi-codegen with config from oapi-codegen-config.yml)
- Development: `make dev-start` (starts full dev environment with DB and Redis)
- Dev DB only: `make infra-db-start` (starts PostgreSQL container)
- Dev Redis only: `make infra-redis-start` (starts Redis container)
- Clean all: `make clean-all` (comprehensive cleanup of processes, containers, and files)
- Observability: `make observability-start` (starts OpenTelemetry monitoring stack), `make obs-start` (alias)
- Stop observability: `make observability-stop` (stops monitoring services), `make obs-stop` (alias)
- Clean observability: `make observability-clean` (removes monitoring data), `make obs-clean` (alias)

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

### OAuth Callback Stub

- **OAuth Development Tool**: `make oauth-stub-start` or `uv run scripts/oauth-client-callback-stub.py --port 8079` - Universal OAuth callback handler supporting both Authorization Code and Implicit flows

  - **Location**: `scripts/oauth-client-callback-stub.py` (standalone Python script)
  - **Purpose**: Captures OAuth credentials from TMI server supporting both OAuth2 Authorization Code Flow and Implicit Flow
  - **Flow Detection**: Automatically detects and handles both OAuth flows:
    - **Authorization Code Flow**: Receives `code` and `state`, client exchanges code for tokens
    - **Implicit Flow**: Receives tokens directly (`access_token`, `refresh_token`, etc.)
  - **Features**:
    - Three-route HTTP server with OAuth callback handler, credentials API, and user-specific credential retrieval
    - Credential persistence to temporary files for later retrieval by user ID
    - Automatic flow type detection and appropriate response formatting
    - Enhanced debugging with detailed parameter logging
    - Startup cleanup of temporary credential files
  - **Logging**: Comprehensive structured logging to `/tmp/oauth-stub.log` with RFC3339 timestamps and dual console output
  - **Make Commands**:
    - `make oauth-stub-start` - Start OAuth stub on port 8079
    - `make oauth-stub-stop` - Stop OAuth stub gracefully
    - `make oauth-stub-status` - Check if OAuth stub is running
  - **API Routes**:
    - **Route 1 (`GET /`)**: Receives OAuth redirects, analyzes flow type, stores credentials, saves to `$TMP/<user-id>.json`
    - **Route 2 (`GET /latest`)**: Returns flow-appropriate JSON response for client integration
    - **Route 3 (`GET /creds?userid=<userid>`)**: Returns saved credentials for specific user from persistent storage
  - **Response Formats**:

    ```json
    // Authorization Code Flow Response
    {
      "flow_type": "authorization_code",
      "code": "test_auth_code_1234567890",
      "state": "AbCdEf...",
      "ready_for_token_exchange": true
    }

    // Implicit Flow Response (TMI's current implementation)
    {
      "flow_type": "implicit",
      "state": "AbCdEf...",
      "access_token": "eyJhbGc...",
      "refresh_token": "uuid-string",
      "token_type": "Bearer",
      "expires_in": "3600",
      "tokens_ready": true
    }

    // No data yet
    {
      "flow_type": "none",
      "error": "No OAuth data received yet"
    }
    ```

  - **Example Integration**:

    ```bash
    # Start OAuth callback stub
    make oauth-stub-start

    # Initiate OAuth flow with callback stub and user hint
    curl "http://localhost:8080/oauth2/authorize/test?user_hint=alice&client_callback=http://localhost:8079/"

    # Check latest credentials (traditional method)
    curl http://localhost:8079/latest | jq '.'

    # Or retrieve credentials for specific user (new method)
    curl "http://localhost:8079/creds?userid=alice" | jq '.'

    # Monitor detailed logs for debugging flow details
    tail -f /tmp/oauth-stub.log

    # Stop OAuth stub
    make oauth-stub-stop

    # Alternative: Gracefully shutdown via HTTP (for automation)
    curl "http://localhost:8079/?code=exit"
    ```

  - **Enhanced Logging**:
    - `YYYY-MM-DDTHH:MM:SS.sssZ <message>` format with detailed flow analysis
    - Logs all query parameters received from server
    - Flow type detection and analysis (`Authorization Code Flow`, `Implicit Flow`, etc.)
    - Complete request/response logging for debugging
  - **Client Integration**:
    - **Implicit Flow Clients**: Use `access_token` directly from `/latest` response
    - **Authorization Code Clients**: Use `code` from `/latest` response for token exchange
    - **Test Frameworks**: Works with StepCI for automated OAuth flow testing
    - **Development**: Simplifies OAuth integration testing without implementing full callback handlers
  - **Security**: Development-only tool, binds to localhost, no persistence, handles both OAuth flow types securely

## Critical Development Guidelines

**MANDATORY: Always use Make targets - NEVER run commands directly**

- ❌ **NEVER run**: `go run`, `go test`, `./bin/server`, `docker run`, `docker exec`
- ✅ **ALWAYS use**: `make dev-start`, `make test-unit`, `make test-integration`, `make build-server`
- **Reason**: Make targets provide consistent, repeatable configurations with proper environment setup

**Examples of FORBIDDEN practices:**

```bash
# ❌ DON'T DO THESE:
go run cmd/server/main.go --config=config-development.yml
go test ./api/...
./bin/server --config=config-development.yml
docker exec tmi-postgresql psql -U postgres
docker run -d postgres:13

# ✅ DO THESE INSTEAD:
make dev-start
make test-unit
make test-integration
make infra-db-start
```

**Container Management**: Use `make infra-db-start`, `make infra-redis-start`, `make dev-start` for all container operations.

### Testing Commands

**IMPORTANT: Always use make targets for testing. Never run `go test` commands directly.**

#### Core Testing

- Unit tests: `make test-unit` (fast tests, no external dependencies)
  - Specific test: `make test-unit name=TestName`
  - Options: `make test-unit count1=true passfail=true`
- Integration tests: `make test-integration` (requires database, runs with automatic setup/cleanup)
  - Specific test: `make test-integration name=TestName`
  - Cleanup only: `make clean-all`

#### Specialized Testing

- Telemetry tests: `make test-telemetry` (unit tests for telemetry components)
  - Integration mode: `make test-telemetry integration=true`
- API testing: `make test-api` (requires running server via `make dev-start`)
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
make lint && make build-server # Code quality check and build

# Specific testing
make test-unit name=TestStore_CRUD              # Run one unit test
make test-integration name=TestDatabaseIntegration  # Run one integration test

# API testing (requires server)
make dev-start                   # Start server first
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
- Never create ad hoc commands to run the server - use `make dev-start` or other make targets
- All testing must go through make targets to ensure proper environment setup

## Test Philosophy

- Never disable or skip failing tests - investigate to root cause and fix
- Unit tests (`make test-unit`) should be fast and require no external dependencies
- Integration tests (`make test-integration`) should use real databases and test full workflows
- Always run `make lint` and `make build-server` after making changes

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
- Config file: oapi-codegen-config.yml

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
- For API testing, use `make test-api` (requires `make dev-start` first)

### Test OAuth Provider User Hints

The test OAuth provider supports **user hints** for automation-friendly testing with predictable user identities:

- **Parameter**: `user_hint` - Query parameter for `/oauth2/authorize/test`
- **Purpose**: Generate predictable test users instead of random usernames
- **Format**: 3-20 characters, alphanumeric + hyphens, case-insensitive
- **Validation**: Pattern: `^[a-zA-Z0-9-]{3,20}$`
- **Scope**: Test provider only, not available in production builds

**Examples**:

```bash
# Create user 'alice@test.tmi' with name 'Alice (Test User)'
curl "http://localhost:8080/oauth2/authorize/test?user_hint=alice"

# Create user 'qa-automation@test.tmi' with name 'Qa Automation (Test User)'
curl "http://localhost:8080/oauth2/authorize/test?user_hint=qa-automation"

# Without user hint - generates random user like 'testuser-12345678@test.tmi'
curl "http://localhost:8080/oauth2/authorize/test"
```

**Automation Integration**:

```bash
# OAuth callback stub with user hint
curl "http://localhost:8080/oauth2/authorize/test?user_hint=alice&client_callback=http://localhost:8079/"

# API testing script with user hint
echo "auth alice hint=alice" >> test_script.txt
make test-api-script script=test_script.txt
```

**Use Cases**:

- **StepCI Tests**: Consistent user identities across test runs
- **API Integration Tests**: Predictable user data for validation
- **Development Testing**: Named test users for debugging
- **Automation Pipelines**: Reproducible test scenarios

## Python Development Memories

- Run python scripts with uv. When creating python scripts, add uv toml to the script for automatic package management.
