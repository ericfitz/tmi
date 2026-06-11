# Audit Alert Sink via Webhook Event Type (#395) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every system-audit event (admin write / step-up) additionally fires a `system_audit.admin_write` webhook event, delivered through the existing webhook pipeline; an operator-pinned subscription that no admin API can suppress satisfies T7.

**Architecture:** New event type in `api/events.go` + OpenAPI enum; a decorator on `SystemAuditRepository.Create` emits the event; a new `alerting:` operator config block materializes a pinned `webhook_subscriptions` row at boot (new `operator_pinned` column); `/admin/webhooks` handlers redact and refuse mutation of pinned rows.

**Tech Stack:** Go, GORM, Redis Streams, existing webhook delivery worker, oapi-codegen.

**Spec:** `docs/superpowers/specs/2026-06-11-395-audit-alert-sink-design.md` — read it first.

**Branch:** work on `dev/1.4.0`.

---

### Task 1: `operator_pinned` column + pinned-row guards in the webhook store (TDD)

**Files:**
- Modify: the webhook subscription GORM model (find it: `grep -rn "type.*WebhookSubscription" api/models/ api/webhook_store*.go` — the DB model with GORM tags, likely `api/models/*.go`)
- Modify: `api/webhook_store.go` / `api/webhook_store_gorm.go`
- Create: `api/webhook_pinned_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

// Test that the store-level model carries OperatorPinned and that it
// round-trips through the GORM store. Use the in-memory SQLite pattern from
// existing webhook store tests (see api/webhook_store_gorm.go's test file
// for the setup helper to reuse).

func TestWebhookSubscription_OperatorPinnedRoundTrip(t *testing.T) {
	db := setupWebhookStoreTestDB(t) // reuse/adapt the existing helper
	store := NewGormWebhookSubscriptionStore(db) // match the real constructor name

	sub := /* minimal valid subscription fixture, copied from an existing store test */
	sub.OperatorPinned = true
	created, err := store.Create(context.Background(), sub)
	require.NoError(t, err)

	got, err := store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.True(t, got.OperatorPinned)
}
```

(Adapt names to the real store interface — `WebhookSubscriptionStoreInterface` at `api/webhook_store.go:77`. The contract: the flag persists and is readable.)

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestWebhookSubscription_OperatorPinnedRoundTrip`
Expected: FAIL — unknown field `OperatorPinned`

- [ ] **Step 3: Implement**

Add to the subscription DB model (GORM tags matching neighboring boolean columns):

```go
	// OperatorPinned marks the subscription as materialized from operator
	// config (alerting block, #395). Pinned rows cannot be modified or
	// deleted through /admin/webhooks and their URL is redacted in reads.
	OperatorPinned bool `gorm:"not null;default:false" json:"operator_pinned"`
```

Propagate through the store's domain struct (`api/webhook_store.go` `WebhookSubscription`) and the GORM mapping in `api/webhook_store_gorm.go` (both directions).

- [ ] **Step 4: Run tests + sync schema validator**

Run: `make test-unit name=TestWebhookSubscription_OperatorPinnedRoundTrip` — PASS, then `make build-server && make test-unit`.
Sync: `grep -n "webhook_subscriptions" internal/dbschema/schema.go cmd/dbtool/ -r` — add the column to any expected-schema listing found.

- [ ] **Step 5: Commit**

```bash
git add -A api/ internal/dbschema/ cmd/dbtool/
git commit -m "feat(models): operator_pinned flag on webhook subscriptions

Refs #395.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Event type — code + OpenAPI enum

**Files:**
- Modify: `api/events.go`
- Modify: `api-schema/tmi-openapi.json` (every `WebhookEventType` enum occurrence)

- [ ] **Step 1: Add the constant**

In `api/events.go`, after the extraction events block:

```go
	// System audit events (T7, #395): emitted for every system_audit_entries
	// write (admin-write middleware and step-up adapter paths).
	EventSystemAuditAdminWrite = "system_audit.admin_write"
```

- [ ] **Step 2: Add to the OpenAPI enums**

```bash
cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.$(date +%Y%m%d_%H%M%S).backup
grep -n '"threat_model.created"' api-schema/tmi-openapi.json
```

Every enum array that lists the webhook event types (the grep showed ~5 occurrences: subscription events, test-request event type, etc.) gets `"system_audit.admin_write"` appended. Use jq to patch each enum array by path, or Edit with unique context. Then:

```bash
jq empty api-schema/tmi-openapi.json && make validate-openapi && make generate-api && make build-server && make test-unit
```

If any handler validates events against a hardcoded Go list (grep `"threat_model.created"` in `api/*.go` excluding generated api.go and events.go), add the new type there too.

- [ ] **Step 3: Commit**

```bash
rm -f api-schema/tmi-openapi.json.*.backup
git add api/events.go api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): system_audit.admin_write webhook event type

Refs #395.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Alerting decorator on SystemAuditRepository (TDD)

**Files:**
- Create: `api/system_audit_alerting.go`
- Create: `api/system_audit_alerting_test.go`
- Modify: `cmd/server/main.go` (wrap the repo at construction)

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureEmitter struct{ events []EventPayload }

func (c *captureEmitter) Emit(_ context.Context, p EventPayload) error {
	c.events = append(c.events, p)
	return nil
}

type stubSysAuditRepo struct {
	SystemAuditRepository
	createErr error
	created   []models.SystemAuditEntry
}

func (s *stubSysAuditRepo) Create(_ context.Context, e models.SystemAuditEntry) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, e)
	return nil
}

func TestAlertingRepo_EmitsOnCreate(t *testing.T) {
	em := &captureEmitter{}
	inner := &stubSysAuditRepo{}
	repo := NewAlertingSystemAuditRepository(inner, em, "test-operator")

	entry := models.SystemAuditEntry{
		ID:         models.DBVarchar("e-1"),
		ActorEmail: models.DBVarchar("charlie@tmi.local"),
		HTTPMethod: models.DBVarchar("PUT"),
		HTTPPath:   models.DBText("/admin/settings/x"),
		FieldPath:  models.DBVarchar("x"),
	}
	require.NoError(t, repo.Create(context.Background(), entry))

	require.Len(t, em.events, 1)
	ev := em.events[0]
	assert.Equal(t, EventSystemAuditAdminWrite, ev.EventType)
	assert.Equal(t, "e-1", ev.Data["entry_id"])
	assert.Equal(t, "charlie@tmi.local", ev.Data["actor_email"])
	assert.Equal(t, "test-operator", ev.Data["operator_name"])
}

func TestAlertingRepo_NoEmitOnCreateFailure(t *testing.T) {
	em := &captureEmitter{}
	inner := &stubSysAuditRepo{createErr: errors.New("db down")}
	repo := NewAlertingSystemAuditRepository(inner, em, "test-operator")

	err := repo.Create(context.Background(), models.SystemAuditEntry{})
	require.Error(t, err)
	assert.Empty(t, em.events, "no alert without a persisted audit row")
}
```

NOTE: check how `EventEmitter` exposes emission (`api/events.go:99+`) — if there is no small interface, define `auditAlertEmitter interface { Emit(ctx, EventPayload) error }` in the new file matching the EventEmitter's actual method name/signature (read `events.go:120-190`), and adapt the test stub.

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestAlertingRepo`
Expected: FAIL — `undefined: NewAlertingSystemAuditRepository`

- [ ] **Step 3: Implement**

```go
package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
)

// alertingSystemAuditRepository decorates SystemAuditRepository so every
// successfully persisted system-audit entry also emits a
// system_audit.admin_write webhook event (T7 out-of-band alert, #395).
// Emission is non-fatal: the in-band audit row is the durable record.
type alertingSystemAuditRepository struct {
	SystemAuditRepository
	emitter      auditAlertEmitter
	operatorName string
}

func NewAlertingSystemAuditRepository(inner SystemAuditRepository, emitter auditAlertEmitter, operatorName string) SystemAuditRepository {
	return &alertingSystemAuditRepository{SystemAuditRepository: inner, emitter: emitter, operatorName: operatorName}
}

func (r *alertingSystemAuditRepository) Create(ctx context.Context, entry models.SystemAuditEntry) error {
	if err := r.SystemAuditRepository.Create(ctx, entry); err != nil {
		return err
	}
	payload := EventPayload{
		EventType:  EventSystemAuditAdminWrite,
		ObjectID:   string(entry.ID),
		ObjectType: "system_audit_entry",
		Timestamp:  time.Now().UTC(),
		Data: map[string]any{
			"entry_id":           string(entry.ID),
			"actor_email":        string(entry.ActorEmail),
			"actor_provider":     string(entry.ActorProvider),
			"actor_display_name": string(entry.ActorDisplayName),
			"http_method":        string(entry.HTTPMethod),
			"http_path":          string(entry.HTTPPath),
			"field_path":         string(entry.FieldPath),
			"old_value_redacted": nullableTextOrNil(entry.OldValueRedacted),
			"new_value_redacted": nullableTextOrNil(entry.NewValueRedacted),
			"change_summary":     nullableTextOrNil(entry.ChangeSummary),
			"operator_name":      r.operatorName,
		},
	}
	if err := r.emitter.Emit(ctx, payload); err != nil {
		slogging.Get().Error("system audit alert emit failed (in-band audit row persisted): %v", err)
	}
	return nil
}
```

(Match `auditAlertEmitter` to EventEmitter's real method; reuse `nullableTextOrNil` from #398's handlers if landed, else inline the two-liner. `OwnerID` in EventPayload: check whether the consumer requires it — set to actor email or leave empty per how non-TM events like `addon.invoked` populate it.)

Wire in `cmd/server/main.go`: where `NewSystemAuditRepository(...)` is constructed for the admin-audit middleware and the step-up adapter, wrap it:

```go
	sysAuditRepo := api.NewSystemAuditRepository(gormDB.DB())
	if cfg.Alerting.Enabled || /* admins may subscribe regardless */ true {
		sysAuditRepo = api.NewAlertingSystemAuditRepository(sysAuditRepo, api.GlobalEventEmitter, cfg.Operator.Name)
	}
```

(Emission is harmless without subscribers — the consumer simply matches zero subscriptions — so wrap unconditionally; drop the conditional entirely.)

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestAlertingRepo` — PASS; `make build-server && make test-unit` — green.

- [ ] **Step 5: Commit**

```bash
git add api/system_audit_alerting.go api/system_audit_alerting_test.go cmd/server/main.go
git commit -m "feat(api): emit system_audit.admin_write event on every system-audit write

Refs #395.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Alerting config + boot materialization of the pinned subscription (TDD)

**Files:**
- Modify: `internal/config/config.go`
- Modify: `config-development.yaml` (+ regenerate `config-example.yml` via genconfig — check `make list-targets`)
- Create: `api/webhook_pinned_bootstrap.go`
- Create: `api/webhook_pinned_bootstrap_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Config block**

In `internal/config/config.go`, add to `Config`:

```go
	Alerting AlertingConfig `yaml:"alerting"`
```

and:

```go
// AlertingConfig is the operator-controlled out-of-band audit alert sink
// (T7, #395). Deliberately NOT in the admin settings table: no admin API
// may read or change it, so a hostile insider with the admin role cannot
// suppress the alert from inside TMI.
type AlertingConfig struct {
	Enabled       bool   `yaml:"enabled" env:"TMI_ALERTING_ENABLED"`
	WebhookURL    string `yaml:"webhook_url" env:"TMI_ALERTING_WEBHOOK_URL"`
	WebhookSecret string `yaml:"webhook_secret" env:"TMI_ALERTING_WEBHOOK_SECRET"`
}
```

Follow the file's existing env-binding mechanics (see how `OperatorConfig`/`ObservabilityConfig` fields get their env overrides applied — replicate exactly). If the secrets provider has a standard-keys struct (`internal/secrets/provider.go:96-143`), add `AlertWebhookSecret` there and resolve it in main.go the same way the settings-encryption key is resolved.

- [ ] **Step 2: Bootstrap test (failing)**

`api/webhook_pinned_bootstrap_test.go` — in-memory SQLite store; contract:

```go
func TestEnsurePinnedAlertSubscription(t *testing.T) {
	store := /* sqlite-backed subscription store, as in Task 1's test */

	// 1. enabled + URL → creates an active pinned subscription for the event
	sub, err := EnsurePinnedAlertSubscription(ctx, store, AlertingBootstrap{
		Enabled: true, URL: "https://alerts.example.com/hook", Secret: "s1",
	})
	require.NoError(t, err)
	assert.True(t, sub.OperatorPinned)
	assert.Contains(t, sub.Events, EventSystemAuditAdminWrite)

	// 2. idempotent: second call updates, does not duplicate
	sub2, err := EnsurePinnedAlertSubscription(ctx, store, AlertingBootstrap{
		Enabled: true, URL: "https://alerts2.example.com/hook", Secret: "s2",
	})
	require.NoError(t, err)
	assert.Equal(t, sub.ID, sub2.ID)
	assert.Equal(t, "https://alerts2.example.com/hook", sub2.URL)
	// total pinned rows == 1 (list and count)

	// 3. disabled → pinned row deactivated (status per the store's status
	// vocabulary — inspect WebhookSubscription's status/active field and
	// assert the deactivated state)
	_, err = EnsurePinnedAlertSubscription(ctx, store, AlertingBootstrap{Enabled: false})
	require.NoError(t, err)
}
```

- [ ] **Step 3: Implement `EnsurePinnedAlertSubscription`**

`api/webhook_pinned_bootstrap.go`: small `AlertingBootstrap{Enabled, URL, Secret}` input struct (keeps `internal/config` out of the api package — main.go maps config → this struct). Logic: find existing subscription with `OperatorPinned=true` (add a store lookup or filter the list); if enabled: create or update (URL, secret, events `[EventSystemAuditAdminWrite]`, owner = operator, status = active — explicitly bypassing the challenge flow; set whatever status/verified fields the challenge would have set, with a comment saying operator config is trusted input); if disabled: deactivate if present. Validate the URL with the webhook URL validator (`api/webhook_url_validator.go`) and fail loudly (log Error, return error — main.go treats it non-fatal but logs at Error) on an invalid/SSRF-blocked URL.

Wire in `cmd/server/main.go` after the webhook stores are constructed and before workers start:

```go
	if _, err := api.EnsurePinnedAlertSubscription(ctx, webhookStore, api.AlertingBootstrap{
		Enabled: cfg.Alerting.Enabled,
		URL:     cfg.Alerting.WebhookURL,
		Secret:  cfg.Alerting.WebhookSecret,
	}); err != nil {
		logger.Error("audit alert sink bootstrap failed (T7 out-of-band alerting NOT active): %v", err)
	}
```

Also add the `alerting:` section to `config-development.yaml` (enabled false by default, commented example URL) and regenerate the annotated example config if a genconfig target exists.

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestEnsurePinnedAlertSubscription` — PASS; full `make build-server && make test-unit`.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go config-development.yaml config-example.yml api/webhook_pinned_bootstrap.go api/webhook_pinned_bootstrap_test.go cmd/server/main.go internal/secrets/
git commit -m "feat(config): operator-pinned audit alert subscription bootstrap

Refs #395.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Handler guards — pinned rows are immutable and redacted (TDD)

**Files:**
- Modify: `api/webhook_handlers.go`
- Create or extend: `api/webhook_pinned_handlers_test.go`

- [ ] **Step 1: Write failing tests**

Using the existing webhook handler test setup (see `api/webhook_handlers_test.go` for the gin/test-server pattern):

1. `PUT/PATCH/DELETE /admin/webhooks/{id}` on a pinned subscription → **403** with message containing "operator-pinned".
2. `GET /admin/webhooks` list containing a pinned row → that row's `url` is `"(operator-pinned)"` and `operator_pinned: true`; non-pinned rows unredacted.
3. `GET /admin/webhooks/{id}` on the pinned row → same redaction.
4. Creating an admin subscription whose `events` includes `system_audit.admin_write` → allowed (201) — the "both" decision's convenience feed.

- [ ] **Step 2: Run to verify failures**

Run: `make test-unit name=TestWebhookPinned` (match your test names)
Expected: FAIL (mutations currently succeed; URL not redacted)

- [ ] **Step 3: Implement**

In each mutating handler (update/delete/regenerate-secret — enumerate every write path in `api/webhook_handlers.go`), after loading the subscription:

```go
	if sub.OperatorPinned {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "operator-pinned subscription is managed by server configuration and cannot be modified through the API",
		})
		return
	}
```

In the read paths, redact before serializing:

```go
	if sub.OperatorPinned {
		sub.URL = "(operator-pinned)"
	}
```

(Never include the pinned row's secret in any response — verify secrets are already excluded from reads for all subscriptions; if a regenerate-secret or reveal path exists, the pinned guard above must cover it.)

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestWebhookPinned` — PASS; `make build-server && make test-unit && make lint`.

- [ ] **Step 5: Commit**

```bash
git add api/webhook_handlers.go api/webhook_pinned_handlers_test.go
git commit -m "feat(api): pinned alert subscriptions immutable + URL-redacted via admin API

Refs #395.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: End-to-end integration test

**Files:**
- Create: `test/integration/workflows/audit_alert_sink_test.go`

- [ ] **Step 1: Write the test**

Conventions from `client_credentials_test.go` (INTEGRATION_TESTS guard, framework client). The integration environment has Redis and the webhook workers running. Flow:

1. Start an in-test HTTP receiver (`httptest.NewServer` — confirm the integration server's SSRF webhook config permits localhost in test config, `TMI_SSRF_WEBHOOK_ALLOWLIST`; if not, set the allowlist in the test env or skip with a clear message documenting the required env).
2. This test cannot reconfigure the running server's operator config — so cover the T7 path at unit level (Task 4/5) and cover the PIPELINE here via the convenience-feed path: as admin, `POST /admin/webhooks` subscribing the test receiver to `system_audit.admin_write` (Task 5 allows it). Complete the challenge handshake if the test framework has a helper (grep `challenge` in `test/integration/`).
3. `PUT /admin/settings/test.alertsink` → 200.
4. Poll the receiver (≤30s): exactly ≥1 POST arrives whose body parses as the webhook envelope with `event_type == "system_audit.admin_write"`, `data.field_path == "test.alertsink"`, `data.actor_email == charlie's email`; verify the `X-Webhook-Signature` HMAC with the subscription secret.
5. Verify via `GET /admin/audit/system?field_path=test.alertsink` (#398, if landed) or DB-side check that the in-band row also exists — the issue's acceptance criterion: BOTH the audit row and the alert.

- [ ] **Step 2: Run**

Run: `make test-integration`
Expected: green.

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/audit_alert_sink_test.go
git commit -m "test(integration): admin write produces audit row + out-of-band webhook alert

Refs #395.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Gates, reviews, docs, close-out

- [ ] **Step 1: Full gates**

`make lint && make build-server && make test-unit && make test-integration`

- [ ] **Step 2: MANDATORY — oracle-db-admin review**

Diff: webhook subscription model (new column), bootstrap upsert, dbschema/dbtool sync. Question: boolean column addition + default on Oracle via AutoMigrate; the bootstrap's find-or-create under the serializable retry wrapper.

- [ ] **Step 3: API battery** — postman/newman; `make cats-fuzz` + `make analyze-cats-results` (new enum value + 403 paths); `security-review` skill (this IS a security control — pay attention to: pinned-row bypass via any webhook write path, secret/URL leakage in responses or logs, SSRF posture of the pinned URL). Stop and surface findings.

- [ ] **Step 4: Oracle ADB** — `make test-integration-oci`.

- [ ] **Step 5: Wiki** — operator documentation (local checkout `/Users/efitz/Projects/tmi-wiki`): the `alerting:` config block, secrets-provider key, what the alert payload contains, Slack-relay note, durability posture (Redis-grade; in-band row is the durable record), and that admins can additionally subscribe to `system_audit.admin_write` as a convenience feed. Commit + push wiki.

- [ ] **Step 6: Land and close**

```bash
git pull --rebase && git push && git status
gh issue comment 395 --body "Implemented on dev/1.4.0 as a webhook event type (system_audit.admin_write) through the existing delivery pipeline (SafeHTTPClient, HMAC, retries, circuit breaker). T7 control = operator-pinned subscription from the alerting: config block — not modifiable, deletable, or readable via any admin API; admins may additionally subscribe convenience feeds to the same event. Durability is Redis-grade by design; the in-band audit row (tamper-protected per #400) is the durable record. Design: docs/superpowers/specs/2026-06-11-395-audit-alert-sink-design.md."
gh issue close 395
```

---

## Self-Review Notes (already applied)

- Spec coverage: column+guards (T1, T5), event type (T2), decorator emission covering both writers (T3), operator config + boot materialization + challenge bypass (T4), end-to-end acceptance (T6), reviews/docs/close (T7).
- The integration test deliberately exercises the convenience-feed path because the running server's operator config can't be mutated mid-test; the pinned path's behaviors (bootstrap, immutability, redaction) are unit-covered. If the integration framework CAN restart the server with custom config, prefer covering the pinned path end-to-end too.
- Known-unknowns flagged with in-repo verification commands: subscription model location/constructor names, EventEmitter method signature, status vocabulary for deactivation, challenge-flow fields, SSRF localhost allowance in test env.
