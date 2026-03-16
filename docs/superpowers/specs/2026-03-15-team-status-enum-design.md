# Design: Team Status Enum

**Issue**: [#181](https://github.com/ericfitz/tmi/issues/181) — fix: team status should be an enum, not a freeform string
**Date**: 2026-03-15
**Status**: Approved

## Problem

`TeamBase.status` is a freeform `string` with `maxLength: 128` and a control-character pattern. This allows any arbitrary value, providing no server-side validation. The UX needs a constrained set of values for dropdown presentation, and data consistency requires server enforcement.

## Design

### Approach: Inline Enum in OpenAPI + Server-Side Default

Follows the existing pattern used by other enums in the spec (role, shape, diagram type, health status) — inline enum on the property, no separate schema.

### Enum Values

| Value | Meaning |
|-------|---------|
| `active` | Team is operational (default) |
| `on_hold` | Team activities are paused |
| `winding_down` | Team is being decommissioned |
| `archived` | Team is no longer active |
| `forming` | Team is being assembled |
| `merging` | Team is merging with another team |
| `splitting` | Team is splitting into multiple teams |

### OpenAPI Schema Changes

#### 1. `TeamBase.properties.status`

**Before:**
```json
{
  "type": "string",
  "maxLength": 128,
  "pattern": "^[^\\x00-\\x1F]*$",
  "description": "Team status (lifecycle, archival, deprecation, etc.)",
  "example": "active"
}
```

**After:**
```json
{
  "type": "string",
  "nullable": true,
  "enum": ["active", "on_hold", "winding_down", "archived", "forming", "merging", "splitting"],
  "description": "Team lifecycle status. Defaults to 'active' if not provided or set to null.",
  "example": "active"
}
```

- `nullable: true` — the field can be explicitly set to JSON `null`, which triggers the server-side default to `"active"`. The field is also omittable (it is not in the `required` array).
- `maxLength` and `pattern` removed — enum is the validator.
- OpenAPI validation middleware rejects invalid non-null values before they reach handlers.

#### 2. `TeamListItem.properties.status`

Currently a separate freeform definition:
```json
{ "type": "string", "maxLength": 128, "nullable": true }
```

**Update to match** the same enum constraint:
```json
{
  "type": "string",
  "nullable": true,
  "enum": ["active", "on_hold", "winding_down", "archived", "forming", "merging", "splitting"],
  "description": "Team lifecycle status. Defaults to 'active' if not provided or set to null.",
  "example": "active"
}
```

#### 3. `TeamInput`

`TeamInput` is defined as `$ref: "#/components/schemas/TeamBase"`, so it inherits the enum constraint automatically. No change needed.

#### 4. List Teams Query Parameter (`GET /teams?status=...`)

The `status` query parameter currently accepts a freeform string. Update its schema to use an `items`-based enum so the server validates filter values:

**Before:**
```json
{
  "name": "status",
  "in": "query",
  "schema": { "type": "string", "maxLength": 256 },
  "description": "Filter by status (exact match, comma-separated for multiple)"
}
```

**After:**
```json
{
  "name": "status",
  "in": "query",
  "schema": { "type": "string", "maxLength": 256 },
  "description": "Filter by team lifecycle status (exact match, comma-separated for multiple). Valid values: active, on_hold, winding_down, archived, forming, merging, splitting"
}
```

Note: We keep this as a plain string (not enum) because it supports comma-separated multiple values. The description documents valid values. Invalid filter values will silently match no records (existing behavior for all list filters) — this is acceptable for query parameters and does not need additional validation.

### Server-Side Default Logic

In `team_store_gorm.go`, in both the `Create` and `Update` methods:

- If `team.Status` is nil, set it to `"active"` before writing the record.
- The `PatchTeam` handler applies JSON Patch operations to the existing team struct, then calls `Update`. So defaulting in `Update` covers the PATCH path — no separate PATCH logic is needed.
- This means nulling status via PATCH (`{"op": "replace", "path": "/status", "value": null}`) resets it to `"active"`. This is **intentional** — teams should always have a lifecycle status. The API description documents this: "Defaults to 'active' if not provided or set to null."

### Generated Type and Store Type Conversions

oapi-codegen will likely generate a named type (e.g., `TeamBaseStatus`) with constants. The exact generated type name should be confirmed after running `make generate-api`. The API type for `Team.Status` and `TeamListItem.Status` will change from `*string` to a pointer to the generated enum type.

The GORM model (`TeamRecord.Status`) remains `*string`. The following store code needs type conversion:

- **`recordToAPI` method**: `team.Status = record.Status` → convert `*string` to enum pointer
- **`Create` method**: `Status: team.Status` → convert enum pointer to `*string`
- **`Update` method**: `"status": team.Status` → convert enum pointer to `*string`
- **`TeamListItem` population** in `List` method → same conversion

These are simple casts: `EnumType(str)` and `string(enumVal)`.

### Database

No migration required. Existing `varchar(128)` column accommodates all enum values.

**Existing non-enum data**: Rows with null status get the default on next update. Rows with non-enum string values (if any exist) will be returned as-is by GET until updated. This is a known temporary inconsistency — the `recordToAPI` conversion will map unknown values by casting them to the enum type. Go doesn't enforce enum values at runtime, so these will serialize correctly. They will be corrected to valid values on next write, since the OpenAPI middleware validates input.

### Testing

- Update existing unit tests for Create/Update/Patch to verify:
  - Omitting status on create defaults to `"active"`
  - Nulling status via PATCH defaults to `"active"`
  - Explicit valid enum values are accepted and persisted
- Verify `TeamListItem` status field is correctly typed in list responses
- OpenAPI middleware handles invalid value rejection (no store-level test needed for invalid values)
- Run integration tests (`make test-integration`) to verify enum values round-trip correctly through PostgreSQL

## Alternatives Considered

1. **Separate `TeamStatus` schema with `$ref`** — Rejected: no other enum in the spec uses this pattern; would be inconsistent.
2. **Database CHECK constraint** — Rejected: requires migration, existing null/non-enum rows would need data migration, GORM auto-migrate doesn't handle CHECK constraints well, and OpenAPI middleware already validates.
