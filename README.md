# TMI - Threat Modeling Improved

A collaborative threat modeling server built with Go.

## Overview

TMI (Threat Modeling Improved) is a server based web application enabling collaborative threat modeling with support for:

- Real-time collaborative diagram editing via WebSockets
- Role-based access control (reader, writer, owner)
- OAuth authentication with JWT
- RESTful API with OpenAPI 3.0 specification
- MCP integration (planned)

The associated Angular/Typescript front-end web application is called [TMI-UX](https://github.com/ericfitz/tmi-ux).

## Getting Started

### Prerequisites

- Go 1.24+
- golangci-lint (for linting)
- git
- make
- Docker Desktop (to run the database & redis containers)

### Installation

```bash
git clone https://github.com/ericfitz/tmi.git
cd tmi
go mod download
```

### Running the server

```bash
make build-server
make dev
```

The server will start on port 8080 by default. The first time you run it, it has to download, create, and start the database and redis containers. This might time out on slow machines; run it again if so. Subsequent runs will not require this.

## Project Structure

- `api/` - API types and handlers
- `cmd/server/` - Server entry point and configuration
- `tmi-api-v1_0.md` - API documentation
- `shared/api-specs/tmi-openapi.json` - OpenAPI specification

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

## Development

### Building

```bash
make build-server
```

### Testing

```bash
make test-unit
```

Run a specific test:

```bash
make test-unit name=TestGetDiagrams
```

### Linting

```bash
make lint
```

### Generating API code

```bash
make generate-api
```

## API Documentation

See [tmi-api-v1_0.md](tmi-api-v1_0.md) for detailed API documentation.

## Configuration

Server configuration can be set via environment variables or using a `.env` file:

1. Copy the `.env.example` file to `.env`
2. Modify the values as needed
3. Start the server, which will automatically load the `.env` file

You can also specify a custom .env file with:

```bash
./bin/server --env=/path/to/custom.env
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
