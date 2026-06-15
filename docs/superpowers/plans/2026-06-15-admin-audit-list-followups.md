# Admin Audit List Follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add bidirectional `prev_cursor` traversal, an `around={entry_id}` anchor mode, and streaming CSV/NDJSON export to the admin audit list endpoints (#464).

**Architecture:** A single generic keyset engine (`api/audit_keyset.go`) drives both list endpoints. The opaque cursor gains a direction flag so the client never tracks direction. `prev`/`next` null-ness is determined by an indexed EXISTS probe past each page boundary. Export streams the whole filtered set in keyset batches so memory stays bounded.

**Tech Stack:** Go, GORM, Gin, oapi-codegen, testify, in-memory SQLite for unit tests.

**Spec:** `docs/superpowers/specs/2026-06-15-admin-audit-list-followups-design.md`

---

## File Structure

- `api/audit_cursor.go` — cursor struct + encode/decode (add `Dir`).
- `api/audit_keyset.go` *(new)* — generic `fetchKeysetPage`, `fetchAroundPage`, `fetchSide`, `keysetCursorIfExists`, `errAuditAnchorNotFound`.
- `api/audit_keyset_test.go` *(new)* — unit tests for the generic engine via `models.SystemAuditEntry`.
- `api/system_audit_repository.go` — `List` returns `prev`; add `Around`, `StreamFiltered`.
- `api/audit_store.go` — `ListAuditEntriesAdmin` returns `prev`; add `AroundAuditEntriesAdmin`.
- `api/audit_service.go` — interface signature updates.
- `api/admin_audit_handlers.go` — branching (export / around / list), `prev_cursor`, 400/404.
- `api-schema/tmi-openapi.json` — params, `prev_cursor`, 404, export content types.
- Test fakes/mocks: `api/admin_audit_middleware_test.go`, `api/audit_debouncer_test.go`.
- Existing call-site tests: `api/system_audit_repository_list_test.go`, `api/admin_audit_list_test.go`.
- New integration coverage: `api/audit_append_only_integration_test.go` pattern → add cases (or a new `api/admin_audit_followups_integration_test.go`).

---

## Task 1: Cursor direction flag

**Files:**
- Modify: `api/audit_cursor.go`
- Modify (callers): `api/audit_store.go:298`, `api/system_audit_repository.go:126`
- Test: `api/audit_cursor_test.go`

- [ ] **Step 1: Write failing tests**

Append to `api/audit_cursor_test.go`:

```go
func TestEncodeDecodeCursor_Direction(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	enc := encodeAuditCursor(now, "id-1", dirBackward)
	c, err := decodeAuditCursor(enc)
	require.NoError(t, err)
	require.Equal(t, dirBackward, c.Dir)
	require.Equal(t, "id-1", c.ID)
	require.True(t, now.Equal(c.CreatedAt))
}

func TestDecodeCursor_RejectsBadDirection(t *testing.T) {
	// hand-craft a cursor with an illegal direction
	raw := `{"t":"2026-01-01T00:00:00Z","i":"x","d":"q"}`
	enc := base64.RawURLEncoding.EncodeToString([]byte(raw))
	_, err := decodeAuditCursor(enc)
	require.Error(t, err)
}

func TestDecodeCursor_EmptyDirectionIsForward(t *testing.T) {
	enc := encodeAuditCursor(time.Now().UTC(), "id-2", dirForward)
	c, err := decodeAuditCursor(enc)
	require.NoError(t, err)
	require.Equal(t, dirForward, c.Dir)
}
```

Add imports `encoding/base64`, `time`, `testify/require` if not present.

- [ ] **Step 2: Run, verify fail**

Run: `make test-unit name=TestEncodeDecodeCursor_Direction`
Expected: FAIL (compile error — `dirBackward` undefined, `encodeAuditCursor` arity).

- [ ] **Step 3: Implement**

Replace the body of `api/audit_cursor.go` with:

```go
package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// auditCursor is the keyset-pagination position for audit list endpoints:
// the (created_at, id) of a boundary row plus the traversal direction. Encoded
// opaque so clients cannot depend on its structure (#398, #464).
type auditCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"i"`
	Dir       string    `json:"d,omitempty"` // "" / "f" = older (forward), "b" = newer (backward)
}

const (
	// dirForward walks toward older entries (created_at DESC continues).
	dirForward = "f"
	// dirBackward walks toward newer entries.
	dirBackward = "b"
)

func encodeAuditCursor(createdAt time.Time, id, dir string) string {
	if dir == "" {
		dir = dirForward
	}
	b, _ := json.Marshal(auditCursor{CreatedAt: createdAt.UTC(), ID: id, Dir: dir})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeAuditCursor(s string) (*auditCursor, error) {
	if s == "" {
		return nil, fmt.Errorf("empty cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	var c auditCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("invalid cursor payload: %w", err)
	}
	if c.CreatedAt.IsZero() || c.ID == "" {
		return nil, fmt.Errorf("incomplete cursor")
	}
	switch c.Dir {
	case "", dirForward:
		c.Dir = dirForward
	case dirBackward:
		// ok
	default:
		return nil, fmt.Errorf("invalid cursor direction")
	}
	return &c, nil
}
```

Then fix the two existing callers (they will be rewritten in Tasks 3/4, but must compile now):

`api/audit_store.go` ~line 298: `enc := encodeAuditCursor(last.CreatedAt, string(last.ID))` → `enc := encodeAuditCursor(last.CreatedAt, string(last.ID), dirForward)`

`api/system_audit_repository.go` ~line 126: `enc := encodeAuditCursor(last.CreatedAt, string(last.ID))` → `enc := encodeAuditCursor(last.CreatedAt, string(last.ID), dirForward)`

- [ ] **Step 4: Run, verify pass**

Run: `make test-unit name=TestEncodeDecodeCursor_Direction count1=true` then `make test-unit name=TestDecodeCursor`
Expected: PASS. Also `make build-server` succeeds.

- [ ] **Step 5: Commit**

```bash
git add api/audit_cursor.go api/audit_cursor_test.go api/audit_store.go api/system_audit_repository.go
git commit -m "feat(audit): add direction flag to opaque audit cursor (#464)"
```

---

## Task 2: Generic keyset engine

**Files:**
- Create: `api/audit_keyset.go`
- Test: `api/audit_keyset_test.go`

- [ ] **Step 1: Write failing tests**

Create `api/audit_keyset_test.go`:

```go
package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func keysetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	return db
}

// seedKS inserts a row aged ageMinutes in the past and returns its id.
func seedKS(t *testing.T, db *gorm.DB, ageMinutes int) string {
	t.Helper()
	e := models.SystemAuditEntry{
		ID:               models.DBVarchar(uuid.New().String()),
		ActorEmail:       models.DBVarchar("a@b.c"),
		ActorProvider:    models.DBVarchar("tmi"),
		ActorProviderID:  models.DBVarchar("a"),
		ActorDisplayName: models.DBVarchar("A"),
		HTTPMethod:       models.DBVarchar("PUT"),
		HTTPPath:         models.DBText("/admin/x"),
		FieldPath:        models.DBVarchar("f"),
	}
	require.NoError(t, db.Create(&e).Error)
	ts := time.Now().UTC().Add(-time.Duration(ageMinutes) * time.Minute)
	require.NoError(t, db.Exec("UPDATE system_audit_entries SET created_at = ? WHERE id = ?", ts, e.ID).Error)
	return string(e.ID)
}

func sysKeyOf(e models.SystemAuditEntry) (time.Time, string) { return e.CreatedAt, string(e.ID) }

func newSysQuery(db *gorm.DB) func() *gorm.DB {
	return func() *gorm.DB { return db.WithContext(context.Background()).Model(&models.SystemAuditEntry{}) }
}

func TestFetchKeysetPage_ForwardThenBackward(t *testing.T) {
	db := keysetTestDB(t)
	// 5 rows, newest (age 10) .. oldest (age 50)
	for age := 10; age <= 50; age += 10 {
		seedKS(t, db, age)
	}
	nq := newSysQuery(db)

	// first page (no cursor): newest 2
	page1, prev1, next1, err := fetchKeysetPage(nq, nil, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Nil(t, prev1, "first page has nothing newer")
	require.NotNil(t, next1)

	// older page from next1
	c2, err := decodeAuditCursor(*next1)
	require.NoError(t, err)
	page2, prev2, next2, err := fetchKeysetPage(nq, c2, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotNil(t, prev2)
	require.NotNil(t, next2)
	// no overlap between page1 and page2
	require.NotEqual(t, string(page1[1].ID), string(page2[0].ID))

	// walk back newer from page2's prev cursor -> should return page1 contents
	cb, err := decodeAuditCursor(*prev2)
	require.NoError(t, err)
	back, _, _, err := fetchKeysetPage(nq, cb, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, back, 2)
	require.Equal(t, string(page1[0].ID), string(back[0].ID))
	require.Equal(t, string(page1[1].ID), string(back[1].ID))
}

func TestFetchKeysetPage_LastPageNextNil(t *testing.T) {
	db := keysetTestDB(t)
	for age := 10; age <= 30; age += 10 {
		seedKS(t, db, age)
	}
	nq := newSysQuery(db)
	page1, _, next1, err := fetchKeysetPage(nq, nil, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotNil(t, next1)
	c2, _ := decodeAuditCursor(*next1)
	last, prev2, next2, err := fetchKeysetPage(nq, c2, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, last, 1)
	require.NotNil(t, prev2)
	require.Nil(t, next2, "last page has nothing older")
}

func TestFetchAroundPage_Centers(t *testing.T) {
	db := keysetTestDB(t)
	ids := make([]string, 0, 7)
	for age := 70; age >= 10; age -= 10 { // oldest..newest insertion; ages 70..10
		ids = append(ids, seedKS(t, db, age))
	}
	// ids[0] oldest (age70) ... ids[6] newest (age10). Anchor on the middle one (age40).
	anchorID := ids[3]
	nq := newSysQuery(db)
	fetchAnchor := func() (*models.SystemAuditEntry, error) {
		var row models.SystemAuditEntry
		err := db.Where("id = ?", anchorID).First(&row).Error
		if err != nil {
			return nil, nil
		}
		return &row, nil
	}
	page, prev, next, err := fetchAroundPage(nq, fetchAnchor, 5, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, page, 5)
	// anchor is centered (index 2 of 5)
	require.Equal(t, anchorID, string(page[2].ID))
	require.NotNil(t, prev)
	require.NotNil(t, next)
	// display order newest->oldest
	require.True(t, page[0].CreatedAt.After(page[4].CreatedAt))
}

func TestFetchAroundPage_NotFound(t *testing.T) {
	db := keysetTestDB(t)
	nq := newSysQuery(db)
	fetchAnchor := func() (*models.SystemAuditEntry, error) { return nil, nil }
	_, _, _, err := fetchAroundPage(nq, fetchAnchor, 5, sysKeyOf)
	require.ErrorIs(t, err, errAuditAnchorNotFound)
}
```

- [ ] **Step 2: Run, verify fail**

Run: `make test-unit name=TestFetchKeysetPage_ForwardThenBackward`
Expected: FAIL (compile error — engine undefined).

- [ ] **Step 3: Implement**

Create `api/audit_keyset.go`:

```go
package api

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// errAuditAnchorNotFound is returned by fetchAroundPage when the anchor entry
// id does not exist; handlers map it to 404 (#464).
var errAuditAnchorNotFound = errors.New("audit anchor entry not found")

// fetchKeysetPage runs a bidirectional keyset query and computes prev/next
// cursors. newQuery returns a fresh FILTERED query (Model set, no
// order/limit/cursor) — it is called multiple times (page query + two EXISTS
// probes). Returned rows are always in display order: created_at DESC, id DESC.
// keyOf extracts (created_at, id) from a row. The expanded comparison form and
// explicit ASC/DESC are Oracle-safe; the (created_at, id) index serves both
// scan directions (#464).
func fetchKeysetPage[T any](
	newQuery func() *gorm.DB,
	cursor *auditCursor,
	limit int,
	keyOf func(T) (time.Time, string),
) ([]T, *string, *string, error) {
	backward := cursor != nil && cursor.Dir == dirBackward

	q := newQuery()
	switch {
	case cursor != nil && backward:
		q = q.Where("created_at > ? OR (created_at = ? AND id > ?)",
			cursor.CreatedAt, cursor.CreatedAt, cursor.ID).
			Order("created_at ASC, id ASC")
	case cursor != nil:
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)",
			cursor.CreatedAt, cursor.CreatedAt, cursor.ID).
			Order("created_at DESC, id DESC")
	default:
		q = q.Order("created_at DESC, id DESC")
	}

	var rows []T
	if err := q.Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, nil, err
	}
	if backward {
		reverse(rows)
	}
	if len(rows) == 0 {
		return rows, nil, nil, nil
	}

	firstT, firstID := keyOf(rows[0])
	lastT, lastID := keyOf(rows[len(rows)-1])
	prev, err := keysetCursorIfExists(newQuery(), firstT, firstID, dirBackward)
	if err != nil {
		return nil, nil, nil, err
	}
	next, err := keysetCursorIfExists(newQuery(), lastT, lastID, dirForward)
	if err != nil {
		return nil, nil, nil, err
	}
	return rows, prev, next, nil
}

// fetchAroundPage returns a page of `limit` rows centered on the anchor entry,
// with ~half newer and ~half older. fetchAnchor loads the anchor by id ignoring
// filters; a nil anchor yields errAuditAnchorNotFound. Surrounding rows respect
// the filters baked into newQuery. The anchor is always included and centered.
func fetchAroundPage[T any](
	newQuery func() *gorm.DB,
	fetchAnchor func() (*T, error),
	limit int,
	keyOf func(T) (time.Time, string),
) ([]T, *string, *string, error) {
	anchor, err := fetchAnchor()
	if err != nil {
		return nil, nil, nil, err
	}
	if anchor == nil {
		return nil, nil, nil, errAuditAnchorNotFound
	}
	anchorT, anchorID := keyOf(*anchor)

	newerWant := (limit - 1) / 2
	newer, err := fetchSide(newQuery(), anchorT, anchorID, dirBackward, newerWant)
	if err != nil {
		return nil, nil, nil, err
	}
	olderWant := limit - 1 - len(newer)
	older, err := fetchSide(newQuery(), anchorT, anchorID, dirForward, olderWant)
	if err != nil {
		return nil, nil, nil, err
	}
	// Backfill the newer side when the older side was deficient, so the page
	// fills to `limit` whenever enough rows exist on either side.
	if len(newer)+len(older)+1 < limit {
		newerWant2 := limit - 1 - len(older)
		if newerWant2 > len(newer) {
			newer, err = fetchSide(newQuery(), anchorT, anchorID, dirBackward, newerWant2)
			if err != nil {
				return nil, nil, nil, err
			}
		}
	}

	reverse(newer) // ASC closest-first -> display order newest->oldest
	page := make([]T, 0, len(newer)+1+len(older))
	page = append(page, newer...)
	page = append(page, *anchor)
	page = append(page, older...) // DESC closest-first == newest->oldest

	firstT, firstID := keyOf(page[0])
	lastT, lastID := keyOf(page[len(page)-1])
	prev, err := keysetCursorIfExists(newQuery(), firstT, firstID, dirBackward)
	if err != nil {
		return nil, nil, nil, err
	}
	next, err := keysetCursorIfExists(newQuery(), lastT, lastID, dirForward)
	if err != nil {
		return nil, nil, nil, err
	}
	return page, prev, next, nil
}

// fetchSide returns up to n rows on one side of the anchor, ordered
// closest-to-anchor first. dirBackward = newer rows; dirForward = older rows.
func fetchSide[T any](q *gorm.DB, t time.Time, id, dir string, n int) ([]T, error) {
	if n <= 0 {
		return nil, nil
	}
	if dir == dirBackward {
		q = q.Where("created_at > ? OR (created_at = ? AND id > ?)", t, t, id).
			Order("created_at ASC, id ASC")
	} else {
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", t, t, id).
			Order("created_at DESC, id DESC")
	}
	var rows []T
	if err := q.Limit(n).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// keysetCursorIfExists returns an encoded cursor anchored at (t, id) in the
// given direction, or nil when no row exists beyond that boundary. Uses an
// indexed SELECT id ... LIMIT 1 probe.
func keysetCursorIfExists(q *gorm.DB, t time.Time, id, dir string) (*string, error) {
	var cmp string
	if dir == dirBackward {
		cmp = "created_at > ? OR (created_at = ? AND id > ?)"
	} else {
		cmp = "created_at < ? OR (created_at = ? AND id < ?)"
	}
	var ids []string
	if err := q.Where(cmp, t, t, id).Select("id").Limit(1).Find(&ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	enc := encodeAuditCursor(t, id, dir)
	return &enc, nil
}

// reverse reverses a slice in place.
func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
```

- [ ] **Step 4: Run, verify pass**

Run: `make test-unit name=TestFetchKeysetPage` then `make test-unit name=TestFetchAroundPage`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/audit_keyset.go api/audit_keyset_test.go
git commit -m "feat(audit): generic bidirectional keyset + around engine (#464)"
```

---

## Task 3: SystemAuditRepository — bidirectional List, Around, StreamFiltered

**Files:**
- Modify: `api/system_audit_repository.go`
- Modify (fake): `api/admin_audit_middleware_test.go:35` (+ add new methods)
- Modify (existing tests): `api/system_audit_repository_list_test.go` (call sites now return prev)
- Test: `api/system_audit_repository_list_test.go` (add bidirectional/around/stream cases)

- [ ] **Step 1: Update the interface + implementation**

In `api/system_audit_repository.go`, change the interface `List` signature and add two methods:

```go
	// List returns entries matching the filter, newest first, with bidirectional
	// keyset pagination. Returns (page, total matching the filter, prev cursor,
	// next cursor). Cursors are nil when no further rows exist that direction
	// (#398, #464).
	List(ctx context.Context, f SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, *string, error)
	// Around returns a page of f.Limit entries centered on anchorID (~half newer,
	// ~half older). Returns errAuditAnchorNotFound when the id is unknown (#464).
	Around(ctx context.Context, f SystemAuditFilter, anchorID string) ([]models.SystemAuditEntry, int, *string, *string, error)
	// StreamFiltered keyset-iterates the entire filtered set newest-first in
	// batches of `batch`, invoking fn per batch until exhausted. Ignores
	// f.Limit/f.Cursor. Used by CSV/NDJSON export (#464).
	StreamFiltered(ctx context.Context, f SystemAuditFilter, batch int, fn func([]models.SystemAuditEntry) error) error
```

Replace the `List` method body and add the new methods (keep `applyFilter`, `escapeLikePrefix`, `Create`, `ListByActor`, `GetByID` as-is):

```go
func sysAuditKeyOf(e models.SystemAuditEntry) (time.Time, string) {
	return e.CreatedAt, string(e.ID)
}

// List returns system audit entries matching the filter with bidirectional
// keyset pagination ordered (created_at DESC, id DESC).
func (r *systemAuditRepoGORM) List(ctx context.Context, f SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, *string, error) {
	total, err := r.countFiltered(ctx, f)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	newQuery := func() *gorm.DB {
		return r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f)
	}
	rows, prev, next, err := fetchKeysetPage(newQuery, f.Cursor, f.Limit, sysAuditKeyOf)
	if err != nil {
		return nil, 0, nil, nil, fmt.Errorf("list system audit entries: %w", err)
	}
	return rows, total, prev, next, nil
}

// Around returns a page centered on anchorID.
func (r *systemAuditRepoGORM) Around(ctx context.Context, f SystemAuditFilter, anchorID string) ([]models.SystemAuditEntry, int, *string, *string, error) {
	total, err := r.countFiltered(ctx, f)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	newQuery := func() *gorm.DB {
		return r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f)
	}
	fetchAnchor := func() (*models.SystemAuditEntry, error) {
		return r.GetByID(ctx, anchorID) // by id, ignoring filters
	}
	rows, prev, next, err := fetchAroundPage(newQuery, fetchAnchor, f.Limit, sysAuditKeyOf)
	if err != nil {
		if errors.Is(err, errAuditAnchorNotFound) {
			return nil, 0, nil, nil, err
		}
		return nil, 0, nil, nil, fmt.Errorf("around system audit entries: %w", err)
	}
	return rows, total, prev, next, nil
}

// StreamFiltered keyset-iterates the entire filtered set newest-first.
func (r *systemAuditRepoGORM) StreamFiltered(ctx context.Context, f SystemAuditFilter, batch int, fn func([]models.SystemAuditEntry) error) error {
	if batch <= 0 {
		batch = 1000
	}
	var cursor *auditCursor
	for {
		q := r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f)
		if cursor != nil {
			q = q.Where("created_at < ? OR (created_at = ? AND id < ?)",
				cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
		}
		var rows []models.SystemAuditEntry
		if err := q.Order("created_at DESC, id DESC").Limit(batch).Find(&rows).Error; err != nil {
			return fmt.Errorf("stream system audit entries: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		if err := fn(rows); err != nil {
			return err
		}
		if len(rows) < batch {
			return nil
		}
		last := rows[len(rows)-1]
		cursor = &auditCursor{CreatedAt: last.CreatedAt, ID: string(last.ID), Dir: dirForward}
	}
}

// countFiltered returns the total rows matching the filter (ignoring cursor).
func (r *systemAuditRepoGORM) countFiltered(ctx context.Context, f SystemAuditFilter) (int, error) {
	var total int64
	if err := r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("count system audit entries: %w", err)
	}
	return int(total), nil
}
```

Add `"errors"` to the imports if not already present (it is — `GetByID` uses it). Remove the now-unused old `List` body. Ensure `time` import remains (used by `sysAuditKeyOf`).

- [ ] **Step 2: Update the fake repo**

In `api/admin_audit_middleware_test.go`, the `fakeSystemAuditRepo` must satisfy the new interface. Replace its `List` and add `Around`/`StreamFiltered`:

```go
func (f *fakeSystemAuditRepo) List(_ context.Context, _ SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, *string, error) {
	return nil, 0, nil, nil, f.err
}

func (f *fakeSystemAuditRepo) Around(_ context.Context, _ SystemAuditFilter, _ string) ([]models.SystemAuditEntry, int, *string, *string, error) {
	return nil, 0, nil, nil, f.err
}

func (f *fakeSystemAuditRepo) StreamFiltered(_ context.Context, _ SystemAuditFilter, _ int, _ func([]models.SystemAuditEntry) error) error {
	return f.err
}
```

- [ ] **Step 3: Update existing list-test call sites**

In `api/system_audit_repository_list_test.go`, every `repo.List(...)` now returns 5 values. Update each call. The patterns:

```go
rows, total, _, err := repo.List(...)          // OLD
rows, total, _, _, err := repo.List(...)        // NEW (add one blank for prev)

page1, total, next, err := repo.List(...)       // OLD
page1, total, _, next, err := repo.List(...)    // NEW (prev blank, keep next)

page2, _, next2, err := repo.List(...)          // OLD
page2, _, _, next2, err := repo.List(...)       // NEW
```

Apply to all `repo.List` occurrences in that file (and `_, total, _, err` → `_, total, _, _, err`).

- [ ] **Step 4: Add new repo tests**

Append to `api/system_audit_repository_list_test.go`:

```go
func TestSystemAuditList_Bidirectional(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()
	for age := 10; age <= 50; age += 10 {
		seedSysAuditRow(t, db, "c@tmi.local", "tmi", "PUT", "/admin/x", "f", age)
	}
	p1, _, prev1, next1, err := repo.List(ctx, SystemAuditFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, p1, 2)
	require.Nil(t, prev1)
	require.NotNil(t, next1)

	c2, _ := decodeAuditCursor(*next1)
	p2, _, prev2, _, err := repo.List(ctx, SystemAuditFilter{Limit: 2, Cursor: c2})
	require.NoError(t, err)
	require.NotNil(t, prev2)

	// walk back newer to p1
	cb, _ := decodeAuditCursor(*prev2)
	back, _, _, _, err := repo.List(ctx, SystemAuditFilter{Limit: 2, Cursor: cb})
	require.NoError(t, err)
	require.Equal(t, string(p1[0].ID), string(back[0].ID))
}

func TestSystemAuditList_Around(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()
	var mid string
	for age := 70; age >= 10; age -= 10 {
		id := seedSysAuditRow(t, db, "c@tmi.local", "tmi", "PUT", "/admin/x", "f", age)
		if age == 40 {
			mid = id
		}
	}
	page, total, prev, next, err := repo.Around(ctx, SystemAuditFilter{Limit: 5}, mid)
	require.NoError(t, err)
	require.Equal(t, 7, total)
	require.Len(t, page, 5)
	require.Equal(t, mid, string(page[2].ID))
	require.NotNil(t, prev)
	require.NotNil(t, next)

	_, _, _, _, err = repo.Around(ctx, SystemAuditFilter{Limit: 5}, uuid.New().String())
	require.ErrorIs(t, err, errAuditAnchorNotFound)
}

func TestSystemAuditStreamFiltered_Batches(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()
	for age := 1; age <= 5; age++ {
		seedSysAuditRow(t, db, "c@tmi.local", "tmi", "PUT", "/admin/x", "f", age)
	}
	var seen int
	var batches int
	err := repo.StreamFiltered(ctx, SystemAuditFilter{}, 2, func(rows []models.SystemAuditEntry) error {
		batches++
		seen += len(rows)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 5, seen)
	require.Equal(t, 3, batches) // 2 + 2 + 1
}
```

- [ ] **Step 5: Run, verify pass**

Run: `make test-unit name=TestSystemAudit` then `make build-server`
Expected: PASS / build OK.

- [ ] **Step 6: Commit**

```bash
git add api/system_audit_repository.go api/admin_audit_middleware_test.go api/system_audit_repository_list_test.go
git commit -m "feat(audit): bidirectional List, Around, StreamFiltered on system audit repo (#464)"
```

---

## Task 4: Threat-model audit service — bidirectional + Around

**Files:**
- Modify: `api/audit_service.go` (interface)
- Modify: `api/audit_store.go` (impl)
- Modify (mock): `api/audit_debouncer_test.go:68`
- Modify (existing tests): `api/admin_audit_list_test.go` (call sites)
- Test: `api/admin_audit_list_test.go` (add bidirectional/around)

- [ ] **Step 1: Update the interface**

In `api/audit_service.go` around line 101, replace the `ListAuditEntriesAdmin` declaration and add `AroundAuditEntriesAdmin`:

```go
	// ListAuditEntriesAdmin lists audit entries across ALL threat models with
	// bidirectional keyset pagination. Returns (rows, total, prev, next) (#464).
	ListAuditEntriesAdmin(ctx context.Context, limit int, cursor *auditCursor, filters *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error)
	// AroundAuditEntriesAdmin returns a page of `limit` entries centered on
	// anchorID. Returns errAuditAnchorNotFound for an unknown id (#464).
	AroundAuditEntriesAdmin(ctx context.Context, limit int, anchorID string, filters *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error)
```

- [ ] **Step 2: Update the implementation**

In `api/audit_store.go`, replace `ListAuditEntriesAdmin` (lines ~278-302) and add `AroundAuditEntriesAdmin`:

```go
func adminAuditKeyOf(e models.AuditEntry) (time.Time, string) {
	return e.CreatedAt, string(e.ID)
}

// ListAuditEntriesAdmin lists audit entries across all threat models with
// bidirectional keyset pagination ordered (created_at DESC, id DESC).
func (s *GormAuditService) ListAuditEntriesAdmin(ctx context.Context, limit int, cursor *auditCursor, filters *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error) {
	var total int64
	if err := applyAuditFilters(s.db.WithContext(ctx).Model(&models.AuditEntry{}), filters).Count(&total).Error; err != nil {
		return nil, 0, nil, nil, fmt.Errorf("failed to count audit entries: %w", err)
	}
	newQuery := func() *gorm.DB {
		return applyAuditFilters(s.db.WithContext(ctx).Model(&models.AuditEntry{}), filters)
	}
	rows, prev, next, err := fetchKeysetPage(newQuery, cursor, limit, adminAuditKeyOf)
	if err != nil {
		return nil, 0, nil, nil, fmt.Errorf("failed to list audit entries: %w", err)
	}
	return toAuditEntryResponses(rows), int(total), prev, next, nil
}

// AroundAuditEntriesAdmin returns a page centered on anchorID.
func (s *GormAuditService) AroundAuditEntriesAdmin(ctx context.Context, limit int, anchorID string, filters *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error) {
	var total int64
	if err := applyAuditFilters(s.db.WithContext(ctx).Model(&models.AuditEntry{}), filters).Count(&total).Error; err != nil {
		return nil, 0, nil, nil, fmt.Errorf("failed to count audit entries: %w", err)
	}
	newQuery := func() *gorm.DB {
		return applyAuditFilters(s.db.WithContext(ctx).Model(&models.AuditEntry{}), filters)
	}
	fetchAnchor := func() (*models.AuditEntry, error) {
		var entry models.AuditEntry
		err := s.db.WithContext(ctx).Where("id = ?", anchorID).First(&entry).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &entry, nil
	}
	rows, prev, next, err := fetchAroundPage(newQuery, fetchAnchor, limit, adminAuditKeyOf)
	if err != nil {
		if errors.Is(err, errAuditAnchorNotFound) {
			return nil, 0, nil, nil, err
		}
		return nil, 0, nil, nil, fmt.Errorf("failed to fetch audit entries around anchor: %w", err)
	}
	return toAuditEntryResponses(rows), int(total), prev, next, nil
}
```

(`errors`, `gorm`, `time`, `models` are already imported in `audit_store.go`.)

- [ ] **Step 3: Update the mock**

In `api/audit_debouncer_test.go` around line 68 replace the mock method and add the new one:

```go
func (m *mockAuditService) ListAuditEntriesAdmin(_ context.Context, _ int, _ *auditCursor, _ *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error) {
	return nil, 0, nil, nil, nil
}

func (m *mockAuditService) AroundAuditEntriesAdmin(_ context.Context, _ int, _ string, _ *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error) {
	return nil, 0, nil, nil, nil
}
```

- [ ] **Step 4: Update existing call sites + add tests**

In `api/admin_audit_list_test.go`, update each `ListAuditEntriesAdmin` call to 5 returns (insert a blank for prev: `rows, total, _, err :=` → `rows, total, _, _, err :=`; `page, _, next, err :=` → `page, _, _, next, err :=`). Then append:

```go
func TestListAuditEntriesAdmin_Bidirectional(t *testing.T) {
	db := setupAdminAuditListDB(t)
	svc := NewGormAuditService(db)
	ctx := context.Background()
	tm := uuid.New().String()
	for i := 0; i < 5; i++ {
		seedAdminAuditEntry(t, db, "c@tmi.local", "tmi", tm, (i+1)*10)
	}
	p1, _, prev1, next1, err := svc.ListAuditEntriesAdmin(ctx, 2, nil, nil)
	require.NoError(t, err)
	require.Len(t, p1, 2)
	require.Nil(t, prev1)
	require.NotNil(t, next1)
	c2, _ := decodeAuditCursor(*next1)
	_, _, prev2, _, err := svc.ListAuditEntriesAdmin(ctx, 2, c2, nil)
	require.NoError(t, err)
	require.NotNil(t, prev2)
}

func TestAroundAuditEntriesAdmin(t *testing.T) {
	db := setupAdminAuditListDB(t)
	svc := NewGormAuditService(db)
	ctx := context.Background()
	tm := uuid.New().String()
	var mid string
	for i := 0; i < 7; i++ {
		id := seedAdminAuditEntry(t, db, "c@tmi.local", "tmi", tm, (7-i)*10) // newest..oldest
		if i == 3 {
			mid = id
		}
	}
	page, total, prev, next, err := svc.AroundAuditEntriesAdmin(ctx, 5, mid, nil)
	require.NoError(t, err)
	require.Equal(t, 7, total)
	require.Len(t, page, 5)
	require.Equal(t, mid, page[2].ID)
	require.NotNil(t, prev)
	require.NotNil(t, next)

	_, _, _, _, err = svc.AroundAuditEntriesAdmin(ctx, 5, uuid.New().String(), nil)
	require.ErrorIs(t, err, errAuditAnchorNotFound)
}
```

- [ ] **Step 5: Run, verify pass**

Run: `make test-unit name=TestListAuditEntriesAdmin` then `make test-unit name=TestAroundAuditEntriesAdmin` then `make build-server`
Expected: PASS / build OK.

- [ ] **Step 6: Commit**

```bash
git add api/audit_service.go api/audit_store.go api/audit_debouncer_test.go api/admin_audit_list_test.go
git commit -m "feat(audit): bidirectional + around for threat-model admin audit (#464)"
```

---

## Task 5: OpenAPI spec + regenerate

**Files:**
- Modify: `api-schema/tmi-openapi.json`
- Regenerate: `api/api.go`

- [ ] **Step 1: Add `prev_cursor` to both response schemas**

In `ListSystemAuditEntriesResponse` and `ListAdminAuditEntriesResponse` (around lines 11733 and 11763), add after the `next_cursor` property in each:

```json
          "prev_cursor": {
            "type": "string",
            "nullable": true,
            "description": "Cursor for the previous (newer) page; absent or null when at the newest end."
          }
```

(Use `jq` for surgical edits given file size; back up first per CLAUDE.md large-JSON rules.)

- [ ] **Step 2: Add new parameter components**

After the `AuditThreatModelId` parameter (around line 13628) add:

```json
      ,"AuditAround": {
        "name": "around",
        "in": "query",
        "required": false,
        "description": "Return a page centered on this entry id (~half newer, ~half older, entry included). Mutually exclusive with cursor.",
        "schema": {
          "type": "string",
          "format": "uuid"
        }
      },
      "AuditExportFormat": {
        "name": "format",
        "in": "query",
        "required": false,
        "description": "When set, stream the entire filtered set as an attachment instead of a JSON page. Honors all active filters; ignores cursor/limit/around.",
        "schema": {
          "type": "string",
          "enum": ["csv", "ndjson"]
        }
      }
```

- [ ] **Step 3: Wire params into operations**

`listSystemAuditEntries` parameters array (around line 66549, after `AuditCursor`): add
```json
            ,{ "$ref": "#/components/parameters/AuditAround" }
            ,{ "$ref": "#/components/parameters/AuditExportFormat" }
```
`listAdminThreatModelAuditEntries` parameters array: add only
```json
            ,{ "$ref": "#/components/parameters/AuditAround" }
```

- [ ] **Step 4: Add export content types + 404 to system list 200**

In `listSystemAuditEntries` 200 response `content`, alongside `application/json` add:
```json
            ,"text/csv": { "schema": { "type": "string" } }
            ,"application/x-ndjson": { "schema": { "type": "string" } }
```
And add a `Content-Disposition` entry to that 200's `headers`:
```json
            ,"Content-Disposition": {
              "description": "Set to attachment with a filename when format=csv|ndjson.",
              "schema": { "type": "string" }
            }
```
Add a `404` response to BOTH `listSystemAuditEntries` and `listAdminThreatModelAuditEntries` (unknown `around` id):
```json
          ,"404": {
            "description": "Not Found - the around entry id does not exist",
            "content": { "application/json": { "schema": { "$ref": "#/components/schemas/Error" } } }
          }
```

- [ ] **Step 5: Validate + regenerate**

Run:
```bash
make validate-openapi
make generate-api
```
Expected: validation passes; `api/api.go` regenerates. Confirm new fields:
```bash
grep -n "Around\b" api/api.go | head
grep -n "ListSystemAuditEntriesParamsFormat\|Format \*" api/api.go | head
```
Expected: `ListSystemAuditEntriesParams` now has `Around *AuditAround` and `Format *ListSystemAuditEntriesParamsFormat`; `ListAdminThreatModelAuditEntriesParams` has `Around *AuditAround`.

- [ ] **Step 6: Build + commit**

```bash
make build-server
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): OpenAPI for prev_cursor, around, and audit export (#464)"
```

---

## Task 6: Handlers — wire prev_cursor, around, export

**Files:**
- Modify: `api/admin_audit_handlers.go`
- Test: `api/admin_audit_list_test.go` (handler-level) or extend integration in Task 7

- [ ] **Step 1: Add the export writer helpers**

Append to `api/admin_audit_handlers.go` (new imports: `encoding/csv`, `errors`, `fmt`, `strings`):

```go
// systemAuditCSVHeader is the flat column order for CSV export (#464).
var systemAuditCSVHeader = []string{
	"id", "created_at", "actor_email", "actor_provider", "actor_provider_id",
	"actor_display_name", "http_method", "http_path", "field_path",
	"old_value_redacted", "new_value_redacted", "change_summary",
}

func nullableText(n models.NullableDBText) string {
	if n.Valid {
		return n.String
	}
	return ""
}

func systemAuditCSVRecord(e models.SystemAuditEntry) []string {
	return []string{
		string(e.ID),
		e.CreatedAt.UTC().Format(time.RFC3339Nano),
		string(e.ActorEmail),
		string(e.ActorProvider),
		string(e.ActorProviderID),
		string(e.ActorDisplayName),
		string(e.HTTPMethod),
		string(e.HTTPPath),
		string(e.FieldPath),
		nullableText(e.OldValueRedacted),
		nullableText(e.NewValueRedacted),
		nullableText(e.ChangeSummary),
	}
}

// streamSystemAuditExport streams the filtered set as csv or ndjson. Headers are
// written lazily on the first batch (or on a zero-row success) so a failure
// before the first byte can still return 500. A mid-stream failure (headers
// already sent) is logged and truncates the response.
func (s *Server) streamSystemAuditExport(c *gin.Context, logger *slogging.ContextLogger, f SystemAuditFilter, format string) {
	ext := format
	contentType := "text/csv; charset=utf-8"
	if format == "ndjson" {
		contentType = "application/x-ndjson"
	}
	filename := fmt.Sprintf("system-audit-%s.%s", time.Now().UTC().Format("20060102T150405Z"), ext)

	var started bool
	var csvW *csv.Writer
	writeHead := func() error {
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		c.Status(http.StatusOK)
		if format == "csv" {
			csvW = csv.NewWriter(c.Writer)
			if err := csvW.Write(systemAuditCSVHeader); err != nil {
				return err
			}
		}
		started = true
		return nil
	}

	err := s.systemAuditRepo.StreamFiltered(c.Request.Context(), f, 1000, func(rows []models.SystemAuditEntry) error {
		if !started {
			if err := writeHead(); err != nil {
				return err
			}
		}
		if format == "csv" {
			for _, r := range rows {
				if err := csvW.Write(systemAuditCSVRecord(r)); err != nil {
					return err
				}
			}
			csvW.Flush()
			if err := csvW.Error(); err != nil {
				return err
			}
		} else {
			for _, r := range rows {
				data, err := json.Marshal(systemAuditEntryToAPI(r))
				if err != nil {
					return err
				}
				if _, err := c.Writer.Write(append(data, '\n')); err != nil {
					return err
				}
			}
		}
		c.Writer.Flush()
		return nil
	})

	if err != nil {
		if !started {
			logger.Error("Failed to start system audit export: %v", err)
			HandleRequestError(c, ServerError("Failed to export system audit entries"))
			return
		}
		logger.Error("System audit export terminated mid-stream: %v", err)
		return
	}
	if !started {
		// Zero rows: still emit headers (+ CSV header row) for a valid empty file.
		if err := writeHead(); err != nil {
			logger.Error("Failed to write empty system audit export: %v", err)
			HandleRequestError(c, ServerError("Failed to export system audit entries"))
			return
		}
		if format == "csv" {
			csvW.Flush()
		}
	}
}
```

- [ ] **Step 2: Rewire `ListSystemAuditEntries`**

Replace the tail of `ListSystemAuditEntries` (from where `filter` is built through the response write) with branching. Build the filter without limit/cursor first, then branch:

```go
	filter := SystemAuditFilter{
		ActorEmail:    actorEmail,
		ActorProvider: actorProvider,
		CreatedAfter:  createdAfter,
		CreatedBefore: createdBefore,
		HTTPMethod:    httpMethod,
		PathPrefix:    pathPrefix,
		FieldPath:     fieldPath,
		Limit:         limit,
		Cursor:        cursor,
	}

	// Export branch: stream the whole filtered set, ignore pagination.
	if params.Format != nil {
		s.streamSystemAuditExport(c, logger, filter, string(*params.Format))
		return
	}

	// Around branch: page centered on an entry. Mutually exclusive with cursor.
	if params.Around != nil {
		if cursor != nil {
			HandleRequestError(c, InvalidInputError("cursor and around are mutually exclusive"))
			return
		}
		rows, total, prev, next, err := s.systemAuditRepo.Around(c.Request.Context(), filter, params.Around.String())
		if err != nil {
			if errors.Is(err, errAuditAnchorNotFound) {
				HandleRequestError(c, NotFoundError("System audit entry not found"))
				return
			}
			logger.Error("Failed to fetch system audit entries around anchor: %v", err)
			HandleRequestError(c, ServerError("Failed to list system audit entries"))
			return
		}
		s.writeSystemAuditPage(c, logger, rows, total, limit, prev, next)
		return
	}

	rows, total, prev, next, err := s.systemAuditRepo.List(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to list system audit entries: %v", err)
		HandleRequestError(c, ServerError("Failed to list system audit entries"))
		return
	}
	s.writeSystemAuditPage(c, logger, rows, total, limit, prev, next)
}

// writeSystemAuditPage serializes a paginated system audit response.
func (s *Server) writeSystemAuditPage(c *gin.Context, logger *slogging.ContextLogger, rows []models.SystemAuditEntry, total, limit int, prev, next *string) {
	entries := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, systemAuditEntryToAPI(r))
	}
	writeAdminAuditJSON(c, logger, gin.H{
		"entries":     entries,
		"total":       total,
		"limit":       limit,
		"next_cursor": next,
		"prev_cursor": prev,
	})
}
```

(The `limit := adminAuditPageLimit(params.Limit)` and the `cursor` decode block above remain unchanged.)

- [ ] **Step 3: Rewire `ListAdminThreatModelAuditEntries`**

Replace its tail (from the `ListAuditEntriesAdmin` call) with around/list branching:

```go
	if params.Around != nil {
		if cursor != nil {
			HandleRequestError(c, InvalidInputError("cursor and around are mutually exclusive"))
			return
		}
		rows, total, prev, next, err := GlobalAuditService.AroundAuditEntriesAdmin(c.Request.Context(), limit, params.Around.String(), filters)
		if err != nil {
			if errors.Is(err, errAuditAnchorNotFound) {
				HandleRequestError(c, NotFoundError("Audit entry not found"))
				return
			}
			logger.Error("Failed to fetch audit entries around anchor: %v", err)
			HandleRequestError(c, ServerError("Failed to list audit entries"))
			return
		}
		writeAdminAuditJSON(c, logger, gin.H{
			"entries":     toAPIAuditEntries(rows),
			"total":       total,
			"limit":       limit,
			"next_cursor": next,
			"prev_cursor": prev,
		})
		return
	}

	rows, total, prev, next, err := GlobalAuditService.ListAuditEntriesAdmin(c.Request.Context(), limit, cursor, filters)
	if err != nil {
		logger.Error("Failed to list audit entries: %v", err)
		HandleRequestError(c, ServerError("Failed to list audit entries"))
		return
	}
	writeAdminAuditJSON(c, logger, gin.H{
		"entries":     toAPIAuditEntries(rows),
		"total":       total,
		"limit":       limit,
		"next_cursor": next,
		"prev_cursor": prev,
	})
}
```

- [ ] **Step 4: Build + lint**

Run: `make build-server && make lint`
Expected: build OK; lint clean (remove any now-unused imports flagged).

- [ ] **Step 5: Handler unit test (mutual exclusivity)**

Add to `api/admin_audit_list_test.go` a focused test that a Server with a fake repo returns 400 when both cursor and around are set. Use the existing `fakeSystemAuditRepo` and a minimal `Server{systemAuditRepo: ...}`; build a `gin` test context with query `?around=<uuid>&cursor=abc`. (Follow the construction already used in `api/admin_audit_middleware_test.go`.)

```go
func TestListSystemAudit_CursorAndAroundConflict(t *testing.T) {
	s := &Server{systemAuditRepo: &fakeSystemAuditRepo{}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/admin/audit/system", nil)
	around := openapi_types.UUID(uuid.New())
	curStr := "abc"
	// cursor "abc" is not a valid cursor, but the conflict check fires only when
	// cursor decodes; use a real cursor instead:
	realCur := encodeAuditCursor(time.Now().UTC(), uuid.New().String(), dirForward)
	curStr = realCur
	s.ListSystemAuditEntries(c, ListSystemAuditEntriesParams{Around: &around, Cursor: &curStr})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

Add imports `net/http`, `net/http/httptest`, `github.com/gin-gonic/gin`, `openapi_types "github.com/oapi-codegen/runtime/types"` as needed.

- [ ] **Step 6: Run + commit**

Run: `make test-unit name=TestListSystemAudit_CursorAndAroundConflict`
Expected: PASS.

```bash
git add api/admin_audit_handlers.go api/admin_audit_list_test.go
git commit -m "feat(api): wire prev_cursor, around, and CSV/NDJSON export into audit handlers (#464)"
```

---

## Task 7: Integration tests, Oracle review, quality gates

**Files:**
- Create or extend: `api/admin_audit_followups_integration_test.go`

- [ ] **Step 1: Integration tests**

Add integration cases (build tag / pattern matching the existing `api/audit_append_only_integration_test.go`) exercising the full HTTP stack against PostgreSQL:
- Forward then backward traversal across 3 pages with no duplicate/skipped ids; insert a new row between page reads and confirm keyset stability.
- `around=<id>` returns a centered page with working `prev_cursor`/`next_cursor`; unknown id → 404.
- `format=csv` returns `Content-Disposition: attachment`, a header row, and only filtered rows (apply an `actor_email` filter and assert the row count).
- `format=ndjson` returns one JSON object per line over the filtered set.
- Default (no `format`) still returns JSON with both `prev_cursor` and `next_cursor` keys.

- [ ] **Step 2: Run integration**

Run: `make test-integration`
Expected: PASS. Investigate and fix any failure at root cause (no skips).

- [ ] **Step 3: Oracle compatibility review**

Invoke the `oracle-db-admin` skill and dispatch the subagent over the DB-touching diff (new keyset predicates, EXISTS probes, `StreamFiltered` iteration, ASC/DESC ordering). Address every BLOCKING finding; fold APPROVED-WITH-NOTES items in or file follow-ups.

- [ ] **Step 4: Full quality gates**

Run:
```bash
make lint
make build-server
make test-unit
make test-integration
```
Expected: all green.

- [ ] **Step 5: Security regression scan**

Invoke the `security-regression` skill over the branch diff (handlers return streamed data + new error paths). Address any findings.

- [ ] **Step 6: Final commit**

```bash
git add api/admin_audit_followups_integration_test.go
git commit -m "test(audit): integration coverage for bidirectional, around, and export (#464)"
```

---

## Self-Review notes

- **Spec coverage:** prev_cursor (Tasks 2–4,6), around + 404 (Tasks 2–4,6), export (Tasks 3,6), OpenAPI for all three (Task 5), integration tests (Task 7). ✔
- **Type consistency:** `List`/`ListAuditEntriesAdmin` everywhere return `(rows, total, prev, next, err)`; `Around`/`AroundAuditEntriesAdmin` same shape; `StreamFiltered(ctx, f, batch, fn)` matches between interface, impl, and fake; `fetchKeysetPage`/`fetchAroundPage`/`fetchSide`/`keysetCursorIfExists`/`reverse` names are stable across tasks. ✔
- **No placeholders:** all steps carry real code/commands. ✔
```
