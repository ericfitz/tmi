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

- Go 1.25+
- Docker Desktop (for database & Redis containers)
- Make (for build automation)

### Installation

```bash
git clone https://github.com/ericfitz/tmi.git
cd tmi
make start-dev
```

The complete development environment (server + database + Redis) will start automatically on port 8080.

## Project Structure

- `api/` - API types and handlers
- `cmd/server/` - Server entry point and configuration
- `api-schema/tmi-openapi.json` - OpenAPI specification

## Architecture

### Data Storage Pattern

The project uses strongly-typed concurrent maps for in-memory storage:

```go
// Store provides thread-safe storage for a specific entity type
type Store[T any] struct {
    data  map[string]T
    mutex sync.RWMutex
}

// DiagramStore stores diagrams by UUID
var DiagramStore = NewStore[api.Diagram]()

// ThreatModelStore stores threat models by UUID
var ThreatModelStore = NewStore[api.ThreatModel]()
```

Benefits of this approach:

- Type safety with generics
- Concurrency protection with mutexes
- Clear separation between different entity stores
- Easy to replace with a database implementation later

This pattern is used for all entity types (diagrams, threat models, threats) and provides:

- CRUD operations
- Atomic updates
- Support for filtering and queries

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
make start-dev          # Start complete dev environment
make build-server       # Build production binary
make test-unit                # Run unit tests
make test-integration-new     # Run integration tests (server must be running)
make cats-fuzz               # Run security fuzzing
make lint               # Run code linting
```

## Configuration

Server configuration can be set via environment variables or using a `.env` file:

1. Copy the `.env.example` file to `.env`
2. Modify the values as needed
3. Start the server, which will automatically load the `.env` file

You can also specify a custom .env file with:

```bash
./bin/tmiserver --env=/path/to/custom.env
```

Available configuration options:

| Variable             | Default                           | Description                                 |
| -------------------- | --------------------------------- | ------------------------------------------- |
| SERVER_PORT          | 8080                              | HTTP/HTTPS server port                      |
| SERVER_INTERFACE     | 0.0.0.0                           | Network interface to listen on              |
| SERVER_READ_TIMEOUT  | 5s                                | HTTP read timeout                           |
| SERVER_WRITE_TIMEOUT | 10s                               | HTTP write timeout                          |
| SERVER_IDLE_TIMEOUT  | 60s                               | HTTP idle timeout                           |
| LOG_LEVEL            | info                              | Logging level (debug, info, warn, error)    |
| TLS_ENABLED          | false                             | Enable HTTPS/TLS                            |
| TLS_CERT_FILE        |                                   | Path to TLS certificate file                |
| TLS_KEY_FILE         |                                   | Path to TLS private key file                |
| TLS_SUBJECT_NAME     | [hostname]                        | Subject name for certificate validation     |
| TLS_HTTP_REDIRECT    | true                              | Redirect HTTP to HTTPS when TLS is enabled  |
| JWT_SECRET           | secret                            | JWT signing secret (change for production!) |
| JWT_EXPIRES_IN       | 24h                               | JWT expiration                              |
| OAUTH_URL            | https://oauth-provider.com/oauth2 | OAuth provider URL                          |
| OAUTH_SECRET         |                                   | OAuth client secret                         |
| DB_URL               | localhost                         | Database URL                                |
| DB_USERNAME          |                                   | Database username                           |
| DB_PASSWORD          |                                   | Database password                           |
| DB_NAME              | tmi                               | Database name                               |
| ENV                  | development                       | Environment (development or production)     |

### WebSocket URLs

When TLS is enabled (`TLS_ENABLED=true`), clients should connect using secure WebSocket URLs:

- Use `wss://` instead of `ws://` for WebSocket connections
- Example: `wss://your-server.com:8080/ws/diagrams/123`

When TLS is disabled, use standard WebSocket URLs:

- Example: `ws://your-server.com:8080/ws/diagrams/123`

You can use the `/api/server-info` endpoint to get the correct WebSocket base URL automatically.

## License

See [license.txt](license.txt)
