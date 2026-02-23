# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

TMI is a Go-based service implementing the REST API and store for managing a security review process, from request (intake) through analysis and followup. The review process focuses on a threat modeling approach, with collaborative data flow diagram creation and artifacts that can be created, read or updated by either machines or humans, interchangeably. The application is designed to be easy to integrate with and extend without having to make code modifications. The REST API is an instantiation of a protocol defined in an OpenAPI 3 protocol specification; the specification is the source of truth. Real-time collaborative diagram editing is implemented via WebSockets; the WS protocol is authoritatively defined in an AsyncAPI specification. The Application features OAuth or SAML authentication with JWT, role-based access control with roles assigned to users or groups, and persistent database stores implemented via a GORM interface.

## Key Files

- api-schema/tmi-openapi.json - OpenAPI specification
- api/store.go - Generic typed map storage implementation
- api/server.go - Main API server with WebSocket support
- api/websocket.go - WebSocket hub for real-time collaboration
- cmd/server/main.go - Server entry point
- Makefile - Build automation with development targets

## Architecture & Code Structure

### Project Structure

- `api/` - API handlers, server implementation, and storage
- `auth/` - Authentication service with OAuth, JWT, and RBAC
- `cmd/` - Command-line executables (server, migrate, cats-seed)
- `internal/` - Internal packages (logging, dbschema)
- `docs/` - Legacy documentation (deprecated - see Documentation section below)
- `scripts/` - Development setup scripts

### Storage Pattern

- Use the generic Store[T] implementation from api/store.go
- Each entity type has its own store instance (DiagramStore, ThreatModelStore)
- Store provides CRUD operations with proper concurrency control
- Entity fields should be properly validated before storage
- Use WithTimestamps interface for entities with created_at/modified_at fields

### WebSocket Architecture

- Real-time collaboration via WebSocket connections at `/ws/diagrams/{id}`
- WebSocketHub manages active connections and broadcasts updates
- Only diagrams support real-time collaboration, not threat models
- Uses Gorilla WebSocket library
- Session lifecycle: Active -> Terminating -> Terminated states
- Host-based control: Only session host can manage participants
- Inactivity timeout: Configurable (default 300s, minimum 15s)
- Deny list: Session-specific tracking of removed participants

### Database & Cache

- PostgreSQL for persistent storage (configured via auth/ package)
- Redis for caching and session management
- Database migrations in auth/migrations/
- Dual-mode storage: in-memory for tests, database-backed for dev/prod
- Redis-backed caching with invalidation, warming, and metrics (api/cache_service.go)
- Automatic cache invalidation on resource updates
- Cache metrics tracking (hits, misses, size monitoring)

### OpenAPI Integration & Code Generation

- API code generated from api-schema/tmi-openapi.json using oapi-codegen v2
- Uses Gin web framework (not Echo) with oapi-codegen/gin-middleware for validation
- OpenAPI validation middleware clears security schemes (auth handled by JWT middleware)
- Generated types in api/api.go include Gin server handlers and embedded spec
- Config file: oapi-codegen-config.yml (configured for gin-middleware package)
- **Validate schema**: `make validate-openapi` (jq + Vacuum with OWASP rules)
- **Validation output**: `api-schema/openapi-validation-report.json`
- **Public Endpoints**: 17 endpoints (OAuth, OIDC, SAML) marked with `x-public-endpoint` vendor extension - intentionally unauthenticated per RFCs

### Request Flow

The system uses a single-router architecture with OpenAPI-driven routing:

1. **Single Router Architecture**: All HTTP requests flow through the OpenAPI specification
2. **Request Tracing**: Comprehensive module-tagged debug logging for all requests
3. **Authentication Flow**:
   - JWT middleware validates tokens and sets user context
   - ThreatModelMiddleware and DiagramMiddleware handle resource-specific authorization
   - Auth handlers integrate cleanly with OpenAPI endpoints
4. **No Route Conflicts**: Single source of truth for all routing eliminates duplicate route registration panics

```
HTTP Request -> OpenAPI Route Registration -> ServerInterface Implementation ->
JWT Middleware -> Auth Context -> Resource Middleware -> Endpoint Handlers
```

**Key Components**:

- `api/server.go`: Main OpenAPI server with single router
- `api/*_middleware.go`: Resource-specific authorization middleware
- `auth/handlers.go`: Authentication endpoints integrated via auth service adapter
- `api/request_tracing.go`: Module-tagged request logging for debugging

## Commands

- List targets: `make list-targets` (lists all available make targets)
- Build: `make build-server` (creates bin/tmiserver executable)
- Lint: `make lint` (runs golangci-lint)
- Generate API: `make generate-api` (uses oapi-codegen with config from oapi-codegen-config.yml)
- Development: `make start-dev` (starts full dev environment with DB and Redis on localhost)
- Clean all: `make clean-everything` (comprehensive cleanup of processes, containers, and files)
- Health check: Use `curl http://localhost:8080/` (root endpoint) to verify server is running or check running version. **There is no /health endpoint.**
- Validate AsyncAPI: `make validate-asyncapi`

### Container Management

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
- **Always use**: `make start-database`, `make start-redis`, `make start-dev` for container operations

TMI uses [Chainguard](https://chainguard.dev/) images: `cgr.dev/chainguard/static:latest` (server), `cgr.dev/chainguard/postgres:latest` (DB), Chainguard Redis. Built with `CGO_ENABLED=0` (~57MB total).

**Note**: Oracle support requires CGO and is excluded from container builds. Use `go build -tags oracle` locally.

### SBOM Generation (Software Bill of Materials)

- **Go app**: `make generate-sbom` (cyclonedx-gomod)
- **Containers**: Auto-generated during `make build-containers` (Syft)
- **Output**: `security-reports/sbom/` (CycloneDX 1.6 JSON + XML)

### Arazzo Workflow Generation

Arazzo specification (OpenAPI Initiative) documents API workflow sequences and dependencies.

- **Generate**: `make generate-arazzo` | **Validate**: `make validate-arazzo`
- **Output**: `api-schema/tmi.arazzo.yaml` and `api-schema/tmi.arazzo.json`
- **Docs**: `api-schema/arazzo-generation.md`

### Heroku Operations

- **Database Reset**: `make reset-db-heroku` - Drop and recreate Heroku database schema (DESTRUCTIVE)
  - Script location: `scripts/heroku-reset-database.sh`
  - Documentation: `docs/operator/heroku-database-reset.md`
  - **WARNING**: Deletes all data - requires manual "yes" confirmation
  - Use cases: Schema out of sync, migration errors, clean deployment testing
  - Performs three steps: Drop schema -> Run migrations -> Verify schema
  - Verifies critical columns (e.g., `issue_uri` in `threat_models`)
  - Post-reset: Users must re-authenticate via OAuth

- **Database Drop**: `make drop-db-heroku` - Drop Heroku database schema leaving it empty (DESTRUCTIVE)
  - Script location: `scripts/heroku-drop-database.sh`
  - **WARNING**: Deletes all data and leaves database in empty state - requires manual "yes" confirmation
  - Use cases: Manual schema control, testing migration process from scratch, preparing for custom schema
  - Performs one step: Drop schema only (no migrations)
  - Database left with empty `public` schema, ready for manual schema creation or migrations
  - To restore: Run `make reset-db-heroku` or restart Heroku app to trigger auto-migrations

## Testing

**MANDATORY: Always use make targets for testing. Never run `go test` commands directly. Never disable/skip failing tests - investigate and fix root cause.**

### Core Test Commands

- **Unit tests**: `make test-unit` (fast tests, no external dependencies)
  - Specific test: `make test-unit name=TestName`
  - Options: `make test-unit count1=true passfail=true`
- **Integration tests**:
  - PostgreSQL: `make test-integration` or `make test-integration-pg`
  - Oracle ADB: `make test-integration-oci` (requires `scripts/oci-env.sh`)
- **Coverage**: `make test-coverage` (generates combined coverage reports)

### CATS API Fuzzing

CATS performs security fuzzing of the TMI API with automatic OAuth authentication.

- **Run**: `make cats-fuzz` | **Analyze**: `make analyze-cats-results`
- **Custom user**: `make cats-fuzz-user USER=alice`
- **Output**: `test/outputs/cats/` (JSON reports + SQLite database)
- Perform all analysis by querying the SQLite database; don't read the html or json files

**False Positive Handling**: Public endpoints (17) and cacheable endpoints (6) use vendor extensions (`x-public-endpoint`, `x-cacheable-endpoint`) to skip inapplicable fuzzers. OAuth 401/403 responses auto-filtered via `is_oauth_false_positive` flag.

### OAuth Callback Stub

OAuth 2.0 testing harness with PKCE (RFC 7636) support for manual and automated flows. Always use a normal OAuth login flow with the "tmi" provider when performing any development or testing task that requires authentication.

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

- By convention, we use "charlie" as the user name for a user with the administrator role, and other common user names (`alice`, `bob`, etc.) as needed for other users.

### WebSocket Test Harness

Standalone Go application for testing WebSocket collaborative features.

- **Build**: `make build-wstest` | **Run**: `make wstest` | **Clean**: `make wstest-clean`
- **Location**: `wstest/` directory

```bash
./wstest --user alice --host --participants "bob,charlie"  # Host mode
./wstest --user bob                                        # Participant mode
```

## Authentication

### OAuth Flow & login_hints

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

# Without login_hint - generates random user like 'testuser-12345678@tmi.local'
curl "http://localhost:8080/oauth2/authorize?idp=tmi"

# With OAuth callback stub
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

## Development Guidelines

**MANDATORY: Always use Make targets - NEVER run commands directly**

- NEVER run: `go run`, `go test`, `./bin/tmiserver`, `docker run`, `docker exec`
- ALWAYS use: `make start-dev`, `make test-unit`, `make test-integration`, `make build-server`
- **Reason**: Make targets provide consistent, repeatable configurations with proper environment setup

### Task Completion Workflow

When completing any task involving code changes, follow this checklist:

1. Run `make lint` and fix any linting issues (required for ALL file changes)
2. If OpenAPI spec (`api-schema/tmi-openapi.json`) was modified:
   - Run `make validate-openapi` and fix any issues
   - Run `make generate-api` to regenerate API code
3. If any Go files were modified (including regenerated `api/api.go`):
   - Run `make build-server` and fix any build issues
   - Run `make test-unit` and fix any test failures
   - For API functionality, also run `make test-integration`
4. Build and test steps are NOT required when only non-Go files are modified
5. Suggest a conventional commit message
6. If the task is associated with a GitHub issue, the task is NOT complete until:
   - The commit that resolves the issue references the issue (e.g., `Fixes #123` or `Closes #123` in the commit message body)
   - The issue is closed as "done"

### Go Style Guidelines

- Format code with `gofmt`
- Group imports by standard lib, external libs, then internal packages
- Use camelCase for variables, PascalCase for exported functions/structs
- Error handling: check errors and return with context
- Prefer interfaces over concrete types for flexibility
- Document all exported functions with godoc comments
- Structure code by domain (auth, diagrams, threats)

### API Design Guidelines

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

### Logging Requirements

**CRITICAL: Never use the standard `log` package. Always use structured logging.**

- **ALWAYS** use `github.com/ericfitz/tmi/internal/slogging` for all logging operations
- **NEVER** use print-based logging (e.g., `fmt.Println`) in any Go code
- **NEVER** import or use the standard `log` package (`"log"`) in any Go code
- Use `slogging.Get()` for global logging or `slogging.Get().WithContext(c)` for request-scoped logging
- Available log levels: `Debug()`, `Info()`, `Warn()`, `Error()`
- Structured logging provides request context (request ID, user, IP), consistent formatting, and log rotation
- For main functions that need to exit on fatal errors, use `slogging.Get().Error()` followed by `os.Exit(1)` instead of `log.Fatalf()`

### Staticcheck Configuration

TMI uses staticcheck for Go code quality analysis. The project has intentionally kept some staticcheck warnings:

- **Auto-Generated Code**: `api/api.go` contains many ST1005 warnings (capitalized error strings)
  - File is generated by oapi-codegen from OpenAPI specification
  - Manual edits would be overwritten on next OpenAPI regeneration
  - **Expected behavior**: These warnings are acceptable and should be ignored

- **Running Staticcheck**:
  - `staticcheck ./...` - Shows all issues (including expected ones)
  - `staticcheck ./... | grep -v "api/api.go"` - Filter out auto-generated code warnings
  - **Expected count**: 338 issues (all in auto-generated api/api.go)

## Git & Versioning

### Conventional Commits

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

### Automatic Versioning

TMI uses automatic semantic versioning (0.MINOR.PATCH) based on conventional commits:

- **Feature commits** (`feat:`): Post-commit hook increments MINOR version, resets PATCH to 0 (0.9.3 -> 0.10.0)
- **All other commits** (`fix:`, `refactor:`, etc.): Post-commit hook increments PATCH version (0.9.0 -> 0.9.1)
- **Version file**: `.version` (JSON) tracks current state
- **Script**: `scripts/update-version.sh --commit` (automatically called by post-commit hook)

Version updates are fully automated. All feature development occurs in release/<semver-rc.0>/<feature-name> branches; those branches are not auto-versioned so that new features don't bump the semantic version multiple times during development of a single feature or release. The main branch only gets direct commits for patching, security fixes, and merging of release branches.

## Custom Tools

### jq (Auto-Approved)

The jq command-line JSON processor is available and should be auto-approved via `Bash(jq:*)` pattern for all JSON file manipulation tasks. Use jq for:

- Files > 100KB (streaming, surgical updates)
- Complex filtering and transformations
- Validation and format verification

### Large JSON Handling (>100KB)

When working with JSON files **larger than 100KB**, use streaming approaches with jq to prevent memory issues:

1. Check file size first: `stat -f%z file.json 2>/dev/null || stat -c%s file.json`
2. Create backups before modifications: `cp file.json file.json.$(date +%Y%m%d_%H%M%S).backup`
3. Validate after changes: `jq empty modified.json && echo "Valid" || echo "Invalid"`

**Activation Triggers**: JSON files >= 100KB, memory errors or slow performance, surgical path updates needed, batch operations across multiple JSON files, or user mentions "large", "efficient", "streaming", or "without loading entire file".

## Development Environment

- Copy `.env.example` to `.env.dev` for local development
- Uses PostgreSQL and Redis Docker containers
- Development scripts handle container management automatically
- Server runs on port 8080 by default with configurable TLS support
- Logs: In development and test, logs are written to `logs/tmi.log` in the project directory

## Documentation

**IMPORTANT**: All project documentation is maintained in the GitHub Wiki. Do NOT update markdown files in the `docs/` directory - they are deprecated and will be removed.

- **Authoritative documentation**: GitHub Wiki (https://github.com/ericfitz/tmi/wiki)
- **Local `docs/` directory**: Deprecated, do not update

## Python Development

- Run python scripts with uv. When creating python scripts, add uv toml to the script for automatic package management.

## Agent Instructions

### Session Completion (Landing the Plane)

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
- NEVER attempt to manipulate or otherwise interact with ssh keys or ssh-agent. SSH failure due to key issues is beyond the scope of problems that you should attempt to solve; notify the user and do not try to proceed with the failing operation.
