# Postman Collection OAuth Setup

## OAuth Authentication Flow

The collection uses oauth-client-callback-stub.py (port 8079) to capture OAuth tokens from TMI's implicit flow.

## REST API Flow

Certain REST APIs are dependent on other APIs having been called previously, such as that deletion of an object requires that the object have been created first, or manipulation of a sub-entity requires that the parent object have been created, and then the sub-entity have been created, before the sub-entity can be referenced and manipulated. These order-dependent workflows are described in postman/docs/api-workflows.json

## Key Configuration

- Collection has pre-request script that triggers OAuth and retrieves tokens
- Use IPv4 (127.0.0.1) not localhost to avoid IPv6 issues with newman
- OAuth stub saves tokens by user: `GET /creds?userid=alice` returns tokens for alice@test.tmi

## Variables Required

```javascript
baseUrl: "http://127.0.0.1:8080"; // TMI server
oauthStubUrl: "http://127.0.0.1:8079"; // OAuth stub
loginHint: "alice"; // User for test provider
access_token: ""; // Set by pre-request script
```

## Pre-Request Script Pattern

1. Check if cached token is valid (exp - 60 seconds)
2. If not, trigger OAuth: `GET /oauth2/authorize?idp=test&login_hint=X&client_callback=stub&scope=openid`
3. Wait 2 seconds for redirect processing
4. Query stub: `GET /creds?userid=X` to get saved token
5. Store in `pm.collectionVariables.set('access_token', token)`

## Request Auth Configuration

- Collection level: Bearer token using `{{access_token}}`
- Individual requests may override - check they use `{{access_token}}` not `{{bearerToken}}`

## Testing Pattern

```bash
# OAuth stub must be running
make start-oauth-stub

# Run with newman
newman run tmi-postman-collection.json \
  --env-var "loginHint=testuser" \
  --timeout-request 5000
```

## Common Issues

- 401 "invalid number of segments": Token variable mismatch
- ECONNREFUSED ::1:8079: IPv6 issue, use 127.0.0.1
- 404 from stub: User doesn't exist, token not saved yet
- Tests expect 401: Update to expect 200 for authenticated requests

## Shell Script Flow (test_diagram_metadata.sh pattern)

```bash
curl -sL "http://localhost:8080/oauth2/authorize?idp=test&login_hint=$USER&client_callback=http://localhost:8079/&scope=openid"
sleep 2
TOKEN=$(curl -s "http://localhost:8079/creds?userid=$USER" | jq -r '.access_token')
```

The `-L` flag is crucial - it follows redirects allowing the stub to capture tokens.
