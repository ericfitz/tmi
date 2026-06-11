# System Audit Retention + Tamper Protection (#400) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add retention pruning and age-floored append-only tamper protection for `system_audit_entries` (T7 evidence of `/admin/*` writes).

**Architecture:** Extends the #453 age-floor machinery: a third trigger on `system_audit_entries` (UPDATE always blocked, DELETE allowed past a 90-day-minimum floor), a new `SYSTEM_AUDIT_RETENTION_DAYS` env knob (default 365, min 90), a `PruneSystemAuditEntries` method wired into the existing 24h `AuditPruner` cycle.

**Tech Stack:** Go, GORM, PostgreSQL (plpgsql), Oracle ADB (PL/SQL), testify.

**Spec:** `docs/superpowers/specs/2026-06-11-400-system-audit-retention-design.md` — read it first.

**PREREQUISITE:** The #453 plan (`docs/superpowers/plans/2026-06-11-453-audit-age-floor.md`) must be fully implemented first — this plan modifies the `AuditFloorConfig` type, `clampFloor`, `pruneFailureMessage`, and exported retention readers that #453 creates. Verify before starting: `grep -n "AuditFloorConfig" internal/dbschema/audit_append_only.go` must return a hit.

**Branch:** work on `dev/1.4.0`.

---

### Task 1: Retention config reader with 90-day clamp (TDD)

**Files:**
- Modify: `api/audit_store.go`
- Create or extend: `api/audit_retention_config_test.go`

- [ ] **Step 1: Write the failing test**

Create `api/audit_retention_config_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemAuditRetentionDays_Default(t *testing.T) {
	// t.Setenv with empty registers a cleanup AND unsets for this test
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "")
	assert.Equal(t, 365, SystemAuditRetentionDays())
}

func TestSystemAuditRetentionDays_Configured(t *testing.T) {
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "180")
	assert.Equal(t, 180, SystemAuditRetentionDays())
}

func TestSystemAuditRetentionDays_ClampsToMinimum(t *testing.T) {
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "30")
	assert.Equal(t, 90, SystemAuditRetentionDays(),
		"system audit retention must clamp to the 90-day evidence minimum")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestSystemAuditRetentionDays`
Expected: FAIL — `undefined: SystemAuditRetentionDays`

- [ ] **Step 3: Implement**

In `api/audit_store.go`:

1. Add to the retention constants block (which after #453 holds `defaultAuditRetentionDays` etc.):

```go
	defaultSystemAuditRetentionDays = 365
	// minSystemAuditRetentionDays is the documented evidence minimum: system
	// audit rows are T7 evidence and accidentally-aggressive pruning destroys
	// investigative value (#400).
	minSystemAuditRetentionDays = 90
```

2. Add the exported reader next to `AuditRetentionDays()` (created by #453):

```go
// SystemAuditRetentionDays returns the configured system-audit retention in
// days (SYSTEM_AUDIT_RETENTION_DAYS, default 365), clamped to a 90-day
// minimum. Exported because the append-only trigger installation derives its
// delete age floor from the same value (#400).
func SystemAuditRetentionDays() int {
	days := getEnvInt("SYSTEM_AUDIT_RETENTION_DAYS", defaultSystemAuditRetentionDays)
	if days < minSystemAuditRetentionDays {
		slogging.Get().Warn("SYSTEM_AUDIT_RETENTION_DAYS=%d is below the %d-day evidence minimum; using %d",
			days, minSystemAuditRetentionDays, minSystemAuditRetentionDays)
		return minSystemAuditRetentionDays
	}
	return days
}
```

3. Add the field to `GormAuditService` (struct currently has `auditRetentionDays`, `versionRetentionCount`, `versionRetentionDays`, `tombstoneRetentionDays`):

```go
	systemAuditRetentionDays int
```

and populate it in `NewGormAuditService`:

```go
		systemAuditRetentionDays: SystemAuditRetentionDays(),
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestSystemAuditRetentionDays`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/audit_store.go api/audit_retention_config_test.go
git commit -m "feat(api): add SYSTEM_AUDIT_RETENTION_DAYS with 90-day evidence minimum

Refs #400.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Third age-floored trigger on system_audit_entries (TDD)

**Files:**
- Modify: `internal/dbschema/audit_append_only.go`
- Modify: `internal/dbschema/audit_append_only_test.go`
- Modify: `cmd/server/main.go` (the `InstallAuditAppendOnlyTriggers` call, ~line 409)

- [ ] **Step 1: Extend the floor test (failing)**

In `internal/dbschema/audit_append_only_test.go`, add cases to the `TestAuditFloorConfig_Floors` table (add a `wantSystemAuditFloor int` column to the struct and an assertion `assert.Equal(t, tt.wantSystemAuditFloor, tt.cfg.systemAuditEntriesFloorDays())`):

```go
		{
			name:                  "system audit default",
			cfg:                   AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 30, SystemAuditRetentionDays: 365},
			wantAuditFloor:        364,
			wantSnapshotFloor:     29,
			wantSystemAuditFloor:  364,
		},
		{
			name:                  "system audit clamps to 90-day hard minimum",
			cfg:                   AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 30, SystemAuditRetentionDays: 30},
			wantAuditFloor:        364,
			wantSnapshotFloor:     29,
			wantSystemAuditFloor:  90,
		},
```

Set `SystemAuditRetentionDays: 365` and `wantSystemAuditFloor: 364` on the pre-existing #453 cases too (zero would clamp to 90 and is fine, but explicit values keep the table readable).

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestAuditFloorConfig_Floors`
Expected: FAIL — unknown field `SystemAuditRetentionDays` / `undefined: systemAuditEntriesFloorDays`

- [ ] **Step 3: Implement the floor + triggers**

In `internal/dbschema/audit_append_only.go`:

1. Add to `AuditFloorConfig`:

```go
	SystemAuditRetentionDays int
```

2. Add next to the other hard-minimum constants:

```go
	// systemAuditFloorHardMinDays matches the 90-day evidence minimum on
	// SYSTEM_AUDIT_RETENTION_DAYS: system_audit_entries is pure T7 evidence
	// and nothing legitimate ever deletes young rows from it (#400).
	systemAuditFloorHardMinDays = 90
```

3. Add the floor method:

```go
func (c AuditFloorConfig) systemAuditEntriesFloorDays() int {
	return clampFloor(c.SystemAuditRetentionDays, systemAuditFloorHardMinDays)
}
```

4. In `InstallAuditAppendOnlyTriggers`, compute `systemAuditFloor := floors.systemAuditEntriesFloorDays()`, add the misconfig warning, and pass the floor to both installers:

```go
	if floors.SystemAuditRetentionDays-1 < systemAuditFloorHardMinDays {
		logger.Warn("InstallAuditAppendOnlyTriggers: configured SYSTEM_AUDIT_RETENTION_DAYS=%d is below the %d-day evidence floor; pruning of system_audit_entries younger than %d days will be blocked",
			floors.SystemAuditRetentionDays, systemAuditFloorHardMinDays, systemAuditFloorHardMinDays)
	}
```

and change the installer signatures to take the third floor:
`installPostgresAppendOnly(ctx, db, logger, auditFloor, snapshotFloor, systemAuditFloor)` (same for Oracle).

5. In `installPostgresAppendOnly`, append to `statements` (the guard function from #453 already takes the floor as `TG_ARGV[0]` — reuse it):

```go
		// system_audit_entries trigger (#400)
		`DROP TRIGGER IF EXISTS tmi_system_audit_entries_no_mutate ON system_audit_entries;`,
		fmt.Sprintf(`CREATE TRIGGER tmi_system_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON system_audit_entries
		 FOR EACH ROW EXECUTE FUNCTION tmi_audit_append_only_guard('%d');`, systemAuditFloorDays),
```

and update the closing log line to mention all three floors.

6. In `installOracleAppendOnly`, append:

```go
		fmt.Sprintf(`CREATE OR REPLACE TRIGGER tmi_system_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON system_audit_entries
		 FOR EACH ROW
		 BEGIN
		   IF DELETING AND :OLD.created_at < SYS_EXTRACT_UTC(SYSTIMESTAMP) - NUMTODSINTERVAL(%d, 'DAY') THEN
		     NULL;
		   ELSE
		     RAISE_APPLICATION_ERROR(-20001, 'system audit history is append-only: ' || (CASE WHEN UPDATING THEN 'UPDATE' ELSE 'DELETE' END) || ' on system_audit_entries blocked by tmi_system_audit_entries_no_mutate (DELETE allowed only for rows older than %d days)');
		   END IF;
		 END;`, systemAuditFloorDays, systemAuditFloorDays),
```

(keep the word "append-only" in the message — `dberrors` classifies PG P0001 by that substring)
and update the closing log line.

7. Update the function and package doc comments to say three tables are protected.

8. In `cmd/server/main.go`, add the field to the existing `AuditFloorConfig` literal (from #453):

```go
	SystemAuditRetentionDays: api.SystemAuditRetentionDays(),
```

- [ ] **Step 4: Run tests and build**

Run: `make test-unit name=TestAuditFloorConfig_Floors` — PASS.
Run: `make build-server` — succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/dbschema/audit_append_only.go internal/dbschema/audit_append_only_test.go cmd/server/main.go
git commit -m "feat(db): age-floored append-only trigger on system_audit_entries

Refs #400.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: PruneSystemAuditEntries (TDD via integration-style unit on SQLite)

**Files:**
- Modify: `api/audit_service.go` (interface)
- Modify: `api/audit_store.go` (implementation)
- Modify: `api/audit_pruner.go` (fourth prune call)
- Modify: `api/audit_debouncer_test.go` (mock — add stub)
- Create: `api/system_audit_prune_test.go`

- [ ] **Step 1: Write the failing test**

SQLite has no triggers installed (skipped dialect), so prune logic is unit-testable in-memory. Create `api/system_audit_prune_test.go`:

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

func setupSystemAuditPruneDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	return db
}

func seedSystemAuditEntry(t *testing.T, db *gorm.DB, ageDays int) string {
	t.Helper()
	entry := models.SystemAuditEntry{
		ID:               models.DBVarchar(uuid.New().String()),
		ActorEmail:       models.DBVarchar("charlie@tmi.local"),
		ActorProvider:    models.DBVarchar("tmi"),
		ActorProviderID:  models.DBVarchar("charlie"),
		ActorDisplayName: models.DBVarchar("Charlie"),
		HTTPMethod:       models.DBVarchar("PUT"),
		HTTPPath:         models.DBText("/admin/settings/test"),
		FieldPath:        models.DBVarchar("test"),
	}
	require.NoError(t, db.Create(&entry).Error)
	backdated := time.Now().UTC().AddDate(0, 0, -ageDays)
	require.NoError(t, db.Exec("UPDATE system_audit_entries SET created_at = ? WHERE id = ?", backdated, entry.ID).Error)
	return string(entry.ID)
}

func TestPruneSystemAuditEntries(t *testing.T) {
	db := setupSystemAuditPruneDB(t)
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "100")

	oldID := seedSystemAuditEntry(t, db, 150)   // past retention
	youngID := seedSystemAuditEntry(t, db, 50)  // within retention

	svc := NewGormAuditService(db)
	pruned, err := svc.PruneSystemAuditEntries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, pruned)

	var count int64
	require.NoError(t, db.Model(&models.SystemAuditEntry{}).Where("id = ?", oldID).Count(&count).Error)
	assert.Equal(t, int64(0), count, "aged entry should be pruned")
	require.NoError(t, db.Model(&models.SystemAuditEntry{}).Where("id = ?", youngID).Count(&count).Error)
	assert.Equal(t, int64(1), count, "young entry must survive")
}

func TestPruneSystemAuditEntries_NothingToPrune(t *testing.T) {
	db := setupSystemAuditPruneDB(t)
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "100")
	seedSystemAuditEntry(t, db, 10)

	svc := NewGormAuditService(db)
	pruned, err := svc.PruneSystemAuditEntries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, pruned)
}
```

NOTE for the implementer: verify the `models.SystemAuditEntry` field names/types against `api/models/system_audit.go` (shown in the spec exploration: `ID`, `ActorEmail`, `ActorProvider`, `ActorProviderID`, `ActorDisplayName`, `HTTPMethod`, `HTTPPath`, `FieldPath`, `CreatedAt`) and whether `ID` is auto-generated (if a BeforeCreate hook exists, drop the explicit ID).

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestPruneSystemAuditEntries`
Expected: FAIL — `svc.PruneSystemAuditEntries undefined`

- [ ] **Step 3: Implement**

1. In `api/audit_service.go`, add to `AuditServiceInterface` (after `PruneAuditEntries`):

```go
	// PruneSystemAuditEntries removes system audit entries older than the
	// configured retention period (SYSTEM_AUDIT_RETENTION_DAYS, default 365,
	// minimum 90). Returns the number of entries pruned.
	PruneSystemAuditEntries(ctx context.Context) (int, error)
```

2. In `api/audit_store.go`, add the implementation (after `PruneAuditEntries`):

```go
// PruneSystemAuditEntries removes system audit entries older than the
// configured retention period. Unlike threat-model audit, there are no
// tombstone rows to preserve — every row past retention is deleted.
func (s *GormAuditService) PruneSystemAuditEntries(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -s.systemAuditRetentionDays)

	var pruned int
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		res := tx.Where("created_at < ?", cutoff).Delete(&models.SystemAuditEntry{})
		if res.Error != nil {
			return fmt.Errorf("failed to prune system audit entries: %w", res.Error)
		}
		pruned = int(res.RowsAffected)
		return nil
	})

	return pruned, err
}
```

3. In `api/audit_pruner.go` `prune()`, add after the `PruneAuditEntries` block (before tombstones):

```go
	// Prune system audit entries (admin-write evidence, #400)
	systemPruned, err := p.auditService.PruneSystemAuditEntries(ctx)
	if err != nil {
		logger.Error("%s", pruneFailureMessage("system audit entries", err))
	} else if systemPruned > 0 {
		logger.Info("pruned %d system audit entries", systemPruned)
	}
```

4. In `api/audit_debouncer_test.go`, add to `mockAuditService`:

```go
func (m *mockAuditService) PruneSystemAuditEntries(_ context.Context) (int, error) {
	return 0, nil
}
```

(If other mocks of `AuditServiceInterface` exist — `grep -rn "AuditServiceInterface" --include="*_test.go" api/` — add the stub there too.)

- [ ] **Step 4: Run tests and build**

Run: `make test-unit name=TestPruneSystemAuditEntries` — PASS.
Run: `make build-server && make test-unit` — all green.

- [ ] **Step 5: Commit**

```bash
git add api/audit_service.go api/audit_store.go api/audit_pruner.go api/audit_debouncer_test.go api/system_audit_prune_test.go
git commit -m "feat(api): prune system_audit_entries past retention in the audit pruner

Refs #400.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: PostgreSQL integration test for the system-audit trigger

**Files:**
- Create: `api/system_audit_append_only_integration_test.go`

- [ ] **Step 1: Write the integration test**

Mirror the #453 integration test (`api/audit_append_only_integration_test.go` — reuse its `openAppendOnlyIntegrationDB` helper if package-visible, otherwise duplicate the open logic):

```go
//go:build dev || test || integration

package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemAuditAppendOnly_AgeFloor_Integration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t) // skips when TEST_DB_* unset
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	ctx := context.Background()

	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_system_audit_entries_no_mutate ON system_audit_entries").Error)

	oldID := seedSystemAuditEntry(t, db, 100)  // older than the 90-day hard-min floor
	youngID := seedSystemAuditEntry(t, db, 10) // younger than the floor

	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:       365,
		VersionRetentionDays:     90,
		TombstoneRetentionDays:   30,
		SystemAuditRetentionDays: 91, // floor = 90
	}))
	t.Cleanup(func() {
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_system_audit_entries_no_mutate ON system_audit_entries").Error
		_ = db.Exec("DELETE FROM system_audit_entries WHERE id = ?", youngID).Error
	})

	t.Run("delete of aged row succeeds", func(t *testing.T) {
		res := db.Exec("DELETE FROM system_audit_entries WHERE id = ?", oldID)
		require.NoError(t, res.Error)
		assert.Equal(t, int64(1), res.RowsAffected)
	})

	t.Run("delete of young row is blocked", func(t *testing.T) {
		err := db.Exec("DELETE FROM system_audit_entries WHERE id = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation))
	})

	t.Run("update is blocked regardless of age", func(t *testing.T) {
		err := db.Exec("UPDATE system_audit_entries SET actor_email = 'evil@tmi.local' WHERE id = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation))
	})

	t.Run("prune deletes aged rows through the trigger", func(t *testing.T) {
		agedID := func() string {
			// seed pre-dates trigger? No — INSERTs are always allowed; only
			// the backdating UPDATE is blocked. Backdate via direct SQL is an
			// UPDATE, so drop + reinstall around the seed.
			require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_system_audit_entries_no_mutate ON system_audit_entries").Error)
			id := seedSystemAuditEntry(t, db, 200)
			require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
				AuditRetentionDays:       365,
				VersionRetentionDays:     90,
				TombstoneRetentionDays:   30,
				SystemAuditRetentionDays: 91,
			}))
			return id
		}()

		t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "91")
		svc := NewGormAuditService(db)
		pruned, err := svc.PruneSystemAuditEntries(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, pruned, 1)

		var count int64
		require.NoError(t, db.Model(&models.SystemAuditEntry{}).Where("id = ?", agedID).Count(&count).Error)
		assert.Equal(t, int64(0), count)
	})
}
```

(`seedSystemAuditEntry` comes from Task 3's `api/system_audit_prune_test.go` — same package, no build-tag mismatch problem since plain test files are included in tagged builds. If the #453 helper `openAppendOnlyIntegrationDB` was named differently, adapt.)

- [ ] **Step 2: Run against PostgreSQL**

Run: `make test-integration name=TestSystemAuditAppendOnly_AgeFloor_Integration` (or full `make test-integration` if `name=` is unsupported)
Expected: PASS.

- [ ] **Step 3: Run the full integration suite**

Run: `make test-integration`
Expected: all green — in particular, no existing test does UPDATE/DELETE on young `system_audit_entries` rows (the #355 tests write and read; if one mutates, fix the test to seed instead of mutate).

- [ ] **Step 4: Commit**

```bash
git add api/system_audit_append_only_integration_test.go
git commit -m "test(integration): cover system_audit_entries age-floored trigger

Refs #400.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Quality gates, Oracle review, close-out

**Files:**
- None new (verification + ops)

- [ ] **Step 1: Full local gates**

Run: `make lint && make build-server && make test-unit && make test-integration`
Expected: all green.

- [ ] **Step 2: MANDATORY — oracle-db-admin review**

Invoke the `oracle-db-admin` skill and dispatch the subagent with the diff of `internal/dbschema/audit_append_only.go`, `api/audit_store.go`, `api/audit_pruner.go`, `api/audit_service.go`, `cmd/server/main.go`. Specific question: the third trigger's `:OLD.created_at` comparison on `system_audit_entries` — same column-type/UTC semantics as the #453 triggers? Address every BLOCKING finding.

- [ ] **Step 3: Oracle ADB verification**

Run: `make test-integration-oci` (requires `scripts/oci-env.sh`)
Expected: green; the three triggers install on a real ADB connection.

- [ ] **Step 4: Security review**

Run the `security-review` skill on the branch changes (T7 evidence protection is security-sensitive). Stop and surface findings to the user before continuing.

- [ ] **Step 5: Config documentation**

`SYSTEM_AUDIT_RETENTION_DAYS` is a new env knob. Check how the existing retention vars are documented: `grep -rn "AUDIT_RETENTION_DAYS" config-example.yml .env.dev cmd/genconfig/ 2>/dev/null`. If the genconfig generator or example config enumerates them, add the new variable there the same way (then regenerate: check `make list-targets` for a genconfig target). Also add the variable to the wiki's configuration/retention page (local checkout `/Users/efitz/Projects/tmi-wiki`): document default 365, minimum 90, and that the trigger floor is baked at boot. Commit and push the wiki repo.

- [ ] **Step 6: Close the issue and land per session-completion workflow**

```bash
git pull --rebase
git push
git status   # MUST show "up to date with origin"
gh issue comment 400 --body "Resolved on branch dev/1.4.0: SYSTEM_AUDIT_RETENTION_DAYS (default 365, min 90), PruneSystemAuditEntries in the 24h pruner cycle, and an age-floored append-only trigger on system_audit_entries (UPDATEs always blocked; DELETEs only past the floor). Archive and partitioning deliberately deferred — see #454. Verified on PG integration suite and Oracle ADB; oracle-db-admin verdict recorded in the session summary."
gh issue close 400
```

(Commits are on `dev/1.4.0`, not `main` — explicit close required.)

---

## Self-Review Notes (already applied)

- Spec coverage: config reader + clamp (Task 1), trigger + floor (Task 2), prune method + pruner wiring (Task 3), PG integration incl. blocked young delete/update and end-to-end prune (Task 4), Oracle + reviews + config docs + close (Task 5). Hard delete and no-partitioning need no code; #454 already filed.
- Dependency on #453 stated up top with a concrete verification grep.
- Type consistency: `AuditFloorConfig.SystemAuditRetentionDays` and `systemAuditEntriesFloorDays()` used identically in Tasks 2 and 4; `seedSystemAuditEntry` defined in Task 3, reused in Task 4; `pruneFailureMessage` comes from #453.
