# Design: Out-of-band audit alert sink via webhook event type (#395)

**Issue:** [#395](https://github.com/ericfitz/tmi/issues/395) — feat(security): out-of-band audit alert sink for /admin/* writes (T7 follow-up)
**Date:** 2026-06-11
**Status:** Approved

## Problem

T7 mitigation layer 2: a hostile insider with the TMI admin role must not be able to suppress
the audit signal from inside TMI. #355 writes the in-band `system_audit_entries` row; this
design pushes every such event to an external destination outside the TMI process boundary.

## Decisions

1. **Reuse the webhook pipeline via a new event type** (user direction). The existing chain —
   `EventEmitter` (Redis Stream, `api/events.go`) → `WebhookEventConsumer`
   (`api/webhook_event_consumer.go`) → `WebhookDeliveryWorker`
   (`api/webhook_delivery_worker.go`: SafeHTTPClient, HMAC signing, 1/5/15/30-minute retries ×5
   attempts, per-target circuit breaker, queryable delivery records) — carries the alert. No
   new transport, no SMTP (none exists in TMI).
2. **Protection = "both":** an **operator-pinned subscription** satisfies T7 (no admin API can
   modify, delete, or read its URL), and ordinary **admin-managed subscriptions** may also
   subscribe to `system_audit.*` events as convenience feeds.
3. **Durability is Redis-grade, accepted:** ~51 minutes of retry backoff covers transient sink
   outages (the issue's requirement); failed deliveries remain queryable 7 days; a Redis wipe
   loses queued alerts, but the in-band DB audit row (tamper-protected by #400) remains the
   durable record.
4. **Generic JSON payload only in v1** (standard webhook envelope). Slack's `{"text":...}`
   shape needs a relay or Slack Workflow webhook; a per-subscription format knob is future
   work if demanded.

## Design

### 1. Event type — `api/events.go`

```go
// System audit events (T7, #395)
EventSystemAuditAdminWrite = "system_audit.admin_write"
```

Payload: standard `EventPayload` envelope with `Data` carrying the audit record —
`entry_id` (deep-links to `GET /admin/audit/system/{entry_id}`, #398), `actor_email`,
`actor_provider`, `actor_display_name`, `http_method`, `http_path`, `field_path`,
`old_value_redacted`, `new_value_redacted`, `change_summary`, plus `operator_name` from
`OperatorConfig` so multi-instance fleets can attribute alerts.

The event type is added to the OpenAPI `WebhookEventType` enum occurrences →
`make validate-openapi` + `make generate-api`.

### 2. Emission point — decorator on the audit repository

A thin decorator (`api/system_audit_alerting.go`) wraps `SystemAuditRepository`:

```go
Create(ctx, entry) → inner.Create(ctx, entry); on success → GlobalEventEmitter emit
                     EventSystemAuditAdminWrite (non-fatal: emit errors are logged, never
                     fail the admin request — same posture as the audit write itself)
```

Both existing writers (admin-audit middleware and `StepUpAuditAdapter`) call the same
repository, so one decoration covers every event the in-band log captures — the issue's
acceptance criterion. `cmd/server/main.go` wires the decorated repo wherever the plain repo is
injected today.

### 3. Operator-pinned subscription — the T7 control

**Config** (`internal/config/config.go`, new top-level block; NOT the admin settings table —
no admin API path may read or change it):

```go
type AlertingConfig struct {
	Enabled       bool   `yaml:"enabled" env:"TMI_ALERTING_ENABLED"`
	WebhookURL    string `yaml:"webhook_url" env:"TMI_ALERTING_WEBHOOK_URL"`
	WebhookSecret string `yaml:"webhook_secret" env:"TMI_ALERTING_WEBHOOK_SECRET"` // resolvable via secrets provider
}
```

**Boot materialization:** when enabled, an idempotent upsert creates/updates a
`webhook_subscriptions` row flagged with a new column `operator_pinned` (bool, default
false), subscribed to `system_audit.admin_write`, **active immediately** — the challenge
round-trip is skipped because the URL is trusted operator input (challenge exists to verify
untrusted admin-supplied URLs). URL/secret changes in config are reconciled at boot.
When disabled, the pinned row (if present) is deactivated at boot.

**Admin API behavior** (`api/webhook_handlers.go`):
- List/Get: pinned row visible with the URL redacted (e.g., `"url": "(operator-pinned)"`)
  so admins know the control exists but cannot learn or target the destination.
- PUT/PATCH/DELETE on a pinned row → 403 ("operator-pinned subscription is managed by
  server configuration").
- Admin-created subscriptions may include `system_audit.admin_write` in their `events` —
  normal lifecycle (challenge, deletable). Their deletion/change is itself an audited admin
  write, which fires an alert to the pinned sink.

**SSRF:** the pinned URL goes through the same `SafeHTTPClient` + webhook SSRF config as
every other webhook target.

### 4. Schema change

`webhook_subscriptions.operator_pinned BOOLEAN NOT NULL DEFAULT false` via GORM tag on the
subscription model → AutoMigrate. Sync `internal/dbschema/schema.go` validator and
`cmd/dbtool` expectations. **Mandatory oracle-db-admin review.**

## Out of scope

- SMTP/email sink; per-subscription payload formats (Slack); multiple pinned sinks;
  batching/digests; alert retention beyond the webhook pipeline's existing record TTLs.

## Testing

- **Unit:** decorator emits on successful create and not on failed create; emit failure is
  non-fatal; boot materialization idempotency (create → reconcile URL change → disable);
  handler 403s on pinned-row mutations; URL redaction in list/get responses.
- **Integration (PG + Redis):** `PUT /admin/settings/{key}` as admin → audit row exists AND
  a `system_audit.admin_write` event lands in the Redis stream → delivery record created for
  the pinned subscription → stub HTTP receiver gets the POST with valid HMAC signature
  (acceptance criterion from the issue). Admin convenience subscription to the same event
  also receives it. Mutation of the pinned subscription via API → 403 (and the attempt
  itself, if it had succeeded, would have been audited — assert the 403 produces no audit row
  since only 2xx is audited).
- **API change** ⇒ postman/newman + CATS analysis at session close; zero-500 policy.
- **Oracle:** `make test-integration-oci`; oracle-db-admin review of the column addition.

## Implementation shape

One new column, one config block, one decorator file, boot wiring, handler guards, OpenAPI
enum addition + regeneration, tests. The delivery pipeline itself is untouched.
