# OAuth 2.0 Flow Diagrams with Stub Tool Integration

This document shows how the OAuth Client Callback Stub integrates with OAuth 2.0 flows during testing scenarios.

## Authorization Code Flow with OAuth Stub

```mermaid
sequenceDiagram
    participant Test as Test Framework<br/>(StepCI/Jest)
    participant Stub as OAuth Callback Stub<br/>(Port 8079)
    participant App as Application Server<br/>(Your OAuth Client)
    participant Auth as Authorization Server<br/>(OAuth Provider)

    Note over Test, Auth: Authorization Code Flow Testing

    Test->>+Stub: 1. Start OAuth stub server
    Stub-->>Test: Server ready on localhost:8079

    Test->>+App: 2. Initiate OAuth flow<br/>GET /auth/login/provider?client_callback=http://localhost:8079/
    App->>App: 3. Generate state parameter
    App->>+Auth: 4. Redirect to authorization URL<br/>GET /authorize?client_id=...&redirect_uri=http://localhost:8079/&state=...&response_type=code
    
    Auth->>Auth: 5. User authentication<br/>(simulated in test provider)
    Auth->>+Stub: 6. Authorization callback<br/>GET /?code=auth_code_123&state=abc123
    Stub->>Stub: 7. Detect Authorization Code Flow<br/>Store code and state
    Stub-->>-Auth: 302 Redirect response
    
    Test->>+Stub: 8. Retrieve captured credentials<br/>GET /latest
    Stub-->>-Test: {"flow_type": "authorization_code", "code": "auth_code_123", "state": "abc123"}
    
    Test->>+App: 9. Exchange code for tokens<br/>POST /auth/token/provider<br/>{"code": "auth_code_123", "redirect_uri": "http://localhost:8079/"}
    App->>+Auth: 10. Token exchange<br/>POST /token<br/>grant_type=authorization_code&code=auth_code_123
    Auth-->>-App: {"access_token": "token_456", "refresh_token": "refresh_789", "expires_in": 3600}
    App-->>-Test: {"access_token": "token_456", "refresh_token": "refresh_789", "expires_in": 3600}
    
    Test->>+App: 11. Use access token<br/>GET /api/user/me<br/>Authorization: Bearer token_456
    App-->>-Test: {"user": {...}, "authenticated": true}
    
    Test->>+Stub: 12. Stop OAuth stub<br/>GET /?code=exit
    Stub-->>-Test: Server shutdown
```

## Implicit Flow with OAuth Stub

```mermaid
sequenceDiagram
    participant Test as Test Framework<br/>(StepCI/Jest)
    participant Stub as OAuth Callback Stub<br/>(Port 8079)
    participant App as Application Server<br/>(Your OAuth Client)
    participant Auth as Authorization Server<br/>(OAuth Provider)

    Note over Test, Auth: Implicit Flow Testing

    Test->>+Stub: 1. Start OAuth stub server
    Stub-->>Test: Server ready on localhost:8079

    Test->>+App: 2. Initiate OAuth flow<br/>GET /auth/login/provider?client_callback=http://localhost:8079/
    App->>App: 3. Generate state parameter
    App->>+Auth: 4. Redirect to authorization URL<br/>GET /authorize?client_id=...&redirect_uri=http://localhost:8079/&state=...&response_type=token
    
    Auth->>Auth: 5. User authentication<br/>(simulated in test provider)
    Auth->>+Stub: 6. Implicit flow callback<br/>GET /?access_token=token_456&token_type=Bearer&expires_in=3600&state=abc123
    Stub->>Stub: 7. Detect Implicit Flow<br/>Store tokens directly
    Stub-->>-Auth: 302 Redirect response
    
    Test->>+Stub: 8. Retrieve captured credentials<br/>GET /latest
    Stub-->>-Test: {"flow_type": "implicit", "access_token": "token_456", "token_type": "Bearer", "expires_in": 3600, "state": "abc123"}
    
    Note over Test, App: No token exchange needed - tokens received directly
    
    Test->>+App: 9. Use access token immediately<br/>GET /api/user/me<br/>Authorization: Bearer token_456
    App-->>-Test: {"user": {...}, "authenticated": true}
    
    Test->>+Stub: 10. Stop OAuth stub<br/>GET /?code=exit
    Stub-->>-Test: Server shutdown
```

## Flow Comparison Summary

```mermaid
flowchart TD
    Start([OAuth Flow Testing]) --> StubStart[Start OAuth Callback Stub]
    StubStart --> InitFlow[Initiate OAuth Flow via Test]
    InitFlow --> AuthServer[Authorization Server Processing]
    
    AuthServer --> FlowType{Flow Type?}
    
    FlowType -->|Authorization Code| CodeFlow[Authorization Code Flow]
    FlowType -->|Implicit| TokenFlow[Implicit Flow]
    
    CodeFlow --> CodeCallback[Stub receives code + state]
    CodeCallback --> CodeCapture[Test retrieves: code, state]
    CodeCapture --> TokenExchange[Test exchanges code for tokens]
    TokenExchange --> UseTokens1[Test uses tokens for API calls]
    
    TokenFlow --> TokenCallback[Stub receives tokens + state]
    TokenCallback --> TokenCapture[Test retrieves: tokens, state]
    TokenCapture --> UseTokens2[Test uses tokens directly]
    
    UseTokens1 --> Cleanup[Stop OAuth Stub]
    UseTokens2 --> Cleanup
    Cleanup --> End([Test Complete])
    
    style StubStart fill:#e1f5fe
    style CodeCallback fill:#fff3e0
    style TokenCallback fill:#f3e5f5
    style Cleanup fill:#e8f5e8
```

## Key Integration Points

### OAuth Stub Tool Responsibilities

1. **Server Management**
   - Start HTTP server on configurable port (default 8079)
   - Provide callback endpoint for OAuth redirects
   - Offer credentials API for test consumption

2. **Flow Detection**
   - Automatically detect Authorization Code vs Implicit flow
   - Parse and store appropriate parameters
   - Return flow-specific JSON responses

3. **Test Integration**
   - Solve StepCI variable substitution limitations
   - Provide reliable credential retrieval mechanism
   - Enable graceful shutdown via HTTP request

### Test Framework Integration

1. **Setup Phase**
   - Start OAuth stub server
   - Configure application with stub callback URL
   - Clear any existing credentials

2. **Execution Phase**
   - Trigger OAuth flow through application
   - Wait for callback to be captured
   - Retrieve credentials from stub API

3. **Verification Phase**
   - Use captured credentials appropriately per flow type
   - Test API endpoints with obtained tokens
   - Validate expected behavior

4. **Cleanup Phase**
   - Stop OAuth stub server
   - Clean up test data

### Flow-Specific Differences

| Aspect | Authorization Code Flow | Implicit Flow |
|--------|------------------------|---------------|
| **Callback Parameters** | `code`, `state` | `access_token`, `token_type`, `expires_in`, `state` |
| **Additional Steps** | Code exchange required | Direct token usage |
| **Security** | More secure (server-side exchange) | Less secure (client-side tokens) |
| **Test Complexity** | Higher (2-step process) | Lower (direct token usage) |
| **Stub Response** | `{"flow_type": "authorization_code", "code": "...", "state": "..."}` | `{"flow_type": "implicit", "access_token": "...", "token_type": "...", "expires_in": ..., "state": "..."}` |

## Usage in Different Test Scenarios

### Unit Testing
- Mock OAuth stub responses
- Test flow detection logic
- Validate credential parsing

### Integration Testing
- Real OAuth stub server
- Full flow execution
- End-to-end credential handling

### API Testing
- Use captured tokens for authenticated requests
- Test token refresh flows (Authorization Code only)
- Validate token expiration handling

### Load Testing
- Multiple concurrent OAuth flows
- Stub server performance under load
- Token usage rate limiting