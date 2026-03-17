# Design: OpenAPI Validation Fixes

**Date**: 2026-03-16
**Issue**: #185 (branch cleanup — validation warnings/info on release/1.3.0)
**Scope**: `api-schema/tmi-openapi.json` only — no Go code changes

## Problem

`make validate-openapi` reports 0 errors, 2 warnings, 46 info (score 94/100). The 2 warnings and 27 of the info items are actionable. The remaining 19 info items are intentional (public endpoints with empty security).

## Changes

### Warning 1: `WsTicketResponse` missing description

Add `"description"` field to `components.schemas.WsTicketResponse`.

Value: `"Response containing a short-lived, single-use authentication ticket for WebSocket connection"`

### Warning 2: `/ws/ticket` GET missing 500 response

Add a `"500"` response to `paths["/ws/ticket"].get.responses` referencing `components.schemas.Error`, with rate limit headers matching the existing sibling responses (400, 401, 404).

### Info: Missing examples (27 items)

Add `"example"` at the schema/component level. For schemas referenced via direct `$ref`, this propagates to referencing endpoints automatically. For `allOf` composed schemas (`ThreatModel`, `Asset`, `Note`), Vacuum does not propagate examples through `allOf`, so those schemas need their own `example` added directly.

#### `AuditEntry` schema example

```json
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
```

Also add `"example": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"` to `AuditEntry.properties.id`.

#### `ListAuditTrailResponse` schema example

```json
{
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
}
```

Also add example to `audit_entries` property: same array with one entry.

#### `RollbackResponse` schema example

Uses a concrete threat model as the restored entity. Note: `owner` is a `User` object (via `$ref` to `User` schema), and all required `ThreatModelBase` fields (`name`, `owner`, `authorization`, `threat_model_framework`) are included.

```json
{
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
}
```

Also add example to `restored_entity` property (same object).

#### `WsTicketResponse` schema example

```json
{
  "ticket": "tmi_ws_abc123def456"
}
```

Also add `"example": "tmi_ws_abc123def456"` to `WsTicketResponse.properties.ticket`.

#### `PriorityQueryParam` parameter

Add `"example"` to both the parameter and its schema:

```json
"example": ["high", "critical"]
```

#### `StatusQueryParam` parameter

Add `"example"` to both the parameter and its schema:

```json
"example": ["identified", "mitigated"]
```

#### Restore endpoint schemas (`ThreatModel`, `Asset`, `Note`)

These three schemas use `allOf` composition. Vacuum does not propagate examples through `allOf`, so each needs its own `example` added directly to the schema. The three restore endpoints (`POST .../restore`) return these schemas and are flagged for missing examples.

**`ThreatModel` schema example:**

```json
{
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
```

**`Asset` schema example:**

```json
{
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
```

**`Note` schema example:**

```json
{
  "id": "d4e5f6a7-b8c9-0123-defa-23456789abcd",
  "name": "Security Review Notes",
  "content": "Reviewed authentication flow. Identified potential session fixation risk.",
  "description": "Notes from initial security review session",
  "created_at": "2026-01-12T11:00:00Z",
  "modified_at": "2026-01-13T16:00:00Z"
}
```

## What this does NOT change

- The 19 "empty security" OWASP info items — these are intentionally public endpoints marked with `x-public-endpoint`.

## Expected result

After these changes, `make validate-openapi` should report: 0 errors, 0 warnings, 19 info (all expected public endpoint security items).
