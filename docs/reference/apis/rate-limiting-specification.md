# TMI API Rate Limiting Specification

**Version:** 1.0.0
**Last Updated:** 2025-11-21
**Status:** Specification (Implementation Pending)

## Overview

The TMI API implements rate limiting to protect against abuse, ensure fair resource allocation, and maintain service availability. Rate limits are applied at multiple scopes depending on the endpoint category and authentication status.

This document provides comprehensive details about the rate limiting strategy documented in the OpenAPI specification via `x-rate-limit` extensions.

## Table of Contents

1. [Rate Limiting Strategy](#rate-limiting-strategy)
2. [Tier Definitions](#tier-definitions)
3. [Multi-Scope Rate Limiting](#multi-scope-rate-limiting)
4. [Configurable Quotas](#configurable-quotas)
5. [Rate Limit Headers](#rate-limit-headers)
6. [Client Integration](#client-integration)
7. [Database Schema](#database-schema)
8. [Implementation Notes](#implementation-notes)

---

## Rate Limiting Strategy

TMI uses a **tiered rate limiting approach** with four distinct tiers:

| Tier | Name | Scope | Configurable | Endpoint Count |
|------|------|-------|--------------|----------------|
| 1 | Public Discovery | IP | No | 5 |
| 2 | Auth Flows | Multi-scope | No | 9 |
| 3 | Resource Operations | User | Yes | 112 |
| 4 | Webhooks | User | Yes | 7 |

### Design Principles

1. **Unauthenticated endpoints** use IP-based rate limiting
2. **Authenticated endpoints** use user-based rate limiting (extracted from JWT subject)
3. **Auth flow endpoints** use multi-scope limiting to balance security and usability
4. **Webhook endpoints** leverage existing database-backed quota system
5. **Configurable limits** allow per-user customization for resource operations and webhooks

---

## Tier Definitions

### Tier 1: Public Discovery

**Applies to:** Unauthenticated endpoints that provide API metadata and discovery information.

**Endpoints:**
- `GET /` - API information
- `GET /.well-known/openid-configuration` - OpenID configuration
- `GET /.well-known/oauth-authorization-server` - OAuth metadata
- `GET /.well-known/jwks.json` - JSON Web Key Set
- `GET /.well-known/oauth-protected-resource` - Protected resource metadata

**Rate Limit Configuration:**
```yaml
scope: ip
tier: public-discovery
limits:
  - type: requests_per_minute
    default: 10
    configurable: false
    tracking_method: Source IP address
```

**Rationale:**
- These endpoints are cacheable and low-cost
- Low limit (10/min) prevents excessive polling
- IP-based tracking is appropriate for unauthenticated access

---

### Tier 2: Auth Flows

**Applies to:** OAuth 2.0 and SAML 2.0 authentication endpoints.

**Endpoints:**
- OAuth: `/oauth2/authorize`, `/oauth2/callback`, `/oauth2/token`, `/oauth2/refresh`, `/oauth2/introspect`
- SAML: `/saml/login`, `/saml/acs`, `/saml/slo` (GET and POST)

**Rate Limit Configuration:**
```yaml
strategy: multi-scope
tier: auth-flows
scopes:
  - name: session
    limits:
      - type: requests_per_minute
        default: 5
        configurable: false
        tracking_method: OAuth state parameter or SAML request ID
  - name: ip
    limits:
      - type: requests_per_minute
        default: 100
        configurable: false
        tracking_method: Source IP address
  - name: user_identifier
    limits:
      - type: attempts_per_hour
        default: 10
        configurable: false
        tracking_method: login_hint parameter or email address
enforcement: Most restrictive limit applies
```

**Multi-Scope Enforcement:**

Auth flow endpoints use **three concurrent rate limit scopes**:

1. **Session Scope** (5 requests/minute)
   - Prevents individual browser sessions from hammering the endpoint
   - Tracked via OAuth `state` parameter or SAML request ID
   - Protects against misconfigured clients or tight retry loops

2. **IP Scope** (100 requests/minute)
   - Prevents DoS from single IP address
   - High limit allows large organizations (corporate NAT, universities)
   - Addresses shared IP concern for multi-user applications

3. **User Identifier Scope** (10 attempts/hour)
   - Prevents credential stuffing attacks on specific accounts
   - Tracked via `login_hint` parameter (OAuth) or email/username (form inputs)
   - Independent of session or IP for maximum protection

**Example Scenarios:**

| Scenario | Session Limit | IP Limit | User Limit | Result |
|----------|---------------|----------|------------|--------|
| Single user, normal login | 1/min | 1/min | 1/hour | ✅ Allowed |
| User refreshing page rapidly | 6/min | 6/min | 6/hour | ❌ Blocked (session limit) |
| Corporate office (100 users) | 1/min each | 100/min total | 1/hour each | ✅ Allowed |
| Attacker trying alice@example.com | 5/min | 5/min | 11/hour | ❌ Blocked (user limit) |
| Distributed botnet | Varies | Varies | 11/hour per user | ❌ Blocked (user limit) |

**Rationale:**
- Single IP limit alone would block legitimate users in shared environments
- Session tracking prevents tight retry loops
- User identifier tracking prevents account takeover attempts
- **Most restrictive limit applies** - any scope hitting its limit blocks the request

---

### Tier 3: Resource Operations

**Applies to:** All authenticated endpoints for threat models, diagrams, users, and collaboration.

**Endpoints:**
- User management: `/users/me`, `/oauth2/userinfo`
- Threat models: `/threat_models/*`
- Diagrams: `/threat_models/{id}/diagrams/*`
- Sub-resources: Assets, threats, documents, notes, repositories, metadata
- Collaboration: `/collaboration/sessions`

**Rate Limit Configuration:**
```yaml
scope: user
tier: resource-operations
limits:
  - type: requests_per_minute
    default: 100
    configurable: true
    quota_source: user_api_quotas
```

**User-Based Tracking:**
- Rate limit applied per JWT subject (user ID)
- Default: 100 requests/minute per user
- **Configurable:** Operators can customize limits per user via database

**Quota Source:**
- Table: `user_api_quotas` (proposed, similar to `webhook_quotas`)
- Schema should include:
  - `user_id` (UUID, primary key, foreign key to users)
  - `max_requests_per_minute` (INT, default 100)
  - `max_requests_per_hour` (INT, default 6000, optional)
  - `created_at`, `modified_at` (timestamps)

**Rationale:**
- 100 req/min supports interactive UI usage and reasonable automation
- User-based tracking ensures fair allocation across all users
- Configurability allows VIP users, integrations, or CI/CD to have higher limits
- Existing pattern from webhook quotas ensures consistency

---

### Tier 4: Webhooks

**Applies to:** Webhook subscription management and delivery history.

**Endpoints:**
- `/webhooks/subscriptions` (GET, POST)
- `/webhooks/subscriptions/{id}` (GET, DELETE)
- `/webhooks/subscriptions/{id}/test` (POST)
- `/webhooks/deliveries` (GET)
- `/webhooks/deliveries/{id}` (GET)

**Rate Limit Configuration:**
```yaml
scope: user
tier: webhooks
limits:
  - type: subscription_requests_per_minute
    default: 10
    configurable: true
    quota_source: webhook_quotas.max_subscription_requests_per_minute
  - type: subscription_requests_per_day
    default: 20
    configurable: true
    quota_source: webhook_quotas.max_subscription_requests_per_day
  - type: events_per_minute
    default: 12
    configurable: true
    quota_source: webhook_quotas.max_events_per_minute
  - type: max_subscriptions
    default: 10
    configurable: true
    quota_source: webhook_quotas.max_subscriptions
```

**Multiple Rate Limits:**

Webhook endpoints enforce **four distinct limits**:

1. **Subscription Requests Per Minute** (10/min)
   - Applies to: POST, DELETE on `/webhooks/subscriptions`
   - Prevents rapid subscription churn

2. **Subscription Requests Per Day** (20/day)
   - Applies to: POST, DELETE on `/webhooks/subscriptions`
   - Prevents subscription quota farming

3. **Events Per Minute** (12/min)
   - Applies to: Webhook event publications (not HTTP API calls)
   - Limits rate of events sent to user's subscriptions

4. **Max Subscriptions** (10 total)
   - Static limit on number of active subscriptions per user
   - Prevents resource exhaustion

**Existing Implementation:**

Webhook rate limiting is **fully implemented**:
- Database table: `webhook_quotas` (see [auth/migrations/002_business_domain.up.sql](../../auth/migrations/002_business_domain.up.sql))
- Rate limiter: [api/webhook_rate_limiter.go](../../../api/webhook_rate_limiter.go)
- Storage: Redis sorted sets for sliding window algorithm
- Tests: [api/webhook_rate_limiter_test.go](../../../api/webhook_rate_limiter_test.go)

**Status:** Rate limiting code exists but not integrated into HTTP handlers yet.

**Rationale:**
- Multiple limits provide granular control over webhook usage
- Database-backed quotas proven effective in implementation
- Configurable limits support different subscription tiers
- Event publication limit prevents webhook spam

---

## Multi-Scope Rate Limiting

### Overview

Multi-scope rate limiting applies **multiple independent rate limits** to a single request, enforcing the **most restrictive limit**. This approach balances security with usability.

### How It Works

For each request to an auth flow endpoint:

1. **Extract identifiers** from request:
   - Session ID (OAuth `state` or SAML request ID)
   - Source IP address
   - User identifier (`login_hint`, email, or username)

2. **Check all scopes** against their respective limits:
   - Session: 5 requests/minute
   - IP: 100 requests/minute
   - User: 10 attempts/hour

3. **Enforce most restrictive limit**:
   - If ANY scope exceeds its limit → Return 429
   - If ALL scopes are under limit → Allow request

4. **Record request** in all applicable scopes

### Tracking Mechanisms

**Session Tracking:**
- OAuth: Extract from `state` query parameter
- SAML: Extract from `SAMLRequest` or `RelayState`
- Lifespan: Typically 5-15 minutes (OAuth spec)
- Storage: Redis sorted set per session ID

**IP Tracking:**
- Source IP from `X-Forwarded-For` (if trusted proxy) or direct connection
- Storage: Redis sorted set per IP address

**User Identifier Tracking:**
- OAuth: `login_hint` query parameter (optional)
- Form login: Username or email field
- Only tracked when identifier is provided
- Storage: Redis sorted set per normalized identifier (lowercase email)

### Redis Key Patterns

```
# Session scope
ratelimit:session:{state_or_request_id}:minute

# IP scope
ratelimit:ip:{ip_address}:minute

# User identifier scope
ratelimit:user:{normalized_email}:hour
```

### Graceful Degradation

If Redis is unavailable:
- **Session and user limits**: Disabled (logs warning)
- **IP limit**: Falls back to in-memory tracking (loses distributed state)
- **Service continues**: Rate limiting disabled to maintain availability

---

## Configurable Quotas

### Overview

Tiers 3 (Resource Operations) and 4 (Webhooks) support **per-user configurable quotas** stored in PostgreSQL. This allows operators to:
- Increase limits for VIP users or integrations
- Implement tiered subscription plans
- Grant higher quotas to CI/CD systems
- Throttle specific users if needed

### Database Schema

#### Webhook Quotas (Existing)

**Table:** `webhook_quotas`

```sql
CREATE TABLE IF NOT EXISTS webhook_quotas (
    owner_id UUID PRIMARY KEY,
    max_subscriptions INT NOT NULL DEFAULT 10,
    max_events_per_minute INT NOT NULL DEFAULT 12,
    max_subscription_requests_per_minute INT NOT NULL DEFAULT 10,
    max_subscription_requests_per_day INT NOT NULL DEFAULT 20,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
);
```

**Location:** [auth/migrations/002_business_domain.up.sql](../../auth/migrations/002_business_domain.up.sql)

#### User API Quotas (Proposed)

**Table:** `user_api_quotas` (to be created)

```sql
CREATE TABLE IF NOT EXISTS user_api_quotas (
    user_id UUID PRIMARY KEY,
    max_requests_per_minute INT NOT NULL DEFAULT 100,
    max_requests_per_hour INT DEFAULT NULL,  -- Optional hourly limit
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Trigger for automatic timestamp updates
CREATE TRIGGER update_user_api_quotas_modified_at
    BEFORE UPDATE ON user_api_quotas
    FOR EACH ROW
    EXECUTE FUNCTION update_modified_at_column();
```

### Quota Retrieval Pattern

Rate limiters follow this pattern (from webhook implementation):

```go
func (rl *RateLimiter) getQuotaOrDefault(userID string) Quota {
    quota, err := rl.quotaStore.Get(userID)
    if err != nil {
        // No custom quota found, use defaults
        return DefaultQuota
    }
    return quota
}
```

### Admin API Endpoints (Proposed)

To manage user quotas, operators would use admin endpoints:

```
GET    /admin/quotas/users/{user_id}        # Get user's quota
PUT    /admin/quotas/users/{user_id}        # Set custom quota
DELETE /admin/quotas/users/{user_id}        # Reset to defaults
GET    /admin/quotas/webhooks/{user_id}     # Get webhook quota
PUT    /admin/quotas/webhooks/{user_id}     # Set webhook quota
```

**Note:** Admin endpoints not yet implemented.

---

## Rate Limit Headers

When a rate limit is enforced, the API returns HTTP 429 with informative headers:

### Response Headers

| Header | Type | Description | Example |
|--------|------|-------------|---------|
| `X-RateLimit-Limit` | Integer | Maximum requests allowed in window | `100` |
| `X-RateLimit-Remaining` | Integer | Requests remaining in current window | `0` |
| `X-RateLimit-Reset` | Integer | Unix timestamp when window resets | `1640000000` |
| `Retry-After` | Integer | Seconds to wait before retrying | `60` |

### Example 429 Response

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1732233600
Retry-After: 45

{
  "code": "rate_limit_exceeded",
  "message": "Rate limit exceeded: 100 requests per minute. Retry after 45 seconds.",
  "details": {
    "limit": 100,
    "window": "minute",
    "retry_after": 45
  }
}
```

### Multi-Scope Headers

For auth flow endpoints with multi-scope limits, headers reflect the **most restrictive scope**:

```http
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 5
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1732233660
Retry-After: 30
X-RateLimit-Scope: session

{
  "code": "rate_limit_exceeded",
  "message": "Rate limit exceeded: 5 requests per minute per session. Retry after 30 seconds.",
  "details": {
    "limit": 5,
    "scope": "session",
    "window": "minute",
    "retry_after": 30
  }
}
```

---

## Client Integration

### Best Practices

1. **Always check rate limit headers** in responses (even 200 OK)
2. **Implement exponential backoff** when receiving 429
3. **Respect Retry-After header** before retrying
4. **Pre-emptively throttle** when `X-RateLimit-Remaining` is low

### Sample Client Code

#### Python

```python
import requests
import time

def make_request_with_retry(url, headers, max_retries=3):
    for attempt in range(max_retries):
        response = requests.get(url, headers=headers)

        if response.status_code == 429:
            retry_after = int(response.headers.get('Retry-After', 60))
            print(f"Rate limited. Waiting {retry_after} seconds...")
            time.sleep(retry_after)
            continue

        # Check remaining quota
        remaining = int(response.headers.get('X-RateLimit-Remaining', 100))
        if remaining < 10:
            print(f"Warning: Only {remaining} requests remaining")

        return response

    raise Exception("Max retries exceeded")
```

#### Go

```go
func makeRequestWithRetry(url string, token string, maxRetries int) (*http.Response, error) {
    client := &http.Client{}

    for attempt := 0; attempt < maxRetries; attempt++ {
        req, _ := http.NewRequest("GET", url, nil)
        req.Header.Set("Authorization", "Bearer " + token)

        resp, err := client.Do(req)
        if err != nil {
            return nil, err
        }

        if resp.StatusCode == 429 {
            retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
            if retryAfter == 0 {
                retryAfter = 60
            }
            log.Printf("Rate limited. Waiting %d seconds...", retryAfter)
            time.Sleep(time.Duration(retryAfter) * time.Second)
            continue
        }

        // Check remaining quota
        remaining, _ := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
        if remaining < 10 {
            log.Printf("Warning: Only %d requests remaining", remaining)
        }

        return resp, nil
    }

    return nil, fmt.Errorf("max retries exceeded")
}
```

#### JavaScript/TypeScript

```typescript
async function makeRequestWithRetry(
    url: string,
    token: string,
    maxRetries: number = 3
): Promise<Response> {
    for (let attempt = 0; attempt < maxRetries; attempt++) {
        const response = await fetch(url, {
            headers: { 'Authorization': `Bearer ${token}` }
        });

        if (response.status === 429) {
            const retryAfter = parseInt(response.headers.get('Retry-After') || '60');
            console.log(`Rate limited. Waiting ${retryAfter} seconds...`);
            await new Promise(resolve => setTimeout(resolve, retryAfter * 1000));
            continue;
        }

        // Check remaining quota
        const remaining = parseInt(response.headers.get('X-RateLimit-Remaining') || '100');
        if (remaining < 10) {
            console.warn(`Warning: Only ${remaining} requests remaining`);
        }

        return response;
    }

    throw new Error('Max retries exceeded');
}
```

---

## Database Schema

### Existing Tables

#### webhook_quotas

See [auth/migrations/002_business_domain.up.sql](../../auth/migrations/002_business_domain.up.sql) for complete schema.

**Purpose:** Store per-user webhook rate limits and subscription quotas.

**Key Fields:**
- `owner_id` - User UUID (primary key)
- `max_subscriptions` - Maximum active subscriptions (default: 10)
- `max_events_per_minute` - Event publication rate (default: 12)
- `max_subscription_requests_per_minute` - API request rate (default: 10)
- `max_subscription_requests_per_day` - Daily API quota (default: 20)

### Proposed Tables

#### user_api_quotas

**Purpose:** Store per-user API rate limits for resource operations.

**Schema:**
```sql
CREATE TABLE IF NOT EXISTS user_api_quotas (
    user_id UUID PRIMARY KEY,
    max_requests_per_minute INT NOT NULL DEFAULT 100,
    max_requests_per_hour INT DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
```

**Indexes:**
```sql
CREATE INDEX idx_user_api_quotas_user_id ON user_api_quotas(user_id);
```

**Trigger:**
```sql
CREATE TRIGGER update_user_api_quotas_modified_at
    BEFORE UPDATE ON user_api_quotas
    FOR EACH ROW
    EXECUTE FUNCTION update_modified_at_column();
```

---

## Implementation Notes

### Current Status

**Implemented:**
- ✅ OpenAPI specification with `x-rate-limit` extensions
- ✅ 429 response component with proper headers
- ✅ Webhook rate limiter (Redis-based sliding window)
- ✅ Webhook quota database storage
- ✅ Comprehensive test coverage for webhook rate limiting

**Not Yet Implemented:**
- ❌ Rate limiting middleware for non-webhook endpoints
- ❌ `user_api_quotas` database table
- ❌ Multi-scope rate limiter for auth flows
- ❌ Integration of webhook rate limiter into HTTP handlers
- ❌ Admin API for quota management
- ❌ Observability/metrics for rate limit hits

### Implementation Roadmap

**Phase 1: Foundation** (Complete)
- ✅ Design rate limiting strategy
- ✅ Document in OpenAPI specification
- ✅ Create comprehensive documentation

**Phase 2: Infrastructure** (Next)
- Create `user_api_quotas` migration
- Implement generic rate limiter middleware
- Add multi-scope rate limiter for auth flows
- Create quota store interface and implementations

**Phase 3: Integration**
- Integrate rate limiting middleware into server
- Hook webhook rate limiter into HTTP handlers
- Add rate limit header injection
- Test end-to-end

**Phase 4: Operations**
- Implement admin API for quota management
- Add Prometheus metrics
- Create operational runbook
- Document quota adjustment procedures

### Technology Stack

**Rate Limiting:**
- **Algorithm:** Sliding window (token bucket alternative)
- **Storage:** Redis sorted sets (ZSET)
- **Key Pattern:** `ratelimit:{scope}:{identifier}:{window}`
- **TTL:** Window duration + 60 seconds buffer

**Database:**
- **Storage:** PostgreSQL
- **Tables:** `webhook_quotas`, `user_api_quotas` (proposed)
- **Access:** Via store interface pattern

**Graceful Degradation:**
- Redis unavailable → Rate limiting disabled, logs warning
- Database unavailable → Falls back to default quotas
- Maintains service availability over strict enforcement

### Performance Considerations

**Redis Operations:**
- Rate limit checks: 2-3 Redis commands (ZREMRANGEBYSCORE, ZCOUNT, ZADD)
- Pipelined for atomicity and performance
- Expected latency: <5ms per check

**Database Queries:**
- Quota lookups cached in-memory (TTL: 60 seconds)
- No database query on every request
- Quota changes take effect within 60 seconds

**Sliding Window Cleanup:**
- Automatic via ZREMRANGEBYSCORE before each check
- TTL ensures old keys are eventually cleaned up
- No separate cleanup job needed

### Security Considerations

**Distributed Attacks:**
- Multi-scope limiting prevents single-vector attacks
- User identifier tracking stops credential stuffing
- IP limiting prevents single-IP DoS

**Quota Bypass:**
- JWT validation ensures user identity
- Redis atomic operations prevent race conditions
- Database foreign key constraints prevent orphaned quotas

**Information Disclosure:**
- Rate limit headers reveal system limits (acceptable for public API)
- Error messages don't expose internal implementation details
- Quota configuration not exposed via user-facing APIs

---

## References

### Related Documentation

- [OpenAPI Specification](./tmi-openapi.json) - Full API specification with rate limit extensions
- [Webhook Rate Limiting](../../../api/webhook_rate_limiter.go) - Implementation reference
- [Webhook Quotas Migration](../../auth/migrations/002_business_domain.up.sql) - Database schema
- [Webhook Configuration](../../operator/webhook-configuration.md) - Operator guide

### Standards and RFCs

- [RFC 6749](https://datatracker.ietf.org/doc/html/rfc6749) - OAuth 2.0 Authorization Framework
- [RFC 6585](https://datatracker.ietf.org/doc/html/rfc6585) - HTTP Status Code 429 (Too Many Requests)
- [IETF Draft: RateLimit Headers](https://datatracker.ietf.org/doc/draft-ietf-httpapi-ratelimit-headers/) - Standard rate limit headers

### Tools and Libraries

- [Redis](https://redis.io/) - In-memory data store for rate limiting
- [go-redis](https://github.com/go-redis/redis) - Go Redis client
- [oapi-codegen](https://github.com/deepmap/oapi-codegen) - OpenAPI code generation

---

## Changelog

### 1.0.0 (2025-11-21)

- Initial specification
- Four-tier rate limiting strategy
- Multi-scope auth flow protection
- Database-backed configurable quotas
- Comprehensive client integration guide
