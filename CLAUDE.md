# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains API documentation and Go implementation for a Collaborative Threat Modeling Interface (TMI). It's a server-based web application enabling collaborative threat modeling with real-time diagram editing via WebSockets, role-based access control, OAuth authentication with JWT, and a RESTful API with OpenAPI 3.0 specification.

## Key Files

- docs/reference/apis/tmi-openapi.json - OpenAPI specification
- api/store.go - Generic typed map storage implementation
- api/server.go - Main API server with WebSocket support
- api/websocket.go - WebSocket hub for real-time collaboration
- cmd/server/main.go - Server entry point
- Makefile - Build automation with development targets

## Custom Tools

### fx

The run_fx tool is available for json file manipulation

### jq

The run_jq tool is available for json file manipulation

### Specialized JSON Handling

When working with JSON files **larger than 100KB** or requiring complex manipulations, apply specialized JSON processing techniques from the `json_agent` configuration. This agent provides memory-efficient strategies using `jq` and `fx` tools for streaming, surgical updates, and validation.

#### Activation Triggers

- JSON files ≥ 100KB (check with `ls -lh` or `stat`)
- Memory errors or slow performance with standard tools
- Need for surgical updates (modify specific paths without full rewrite)
- Batch operations across multiple JSON files
- User mentions "large", "efficient", "streaming", or "without loading entire file"

#### Quick Tool Selection

- **jq**: Preferred for files > 100KB, streaming operations, surgical path updates
- **fx**: Better for complex JavaScript logic, interactive exploration on files < 10MB
- **Standard tools**: Only for files < 100KB with simple operations

#### Always Remember

1. Check file size first: `stat -f%z file.json 2>/dev/null || stat -c%s file.json`
2. Create backups before modifications: `cp file.json file.json.$(date +%Y%m%d_%H%M%S).backup`
3. Validate after changes: `jq empty modified.json && echo "Valid" || echo "Invalid"`

For any JSON ≥ 100KB, immediately switch to streaming approaches with jq to prevent memory issues and ensure responsive performance.

## Automatic Versioning

TMI uses automatic semantic versioning (0.MINOR.PATCH):
- **Build**: `make build-server` increments patch version (0.9.0 → 0.9.1)
- **Commit**: Pre-commit hook increments minor version, resets patch (0.9.3 → 0.10.0)
- **Version file**: `.version` (JSON) tracks current state
- **Script**: `scripts/update-version.sh --build` or `--commit`
- **Documentation**: See `docs/developer/setup/automatic-versioning.md`

The major version remains at 0 during initial development. Version updates are fully automated—no manual intervention required.

## Commands

- List targets: `make list-targets` (lists all available make targets)
- Build: `make build-server` (creates bin/tmiserver executable, auto-increments patch version)
- Lint: `make lint` (runs golangci-lint)
- Generate API: `make generate-api` (uses oapi-codegen with config from oapi-codegen-config.yml)
- Development: `make start-dev` (starts full dev environment with DB and Redis)
- Secure Development: `make start-dev-secure` (starts server using existing secure containers)
- Dev DB only: `make start-database` (starts PostgreSQL container)
- Dev Redis only: `make start-redis` (starts Redis container)
- Clean all: `make clean-everything` (comprehensive cleanup of processes, containers, and files)
- Observability: `make observability-start` (starts OpenTelemetry monitoring stack), `make obs-start` (alias)
- Stop observability: `make observability-stop` (stops monitoring services), `make obs-stop` (alias)
- Clean observability: `make observability-clean` (removes monitoring data), `make obs-clean` (alias)

### Container Management (Docker Scout Integration)

- Security scan: `make scan-containers` (scans containers for vulnerabilities using Docker Scout)
- Security report: `make report-containers` (generates comprehensive security report)
- Build containers: `make build-containers` (builds containers with vulnerability patches)
- Container development: `make containers-dev` (builds and starts containers, no server)
- Start server with containers: `make start-dev-existing` (starts server using existing containers)
- Full container workflow: `make containers-all` (builds containers and generates reports)

### SBOM Generation (Software Bill of Materials)

TMI uses two complementary tools for comprehensive SBOM generation:

#### cyclonedx-gomod (Go Components)
- Generate Go app SBOM: `make generate-sbom` (creates JSON + XML for server application)
- Generate all Go SBOMs: `make generate-sbom-all` (app + module dependencies)
- Build with SBOM: `make build-with-sbom` (builds tmiserver binary + generates SBOM)
- Check tool: `make check-cyclonedx` (verifies cyclonedx-gomod is installed)
- Install: `brew install cyclonedx/cyclonedx/cyclonedx-gomod` or `go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest`

#### Syft (Container Images)
- Automatically used during: `make build-containers` (scans all container images)
- Scans PostgreSQL (Chainguard base), Redis (distroless base), Server (distroless base) containers
- Check tool: `make check-syft` (verifies Syft is installed)
- Install: `brew install syft` or `curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /usr/local/bin`

**Output Location**: `security-reports/sbom/` (CycloneDX JSON + XML formats)
**Container Integration**: SBOMs automatically generated during `make build-containers`
**Formats**: CycloneDX 1.6 specification for all SBOMs (consistent across both tools)

### OpenAPI Schema Management

- JSON Patcher Tool: `python3 scripts/patch-json.py` - Utility for making precise modifications to OpenAPI specification
  - Patch schema: `python3 scripts/patch-json.py -s docs/reference/apis/tmi-openapi.json -p "$.components.schemas.SchemaName" -f patch.json`
  - Creates automatic backups and validates JSON structure
  - Useful for implementing Input/Output schema separation or other targeted schema modifications
- Validate OpenAPI: `make validate-openapi [file=path/to/spec.json]` (validates OpenAPI specification with comprehensive JSON syntax and detailed analysis)

### OAuth Callback Stub

- **OAuth Development Tool**: `make start-oauth-stub` or `uv run scripts/oauth-client-callback-stub.py --port 8079` - Universal OAuth callback handler supporting both Authorization Code and Implicit flows

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
    - `make start-oauth-stub` - Start OAuth stub on port 8079
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
    make start-oauth-stub

    # Initiate OAuth flow with callback stub and login_hint
    curl "http://localhost:8080/oauth2/authorize?idp=test&login_hint=alice&client_callback=http://localhost:8079/"

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

### WebSocket Test Harness

- **WebSocket Testing Tool**: `make wstest` - Standalone Go application for testing and debugging WebSocket collaborative features

  - **Location**: `ws-test-harness/` directory contains the Go source code
  - **Purpose**: Test WebSocket connections, diagnose collaboration bugs, and validate message flows
  - **Features**:
    - OAuth authentication with test provider using login hints
    - Host mode: Creates threat models, diagrams, and starts collaboration sessions
    - Participant mode: Polls for and joins existing collaboration sessions
    - Comprehensive logging of all WebSocket messages with timestamps
    - Supports multiple concurrent instances for multi-user testing
    - 30-second timeout to prevent runaway processes
  - **Make Commands**:
    - `make build-wstest` - Build the test harness binary
    - `make wstest` - Launch 3-terminal test (alice as host, bob & charlie as participants)
    - `make wstest-clean` - Stop all running test harness instances
  - **Direct Usage**:

    ```bash
    # Build the test harness
    cd ws-test-harness && go build -o ws-test-harness

    # Run as host (creates new collaboration session)
    ./ws-test-harness --user alice --host --participants "bob,charlie"

    # Run as participant (joins existing session)
    ./ws-test-harness --user bob

    # With custom server
    ./ws-test-harness --server http://localhost:8080 --user alice --host
    ```

  - **Debugging WebSocket Issues**:
    - All WebSocket messages are logged with timestamps and pretty-printed JSON
    - Check expected initial messages: `current_presenter`, `participants_update`
    - Add test cases by modifying the message handling in `connectToWebSocket()`
    - Use for regression testing when modifying WebSocket protocols
  - **Test Scenarios**:

    ```bash
    # Basic collaboration test
    make start-dev  # Ensure server is running
    make wstest     # Launches alice (host), bob, and charlie (participants)
    # Watch the terminals for WebSocket activity
    make wstest-clean  # Clean up when done

    # Manual multi-user test
    ./ws-test-harness --user alice --host --participants "bob,charlie,dave" &
    sleep 5
    ./ws-test-harness --user bob &
    ./ws-test-harness --user charlie &
    ./ws-test-harness --user dave &
    ```

  - **Adding Test Cases**:
    - Modify `ws-test-harness/main.go` to add new test scenarios
    - Send test messages after connection in `connectToWebSocket()`
    - Validate expected responses in the message reader goroutine
    - Use for testing edge cases, error conditions, and protocol changes

## Critical Development Guidelines

**MANDATORY: Always use Make targets - NEVER run commands directly**

- ❌ **NEVER run**: `go run`, `go test`, `./bin/tmiserver`, `docker run`, `docker exec`
- ✅ **ALWAYS use**: `make start-dev`, `make test-unit`, `make test-integration`, `make build-server`
- **Reason**: Make targets provide consistent, repeatable configurations with proper environment setup

**Examples of FORBIDDEN practices:**

```bash
# ❌ DON'T DO THESE:
go run cmd/server/main.go --config=config-development.yml
go test ./api/...
./bin/tmiserver --config=config-development.yml
docker exec tmi-postgresql psql -U postgres
docker run -d postgres:13

# ✅ DO THESE INSTEAD:
make start-dev
make test-unit
make test-integration
make start-database
```

**Container Management**: Use `make start-database`, `make start-redis`, `make start-dev` for all container operations.

### Testing Commands

**IMPORTANT: Always use make targets for testing. Never run `go test` commands directly.**

#### Core Testing

- Unit tests: `make test-unit` (fast tests, no external dependencies)
  - Specific test: `make test-unit name=TestName`
  - Options: `make test-unit count1=true passfail=true`
- Integration tests: `make test-integration` (requires database, runs with automatic setup/cleanup)
  - Specific test: `make test-integration name=TestName`
  - Cleanup only: `make clean-everything`

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
make start-dev                   # Start server first

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
- `docs/` - Comprehensive documentation organized by audience (developer, operator, agent, reference)
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

## Documentation Organization

The `docs/` directory is organized by audience for easy navigation:

- **`docs/developer/`** - Development setup, testing, and client integration guides
- **`docs/operator/`** - Deployment, database operations, and monitoring documentation
- **`docs/agent/`** - AI agent context and visual architecture references
- **`docs/reference/`** - Technical specifications, schemas, and API documentation

Key developer documentation:
- Development setup: `docs/developer/setup/development-setup.md`
- Integration testing: `docs/developer/testing/integration-testing.md`
- Client integration: `docs/developer/integration/client-integration-guide.md`
- OAuth setup: `docs/developer/setup/oauth-integration.md`

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
- Never create ad hoc commands to run the server - use `make start-dev` or other make targets
- All testing must go through make targets to ensure proper environment setup

## Test Philosophy

- Never disable or skip failing tests - investigate to root cause and fix
- Unit tests (`make test-unit`) should be fast and require no external dependencies
- Integration tests (`make test-integration`) should use real databases and test full workflows
- Always run `make lint` and `make build-server` after making changes

## Logging Requirements

**CRITICAL: Never use the standard `log` package. Always use structured logging.**

- **ALWAYS** use `github.com/ericfitz/tmi/internal/slogging` for all logging operations
- **NEVER** import or use the standard `log` package (`"log"`) in any Go code
- Use `slogging.Get()` for global logging or `slogging.Get().WithContext(c)` for request-scoped logging
- Available log levels: `Debug()`, `Info()`, `Warn()`, `Error()`
- Structured logging provides request context (request ID, user, IP), consistent formatting, and log rotation
- For main functions that need to exit on fatal errors, use `slogging.Get().Error()` followed by `os.Exit(1)` instead of `log.Fatalf()`

### OpenAPI Integration

- API code generated from docs/reference/apis/tmi-openapi.json using oapi-codegen v2
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

````

**Key Components**:

- `api/server.go`: Main OpenAPI server with single router
- `api/*_middleware.go`: Resource-specific authorization middleware
- `auth/handlers.go`: Authentication endpoints integrated via auth service adapter
- `api/request_tracing.go`: Module-tagged request logging for debugging

## Authentication Memories

- Always use a normal oauth login flow with the "test" provider when performing any development or testing task that requires authentication
- The oauth-client-callback-stub can receive callbacks from the test oauth provider with the token, and you can retrieve the token from the oauth-client-callback-stub with a REST api call.
    - start stub: make start-oauth-stub
    - stop stub: make oauth-stub-stop
    - get JWT:
        - start the stub
        - perform a normal authorization request, using http://localhost:8079 as the callback url and specifying a user name as a login_hint
        - retrieve the JWT from http://localhost:8079/creds?userid=<username-hint>

### Test OAuth Provider login_hints

The test OAuth provider supports **login_hints** for automation-friendly testing with predictable user identities:

- **Parameter**: `login_hint` - Query parameter for `/oauth2/authorize?idp=test`
- **Purpose**: Generate predictable test users instead of random usernames
- **Format**: 3-20 characters, alphanumeric + hyphens, case-insensitive
- **Validation**: Pattern: `^[a-zA-Z0-9-]{3,20}$`
- **Scope**: Test provider only, not available in production builds

**Examples**:

```bash
# Create user 'alice@test.tmi' with name 'Alice (Test User)'
curl "http://localhost:8080/oauth2/authorize?idp=test&login_hint=alice"

# Create user 'qa-automation@test.tmi' with name 'Qa Automation (Test User)'
curl "http://localhost:8080/oauth2/authorize?idp=test&login_hint=qa-automation"

# Without login_hint - generates random user like 'testuser-12345678@test.tmi'
curl "http://localhost:8080/oauth2/authorize?idp=test"
````

**Automation Integration**:

```bash
# OAuth callback stub with login_hint
curl "http://localhost:8080/oauth2/authorize?idp=test&login_hint=alice&client_callback=http://localhost:8079/"

## Python Development Memories

- Run python scripts with uv. When creating python scripts, add uv toml to the script for automatic package management.
```
