# WebSocket Test Harness

A standalone Go application for testing the TMI WebSocket interface for collaborative threat modeling.

## Features

- OAuth authentication with test provider using login hints
- Host mode: Creates threat models, adds participants, creates diagrams, and starts collaboration sessions
- Participant mode: Polls for available collaboration sessions and joins them
- Comprehensive logging of all network interactions and WebSocket messages
- Supports multiple concurrent instances

## Building

```bash
cd ws-test-harness
go mod tidy
go build -o ws-test-harness
```

## Usage

### Host Mode

Start as a host to create a new collaboration session:

```bash
# Basic host mode
./ws-test-harness --user alice --host

# Host mode with participants
./ws-test-harness --user alice --host --participants "bob,charlie,dave"

# Custom server
./ws-test-harness --server http://localhost:8080 --user alice --host
```

### Participant Mode

Start as a participant to join existing collaboration sessions:

```bash
# Join any available session
./ws-test-harness --user bob

# Multiple participants
./ws-test-harness --user charlie &
./ws-test-harness --user dave &
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
./ws-test-harness --user alice --host --participants "bob"
```

Terminal 2 (Participant):
```bash
./ws-test-harness --user bob
```

### Multi-User Collaboration

Terminal 1 (Host):
```bash
./ws-test-harness --user alice --host --participants "bob,charlie,dave"
```

Terminals 2-4 (Participants):
```bash
./ws-test-harness --user bob
./ws-test-harness --user charlie
./ws-test-harness --user dave
```

## Expected WebSocket Messages

Upon joining a collaboration session, clients should receive:

1. `participant_joined` - Notification of your own join
2. `participants_update` - Full list of current participants
3. `current_presenter` - Current presenter information (if any)

All subsequent WebSocket messages (sent and received) are logged with timestamps and pretty-printed JSON formatting.

## OAuth Flow

The harness implements the implicit OAuth flow:

1. Starts a local HTTP server on a random port for the callback
2. Makes a GET request to `/oauth2/authorize?idp=test&login_hint=<user>&client_callback=<url>`
3. Receives tokens via the callback URL
4. Uses the access token for all subsequent API calls and WebSocket connection

## Logging

All network interactions are logged to console:
- HTTP requests show method, URL, and request bodies
- HTTP responses show status codes and response bodies
- WebSocket messages are timestamped and pretty-printed
- OAuth callback parameters are logged in detail

## Exit

Use Ctrl+C to gracefully shutdown the application. The WebSocket connection will be properly closed.