# Age-Floored Append-Only Audit Triggers (#453) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make audit retention pruning work on both PostgreSQL and Oracle by teaching the T19 append-only triggers to allow DELETEs of rows older than a config-derived age floor, while making audit rows fully immutable (no UPDATEs ever).

**Architecture:** The triggers in `internal/dbschema/audit_append_only.go` gain per-table delete age floors baked into the SQL at install time (boot), derived from the same env config the pruner reads. `executePrune` stops UPDATE-ing `audit_entries.version`; the dead `DeleteThreatModelAudit` is removed; the pruner logs an actionable error on `ErrAppendOnlyViolation`.

**Tech Stack:** Go, GORM, PostgreSQL (plpgsql trigger), Oracle ADB (PL/SQL trigger), testify.

**Spec:** `docs/superpowers/specs/2026-06-11-453-audit-age-floor-design.md` — read it first.

**Branch:** work on `dev/1.4.0`.

**Floor policy (from spec):**

| Table | Configured source | Installed floor | Hard minimum |
|---|---|---|---|
| `audit_entries` | `AUDIT_RETENTION_DAYS` (default 365) | configured − 1 = 364 | 30 |
| `version_snapshots` | `min(VERSION_RETENTION_DAYS, TOMBSTONE_RETENTION_DAYS)` (default min(90,30)=30) | configured − 1 = 29 | 7 |

---

### Task 1: Floor computation in dbschema (TDD)

**Files:**
- Modify: `internal/dbschema/audit_append_only.go`
- Create: `internal/dbschema/audit_append_only_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/dbschema/audit_append_only_test.go`:

```go
package dbschema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuditFloorConfig_Floors(t *testing.T) {
	tests := []struct {
		name             string
		cfg              AuditFloorConfig
		wantAuditFloor   int
		wantSnapshotFloor int
	}{
		{
			name:              "defaults",
			cfg:               AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 30},
			wantAuditFloor:    364,
			wantSnapshotFloor: 29,
		},
		{
			name:              "audit retention below hard minimum clamps to 30",
			cfg:               AuditFloorConfig{AuditRetentionDays: 10, VersionRetentionDays: 90, TombstoneRetentionDays: 30},
			wantAuditFloor:    30,
			wantSnapshotFloor: 29,
		},
		{
			name:              "snapshot floor uses the smaller of version and tombstone retention",
			cfg:               AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 20, TombstoneRetentionDays: 30},
			wantAuditFloor:    364,
			wantSnapshotFloor: 19,
		},
		{
			name:              "snapshot floor clamps to hard minimum 7",
			cfg:               AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 3},
			wantAuditFloor:    364,
			wantSnapshotFloor: 7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantAuditFloor, tt.cfg.auditEntriesFloorDays())
			assert.Equal(t, tt.wantSnapshotFloor, tt.cfg.versionSnapshotsFloorDays())
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestAuditFloorConfig_Floors`
Expected: FAIL — `undefined: AuditFloorConfig`

- [ ] **Step 3: Implement the floor config**

Add to `internal/dbschema/audit_append_only.go` (below the imports, above `InstallAuditAppendOnlyTriggers`):

```go
// AuditFloorConfig carries the retention configuration used to derive the
// per-table delete age floors baked into the append-only triggers at install
// time. The values come from the same env config the audit pruner reads
// (AUDIT_RETENTION_DAYS, VERSION_RETENTION_DAYS, TOMBSTONE_RETENTION_DAYS),
// so the trigger floor and the pruner cutoff cannot drift within one boot.
type AuditFloorConfig struct {
	AuditRetentionDays     int
	VersionRetentionDays   int
	TombstoneRetentionDays int
}

const (
	// auditFloorHardMinDays is the lowest delete age floor that may be
	// installed on audit_entries regardless of configuration — a
	// misconfigured retention must not gut T19 tamper resistance.
	auditFloorHardMinDays = 30
	// snapshotFloorHardMinDays is the equivalent for version_snapshots.
	// It is lower because snapshots are rollback payloads, not the
	// tamper-evident record, and PurgeTombstones legitimately deletes
	// them TOMBSTONE_RETENTION_DAYS after soft-deletion.
	snapshotFloorHardMinDays = 7
)

// clampFloor converts a configured retention into an installed trigger
// floor: one day of clock-skew margin below the retention (the pruner
// compares app-side time, the trigger DB-side time), but never below the
// hard minimum.
func clampFloor(configuredDays, hardMinDays int) int {
	floor := configuredDays - 1
	if floor < hardMinDays {
		return hardMinDays
	}
	return floor
}

func (c AuditFloorConfig) auditEntriesFloorDays() int {
	return clampFloor(c.AuditRetentionDays, auditFloorHardMinDays)
}

func (c AuditFloorConfig) versionSnapshotsFloorDays() int {
	v := c.VersionRetentionDays
	if c.TombstoneRetentionDays < v {
		v = c.TombstoneRetentionDays
	}
	return clampFloor(v, snapshotFloorHardMinDays)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestAuditFloorConfig_Floors`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/dbschema/audit_append_only.go internal/dbschema/audit_append_only_test.go
git commit -m "feat(db): add age-floor computation for append-only audit triggers

Refs #453.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Age-floored trigger SQL (PG + Oracle) and new install signature

**Files:**
- Modify: `internal/dbschema/audit_append_only.go` (whole file below the Task 1 additions)
- Modify: `cmd/server/main.go:409`
- Modify: `api/audit_store.go:36-58` (exported retention readers)

- [ ] **Step 1: Change the install signature and rewrite the policy comments**

In `internal/dbschema/audit_append_only.go`, replace the function doc comment and signature of `InstallAuditAppendOnlyTriggers` (currently lines 33-64). The old "operator escape hatch / DISABLE TRIGGER is the only supported path" comment is now wrong — replace it entirely:

```go
// InstallAuditAppendOnlyTriggers installs triggers on audit_entries and
// version_snapshots that block all UPDATEs and block DELETEs of rows
// younger than a per-table age floor. Idempotent across re-runs (the
// server reinstalls on every boot, so floor changes take effect on
// restart).
//
// Retention policy (#453): aged-out pruning is a supported, in-app
// operation — the scheduled AuditPruner deletes rows older than the
// configured retention, and the trigger's age floor (retention minus a
// 1-day clock-skew margin, never below a hard minimum) permits exactly
// that. There is no bypass flag or privileged session state: an attacker
// holding the app's DB credentials cannot delete or modify any row
// younger than the floor, and cannot UPDATE any row ever. Audit rows are
// immutable evidence; a pruned version snapshot simply makes the
// corresponding rollback return 410 Gone.
//
// On Oracle ADB the triggers use CREATE OR REPLACE TRIGGER; on
// PostgreSQL, CREATE OR REPLACE FUNCTION + DROP TRIGGER IF EXISTS +
// CREATE TRIGGER. SQLite is skipped.
func InstallAuditAppendOnlyTriggers(ctx context.Context, db *gorm.DB, floors AuditFloorConfig) error {
	logger := slogging.Get()
	dialect := db.Name()

	auditFloor := floors.auditEntriesFloorDays()
	snapshotFloor := floors.versionSnapshotsFloorDays()
	if floors.AuditRetentionDays-1 < auditFloorHardMinDays {
		logger.Warn("InstallAuditAppendOnlyTriggers: configured AUDIT_RETENTION_DAYS=%d is below the %d-day immutability floor; pruning of audit_entries younger than %d days will be blocked",
			floors.AuditRetentionDays, auditFloorHardMinDays, auditFloorHardMinDays)
	}
	if min(floors.VersionRetentionDays, floors.TombstoneRetentionDays)-1 < snapshotFloorHardMinDays {
		logger.Warn("InstallAuditAppendOnlyTriggers: configured snapshot retention (min of VERSION_RETENTION_DAYS=%d, TOMBSTONE_RETENTION_DAYS=%d) is below the %d-day immutability floor; snapshot deletion younger than %d days will be blocked",
			floors.VersionRetentionDays, floors.TombstoneRetentionDays, snapshotFloorHardMinDays, snapshotFloorHardMinDays)
	}

	switch dialect {
	case "postgres":
		return installPostgresAppendOnly(ctx, db, logger, auditFloor, snapshotFloor)
	case "oracle":
		return installOracleAppendOnly(ctx, db, logger, auditFloor, snapshotFloor)
	case "sqlite":
		logger.Info("InstallAuditAppendOnlyTriggers: skipping on dialect %q (single-process SQLite is single-writer)", dialect)
		return nil
	default:
		logger.Warn("InstallAuditAppendOnlyTriggers: unsupported dialect %q, skipping; T19 protection is NOT in effect", dialect)
		return nil
	}
}
```

(Go 1.21+ has the builtin `min`; this repo is on a newer Go — check `go.mod`, and if below 1.21 inline the comparison instead.)

Also update the package doc comment at the top of the file: replace the sentence about the trigger raising "an exception on UPDATE or DELETE" with "raises an exception on any UPDATE, and on DELETE of rows younger than a per-table age floor derived from retention config (#453)".

- [ ] **Step 2: Rewrite the PostgreSQL installer**

Replace `installPostgresAppendOnly` (currently lines 66-101) with:

```go
func installPostgresAppendOnly(ctx context.Context, db *gorm.DB, logger *slogging.Logger, auditFloorDays, snapshotFloorDays int) error {
	statements := []string{
		// Guard function. The delete age floor arrives as a trigger
		// argument (TG_ARGV[0], days). RAISE EXCEPTION surfaces a clean
		// SQLSTATE 'P0001'; dberrors.classifyPgError matches P0001 plus
		// the "append-only" substring, so keep that word in the message.
		`CREATE OR REPLACE FUNCTION tmi_audit_append_only_guard()
		 RETURNS trigger AS $$
		 BEGIN
		   IF TG_OP = 'DELETE' AND OLD.created_at < now() - make_interval(days => TG_ARGV[0]::integer) THEN
		     RETURN OLD;
		   END IF;
		   RAISE EXCEPTION 'audit history is append-only: % on % blocked by tmi_audit_append_only_guard (DELETE allowed only for rows older than % days)',
		     TG_OP, TG_TABLE_NAME, TG_ARGV[0]
		     USING ERRCODE = 'P0001';
		 END;
		 $$ LANGUAGE plpgsql;`,

		// audit_entries trigger
		`DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries;`,
		fmt.Sprintf(`CREATE TRIGGER tmi_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON audit_entries
		 FOR EACH ROW EXECUTE FUNCTION tmi_audit_append_only_guard('%d');`, auditFloorDays),

		// version_snapshots trigger
		`DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots;`,
		fmt.Sprintf(`CREATE TRIGGER tmi_version_snapshots_no_mutate
		 BEFORE UPDATE OR DELETE ON version_snapshots
		 FOR EACH ROW EXECUTE FUNCTION tmi_audit_append_only_guard('%d');`, snapshotFloorDays),
	}

	for _, sql := range statements {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("postgres install: %w (sql: %s)", err, sql)
		}
	}
	logger.Info("InstallAuditAppendOnlyTriggers: postgres triggers installed (audit_entries floor=%dd, version_snapshots floor=%dd)", auditFloorDays, snapshotFloorDays)
	return nil
}
```

- [ ] **Step 3: Rewrite the Oracle installer**

Replace `installOracleAppendOnly` (currently lines 103-131) with:

```go
func installOracleAppendOnly(ctx context.Context, db *gorm.DB, logger *slogging.Logger, auditFloorDays, snapshotFloorDays int) error {
	// Oracle CREATE OR REPLACE TRIGGER is atomic — no DROP/CREATE pair.
	// RAISE_APPLICATION_ERROR(-20001, ...) bubbles up as ORA-20001;
	// dberrors.classifyOracleCode maps 20001 to ErrAppendOnlyViolation.
	// created_at values are written in UTC, so the floor comparison uses
	// SYS_EXTRACT_UTC(SYSTIMESTAMP).
	statements := []string{
		fmt.Sprintf(`CREATE OR REPLACE TRIGGER tmi_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON audit_entries
		 FOR EACH ROW
		 BEGIN
		   IF DELETING AND :OLD.created_at < SYS_EXTRACT_UTC(SYSTIMESTAMP) - NUMTODSINTERVAL(%d, 'DAY') THEN
		     NULL;
		   ELSE
		     RAISE_APPLICATION_ERROR(-20001, 'audit history is append-only: ' || (CASE WHEN UPDATING THEN 'UPDATE' ELSE 'DELETE' END) || ' on audit_entries blocked by tmi_audit_entries_no_mutate (DELETE allowed only for rows older than %d days)');
		   END IF;
		 END;`, auditFloorDays, auditFloorDays),
		fmt.Sprintf(`CREATE OR REPLACE TRIGGER tmi_version_snapshots_no_mutate
		 BEFORE UPDATE OR DELETE ON version_snapshots
		 FOR EACH ROW
		 BEGIN
		   IF DELETING AND :OLD.created_at < SYS_EXTRACT_UTC(SYSTIMESTAMP) - NUMTODSINTERVAL(%d, 'DAY') THEN
		     NULL;
		   ELSE
		     RAISE_APPLICATION_ERROR(-20001, 'version snapshots are append-only: ' || (CASE WHEN UPDATING THEN 'UPDATE' ELSE 'DELETE' END) || ' on version_snapshots blocked by tmi_version_snapshots_no_mutate (DELETE allowed only for rows older than %d days)');
		   END IF;
		 END;`, snapshotFloorDays, snapshotFloorDays),
	}

	for _, sql := range statements {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("oracle install: %w (sql: %s)", err, sql)
		}
	}
	logger.Info("InstallAuditAppendOnlyTriggers: oracle triggers installed (audit_entries floor=%dd, version_snapshots floor=%dd)", auditFloorDays, snapshotFloorDays)
	return nil
}
```

- [ ] **Step 4: Export the retention readers in the api package**

In `api/audit_store.go`, replace the body of `NewGormAuditService` (lines 36-44) and add three exported helpers right after it:

```go
// NewGormAuditService creates a new GormAuditService with configuration from environment.
func NewGormAuditService(db *gorm.DB) *GormAuditService {
	return &GormAuditService{
		db:                     db,
		auditRetentionDays:     AuditRetentionDays(),
		versionRetentionCount:  getEnvInt("VERSION_RETENTION_COUNT", defaultVersionRetentionCount),
		versionRetentionDays:   VersionRetentionDays(),
		tombstoneRetentionDays: TombstoneRetentionDays(),
	}
}

// AuditRetentionDays returns the configured audit-entry retention in days
// (AUDIT_RETENTION_DAYS, default 365). Exported because the append-only
// trigger installation derives its delete age floor from the same value,
// so the pruner cutoff and the trigger floor cannot drift (#453).
func AuditRetentionDays() int {
	return getEnvInt("AUDIT_RETENTION_DAYS", defaultAuditRetentionDays)
}

// VersionRetentionDays returns the configured version-snapshot retention in
// days (VERSION_RETENTION_DAYS, default 90). See AuditRetentionDays.
func VersionRetentionDays() int {
	return getEnvInt("VERSION_RETENTION_DAYS", defaultVersionRetentionDays)
}

// TombstoneRetentionDays returns the configured tombstone retention in days
// (TOMBSTONE_RETENTION_DAYS, default 30). See AuditRetentionDays.
func TombstoneRetentionDays() int {
	return getEnvInt("TOMBSTONE_RETENTION_DAYS", defaultTombstoneRetentionDays)
}
```

- [ ] **Step 5: Update the call site in cmd/server/main.go**

At `cmd/server/main.go:409`, the current call is:

```go
if err := dbschema.InstallAuditAppendOnlyTriggers(ctx, gormDB.DB()); err != nil {
	logger.Warn("InstallAuditAppendOnlyTriggers failed (non-fatal; T19 protection NOT in effect): %v", err)
```

Replace with:

```go
if err := dbschema.InstallAuditAppendOnlyTriggers(ctx, gormDB.DB(), dbschema.AuditFloorConfig{
	AuditRetentionDays:     api.AuditRetentionDays(),
	VersionRetentionDays:   api.VersionRetentionDays(),
	TombstoneRetentionDays: api.TombstoneRetentionDays(),
}); err != nil {
	logger.Warn("InstallAuditAppendOnlyTriggers failed (non-fatal; T19 protection NOT in effect): %v", err)
```

(`cmd/server/main.go` already imports the `api` package; verify the import alias used there and match it.)

- [ ] **Step 6: Build and run unit tests**

Run: `make build-server` then `make test-unit`
Expected: both succeed. If any other caller of `InstallAuditAppendOnlyTriggers` fails to compile, update it the same way as Step 5 (a grep found only `cmd/server/main.go`, but re-check: `grep -rn "InstallAuditAppendOnlyTriggers" --include="*.go" .`).

- [ ] **Step 7: Commit**

```bash
git add internal/dbschema/audit_append_only.go api/audit_store.go cmd/server/main.go
git commit -m "feat(db): age-floor the append-only audit triggers on PG and Oracle

DELETEs of rows older than the config-derived floor are permitted so the
scheduled pruner works; UPDATEs remain blocked unconditionally.

Refs #453.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Audit rows fully immutable — remove version-nulling and dead delete path

**Files:**
- Modify: `api/audit_store.go` (executePrune lines 488-534, DeleteThreatModelAudit lines 323-353)
- Modify: `api/audit_service.go` (interface lines 73-75 and 81-85)
- Modify: `api/audit_debouncer_test.go:52` (mock)

- [ ] **Step 1: Remove the version-nulling UPDATE from executePrune**

In `api/audit_store.go`, `executePrune` currently (a) plucks `audit_entry_id`s for the doomed snapshots and (b) UPDATEs those audit entries to `version = NULL`. Delete both parts — the audit row keeps its version number as historical fact; a missing snapshot already yields `410 Gone` via `GetSnapshot`'s error path in `RollbackToVersion` (`api/audit_handlers.go`). The function becomes:

```go
// executePrune deletes version snapshots below the boundary. The parent
// audit entries are immutable and keep their version numbers; a missing
// snapshot means the content was pruned and rollback returns 410 Gone.
func (s *GormAuditService) executePrune(ctx context.Context, objectType, objectID string, boundary int) (int, error) {
	var pruned int

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Get IDs of snapshots to delete
		var snapshotIDs []string
		err := tx.Model(&models.VersionSnapshot{}).
			Where("object_type = ? AND object_id = ? AND version < ?", objectType, objectID, boundary).
			Pluck("id", &snapshotIDs).Error
		if err != nil {
			return err
		}

		if len(snapshotIDs) == 0 {
			return nil
		}

		// Delete version snapshots
		if err := tx.Where("id IN ?", snapshotIDs).Delete(&models.VersionSnapshot{}).Error; err != nil {
			return err
		}

		pruned = len(snapshotIDs)
		return nil
	})

	return pruned, err
}
```

- [ ] **Step 2: Remove DeleteThreatModelAudit everywhere**

It is called nowhere (verified by grep) and "delete all audit history regardless of age" is incompatible with the age-floor policy. Remove:

1. `api/audit_store.go` lines 323-353 — the entire `DeleteThreatModelAudit` method and its doc comment.
2. `api/audit_service.go` lines 73-75 — the interface method and its doc comment.
3. `api/audit_debouncer_test.go:52` — the `mockAuditService.DeleteThreatModelAudit` stub.

- [ ] **Step 3: Fix the PruneVersionSnapshots interface doc**

In `api/audit_service.go` (lines 81-85), the doc says "Sets version=NULL on corresponding audit entries." Replace the comment block with:

```go
	// PruneVersionSnapshots removes version snapshots outside the configured retention window.
	// Always stops at checkpoint boundaries to ensure remaining diffs can be reconstructed.
	// Audit entries are immutable and keep their version numbers; rollback to a pruned
	// version returns an error (the handler maps it to 410 Gone).
	// Returns the number of snapshots pruned.
	PruneVersionSnapshots(ctx context.Context) (int, error)
```

- [ ] **Step 4: Build, run unit tests, fix fallout**

Run: `make build-server` then `make test-unit`
Expected: compile errors or test failures only where a test asserted the version-nulling behavior or stubbed `DeleteThreatModelAudit`. Fix by deleting the stale assertions/stubs — do NOT re-add the nulling. If a test asserts `entry.Version == nil` after pruning, invert it to assert the version is preserved.

- [ ] **Step 5: Commit**

```bash
git add api/audit_store.go api/audit_service.go api/audit_debouncer_test.go
git commit -m "refactor(api): make audit entries fully immutable during snapshot pruning

Remove the version=NULL UPDATE (blocked by the append-only trigger; the
version number is historical fact) and the never-called
DeleteThreatModelAudit, which is incompatible with the age-floor policy.

Refs #453.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Actionable pruner logging on append-only violations (TDD)

**Files:**
- Modify: `api/audit_pruner.go`
- Create: `api/audit_pruner_test.go` (or extend if it exists — check first)

- [ ] **Step 1: Write the failing test**

The retry wrapper returns the ORIGINAL driver error, not a classified one (it calls `dberrors.Classify` only inside its retry predicates — see `auth/db/retry.go:111`), so the pruner must classify before checking. Test the message helper with both a pre-wrapped sentinel and a plain error:

```go
package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/stretchr/testify/assert"
)

func TestPruneFailureMessage_AppendOnlyViolation(t *testing.T) {
	err := fmt.Errorf("failed to prune audit entries: %w",
		dberrors.Wrap(errors.New("ERROR: audit history is append-only (SQLSTATE P0001)"), dberrors.ErrAppendOnlyViolation))
	msg := pruneFailureMessage("audit entries", err)
	assert.Contains(t, msg, "append-only trigger")
	assert.Contains(t, msg, "restart the server")
}

func TestPruneFailureMessage_GenericError(t *testing.T) {
	msg := pruneFailureMessage("version snapshots", errors.New("connection refused"))
	assert.Contains(t, msg, "failed to prune version snapshots")
	assert.NotContains(t, msg, "append-only trigger")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestPruneFailureMessage`
Expected: FAIL — `undefined: pruneFailureMessage`

- [ ] **Step 3: Implement**

In `api/audit_pruner.go`, add imports `errors`, `fmt`, and `github.com/ericfitz/tmi/internal/dberrors`, then add:

```go
// pruneFailureMessage builds an operator-actionable log message for a prune
// failure. Append-only trigger violations get a specific message: they mean
// the retention config was lowered after boot (the trigger floor is baked in
// at install time) or the floor's hard minimum is above the configured
// retention — both fixed by aligning config and restarting.
func pruneFailureMessage(what string, err error) string {
	classified := dberrors.Classify(err)
	if errors.Is(classified, dberrors.ErrAppendOnlyViolation) || errors.Is(err, dberrors.ErrAppendOnlyViolation) {
		return fmt.Sprintf("failed to prune %s: blocked by the append-only trigger's delete age floor; the configured retention is below the floor installed at boot — align retention config and restart the server to reinstall triggers: %v", what, err)
	}
	return fmt.Sprintf("failed to prune %s: %v", what, err)
}
```

Then in `prune()` (lines 63-89), replace the three error logs:

```go
	// Prune version snapshots first (they reference audit entries)
	snapshotsPruned, err := p.auditService.PruneVersionSnapshots(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("version snapshots", err))
	} else if snapshotsPruned > 0 {
		logger.Info("pruned %d version snapshots", snapshotsPruned)
	}

	// Then prune audit entries
	entriesPruned, err := p.auditService.PruneAuditEntries(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("audit entries", err))
	} else if entriesPruned > 0 {
		logger.Info("pruned %d audit entries", entriesPruned)
	}

	// Purge expired tombstones (soft-deleted entities past retention period)
	tombstonesPurged, err := p.auditService.PurgeTombstones(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("expired tombstones", err))
	} else if tombstonesPurged > 0 {
		logger.Info("purged %d expired tombstones", tombstonesPurged)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestPruneFailureMessage`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/audit_pruner.go api/audit_pruner_test.go
git commit -m "feat(api): log actionable error when pruning hits the append-only floor

Refs #453.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: PostgreSQL integration test for the age floor

**Files:**
- Create: `api/audit_append_only_integration_test.go`

- [ ] **Step 1: Write the integration test**

Follow the repo pattern from `api/extraction_job_store_integration_test.go`: build tag `//go:build dev || test || integration`, real PG via `TEST_DB_*` env vars. The trigger behavior only exists on PG/Oracle, so skip (don't fall back to SQLite) when `TEST_DB_*` is unset. Backdating rows must happen BEFORE trigger installation (UPDATEs are blocked afterward).

```go
//go:build dev || test || integration

package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openAppendOnlyIntegrationDB opens the real PostgreSQL integration database.
// The append-only triggers only exist on PG/Oracle, so this test SKIPS when
// TEST_DB_* is unset instead of falling back to SQLite.
func openAppendOnlyIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	port := os.Getenv("TEST_DB_PORT")
	user := os.Getenv("TEST_DB_USER")
	password := os.Getenv("TEST_DB_PASSWORD")
	dbname := os.Getenv("TEST_DB_NAME")
	if host == "" || port == "" || user == "" || dbname == "" {
		t.Skip("TEST_DB_* not set; append-only trigger test requires PostgreSQL")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		host, port, user, password, dbname,
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err, "open PostgreSQL integration DB")
	require.NoError(t, db.AutoMigrate(&models.AuditEntry{}, &models.VersionSnapshot{}))
	return db
}

// seedBackdatedEntry inserts an audit entry and then backdates created_at by
// rawSQL. Must run BEFORE trigger installation (the backdate is an UPDATE).
func seedBackdatedEntry(t *testing.T, db *gorm.DB, ageDays int) string {
	t.Helper()
	v := 1
	entry := models.AuditEntry{
		ThreatModelID: models.DBVarchar(uuid.New().String()),
		ObjectType:    models.DBVarchar("threat_model"),
		ObjectID:      models.DBVarchar(uuid.New().String()),
		Version:       &v,
		ChangeType:    models.DBVarchar("created"),
		ActorEmail:    models.DBVarchar("alice@tmi.local"),
		ActorProvider: models.DBVarchar("tmi"),
	}
	require.NoError(t, db.Create(&entry).Error)
	backdated := time.Now().UTC().AddDate(0, 0, -ageDays)
	require.NoError(t, db.Exec("UPDATE audit_entries SET created_at = ? WHERE id = ?", backdated, entry.ID).Error)
	return string(entry.ID)
}

func TestAppendOnlyTriggers_AgeFloor_Integration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	ctx := context.Background()

	// Clean slate for this test's rows; remove any pre-existing triggers so
	// the backdating UPDATEs work.
	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error)
	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error)

	oldID := seedBackdatedEntry(t, db, 40)   // older than the 30-day floor below
	youngID := seedBackdatedEntry(t, db, 5)  // younger than the floor

	// Install with floors: audit_entries 30d (retention 31 → 30), snapshots 7d.
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:     31,
		VersionRetentionDays:   90,
		TombstoneRetentionDays: 8,
	}))
	t.Cleanup(func() {
		// Leave the dev DB in its normal state for other tests.
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error
		_ = db.Exec("DELETE FROM audit_entries WHERE id IN ?", []string{youngID}).Error
	})

	t.Run("delete of aged row succeeds", func(t *testing.T) {
		res := db.Exec("DELETE FROM audit_entries WHERE id = ?", oldID)
		require.NoError(t, res.Error)
		assert.Equal(t, int64(1), res.RowsAffected)
	})

	t.Run("delete of young row is blocked and classifies", func(t *testing.T) {
		err := db.Exec("DELETE FROM audit_entries WHERE id = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation, got: %v", err)
	})

	t.Run("update is blocked regardless of age", func(t *testing.T) {
		err := db.Exec("UPDATE audit_entries SET change_type = 'updated' WHERE id = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation, got: %v", err)
	})
}

func TestPruneAuditEntries_WorksWithTriggers_Integration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	ctx := context.Background()

	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error)
	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error)

	// Entry aged past a 35-day retention (and past the 30-day hard-min floor).
	prunableID := seedBackdatedEntry(t, db, 40)

	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:     35,
		VersionRetentionDays:   90,
		TombstoneRetentionDays: 30,
	}))
	t.Cleanup(func() {
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error
	})

	t.Setenv("AUDIT_RETENTION_DAYS", "35")
	svc := NewGormAuditService(db)
	pruned, err := svc.PruneAuditEntries(ctx)
	require.NoError(t, err, "prune must succeed through the age-floored trigger")
	assert.GreaterOrEqual(t, pruned, 1)

	var count int64
	require.NoError(t, db.Model(&models.AuditEntry{}).Where("id = ?", prunableID).Count(&count).Error)
	assert.Equal(t, int64(0), count, "backdated entry should have been pruned")
}
```

NOTE for the implementer: check the actual field names/types on `models.AuditEntry` (`api/models/audit.go`) before relying on this snippet — `DBVarchar`, `ID` type, and the `created` change-type constant must match the model. Adjust the seed helper accordingly; the test's assertions are the contract.

- [ ] **Step 2: Run against PostgreSQL**

Run: `make test-integration name=TestAppendOnlyTriggers_AgeFloor_Integration` (check `make list-targets` for the exact arg convention; if `name=` is not supported for `test-integration`, run the whole suite)
Expected: PASS. Also run `make test-integration name=TestPruneAuditEntries_WorksWithTriggers_Integration` — PASS.

- [ ] **Step 3: Run the full integration suite**

Run: `make test-integration`
Expected: all green — this catches any existing test that relied on deleting/updating young audit rows under the new triggers.

- [ ] **Step 4: Commit**

```bash
git add api/audit_append_only_integration_test.go
git commit -m "test(integration): cover age-floored append-only triggers on PostgreSQL

Refs #453.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Quality gates, Oracle review, wiki, close-out

**Files:**
- None new (verification + ops)

- [ ] **Step 1: Lint and full local gates**

Run: `make lint` — fix anything it reports.
Run: `make build-server && make test-unit && make test-integration`
Expected: all green.

- [ ] **Step 2: Check cmd/dbtool for trigger awareness**

Run: `grep -rn "tmi_audit\|no_mutate\|append.only" cmd/dbtool/`
If dbtool has a schema-verification feature that enumerates these triggers, update it to reflect the new floor-parameterized definitions; if the grep is empty, nothing to do (no column/index changed).

- [ ] **Step 3: MANDATORY — oracle-db-admin review**

Invoke the `oracle-db-admin` skill and dispatch the subagent with the diff of `internal/dbschema/audit_append_only.go`, `api/audit_store.go`, `api/audit_pruner.go`, and `cmd/server/main.go`. Specific questions to pose:
- Is `:OLD.created_at < SYS_EXTRACT_UTC(SYSTIMESTAMP) - NUMTODSINTERVAL(n, 'DAY')` the correct UTC-safe comparison for the column type GORM creates for `created_at` on Oracle (TIMESTAMP vs TIMESTAMP WITH TIME ZONE)?
- Any issue with CREATE OR REPLACE TRIGGER under the serializable-by-default transaction posture (#449)?

Address every BLOCKING finding before proceeding; fold APPROVED WITH NOTES items in or file follow-ups.

- [ ] **Step 4: Oracle ADB verification**

Run: `make test-integration-oci` (requires `scripts/oci-env.sh`)
Expected: green, including the trigger install on a real ADB connection (acceptance criterion on #453). If the new PG-only integration test needs an Oracle twin, the oracle-db-admin review will have said so — follow its guidance.

- [ ] **Step 5: Security review**

Run the `security-review` skill on the branch changes (trigger semantics are security-sensitive: T19). Stop and surface any findings to the user before continuing.

- [ ] **Step 6: File the follow-up issue for the snapshot-orphan leak**

The TM hard-delete cascade (`api/tombstone_store.go` `hardDeleteTx`) never deletes children's `version_snapshots` — pre-existing leak, out of #453's scope. Use the `github:create-issue` skill: type bug, title `bug(api): TM hard-delete cascade orphans version_snapshots of child entities`, body referencing this plan and `api/audit_store.go` PurgeTombstones (which DOES clean up sub-resource snapshots) as the inconsistent counterpart. Milestone: leave for triage (no milestone). Status: Backlog.

- [ ] **Step 7: Update the wiki**

The old code comment promised an operator runbook for `DISABLE TRIGGER`-based archival as "the only supported path". The policy is now age-floored in-app pruning. Update the TMI wiki (https://github.com/ericfitz/tmi/wiki — local checkout at `/Users/efitz/Projects/tmi-wiki` per `.local-projects.json`): find the audit/retention or operator-runbook page (`grep -ril "append-only\|DISABLE TRIGGER\|audit" /Users/efitz/Projects/tmi-wiki`) and rewrite the retention section to describe: the age-floor policy, the floor table from the spec, the boot-time install, the hard minimums, and that `DISABLE TRIGGER` is no longer needed for retention (still available to DBAs for true emergencies). Commit and push the wiki repo.

- [ ] **Step 8: Close the issue and land per session-completion workflow**

```bash
git pull --rebase
git push
git status   # MUST show "up to date with origin"
gh issue comment 453 --body "Resolved on branch dev/1.4.0 (see commits referencing #453: age-floored append-only triggers, immutable audit rows, actionable pruner logging). Verified on PostgreSQL integration suite and Oracle ADB (test-integration-oci); oracle-db-admin verdict recorded in the session summary."
gh issue close 453
```

(Commits are on `dev/1.4.0`, not `main`, so GitHub will not auto-close — the explicit close is required.)

---

## Self-Review Notes (already applied)

- Spec coverage: trigger floors (Tasks 1-2), immutable audit rows + dead-code removal (Task 3), pruner logging (Task 4), PG integration tests incl. blocked-young-delete, blocked-update, end-to-end prune (Task 5), Oracle verification + oracle-db-admin + wiki + follow-up issue (Task 6). The "floor clamps to 30 when retention configured lower" behavior is covered at unit level (Task 1 table case 2) rather than integration level — acceptable, the clamp is pure Go.
- The 410-rollback path needs no code change (`RollbackToVersion` branch (b): `GetSnapshot` error → `GoneError`); the legacy `entry.Version == nil` branch stays for rows nulled before this change.
- Type consistency: `AuditFloorConfig` fields and the two floor methods are used with identical names in Tasks 1, 2, and 5; `pruneFailureMessage` matches between Task 4 test and impl.
