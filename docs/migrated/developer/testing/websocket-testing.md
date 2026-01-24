# WebSocket Test Harness

<!-- Migrated to wiki: WebSocket-Test-Harness.md on 2025-01-24 -->

A standalone Go application for testing the TMI WebSocket interface for collaborative threat modeling.

**Location**: `/Users/efitz/Projects/tmi/wstest/`

## Features

- OAuth authentication with TMI test provider using login hints (uses `idp=test`)
- Host mode: Creates threat models, adds participants, creates diagrams, and starts collaboration sessions
- Participant mode: Polls for available collaboration sessions and joins them
- Comprehensive logging of all network interactions and WebSocket messages
- Supports multiple concurrent instances
- Uses the [Gorilla WebSocket](https://github.com/gorilla/websocket) library

## Building

Use the make target (preferred):
```bash
make build-wstest
```

Or build directly:
```bash
cd wstest
go mod tidy
go build -o wstest
```

## Usage

### Host Mode

Start as a host to create a new collaboration session:

```bash
# Basic host mode
./wstest --user alice --host

# Host mode with participants
./wstest --user alice --host --participants "bob,charlie,dave"

# Custom server
./wstest --server http://localhost:8080 --user alice --host
```

### Participant Mode

Start as a participant to join existing collaboration sessions:

```bash
# Join any available session
./wstest --user bob

# Multiple participants
./wstest --user charlie &
./wstest --user dave &
```

### Command Line Options

- `--server <url>`: Server URL (default: localhost:8080)
- `--user <hint>`: User login hint (required)
- `--host`: Run in host mode
- `--participants <list>`: Comma-separated list of participant hints (host mode only)

## Test Scenarios

### Basic Two-User Test

Terminal 1 (Host):

```bash
./wstest --user alice --host --participants "bob"
```

Terminal 2 (Participant):

```bash
./wstest --user bob
```

### Multi-User Collaboration

Terminal 1 (Host):

```bash
./wstest --user alice --host --participants "bob,charlie,dave"
```

Terminals 2-4 (Participants):

```bash
./wstest --user bob
./wstest --user charlie
./wstest --user dave
```

## Expected WebSocket Messages

Upon joining a collaboration session, clients should receive:

1. `participants_update` - Full list of current participants (includes `current_presenter` if any)

The `participants_update` message includes:
- `initiating_user` - The user who triggered the update (or null for system events)
- `participants` - Array of all participants with permissions and last activity
- `host` - The session host
- `current_presenter` - Current presenter (may be null if no presenter)

All subsequent WebSocket messages (sent and received) are logged with timestamps and pretty-printed JSON formatting.

## OAuth Flow

The harness implements the OAuth authorization code flow:

1. Starts a local HTTP server on a random port for the callback
2. Makes a GET request to `/oauth2/authorize?idp=test&login_hint=<user>&client_callback=<url>&scope=openid+email+profile`
3. Follows the redirect to the local callback server
4. Exchanges the authorization code for tokens via POST to `/oauth2/token`
5. Uses the access token for all subsequent API calls and WebSocket connection

**Note**: The harness uses `idp=test` which is the TMI development OAuth provider that creates test users based on login hints.

## Logging

All network interactions are logged to console:

- HTTP requests show method, URL, and request bodies
- HTTP responses show status codes and response bodies
- WebSocket messages are timestamped and pretty-printed
- OAuth callback parameters are logged in detail

## Exit

Use Ctrl+C to gracefully shutdown the application. The WebSocket connection will be properly closed.

## Make Targets

The following make targets are available for WebSocket testing:

| Target | Description |
|--------|-------------|
| `make build-wstest` | Build the WebSocket test harness binary |
| `make wstest` | Launch 3-terminal test (alice as host, bob & charlie as participants) |
| `make monitor-wstest` | Run WebSocket harness with user 'monitor' in foreground |
| `make clean-wstest` | Stop all running WebSocket test harness instances |

**Note**: The `make wstest` target includes a 30-second timeout to prevent runaway processes.

## Related Documentation

- [WebSocket API Reference](https://github.com/ericfitz/tmi/wiki/WebSocket-API-Reference) - Full WebSocket API documentation
- [AsyncAPI Specification](https://github.com/ericfitz/tmi/blob/main/docs/reference/apis/tmi-asyncapi.yml) - WebSocket message schemas
- [Collaborative Threat Modeling](https://github.com/ericfitz/tmi/wiki/Collaborative-Threat-Modeling) - Collaboration features overview
