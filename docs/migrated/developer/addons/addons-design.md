# Add-ons Feature - Design Document

**Version:** 1.1
**Status:** Implementation Complete
**Author:** TMI Development Team
**Date:** 2025-11-08
**Last Updated:** 2026-01-24

> **Note**: This document was the original design specification. The implementation is now complete.
> For user-facing documentation, see the [Addon System wiki page](https://github.com/ericfitz/tmi/wiki/Addon-System).

## Overview

The add-ons feature enables extensibility in TMI through webhook-based invocations. Add-ons are registered by administrators and can be invoked by authenticated users to trigger external services. This design leverages the existing webhook infrastructure while adding user-initiated invocation capabilities.

## Goals

1. **Extensibility**: Enable third-party integrations without modifying TMI core
2. **Admin Control**: Only administrators can register/unregister add-ons
3. **User Access**: All authenticated users can invoke add-ons
4. **Performance**: Use Redis for ephemeral invocation tracking
5. **Security**: HMAC authentication, rate limiting, XSS prevention, SSRF protection (inherited from webhooks)

## Architecture

### Component Overview

```
┌─────────────┐
│   Client    │
│     UI      │
└──────┬──────┘
       │ POST /addons/{id}/invoke
       ▼
┌─────────────────────────────────┐
│   TMI API Server                │
│  ┌──────────────────────────┐   │
│  │ Addon Invocation Handler │   │
│  └────────┬─────────────────┘   │
│           │                     │
│  ┌────────▼──────────┐          │
│  │  Rate Limiter     │          │
│  │  (Redis)          │          │
│  └────────┬──────────┘          │
│           │                     │
│  ┌────────▼──────────────────┐  │
│  │ Invocation Store (Redis)  │  │
│  │ - Create invocation       │  │
│  │ - TTL: 7 days             │  │
│  └────────┬──────────────────┘  │
│           │                     │
│  ┌────────▼──────────────────┐  │
│  │ Addon Invocation Worker   │  │
│  │ - Send HTTP POST          │  │
│  │ - HMAC signature          │  │
│  └────────┬──────────────────┘  │
└───────────┼─────────────────────┘
            │
            │ HTTPS + HMAC
            ▼
┌──────────────────────┐
│  External Webhook    │
│  Service             │
└──────────┬───────────┘
           │ POST /invocations/{id}/status
           │ (HMAC signed)
           ▼
┌──────────────────────────────┐
│  TMI Status Update Handler   │
│  - Verify HMAC signature     │
│  - Update Redis invocation   │
└──────────────────────────────┘
```

## Data Models

### 1. Administrators (Database)

**Table:** `administrators`

> **Implementation Note**: The actual GORM model (`api/models/models.go`) differs from the original design below.
> The current implementation uses `UserInternalUUID` and `GroupInternalUUID` as separate indexed columns
> with a string `ID` primary key, rather than the `subject`/`subject_type` pattern below.

| Column              | Type         | Constraints                           | Description                          |
|---------------------|--------------|---------------------------------------|--------------------------------------|
| id                  | VARCHAR(36)  | PRIMARY KEY                           | Administrator record ID              |
| user_internal_uuid  | VARCHAR(36)  | INDEXED, unique per subject_type      | User's internal UUID (for user-type) |
| group_internal_uuid | VARCHAR(36)  | INDEXED                               | Group's internal UUID (for group-type)|
| subject_type        | VARCHAR(10)  | NOT NULL                              | Type: "user" or "group"              |
| provider            | VARCHAR(100) | NOT NULL                              | OAuth/SAML provider                  |
| granted_at          | TIMESTAMPTZ  | NOT NULL, DEFAULT NOW()               | When admin privilege was granted     |
| granted_by_internal_uuid | VARCHAR(36) | FK to users, NULL allowed         | Who granted admin privilege          |
| notes               | VARCHAR(1000)| NULL                                  | Optional notes about admin grant     |

**Indexes:**
- PRIMARY KEY on `id`
- Composite unique index on (`user_internal_uuid`, `subject_type`)
- Composite unique index on (`group_internal_uuid`, `subject_type`, `provider`)

**Notes:**
- `subject_type = 'user'`: Uses `user_internal_uuid` to reference the user
- `subject_type = 'group'`: Uses `group_internal_uuid` to reference the group
- Provider field allows disambiguation when same user exists across multiple identity providers

### 2. Add-ons (Database)

**Table:** `addons`

| Column            | Type         | Constraints                         | Description                              |
|-------------------|--------------|-------------------------------------|------------------------------------------|
| id                | UUID         | PRIMARY KEY, DEFAULT uuid_v4()      | Add-on identifier                        |
| created_at        | TIMESTAMPTZ  | NOT NULL, DEFAULT NOW()             | Creation timestamp                       |
| name              | VARCHAR(255) | NOT NULL                            | Display name (XSS-safe)                  |
| webhook_id        | UUID         | NOT NULL, FK to webhook_subscriptions CASCADE | Associated webhook                |
| description       | TEXT         | NULL                                | UI description (XSS-safe)                |
| icon              | VARCHAR(60)  | NULL                                | Icon identifier (validated)              |
| objects           | TEXT[]       | NULL                                | TMI object types (UI hint)               |
| threat_model_id   | UUID         | NULL, FK to threat_models           | Scope to specific threat model           |

**Indexes:**
- PRIMARY KEY on `id`
- Index on `webhook_id` (for cascade lookups)
- Index on `threat_model_id` WHERE NOT NULL

**Constraints:**
- CASCADE delete when webhook is deleted
- `icon` validation: FontAwesome or Material Symbols format, max 60 chars
- `objects` validation: each element in allowed taxonomy

### 3. Add-on Invocations (Redis - Ephemeral)

**Key Pattern:** `addon:invocation:{invocation_id}`

**Data Structure (JSON):**
```json
{
  "id": "uuid",
  "addon_id": "uuid",
  "threat_model_id": "uuid",
  "object_type": "asset",          // Optional, must be in addon.objects if provided
  "object_id": "uuid",              // Optional, id of specific object
  "invoked_by": "user_id (uuid)",
  "payload": { /* user data */ },   // Max 1KB
  "status": "pending",              // pending | in_progress | completed | failed
  "status_percent": 0,              // 0-100
  "status_message": "text",         // Optional status description
  "created_at": "RFC3339",
  "status_updated_at": "RFC3339"
}
```

**TTL:** 7 days (604,800 seconds)

**Status State Machine:**
```
pending → in_progress → completed
                      → failed
```

### 4. Add-on Invocation Quotas (Database)

**Table:** `addon_invocation_quotas`

| Column                     | Type    | Constraints                  | Description                           |
|----------------------------|---------|------------------------------|---------------------------------------|
| owner_internal_uuid        | VARCHAR(36) | PRIMARY KEY, FK to users | User quota applies to                 |
| max_active_invocations     | INT     | NOT NULL, DEFAULT 3          | Max concurrent incomplete invocations |
| max_invocations_per_hour   | INT     | NOT NULL, DEFAULT 10         | Rate limit: invocations per hour      |
| created_at                 | TIMESTAMPTZ | NOT NULL, DEFAULT NOW()  | Quota record creation time            |
| modified_at                | TIMESTAMPTZ | NOT NULL, DEFAULT NOW()  | Last modification time                |

**Defaults:**
- 3 active (incomplete) invocations at a time (see `DefaultMaxActiveInvocations` in `api/addon_invocation_quota_store.go`)
- 10 invocations per hour (sliding window)

**Additional Behavior:**
- **Inactivity Timeout**: Invocations are marked `failed` after 15 minutes of inactivity (see `AddonInvocationTimeout` in `api/addon_invocation_store.go`)

### 5. Administrator Configuration (YAML)

**Config File:** `config-development.yml`, `config-production.yml`, `.env.example`

**Structure:**
```yaml
administrators:
  - provider: "google"
    provider_id: "101155414856250184779"  # Provider-assigned user ID
    email: "admin@example.com"
    subject_type: "user"
  - provider: "github"
    provider_id: "12345678"
    email: "admin@github.com"
    subject_type: "user"
```

> **Note**: The actual config format includes `provider`, `provider_id`, and `email` fields
> rather than just `subject` and `subject_type`. This allows matching administrators
> across different OAuth/SAML providers.

**Loading:**
- Parsed on server startup
- Administrators inserted/updated in database via GORM AutoMigrate
- User lookup by provider and provider_id for authorization checks

## Authorization Model

Add-ons have a mixed authorization model to support platform extensibility:

| Operation | Non-Admin Users | Administrators |
|-----------|----------------|----------------|
| **List add-ons** | All add-ons (public visibility) | All add-ons |
| **Get add-on** | All add-ons (public visibility) | All add-ons |
| **Create add-on** | Not allowed | Admin only |
| **Delete add-on** | Not allowed | Admin only |
| **Invoke add-on** | Yes (any add-on) | Yes |
| **List invocations** | Own invocations only | All invocations |
| **Get invocation** | Own invocations only | All invocations |

**Key Points:**

- **Public Discovery**: All authenticated users can list and view add-on details to enable discovery of platform extensions
- **Admin Control**: Only administrators can register and delete add-ons
- **User Invocation**: Any authenticated user can invoke any add-on
- **Invocation Privacy**: Users can only see their own invocation status; administrators can see all invocations
- **Rate Limits**: Apply to all users (including admins) for invocations

**Rationale for Public Visibility:**

Add-ons are designed as discoverable platform extensions. Users need to see available add-ons to invoke them. This is similar to app stores or plugin marketplaces where discovery is public but installation/management is restricted.

## API Specification

### Administrator-Only Endpoints

#### POST /addons
**Create new add-on (Admin only)**

**Request:**
```json
{
  "name": "STRIDE Analysis",
  "webhook_id": "uuid",
  "description": "Performs automated STRIDE threat analysis",
  "icon": "material-symbols:security",
  "objects": ["threat_model", "asset"],
  "threat_model_id": "uuid"  // Optional: scope to specific TM
}
```

**Response:** 201 Created
```json
{
  "id": "uuid",
  "created_at": "2025-11-08T12:00:00Z",
  "name": "STRIDE Analysis",
  "webhook_id": "uuid",
  "description": "Performs automated STRIDE threat analysis",
  "icon": "material-symbols:security",
  "objects": ["threat_model", "asset"],
  "threat_model_id": "uuid"
}
```

**Errors:**
- 400: Invalid icon format, unknown object type, XSS detected
- 401: Not authenticated
- 403: Not an administrator
- 404: Webhook not found

#### DELETE /addons/{addon_id}
**Delete add-on (Admin only)**

**Response:** 204 No Content (success)

**Errors:**
- 401: Not authenticated
- 403: Not an administrator
- 404: Add-on not found
- 409: Cannot delete - active invocations exist

### Authenticated User Endpoints

#### GET /addons
**List all add-ons**

**Query Parameters:**
- `limit`: Number of results (default: 50, max: 500)
- `offset`: Pagination offset (default: 0)
- `threat_model_id`: Filter by threat model (optional)

**Response:** 200 OK
```json
{
  "addons": [
    {
      "id": "uuid",
      "name": "STRIDE Analysis",
      "description": "...",
      "icon": "material-symbols:security",
      "objects": ["threat_model", "asset"],
      "threat_model_id": "uuid",
      "created_at": "2025-11-08T12:00:00Z"
    }
  ],
  "total": 42,
  "limit": 50,
  "offset": 0
}
```

#### GET /addons/{addon_id}
**Get single add-on**

**Response:** 200 OK (same schema as POST response)

**Errors:**
- 401: Not authenticated
- 404: Add-on not found

#### POST /addons/{addon_id}/invoke
**Invoke add-on (trigger webhook)**

**Request:**
```json
{
  "threat_model_id": "uuid",      // Required
  "object_type": "asset",         // Optional, must be in addon.objects
  "object_id": "uuid",            // Optional
  "payload": {                    // Max 1KB total
    "custom_key": "value"
  }
}
```

**Response:** 202 Accepted
```json
{
  "invocation_id": "uuid",
  "status": "pending",
  "created_at": "2025-11-08T12:00:00Z"
}
```

**Errors:**
- 400: Invalid request, payload too large (>1KB), object_type not in addon.objects
- 401: Not authenticated
- 404: Add-on not found
- 429: Rate limit exceeded (1 active or 10/hour quota)

#### GET /invocations
**List invocations**

**Query Parameters:**
- `limit`: Number of results (default: 50, max: 500)
- `offset`: Pagination offset
- `status`: Filter by status (pending, in_progress, completed, failed)
- `addon_id`: Filter by specific add-on

**Authorization:**
- Regular users: Only see own invocations (`invoked_by = current_user`)
- Admins: See all invocations

**Response:** 200 OK
```json
{
  "invocations": [
    {
      "id": "uuid",
      "addon_id": "uuid",
      "threat_model_id": "uuid",
      "object_type": "asset",
      "object_id": "uuid",
      "invoked_by": "uuid",
      "payload": { "key": "value" },
      "status": "in_progress",
      "status_percent": 45,
      "status_message": "Processing assets...",
      "created_at": "2025-11-08T12:00:00Z",
      "status_updated_at": "2025-11-08T12:01:30Z"
    }
  ],
  "total": 5,
  "limit": 50,
  "offset": 0
}
```

#### GET /invocations/{invocation_id}
**Get single invocation**

**Authorization:** Same as GET /invocations (own or admin)

**Response:** 200 OK (same schema as list item)

**Errors:**
- 401: Not authenticated
- 403: Not authorized (not your invocation and not admin)
- 404: Invocation not found or expired

### Anonymous Endpoint (HMAC Auth)

#### POST /invocations/{invocation_id}/status
**Update invocation status (webhook callback)**

**Authentication:** HMAC signature verification using webhook secret

**Request:**
```json
{
  "status": "in_progress",
  "status_percent": 75,
  "status_message": "Analyzing threats..."
}
```

**Response:** 200 OK
```json
{
  "id": "uuid",
  "status": "in_progress",
  "status_percent": 75,
  "status_updated_at": "2025-11-08T12:02:00Z"
}
```

**Errors:**
- 400: Invalid status transition, invalid percent (not 0-100)
- 401: Invalid HMAC signature
- 404: Invocation not found or expired
- 409: Invalid status transition

## Validation Rules

### Icon Validation

**Max Length:** 60 characters

**Material Symbols Format:**
- Pattern: `material-symbols:[a-z]([a-z0-9_]*[a-z0-9])?`
- Rules:
  - Prefix: `material-symbols:`
  - Icon name: snake_case
  - Must start with lowercase letter
  - Can contain: lowercase letters, digits, underscores
  - No consecutive underscores
  - Cannot end with underscore
  - Must end with letter or digit
- Examples:
  - ✅ `material-symbols:security`
  - ✅ `material-symbols:lock_open`
  - ✅ `material-symbols:shield_lock_outline`
  - ❌ `material-symbols:Security` (uppercase)
  - ❌ `material-symbols:lock__open` (double underscore)
  - ❌ `material-symbols:lock_` (trailing underscore)

**FontAwesome Format:**
- Pattern: `fa-[a-z]([a-z]*[a-z])?(\-[a-z]+)? fa-([a-z]+)(-[a-z]+)*`
- Rules:
  - Two parts: style + icon key, separated by space
  - Style: `fa-{style}` where style is lowercase letters with optional single hyphens
  - Icon key: `fa-{icon}` where icon is lowercase letters with hyphens between words
  - No consecutive hyphens
  - Cannot end with hyphen
- Examples:
  - ✅ `fa-solid fa-rocket`
  - ✅ `fa-regular fa-user-shield`
  - ✅ `fa-duotone fa-server-security`
  - ❌ `fa-Solid fa-rocket` (uppercase)
  - ❌ `fa-solid fa-rocket--launch` (double hyphen)
  - ❌ `fa-solid fa-rocket-` (trailing hyphen)

### Objects Taxonomy

**Valid Object Types:**
- `threat_model`
- `diagram`
- `asset`
- `threat`
- `document`
- `note`
- `repository`
- `metadata`

**Validation:**
- Each element in `objects` array must be in taxonomy
- Array can be empty or null (no restrictions)
- Duplicates are allowed but discouraged
- Used as UI hint only (not enforced on invocation)

### XSS Prevention

**Fields requiring XSS validation:**
- `name`: No HTML tags, max 255 chars
- `description`: No script tags, no event handlers, no javascript: URLs

**Validator:** Use existing `no_html_injection` validator from validation registry

**Blocked patterns:**
- `<script>`
- `<iframe>`
- `javascript:`
- Event handlers: `onload=`, `onerror=`, `onclick=`, etc.

### Payload Size Limit

**Max size:** 1 KB (1024 bytes)

**Validation:**
- Measure JSON-serialized size of `payload` field
- Return 400 Bad Request if exceeded
- Error message: "Payload exceeds maximum size of 1024 bytes"

## Rate Limiting

### Invocation Rate Limits

**Per-User Quotas:**
1. **Active invocations:** 1 concurrent incomplete invocation
   - Status: `pending` or `in_progress`
   - Check: Count active invocations in Redis before creating new one

2. **Hourly rate:** 10 invocations per hour
   - Sliding window (3600 seconds)
   - Implementation: Redis sorted set with timestamps
   - Key pattern: `addon:ratelimit:hour:{user_id}`

**Quota Storage:**
- Database table: `addon_invocation_quotas`
- Defaults applied if no record exists
- Admin-configurable per user

**Rate Limit Algorithm (Sliding Window):**
```
1. Current time: now = time.Now().Unix()
2. Window start: start = now - 3600
3. Redis key: "addon:ratelimit:hour:{user_id}"
4. Remove old entries: ZREMRANGEBYSCORE key 0 start
5. Count entries: count = ZCOUNT key start now
6. Check quota: if count >= quota, return 429
7. Add new entry: ZADD key now "{now}:{nano}"
8. Set TTL: EXPIRE key 3660 (window + 60s buffer)
```

**Error Response (429):**

Active invocation limit exceeded:
```json
{
  "error": "rate_limit_exceeded",
  "error_description": "Active invocation limit reached: 3/3 concurrent invocations.",
  "details": {
    "context": {
      "limit": 3,
      "current": 3,
      "retry_after": 542,
      "blocking_invocations": [
        {
          "invocation_id": "abc-123",
          "addon_id": "def-456",
          "status": "in_progress",
          "created_at": "2025-01-15T12:00:00Z",
          "expires_at": "2025-01-15T12:15:00Z",
          "seconds_remaining": 542
        }
      ]
    },
    "suggestion": "Wait for an existing invocation to complete, or retry after 542 seconds when the oldest will timeout."
  }
}
```

Hourly rate limit exceeded:
```json
{
  "error": "rate_limit_exceeded",
  "error_description": "Hourly invocation limit exceeded: 10/10. Retry after 1234 seconds.",
  "details": {
    "context": {
      "retry_after": 1234,
      "limit": 10,
      "current": 10
    }
  }
}
```

## Security

### Administrator Authorization

**Middleware:** `AdministratorMiddleware`

**Flow:**
1. Extract JWT from Authorization header
2. Get user_id and email from JWT claims
3. Get groups from JWT claims (if present)
4. Query administrators table:
   ```sql
   SELECT 1 FROM administrators
   WHERE (subject_type = 'user' AND subject IN (user_id, email))
      OR (subject_type = 'group' AND subject = ANY(groups))
   LIMIT 1
   ```
5. If found: proceed
6. If not found: return 403 Forbidden

**Bootstrap Process:**
1. On server startup, read config file `administrators` array
2. For each entry, insert/update in administrators table
3. Set `granted_by = NULL`, `granted_at = NOW()`
4. Log admin grants at INFO level

### HMAC Signature Verification

**Status Update Endpoint:** POST /invocations/{id}/status

**Process:**
1. Get invocation from Redis by ID
2. Get addon from database by invocation.addon_id
3. Get webhook from database by addon.webhook_id
4. Extract webhook.secret
5. Verify HMAC-SHA256 signature in request header `X-Webhook-Signature`
6. Expected header: `sha256={hex_signature}`
7. Compute: `HMAC-SHA256(webhook.secret, request_body)`
8. Compare: constant-time comparison to prevent timing attacks
9. If valid: proceed with update
10. If invalid: return 401 Unauthorized

**Implementation:** Reuse existing webhook signature verification from `webhook_delivery_worker.go`

### SSRF Protection

**Inherited from Webhooks:**
- All webhook URLs validated against deny list
- HTTPS-only enforcement
- Private IP range blocking (10.x, 172.16-31.x, 192.168.x, 127.x, etc.)
- Cloud metadata endpoint blocking (169.254.169.254, etc.)
- Kubernetes service blocking

**Add-on specific:**
- No additional SSRF concerns (uses existing webhook infrastructure)

## Webhook Integration

### Webhook Payload Structure

**Event Type:** `addon.invoked` (webhook subscription event for add-on invocations)

**HTTP Request:**
```
POST {webhook.url}
Content-Type: application/json
X-Webhook-Signature: sha256={hmac_signature}
X-Invocation-Id: {invocation_id}

{
  "event_type": "addon.invoked",
  "invocation_id": "uuid",
  "addon_id": "uuid",
  "threat_model_id": "uuid",
  "object_type": "asset",      // Optional
  "object_id": "uuid",          // Optional
  "timestamp": "2025-11-08T12:00:00Z",
  "payload": {
    /* User-provided data, max 1KB */
  },
  "callback_url": "https://tmi.example.com/invocations/{invocation_id}/status"
}
```

**Notable Exclusions:**
- No user information sent (privacy/security)
- No addon name (webhook knows what it is)

### Webhook Worker

**Component:** `AddonInvocationWorker` (`api/addon_invocation_worker.go`)

**Responsibilities:**
1. Send HTTP POST to webhook URL (30-second timeout)
2. Compute HMAC-SHA256 signature using webhook secret
3. Handle HTTP response:
   - 200-299 without `X-TMI-Callback: async` header: Auto-complete (set status to `completed`)
   - 200-299 with `X-TMI-Callback: async` header: Set status to `in_progress`, webhook will call back
   - 4xx/5xx: Failure, set status to `failed`
4. Update invocation status in Redis after delivery

> **Note**: Retry logic is **not currently implemented**. The worker makes a single attempt
> and fails immediately on error. The comment "no retries for now" appears in the source code.
> Webhook services should be designed to handle failures gracefully.

**Worker Lifecycle:**
- Started on server startup
- Graceful shutdown on SIGTERM/SIGINT
- Channel-based work queue with 100-invocation buffer

## Add-on Deletion

### Deletion Workflow

**Endpoint:** DELETE /addons/{addon_id}

**Steps:**
1. Verify user is administrator (middleware)
2. Check for active invocations in Redis:
   ```
   SCAN addon:invocation:* MATCH pattern
   Filter by addon_id and status in [pending, in_progress]
   ```
3. If active invocations exist:
   - Return 409 Conflict
   - Message: "Cannot delete add-on '{name}' - {count} active invocations exist"
4. If no active invocations:
   - DELETE FROM addons WHERE id = addon_id
   - Cascade: webhook relationship (no action needed, FK handles it)
5. Return 204 No Content

**Webhook Cascade:**
- When webhook is deleted: ON DELETE CASCADE removes all addons
- No blocking check needed (webhook owner's decision)

**Future Enhancement:**
- Background job to retry blocked deletions
- Check periodically if active invocations completed
- Auto-delete addon when clear

## Redis Schema

### Keys and Data Structures

#### Invocation Data
**Key:** `addon:invocation:{invocation_id}`
**Type:** String (JSON)
**TTL:** 604800 seconds (7 days)
**Value:**
```json
{
  "id": "uuid",
  "addon_id": "uuid",
  "threat_model_id": "uuid",
  "object_type": "asset",
  "object_id": "uuid",
  "invoked_by": "uuid",
  "payload": {},
  "status": "in_progress",
  "status_percent": 50,
  "status_message": "Processing...",
  "created_at": "RFC3339",
  "status_updated_at": "RFC3339"
}
```

#### Active Invocation Tracking
**Key:** `addon:active:{user_id}`
**Type:** String (invocation_id)
**TTL:** 3600 seconds (1 hour)
**Purpose:** Track user's current active invocation for quota enforcement

**Operations:**
- SET when invocation created with status=pending
- DELETE when invocation status → completed/failed
- GET before creating new invocation (quota check)

#### Rate Limit Tracking
**Key:** `addon:ratelimit:hour:{user_id}`
**Type:** Sorted Set
**Score:** Unix timestamp
**Member:** `{timestamp}:{nanosecond}` (for uniqueness)
**TTL:** 3660 seconds (1 hour + 60s buffer)

**Operations:**
- ZREMRANGEBYSCORE: Remove entries older than 1 hour
- ZCOUNT: Count entries in current window
- ZADD: Add new invocation timestamp
- EXPIRE: Reset TTL after each operation

### Redis Operations Summary

| Operation                | Redis Command                                      | Purpose                          |
|--------------------------|---------------------------------------------------|----------------------------------|
| Create invocation        | SET addon:invocation:{id} JSON EX 604800          | Store invocation data            |
| Get invocation           | GET addon:invocation:{id}                         | Retrieve invocation              |
| Update invocation        | SET addon:invocation:{id} JSON EX 604800          | Update status/progress           |
| List user invocations    | SCAN addon:invocation:* + filter by invoked_by    | Pagination support               |
| Track active invocation  | SET addon:active:{user_id} invocation_id EX 3600  | Active quota enforcement         |
| Clear active invocation  | DEL addon:active:{user_id}                        | On completion/failure            |
| Check hourly rate        | ZCOUNT addon:ratelimit:hour:{user_id} start now   | Rate limit check                 |
| Add rate limit entry     | ZADD addon:ratelimit:hour:{user_id} now timestamp | Track new invocation             |
| Clean old rate entries   | ZREMRANGEBYSCORE addon:ratelimit:hour:{user_id} 0 start | Sliding window cleanup    |

## Implementation Order

> **Note**: TMI uses GORM AutoMigrate for schema management. SQL migration files are no longer used.
> Schema is defined in `api/models/models.go` and automatically applied on server startup.

### Phase 1: Foundation
1. Create design document (this file)
2. Create feature branch `feature/addons`
3. Update config files (all YAML + .env.example)
4. Add GORM models to `api/models/models.go` (Administrator, Addon, AddonInvocationQuota)

### Phase 2: Administrator ACL
5. `api/administrator_store.go` - Interface
6. `api/administrator_database_store.go` - PostgreSQL implementation
7. `api/administrator_middleware.go` - JWT + admin check
8. Config loader (integrate into server startup)
9. Unit tests: `api/administrator_*_test.go`

### Phase 3: Add-on Core
10. `api/addon_store.go` - Interface
11. `api/addon_database_store.go` - PostgreSQL implementation
12. `api/addon_validation.go` - Icon, objects, XSS validators
13. `api/addon_handlers.go` - POST/GET/DELETE /addons
14. Unit tests: `api/addon_*_test.go`

### Phase 4: Invocation Storage & Rate Limiting
15. `api/addon_invocation_store.go` - Redis-backed store
16. `api/addon_rate_limiter.go` - Sliding window rate limiter
17. `api/addon_quota_store.go` - Database quota management
18. Unit tests: `api/addon_invocation_*_test.go`

### Phase 5: Invocation Execution
19. `api/addon_invocation_handlers.go` - POST /addons/{id}/invoke, GET /invocations
20. `api/addon_invocation_worker.go` - Async webhook delivery
21. `api/addon_status_handler.go` - POST /invocations/{id}/status (HMAC auth)
22. Integration tests: End-to-end flow

### Phase 6: Deletion & Edge Cases
23. Implement deletion blocking logic in DELETE /addons handler
24. Add cascade handling tests
25. Test invocation expiration (Redis TTL)

### Phase 7: OpenAPI & Documentation
26. Update `docs/reference/apis/tmi-openapi.json`
27. Add schemas: AddonRequest, AddonResponse, InvocationRequest, etc.
28. Write `docs/operator/addons/addon-configuration.md`
29. Write `docs/developer/addons/addon-development-guide.md`
30. Update webhook documentation with addon integration notes

### Phase 8: Testing & Validation
31. Run `make lint` and fix all issues
32. Run `make test-unit` and fix failures
33. Run `make test-integration` and fix failures
34. Manual testing: Full workflow with oauth-client-callback-stub
35. Review and address TODOs/FIXMEs

## Testing Strategy

### Unit Tests

**Administrator ACL:**
- Config parsing with valid/invalid YAML
- Admin check: user by email, by user_id, by group
- Middleware: authorized/unauthorized scenarios

**Add-on Validation:**
- Icon regex: all valid/invalid cases
- Objects taxonomy: valid types, invalid types, empty arrays
- XSS detection: script tags, event handlers, javascript: URLs
- Payload size: exactly 1KB, 1KB+1 byte, empty

**Add-on Store:**
- CRUD operations: create, get, list (pagination), delete
- Webhook cascade: delete webhook → addons deleted
- Constraint violations: non-existent webhook_id

**Rate Limiter:**
- Sliding window: boundary conditions, window edges
- Active invocation tracking: SET/GET/DELETE operations
- Quota enforcement: exactly at limit, over limit, under limit

### Integration Tests

**End-to-End Flow:**
1. Bootstrap admin from config
2. Admin creates add-on
3. User invokes add-on (creates invocation in Redis)
4. Worker sends webhook (mock HTTP server)
5. Webhook updates status via callback
6. User retrieves invocation status
7. Admin deletes add-on (after invocation completes)

**Database Integration:**
- Migration up/down
- Cascade deletes (webhook → addon)
- Foreign key constraints

**Redis Integration:**
- Invocation CRUD with TTL
- Rate limiting with sorted sets
- Active invocation tracking

### Manual Testing

**Tools:**
- OAuth callback stub for user auth
- Mock webhook server (could use webhook.site or local server)
- Redis CLI for inspecting keys

**Scenarios:**
1. Admin workflow: register add-on, delete add-on
2. User workflow: invoke add-on, check status, hit rate limits
3. Webhook workflow: receive invocation, send status updates
4. Edge cases: expired invocations, blocked deletions, invalid HMAC

## Error Handling

### Error Categories

**Client Errors (4xx):**
- 400 Bad Request: Invalid input, validation failures, payload too large
- 401 Unauthorized: Missing/invalid JWT, invalid HMAC signature
- 403 Forbidden: Not an administrator
- 404 Not Found: Add-on/invocation not found, expired
- 409 Conflict: Cannot delete add-on with active invocations, invalid status transition
- 429 Too Many Requests: Rate limit exceeded

**Server Errors (5xx):**
- 500 Internal Server Error: Database/Redis errors, unexpected failures
- 503 Service Unavailable: Redis connection failed, webhook unreachable

### Error Response Format

```json
{
  "error": "error_code",
  "message": "Human-readable description",
  "details": {
    "field": "Additional context"
  }
}
```

### Retry Guidance

**Rate Limits:**
- Include `Retry-After` header (seconds)
- Calculate based on oldest entry in sliding window

**Webhook Failures:**
- Exponential backoff with jitter
- Final failure: set invocation status to "failed"
- Status message includes error details

## Monitoring & Observability

### Metrics to Track

**Add-on Metrics:**
- Total add-ons registered (gauge)
- Add-ons created/deleted (counter)
- Add-ons per webhook (histogram)

**Invocation Metrics:**
- Invocations created (counter)
- Invocations by status (gauge: pending, in_progress, completed, failed)
- Invocation duration (histogram: created_at to completed_at)
- Rate limit violations (counter)

**Webhook Delivery:**
- Webhook invocations sent (counter)
- Webhook failures by status code (counter)
- Webhook retry attempts (histogram)
- HMAC signature failures (counter)

### Logging

**Structured Logging (slogging):**
- Use `slogging.Get()` for all logging
- Include context: invocation_id, addon_id, user_id

**Log Levels:**
- DEBUG: Rate limit checks, Redis operations
- INFO: Add-on created/deleted, invocation created, status updated
- WARN: Rate limit exceeded, invalid HMAC, deletion blocked
- ERROR: Database failures, Redis failures, webhook unreachable

**Example:**
```go
logger.Info("Add-on invoked: addon_id=%s, invocation_id=%s, user=%s",
    addonID, invocationID, userID)
```

## Future Enhancements

### Potential Additions

1. **Invocation Logs:** Store detailed webhook request/response logs
2. **Webhook Replay:** Allow users to retry failed invocations
3. **Add-on Marketplace:** Public registry of community add-ons
4. **Scoped Permissions:** Add-ons require specific permissions (read assets, write threats, etc.)
5. **Batch Invocations:** Invoke add-on on multiple objects at once
6. **Scheduled Invocations:** Cron-style periodic invocation
7. **Invocation History:** Long-term storage beyond 7-day Redis TTL
8. **Admin API:** Manage administrators via REST (POST/DELETE /administrators)
9. **Quota Management UI:** Adjust per-user quotas via API
10. **Webhook Templates:** Pre-configured add-on templates for common integrations

### Non-Goals (Out of Scope)

- Synchronous add-on execution (always async via webhooks)
- Add-on sandboxing/code execution within TMI (external webhooks only)
- Built-in add-on marketplace or discovery
- Add-on versioning or A/B testing
- Complex workflow orchestration (single invocation → single webhook call)

## Appendix

### Icon Validation Regex

**Material Symbols:**
```regex
^material-symbols:[a-z]([a-z0-9_]*[a-z0-9])?$
```

**FontAwesome:**
```regex
^fa-[a-z]([a-z]*[a-z])?(\-[a-z]+)? fa-([a-z]+)(-[a-z]+)*$
```

**Combined Validator (Go):**
```go
func ValidateIcon(icon string) error {
    if len(icon) > 60 {
        return errors.New("icon exceeds maximum length of 60 characters")
    }

    materialPattern := regexp.MustCompile(`^material-symbols:[a-z]([a-z0-9_]*[a-z0-9])?$`)
    faPattern := regexp.MustCompile(`^fa-[a-z]([a-z]*[a-z])?(\-[a-z]+)? fa-([a-z]+)(-[a-z]+)*$`)

    if materialPattern.MatchString(icon) || faPattern.MatchString(icon) {
        return nil
    }

    return errors.New("icon must be valid Material Symbols or FontAwesome format")
}
```

### TMI Object Taxonomy

Complete list of valid object types for `objects` field:

1. `threat_model` - Root threat modeling workspace
2. `diagram` - Data Flow Diagrams (DFD)
3. `asset` - Assets with sensitivity levels
4. `threat` - Security threats with severity/likelihood
5. `document` - Reference documents and links
6. `note` - Markdown notes
7. `repository` - Source code repositories
8. `metadata` - Key-value pairs on any object

### Redis Key Patterns

All Redis keys used by add-ons feature:

```
addon:invocation:{invocation_id}        # Invocation data (JSON, TTL: 7 days)
addon:active:{user_id}                  # Active invocation ID (String, TTL: 1 hour)
addon:ratelimit:hour:{user_id}          # Hourly rate limit (Sorted Set, TTL: 1h+1m)
```

### Database Schema Summary

> **Note**: This SQL is illustrative. Actual schema is managed by GORM AutoMigrate from `api/models/models.go`.
> Column names may use snake_case in database but the authoritative source is the GORM model.

```sql
-- Administrators (actual GORM schema differs - see api/models/models.go)
CREATE TABLE administrators (
    id VARCHAR(36) PRIMARY KEY,
    user_internal_uuid VARCHAR(36),
    group_internal_uuid VARCHAR(36),
    subject_type VARCHAR(10) NOT NULL,
    provider VARCHAR(100) NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    granted_by_internal_uuid VARCHAR(36),
    notes VARCHAR(1000)
);

-- Add-ons
CREATE TABLE addons (
    id VARCHAR(36) PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    name VARCHAR(256) NOT NULL,
    webhook_id VARCHAR(36) NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    description VARCHAR(1024),
    icon VARCHAR(60),
    objects TEXT,  -- JSON array stored as text
    threat_model_id VARCHAR(36) REFERENCES threat_models(id) ON DELETE CASCADE
);

-- Invocation Quotas
CREATE TABLE addon_invocation_quotas (
    owner_internal_uuid VARCHAR(36) PRIMARY KEY REFERENCES users(internal_uuid) ON DELETE CASCADE,
    max_active_invocations INT NOT NULL DEFAULT 3,
    max_invocations_per_hour INT NOT NULL DEFAULT 10,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## Verification Summary

*Verified on: 2026-01-24*

### Items Verified Against Source Code

| Item | Status | Source |
|------|--------|--------|
| Addon table schema | Verified | `api/models/models.go` lines 640-658 |
| Administrator table schema | Verified (updated) | `api/models/models.go` lines 609-628 |
| AddonInvocationQuota table schema | Verified (updated) | `api/models/models.go` lines 670-684 |
| Default max active invocations = 3 | Verified | `api/addon_invocation_quota_store.go` line 15 |
| Default max invocations per hour = 10 | Verified | `api/addon_invocation_quota_store.go` line 16 |
| Invocation TTL = 7 days | Verified | `api/addon_invocation_store.go` line 44 |
| Inactivity timeout = 15 minutes | Verified | `api/addon_invocation_store.go` line 47 |
| Redis key patterns | Verified | `api/addon_invocation_store.go`, `api/addon_rate_limiter.go` |
| Icon validation regex | Verified | `api/addon_validation.go` lines 28, 34 |
| Object types taxonomy | Verified | `api/addon_validation.go` lines 12-21 |
| HMAC signature format | Verified | `api/addon_invocation_worker.go` lines 258-262 |
| OpenAPI endpoints exist | Verified | `docs/reference/apis/tmi-openapi.json` |
| Schema validation tables | Verified | `internal/dbschema/validator.go` lines 68-71 |

### Corrections Made

1. **Administrator schema**: Updated to reflect GORM model with `UserInternalUUID`, `GroupInternalUUID`, and `Provider` columns
2. **Admin config format**: Updated YAML example to show `provider`, `provider_id`, and `email` fields
3. **Default active invocations**: Changed from 1 to 3 per actual implementation
4. **Added inactivity timeout**: 15 minutes for invocations
5. **Webhook worker**: Noted that retry logic is not implemented
6. **Error response format**: Updated to show actual detailed error structure with blocking invocations
7. **Database schema**: Updated SQL examples to match GORM-generated schema
8. **Implementation notes**: Added notes about GORM AutoMigrate replacing SQL migrations

### Items Not Verified (External References)

| Item | Status | Notes |
|------|--------|-------|
| Material Symbols icon library | Not verified | External Google resource |
| FontAwesome icon library | Not verified | External FontAwesome resource |

---

**Document Version History:**

| Version | Date       | Changes                                    |
|---------|------------|--------------------------------------------|
| 1.0     | 2025-11-08 | Initial design document                    |
| 1.1     | 2026-01-24 | Verified against implementation, corrected schema and config format, updated defaults |
