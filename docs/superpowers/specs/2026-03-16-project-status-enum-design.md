# Design: Project Status Enum

**Issue**: [#184](https://github.com/ericfitz/tmi/issues/184)
**Date**: 2026-03-16
**Branch**: release/1.3.0

## Problem

The Project resource's `status` field is defined as free-text (`type: string`, `maxLength: 128`) in the OpenAPI spec. This prevents reliable dropdown filters in the UI and leads to inconsistent data. The Team resource already uses a constrained enum for its status field.

## Approach

Create a named `ProjectStatus` enum schema in `components/schemas` and reference it via `$ref` from `ProjectBase.status` and `ProjectListItem.status`. This establishes the named-schema-with-ref pattern (a follow-up issue [#185](https://github.com/ericfitz/tmi/issues/185) will align TeamBase to the same pattern).

The enum values are not a state machine — they are unconstrained buckets. No transition validation is needed.

## OpenAPI Schema Changes

### New schema: `ProjectStatus`

```json
"ProjectStatus": {
  "type": "string",
  "enum": [
    "active", "planning", "on_hold", "cancelled",
    "in_development", "in_review", "mvp",
    "limited_availability", "general_availability",
    "deprecated", "end_of_life", "archived"
  ],
  "description": "Project lifecycle status. Defaults to 'active' if not provided or set to null."
}
```

### Updated references

Since the project uses OpenAPI 3.0.3, `$ref` cannot have sibling properties (like `nullable`). Use the `allOf` wrapper pattern:

1. **`ProjectBase.status`** — replace the free-text string definition with:
   ```json
   "status": {
     "nullable": true,
     "allOf": [{ "$ref": "#/components/schemas/ProjectStatus" }],
     "description": "Project lifecycle status. Defaults to 'active' if not provided or set to null.",
     "example": "active"
   }
   ```
   This removes the existing `maxLength: 128` and `pattern` constraints, which are superseded by the enum validation.

2. **`ProjectListItem.status`** — same `allOf` + `nullable` wrapper pattern

3. **`GET /projects` status query parameter** — remains free-text string (comma-separated filter accepting multiple enum values)

## Generated Code Impact

After `make generate-api`, oapi-codegen produces:
- `type ProjectStatus string` with 12 constants (`ProjectStatusActive`, `ProjectStatusPlanning`, etc.)
- `ProjectBase.Status` changes from `*string` to `*ProjectStatus`
- `ProjectListItem.Status` changes from `*string` to `*ProjectStatus`

**Note**: Unlike the current Team pattern (which uses inline enums producing separate `TeamBaseStatus` and `TeamListItemStatus` types), both `ProjectBase` and `ProjectListItem` reference the same named schema, so oapi-codegen generates a single unified `ProjectStatus` type for both.

## Store Layer Changes

**File**: `api/project_store_gorm.go`

The GORM model (`ProjectRecord`) stores status as `*string`. Add conversion functions mirroring `team_store_gorm.go`:

- `projectStatusToString(*ProjectStatus) *string` — for writing to DB
- `stringToProjectStatus(*string) *ProjectStatus` — for reading from DB
- `const projectStatusDefault = "active"` — applied when status is nil on create

### Specific code locations requiring changes

1. **`Create` method** (~line 83): Currently does `Status: project.Status` with no nil defaulting. Add nil-check-and-default block mirroring Team's pattern (`team_store_gorm.go` lines 116-119), then use `projectStatusToString()` when building the record.

2. **`Update` method** (~line 248): The conditional `if project.Status != nil { updates["status"] = *project.Status }` needs conversion via `string(*project.Status)` or `projectStatusToString()`. Also add nil-defaulting to "active" on Update, mirroring Team's pattern (`team_store_gorm.go` lines 325-328).

3. **`recordToAPI` method** (~line 763): Currently does `Status: record.Status` where both are `*string`. Must change to `Status: stringToProjectStatus(record.Status)`.

4. **`List` method result building** (~line 473): Currently does `Status: r.Status` from a raw SQL result struct (`*string`). Must change to `Status: stringToProjectStatus(r.Status)`.

## Handler Changes

**File**: `api/project_handlers.go`

- `CreateProject`: `req.Status` type changes from `*string` to `*ProjectStatus` — the store's Create method handles conversion
- `UpdateProject`: Same type change, store's Update method handles conversion
- `PatchProject`: Uses `ApplyPatchOperations` which works via JSON marshal/unmarshal — `*ProjectStatus` will deserialize correctly from JSON patch string values. Worth explicit test coverage.
- Status filter parsing (`GET /projects`) stays as `[]string` since query param is free-text

## Test Changes

- **`api/project_handlers_test.go`**: Use `ProjectStatus(...)` typed values instead of raw string pointers
- **`test/integration/framework/fixtures.go`**: The `WithStatus` helper takes a `string` and the fixture sends JSON over HTTP, so the Go type itself may not need to change. Ensure all fixture status values use valid enum values (they go through OpenAPI validation).
- **OpenAPI examples**: `ProjectBase.status` already uses `"example": "active"` which is a valid enum value. Verify no other examples need updating.

## Database

No migration needed. Column stays `varchar(128)`. Enum is enforced at the API layer by OpenAPI validation middleware. Existing non-enum values remain readable but will fail validation on write.

## Related Issues

- [#185](https://github.com/ericfitz/tmi/issues/185): Extract TeamBase inline status enum to named TeamStatus schema (follow-up to align patterns)
