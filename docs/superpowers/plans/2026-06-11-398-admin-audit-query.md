# Admin Audit Query Endpoints (#398) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Four admin-only read endpoints over the two audit streams — `GET /admin/audit/system(/{entry_id})` and `GET /admin/audit/threat_models(/{entry_id})` — with keyset cursor pagination and investigative filters.

**Architecture:** OpenAPI-first (spec → `make generate-api` → handlers on `*Server`). Keyset cursor over `(created_at, id)` encoded base64url. Store layer: extended `AuditFilters` + cross-TM list on the audit service; new filtered `List`/`GetByID` on `SystemAuditRepository`. One new index `idx_audit_actor (actor_email, created_at)` on `audit_entries`.

**Tech Stack:** Go, Gin, oapi-codegen, GORM, PostgreSQL/Oracle, testify.

**Spec:** `docs/superpowers/specs/2026-06-11-398-admin-audit-query-design.md` — read it first.

**Branch:** work on `dev/1.4.0`. Follow-ups #456/#457 (per-TM trail normalization/cursor) are NOT part of this plan.

---

### Task 1: Cursor helper (TDD)

**Files:**
- Create: `api/audit_cursor.go`
- Create: `api/audit_cursor_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditCursor_RoundTrip(t *testing.T) {
	ts := time.Date(2026, 6, 11, 12, 34, 56, 789000000, time.UTC)
	enc := encodeAuditCursor(ts, "abc-123")
	dec, err := decodeAuditCursor(enc)
	require.NoError(t, err)
	assert.True(t, dec.CreatedAt.Equal(ts))
	assert.Equal(t, "abc-123", dec.ID)
}

func TestAuditCursor_Invalid(t *testing.T) {
	for _, bad := range []string{"", "!!!not-base64!!!", "aGVsbG8"} { // last decodes to "hello", not JSON
		_, err := decodeAuditCursor(bad)
		assert.Error(t, err, "input %q must be rejected", bad)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestAuditCursor`
Expected: FAIL — `undefined: encodeAuditCursor`

- [ ] **Step 3: Implement**

Create `api/audit_cursor.go`:

```go
package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// auditCursor is the keyset-pagination position for audit list endpoints:
// the (created_at, id) of the last row returned. Encoded opaque so clients
// cannot depend on its structure (#398).
type auditCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"i"`
}

func encodeAuditCursor(createdAt time.Time, id string) string {
	b, _ := json.Marshal(auditCursor{CreatedAt: createdAt.UTC(), ID: id})
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
	return &c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestAuditCursor`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/audit_cursor.go api/audit_cursor_test.go
git commit -m "feat(api): opaque keyset cursor helper for audit list pagination

Refs #398.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Cross-TM audit list on the audit service (TDD)

**Files:**
- Modify: `api/audit_service.go` (AuditFilters + interface)
- Modify: `api/audit_store.go` (applyAuditFilters + new method)
- Modify: `api/audit_debouncer_test.go` (mock stub)
- Create: `api/admin_audit_list_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAdminAuditListDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.AuditEntry{}))
	return db
}

func seedAdminAuditEntry(t *testing.T, db *gorm.DB, actorEmail, provider, tmID string, ageMinutes int) string {
	t.Helper()
	v := 1
	e := models.AuditEntry{
		ThreatModelID: models.DBVarchar(tmID),
		ObjectType:    models.DBVarchar("threat_model"),
		ObjectID:      models.DBVarchar(uuid.New().String()),
		Version:       &v,
		ChangeType:    models.DBVarchar("created"),
		ActorEmail:    models.DBVarchar(actorEmail),
		ActorProvider: models.DBVarchar(provider),
	}
	require.NoError(t, db.Create(&e).Error)
	ts := time.Now().UTC().Add(-time.Duration(ageMinutes) * time.Minute)
	require.NoError(t, db.Exec("UPDATE audit_entries SET created_at = ? WHERE id = ?", ts, e.ID).Error)
	return string(e.ID)
}

func TestListAuditEntriesAdmin_CursorIteration(t *testing.T) {
	db := setupAdminAuditListDB(t)
	tmA, tmB := uuid.New().String(), uuid.New().String()
	// 5 entries across two TMs, distinct timestamps
	for i := 0; i < 5; i++ {
		tm := tmA
		if i%2 == 1 {
			tm = tmB
		}
		seedAdminAuditEntry(t, db, "alice@tmi.local", "tmi", tm, i+1)
	}

	svc := NewGormAuditService(db)
	page1, total, next, err := svc.ListAuditEntriesAdmin(context.Background(), 2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	require.Len(t, page1, 2)
	require.NotNil(t, next, "full page must yield a next cursor")

	cur, err := decodeAuditCursor(*next)
	require.NoError(t, err)
	page2, _, next2, err := svc.ListAuditEntriesAdmin(context.Background(), 2, cur, nil)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotNil(t, next2)

	cur2, err := decodeAuditCursor(*next2)
	require.NoError(t, err)
	page3, _, next3, err := svc.ListAuditEntriesAdmin(context.Background(), 2, cur2, nil)
	require.NoError(t, err)
	require.Len(t, page3, 1)
	assert.Nil(t, next3, "short page must not yield a next cursor")

	// no duplicates, no gaps across pages
	seen := map[string]bool{}
	for _, p := range [][]AuditEntryResponse{page1, page2, page3} {
		for _, e := range p {
			assert.False(t, seen[e.ID], "duplicate entry %s across pages", e.ID)
			seen[e.ID] = true
		}
	}
	assert.Len(t, seen, 5)
}

func TestListAuditEntriesAdmin_Filters(t *testing.T) {
	db := setupAdminAuditListDB(t)
	tmA, tmB := uuid.New().String(), uuid.New().String()
	seedAdminAuditEntry(t, db, "alice@tmi.local", "tmi", tmA, 10)
	seedAdminAuditEntry(t, db, "bob@tmi.local", "google", tmB, 20)

	svc := NewGormAuditService(db)

	provider := "google"
	rows, total, _, err := svc.ListAuditEntriesAdmin(context.Background(), 50, nil,
		&AuditFilters{ActorProvider: &provider})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, rows, 1)

	rows, total, _, err = svc.ListAuditEntriesAdmin(context.Background(), 50, nil,
		&AuditFilters{ThreatModelID: &tmA})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, rows, 1)
}
```

NOTE: check `AuditEntryResponse`'s ID field name in `api/audit_service.go` (the response type used by `toAuditEntryResponses`) and adjust `e.ID` accordingly.

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestListAuditEntriesAdmin`
Expected: FAIL — `svc.ListAuditEntriesAdmin undefined`

- [ ] **Step 3: Implement**

1. In `api/audit_service.go`, extend `AuditFilters` (currently ObjectType/ObjectID/ChangeType/ActorEmail/After/Before):

```go
type AuditFilters struct {
	ObjectType    *string
	ObjectID      *string
	ChangeType    *string
	ActorEmail    *string
	ActorProvider *string // admin cross-TM queries (#398)
	ThreatModelID *string // admin cross-TM queries (#398); per-TM reads still pass the scoped WHERE
	After         *time.Time
	Before        *time.Time
}
```

2. Add to `AuditServiceInterface`:

```go
	// ListAuditEntriesAdmin lists audit entries across ALL threat models for
	// admin investigation (#398). Keyset pagination: pass the decoded cursor
	// of the previous page (nil for the first page); returns the next-page
	// cursor (nil when exhausted), encoded for the wire.
	ListAuditEntriesAdmin(ctx context.Context, limit int, cursor *auditCursor, filters *AuditFilters) ([]AuditEntryResponse, int, *string, error)
```

3. In `api/audit_store.go`, extend `applyAuditFilters` with the two new filters:

```go
	if filters.ActorProvider != nil {
		query = query.Where("actor_provider = ?", *filters.ActorProvider)
	}
	if filters.ThreatModelID != nil {
		query = query.Where("threat_model_id = ?", *filters.ThreatModelID)
	}
```

4. Add the implementation (near `GetThreatModelAuditTrail`):

```go
// ListAuditEntriesAdmin lists audit entries across all threat models with
// keyset pagination ordered (created_at DESC, id DESC). The cursor predicate
// uses the expanded comparison form because Oracle has no row-value
// comparison.
func (s *GormAuditService) ListAuditEntriesAdmin(ctx context.Context, limit int, cursor *auditCursor, filters *AuditFilters) ([]AuditEntryResponse, int, *string, error) {
	count := applyAuditFilters(s.db.WithContext(ctx).Model(&models.AuditEntry{}), filters)
	var total int64
	if err := count.Count(&total).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("failed to count audit entries: %w", err)
	}

	q := applyAuditFilters(s.db.WithContext(ctx).Model(&models.AuditEntry{}), filters)
	if cursor != nil {
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)",
			cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	var entries []models.AuditEntry
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&entries).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("failed to list audit entries: %w", err)
	}

	var next *string
	if len(entries) == limit && limit > 0 {
		last := entries[len(entries)-1]
		enc := encodeAuditCursor(last.CreatedAt, string(last.ID))
		next = &enc
	}
	return toAuditEntryResponses(entries), int(total), next, nil
}
```

(Two separate query builders — do NOT reuse one `*gorm.DB` chain for count and list; GORM chains accumulate clauses.)

5. Add the stub to `mockAuditService` in `api/audit_debouncer_test.go` (and any other `AuditServiceInterface` mock — `grep -rn "AuditServiceInterface" --include="*_test.go" api/`):

```go
func (m *mockAuditService) ListAuditEntriesAdmin(_ context.Context, _ int, _ *auditCursor, _ *AuditFilters) ([]AuditEntryResponse, int, *string, error) {
	return nil, 0, nil, nil
}
```

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestListAuditEntriesAdmin` — PASS. Then `make build-server && make test-unit` — green.

- [ ] **Step 5: Commit**

```bash
git add api/audit_service.go api/audit_store.go api/audit_debouncer_test.go api/admin_audit_list_test.go
git commit -m "feat(api): cross-TM audit entry listing with keyset pagination

Refs #398.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: SystemAuditRepository filtered list + GetByID (TDD)

**Files:**
- Modify: `api/system_audit_repository.go`
- Create: `api/system_audit_repository_list_test.go`

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSysAuditListDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	return db
}

func seedSysAuditRow(t *testing.T, db *gorm.DB, actor, provider, method, path, field string, ageMinutes int) string {
	t.Helper()
	e := models.SystemAuditEntry{
		ID:               models.DBVarchar(uuid.New().String()),
		ActorEmail:       models.DBVarchar(actor),
		ActorProvider:    models.DBVarchar(provider),
		ActorProviderID:  models.DBVarchar(actor),
		ActorDisplayName: models.DBVarchar("Test"),
		HTTPMethod:       models.DBVarchar(method),
		HTTPPath:         models.DBText(path),
		FieldPath:        models.DBVarchar(field),
	}
	require.NoError(t, db.Create(&e).Error)
	ts := time.Now().UTC().Add(-time.Duration(ageMinutes) * time.Minute)
	require.NoError(t, db.Exec("UPDATE system_audit_entries SET created_at = ? WHERE id = ?", ts, e.ID).Error)
	return string(e.ID)
}

func TestSystemAuditList_FiltersAndCursor(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()

	seedSysAuditRow(t, db, "charlie@tmi.local", "tmi", "PUT", "/admin/settings/a", "a", 10)
	seedSysAuditRow(t, db, "charlie@tmi.local", "tmi", "DELETE", "/admin/settings/b", "b", 20)
	seedSysAuditRow(t, db, "dave@tmi.local", "google", "PUT", "/admin/quotas/users/x", "quota", 30)

	method := "PUT"
	rows, total, _, err := repo.List(ctx, SystemAuditFilter{HTTPMethod: &method, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, rows, 2)

	prefix := "/admin/settings"
	rows, total, _, err = repo.List(ctx, SystemAuditFilter{PathPrefix: &prefix, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, rows, 2)

	// LIKE metacharacters in the prefix must be treated literally
	weird := "/admin/100%_done"
	_, total, _, err = repo.List(ctx, SystemAuditFilter{PathPrefix: &weird, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 0, total)

	// cursor iteration: page size 2 over 3 rows
	page1, total, next, err := repo.List(ctx, SystemAuditFilter{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, page1, 2)
	require.NotNil(t, next)
	cur, err := decodeAuditCursor(*next)
	require.NoError(t, err)
	page2, _, next2, err := repo.List(ctx, SystemAuditFilter{Limit: 2, Cursor: cur})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Nil(t, next2)
}

func TestSystemAuditGetByID(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()

	id := seedSysAuditRow(t, db, "charlie@tmi.local", "tmi", "PUT", "/admin/settings/a", "a", 1)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "charlie@tmi.local", string(got.ActorEmail))

	got, err = repo.GetByID(ctx, uuid.New().String())
	require.NoError(t, err, "unknown id is not an error")
	assert.Nil(t, got, "unknown id returns nil entry")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestSystemAudit`
Expected: FAIL — `undefined: SystemAuditFilter` / `repo.List undefined`

- [ ] **Step 3: Implement**

In `api/system_audit_repository.go`:

```go
// SystemAuditFilter carries the admin query dimensions for system audit
// entries (#398). All filter fields are optional and AND-combined.
type SystemAuditFilter struct {
	ActorEmail    *string
	ActorProvider *string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	HTTPMethod    *string
	PathPrefix    *string // matched as LIKE '<escaped>%' ESCAPE '\'
	FieldPath     *string
	Limit         int
	Cursor        *auditCursor
}
```

Extend the interface:

```go
type SystemAuditRepository interface {
	Create(ctx context.Context, entry models.SystemAuditEntry) error
	ListByActor(ctx context.Context, actorEmail string, from, to time.Time) ([]models.SystemAuditEntry, error)
	// List returns entries matching the filter, newest first, with keyset
	// pagination. Returns (page, total matching the filter, encoded next
	// cursor or nil) (#398).
	List(ctx context.Context, f SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, error)
	// GetByID returns the entry or nil when not found.
	GetByID(ctx context.Context, id string) (*models.SystemAuditEntry, error)
}
```

Implementation on `systemAuditRepoGORM`:

```go
// escapeLikePrefix escapes LIKE metacharacters and appends the wildcard so a
// user-supplied prefix is matched literally. The ESCAPE '\' clause is
// specified explicitly — required on Oracle, harmless on PostgreSQL.
func escapeLikePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s) + "%"
}

func (r *systemAuditRepoGORM) applyFilter(q *gorm.DB, f SystemAuditFilter) *gorm.DB {
	if f.ActorEmail != nil {
		q = q.Where("actor_email = ?", *f.ActorEmail)
	}
	if f.ActorProvider != nil {
		q = q.Where("actor_provider = ?", *f.ActorProvider)
	}
	if f.CreatedAfter != nil {
		q = q.Where("created_at >= ?", *f.CreatedAfter)
	}
	if f.CreatedBefore != nil {
		q = q.Where("created_at <= ?", *f.CreatedBefore)
	}
	if f.HTTPMethod != nil {
		q = q.Where("http_method = ?", *f.HTTPMethod)
	}
	if f.PathPrefix != nil {
		q = q.Where(`http_path LIKE ? ESCAPE '\'`, escapeLikePrefix(*f.PathPrefix))
	}
	if f.FieldPath != nil {
		q = q.Where("field_path = ?", *f.FieldPath)
	}
	return q
}

func (r *systemAuditRepoGORM) List(ctx context.Context, f SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, error) {
	var total int64
	if err := r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f).Count(&total).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("count system audit entries: %w", err)
	}

	q := r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f)
	if f.Cursor != nil {
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)",
			f.Cursor.CreatedAt, f.Cursor.CreatedAt, f.Cursor.ID)
	}
	var rows []models.SystemAuditEntry
	if err := q.Order("created_at DESC, id DESC").Limit(f.Limit).Find(&rows).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("list system audit entries: %w", err)
	}

	var next *string
	if f.Limit > 0 && len(rows) == f.Limit {
		last := rows[len(rows)-1]
		enc := encodeAuditCursor(last.CreatedAt, string(last.ID))
		next = &enc
	}
	return rows, int(total), next, nil
}

func (r *systemAuditRepoGORM) GetByID(ctx context.Context, id string) (*models.SystemAuditEntry, error) {
	var row models.SystemAuditEntry
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get system audit entry: %w", err)
	}
	return &row, nil
}
```

Add imports (`errors`, `fmt`, `strings`). Update any test doubles implementing `SystemAuditRepository` (`grep -rn "SystemAuditRepository" --include="*_test.go" .` — the step-up adapter tests may stub it).

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestSystemAudit` — PASS. Then `make build-server && make test-unit`.

- [ ] **Step 5: Commit**

```bash
git add api/system_audit_repository.go api/system_audit_repository_list_test.go
git commit -m "feat(api): filtered keyset listing + GetByID for system audit entries

Refs #398.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: idx_audit_actor index on audit_entries

**Files:**
- Modify: `api/models/audit.go`

- [ ] **Step 1: Add the index tags**

In `api/models/audit.go`, change two fields of `AuditEntry`:

```go
	ActorEmail       DBVarchar      `gorm:"size:320;not null;index:idx_audit_actor,priority:1"`
```

and on `CreatedAt`, append the new composite membership (keeping the existing tags):

```go
	CreatedAt        time.Time      `gorm:"not null;autoCreateTime;index:idx_audit_tm_created,priority:2;index:idx_audit_actor,priority:2"`
```

- [ ] **Step 2: Build, test, check dbtool**

Run: `make build-server && make test-unit`
Then: `grep -rn "idx_audit\|audit_entries" cmd/dbtool/ | head` — if dbtool enumerates expected indexes (see `internal/dbschema/schema.go` too: `grep -n "idx_audit" internal/dbschema/schema.go`), add `idx_audit_actor` there to keep the validator in sync (CLAUDE.md rule: dbtool/schema utilities updated on schema change).

- [ ] **Step 3: Commit**

```bash
git add api/models/audit.go internal/dbschema/schema.go cmd/dbtool/  # whichever changed
git commit -m "feat(models): add idx_audit_actor (actor_email, created_at) for cross-TM queries

Refs #398.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: OpenAPI spec — four operations, schemas, parameters

**Files:**
- Modify: `api-schema/tmi-openapi.json` (large file — use jq for surgical inserts, with a backup first)

- [ ] **Step 1: Backup and inspect anchors**

```bash
cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.$(date +%Y%m%d_%H%M%S).backup
jq -r '.components.parameters | keys[]' api-schema/tmi-openapi.json | grep -iE "created|limit|cursor|audit"
jq -r '.components.schemas.AuditActor | keys' api-schema/tmi-openapi.json
```

Confirm the exact names of: `CreatedAfterQueryParam`, `CreatedBeforeQueryParam`, `AuditChangeType`, `AuditObjectType`, `AuditActorEmail`, and the `AuditActor` + `AuditEntry` schemas. Adjust the `$ref`s below if they differ.

- [ ] **Step 2: Add new parameter components**

Add to `.components.parameters` (via `jq '.components.parameters += {...}'` or the Edit tool on the parameters block):

```json
{
  "AuditPageLimit": {
    "name": "limit", "in": "query", "required": false,
    "description": "Maximum number of entries to return per page.",
    "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 50}
  },
  "AuditCursor": {
    "name": "cursor", "in": "query", "required": false,
    "description": "Opaque pagination cursor from the previous page's next_cursor. Omit for the first page.",
    "schema": {"type": "string", "maxLength": 512}
  },
  "AuditActorProvider": {
    "name": "actor_provider", "in": "query", "required": false,
    "description": "Filter by the actor's identity provider.",
    "schema": {"type": "string", "maxLength": 100}
  },
  "AuditHTTPMethod": {
    "name": "http_method", "in": "query", "required": false,
    "description": "Filter system audit entries by HTTP method.",
    "schema": {"type": "string", "enum": ["POST", "PUT", "PATCH", "DELETE"]}
  },
  "AuditPathPrefix": {
    "name": "path_prefix", "in": "query", "required": false,
    "description": "Filter system audit entries whose request path starts with this prefix (matched literally).",
    "schema": {"type": "string", "maxLength": 1024}
  },
  "AuditFieldPath": {
    "name": "field_path", "in": "query", "required": false,
    "description": "Filter system audit entries by exact field path.",
    "schema": {"type": "string", "maxLength": 1024}
  },
  "AuditThreatModelId": {
    "name": "threat_model_id", "in": "query", "required": false,
    "description": "Filter audit entries to a single threat model.",
    "schema": {"type": "string", "format": "uuid"}
  }
}
```

- [ ] **Step 3: Add new schemas**

Add to `.components.schemas`:

```json
{
  "SystemAuditEntry": {
    "type": "object",
    "description": "An immutable system-level audit record of a successful /admin/* write (T7 evidence). Old/new values are redacted at write time.",
    "required": ["id", "actor", "http_method", "http_path", "field_path", "created_at"],
    "properties": {
      "id": {"type": "string", "format": "uuid", "description": "Entry identifier."},
      "actor": {"$ref": "#/components/schemas/AuditActor"},
      "http_method": {"type": "string", "description": "HTTP method of the audited request."},
      "http_path": {"type": "string", "description": "Request path of the audited request."},
      "field_path": {"type": "string", "description": "Dotted path of the changed field."},
      "old_value_redacted": {"type": "string", "nullable": true, "description": "Previous value, redacted at write time."},
      "new_value_redacted": {"type": "string", "nullable": true, "description": "New value, redacted at write time."},
      "change_summary": {"type": "string", "nullable": true, "description": "Human-readable change summary."},
      "created_at": {"type": "string", "format": "date-time", "description": "When the audited write completed."}
    }
  },
  "ListSystemAuditEntriesResponse": {
    "type": "object",
    "description": "Cursor-paginated list of system audit entries.",
    "required": ["entries", "total", "limit"],
    "properties": {
      "entries": {"type": "array", "items": {"$ref": "#/components/schemas/SystemAuditEntry"}},
      "total": {"type": "integer", "description": "Total entries matching the filter."},
      "limit": {"type": "integer", "description": "Page size used."},
      "next_cursor": {"type": "string", "nullable": true, "description": "Cursor for the next page; absent or null when exhausted."}
    }
  },
  "ListAdminAuditEntriesResponse": {
    "type": "object",
    "description": "Cursor-paginated cross-threat-model list of audit entries.",
    "required": ["entries", "total", "limit"],
    "properties": {
      "entries": {"type": "array", "items": {"$ref": "#/components/schemas/AuditEntry"}},
      "total": {"type": "integer", "description": "Total entries matching the filter."},
      "limit": {"type": "integer", "description": "Page size used."},
      "next_cursor": {"type": "string", "nullable": true, "description": "Cursor for the next page; absent or null when exhausted."}
    }
  }
}
```

- [ ] **Step 4: Add the four paths**

Add to `.paths` (model each operation on the existing `/admin/users` GET: `tags: ["Administration"]`, `security: [{"bearerAuth": []}]`, `"x-admin-only": true`, `"x-tmi-authz": {"ownership": "none", "roles": ["admin"]}`, error responses 400/401/403/(404 for the by-id ones)/429/500 using the file's existing error-response `$ref` pattern — copy it from `/admin/users`):

```json
{
  "/admin/audit/system": {
    "get": {
      "tags": ["Administration"],
      "summary": "List system audit entries",
      "description": "Cursor-paginated, filterable list of system-level admin-write audit records. Admin role required; read-only (no step-up).",
      "operationId": "listSystemAuditEntries",
      "security": [{"bearerAuth": []}],
      "parameters": [
        {"$ref": "#/components/parameters/AuditActorEmail"},
        {"$ref": "#/components/parameters/AuditActorProvider"},
        {"$ref": "#/components/parameters/CreatedAfterQueryParam"},
        {"$ref": "#/components/parameters/CreatedBeforeQueryParam"},
        {"$ref": "#/components/parameters/AuditHTTPMethod"},
        {"$ref": "#/components/parameters/AuditPathPrefix"},
        {"$ref": "#/components/parameters/AuditFieldPath"},
        {"$ref": "#/components/parameters/AuditPageLimit"},
        {"$ref": "#/components/parameters/AuditCursor"}
      ],
      "responses": {
        "200": {"description": "Paginated system audit entries", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ListSystemAuditEntriesResponse"}}}}
      },
      "x-admin-only": true,
      "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
    }
  },
  "/admin/audit/system/{entry_id}": {
    "get": {
      "tags": ["Administration"],
      "summary": "Get system audit entry",
      "operationId": "getSystemAuditEntry",
      "security": [{"bearerAuth": []}],
      "parameters": [
        {"name": "entry_id", "in": "path", "required": true, "schema": {"type": "string", "format": "uuid"}}
      ],
      "responses": {
        "200": {"description": "System audit entry", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/SystemAuditEntry"}}}}
      },
      "x-admin-only": true,
      "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
    }
  },
  "/admin/audit/threat_models": {
    "get": {
      "tags": ["Administration"],
      "summary": "List threat-model audit entries across all threat models",
      "description": "Cursor-paginated cross-threat-model admin view of the threat-model audit stream. Admin role required; read-only (no step-up).",
      "operationId": "listAdminThreatModelAuditEntries",
      "security": [{"bearerAuth": []}],
      "parameters": [
        {"$ref": "#/components/parameters/AuditActorEmail"},
        {"$ref": "#/components/parameters/AuditActorProvider"},
        {"$ref": "#/components/parameters/CreatedAfterQueryParam"},
        {"$ref": "#/components/parameters/CreatedBeforeQueryParam"},
        {"$ref": "#/components/parameters/AuditChangeType"},
        {"$ref": "#/components/parameters/AuditObjectType"},
        {"$ref": "#/components/parameters/AuditThreatModelId"},
        {"$ref": "#/components/parameters/AuditPageLimit"},
        {"$ref": "#/components/parameters/AuditCursor"}
      ],
      "responses": {
        "200": {"description": "Paginated audit entries", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ListAdminAuditEntriesResponse"}}}}
      },
      "x-admin-only": true,
      "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
    }
  },
  "/admin/audit/threat_models/{entry_id}": {
    "get": {
      "tags": ["Administration"],
      "summary": "Get a threat-model audit entry by id (admin)",
      "operationId": "getAdminThreatModelAuditEntry",
      "security": [{"bearerAuth": []}],
      "parameters": [
        {"name": "entry_id", "in": "path", "required": true, "schema": {"type": "string", "format": "uuid"}}
      ],
      "responses": {
        "200": {"description": "Audit entry", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/AuditEntry"}}}}
      },
      "x-admin-only": true,
      "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
    }
  }
}
```

Add the standard error responses (400/401/403/429/500, plus 404 on the two `{entry_id}` operations) by copying the exact `$ref` style used on `GET /admin/users` — the spec validator will flag missing ones.

- [ ] **Step 5: Validate and regenerate**

```bash
jq empty api-schema/tmi-openapi.json && echo Valid
make validate-openapi   # fix anything it reports
make generate-api       # regenerates api/api.go with the 4 ServerInterface methods
make build-server       # EXPECTED TO FAIL: Server is missing the 4 new methods — Task 6 adds them
```

- [ ] **Step 6: Commit (spec + generated code together with handlers in Task 6 — no commit yet)**

Hold the commit until Task 6 makes the build green (the repo should not have a commit that doesn't build).

---

### Task 6: Handlers

**Files:**
- Create: `api/admin_audit_handlers.go`
- Modify: server wiring (see Step 2)

- [ ] **Step 1: Implement the four handlers**

Create `api/admin_audit_handlers.go`. Generated param/struct names come from the operationIds — verify with `grep -n "ListSystemAuditEntriesParams\|ListAdminThreatModelAuditEntriesParams" api/api.go` after generation and adjust:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// adminAuditPageLimit applies the AuditPageLimit parameter defaults/bounds.
func adminAuditPageLimit(p *int) int {
	if p == nil {
		return 50
	}
	if *p < 1 {
		return 1
	}
	if *p > 100 {
		return 100
	}
	return *p
}

// ListSystemAuditEntries handles GET /admin/audit/system (#398).
func (s *Server) ListSystemAuditEntries(c *gin.Context, params ListSystemAuditEntriesParams) {
	logger := slogging.Get().WithContext(c)

	var cursor *auditCursor
	if params.Cursor != nil {
		decoded, err := decodeAuditCursor(*params.Cursor)
		if err != nil {
			HandleRequestError(c, InvalidInputError("Invalid pagination cursor"))
			return
		}
		cursor = decoded
	}

	limit := adminAuditPageLimit(params.Limit)
	filter := SystemAuditFilter{
		ActorEmail:    params.ActorEmail,
		ActorProvider: params.ActorProvider,
		CreatedAfter:  params.CreatedAfter,
		CreatedBefore: params.CreatedBefore,
		HTTPMethod:    (*string)(params.HttpMethod),
		PathPrefix:    params.PathPrefix,
		FieldPath:     params.FieldPath,
		Limit:         limit,
		Cursor:        cursor,
	}

	rows, total, next, err := s.systemAuditRepo.List(c.Request.Context(), filter)
	if err != nil {
		logger.Error("Failed to list system audit entries: %v", err)
		HandleRequestError(c, ServerError("Failed to list system audit entries"))
		return
	}

	entries := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, systemAuditEntryToAPI(r))
	}
	writeAdminAuditJSON(c, logger, gin.H{
		"entries": entries, "total": total, "limit": limit, "next_cursor": next,
	})
}

// GetSystemAuditEntry handles GET /admin/audit/system/{entry_id} (#398).
func (s *Server) GetSystemAuditEntry(c *gin.Context, entryId EntryId) {
	logger := slogging.Get().WithContext(c)
	row, err := s.systemAuditRepo.GetByID(c.Request.Context(), entryId.String())
	if err != nil {
		logger.Error("Failed to get system audit entry: %v", err)
		HandleRequestError(c, ServerError("Failed to get system audit entry"))
		return
	}
	if row == nil {
		HandleRequestError(c, NotFoundError("System audit entry not found"))
		return
	}
	writeAdminAuditJSON(c, logger, systemAuditEntryToAPI(*row))
}

// ListAdminThreatModelAuditEntries handles GET /admin/audit/threat_models (#398).
func (s *Server) ListAdminThreatModelAuditEntries(c *gin.Context, params ListAdminThreatModelAuditEntriesParams) {
	logger := slogging.Get().WithContext(c)

	var cursor *auditCursor
	if params.Cursor != nil {
		decoded, err := decodeAuditCursor(*params.Cursor)
		if err != nil {
			HandleRequestError(c, InvalidInputError("Invalid pagination cursor"))
			return
		}
		cursor = decoded
	}

	limit := adminAuditPageLimit(params.Limit)
	filters := &AuditFilters{
		ActorEmail:    params.ActorEmail,
		ActorProvider: params.ActorProvider,
		After:         params.CreatedAfter,
		Before:        params.CreatedBefore,
		ChangeType:    (*string)(params.ChangeType),
		ObjectType:    (*string)(params.ObjectType),
	}
	if params.ThreatModelId != nil {
		tm := params.ThreatModelId.String()
		filters.ThreatModelID = &tm
	}

	rows, total, next, err := s.auditService.ListAuditEntriesAdmin(c.Request.Context(), limit, cursor, filters)
	if err != nil {
		logger.Error("Failed to list audit entries: %v", err)
		HandleRequestError(c, ServerError("Failed to list audit entries"))
		return
	}
	writeAdminAuditJSON(c, logger, gin.H{
		"entries": rows, "total": total, "limit": limit, "next_cursor": next,
	})
}

// GetAdminThreatModelAuditEntry handles GET /admin/audit/threat_models/{entry_id} (#398).
func (s *Server) GetAdminThreatModelAuditEntry(c *gin.Context, entryId EntryId) {
	logger := slogging.Get().WithContext(c)
	entry, err := s.auditService.GetAuditEntry(c.Request.Context(), entryId.String())
	if err != nil {
		logger.Error("Failed to get audit entry: %v", err)
		HandleRequestError(c, ServerError("Failed to get audit entry"))
		return
	}
	if entry == nil {
		HandleRequestError(c, NotFoundError("Audit entry not found"))
		return
	}
	writeAdminAuditJSON(c, logger, toAPIAuditEntry(*entry))
}

// writeAdminAuditJSON marshals explicitly so serialization errors return 500
// instead of a silent empty 200 (same rationale as ListAdminUsers).
func writeAdminAuditJSON(c *gin.Context, logger *slogging.Logger, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("Failed to marshal admin audit response: %v", err)
		HandleRequestError(c, ServerError("Failed to serialize response"))
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}
```

Plus the model→API mapper (snake_case properties per the schema):

```go
func systemAuditEntryToAPI(e models.SystemAuditEntry) gin.H {
	return gin.H{
		"id": string(e.ID),
		"actor": gin.H{
			"email":        string(e.ActorEmail),
			"provider":     string(e.ActorProvider),
			"provider_id":  string(e.ActorProviderID),
			"display_name": string(e.ActorDisplayName),
		},
		"http_method":        string(e.HTTPMethod),
		"http_path":          string(e.HTTPPath),
		"field_path":         string(e.FieldPath),
		"old_value_redacted": nullableTextOrNil(e.OldValueRedacted),
		"new_value_redacted": nullableTextOrNil(e.NewValueRedacted),
		"change_summary":     nullableTextOrNil(e.ChangeSummary),
		"created_at":         e.CreatedAt.UTC(),
	}
}
```

Implementer notes (resolve while coding, all are verifiable in-repo):
- Match the `AuditActor` schema's property names exactly (Step 1 of Task 5 inspected it).
- `nullableTextOrNil`: check how existing handlers serialize `models.NullableDBText` (grep `NullableDBText` in `api/*.go` handlers); reuse the existing helper if one exists, else add this small one.
- The generated path-param type may be `EntryId` or an inline `openapi_types.UUID` — match `api/api.go`.
- `InvalidInputError` / `NotFoundError` / `ServerError`: confirm exact helper names in `api/request_errors.go` (or wherever `HandleRequestError` peers live); `GoneError` exists, so the family does too.
- Wiring: confirm how `*Server` reaches the audit service in existing handlers (`grep -n "auditService" api/server.go api/audit_handlers.go`). If audit handlers live on a separate `AuditHandler` struct and `*Server` delegates, follow that pattern. `s.systemAuditRepo` likely needs adding to the `Server` struct + its constructor, fed from `cmd/server/main.go` where `NewSystemAuditRepository` is already constructed for the audit middleware — pass the same instance.

- [ ] **Step 2: Build, lint, unit tests**

Run: `make build-server && make lint && make test-unit`
Expected: all green (the interface is now fully implemented).

- [ ] **Step 3: Commit spec + generated + handlers together**

```bash
git add api-schema/tmi-openapi.json api/api.go api/admin_audit_handlers.go api/server.go cmd/server/main.go
git commit -m "feat(api): admin audit query endpoints for system and threat-model streams

GET /admin/audit/system(/{entry_id}) and /admin/audit/threat_models(/{entry_id})
with keyset cursor pagination and investigative filters. Admin role required;
read-only, so no step-up. Fixes #398 pending verification.

Refs #398.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

(Delete the spec backup file; do not commit it.)

---

### Task 7: Integration test

**Files:**
- Create: `test/integration/workflows/admin_audit_query_test.go`

- [ ] **Step 1: Write the test**

Follow the file conventions of `test/integration/workflows/client_credentials_test.go` (`INTEGRATION_TESTS` guard, `framework.AuthenticateAdmin`, `framework.NewClient`). Flow:

1. As admin (`charlie`): `PUT /admin/settings/test.auditquery` twice with different values (generates ≥2 system audit rows via the #355 middleware), then create+delete a threat model (generates threat-model audit rows).
2. `GET /admin/audit/system?actor_email=<charlie's email>&limit=1` → 200; assert `entries` has 1 row with `actor.email`, `http_method=PUT`, `field_path` populated; `total >= 2`; `next_cursor` non-null. Follow the cursor → second page; assert the entry differs from page 1 (no duplicates).
3. `GET /admin/audit/system?path_prefix=/admin/settings/test.auditquery` → all returned rows have matching `http_path` prefix.
4. `GET /admin/audit/system/{entry_id}` with an id from step 2 → 200, same row; with a random UUID → 404; with `cursor=!!!` on the list → 400.
5. `GET /admin/audit/threat_models?threat_model_id=<id>` → 200 with the created/deleted entries; `change_type=deleted` filter narrows correctly.
6. `GET /admin/audit/threat_models/{entry_id}` → 200 / random UUID → 404.
7. Negative authz: a fresh non-admin user (`framework` flow with a `UniqueUserID`) gets 403 on all four endpoints.

Assert concrete JSON fields, not just status codes. Mark each subtest with `t.Run`.

- [ ] **Step 2: Run**

Run: `make test-integration`
Expected: all green including the new test.

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/admin_audit_query_test.go
git commit -m "test(integration): cover admin audit query endpoints

Refs #398.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: Gates, reviews, close-out

- [ ] **Step 1: Full gates**

Run: `make lint && make build-server && make test-unit && make test-integration`

- [ ] **Step 2: MANDATORY — oracle-db-admin review**

Dispatch with the diff of `api/models/audit.go`, `api/audit_store.go`, `api/system_audit_repository.go`, `api/audit_cursor.go`. Specific questions:
- New `idx_audit_actor` via AutoMigrate on Oracle (identifier length, existing-table index build).
- Keyset predicate `created_at < ? OR (created_at = ? AND id < ?)` — timestamp equality round-trip through godror into TIMESTAMP(6); any precision-loss risk that breaks cursor continuity?
- `LIKE ? ESCAPE '\'` against the `http_path` CLOB on Oracle.
Address every BLOCKING finding.

- [ ] **Step 3: API-change test battery (per CLAUDE.md)**

- Postman/newman: check `make list-targets` for the postman target; run and fix failures (the new endpoints may need collection entries — add them).
- `make cats-fuzz` then `make analyze-cats-results` (query the SQLite DB). Triage new-endpoint findings; zero-500 policy — any 500 from fuzzing the cursor/filters is a must-fix (cursor decode and UUID parsing must 400).
- Run the `security-review` skill; stop and surface findings.

- [ ] **Step 4: Oracle ADB verification**

Run: `make test-integration-oci`.

- [ ] **Step 5: Wiki**

Add an "Admin audit queries" section to the wiki's audit page (local checkout `/Users/efitz/Projects/tmi-wiki`): the four endpoints, filters, cursor iteration example (`curl` with `next_cursor` loop), and the no-step-up-on-reads rationale. Commit and push the wiki.

- [ ] **Step 6: Land and close**

```bash
git pull --rebase && git push && git status   # up to date with origin
gh issue comment 398 --body "Implemented on dev/1.4.0: GET /admin/audit/system(/{entry_id}) and GET /admin/audit/threat_models(/{entry_id}) — admin-only, keyset cursor pagination, field-qualified time filters (created_after/created_before). Step-up not required on reads (write-only step-up table). Follow-ups: #456 (per-TM filter-name normalization), #457 (per-TM cursor migration). Design: docs/superpowers/specs/2026-06-11-398-admin-audit-query-design.md."
gh issue close 398
```

---

## Self-Review Notes (already applied)

- Spec coverage: cursor helper (T1), cross-TM list + filters (T2), system repo list/get (T3), index (T4), OpenAPI (T5), handlers (T6), integration (T7), reviews/CATS/wiki/close (T8).
- Build-breaking window handled: Task 5 deliberately ends red (generated interface unimplemented) and Task 6 commits spec+generated+handlers atomically.
- Type consistency: `auditCursor`/`encodeAuditCursor`/`decodeAuditCursor` (T1) used in T2/T3/T6; `SystemAuditFilter` fields match between T3 impl and T6 handler; `AuditFilters.ActorProvider/ThreatModelID` match T2 and T6.
- Known-unknowns are flagged as implementer notes with in-repo verification commands (generated param names, error-helper names, Server wiring, AuditActor property names) rather than guessed silently.
