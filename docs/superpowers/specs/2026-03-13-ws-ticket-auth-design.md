# WebSocket Ticket Authentication (AUTH-VULN-007 Remediation)

**Date**: 2026-03-13
**Issue**: [#173](https://github.com/ericfitz/tmi/issues/173)
**Status**: Approved

## Problem

The tmi-ux client cannot establish WebSocket connections because the server lacks the `GET /ws/ticket` endpoint. The client was updated (PR tmi-ux#454) to replace direct JWT token passing in WebSocket URLs (`?token=<jwt>`) with a short-lived ticket/nonce pattern, per AUTH-VULN-007 remediation. JWT tokens in WebSocket URLs are a security risk — they appear in browser history, server logs, proxy logs, and referrer headers.

## Solution

Implement a ticket-based WebSocket authentication flow:

1. Client authenticates via REST to obtain a short-lived, single-use ticket
2. Client passes the ticket as a query parameter on the WebSocket URL
3. Server validates and consumes the ticket during WebSocket upgrade
4. The old `?token=` JWT parameter is removed entirely (no backward compatibility)

## Scope

This design covers diagram collaboration WebSockets only (`/threat_models/{id}/diagrams/{id}/ws`). The notification WebSocket at `/ws/notifications` is out of scope — it currently authenticates via JWT context set by the JWT middleware (Bearer header or cookie on the HTTP upgrade request), which does not expose tokens in URLs. A future issue can migrate it to ticket-based auth if desired.

## Design

### 1. Ticket Store

A new `TicketStore` interface with dual implementations (in-memory for tests, Redis for dev/prod). The design pattern follows `auth.StateStore` (interface + dual implementation + cleanup goroutine), but the store lives in `api/` since it is consumed exclusively by the API server and WebSocket hub, not the auth service.

**Location**: `api/ticket_store.go`

**Interface**:

```go
type TicketStore interface {
    IssueTicket(ctx context.Context, userID, provider, sessionID string, ttl time.Duration) (string, error)
    ValidateTicket(ctx context.Context, ticket string) (userID, provider, sessionID string, err error)
}
```

**Ticket properties**:
- 32-byte cryptographically random value (crypto/rand), base64url-encoded
- 30-second TTL
- Single-use: consumed (deleted) on validation
- Scoped to a specific user and collaboration session
- Stores `provider` alongside `userID` so that the WebSocket middleware can look up the full user record via `GetUserByProviderID(provider, userID)` — the same lookup path used by JWT claims extraction

**In-memory implementation** (`InMemoryTicketStore`):
- `map[string]*ticketEntry` with `sync.RWMutex`
- Periodic cleanup goroutine for expired tickets
- `Close()` method on the concrete type (not the interface) to stop the cleanup goroutine and prevent leaks, matching the `InMemoryStateStore` pattern
- Used in tests

**Redis implementation** (`RedisTicketStore`):
- Key format: `ws_ticket:<token>`
- Value: JSON `{"user_id": "...", "provider": "...", "session_id": "..."}`
- Redis TTL handles expiry automatically
- **Atomic single-use validation**: Use Redis `GETDEL` command (Redis 6.2+) for atomic get-and-delete. This prevents a race condition where two concurrent requests could both read the ticket before either deletes it.
- Used in dev/prod

### 2. OpenAPI Spec & Endpoint

**New endpoint**: `GET /ws/ticket`

Using GET despite the side effect (ticket creation) because: (a) the tmi-ux client already implements this as a GET request, (b) the ticket value is returned in the response body not the URL, and (c) it's semantically a "get me a ticket" operation. The response includes `Cache-Control: no-store` to prevent intermediate caching.

- **Query parameter**: `session_id` (required, UUID format) — the collaboration session the ticket is scoped to
- **Authentication**: JWT (Bearer header or HttpOnly cookie), via existing JWT middleware
- **Response headers**: `Cache-Control: no-store`
- **Response 200**: `{"ticket": "<base64url-token>"}`
- **Response 400**: Missing or invalid `session_id`
- **Response 401**: Unauthenticated
- **Response 404**: Session not found or user is not a participant
- **Rate limiting**: Covered by the existing API rate limiter which applies to all OpenAPI-routed endpoints

**Handler location**: `api/ws_ticket_handler.go`

**Handler logic** (`Server.GetWsTicket`):
1. Extract authenticated user ID from JWT context
2. Validate `session_id` refers to an active collaboration session
3. Validate the authenticated user is a participant (or host) of that session
4. Call `TicketStore.IssueTicket(ctx, userID, provider, sessionID, 30*time.Second)`
5. Return `{"ticket": "<token>"}` with `Cache-Control: no-store`

**Server wiring**:
- `TicketStore` added as a field on the `Server` struct
- Initialized in `cmd/server/main.go`: `RedisTicketStore` in prod, `InMemoryTicketStore` in tests

### 3. WebSocket Handshake Validation

Replace the existing `?token=` JWT-based WebSocket authentication with ticket-only validation for diagram collaboration WebSockets.

**Route conflict mitigation**: The `TokenExtractor.ExtractToken` method in `cmd/server/jwt_auth.go` uses `strings.HasPrefix(c.Request.URL.Path, "/ws/")` to detect WebSocket paths and apply WebSocket-specific token extraction. The new `GET /ws/ticket` endpoint is a REST call that should use normal JWT extraction (Bearer header / cookie). The WebSocket path detection must be updated to exclude `/ws/ticket` so it falls through to the standard JWT extraction flow.

**Changes to `cmd/server/jwt_auth.go`** (`TokenExtractor.ExtractToken`):

The current WebSocket branch (lines 28-39) checks for `?token=` then falls back to cookie. Changes:
1. Exclude `/ws/ticket` from the WebSocket path detection (so it uses normal JWT auth)
2. For actual WebSocket paths: extract `?ticket=` query parameter
3. Validate via `TicketStore.ValidateTicket(ctx, ticket)` — returns `userID` and `sessionID`
4. Cross-check the `sessionID` from the ticket against `?session_id=` on the WebSocket URL
5. No ticket or invalid ticket → 401

**User identity enrichment**: The ticket stores only `user_id` and `session_id`. After ticket validation, the middleware must look up the full user record from the database by `user_id` to populate context variables needed by the WebSocket handler (`userEmail`, `userDisplayName`, `userProvider`, `userGroups`, etc.). This mirrors the `fetchAndSetUserObject` pattern already used by the JWT claims extractor.

The ticket store must be accessible from the middleware. Pass via Gin context or package-level reference.

**Key security properties**:
- Ticket is single-use — replay is impossible
- Ticket is session-scoped — cannot be used for a different session
- 30s TTL — minimal exposure window
- No JWT in URL — eliminates the original AUTH-VULN-007 vulnerability

### 4. AsyncAPI Spec Update

Update `tmi-asyncapi.yml`:
- Remove the `?token=<jwt>` security scheme documentation
- Document the `?ticket=<nonce>` authentication pattern
- Document that tickets are obtained via `GET /ws/ticket?session_id={uuid}`
- Document ticket properties: single-use, 30s TTL, session-scoped

### 5. Testing

**Unit tests** (`api/ticket_store_test.go`):
- Issue and validate a ticket (happy path)
- Ticket is single-use (second validation fails)
- Expired ticket is rejected
- Invalid/unknown ticket is rejected
- Session ID mismatch between ticket and WebSocket query param is rejected

**Unit tests** (`api/ws_ticket_handler_test.go`):
- `GET /ws/ticket` returns ticket for valid session + authenticated user
- 401 for unauthenticated request
- 404 for nonexistent session
- 404 for session where user is not a participant
- Missing/invalid `session_id` parameter returns 400

**Integration tests**:
- Update existing WebSocket collaboration tests to use the ticket flow instead of `?token=`

### 6. wstest CLI Tool

Update [wstest/](../../wstest/) to use the new ticket flow:
- After creating/joining a collaboration session, call `GET /ws/ticket?session_id={uuid}` to obtain a ticket
- Pass `?ticket=<token>` instead of `?token=<jwt>` on the WebSocket URL

## Connection Flow (After Implementation)

```
Client                          Server
  |                               |
  |  POST .../collaborate         |
  |------------------------------>|  Create/retrieve session
  |  201 {session_id, ws_url}     |
  |<------------------------------|
  |                               |
  |  GET /ws/ticket?session_id=X  |
  |------------------------------>|  Validate user is participant
  |  200 {ticket: "<nonce>"}      |  Issue 30s single-use ticket
  |<------------------------------|
  |                               |
  |  WS .../ws?session_id=X       |
  |     &ticket=<nonce>           |
  |------------------------------>|  Validate ticket (consume)
  |  101 Switching Protocols      |  Cross-check session_id
  |<----------------------------->|  Look up user by ID for context
  |       WebSocket open          |
```

## Files Changed

| File | Change |
|------|--------|
| `api/ticket_store.go` | New: TicketStore interface + InMemoryTicketStore (with Close()) |
| `api/ticket_store_redis.go` | New: RedisTicketStore (using GETDEL for atomicity) |
| `api/ticket_store_test.go` | New: Unit tests for both implementations |
| `api/ws_ticket_handler.go` | New: GetWsTicket handler |
| `api/ws_ticket_handler_test.go` | New: Handler unit tests |
| `api/server.go` | Add TicketStore field to Server struct |
| `api-schema/tmi-openapi.json` | Add GET /ws/ticket endpoint |
| `api/api.go` | Regenerated from OpenAPI spec |
| `cmd/server/main.go` | Wire up TicketStore (Redis in prod) |
| `cmd/server/jwt_auth.go` | Exclude /ws/ticket from WS detection; replace ?token= with ?ticket= validation; add user lookup after ticket validation |
| `api-schema/tmi-asyncapi.yml` | Update security scheme |
| `wstest/` | Update to use ticket flow |
| Existing integration tests | Update to use ticket flow |
