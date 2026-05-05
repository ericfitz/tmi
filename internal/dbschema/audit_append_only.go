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
// raises an exception on UPDATE or DELETE. The trigger is the last-line
// defense — if it fires, something at the application or operator layer is
// trying to mutate immutable history, and the right behavior is to refuse
// the operation.
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

// InstallAuditAppendOnlyTriggers installs UPDATE/DELETE-blocking triggers
// on audit_entries and version_snapshots. Idempotent across re-runs.
//
// On Oracle ADB, the call uses PL/SQL CREATE OR REPLACE TRIGGER so the
// statement is safe to re-run on every server start; on PostgreSQL, the
// driver supports CREATE OR REPLACE FUNCTION + DROP TRIGGER IF EXISTS +
// CREATE TRIGGER. SQLite is skipped.
//
// Operator escape hatch: legitimate retention/archival jobs need a way to
// purge old audit_entries. The trigger blocks UPDATE/DELETE from any
// connection by default; a privileged "audit_admin" role can disable the
// trigger for the duration of the archival job
// (ALTER TABLE audit_entries DISABLE TRIGGER ...; ... ; ENABLE TRIGGER ...).
// This is documented in the operator runbook (wiki) and is the only
// supported path for legitimate audit mutation.
func InstallAuditAppendOnlyTriggers(ctx context.Context, db *gorm.DB) error {
	logger := slogging.Get()
	dialect := db.Name()

	switch dialect {
	case "postgres":
		return installPostgresAppendOnly(ctx, db, logger)
	case "oracle":
		return installOracleAppendOnly(ctx, db, logger)
	case "sqlite":
		logger.Info("InstallAuditAppendOnlyTriggers: skipping on dialect %q (single-process SQLite is single-writer)", dialect)
		return nil
	default:
		logger.Warn("InstallAuditAppendOnlyTriggers: unsupported dialect %q, skipping; T19 protection is NOT in effect", dialect)
		return nil
	}
}

func installPostgresAppendOnly(ctx context.Context, db *gorm.DB, logger *slogging.Logger) error {
	statements := []string{
		// Helper function. RAISE EXCEPTION aborts the row mutation and
		// surfaces a clean SQLSTATE 'P0001' to the application — it does
		// NOT roll back the entire surrounding transaction, the caller's
		// other changes are preserved if they handle the error.
		`CREATE OR REPLACE FUNCTION tmi_audit_append_only_guard()
		 RETURNS trigger AS $$
		 BEGIN
		   RAISE EXCEPTION 'audit history is append-only: % on % blocked by tmi_audit_append_only_guard',
		     TG_OP, TG_TABLE_NAME
		     USING ERRCODE = 'P0001';
		 END;
		 $$ LANGUAGE plpgsql;`,

		// audit_entries trigger
		`DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries;`,
		`CREATE TRIGGER tmi_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON audit_entries
		 FOR EACH ROW EXECUTE FUNCTION tmi_audit_append_only_guard();`,

		// version_snapshots trigger
		`DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots;`,
		`CREATE TRIGGER tmi_version_snapshots_no_mutate
		 BEFORE UPDATE OR DELETE ON version_snapshots
		 FOR EACH ROW EXECUTE FUNCTION tmi_audit_append_only_guard();`,
	}

	for _, sql := range statements {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("postgres install: %w (sql: %s)", err, sql)
		}
	}
	logger.Info("InstallAuditAppendOnlyTriggers: postgres triggers installed on audit_entries + version_snapshots")
	return nil
}

func installOracleAppendOnly(ctx context.Context, db *gorm.DB, logger *slogging.Logger) error {
	// Oracle CREATE OR REPLACE TRIGGER is atomic — no need for explicit
	// DROP/CREATE pair. RAISE_APPLICATION_ERROR(-20001, ...) bubbles up
	// as ORA-20001 to the application; the dberrors.Classify path treats
	// it as a constraint-class error. The exact error number is in the
	// reserved -20000..-20999 range for application-defined errors.
	statements := []string{
		`CREATE OR REPLACE TRIGGER tmi_audit_entries_no_mutate
		 BEFORE UPDATE OR DELETE ON audit_entries
		 FOR EACH ROW
		 BEGIN
		   RAISE_APPLICATION_ERROR(-20001, 'audit history is append-only: ' || (CASE WHEN UPDATING THEN 'UPDATE' ELSE 'DELETE' END) || ' on audit_entries blocked by tmi_audit_entries_no_mutate');
		 END;`,
		`CREATE OR REPLACE TRIGGER tmi_version_snapshots_no_mutate
		 BEFORE UPDATE OR DELETE ON version_snapshots
		 FOR EACH ROW
		 BEGIN
		   RAISE_APPLICATION_ERROR(-20001, 'version snapshots are append-only: ' || (CASE WHEN UPDATING THEN 'UPDATE' ELSE 'DELETE' END) || ' on version_snapshots blocked by tmi_version_snapshots_no_mutate');
		 END;`,
	}

	for _, sql := range statements {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("oracle install: %w (sql: %s)", err, sql)
		}
	}
	logger.Info("InstallAuditAppendOnlyTriggers: oracle triggers installed on audit_entries + version_snapshots")
	return nil
}
