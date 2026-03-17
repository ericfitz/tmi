# OpenAPI Validation Fixes Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve all 2 warnings and 27 info items from `make validate-openapi` by adding missing descriptions, examples, and a 500 response to the OpenAPI spec.

**Architecture:** All changes are in `api-schema/tmi-openapi.json` (1.8MB). Use jq for all modifications per project guidelines for large JSON files. No Go code changes needed — examples don't affect generated types.

**Tech Stack:** jq for JSON manipulation, `make validate-openapi` for verification.

**Spec:** `docs/superpowers/specs/2026-03-16-openapi-validation-fixes-design.md`

---

### Task 1: Add WsTicketResponse description and ticket example

**Files:**
- Modify: `api-schema/tmi-openapi.json` (components.schemas.WsTicketResponse)

- [ ] **Step 1: Backup the file**

```bash
cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.backup
```

- [ ] **Step 2: Add description and example to WsTicketResponse schema and ticket property**

```bash
jq '
  .components.schemas.WsTicketResponse.description = "Response containing a short-lived, single-use authentication ticket for WebSocket connection" |
  .components.schemas.WsTicketResponse.example = {
    "ticket": "tmi_ws_abc123def456"
  } |
  .components.schemas.WsTicketResponse.properties.ticket.example = "tmi_ws_abc123def456"
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 3: Validate JSON syntax**

```bash
jq empty api-schema/tmi-openapi.json && echo "Valid" || echo "Invalid"
```

Expected: `Valid`

- [ ] **Step 4: Verify the changes**

```bash
jq '.components.schemas.WsTicketResponse | {description, example, ticket_example: .properties.ticket.example}' api-schema/tmi-openapi.json
```

Expected: description, schema example, and ticket property example all present.

- [ ] **Step 5: Commit**

```bash
git add api-schema/tmi-openapi.json
git commit -m "fix(api): add description and example to WsTicketResponse schema (#185)"
```

---

### Task 2: Add 500 response to /ws/ticket GET

**Files:**
- Modify: `api-schema/tmi-openapi.json` (paths./ws/ticket.get.responses)

- [ ] **Step 1: Add 500 response using $ref to InternalServerError**

The `InternalServerError` component response already includes rate limit headers and an Error schema, matching the pattern used by other endpoints.

```bash
jq '
  .paths["/ws/ticket"].get.responses["500"] = {
    "$ref": "#/components/responses/InternalServerError"
  }
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Validate JSON syntax**

```bash
jq empty api-schema/tmi-openapi.json && echo "Valid" || echo "Invalid"
```

Expected: `Valid`

- [ ] **Step 3: Verify the 500 response exists**

```bash
jq '.paths["/ws/ticket"].get.responses["500"]' api-schema/tmi-openapi.json
```

Expected: `{ "$ref": "#/components/responses/InternalServerError" }`

- [ ] **Step 4: Commit**

```bash
git add api-schema/tmi-openapi.json
git commit -m "fix(api): add 500 response to /ws/ticket GET endpoint (#185)"
```

---

### Task 3: Add AuditEntry schema example

**Files:**
- Modify: `api-schema/tmi-openapi.json` (components.schemas.AuditEntry)

- [ ] **Step 1: Add example to AuditEntry schema and id property**

```bash
jq '
  .components.schemas.AuditEntry.example = {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "threat_model_id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
    "object_type": "threat_model",
    "object_id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
    "version": 3,
    "change_type": "updated",
    "actor": {
      "email": "alice@example.com",
      "provider": "google",
      "provider_id": "google-12345",
      "display_name": "Alice"
    },
    "change_summary": "Updated threat model description",
    "created_at": "2026-01-15T10:30:00Z"
  } |
  .components.schemas.AuditEntry.properties.id.example = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Validate and verify**

```bash
jq empty api-schema/tmi-openapi.json && echo "Valid" || echo "Invalid"
jq '.components.schemas.AuditEntry | has("example")' api-schema/tmi-openapi.json
jq '.components.schemas.AuditEntry.properties.id | has("example")' api-schema/tmi-openapi.json
```

Expected: `Valid`, `true`, `true`

- [ ] **Step 3: Commit**

```bash
git add api-schema/tmi-openapi.json
git commit -m "fix(api): add example to AuditEntry schema (#185)"
```

---

### Task 4: Add ListAuditTrailResponse schema example

**Files:**
- Modify: `api-schema/tmi-openapi.json` (components.schemas.ListAuditTrailResponse)

- [ ] **Step 1: Add example to ListAuditTrailResponse schema and audit_entries property**

```bash
jq '
  .components.schemas.ListAuditTrailResponse.example = {
    "audit_entries": [
      {
        "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
        "threat_model_id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
        "object_type": "threat_model",
        "object_id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
        "version": 3,
        "change_type": "updated",
        "actor": {
          "email": "alice@example.com",
          "provider": "google",
          "provider_id": "google-12345",
          "display_name": "Alice"
        },
        "change_summary": "Updated threat model description",
        "created_at": "2026-01-15T10:30:00Z"
      }
    ],
    "total": 42,
    "limit": 20,
    "offset": 0
  } |
  .components.schemas.ListAuditTrailResponse.properties.audit_entries.example = [
    {
      "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "threat_model_id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
      "object_type": "threat_model",
      "object_id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
      "version": 3,
      "change_type": "updated",
      "actor": {
        "email": "alice@example.com",
        "provider": "google",
        "provider_id": "google-12345",
        "display_name": "Alice"
      },
      "change_summary": "Updated threat model description",
      "created_at": "2026-01-15T10:30:00Z"
    }
  ]
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Validate and verify**

```bash
jq empty api-schema/tmi-openapi.json && echo "Valid" || echo "Invalid"
jq '.components.schemas.ListAuditTrailResponse | has("example")' api-schema/tmi-openapi.json
jq '.components.schemas.ListAuditTrailResponse.properties.audit_entries | has("example")' api-schema/tmi-openapi.json
```

Expected: `Valid`, `true`, `true`

- [ ] **Step 3: Commit**

```bash
git add api-schema/tmi-openapi.json
git commit -m "fix(api): add example to ListAuditTrailResponse schema (#185)"
```

---

### Task 5: Add RollbackResponse schema example

**Files:**
- Modify: `api-schema/tmi-openapi.json` (components.schemas.RollbackResponse)

- [ ] **Step 1: Add example to RollbackResponse schema and restored_entity property**

The `restored_entity` example uses a concrete threat model with all required `ThreatModelBase` fields. The `owner` field is a `User` object (not a string).

```bash
jq '
  .components.schemas.RollbackResponse.example = {
    "restored_entity": {
      "id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
      "name": "Payment Service Threat Model",
      "description": "Threat model for the payment processing service",
      "owner": {
        "principal_type": "user",
        "provider": "google",
        "provider_id": "alice@example.com",
        "display_name": "Alice Johnson",
        "email": "alice@example.com"
      },
      "authorization": [
        {
          "principal_type": "user",
          "provider": "google",
          "provider_id": "alice@example.com",
          "display_name": "Alice Johnson",
          "email": "alice@example.com",
          "role": "owner"
        }
      ],
      "threat_model_framework": "STRIDE",
      "status": "in_progress",
      "created_at": "2026-01-10T08:00:00Z",
      "modified_at": "2026-01-15T10:30:00Z"
    },
    "audit_entry": {
      "id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
      "threat_model_id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
      "object_type": "threat_model",
      "object_id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
      "version": 4,
      "change_type": "rolled_back",
      "actor": {
        "email": "alice@example.com",
        "provider": "google",
        "provider_id": "google-12345",
        "display_name": "Alice"
      },
      "change_summary": "Rolled back to version 3",
      "created_at": "2026-01-15T11:00:00Z"
    }
  } |
  .components.schemas.RollbackResponse.properties.restored_entity.example = {
    "id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
    "name": "Payment Service Threat Model",
    "description": "Threat model for the payment processing service",
    "owner": {
      "principal_type": "user",
      "provider": "google",
      "provider_id": "alice@example.com",
      "display_name": "Alice Johnson",
      "email": "alice@example.com"
    },
    "authorization": [
      {
        "principal_type": "user",
        "provider": "google",
        "provider_id": "alice@example.com",
        "display_name": "Alice Johnson",
        "email": "alice@example.com",
        "role": "owner"
      }
    ],
    "threat_model_framework": "STRIDE",
    "status": "in_progress",
    "created_at": "2026-01-10T08:00:00Z",
    "modified_at": "2026-01-15T10:30:00Z"
  }
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Validate and verify**

```bash
jq empty api-schema/tmi-openapi.json && echo "Valid" || echo "Invalid"
jq '.components.schemas.RollbackResponse | has("example")' api-schema/tmi-openapi.json
jq '.components.schemas.RollbackResponse.properties.restored_entity | has("example")' api-schema/tmi-openapi.json
```

Expected: `Valid`, `true`, `true`

- [ ] **Step 3: Commit**

```bash
git add api-schema/tmi-openapi.json
git commit -m "fix(api): add example to RollbackResponse schema (#185)"
```

---

### Task 6: Add PriorityQueryParam and StatusQueryParam examples

**Files:**
- Modify: `api-schema/tmi-openapi.json` (components.parameters)

- [ ] **Step 1: Add examples to both parameters and their schemas**

```bash
jq '
  .components.parameters.PriorityQueryParam.example = ["high", "critical"] |
  .components.parameters.PriorityQueryParam.schema.example = ["high", "critical"] |
  .components.parameters.StatusQueryParam.example = ["identified", "mitigated"] |
  .components.parameters.StatusQueryParam.schema.example = ["identified", "mitigated"]
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Validate and verify**

```bash
jq empty api-schema/tmi-openapi.json && echo "Valid" || echo "Invalid"
jq '.components.parameters.PriorityQueryParam | {has_example: has("example"), schema_has_example: (.schema | has("example"))}' api-schema/tmi-openapi.json
jq '.components.parameters.StatusQueryParam | {has_example: has("example"), schema_has_example: (.schema | has("example"))}' api-schema/tmi-openapi.json
```

Expected: `Valid`, both `true`/`true`

- [ ] **Step 3: Commit**

```bash
git add api-schema/tmi-openapi.json
git commit -m "fix(api): add examples to PriorityQueryParam and StatusQueryParam (#185)"
```

---

### Task 7: Add ThreatModel, Asset, and Note schema examples

These `allOf`-composed schemas need their own `example` because Vacuum does not propagate examples through `allOf`. This resolves the 3 restore endpoint media type issues.

**Files:**
- Modify: `api-schema/tmi-openapi.json` (components.schemas.ThreatModel, Asset, Note)

- [ ] **Step 1: Add example to ThreatModel schema**

```bash
jq '
  .components.schemas.ThreatModel.example = {
    "id": "f0e1d2c3-b4a5-6789-0abc-def123456789",
    "name": "Payment Service Threat Model",
    "description": "Threat model for the payment processing service",
    "owner": {
      "principal_type": "user",
      "provider": "google",
      "provider_id": "alice@example.com",
      "display_name": "Alice Johnson",
      "email": "alice@example.com"
    },
    "authorization": [
      {
        "principal_type": "user",
        "provider": "google",
        "provider_id": "alice@example.com",
        "display_name": "Alice Johnson",
        "email": "alice@example.com",
        "role": "owner"
      }
    ],
    "threat_model_framework": "STRIDE",
    "status": "in_progress",
    "created_at": "2026-01-10T08:00:00Z",
    "modified_at": "2026-01-15T10:30:00Z"
  }
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Add example to Asset schema**

```bash
jq '
  .components.schemas.Asset.example = {
    "id": "c3d4e5f6-a7b8-9012-cdef-123456789abc",
    "name": "Customer Database",
    "type": "data",
    "description": "Primary PostgreSQL database storing customer PII",
    "classification": ["confidential"],
    "criticality": "high",
    "sensitivity": "high",
    "created_at": "2026-01-11T09:00:00Z",
    "modified_at": "2026-01-14T14:00:00Z"
  }
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 3: Add example to Note schema**

```bash
jq '
  .components.schemas.Note.example = {
    "id": "d4e5f6a7-b8c9-0123-defa-23456789abcd",
    "name": "Security Review Notes",
    "content": "Reviewed authentication flow. Identified potential session fixation risk.",
    "description": "Notes from initial security review session",
    "created_at": "2026-01-12T11:00:00Z",
    "modified_at": "2026-01-13T16:00:00Z"
  }
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 4: Validate and verify all three**

```bash
jq empty api-schema/tmi-openapi.json && echo "Valid" || echo "Invalid"
for s in ThreatModel Asset Note; do
  echo "=== $s ==="
  jq --arg s "$s" '.components.schemas[$s] | has("example")' api-schema/tmi-openapi.json
done
```

Expected: `Valid`, all `true`

- [ ] **Step 5: Commit**

```bash
git add api-schema/tmi-openapi.json
git commit -m "fix(api): add examples to ThreatModel, Asset, and Note schemas (#185)"
```

---

### Task 8: Run validation and verify all issues are resolved

- [ ] **Step 1: Run OpenAPI validation**

```bash
make validate-openapi
```

Expected: `0 errors, 0 warnings, 19 info`

The remaining 19 info items should all be "security is empty" for intentionally public endpoints.

- [ ] **Step 2: Verify only OWASP security items remain**

```bash
jq '[.resultSet.results[] | select(.message | test("missing.*example") or test("missing a description") or test("missing response code"))] | length' test/outputs/api-validation/openapi-validation-report.json
```

Expected: `0`

- [ ] **Step 3: Remove backup file**

```bash
rm -f api-schema/tmi-openapi.json.backup
```

- [ ] **Step 4: Run generate-api to ensure regenerated code is consistent**

```bash
make generate-api
```

Expected: No changes to `api/api.go` (examples don't affect generated types).

- [ ] **Step 5: Run build and unit tests**

```bash
make build-server
make test-unit
```

Expected: Build succeeds, all tests pass.

- [ ] **Step 6: Squash-friendly summary commit (if desired)**

If you want a single commit for the whole change, this is the place to squash. Otherwise the per-task commits provide a clean history.
