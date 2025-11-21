# Rate Limiting OpenAPI Changes Summary

**Date:** 2025-11-21
**Changed By:** Claude Code
**Change Type:** Security Enhancement - OpenAPI Specification Update

## Overview

Updated the TMI OpenAPI specification to document comprehensive rate limiting strategy addressing security scan findings from ratemyopenapi.com. This is a **documentation-only change** - no server implementation modifications were made.

## Security Scan Results

### Before Changes
- **Score:** 79/100
- **Issues:**
  - 161 rate limiting violations
  - 264 missing 429 responses
  - Lack of documented rate limit policies

### After Changes (Expected)
- **Score:** 95+/100
- **Fixed:**
  - All 133 operations now have `x-rate-limit` extensions
  - All 133 operations now have 429 response definitions
  - Comprehensive rate limiting documentation

## Changes Made

### 1. Added Reusable 429 Response Component

**Location:** `components.responses.TooManyRequests`

**Includes:**
- Standard rate limit headers (X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Retry-After)
- Example error response with rate limit details
- Clear description of rate limit exceeded condition

### 2. Added Rate Limit Extensions to All Operations

**Total Operations Updated:** 133

**Distribution by Tier:**
| Tier | Name | Operations | Configurable |
|------|------|-----------|--------------|
| 1 | Public Discovery | 5 | No |
| 2 | Auth Flows | 9 | No |
| 3 | Resource Operations | 112 | Yes |
| 4 | Webhooks | 7 | Yes |

**Extension Format:**

Each operation now includes an `x-rate-limit` extension documenting:
- Scope (ip, user, or multi-scope)
- Tier classification
- Rate limits with defaults
- Configurability status
- Tracking methods
- Quota sources (for configurable limits)

### 3. Added 429 Responses to All Operations

**Operations Updated:** 133 (100% coverage)

Each operation now includes:
```yaml
responses:
  "429":
    $ref: "#/components/responses/TooManyRequests"
```

### 4. Created Comprehensive Documentation

**New Documentation:**
- [rate-limiting-specification.md](./rate-limiting-specification.md) - 500+ line comprehensive guide

**Contents:**
- Four-tier rate limiting strategy
- Multi-scope auth flow protection
- Configurable quota system
- Database schema proposals
- Client integration examples (Python, Go, JavaScript)
- Implementation roadmap
- Security considerations

## Rate Limiting Strategy

### Tier 1: Public Discovery (5 endpoints)
- **Scope:** IP-based
- **Limit:** 10 requests/minute per IP
- **Rationale:** Low-cost cacheable endpoints

### Tier 2: Auth Flows (9 endpoints)
- **Scope:** Multi-scope (session + IP + user identifier)
- **Limits:**
  - 5 requests/minute per session
  - 100 requests/minute per IP
  - 10 attempts/hour per user identifier
- **Rationale:** Balances security with shared IP environments (corporate NAT, universities)

### Tier 3: Resource Operations (112 endpoints)
- **Scope:** User-based (JWT subject)
- **Limit:** 100 requests/minute per user
- **Configurable:** Yes (via proposed `user_api_quotas` table)
- **Rationale:** Fair allocation, supports interactive UIs and reasonable automation

### Tier 4: Webhooks (7 endpoints)
- **Scope:** User-based
- **Limits:**
  - 10 subscription requests/minute
  - 20 subscription requests/day
  - 12 events/minute
  - 10 max subscriptions
- **Configurable:** Yes (via existing `webhook_quotas` table)
- **Rationale:** Leverages existing database-backed quota system

## Technical Details

### Scripts Created

**1. `/tmp/add-rate-limit-extensions.jq`**
- Classifies endpoints by path and method
- Assigns appropriate rate limit tier
- Adds `x-rate-limit` extension with tier-specific configuration

**2. `/tmp/add-429-responses.jq`**
- Adds 429 response reference to all HTTP operations
- Preserves existing responses
- Idempotent (won't duplicate if run multiple times)

### Files Modified

**Primary Change:**
- `docs/reference/apis/tmi-openapi.json` (483 KB)

**Backup Created:**
- `docs/reference/apis/tmi-openapi.json.YYYYMMDD_HHMMSS.backup`

**New Files:**
- `docs/reference/apis/rate-limiting-specification.md`
- `docs/reference/apis/rate-limiting-changes-summary.md` (this file)

### Validation

**OpenAPI Validation:** ✅ Passed
```
make validate-openapi
✅ Validation successful - no issues found!
```

**Coverage Verification:**
- 133/133 operations have `x-rate-limit` extensions (100%)
- 133/133 operations have 429 responses (100%)
- All tiers properly categorized

## Implementation Status

### Completed (Specification Only)
- ✅ OpenAPI `x-rate-limit` extensions
- ✅ 429 response components and references
- ✅ Comprehensive documentation
- ✅ Client integration examples
- ✅ Database schema proposals

### Not Implemented (Server-Side)
- ❌ Rate limiting middleware
- ❌ Multi-scope rate limiter for auth flows
- ❌ `user_api_quotas` database table
- ❌ Admin API for quota management
- ❌ Rate limit header injection
- ❌ Webhook handler integration (code exists but not connected)

### Existing Implementation
- ✅ Webhook rate limiter (Redis-based, tested)
- ✅ Webhook quota database storage
- ⚠️ Not yet integrated into HTTP handlers

## Next Steps

To complete rate limiting implementation:

1. **Create `user_api_quotas` migration**
   - Based on existing `webhook_quotas` pattern
   - Add indexes and triggers

2. **Implement generic rate limiting middleware**
   - Create middleware that reads `x-rate-limit` from OpenAPI spec
   - Support multi-scope checking for auth flows
   - Inject rate limit headers into responses

3. **Integrate webhook rate limiter**
   - Connect existing `WebhookRateLimiter` to HTTP handlers
   - Remove "not yet implemented" stubs

4. **Add admin API**
   - Endpoints for quota management
   - RBAC protection (admin-only)

5. **Add observability**
   - Prometheus metrics for rate limit hits
   - Grafana dashboards
   - Alerting for quota exhaustion

6. **Re-scan with security tool**
   - Submit updated spec to ratemyopenapi.com
   - Verify score improvement to 95+

## Database Schema Proposals

### user_api_quotas (New Table)

```sql
CREATE TABLE IF NOT EXISTS user_api_quotas (
    user_id UUID PRIMARY KEY,
    max_requests_per_minute INT NOT NULL DEFAULT 100,
    max_requests_per_hour INT DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_user_api_quotas_user_id ON user_api_quotas(user_id);

CREATE TRIGGER update_user_api_quotas_modified_at
    BEFORE UPDATE ON user_api_quotas
    FOR EACH ROW
    EXECUTE FUNCTION update_modified_at_column();
```

### webhook_quotas (Existing)

Already implemented in `auth/migrations/005_webhooks.up.sql`.

## Multi-Scope Rate Limiting Design

### Problem Solved

Single IP-based rate limiting for auth flows would block legitimate users in shared environments:
- Corporate offices behind NAT
- University campuses
- Mobile carrier NAT
- Public WiFi

### Solution

Three concurrent rate limit scopes, enforcing **most restrictive limit**:

1. **Session Scope** (5/min) - Prevents tight retry loops
2. **IP Scope** (100/min) - Allows large organizations, blocks DoS
3. **User Identifier Scope** (10/hour) - Prevents credential stuffing

### Example Scenarios

| Scenario | Result |
|----------|--------|
| Single user, normal login | ✅ Allowed |
| User refreshing rapidly | ❌ Blocked (session limit) |
| Corporate office (100 users) | ✅ Allowed (high IP limit) |
| Credential stuffing attack | ❌ Blocked (user identifier limit) |
| Distributed botnet | ❌ Blocked (user identifier limit) |

## Client Integration

### Required Client Changes

Clients should:
1. Check `X-RateLimit-Remaining` header in all responses
2. Respect `Retry-After` header in 429 responses
3. Implement exponential backoff
4. Pre-emptively throttle when quota is low

### Example Code Provided

Documentation includes complete examples in:
- Python (requests library)
- Go (net/http)
- JavaScript/TypeScript (fetch API)

## Testing Recommendations

### OpenAPI Spec Validation
```bash
make validate-openapi
```

### Re-scan with Security Tool
```bash
# Upload updated spec to ratemyopenapi.com
# Expected score: 95+/100
```

### Manual Verification
```bash
# Count operations by tier
jq -r '.paths | to_entries[] | .value | to_entries[] |
  select(.key | test("^(get|post|put|patch|delete)$")) |
  .value."x-rate-limit".tier // .value."x-rate-limit".strategy' \
  docs/reference/apis/tmi-openapi.json | sort | uniq -c

# Verify 429 responses
jq -r '.paths | to_entries[] | .value | to_entries[] |
  select(.key | test("^(get|post|put|patch|delete)$")) |
  select(.value.responses."429") | "HAS_429"' \
  docs/reference/apis/tmi-openapi.json | wc -l
```

## Breaking Changes

**None.** This is a documentation-only change to the OpenAPI specification. No API behavior changes.

## Rollback Procedure

If needed, restore from backup:
```bash
# Find latest backup
ls -lt docs/reference/apis/tmi-openapi.json.*.backup | head -1

# Restore (replace timestamp)
cp docs/reference/apis/tmi-openapi.json.YYYYMMDD_HHMMSS.backup \
   docs/reference/apis/tmi-openapi.json
```

## References

- Security scan report: https://ratemyopenapi.com/report/6dbbef53-a1f5-426e-80a8-52aeaee9407f
- [Rate Limiting Specification](./rate-limiting-specification.md)
- [OpenAPI Specification](./tmi-openapi.json)
- [Webhook Rate Limiter Implementation](../../../api/webhook_rate_limiter.go)
- [RFC 6585 - HTTP Status Code 429](https://datatracker.ietf.org/doc/html/rfc6585)

## Changelog

### 2025-11-21

**Added:**
- `x-rate-limit` extensions to all 133 operations
- 429 response component with rate limit headers
- 429 response references to all operations
- Comprehensive rate limiting documentation
- Client integration examples
- Database schema proposals
- Implementation roadmap

**Modified:**
- `docs/reference/apis/tmi-openapi.json` (483 KB)

**Created:**
- `docs/reference/apis/rate-limiting-specification.md`
- `docs/reference/apis/rate-limiting-changes-summary.md`
- `/tmp/add-rate-limit-extensions.jq`
- `/tmp/add-429-responses.jq`
