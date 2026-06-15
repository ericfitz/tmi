# Admin audit list follow-ups — design (#464)

Origin: client-side requirements from tmi-ux#679. #398 shipped
`GET /admin/audit/system(/{entry_id})` and `GET /admin/audit/threat_models(/{entry_id})`
with forward-only opaque keyset cursors. Three requirements remain:

1. `prev_cursor` / bidirectional traversal (‹ Newer / Older ›).
2. `around={entry_id}` anchor mode (permalink: page centered on an entry).
3. Streaming CSV/NDJSON export of the system audit view.

## Decisions (locked)

- **Direction is embedded in the opaque cursor**, not a separate query param. The
  token carries a `Dir` flag; responses return `prev_cursor` + `next_cursor` and the
  client passes whichever back without knowing direction.
- **`prev_cursor` + `around` apply to BOTH list endpoints** (`/admin/audit/system`
  and `/admin/audit/threat_models`); they are identical keyset lists. **Export
  (`format=csv|ndjson`) is system-only** per the issue.
- **Cursor null-ness is determined by an EXISTS probe** past each page boundary
  (accurate "null = no more pages that direction"), not the `len(page)==limit`
  heuristic.
- **Export streams the entire filtered set** (honoring all active filters), ignoring
  `cursor`/`limit`/`around`, keyset-batched so memory stays bounded.
- **CSV is the full flat row**; NDJSON mirrors the JSON API shape.

## 1. Cursor — bidirectional, opaque (`api/audit_cursor.go`)

```go
type auditCursor struct {
    CreatedAt time.Time `json:"t"`
    ID        string    `json:"i"`
    Dir       string    `json:"d,omitempty"` // "" / "f" = older (forward), "b" = newer (backward)
}

const (
    dirForward  = "f" // older entries (created_at DESC continues)
    dirBackward = "b" // newer entries
)
```

- `encodeAuditCursor(createdAt time.Time, id, dir string) string` — gains `dir`.
- `decodeAuditCursor` validates `Dir ∈ {"", "f", "b"}`; anything else → invalid cursor (400).
- An empty/absent `Dir` decodes as forward (older), preserving the #398 contract for any
  cursor minted before this change.
- Responses return `next_cursor` (forward, anchored at the page's **oldest** row) and
  `prev_cursor` (backward, anchored at the page's **newest** row).

## 2. Shared keyset engine (new `api/audit_keyset.go`)

Both list endpoints run identical keyset logic over different models, so it is factored
into one generic helper instead of duplicating across `system_audit_repository.go` and
`audit_store.go`.

```go
// fetchKeysetPage runs a bidirectional keyset query and computes prev/next cursors.
// newQuery returns a fresh FILTERED query (Model set, no order/limit/cursor) — it is
// called multiple times (page query + two EXISTS probes). Returned rows are always in
// display order: created_at DESC, id DESC.
func fetchKeysetPage[T any](
    newQuery func() *gorm.DB,
    cursor *auditCursor,
    limit int,
    keyOf func(T) (time.Time, string),
) (rows []T, prev *string, next *string, err error)
```

Algorithm:

1. `dir` = `cursor.Dir` (forward when no cursor).
2. Page query from `newQuery()`:
   - forward + cursor: `WHERE created_at < ? OR (created_at = ? AND id < ?)`, ORDER BY `created_at DESC, id DESC`.
   - backward + cursor: `WHERE created_at > ? OR (created_at = ? AND id > ?)`, ORDER BY `created_at ASC, id ASC`, then **reverse** the slice to display order.
   - no cursor: ORDER BY `created_at DESC, id DESC`.
   - `LIMIT limit`.
3. Empty page → return `(nil, nil, nil, nil)`.
4. `first = rows[0]` (newest), `last = rows[len-1]` (oldest).
5. EXISTS probe (each is `newQuery().Where(...).Select("id").Limit(1)`, check len):
   - newer than `first` → `prev = encode(first.t, first.id, dirBackward)`, else nil.
   - older than `last` → `next = encode(last.t, last.id, dirForward)`, else nil.

Expanded comparison form (no row-value comparison) + explicit ASC/DESC are Oracle-safe;
the `(created_at, id)` index serves both scan directions.

`systemAuditRepoGORM.List` and `GormAuditService.ListAuditEntriesAdmin` are refactored to
call `fetchKeysetPage`, returning `(rows, total, prev, next, err)`. `total` is still a
separate filtered `COUNT`.

## 3. `around={entry_id}` (helper `fetchAroundPage` in `api/audit_keyset.go`)

```go
func fetchAroundPage[T any](
    newQuery func() *gorm.DB,
    fetchAnchor func() (*T, error), // by id, IGNORING filters; nil => 404
    limit int,
    keyOf func(T) (time.Time, string),
) (rows []T, prev *string, next *string, err error)
```

- Fetch anchor by id ignoring filters. `nil` → handler returns **404**.
- `newerWant = (limit-1)/2`; fetch up to that many **newer** rows (filtered), reverse to
  display order (these sit above the anchor).
- `olderWant = limit-1-len(newer)`; fetch up to that many **older** rows (filtered).
- One **backfill pass** on the deficient side (e.g. anchor near the oldest end → few
  older → fetch more newer) so the page fills to `limit` when enough rows exist either way.
- Page = `newer (newest→oldest)` ++ `[anchor]` ++ `older (newest→oldest)`. Anchor always
  included and centered.
- `prev`/`next` via the same EXISTS probe → both cursors usable for normal traversal from
  the permalink.
- The anchor is fetched by id regardless of filters; surrounding rows respect filters. If
  the anchor itself does not match the active filters it is still shown (it is the
  permalink target). This is intentional and documented.

## 4. Streaming export — system only

New repository method:

```go
// StreamFiltered keyset-iterates the entire filtered set newest-first in batches of
// `batch`, invoking fn per batch until exhausted. Ignores f.Limit/f.Cursor.
StreamFiltered(ctx context.Context, f SystemAuditFilter, batch int,
    fn func([]models.SystemAuditEntry) error) error
```

- Internal loop reuses `applyFilter` + the forward cursor predicate, advancing the cursor
  to the last row of each batch. Batch size 1000.
- Handler, when `format` is `csv` or `ndjson`:
  - `Content-Disposition: attachment; filename="system-audit-<RFC3339-compact>.<ext>"`.
  - CSV: `Content-Type: text/csv; charset=utf-8`; header row then `csv.Writer` rows, flush
    per batch. Columns: `id, created_at, actor_email, actor_provider, actor_provider_id,
    actor_display_name, http_method, http_path, field_path, old_value_redacted,
    new_value_redacted, change_summary`. `created_at` as RFC3339 UTC. Nullable redacted/
    summary fields emit empty string when null.
  - NDJSON: `Content-Type: application/x-ndjson`; one `json.Marshal(systemAuditEntryToAPI(r))`
    per line.
  - JSON remains the default when `format` is absent.
- Error semantics: failure before the first byte → 500 via `HandleRequestError`; failure
  mid-stream (headers already sent, status 200) → log and terminate the truncated response.
  Standard streaming tradeoff.

## 5. Handlers (`api/admin_audit_handlers.go`)

- `ListSystemAuditEntries`: branch in order — `format != nil` → export; else
  `around != nil` → around page; else → bidirectional cursor list. Add `prev_cursor` to the
  JSON response. `cursor` + `around` together → 400.
- `ListAdminThreatModelAuditEntries`: same branching minus export; add `prev_cursor`; 400 on
  `cursor` + `around`; 404 on unknown `around` id.
- Single-entry GETs unchanged.
- `adminAuditPageLimit` unchanged (1..100, default 50).

## 6. OpenAPI (`api-schema/tmi-openapi.json`) + regen

- `ListSystemAuditEntriesResponse` and `ListAdminAuditEntriesResponse`: add `prev_cursor`
  (string, nullable) alongside `next_cursor`.
- New parameters:
  - `AuditAround` — `around`, query, uuid, optional. Added to both list operations.
  - `AuditExportFormat` — `format`, query, enum `["csv","ndjson"]`, optional. System list only.
- `/admin/audit/system` GET 200: add `text/csv` and `application/x-ndjson` content schemas
  (type string, binary-ish) and a `Content-Disposition` response header.
- Both list operations: add a `404` response (unknown `around` id).
- `make validate-openapi` then `make generate-api`. Regenerated `ListSystemAuditEntriesParams`
  / `ListAdminThreatModelAuditEntriesParams` pick up `Around` and (system) `Format`.

## 7. Tests

- **Repo unit** (`system_audit_repository_list_test.go`, `audit_store` tests): bidirectional
  keyset correctness, EXISTS null edges (first page prev=nil, last page next=nil), around
  centering + backfill, `StreamFiltered` batching across a boundary.
- **Integration**: newer/older traversal with no duplicates/skips while rows are inserted
  between page reads; `around` returns centered page with working cursors; `around` unknown
  id → 404; `format=csv` and `format=ndjson` stream the filtered set with
  `Content-Disposition`; JSON default unaffected.
- **Handler unit** (`admin_audit_list_test.go`): param branching, 400 on cursor+around.

## 8. Cross-cutting

- DB-touching (new keyset predicates, EXISTS probes, streaming iteration) → run the
  `oracle-db-admin` subagent before completion; address BLOCKING findings.
- `make lint`, `make build-server`, `make test-unit`, `make test-integration`.
- Subagent-driven execution: one task per unit with review between tasks.
- Conventional commits; reference `#464`; close the issue when merged.

## Out of scope

- Changing the existing filter set, redaction, or the single-entry GET endpoints.
- Export on `/admin/audit/threat_models` (system-only per the issue).
- Any client (tmi-ux) work.
