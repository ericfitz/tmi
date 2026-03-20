# Design: Nullable Optional Fields & PUT Replacement Semantics

**Date:** 2026-03-20
**Issues:** #200, #208
**Branch:** release/1.3.0

## Problem

Two related bugs prevent users from clearing optional fields once set:

1. **#200 — Schema**: Optional string fields with format constraints (uri, email, uuid) lack `nullable: true`, so clients cannot send `null` to clear them. The format validators reject empty strings, leaving no valid "clear" value. Plain string fields without format constraints (description, mitigation, etc.) have a subtler problem: the store layer's conditional map building and GORM's zero-value skipping silently preserve old values when the field is omitted or nil.

2. **#208 — PUT semantics**: Some PUT handlers use merge/PATCH-like semantics (preserving omitted fields) instead of full resource replacement. Combined with #200, users have no way to clear fields through either omission or explicit null.

## Solution

### Part 1: OpenAPI Schema — Add `nullable: true`

Add `nullable: true` to 18 optional string fields across 7 schemas. These are user-editable fields not in the `required` array that users should be able to clear.

| Schema | Fields |
|--------|--------|
| ThreatBase | `description`, `mitigation`, `issue_uri`, `severity`, `priority`, `status` |
| ThreatModelBase | `description`, `issue_uri` |
| ThreatModelInput | `threat_model_framework` |
| ProjectBase | `description`, `uri` |
| RepositoryBase | `name`, `type` |
| TeamBase | `description`, `uri`, `email_address` |
| SurveyBase | `description`, `status` |

Fields already correctly nullable (no changes needed): ThreatBase `diagram_id`/`cell_id`/`asset_id`/`metadata`, ThreatModelBase `status`/`project_id`/`metadata`/`security_reviewer`, ThreatModelInput `description`/`issue_uri`/`metadata`, AssetBase `description`/`criticality`/`sensitivity`/`classification`, ProjectBase `status`, TeamBase `status`, SurveyResponseBase `linked_threat_model_id`/`project_id`/`ui_state`, DocumentBase `description`, RepositoryBase `description`.

**TriageNoteBase**: Only has `name` and `content`, both required. No changes needed.

After schema changes: `make validate-openapi && make generate-api`.

### Part 2: Store Layer — Map-Based Updates

Convert all store `Update` methods to map-based `Updates()` that explicitly include all fields. When an API field is nil, the corresponding map entry must be set to `nil` so GORM writes NULL to the database.

The current problem: struct-based `Updates()` skips Go zero-value fields, and conditional map building (`if field != nil { map[key] = field }`) omits nil fields entirely. Both prevent clearing.

**Database columns**: All affected fields in the GORM models already use pointer types (`*string`), so the database columns accept NULL. No migration needed.

**Stores requiring changes:**

| Store | File | Current Pattern | Change |
|-------|------|-----------------|--------|
| GormThreatStore | threat_store_gorm.go | Struct-based `Updates()` via `toGormModel()` | Convert to map-based; include all nullable fields unconditionally |
| GormAssetStore | asset_store_gorm.go | Fetch existing + selective merge + `Save()` | Convert to map-based; include all fields unconditionally |
| GormDocumentStore | document_store_gorm.go | Map-based but conditional for `IncludeInReport`/`TimmyEnabled` | Include all fields unconditionally |
| GormRepositoryStore | repository_store_gorm.go | Map-based but conditional for `IncludeInReport`/`TimmyEnabled` | Include all fields unconditionally |
| GormNoteStore | note_store_gorm.go | Map-based but conditional for `IncludeInReport`/`TimmyEnabled` | Include all fields unconditionally |
| GormSurveyResponseStore | survey_response_store_gorm.go | Map starts empty; only adds non-nil fields (`answers`, `ui_state`, `project_id`) | Include all updatable fields unconditionally; also add `linked_threat_model_id` which is currently missing entirely |
| GormProjectStore | project_store_gorm.go | Map-based but conditional for `description` and `uri` | Include `description` and `uri` unconditionally |
| GormThreatModelStore | database_store_gorm.go | Map-based but conditional for `alias` | Include `alias` unconditionally |

**Stores already correct (verification only):**

| Store | File | Notes |
|-------|------|-------|
| GormTeamStore | team_store_gorm.go | Already handles `email_address` nil case correctly (sets to nil in map) |
| GormSurveyStore | survey_store_gorm.go | Verify all nullable fields included |

**Pattern for nullable fields in maps:**

```go
// Before (broken — nil fields silently preserved):
if threat.Description != nil {
    updates["description"] = threat.Description
}

// After (correct — nil writes NULL):
updates["description"] = threat.Description  // nil => NULL in DB
```

**Special case — ThreatStore custom types:** The threat store currently uses struct-based `Updates()` because map-based `Updates()` bypasses GORM custom type `Value()` methods (e.g., `StringArray`, `CVSSArray`). The fix: call `Value()` explicitly when building the map.

```go
updates["threat_type"] = models.StringArray(threat.ThreatType)  // calls Value() on exec
updates["cvss"] = cvssArray                                      // pre-serialized
```

### Part 3: Handler Layer — Remove Merge Logic

**UpdateThreatModel** (threat_model_handlers.go, lines 418-476): Remove the field-preservation pattern that checks `if request.Field != nil { use it } else { keep existing }` for:
- `Owner` (lines 418-429)
- `ThreatModelFramework` (lines 431-435)
- `Authorization` (lines 437-441)
- `Metadata` (lines 443-447)
- `SecurityReviewer` (lines 449-453)

The handler should construct the updated object directly from the request body. Omitted fields should become nil/zero, which the store writes as NULL.

**Note on `ThreatModelFramework`:** The PUT endpoint uses `ThreatModelInput` as its request body schema, where `threat_model_framework` is optional (only `name` is required). Removing the merge logic is still safe because the store layer (`database_store_gorm.go`) defaults an empty framework to `DefaultThreatModelFramework`. If the client omits the field, it becomes an empty string, and the store applies the default.

Other PUT handlers (threats, documents, notes, etc.) already pass the request body directly to the store — no handler changes needed there.

**Server-managed fields** that handlers must still preserve regardless of request content:
- `created_at`, `modified_at` (set by server)
- `created_by` (set by server from auth context; note: `modified_by` is not currently set by the UpdateThreatModel handler — this is a pre-existing gap, out of scope for this change)
- `id` (from URL path, not request body)
- Sub-entities (diagrams, threats, documents) — managed via their own endpoints
- `is_confidential` — immutable after creation

### Part 4: Scope Boundaries

**In scope:**
- OpenAPI schema `nullable: true` additions
- Store Update method changes to map-based with unconditional field inclusion
- UpdateThreatModel handler merge logic removal
- Regenerating API code after schema changes

**Out of scope:**
- PATCH endpoints (already work via JSON Patch, unaffected)
- Metadata bulk replace (already correct replacement semantics)
- In-memory stores (none exist for Update methods)
- Create operations (not affected)
- Diagram PUT (cells are managed via WebSocket, not PUT body)
- TriageNoteBase (both fields required, no nullable changes needed)

## Testing

- **Unit tests**: For each affected resource, verify PUT with a nullable field omitted clears it (field is nil/NULL after update)
- **Integration tests**: Full round-trip: create with value, PUT without it, GET confirms cleared. Both PostgreSQL and Oracle.
- **Regression**: All existing tests must pass — valid PUT requests with all fields populated should continue working identically
- **CATS fuzzing**: Re-run after changes to verify no new 500 errors from nullable field handling

## Risks

- **Client compatibility**: Clients sending partial PUT bodies (omitting fields they don't want to change) will now see those fields cleared. This is correct REST behavior but could surprise clients relying on the current merge behavior. The TMI client (tmi-ux) already sends full resource representations on PUT, so this should not be an issue.
- **Oracle NULL handling**: Oracle treats empty strings as NULL. The map-based approach writes explicit NULL for nil fields, which is consistent with Oracle's behavior. No special handling needed.
- **Rollback**: If unexpected breakage occurs, the changes can be reverted as a single commit. No database migrations are involved — all affected columns already accept NULL values in their GORM model definitions (`*string` pointer types). The schema change is additive (`nullable: true` is less restrictive), so rolling back the schema would be the only breaking direction.
