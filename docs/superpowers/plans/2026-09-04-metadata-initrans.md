# Metadata INITRANS and Retired Timestamp Indexes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the four dead timestamp indexes from `models.Metadata` and raise INITRANS to 16 on the Oracle METADATA table and its six surviving indexes, as two idempotent post-AutoMigrate ensure-steps wired into all three migration entry points.

**Architecture:** Two new functions in `internal/dbschema` follow the existing ensure-step template (`EnsureUserProviderLookupUnique`): gate on the table existing, probe the catalog with retry, return early when nothing is needed, issue DDL through `execMigrationDDL` under `withDDLRetry`, re-probe, log ERROR and return nil on a DDL failure so the next boot retries. `DropRetiredMetadataIndexes` runs on PostgreSQL and Oracle (SQLite is a no-op); `EnsureMetadataInitrans` is Oracle-only. Both are called from the unconditional tail of the migration sequence at all three call sites, drop first.

**Tech Stack:** Go 1.26, GORM, gorm-oracle (godror), PostgreSQL, SQLite (unit-test dialect only), testify, Make targets.

**Spec:** `docs/superpowers/specs/2026-09-04-metadata-initrans-design.md` (the approved design comment on issue #783, which folds in #784).

## Global Constraints

- **Branch:** all work on `dev/1.9.16/metadata-initrans`, created off `main`. Do not commit to `main`.
- **Make targets only.** Never run `go test`, `go run`, `go build`, or a binary directly. Unit tests: `make test-unit name=<TestName> count1=true passfail=true` (the `name` value is a `-run` regex and must not contain `|`; use a shared prefix to run several tests). Lint: `make lint`. Build: `make build-server`. PostgreSQL integration: `make test-integration`. Oracle: `make test-integration-oci` (needs `scripts/oci-env.sh` and a running `make dev-up DB=oracle` server).
- **Oracle DDL through gorm silently no-ops on a repeated byte-identical statement** (PrepareStmt cache). Every DDL statement in production code goes through `execMigrationDDL` (`internal/dbschema/oracle_ddl.go:68`), never `db.Exec`. Catalog SELECTs may use `db.Raw`.
- **Oracle catalog probes use `ALL_TABLES` / `ALL_INDEXES` / `ALL_CONSTRAINTS` filtered on `oracleCurrentSchema`** (`internal/dbschema/catalog_probe.go:37`), never `USER_*` views. Oracle folds unquoted identifiers to upper case: every name bound into a catalog query or DDL is `strings.ToUpper`ed.
- **INITRANS target is 16** (`metadataInitransTarget`). Surviving Oracle index names: the system-named primary-key index (discovered from `ALL_CONSTRAINTS`), `IDX_METADATA_UNIQUE`, `IDX_METADATA_ENTITY_TYPE_ID`, `IDX_METADATA_ENTITY_ID`, `IDX_METADATA_KEY`, `IDX_METADATA_KEY_VALUE`. Retired names: `idx_metadata_created`, `idx_metadata_modified`, `idx_metadata_entity_created`, `idx_metadata_entity_modified`.
- **Never fatal on DDL failure.** An ensure-step returns an error only when it cannot read the catalog at all. A failed DDL logs ERROR and returns nil.
- **SEM markers.** Every new function, method, type, or package-level var/const block that other code calls gets a one-line `// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: <intent, <=12 words, lead with a canonical verb>` comment directly above it (current HEAD sha). An existing function whose behavior changes gets its description text updated but keeps its existing sha. Unchanged functions are left alone.
- **Logging:** `github.com/ericfitz/tmi/internal/slogging` only (`slogging.Get()`); never `log` or `fmt.Println`.
- **gofmt** formatting; imports grouped stdlib / external / internal. `make lint` must pass before every commit.
- **Commits:** conventional commits, imperative mood. Every commit message ends with these two trailer lines:

  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv
  ```

  The resolving commit (Task 6) carries `Fixes #783` and `Fixes #784` in its body.
- **Model fingerprint side effect (expected):** removing the four index tags changes `ComputeModelsFingerprint`, so the first boot after this change runs a full AutoMigrate pass once (not the #480 fast path) and re-stamps. No action needed; mention it in the PR body.
- **Oracle review is mandatory** (Task 8) before the PR is opened; address every BLOCKING finding.

---

### Task 1: Branch and design documents

**Files:**
- Create: `docs/superpowers/specs/2026-09-04-metadata-initrans-design.md` (already written; commit it)
- Create: `docs/superpowers/plans/2026-09-04-metadata-initrans.md` (this file; commit it)

- [ ] **Step 1: Create the branch off main**

```bash
cd /Users/efitz/Projects/tmi
git checkout main
git pull --rebase
git checkout -b dev/1.9.16/metadata-initrans
```

Expected: `Switched to a new branch 'dev/1.9.16/metadata-initrans'`.

- [ ] **Step 2: Commit the spec and plan**

```bash
git add docs/superpowers/specs/2026-09-04-metadata-initrans-design.md docs/superpowers/plans/2026-09-04-metadata-initrans.md
git commit -m "docs(db): add approved design and plan for metadata INITRANS and retired indexes

Refs #783, #784

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 2: Index name constants, DDL builders, and the per-dialect index-existence probe

**Files:**
- Create: `internal/dbschema/metadata_index.go`
- Test: `internal/dbschema/metadata_index_test.go`

**Interfaces:**
- Consumes: `oracleCurrentSchema` (`internal/dbschema/catalog_probe.go:37`).
- Produces (used by Tasks 3, 4, 5):
  - `var retiredMetadataIndexes []string` (lowercase model names)
  - `const metadataInitransTarget = 16`
  - `var metadataInitransIndexes []string` (lowercase model names of the five named survivors; the PK index is discovered at runtime)
  - `func retiredMetadataIndexDropDDL(dialect, indexName string) string`
  - `func metadataTableInitransDDL(upperTable string) []string`
  - `func metadataIndexInitransDDL(upperIndex string) string`
  - `func metadataIndexExists(db *gorm.DB, indexName, tableName string) (bool, error)` (oracle, postgres, sqlite)

- [ ] **Step 1: Write the failing tests**

Create `internal/dbschema/metadata_index_test.go`:

```go
package dbschema

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newMetadataIndexTestDB builds an in-memory SQLite database carrying the real
// models.Metadata table plus the four pre-#784 timestamp indexes, i.e. the
// state every database created before #784 is in. The real model is used
// rather than a test-local mirror because the point of these tests is the
// model's own index set.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: build an in-memory SQLite DB with the metadata table and its pre-#784 retired indexes (pure)
func newMetadataIndexTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Metadata{}))
	for _, name := range retiredMetadataIndexes {
		require.NoError(t, db.Exec("CREATE INDEX "+name+" ON metadata (created_at)").Error)
	}
	return db
}

// TestRetiredMetadataIndexDropDDL pins the per-dialect DROP text: PostgreSQL
// has IF EXISTS; Oracle does not and folds unquoted names to upper case, so a
// lowercase DROP would raise ORA-01418.
func TestRetiredMetadataIndexDropDDL(t *testing.T) {
	assert.Equal(t, "DROP INDEX IF EXISTS idx_metadata_created", retiredMetadataIndexDropDDL("postgres", "idx_metadata_created"))
	assert.Equal(t, "DROP INDEX IDX_METADATA_CREATED", retiredMetadataIndexDropDDL("oracle", "idx_metadata_created"))
}

// TestMetadataInitransDDL pins the Oracle statements: INITRANS on the table
// affects only new blocks, so MOVE ONLINE rewrites the existing ones; each
// index is rebuilt online with the new INITRANS.
func TestMetadataInitransDDL(t *testing.T) {
	assert.Equal(t, []string{
		"ALTER TABLE METADATA INITRANS 16",
		"ALTER TABLE METADATA MOVE ONLINE",
	}, metadataTableInitransDDL("METADATA"))
	assert.Equal(t, "ALTER INDEX IDX_METADATA_KEY REBUILD ONLINE INITRANS 16", metadataIndexInitransDDL("IDX_METADATA_KEY"))
}

// TestMetadataIndexExists_SQLite covers the sqlite branch of the probe, which
// the no-op test in Task 4 relies on to prove nothing was dropped.
func TestMetadataIndexExists_SQLite(t *testing.T) {
	db := newMetadataIndexTestDB(t)

	exists, err := metadataIndexExists(db, "idx_metadata_created", "metadata")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = metadataIndexExists(db, "idx_metadata_does_not_exist", "metadata")
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestMetadataInitransIndexes_NamesMatchModel pins the survivor list against
// the model: every name EnsureMetadataInitrans will rebuild must be an index
// AutoMigrate actually creates, and no retired name may survive in the model.
func TestMetadataInitransIndexes_NamesMatchModel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Metadata{}))

	for _, name := range metadataInitransIndexes {
		exists, err := metadataIndexExists(db, name, "metadata")
		require.NoError(t, err)
		assert.True(t, exists, "surviving index %s must be created by the model", name)
	}
	for _, name := range retiredMetadataIndexes {
		exists, err := metadataIndexExists(db, name, "metadata")
		require.NoError(t, err)
		assert.False(t, exists, "retired index %s must no longer be in the model (#784)", name)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-unit name=TestMetadata count1=true passfail=true`
Expected: FAIL to compile (`undefined: retiredMetadataIndexes`, `undefined: metadataIndexExists`, ...).

- [ ] **Step 3: Write the implementation**

Create `internal/dbschema/metadata_index.go`:

```go
// Package dbschema: retirement of the four dead metadata timestamp indexes
// (#784) and the INITRANS raise on METADATA and its surviving indexes (#783).
//
// #783 measured a false ORA-08177 on back-to-back BulkReplace calls against
// the same entity with no concurrent writer: GORM emits a bare CREATE TABLE,
// so metadata blocks carry Oracle's default INITRANS 1 (2 for index blocks),
// and the second transaction recycles the first one's ITL slot on the very
// blocks it just wrote. Ten delete-then-insert metadata callers still run
// inside SERIALIZABLE parent transactions, so INITRANS 16 is defense in
// depth. The aggravator was four timestamp-leading indexes whose right-edge
// leaf block every same-batch row landed on; nothing reads metadata by
// created_at or modified_at, so they are dropped rather than rebuilt.
//
// AutoMigrate never drops an index and never sets physical attributes, so
// both are raw DDL here, on the EnsureUserProviderLookupUnique template.
package dbschema

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// retiredMetadataIndexes are the four timestamp-leading indexes removed from
// models.Metadata in #784, spelled as the model spelled them. AutoMigrate never
// drops an index, so a database created before #784 keeps them until
// DropRetiredMetadataIndexes runs.
var retiredMetadataIndexes = []string{
	"idx_metadata_created",
	"idx_metadata_modified",
	"idx_metadata_entity_created",
	"idx_metadata_entity_modified",
}

// metadataInitransTarget is the ITL slot count EnsureMetadataInitrans raises
// the metadata table and its indexes to. Oracle's default is 1 for table
// blocks and 2 for index blocks; the #783 review recommended 8-16 and the
// approved design picked 16. Each slot costs 24 bytes per block.
const metadataInitransTarget = 16

// metadataInitransIndexes are the named secondary indexes of models.Metadata
// that survive #784, spelled as the model spells them. The primary-key index
// is deliberately absent: gorm-oracle emits a bare PRIMARY KEY clause, so its
// index is system-named (SYS_C...) and is discovered from ALL_CONSTRAINTS at
// runtime by metadataInitransProbe.
var metadataInitransIndexes = []string{
	"idx_metadata_unique",
	"idx_metadata_entity_type_id",
	"idx_metadata_entity_id",
	"idx_metadata_key",
	"idx_metadata_key_value",
}

// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: build the per-dialect DROP INDEX statement for a retired metadata index (pure)
func retiredMetadataIndexDropDDL(dialect, indexName string) string {
	if dialect == "oracle" {
		// No IF EXISTS on Oracle index DDL; the caller probes the catalog
		// first. Uppercase because TMI runs with SkipQuoteIdentifiers.
		return "DROP INDEX " + strings.ToUpper(indexName)
	}
	return "DROP INDEX IF EXISTS " + indexName
}

// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: build the ALTER TABLE statements that raise INITRANS and rewrite existing blocks (pure)
func metadataTableInitransDDL(upperTable string) []string {
	// INITRANS on a table applies only to blocks formatted afterwards; MOVE
	// ONLINE rewrites the existing blocks with the new setting while keeping
	// the indexes usable (12.2+). Two statements rather than one MOVE with
	// physical attributes so the catalog change and the block rewrite are
	// separately visible in the log.
	return []string{
		fmt.Sprintf("ALTER TABLE %s INITRANS %d", upperTable, metadataInitransTarget),
		fmt.Sprintf("ALTER TABLE %s MOVE ONLINE", upperTable),
	}
}

// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: build the ALTER INDEX statement that rebuilds an index online with the target INITRANS (pure)
func metadataIndexInitransDDL(upperIndex string) string {
	return fmt.Sprintf("ALTER INDEX %s REBUILD ONLINE INITRANS %d", upperIndex, metadataInitransTarget)
}

// metadataIndexExists reports whether indexName exists on tableName in the
// schema this session resolves unqualified identifiers against.
//
// Oracle probes ALL_INDEXES pinned to CURRENT_SCHEMA for both OWNER and
// TABLE_OWNER (#736): an unqualified DROP INDEX resolves the name in
// CURRENT_SCHEMA, so a foreign-owned same-named index must not read as ours.
// PostgreSQL and SQLite have no such split. gorm's HasIndex is not used for
// the reason userProviderLookupIndexExists gives: on Oracle it binds the
// lowercase tag name against the uppercase catalog and always answers absent.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: probe whether a named index exists on a table in the current schema, per dialect (reads DB)
func metadataIndexExists(db *gorm.DB, indexName, tableName string) (bool, error) {
	var cnt int64
	var err error
	switch db.Name() {
	case "oracle":
		err = db.Raw(
			"SELECT COUNT(*) FROM ALL_INDEXES WHERE INDEX_NAME = ? AND TABLE_NAME = ? "+
				"AND OWNER = "+oracleCurrentSchema+" AND TABLE_OWNER = "+oracleCurrentSchema,
			strings.ToUpper(indexName), strings.ToUpper(tableName),
		).Scan(&cnt).Error
	case "postgres":
		err = db.Raw(
			"SELECT COUNT(*) FROM pg_indexes WHERE indexname = ? AND tablename = ? AND schemaname = current_schema()",
			indexName, tableName,
		).Scan(&cnt).Error
	case "sqlite":
		// Test-fixture dialect only.
		err = db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ?",
			indexName, tableName,
		).Scan(&cnt).Error
	default:
		return false, fmt.Errorf("metadataIndexExists: unsupported dialect %q", db.Name())
	}
	return cnt > 0, err
}
```

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestMetadata count1=true passfail=true`
Expected: `TestRetiredMetadataIndexDropDDL`, `TestMetadataInitransDDL`, `TestMetadataIndexExists_SQLite` PASS. `TestMetadataInitransIndexes_NamesMatchModel` FAILS on the retired-index assertions (the model still carries them); Task 3 fixes that. The surviving-index assertions in it must already pass; if one fails, the name in `metadataInitransIndexes` is misspelled relative to `api/models/models.go:552`.

- [ ] **Step 5: Lint and commit**

Run: `make lint`
Expected: no findings in the two new files.

```bash
git add internal/dbschema/metadata_index.go internal/dbschema/metadata_index_test.go
git commit -m "feat(dbschema): add metadata index name tables, DDL builders, and index probe

Groundwork for retiring the metadata timestamp indexes (#784) and raising
INITRANS on METADATA (#783). TestMetadataInitransIndexes_NamesMatchModel is
expected red until the model tags are removed in the next commit.

Refs #783, #784

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 3: Remove the four timestamp index tags from `models.Metadata`

**Files:**
- Modify: `api/models/models.go:552-560` (the `Metadata` struct)
- Test: `internal/dbschema/metadata_index_test.go` (`TestMetadataInitransIndexes_NamesMatchModel`, written in Task 2)

**Interfaces:**
- Produces: `models.Metadata` with exactly six indexes: the primary key, `idx_metadata_unique` (unique), `idx_metadata_entity_type_id`, `idx_metadata_entity_id`, `idx_metadata_key`, `idx_metadata_key_value`.

- [ ] **Step 1: Confirm the test is red**

Run: `make test-unit name=TestMetadataInitransIndexes_NamesMatchModel count1=true passfail=true`
Expected: FAIL with `retired index idx_metadata_created must no longer be in the model (#784)` (and the other three).

- [ ] **Step 2: Edit the struct**

Replace the `Metadata` struct at `api/models/models.go:552-560` with:

```go
type Metadata struct {
	ID         DBVarchar `gorm:"primaryKey;not null;size:36"`
	EntityType DBVarchar `gorm:"size:50;not null;index:idx_metadata_entity_type_id,priority:1;index:idx_metadata_unique,priority:1,unique"`
	EntityID   DBVarchar `gorm:"size:36;not null;index:idx_metadata_entity_id;index:idx_metadata_entity_type_id,priority:2;index:idx_metadata_unique,priority:2;index:idx_metadata_key_value,priority:1"`
	Key        DBVarchar `gorm:"size:256;not null;index:idx_metadata_key;index:idx_metadata_unique,priority:3;index:idx_metadata_key_value,priority:2"`
	Value      DBVarchar `gorm:"size:1024;not null;index:idx_metadata_key_value,priority:3"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt time.Time `gorm:"not null;autoUpdateTime"`
}
```

Only the `CreatedAt`, `ModifiedAt`, and `EntityType` tags change: `index:idx_metadata_created`, `index:idx_metadata_modified`, `index:idx_metadata_entity_created,priority:N`, and `index:idx_metadata_entity_modified,priority:N` are removed. Every other tag fragment is byte-identical to what is there now.

Update the comment block directly above the struct (currently the two lines beginning `// Metadata represents key-value metadata for entities` and `// Note: Explicit column tags removed for Oracle compatibility`) to:

```go
// Metadata represents key-value metadata for entities
// Note: Explicit column tags removed for Oracle compatibility
// Note: no index leads with created_at or modified_at (#784): nothing reads
// metadata by timestamp, and every same-batch row shares one timestamp, so
// such indexes only concentrated writes on one leaf block (#783).
```

Keep the existing `// SEM@db6c3b75...` line unchanged: the struct's intent ("DB model for arbitrary key-value metadata on any entity") did not change.

- [ ] **Step 3: Run the tests**

Run: `make test-unit name=TestMetadata count1=true passfail=true`
Expected: all four PASS.

- [ ] **Step 4: Confirm nothing else names the retired indexes**

Run:

```bash
rg -n "idx_metadata_(created|modified|entity_created|entity_modified)" --type go -g '!api/api.go' /Users/efitz/Projects/tmi
```

Expected: only `internal/dbschema/metadata_index.go` (the `retiredMetadataIndexes` table). `internal/dbschema/schema.go:405-411` lists a different, older metadata index set for `ValidateSchema` and does not mention these four; leave it alone.

- [ ] **Step 5: Build, full unit tests, lint, commit**

Run: `make build-server`, then `make test-unit`, then `make lint`.
Expected: build OK, all unit tests PASS, lint clean.

```bash
git add api/models/models.go
git commit -m "refactor(models): drop the four timestamp-leading indexes from Metadata

No reader orders metadata by created_at or modified_at, and with every
same-batch row sharing one timestamp these indexes concentrated inserts on
a single right-edge leaf block, where INITRANS-1 ITL slots run out (#783).
Existing databases keep the indexes until DropRetiredMetadataIndexes runs;
the model fingerprint changes, so the next boot runs one full AutoMigrate.

Refs #783, #784

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 4: `DropRetiredMetadataIndexes`

**Files:**
- Modify: `internal/dbschema/metadata_index.go`
- Test: `internal/dbschema/metadata_index_test.go`

**Interfaces:**
- Consumes: `requireMigrationTable(db, table, step) (bool, error)` (`catalog_probe.go:87`), `withMigrationRetry(label, fn) error` (`group_dedupe.go:243`), `withDDLRetry(label, fn) error` (`oracle_ddl.go:132`), `execMigrationDDL(db, ddl) error` (`oracle_ddl.go:68`), plus Task 2's `retiredMetadataIndexes`, `retiredMetadataIndexDropDDL`, `metadataIndexExists`.
- Produces: `func DropRetiredMetadataIndexes(db *gorm.DB) error` (exported; wired in Task 6, called by the OCI test in Task 7).

- [ ] **Step 1: Write the failing tests**

Append to `internal/dbschema/metadata_index_test.go`:

```go
// TestDropRetiredMetadataIndexes_SQLiteIsNoOp pins the design decision that
// the drop only runs on the two production dialects: on the SQLite test
// fixture it must return nil and leave every retired index in place.
func TestDropRetiredMetadataIndexes_SQLiteIsNoOp(t *testing.T) {
	db := newMetadataIndexTestDB(t)

	require.NoError(t, DropRetiredMetadataIndexes(db))
	require.NoError(t, DropRetiredMetadataIndexes(db), "must be idempotent")

	for _, name := range retiredMetadataIndexes {
		exists, err := metadataIndexExists(db, name, "metadata")
		require.NoError(t, err)
		assert.True(t, exists, "SQLite is a test-only dialect; %s must be left alone", name)
	}
}

// TestDropRetiredMetadataIndexes_NoTable covers a database whose metadata
// table does not exist yet: nothing to do, no error.
func TestDropRetiredMetadataIndexes_NoTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, DropRetiredMetadataIndexes(db))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-unit name=TestDropRetiredMetadataIndexes count1=true passfail=true`
Expected: FAIL to compile (`undefined: DropRetiredMetadataIndexes`).

- [ ] **Step 3: Write the implementation**

Add to the import block of `internal/dbschema/metadata_index.go`:

```go
	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
```

(final import block: stdlib `fmt`, `strings`; then `github.com/ericfitz/tmi/api/models`, `github.com/ericfitz/tmi/internal/slogging`, `gorm.io/gorm`).

Append to `internal/dbschema/metadata_index.go`:

```go
// DropRetiredMetadataIndexes removes the four timestamp-leading metadata
// indexes that #784 took out of models.Metadata from a database that still
// carries them. Idempotent: on a current database it costs one catalog query
// per retired name.
//
// Safe, and required, to call on every boot after AutoMigrate, including when
// the #480 fingerprint fast path skips AutoMigrate: AutoMigrate never drops
// an index, so nothing else would ever remove them.
//
// Runs on PostgreSQL and Oracle only. SQLite is the unit-test dialect and
// carries no production data, so it is left untouched (the design's chosen
// no-op path, pinned by TestDropRetiredMetadataIndexes_SQLiteIsNoOp).
//
// Never aborts startup on a DDL failure, only on a failure to read the
// catalog: a retired index that survives is a write-cost nuisance, not a
// correctness problem, and the next boot or `tmi-dbtool --schema` retries.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: drop the retired metadata timestamp indexes if present, else warn and continue (mutates DB)
func DropRetiredMetadataIndexes(db *gorm.DB) error {
	table := (&models.Metadata{}).TableName()
	present, err := requireMigrationTable(db, table, "retired metadata index drop (#784)")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}

	dialect := db.Name()
	if dialect != "postgres" && dialect != "oracle" {
		return nil
	}

	logger := slogging.Get()
	for _, name := range retiredMetadataIndexes {
		var exists bool
		if err := withMigrationRetry("retired metadata index probe", func() error {
			var probeErr error
			exists, probeErr = metadataIndexExists(db, name, table)
			return probeErr
		}); err != nil {
			return fmt.Errorf("failed to check whether %s exists on %s: %w", name, table, err)
		}
		if !exists {
			continue
		}

		ddl := retiredMetadataIndexDropDDL(dialect, name)
		if err := withDDLRetry("retired metadata index drop "+name, func() error {
			return execMigrationDDL(db, ddl)
		}); err != nil {
			// Oracle commits before and after every DDL statement, so a
			// transient failure can arrive after the DROP took effect. Ask
			// the catalog what is true rather than inferring it from the
			// error (the same reasoning as EnsureUserProviderLookupUnique).
			var still bool
			probeErr := withMigrationRetry("retired metadata index post-drop probe", func() error {
				var e error
				still, e = metadataIndexExists(db, name, table)
				return e
			})
			if probeErr == nil && !still {
				logger.Warn("the DROP of %s reported an error (%v) but the index is gone; continuing (#784)", name, err)
				continue
			}
			logger.Error(
				"failed to drop retired metadata index %s: %v. The index is harmless but still costs every metadata write; "+
					"startup is continuing and the next boot or `tmi-dbtool --schema` with an admin-privileged database user retries (#784)",
				name, err)
			continue
		}
		logger.Info("dropped retired metadata index %s from %s (#784)", name, table)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestDropRetiredMetadataIndexes count1=true passfail=true`
Expected: both PASS.

- [ ] **Step 5: Lint and commit**

Run: `make lint`
Expected: clean.

```bash
git add internal/dbschema/metadata_index.go internal/dbschema/metadata_index_test.go
git commit -m "feat(dbschema): add DropRetiredMetadataIndexes ensure-step

Drops idx_metadata_created, idx_metadata_modified,
idx_metadata_entity_created, and idx_metadata_entity_modified where a
database still carries them: DROP INDEX IF EXISTS on PostgreSQL, a
CURRENT_SCHEMA-scoped ALL_INDEXES probe then DROP INDEX through
execMigrationDDL on Oracle, no-op on SQLite. Logs and continues on a DDL
failure so the next boot retries.

Refs #783, #784

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 5: `EnsureMetadataInitrans`

**Files:**
- Modify: `internal/dbschema/metadata_index.go`
- Test: `internal/dbschema/metadata_index_test.go`

**Interfaces:**
- Consumes: Task 2's `metadataInitransTarget`, `metadataInitransIndexes`, `metadataTableInitransDDL`, `metadataIndexInitransDDL`; `requireMigrationTable`, `withMigrationRetry`, `withDDLRetry`, `execMigrationDDL`, `oracleCurrentSchema`.
- Produces:
  - `type metadataInitransState struct { Table int64; Indexes map[string]int64 }` with method `below() (tableBelow bool, indexesBelow []string)`
  - `func metadataInitransProbe(db *gorm.DB, table string) (metadataInitransState, error)` (Oracle only)
  - `func EnsureMetadataInitrans(db *gorm.DB) error` (exported; wired in Task 6, called by the OCI test in Task 7)

- [ ] **Step 1: Write the failing tests**

Append to `internal/dbschema/metadata_index_test.go`:

```go
// TestMetadataInitransState_Below pins the skip decision: only objects
// strictly below the target need DDL, and the index list is sorted so the
// log line and the DDL order are stable across boots.
func TestMetadataInitransState_Below(t *testing.T) {
	state := metadataInitransState{
		Table: 1,
		Indexes: map[string]int64{
			"SYS_C0012345":                2,
			"IDX_METADATA_KEY":            16,
			"IDX_METADATA_ENTITY_TYPE_ID": 2,
			"IDX_METADATA_UNIQUE":         32,
		},
	}
	tableBelow, indexesBelow := state.below()
	assert.True(t, tableBelow)
	assert.Equal(t, []string{"IDX_METADATA_ENTITY_TYPE_ID", "SYS_C0012345"}, indexesBelow)

	done := metadataInitransState{Table: 16, Indexes: map[string]int64{"IDX_METADATA_KEY": 16}}
	tableBelow, indexesBelow = done.below()
	assert.False(t, tableBelow)
	assert.Empty(t, indexesBelow)
}

// TestEnsureMetadataInitrans_NonOracleIsNoOp covers every non-Oracle dialect:
// INITRANS is an Oracle physical attribute, so the step returns before it
// even looks for the table.
func TestEnsureMetadataInitrans_NonOracleIsNoOp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, EnsureMetadataInitrans(db), "no table, no Oracle, no error")

	db = newMetadataIndexTestDB(t)
	require.NoError(t, EnsureMetadataInitrans(db))
	require.NoError(t, EnsureMetadataInitrans(db), "must be idempotent")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-unit name=TestMetadataInitransState count1=true passfail=true` and `make test-unit name=TestEnsureMetadataInitrans count1=true passfail=true`
Expected: FAIL to compile (`undefined: metadataInitransState`, `undefined: EnsureMetadataInitrans`).

- [ ] **Step 3: Write the implementation**

Add `"sort"` to the stdlib import group of `internal/dbschema/metadata_index.go`, then append:

```go
// metadataInitransState is one reading of the ITL configuration of the
// metadata table and its surviving indexes. Indexes is keyed by the uppercase
// catalog name and holds only the indexes the catalog actually returned; a
// surviving index that is missing from the catalog is warned about by the
// caller and otherwise ignored, since there is nothing to rebuild.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: hold the catalog INITRANS values of the metadata table and its indexes (pure)
type metadataInitransState struct {
	Table   int64
	Indexes map[string]int64
}

// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: report which of the table and its indexes sit below the INITRANS target (pure)
func (s metadataInitransState) below() (tableBelow bool, indexesBelow []string) {
	tableBelow = s.Table < metadataInitransTarget
	for name, ini := range s.Indexes {
		if ini < metadataInitransTarget {
			indexesBelow = append(indexesBelow, name)
		}
	}
	sort.Strings(indexesBelow)
	return tableBelow, indexesBelow
}

// metadataInitransProbe reads INI_TRANS for the metadata table, its
// primary-key index, and its named surviving indexes from the Oracle catalog,
// scoped to the session's CURRENT_SCHEMA (#736). The primary-key index is
// system-named, so its name comes from ALL_CONSTRAINTS.INDEX_NAME for the
// table's CONSTRAINT_TYPE = 'P' row.
//
// The scan targets are untagged on purpose: Oracle returns UPPERCASE result
// labels and an untagged field's DBName follows the active dialect's naming
// strategy, so IndexName binds to INDEX_NAME and IniTrans to INI_TRANS (the
// same convention as userProviderLookupIndexState).
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: fetch INITRANS of the metadata table, PK index, and named indexes from the Oracle catalog (reads DB)
func metadataInitransProbe(db *gorm.DB, table string) (metadataInitransState, error) {
	state := metadataInitransState{Indexes: map[string]int64{}}
	upperTable := strings.ToUpper(table)

	var tableIni []int64
	if err := db.Raw(
		"SELECT INI_TRANS FROM ALL_TABLES WHERE TABLE_NAME = ? AND OWNER = "+oracleCurrentSchema,
		upperTable,
	).Scan(&tableIni).Error; err != nil {
		return state, err
	}
	if len(tableIni) == 0 {
		return state, fmt.Errorf("table %s not found in ALL_TABLES for CURRENT_SCHEMA", upperTable)
	}
	state.Table = tableIni[0]

	var pkIndexes []string
	if err := db.Raw(
		"SELECT INDEX_NAME FROM ALL_CONSTRAINTS WHERE TABLE_NAME = ? AND CONSTRAINT_TYPE = 'P' "+
			"AND INDEX_NAME IS NOT NULL AND OWNER = "+oracleCurrentSchema,
		upperTable,
	).Scan(&pkIndexes).Error; err != nil {
		return state, err
	}

	names := make([]string, 0, len(pkIndexes)+len(metadataInitransIndexes))
	names = append(names, pkIndexes...)
	for _, n := range metadataInitransIndexes {
		names = append(names, strings.ToUpper(n))
	}

	var rows []struct {
		IndexName string
		IniTrans  int64
	}
	if err := db.Raw(
		"SELECT INDEX_NAME, INI_TRANS FROM ALL_INDEXES WHERE TABLE_NAME = ? AND INDEX_NAME IN ? "+
			"AND OWNER = "+oracleCurrentSchema+" AND TABLE_OWNER = "+oracleCurrentSchema,
		upperTable, names,
	).Scan(&rows).Error; err != nil {
		return state, err
	}
	for _, r := range rows {
		state.Indexes[r.IndexName] = r.IniTrans
	}
	return state, nil
}

// EnsureMetadataInitrans raises INITRANS to metadataInitransTarget on the
// Oracle metadata table and its six surviving indexes (#783). Idempotent: on
// a database already at or above the target it costs three catalog queries.
// No-op on every other dialect; INITRANS is an Oracle physical attribute.
//
// Safe, and required, to call on every boot after AutoMigrate, including when
// the #480 fingerprint fast path skips AutoMigrate: physical attributes are
// not part of the model fingerprint, and AutoMigrate could not set them
// anyway.
//
// Order matters at the call sites: DropRetiredMetadataIndexes runs first so
// the four retired indexes are never rebuilt here.
//
// Never aborts startup on a DDL failure, only on a failure to read the
// catalog. ALTER TABLE ... MOVE ONLINE and ALTER INDEX ... REBUILD ONLINE are
// privileged, take DDL locks, and can fail on a busy table or under a
// runtime user without ALTER; in every such case the database is left as it
// was (each statement is its own transaction) and the next boot or
// `tmi-dbtool --schema` retries. The outcome is decided by re-reading the
// catalog after the DDL, not by trusting the DDL's error.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: raise INITRANS on the Oracle metadata table and its indexes to the target, else warn and continue (mutates DB)
func EnsureMetadataInitrans(db *gorm.DB) error {
	if db.Name() != "oracle" {
		return nil
	}
	table := (&models.Metadata{}).TableName()
	present, err := requireMigrationTable(db, table, "metadata INITRANS raise (#783)")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}

	logger := slogging.Get()
	upperTable := strings.ToUpper(table)

	var state metadataInitransState
	if err := withMigrationRetry("metadata INITRANS probe", func() error {
		var probeErr error
		state, probeErr = metadataInitransProbe(db, table)
		return probeErr
	}); err != nil {
		return fmt.Errorf("failed to read the INITRANS settings of %s: %w", upperTable, err)
	}
	for _, n := range metadataInitransIndexes {
		if _, ok := state.Indexes[strings.ToUpper(n)]; !ok {
			logger.Warn("%s not found on %s in CURRENT_SCHEMA; skipping its INITRANS raise (#783)", strings.ToUpper(n), upperTable)
		}
	}

	tableBelow, indexesBelow := state.below()
	if !tableBelow && len(indexesBelow) == 0 {
		return nil
	}
	logger.Info("raising INITRANS to %d on %s (table at %d; indexes below target: %v) (#783)",
		metadataInitransTarget, upperTable, state.Table, indexesBelow)

	if tableBelow {
		for _, ddl := range metadataTableInitransDDL(upperTable) {
			if err := withDDLRetry("metadata INITRANS "+ddl, func() error {
				return execMigrationDDL(db, ddl)
			}); err != nil {
				logger.Error("%q failed: %v (#783)", ddl, err)
				break
			}
		}
	}
	for _, name := range indexesBelow {
		ddl := metadataIndexInitransDDL(name)
		if err := withDDLRetry("metadata INITRANS "+ddl, func() error {
			return execMigrationDDL(db, ddl)
		}); err != nil {
			logger.Error("%q failed: %v (#783)", ddl, err)
		}
	}

	if err := withMigrationRetry("metadata INITRANS post-DDL probe", func() error {
		var probeErr error
		state, probeErr = metadataInitransProbe(db, table)
		return probeErr
	}); err != nil {
		return fmt.Errorf("failed to verify the INITRANS settings of %s after raising them: %w", upperTable, err)
	}
	tableBelow, indexesBelow = state.below()
	if tableBelow || len(indexesBelow) > 0 {
		logger.Error(
			"INITRANS on %s is still below %d after the raise (table at %d; indexes below target: %v). "+
				"Back-to-back metadata writes on one entity inside a SERIALIZABLE transaction remain exposed to a false ORA-08177; "+
				"startup is continuing and the next boot or `tmi-dbtool --schema` with an admin-privileged database user retries (#783)",
			upperTable, metadataInitransTarget, state.Table, indexesBelow)
		return nil
	}
	logger.Info("INITRANS on %s and its %d surviving indexes is now >= %d (#783)", upperTable, len(state.Indexes), metadataInitransTarget)
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestMetadata count1=true passfail=true` and `make test-unit name=TestEnsureMetadataInitrans count1=true passfail=true`
Expected: all PASS.

- [ ] **Step 5: Lint and commit**

Run: `make lint`
Expected: clean. If golangci-lint flags the loop-closure captures of `ddl`, the module is Go 1.26 (per-iteration loop variables), so the capture is correct; fix the linter's actual complaint rather than restructuring.

```bash
git add internal/dbschema/metadata_index.go internal/dbschema/metadata_index_test.go
git commit -m "feat(dbschema): add EnsureMetadataInitrans ensure-step (Oracle only)

Probes ALL_TABLES.INI_TRANS for METADATA and ALL_INDEXES.INI_TRANS for its
system-named PK index (via ALL_CONSTRAINTS) and five named survivors, all
scoped to CURRENT_SCHEMA. When anything is below 16: ALTER TABLE INITRANS
16, ALTER TABLE MOVE ONLINE, then ALTER INDEX REBUILD ONLINE INITRANS 16
per index, every statement through execMigrationDDL. Re-probes afterwards
and logs ERROR (returning nil) if still below target.

Refs #783

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 6: Wire both steps at the three migration call sites

**Files:**
- Modify: `cmd/server/main.go:549-551` (insert after the `EnsureSparseUserEmailIndex` block, before the `// Normalize legacy severity` comment)
- Modify: `auth/config_adapter.go:315-317` (insert after the `EnsureSparseUserEmailIndex` block, before the `// #813: remove system_settings` comment)
- Modify: `cmd/dbtool/schema.go:131-133` (insert after the `EnsureSparseUserEmailIndex` block, before the `// #813: remove system_settings` comment)

**Interfaces:**
- Consumes: `dbschema.DropRetiredMetadataIndexes(db *gorm.DB) error` (Task 4), `dbschema.EnsureMetadataInitrans(db *gorm.DB) error` (Task 5). Both return an error only on a catalog-read failure.

- [ ] **Step 1: Wire `cmd/server/main.go`**

In `migrateSchema`, directly after the closing brace of the `if err := dbschema.EnsureSparseUserEmailIndex(gormDB.DB()); err != nil { ... }` block (line 549) and before the blank line preceding `// Normalize legacy severity enum values to snake_case` (line 551), insert:

```go

	// #784: the four timestamp-leading metadata indexes were removed from the
	// model (nothing reads metadata by created_at/modified_at) and AutoMigrate
	// never drops an index, so retire them here. Runs even when the
	// fingerprint fast path skips AutoMigrate. Never fatal on a DDL failure:
	// it logs and continues, and the next boot retries.
	if err := dbschema.DropRetiredMetadataIndexes(gormDB.DB()); err != nil {
		return fmt.Errorf("failed to check the retired metadata indexes: %w", err)
	}

	// #783: raise INITRANS to 16 on METADATA and its surviving indexes
	// (Oracle only) so back-to-back same-entity metadata writes inside a
	// SERIALIZABLE transaction cannot recycle ITL slots into a false
	// ORA-08177. Runs after the drop so retired indexes are never rebuilt.
	// Never fatal on a DDL failure: logs and continues.
	if err := dbschema.EnsureMetadataInitrans(gormDB.DB()); err != nil {
		return fmt.Errorf("failed to check the metadata INITRANS settings: %w", err)
	}
```

Leave the `// SEM@72dd09a3...` marker on `migrateSchema` as is: its description ("run the schema-evolution sequence: AutoMigrate, backfills, index upgrades, and seeding") still covers the new steps.

- [ ] **Step 2: Wire `auth/config_adapter.go`**

In `migrateSchemaForConfigAdapter`, directly after the closing brace of the `if err := dbschema.EnsureSparseUserEmailIndex(gormDB.DB()); err != nil { ... }` block (line 315) and before the blank line preceding `// #813: remove system_settings rows` (line 317), insert:

```go

	// #784 / #783: retire the metadata timestamp indexes, then raise INITRANS
	// on METADATA and its survivors (Oracle only) -- same placement and
	// reasoning as cmd/server/main.go's migrateSchema and
	// cmd/dbtool/schema.go's runSchemaLocked. Both are idempotent, run even
	// when the fast path above skipped AutoMigrate, and never fail on DDL.
	if err := dbschema.DropRetiredMetadataIndexes(gormDB.DB()); err != nil {
		return fmt.Errorf("failed to check the retired metadata indexes: %w", err)
	}
	if err := dbschema.EnsureMetadataInitrans(gormDB.DB()); err != nil {
		return fmt.Errorf("failed to check the metadata INITRANS settings: %w", err)
	}
```

Leave the `// SEM@ebd32e78...` marker on `migrateSchemaForConfigAdapter` unchanged.

- [ ] **Step 3: Wire `cmd/dbtool/schema.go`**

In `runSchemaLocked`, directly after the closing brace of the `if err := dbschema.EnsureSparseUserEmailIndex(db.DB()); err != nil { ... }` block (line 131) and before the blank line preceding `// #813: remove system_settings rows` (line 133), insert:

```go

	// #784 / #783: retire the metadata timestamp indexes, then raise INITRANS
	// on METADATA and its survivors (Oracle only). dbtool is the
	// admin-privileged remediation path, so this is the entry point most
	// likely to succeed at the MOVE ONLINE / REBUILD ONLINE where a DDL-less
	// server runtime user can only log that it is needed. Same placement as
	// cmd/server/main.go's migrateSchema and auth/config_adapter.go.
	if err := dbschema.DropRetiredMetadataIndexes(db.DB()); err != nil {
		return fmt.Errorf("failed to check the retired metadata indexes: %w", err)
	}
	if err := dbschema.EnsureMetadataInitrans(db.DB()); err != nil {
		return fmt.Errorf("failed to check the metadata INITRANS settings: %w", err)
	}
```

Leave the `// SEM@5abf61d0...` marker on `runSchemaLocked` unchanged.

- [ ] **Step 4: Build, unit tests, lint**

Run: `make build-server`, `make test-unit`, `make lint`.
Expected: build OK, all PASS, lint clean.

- [ ] **Step 5: PostgreSQL integration tests**

Run: `make test-integration`
Expected: PASS. In `logs/tmi.log` from that run, confirm the four `dropped retired metadata index ... (#784)` lines appear exactly once (the isolated test container was migrated by the pre-change model, so the indexes existed) and that no `(#783)` ERROR line appears (`EnsureMetadataInitrans` is a no-op on PostgreSQL). If the test container was provisioned fresh with the new model, the drop lines are absent and that is also correct.

- [ ] **Step 6: Commit (the resolving commit)**

```bash
git add cmd/server/main.go auth/config_adapter.go cmd/dbtool/schema.go
git commit -m "fix(db): retire metadata timestamp indexes and raise METADATA INITRANS to 16

Wires DropRetiredMetadataIndexes and EnsureMetadataInitrans into the
unconditional post-AutoMigrate tail of all three migration entry points
(server boot, auth config adapter, tmi-dbtool --schema), drop first so
retired indexes are never rebuilt. Both run under the migration advisory
lock, are idempotent, and log rather than fail on DDL errors.

INITRANS 16 on METADATA and its six surviving indexes closes the ITL-slot
recycling that produced a false ORA-08177 on back-to-back same-entity
metadata writes under SERIALIZABLE; dropping the four timestamp-leading
indexes removes the single hot leaf block every same-batch row landed on.

Fixes #783
Fixes #784

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 7: OCI integration test probe

**Files:**
- Modify: `api/metadata_bulk_oracle_test.go` (imports at lines 5-15; append a subtest inside `TestMetadataBulkWriteOracleIntegration` after the `BulkReplace of 100 entries` subtest that ends at line 194)

**Interfaces:**
- Consumes: `dbschema.DropRetiredMetadataIndexes`, `dbschema.EnsureMetadataInitrans`; the test-local `db` opened by `openAuditAppendOnlyOracleDB` (`api/audit_append_only_oracle_test.go:29`, same package, `oracle` build tag).

- [ ] **Step 1: Add the import**

In the import block of `api/metadata_bulk_oracle_test.go`, add `"github.com/ericfitz/tmi/internal/dbschema"` after `"github.com/ericfitz/tmi/api/models"` (internal group; keep the file gofmt-grouped).

- [ ] **Step 2: Append the subtest**

Insert before the final closing `}` of `TestMetadataBulkWriteOracleIntegration` (after line 194):

```go

	t.Run("INITRANS >= 16 and retired timestamp indexes absent (#783, #784)", func(t *testing.T) {
		// Drive the two ensure-steps directly so the assertion does not
		// depend on which build last migrated the shared ADB, then call
		// them again to pin idempotence. Both must return nil on a
		// catalog-readable database whatever the DDL outcome; the probes
		// below are what decide pass/fail.
		require.NoError(t, dbschema.DropRetiredMetadataIndexes(db))
		require.NoError(t, dbschema.EnsureMetadataInitrans(db))
		require.NoError(t, dbschema.DropRetiredMetadataIndexes(db), "must be idempotent")
		require.NoError(t, dbschema.EnsureMetadataInitrans(db), "must be idempotent")

		// Same CURRENT_SCHEMA scoping as the production probes (#736);
		// spelled out rather than imported so the test asserts against what
		// Oracle holds, independently of the code under test.
		const owner = "SYS_CONTEXT('USERENV','CURRENT_SCHEMA')"

		var tableIni []int64
		require.NoError(t, db.Raw(
			"SELECT INI_TRANS FROM ALL_TABLES WHERE TABLE_NAME = 'METADATA' AND OWNER = "+owner,
		).Scan(&tableIni).Error)
		require.Len(t, tableIni, 1, "METADATA must exist in CURRENT_SCHEMA")
		assert.GreaterOrEqual(t, tableIni[0], int64(16), "METADATA INI_TRANS (#783)")

		var pk []string
		require.NoError(t, db.Raw(
			"SELECT INDEX_NAME FROM ALL_CONSTRAINTS WHERE TABLE_NAME = 'METADATA' AND CONSTRAINT_TYPE = 'P' "+
				"AND INDEX_NAME IS NOT NULL AND OWNER = "+owner,
		).Scan(&pk).Error)
		require.Len(t, pk, 1, "METADATA must have exactly one primary-key constraint backed by an index")

		survivors := append([]string{pk[0]},
			"IDX_METADATA_UNIQUE", "IDX_METADATA_ENTITY_TYPE_ID", "IDX_METADATA_ENTITY_ID",
			"IDX_METADATA_KEY", "IDX_METADATA_KEY_VALUE")
		var rows []struct {
			IndexName string
			IniTrans  int64
		}
		require.NoError(t, db.Raw(
			"SELECT INDEX_NAME, INI_TRANS FROM ALL_INDEXES WHERE TABLE_NAME = 'METADATA' AND INDEX_NAME IN ? "+
				"AND OWNER = "+owner+" AND TABLE_OWNER = "+owner,
			survivors,
		).Scan(&rows).Error)
		got := make(map[string]int64, len(rows))
		for _, r := range rows {
			got[r.IndexName] = r.IniTrans
		}
		for _, name := range survivors {
			ini, ok := got[name]
			assert.True(t, ok, "surviving index %s must exist on METADATA", name)
			assert.GreaterOrEqual(t, ini, int64(16), "%s INI_TRANS (#783)", name)
		}

		retired := []string{"IDX_METADATA_CREATED", "IDX_METADATA_MODIFIED", "IDX_METADATA_ENTITY_CREATED", "IDX_METADATA_ENTITY_MODIFIED"}
		var leftover []string
		require.NoError(t, db.Raw(
			"SELECT INDEX_NAME FROM ALL_INDEXES WHERE TABLE_NAME = 'METADATA' AND INDEX_NAME IN ? "+
				"AND OWNER = "+owner+" AND TABLE_OWNER = "+owner,
			retired,
		).Scan(&leftover).Error)
		assert.Empty(t, leftover, "retired timestamp indexes must be gone from METADATA (#784)")
	})
```

Also update the doc comment above `TestMetadataBulkWriteOracleIntegration` by adding one bullet after the `BulkReplace with 100 entries` bullet (before the blank `//` line that precedes `// Run via`):

```go
//   - The final subtest drives DropRetiredMetadataIndexes and
//     EnsureMetadataInitrans against the live catalog and asserts INI_TRANS
//     >= 16 on METADATA and its six surviving indexes and that the four
//     retired timestamp indexes are absent (#783, #784 acceptance).
```

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: clean. (The file is behind the `oracle` build tag; if the lint target does not compile it, `make test-integration-oci` in the next step is the compile check.)

- [ ] **Step 4: Run the Oracle integration suite**

Prerequisites: `scripts/oci-env.sh` present (it sources the ADB password from `~/.keys/ORACLE_PASSWORD`; never inspect that file) and a running Oracle-backed server: `make dev-up DB=oracle` built from this branch, so the server boot itself exercises the wiring from Task 6 against the live ADB.

Run: `make test-integration-oci`
Expected: PASS, including `TestMetadataBulkWriteOracleIntegration/INITRANS_>=_16_and_retired_timestamp_indexes_absent_(#783,_#784)`. Read `logs/tmi.log` for the server boot: expect four `dropped retired metadata index ... (#784)` INFO lines, one `raising INITRANS to 16 on METADATA ...` INFO line, and one `INITRANS on METADATA and its 6 surviving indexes is now >= 16 (#783)` INFO line on the first boot, and neither on a second boot. Any `(#783)` or `(#784)` ERROR line is a failure to investigate, not to work around: capture the Oracle error text for Task 8.

If the Always Free ADB is auto-stopped, start it first; if `make test-integration-oci` fails on `ORA-00054` during MOVE ONLINE or REBUILD ONLINE, rerun once (a concurrent session from the dev server holding a DML lock), then treat a second failure as real.

- [ ] **Step 5: Commit**

```bash
git add api/metadata_bulk_oracle_test.go
git commit -m "test(oracle): assert METADATA INITRANS >= 16 and retired indexes absent

Drives DropRetiredMetadataIndexes and EnsureMetadataInitrans against the
live ADB inside TestMetadataBulkWriteOracleIntegration, then probes
ALL_TABLES, ALL_CONSTRAINTS, and ALL_INDEXES under CURRENT_SCHEMA for the
#783 / #784 acceptance criteria.

Refs #783, #784

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 8: Oracle compatibility review (mandatory)

**Files:**
- Review target: the full branch diff, `git diff main...dev/1.9.16/metadata-initrans`.

- [ ] **Step 1: Dispatch the reviewer**

Use the Agent tool with `subagent_type: "oracle-db-admin"` and this prompt (fill in nothing; it is complete):

```
Review the full diff of branch dev/1.9.16/metadata-initrans against main in /Users/efitz/Projects/tmi (`git diff main...dev/1.9.16/metadata-initrans`). Design: docs/superpowers/specs/2026-09-04-metadata-initrans-design.md. New code: internal/dbschema/metadata_index.go (DropRetiredMetadataIndexes, EnsureMetadataInitrans, metadataInitransProbe), model change in api/models/models.go (Metadata), wiring in cmd/server/main.go, auth/config_adapter.go, cmd/dbtool/schema.go, OCI test in api/metadata_bulk_oracle_test.go. Target: Oracle ADB 23ai (tmiadb). Every DDL statement goes through execMigrationDDL (pinned *sql.Conn, DDL_LOCK_TIMEOUT=30) under withDDLRetry; catalog probes use ALL_* views scoped to SYS_CONTEXT('USERENV','CURRENT_SCHEMA').

Answer these explicitly, then give the verdict (APPROVED / APPROVED WITH NOTES / BLOCKING ISSUES) with numbered findings:
1. Is `ALTER TABLE METADATA MOVE ONLINE` supported on ADB 23ai, and does it keep the table's indexes VALID (index-maintaining) rather than marking them UNUSABLE? If not, what is the correct statement or sequence?
2. Is INITRANS 16 the right value for a table with ~1KB rows and 100-row batched inserts by up to a handful of concurrent writers, or should it be 8 or higher? Consider the 24-byte-per-slot block cost.
3. Is the primary-key index discovery query correct: `SELECT INDEX_NAME FROM ALL_CONSTRAINTS WHERE TABLE_NAME = 'METADATA' AND CONSTRAINT_TYPE = 'P' AND INDEX_NAME IS NOT NULL AND OWNER = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')`? Are there cases on gorm-oracle-created tables where INDEX_NAME is NULL or the index owner differs?
4. Does `ALTER INDEX ... REBUILD ONLINE INITRANS 16` need the DDL_LOCK_TIMEOUT that execMigrationDDL sets, and can REBUILD ONLINE or MOVE ONLINE block, or be blocked by, the server's own concurrent metadata DML during a rolling deploy in a way withDDLRetry (4 attempts, ORA-00054 retried) does not cover?
5. Would a single `ALTER TABLE METADATA MOVE ONLINE INITRANS 16` be preferable to the two-statement sequence, and does the two-statement form leave any window where new blocks are formatted at INITRANS 1?
6. Does the read of INI_TRANS from ALL_TABLES / ALL_INDEXES reflect the post-MOVE / post-REBUILD value immediately (so the re-probe is trustworthy), and is `IN ?` with a bound list safe for these catalog views under godror?
```

- [ ] **Step 2: Act on the verdict**

- `APPROVED`: note it in the PR body and continue to Task 9.
- `APPROVED WITH NOTES`: fix every easy item now in the affected file, rerun the affected unit tests with `make test-unit name=TestMetadata count1=true passfail=true`, `make lint`, and commit as `fix(dbschema): address oracle-db-admin review notes for metadata INITRANS` with the standard trailers; file a follow-up issue for anything deferred and name it in the PR body.
- `BLOCKING ISSUES`: fix every item (rerunning `make test-integration-oci` if the DDL text or probe changed), commit, and re-dispatch the reviewer with the same prompt plus a line naming what changed. Do not argue with a finding; if one seems wrong, stop and ask the user to adjudicate.

---

### Task 9: Final gates, push, and pull request

**Files:**
- Modify (only if a checkpoint is needed): `HANDOFF.md` (untracked, never committed)

- [ ] **Step 1: Run the full gate set**

Run in order: `make lint`, `make build-server`, `make test-unit`, `make test-integration`.
Expected: all PASS. If any fails, fix the cause, commit with a `fix(...)` message and the standard trailers, and rerun from `make lint`.

- [ ] **Step 2: Refresh SEM markers on the touched files**

Run: `/sem-annotate --update internal/dbschema/metadata_index.go internal/dbschema/metadata_index_test.go api/models/models.go cmd/server/main.go auth/config_adapter.go cmd/dbtool/schema.go api/metadata_bulk_oracle_test.go`
Expected: only entities that are new or whose logic changed are (re)written; the hand-written `SEM@2e43fddc...` markers from the tasks above are accepted or normalized. If the tool changes anything, `make lint`, then commit as `chore(sem): refresh intent markers for metadata INITRANS change` with the standard trailers.

- [ ] **Step 3: Security review**

Invoke the `security-review` skill on the branch. Expected: no findings (the change is startup DDL built from in-package constants; no user input reaches a statement). If it reports issues, stop, report them, and ask the user what to do.

- [ ] **Step 4: Push**

```bash
git pull --rebase origin main
git push -u origin dev/1.9.16/metadata-initrans
git status
```

Expected: `Your branch is up to date with 'origin/dev/1.9.16/metadata-initrans'`. If push fails on SSH, the user has not touched the Touch ID prompt yet; wait and rerun the same command.

- [ ] **Step 5: Open the PR**

The PR title drives the automatic version bump (`fix:` gives PATCH: 1.9.15 to 1.9.16).

```bash
gh pr create --base main --head dev/1.9.16/metadata-initrans \
  --title "fix(db): retire metadata timestamp indexes and raise METADATA INITRANS to 16" \
  --body "$(cat <<'EOF'
## Summary

- Removes the four timestamp-leading indexes (`idx_metadata_created`, `idx_metadata_modified`, `idx_metadata_entity_created`, `idx_metadata_entity_modified`) from `models.Metadata`; nothing reads metadata by timestamp, and they concentrated every same-batch insert on one leaf block.
- New ensure-step `DropRetiredMetadataIndexes` removes them from existing databases (PostgreSQL `DROP INDEX IF EXISTS`; Oracle `ALL_INDEXES` probe under `CURRENT_SCHEMA` then `DROP INDEX` through `execMigrationDDL`; SQLite no-op).
- New Oracle-only ensure-step `EnsureMetadataInitrans` raises INITRANS to 16 on `METADATA` and its six surviving indexes (`ALTER TABLE ... INITRANS 16`, `ALTER TABLE ... MOVE ONLINE`, `ALTER INDEX ... REBUILD ONLINE INITRANS 16`), re-probing the catalog afterwards and logging rather than failing on DDL errors.
- Both wired into the unconditional post-AutoMigrate tail at all three call sites (server boot, auth config adapter, `tmi-dbtool --schema`), drop first.
- OCI integration test asserts `INI_TRANS >= 16` on the table and every surviving index and that the retired indexes are absent.

Design: `docs/superpowers/specs/2026-09-04-metadata-initrans-design.md`. Plan: `docs/superpowers/plans/2026-09-04-metadata-initrans.md`.

Note: the model fingerprint changes, so the first boot after deploy runs one full AutoMigrate pass and re-stamps.

Oracle review: <verdict from Task 8, with any follow-up issue numbers>.

Fixes #783
Fixes #784

## Test plan

- [ ] `make lint`, `make build-server`, `make test-unit`
- [ ] `make test-integration` (PostgreSQL)
- [ ] `make test-integration-oci` against tmiadb, including the new INITRANS subtest
- [ ] Server boot log against Oracle shows the drop and INITRANS lines once, then silence on the next boot

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv
EOF
)"
```

Replace the `<verdict from Task 8 ...>` placeholder with the actual verdict text before running the command.

- [ ] **Step 6: Record progress**

Append one line to `PROGRESS.md` under the current date describing the pushed branch and PR number, and update `HANDOFF.md` (untracked) with the PR URL and "awaiting merge; #783 and #784 close automatically on squash-merge to main". Commit and push `PROGRESS.md` only:

```bash
git add PROGRESS.md
git commit -m "docs(progress): record metadata INITRANS branch and PR

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
git push
git status
```

Expected: up to date with origin.

---

## Self-review

**Spec coverage.**
- Spec item 1 (drop four indexes: model tags, `DropRetiredMetadataIndexes`, PG `DROP INDEX IF EXISTS`, Oracle `ALL_INDEXES` probe under `CURRENT_SCHEMA` then `execMigrationDDL`): Tasks 2, 3, 4.
- Spec item 2 (`EnsureMetadataInitrans`, target 16, probe `ALL_TABLES.INI_TRANS` / `ALL_INDEXES.INI_TRANS`, PK discovery, skip at target, the three DDL forms, re-probe, log ERROR and return nil, `execMigrationDDL` / `withDDLRetry`): Task 5.
- Spec item 3 (wired at all three call sites in the unconditional tail, drop before INITRANS): Task 6.
- Spec item 4 (OCI test probe; no dbtool command; BulkReplace test unchanged): Task 7.
- Spec item 5 (not doing): no task touches other tables or `saveEntityMetadata`.
- Spec item 6 (Oracle review, `MOVE ONLINE` question): Task 8.
- Unit tests on SQLite for the no-op paths and pure DDL-string tests: Tasks 2, 4, 5.

**Placeholder scan.** The only intentional fill-in is the review verdict in the PR body (Task 9 Step 5), which cannot be known before Task 8 runs and is called out explicitly.

**Type consistency.** `metadataInitransState.Indexes` is `map[string]int64` in the type (Task 5), the `below()` test (Task 5), and the probe (Task 5). `metadataIndexExists(db, indexName, tableName string) (bool, error)` is used with that signature in Tasks 2, 4. `retiredMetadataIndexDropDDL(dialect, indexName string) string`, `metadataTableInitransDDL(upperTable string) []string`, `metadataIndexInitransDDL(upperIndex string) string` match between Task 2's tests, Task 2's implementation, and Task 5's callers. The exported names `DropRetiredMetadataIndexes` and `EnsureMetadataInitrans` are spelled identically in Tasks 4, 5, 6, 7, and 8.
