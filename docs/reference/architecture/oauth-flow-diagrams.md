# OAuth Flow Diagrams

This document provides comprehensive visual documentation of authentication flows in the TMI system, including OAuth 2.0 with PKCE, SAML, token management, and session lifecycle.

## Table of Contents

- [OAuth 2.0 Authorization Code Flow with PKCE](#oauth-20-authorization-code-flow-with-pkce)
- [Token Refresh Flow](#token-refresh-flow)
- [State Management and CSRF Protection](#state-management-and-csrf-protection)
- [Multi-Provider Support](#multi-provider-support)
- [SAML Authentication Flow](#saml-authentication-flow)
- [Session Lifecycle and JWT Token Handling](#session-lifecycle-and-jwt-token-handling)
- [Error Handling Flows](#error-handling-flows)

## OAuth 2.0 Authorization Code Flow with PKCE

TMI implements OAuth 2.0 Authorization Code Flow with PKCE (Proof Key for Code Exchange, RFC 7636) for enhanced security. PKCE prevents authorization code interception attacks and is essential for public clients that cannot securely store client secrets.

### Complete PKCE Flow Sequence

```
┌─────────┐                          ┌─────────────┐                    ┌─────────────┐                 ┌──────────┐
│ Client  │                          │ TMI Server  │                    │   OAuth     │                 │  Redis   │
│   App   │                          │             │                    │  Provider   │                 │  Cache   │
└────┬────┘                          └──────┬──────┘                    └──────┬──────┘                 └────┬─────┘
     │                                      │                                  │                             │
     │  1. User initiates login             │                                  │                             │
     ├─────────────────────────────────────►│                                  │                             │
     │                                      │                                  │                             │
     │                                      │  2. Client generates PKCE params │                             │
     │◄─────────────────────────────────────┤     code_verifier (random 43-128 chars)                       │
     │  Redirect to /oauth2/authorize       │     code_challenge = BASE64URL(SHA256(code_verifier))         │
     │  ?idp=google                         │     code_challenge_method = S256                               │
     │  &code_challenge=xyz...              │     state = random 32 bytes                                    │
     │  &code_challenge_method=S256         │                                  │                             │
     │  &state=abc...                       │                                  │                             │
     │  &client_callback=http://client/cb   │                                  │                             │
     │                                      │                                  │                             │
     │  3. POST /oauth2/authorize           │                                  │                             │
     ├─────────────────────────────────────►│                                  │                             │
     │                                      │                                  │                             │
     │                                      │  4. Store PKCE challenge + state │                             │
     │                                      ├─────────────────────────────────────────────────────────────►│
     │                                      │  SET pkce:state = {              │                             │
     │                                      │    code_challenge: "xyz...",     │                             │
     │                                      │    code_challenge_method: "S256", │                            │
     │                                      │    provider: "google"            │                             │
     │                                      │  }                               │                             │
     │                                      │  TTL = 10 minutes                │                             │
     │                                      │◄─────────────────────────────────────────────────────────────┤
     │                                      │                                  │                             │
     │  5. Redirect to OAuth provider       │                                  │                             │
     │◄─────────────────────────────────────┤                                  │                             │
     │  302 https://accounts.google.com/    │                                  │                             │
     │      oauth2/auth?...                 │                                  │                             │
     │                                      │                                  │                             │
     │  6. User authenticates with provider │                                  │                             │
     ├─────────────────────────────────────────────────────────────────────────►                             │
     │                                      │                                  │                             │
     │  7. Provider authenticates user      │                                  │                             │
     │  (Login page, consent screen)        │                                  │                             │
     │◄─────────────────────────────────────────────────────────────────────────┤                             │
     │                                      │                                  │                             │
     │  8. Provider redirects to TMI callback                                  │                             │
     │◄─────────────────────────────────────────────────────────────────────────┤                             │
     │  GET /oauth2/callback?code=auth_code&state=abc...                       │                             │
     │                                      │                                  │                             │
     │  9. Forward callback to TMI          │                                  │                             │
     ├─────────────────────────────────────►│                                  │                             │
     │                                      │                                  │                             │
     │                                      │  10. Retrieve PKCE challenge     │                             │
     │                                      ├─────────────────────────────────────────────────────────────►│
     │                                      │  GET pkce:state                  │                             │
     │                                      │◄─────────────────────────────────────────────────────────────┤
     │                                      │  {code_challenge, method}        │                             │
     │                                      │                                  │                             │
     │                                      │  11. Bind PKCE to auth code      │                             │
     │                                      ├─────────────────────────────────────────────────────────────►│
     │                                      │  SET pkce:auth_code = {          │                             │
     │                                      │    code_challenge: "xyz...",     │                             │
     │                                      │    code_challenge_method: "S256"  │                            │
     │                                      │  }                               │                             │
     │                                      │  TTL = 10 minutes                │                             │
     │                                      │  DEL pkce:state                  │                             │
     │                                      │◄─────────────────────────────────────────────────────────────┤
     │                                      │                                  │                             │
     │  12. Redirect to client callback     │                                  │                             │
     │◄─────────────────────────────────────┤                                  │                             │
     │  302 http://client/cb?code=auth_code&state=abc...                       │                             │
     │                                      │                                  │                             │
     │  13. Client exchanges code for tokens│                                  │                             │
     │  POST /oauth2/token?idp=google       │                                  │                             │
     │  {                                   │                                  │                             │
     │    grant_type: "authorization_code", │                                  │                             │
     │    code: "auth_code",                │                                  │                             │
     │    code_verifier: "original_verifier",│                                 │                             │
     │    redirect_uri: "http://client/cb"  │                                  │                             │
     │  }                                   │                                  │                             │
     ├─────────────────────────────────────►│                                  │                             │
     │                                      │                                  │                             │
     │                                      │  14. Retrieve PKCE challenge     │                             │
     │                                      ├─────────────────────────────────────────────────────────────►│
     │                                      │  GET pkce:auth_code              │                             │
     │                                      │◄─────────────────────────────────────────────────────────────┤
     │                                      │  {code_challenge, method}        │                             │
     │                                      │                                  │                             │
     │                                      │  15. Validate PKCE               │                             │
     │                                      │  - Compute: SHA256(code_verifier)│                             │
     │                                      │  - Compare with code_challenge   │                             │
     │                                      │  - Must match or reject request  │                             │
     │                                      │                                  │                             │
     │                                      │  16. Exchange code with provider │                             │
     │                                      ├─────────────────────────────────────────────────────────────►│
     │                                      │  POST /oauth/token               │                             │
     │                                      │  {code, client_id, client_secret}│                             │
     │                                      │◄─────────────────────────────────┤                             │
     │                                      │  {access_token, id_token}        │                             │
     │                                      │                                  │                             │
     │                                      │  17. Get user info from provider │                             │
     │                                      ├─────────────────────────────────────────────────────────────►│
     │                                      │  GET /userinfo (with access_token)                             │
     │                                      │◄─────────────────────────────────┤                             │
     │                                      │  {email, name, id}               │                             │
     │                                      │                                  │                             │
     │                                      │  18. Create/update user in DB    │                             │
     │                                      │  19. Generate TMI JWT tokens     │                             │
     │                                      │  20. Delete PKCE challenge       │                             │
     │                                      ├─────────────────────────────────────────────────────────────►│
     │                                      │  DEL pkce:auth_code              │                             │
     │                                      │◄─────────────────────────────────────────────────────────────┤
     │                                      │                                  │                             │
     │  21. Return TMI tokens               │                                  │                             │
     │◄─────────────────────────────────────┤                                  │                             │
     │  {                                   │                                  │                             │
     │    access_token: "eyJhbGc...",       │                                  │                             │
     │    refresh_token: "eyJhbGc...",      │                                  │                             │
     │    token_type: "Bearer",             │                                  │                             │
     │    expires_in: 3600                  │                                  │                             │
     │  }                                   │                                  │                             │
     │                                      │                                  │                             │
```

### PKCE Parameter Generation

```
Client-Side PKCE Generation:
┌─────────────────────────────────────────────────────────────────┐
│ 1. Generate code_verifier                                       │
│    - Random string: 43-128 characters                           │
│    - Characters: [A-Z][a-z][0-9]-._~                           │
│    - Example: dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk       │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. Compute code_challenge                                       │
│    - Method: S256 (SHA-256 hash)                               │
│    - Process: BASE64URL(SHA256(code_verifier))                 │
│    - Output: 43 characters (no padding)                        │
│    - Example: E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM       │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. Send to authorization endpoint                              │
│    - code_challenge: computed hash                             │
│    - code_challenge_method: "S256"                             │
│    - Store code_verifier locally for token exchange            │
└─────────────────────────────────────────────────────────────────┘
```

### Server-Side PKCE Validation

```
TMI Server PKCE Validation Flow:
┌─────────────────────────────────────────────────────────────────┐
│ 1. Receive token exchange request                              │
│    - authorization_code: received from OAuth provider           │
│    - code_verifier: provided by client                         │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. Retrieve stored PKCE challenge                              │
│    - Lookup: pkce:authorization_code in Redis                  │
│    - Get: code_challenge, code_challenge_method                │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. Compute challenge from verifier                             │
│    - computed_challenge = BASE64URL(SHA256(code_verifier))     │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. Validate challenges match                                   │
│    - Compare: computed_challenge == stored_challenge           │
│    - If match: Continue token exchange                         │
│    - If mismatch: Reject with 400 Bad Request                  │
│    - Delete PKCE record after validation (one-time use)        │
└─────────────────────────────────────────────────────────────────┘
```

## Token Refresh Flow

JWT tokens expire after 1 hour. The refresh token (valid for 30 days) allows obtaining new access tokens without re-authentication.

### Token Refresh Sequence

```
┌─────────┐                          ┌─────────────┐                 ┌──────────┐            ┌──────────┐
│ Client  │                          │ TMI Server  │                 │  Redis   │            │PostgreSQL│
│   App   │                          │             │                 │  Cache   │            │    DB    │
└────┬────┘                          └──────┬──────┘                 └────┬─────┘            └────┬─────┘
     │                                      │                             │                       │
     │  1. Access token expires             │                             │                       │
     │  (Client detects expiration)         │                             │                       │
     │                                      │                             │                       │
     │  2. POST /oauth2/refresh             │                             │                       │
     │  {                                   │                             │                       │
     │    refresh_token: "eyJhbGc..."       │                             │                       │
     │  }                                   │                             │                       │
     ├─────────────────────────────────────►│                             │                       │
     │                                      │                             │                       │
     │                                      │  3. Validate JWT signature  │                       │
     │                                      │  - Verify using key manager │                       │
     │                                      │  - Check token not expired  │                       │
     │                                      │  - Extract claims (sub, email)                      │
     │                                      │                             │                       │
     │                                      │  4. Check token not blacklisted                     │
     │                                      ├────────────────────────────►│                       │
     │                                      │  GET blacklist:token_jti    │                       │
     │                                      │◄────────────────────────────┤                       │
     │                                      │  (not found = valid)        │                       │
     │                                      │                             │                       │
     │                                      │  5. Verify user exists                              │
     │                                      ├────────────────────────────────────────────────────►│
     │                                      │  SELECT * FROM users        │                       │
     │                                      │  WHERE email = ?            │                       │
     │                                      │◄────────────────────────────────────────────────────┤
     │                                      │  User record                │                       │
     │                                      │                             │                       │
     │                                      │  6. Update last login       │                       │
     │                                      ├────────────────────────────────────────────────────►│
     │                                      │  UPDATE users SET           │                       │
     │                                      │  last_login = NOW()         │                       │
     │                                      │◄────────────────────────────────────────────────────┤
     │                                      │                             │                       │
     │                                      │  7. Generate new token pair │                       │
     │                                      │  - New access_token (1h exp)│                       │
     │                                      │  - New refresh_token (30d exp)                      │
     │                                      │  - Include updated claims   │                       │
     │                                      │                             │                       │
     │  8. Return new tokens                │                             │                       │
     │◄─────────────────────────────────────┤                             │                       │
     │  {                                   │                             │                       │
     │    access_token: "eyJhbGc...",       │                             │                       │
     │    refresh_token: "eyJhbGc...",      │                             │                       │
     │    token_type: "Bearer",             │                             │                       │
     │    expires_in: 3600                  │                             │                       │
     │  }                                   │                             │                       │
     │                                      │                             │                       │
     │  9. Store new tokens                 │                             │                       │
     │  10. Use new access_token            │                             │                       │
     │                                      │                             │                       │
```

### Token Lifecycle States

```
Token State Diagram:

┌──────────────┐  User Login   ┌──────────────┐  < 60s to expiry  ┌──────────────┐
│              ├──────────────►│              ├──────────────────►│              │
│   No Token   │               │    Active    │                   │   Refreshing │
│              │◄──────────────┤              │◄──────────────────┤              │
└──────────────┘  Logout/Error └──────┬───────┘  Refresh Complete └──────────────┘
                                      │
                                      │ Expired (> 1h)
                                      ▼
                               ┌──────────────┐
                               │              │
                               │   Expired    │
                               │              │
                               └──────┬───────┘
                                      │
                                      │ Refresh token valid (< 30d)
                                      ▼
                               ┌──────────────┐
                               │              │
                               │   Refreshing │
                               │              │
                               └──────┬───────┘
                                      │
                                      │ Refresh token expired (> 30d)
                                      ▼
                               ┌──────────────┐
                               │              │
                               │  Re-login    │
                               │   Required   │
                               └──────────────┘
```

### Automatic Token Refresh

```
Client-Side Token Management:

┌─────────────────────────────────────────────────────────────────┐
│ Before API Request:                                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ 1. Check token expiration                                       │
│    - Current time > (expires_at - 60s)?                        │
│                                                                 │
│ 2. If expiring soon:                                           │
│    - Call /oauth2/refresh with refresh_token                   │
│    - Wait for new token pair                                   │
│    - Update stored tokens                                      │
│    - Update expiration time                                    │
│                                                                 │
│ 3. If refresh failed:                                          │
│    - Clear all tokens                                          │
│    - Redirect to login                                         │
│                                                                 │
│ 4. Make API request with valid access_token                    │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

API Request with Auto-Refresh:

  GET /api/resource ─┐
                     │  Is token expired?
                     ├─► No ──► Use access_token ──► Make Request
                     │
                     └─► Yes ─┐
                              │  Call /oauth2/refresh
                              ├─► Success ──► Update tokens ──► Make Request
                              │
                              └─► Failed ──► Clear tokens ──► Redirect to login
```

## State Management and CSRF Protection

State parameters prevent Cross-Site Request Forgery (CSRF) attacks by ensuring OAuth callbacks can only be completed by the client that initiated the flow.

### State Flow Sequence

```
┌─────────┐                          ┌─────────────┐                 ┌──────────┐
│ Client  │                          │ TMI Server  │                 │  Redis   │
│   App   │                          │             │                 │  Cache   │
└────┬────┘                          └──────┬──────┘                 └────┬─────┘
     │                                      │                             │
     │  1. Generate state parameter         │                             │
     │  - Random 32 bytes                   │                             │
     │  - Base64URL encoded                 │                             │
     │  - Example: "8xk7f3n2m9..."          │                             │
     │                                      │                             │
     │  2. Store state locally              │                             │
     │  sessionStorage.setItem(             │                             │
     │    "oauth_state", state)             │                             │
     │                                      │                             │
     │  3. Send to authorization endpoint   │                             │
     │  GET /oauth2/authorize?              │                             │
     │    idp=google&state=8xk7f3n2m9...    │                             │
     ├─────────────────────────────────────►│                             │
     │                                      │                             │
     │                                      │  4. Store state server-side │
     │                                      ├────────────────────────────►│
     │                                      │  SET oauth_state:8xk7f3n2m9 │
     │                                      │  {                          │
     │                                      │    provider: "google",      │
     │                                      │    client_callback: "...",  │
     │                                      │    login_hint: "alice"      │
     │                                      │  }                          │
     │                                      │  TTL = 10 minutes           │
     │                                      │◄────────────────────────────┤
     │                                      │                             │
     │  [OAuth flow continues...]           │                             │
     │                                      │                             │
     │  5. OAuth callback with state        │                             │
     │  GET /oauth2/callback?               │                             │
     │    code=auth_code&state=8xk7f3n2m9   │                             │
     ├─────────────────────────────────────►│                             │
     │                                      │                             │
     │                                      │  6. Retrieve & validate     │
     │                                      ├────────────────────────────►│
     │                                      │  GET oauth_state:8xk7f3n2m9 │
     │                                      │◄────────────────────────────┤
     │                                      │  {provider, callback, hint} │
     │                                      │                             │
     │                                      │  7. Delete state (one-time) │
     │                                      ├────────────────────────────►│
     │                                      │  DEL oauth_state:8xk7f3n2m9 │
     │                                      │◄────────────────────────────┤
     │                                      │                             │
     │  8. Redirect to client               │                             │
     │  with code and state                 │                             │
     │◄─────────────────────────────────────┤                             │
     │  http://client/cb?code=...&          │                             │
     │  state=8xk7f3n2m9                    │                             │
     │                                      │                             │
     │  9. Validate state matches           │                             │
     │  stored_state = sessionStorage       │                             │
     │    .getItem("oauth_state")           │                             │
     │  if (state !== stored_state)         │                             │
     │    throw "CSRF attack detected"      │                             │
     │                                      │                             │
     │  10. Clear stored state              │                             │
     │  sessionStorage.removeItem(          │                             │
     │    "oauth_state")                    │                             │
     │                                      │                             │
```

### State Validation Logic

```
Server-Side State Validation:

┌─────────────────────────────────────────────────────────────────┐
│ 1. Parse state parameter from callback                         │
│    - Extract from URL query parameter                          │
│    - Validate format (base64url encoded)                       │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. Lookup state in Redis                                       │
│    - Key: oauth_state:{state_value}                            │
│    - If not found: Reject (expired or invalid)                 │
│    - If found: Extract stored data                             │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. Validate state data                                         │
│    - Provider ID matches request                               │
│    - Timestamp within expiration window (10 min)               │
│    - Client callback URL is valid absolute URL                 │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. Delete state immediately                                    │
│    - One-time use prevents replay attacks                      │
│    - DEL oauth_state:{state_value}                             │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 5. Return validated state data                                 │
│    - Provider ID for token exchange                            │
│    - Client callback URL for redirect                          │
│    - Optional login_hint for test provider                     │
└─────────────────────────────────────────────────────────────────┘

Client-Side State Validation:

┌─────────────────────────────────────────────────────────────────┐
│ 1. Receive OAuth callback                                      │
│    - Parse state from URL                                      │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. Retrieve stored state                                       │
│    - sessionStorage.getItem("oauth_state")                     │
│    - If not found: Reject (possible CSRF)                      │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. Compare states                                              │
│    - callback_state === stored_state                           │
│    - If mismatch: Reject with CSRF warning                     │
│    - If match: Continue OAuth flow                             │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. Clear stored state                                          │
│    - sessionStorage.removeItem("oauth_state")                  │
│    - Prevent state reuse                                       │
└─────────────────────────────────────────────────────────────────┘
```

## Multi-Provider Support

TMI supports multiple OAuth providers (Google, GitHub, Microsoft) and a test provider for development. All providers use the same PKCE-enhanced flow.

### Provider Discovery Flow

```
┌─────────┐                          ┌─────────────┐
│ Client  │                          │ TMI Server  │
│   App   │                          │             │
└────┬────┘                          └──────┬──────┘
     │                                      │
     │  1. GET /oauth2/providers            │
     ├─────────────────────────────────────►│
     │                                      │
     │                                      │  2. Load enabled providers
     │                                      │  from configuration
     │                                      │
     │  3. Return provider list             │
     │◄─────────────────────────────────────┤
     │  {                                   │
     │    "providers": [                    │
     │      {                               │
     │        "id": "google",               │
     │        "name": "Google",             │
     │        "icon": "fa-brands fa-google",│
     │        "auth_url": "http://...       │
     │          /oauth2/authorize?idp=google│
     │        "client_id": "675196260523..." │
     │      },                              │
     │      {                               │
     │        "id": "github",               │
     │        "name": "GitHub",             │
     │        ...                           │
     │      }                               │
     │    ]                                 │
     │  }                                   │
     │                                      │
     │  4. Render provider buttons          │
     │  - Google login button               │
     │  - GitHub login button               │
     │  - Microsoft login button            │
     │  - Test provider (dev only)          │
     │                                      │
```

### Provider-Specific Authentication

```
Google OAuth Flow:
┌──────────────────────────────────────────────────────────────┐
│ Authorization URL:                                           │
│   https://accounts.google.com/o/oauth2/auth                 │
│                                                              │
│ Token URL:                                                   │
│   https://oauth2.googleapis.com/token                       │
│                                                              │
│ UserInfo URL:                                                │
│   https://www.googleapis.com/oauth2/v3/userinfo             │
│                                                              │
│ Scopes:                                                      │
│   openid, profile, email                                    │
│                                                              │
│ Claims Mapping:                                              │
│   subject_claim: "sub" (user ID)                            │
│   email_claim: "email"                                      │
│   name_claim: "name"                                        │
└──────────────────────────────────────────────────────────────┘

GitHub OAuth Flow:
┌──────────────────────────────────────────────────────────────┐
│ Authorization URL:                                           │
│   https://github.com/login/oauth/authorize                  │
│                                                              │
│ Token URL:                                                   │
│   https://github.com/login/oauth/access_token               │
│                                                              │
│ UserInfo URLs (multiple):                                    │
│   1. https://api.github.com/user                            │
│   2. https://api.github.com/user/emails                     │
│                                                              │
│ Scopes:                                                      │
│   user:email                                                │
│                                                              │
│ Auth Header Format:                                          │
│   token {access_token} (NOT Bearer)                         │
│                                                              │
│ Claims Mapping:                                              │
│   subject_claim: "id"                                       │
│   email_claim: "[0].email" (from emails endpoint)           │
│   name_claim: "name"                                        │
│   email_verified_claim: "[0].verified"                      │
└──────────────────────────────────────────────────────────────┘

Microsoft OAuth Flow:
┌──────────────────────────────────────────────────────────────┐
│ Authorization URL:                                           │
│   https://login.microsoftonline.com/consumers/              │
│   oauth2/v2.0/authorize                                     │
│                                                              │
│ Token URL:                                                   │
│   https://login.microsoftonline.com/consumers/              │
│   oauth2/v2.0/token                                         │
│                                                              │
│ UserInfo URL:                                                │
│   https://graph.microsoft.com/v1.0/me                       │
│                                                              │
│ Scopes:                                                      │
│   openid, profile, email, User.Read                         │
│                                                              │
│ Claims Mapping:                                              │
│   subject_claim: "id"                                       │
│   email_claim: "mail" (Microsoft-specific)                  │
│   name_claim: "displayName" (Microsoft-specific)            │
│   email_verified_claim: "true" (literal)                    │
└──────────────────────────────────────────────────────────────┘

Test Provider Flow (Development Only):
┌──────────────────────────────────────────────────────────────┐
│ Purpose:                                                     │
│   Simplified OAuth for development and testing              │
│                                                              │
│ Features:                                                    │
│   - No external provider needed                             │
│   - Instant authentication                                  │
│   - Predictable user identities via login_hint              │
│   - PKCE validation for testing security flows              │
│                                                              │
│ Login Hint Support:                                          │
│   - login_hint=alice → alice@tmi.local                       │
│   - login_hint=bob → bob@tmi.local                           │
│   - No hint → random testuser-{timestamp}@tmi.local          │
│                                                              │
│ Availability:                                                │
│   - Development builds only                                 │
│   - Returns 404 in production                               │
└──────────────────────────────────────────────────────────────┘
```

### Provider Configuration

```
Generic Provider Configuration Pattern:

┌─────────────────────────────────────────────────────────────────┐
│ providers:                                                      │
│   {provider_id}:                                                │
│     id: "{provider_id}"                                         │
│     name: "Display Name"                                        │
│     enabled: true                                               │
│     icon: "fa-brands fa-{provider}"                            │
│     client_id: "${OAUTH_PROVIDERS_{PROVIDER}_CLIENT_ID}"       │
│     client_secret: "${OAUTH_PROVIDERS_{PROVIDER}_CLIENT_SECRET}"│
│     authorization_url: "https://..."                            │
│     token_url: "https://..."                                    │
│     auth_header_format: "Bearer %s" (or "token %s" for GitHub) │
│     accept_header: "application/json"                           │
│     userinfo:                                                   │
│       - url: "https://api.provider.com/user"                   │
│         claims:                                                 │
│           subject_claim: "id"                                   │
│           email_claim: "email"                                  │
│           name_claim: "name"                                    │
│     issuer: "https://provider.com"                             │
│     jwks_url: "https://provider.com/.well-known/jwks.json"     │
│     scopes: ["openid", "profile", "email"]                     │
└─────────────────────────────────────────────────────────────────┘

Claim Mapping Patterns:
┌─────────────────────────────────────────────────────────────────┐
│ Simple field:        "field_name"                              │
│ Nested field:        "parent.child.field"                      │
│ Array access:        "[0].field"                               │
│ Literal value:       "true" or "false"                         │
└─────────────────────────────────────────────────────────────────┘
```

## SAML Authentication Flow

SAML 2.0 provides enterprise SSO capability with support for identity provider-initiated and service provider-initiated flows.

### SAML SP-Initiated Flow

```
┌─────────┐                    ┌─────────────┐                    ┌─────────────┐                 ┌──────────┐
│ Client  │                    │ TMI Server  │                    │   Identity  │                 │  Redis   │
│   App   │                    │     (SP)    │                    │   Provider  │                 │  Cache   │
└────┬────┘                    └──────┬──────┘                    └──────┬──────┘                 └────┬─────┘
     │                                │                                  │                             │
     │  1. User initiates SAML login  │                                  │                             │
     │  GET /saml/{provider}/login    │                                  │                             │
     ├───────────────────────────────►│                                  │                             │
     │                                │                                  │                             │
     │                                │  2. Generate SAML AuthnRequest   │                             │
     │                                │  - Request ID: id-{timestamp}    │                             │
     │                                │  - RelayState: random value      │                             │
     │                                │  - Destination: IdP SSO URL      │                             │
     │                                │  - Sign request with SP key      │                             │
     │                                │                                  │                             │
     │                                │  3. Store RelayState             │                             │
     │                                ├─────────────────────────────────────────────────────────────►│
     │                                │  SET saml_state:{relay_state}    │                             │
     │                                │  {                               │                             │
     │                                │    provider: "{provider_id}",    │                             │
     │                                │    client_callback: "...",       │                             │
     │                                │    timestamp: ...                │                             │
     │                                │  }                               │                             │
     │                                │  TTL = 10 minutes                │                             │
     │                                │◄─────────────────────────────────────────────────────────────┤
     │                                │                                  │                             │
     │  4. Redirect to IdP            │                                  │                             │
     │◄───────────────────────────────┤                                  │                             │
     │  302 https://idp.example.com/  │                                  │                             │
     │      sso?SAMLRequest=base64... │                                  │                             │
     │      &RelayState=random...     │                                  │                             │
     │                                │                                  │                             │
     │  5. Forward to IdP             │                                  │                             │
     ├─────────────────────────────────────────────────────────────────►│                             │
     │                                │                                  │                             │
     │  6. User authenticates         │                                  │                             │
     │  (IdP login page)              │                                  │                             │
     │◄─────────────────────────────────────────────────────────────────┤                             │
     │                                │                                  │                             │
     │  7. IdP creates SAML Response  │                                  │                             │
     │  with signed assertion         │                                  │                             │
     │                                │                                  │                             │
     │  8. POST to ACS endpoint       │                                  │                             │
     │◄─────────────────────────────────────────────────────────────────┤                             │
     │  POST /saml/{provider}/acs     │                                  │                             │
     │  SAMLResponse=base64...        │                                  │                             │
     │  RelayState=random...          │                                  │                             │
     │                                │                                  │                             │
     │  9. Forward ACS request        │                                  │                             │
     ├───────────────────────────────►│                                  │                             │
     │                                │                                  │                             │
     │                                │  10. Validate RelayState         │                             │
     │                                ├─────────────────────────────────────────────────────────────►│
     │                                │  GET saml_state:{relay_state}    │                             │
     │                                │◄─────────────────────────────────────────────────────────────┤
     │                                │  {provider, callback}            │                             │
     │                                │                                  │                             │
     │                                │  11. Validate SAML Response      │                             │
     │                                │  - Decode base64 XML             │                             │
     │                                │  - Verify XML signature          │                             │
     │                                │  - Validate Issuer matches IdP   │                             │
     │                                │  - Validate Audience matches SP  │                             │
     │                                │  - Check NotBefore/NotOnOrAfter  │                             │
     │                                │  - Decrypt assertion if encrypted│                             │
     │                                │                                  │                             │
     │                                │  12. Extract user attributes     │                             │
     │                                │  - NameID → email                │                             │
     │                                │  - Attributes → name, groups     │                             │
     │                                │                                  │                             │
     │                                │  13. Create/update user in DB    │                             │
     │                                │  14. Generate TMI JWT tokens     │                             │
     │                                │                                  │                             │
     │  15. Redirect to client        │                                  │                             │
     │  with tokens                   │                                  │                             │
     │◄───────────────────────────────┤                                  │                             │
     │  302 http://client/cb#         │                                  │                             │
     │      access_token=eyJhbGc...   │                                  │                             │
     │      &refresh_token=eyJhbGc... │                                  │                             │
     │      &token_type=Bearer        │                                  │                             │
     │      &expires_in=3600          │                                  │                             │
     │                                │                                  │                             │
```

### SAML Assertion Validation

```
SAML Response Validation Steps:

┌─────────────────────────────────────────────────────────────────┐
│ 1. Decode SAML Response                                        │
│    - Base64 decode SAMLResponse parameter                      │
│    - Parse XML with strict settings                            │
│    - Limit size to prevent XML bombs (100KB max)               │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. Verify Digital Signature                                    │
│    - Check Response signature OR Assertion signature           │
│    - Use IdP public key from metadata                          │
│    - Validate signature algorithm (RSA-SHA256, RSA-SHA384)     │
│    - Reject if signature invalid or missing                    │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. Validate Issuer                                             │
│    - Response.Issuer matches IdP EntityID                      │
│    - Assertion.Issuer matches IdP EntityID                     │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. Validate Destination                                        │
│    - Response.Destination matches SP ACS URL                   │
│    - Assertion.Destination matches SP ACS URL                  │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 5. Validate Audience                                           │
│    - AudienceRestriction contains SP EntityID                  │
│    - Reject if SP not in audience                              │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 6. Validate Time Conditions                                    │
│    - Current time >= Assertion.NotBefore                       │
│    - Current time <= Assertion.NotOnOrAfter                    │
│    - Account for clock skew (60s tolerance)                    │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 7. Decrypt Encrypted Assertion (if applicable)                │
│    - Use SP private key to decrypt                             │
│    - Validate decrypted assertion                              │
└─────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│ 8. Extract User Information                                    │
│    - NameID: User identifier (email format)                    │
│    - Attributes: name, groups, roles, custom claims            │
│    - Map to TMI user schema                                    │
└─────────────────────────────────────────────────────────────────┘
```

### SAML Single Logout Flow

```
┌─────────┐                    ┌─────────────┐                    ┌─────────────┐
│ Client  │                    │ TMI Server  │                    │   Identity  │
│   App   │                    │     (SP)    │                    │   Provider  │
└────┬────┘                    └──────┬──────┘                    └──────┬──────┘
     │                                │                                  │
     │  1. User initiates logout      │                                  │
     │  POST /oauth2/logout           │                                  │
     ├───────────────────────────────►│                                  │
     │                                │                                  │
     │                                │  2. Blacklist JWT token          │
     │                                │  3. Invalidate user sessions     │
     │                                │                                  │
     │                                │  4. Create LogoutRequest         │
     │                                │  - Include NameID from assertion │
     │                                │  - Sign request                  │
     │                                │                                  │
     │  5. Redirect to IdP SLO        │                                  │
     │◄───────────────────────────────┤                                  │
     │  302 https://idp.example.com/  │                                  │
     │      slo?SAMLRequest=base64... │                                  │
     │                                │                                  │
     │  6. Forward to IdP             │                                  │
     ├─────────────────────────────────────────────────────────────────►│
     │                                │                                  │
     │                                │  7. IdP processes logout         │
     │                                │  8. IdP invalidates SSO session  │
     │                                │                                  │
     │  9. POST LogoutResponse        │                                  │
     │◄─────────────────────────────────────────────────────────────────┤
     │  POST /saml/{provider}/slo     │                                  │
     │  SAMLResponse=base64...        │                                  │
     │                                │                                  │
     │  10. Forward to TMI            │                                  │
     ├───────────────────────────────►│                                  │
     │                                │                                  │
     │                                │  11. Validate LogoutResponse     │
     │                                │  - Verify signature              │
     │                                │  - Check InResponseTo matches    │
     │                                │  - Validate Status Success       │
     │                                │                                  │
     │  12. Return success            │                                  │
     │◄───────────────────────────────┤                                  │
     │  {message: "Logout successful"}│                                  │
     │                                │                                  │
```

## Session Lifecycle and JWT Token Handling

### JWT Token Structure

```
JWT Token Anatomy:

Header:
┌─────────────────────────────────────────────────────────────────┐
│ {                                                               │
│   "alg": "HS256",        // Signing algorithm                   │
│   "typ": "JWT",          // Token type                          │
│   "kid": "tmi-key-1"     // Key ID for rotation                 │
│ }                                                               │
└─────────────────────────────────────────────────────────────────┘

Payload (Access Token):
┌─────────────────────────────────────────────────────────────────┐
│ {                                                               │
│   "sub": "google-12345678",          // Subject (provider-id)   │
│   "email": "user@example.com",       // User email              │
│   "name": "John Doe",                // Display name            │
│   "provider": "google",              // OAuth provider          │
│   "groups": ["team-a", "team-b"],    // User groups (optional)  │
│   "iss": "http://localhost:8080",    // Issuer (TMI server)     │
│   "aud": "http://localhost:8080",    // Audience (TMI server)   │
│   "iat": 1640000000,                 // Issued at (Unix time)   │
│   "exp": 1640003600,                 // Expiration (1h later)   │
│   "jti": "uuid-token-identifier"     // JWT ID for blacklisting │
│ }                                                               │
└─────────────────────────────────────────────────────────────────┘

Payload (Refresh Token):
┌─────────────────────────────────────────────────────────────────┐
│ {                                                               │
│   "sub": "google-12345678",          // Subject (provider-id)   │
│   "email": "user@example.com",       // User email              │
│   "type": "refresh",                 // Token type indicator    │
│   "iss": "http://localhost:8080",    // Issuer                  │
│   "aud": "http://localhost:8080",    // Audience                │
│   "iat": 1640000000,                 // Issued at               │
│   "exp": 1642592000,                 // Expiration (30d later)  │
│   "jti": "uuid-refresh-identifier"   // JWT ID for blacklisting │
│ }                                                               │
└─────────────────────────────────────────────────────────────────┘

Signature:
┌─────────────────────────────────────────────────────────────────┐
│ HMACSHA256(                                                     │
│   base64UrlEncode(header) + "." + base64UrlEncode(payload),    │
│   secret_key                                                    │
│ )                                                               │
└─────────────────────────────────────────────────────────────────┘

Complete Token Format:
┌─────────────────────────────────────────────────────────────────┐
│ {base64_header}.{base64_payload}.{signature}                   │
│ eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.                          │
│ eyJzdWIiOiJnb29nbGUtMTIzNDU2NzgiLCJlbWFpbCI6InVzZXJAZXhhbXBs... │
│ SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c                   │
└─────────────────────────────────────────────────────────────────┘
```

### Session State Management

```
Session State Diagram:

┌──────────────┐
│              │  Login (OAuth/SAML)
│ No Session   ├─────────────────────┐
│              │                     │
└──────────────┘                     │
                                     ▼
                              ┌──────────────┐
                              │              │
                              │   Active     │◄──────────────┐
                              │   Session    │               │
                              │              │  Token Refresh│
                              └──────┬───────┘               │
                                     │                       │
                    ┌────────────────┼────────────────┐      │
                    │                │                │      │
         API Request│     Token Expired (1h)  Logout/│      │
                    │                │         Error  │      │
                    ▼                ▼                ▼      │
         ┌──────────────┐  ┌──────────────┐  ┌──────────────┤
         │              │  │              │  │              │
         │   In Use     │  │  Refreshing  │  │  Terminated  │
         │              │  │              │  │              │
         └──────┬───────┘  └──────┬───────┘  └──────────────┘
                │                 │
                │                 │ Refresh Success
                └─────────────────┴──────────────────────────┘

Session Storage in Redis:

┌─────────────────────────────────────────────────────────────────┐
│ Key: session:{user_internal_uuid}                              │
│ Value: {                                                       │
│   "user_id": "google-12345678",                                │
│   "email": "user@example.com",                                 │
│   "name": "John Doe",                                          │
│   "provider": "google",                                        │
│   "groups": ["team-a", "team-b"],                              │
│   "last_activity": "2024-01-01T12:00:00Z",                     │
│   "created_at": "2024-01-01T10:00:00Z"                         │
│ }                                                              │
│ TTL: 30 days (matches refresh token)                           │
└─────────────────────────────────────────────────────────────────┘

Token Blacklist in Redis:

┌─────────────────────────────────────────────────────────────────┐
│ Key: blacklist:{jti}                                           │
│ Value: "1"                                                     │
│ TTL: Token expiration time                                     │
│                                                                │
│ Purpose: Prevent use of explicitly revoked tokens              │
│ Used for: Logout, password change, security events             │
└─────────────────────────────────────────────────────────────────┘
```

### Token Validation Sequence

```
┌─────────┐                          ┌─────────────┐                 ┌──────────┐
│ Client  │                          │ TMI Server  │                 │  Redis   │
│   API   │                          │  Middleware │                 │  Cache   │
└────┬────┘                          └──────┬──────┘                 └────┬─────┘
     │                                      │                             │
     │  1. API Request with Bearer token    │                             │
     │  GET /threat_models                  │                             │
     │  Authorization: Bearer eyJhbGc...    │                             │
     ├─────────────────────────────────────►│                             │
     │                                      │                             │
     │                                      │  2. Extract token from header                          │
     │                                      │  - Validate format: "Bearer {token}"                  │
     │                                      │  - Extract JWT token string                           │
     │                                      │                             │
     │                                      │  3. Parse and verify JWT    │
     │                                      │  - Decode header, payload   │
     │                                      │  - Verify signature with key│
     │                                      │  - Check algorithm is HS256 │
     │                                      │                             │
     │                                      │  4. Validate claims         │
     │                                      │  - iss matches TMI server   │
     │                                      │  - aud matches TMI server   │
     │                                      │  - exp > current time       │
     │                                      │  - iat <= current time      │
     │                                      │                             │
     │                                      │  5. Check token not blacklisted                        │
     │                                      ├────────────────────────────►│
     │                                      │  GET blacklist:{jti}        │
     │                                      │◄────────────────────────────┤
     │                                      │  (nil = not blacklisted)    │
     │                                      │                             │
     │                                      │  6. Extract user from claims│
     │                                      │  - sub: provider user ID    │
     │                                      │  - email: user email        │
     │                                      │  - name: display name       │
     │                                      │  - provider: OAuth provider │
     │                                      │  - groups: user groups      │
     │                                      │                             │
     │                                      │  7. Load full user from DB  │
     │                                      │  - Verify user exists       │
     │                                      │  - Get user permissions     │
     │                                      │  - Check user not disabled  │
     │                                      │                             │
     │                                      │  8. Set user in request context                        │
     │                                      │  - c.Set("user", user)      │
     │                                      │  - c.Set("userEmail", email)│
     │                                      │  - c.Set("userProvider", provider)                     │
     │                                      │                             │
     │                                      │  9. Continue to handler     │
     │                                      │  - User authenticated       │
     │                                      │  - User context available   │
     │                                      │                             │
```

## Error Handling Flows

### OAuth Error Scenarios

```
OAuth Error Flow Diagram:

Provider Authentication Error:
┌─────────┐                    ┌─────────────┐                    ┌─────────────┐
│ Client  │                    │ TMI Server  │                    │   OAuth     │
│   App   │                    │             │                    │  Provider   │
└────┬────┘                    └──────┬──────┘                    └──────┬──────┘
     │                                │                                  │
     │  Redirect to provider          │                                  │
     ├─────────────────────────────────────────────────────────────────►│
     │                                │                                  │
     │                                │  User denies consent             │
     │◄─────────────────────────────────────────────────────────────────┤
     │  302 /oauth2/callback?         │                                  │
     │      error=access_denied       │                                  │
     │      &error_description=User cancelled                            │
     │                                │                                  │
     │  Forward callback              │                                  │
     ├───────────────────────────────►│                                  │
     │                                │                                  │
     │                                │  Detect error parameter          │
     │                                │                                  │
     │  Redirect to client with error │                                  │
     │◄───────────────────────────────┤                                  │
     │  302 http://client/cb#         │                                  │
     │      error=access_denied       │                                  │
     │      &error_description=User cancelled                            │
     │                                │                                  │
     │  Display error to user         │                                  │
     │  "Authentication was cancelled"│                                  │
     │                                │                                  │

Invalid State Error:
┌─────────┐                    ┌─────────────┐                 ┌──────────┐
│ Client  │                    │ TMI Server  │                 │  Redis   │
│   App   │                    │             │                 │  Cache   │
└────┬────┘                    └──────┬──────┘                 └────┬─────┘
     │                                │                             │
     │  Callback with state           │                             │
     ├───────────────────────────────►│                             │
     │  GET /oauth2/callback?         │                             │
     │      code=xyz&state=invalid    │                             │
     │                                │                             │
     │                                │  Lookup state               │
     │                                ├────────────────────────────►│
     │                                │  GET oauth_state:invalid    │
     │                                │◄────────────────────────────┤
     │                                │  (nil - not found)          │
     │                                │                             │
     │  Return error response         │                             │
     │◄───────────────────────────────┤                             │
     │  400 Bad Request               │                             │
     │  {                             │                             │
     │    "error": "invalid_state",   │                             │
     │    "error_description":        │                             │
     │      "State parameter invalid or expired"                    │
     │  }                             │                             │
     │                                │                             │

PKCE Validation Error:
┌─────────┐                    ┌─────────────┐                 ┌──────────┐
│ Client  │                    │ TMI Server  │                 │  Redis   │
│   App   │                    │             │                 │  Cache   │
└────┬────┘                    └──────┬──────┘                 └────┬─────┘
     │                                │                             │
     │  Token exchange request        │                             │
     ├───────────────────────────────►│                             │
     │  POST /oauth2/token            │                             │
     │  {                             │                             │
     │    code: "xyz",                │                             │
     │    code_verifier: "wrong"      │                             │
     │  }                             │                             │
     │                                │                             │
     │                                │  Get stored challenge       │
     │                                ├────────────────────────────►│
     │                                │  GET pkce:xyz               │
     │                                │◄────────────────────────────┤
     │                                │  {code_challenge, method}   │
     │                                │                             │
     │                                │  Compute challenge          │
     │                                │  SHA256(code_verifier)      │
     │                                │  != stored_challenge        │
     │                                │                             │
     │                                │  Delete PKCE record         │
     │                                ├────────────────────────────►│
     │                                │  DEL pkce:xyz               │
     │                                │◄────────────────────────────┤
     │                                │                             │
     │  Return error response         │                             │
     │◄───────────────────────────────┤                             │
     │  400 Bad Request               │                             │
     │  {                             │                             │
     │    "error": "invalid_grant",   │                             │
     │    "error_description":        │                             │
     │      "PKCE verification failed"│                             │
     │  }                             │                             │
     │                                │                             │

Token Expired Error:
┌─────────┐                    ┌─────────────┐
│ Client  │                    │ TMI Server  │
│   App   │                    │             │
└────┬────┘                    └──────┬──────┘
     │                                │
     │  API request with expired token│
     ├───────────────────────────────►│
     │  Authorization: Bearer eyJhbGc...                      │
     │                                │
     │                                │  Validate JWT
     │                                │  - exp < current_time
     │                                │
     │  Return 401 Unauthorized       │
     │◄───────────────────────────────┤
     │  401 Unauthorized              │
     │  {                             │
     │    "error": "token_expired",   │
     │    "error_description":        │
     │      "Access token has expired"│
     │  }                             │
     │                                │
     │  Client detects 401            │
     │  - Attempt token refresh       │
     │  - If refresh fails, redirect to login                 │
     │                                │
```

### Error Response Format

```
OAuth 2.0 Error Response (Query Parameters):

GET /oauth2/callback?
  error=invalid_request
  &error_description=The%20request%20is%20missing%20a%20required%20parameter
  &error_uri=https://docs.example.com/oauth/errors/invalid_request
  &state=abc123

Common OAuth Error Codes:
┌─────────────────────────────────────────────────────────────────┐
│ invalid_request       - Missing or invalid parameters          │
│ invalid_client        - Client authentication failed           │
│ invalid_grant         - Authorization grant invalid/expired    │
│ unauthorized_client   - Client not authorized for grant type   │
│ unsupported_grant_type - Grant type not supported              │
│ invalid_scope         - Requested scope invalid                │
│ access_denied         - User denied authorization              │
│ server_error          - Internal server error                  │
│ temporarily_unavailable - Service temporarily unavailable      │
└─────────────────────────────────────────────────────────────────┘

TMI Custom Error Codes (Fragment):

http://client/callback#
  error=pkce_validation_failed
  &error_description=Code%20verifier%20does%20not%20match%20challenge

TMI-Specific Error Codes:
┌─────────────────────────────────────────────────────────────────┐
│ pkce_validation_failed - PKCE code_verifier mismatch           │
│ invalid_state          - State parameter invalid/expired       │
│ token_expired          - JWT token expired                     │
│ token_blacklisted      - Token has been revoked                │
│ provider_unavailable   - OAuth provider not available          │
│ saml_validation_failed - SAML assertion validation failed      │
└─────────────────────────────────────────────────────────────────┘

API Error Response (JSON):

HTTP/1.1 400 Bad Request
Content-Type: application/json

{
  "error": "invalid_request",
  "error_description": "code_challenge parameter is required for PKCE",
  "error_code": "PKCE_CHALLENGE_MISSING"
}

HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer error="invalid_token",
                  error_description="Token signature validation failed"
Content-Type: application/json

{
  "error": "invalid_token",
  "error_description": "Token signature validation failed"
}
```

## Summary

This document provides comprehensive visual documentation of all authentication flows in TMI:

1. **OAuth 2.0 with PKCE**: Enhanced security flow preventing authorization code interception
2. **Token Refresh**: Automatic token renewal without re-authentication
3. **State Management**: CSRF protection through state parameter validation
4. **Multi-Provider**: Support for Google, GitHub, Microsoft, and test providers
5. **SAML Authentication**: Enterprise SSO with SP-initiated and IdP-initiated flows
6. **Session Lifecycle**: Complete JWT token handling from generation to expiration
7. **Error Handling**: Comprehensive error scenarios and recovery flows

All flows are production-ready, security-hardened, and follow industry best practices including RFC 7636 (PKCE), RFC 6749 (OAuth 2.0), and SAML 2.0 specifications.

## Related Documentation

- [OAuth Integration Setup](../../developer/setup/oauth-integration.md) - Configure OAuth providers
- [Client OAuth Integration](../../developer/integration/client-oauth-integration.md) - Implement OAuth in client applications
- [Architecture Overview](README.md) - System architecture and design patterns
- [API Specification](../apis/tmi-openapi.json) - Complete API documentation
