# OAuth Client Callback Stub

A lightweight HTTP server that captures OAuth authorization callbacks for development and testing. This tool automatically detects and handles both **Authorization Code Flow** and **Implicit Flow**, making it ideal for testing OAuth integrations without implementing full callback handlers.

## Features

- **Universal Flow Support**: Automatically detects and handles both OAuth2 flows
- **Triple Route Architecture**: Callback handler + credentials API + user-specific credential retrieval
- **Credential Persistence**: Saves credentials to temporary files for later retrieval by user ID
- **User-Specific Access**: Retrieve credentials for specific users via `/creds?userid=<id>` endpoint
- **Automatic Cleanup**: Cleans up temporary credential files on startup
- **Comprehensive Logging**: Structured logs with flow analysis
- **StepCI Integration**: Solves variable substitution limitations in API tests
- **Development-Focused**: Localhost-only, graceful shutdown
- **Flow-Aware Responses**: Returns appropriate JSON for detected flow type

## Quick Start

### Basic Usage

```bash
# Start the stub server
python3 oauth-client-callback-stub.py --port 8079

# In another terminal, trigger OAuth flow
curl "http://your-oauth-server/oauth2/authorize/provider?client_callback=http://localhost:8079/"

# For TMI test provider with login_hints (predictable test users)
curl "http://localhost:8080/oauth2/authorize/test?login_hint=alice&client_callback=http://localhost:8079/"

# Retrieve captured credentials
curl http://localhost:8079/latest
```

### Using uv (Recommended)

```bash
# Start with automatic dependency management
uv run oauth-client-callback-stub.py --port 8079
```

## Installation

The tool uses Python's standard library with optional `uv` for dependency management. No external dependencies required.

### Prerequisites

- Python 3.7+
- Optional: [uv](https://github.com/astral-sh/uv) for enhanced Python tooling

### Setup

```bash
# Clone or download the script
wget https://example.com/oauth-client-callback-stub.py
chmod +x oauth-client-callback-stub.py

# Or with uv
uv add oauth-client-callback-stub.py
```

## Usage

### Command Line Options

```bash
python3 oauth-client-callback-stub.py [OPTIONS]

Options:
  --port PORT    Server port (default: 8079)
  --host HOST    Server host (default: localhost)
  --help         Show help message
```

### Server Management

#### Starting the Server

```bash
# Basic start
python3 oauth-client-callback-stub.py --port 8079

# With custom host/port
python3 oauth-client-callback-stub.py --host 0.0.0.0 --port 8080

# Background process
python3 oauth-client-callback-stub.py --port 8079 &
```

#### Stopping the Server

```bash
# Graceful HTTP shutdown
curl "http://localhost:8079/?code=exit"

# Force kill (find PID first)
pgrep -f oauth-client-callback-stub.py
kill <PID>

# Kill all instances
pkill -f oauth-client-callback-stub.py
```

#### Status Check

```bash
# Check if running
pgrep -f oauth-client-callback-stub.py > /dev/null && echo "Running" || echo "Stopped"

# Show process details
ps aux | grep oauth-client-callback-stub.py
```

## API Reference

### Routes

#### `GET /` - OAuth Callback Handler

Receives OAuth redirects from authorization servers and stores credentials.

**Authorization Code Flow Example:**

```
GET /?code=auth_code_123&state=abc123
```

**Implicit Flow Example:**

```
GET /?access_token=token_456&token_type=Bearer&expires_in=3600&state=abc123
```

**Special Command:**

```
GET /?code=exit  # Graceful server shutdown
```

#### `GET /latest` - Latest Credentials API

Returns the most recently captured OAuth credentials in flow-specific format.

#### `GET /creds?userid=<userid>` - User-Specific Credentials API

Retrieves saved credentials for a specific user ID from persistent storage.

**Parameters:**

- `userid` (required): User ID part before `@test.tmi` (e.g., `alice` for `alice@test.tmi`)
- Must match regex: `^[a-zA-Z0-9][a-zA-Z0-9-]{1,18}[a-zA-Z0-9]$`

**Responses:**

```bash
# Success (200)
curl "http://localhost:8079/creds?userid=alice"
{
  "flow_type": "implicit",
  "state": "test-state",
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_in": "3600",
  "tokens_ready": true
}

# Missing parameter (400)
curl "http://localhost:8079/creds"
{"error": "Missing required parameter: userid"}

# Invalid parameter (400)
curl "http://localhost:8079/creds?userid=a"
{"error": "Invalid userid parameter: a. Must match pattern ^[a-zA-Z0-9][a-zA-Z0-9-]{1,18}[a-zA-Z0-9]$"}

# User not found (404)
curl "http://localhost:8079/creds?userid=nonexistent"
{"error": "No credentials found for user: nonexistent@test.tmi"}
```

**Authorization Code Flow Response:**

```json
{
  "flow_type": "authorization_code",
  "code": "auth_code_123",
  "state": "abc123"
}
```

**Implicit Flow Response:**

```json
{
  "flow_type": "implicit",
  "access_token": "token_456",
  "token_type": "Bearer",
  "expires_in": 3600,
  "state": "abc123"
}
```

**No Data Response:**

```json
{
  "flow_type": null,
  "error": "No OAuth credentials captured yet"
}
```

## Logging

### Log Location

All logs are written to `/tmp/oauth-stub.log` with dual output to console.

### Log Format

```
YYYY-MM-DDTHH:MM:SS.sssZ <message>
```

### Sample Log Output

```
2025-01-15T10:30:15.123Z Starting OAuth callback stub server on localhost:8079
2025-01-15T10:30:15.124Z Server running at http://localhost:8079/
2025-01-15T10:30:15.124Z Routes:
2025-01-15T10:30:15.124Z   GET / - OAuth callback handler
2025-01-15T10:30:15.124Z   GET /latest - Latest credentials API
2025-01-15T10:30:15.124Z   GET /?code=exit - Graceful shutdown
2025-01-15T10:30:25.456Z [192.168.1.100] GET /?access_token=eyJhbGc...&token_type=Bearer&expires_in=3600&state=AbCdEf123 HTTP/1.1 200 - {"flow_type": "implicit", "credentials_captured": true}
2025-01-15T10:30:30.789Z [127.0.0.1] GET /latest HTTP/1.1 200 - {"flow_type": "implicit", "access_token": "eyJhbGc...", "token_type": "Bearer", "expires_in": 3600, "state": "AbCdEf123"}
```

### Monitoring Logs

```bash
# Follow logs in real-time
tail -f /tmp/oauth-stub.log

# Search for specific events
grep "credentials_captured" /tmp/oauth-stub.log

# Filter by flow type
grep "implicit" /tmp/oauth-stub.log
grep "authorization_code" /tmp/oauth-stub.log
```

## StepCI Integration

The OAuth stub solves StepCI's variable substitution limitations by providing a reliable way to capture and retrieve OAuth credentials.

### Basic StepCI Test

```yaml
# oauth-test.yml
version: "1.1"
name: OAuth Flow Test
env:
  host: localhost:8080
  stub_host: localhost:8079
tests:
  oauth_flow:
    steps:
      # Step 1: Clear any existing credentials
      - name: clear_credentials
        http:
          url: http://${{env.stub_host}}/latest
          method: GET

      # Step 2: Initiate OAuth flow with stub callback
      - name: start_oauth
        http:
          url: http://${{env.host}}/oauth2/authorize/test?client_callback=http://${{env.stub_host}}/
          method: GET
          follow_redirects: true

      # Step 3: Retrieve captured credentials
      - name: get_credentials
        http:
          url: http://${{env.stub_host}}/latest
          method: GET
          check:
            status: 200
            json:
              flow_type:
                not_eq: null
```

### Flow-Aware StepCI Test

```yaml
# advanced-oauth-test.yml
version: "1.1"
name: Universal OAuth Flow Test
env:
  host: localhost:8080
  stub_host: localhost:8079
tests:
  universal_oauth:
    steps:
      - name: initiate_oauth
        http:
          url: http://${{env.host}}/oauth2/authorize/test?client_callback=http://${{env.stub_host}}/
          method: GET
          follow_redirects: true

      - name: get_oauth_result
        http:
          url: http://${{env.stub_host}}/latest
          method: GET
          captures:
            flow_type: json.flow_type
            auth_code: json.code
            access_token: json.access_token

      # Conditional execution based on flow type
      - name: exchange_code
        if: captures.flow_type == 'authorization_code'
        http:
          url: http://${{env.host}}/oauth2/token/test
          method: POST
          json:
            code: ${{captures.auth_code}}
            redirect_uri: http://${{env.stub_host}}/
          check:
            status: 200

      - name: use_implicit_token
        if: captures.flow_type == 'implicit'
        http:
          url: http://${{env.host}}/api/user/me
          method: GET
          headers:
            Authorization: Bearer ${{captures.access_token}}
          check:
            status: 200
```

### Running StepCI Tests

```bash
# Install StepCI
npm install -g stepci

# Run basic test
stepci run oauth-test.yml

# Run with environment overrides
stepci run oauth-test.yml --env host=localhost:9000

# Run with verbose output
stepci run oauth-test.yml --verbose
```

## Common Use Cases

### 1. OAuth Provider Testing

Test OAuth providers without implementing callback handlers:

```bash
# Start stub
python3 oauth-client-callback-stub.py --port 8079

# Test your OAuth provider
curl "http://localhost:8080/oauth2/authorize/google?client_callback=http://localhost:8079/"

# Test TMI test provider with specific user (automation-friendly)
curl "http://localhost:8080/oauth2/authorize/test?login_hint=alice&client_callback=http://localhost:8079/"

# Test TMI test provider with random user (backwards compatible)
curl "http://localhost:8080/oauth2/authorize/test?client_callback=http://localhost:8079/"

# Check what was received
curl http://localhost:8079/latest

# Or retrieve credentials for specific user
curl "http://localhost:8079/creds?userid=alice"
```

**TMI Test Provider login_hints:**

For predictable test users in automated testing:

```bash
# Create specific users for testing
curl "http://localhost:8080/oauth2/authorize/test?login_hint=alice&client_callback=http://localhost:8079/"
curl "http://localhost:8080/oauth2/authorize/test?login_hint=bob&client_callback=http://localhost:8079/"
curl "http://localhost:8080/oauth2/authorize/test?login_hint=qa-automation&client_callback=http://localhost:8079/"

# Results in users: alice@test.tmi, bob@test.tmi, qa-automation@test.tmi
# login_hint format: 3-20 characters, alphanumeric + hyphens, case-insensitive
```

### 2. API Integration Testing

Capture real OAuth tokens for API testing:

```javascript
// test-oauth-api.js
const fetch = require("node-fetch");

async function testOAuthFlow(userId = null) {
  let creds;

  if (userId) {
    // Get credentials for specific user
    const response = await fetch(
      `http://localhost:8079/creds?userid=${userId}`
    );
    if (response.status === 404) {
      console.log(`No credentials found for user: ${userId}`);
      return;
    }
    creds = await response.json();
  } else {
    // Get latest credentials from stub
    const response = await fetch("http://localhost:8079/latest");
    creds = await response.json();
  }

  if (creds.flow_type === "implicit") {
    // Use token directly
    const apiResponse = await fetch("http://localhost:8080/api/data", {
      headers: { Authorization: `Bearer ${creds.access_token}` },
    });
    console.log("API Response:", await apiResponse.json());
  } else if (creds.flow_type === "authorization_code") {
    // Exchange code for token first
    const tokenResponse = await fetch("http://localhost:8080/oauth2/token", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        code: creds.code,
        redirect_uri: "http://localhost:8079/",
      }),
    });
    const tokens = await tokenResponse.json();
    console.log("Tokens:", tokens);
  }
}

// Test latest credentials
testOAuthFlow();

// Test specific user's credentials
testOAuthFlow("alice");
```

### 3. Multi-Provider Testing

Test different OAuth providers in sequence:

```bash
#!/bin/bash
# test-providers.sh

PROVIDERS=("google" "github" "microsoft")
STUB_PORT=8079

# Start stub
python3 oauth-client-callback-stub.py --port $STUB_PORT &
STUB_PID=$!

for provider in "${PROVIDERS[@]}"; do
    echo "Testing provider: $provider"

    # Trigger OAuth flow
    curl -s "http://localhost:8080/oauth2/authorize/$provider?client_callback=http://localhost:$STUB_PORT/" > /dev/null

    # Get results
    result=$(curl -s "http://localhost:$STUB_PORT/latest")
    echo "Result: $result"
    echo "---"

    sleep 2
done

# Cleanup
kill $STUB_PID
```

## Troubleshooting

### Common Issues

**Server Won't Start**

```bash
# Check if port is in use
lsof -i :8079
netstat -tulpn | grep 8079

# Use different port
python3 oauth-client-callback-stub.py --port 8080
```

**No Credentials Captured**

```bash
# Check logs for errors
tail -f /tmp/oauth-stub.log

# Verify callback URL format
echo "Callback should be: http://localhost:8079/"

# Test direct access
curl "http://localhost:8079/?code=test&state=test"
curl "http://localhost:8079/latest"
```

**StepCI Variable Issues**

```bash
# Check captures in StepCI verbose mode
stepci run test.yml --verbose

# Verify JSON response format
curl -s http://localhost:8079/latest | jq '.'
```

### Debug Mode

Add debug logging by modifying the script:

```python
import logging
logging.basicConfig(level=logging.DEBUG)
```

### Log Analysis

```bash
# Count successful captures
grep -c "credentials_captured.*true" /tmp/oauth-stub.log

# Find flow type distribution
grep -o "flow_type.*implicit\|flow_type.*authorization_code" /tmp/oauth-stub.log | sort | uniq -c

# Check for errors
grep -i error /tmp/oauth-stub.log
```

## Security Considerations

**Development Only**: Never use in production environments

### Risks

- **Log Sensitivity**: Display output and logs will contain auth codes and tokens - secure log files appropriately
- **Network Exposure**: Intentionally exposes auth codes and tokens via unauthenticated http - ensure firewall blocks external access to stub port

### Compensating Controls

- **Localhost Binding**: Server only accepts local connections
- **File Permissions**: Credential files readable by the running user only
- **Temporary Persistence**: Credentials saved to `$TMP/*.json` files, cleaned up on restart

## Advanced Usage

### Custom Response Handling

Modify the stub to handle custom OAuth parameters:

```python
# Add to request handler
custom_param = parsed_query.get('custom_param', [None])[0]
if custom_param:
    credentials['custom_param'] = custom_param
```

### Integration with Test Frameworks

**Jest Example:**

```javascript
// oauth-stub-helper.js
const fetch = require("node-fetch");

class OAuthStubHelper {
  constructor(port = 8079) {
    this.baseUrl = `http://localhost:${port}`;
  }

  async getLatestCredentials() {
    const response = await fetch(`${this.baseUrl}/latest`);
    return response.json();
  }

  async waitForCredentials(timeout = 10000) {
    const start = Date.now();
    while (Date.now() - start < timeout) {
      const creds = await this.getLatestCredentials();
      if (creds.flow_type) return creds;
      await new Promise((r) => setTimeout(r, 500));
    }
    throw new Error("Timeout waiting for OAuth credentials");
  }
}

module.exports = OAuthStubHelper;
```

### Automated Testing Pipeline

```yaml
# .github/workflows/oauth-test.yml
name: OAuth Integration Tests
on: [push, pull_request]
jobs:
  oauth-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - name: Setup Python
        uses: actions/setup-python@v2
        with:
          python-version: "3.9"
      - name: Start OAuth stub
        run: python3 scripts/oauth-client-callback-stub.py --port 8079 &
      - name: Start test server
        run: ./start-test-server.sh
      - name: Run OAuth tests
        run: stepci run tests/oauth-flow.yml
      - name: Stop servers
        run: |
          curl "http://localhost:8079/?code=exit"
          ./stop-test-server.sh
```

## Contributing

This tool is designed to be simple and focused. When making modifications:

1. Maintain backward compatibility with existing StepCI tests
2. Preserve the dual-route architecture (callback + API)
3. Keep logging comprehensive but not verbose
4. Test with both OAuth flows
5. Update documentation for any new features

## License

Licensed under Apache 2.0 license terms. This tool is provided as-is for development and testing purposes. Use at your own discretion in development environments only.
