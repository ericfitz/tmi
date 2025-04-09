# TMI - Threat Modeling Improved

A collaborative threat modeling interface built with Go.

## Overview

TMI (Threat Modeling Improved) is an API server enabling collaborative threat modeling with support for:

- Real-time collaborative diagram editing via WebSockets
- Role-based access control (reader, writer, owner)
- OAuth authentication with JWT
- RESTful API with OpenAPI 3.0 specification

## Getting Started

### Prerequisites

- Go 1.24+
- golangci-lint (for linting)

### Installation

```bash
git clone https://github.com/ericfitz/tmi.git
cd tmi
go mod download
```

### Running the server

```bash
make build
./bin/server
```

The server will start on port 8080 by default.

## Project Structure

- `api/` - API types and handlers
- `cmd/server/` - Server entry point and configuration
- `tmi-api-v1_0.md` - API documentation
- `tmi-openapi.json` - OpenAPI specification

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
make build
```

### Testing

```bash
make test
```

Run a specific test:

```bash
make test-one name=TestGetDiagrams
```

### Linting

```bash
make lint
```

### Generating API code

```bash
make gen-api
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

| Variable           | Default               | Description                  |
| ------------------ | --------------------- | ---------------------------- |
| SERVER_PORT        | 8080                  | HTTP server port            |
| SERVER_READ_TIMEOUT| 5s                    | HTTP read timeout           |
| SERVER_WRITE_TIMEOUT| 10s                   | HTTP write timeout          |
| SERVER_IDLE_TIMEOUT| 60s                   | HTTP idle timeout           |
| LOG_LEVEL          | info                  | Logging level (debug, info, warn, error) |
| JWT_SECRET         | secret                | JWT signing secret (change for production!) |
| JWT_EXPIRES_IN     | 24h                   | JWT expiration              |
| OAUTH_URL          | https://oauth-provider.com/auth | OAuth provider URL |
| OAUTH_SECRET       |                       | OAuth client secret         |
| DB_URL             | localhost             | Database URL                |
| DB_USERNAME        |                       | Database username           |
| DB_PASSWORD        |                       | Database password           |
| DB_NAME            | tmi                   | Database name               |

## License

See [license.txt](license.txt)