# Webhook Delivery Unification Design

**Issue:** #194 (Phase 1), new issue (Phases 2-3)
**Date:** 2026-03-29
**Status:** Approved

## Problem

The current webhook infrastructure maintains two distinct delivery paths:

1. **Resource-change events** flow through Redis Streams → `WebhookEventConsumer` → Postgres-backed `WebhookDeliveryWorker` with retry logic
2. **Addon invocations** flow through `AddonInvocationWorker` with direct HTTP sends, Redis-backed state, bidirectional callbacks, but no retries

These paths use different headers (`X-Webhook-Delivery-Id` vs `X-Invocation-Id` + `X-Addon-Id`), different payload shapes (`EventPayload` vs `AddonInvocationPayload`), different storage backends (Postgres vs Redis), and different delivery semantics. This makes the system harder to understand, extend, and maintain.

## Design Decisions

- **Unified envelope**: All webhook deliveries use the same JSON payload shape
- **Unified headers**: All deliveries use the same HTTP headers; `X-Invocation-Id` and `X-Addon-Id` are removed
- **Redis-only delivery state**: All delivery records move to Redis with TTL-based expiry (no Postgres)
- **Generalized callbacks**: Any webhook delivery can use the `X-TMI-Callback: async` response header and the status callback endpoint, not just addon invocations
- **Single delivery pipeline**: Addon invocations emit events to Redis Streams and flow through the same delivery worker as resource-change events
- **Delivery ID = Invocation ID**: For addon invocations, the `X-Webhook-Delivery-Id` serves as the invocation identifier; no separate invocation ID concept

## Unified Webhook Payload Envelope

All webhook deliveries use this JSON envelope:

```json
{
  "event_type": "threat_model.updated",
  "threat_model_id": "...",
  "timestamp": "2026-03-29T12:00:00Z",
  "object_type": "threat_model",
  "object_id": "...",
  "data": { ... }
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `event_type` | yes | From the `WebhookEventType` enum |
| `threat_model_id` | yes | All events are threat-model-scoped |
| `timestamp` | yes | ISO8601 |
| `object_type` | no | Type of object acted on |
| `object_id` | no | UUID of that object |
| `data` | yes | Event-specific payload |

For addon invocations, `data` contains:

```json
{
  "addon_id": "...",
  "user_data": { ... }
}
```

For resource-change events, `data` contains resource-specific fields (unchanged from current behavior).

The callback URL is not included in the payload — it follows a well-known pattern: `{base_url}/webhook-deliveries/{delivery_id}/status`, where the delivery ID is in the `X-Webhook-Delivery-Id` header.

## Unified Headers

All webhook deliveries use these headers:

| Header | Required | Description |
|--------|----------|-------------|
| `Content-Type` | yes | `application/json` |
| `User-Agent` | yes | `TMI-Webhook/1.0` |
| `X-Webhook-Event` | yes | Event type |
| `X-Webhook-Delivery-Id` | yes | UUID of this delivery |
| `X-Webhook-Subscription-Id` | yes | UUID of the subscription |
| `X-Webhook-Signature` | if secret configured | HMAC-SHA256 signature |

**Removed headers:**
- `X-Invocation-Id` — replaced by `X-Webhook-Delivery-Id`
- `X-Addon-Id` — moved into payload `data.addon_id`

**Response header (now available for all deliveries):**
- `X-TMI-Callback: async` — webhook receiver sets this to indicate it will call back with status updates

## Unified Delivery Model (Redis)

All delivery state is stored in Redis, replacing both the Postgres `webhook_deliveries` table and the Redis-based `AddonInvocation` store.

**Redis key pattern:** `webhook:delivery:{delivery_id}`

**Delivery record fields:**

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID | Delivery ID |
| `subscription_id` | UUID | Webhook subscription |
| `event_type` | string | e.g., `threat_model.updated`, `addon.invoked` |
| `status` | string | `pending`, `in_progress`, `delivered`, `failed` |
| `status_percent` | int | 0-100, for callback progress reporting |
| `status_message` | string | Human-readable status from callback |
| `payload` | string | JSON payload sent to webhook |
| `attempts` | int | Delivery attempt count |
| `next_retry_at` | timestamp | When to retry next |
| `last_error` | string | Last failure reason |
| `created_at` | timestamp | When delivery was created |
| `delivered_at` | timestamp | When successfully delivered |
| `last_activity_at` | timestamp | Last callback or delivery attempt |

**Addon-specific fields** (only populated for `addon.invoked` events):

| Field | Type | Description |
|-------|------|-------------|
| `addon_id` | UUID | The addon that was invoked |
| `invoked_by_uuid` | UUID | User who invoked |
| `invoked_by_email` | string | User email |
| `invoked_by_name` | string | User display name |

**TTLs:**
- `pending` / `in_progress`: 4 hours (stale backstop)
- `failed`: 7 days
- `completed` / `delivered`: 7 days

TTLs refresh on status updates. The cleanup worker marks `in_progress` deliveries as `failed` after 15 minutes of inactivity (same as current addon timeout). The 4-hour TTL is a backstop for anything the cleanup worker misses.

## API Endpoints

**New endpoints:**

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/webhook-deliveries/{id}` | JWT (owner/invoker/admin) or HMAC | Get delivery status |
| `POST` | `/webhook-deliveries/{id}/status` | HMAC | Update delivery status (callback) |

`GET /webhook-deliveries/{id}` supports dual authentication:
- **JWT**: Subscription owner, addon invoker, or admin can view
- **HMAC**: Webhook receiver can check its own delivery status using the subscription's secret

`POST /webhook-deliveries/{id}/status` accepts:

```json
{
  "status": "in_progress | completed | failed",
  "status_percent": 45,
  "status_message": "Processing threats..."
}
```

**Modified endpoints:**
- `POST /addons/{id}/invoke` — creates unified delivery record, emits event to Redis Streams
- `GET /admin/webhooks/deliveries` — repointed to Redis
- `GET /admin/webhooks/deliveries/{delivery_id}` — repointed to Redis

**Removed endpoints:**
- `POST /invocations/{id}/status` — replaced by `POST /webhook-deliveries/{id}/status`
- `GET /invocations/{id}` — replaced by `GET /webhook-deliveries/{id}`
- `GET /invocations` — removed (admin list endpoint covers this use case)

## Delivery Pipeline

**Unified flow for all events:**

1. **Event emission**: Resource handlers and `POST /addons/{id}/invoke` both emit events to Redis Streams via `GlobalEventEmitter.EmitEvent()`
2. **Event consumption**: `WebhookEventConsumer` reads from the stream, matches subscriptions, creates delivery records in Redis
3. **Delivery**: Unified delivery worker sends HTTP POST with standard headers and envelope payload
4. **Response handling**:
   - 2xx + `X-TMI-Callback: async` → status `in_progress`, await callbacks
   - 2xx without → status `delivered`
   - Non-2xx → retry with exponential backoff (1, 5, 15, 30 min, max 5 attempts), then `failed`
5. **Callbacks**: Webhook receiver can POST to `/webhook-deliveries/{id}/status` for any delivery type
6. **Cleanup**: Single cleanup worker handles stale `in_progress` deliveries (15 min inactivity timeout)

**Addon invocation pre-processing** (in `POST /addons/{id}/invoke`, before emitting):
- Validation, rate limiting, deduplication — unchanged
- Create delivery record in Redis with addon-specific fields
- Emit `addon.invoked` event to Redis Streams with the delivery ID

## Naming Changes

All references to `invocation_id` are renamed to `delivery_id` across:
- Go source code (structs, variables, function names)
- OpenAPI specification (`api-schema/tmi-openapi.json`)
- Documentation in the wiki (`/Users/efitz/Projects/tmi/tmi.wiki`)
- Any remaining docs in the repository

## Phasing

### Phase 1: Unified payload and headers on addon invocations

Scope: Original #194 issue. Modify `AddonInvocationWorker` to use the unified envelope and header set.

- Change `AddonInvocationPayload` to match the unified envelope
- Remove `X-Invocation-Id` and `X-Addon-Id` headers
- Add `X-Webhook-Delivery-Id` header (using invocation ID as the value)
- Change `User-Agent` from `TMI-Addon-Worker/1.0` to `TMI-Webhook/1.0`
- Rename `invocation_id` → `delivery_id` across code, OpenAPI spec, docs, and wiki
- Update existing tests

Note: The existing `EventPayload` struct uses `ResourceID`/`ResourceType` field names internally. Phase 1 only changes the addon invocation payload shape and headers. The `EventPayload` struct fields are renamed to `ObjectID`/`ObjectType` in Phase 2 when the payloads merge.

Shippable on its own. Closes #194.

### Phase 2: Unified Redis delivery model and migrate addon invocations

Scope: Build the new delivery store, new endpoints, and migrate addon invocations onto the unified pipeline.

- Create `WebhookDeliveryRedisStore` with unified delivery record
- Add `POST /webhook-deliveries/{id}/status` endpoint (replaces `POST /invocations/{id}/status`)
- Add `GET /webhook-deliveries/{id}` endpoint with JWT+HMAC dual auth (replaces `GET /invocations/{id}`)
- Remove `GET /invocations` list endpoint
- Modify `POST /addons/{id}/invoke` to create unified delivery records and emit to Redis Streams
- Remove `AddonInvocationWorker` and `AddonInvocationStore`
- Route `addon.invoked` events through `WebhookEventConsumer` → unified delivery worker
- Add `X-TMI-Callback: async` handling to delivery worker
- Update cleanup worker to handle unified deliveries
- Update OpenAPI spec, tests, docs, wiki

### Phase 3: Migrate resource-change deliveries from Postgres to Redis

Scope: Move the remaining delivery path onto the unified Redis-backed model.

- Repoint `WebhookEventConsumer` to create Redis delivery records instead of Postgres
- Repoint admin endpoints to read from Redis
- Remove `GormWebhookDeliveryStore` and `webhook_deliveries` Postgres table
- Add migration to drop the Postgres table
- Update tests

## Future Considerations

Nothing in this design blocks further evolution:
- Addon invocation pre-processing (callback setup, Redis record creation) happens before event emission, so the event stream only sees standard events
- The unified envelope and headers mean any new event type slots in without delivery infrastructure changes
- Retry logic applies uniformly; event-type-specific retry policies could be added later if needed
