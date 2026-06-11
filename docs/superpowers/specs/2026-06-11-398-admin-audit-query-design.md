# Design: Admin audit query endpoints (#398)

**Issue:** [#398](https://github.com/ericfitz/tmi/issues/398) — feat(api): admin endpoints to query system + threat-model audit logs
**Date:** 2026-06-11
**Status:** Approved

## Problem

Two audit streams exist with no admin-facing read path: `system_audit_entries` (#355,
admin-write evidence; only `ListByActor` exists, used internally) and `audit_entries`
(threat-model audit; readable only through per-TM handlers that require TM-level access and
a fixed `threat_model_id`). Investigators need cross-cutting queries by actor, time range,
and action shape.

## Decisions

1. **Keyset cursor pagination**, not limit/offset. The audit tables are append-only streams
   ordered `created_at DESC`; offset paging suffers page-drift duplicates during writes and
   deep-offset cost. Opaque cursor over `(created_at, id)`; response carries `next_cursor`
   (absent/null when exhausted) plus `total` and `limit`. This deviates from the API-wide
   limit/offset convention deliberately (CloudTrail/GitHub-audit precedent); follow-ups #456
   and #457 normalize the per-TM audit trail to the same patterns (breaking changes OK —
   audit trail not yet delivered to customers).
2. **Field-qualified time filters**: `created_after` / `created_before` (reusing the
   existing `CreatedAfterQueryParam`/`CreatedBeforeQueryParam` component refs from
   `/admin/users`). The issue text's `from`/`to` never ships; bare `after`/`before` is not
   propagated (its removal from the per-TM trail is #456).
3. **One new index**: `idx_audit_actor (actor_email, created_at)` on `audit_entries` —
   mirrors the existing `idx_sysaudit_actor`. Deliberately not indexing `actor_provider`
   (low cardinality; post-filter after email narrowing is negligible at per-email row
   counts), `http_method` (near-zero cardinality), or `http_path` prefix (TEXT/CLOB).
4. **No step-up on reads** — satisfied automatically: the step-up route table only gates
   write methods under `/admin/*`. Investigators don't re-auth every 5 minutes.
5. **Admin role required** on all four operations via `x-tmi-authz {ownership: none, roles:
   [admin]}` — which (per #399) also categorically denies service-account tokens.

## API surface

| Operation | operationId | Query parameters |
|---|---|---|
| `GET /admin/audit/system` | `listSystemAuditEntries` | `actor_email`, `actor_provider`, `created_after`, `created_before`, `http_method`, `path_prefix`, `field_path`, `limit`, `cursor` |
| `GET /admin/audit/system/{entry_id}` | `getSystemAuditEntry` | — |
| `GET /admin/audit/threat_models` | `listAdminThreatModelAuditEntries` | `actor_email`, `actor_provider`, `created_after`, `created_before`, `change_type`, `object_type`, `threat_model_id`, `limit`, `cursor` |
| `GET /admin/audit/threat_models/{entry_id}` | `getAdminThreatModelAuditEntry` | — |

- Tag: `Administration`. `x-admin-only: true` like the other admin operations.
- `limit`: 1–100, default 50. `cursor`: opaque string; invalid → 400.
- All filters optional and AND-combined. `change_type`/`object_type` reuse the existing
  `AuditChangeType`/`AuditObjectType` enum parameter components. `path_prefix` filters
  `http_path LIKE '<escaped>%'` (escape `%`/`_`/`\`), unindexed by design.
- Responses: new `SystemAuditEntry` schema (id, actor {email, provider, provider_id,
  display_name}, http_method, http_path, field_path, old_value_redacted,
  new_value_redacted, change_summary, created_at — redacted values exactly as written by
  #355); list wrappers `ListSystemAuditEntriesResponse` / `ListAdminAuditEntriesResponse`
  with `{entries, total, limit, next_cursor}`. The cross-TM list reuses the existing
  `AuditEntry` schema (already carries `threat_model_id`).
- Single-entry endpoints: 404 when the id does not exist (or is not a valid UUID per
  OpenAPI format validation → 400). Errors documented: 400/401/403/404/429/500.

## Keyset cursor mechanics

Cursor = `base64url(JSON{t: created_at RFC3339Nano, i: id})` of the last row returned.
Query continuation (Oracle has no row-value comparison — expanded form):

```sql
WHERE ... filters ...
  AND (created_at < :t OR (created_at = :t AND id < :i))
ORDER BY created_at DESC, id DESC
LIMIT :limit
```

`total` is the filter-matching COUNT (computed per request — acceptable at admin query
rates). `next_cursor` is set iff a full page was returned; clients iterate until absent.
Timestamp equality round-trip (Go time → driver → TIMESTAMP(6)) is an explicit
oracle-db-admin review question.

## Implementation layers

1. **OpenAPI** — add paths/schemas/params to `api-schema/tmi-openapi.json`; new
   `AuditCursorQueryParam` + `AuditPageLimitQueryParam` components; `make validate-openapi`;
   `make generate-api`.
2. **Store** —
   - `AuditFilters` gains `ActorProvider *string` and `ThreatModelID *string`;
     `applyAuditFilters` extended.
   - New `ListAuditEntriesAdmin(ctx, limit int, cursor *auditCursor, filters *AuditFilters)
     ([]AuditEntryResponse, int, *auditCursor, error)` on the audit service (cross-TM — no
     fixed `threat_model_id`).
   - `SystemAuditRepository` gains `List(ctx, f SystemAuditFilter) ([]models.SystemAuditEntry,
     int, *auditCursor, error)` and `GetByID(ctx, id string)`; existing `ListByActor` stays
     for its #355 callers.
   - Shared cursor encode/decode helper (`api/audit_cursor.go`).
3. **Model** — `AuditEntry.ActorEmail` gains
   `index:idx_audit_actor,priority:1`; `CreatedAt` gains `index:idx_audit_actor,priority:2`
   (schema change → oracle-db-admin review + dbtool check).
4. **Handlers** — new `api/admin_audit_handlers.go` on `*Server`, following the
   `ListAdminUsers` pattern (param extraction → filter → store → explicit `json.Marshal`).

## Testing

- **Unit:** cursor round-trip + invalid-cursor; filter building (incl. LIKE escaping);
  handlers + stores on in-memory SQLite (seeded, iterate two pages via cursor, verify no
  duplicates/no gaps, totals, each filter).
- **Integration:** all four endpoints against PG — seeded entries, cursor iteration across
  page boundary, filters, 404 unknown id, 400 invalid cursor, 403 for non-admin and for CC
  token (already pinned suite-wide by #399).
- **API change** ⇒ per CLAUDE.md: postman/newman + CATS fuzz analysis at session close;
  zero-500 policy (bad cursor/UUID/timestamps must 400, never 500).
- **Mandatory:** oracle-db-admin review (new index via AutoMigrate; keyset comparison and
  timestamp equality on Oracle; CLOB LIKE for path_prefix).

## Out of scope

- Per-TM audit trail normalization (#456) and cursor migration (#457).
- Export formats (CSV), aggregation/statistics endpoints, retention (#400).
