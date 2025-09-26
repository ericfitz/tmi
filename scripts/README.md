# Active Scripts Directory

This directory contains scripts that are actively used by the refactored build system and development workflow.

## Core Build System Scripts

### Configuration Management

- **`yaml-to-make.py`** - Converts YAML config files to Makefile variables (used by refactored Makefile)
- **`load-config.mk`** - Makefile include for loading YAML configurations

## Development and Analysis Tools

### Code Analysis

- **`analyze_endpoints.py`** - API endpoint analysis and documentation
- **`cleanup_dead_code.py`** - Automated dead code detection and cleanup
- **`validate_openapi.py`** - OpenAPI specification validation
- **`validate_asyncapi.py`** - AsyncAPI specification validation

### Development Utilities

- **`patch-json.py`** - Precise JSON modification utility for OpenAPI specs
- **`oauth-client-callback-stub.py`** - Universal OAuth callback handler supporting both Authorization Code and Implicit flows for development testing. Use `make start-oauth-stub` to run.
  - **Features**: OAuth callback capture, credential persistence, user-specific credential retrieval
  - **Endpoints**: `GET /` (OAuth callback), `GET /latest` (latest credentials), `GET /creds?userid=<id>` (user-specific credentials)
  - **Persistence**: Saves credentials to `$TMP/<user-id>.json` files for later retrieval
  - **Cleanup**: Automatically cleans up temp files on startup

## Container Management

### Deployment Containers

- **`make-containers-dev-local.sh`** - Local development container setup
- **`make-containers-dev-ecs.sh`** - ECS deployment container management

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
python3 scripts/analyze_endpoints.py
python3 scripts/validate_openapi.py shared/api-specs/tmi-openapi.json
python3 scripts/patch-json.py -s shared/api-specs/tmi-openapi.json -p "$.components.schemas"
```

# OAuth callback stub for development

make start-oauth-stub # Start OAuth callback handler
make oauth-stub-status # Check if running
make oauth-stub-stop # Stop gracefully

````

### For Container Management

```bash
./scripts/make-containers-dev-local.sh    # Local development
./scripts/make-containers-dev-ecs.sh      # ECS deployment
````

## Dependencies

- **Python scripts**: Use uv with TOML configuration for package management
- **Shell scripts**: Standard bash with Docker dependencies
- **Makefile includes**: Require YAML parsing via Python

See individual script headers for specific usage instructions and dependencies.
