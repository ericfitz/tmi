# Design: Add `timmy_enabled` boolean property to all sub-entities

**Issue**: [#183](https://github.com/ericfitz/tmi/issues/183)
**Date**: 2026-03-15
**Status**: Approved

## Overview

Add a `timmy_enabled` boolean property to 6 sub-entity types within threat models. This property controls whether the Timmy AI assistant feature is enabled for the entity. Follows the same pattern as the existing `include_in_report` property.

TriageNote is intentionally excluded — it has a different purpose (ephemeral triage content) and does not carry `include_in_report` or other reporting/feature-flag properties.

## Affected Schemas

The property must be added to each schema that currently carries `include_in_report`. These are:

| Schema             | Role                          |
|--------------------|-------------------------------|
| AssetBase          | Base properties for Asset     |
| DocumentBase       | Base properties for Document  |
| RepositoryBase     | Base properties for Repository|
| NoteBase           | Base properties for Note      |
| ThreatBase         | Base properties for Threat    |
| BaseDiagram        | Base properties for Diagram   |
| BaseDiagramInput   | Diagram creation/update input (independent, not inherited from BaseDiagram) |
| DiagramListItem    | Diagram list response item    |
| NoteListItem       | Note list response item       |

**Note on diagrams**: `BaseDiagramInput` does NOT use `allOf` to reference `BaseDiagram`. It defines its properties independently. The property must be added to both schemas separately, matching the `include_in_report` pattern.

For the other 5 entity types, the `*Input` schemas reference their `*Base` via `allOf`, so the property is inherited automatically.

## Property Definition

```json
"timmy_enabled": {
  "type": "boolean",
  "default": true,
  "description": "Whether the Timmy AI assistant is enabled for this entity"
}
```

- **Type**: boolean
- **Default**: true
- **Nullable**: no
- **Required**: no

## Server-side Behavior

The Go generated types use `*bool` for optional booleans. The codebase uses the custom `DBBool` type (from `api/models/models.go`) for database storage, which handles the Go zero-value problem — distinguishing between "omitted" and "explicitly false".

- **Creation (POST)**: If `timmy_enabled` is omitted (`nil` pointer), defaults to `true`
- **Full replacement (PUT)**: If `timmy_enabled` is omitted (`nil` pointer), defaults to `true`
- **Partial update (PATCH)**: Standard RFC 6902 operations; remove resets to database default (`true`)
- **Read (GET)**: Always included in response

Conversion between API `*bool` and DB `DBBool` follows the existing `IncludeInReport` pattern in each `*_store_gorm.go` file.

## Database Changes

Add `timmy_enabled` column to each entity's table (assets, documents, repositories, notes, diagrams, threats):
- Go type: `DBBool`
- GORM tag: `gorm:"default:1"`
- Migration adds column with default so existing rows automatically get `true`

## No Changes Required

- **Endpoints**: No new routes needed
- **RBAC/Auth**: Same permissions as parent entity
- **WebSocket**: Diagram WS protocol is for cell editing, not metadata
- **Filtering/Sorting**: Not needed for this property at this time
- **TriageNote**: Excluded (no `include_in_report`, different entity purpose)

## Testing

- Unit tests for default behavior on create and update for each entity type
- Integration tests for CRUD operations including PATCH remove behavior
- CATS fuzzing automatically covers the new property once added to the OpenAPI spec
