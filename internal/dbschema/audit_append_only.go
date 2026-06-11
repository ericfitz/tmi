// Package dbschema: append-only triggers for audit_entries and version_snapshots.
//
// T19 (#356): even with the application-level "audit-emit on every mutation"
// instrumentation, two failure modes remain:
//   - a code path that mutates a row but forgets to call the audit-emit helper
//     produces a silent change.
//   - a code path with PATCH access to the audit_entries table itself, an
//     admin running a raw SQL DELETE, or a hostile migration can erase or
//     alter history.
//
// The fix is a DB-level trigger on audit_entries and version_snapshots that
// raises an exception on any UPDATE, and on DELETE of rows younger than a
// per-table age floor derived from retention config (#453). The trigger is
// the last-line defense — if it fires, something at the application or
// operator layer is trying to mutate immutable history, and the right
// behavior is to refuse the operation.
//
// The triggers are installed idempotently via CREATE OR REPLACE; the two
// dialects (PostgreSQL and Oracle ADB) need slightly different syntax.
// SQLite (used by some narrow unit tests) does not support BEFORE-statement
// triggers in the same way; we skip on that dialect — single-process SQLite
// is single-writer and the at-rest tampering scenarios this guards against
// don't apply to an in-memory test DB.
package dbschema

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

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
