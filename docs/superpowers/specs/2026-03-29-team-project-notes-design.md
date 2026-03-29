# Team and Project Notes Design

**Issue:** [#217 — feat: team and project notes](https://github.com/ericfitz/tmi/issues/217)
**Date:** 2026-03-29
**Status:** Draft

## Summary

Add notes as full sub-resources to teams and projects, following the existing threat model notes pattern. Notes support markdown content with a role-based `sharable` visibility flag that enables security reviewers and admins to create internal-only notes.

## Schema

### TeamProjectNoteBase (user-writable fields)

| Field | Type | Required | Constraints | Description |
|-------|------|----------|-------------|-------------|
| `name` | string | yes | 1-256 chars | Note name |
| `content` | string | yes | 1-262144 chars, markdown | Note content |
| `description` | string | no | max 2048 chars, nullable | Description of note purpose |
| `timmy_enabled` | boolean | no | default true | Whether Timmy AI assistant is enabled |
| `sharable` | boolean | no | role-restricted | Controls visibility (see Authorization) |

### TeamNote / ProjectNote (full object)

Extends `TeamProjectNoteBase` with server-generated fields:

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID | readOnly |
| `created_at` | datetime | readOnly |
| `modified_at` | datetime | readOnly |

### TeamNoteListItem / ProjectNoteListItem

All fields from the full object **except `content`** (omitted to keep list payloads small):

`id`, `name`, `description`, `timmy_enabled`, `sharable`, `created_at`, `modified_at`

### Input types

`TeamNoteInput` / `ProjectNoteInput` — aliases for `TeamProjectNoteBase`.

### Differences from threat model notes

- No `include_in_report` field
- No `metadata` array or metadata sub-resource endpoints
- No soft-delete/restore — hard delete only
- New `sharable` field with role-based access control

## Endpoints

### Team Notes

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/teams/{team_id}/notes` | List notes (paginated) |
| `POST` | `/teams/{team_id}/notes` | Create note |
| `GET` | `/teams/{team_id}/notes/{note_id}` | Get note |
| `PUT` | `/teams/{team_id}/notes/{note_id}` | Replace note |
| `PATCH` | `/teams/{team_id}/notes/{note_id}` | JSON Patch update |
| `DELETE` | `/teams/{team_id}/notes/{note_id}` | Hard delete |

### Project Notes

Same pattern at `/projects/{project_id}/notes[/{note_id}]`.

### Pagination

`limit` and `offset` query parameters on list endpoints, matching existing API patterns.

## Authorization

### The `sharable` flag

The `sharable` boolean controls note visibility. Its default and mutability depend on the user's role:

| | Regular team/project member | Admin / Security reviewer |
|---|---|---|
| **Default value** | `true` (always) | `false` |
| **Can set explicitly** | No — including `sharable` in request returns 403 | Yes — can set to `true` or `false` |
| **Can modify** | No — including `sharable` in PATCH returns 403 | Yes — can change in either direction |

### Per-endpoint authorization

| Action | Regular member | Admin / Security reviewer |
|--------|---------------|--------------------------|
| **List** | Sees only `sharable=true` notes | Sees all notes |
| **Get** | Only `sharable=true`; 404 for non-sharable | Any note |
| **Create** | Always `sharable=true`; 403 if `sharable` field included | Defaults `sharable=false`; can set explicitly |
| **Update/Patch** | Only `sharable=true` notes; 403 if `sharable` field included | Any note; can change `sharable` |
| **Delete** | Only `sharable=true` notes; 404 for non-sharable | Any note |

### Information hiding

Non-privileged users receive **404** (not 403) when accessing non-sharable notes, to avoid leaking existence.

### Prerequisite checks

Before any note operation:
- Teams: `IsTeamMemberOrAdmin` — user must be a team member
- Projects: `IsProjectTeamMemberOrAdmin` — user must be a member of the project's parent team

## Database

### team_notes table

| Column | Type | Constraints |
|--------|------|-------------|
| `id` | varchar(36) | PK |
| `team_id` | varchar(36) | NOT NULL, FK → team_records.id ON DELETE CASCADE |
| `name` | varchar(256) | NOT NULL, indexed |
| `content` | DBText | NOT NULL |
| `description` | varchar(2048) | nullable |
| `timmy_enabled` | DBBool | default true |
| `sharable` | DBBool | NOT NULL |
| `created_at` | timestamp | NOT NULL, auto |
| `modified_at` | timestamp | NOT NULL, auto |

**Indexes:** `(team_id)` for listing, `(team_id, name)` composite for lookups.

### project_notes table

Identical structure with `project_id` FK → project_records.id instead of `team_id`.

**Indexes:** `(project_id)` for listing, `(project_id, name)` composite for lookups.

### GORM types

Use GORM-friendly types for cross-database compatibility (PostgreSQL and Oracle):
- `DBText` for content (maps to appropriate text type per DB)
- `DBBool` for boolean fields (handles Oracle's lack of native boolean)
- Standard GORM tags for timestamps (`autoCreateTime`, `autoUpdateTime`)

### Cascade delete

Deleting a team or project cascades to delete all associated notes via FK constraint.

### Migration

Single migration file adding both tables.

## Store Layer

### TeamNoteStoreInterface

```
Create(ctx, note, teamID) → (*TeamNote, error)
Get(ctx, id) → (*TeamNote, error)
Update(ctx, id, note, teamID) → (*TeamNote, error)
Delete(ctx, id) → error
Patch(ctx, id, operations) → (*TeamNote, error)
List(ctx, teamID, offset, limit, includeNonSharable) → ([]TeamNoteListItem, int, error)
Count(ctx, teamID, includeNonSharable) → (int, error)
```

### ProjectNoteStoreInterface

Same shape with `projectID` instead of `teamID`.

### Default handling for `sharable`

The database column has no default — the handler is responsible for setting the value before insert:
- Regular users: handler forces `sharable=true` (user cannot provide the field)
- Admins/security reviewers: handler defaults to `sharable=false` if not explicitly provided

### Implementation details

- `includeNonSharable` parameter on List/Count: handlers pass `true` for admins/security reviewers, `false` for regular users, filtering at the query level
- GORM implementations: `GormTeamNoteStore`, `GormProjectNoteStore`
- Registered in `InitializeGormStores`
- No cache layer (lower-traffic than threat model notes)

## Handler Layer

### Files

- `team_note_handlers.go` — handlers for `/teams/{team_id}/notes[/{note_id}]`
- `project_note_handlers.go` — handlers for `/projects/{project_id}/notes[/{note_id}]`

### Authorization flow

1. Extract user identity and roles from JWT context
2. Check team/project membership (`IsTeamMemberOrAdmin` / `IsProjectTeamMemberOrAdmin`) — 403 if not
3. Check `IsAdmin` or `IsSecurityReviewer` for privilege level
4. Apply role-based logic per the authorization table above

### JSON Patch

Validate that patch operations targeting `/sharable` are rejected with 403 for non-privileged users.

## Testing

### Unit tests

- **Store tests** for both stores: CRUD operations, list filtering by `sharable`, count with/without non-sharable
- **Handler tests** for authorization:
  - Regular user cannot see/modify non-sharable notes
  - Regular user gets 403 when including `sharable` in request
  - Admin/security reviewer sees all notes, can set `sharable`
  - 404 (not 403) for non-sharable notes accessed by regular users
  - JSON Patch operations on `/sharable` rejected for regular users
- **Cascade delete**: deleting a team/project removes its notes

### Integration tests

- Full API flow: create team → create notes (sharable and non-sharable) → list/get/update/patch/delete with different user roles
- Pagination verification on list endpoints

### CATS fuzzing

New endpoints are automatically picked up by CATS. No special configuration needed since these endpoints require authentication.
