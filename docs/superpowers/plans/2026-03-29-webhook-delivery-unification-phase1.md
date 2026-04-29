# Webhook Delivery Unification Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify the addon invocation webhook payload and headers to match the resource-change webhook format, and rename `invocation_id` to `delivery_id` throughout the codebase, OpenAPI spec, docs, and wiki.

**Architecture:** The `AddonInvocationPayload` struct is replaced with a `WebhookDeliveryPayload` struct using the unified envelope (`event_type`, `threat_model_id`, `timestamp`, `object_type`, `object_id`, `data`). Headers are standardized to match resource-change webhooks (`X-Webhook-Delivery-Id` replaces `X-Invocation-Id`, `X-Addon-Id` is removed). The `User-Agent` is unified to `TMI-Webhook/1.0`. All references to `invocation_id` become `delivery_id`.

**Tech Stack:** Go, OpenAPI 3.0.3, oapi-codegen, Gin, jq

**Spec:** `docs/superpowers/specs/2026-03-29-webhook-delivery-unification-design.md`

---

### Task 1: Update OpenAPI Spec — Rename invocation_id to delivery_id

**Files:**
- Modify: `api-schema/tmi-openapi.json`
- Modify: `api-schema/tmi-openapi-3.1-experimental.json`

- [ ] **Step 1: Update InvokeAddonResponse schema**

In `api-schema/tmi-openapi.json`, change the `InvokeAddonResponse` schema:
- Rename `invocation_id` property to `delivery_id` in `required` array, `properties`, and `example`
- Update the description from "Invocation identifier for tracking" to "Delivery identifier for tracking"

Use jq:

```bash
jq '
  .components.schemas.InvokeAddonResponse.required = (.components.schemas.InvokeAddonResponse.required | map(if . == "invocation_id" then "delivery_id" else . end)) |
  .components.schemas.InvokeAddonResponse.properties = (
    .components.schemas.InvokeAddonResponse.properties |
    to_entries |
    map(if .key == "invocation_id" then .key = "delivery_id" | .value.description = "Delivery identifier for tracking" else . end) |
    from_entries
  ) |
  .components.schemas.InvokeAddonResponse.example = (
    .components.schemas.InvokeAddonResponse.example |
    with_entries(if .key == "invocation_id" then .key = "delivery_id" else . end)
  ) |
  .components.schemas.InvokeAddonResponse.description = "Response from addon invocation including delivery ID"
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Update InvocationResponse schema description**

The `InvocationResponse` schema uses `id` (not `invocation_id`) for the identifier field, so the property name does not change. Update the description:

```bash
jq '
  .components.schemas.InvocationResponse.properties.id.description = "Delivery identifier"
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 3: Apply same changes to experimental spec**

Apply the same `InvokeAddonResponse` changes to `api-schema/tmi-openapi-3.1-experimental.json`:

```bash
jq '
  .components.schemas.InvokeAddonResponse.required = (.components.schemas.InvokeAddonResponse.required | map(if . == "invocation_id" then "delivery_id" else . end)) |
  .components.schemas.InvokeAddonResponse.properties = (
    .components.schemas.InvokeAddonResponse.properties |
    to_entries |
    map(if .key == "invocation_id" then .key = "delivery_id" | .value.description = "Delivery identifier for tracking" else . end) |
    from_entries
  ) |
  .components.schemas.InvokeAddonResponse.example = (
    .components.schemas.InvokeAddonResponse.example |
    with_entries(if .key == "invocation_id" then .key = "delivery_id" else . end)
  ) |
  .components.schemas.InvokeAddonResponse.description = "Response from addon invocation including delivery ID"
' api-schema/tmi-openapi-3.1-experimental.json > api-schema/tmi-openapi-3.1-experimental.json.tmp && mv api-schema/tmi-openapi-3.1-experimental.json.tmp api-schema/tmi-openapi-3.1-experimental.json
```

- [ ] **Step 4: Validate OpenAPI spec**

```bash
make validate-openapi
```

Expected: validation passes with no errors.

- [ ] **Step 5: Regenerate API code**

```bash
make generate-api
```

Expected: `api/api.go` regenerated. The `InvokeAddonResponse` struct now has `DeliveryId` instead of `InvocationId`.

- [ ] **Step 6: Commit**

```bash
git add api-schema/tmi-openapi.json api-schema/tmi-openapi-3.1-experimental.json api/api.go
git commit -m "$(cat <<'EOF'
refactor(api): rename invocation_id to delivery_id in OpenAPI spec (#194)

Part of webhook delivery unification. Renames invocation_id to
delivery_id in InvokeAddonResponse schema and regenerates API code.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Replace AddonInvocationPayload with Unified Envelope

**Files:**
- Modify: `api/addon_invocation_worker.go:27-38` (payload struct)
- Modify: `api/addon_invocation_worker.go:156-167` (payload construction)
- Modify: `api/addon_invocation_worker.go:188-193` (headers)

- [ ] **Step 1: Write a test for the new payload shape**

Create a test in `api/addon_invocation_worker_test.go` that verifies the payload structure and headers. First, check if the file exists:

```bash
ls api/addon_invocation_worker_test.go 2>/dev/null || echo "does not exist"
```

If it does not exist, create it. If it does, add to it. The test should verify:

```go
package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookDeliveryPayload_MarshalJSON(t *testing.T) {
	addonID := uuid.New()
	threatModelID := uuid.New()
	objectID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)

	userData := json.RawMessage(`{"key":"value"}`)
	data := WebhookDeliveryData{
		AddonID:  &addonID,
		UserData: &userData,
	}
	dataBytes, err := json.Marshal(data)
	require.NoError(t, err)

	payload := WebhookDeliveryPayload{
		EventType:     "addon.invoked",
		ThreatModelID: threatModelID,
		Timestamp:     now,
		ObjectType:    "threat",
		ObjectID:      &objectID,
		Data:          json.RawMessage(dataBytes),
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(payloadBytes, &result)
	require.NoError(t, err)

	assert.Equal(t, "addon.invoked", result["event_type"])
	assert.Equal(t, threatModelID.String(), result["threat_model_id"])
	assert.Equal(t, "threat", result["object_type"])
	assert.Equal(t, objectID.String(), result["object_id"])
	assert.NotNil(t, result["data"])

	// Verify no removed fields are present
	assert.Nil(t, result["invocation_id"])
	assert.Nil(t, result["addon_id"])
	assert.Nil(t, result["callback_url"])

	// Verify data contains addon_id
	dataMap, ok := result["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, addonID.String(), dataMap["addon_id"])
	assert.NotNil(t, dataMap["user_data"])
}

func TestWebhookDeliveryPayload_OptionalFields(t *testing.T) {
	threatModelID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)

	payload := WebhookDeliveryPayload{
		EventType:     "threat_model.updated",
		ThreatModelID: threatModelID,
		Timestamp:     now,
		Data:          json.RawMessage(`{"name":"test"}`),
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(payloadBytes, &result)
	require.NoError(t, err)

	// object_type and object_id should be absent when not set
	_, hasObjectType := result["object_type"]
	_, hasObjectID := result["object_id"]
	assert.False(t, hasObjectType)
	assert.False(t, hasObjectID)
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
make test-unit name=TestWebhookDeliveryPayload
```

Expected: FAIL — `WebhookDeliveryPayload` and `WebhookDeliveryData` types do not exist yet.

- [ ] **Step 3: Replace the payload struct**

In `api/addon_invocation_worker.go`, replace `AddonInvocationPayload` with:

```go
// WebhookDeliveryPayload represents the unified payload sent to webhook endpoints.
// Used for all webhook deliveries (resource-change events and addon invocations).
type WebhookDeliveryPayload struct {
	EventType     string          `json:"event_type"`
	ThreatModelID uuid.UUID       `json:"threat_model_id"`
	Timestamp     time.Time       `json:"timestamp"`
	ObjectType    string          `json:"object_type,omitempty"`
	ObjectID      *uuid.UUID      `json:"object_id,omitempty"`
	Data          json.RawMessage `json:"data"`
}

// WebhookDeliveryData contains addon-specific fields within the unified payload data.
type WebhookDeliveryData struct {
	AddonID  *uuid.UUID       `json:"addon_id,omitempty"`
	UserData *json.RawMessage `json:"user_data,omitempty"`
}
```

- [ ] **Step 4: Update payload construction in processInvocation**

In `api/addon_invocation_worker.go`, replace the payload construction block (around lines 156-167) with:

```go
	// Build addon-specific data
	userData := json.RawMessage(invocation.Data)
	deliveryData := WebhookDeliveryData{
		AddonID:  &invocation.AddonID,
		UserData: &userData,
	}
	dataBytes, err := json.Marshal(deliveryData)
	if err != nil {
		logger.Error("failed to marshal delivery data: %v", err)
		invocation.Status = InvocationStatusFailed
		invocation.StatusMessage = fmt.Sprintf("Failed to marshal data: %v", err)
		_ = GlobalAddonInvocationStore.Update(ctx, invocation)
		return err
	}

	// Build unified payload
	payload := WebhookDeliveryPayload{
		EventType:     "addon.invoked",
		ThreatModelID: invocation.ThreatModelID,
		ObjectType:    invocation.ObjectType,
		ObjectID:      invocation.ObjectID,
		Timestamp:     invocation.CreatedAt,
		Data:          json.RawMessage(dataBytes),
	}
```

- [ ] **Step 5: Update headers in processInvocation**

In `api/addon_invocation_worker.go`, replace the header block (around lines 188-193) with:

```go
	// Add unified headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", "addon.invoked")
	req.Header.Set("X-Webhook-Delivery-Id", invocationID.String())
	req.Header.Set("X-Webhook-Subscription-Id", webhook.Id.String())
	req.Header.Set("User-Agent", "TMI-Webhook/1.0")
```

- [ ] **Step 6: Remove the callback URL construction**

Delete the `callbackURL` variable and the line that builds it (around line 154):

```go
	// Build callback URL using configured base URL
	callbackURL := fmt.Sprintf("%s/invocations/%s/status", w.baseURL, invocationID)
```

This is no longer needed — the callback URL follows a well-known pattern.

- [ ] **Step 7: Run the test to verify it passes**

```bash
make test-unit name=TestWebhookDeliveryPayload
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add api/addon_invocation_worker.go api/addon_invocation_worker_test.go
git commit -m "$(cat <<'EOF'
refactor(api): replace AddonInvocationPayload with unified webhook envelope (#194)

Introduces WebhookDeliveryPayload and WebhookDeliveryData structs.
Replaces X-Invocation-Id and X-Addon-Id headers with X-Webhook-Delivery-Id.
Unifies User-Agent to TMI-Webhook/1.0. Removes callback_url from payload.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Rename invocation_id to delivery_id in Go Source

**Files:**
- Modify: `api/addon_invocation_handlers.go:227` (log message)
- Modify: `api/addon_invocation_cleanup_worker.go:75,98` (log messages)
- Modify: `api/addon_rate_limiter.go:68` (map key)
- Modify: `api/uuid_validation_middleware.go:25` (parameter name)

- [ ] **Step 1: Update log messages in addon_invocation_handlers.go**

In `api/addon_invocation_handlers.go`, change line 227:

```go
// Before:
logger.Info("Add-on invoked: addon_id=%s, invocation_id=%s, %s",
// After:
logger.Info("Add-on invoked: addon_id=%s, delivery_id=%s, %s",
```

- [ ] **Step 2: Update log messages in addon_invocation_cleanup_worker.go**

In `api/addon_invocation_cleanup_worker.go`, change line 75:

```go
// Before:
logger.Warn("timing out stale invocation: invocation_id=%s, addon_id=%s, user=%s, last_activity=%s, status=%s",
// After:
logger.Warn("timing out stale invocation: delivery_id=%s, addon_id=%s, user=%s, last_activity=%s, status=%s",
```

And line 98:

```go
// Before:
logger.Info("invocation timed out: invocation_id=%s, addon_id=%s", invocation.ID, invocation.AddonID)
// After:
logger.Info("invocation timed out: delivery_id=%s, addon_id=%s", invocation.ID, invocation.AddonID)
```

- [ ] **Step 3: Update map key in addon_rate_limiter.go**

In `api/addon_rate_limiter.go`, change line 68:

```go
// Before:
"invocation_id":     inv.ID.String(),
// After:
"delivery_id":       inv.ID.String(),
```

- [ ] **Step 4: Update UUID validation middleware**

In `api/uuid_validation_middleware.go`, change line 25:

```go
// Before:
"invocation_id",
// After:
"delivery_id",
```

- [ ] **Step 5: Update the InvokeAddon handler response**

In `api/addon_invocation_handlers.go`, find the response construction (around line 220-224). After regenerating the API code in Task 1, the `InvokeAddonResponse` struct now uses `DeliveryId` instead of `InvocationId`. Update the field name:

```go
// Before:
response := InvokeAddonResponse{
	InvocationId: invocation.ID,
	Status:       statusToInvokeAddonResponseStatus(invocation.Status),
	CreatedAt:    invocation.CreatedAt,
}
// After:
response := InvokeAddonResponse{
	DeliveryId: invocation.ID,
	Status:     statusToInvokeAddonResponseStatus(invocation.Status),
	CreatedAt:  invocation.CreatedAt,
}
```

- [ ] **Step 6: Build to verify compilation**

```bash
make build-server
```

Expected: builds successfully.

- [ ] **Step 7: Commit**

```bash
git add api/addon_invocation_handlers.go api/addon_invocation_cleanup_worker.go api/addon_rate_limiter.go api/uuid_validation_middleware.go
git commit -m "$(cat <<'EOF'
refactor(api): rename invocation_id to delivery_id in Go source (#194)

Updates log messages, rate limiter map keys, UUID validation middleware,
and handler response to use delivery_id consistently.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Update Unit Tests

**Files:**
- Modify: `api/addon_invocation_handlers_test.go:344`
- Modify: `api/uuid_validation_middleware_test.go:72,160`

- [ ] **Step 1: Update addon invocation handler test**

In `api/addon_invocation_handlers_test.go`, change line 344:

```go
// Before:
assert.NotEmpty(t, response["invocation_id"])
// After:
assert.NotEmpty(t, response["delivery_id"])
```

Search for any other occurrences of `invocation_id` in this file and update them too.

- [ ] **Step 2: Update UUID validation middleware test**

In `api/uuid_validation_middleware_test.go`, change line 72:

```go
// Before:
"invocation_id",
// After:
"delivery_id",
```

And line 160:

```go
// Before:
{"invocation_id", true},
// After:
{"delivery_id", true},
```

- [ ] **Step 3: Run all unit tests**

```bash
make test-unit
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add api/addon_invocation_handlers_test.go api/uuid_validation_middleware_test.go
git commit -m "$(cat <<'EOF'
test(api): update tests for invocation_id to delivery_id rename (#194)

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Update Postman Test Collection

**Files:**
- Modify: `test/postman/addons-tests-collection.json:579`

- [ ] **Step 1: Update the assertion**

In `test/postman/addons-tests-collection.json`, change line 579:

```json
// Before:
"    pm.expect(jsonData).to.have.property('invocation_id');",
// After:
"    pm.expect(jsonData).to.have.property('delivery_id');",
```

Search for any other occurrences of `invocation_id` in this file and update them.

- [ ] **Step 2: Commit**

```bash
git add test/postman/addons-tests-collection.json
git commit -m "$(cat <<'EOF'
test(api): update Postman collection for delivery_id rename (#194)

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Update Documentation in docs/

**Files:**
- Modify: `docs/migrated/developer/addons/addon-development-guide.md`
- Modify: `docs/migrated/developer/addons/addons-design.md`
- Modify: `docs/migrated/operator/addons/addon-configuration.md`
- Modify: `docs/migrated/reference/apis/rate-limiting-specification.md`

Note: The `docs/` directory is deprecated in favor of the wiki, but these files should still be updated for consistency since they exist.

- [ ] **Step 1: Update addon-development-guide.md**

Apply these replacements throughout `docs/migrated/developer/addons/addon-development-guide.md`:
- `X-Invocation-Id` → `X-Webhook-Delivery-Id`
- `X-Addon-Id` (header references) → remove these lines
- `User-Agent: TMI-Addon-Worker/1.0` → `User-Agent: TMI-Webhook/1.0`
- `invocation_id` (in JSON payloads/code) → `delivery_id`
- `"invocation_id"` (in Python dict access) → `"delivery_id"`
- `/invocations/{invocation_id}/status` → `/webhook-deliveries/{delivery_id}/status`
- `callback_url` references in payload examples → remove

- [ ] **Step 2: Update addons-design.md**

Apply the same replacements throughout `docs/migrated/developer/addons/addons-design.md`:
- `invocation_id` → `delivery_id` (in all contexts: Redis keys, JSON, schemas, endpoints)
- `X-Invocation-Id` → `X-Webhook-Delivery-Id`
- `X-Addon-Id` header references → remove
- `TMI-Addon-Worker/1.0` → `TMI-Webhook/1.0`
- `/invocations/{invocation_id}` → `/webhook-deliveries/{delivery_id}`
- `callback_url` in payload examples → remove

- [ ] **Step 3: Update addon-configuration.md**

Apply the same replacements in `docs/migrated/operator/addons/addon-configuration.md`:
- `invocation_id` → `delivery_id`
- `X-Invocation-Id` → `X-Webhook-Delivery-Id`
- `X-Addon-Id` header → remove
- `TMI-Addon-Worker/1.0` → `TMI-Webhook/1.0`

- [ ] **Step 4: Update rate-limiting-specification.md**

In `docs/migrated/reference/apis/rate-limiting-specification.md`, change:
- `/addons/invocations/{invocation_id}` → `/webhook-deliveries/{delivery_id}`

- [ ] **Step 5: Commit**

```bash
git add docs/migrated/
git commit -m "$(cat <<'EOF'
docs: rename invocation_id to delivery_id and update headers in docs (#194)

Updates addon development guide, design doc, operator config, and
rate limiting spec to reflect unified webhook delivery headers and
the invocation_id to delivery_id rename.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Update Wiki

**Files:**
- Modify: `/Users/efitz/Projects/tmi.wiki/Extending-TMI.md`
- Modify: `/Users/efitz/Projects/tmi.wiki/Addon-System.md`
- Modify: `/Users/efitz/Projects/tmi.wiki/REST-API-Reference.md`
- Modify: `/Users/efitz/Projects/tmi.wiki/API-Rate-Limiting.md`

- [ ] **Step 1: Update Extending-TMI.md**

Apply these replacements throughout `/Users/efitz/Projects/tmi.wiki/Extending-TMI.md`:
- `X-Invocation-Id` → `X-Webhook-Delivery-Id`
- `X-Addon-Id` header lines → remove
- `User-Agent: TMI-Addon-Worker/1.0` → `User-Agent: TMI-Webhook/1.0`
- `invocation_id` (JSON keys, Python dict access, variable names) → `delivery_id`
- `/invocations/{invocation_id}/status` → `/webhook-deliveries/{delivery_id}/status`
- `callback_url` in payload examples → remove
- Update Python function signatures: `def update_status(invocation_id,` → `def update_status(delivery_id,`
- Update Python variable names: `invocation_id = payload['invocation_id']` → `delivery_id = payload['delivery_id']`
- Update Python f-strings: `f"...invocations/{invocation_id}/status"` → `f"...webhook-deliveries/{delivery_id}/status"`

- [ ] **Step 2: Update Addon-System.md**

Apply the same set of replacements throughout `/Users/efitz/Projects/tmi.wiki/Addon-System.md`:
- `X-Invocation-Id` → `X-Webhook-Delivery-Id`
- `X-Addon-Id` header lines → remove
- `User-Agent: TMI-Addon-Worker/1.0` → `User-Agent: TMI-Webhook/1.0`
- `invocation_id` → `delivery_id` (all contexts)
- `/invocations/{invocation_id}` → `/webhook-deliveries/{delivery_id}`
- `callback_url` references → remove
- Python code variable/parameter renames same as Step 1

- [ ] **Step 3: Update REST-API-Reference.md**

In `/Users/efitz/Projects/tmi.wiki/REST-API-Reference.md`:
- `invocation_id` → `delivery_id`
- `GET /invocations/{invocation_id}` → `GET /webhook-deliveries/{delivery_id}`

- [ ] **Step 4: Update API-Rate-Limiting.md**

In `/Users/efitz/Projects/tmi.wiki/API-Rate-Limiting.md`:
- `/addons/invocations/{invocation_id}` → `/webhook-deliveries/{delivery_id}`

- [ ] **Step 5: Commit wiki changes**

```bash
cd /Users/efitz/Projects/tmi.wiki
git add Extending-TMI.md Addon-System.md REST-API-Reference.md API-Rate-Limiting.md
git commit -m "$(cat <<'EOF'
docs: rename invocation_id to delivery_id and update webhook headers (#194)

Updates wiki pages to reflect unified webhook delivery headers
(X-Webhook-Delivery-Id replaces X-Invocation-Id, X-Addon-Id removed,
User-Agent unified to TMI-Webhook/1.0) and the invocation_id to
delivery_id rename.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
cd /Users/efitz/Projects/tmi
```

---

### Task 8: Run Full Verification

**Files:** None (verification only)

- [ ] **Step 1: Lint**

```bash
make lint
```

Expected: passes with no new issues.

- [ ] **Step 2: Build**

```bash
make build-server
```

Expected: builds successfully.

- [ ] **Step 3: Run unit tests**

```bash
make test-unit
```

Expected: all tests pass.

- [ ] **Step 4: Search for any remaining invocation_id references**

```bash
grep -r "invocation_id" api/ --include="*.go" | grep -v "_test.go" | grep -v "api.go"
grep -r "X-Invocation-Id" api/ --include="*.go"
grep -r "X-Addon-Id" api/ --include="*.go"
grep -r "TMI-Addon-Worker" api/ --include="*.go"
grep -r "AddonInvocationPayload" api/ --include="*.go"
grep -r "callback_url\|CallbackURL\|callbackURL" api/ --include="*.go" | grep -v "_test.go"
```

Expected: no matches for any of these (except possibly in comments referencing the migration). Fix any remaining references found.

- [ ] **Step 5: Fix any issues found and re-run tests**

If any issues were found in Step 4, fix them, then re-run:

```bash
make lint && make build-server && make test-unit
```

---

### Task 9: Create GitHub Issue for Phases 2-3

**Files:** None (GitHub operations only)

- [ ] **Step 1: Create the issue**

```bash
gh issue create --repo ericfitz/tmi --title "refactor: unify webhook delivery pipeline and migrate to Redis" --milestone "1.4.0" --label "api" --body "$(cat <<'ISSUE_EOF'
## Summary

Complete the webhook delivery unification started in #194. This issue covers Phases 2 and 3 of the design.

**Design spec:** `docs/superpowers/specs/2026-03-29-webhook-delivery-unification-design.md`

## Background

Issue #194 (Phase 1) unified the addon invocation webhook payload and headers to match resource-change webhooks, and renamed `invocation_id` to `delivery_id`. This issue completes the convergence by building a unified Redis-backed delivery pipeline.

### Design Decisions

- All delivery state moves to Redis (replacing both Postgres `webhook_deliveries` and Redis `AddonInvocation` stores)
- Any webhook delivery can use `X-TMI-Callback: async` for bidirectional status callbacks (not just addon invocations)
- Addon invocations emit events to Redis Streams and flow through the same delivery worker as resource-change events
- New `GET /webhook-deliveries/{id}` endpoint supports JWT or HMAC authentication
- New `POST /webhook-deliveries/{id}/status` endpoint replaces `POST /invocations/{id}/status`

### Redis TTLs

- `pending` / `in_progress`: 4 hours
- `failed` / `completed` / `delivered`: 7 days

## Phase 2: Unified Redis Delivery Model + Migrate Addon Invocations

- [ ] Create `WebhookDeliveryRedisStore` with unified delivery record
- [ ] Add `POST /webhook-deliveries/{id}/status` endpoint (replaces `POST /invocations/{id}/status`)
- [ ] Add `GET /webhook-deliveries/{id}` endpoint with JWT+HMAC dual auth (replaces `GET /invocations/{id}`)
- [ ] Remove `GET /invocations` list endpoint
- [ ] Modify `POST /addons/{id}/invoke` to create unified delivery records and emit to Redis Streams
- [ ] Remove `AddonInvocationWorker` and `AddonInvocationStore`
- [ ] Route `addon.invoked` events through `WebhookEventConsumer` → unified delivery worker
- [ ] Add `X-TMI-Callback: async` response handling to delivery worker
- [ ] Update cleanup worker to handle unified deliveries
- [ ] Update OpenAPI spec, tests, docs, wiki

## Phase 3: Migrate Resource-Change Deliveries from Postgres to Redis

- [ ] Repoint `WebhookEventConsumer` to create Redis delivery records instead of Postgres
- [ ] Repoint admin endpoints (`/admin/webhooks/deliveries/*`) to read from Redis
- [ ] Remove `GormWebhookDeliveryStore` and `webhook_deliveries` Postgres table
- [ ] Add migration to drop the Postgres table
- [ ] Update tests
ISSUE_EOF
)"
```

- [ ] **Step 2: Update issue #194**

Add a comment to #194 linking to the new issue and noting that Phase 1 is complete:

```bash
gh issue comment 194 --repo ericfitz/tmi --body "$(cat <<'COMMENT_EOF'
Phase 1 (unified payload/headers + invocation_id→delivery_id rename) is complete.

Phases 2-3 (unified Redis delivery pipeline, migrate addon invocations and resource-change deliveries) are tracked in the new issue created above.

Design spec: `docs/superpowers/specs/2026-03-29-webhook-delivery-unification-design.md`
COMMENT_EOF
)"
```

- [ ] **Step 3: Add the new issue to the TMI project**

```bash
# Get the new issue number from the creation output, then:
gh project item-add 2 --owner ericfitz --url https://github.com/ericfitz/tmi/issues/<NEW_ISSUE_NUMBER>
```
