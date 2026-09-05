// Package dbschema: one-time backfill of system_settings.origin for rows
// that predate the column, plus the CHECK constraint that enforces its
// invariant going forward (#794).
//
// SystemSetting.Origin distinguishes a row SeedDefaults inserted from one an
// operator deliberately set (api/models/system_setting.go). NULL reads as
// SEEDED -- the fail-safe direction for data loss -- but that same polarity
// is wrong for a row that already existed before this column shipped: on
// every live deployment (RDS, k3s) a pre-existing row was, under the old
// rules, either the only value the operator could see or one they had
// genuinely edited via the admin API. Leaving it NULL after upgrade would
// silently hand its authority to config, which is exactly the class of
// regression #794 introduced this column to prevent (oracle-db-admin
// review: the change is otherwise inert on the deployments that had the
// 2026-08-20 outage, because SeedDefaults skips rows that already exist and
// nothing else ever stamps a pre-existing row).
//
// BackfillSystemSettingOrigin stamps a NULL-origin row explicit only when it
// shows operator intent: modified_by is set (an admin API write, or a
// dbtool import, from before Origin existed), or its value no longer
// matches what SeedDefaults would seed for that key today. Every other
// NULL-origin row -- one that still holds the untouched registry default --
// is left alone, so it correctly reads as seeded going forward. This is
// deliberately not a blanket `UPDATE ... SET origin = 'seeded' WHERE origin
// IS NULL`: that would demote genuinely admin-set rows the moment their
// value happens to equal the current default.
//
// The value-comparison signal cannot see through at-rest encryption (see
// expectedSeedValues): an encrypted row is left on the modified_by signal
// alone and otherwise stays NULL. That is a deliberate choice, not an
// oversight -- oracle-db-admin review, #794. Defaulting an
// encrypted-and-unmatched row to explicit would have reproduced the
// 2026-08-20 outage class on every encrypted deployment (a seeded, encrypted
// auth.oauth_callback_url row would keep outranking a correctly-set env
// var, exactly as it did before this column existed), so this backfill
// prefers "correctly seeded" as the default reading for a row it cannot
// evaluate, at the cost of a narrow, accepted gap: a pre-#794
// dbtool-imported encrypted value that never recorded modified_by (dbtool's
// import path did not stamp it before Origin existed) reads as seeded
// rather than explicit after upgrade. Every writer as of #794 --
// SettingsService.Set, SeedDefaults, and dbtool's config-import and seed
// paths -- stamps Origin directly, so this gap cannot recur going forward.
package dbschema

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// systemSettingOriginCheckName is the CHECK constraint enforcing that
// system_settings.origin is NULL, "seeded", or "explicit" -- never any other
// value, and never the empty string (which Oracle binds as NULL anyway).
const systemSettingOriginCheckName = "chk_system_settings_origin"

// expectedSeedValues mirrors SettingsService.SeedDefaults's key/value
// construction (api/settings_service.go) exactly: the explicit
// models.DefaultSystemSettings() entries, plus config.DefaultOperationalSettings()
// entries for keys not already covered (empty config values are never
// seeded, and an explicit default wins on collision). The result is "what
// SeedDefaults would insert today for a database with none of these rows",
// which is what a NULL-origin row's value is compared against to decide
// whether an operator has since changed it.
//
// This is a plaintext comparison and is never meaningful against an
// at-rest-encrypted value (crypto.IsEncrypted): SeedDefaults encrypts every
// value it inserts when a settings encryptor is configured (not only
// Class.Secret keys), so an untouched encrypted row's ciphertext never
// equals the plaintext default computed here regardless of whether an
// operator ever touched it. BackfillSystemSettingOrigin does not attempt
// this comparison for encrypted rows at all -- see its loop -- so this map
// is only ever consulted for plaintext values.
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: build the key/value map SeedDefaults would insert today, mirroring its dedupe rule (pure)
func expectedSeedValues() map[string]string {
	defaults := models.DefaultSystemSettings()
	expected := make(map[string]string, len(defaults))
	for _, d := range defaults {
		expected[string(d.SettingKey)] = string(d.Value)
	}
	for _, ms := range config.DefaultOperationalSettings() {
		if ms.Value == "" {
			continue
		}
		if _, ok := expected[ms.Key]; ok {
			continue
		}
		expected[ms.Key] = ms.Value
	}
	return expected
}

// BackfillSystemSettingOrigin stamps origin = 'explicit' on every
// NULL-origin system_settings row that shows operator intent -- modified_by
// is set, or its value differs from (or is not covered by) what
// expectedSeedValues reports SeedDefaults would seed for that key today.
// Every other NULL-origin row is left as NULL, which now reads as seeded
// (see the package comment).
//
// The modified_by signal is written only by SettingsService.Set (the admin
// PUT and dbtool import paths). ReEncryptAll used to stamp it on every row
// it rotated, which would have made this backfill read a seeded row as
// operator-set; since #805 it writes only the ciphertext column, so key
// rotation can never manufacture intent. No install ever ran reencrypt
// before that fix, so no remediation of existing rows is needed.
//
// Idempotent and cheap in steady state: once a row is stamped, it no longer
// matches `origin IS NULL` and is never re-examined. Safe to run on every
// boot, ahead of AutoMigrate, mirroring DeduplicateGroups (group_dedupe.go).
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: backfill explicit origin onto pre-existing system_settings rows that show operator intent (writes DB)
func BackfillSystemSettingOrigin(db *gorm.DB) (int64, error) {
	logger := slogging.Get()
	settingsTable := (&models.SystemSetting{}).TableName()

	present, err := requireMigrationTable(db, settingsTable, "system_settings.origin backfill (#794)")
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, nil
	}

	var rows []models.SystemSetting
	err = withMigrationRetry("system_settings origin backfill scan", func() error {
		rows = rows[:0]
		return db.Where("origin IS NULL").Find(&rows).Error
	})
	if err != nil {
		return 0, fmt.Errorf("failed to load NULL-origin system_settings rows: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}

	expected := expectedSeedValues()

	var explicitKeys []string
	for _, row := range rows {
		key := string(row.SettingKey)
		if row.ModifiedBy.Valid {
			// An admin API write or dbtool import touched this row before
			// Origin existed to record it.
			explicitKeys = append(explicitKeys, key)
			continue
		}
		if crypto.IsEncrypted(string(row.Value)) {
			// Ciphertext can never equal a plaintext registry default -- see
			// expectedSeedValues and the function comment above. Rely on
			// modified_by alone for an encrypted row; leave it NULL here.
			continue
		}
		defaultValue, known := expected[key]
		if !known || string(row.Value) != defaultValue {
			// Either not a registry key SeedDefaults would have inserted at
			// all (so something else deliberately created it), or its value
			// has moved away from the current registry default.
			explicitKeys = append(explicitKeys, key)
		}
		// Otherwise: value matches the registry default and no modified_by
		// is recorded -- leave origin NULL (reads as seeded).
	}
	if len(explicitKeys) == 0 {
		return 0, nil
	}

	var totalUpdated int64
	for _, chunk := range chunkStrings(explicitKeys) {
		err = withMigrationRetry("system_settings origin backfill update", func() error {
			// UpdateColumn, not Update: SystemSetting.ModifiedAt carries
			// autoUpdateTime, and a plain Update injects it into the SET
			// clause even though this call only names "origin" -- silently
			// destroying every touched row's real last-modified timestamp,
			// which is operator-visible via the admin config API
			// (oracle-db-admin review, #794). UpdateColumn skips both hooks
			// and that auto-timestamp injection.
			result := db.Model(&models.SystemSetting{}).
				Where("setting_key IN ? AND origin IS NULL", chunk).
				UpdateColumn("origin", models.SystemSettingOriginExplicit)
			if result.Error != nil {
				return result.Error
			}
			totalUpdated += result.RowsAffected
			return nil
		})
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to stamp explicit origin on backfilled system_settings rows: %w", err)
		}
	}

	if totalUpdated > 0 {
		logger.Info("system_settings origin backfill: stamped %d pre-existing row(s) explicit (#794)", totalUpdated)
	}
	return totalUpdated, nil
}

// EnsureSystemSettingOriginCheckConstraint adds systemSettingOriginCheckName
// to system_settings, restricting origin to NULL, 'seeded', or 'explicit'.
// Unlike EnsureSparseUserEmailIndex/EnsureUserProviderLookupUnique, the CHECK
// clause itself is dialect-independent -- PostgreSQL and Oracle both accept
// `CHECK (origin IS NULL OR origin IN ('seeded','explicit'))` verbatim -- so
// only the idempotency story differs. Neither engine has an
// `ADD CONSTRAINT IF NOT EXISTS`, so the catalog is probed first, the same
// pattern #732/#736 use for indexes and tables.
//
// A no-op on SQLite: it is a test-fixture-only dialect here (see
// sparseUserEmailIndexExists), and unlike CREATE INDEX, SQLite's ALTER TABLE
// has no ADD CONSTRAINT form at all -- there is nothing to probe or run.
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: add the origin CHECK constraint to system_settings, idempotently per dialect (writes DB)
func EnsureSystemSettingOriginCheckConstraint(db *gorm.DB) error {
	settingsTable := (&models.SystemSetting{}).TableName()

	present, err := requireMigrationTable(db, settingsTable, "system_settings.origin CHECK constraint (#794)")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}

	if db.Name() == "sqlite" {
		slogging.Get().Debug(
			"skipping %s: SQLite has no ALTER TABLE ADD CONSTRAINT form; this is a test-fixture-only dialect (#794)",
			systemSettingOriginCheckName)
		return nil
	}

	var exists bool
	err = withMigrationRetry("system_settings origin check-constraint existence probe", func() error {
		var probeErr error
		exists, probeErr = systemSettingOriginCheckExists(db, settingsTable)
		return probeErr
	})
	if err != nil {
		return fmt.Errorf("failed to check whether %s exists: %w", systemSettingOriginCheckName, err)
	}
	if exists {
		return nil
	}

	var ddl string
	if db.Name() == "oracle" {
		// Unquoted, so Oracle folds it to upper case regardless; written
		// upper case here to match what the catalog probe below looks for,
		// the same convention userProviderLookupDDL and
		// EnsureSparseUserEmailIndex use.
		ddl = fmt.Sprintf(
			`ALTER TABLE %s ADD CONSTRAINT %s CHECK (ORIGIN IS NULL OR ORIGIN IN ('seeded','explicit'))`,
			strings.ToUpper(settingsTable), strings.ToUpper(systemSettingOriginCheckName))
	} else {
		ddl = fmt.Sprintf(
			`ALTER TABLE %s ADD CONSTRAINT %s CHECK (origin IS NULL OR origin IN ('seeded','explicit'))`,
			settingsTable, systemSettingOriginCheckName)
	}

	err = withDDLRetry("system_settings origin check-constraint create", func() error {
		return execMigrationDDL(db, ddl)
	})
	switch {
	case err == nil:
		return nil
	case db.Name() == "oracle" && strings.Contains(err.Error(), "ORA-02264"):
		// ORA-02264 ("name already used by an existing constraint") is
		// ambiguous: it is raised both when a concurrent replica won the
		// create race for OUR constraint, and when the name collides with an
		// unrelated constraint on another table (Oracle constraint names are
		// unique per schema, not per table) or a same-named constraint an
		// operator has since DISABLEd (which the probe above already read as
		// absent). Re-probe rather than assume success, the same ambiguity
		// #732/1.8.4-round-3 resolved for ORA-00955 via
		// verifySparseIndexEnforced (oracle-db-admin review, #794).
		return verifySystemSettingOriginCheckEnforced(db, settingsTable)
	case db.Name() == "postgres" && strings.Contains(err.Error(), "already exists"):
		// PostgreSQL: `constraint "..." for relation "..." already exists`
		// (SQLSTATE 42710) -- the identical concurrent-replica ambiguity.
		return verifySystemSettingOriginCheckEnforced(db, settingsTable)
	case dberrors.IsRetryable(dberrors.Classify(err)):
		// Lock contention or a connection blip that outlived the retry
		// budget. Mirrors EnsureSparseUserEmailIndex's warn-and-continue
		// policy: a missing defense-in-depth constraint is a smaller outage
		// than a server that will not boot, and the next boot retries.
		slogging.Get().Error(
			"failed to create %s after %d attempts against %s (transient error: %v). "+
				"The origin column's NULL/seeded/explicit invariant is NOT currently enforced at the database level; "+
				"startup is continuing and the next boot will retry",
			systemSettingOriginCheckName, ddlMaxAttempts, settingsTable, err)
		return nil
	default:
		return fmt.Errorf("failed to create %s: %w", systemSettingOriginCheckName, err)
	}
}

// verifySystemSettingOriginCheckEnforced re-probes for systemSettingOriginCheckName
// after a create attempt that raised an ambiguous "already exists"-shaped
// error (ORA-02264 or PostgreSQL's constraint-already-exists text). Mirrors
// sparse_user_index.go's verifySparseIndexEnforced: a same-named collision
// (a different table's constraint on Oracle, where names are unique per
// schema) or an operator-disabled constraint produces the identical error as
// a genuine concurrent-replica race, so re-checking is the only way to tell
// "enforced" from "not enforced but silently reported as fine forever."
// Never returns an error itself -- an unenforced invariant is logged, not
// fatal, matching this function's own warn-and-continue policy throughout
// (oracle-db-admin review, #794).
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: confirm the origin CHECK constraint exists after an ambiguous create/collision error, logging ERROR if not (reads DB)
func verifySystemSettingOriginCheckEnforced(db *gorm.DB, settingsTable string) error {
	var nowExists bool
	err := withMigrationRetry("system_settings origin check-constraint post-create verification", func() error {
		var probeErr error
		nowExists, probeErr = systemSettingOriginCheckExists(db, settingsTable)
		return probeErr
	})
	if err != nil {
		// The create itself may well have succeeded; surface the
		// verification failure rather than silently assuming either outcome.
		return fmt.Errorf("failed to verify %s after an ambiguous create error: %w", systemSettingOriginCheckName, err)
	}
	if !nowExists {
		slogging.Get().Error(
			"%s does not exist on %s after an attempted create that reported it as already present -- "+
				"either the name collides with an unrelated constraint (Oracle constraint names are unique "+
				"per schema, not per table), or an operator has since disabled it. "+
				"The origin column's NULL/seeded/explicit invariant is NOT currently enforced at the database "+
				"level; startup is continuing without it (#794)",
			systemSettingOriginCheckName, settingsTable)
	}
	return nil
}

// systemSettingOriginCheckExists reports whether systemSettingOriginCheckName
// already exists as an enabled/valid CHECK constraint on settingsTable, per
// dialect. Probed by name only (not structurally, unlike #756's index
// probes): unlike idx_users_provider_lookup or the sparse-email index, this
// constraint name is new in #794 -- no deployed database can already hold a
// differently-defined object under it -- so there is no legacy "impostor"
// case to guard against.
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: probe whether the system_settings origin CHECK constraint already exists, per dialect (reads DB)
func systemSettingOriginCheckExists(db *gorm.DB, settingsTable string) (bool, error) {
	var cnt int64
	var err error
	switch db.Name() {
	case "oracle":
		err = db.Raw(
			"SELECT COUNT(*) FROM ALL_CONSTRAINTS WHERE CONSTRAINT_NAME = ? AND TABLE_NAME = ? "+
				"AND OWNER = "+oracleCurrentSchema+" AND CONSTRAINT_TYPE = 'C' AND STATUS = 'ENABLED'",
			strings.ToUpper(systemSettingOriginCheckName), strings.ToUpper(settingsTable),
		).Scan(&cnt).Error
	case "postgres":
		err = db.Raw(
			"SELECT COUNT(*) FROM information_schema.table_constraints "+
				"WHERE constraint_name = ? AND table_name = ? AND constraint_type = 'CHECK' "+
				"AND constraint_schema = current_schema()",
			systemSettingOriginCheckName, settingsTable,
		).Scan(&cnt).Error
	default:
		// Unsupported dialect other than the sqlite short-circuit in the
		// caller: fail closed (assume absent) so behavior falls back to
		// attempting the DDL rather than silently skipping enforcement.
		slogging.Get().Warn("systemSettingOriginCheckExists: unsupported dialect %q, assuming constraint absent", db.Name())
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}
