# Active Scripts Directory

This directory contains scripts that are actively used by the refactored build system and development workflow.

## Core Build System Scripts

### Configuration Management

- **`load-config.mk`** - Makefile include for loading YAML configurations into Make variables

### Version Management

- **`update-version.sh`** - Automatic version management for TMI based on conventional commit types (feat: increments MINOR, others increment PATCH)

## Development and Analysis Tools

### Code Analysis

- **`analyze_endpoints.py`** - Analyzes all TMI API endpoints to determine authentication requirements, access patterns, and endpoint characteristics
- **`validate_openapi.py`** - Comprehensive OpenAPI specification validation including JSON syntax, schema validation, and CATS compatibility
- **`validate_asyncapi.py`** - AsyncAPI specification validation using Pydantic and JSON Schema against AsyncAPI 3.0.0

### OpenAPI Specification Tools

- **`add-400-responses.sh`** - Adds missing 400 Bad Request responses to OpenAPI specification based on CATS fuzzer analysis
- **`add-unexpected-responses.sh`** - Adds missing response codes identified by CATS "Unexpected response code" analysis
- **`add-public-endpoint-markers.sh`** - Adds vendor extensions (x-public-endpoint, x-authentication-required) to public endpoints in OpenAPI spec
- **`add-retry-after-headers.py`** - Adds Retry-After headers to all 429 responses for RFC 6585 compliance
- **`clean-redundant-ref-headers.py`** - Removes redundant headers from responses that use $ref to component references
- **`fix-openapi-issues.py`** - Fixes RateMyOpenAPI issues in TMI OpenAPI specification
- **`fix-openapi-spectral-issues.py`** - Fixes Spectral $ref sibling issues where rate limit headers were added alongside $ref properties
- **`add-addon-endpoints.py`** - Adds addon-related endpoints, schemas, and security requirements to OpenAPI specification

### Arazzo Workflow Tools

- **`generate_arazzo_scaffold.sh`** - Generates base Arazzo scaffold from OpenAPI using Redocly CLI
- **`enhance_arazzo_with_workflows.py`** - Enhances Arazzo specifications with TMI workflow knowledge from api-workflows.json
- **`validate_arazzo.py`** - Validates Arazzo specification using Spectral

### Development Utilities

- **`oauth-client-callback-stub.py`** - Comprehensive OAuth 2.0 testing harness with PKCE support (RFC 7636). Use `make start-oauth-stub` to run.
  - **Features**: OAuth callback capture, credential persistence, automated end-to-end flows, token refresh
  - **Endpoints**: `POST /oauth/init` (initialize PKCE flow), `POST /flows/start` (automated e2e), `GET /flows/{id}` (poll status), `GET /` (OAuth callback), `GET /latest` (latest credentials), `GET /creds?userid=<id>` (user-specific credentials), `POST /refresh` (token refresh)
  - **Persistence**: Saves credentials to `$TMP/<user-id>.json` files for later retrieval
  - **Logging**: Comprehensive structured logging to `/tmp/oauth-stub.log`

- **`json-query.sh`** - JSON query utility using jq with configurable depth

- **`list-endpoints.sh`** - Lists API endpoints from the OpenAPI specification

### Testing Tools

- **`generate-test-matrix.py`** - Generates test matrix from Newman API test results showing endpoint coverage
- **`add_postman_tests.py`** - Adds appropriate tests to Postman request items based on method and expected responses

### Code Fix Scripts

- **`fix-validate-auth-calls.sh`** - Fixes all ValidateAuthenticatedUser calls to handle 4 return values
- **`fix-validate-auth-signature.py`** - Fixes ValidateAuthenticatedUser calls to handle the new 4-value return signature (was 3-value)
- **`fix-test-auth-setup.py`** - Adds c.Set("userID", ...) after every c.Set("userEmail", ...) in test files
- **`fix-addon-handler-types.py`** - Fixes type mismatches in addon handler files to work with OpenAPI-generated types

## CATS Fuzzing Tools

- **`run-cats-fuzz.sh`** - CATS fuzzing script with OAuth integration; automates authentication and runs CATS fuzzing against TMI API
- **`cats-create-test-data.sh`** - Creates prerequisite test data for CATS fuzzing to eliminate false positives (threat models, threats, diagrams, etc.)
- **`cats-prepare-database.sh`** - Prepares the database for CATS fuzzing by granting admin privileges to the test user
- **`cats-set-max-quotas.sh`** - Sets maximum quotas and rate limits for CATS test user to prevent rate-limit errors during fuzzing
- **`parse-cats-results.py`** - Parses CATS fuzzer test result JSON files into a normalized SQLite database
- **`query-cats-results.sh`** - Provides quick SQL queries against the parsed CATS results database

## Container Management

### Build and Deployment

- **`build-containers.sh`** - Container build script with Docker Scout security scanning and automated vulnerability patching
- **`build-promtail-container.sh`** - Builds Promtail container with Chainguard static base for logging infrastructure
- **`make-containers-dev-local.sh`** - Local development container setup with security scanning

### Heroku Operations

- **`heroku-reset-database.sh`** - Drops and recreates the Heroku PostgreSQL database schema from scratch (DESTRUCTIVE)
- **`heroku-drop-database.sh`** - Drops the Heroku PostgreSQL database schema, leaving it empty without running migrations (DESTRUCTIVE)
- **`configure-heroku-env.sh`** - Configures Heroku environment variables for TMI server
- **`setup-heroku-env.py`** - Automated configuration of Heroku environment variables for TMI server and client applications

## Directory Structure

```
scripts/
├── config/                    # YAML configuration files for Makefile targets
├── unused/                    # Deprecated scripts moved here for reference
├── *.py                       # Python utilities and analysis tools
├── *.sh                       # Shell scripts for container management
└── *.mk                       # Makefile includes and configuration loading
```

## Usage Patterns

### For Build System

Most build operations now use the refactored Makefile:

```bash
make test-unit                 # Instead of old shell scripts
make test-integration         # Replaces start-integration-tests.sh
make start-dev                # Replaces start-dev.sh
```

### For Development Analysis

```bash
uv run scripts/analyze_endpoints.py
uv run scripts/validate_openapi.py docs/reference/apis/tmi-openapi.json
uv run scripts/validate_asyncapi.py docs/reference/apis/tmi-asyncapi.yaml
```

### OAuth Callback Stub for Development

```bash
make start-oauth-stub          # Start OAuth callback handler
make oauth-stub-status         # Check if running
make oauth-stub-stop           # Stop gracefully
```

### For Container Management

```bash
./scripts/make-containers-dev-local.sh    # Local development
./scripts/build-containers.sh             # Production build with security scanning
```

### For CATS Fuzzing

```bash
make cats-fuzz                 # Run CATS fuzzing with OAuth
make cats-analyze              # Parse and query results
```

## Dependencies

- **Python scripts**: Use uv with inline TOML configuration for package management
- **Shell scripts**: Standard bash with Docker dependencies
- **Makefile includes**: Require YAML parsing via Python

See individual script headers for specific usage instructions and dependencies.
