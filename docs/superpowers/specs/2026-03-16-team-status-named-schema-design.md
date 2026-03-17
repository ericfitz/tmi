# Design: Extract TeamBase Inline Status Enum to Named TeamStatus Schema

**Issue**: [#185](https://github.com/ericfitz/tmi/issues/185)
**Date**: 2026-03-16
**Branch**: release/1.3.0

## Problem

`TeamBase.status` defines its enum values inline, which causes oapi-codegen to generate three separate but identical types: `TeamBaseStatus`, `TeamStatus`, and `TeamListItemStatus`. This is inconsistent with the named-schema-with-`$ref` pattern established by `ProjectStatus` in #184, and the redundant types require unnecessary casts in handler code (e.g., `(*TeamStatus)(req.Status)`).

## Approach

Extract the inline enum to a named `TeamStatus` schema in `components/schemas` and reference it via `$ref` from all three locations (`TeamBase`, `Team`, `TeamListItem`). This collapses three generated types into one unified `TeamStatus` type.

No API-level breaking change — the JSON wire format is identical.

## OpenAPI Schema Changes

### New schema: `TeamStatus`

Add to `components/schemas`:

```json
"TeamStatus": {
  "type": "string",
  "enum": ["active", "on_hold", "winding_down", "archived", "forming", "merging", "splitting"],
  "description": "Team lifecycle status. Defaults to 'active' if not provided or set to null."
}
```

### Updated references

Since the project uses OpenAPI 3.0.3, `$ref` cannot have sibling properties (like `nullable`). Use the `allOf` wrapper pattern established by `ProjectStatus`:

1. **`TeamBase.status`** — replace the inline enum with:
   ```json
   "status": {
     "nullable": true,
     "allOf": [{ "$ref": "#/components/schemas/TeamStatus" }],
     "description": "Team lifecycle status. Defaults to 'active' if not provided or set to null.",
     "example": "active"
   }
   ```

2. **`TeamListItem.status`** — same `allOf` + `nullable` wrapper pattern

3. **`Team.status`** — inherits from `TeamBase` via `allOf`. The `Team` schema does not define its own `status` property, so it picks up the `$ref` automatically. No separate change needed.

4. **`GET /teams` status query parameter** — remains free-text string (comma-separated filter). Description already lists valid values (updated in #181). No change needed.

Note: No `TeamStatus` named schema currently exists in `components/schemas` — this is a new schema creation.

## Generated Code Impact

After `make generate-api`, oapi-codegen produces:
- Single `type TeamStatus string` with 7 constants (`TeamStatusActive`, `TeamStatusOnHold`, etc.)
- `TeamBase.Status` changes from `*TeamBaseStatus` to `*TeamStatus`
- `TeamInput` is a type alias for `TeamBase`, so `TeamInput.Status` also changes from `*TeamBaseStatus` to `*TeamStatus`
- `Team.Status` remains `*TeamStatus` (same name, but now references the named schema type instead of a separate inline-generated type)
- `TeamListItem.Status` changes from `*TeamListItemStatus` to `*TeamStatus`

The old types `TeamBaseStatus` and `TeamListItemStatus` are eliminated, along with their associated constants (`TeamBaseStatusActive`, `TeamListItemStatusActive`, etc.). Exact constant names for the new `TeamStatus` type should be confirmed after code generation.

## Store Layer Changes

**File**: `api/team_store_gorm.go`

- `stringToTeamListItemStatus` function — **remove** (no longer needed; `TeamListItemStatus` type is gone)
- `teamStatusToString` — no change needed (already converts `*TeamStatus` to `*string`)
- `stringToTeamStatus` — no change needed (already converts `*string` to `*TeamStatus`)
- All call sites that used `stringToTeamListItemStatus` switch to `stringToTeamStatus`
- Defaulting logic (`const teamStatusDefault = "active"`) — unchanged

## Handler Changes

**File**: `api/team_handlers.go`

- Remove type casts `(*TeamStatus)(req.Status)` in `CreateTeam` (line ~108) and `UpdateTeam` (line ~211) — `req.Status` is now directly `*TeamStatus` since `TeamInput` (alias for `TeamBase`) uses the same type
- `PatchTeam` has no cast to remove (uses `ApplyPatchOperations` → `Update`), but benefits from unified type in the store layer

### Other files

Grep for `TeamBaseStatus`, `TeamListItemStatus`, and `stringToTeamListItemStatus` across the codebase to ensure no references are missed outside the primary files listed above.

## Test Changes

**File**: `api/team_handlers_test.go`

- Update any references to `TeamBaseStatus` constants to use `TeamStatus` constants
- Add tests mirroring the ProjectStatus pattern from #184:
  - `TestCreateTeamWithStatus`: Explicit status preserved
  - `TestUpdateTeamWithStatus`: Valid status applied
  - `TestListTeamsWithStatus`: Listed items include typed status
  - `TestPatchTeamWithStatus`: Patch operation on status field

## Database

No migration needed. Column stays `varchar(128)`. Enum is enforced at the API layer.

## Related Issues

- [#184](https://github.com/ericfitz/tmi/issues/184): Establishes the named-schema-with-`$ref` pattern for `ProjectStatus`
- [#181](https://github.com/ericfitz/tmi/issues/181): Original issue that added the inline Team status enum
