# TMI - Threat Modeling Improved

A collaborative threat modeling server built with Go.

Try it yourself at [https://www.tmi.dev](https://www.tmi.dev)\
API server online at [https://api.tmi.dev](https://api.tmi.dev)\
API clients available at [https://github.com/ericfitz/tmi-clients](https://github.com/ericfitz/tmi-clients)

## Overview

TMI (Threat Modeling Improved) is a collaborative platform for managing an organization's security review process, including threat modeling. Our mission is to reduce the toil of security reviewing, and to make threat modeling accessible, efficient, and integrated into the software development lifecycle.

The platform features security review organization and state management, interactive data flow diagram creation with real-time collaboration, and comprehensive threat documentation capabilities. Built with modern web technologies, TMI helps teams manage the security review process and identify, analyze, and mitigate security threats through collaborative modeling.

This project is the TMI server back-end, a Go service that implements the TMI REST API.

The associated Angular/Typescript front-end web application is called [TMI-UX](https://github.com/ericfitz/tmi-ux).

## Quick Start

For detailed setup instructions, see [Development Setup Guide](https://github.com/ericfitz/tmi/wiki/Getting-Started-with-Development).

### Prerequisites

- Go 1.26+
- Docker Desktop (for database & Redis containers)
- Make (for build automation)

### Installation

```bash
git clone https://github.com/ericfitz/tmi.git
cd tmi
make dev-up CLUSTER=docker-desktop
```

The complete development environment (the server, database and Redis, deployed into a Kubernetes cluster) will start automatically, with the server reachable on port 8080. `CLUSTER` is required: `docker-desktop` (local) or `k3s` (remote).

## Project Structure

- `api/` - API types and handlers
- `cmd/server/` - Server entry point and configuration
- `api-schema/tmi-openapi.json` - OpenAPI specification

## Architecture

### Data Storage Pattern

Persistence goes through GORM. Entity models live in `api/models/` and are the single source of truth for the schema: tables are created and migrated from the model struct tags by `AutoMigrate()`. The database is PostgreSQL or Oracle Autonomous Database (a supported production target); the stores are accessed through the interfaces declared in `api/store.go`, with GORM-backed implementations in `api/*_store_gorm.go`. Redis is used for caching and sessions. Unit tests of the GORM stores run against SQLite.

## Documentation

Comprehensive documentation is organized by audience:

### 📖 For Developers

- **[Setup Guide](https://github.com/ericfitz/tmi/wiki/Getting-Started-with-Development)** - Local development environment
- **[Testing Guide](https://github.com/ericfitz/tmi/wiki/Testing)** - Comprehensive testing documentation
- **[Client Integration](https://github.com/ericfitz/tmi/wiki/API-Integration)** - API and WebSocket integration

### 🚀 For Operations Teams

- **[Deployment Guide](https://github.com/ericfitz/tmi/wiki/Deploying-TMI-Server)** - Production deployment
- **[Database Operations](https://github.com/ericfitz/tmi/wiki/Database-Operations)** - Database management
- **[Container Security](https://github.com/ericfitz/tmi/wiki/Security-Operations)** - Secure containerization

### 📋 Complete Documentation Index

See **[TMI Wiki](https://github.com/ericfitz/tmi/wiki)** for the complete documentation catalog organized by role and topic.

## Development Commands

```bash
make dev-up CLUSTER=docker-desktop      # Start complete dev environment
make dev-status CLUSTER=docker-desktop  # Show dev environment status
make dev-down CLUSTER=docker-desktop    # Tear down dev environment (keep db data)
make build-server       # Build production binary
make test-unit                # Run unit tests
make test-integration         # Run integration tests (needs a running stack)
make cats-fuzz               # Run security fuzzing
make lint               # Run code linting
```

## Configuration

The server reads a YAML configuration file passed with `--config`, and every setting can be overridden with a `TMI_`-prefixed environment variable (for example `TMI_SERVER_PORT`, `TMI_JWT_EXPIRATION_SECONDS`, `TMI_REDIS_URL`). The YAML file holds the bootstrap keys needed to start the server; operational settings are stored in the database settings service and can be overridden with the environment or the YAML file.

```bash
./bin/tmiserver --config=/path/to/config.yml
./bin/tmiserver --generate-config    # write an annotated example configuration
```

See [config-example.yml](config-example.yml) for the bootstrap keys and their defaults, and the [TMI Wiki](https://github.com/ericfitz/tmi/wiki) for the full configuration reference. The environment variable for each key is declared with the key in `internal/config/config.go`.

### WebSocket URLs

The collaborative editing WebSocket for a diagram is `GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/ws`, and server-wide notifications are on `GET /ws/notifications`. Both are specified in the AsyncAPI document (`api-schema/tmi-asyncapi.yml`) and authenticate with a short-lived ticket passed as the `ticket` query parameter.

When the client reaches the server over TLS, use `wss://` instead of `ws://`. Example: `wss://your-server.com/threat_models/{threat_model_id}/diagrams/{diagram_id}/ws?ticket=...`

Use `GET /api/server-info` to discover the correct WebSocket base URL, scheme and TLS settings automatically.

## License

See [license.txt](license.txt)
