# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains API documentation and Go implementation for a Collaborative Threat Modeling Interface (TMI). It's a server-based web application enabling collaborative threat modeling with real-time diagram editing via WebSockets, role-based access control, OAuth authentication with JWT, and a RESTful API with OpenAPI 3.0 specification.

## Key Files

- api-schema/tmi-openapi.json - OpenAPI specification
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
- Dev DB only: `make start-database` (starts PostgreSQL container)
- Dev Redis only: `make start-redis` (starts Redis container)
- Clean all: `make clean-everything` (comprehensive cleanup of processes, containers, and files)
- Health check: Use `curl http://localhost:8080/` (root endpoint) to verify server is running
- Observability: `make observability-start` (starts OpenTelemetry monitoring stack), `make obs-start` (alias)
- Stop observability: `make observability-stop` (stops monitoring services), `make obs-stop` (alias)
- Clean observability: `make observability-clean` (removes monitoring data), `make obs-clean` (alias)

### Container Management (Grype Integration)

- Build individual containers (faster for iterative development):
  - `make build-container-db` (PostgreSQL container only)
  - `make build-container-redis` (Redis container only)
  - `make build-container-tmi` (TMI server container only)
- Build all containers: `make build-containers` (builds db, redis, tmi serially)
- Check scanner: `make check-grype` (verify Grype vulnerability scanner is installed)
- Security scan: `make scan-containers` (scans containers for vulnerabilities using Grype)
- Security report: `make report-containers` (generates comprehensive security report)
- Container development: `make containers-dev` (builds and starts containers, no server)
- Full container workflow: `make containers-all` (builds containers and generates reports)

### SBOM Generation (Software Bill of Materials)

- **Go app**: `make generate-sbom` (cyclonedx-gomod)
- **Containers**: Auto-generated during `make build-containers` (Syft)
- **Output**: `security-reports/sbom/` (CycloneDX 1.6 JSON + XML)

### Container Base Images

TMI uses [Chainguard](https://chainguard.dev/) images: `cgr.dev/chainguard/static:latest` (server), `cgr.dev/chainguard/postgres:latest` (DB), Chainguard Redis. Built with `CGO_ENABLED=0` (~57MB total).

**Note**: Oracle support requires CGO and is excluded from container builds. Use `go build -tags oracle` locally.

### OpenAPI Schema Management

- **Validate**: `make validate-openapi` (jq + Vacuum with OWASP rules)
- **Output**: `api-schema/openapi-validation-report.json`
- **Public Endpoints**: 17 endpoints (OAuth, OIDC, SAML) marked with `x-public-endpoint` vendor extension - intentionally unauthenticated per RFCs

### CATS API Fuzzing

CATS performs security fuzzing of the TMI API with automatic OAuth authentication.

- **Run**: `make cats-fuzz` | **Analyze**: `make analyze-cats-results`
- **Custom user**: `make cats-fuzz-user USER=alice`
- **Output**: `test/outputs/cats/` (JSON reports + SQLite database)

**Known Issue**: CATS 13.5.0 `MassAssignmentFuzzer` crashes on nested arrays - workaround configured in `run-cats-fuzz.sh`.

**False Positive Handling**: Public endpoints (17) and cacheable endpoints (6) use vendor extensions (`x-public-endpoint`, `x-cacheable-endpoint`) to skip inapplicable fuzzers. OAuth 401/403 responses auto-filtered via `is_oauth_false_positive` flag.

### Arazzo Workflow Generation

Arazzo specification (OpenAPI Initiative) documents API workflow sequences and dependencies.

- **Generate**: `make generate-arazzo` | **Validate**: `make validate-arazzo`
- **Output**: `api-schema/tmi.arazzo.yaml` and `api-schema/tmi.arazzo.json`
- **Docs**: `api-schema/arazzo-generation.md`

### OAuth Callback Stub

OAuth 2.0 testing harness with PKCE (RFC 7636) support for manual and automated flows.

- **Start**: `make start-oauth-stub` | **Stop**: `make oauth-stub-stop`
- **Location**: `scripts/oauth-client-callback-stub.py`
- **Logs**: `/tmp/oauth-stub.log`

**Key Endpoints**:
| Endpoint | Purpose |
|----------|---------|
| `POST /oauth/init` | Initialize OAuth flow, returns authorization URL |
| `POST /flows/start` | Start automated e2e flow, returns flow_id |
| `GET /flows/{id}` | Poll flow status and retrieve tokens |
| `GET /creds?userid=X` | Retrieve saved credentials for user |
| `POST /refresh` | Refresh access token |

**Quick JWT Retrieval**:

```bash
make start-oauth-stub
curl -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid": "alice"}'
# Wait for flow completion, then:
curl "http://localhost:8079/creds?userid=alice" | jq '.access_token'
```

### WebSocket Test Harness

Standalone Go application for testing WebSocket collaborative features.

- **Build**: `make build-wstest` | **Run**: `make wstest` | **Clean**: `make wstest-clean`
- **Location**: `wstest/` directory

**Usage**:

```bash
./wstest --user alice --host --participants "bob,charlie"  # Host mode
./wstest --user bob                                        # Participant mode
```

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

- **Unit tests**: `make test-unit` (fast tests, no external dependencies)
  - Specific test: `make test-unit name=TestName`
  - Options: `make test-unit count1=true passfail=true`

- **Integration tests**:
  - PostgreSQL: `make test-integration` or `make test-integration-pg`
  - Oracle ADB: `make test-integration-oci` (requires `scripts/oci-env.sh`)

- **Security fuzzing**: `make cats-fuzz` (CATS security testing)
  - Analyze results: `make cats-analyze`
  - Custom user: `make cats-fuzz-user USER=alice`

- **WebSocket testing**: `make wstest` (multi-user collaboration tests)

- **Coverage**: `make test-coverage` (generates combined coverage reports)

**Philosophy**: Never disable/skip failing tests - investigate and fix root cause.

### Heroku Operations

- **Database Reset**: `make reset-db-heroku` - Drop and recreate Heroku database schema (DESTRUCTIVE)
  - Script location: `scripts/heroku-reset-database.sh`
  - Documentation: `docs/operator/heroku-database-reset.md`
  - **WARNING**: Deletes all data - requires manual "yes" confirmation
  - Use cases: Schema out of sync, migration errors, clean deployment testing
  - Performs three steps: Drop schema → Run migrations → Verify schema
  - Verifies critical columns (e.g., `issue_uri` in `threat_models`)
  - Post-reset: Users must re-authenticate via OAuth

- **Database Drop**: `make drop-db-heroku` - Drop Heroku database schema leaving it empty (DESTRUCTIVE)
  - Script location: `scripts/heroku-drop-database.sh`
  - **WARNING**: Deletes all data and leaves database in empty state - requires manual "yes" confirmation
  - Use cases: Manual schema control, testing migration process from scratch, preparing for custom schema
  - Performs one step: Drop schema only (no migrations)
  - Database left with empty `public` schema, ready for manual schema creation or migrations
  - To restore: Run `make reset-db-heroku` or restart Heroku app to trigger auto-migrations

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
- `cmd/` - Command-line executables (server, migrate, cats-seed)
- `internal/` - Internal packages (logging, dbschema)
- `docs/` - Legacy documentation (deprecated - see Documentation section below)
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

## Documentation

**IMPORTANT**: All project documentation is maintained in the GitHub Wiki. Do NOT update markdown files in the `docs/` directory - they are deprecated and may be removed.

- **Authoritative documentation**: GitHub Wiki (https://github.com/ericfitz/tmi/wiki)
- **Local `docs/` directory**: Deprecated, do not update

When documentation changes are needed, update the wiki instead of local markdown files.

## User Preferences

- After changing any file, run `make lint` and fix any issues caused by the change
- After changing the OpenAPI specification (`api-schema/tmi-openapi.json`):
  1. Run `make validate-openapi` and fix any validation issues
  2. Run `make generate-api` to regenerate the API code
  3. Run `make lint` and fix any linting issues
  4. Run `make build-server` and fix any build issues
  5. Run `make test-unit` and fix any test failures
- After changing any Go file (`.go`), run `make build-server` and `make test-unit` and fix any issues
- Do not need to run `make build-server` or `make test-unit` if no Go files were modified
- Do not disable or skip failing tests, either diagnose to root cause and fix either the test issue or code issue, or ask the user what to do
- Always use make targets for testing - never run `go test` commands directly
- For API functionality, run `make test-integration` to ensure full integration works

## Task Completion Requirements

When completing any task involving code changes, follow this checklist:

1. Run `make lint` and fix any linting issues (required for ALL file changes)
2. If OpenAPI spec was modified:
   - Run `make validate-openapi` and fix any issues
   - Run `make generate-api` to regenerate API code
3. If any Go files were modified (including regenerated `api/api.go`):
   - Run `make build-server` and fix any build issues
   - Run `make test-unit` and fix any test failures
4. Suggest a conventional commit message
5. If the task is associated with a GitHub issue, the task is NOT complete until:
   - The commit that resolves the issue references the issue (e.g., `Fixes #123` or `Closes #123` in the commit message body)
   - The issue is closed as "done"

**Note**: Build and test steps are only required when Go files are modified. For non-Go changes (documentation, scripts, configuration), only linting is required.

## Branching Strategy

Each release is developed on a `release/<semver>` branch (e.g., `release/1.2.0`) created from `main`. Individual features for that release are developed on feature branches created from the release branch (e.g., `feature/1.2/foo`). When a feature is complete, it is merged back into the release branch. When all features are complete and tested, the release branch is merged into `main`.

```
main
 └── release/1.2.0              ← created from main
      ├── feature/1.2/foo       ← branched from release/1.2.0, merged back when done
      ├── feature/1.2/bar
      └── feature/1.2/baz
```

## Git Commit Guidelines

**ALWAYS use conventional commits**

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

## Logging Requirements

**CRITICAL: Never use the standard `log` package. Always use structured logging.**

- **ALWAYS** use `github.com/ericfitz/tmi/internal/slogging` for all logging operations
- **NEVER** use the print-based logging in any Go code
- **NEVER** import or use the standard `log` package (`"log"`) in any Go code
- Use `slogging.Get()` for global logging or `slogging.Get().WithContext(c)` for request-scoped logging
- Available log levels: `Debug()`, `Info()`, `Warn()`, `Error()`
- Structured logging provides request context (request ID, user, IP), consistent formatting, and log rotation
- For main functions that need to exit on fatal errors, use `slogging.Get().Error()` followed by `os.Exit(1)` instead of `log.Fatalf()`

### OpenAPI Integration

- API code generated from api-schema/tmi-openapi.json using oapi-codegen v2
- Uses Gin web framework (not Echo) with oapi-codegen/gin-middleware for validation
- OpenAPI validation middleware clears security schemes (auth handled by JWT middleware)
- Generated types in api/api.go include Gin server handlers and embedded spec
- Config file: oapi-codegen-config.yml (configured for gin-middleware package)

## Clean Architecture - Request Flow

**Current Architecture (Post-Cleanup)**:

The system uses a single-router architecture with OpenAPI-driven routing:

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

- Always use a normal oauth login flow with the "tmi" provider when performing any development or testing task that requires authentication
- The oauth-client-callback-stub can receive callbacks from the TMI oauth provider with the token, and you can retrieve the token from the oauth-client-callback-stub with a REST api call.
  - start stub: make start-oauth-stub
  - stop stub: make oauth-stub-stop
  - get JWT:
    - start the stub
    - perform a normal authorization request, using http://localhost:8079 as the callback url and specifying a user name as a login_hint
    - retrieve the JWT from http://localhost:8079/creds?userid=<username-hint>

### TMI OAuth Provider login_hints

The TMI OAuth provider supports **login_hints** for automation-friendly testing with predictable user identities:

- **Parameter**: `login_hint` - Query parameter for `/oauth2/authorize?idp=tmi`
- **Purpose**: Generate predictable test users instead of random usernames
- **Format**: 3-20 characters, alphanumeric + hyphens, case-insensitive
- **Validation**: Pattern: `^[a-zA-Z0-9-]{3,20}$`
- **Scope**: TMI provider only, not available in production builds

**Examples**:

```bash
# Create user 'alice@tmi.local' with name 'Alice (TMI User)'
curl "http://localhost:8080/oauth2/authorize?idp=tmi&login_hint=alice"

# Create user 'qa-automation@tmi.local' with name 'Qa Automation (TMI User)'
curl "http://localhost:8080/oauth2/authorize?idp=tmi&login_hint=qa-automation"

# Without login_hint - generates random user like 'testuser-12345678@tmi.local'
curl "http://localhost:8080/oauth2/authorize?idp=tmi"
```

**Automation Integration**:

```bash
# OAuth callback stub with login_hint
curl "http://localhost:8080/oauth2/authorize?idp=tmi&login_hint=alice&client_callback=http://localhost:8079/"
```

### Client Credentials Grant (Machine-to-Machine Authentication)

OAuth 2.0 Client Credentials Grant (RFC 6749 Section 4.4) for webhooks, addons, and automation.

**Pattern**: Like GitHub PATs - secret only shown once at creation, full API access as creating user.

**API Endpoints**:
| Endpoint | Purpose |
|----------|---------|
| `POST /me/client_credentials` | Create credential (returns secret once) |
| `GET /me/client_credentials` | List credentials (no secrets) |
| `DELETE /me/client_credentials/{id}` | Delete and revoke credential |

**Token Exchange**:

```bash
curl -X POST http://localhost:8080/oauth2/token \
  -d "grant_type=client_credentials" -d "client_id=tmi_cc_..." -d "client_secret=..."
# Returns: {"access_token": "...", "token_type": "Bearer", "expires_in": 3600}
```

**Security**: Client ID format `tmi_cc_*`, bcrypt-hashed secrets, 1-hour token lifetime, JWT subject `sa:{id}:{owner}`.

## Python Development Memories

- Run python scripts with uv. When creating python scripts, add uv toml to the script for automatic package management.

## Staticcheck Configuration

TMI uses staticcheck for Go code quality analysis. The project has intentionally kept some staticcheck warnings:

- **Auto-Generated Code**: `api/api.go` contains many ST1005 warnings (capitalized error strings)
  - File is generated by oapi-codegen from OpenAPI specification
  - Manual edits would be overwritten on next OpenAPI regeneration
  - Not worth customizing oapi-codegen templates for style compliance
  - **Expected behavior**: These warnings are acceptable and should be ignored

- **Auth Handler Functions**: 4 unused functions in [auth/handlers.go](auth/handlers.go) - left for potential future refactoring:
  - `setUserHintContext` (line 559)
  - `exchangeCodeAndGetUser` (line 571)
  - `createOrGetUser` (line 648)
  - `generateAndReturnTokens` (line 716)

- **Running Staticcheck**:
  - `staticcheck ./...` - Shows all issues (including expected ones)
  - `staticcheck ./... | grep -v "api/api.go"` - Filter out auto-generated code warnings
  - **Expected count**: 344 total issues (338 in api/api.go + 6 intentionally unused functions)
  - **Clean hand-written code**: 19 unused code items were removed for 1.0 release

## Agent Instructions

### Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**

- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
