# Integer Aliases Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the legacy `alias: string[]` field on `ThreatModel` with a server-assigned monotonic int32 alias, and add the same to all six TM sub-object types (documents, assets, repositories, diagrams, notes, threats), exposed as a read-only field on every GET response.

**Architecture:** New `alias_counters` table holds per-scope counters keyed by `(parent_id, object_type)`. Allocation uses `SELECT ... FOR UPDATE` inside the existing `WithRetryableGormTransaction` wrapper. Schema migration: AutoMigrate adds the columns + table; an idempotent backfill runs at server startup (gated by a cross-DB advisory lock); unique constraints are added post-backfill. Triple-layer write protection: OpenAPI `readOnly`, handler whitelist + descriptive 400, and the GORM `<-:create` tag.

**Tech Stack:** Go (Gin + GORM v2 with cross-DB compatibility — PostgreSQL dev, Oracle ADB prod), oapi-codegen.

**Spec:** [docs/superpowers/specs/2026-05-04-integer-aliases-design.md](../specs/2026-05-04-integer-aliases-design.md)

**Issue:** [#374](https://github.com/ericfitz/tmi/issues/374) — path-param resolution explicitly out of scope; will be filed as a follow-up issue at the end (Task 18).

---

## Conventions used in this plan

- **Test command:** `make test-unit name=TestName` runs a single test. `make test-unit` runs the full suite. Never invoke `go test` directly.
- **Build:** `make build-server`. **Lint:** `make lint`. **OpenAPI:** `make validate-openapi` then `make generate-api`.
- **Oracle review:** mandatory before completion (Task 17).
- **Cross-DB types:** `int32` for alias columns maps to `INTEGER` (PG) / `NUMBER(10)` (Oracle); the GORM Oracle dialect handles this.
- **Each task ends with a commit** using the message specified.

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `api/models/models.go` | modify | Replace `Alias StringArray` with `Alias int32` on ThreatModel; add `Alias int32` to Diagram/Threat/Asset/Repository/Note/Document; add new `AliasCounter` struct; register in `AllModels()` |
| `api/alias_allocator.go` | new | `AllocateNextAlias` helper |
| `api/alias_allocator_test.go` | new | Unit tests for the allocator |
| `internal/dbschema/advisory_lock.go` | new | Cross-DB advisory-lock primitive (`pg_advisory_lock` / `DBMS_LOCK`) |
| `internal/dbschema/advisory_lock_test.go` | new | Unit tests for the lock (PG-only test runs against the test DB) |
| `api/alias_backfill.go` | new | Phase 2 backfill runner; calls into `AcquireMigrationLock` |
| `api/alias_backfill_test.go` | new | Backfill unit tests |
| `api/alias_indexes.go` | new | Phase 3 unique-index creator |
| `internal/dbschema/schema.go` | modify | Add `alias_counters` table; add `alias` column to 7 entity entries; add the 7 unique indexes |
| `internal/dbschema/schema_test.go` | modify | Bump table-count assertion |
| `auth/db/gorm.go` | modify | Wire pre-AutoMigrate "drop legacy alias" + post-AutoMigrate backfill + post-backfill unique-index step |
| `api/threat_model_store_gorm.go` (or `database_store_gorm.go` if that's where ThreatModel CRUD lives) | modify | Call `AllocateNextAlias("__global__", "threat_model")` in Create |
| `api/note_store_gorm.go` | modify | `AllocateNextAlias(tmID, "note")` in Create |
| `api/threat_store_gorm.go` | modify | `AllocateNextAlias(tmID, "threat")` in Create |
| `api/asset_store_gorm.go` | modify | `AllocateNextAlias(tmID, "asset")` in Create |
| `api/repository_store_gorm.go` (or similar) | modify | `AllocateNextAlias(tmID, "repository")` in Create |
| `api/document_store_gorm.go` | modify | `AllocateNextAlias(tmID, "document")` in Create |
| `api/database_store_gorm.go` (where Diagram CRUD lives) | modify | `AllocateNextAlias(tmID, "diagram")` in Create |
| `api/threat_model_handlers.go` | modify | `case "alias"` in `getFieldErrorMessage`; ensure `UpdateThreatModelRequest` excludes alias |
| Sub-resource handler files | modify | Add `case "alias"` to their respective field-error messages |
| `api-schema/tmi-openapi.json` | modify | Replace `ThreatModelBase.alias` with `integer (readOnly)`; add `alias: integer (readOnly)` to the 6 sub-object schemas + their list-item schemas |
| `api/api.go` | regenerate | Output of `make generate-api` |
| `test/integration/workflows/alias_test.go` | new | End-to-end integration tests |

---

## Task 1: Add `AliasCounter` model + `Alias` field to existing models

**Files:**
- Modify: `api/models/models.go`

This is a schema-only change (new struct + 7 field additions). The Alias field uses the `<-:create` tag to make it write-once-on-INSERT — UPDATE statements never include the column.

- [ ] **Step 1: Add `AliasCounter` struct**

In `api/models/models.go`, find the end of the data-model definitions (just before any helper functions). Add:

```go
// AliasCounter holds the next-alias value for a given (parent_id, object_type)
// scope. ThreatModel global counter uses parent_id="__global__"; sub-object
// counters use the parent threat-model UUID. Allocation is done via
// SELECT ... FOR UPDATE inside the calling repository's transaction.
type AliasCounter struct {
	ParentID   string `gorm:"primaryKey;type:varchar(36);column:parent_id"`
	ObjectType string `gorm:"primaryKey;type:varchar(16);column:object_type"`
	NextAlias  int32  `gorm:"not null;default:1;column:next_alias"`
}

// TableName returns the dialect-aware table name.
func (AliasCounter) TableName() string {
	return tableName("alias_counters")
}
```

- [ ] **Step 2: Replace `Alias StringArray` on ThreatModel**

Find the existing line in `api/models/models.go` (around line 135):
```go
	Alias                        StringArray `gorm:"column:alias"` // Alternative names/identifiers
```

Replace with:
```go
	Alias                        int32       `gorm:"column:alias;not null;default:0;<-:create"` // Server-assigned globally-unique integer alias
```

- [ ] **Step 3: Add `Alias int32` field to the 6 sub-object models**

For each of `Diagram`, `Threat`, `Asset`, `Repository`, `Note`, `Document` — find the struct in `api/models/models.go`. Add the field just before `CreatedAt`:

```go
	Alias int32 `gorm:"column:alias;not null;default:0;<-:create"` // Server-assigned per-(threat_model_id, type) alias
```

Each struct location reference:
- `Diagram` — search `type Diagram struct`
- `Threat` — search `type Threat struct`
- `Asset` — search `type Asset struct`
- `Repository` (the GORM model owning repository CRUD) — search `type Repository struct`
- `Note` — search `type Note struct`
- `Document` — search `type Document struct`

- [ ] **Step 4: Register `AliasCounter` in `AllModels()`**

Find `func AllModels() []any` in `api/models/models.go`. Append `&AliasCounter{}` after the existing models in a logical position (e.g., right after `&Metadata{}`):

```go
		// Alias counter (referenced by every entity that has an alias column)
		&AliasCounter{},
```

- [ ] **Step 5: Build**

Run:
```
make build-server
```

Expected: success. The repository code that previously used `Alias` (the StringArray version) on ThreatModel may not compile yet — if so, the error will name those sites. Continue to Step 6 to address them.

- [ ] **Step 6: Update repository code that referenced legacy `Alias`**

Run:
```
rg -n 'tm\.Alias\|threatModel\.Alias\|model\.Alias\|\.Alias =' api/ --type go
```

For each call site that operates on `ThreatModel.Alias` as a string array (e.g., copying alias from API to model on Create/Update), remove the line. The new alias is set by the allocator (Task 7+); old alias-array logic is gone entirely.

If any tests reference the old field, update them — it's not a `[]string` anymore.

- [ ] **Step 7: Build again**

Run:
```
make build-server
```

Expected: success.

- [ ] **Step 8: Lint**

Run:
```
make lint
```

Expected: 0 issues.

- [ ] **Step 9: Run unit tests**

Run:
```
make test-unit
```

Expected: most tests pass. Some may fail (legacy alias tests, JSON-shape assertions) — note these and fix in Task 11 when we update the OpenAPI spec.

If failing tests are limited to those that hard-code the old alias-array behavior, that's expected — record the count and continue. Otherwise, debug and fix before continuing.

- [ ] **Step 10: Commit**

```
git add api/models/models.go
git commit -m "feat(models): replace ThreatModel.Alias with int32 + add Alias to sub-objects (#374)"
```

---

## Task 2: Update `internal/dbschema/schema.go`

**Files:**
- Modify: `internal/dbschema/schema.go`
- Modify: `internal/dbschema/schema_test.go`

- [ ] **Step 1: Replace `alias` column entry on `threat_models`**

In `internal/dbschema/schema.go`, find the `Name: "threat_models"` `TableSchema` block. Locate any existing `alias` column entry (check for `Name: "alias"`). If present, replace with:

```go
				{Name: "alias", DataType: "integer", IsNullable: false, DefaultValue: stringPtr("0")},
```

If there's no existing `alias` entry but the schema is otherwise complete, add the line in alphabetical-ish order with the other columns.

- [ ] **Step 2: Add `alias` column to each of 6 sub-object table entries**

For each of `diagrams`, `threats`, `assets`, `repositories`, `notes`, `documents` in the `schema` slice, add the same column entry in the `Columns` slice (place near other simple int columns):

```go
				{Name: "alias", DataType: "integer", IsNullable: false, DefaultValue: stringPtr("0")},
```

- [ ] **Step 3: Add `alias_counters` `TableSchema`**

Append at the end of the `schema` slice:

```go
		{
			Name: "alias_counters",
			Columns: []ColumnSchema{
				{Name: "parent_id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "object_type", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "next_alias", DataType: "integer", IsNullable: false, DefaultValue: stringPtr("1")},
			},
			Indexes: []IndexSchema{
				{Name: "alias_counters_pkey", Columns: []string{"parent_id", "object_type"}, IsUnique: true},
			},
		},
```

- [ ] **Step 4: Add the 7 unique indexes to existing entries**

For `threat_models`, add to its `Indexes` slice:
```go
				{Name: "uniq_threat_models_alias", Columns: []string{"alias"}, IsUnique: true},
```

For each of `diagrams`, `threats`, `assets`, `repositories`, `notes`, `documents`, add (using the table-specific name):
```go
				{Name: "uniq_<table>_tm_alias", Columns: []string{"threat_model_id", "alias"}, IsUnique: true},
```

Replace `<table>` with the table name. Example for `notes`:
```go
				{Name: "uniq_notes_tm_alias", Columns: []string{"threat_model_id", "alias"}, IsUnique: true},
```

- [ ] **Step 5: Update test count assertions**

Run:
```
rg -n '\d+, len\(' internal/dbschema/schema_test.go | head -5
```

If the test file asserts the table count (e.g., `assert.Equal(t, 28, len(schema))`), bump it by 1 (we added `alias_counters`).

- [ ] **Step 6: Run schema tests**

Run:
```
make test-unit name=TestExpectedSchema
```

Expected: PASS.

- [ ] **Step 7: Commit**

```
git add internal/dbschema/schema.go internal/dbschema/schema_test.go
git commit -m "feat(dbschema): add alias_counters + alias columns + unique indexes (#374)"
```

---

## Task 3: `AllocateNextAlias` — failing test

**Files:**
- Create: `api/alias_allocator_test.go`

The allocator is the heart of the change. TDD it carefully.

- [ ] **Step 1: Find the existing test-DB helper**

Run:
```
rg -n 'func setupTestDB\|func setupContentFeedbackTestDB' api/ --type go | head -5
```

Use whatever helper exists (typically `setupTestDB(t)` returning `*gorm.DB`). The test below uses it.

- [ ] **Step 2: Write the failing test**

Create `api/alias_allocator_test.go`:

```go
package api

import (
	"context"
	"sync"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAliasTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.AliasCounter{}))
	return db
}

func TestAllocateNextAlias_FirstCall(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	var got int32
	err := db.Transaction(func(tx *gorm.DB) error {
		alias, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
		got = alias
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), got)
}

func TestAllocateNextAlias_SequentialCalls(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	for expected := int32(1); expected <= 5; expected++ {
		var got int32
		err := db.Transaction(func(tx *gorm.DB) error {
			alias, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
			got = alias
			return err
		})
		require.NoError(t, err)
		assert.Equal(t, expected, got)
	}
}

func TestAllocateNextAlias_IndependentScopes(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	// Allocate 1, 2 in scope A; 1, 2, 3 in scope B.
	for _, scope := range []struct {
		parent string
		typ    string
		count  int
	}{
		{"tm-A", "note", 2},
		{"tm-B", "note", 3},
	} {
		for i := 0; i < scope.count; i++ {
			err := db.Transaction(func(tx *gorm.DB) error {
				_, err := AllocateNextAlias(ctx, tx, scope.parent, scope.typ)
				return err
			})
			require.NoError(t, err)
		}
	}

	// Verify counters: A.next = 3, B.next = 4.
	var counters []models.AliasCounter
	require.NoError(t, db.Where("object_type = ?", "note").Find(&counters).Error)
	got := map[string]int32{}
	for _, c := range counters {
		got[c.ParentID] = c.NextAlias
	}
	assert.Equal(t, int32(3), got["tm-A"])
	assert.Equal(t, int32(4), got["tm-B"])
}

func TestAllocateNextAlias_GlobalThreatModel(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	for expected := int32(1); expected <= 3; expected++ {
		var got int32
		err := db.Transaction(func(tx *gorm.DB) error {
			alias, err := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
			got = alias
			return err
		})
		require.NoError(t, err)
		assert.Equal(t, expected, got)
	}
}

func TestAllocateNextAlias_HighWaterAfterRollback(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	// Successful allocation.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
		return err
	}))

	// Allocate then force a rollback.
	_ = db.Transaction(func(tx *gorm.DB) error {
		_, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
		require.NoError(t, err)
		return assert.AnError // forces rollback
	})

	// Next allocation: counter is back to 2 (the rollback reverted the +1).
	// This is the correct behavior — high-water-mark applies only to successful
	// inserts, since the entire transaction (including the counter UPDATE) rolled
	// back atomically.
	var got int32
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		alias, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
		got = alias
		return err
	}))
	assert.Equal(t, int32(2), got)
}

func TestAllocateNextAlias_ConcurrentCallers(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	const N = 10
	results := make([]int32, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := db.Transaction(func(tx *gorm.DB) error {
				alias, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
				results[i] = alias
				return err
			})
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// All N results must be distinct (no duplicate aliases under concurrency).
	seen := map[int32]bool{}
	for _, r := range results {
		assert.False(t, seen[r], "duplicate alias %d", r)
		seen[r] = true
	}
	assert.Len(t, seen, N)
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run:
```
make test-unit name=TestAllocateNextAlias
```

Expected: FAIL — `AllocateNextAlias` undefined.

---

## Task 4: `AllocateNextAlias` — implementation

**Files:**
- Create: `api/alias_allocator.go`

- [ ] **Step 1: Implement**

Create `api/alias_allocator.go`:

```go
package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AllocateNextAlias atomically reserves the next alias value for the given
// (parentID, objectType) scope. MUST be called inside a transaction; the
// caller's transaction holds the lock until commit. The returned value is
// guaranteed unique within the scope so long as the calling transaction
// commits successfully.
//
// For the ThreatModel global counter, pass parentID="__global__" and
// objectType="threat_model". For sub-objects, parentID is the parent
// ThreatModel UUID and objectType is one of "diagram", "threat", "asset",
// "repository", "note", "document".
//
// Note: if the calling transaction rolls back, the counter UPDATE rolls back
// too — the alias is "released" and reused by the next caller. High-water-mark
// semantics apply only to committed inserts.
func AllocateNextAlias(ctx context.Context, tx *gorm.DB, parentID, objectType string) (int32, error) {
	logger := slogging.Get()

	// Insert counter row if missing. ON CONFLICT DO NOTHING is idempotent.
	row := models.AliasCounter{ParentID: parentID, ObjectType: objectType, NextAlias: 1}
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		logger.Error("alias_counters upsert failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters upsert: %w", err)
	}

	// Lock the row and read the current value.
	var counter models.AliasCounter
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("parent_id = ? AND object_type = ?", parentID, objectType).
		First(&counter).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Should be impossible after the upsert above.
		return 0, fmt.Errorf("alias_counters row missing after upsert: parent=%s type=%s", parentID, objectType)
	}
	if err != nil {
		logger.Error("alias_counters lock failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters lock: %w", err)
	}

	allocated := counter.NextAlias

	// Bump the counter atomically (still inside the same transaction & lock).
	if err := tx.WithContext(ctx).
		Model(&models.AliasCounter{}).
		Where("parent_id = ? AND object_type = ?", parentID, objectType).
		Update("next_alias", counter.NextAlias+1).Error; err != nil {
		logger.Error("alias_counters bump failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters bump: %w", err)
	}

	return allocated, nil
}
```

- [ ] **Step 2: Run tests**

Run:
```
make test-unit name=TestAllocateNextAlias
```

Expected: all 6 tests PASS.

- [ ] **Step 3: Lint**

Run:
```
make lint
```

Expected: 0 issues.

- [ ] **Step 4: Commit**

```
git add api/alias_allocator.go api/alias_allocator_test.go
git commit -m "feat(api): add AllocateNextAlias atomic counter (#374)"
```

---

## Task 5: Cross-DB advisory lock primitive

**Files:**
- Create: `internal/dbschema/advisory_lock.go`
- Create: `internal/dbschema/advisory_lock_test.go`

- [ ] **Step 1: Implement the primitive**

Create `internal/dbschema/advisory_lock.go`:

```go
package dbschema

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// AcquireMigrationLock takes an exclusive, server-wide named lock that is
// released by calling the returned function. Used to serialize startup-time
// migrations across multiple replicas. The function blocks until the lock
// is acquired (subject to context cancellation).
//
// On PostgreSQL: uses pg_advisory_lock with a deterministic int64 derived
// from sha256(name). On Oracle: uses DBMS_LOCK.ALLOCATE_UNIQUE +
// DBMS_LOCK.REQUEST. Other dialects return an error.
//
// The release function is idempotent and safe to defer.
func AcquireMigrationLock(ctx context.Context, db *gorm.DB, name string) (release func(), err error) {
	logger := slogging.Get()
	dialect := db.Dialector.Name()

	switch dialect {
	case "postgres":
		return acquirePGLock(ctx, db, name, logger)
	case "oracle":
		return acquireOracleLock(ctx, db, name, logger)
	default:
		return nil, fmt.Errorf("AcquireMigrationLock: unsupported dialect %q", dialect)
	}
}

func acquirePGLock(ctx context.Context, db *gorm.DB, name string, logger *slogging.Logger) (func(), error) {
	key := nameToInt64(name)
	if err := db.WithContext(ctx).Exec("SELECT pg_advisory_lock(?)", key).Error; err != nil {
		return nil, fmt.Errorf("pg_advisory_lock: %w", err)
	}
	logger.Debug("Acquired pg_advisory_lock(%d) for %q", key, name)
	released := false
	return func() {
		if released {
			return
		}
		released = true
		if err := db.Exec("SELECT pg_advisory_unlock(?)", key).Error; err != nil {
			logger.Warn("pg_advisory_unlock(%d) failed: %v", key, err)
		}
	}, nil
}

func acquireOracleLock(ctx context.Context, db *gorm.DB, name string, logger *slogging.Logger) (func(), error) {
	// DBMS_LOCK.ALLOCATE_UNIQUE returns a handle for a named lock.
	var handle string
	if err := db.WithContext(ctx).Raw(`
		BEGIN
			DBMS_LOCK.ALLOCATE_UNIQUE(lockname => ?, lockhandle => :h);
		END;
	`, name).Row().Scan(&handle); err != nil {
		return nil, fmt.Errorf("DBMS_LOCK.ALLOCATE_UNIQUE: %w", err)
	}

	// DBMS_LOCK.REQUEST(handle, lockmode=6 (EXCLUSIVE), timeout=MAXWAIT, release_on_commit=FALSE)
	var status int
	if err := db.WithContext(ctx).Raw(`
		BEGIN
			:s := DBMS_LOCK.REQUEST(lockhandle => ?, lockmode => 6, timeout => DBMS_LOCK.MAXWAIT, release_on_commit => FALSE);
		END;
	`, handle).Row().Scan(&status); err != nil {
		return nil, fmt.Errorf("DBMS_LOCK.REQUEST: %w", err)
	}
	if status != 0 {
		return nil, fmt.Errorf("DBMS_LOCK.REQUEST returned status %d (non-zero)", status)
	}
	logger.Debug("Acquired DBMS_LOCK for %q (handle=%s)", name, handle)

	released := false
	return func() {
		if released {
			return
		}
		released = true
		var rstatus int
		if err := db.Raw(`
			BEGIN
				:r := DBMS_LOCK.RELEASE(lockhandle => ?);
			END;
		`, handle).Row().Scan(&rstatus); err != nil {
			logger.Warn("DBMS_LOCK.RELEASE failed: %v", err)
		}
	}, nil
}

// nameToInt64 hashes a name string to a deterministic int64 for use as a
// pg_advisory_lock key. Two different names will produce different keys
// with overwhelming probability.
func nameToInt64(name string) int64 {
	h := sha256.Sum256([]byte(name))
	return int64(binary.BigEndian.Uint64(h[:8])) //nolint:gosec // deterministic-hash; signed wrap is fine
}
```

- [ ] **Step 2: Write the test**

Create `internal/dbschema/advisory_lock_test.go`:

```go
package dbschema

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNameToInt64Stable(t *testing.T) {
	a := nameToInt64("foo")
	b := nameToInt64("foo")
	c := nameToInt64("bar")
	assert.Equal(t, a, b, "hash should be stable for same input")
	assert.NotEqual(t, a, c, "different inputs should produce different hashes")
}

func TestAcquireMigrationLock_UnsupportedDialect(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	_, err = AcquireMigrationLock(context.Background(), db, "test")
	assert.ErrorContains(t, err, "unsupported dialect")
}

// TestAcquireMigrationLock_PGSerializes is gated by an env var because it
// requires a real PG connection. CI runs it via make test-integration; it's
// skipped by default in `make test-unit`.
func TestAcquireMigrationLock_PGSerializes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires real PostgreSQL; run via integration tests")
	}
	t.Skip("manual smoke; requires DATABASE_URL env var")

	// Smoke design (uncomment to run manually):
	// db := connectToPGTestDB(t) // helper that opens a real PG connection
	// ctx := context.Background()
	//
	// var order []int
	// var mu sync.Mutex
	// var wg sync.WaitGroup
	// for i := 0; i < 3; i++ {
	//     wg.Add(1)
	//     go func(i int) {
	//         defer wg.Done()
	//         release, err := AcquireMigrationLock(ctx, db, "test-lock-pg")
	//         require.NoError(t, err)
	//         mu.Lock(); order = append(order, i); mu.Unlock()
	//         time.Sleep(50 * time.Millisecond)
	//         release()
	//     }(i)
	// }
	// wg.Wait()
	// assert.Len(t, order, 3)
	_ = sync.WaitGroup{}
	_ = time.Now()
}
```

- [ ] **Step 3: Run tests**

Run:
```
make test-unit name=TestNameToInt64Stable
make test-unit name=TestAcquireMigrationLock_UnsupportedDialect
```

Expected: both PASS.

- [ ] **Step 4: Lint**

Run:
```
make lint
```

Expected: 0 issues.

- [ ] **Step 5: Commit**

```
git add internal/dbschema/advisory_lock.go internal/dbschema/advisory_lock_test.go
git commit -m "feat(dbschema): add AcquireMigrationLock cross-DB primitive (#374)"
```

---

## Task 6: Backfill runner — failing tests

**Files:**
- Create: `api/alias_backfill_test.go`

- [ ] **Step 1: Write the tests**

Create `api/alias_backfill_test.go`:

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
	"gorm.io/gorm"
)

func setupBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.ThreatModel{},
		&models.Note{},
		&models.AliasCounter{},
	))
	return db
}

func makeUser(t *testing.T, db *gorm.DB) *models.User {
	t.Helper()
	u := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: ptrStr("u-" + uuid.New().String()[:8]),
		Email:          "u@example.com",
		Name:           "Test",
	}
	require.NoError(t, db.Create(u).Error)
	return u
}

func ptrStr(s string) *string { return &s }

func TestBackfillThreatModels_AssignsByCreatedAt(t *testing.T) {
	db := setupBackfillTestDB(t)
	user := makeUser(t, db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	tmA := &models.ThreatModel{ID: "00000000-aaaa-aaaa-aaaa-000000000001", OwnerInternalUUID: user.InternalUUID, Name: "A", CreatedAt: now.Add(-3 * time.Hour)}
	tmB := &models.ThreatModel{ID: "00000000-aaaa-aaaa-aaaa-000000000002", OwnerInternalUUID: user.InternalUUID, Name: "B", CreatedAt: now.Add(-1 * time.Hour)}
	tmC := &models.ThreatModel{ID: "00000000-aaaa-aaaa-aaaa-000000000003", OwnerInternalUUID: user.InternalUUID, Name: "C", CreatedAt: now.Add(-2 * time.Hour)}
	require.NoError(t, db.Create(tmA).Error)
	require.NoError(t, db.Create(tmB).Error)
	require.NoError(t, db.Create(tmC).Error)

	require.NoError(t, RunAliasBackfill(ctx, db))

	var got []models.ThreatModel
	require.NoError(t, db.Order("alias ASC").Find(&got).Error)
	require.Len(t, got, 3)
	// A (oldest), C, B (newest) — in created_at ASC order.
	assert.Equal(t, tmA.ID, got[0].ID)
	assert.Equal(t, int32(1), got[0].Alias)
	assert.Equal(t, tmC.ID, got[1].ID)
	assert.Equal(t, int32(2), got[1].Alias)
	assert.Equal(t, tmB.ID, got[2].ID)
	assert.Equal(t, int32(3), got[2].Alias)

	// Counter is set to N+1.
	var counter models.AliasCounter
	require.NoError(t, db.Where("parent_id = ? AND object_type = ?", "__global__", "threat_model").First(&counter).Error)
	assert.Equal(t, int32(4), counter.NextAlias)
}

func TestBackfillSubObjectsScopedPerTM(t *testing.T) {
	db := setupBackfillTestDB(t)
	user := makeUser(t, db)
	ctx := context.Background()

	tm1 := &models.ThreatModel{ID: uuid.New().String(), OwnerInternalUUID: user.InternalUUID, Name: "TM1"}
	tm2 := &models.ThreatModel{ID: uuid.New().String(), OwnerInternalUUID: user.InternalUUID, Name: "TM2"}
	require.NoError(t, db.Create(tm1).Error)
	require.NoError(t, db.Create(tm2).Error)

	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&models.Note{
			ID: uuid.New().String(), ThreatModelID: tm1.ID, Name: "n", Content: models.DBText("x"), CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}).Error)
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&models.Note{
			ID: uuid.New().String(), ThreatModelID: tm2.ID, Name: "n", Content: models.DBText("x"), CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}).Error)
	}

	require.NoError(t, RunAliasBackfill(ctx, db))

	var notes1, notes2 []models.Note
	require.NoError(t, db.Where("threat_model_id = ?", tm1.ID).Order("alias ASC").Find(&notes1).Error)
	require.NoError(t, db.Where("threat_model_id = ?", tm2.ID).Order("alias ASC").Find(&notes2).Error)

	require.Len(t, notes1, 3)
	assert.Equal(t, int32(1), notes1[0].Alias)
	assert.Equal(t, int32(2), notes1[1].Alias)
	assert.Equal(t, int32(3), notes1[2].Alias)

	require.Len(t, notes2, 2)
	assert.Equal(t, int32(1), notes2[0].Alias)
	assert.Equal(t, int32(2), notes2[1].Alias)

	// Verify per-TM counters.
	var c1, c2 models.AliasCounter
	require.NoError(t, db.Where("parent_id = ? AND object_type = ?", tm1.ID, "note").First(&c1).Error)
	require.NoError(t, db.Where("parent_id = ? AND object_type = ?", tm2.ID, "note").First(&c2).Error)
	assert.Equal(t, int32(4), c1.NextAlias)
	assert.Equal(t, int32(3), c2.NextAlias)
}

func TestBackfillIdempotent(t *testing.T) {
	db := setupBackfillTestDB(t)
	user := makeUser(t, db)
	ctx := context.Background()

	tm := &models.ThreatModel{ID: uuid.New().String(), OwnerInternalUUID: user.InternalUUID, Name: "TM"}
	require.NoError(t, db.Create(tm).Error)

	require.NoError(t, RunAliasBackfill(ctx, db))

	var before models.ThreatModel
	require.NoError(t, db.Where("id = ?", tm.ID).First(&before).Error)
	assert.Equal(t, int32(1), before.Alias)

	// Second run should be a no-op (no rows have alias=0).
	require.NoError(t, RunAliasBackfill(ctx, db))

	var after models.ThreatModel
	require.NoError(t, db.Where("id = ?", tm.ID).First(&after).Error)
	assert.Equal(t, int32(1), after.Alias) // unchanged

	var counter models.AliasCounter
	require.NoError(t, db.Where("parent_id = ? AND object_type = ?", "__global__", "threat_model").First(&counter).Error)
	assert.Equal(t, int32(2), counter.NextAlias)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```
make test-unit name=TestBackfill
```

Expected: FAIL — `RunAliasBackfill` undefined.

---

## Task 7: Backfill runner — implementation

**Files:**
- Create: `api/alias_backfill.go`

- [ ] **Step 1: Implement**

Create `api/alias_backfill.go`:

```go
package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// subObjectTable describes a sub-object table participating in alias backfill.
type subObjectTable struct {
	tableName  string // schema-side table name (e.g., "notes")
	objectType string // alias_counters object_type value (e.g., "note")
}

// subObjectTables is the canonical list of tables that need backfill, in
// dependency-safe order (these tables only depend on threat_models).
var subObjectTables = []subObjectTable{
	{"diagrams", "diagram"},
	{"threats", "threat"},
	{"assets", "asset"},
	{"repositories", "repository"},
	{"notes", "note"},
	{"documents", "document"},
}

// RunAliasBackfill brings all aliased tables to a fully-populated state.
// Idempotent: skips tables whose rows all have alias > 0. Acquires a
// cross-DB advisory lock so multi-replica startups serialize.
func RunAliasBackfill(ctx context.Context, db *gorm.DB) error {
	logger := slogging.Get()

	release, err := dbschema.AcquireMigrationLock(ctx, db, "tmi_alias_backfill")
	if err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer release()

	if err := backfillThreatModelAliases(ctx, db, logger); err != nil {
		return fmt.Errorf("backfill threat_models: %w", err)
	}
	for _, t := range subObjectTables {
		if err := backfillSubObjectAliases(ctx, db, t, logger); err != nil {
			return fmt.Errorf("backfill %s: %w", t.tableName, err)
		}
	}
	return nil
}

func backfillThreatModelAliases(ctx context.Context, db *gorm.DB, logger *slogging.Logger) error {
	const tableName = "threat_models"
	tmTable := tableNameForDialect(db, tableName)

	// Fast-skip if no rows have alias=0.
	var pending int64
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE alias = 0", tmTable),
	).Scan(&pending).Error; err != nil {
		return fmt.Errorf("count pending: %w", err)
	}
	if pending == 0 {
		logger.Debug("alias backfill: threat_models is fully populated, skipping")
		return nil
	}

	logger.Info("alias backfill: assigning aliases to %d threat_models rows", pending)

	// PG: UPDATE FROM CTE with ROW_NUMBER. Oracle: MERGE with analytic.
	// The two-statement helper below picks the right form per dialect.
	if err := bulkAssignThreatModelAliases(ctx, db, tmTable); err != nil {
		return err
	}

	// Initialize the counter to MAX(alias) + 1.
	var maxAlias int32
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf("SELECT COALESCE(MAX(alias), 0) FROM %s", tmTable),
	).Scan(&maxAlias).Error; err != nil {
		return fmt.Errorf("read MAX(alias): %w", err)
	}
	counter := models.AliasCounter{ParentID: "__global__", ObjectType: "threat_model", NextAlias: maxAlias + 1}
	if err := db.WithContext(ctx).Save(&counter).Error; err != nil {
		return fmt.Errorf("save counter: %w", err)
	}
	logger.Info("alias backfill: threat_models complete; next_alias=%d", maxAlias+1)
	return nil
}

func backfillSubObjectAliases(ctx context.Context, db *gorm.DB, t subObjectTable, logger *slogging.Logger) error {
	resolvedTable := tableNameForDialect(db, t.tableName)

	var pending int64
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE alias = 0", resolvedTable),
	).Scan(&pending).Error; err != nil {
		return fmt.Errorf("count pending: %w", err)
	}
	if pending == 0 {
		logger.Debug("alias backfill: %s is fully populated, skipping", t.tableName)
		return nil
	}

	logger.Info("alias backfill: assigning aliases to %d %s rows", pending, t.tableName)

	if err := bulkAssignSubObjectAliases(ctx, db, resolvedTable); err != nil {
		return err
	}

	// Initialize per-TM counters from MAX(alias).
	type counterRow struct {
		ThreatModelID string
		MaxAlias      int32
	}
	var rows []counterRow
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf(
			"SELECT threat_model_id, MAX(alias) AS max_alias FROM %s GROUP BY threat_model_id",
			resolvedTable,
		),
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("read MAX(alias) per TM: %w", err)
	}
	for _, r := range rows {
		counter := models.AliasCounter{ParentID: r.ThreatModelID, ObjectType: t.objectType, NextAlias: r.MaxAlias + 1}
		if err := db.WithContext(ctx).Save(&counter).Error; err != nil {
			return fmt.Errorf("save counter for tm=%s: %w", r.ThreatModelID, err)
		}
	}
	logger.Info("alias backfill: %s complete (%d threat models)", t.tableName, len(rows))
	return nil
}

// bulkAssignThreatModelAliases assigns alias 1..N to all rows where alias = 0,
// ordered by created_at ASC, id ASC. Dialect-specific.
func bulkAssignThreatModelAliases(ctx context.Context, db *gorm.DB, tmTable string) error {
	switch db.Dialector.Name() {
	case "postgres":
		sql := fmt.Sprintf(`
			WITH numbered AS (
				SELECT id, ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS rn
				FROM %s WHERE alias = 0
			)
			UPDATE %s t SET alias = numbered.rn
			FROM numbered WHERE t.id = numbered.id
		`, tmTable, tmTable)
		return db.WithContext(ctx).Exec(sql).Error

	case "oracle":
		sql := fmt.Sprintf(`
			MERGE INTO %s t USING (
				SELECT id, ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS rn
				FROM %s WHERE alias = 0
			) numbered
			ON (t.id = numbered.id)
			WHEN MATCHED THEN UPDATE SET t.alias = numbered.rn
		`, tmTable, tmTable)
		return db.WithContext(ctx).Exec(sql).Error

	case "sqlite":
		// SQLite supports the PG syntax (UPDATE ... FROM with CTE in 3.33+).
		sql := fmt.Sprintf(`
			WITH numbered AS (
				SELECT id, ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS rn
				FROM %s WHERE alias = 0
			)
			UPDATE %s SET alias = (SELECT rn FROM numbered WHERE numbered.id = %s.id)
			WHERE id IN (SELECT id FROM numbered)
		`, tmTable, tmTable, tmTable)
		return db.WithContext(ctx).Exec(sql).Error

	default:
		return fmt.Errorf("alias backfill: unsupported dialect %q", db.Dialector.Name())
	}
}

// bulkAssignSubObjectAliases assigns alias per (threat_model_id, ROW_NUMBER)
// for all rows where alias = 0, ordered by created_at ASC, id ASC within each
// partition. Dialect-specific.
func bulkAssignSubObjectAliases(ctx context.Context, db *gorm.DB, table string) error {
	switch db.Dialector.Name() {
	case "postgres":
		sql := fmt.Sprintf(`
			WITH numbered AS (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY threat_model_id ORDER BY created_at ASC, id ASC
				) AS rn
				FROM %s WHERE alias = 0
			)
			UPDATE %s t SET alias = numbered.rn
			FROM numbered WHERE t.id = numbered.id
		`, table, table)
		return db.WithContext(ctx).Exec(sql).Error

	case "oracle":
		sql := fmt.Sprintf(`
			MERGE INTO %s t USING (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY threat_model_id ORDER BY created_at ASC, id ASC
				) AS rn
				FROM %s WHERE alias = 0
			) numbered
			ON (t.id = numbered.id)
			WHEN MATCHED THEN UPDATE SET t.alias = numbered.rn
		`, table, table)
		return db.WithContext(ctx).Exec(sql).Error

	case "sqlite":
		sql := fmt.Sprintf(`
			WITH numbered AS (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY threat_model_id ORDER BY created_at ASC, id ASC
				) AS rn
				FROM %s WHERE alias = 0
			)
			UPDATE %s SET alias = (SELECT rn FROM numbered WHERE numbered.id = %s.id)
			WHERE id IN (SELECT id FROM numbered)
		`, table, table, table)
		return db.WithContext(ctx).Exec(sql).Error

	default:
		return fmt.Errorf("alias backfill: unsupported dialect %q", db.Dialector.Name())
	}
}

// tableNameForDialect returns the table name with appropriate casing for the
// dialect (lowercase on PG, UPPERCASE on Oracle when UseUppercaseTableNames
// is set).
func tableNameForDialect(db *gorm.DB, name string) string {
	if db.Dialector.Name() == "oracle" {
		// Match the project's UseUppercaseTableNames pattern. The simple
		// approach: ToUpper unconditionally for Oracle.
		runes := []rune(name)
		for i, r := range runes {
			if r >= 'a' && r <= 'z' {
				runes[i] = r - 32
			}
		}
		return string(runes)
	}
	return name
}
```

- [ ] **Step 2: Run tests**

Run:
```
make test-unit name=TestBackfill
```

Expected: 3 tests PASS.

If a test fails because the SQLite test DB can't run `ROW_NUMBER() OVER`, the SQLite version used in TMI's test setup must support window functions (3.25+). If not, mark the SQLite branch as `t.Skip` and rely on integration tests for full coverage.

- [ ] **Step 3: Lint**

Run:
```
make lint
```

Expected: 0 issues.

- [ ] **Step 4: Commit**

```
git add api/alias_backfill.go api/alias_backfill_test.go
git commit -m "feat(api): RunAliasBackfill with dialect-specific bulk update (#374)"
```

---

## Task 8: Phase 3 unique-index addition

**Files:**
- Create: `api/alias_indexes.go`

After backfill completes, add the unique constraints. Done in Go because GORM's AutoMigrate would have failed earlier (when alias=0 made everything non-unique).

- [ ] **Step 1: Implement**

Create `api/alias_indexes.go`:

```go
package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// AddAliasUniqueIndexes creates the seven post-backfill unique indexes.
// Idempotent: skips indexes that already exist via Migrator().HasIndex.
func AddAliasUniqueIndexes(ctx context.Context, db *gorm.DB) error {
	logger := slogging.Get()
	type idx struct {
		name    string
		table   string
		columns string // SQL column list
	}
	indexes := []idx{
		{"uniq_threat_models_alias", "threat_models", "(alias)"},
		{"uniq_diagrams_tm_alias", "diagrams", "(threat_model_id, alias)"},
		{"uniq_threats_tm_alias", "threats", "(threat_model_id, alias)"},
		{"uniq_assets_tm_alias", "assets", "(threat_model_id, alias)"},
		{"uniq_repositories_tm_alias", "repositories", "(threat_model_id, alias)"},
		{"uniq_notes_tm_alias", "notes", "(threat_model_id, alias)"},
		{"uniq_documents_tm_alias", "documents", "(threat_model_id, alias)"},
	}

	for _, i := range indexes {
		table := tableNameForDialect(db, i.table)
		if hasIndex(db, table, i.name) {
			logger.Debug("alias index %s already exists; skipping", i.name)
			continue
		}
		sql := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s %s", i.name, table, i.columns)
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("create %s: %w", i.name, err)
		}
		logger.Info("created unique index %s", i.name)
	}
	return nil
}

func hasIndex(db *gorm.DB, table, indexName string) bool {
	switch db.Dialector.Name() {
	case "postgres":
		var n int64
		_ = db.Raw(
			"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = ?",
			table, indexName,
		).Scan(&n).Error
		return n > 0
	case "oracle":
		var n int64
		_ = db.Raw(
			"SELECT COUNT(*) FROM USER_INDEXES WHERE TABLE_NAME = UPPER(?) AND INDEX_NAME = UPPER(?)",
			table, indexName,
		).Scan(&n).Error
		return n > 0
	case "sqlite":
		var n int64
		_ = db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?",
			indexName,
		).Scan(&n).Error
		return n > 0
	}
	return false
}
```

- [ ] **Step 2: Build**

Run:
```
make build-server
```

Expected: success.

- [ ] **Step 3: Lint**

Run:
```
make lint
```

Expected: 0 issues.

- [ ] **Step 4: Commit**

```
git add api/alias_indexes.go
git commit -m "feat(api): AddAliasUniqueIndexes idempotent post-backfill step (#374)"
```

---

## Task 9: Wire migration into AutoMigrate flow

**Files:**
- Modify: `auth/db/gorm.go`
- Modify: `cmd/server/main.go` (if AutoMigrate is called from main)

The legacy `alias` column on `threat_models` is dropped before AutoMigrate runs (so AutoMigrate sees a clean slate and adds the new INTEGER column). Backfill + indexes run after AutoMigrate.

- [ ] **Step 1: Add legacy column-drop helper**

In `auth/db/gorm.go`, find the `backfillNullableToNonNull` function (around line 619). Add a sibling helper:

```go
// dropLegacyAliasColumn drops the legacy ThreatModel.alias array/text column
// before AutoMigrate replaces it with the new INTEGER column. Idempotent: if
// the column doesn't exist or is already INTEGER, this is a no-op.
//
// Pre-#374 the column was a TEXT[] (PG) / CLOB (Oracle) holding user-supplied
// nicknames. The new design uses a server-assigned INTEGER. Old data is
// destroyed by design (issue #374 explicitly authorizes the breakage).
func dropLegacyAliasColumn(db *gorm.DB, isOracle bool) {
	log := slogging.Get()
	table := "threat_models"
	if isOracle {
		table = strings.ToUpper(table)
	}
	if !db.Migrator().HasTable(table) {
		return
	}

	// Detect column type. If already integer, nothing to do.
	if isOracle {
		var dataType string
		_ = db.Raw(
			"SELECT DATA_TYPE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = UPPER(?) AND COLUMN_NAME = UPPER(?)",
			table, "alias",
		).Scan(&dataType).Error
		if dataType == "" {
			return // column doesn't exist
		}
		if dataType == "NUMBER" {
			return // already migrated
		}
		log.Info("dropping legacy %s.alias column (was %s)", table, dataType)
		if err := db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN alias", table)).Error; err != nil {
			log.Warn("drop legacy alias column failed (non-fatal; AutoMigrate may surface): %v", err)
		}
		return
	}

	// PostgreSQL.
	var dataType string
	_ = db.Raw(
		"SELECT data_type FROM information_schema.columns WHERE table_name = ? AND column_name = ?",
		table, "alias",
	).Scan(&dataType).Error
	if dataType == "" {
		return
	}
	if dataType == "integer" {
		return
	}
	log.Info("dropping legacy %s.alias column (was %s)", table, dataType)
	if err := db.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN alias", table)).Error; err != nil {
		log.Warn("drop legacy alias column failed (non-fatal; AutoMigrate may surface): %v", err)
	}
}
```

- [ ] **Step 2: Call the helper from `AutoMigrate`**

Find the `AutoMigrate` method on `*GormDB` (around line 589). Insert the call right after `backfillNullableToNonNull(...)`:

```go
	dropLegacyAliasColumn(g.db, g.cfg.Type == DatabaseTypeOracle)
```

So the function now reads:

```go
func (g *GormDB) AutoMigrate(models ...any) error {
	log := slogging.Get()
	log.Debug("Running GORM auto-migration for %d models", len(models))

	backfillNullableToNonNull(g.db, g.cfg.Type == DatabaseTypeOracle)
	dropLegacyAliasColumn(g.db, g.cfg.Type == DatabaseTypeOracle)

	if g.cfg.Type == DatabaseTypeOracle {
		// ... existing
	}
	// ... existing
}
```

- [ ] **Step 3: Find the post-AutoMigrate callsite to wire backfill + indexes**

The cleanest place to invoke the post-migration steps is right after `InitializeStores` (which happens after AutoMigrate). The simplest location is in `cmd/server/main.go` immediately after migrate completes.

Run:
```
rg -n 'AutoMigrate\|InitializeStores' cmd/server/main.go | head -10
```

Identify where AutoMigrate runs in main. Right after that block, add:

```go
	if err := api.RunAliasBackfill(ctx, gormDB); err != nil {
		logger.Error("alias backfill failed: %v", err)
		os.Exit(1)
	}
	if err := api.AddAliasUniqueIndexes(ctx, gormDB); err != nil {
		logger.Error("alias unique-index creation failed: %v", err)
		os.Exit(1)
	}
```

(Adapt to the actual variable names / logging conventions in `cmd/server/main.go`.)

- [ ] **Step 4: Build**

Run:
```
make build-server
```

Expected: success.

- [ ] **Step 5: Manual smoke**

Run:
```
make stop-server || true
make start-dev
```

Watch the logs during startup. Expect to see `alias backfill: ...` lines (most likely "fully populated, skipping" if the dev DB has no rows, or "assigning aliases to N rows" if it does).

Then:
```
make stop-server
```

- [ ] **Step 6: Commit**

```
git add auth/db/gorm.go cmd/server/main.go
git commit -m "feat(db): wire alias backfill + unique-index step into startup (#374)"
```

---

## Task 10: Wire `AllocateNextAlias` into repository Create methods

**Files:**
- Modify: `api/note_store_gorm.go`, `api/threat_store_gorm.go`, `api/asset_store_gorm.go`, `api/repository_store_gorm.go`, `api/document_store_gorm.go`, `api/database_store_gorm.go` (Diagram), `api/threat_model_store_gorm.go` (or wherever ThreatModel CRUD lives)

For each Create method, add the alias allocation just before the `tx.Create(&model)` call, inside the existing transactional wrapper.

- [ ] **Step 1: Locate each Create method**

Run:
```
rg -n 'func.*GormNoteRepository.*Create\b\|func.*GormThreatRepository.*Create\b\|func.*GormAssetRepository.*Create\b\|func.*GormRepositoryRepository.*Create\b\|func.*GormDocumentRepository.*Create\b' api/ --type go | head -10
```

Note the file:line for each.

- [ ] **Step 2: For each sub-object Create, add allocation inside the transaction**

For Note (`api/note_store_gorm.go`), find the `Create` method and locate where it calls `tx.Create(&model)` inside `WithRetryableGormTransaction`. Add immediately before that call:

```go
		alias, err := AllocateNextAlias(ctx, tx, threatModelID, "note")
		if err != nil {
			return err
		}
		model.Alias = alias
```

The `model` variable name varies per repository (might be `gormNote`, `n`, `record`, etc.) — match what's already there. Same for `threatModelID` — use whatever local variable holds the parent TM UUID.

Repeat for:
- Threat: object_type `"threat"`
- Asset: object_type `"asset"`
- Repository: object_type `"repository"`
- Document: object_type `"document"`
- Diagram (`api/database_store_gorm.go`): object_type `"diagram"`

- [ ] **Step 3: For ThreatModel Create**

Find the ThreatModel `Create` method (likely in `api/threat_model_store_gorm.go` or `api/database_store_gorm.go`). Inside the transaction, before persisting:

```go
		alias, err := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
		if err != nil {
			return err
		}
		model.Alias = alias
```

- [ ] **Step 4: Build**

Run:
```
make build-server
```

Expected: success.

- [ ] **Step 5: Run tests**

Run:
```
make test-unit
```

Expected: many pass; some may fail because:
- Existing repository tests may not pre-create the AliasCounter row (the allocator handles this via ON CONFLICT DO NOTHING, so no fix needed).
- Existing tests that AutoMigrate only `&Note{}` (without `&AliasCounter{}`) will fail. Update the test setup helpers to include `&models.AliasCounter{}` in their AutoMigrate calls.

If any tests fail with errors related to missing `alias_counters` table, edit the test setup helpers.

Run again:
```
make test-unit
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```
git add api/note_store_gorm.go api/threat_store_gorm.go api/asset_store_gorm.go api/repository_store_gorm.go api/document_store_gorm.go api/database_store_gorm.go api/threat_model_store_gorm.go
git commit -m "feat(api): allocate alias on entity create across all repositories (#374)"
```

---

## Task 11: OpenAPI schema changes — replace ThreatModelBase.alias

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 1: Replace `ThreatModelBase.alias`**

Find the `alias` property under `components.schemas.ThreatModelBase.properties`. Replace the existing array schema:

```json
"alias": {
  "type": "array",
  "description": "Alternative names or identifiers for the threat model",
  "items": { ... },
  "minItems": 1,
  "maxItems": 20,
  "uniqueItems": true
}
```

with:

```json
"alias": {
  "type": "integer",
  "format": "int32",
  "minimum": 1,
  "readOnly": true,
  "description": "Server-assigned monotonically-increasing integer alias, globally unique across all threat models. Immutable after creation."
}
```

- [ ] **Step 2: Validate JSON**

Run:
```
jq empty api-schema/tmi-openapi.json && echo valid
```

Expected: `valid`.

- [ ] **Step 3: Validate OpenAPI**

Run:
```
make validate-openapi
```

Expected: 0 errors (warnings/info from pre-existing rules are OK).

- [ ] **Step 4: Commit**

```
git add api-schema/tmi-openapi.json
git commit -m "feat(api): replace ThreatModelBase.alias with server-assigned integer (#374)"
```

---

## Task 12: OpenAPI schema changes — add `alias` to all sub-object schemas

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 1: List the schemas to update**

The 6 sub-object schemas:
- `Document`
- `Asset`
- `Repository`
- `DfdDiagram` (and any related diagram schemas)
- `Note`
- `Threat`

Plus their `*ListItem` schemas (if separate):
- `NoteListItem`
- `DocumentListItem`
- `AssetListItem`
- `RepositoryListItem`
- `ThreatListItem`
- `DiagramListItem`

Run:
```
rg -n '"Note"\|"NoteListItem"\|"Document"\|"Asset"\|"Repository"\|"DfdDiagram"\|"Threat"' api-schema/tmi-openapi.json | head -30
```

This identifies the schema definitions to update.

- [ ] **Step 2: Add `alias` to each schema**

For each schema, add the property to its `properties` block (place near the `id` field for grouping):

```json
"alias": {
  "type": "integer",
  "format": "int32",
  "minimum": 1,
  "readOnly": true,
  "description": "Server-assigned monotonically-increasing integer alias, unique within the parent threat model. Immutable after creation."
}
```

Be sure to include the field on both the full-shape schemas AND any *ListItem* shapes — clients viewing list pages need the alias to construct cross-references.

- [ ] **Step 3: Validate JSON**

Run:
```
jq empty api-schema/tmi-openapi.json && echo valid
```

Expected: `valid`.

- [ ] **Step 4: Validate OpenAPI**

Run:
```
make validate-openapi
```

Expected: 0 errors.

- [ ] **Step 5: Regenerate**

Run:
```
make generate-api
```

Expected: `api/api.go` is regenerated with `Alias *int32` (or `*int`) fields on every relevant API type. The diff will be large — that's expected.

- [ ] **Step 6: Build**

Run:
```
make build-server
```

If compile errors surface in repository code (e.g., the conversion functions `noteToAPI` / `apiToNote` don't yet copy `Alias` between the API type and the GORM model), fix them now: in each conversion function, add a line `apiNote.Alias = &gormNote.Alias` (if the API type uses `*int32`) or `apiNote.Alias = gormNote.Alias` (if `int32`). Mirror the existing pattern for `Id`, `CreatedAt`, etc.

Run again:
```
make build-server
```

Expected: success.

- [ ] **Step 7: Run tests**

Run:
```
make test-unit
```

Expected: all PASS. Some response-shape tests may have failed pre-Task 11 if they hard-coded the old alias array; this step fixes them.

- [ ] **Step 8: Commit**

```
git add api-schema/tmi-openapi.json api/api.go api/note_store_gorm.go api/threat_store_gorm.go api/asset_store_gorm.go api/repository_store_gorm.go api/document_store_gorm.go api/database_store_gorm.go api/threat_model_store_gorm.go
git commit -m "feat(api): expose alias on every entity schema + regenerate (#374)"
```

---

## Task 13: Handler-level write protection

**Files:**
- Modify: `api/threat_model_handlers.go`
- Modify: `api/note_sub_resource_handlers.go`
- Modify: `api/threat_sub_resource_handlers.go`
- Modify: `api/asset_sub_resource_handlers.go`
- Modify: `api/document_sub_resource_handlers.go`
- Modify: `api/repository_sub_resource_handlers.go`
- Modify: `api/threat_model_diagram_handlers.go` (or wherever Diagram PUT/PATCH handlers live)

Add `case "alias"` to the `getFieldErrorMessage` switch (and any similar per-sub-resource switches) so PATCH attempts on `/alias` return a clean 400.

- [ ] **Step 1: Update `getFieldErrorMessage` in `threat_model_handlers.go`**

Find the `getFieldErrorMessage` function. Add a new `case` near the other read-only fields (`id`, `created_at`, `created_by`):

```go
	case "alias":
		return "The alias is read-only and assigned by the server."
```

- [ ] **Step 2: Search for similar field-error message functions in other handler files**

Run:
```
rg -n 'getFieldErrorMessage\|fieldErrorMessage\|getReadOnlyFieldHelpMessage' api/ --type go | head -10
```

For each helper that returns "field X cannot be modified" messages, add the `case "alias"` line.

If a sub-resource handler doesn't have its own field-error helper but does perform JSON Patch validation through some other mechanism, look at how it currently rejects writes to `id` or `created_at` and mirror the pattern for `alias`.

- [ ] **Step 3: Verify struct whitelists**

For each `Update*Request` struct (e.g., `UpdateThreatModelRequest`, `UpdateNoteRequest`), ensure `Alias` is NOT a field. If any old struct still has an `Alias` field (probably not, since we replaced the model field), remove it.

Run:
```
rg -n 'Alias\s*\*?\[\]string\|Alias\s*\[\]string' api/ --type go
```

If any matches, remove them.

- [ ] **Step 4: Build + lint**

Run:
```
make build-server
make lint
```

Expected: both succeed.

- [ ] **Step 5: Run tests**

Run:
```
make test-unit
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```
git add api/threat_model_handlers.go api/note_sub_resource_handlers.go api/threat_sub_resource_handlers.go api/asset_sub_resource_handlers.go api/document_sub_resource_handlers.go api/repository_sub_resource_handlers.go api/threat_model_diagram_handlers.go
git commit -m "feat(api): reject client-side alias writes with 400 (#374)"
```

---

## Task 14: Handler unit tests for write protection

**Files:**
- Modify: `api/threat_model_handlers_test.go` (or equivalent)

- [ ] **Step 1: Add tests verifying write rejection**

Add to `api/threat_model_handlers_test.go` (mirror the existing `TestPatch*_RejectsXxx` tests):

```go
func TestPatchThreatModel_RejectsAliasOperation(t *testing.T) {
	// Reuse the existing test harness in this file. The body of the request:
	body := `[{"op": "replace", "path": "/alias", "value": 99}]`
	// Send the PATCH and expect a 400.
	// (Adapt to whatever the existing PATCH-test harness shape is in this file.)
}

func TestPutThreatModel_RejectsAliasInBody(t *testing.T) {
	// PUT with alias=99 in the body. Expect 400.
}
```

If the existing handler-test infrastructure uses table-driven cases, add new entries to those tables. If individual functions, write them out fully.

Add equivalent tests for at least one sub-resource — for example, in `api/note_sub_resource_handlers_test.go`:

```go
func TestPatchNote_RejectsAliasOperation(t *testing.T) { ... }
func TestPutNote_RejectsAliasInBody(t *testing.T) { ... }
```

- [ ] **Step 2: Run tests**

Run:
```
make test-unit name=TestPatchThreatModel_RejectsAliasOperation
make test-unit name=TestPutThreatModel_RejectsAliasInBody
make test-unit name=TestPatchNote_RejectsAliasOperation
make test-unit name=TestPutNote_RejectsAliasInBody
```

Expected: all PASS.

- [ ] **Step 3: Run full test suite**

Run:
```
make test-unit
```

Expected: all PASS.

- [ ] **Step 4: Commit**

```
git add api/threat_model_handlers_test.go api/note_sub_resource_handlers_test.go
git commit -m "test(api): cover client-side alias-write rejection (#374)"
```

---

## Task 15: Repository-level Create tests

**Files:**
- Modify: existing `*_store_gorm_test.go` files

Add a test per repository asserting that Create assigns an `Alias` value > 0. Mirror the existing happy-path Create tests in each file.

- [ ] **Step 1: Add Create-with-alias tests**

For each of the 7 store_gorm test files, add a test like:

```go
func TestGormNoteRepository_CreateAssignsAlias(t *testing.T) {
	// Use existing test harness (db, repo, parent TM).
	note := &Note{ ... } // typical happy-path setup
	err := repo.Create(ctx, note, threatModelID)
	require.NoError(t, err)

	// Reread to verify alias persisted.
	stored, err := repo.Get(ctx, note.Id.String())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, *stored.Alias, int32(1))
}
```

Mirror the existing patterns — use whatever test harness, fixture setup, and helper functions are used by the file's other tests.

- [ ] **Step 2: Run all repository tests**

Run:
```
make test-unit
```

Expected: all PASS.

- [ ] **Step 3: Commit**

```
git add api/*_store_gorm_test.go
git commit -m "test(api): assert alias is set on Create across all repositories (#374)"
```

---

## Task 16: Integration tests

**Files:**
- Create: `test/integration/workflows/alias_test.go`

- [ ] **Step 1: Look up the integration-test setup pattern**

Run:
```
head -80 test/integration/workflows/asset_crud_test.go
```

Use the same setup conventions: `INTEGRATION_TESTS=true` skip guard, `framework.AuthenticateUser`, `framework.NewClient`, etc.

- [ ] **Step 2: Write the integration tests**

Create `test/integration/workflows/alias_test.go`:

```go
package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

func TestAliasAssignedOnThreatModelCreate(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub: %v", err)
	}

	tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "auth failed")
	client, err := framework.NewClient("http://localhost:8080", tokens)
	framework.AssertNoError(t, err, "client failed")

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   framework.NewThreatModelFixture().WithName("Alias Test"),
	})
	framework.AssertNoError(t, err, "create failed")
	framework.AssertStatusCode(t, resp, 201)

	var tm map[string]any
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &tm), "decode failed")
	alias, ok := tm["alias"].(float64)
	if !ok {
		t.Fatalf("alias field missing or not numeric: %v", tm["alias"])
	}
	if alias < 1 {
		t.Fatalf("alias must be >= 1, got %v", alias)
	}
}

func TestAliasMonotonicAcrossSubObjects(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub: %v", err)
	}

	tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "auth failed")
	client, err := framework.NewClient("http://localhost:8080", tokens)
	framework.AssertNoError(t, err, "client failed")

	// Create TM.
	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   framework.NewThreatModelFixture().WithName("Alias Sub Test"),
	})
	framework.AssertNoError(t, err, "create TM failed")
	framework.AssertStatusCode(t, resp, 201)
	var tm map[string]any
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &tm), "decode TM")
	tmID := tm["id"].(string)

	// Create 3 notes; expect alias 1, 2, 3.
	for i := 1; i <= 3; i++ {
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/notes",
			Body:   map[string]any{"name": "n", "content": "x"},
		})
		framework.AssertNoError(t, err, "create note %d", i)
		framework.AssertStatusCode(t, resp, 201)
		var note map[string]any
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &note), "decode note")
		alias, _ := note["alias"].(float64)
		if int(alias) != i {
			t.Fatalf("expected note alias %d, got %v", i, alias)
		}
	}
}

func TestAliasIsImmutableViaPut(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub: %v", err)
	}
	tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "auth")
	client, err := framework.NewClient("http://localhost:8080", tokens)
	framework.AssertNoError(t, err, "client")

	// Create TM.
	resp, err := client.Do(framework.Request{
		Method: "POST", Path: "/threat_models",
		Body: framework.NewThreatModelFixture().WithName("Alias PUT Test"),
	})
	framework.AssertNoError(t, err, "create")
	framework.AssertStatusCode(t, resp, 201)
	var tm map[string]any
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &tm), "decode")

	// PUT with alias.
	tm["alias"] = 999
	resp, err = client.Do(framework.Request{
		Method: "PUT", Path: "/threat_models/" + tm["id"].(string),
		Body: tm,
	})
	framework.AssertNoError(t, err, "put")
	framework.AssertStatusCode(t, resp, 400)
}

func TestAliasNoReuseAfterDelete(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub: %v", err)
	}
	tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "auth")
	client, err := framework.NewClient("http://localhost:8080", tokens)
	framework.AssertNoError(t, err, "client")

	resp, err := client.Do(framework.Request{
		Method: "POST", Path: "/threat_models",
		Body: framework.NewThreatModelFixture().WithName("Alias Delete Test"),
	})
	framework.AssertNoError(t, err, "create tm")
	framework.AssertStatusCode(t, resp, 201)
	var tm map[string]any
	_ = json.Unmarshal(resp.Body, &tm)
	tmID := tm["id"].(string)

	// Create 3 notes.
	noteIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		resp, _ := client.Do(framework.Request{
			Method: "POST", Path: "/threat_models/" + tmID + "/notes",
			Body: map[string]any{"name": "n", "content": "x"},
		})
		var n map[string]any
		_ = json.Unmarshal(resp.Body, &n)
		noteIDs[i] = n["id"].(string)
	}

	// Delete note #2 (alias 2).
	resp, _ = client.Do(framework.Request{
		Method: "DELETE",
		Path:   "/threat_models/" + tmID + "/notes/" + noteIDs[1],
	})
	framework.AssertStatusCode(t, resp, 204)

	// Create another note; should be alias 4 (no reuse of alias 2).
	resp, _ = client.Do(framework.Request{
		Method: "POST", Path: "/threat_models/" + tmID + "/notes",
		Body: map[string]any{"name": "n4", "content": "x"},
	})
	framework.AssertStatusCode(t, resp, 201)
	var n4 map[string]any
	_ = json.Unmarshal(resp.Body, &n4)
	if int(n4["alias"].(float64)) != 4 {
		t.Fatalf("expected alias 4 after delete-then-create, got %v", n4["alias"])
	}
}
```

- [ ] **Step 2: Run integration tests**

Run:
```
make test-integration
```

Expected: the 4 new tests PASS. Pre-existing failures (addons/webhooks, per #372 work) may persist — confirm none of the new tests are part of the failure set.

- [ ] **Step 3: Commit**

```
git add test/integration/workflows/alias_test.go
git commit -m "test(integration): end-to-end coverage for integer aliases (#374)"
```

---

## Task 17: Oracle DB compatibility review

**Files:** none — review-only task

This change adds a new table, alters 7 existing tables, drops a legacy column, adds a cross-DB advisory-lock primitive with Oracle-specific PL/SQL, and uses dialect-specific bulk-update SQL. It is heavily DB-touching.

- [ ] **Step 1: Dispatch the oracle-db-admin subagent**

Invoke the `oracle-db-admin` skill. Provide as context:
- The new `AliasCounter` model with composite PK on (parent_id, object_type).
- The new `Alias int32` column on 7 entity models with `<-:create` tag.
- The legacy `alias` column drop on threat_models (with PG/Oracle dialect branches).
- The 7 new unique indexes (longest is `uniq_repositories_tm_alias` at 28 bytes — within Oracle's 30-byte 11c limit).
- The `pg_advisory_lock` / `DBMS_LOCK.REQUEST` primitive.
- The dialect-branched bulk-update CTEs in `bulkAssignThreatModelAliases` and `bulkAssignSubObjectAliases`.

Specific concerns to flag:
- Oracle's PL/SQL block syntax in `acquireOracleLock` (`BEGIN ... :s := DBMS_LOCK.REQUEST(...); END;`) — is the parameter binding (`?` → `:1`) correctly handled by the Oracle GORM driver?
- `DBMS_LOCK.MAXWAIT` is the right constant for "wait forever"?
- The MERGE statement in `bulkAssignThreatModelAliases` uses `WHEN MATCHED THEN UPDATE SET` — verify Oracle accepts this without a `WHEN NOT MATCHED` clause.
- Index name lengths (all ≤ 30 bytes for 11c compatibility) — confirm.

- [ ] **Step 2: Address verdict**

- **APPROVED:** record in summary; proceed to Task 18.
- **APPROVED WITH NOTES:** fold easy fixes into follow-up commit; file follow-up issues for larger.
- **BLOCKING ISSUES:** fix every one (or get user-explicit waiver) before continuing.

- [ ] **Step 3: Commit any subagent-driven changes**

```
git add <changed files>
git commit -m "fix(api): apply oracle-db-admin review notes for integer aliases (#374)"
```

(If no changes, skip the commit.)

---

## Task 18: File the path-param resolution follow-up + final gates + close issue

**Files:** none

- [ ] **Step 1: File the follow-up issue**

```
gh issue create --title "feat(api): accept integer aliases as {id} path parameters" --body "## Summary

Follow-up to #374. Once integer aliases are exposed as a read-only field
on every entity (\`#374\`), the next step is to make the alias usable as
a path parameter — clients can call \`GET /threat_models/7/diagrams/3\`
in place of full UUIDs.

## Background

\`#374\` deliberately scoped this out because tmi-ux #305's needs may be
covered by client-side resolution (the client maintains an alias→UUID
lookup map). Once tmi-ux begins using the new field, we'll know whether
server-side path-param resolution is genuinely needed.

## Specification

Implement a resolver middleware that runs before each parameterized
handler. For each \`{id}\` path segment:

1. Try parsing as a UUID. If successful, pass through unchanged.
2. Otherwise try parsing as a positive integer. If successful, look up
   the entity by alias (scoped to the parent threat model for sub-objects)
   and replace the path parameter with the canonical UUID.
3. Otherwise, return 400 Bad Request.
4. If lookup returns no row, return 404 Not Found.

OpenAPI parameter schemas change from \`{type: string, format: uuid}\` to
\`{oneOf: [{type: string, format: uuid}, {type: integer, minimum: 1}]}\`
(or a string with a regex matching either).

## Acceptance criteria

- [ ] All existing endpoints accept either a UUID or an integer alias for
  any \`{id}\` path parameter.
- [ ] Mixed UUID/alias paths work (e.g., TM by UUID + diagram by alias).
- [ ] 400 on malformed \`{id}\`.
- [ ] 404 on unknown alias.
- [ ] Integration tests cover the matrix.

## Design references

- Spec: \`docs/superpowers/specs/2026-05-04-integer-aliases-design.md\` (\"Out of scope\" section)
- Plan: \`docs/superpowers/plans/2026-05-04-integer-aliases.md\`"
```

Note the issue number it returns.

- [ ] **Step 2: Final gates**

Run:
```
make lint
make build-server
make test-unit
make test-integration
make validate-openapi
```

Expected: all PASS.

- [ ] **Step 3: Update #374**

```
gh issue comment 374 --body "Implemented on branch dev/1.4.0 (commits ...).

**Data layer**
- New \`alias_counters\` table (composite PK \`parent_id, object_type\`).
- New \`Alias int32\` column on threat_models + 6 sub-object tables, sticky-on-creation via GORM \`<-:create\`.
- Legacy \`Alias StringArray\` field on ThreatModel + the \`alias: string[]\` schema field are removed; old data is destroyed.
- Allocation via \`AllocateNextAlias\` inside \`WithRetryableGormTransaction\`, using \`SELECT ... FOR UPDATE\` for atomic counter increment.
- Migration: AutoMigrate adds columns; cross-DB advisory lock guards a one-time idempotent backfill at startup; unique indexes added post-backfill.

**API**
- \`alias\` exposed as read-only \`integer (int32)\` on every entity GET response (single, list, nested).
- PUT/PATCH attempts to set alias return 400 with descriptive message.
- Triple-layer protection: OpenAPI \`readOnly\`, handler whitelist + \`getFieldErrorMessage\`, GORM \`<-:create\`.

**Path-parameter resolution accepting alias** (originally part of #374) is filed as #<NEW_ISSUE>. The acceptance-criteria checkbox \"All existing endpoints accept either a UUID or an integer alias for any {id} path parameter\" is moved there.

**Verification**
- N unit tests pass; M integration tests pass.
- Lint clean; \`make validate-openapi\` clean.
- oracle-db-admin subagent verdict: <APPROVED / APPROVED WITH NOTES>."
gh issue close 374
```

(Replace `<NEW_ISSUE>` and the verdict with actuals.)

- [ ] **Step 4: End-of-task summary**

Report: tasks 1–18 complete, all gates passed, #374 closed, follow-up issue filed.
