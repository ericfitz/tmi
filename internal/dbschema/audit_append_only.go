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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// AuditFloorConfig carries the retention configuration used to derive the
// per-table delete age floors baked into the append-only triggers at install
// time. The values come from the same env config the audit pruner reads
// (AUDIT_RETENTION_DAYS, VERSION_RETENTION_DAYS, TOMBSTONE_RETENTION_DAYS,
// SYSTEM_AUDIT_RETENTION_DAYS), so the trigger floor and the pruner cutoff
// cannot drift within one boot.
// SEM@c3167c5165bed5f9d97b7f7eef032894393a917a: carry per-table retention days used to derive append-only trigger age floors (pure)
type AuditFloorConfig struct {
	AuditRetentionDays       int
	VersionRetentionDays     int
	TombstoneRetentionDays   int
	SystemAuditRetentionDays int
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
	// systemAuditFloorHardMinDays matches the 90-day evidence minimum on
	// SYSTEM_AUDIT_RETENTION_DAYS: system_audit_entries is pure T7 evidence
	// and nothing legitimate ever deletes young rows from it (#400).
	systemAuditFloorHardMinDays = 90
)

// clampFloor converts a configured retention into an installed trigger
// floor: one day of clock-skew margin below the retention (the pruner
// compares app-side time, the trigger DB-side time), but never below the
// hard minimum.
// SEM@0b84836aa30071e2ed6d01591c7dd3254be74189: compute the trigger delete-age floor from configured retention, enforcing a hard minimum (pure)
func clampFloor(configuredDays, hardMinDays int) int {
	floor := configuredDays - 1
	if floor < hardMinDays {
		return hardMinDays
	}
	return floor
}

// SEM@0b84836aa30071e2ed6d01591c7dd3254be74189: compute the append-only delete-age floor in days for the audit_entries table (pure)
func (c AuditFloorConfig) auditEntriesFloorDays() int {
	return clampFloor(c.AuditRetentionDays, auditFloorHardMinDays)
}

// SEM@0b84836aa30071e2ed6d01591c7dd3254be74189: compute the append-only delete-age floor in days for the version_snapshots table (pure)
func (c AuditFloorConfig) versionSnapshotsFloorDays() int {
	v := c.VersionRetentionDays
	if c.TombstoneRetentionDays < v {
		v = c.TombstoneRetentionDays
	}
	return clampFloor(v, snapshotFloorHardMinDays)
}

// SEM@c3167c5165bed5f9d97b7f7eef032894393a917a: compute the append-only delete-age floor in days for the system_audit_entries table (pure)
func (c AuditFloorConfig) systemAuditEntriesFloorDays() int {
	return clampFloor(c.SystemAuditRetentionDays, systemAuditFloorHardMinDays)
}

// InstallAuditAppendOnlyTriggers installs triggers on audit_entries,
// version_snapshots, and system_audit_entries that block all UPDATEs and
// block DELETEs of rows younger than a per-table age floor. Idempotent
// across re-runs (the server reinstalls on every boot, so floor changes
// take effect on restart).
//
// Retention policy (#453, #400): aged-out pruning is a supported, in-app
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
// SEM@c3167c5165bed5f9d97b7f7eef032894393a917a: install DB-level append-only triggers on audit tables, blocking UPDATE and recent DELETE (mutates DB schema)
func InstallAuditAppendOnlyTriggers(ctx context.Context, db *gorm.DB, floors AuditFloorConfig) error {
	logger := slogging.Get()
	dialect := db.Name()

	auditFloor := floors.auditEntriesFloorDays()
	snapshotFloor := floors.versionSnapshotsFloorDays()
	systemAuditFloor := floors.systemAuditEntriesFloorDays()
	if floors.AuditRetentionDays-1 < auditFloorHardMinDays {
		logger.Warn("InstallAuditAppendOnlyTriggers: configured AUDIT_RETENTION_DAYS=%d is below the %d-day immutability floor; pruning of audit_entries younger than %d days will be blocked",
			floors.AuditRetentionDays, auditFloorHardMinDays, auditFloorHardMinDays)
	}
	if min(floors.VersionRetentionDays, floors.TombstoneRetentionDays)-1 < snapshotFloorHardMinDays {
		logger.Warn("InstallAuditAppendOnlyTriggers: configured snapshot retention (min of VERSION_RETENTION_DAYS=%d, TOMBSTONE_RETENTION_DAYS=%d) is below the %d-day immutability floor; snapshot deletion younger than %d days will be blocked",
			floors.VersionRetentionDays, floors.TombstoneRetentionDays, snapshotFloorHardMinDays, snapshotFloorHardMinDays)
	}
	if floors.SystemAuditRetentionDays-1 < systemAuditFloorHardMinDays {
		logger.Warn("InstallAuditAppendOnlyTriggers: configured SYSTEM_AUDIT_RETENTION_DAYS=%d is below the %d-day evidence floor; pruning of system_audit_entries younger than %d days will be blocked",
			floors.SystemAuditRetentionDays, systemAuditFloorHardMinDays, systemAuditFloorHardMinDays)
	}

	switch dialect {
	case "postgres":
		return installPostgresAppendOnly(ctx, db, logger, auditFloor, snapshotFloor, systemAuditFloor)
	case "oracle":
		return installOracleAppendOnly(ctx, db, logger, auditFloor, snapshotFloor, systemAuditFloor)
	case "sqlite":
		logger.Info("InstallAuditAppendOnlyTriggers: skipping on dialect %q (single-process SQLite is single-writer)", dialect)
		return nil
	default:
		logger.Warn("InstallAuditAppendOnlyTriggers: unsupported dialect %q, skipping; T19 protection is NOT in effect", dialect)
		return nil
	}
}

// SEM@c3167c5165bed5f9d97b7f7eef032894393a917a: install append-only guard function and triggers on PostgreSQL audit tables (mutates DB schema)
func installPostgresAppendOnly(ctx context.Context, db *gorm.DB, logger *slogging.Logger, auditFloorDays, snapshotFloorDays, systemAuditFloorDays int) error {
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

		// system_audit_entries trigger (#400)
		`DROP TRIGGER IF EXISTS tmi_system_audit_entries_no_mutate ON system_audit_entries;`,
		fmt.Sprintf(`CREATE TRIGGER tmi_system_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON system_audit_entries
		 FOR EACH ROW EXECUTE FUNCTION tmi_audit_append_only_guard('%d');`, systemAuditFloorDays),
	}

	for _, sql := range statements {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil { // ddl-via-gorm:ok postgres-only (dispatched by dialect in InstallAuditAppendOnlyTriggers)
			return fmt.Errorf("postgres install: %w (sql: %s)", err, sql)
		}
	}
	logger.Info("InstallAuditAppendOnlyTriggers: postgres triggers installed (audit_entries floor=%dd, version_snapshots floor=%dd, system_audit_entries floor=%dd)", auditFloorDays, snapshotFloorDays, systemAuditFloorDays)
	return nil
}

// SEM@c3167c5165bed5f9d97b7f7eef032894393a917a: install append-only row-level triggers on Oracle ADB audit tables (mutates DB schema)
func installOracleAppendOnly(ctx context.Context, db *gorm.DB, logger *slogging.Logger, auditFloorDays, snapshotFloorDays, systemAuditFloorDays int) error {
	// Oracle CREATE OR REPLACE TRIGGER is atomic — no DROP/CREATE pair.
	// RAISE_APPLICATION_ERROR(-20001, ...) bubbles up as ORA-20001;
	// dberrors.classifyOracleCode maps 20001 to ErrAppendOnlyViolation.
	//
	// Both sides of the floor comparison must be plain UTC TIMESTAMP:
	// SYS_EXTRACT_UTC(:OLD.created_at) strips the time-zone offset from the
	// TIMESTAMP WITH TIME ZONE column, and SYS_EXTRACT_UTC(SYSTIMESTAMP)
	// does the same for the DB clock. Without wrapping :OLD.created_at,
	// Oracle promotes the comparison using SESSIONTIMEZONE, which pooled ADB
	// connections do not reliably set to UTC, producing incorrect results.
	triggers := oracleAppendOnlyTriggers(auditFloorDays, snapshotFloorDays, systemAuditFloorDays)

	// Through execMigrationDDL, never gorm.Exec (#763): under PrepareStmt a
	// byte-identical DDL string re-executed on the same *gorm.DB is a silent
	// no-op on Oracle, so a future "reinstall triggers" self-heal would apply
	// nothing. The pinned session also waits out DDL_LOCK_TIMEOUT on these hot
	// tables instead of failing fast with ORA-00054 during a rolling deploy.
	//
	// Steady-state boots issue no DDL (#893): each body carries a marker
	// comment with the sha256 of its own DDL, and the catalog is probed for a
	// VALID, ENABLED trigger of that name whose source still holds the marker.
	// An unconditional CREATE OR REPLACE recompiled three triggers on hot
	// audit tables and invalidated their library-cache objects on every boot,
	// and each could wait out DDL_LOCK_TIMEOUT under the advisory lock.
	installed := 0
	for _, trg := range triggers {
		sql := oracleTriggerDDLWithMarker(trg.ddl)
		marker := oracleTriggerMarker(trg.ddl)
		current, err := oracleTriggerIsCurrent(ctx, db, trg.name, marker)
		if err != nil {
			// A catalog blip must not block T19: fall through to the
			// unconditional CREATE OR REPLACE this replaced.
			logger.Warn("InstallAuditAppendOnlyTriggers: probing trigger %s failed, reinstalling unconditionally: %v", trg.name, err)
		}
		if current {
			continue
		}
		if err := execMigrationDDL(ctx, db, sql); err != nil {
			return fmt.Errorf("oracle install: %w (sql: %s)", err, sql)
		}
		installed++
		// CREATE OR REPLACE succeeds even when the body fails to compile,
		// leaving an INVALID trigger that enforces nothing; make that visible.
		if current, err = oracleTriggerIsCurrent(ctx, db, trg.name, marker); err != nil || !current {
			logger.Warn("InstallAuditAppendOnlyTriggers: trigger %s is not VALID and ENABLED after install; append-only protection may not be in effect (probe err: %v)", trg.name, err)
		}
	}
	logger.Info("InstallAuditAppendOnlyTriggers: oracle triggers current (%d of %d (re)installed; audit_entries floor=%dd, version_snapshots floor=%dd, system_audit_entries floor=%dd)", installed, len(triggers), auditFloorDays, snapshotFloorDays, systemAuditFloorDays)
	return nil
}

// SEM@0000000000000000000000000000000000000000: build the three Oracle append-only trigger definitions for the given floors (pure)
func oracleAppendOnlyTriggers(auditFloorDays, snapshotFloorDays, systemAuditFloorDays int) []oracleTrigger {
	return []oracleTrigger{
		{"tmi_audit_entries_no_mutate", fmt.Sprintf(`CREATE OR REPLACE TRIGGER tmi_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON audit_entries
		 FOR EACH ROW
		 BEGIN
		   IF DELETING AND SYS_EXTRACT_UTC(:OLD.created_at) < SYS_EXTRACT_UTC(SYSTIMESTAMP) - NUMTODSINTERVAL(%d, 'DAY') THEN
		     NULL;
		   ELSE
		     RAISE_APPLICATION_ERROR(-20001, 'audit history is append-only: ' || (CASE WHEN UPDATING THEN 'UPDATE' ELSE 'DELETE' END) || ' on audit_entries blocked by tmi_audit_entries_no_mutate (DELETE allowed only for rows older than %d days)');
		   END IF;
		 END;`, auditFloorDays, auditFloorDays)},
		{"tmi_version_snapshots_no_mutate", fmt.Sprintf(`CREATE OR REPLACE TRIGGER tmi_version_snapshots_no_mutate
		 BEFORE UPDATE OR DELETE ON version_snapshots
		 FOR EACH ROW
		 BEGIN
		   IF DELETING AND SYS_EXTRACT_UTC(:OLD.created_at) < SYS_EXTRACT_UTC(SYSTIMESTAMP) - NUMTODSINTERVAL(%d, 'DAY') THEN
		     NULL;
		   ELSE
		     RAISE_APPLICATION_ERROR(-20001, 'version snapshots are append-only: ' || (CASE WHEN UPDATING THEN 'UPDATE' ELSE 'DELETE' END) || ' on version_snapshots blocked by tmi_version_snapshots_no_mutate (DELETE allowed only for rows older than %d days)');
		   END IF;
		 END;`, snapshotFloorDays, snapshotFloorDays)},
		{"tmi_system_audit_entries_no_mutate", fmt.Sprintf(`CREATE OR REPLACE TRIGGER tmi_system_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON system_audit_entries
		 FOR EACH ROW
		 BEGIN
		   IF DELETING AND SYS_EXTRACT_UTC(:OLD.created_at) < SYS_EXTRACT_UTC(SYSTIMESTAMP) - NUMTODSINTERVAL(%d, 'DAY') THEN
		     NULL;
		   ELSE
		     RAISE_APPLICATION_ERROR(-20001, 'system audit history is append-only: ' || (CASE WHEN UPDATING THEN 'UPDATE' ELSE 'DELETE' END) || ' on system_audit_entries blocked by tmi_system_audit_entries_no_mutate (DELETE allowed only for rows older than %d days)');
		   END IF;
		 END;`, systemAuditFloorDays, systemAuditFloorDays)},
	}
}

// oracleTrigger is one append-only trigger: its unquoted name and the
// CREATE OR REPLACE TRIGGER statement that defines it (without the marker).
type oracleTrigger struct {
	name string
	ddl  string
}

// oracleTriggerMarkerPrefix opens the marker comment stamped into every
// trigger body; the sha256 of the unmarked DDL follows it.
const oracleTriggerMarkerPrefix = "-- tmi-ddl-sha256:"

// SEM@0000000000000000000000000000000000000000: build the identity marker comment for a trigger DDL text (pure)
func oracleTriggerMarker(ddl string) string {
	sum := sha256.Sum256([]byte(ddl))
	return oracleTriggerMarkerPrefix + hex.EncodeToString(sum[:])
}

// oracleTriggerDDLWithMarker inserts the marker comment as the first line of
// the trigger body (right after BEGIN), where Oracle preserves it verbatim in
// ALL_SOURCE.
// SEM@0000000000000000000000000000000000000000: embed the identity marker into a trigger body after BEGIN (pure)
func oracleTriggerDDLWithMarker(ddl string) string {
	return strings.Replace(ddl, "BEGIN\n", "BEGIN\n\t\t   "+oracleTriggerMarker(ddl)+"\n", 1)
}

// oracleTriggerIsCurrent reports whether a VALID, ENABLED trigger named name
// exists in CURRENT_SCHEMA whose source carries marker. ALL_* views filtered
// to CURRENT_SCHEMA, like every other catalog probe here (#736).
// SEM@0000000000000000000000000000000000000000: probe whether an Oracle trigger with the given identity marker is installed and valid (reads DB)
func oracleTriggerIsCurrent(ctx context.Context, db *gorm.DB, name, marker string) (bool, error) {
	var cnt int64
	err := withMigrationRetry("append-only trigger "+name+" currency probe", func() error {
		return db.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM ALL_TRIGGERS t "+
				"JOIN ALL_OBJECTS o ON o.OWNER = t.OWNER AND o.OBJECT_NAME = t.TRIGGER_NAME AND o.OBJECT_TYPE = 'TRIGGER' "+
				"JOIN ALL_SOURCE s ON s.OWNER = t.OWNER AND s.NAME = t.TRIGGER_NAME AND s.TYPE = 'TRIGGER' "+
				"WHERE t.OWNER = "+oracleCurrentSchema+" AND t.TRIGGER_NAME = ? "+
				"AND t.STATUS = 'ENABLED' AND o.STATUS = 'VALID' AND s.TEXT LIKE ?",
			strings.ToUpper(name), "%"+marker+"%",
		).Scan(&cnt).Error
	})
	return cnt > 0, err
}
