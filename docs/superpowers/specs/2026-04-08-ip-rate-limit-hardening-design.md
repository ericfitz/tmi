# IP Rate Limiting Hardening Design

**Issue:** [#235](https://github.com/ericfitz/tmi/issues/235) - Complete IP-based rate limiting for public discovery endpoints
**Date:** 2026-04-08
**Branch:** dev/1.4.0

## Summary

The IP rate limiting middleware for Tier 1 (public discovery endpoints) is implemented but needs verification, integration tests, configurable rate limits, and trusted proxy support. This spec covers hardening the existing code without structural changes.

## Scope

- Add `TMI_TRUSTED_PROXIES` config for proxy-aware IP extraction
- Add `TMI_RATELIMIT_PUBLIC_RPM` config for configurable rate limits
- Add integration tests using real Redis (via `make test-integration`)
- Add TODO comments for future metrics and structured logging
- Update wiki documentation

## Configuration Changes

### New Config Fields

Two new fields in `ServerConfig` (`internal/config/config.go`):

| Field | YAML Key | Env Var | Type | Default | Description |
|-------|----------|---------|------|---------|-------------|
| `TrustedProxies` | `trusted_proxies` | `TMI_TRUSTED_PROXIES` | `[]string` | _(empty)_ | Comma-separated trusted proxy CIDRs/IPs |
| `RateLimitPublicRPM` | `ratelimit_public_rpm` | `TMI_RATELIMIT_PUBLIC_RPM` | `int` | `10` | Requests per minute per IP for public discovery endpoints |

### Trusted Proxy Behavior

- **When `TMI_TRUSTED_PROXIES` is set:** Call `gin.Engine.SetTrustedProxies()` at startup. Gin's `c.ClientIP()` then validates the `X-Forwarded-For` chain against the trusted list, preventing IP spoofing.
- **When empty (default):** Current behavior preserved. The middleware extracts IPs directly from `X-Forwarded-For` / `X-Real-IP` / `RemoteAddr` without validation. This is acceptable as defense-in-depth (rate limits are lenient at 10 req/min).

### Configurable Rate Limit

- Replace the hardcoded `10` in `IPRateLimitMiddleware` with the value from `RateLimitPublicRPM`.
- The `APIServer` struct already holds the config; pass the value through to the `IPRateLimiter`.
- Default of `10` preserves current behavior when the env var is not set.

## Middleware Changes

### IP Extraction

Update `extractIPAddress()` to be proxy-configuration-aware:

- **When trusted proxies are configured:** Use `c.ClientIP()` directly (Gin validates the `X-Forwarded-For` chain against the trusted proxy list and returns the correct client IP). Skip manual header parsing.
- **When no trusted proxies configured (default):** Keep the current behavior — manually extract from `X-Forwarded-For` first, then `X-Real-IP`, then fall back to `c.ClientIP()`.

The function needs a way to know whether trusted proxies are configured. Pass this as a boolean from the `APIServer` (which holds the config) rather than re-reading config per request. The simplest approach: add a `trustedProxiesConfigured bool` field to `APIServer`, set at startup, and pass it to `extractIPAddress()` or make `extractIPAddress` a method on `APIServer`.

### Rate Limit Value

The `IPRateLimiter` currently has the limit hardcoded. Change it to accept the configured value from `RateLimitPublicRPM` at initialization time.

### TODO Comments

Add in the 429 response path of `IPRateLimitMiddleware`:

```go
// TODO: emit structured log event with IP, endpoint, and remaining count on rate limit block
// TODO: emit rate_limit_blocked metric counter with labels {tier: "public-discovery", ip: extractedIP}
```

## Integration Tests

Tests run as part of `make test-integration` with a real Redis instance.

### Test Cases

| # | Test | Verifies |
|---|------|----------|
| 1 | Send 11 requests to `/.well-known/openid-configuration`, verify 11th returns 429 with headers | Rate limiting enforced on public endpoints |
| 2 | Send requests to `GET /` beyond rate limit, verify none return 429 | Health check excluded |
| 3 | Send requests to authenticated endpoint beyond IP limit, verify no 429 from IP limiter | Non-public endpoints pass through |
| 4 | Check `X-RateLimit-Limit` and `X-RateLimit-Remaining` on 200 responses | Rate limit headers on success |
| 5 | Stop Redis, send request to public endpoint, verify 200 (not 429/500), restart Redis | Fail-open graceful degradation |
| 6 | Send requests with different `X-Forwarded-For` values, verify independent rate limiting | IP extraction works correctly |
| 7 | Start server with `TMI_RATELIMIT_PUBLIC_RPM=5`, verify blocking at 6th request | Configurable rate limit applied |

### Test Infrastructure

- Uses the same build tags and Redis instance as existing integration tests
- Each test flushes relevant Redis keys before running to avoid cross-test pollution
- Test 5 (Redis unavailable) uses a temporary Redis client disconnect or invalid Redis URL

## Wiki Documentation Updates

### Configuration-Reference.md

Add a new "### Rate Limiting" subsection after "### Server Settings":

| Variable | Default | Description |
|----------|---------|-------------|
| `TMI_TRUSTED_PROXIES` | _(none)_ | Comma-separated trusted proxy CIDRs/IPs for X-Forwarded-For validation |
| `TMI_RATELIMIT_PUBLIC_RPM` | 10 | Requests per minute per IP for public discovery endpoints (Tier 1) |

With explanatory note on trusted proxy behavior.

### API-Rate-Limiting.md

1. In Tier 1 definition, change `configurable: false` to `configurable: true` and add `env_var: TMI_RATELIMIT_PUBLIC_RPM`
2. Add "Trusted Proxy Configuration" subsection under Implementation Notes explaining `TMI_TRUSTED_PROXIES` and its effect on IP extraction

## Files Changed

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `TrustedProxies` and `RateLimitPublicRPM` to `ServerConfig` |
| `cmd/server/main.go` | Call `SetTrustedProxies()` when config is set; pass RPM to rate limiter |
| `api/ip_and_auth_rate_limit_middleware.go` | Use configured RPM instead of hardcoded 10; add TODO comments |
| `api/ip_rate_limiter.go` | Accept configurable limit value |
| `api/*_integration_test.go` | New integration tests (7 test cases) |
| `api/rate_limit_middleware_test.go` | Update existing unit tests for configurable limit |
| Wiki: `Configuration-Reference.md` | Add Rate Limiting section |
| Wiki: `API-Rate-Limiting.md` | Update Tier 1 configurability, add trusted proxy docs |

## Out of Scope

- Metrics emission (TODO comments only)
- Structured logging for rate limit events (TODO comments only)
- Changes to Tier 2 auth flow rate limiting (separate issue #234)
- Per-user configurable IP rate limits (Tier 1 is global)
