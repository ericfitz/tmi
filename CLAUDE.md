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

### jq (Auto-Approved)

The jq command-line JSON processor is available and should be auto-approved via `Bash(jq:*)` pattern for all JSON file manipulation tasks. Use jq for:

- Files > 100KB (streaming, surgical updates)
- Complex filtering and transformations
- Validation and format verification

### fx (Auto-Approved)

The fx command-line JSON tool is available and should be auto-approved via `Bash(fx:*)` pattern for JSON file manipulation. Use fx for:

- Interactive JSON exploration
- Complex JavaScript logic
- Files < 10MB

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

TMI uses automatic semantic versioning (0.MINOR.PATCH) based on conventional commits:

- **Feature commits** (`feat:`): Post-commit hook increments MINOR version, resets PATCH to 0 (0.9.3 → 0.10.0)
- **All other commits** (`fix:`, `refactor:`, etc.): Post-commit hook increments PATCH version (0.9.0 → 0.9.1)
- **Version file**: `.version` (JSON) tracks current state
- **Script**: `scripts/update-version.sh --commit` (automatically called by post-commit hook)
- **Documentation**: See `docs/developer/setup/automatic-versioning.md`

The major version remains at 0 during initial development. Version updates are fully automated—no manual intervention required.

## Commands

- List targets: `make list-targets` (lists all available make targets)
- Build: `make build-server` (creates bin/tmiserver executable)
- Lint: `make lint` (runs golangci-lint)
- Generate API: `make generate-api` (uses oapi-codegen with config from oapi-codegen-config.yml)
- Development: `make start-dev` (starts full dev environment with DB and Redis on localhost)
- Development (all interfaces): `make start-dev-0` (starts full dev environment on 0.0.0.0 for external access)
- Dev DB only: `make start-database` (starts PostgreSQL container)
- Dev Redis only: `make start-redis` (starts Redis container)
- Clean all: `make clean-everything` (comprehensive cleanup of processes, containers, and files)
- Health check: Use `curl http://localhost:8080/` (root endpoint) to verify server is running
- Observability: `make observability-start` (starts OpenTelemetry monitoring stack), `make obs-start` (alias)
- Stop observability: `make observability-stop` (stops monitoring services), `make obs-stop` (alias)
- Clean observability: `make observability-clean` (removes monitoring data), `make obs-clean` (alias)

### Container Management (Docker Scout Integration)

- Security scan: `make scan-containers` (scans containers for vulnerabilities using Docker Scout)
- Security report: `make report-containers` (generates comprehensive security report)
- Build containers: `make build-containers` (builds containers with vulnerability patches)
- Container development: `make containers-dev` (builds and starts containers, no server)
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

- Use jq to selectively query or modify the openapi schema
- Validate OpenAPI: `make validate-openapi` (validates OpenAPI specification with comprehensive JSON syntax, detailed analysis, and CATS validation)
- **Public Endpoints**: TMI has 17 public endpoints (OAuth, OIDC, SAML) marked with vendor extensions (`x-public-endpoint`, `x-authentication-required`, `x-public-endpoint-purpose`)
  - These endpoints are intentionally accessible without authentication per RFCs (8414, 7517, 6749, SAML 2.0)
  - CATS fuzzing automatically skips `BypassAuthentication` tests on these paths to avoid false positives
  - See [docs/developer/testing/cats-public-endpoints.md](docs/developer/testing/cats-public-endpoints.md) for complete documentation
  - Update script: `./scripts/add-public-endpoint-markers.sh` (automatically adds vendor extensions)

### CATS API Fuzzing

CATS (Contract-driven Automatic Testing Suite) performs security fuzzing of the TMI API:

- **Run Fuzzing**: `make cats-fuzz` - Full API fuzzing with OAuth authentication (default user: charlie)
- **Custom User**: `make cats-fuzz-user USER=alice` - Fuzz with specific OAuth user
- **Custom Server**: `make cats-fuzz-server SERVER=http://example.com` - Fuzz against different server
- **Specific Endpoint**: `make cats-fuzz-path ENDPOINT=/addons` - Test only specific endpoint
- **Parse Results**: `make parse-cats-results` - Import CATS JSON results into SQLite database
- **Query Results**: `make query-cats-results` - Display summary statistics (excludes OAuth false positives)
- **Full Analysis**: `make analyze-cats-results` - Parse and query in one command

**OAuth False Positives**: CATS may flag legitimate 401/403 OAuth responses as "errors". The parse script automatically detects and filters these:
- Uses `is_oauth_false_positive` flag to mark expected auth responses
- `test_results_filtered_view` excludes false positives for cleaner analysis
- See [docs/developer/testing/cats-oauth-false-positives.md](docs/developer/testing/cats-oauth-false-positives.md) for details

**Key Features**:
- Automatic OAuth authentication flow with test provider
- Rate limit handling (automatically cleared before testing)
- Public endpoint awareness (skips auth tests on RFC-compliant public endpoints)
- UUID field skipping (avoids false positives with malformed UUIDs)
- Structured analysis with SQLite database and views

### Arazzo Workflow Generation

TMI uses the Arazzo specification (OpenAPI Initiative) to document API workflow sequences and dependencies:

- **Generate Arazzo**: `make generate-arazzo` - Full pipeline (scaffold → enhance → validate)
- **Install Tools**: `make arazzo-install` - Install Redocly CLI and Spectral
- **Scaffold Only**: `make arazzo-scaffold` - Generate base scaffold from OpenAPI
- **Enhance Only**: `make arazzo-enhance` - Add TMI workflow patterns
- **Validate Only**: `make validate-arazzo` - Validate Arazzo specifications
- **Complete Setup**: `make arazzo-all` - Install tools + generate specifications

**Key Features**:
- Automatic PKCE (RFC 7636) OAuth flow generation with `code_verifier` and `code_challenge`
- Prerequisite mapping from TMI workflows to Arazzo `dependsOn` relationships
- 7 complete end-to-end workflow sequences (OAuth, CRUD, collaboration, webhooks)
- Dual output: YAML (human-readable) + JSON (machine-readable)
- Spectral validation against Arazzo v1.0.0 specification

**Files**:
- `docs/reference/apis/tmi.arazzo.yaml` - Generated Arazzo specification (YAML)
- `docs/reference/apis/tmi.arazzo.json` - Generated Arazzo specification (JSON)
- `docs/reference/apis/api-workflows.json` - TMI workflow knowledge base (source)
- `docs/reference/apis/arazzo-generation.md` - Complete documentation

**Tools**:
- Redocly CLI - Scaffold generation from OpenAPI
- Python enhancement script - Enrichment with TMI workflow patterns
- Spectral CLI - Arazzo validation with custom TMI rules

**Workflow Coverage**: OAuth PKCE, threat model CRUD, diagram collaboration, threat management, document management, metadata operations, webhooks, and addons.

### OAuth Callback Stub

- **OAuth Testing Harness**: `make start-oauth-stub` or `uv run scripts/oauth-client-callback-stub.py --port 8079` - Comprehensive OAuth test tool with PKCE support

  - **Location**: `scripts/oauth-client-callback-stub.py` (standalone Python script)
  - **Purpose**: Full-featured OAuth 2.0 testing harness supporting manual flows and fully automated end-to-end testing with PKCE (RFC 7636)
  - **Smart Defaults**: All parameters optional with intelligent defaults:
    - `idp`: Defaults to "test" provider
    - `scopes`: Defaults to "openid profile email"
    - `state`, `code_verifier`, `code_challenge`: Auto-generated if not provided
    - Caller-specified values always override defaults
  - **Logging**: Comprehensive structured logging to `/tmp/oauth-stub.log` with RFC3339 timestamps and dual console output
  - **Make Commands**:
    - `make start-oauth-stub` - Start OAuth stub on port 8079
    - `make oauth-stub-stop` - Stop OAuth stub gracefully
    - `make status` - Check if OAuth stub is running

  - **API Endpoints**:

    1. **`POST /oauth/init`** - Initialize OAuth flow with PKCE parameters
       - Generates state, code_verifier, code_challenge, and authorization URL
       - All parameters optional (smart defaults applied)
       - Returns ready-to-use authorization URL with all PKCE parameters
       - Example:
         ```bash
         curl -X POST http://localhost:8079/oauth/init \
           -H 'Content-Type: application/json' \
           -d '{"userid": "alice"}'
         # Response: {"state": "...", "code_challenge": "...", "authorization_url": "http://..."}
         ```

    2. **`POST /refresh`** - Refresh access token using refresh token
       - Exchanges refresh token for new access/refresh tokens
       - Supports optional userid and idp parameters
       - Example:
         ```bash
         curl -X POST http://localhost:8079/refresh \
           -H 'Content-Type: application/json' \
           -d '{"refresh_token": "uuid", "userid": "alice"}'
         # Response: {"success": true, "access_token": "...", "refresh_token": "...", ...}
         ```

    3. **`POST /flows/start`** - Start automated end-to-end OAuth flow
       - Initiates authorization, handles callback, exchanges tokens automatically
       - Returns flow_id for polling status
       - All parameters optional with smart defaults
       - Example:
         ```bash
         curl -X POST http://localhost:8079/flows/start \
           -H 'Content-Type: application/json' \
           -d '{"userid": "bob"}'
         # Response: {"flow_id": "uuid", "status": "...", "poll_url": "/flows/uuid"}
         ```

    4. **`GET /flows/{flow_id}`** - Poll OAuth flow status and retrieve tokens
       - Check flow completion status
       - Retrieve tokens when ready
       - Example:
         ```bash
         curl http://localhost:8079/flows/ae146dae-c67d-4fb4-8b97-469b37c9848e
         # Response: {"flow_id": "...", "status": "completed", "tokens": {...}, "tokens_ready": true}
         ```

    5. **`GET /`** - OAuth callback receiver (redirect endpoint)
       - Receives OAuth redirects from TMI server
       - Automatically exchanges authorization code for tokens
       - Updates flow records for e2e flows
       - Saves credentials to `$TMP/<user-id>.json`

    6. **`GET /latest`** - Get latest OAuth callback data
       - Returns most recent OAuth redirect details
       - Useful for manual testing and debugging

    7. **`GET /creds?userid=<userid>`** - Retrieve saved credentials for user
       - Reads credentials from persistent file storage
       - Example: `curl "http://localhost:8079/creds?userid=alice"`

  - **Usage Examples**:

    ```bash
    # Start OAuth stub
    make start-oauth-stub

    # Example 1: Manual flow with /oauth/init
    curl -X POST http://localhost:8079/oauth/init \
      -H 'Content-Type: application/json' \
      -d '{"userid": "alice"}' | jq -r '.authorization_url'
    # Open the returned URL in browser, tokens auto-exchanged on callback

    # Example 2: Fully automated end-to-end flow
    curl -X POST http://localhost:8079/flows/start \
      -H 'Content-Type: application/json' \
      -d '{"userid": "bob"}' | jq '.flow_id'
    # Poll for completion
    curl http://localhost:8079/flows/{flow_id} | jq '.tokens'

    # Example 3: Token refresh
    curl -X POST http://localhost:8079/refresh \
      -H 'Content-Type: application/json' \
      -d '{"refresh_token": "uuid", "userid": "alice"}' | jq '.'

    # Example 4: Retrieve saved credentials
    curl "http://localhost:8079/creds?userid=alice" | jq '.access_token'

    # Monitor detailed logs
    tail -f /tmp/oauth-stub.log

    # Stop OAuth stub
    make oauth-stub-stop
    ```

  - **Key Features**:
    - **PKCE Support**: Full RFC 7636 implementation with S256 challenge method
    - **Smart Defaults**: Minimal configuration required - all parameters optional
    - **E2E Automation**: Complete flow automation for CI/CD and integration tests
    - **Token Lifecycle**: Supports both initial authorization and token refresh
    - **Debugging**: Comprehensive logging of all PKCE parameters and flow states
    - **Persistence**: Credentials saved to temp files for cross-test retrieval
  - **Security**: Development-only tool, binds to localhost only

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

**NOTE: Integration tests are currently out of date. Do not run `make test-integration` unless explicitly requested by the user.**

#### Core Testing

- Unit tests: `make test-unit` (fast tests, no external dependencies)
  - Specific test: `make test-unit name=TestName`
  - Options: `make test-unit count1=true passfail=true`
- Integration tests: **OUT OF DATE - Do not run unless requested**
  - `make test-integration` (requires database, runs with automatic setup/cleanup)
  - Specific test: `make test-integration name=TestName`
  - Cleanup only: `make clean-everything`
- Coverage tests: `make test-coverage` (generates combined unit + integration coverage reports)

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

### Heroku Operations

- **Database Reset**: `make heroku-reset-db` - Drop and recreate Heroku database schema (DESTRUCTIVE)
  - Script location: `scripts/heroku-reset-database.sh`
  - Documentation: `docs/operator/heroku-database-reset.md`
  - **WARNING**: Deletes all data - requires manual "yes" confirmation
  - Use cases: Schema out of sync, migration errors, clean deployment testing
  - Performs three steps: Drop schema → Run migrations → Verify schema
  - Verifies critical columns (e.g., `issue_uri` in `threat_models`)
  - Post-reset: Users must re-authenticate via OAuth

- **Database Drop**: `make heroku-drop-db` - Drop Heroku database schema leaving it empty (DESTRUCTIVE)
  - Script location: `scripts/heroku-drop-database.sh`
  - **WARNING**: Deletes all data and leaves database in empty state - requires manual "yes" confirmation
  - Use cases: Manual schema control, testing migration process from scratch, preparing for custom schema
  - Performs one step: Drop schema only (no migrations)
  - Database left with empty `public` schema, ready for manual schema creation or migrations
  - To restore: Run `make heroku-reset-db` or restart Heroku app to trigger auto-migrations

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
- Session lifecycle: Active → Terminating → Terminated states
- Host-based control: Only session host can manage participants
- Inactivity timeout: Configurable (default 300s, minimum 15s)
- Deny list: Session-specific tracking of removed participants

### Database Integration

- PostgreSQL for persistent storage (configured via auth/ package)
- Redis for caching and session management
- Database migrations in auth/migrations/
- Development uses Docker containers
- Dual-mode storage: in-memory for tests, database-backed for dev/prod

### Cache Architecture

- Redis-backed caching with invalidation, warming, and metrics
- Cache service in api/cache_service.go provides consistent caching patterns
- Automatic cache invalidation on resource updates
- Cache metrics tracking (hits, misses, size monitoring)

## Development Environment

- Copy `.env.example` to `.env.dev` for local development
- Uses PostgreSQL and Redis Docker containers
- Development scripts handle container management automatically
- Server runs on port 8080 by default with configurable TLS support
- Logs: In development and test, logs are written to `logs/tmi.log` in the project directory

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

## Task Completion Requirements

When completing any task involving code changes, follow this checklist:

1. Run `make lint` and fix any linting issues
2. Run `make build-server` and fix any build issues
3. Run relevant tests (`make test-unit` and/or `make test-integration`) and fix any issues
4. Suggest a conventional commit message

**Conventional Commit Format**:
- Use the format: `<type>(<scope>): <description>`
- Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`, `ci`, `build`, `revert`
- Scope: Optional, indicates the area of change (e.g., `api`, `auth`, `websocket`, `docs`)
- Description: Brief summary in imperative mood (e.g., "add user deletion endpoint" not "added" or "adds")
- Examples:
  - `feat(api): add WebSocket heartbeat mechanism`
  - `fix(auth): correct JWT token expiration validation`
  - `docs(readme): update OAuth setup instructions`
  - `refactor(websocket): simplify hub message broadcasting`
  - `test(integration): add database connection pooling tests`
  - `chore(deps): update Gin framework to v1.11.0`

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
- Uses Gin web framework (not Echo) with oapi-codegen/gin-middleware for validation
- OpenAPI validation middleware clears security schemes (auth handled by JWT middleware)
- Generated types in api/api.go include Gin server handlers and embedded spec
- Config file: oapi-codegen-config.yml (configured for gin-middleware package)

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
```

**Automation Integration**:

```bash
# OAuth callback stub with login_hint
curl "http://localhost:8080/oauth2/authorize?idp=test&login_hint=alice&client_callback=http://localhost:8079/"
```

### Client Credentials Grant (Machine-to-Machine Authentication)

TMI supports OAuth 2.0 Client Credentials Grant (RFC 6749 Section 4.4) for machine-to-machine authentication, enabling webhooks, addons, and automation tools to access TMI APIs without user interaction.

**Overview**:
- **Purpose**: Provide service account authentication for webhooks, addons, CI/CD pipelines, and automation scripts
- **Pattern**: Similar to GitHub Personal Access Tokens (PAT) - secret only shown once at creation
- **Access Model**: Client credentials grant full API access equivalent to the user who created them
- **Identity**: Service account tokens use distinct identity format in logs for clear audit trails
- **Quota**: Default limit of 10 credentials per user (configurable via admin quota system)

**TMI Provider**:
- **Provider IDs**: Both "test" and "tmi" work as aliases in all builds
- **Dev/Test Mode**: Supports both Authorization Code flow (ephemeral users) and Client Credentials Grant
- **Production Mode**: Only supports Client Credentials Grant (Authorization Code flow disabled)
- **Configuration**: Set `TMI_BUILD_MODE=dev` or `TMI_BUILD_MODE=production` in environment

**API Endpoints**:

1. **Create Client Credential** - `POST /client-credentials`
   - Creates a new client credential (client_id + client_secret)
   - Client secret only returned once (cannot be retrieved later)
   - Optional expiration date
   - Requires JWT authentication
   - Example:
     ```bash
     curl -X POST http://localhost:8080/client-credentials \
       -H "Authorization: Bearer $JWT_TOKEN" \
       -H "Content-Type: application/json" \
       -d '{
         "name": "AWS Lambda Security Scanner",
         "description": "Automated security scanning webhook",
         "expires_at": "2026-12-31T23:59:59Z"
       }'
     # Response includes client_secret (ONLY TIME IT'S VISIBLE)
     ```

2. **List Client Credentials** - `GET /client-credentials`
   - Returns all credentials owned by authenticated user
   - Does NOT include client secrets
   - Shows last_used_at, is_active status
   - Example:
     ```bash
     curl http://localhost:8080/client-credentials \
       -H "Authorization: Bearer $JWT_TOKEN"
     ```

3. **Delete Client Credential** - `DELETE /client-credentials/{id}`
   - Permanently deletes a credential
   - Immediately invalidates all tokens issued with that credential
   - Example:
     ```bash
     curl -X DELETE http://localhost:8080/client-credentials/{uuid} \
       -H "Authorization: Bearer $JWT_TOKEN"
     ```

**Token Exchange** (OAuth 2.0 Client Credentials Grant):

```bash
# Exchange client credentials for access token
curl -X POST http://localhost:8080/oauth2/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=tmi_cc_..." \
  -d "client_secret=..."

# Response:
{
  "access_token": "eyJhbGc...",
  "token_type": "Bearer",
  "expires_in": 3600
}
# Note: No refresh_token per RFC 6749 Section 4.4.3
```

**Using Service Account Tokens**:

```bash
# Use access token to call TMI APIs
curl http://localhost:8080/threat-models \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Service account can perform same operations as the user who created it
curl -X POST http://localhost:8080/threat-models \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "New Threat Model", "description": "Created by automation"}'
```

**Security Characteristics**:
- **Client ID Format**: `tmi_cc_{base64url(16_random_bytes)}` - easily identifiable in logs
- **Client Secret**: 32 bytes (43 chars base64url) - cryptographically secure random
- **Secret Storage**: bcrypt hashed (cost 10) in database
- **Secret Visibility**: Plaintext secret only returned at creation time (GitHub PAT pattern)
- **Token Lifetime**: Access tokens expire (default 1 hour), no refresh tokens
- **Revocation**: Deleting credential immediately invalidates all issued tokens
- **Subject Claim**: JWT subject format `sa:{credential_id}:{owner_provider_user_id}`

**Service Account Identity**:
- **JWT Subject**: `sa:{credential_id}:{owner_provider_user_id}` (e.g., `sa:123e4567-e89b:alice@example.com`)
- **Display Name**: `[Service Account] {credential_name}` (e.g., `[Service Account] AWS Lambda Scanner`)
- **Log Format**: Clearly distinguishes service accounts from human users in audit logs
- **Context Variables**: Middleware sets `isServiceAccount=true` and `serviceAccountCredentialID`

**Use Case Example** (AWS Lambda Webhook):
```bash
# 1. User creates webhook subscription for repo events
curl -X POST http://localhost:8080/webhooks \
  -H "Authorization: Bearer $USER_JWT" \
  -d '{"events": ["repo.add"], "url": "https://lambda.amazonaws.com/scanner"}'

# 2. User creates client credential for Lambda
curl -X POST http://localhost:8080/client-credentials \
  -H "Authorization: Bearer $USER_JWT" \
  -d '{"name": "AWS Security Scanner"}' \
  > lambda-creds.json

# 3. Store client_id and client_secret in Lambda environment variables
export CLIENT_ID=$(jq -r '.client_id' lambda-creds.json)
export CLIENT_SECRET=$(jq -r '.client_secret' lambda-creds.json)

# 4. Lambda receives webhook, exchanges credentials for token
TOKEN=$(curl -X POST http://localhost:8080/oauth2/token \
  -d "grant_type=client_credentials" \
  -d "client_id=$CLIENT_ID" \
  -d "client_secret=$CLIENT_SECRET" \
  | jq -r '.access_token')

# 5. Lambda uses token to read threat model and create threats
curl http://localhost:8080/threat-models/{id} \
  -H "Authorization: Bearer $TOKEN"

curl -X POST http://localhost:8080/threat-models/{id}/threats \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name": "SQL Injection", "severity": "High"}'

# Logs show: "[Service Account] AWS Security Scanner" performed actions
```

**Quota Management**:
- Default: 10 credentials per user
- Configurable via existing quota system
- Checked before credential creation
- Only active credentials count toward quota

**Best Practices**:
1. Create separate credentials for each automation/integration
2. Use descriptive names for easy identification in logs
3. Set expiration dates for temporary automations
4. Rotate credentials periodically by creating new ones and deleting old
5. Delete credentials immediately if compromised
6. Monitor `last_used_at` to identify unused credentials

## Python Development Memories

- Run python scripts with uv. When creating python scripts, add uv toml to the script for automatic package management.
