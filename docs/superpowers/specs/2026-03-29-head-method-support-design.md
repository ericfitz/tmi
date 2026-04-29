# HEAD Method Support for GET Endpoints (RFC 9110)

**Issue:** [#76](https://github.com/ericfitz/tmi/issues/76)
**Date:** 2026-03-29
**Status:** Draft

## Context

RFC 9110 Section 9.3.2 recommends that servers SHOULD support HEAD requests for all resources that support GET. TMI currently returns 405 Method Not Allowed for all HEAD requests because the OpenAPI validation middleware rejects methods not defined in the spec.

HEAD requests are useful for bandwidth optimization (checking resource existence without transferring body), health checks, cache validation, and conditional requests. Adding support makes TMI a better HTTP citizen and improves client compatibility.

## Approach: Middleware Interception

A single middleware (`HeadMethodMiddleware`) converts HEAD requests to GET before the OpenAPI validator sees them, wraps the response writer to suppress the body, and lets the entire existing handler pipeline process the request normally.

**Why middleware over other approaches:**
- **Not OpenAPI spec + codegen:** Adding HEAD to all 107 endpoints would double the spec's path entries, massively increase generated code, and require 107 boilerplate handler stubs.
- **Not dynamic route registration:** Coupling to Gin router internals is fragile and would still need OpenAPI validator bypass logic.
- **Middleware is ~50 lines**, requires no spec changes, no codegen changes, and no handler modifications.

## Scope

**107 GET endpoints** receive HEAD support. **4 protocol endpoints excluded** because they initiate side-effect flows (redirects, token exchange, SSO), making HEAD semantically inappropriate per RFC 9110's requirement that HEAD be safe and idempotent:

| Endpoint | Reason for Exclusion |
|----------|---------------------|
| `GET /oauth2/authorize` | Initiates OAuth flow, redirects to IdP |
| `GET /oauth2/callback` | Processes authorization code, exchanges for tokens |
| `GET /saml/{provider}/login` | Initiates SAML SSO |
| `GET /saml/slo` | Processes SAML logout |

## Architecture

### Middleware Placement

```
... JWT auth (main.go:854) ...
... Rate limiting (main.go:857) ...
... EnumNormalizer (main.go:867) ...
→ HeadMethodMiddleware (NEW, ~main.go:868)
... OpenAPI validator (main.go:874) ← now sees GET
... Entity middleware (main.go:878-882)
... Route handlers (main.go:906)
```

All upstream middleware already handles HEAD correctly:
- `MethodNotAllowedHandler` includes HEAD in `validMethods` (uuid_validation_middleware.go:128)
- `ContentTypeValidationMiddleware` skips non-body methods (unicode_validation_middleware.go:94-98)
- `UnicodeNormalizationMiddleware` explicitly skips HEAD (unicode_validation_middleware.go:24)
- `StrictJSONValidationMiddleware` skips HEAD
- `BoundaryValueValidationMiddleware` skips HEAD
- JWT auth works on any method (checks Authorization header)

The only component that rejects HEAD is the OpenAPI validator — so we convert just before it.

### Request Flow

```
HEAD /threat_models HTTP/1.1
Authorization: Bearer <token>

1. Middleware chain runs (auth, rate limit, etc.) — sees HEAD, passes through
2. HeadMethodMiddleware:
   a. Detects HEAD method
   b. Path "/threat_models" not in exclusion list
   c. Sets c.Request.Method = "GET"
   d. Wraps c.Writer with headResponseWriter
   e. Calls c.Next()
3. OpenAPI validator sees GET /threat_models — valid, passes through
4. Handler executes fully (queries DB, assembles response)
5. Handler calls c.JSON(200, data):
   - WriteHeader(200) → flows through to real writer (status recorded)
   - Header("Content-Type", "application/json") → flows through
   - Write(jsonBytes) → headResponseWriter counts bytes, discards them
6. HeadMethodMiddleware unwinds:
   - Restores c.Request.Method = "HEAD" (for logging)
   - Sets Content-Length if not already set by handler
7. Client receives: 200 OK, Content-Type, Content-Length, empty body
```

### Writer Wrapping Interaction

The `JSONErrorHandler` (main.go:499) wraps `c.Writer` with `bufferedResponseWriter` early in the chain. Our `headResponseWriter` wraps that:

```
Real gin.ResponseWriter
  └── bufferedResponseWriter (from JSONErrorHandler)
        └── headResponseWriter (from HeadMethodMiddleware)
```

This works correctly:
- Body writes hit `headResponseWriter.Write()` → counted and discarded, never reach `bufferedResponseWriter.body`
- `WriteHeader` calls flow through `headResponseWriter` (not overridden) → reach `bufferedResponseWriter.WriteHeader` → stores status code
- When `JSONErrorHandler` unwinds, `bufferedResponseWriter.body` is empty → writes empty body to real writer

## Components

### 1. New File: `api/head_method_middleware.go`

**`headResponseWriter`** struct — embeds `gin.ResponseWriter`:
- `Write(b []byte) (int, error)` — counts `len(b)`, returns `len(b), nil` (reports success, discards bytes)
- `WriteString(s string) (int, error)` — counts `len(s)`, returns `len(s), nil`
- `Size() int` — returns counted bytes (overrides Gin's default)
- `Written() bool` — returns `bodyBytes > 0`
- Does NOT override `WriteHeader` — delegates naturally to embedded writer

**Exclusion list** — segment-based pattern matcher:
- Paths split by `/`, compared segment-by-segment
- `*` matches any single segment (for `/saml/*/login`)
- Exclusion patterns computed once at init, not per-request:
  - `["oauth2", "authorize"]`
  - `["oauth2", "callback"]`
  - `["saml", "*", "login"]`
  - `["saml", "slo"]`

**`HeadMethodMiddleware()` function:**
1. If not HEAD → `c.Next()`, return
2. If HEAD and path matches exclusion list → `c.Next()`, return (stays HEAD, will get 405 from OpenAPI validator)
3. Save original writer: `origWriter := c.Writer`
4. `c.Request.Method = http.MethodGet`
5. Wrap `c.Writer` with `headResponseWriter`
6. `c.Next()`
7. Restore `c.Writer = origWriter` (prevents headResponseWriter from being used by unwinding middleware)
8. `c.Request.Method = http.MethodHead` (restore for logging)
9. If `Content-Length` header absent → set to counted bytes

### 2. Modify: `api/openapi_middleware.go`

In `getAllowedMethodsForPath` (~line 320): when `pathItem.Get != nil`, also append `"HEAD"` to the methods list. This ensures 405 responses include HEAD in the `Allow` header for GET-capable paths.

### 3. Modify: `cmd/server/main.go`

Insert `r.Use(api.HeadMethodMiddleware())` at ~line 868, between `EnumNormalizerMiddleware` and the OpenAPI validator setup.

## Performance Note

HEAD requests pay the full handler computation cost (DB queries, serialization) because correct response headers (Content-Length, Content-Type, ETag) require knowing what the body would be. The bandwidth savings (no body over wire) is the primary benefit. Per-handler optimization to skip body construction can be done incrementally later if profiling shows specific endpoints are hot paths for HEAD.

## Testing

### Unit Tests: `api/head_method_middleware_test.go`

| # | Test Case | Assertion |
|---|-----------|-----------|
| 1 | HEAD on normal endpoint | 200, empty body, correct Content-Length |
| 2 | HEAD on excluded `/oauth2/authorize` | Method stays HEAD, not converted |
| 3 | HEAD on excluded `/saml/okta/login` | Pattern match works, stays HEAD |
| 4 | GET request | Passes through unmodified, body present |
| 5 | POST request | Passes through unmodified |
| 6 | HEAD preserves error status codes | Handler returns 404 → HEAD gets 404 |
| 7 | HEAD preserves response headers | Custom headers appear in response |
| 8 | Content-Length fallback | Set when handler doesn't set it |
| 9 | `matchesExcludedPath` | Exact, wildcard, non-match, different segment counts |

### Integration Tests

New workflow in `test/integration/`:
| # | Test Case | Setup |
|---|-----------|-------|
| 1 | HEAD on `GET /` | No auth needed (public) |
| 2 | HEAD on `GET /threat_models` | Authenticated |
| 3 | HEAD on non-existent path | 404 |
| 4 | HEAD without auth on protected endpoint | 401 |

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `api/head_method_middleware.go` | NEW | Middleware, response writer, exclusion logic |
| `api/head_method_middleware_test.go` | NEW | Unit tests |
| `cmd/server/main.go` | MODIFY | Insert middleware at ~line 868 |
| `api/openapi_middleware.go` | MODIFY | Add HEAD to Allow header |
| `test/integration/workflows/head_method_test.go` | NEW | Integration tests |

## Verification

1. `make lint` — passes
2. `make build-server` — compiles
3. `make test-unit` — all tests pass including new HEAD tests
4. `make test-integration` — HEAD integration tests pass
5. Manual verification:
   ```bash
   make start-dev
   curl -I http://localhost:8080/                    # 200, no body
   curl -I http://localhost:8080/threat_models       # 401 (no auth)
   # With auth token:
   curl -I -H "Authorization: Bearer <token>" http://localhost:8080/threat_models  # 200, no body, Content-Length present
   curl -I http://localhost:8080/oauth2/authorize     # 405 (excluded)
   ```
