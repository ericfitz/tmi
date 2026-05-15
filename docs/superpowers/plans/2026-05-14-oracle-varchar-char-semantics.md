# Oracle VARCHAR2 BYTE-vs-CHAR Semantics — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Achieve cross-database length-semantics parity between PostgreSQL and Oracle ADB so that user-supplied multi-byte UTF-8 content (emoji, CJK, accented Latin) cannot trigger `ORA-12899: value too large for column` errors that don't reproduce on PostgreSQL.

**Architecture:** Introduce a new `DBVarchar` / `NullableDBVarchar` cross-dialect GORM type emitting `VARCHAR(N)` on PostgreSQL and `VARCHAR2(N CHAR)` on Oracle. Convert indexed, filtered, enum-like, and identifier columns to it. Migrate large free-text columns (descriptions, verbatim, URLs, audit paths) to the existing `DBText` / `NullableDBText` type (TEXT on PG, CLOB on Oracle). Six batches, one PR each.

**Tech Stack:** Go, GORM, gorm-oracle, PostgreSQL, Oracle ADB, oapi-codegen.

**Spec:** [docs/superpowers/specs/2026-05-14-oracle-varchar-char-semantics-design.md](../specs/2026-05-14-oracle-varchar-char-semantics-design.md)

**Issue:** [#379](https://github.com/ericfitz/tmi/issues/379)

---

## File Structure

### Files created across the plan

- `api/models/types.go` — modified (add `DBVarchar` + `NullableDBVarchar` types)
- `api/models/types_test.go` — modified or created (unit tests for the new types)
- `scripts/oracle-migrate-varchar-char.sql` — created only if Batch 0 verification fails
- `scripts/verify-oracle-char-semantics.sql` — created (post-deploy schema verification query)

### Model files modified across batches 1–5

All under `api/models/`:

- `audit.go`
- `models.go`
- `survey_models.go`
- `system_audit.go`
- `system_setting.go`
- `team_project_models.go`
- `team_project_note_models.go`
- `timmy.go`
- `user_content_token.go`

Each batch touches a subset of these files; see batch-specific field maps below.

### Files NOT touched

- `auth/migrations/` — directory does not exist (see #405)
- `auth/db/gorm.go`, `auth/db/gorm_oracle.go` — no dialect-setup changes needed
- `internal/dberrors/` — error classification already handles ORA-12899 correctly
- `api-schema/tmi-openapi.json` — OpenAPI `maxLength` constraints remain; characters not bytes

---

## Batch 0: Foundation — Add `DBVarchar` types and verify GORM detection

**Purpose:** Land the new types with tests, then verify (on a real Oracle ADB instance) whether GORM's column-type comparison detects the BYTE-vs-CHAR semantic difference. The verification outcome determines whether subsequent batches need a sidecar SQL migration script.

**Risk:** Low. No model fields change. The type is dormant until a field declaration references it.

### Status (post-execution)

Batch 0 has been executed. Summary for plan readers:

- ✅ Task 0.1 — `DBVarchar`/`NullableDBVarchar` added with tests (commits 10f24b09, 106b8107)
- ✅ Task 0.2 — `scripts/verify-oracle-char-semantics.sql` added (commit 569ae35e)
- ✅ Task 0.3 — Detection probe added and run against fresh OCI ADB (commit 8d54f802). **Verdict: GORM does NOT detect the BYTE→CHAR diff.** Confirmed via `USER_TAB_COLUMNS.CHAR_USED` staying `'B'` after switching a field from `gorm:"type:varchar(256)"` to `DBVarchar size:256`.
- ✅ Task 0.4 — Sidecar SQL migration `scripts/oracle-migrate-varchar-char.sql` added (commit 7f5cb9c2). **Required (not conditional)** for every Oracle ADB deploy of Batches 1–5 against any existing schema.
- ⏳ Task 0.5 — PR pending.

### Side-issues filed and fixed during Batch 0

The probe exercise required provisioning a fresh OCI ADB, which surfaced two pre-existing Oracle compatibility bugs masked by AutoMigrate's diff path on existing schemas:

- **#406** (commit 083cc11e) — Three `gorm:"type:text"` fields emitting literal `TEXT` in Oracle DDL, failing AutoMigrate with `ORA-00902` on a fresh schema. Converted to `DBText`/`NullableDBText`.
- **#407** (commit 50efaa2a) — Two composite indexes (`idx_timmy_sessions_tm_user`, `idx_audit_object_version`) declared without their priority-1 leading-column tags, emitting as single-column indexes. `idx_timmy_sessions_tm_user` collided with `idx_timmy_sessions_user` on Oracle (`ORA-01408`). Fixed by adding the missing priority declarations on `ThreatModelID` and `ObjectType`/`ObjectID`.

Both side-issues remain open until the dev/1.4.0 branch lands on main (per CLAUDE.md, `Fixes #N` trailers don't auto-close from feature branches).

### Task 0.1: Add `DBVarchar` and `NullableDBVarchar` types

**Files:**
- Modify: `api/models/types.go` (append after `NewNullableDBText` at line 544)

- [ ] **Step 1: Write the failing test**

Create `api/models/dbvarchar_test.go`:

```go
package models

import (
	"database/sql/driver"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestDBVarchar_GormDBDataType_Postgres(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Config.Dialector = mockDialector{name: dialectPostgres}
	field := &schema.Field{Size: 256}
	got := DBVarchar("").GormDBDataType(db, field)
	want := "varchar(256)"
	if got != want {
		t.Fatalf("GormDBDataType(postgres, size=256) = %q, want %q", got, want)
	}
}

func TestDBVarchar_GormDBDataType_Oracle(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Config.Dialector = mockDialector{name: dialectOracle}
	field := &schema.Field{Size: 256}
	got := DBVarchar("").GormDBDataType(db, field)
	want := "varchar2(256 char)"
	if got != want {
		t.Fatalf("GormDBDataType(oracle, size=256) = %q, want %q", got, want)
	}
}

func TestDBVarchar_GormDBDataType_DefaultSizeWhenUnset(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Config.Dialector = mockDialector{name: dialectOracle}
	field := &schema.Field{Size: 0}
	got := DBVarchar("").GormDBDataType(db, field)
	want := "varchar2(255 char)"
	if got != want {
		t.Fatalf("GormDBDataType(oracle, size=0) = %q, want %q (default 255)", got, want)
	}
}

func TestDBVarchar_Scan(t *testing.T) {
	var v DBVarchar
	if err := v.Scan("hello"); err != nil {
		t.Fatalf("Scan(string): %v", err)
	}
	if string(v) != "hello" {
		t.Fatalf("Scan(string): got %q, want %q", string(v), "hello")
	}
	v = ""
	if err := v.Scan([]byte("world")); err != nil {
		t.Fatalf("Scan([]byte): %v", err)
	}
	if string(v) != "world" {
		t.Fatalf("Scan([]byte): got %q, want %q", string(v), "world")
	}
	v = "stale"
	if err := v.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if string(v) != "" {
		t.Fatalf("Scan(nil): got %q, want empty", string(v))
	}
}

func TestDBVarchar_Value(t *testing.T) {
	v := DBVarchar("hello")
	got, err := v.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got != driver.Value("hello") {
		t.Fatalf("Value: got %v, want %q", got, "hello")
	}
}

func TestNullableDBVarchar_ValidScan(t *testing.T) {
	var v NullableDBVarchar
	if err := v.Scan("hello"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !v.Valid || v.String != "hello" {
		t.Fatalf("Scan(string): got {%q, %v}, want {%q, true}", v.String, v.Valid, "hello")
	}
}

func TestNullableDBVarchar_NilScan(t *testing.T) {
	v := NullableDBVarchar{String: "stale", Valid: true}
	if err := v.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if v.Valid || v.String != "" {
		t.Fatalf("Scan(nil): got {%q, %v}, want {\"\", false}", v.String, v.Valid)
	}
}

func TestNullableDBVarchar_Value(t *testing.T) {
	v := NullableDBVarchar{String: "hi", Valid: true}
	got, err := v.Value()
	if err != nil {
		t.Fatalf("Value(valid): %v", err)
	}
	if got != driver.Value("hi") {
		t.Fatalf("Value(valid): got %v, want %q", got, "hi")
	}

	v = NullableDBVarchar{Valid: false}
	got, err = v.Value()
	if err != nil {
		t.Fatalf("Value(invalid): %v", err)
	}
	if got != nil {
		t.Fatalf("Value(invalid): got %v, want nil", got)
	}
}

func TestNullableDBVarchar_Ptr(t *testing.T) {
	v := NullableDBVarchar{String: "hi", Valid: true}
	p := v.Ptr()
	if p == nil || *p != "hi" {
		t.Fatalf("Ptr(valid): got %v, want pointer to %q", p, "hi")
	}

	v = NullableDBVarchar{Valid: false}
	p = v.Ptr()
	if p != nil {
		t.Fatalf("Ptr(invalid): got %v, want nil", p)
	}
}

func TestNewNullableDBVarchar(t *testing.T) {
	s := "hello"
	v := NewNullableDBVarchar(&s)
	if !v.Valid || v.String != "hello" {
		t.Fatalf("NewNullableDBVarchar(&\"hello\"): got {%q, %v}", v.String, v.Valid)
	}

	v = NewNullableDBVarchar(nil)
	if v.Valid || v.String != "" {
		t.Fatalf("NewNullableDBVarchar(nil): got {%q, %v}", v.String, v.Valid)
	}
}
```

If `mockDialector` does not already exist in the test package, check whether other `types_test.go` files in `api/models/` define a similar helper and reuse it. If none exists, add this minimal helper at the bottom of `dbvarchar_test.go`:

```go
// mockDialector is a minimal gorm.Dialector that only implements Name() for tests.
type mockDialector struct{ name string }

func (m mockDialector) Name() string                                                   { return m.name }
func (m mockDialector) Initialize(*gorm.DB) error                                      { return nil }
func (m mockDialector) Migrator(*gorm.DB) gorm.Migrator                                { return nil }
func (m mockDialector) DataTypeOf(*schema.Field) string                                { return "" }
func (m mockDialector) DefaultValueOf(*schema.Field) clause.Expression                 { return nil }
func (m mockDialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v any)    {}
func (m mockDialector) QuoteTo(clause.Writer, string)                                  {}
func (m mockDialector) Explain(sql string, vars ...any) string                         { return sql }
```

If reusing an existing helper, drop this block and adjust imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestDBVarchar`

Expected: FAIL with `undefined: DBVarchar` and `undefined: NullableDBVarchar` and `undefined: NewNullableDBVarchar`.

- [ ] **Step 3: Implement `DBVarchar` and `NullableDBVarchar`**

Append to `api/models/types.go` (after the existing `NewNullableDBText` function around line 544):

```go
// DBVarchar is a cross-database length-bounded text type with CHAR semantics.
// Uses varchar(N) on PostgreSQL (already char-counted), varchar2(N CHAR) on
// Oracle (avoiding default BYTE semantics under AL32UTF8), varchar(N) on
// MySQL (utf8mb4 is char-counted), nvarchar(N) on SQL Server, varchar(N) on
// SQLite. The length N is carried by the GORM `size:` tag, not by the Go type.
type DBVarchar string

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility.
// The column size is read from field.Size (populated from the `size:` GORM tag).
// A field.Size of 0 falls back to 255 as a safety default; every usage site
// should set size: explicitly.
func (DBVarchar) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	n := field.Size
	if n <= 0 {
		n = 255
	}
	switch db.Name() {
	case dialectPostgres:
		return fmt.Sprintf("varchar(%d)", n)
	case dialectOracle:
		return fmt.Sprintf("varchar2(%d char)", n)
	case dialectMySQL:
		return fmt.Sprintf("varchar(%d)", n)
	case dialectSQLServer:
		return fmt.Sprintf("nvarchar(%d)", n)
	case dialectSQLite:
		return fmt.Sprintf("varchar(%d)", n)
	default:
		return fmt.Sprintf("varchar(%d)", n)
	}
}

// Scan implements the sql.Scanner interface for database reads.
func (v *DBVarchar) Scan(value any) error {
	if value == nil {
		*v = ""
		return nil
	}
	switch s := value.(type) {
	case []byte:
		*v = DBVarchar(s)
	case string:
		*v = DBVarchar(s)
	default:
		return fmt.Errorf("cannot scan type %T into DBVarchar", value)
	}
	return nil
}

// Value implements the driver.Valuer interface for database writes.
func (v DBVarchar) Value() (driver.Value, error) {
	return string(v), nil
}

// String returns the underlying string value.
func (v DBVarchar) String() string {
	return string(v)
}

// NullableDBVarchar is a nullable cross-database length-bounded text type with
// CHAR semantics. Wraps a string with a Valid flag for NULL handling.
// Maps to the same column types as DBVarchar per dialect.
type NullableDBVarchar struct {
	String string
	Valid  bool
}

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility.
func (NullableDBVarchar) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	n := field.Size
	if n <= 0 {
		n = 255
	}
	switch db.Name() {
	case dialectPostgres:
		return fmt.Sprintf("varchar(%d)", n)
	case dialectOracle:
		return fmt.Sprintf("varchar2(%d char)", n)
	case dialectMySQL:
		return fmt.Sprintf("varchar(%d)", n)
	case dialectSQLServer:
		return fmt.Sprintf("nvarchar(%d)", n)
	case dialectSQLite:
		return fmt.Sprintf("varchar(%d)", n)
	default:
		return fmt.Sprintf("varchar(%d)", n)
	}
}

// Scan implements the sql.Scanner interface for database reads.
func (v *NullableDBVarchar) Scan(value any) error {
	if value == nil {
		v.String, v.Valid = "", false
		return nil
	}
	v.Valid = true
	switch s := value.(type) {
	case []byte:
		v.String = string(s)
	case string:
		v.String = s
	default:
		return fmt.Errorf("cannot scan type %T into NullableDBVarchar", value)
	}
	return nil
}

// Value implements the driver.Valuer interface for database writes.
func (v NullableDBVarchar) Value() (driver.Value, error) {
	if !v.Valid {
		return nil, nil
	}
	return v.String, nil
}

// Ptr returns a pointer to the string, or nil if not valid.
func (v NullableDBVarchar) Ptr() *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// NewNullableDBVarchar creates a NullableDBVarchar from a string pointer.
func NewNullableDBVarchar(s *string) NullableDBVarchar {
	if s == nil {
		return NullableDBVarchar{Valid: false}
	}
	return NullableDBVarchar{String: *s, Valid: true}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestDBVarchar`

Expected: PASS. All TestDBVarchar_* and TestNullableDBVarchar_* and TestNewNullableDBVarchar tests green.

- [ ] **Step 5: Run lint and full unit suite**

Run: `make lint && make test-unit`

Expected: PASS. No new lint issues, no broken existing tests.

- [ ] **Step 6: Commit**

```bash
git add api/models/types.go api/models/dbvarchar_test.go
git commit -m "$(cat <<'EOF'
feat(models): add DBVarchar and NullableDBVarchar cross-dialect types

Emits varchar(N) on PostgreSQL and varchar2(N CHAR) on Oracle ADB,
avoiding the default VARCHAR2 BYTE semantics that caused cross-DB
length-validation inconsistency under AL32UTF8 multi-byte content.

Length carried via GORM size: tag, mirroring the existing DBText
and DBBytes patterns in types.go.

Foundation for #379 (Batch 0 of 6). No model fields converted yet.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

### Task 0.2: Author Oracle schema verification script

**Files:**
- Create: `scripts/verify-oracle-char-semantics.sql`

- [ ] **Step 1: Write the verification query**

Create `scripts/verify-oracle-char-semantics.sql`:

```sql
-- verify-oracle-char-semantics.sql
-- Reports VARCHAR2 columns that are still in BYTE semantics mode after the
-- #379 remediation. Expected result after all batches: empty result set.
-- Any rows returned indicate columns that need investigation.
--
-- Run as the TMI app schema owner on Oracle ADB.

SELECT
    TABLE_NAME,
    COLUMN_NAME,
    DATA_LENGTH,
    CHAR_LENGTH,
    CHAR_USED,
    NULLABLE
FROM
    USER_TAB_COLUMNS
WHERE
    DATA_TYPE = 'VARCHAR2'
    AND CHAR_USED = 'B'
ORDER BY
    TABLE_NAME,
    COLUMN_NAME;
```

- [ ] **Step 2: Commit**

```bash
git add scripts/verify-oracle-char-semantics.sql
git commit -m "$(cat <<'EOF'
chore(scripts): add Oracle CHAR-semantics verification query

Reports VARCHAR2 columns still in BYTE mode. Run on Oracle ADB after
each batch of #379 deploys to confirm the migration is taking effect.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

### Task 0.3: GORM detection verification on Oracle ADB

**Purpose:** Empirically confirm whether GORM's column-type diff detects `VARCHAR2(N)` vs `VARCHAR2(N CHAR)` and issues the `ALTER ... MODIFY`. This is a one-time verification — the outcome determines whether a sidecar SQL migration script is required for batches 1–3.

**Files:**
- Modify: `api/models/models.go` (temporary one-field test; reverted before commit)

- [ ] **Step 1: Start with a clean Oracle ADB integration test environment**

Run: `make test-integration-oci`

Expected: PASS. All existing tests green against Oracle ADB. This confirms the baseline schema is BYTE-mode (the existing state).

- [ ] **Step 2: Connect to the Oracle ADB test instance and capture baseline**

Use the OCI test instance referenced by `scripts/oci-env.sh`. Run `scripts/verify-oracle-char-semantics.sql` against it (via `sqlplus` or `sql` CLI, or via a temporary `make` target).

Capture the row count and at least one representative row (e.g., for `THREATS.NAME`). Expected: many rows, all `CHAR_USED = 'B'`.

- [ ] **Step 3: Make a single-field test change**

In `api/models/models.go`, locate the `Metadata` struct's `Key` field:

```go
Key string `gorm:"type:varchar(256);not null;index:idx_metadata_key;index:idx_metadata_unique,priority:3;index:idx_metadata_key_value,priority:2"`
```

Temporarily change to:

```go
Key DBVarchar `gorm:"size:256;not null;index:idx_metadata_key;index:idx_metadata_unique,priority:3;index:idx_metadata_key_value,priority:2"`
```

This is the smallest possible model edit — one field — to trigger GORM's AutoMigrate column-type comparison.

- [ ] **Step 4: Build and run integration tests against Oracle ADB**

Run: `make build-server && make test-integration-oci`

Expected: builds (may surface compile errors at consumer sites for `Metadata.Key`; if so, fix them inline with `string(meta.Key)` conversions in the affected store/handler code). Tests should pass if GORM applies the migration; observe migration log output.

- [ ] **Step 5: Re-run the verification script and capture the result**

Run `scripts/verify-oracle-char-semantics.sql` against the same Oracle ADB instance.

**Decision point:**
- **`METADATA.KEY` no longer appears in the result set (CHAR_USED = 'C'):** GORM is honoring the BYTE-vs-CHAR semantic difference. Proceed to Step 6 (revert) and then Batch 1 as designed. **No sidecar SQL script needed.**
- **`METADATA.KEY` still appears with CHAR_USED = 'B':** GORM is normalizing the type string and skipping the `ALTER`. Proceed to Task 0.4 to author the sidecar script before continuing.

Record the outcome in a comment on issue #379 so subsequent batches reference it.

- [ ] **Step 6: Revert the test change**

Restore the original `Metadata.Key` declaration in `api/models/models.go`:

```go
Key string `gorm:"type:varchar(256);not null;index:idx_metadata_key;index:idx_metadata_unique,priority:3;index:idx_metadata_key_value,priority:2"`
```

Revert any compile-fix consumer-site changes from Step 4.

Run: `make build-server && make test-unit`

Expected: PASS. Working tree shows no changes after revert.

- [ ] **Step 7: Optionally rebuild Oracle ADB integration schema to baseline**

If the test instance is persistent, run `make reset-db-oci` (or the equivalent — check Makefile for the OCI reset target) to wipe the converted column back to BYTE mode for a clean Batch 1 start. If the integration tests use an ephemeral schema, this step is unnecessary.

- [ ] **Step 8: No commit for this task**

This task is a verification probe, not a code change. The git working tree must be clean before starting Batch 1.

Run: `git status`

Expected: `nothing to commit, working tree clean`.

### Task 0.4 (CONDITIONAL): Author sidecar SQL migration script

**Run this task only if Task 0.3 Step 5 found that GORM does NOT honor the BYTE-vs-CHAR diff.**

**Files:**
- Create: `scripts/oracle-migrate-varchar-char.sql`

- [ ] **Step 1: Write the migration script**

Create `scripts/oracle-migrate-varchar-char.sql`:

```sql
-- oracle-migrate-varchar-char.sql
-- One-time migration to convert every VARCHAR2 column in the TMI schema from
-- BYTE semantics to CHAR semantics. Run on Oracle ADB after each batch of
-- the #379 remediation if GORM's AutoMigrate does not detect the difference.
--
-- Idempotent: only ALTERs columns currently in BYTE mode.
-- Safe: ALTER ... MODIFY ... VARCHAR2(N CHAR) preserves existing data
-- (CHAR semantics enforces character count; existing rows are character-counted
-- and were inserted under the OpenAPI character-cap so they already fit).
--
-- Run as the TMI app schema owner.

SET SERVEROUTPUT ON;
BEGIN
    FOR rec IN (
        SELECT TABLE_NAME, COLUMN_NAME, CHAR_LENGTH
        FROM USER_TAB_COLUMNS
        WHERE DATA_TYPE = 'VARCHAR2'
          AND CHAR_USED = 'B'
        ORDER BY TABLE_NAME, COLUMN_NAME
    ) LOOP
        BEGIN
            EXECUTE IMMEDIATE
                'ALTER TABLE "' || rec.TABLE_NAME ||
                '" MODIFY ("' || rec.COLUMN_NAME ||
                '" VARCHAR2(' || rec.CHAR_LENGTH || ' CHAR))';
            DBMS_OUTPUT.PUT_LINE(
                'Converted ' || rec.TABLE_NAME || '.' || rec.COLUMN_NAME ||
                ' to VARCHAR2(' || rec.CHAR_LENGTH || ' CHAR)'
            );
        EXCEPTION
            WHEN OTHERS THEN
                DBMS_OUTPUT.PUT_LINE(
                    'FAILED ' || rec.TABLE_NAME || '.' || rec.COLUMN_NAME ||
                    ': ' || SQLERRM
                );
        END;
    END LOOP;
END;
/

-- Verification after running:
-- @scripts/verify-oracle-char-semantics.sql
-- Expected: empty result set.
```

- [ ] **Step 2: Document the deploy runbook addition**

Add a `## Deploy notes` paragraph to the issue #379 comment describing when the script must be run:
- After each Batch 1, 2, 3, 4, 5 deploy to Oracle ADB.
- Before running `scripts/verify-oracle-char-semantics.sql` as the verification step.

- [ ] **Step 3: Commit**

```bash
git add scripts/oracle-migrate-varchar-char.sql
git commit -m "$(cat <<'EOF'
chore(scripts): add Oracle CHAR-semantics sidecar migration

GORM does not detect the VARCHAR2(N) vs VARCHAR2(N CHAR) semantic
difference (per Batch 0 verification). This script is run after each
#379 batch deploys to Oracle ADB to convert any BYTE-mode columns
that AutoMigrate left behind.

Idempotent: only ALTERs columns currently in BYTE mode.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

### Task 0.5: Open Batch 0 PR

- [ ] **Step 1: Push branch and open PR**

```bash
git push -u origin dev/1.4.0
gh pr create --title "feat(models): DBVarchar cross-dialect type for Oracle CHAR semantics (#379 Batch 0)" --body "$(cat <<'EOF'
## Summary
- Adds `DBVarchar` and `NullableDBVarchar` cross-dialect GORM types
- Emits `varchar(N)` on PostgreSQL, `varchar2(N CHAR)` on Oracle ADB
- Unit tests cover GormDBDataType per dialect, Scan, Value, Ptr, constructor
- Verification script `scripts/verify-oracle-char-semantics.sql` for post-deploy schema checks
- [if applicable] Sidecar `scripts/oracle-migrate-varchar-char.sql` for AutoMigrate gap

## Test plan
- [x] `make lint`
- [x] `make test-unit`
- [x] `make test-integration` (PG)
- [x] `make test-integration-oci` (Oracle ADB)
- [x] Manual GORM detection probe on Oracle ADB (verification protocol from spec §Migration strategy)

## Notes
No model fields converted in this PR. This is the foundation for Batches 1–5 (#379).

Spec: docs/superpowers/specs/2026-05-14-oracle-varchar-char-semantics-design.md

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 2: Dispatch oracle-db-admin subagent**

Invoke the `oracle-db-admin` skill with the diff as context. Address every BLOCKING finding before merging. Fold APPROVED WITH NOTES items into the change or file follow-ups.

- [ ] **Step 3: Merge after green CI and APPROVED verdict**

```bash
# After merge, fetch and update local dev/1.4.0
git fetch origin
git checkout dev/1.4.0
git pull --rebase
```

---

## Batches 1–5: Field migrations

Each batch follows the same execution pattern, applied to a different set of fields. The mechanical rewrite is:

**Before:**
```go
Name   string  `gorm:"type:varchar(256);not null;index:..."`
Status *string `gorm:"type:varchar(128);index:..."`
```

**After (DBVarchar bucket):**
```go
Name   DBVarchar         `gorm:"size:256;not null;index:..."`
Status NullableDBVarchar `gorm:"size:128;index:..."`
```

**After (DBText bucket — Batches 4–5 only):**
```go
Description NullableDBText `gorm:""`
URL         DBText         `gorm:""`
```

Note that `DBText` / `NullableDBText` carry no size — the column is unbounded (TEXT/CLOB).

### Generic batch execution pattern

Each batch task list follows this template. Substitute the batch-specific field map below.

- [ ] **Step 1: Identify all call sites that read the affected fields**

For each field in the batch's field map, run:

```bash
rg -n '\.<FieldName>([^a-zA-Z_]|$)' --type go
```

Capture the list. These are the consumer sites that may need `string(x)` type conversions or `NewNullableDBVarchar(x)` / `.Ptr()` adapters.

- [ ] **Step 2: Apply the type changes to the model struct(s)**

For each field, edit the struct tag and type:
- `string` + `type:varchar(N)` → `DBVarchar` + `size:N`
- `*string` + `type:varchar(N)` → `NullableDBVarchar` + `size:N`
- `string` + `type:varchar(N)` → `DBText` (Batch 4/5 buckets only; drop the `size:` tag)
- `*string` + `type:varchar(N)` → `NullableDBText` (Batch 4/5 buckets only; drop the `size:` tag)

Preserve all other tag components: `not null`, `default:...`, `column:...`, `<-:create`, `primaryKey`, `index:...`, `uniqueIndex:...`.

- [ ] **Step 3: Fix consumer sites until build is green**

Run: `make build-server`

For each compile error:
- Direct string read: `x.Name` → `string(x.Name)` (for `DBVarchar`)
- Direct nullable string read: `*x.Status` → `x.Status.Ptr()` or `x.Status.String` per usage shape
- Construction from `*string`: `x.Status = &s` → `x.Status = NewNullableDBVarchar(&s)`
- Construction from `*string` (text): `x.Description = &s` → `x.Description = NewNullableDBText(&s)`
- Construction from literal: `Name: "foo"` → `Name: DBVarchar("foo")` (struct-init form) or `Name: "foo"` in contexts where the literal converts implicitly (function args may need explicit conversion)

Repeat until `make build-server` is clean.

- [ ] **Step 4: Run unit tests**

Run: `make test-unit`

Fix any test failures. Common cases:
- Test fixtures comparing `expected.Name == actual.Name` where types now differ: cast both to `string(...)`.
- Test fixtures using `&"foo"` literal patterns: use `NewNullableDBVarchar(stringPtr("foo"))` or similar.

- [ ] **Step 5: Run integration tests against PostgreSQL**

Run: `make test-integration`

Expected: PASS. The column type stays `varchar(N)` on PG so AutoMigrate is a no-op.

If any test fails, investigate before proceeding — the change is mechanical and shouldn't break behavior.

- [ ] **Step 6: Run integration tests against Oracle ADB**

Run: `make test-integration-oci`

Expected: PASS. AutoMigrate should issue `ALTER TABLE ... MODIFY (col VARCHAR2(N CHAR))` for each converted column (or be re-run via `scripts/oracle-migrate-varchar-char.sql` per the Batch 0 finding).

- [ ] **Step 7: Add a multi-byte stress test (Batches 3, 4, 5 only)**

Skip for Batches 1 and 2 (UUID and enum content is ASCII-only).

For one representative entity affected by the batch, add an integration test that submits a payload with multi-byte UTF-8 content at the OpenAPI `maxLength` cap. Use a string composed of emoji + CJK characters: `strings.Repeat("🔒漢", maxLength/2)` (approximately — adjust for exact length).

Verify:
- Create succeeds (returns 201).
- Get returns the same content byte-for-byte.
- A payload one character over the cap returns 400 from OpenAPI middleware (not 500 from the DB).

Place the test in the appropriate `*_integration_test.go` file under `api/`.

- [ ] **Step 8: Run lint**

Run: `make lint`

Fix any new lint issues.

- [ ] **Step 9: Dispatch oracle-db-admin subagent**

Required per CLAUDE.md "Oracle Database Compatibility Review" for any DB-touching change. Address BLOCKING findings before commit.

- [ ] **Step 10: Run security regression**

Per CLAUDE.md, run the security-regression skill before any commit touching auth/data-access paths. Most batches in this plan are model-layer only, but Batch 4 touches feedback-content storage which is security-adjacent.

- [ ] **Step 11: Commit and push**

```bash
git add api/models/<files> api/<consumer-files> [tests]
git commit -m "$(cat <<'EOF'
feat(models): migrate <batch-description> to DBVarchar/NullableDBVarchar

Part of #379 (Batch N of 6). Converts <field-count> fields across
<file-count> model files from `type:varchar(N)` GORM tags to the
new DBVarchar cross-dialect type for Oracle CHAR semantics.

Consumer sites updated to use string() conversions and the
NewNullableDBVarchar helper where needed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
git push
```

- [ ] **Step 12: Open PR and merge**

```bash
gh pr create --title "feat(models): migrate <batch-description> for Oracle CHAR semantics (#379 Batch N)" --body "..."
```

After CI green + oracle-db-admin APPROVED, merge.

- [ ] **Step 13: Post-deploy verification (Oracle ADB only)**

After production deploy to Oracle ADB:
1. Run `scripts/oracle-migrate-varchar-char.sql` if Batch 0 indicated it's needed.
2. Run `scripts/verify-oracle-char-semantics.sql` and confirm the batch's columns no longer appear in the result.

---

## Batch-specific field maps

The following sections list every field affected by each batch. Use these as authoritative checklists when applying Step 2 of the generic pattern.

### Batch 1: UUID FK/PK columns (`varchar(36)`) — Bucket A (DBVarchar)

All fields with `type:varchar(36)`. ASCII content (UUIDs), low risk.

| File | Struct.Field | Nullability | Notes |
|------|--------------|-------------|-------|
| `api/models/models.go` | `User.InternalUUID` | NOT NULL | primaryKey |
| `api/models/models.go` | `RefreshTokenRecord.UserInternalUUID` | NOT NULL | indexed |
| `api/models/models.go` | `ClientCredential.OwnerUUID` | NOT NULL | indexed |
| `api/models/models.go` | `ThreatModel.OwnerInternalUUID` | NOT NULL | composite indexes |
| `api/models/models.go` | `ThreatModel.CreatedByInternalUUID` | NOT NULL | indexed |
| `api/models/models.go` | `ThreatModel.SecurityReviewerInternalUUID` | NULLABLE | indexed |
| `api/models/models.go` | `ThreatModel.ProjectID` | NULLABLE | indexed |
| `api/models/models.go` | `Diagram.ID` | NOT NULL | primaryKey |
| `api/models/models.go` | `Diagram.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/models.go` | `Asset.ID` | NOT NULL | primaryKey |
| `api/models/models.go` | `Asset.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/models.go` | `Threat.ID` | NOT NULL | primaryKey |
| `api/models/models.go` | `Threat.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/models.go` | `Threat.DiagramID` | NULLABLE | indexed |
| `api/models/models.go` | `Threat.CellID` | NULLABLE | indexed |
| `api/models/models.go` | `Threat.AssetID` | NULLABLE | indexed |
| `api/models/models.go` | `Group.InternalUUID` | NOT NULL | primaryKey |
| `api/models/models.go` | `ThreatModelAuthorization.ID` | NOT NULL | primaryKey |
| `api/models/models.go` | `ThreatModelAuthorization.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/models.go` | `ThreatModelAuthorization.UserInternalUUID` | NULLABLE | composite indexes |
| `api/models/models.go` | `ThreatModelAuthorization.GroupInternalUUID` | NULLABLE | composite indexes |
| `api/models/models.go` | `Document.ID` | NOT NULL | primaryKey |
| `api/models/models.go` | `Document.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/models.go` | `Note.ID` | NOT NULL | primaryKey |
| `api/models/models.go` | `Note.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/models.go` | `Repository.ID` | NOT NULL | primaryKey |
| `api/models/models.go` | `Repository.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/models.go` | `Metadata.ID` | NOT NULL | primaryKey |
| `api/models/models.go` | `Metadata.EntityID` | NOT NULL | composite indexes |
| `api/models/models.go` | `(diagram_threat_model_id field at line 526)` | NOT NULL | indexed |
| `api/models/models.go` | `(diagram_id field at line 527)` | NOT NULL | indexed |
| `api/models/models.go` | `(session_id field at line 555)` | NOT NULL | indexed |
| `api/models/models.go` | `(user_internal_uuid field at line 556)` | NOT NULL | indexed |
| `api/models/models.go` | `(owner_internal_uuid field at line 582)` | NOT NULL | indexed |
| `api/models/models.go` | `(threat_model_id field at line 583)` | NULLABLE | indexed |
| `api/models/models.go` | `WebhookDenyPattern.ID` (line 638) | NOT NULL | primaryKey |
| `api/models/models.go` | `WebhookTrigger.WebhookID` | NOT NULL | indexed |
| `api/models/models.go` | `WebhookTrigger.ThreatModelID` | NULLABLE | indexed |
| `api/models/models.go` | `GroupMembership.ID` (line 731) | NOT NULL | primaryKey |
| `api/models/models.go` | `GroupMembership.GroupInternalUUID` | NOT NULL | composite uniqueIndex |
| `api/models/models.go` | `GroupMembership.UserInternalUUID` | NULLABLE | composite uniqueIndex |
| `api/models/models.go` | `GroupMembership.MemberGroupInternalUUID` | NULLABLE | indexed |
| `api/models/audit.go` | `AuditEntry.ID` | NOT NULL | primaryKey |
| `api/models/audit.go` | `AuditEntry.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/audit.go` | `AuditEntry.ObjectID` | NOT NULL | composite indexes |
| `api/models/audit.go` | `VersionSnapshot.ID` | NOT NULL | primaryKey |
| `api/models/audit.go` | `VersionSnapshot.AuditEntryID` | NOT NULL | indexed |
| `api/models/audit.go` | `VersionSnapshot.ObjectID` | NOT NULL | composite indexes |
| `api/models/timmy.go` | `TimmySession.ID` | NOT NULL | primaryKey |
| `api/models/timmy.go` | `TimmySession.ThreatModelID` | NOT NULL | indexed |
| `api/models/timmy.go` | `TimmySession.UserID` | NOT NULL | composite indexes |
| `api/models/timmy.go` | `TimmyMessage.ID` | NOT NULL | primaryKey |
| `api/models/timmy.go` | `TimmyMessage.SessionID` | NOT NULL | composite uniqueIndex |
| `api/models/timmy.go` | `TimmyEmbedding.ID` | NOT NULL | primaryKey |
| `api/models/timmy.go` | `TimmyEmbedding.ThreatModelID` | NOT NULL | composite indexes |
| `api/models/timmy.go` | `TimmyEmbedding.EntityID` | NOT NULL | composite indexes |
| `api/models/timmy.go` | `TimmyUsage.ID` | NOT NULL | primaryKey |
| `api/models/timmy.go` | `TimmyUsage.UserID` | NOT NULL | indexed |
| `api/models/timmy.go` | `TimmyUsage.SessionID` | NOT NULL | indexed |
| `api/models/timmy.go` | `TimmyUsage.ThreatModelID` | NOT NULL | indexed |
| `api/models/user_content_token.go` | `UserContentToken.ID` | NOT NULL | primaryKey |
| `api/models/user_content_token.go` | `UserContentToken.UserID` | NOT NULL | composite uniqueIndex |
| `api/models/survey_models.go` | `SurveyTemplate.ID` | NOT NULL | primaryKey |
| `api/models/survey_models.go` | `SurveyTemplate.CreatedByInternalUUID` | NOT NULL | indexed |
| `api/models/survey_models.go` | `SurveyTemplateVersion.ID` | NOT NULL | primaryKey |
| `api/models/survey_models.go` | `SurveyTemplateVersion.TemplateID` | NOT NULL | composite uniqueIndex |
| `api/models/survey_models.go` | `SurveyResponse.ID` | NOT NULL | primaryKey |
| `api/models/survey_models.go` | `SurveyResponse.TemplateID` | NOT NULL | composite indexes |
| `api/models/survey_models.go` | `SurveyResponse.LinkedThreatModelID` | NULLABLE | indexed |
| `api/models/survey_models.go` | `SurveyResponse.CreatedThreatModelID` | NULLABLE | indexed |
| `api/models/survey_models.go` | `SurveyResponse.OwnerInternalUUID` | NULLABLE | indexed |
| `api/models/survey_models.go` | `SurveyResponse.ReviewedByInternalUUID` | NULLABLE | |
| `api/models/survey_models.go` | `SurveyResponse.ProjectID` | NULLABLE | indexed |
| `api/models/survey_models.go` | `TriageNote.SurveyResponseID` | NOT NULL | primaryKey + indexed |
| `api/models/survey_models.go` | `TriageNote.ModifiedByInternalUUID` | NULLABLE | |
| `api/models/survey_models.go` | `SurveyResponseAuthorization.ID` | NOT NULL | primaryKey |
| `api/models/survey_models.go` | `SurveyResponseAuthorization.SurveyResponseID` | NOT NULL | composite indexes |
| `api/models/survey_models.go` | `SurveyResponseAuthorization.UserInternalUUID` | NULLABLE | composite indexes |
| `api/models/survey_models.go` | `SurveyResponseAuthorization.GroupInternalUUID` | NULLABLE | composite indexes |
| `api/models/survey_models.go` | `SurveyAnswer.ID` | NOT NULL | primaryKey |
| `api/models/survey_models.go` | `SurveyAnswer.ResponseID` | NOT NULL | composite indexes |
| `api/models/team_project_models.go` | `Team.ID` (and Project.ID) | NOT NULL | primaryKey |
| `api/models/team_project_models.go` | `TeamMember.TeamID` / `TeamMember.UserInternalUUID` | NOT NULL | indexed |
| `api/models/team_project_models.go` | `Project.ID` / `Project.OwnerInternalUUID` | NOT NULL | indexed |
| `api/models/team_project_models.go` | `ProjectMember.ProjectID` / `ProjectMember.UserInternalUUID` | NOT NULL | indexed |
| `api/models/team_project_note_models.go` | `TeamNote.ID` / `TeamNote.TeamID` | NOT NULL | primaryKey + indexed |
| `api/models/team_project_note_models.go` | `ProjectNote.ID` / `ProjectNote.ProjectID` | NOT NULL | primaryKey + indexed |

**Tally:** ~70 fields.

Apply the generic batch execution pattern above. Skip Step 7 (multi-byte stress test) — UUID content is ASCII.

Commit message scope: `feat(models): migrate UUID identifier columns to DBVarchar (#379 Batch 1)`.

### Batch 2: Enums and short identifiers (`varchar(8)`–`varchar(128)`, non-UUID) — Bucket A

| File | Struct.Field | Size | Nullability |
|------|--------------|-----:|-------------|
| `api/models/models.go` | `ClientCredential.Secret` | 128 | NOT NULL |
| `api/models/models.go` | `ThreatModel.ThreatModelFramework` | 30 | nullable (default STRIDE) |
| `api/models/models.go` | `ThreatModel.Status` | 128 | NOT NULL |
| `api/models/models.go` | `Diagram.Type` | 64 | NULLABLE |
| `api/models/models.go` | `Asset.Type` | 64 | NOT NULL |
| `api/models/models.go` | `Asset.Criticality` | 128 | nullable |
| `api/models/models.go` | `Asset.Sensitivity` | 128 | nullable |
| `api/models/models.go` | `Threat.Severity` | 50 | NULLABLE |
| `api/models/models.go` | `Threat.Likelihood` | 50 | NULLABLE |
| `api/models/models.go` | `Threat.RiskLevel` | 50 | NULLABLE |
| `api/models/models.go` | `Threat.Status` | 128 | NULLABLE |
| `api/models/models.go` | `Document.ContentSource` | 64 | nullable |
| `api/models/models.go` | `Document.AccessStatus` | 32 | nullable |
| `api/models/models.go` | `Document.PickerProviderID` | 64 | NULLABLE |
| `api/models/models.go` | `Repository.Type` | 64 | NULLABLE |
| `api/models/models.go` | `Metadata.EntityType` | 50 | NOT NULL |
| `api/models/models.go` | `ThreatModelAuthorization.SubjectType` | 10 | NOT NULL |
| `api/models/models.go` | `ThreatModelAuthorization.Role` | 6 | NOT NULL |
| `api/models/models.go` | `GroupMembership.SubjectType` | 10 | NOT NULL |
| `api/models/models.go` | `Webhook.Secret` | 128 | nullable |
| `api/models/models.go` | `Webhook.Status` | 128 | nullable |
| `api/models/models.go` | `WebhookTrigger.Icon` | 60 | nullable |
| `api/models/models.go` | `UsabilityFeedback.Sentiment` | 8 | NOT NULL |
| `api/models/models.go` | `UsabilityFeedback.Surface` | 32 | nullable |
| `api/models/models.go` | `UsabilityFeedback.ClientID` | 32 | nullable |
| `api/models/models.go` | `UsabilityFeedback.ClientVersion` | 32 | nullable |
| `api/models/models.go` | `UsabilityFeedback.ClientBuild` | 12 | nullable |
| `api/models/models.go` | `UsabilityFeedback.Viewport` | 11 | nullable |
| `api/models/models.go` | `ContentFeedback.Sentiment` | 8 | NOT NULL |
| `api/models/models.go` | `ContentFeedback.TargetField` | 64 | nullable |
| `api/models/models.go` | `ContentFeedback.FalsePositiveReason` | 32 | nullable |
| `api/models/models.go` | `ContentFeedback.FalsePositiveSubreason` | 40 | nullable |
| `api/models/models.go` | `ContentFeedback.ClientID` | 32 | nullable |
| `api/models/models.go` | `ContentFeedback.ClientVersion` | 32 | nullable |
| `api/models/audit.go` | `AuditEntry.ObjectType` | 50 | NOT NULL |
| `api/models/audit.go` | `AuditEntry.ChangeType` | 20 | NOT NULL |
| `api/models/audit.go` | `VersionSnapshot.ObjectType` | 50 | NOT NULL |
| `api/models/audit.go` | `VersionSnapshot.SnapshotType` | 20 | NOT NULL |
| `api/models/timmy.go` | `TimmySession.SystemPromptHash` | 64 | nullable |
| `api/models/timmy.go` | `TimmySession.Status` | 20 | NOT NULL |
| `api/models/timmy.go` | `TimmyEmbedding.EntityType` | 30 | NOT NULL |
| `api/models/timmy.go` | `TimmyEmbedding.IndexType` | 10 | NOT NULL |
| `api/models/timmy.go` | `TimmyEmbedding.ContentHash` | 64 | NOT NULL |
| `api/models/timmy.go` | `TimmyEmbedding.EmbeddingModel` | 100 | NOT NULL |
| `api/models/user_content_token.go` | `UserContentToken.ProviderID` | 64 | NOT NULL |
| `api/models/user_content_token.go` | `UserContentToken.Status` | 16 | nullable (default active) |
| `api/models/system_audit.go` | `SystemAuditLog.HTTPMethod` | 10 | NOT NULL |
| `api/models/system_audit.go` | `SystemAuditLog.ActorProvider` | 100 | NOT NULL |
| `api/models/system_setting.go` | `SystemSetting.SettingType` | 50 | NOT NULL |
| `api/models/survey_models.go` | `SurveyTemplate.Version` | 64 | NOT NULL |
| `api/models/survey_models.go` | `SurveyTemplate.Status` | 20 | NOT NULL |
| `api/models/survey_models.go` | `SurveyTemplateVersion.Version` | 64 | NOT NULL |
| `api/models/survey_models.go` | `SurveyResponse.TemplateVersion` | 64 | NOT NULL |
| `api/models/survey_models.go` | `SurveyResponse.Status` | 30 | NOT NULL |
| `api/models/survey_models.go` | `SurveyResponseAuthorization.SubjectType` | 10 | NOT NULL |
| `api/models/survey_models.go` | `SurveyResponseAuthorization.Role` | 6 | NOT NULL |
| `api/models/survey_models.go` | `SurveyAnswer.QuestionName` | 256 | NOT NULL — **size > 128 but enum-like identifier; keep in this batch** |
| `api/models/survey_models.go` | `SurveyAnswer.QuestionType` | 64 | NOT NULL |
| `api/models/survey_models.go` | `SurveyAnswer.MapsToTmField` | 128 | NULLABLE |
| `api/models/team_project_models.go` | `TeamMember.Role` | 64 | NOT NULL |
| `api/models/team_project_models.go` | `TeamMember.CustomRole` | 128 | NULLABLE |
| `api/models/team_project_models.go` | `Team.Status` | 128 | NOT NULL |
| `api/models/team_project_models.go` | `Project.Status` | 128 | NOT NULL |
| `api/models/team_project_models.go` | `ProjectMember.Role` | 64 | NOT NULL |
| `api/models/team_project_models.go` | `ProjectMember.CustomRole` | 128 | NULLABLE |

**Tally:** ~40 fields.

Apply the generic batch execution pattern. Skip Step 7 — enum content is ASCII.

Commit message scope: `feat(models): migrate enum and short-identifier columns to DBVarchar (#379 Batch 2)`.

### Batch 3: Names, emails, mid-size identifiers — Bucket A

| File | Struct.Field | Size | Nullability | Notes |
|------|--------------|-----:|-------------|-------|
| `api/models/models.go` | `User.Provider` | 100 | NOT NULL | indexed |
| `api/models/models.go` | `User.ProviderUserID` | 500 | NULLABLE | composite index |
| `api/models/models.go` | `User.Email` | 320 | NOT NULL | indexed |
| `api/models/models.go` | `User.Name` | 256 | nullable (no tag size?) — check |
| `api/models/models.go` | `RefreshTokenRecord.Token` | 4000 | NOT NULL | uniqueIndex |
| `api/models/models.go` | `ClientCredential.ClientID` | 1000 | NOT NULL | uniqueIndex |
| `api/models/models.go` | `ClientCredential.Name` | 256 | NOT NULL | |
| `api/models/models.go` | `ThreatModel.Name` | 256 | NOT NULL | |
| `api/models/models.go` | `Diagram.Name` | 256 | NOT NULL | |
| `api/models/models.go` | `Asset.Name` | 256 | NOT NULL | indexed |
| `api/models/models.go` | `Threat.Name` | 256 | NOT NULL | indexed |
| `api/models/models.go` | `Threat.Priority` | 256 | NULLABLE | indexed |
| `api/models/models.go` | `Group.Provider` | 100 | NOT NULL | indexed |
| `api/models/models.go` | `Group.GroupName` | 500 | NOT NULL | indexed |
| `api/models/models.go` | `Group.Name` | 256 | nullable | |
| `api/models/models.go` | `Document.Name` | 256 | NOT NULL | indexed |
| `api/models/models.go` | `Document.PickerFileID` | 255 | NULLABLE | composite index |
| `api/models/models.go` | `Note.Name` | 256 | NOT NULL | indexed |
| `api/models/models.go` | `Repository.Name` | 256 | NULLABLE | indexed |
| `api/models/models.go` | `Metadata.Key` | 256 | NOT NULL | composite indexes |
| `api/models/models.go` | `Metadata.Value` | 1024 | NOT NULL | composite index |
| `api/models/models.go` | `Webhook.Name` | 256 | NOT NULL | |
| `api/models/models.go` | `Webhook.Challenge` | 1000 | NULLABLE | |
| `api/models/models.go` | `WebhookTrigger.Name` | 256 | NOT NULL | |
| `api/models/models.go` | `WebhookDenyPattern.Pattern` | 256 | NOT NULL | uniqueIndex |
| `api/models/models.go` | `UsabilityFeedback.UserAgent` | 512 | nullable | |
| `api/models/audit.go` | `AuditEntry.ActorEmail` | 320 | NULLABLE (review) | |
| `api/models/audit.go` | `AuditEntry.ActorProvider` | 100 | NULLABLE | |
| `api/models/audit.go` | `AuditEntry.ActorProviderID` | 500 | NULLABLE | |
| `api/models/audit.go` | `AuditEntry.ActorDisplayName` | 256 | NULLABLE | |
| `api/models/timmy.go` | `TimmySession.Title` | 256 | nullable | |
| `api/models/user_content_token.go` | `UserContentToken.ProviderAccountID` | 255 | NOT NULL | |
| `api/models/user_content_token.go` | `UserContentToken.ProviderAccountLabel` | 255 | NULLABLE | |
| `api/models/system_audit.go` | `SystemAuditLog.ActorEmail` | 320 | NOT NULL | composite index |
| `api/models/system_audit.go` | `SystemAuditLog.ActorProviderID` | 500 | NOT NULL | |
| `api/models/system_audit.go` | `SystemAuditLog.ActorDisplayName` | 256 | NOT NULL | |
| `api/models/system_audit.go` | `SystemAuditLog.FieldPath` | 1024 | NOT NULL | **indexed — must stay varchar** |
| `api/models/system_setting.go` | `SystemSetting.SettingKey` | 256 | NOT NULL | |
| `api/models/survey_models.go` | `SurveyTemplate.Name` | 256 | NOT NULL | indexed |
| `api/models/team_project_models.go` | `Team.Name` | 256 | NOT NULL | |
| `api/models/team_project_models.go` | `Team.EmailAddress` | 320 | NULLABLE | |
| `api/models/team_project_models.go` | `Project.Name` | 256 | NOT NULL | |
| `api/models/team_project_note_models.go` | `TeamNote.Name` | 256 | NOT NULL | composite index |
| `api/models/team_project_note_models.go` | `ProjectNote.Name` | 256 | NOT NULL | composite index |

**Tally:** ~44 fields.

**Apply the generic batch execution pattern in full, including Step 7 (multi-byte stress test).**

Test target for Step 7: pick one entity per affected area to exercise:
- A threat model with name = 100 chars of emoji+CJK at the OpenAPI cap.
- A user feedback submission with UserAgent containing UTF-8.
- A diagram with name = 256 chars of emoji+CJK.

Commit message scope: `feat(models): migrate name/email/identifier columns to DBVarchar (#379 Batch 3)`.

### Batch 4: Descriptions and large free-text → DBText / NullableDBText

| File | Struct.Field | Old size | Target type | Nullability |
|------|--------------|---------:|-------------|-------------|
| `api/models/models.go` | `ClientCredential.Description` | 1024 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `ThreatModel.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Diagram.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Asset.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Threat.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Threat.Mitigation` | 1024 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Group.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Document.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Note.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Repository.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `WebhookTrigger.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `UsabilityFeedback.Verbatim` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `ContentFeedback.Verbatim` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/system_setting.go` | `SystemSetting.Value` | 4000 | `DBText` | NOT NULL |
| `api/models/system_setting.go` | `SystemSetting.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/survey_models.go` | `SurveyTemplate.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/survey_models.go` | `SurveyResponse.RevisionNotes` | 4000 | `NullableDBText` | NULLABLE — drop the "Oracle ADB-STANDARD compatibility" comment; no longer relevant |
| `api/models/survey_models.go` | `SurveyAnswer.QuestionTitle` | 1024 | `NullableDBText` | NULLABLE |
| `api/models/team_project_models.go` | `Team.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/team_project_models.go` | `Project.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/team_project_note_models.go` | `TeamNote.Description` | 2048 | `NullableDBText` | NULLABLE |
| `api/models/team_project_note_models.go` | `ProjectNote.Description` | 2048 | `NullableDBText` | NULLABLE |

**Tally:** ~22 fields.

**Apply the generic batch execution pattern in full, including Step 7 (multi-byte stress test).**

**Special considerations for Batch 4:**

- **AutoMigrate column-type change is real:** `varchar(N) → CLOB` on Oracle, `varchar(N) → TEXT` on PostgreSQL. Both DBs execute an `ALTER TABLE ... MODIFY` (Oracle) or `ALTER TABLE ... ALTER COLUMN ... TYPE text` (PG). Confirm migration logs show this in test-integration runs.
- **Rollback window is constrained:** if a user writes a description longer than the old `varchar(2048)` cap, rollback to `varchar` becomes blocked on Oracle. Mitigation already documented in spec §Migration strategy — include a 24h post-deploy length-check in the PR description.
- **`NewNullableDBText` constructor:** consumer sites that previously assigned `*string` will need this. Example:
  ```go
  // Before
  threat.Description = req.Description // *string
  // After
  threat.Description = models.NewNullableDBText(req.Description)
  ```

Test target for Step 7:
- Submit a threat with `description = strings.Repeat("🔒漢", 1024)` (2048 chars).
- Verify it persists and retrieves losslessly on both PG and Oracle.
- Submit one over the OpenAPI cap and verify 400 (not 500) from middleware.

Commit message scope: `feat(models): migrate description and free-text columns to DBText/NullableDBText (#379 Batch 4)`.

### Batch 5: URL columns → DBText / NullableDBText

| File | Struct.Field | Old size | Target type | Nullability |
|------|--------------|---------:|-------------|-------------|
| `api/models/models.go` | `ThreatModel.IssueURI` | 1000 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Threat.IssueURI` | 1000 | `NullableDBText` | NULLABLE |
| `api/models/models.go` | `Document.URI` | 1000 | `DBText` | NOT NULL (review) |
| `api/models/models.go` | `Repository.URI` | 1000 | `DBText` | NOT NULL (review) |
| `api/models/models.go` | `Webhook.URL` | 1024 | `DBText` | NOT NULL (review) |
| `api/models/team_project_models.go` | `Team.URI` | 1000 | `NullableDBText` | NULLABLE |
| `api/models/team_project_models.go` | `Project.URI` | 1000 | `NullableDBText` | NULLABLE |
| `api/models/system_audit.go` | `SystemAuditLog.HTTPPath` | 2048 | `DBText` | NOT NULL |

**Tally:** ~8 fields.

For each field where the nullability column says "(review)", check the existing tag for `not null`. If present, use `DBText`; if absent, use `NullableDBText`.

Apply the generic batch execution pattern, including Step 7.

Test target for Step 7:
- A webhook with a long URL (1024 chars including some UTF-8 in a query parameter value).
- A threat model with IssueURI containing UTF-8.

Commit message scope: `feat(models): migrate URL and audit-path columns to DBText/NullableDBText (#379 Batch 5)`.

---

## Issue closure

After Batch 5 merges to `dev/1.4.0`:

- [ ] **Step 1: Verify post-deploy schema on Oracle ADB**

Run `scripts/verify-oracle-char-semantics.sql` on the OCI test instance. Confirm empty result set.

- [ ] **Step 2: Comment on issue #379 with completion summary**

```bash
gh issue comment 379 --body "$(cat <<'EOF'
## Complete — all 6 batches merged

Final verification: `scripts/verify-oracle-char-semantics.sql` returns empty
result set on Oracle ADB. All TMI VARCHAR2 columns now use CHAR semantics,
and large free-text columns are CLOB on Oracle / TEXT on PostgreSQL.

Batches landed:
- Batch 0: foundation (DBVarchar types) — commit <SHA>
- Batch 1: UUID columns — commit <SHA>
- Batch 2: enums and short identifiers — commit <SHA>
- Batch 3: names, emails, mid-size identifiers — commit <SHA>
- Batch 4: descriptions and large free-text → DBText — commit <SHA>
- Batch 5: URLs and audit paths → DBText — commit <SHA>

Cross-DB parity for length validation is now achieved: a payload that fits on
PostgreSQL fits on Oracle; one that overflows the OpenAPI cap is rejected at
the middleware layer on both.

Closing.
EOF
)"
gh issue close 379
```

Per CLAUDE.md, commits on `dev/1.4.0` don't auto-close — manual close required.

---

## Self-review

**Spec coverage check:**
- ✅ §Approach (new DBVarchar + DBText migration): covered by Batch 0 + Batches 1–5
- ✅ §Architecture (new type definition): Task 0.1 has full code
- ✅ §Field classification: all three buckets covered by per-batch field maps
- ✅ §Batching: §"Batches 1–5" section follows the spec table
- ✅ §Migration strategy (Shape 1/2/3): Task 0.3 verifies Shape 1; Task 0.4 (conditional) covers the gap case; Batch 4 covers Shape 2; Batch 5 confirms Shape 3 reduces to Shape 2 (no URL indexes)
- ✅ §Testing (unit, integration, schema-verification): Task 0.1 unit, generic Step 5/6 integration, Task 0.2 schema verification
- ✅ §Risks (GORM detection, varchar→CLOB lock, rollback, type ripple, index key length): Task 0.3 + Task 0.4 + Batch 4 special considerations + generic Step 3 type fix loop
- ✅ §Open questions: none in spec, none added

**Placeholder scan:**
- No "TBD" / "TODO" / "implement later" in the plan
- All commit messages have actual content
- All code blocks are complete Go/SQL/shell, not pseudocode
- One "(review)" annotation in Batch 5 field map for nullability — directs the implementer to check the existing tag, which is concrete and actionable

**Type consistency:**
- `DBVarchar` used consistently as the non-nullable type, `NullableDBVarchar` as nullable — matches `DBText` / `NullableDBText` precedent
- `NewNullableDBVarchar(*string)` constructor signature matches `NewNullableDBText(*string)`
- `.Ptr()` method shape matches `NullableDBText.Ptr()`
- GORM tag `size:N` used everywhere `type:varchar(N)` was previously used — no mixed conventions
