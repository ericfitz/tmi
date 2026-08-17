// Package dbschema: one-time upgrade of a pre-#701 non-unique
// idx_users_provider_lookup to a genuine UNIQUE index (#732).
//
// #701 changed the GORM tag on users(provider, provider_user_id) from
// `index:` to `uniqueIndex:`. On a database created after that, AutoMigrate
// builds the index unique and there is nothing to do. On a database that
// already carried the plain index under the same name, AutoMigrate never
// upgrades it -- GORM's migrator reconciles an index's *existence*, never its
// uniqueness -- so the constraint #701/#710 intended has been silently absent
// on every pre-#701 deployment since.
//
// On Oracle it is worse than a no-op: gorm-oracle's HasIndex binds the tag's
// lowercase name as a query *value* against USER_INDEXES.INDEX_NAME, which
// holds the uppercase name TMI's unquoted DDL actually created, so HasIndex
// always answers "absent"; AutoMigrate re-issues CREATE UNIQUE INDEX every
// pass, Oracle rejects it at name resolution with ORA-00955 (not the
// ORA-01452 a duplicate-data failure would raise), and TMI's own
// ignoreOracleAlreadyExists swallows that as benign.
//
// AutoMigrate cannot express the fix, so it is raw DDL here: drop the
// non-unique index and recreate it UNIQUE, gated on the table being
// duplicate-free.
package dbschema

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// EnsureUserProviderLookupUnique brings idx_users_provider_lookup to its
// intended UNIQUE definition on a database where it is missing or non-unique
// (#732). Idempotent: on a correct database it costs one catalog query.
//
// Safe -- and required -- to call on every boot after AutoMigrate, including
// when the #480 fingerprint fast path skips AutoMigrate entirely: an index's
// uniqueness is not part of the model fingerprint, so a stamped database
// carrying the pre-#701 index would otherwise never be reconsidered.
//
// The upgrade is gated on users(provider, provider_user_id) being
// duplicate-free. When duplicates exist the index genuinely cannot be made
// unique, so this logs the offending rows at ERROR and returns nil, matching
// CheckDuplicateUserProviderIdentities' (#724) operator-driven policy: merging
// user identities repoints ownership and access rows across many tables and is
// a security decision startup code must not make on its own.
//
// This function never aborts startup on a DDL failure -- only on a failure to
// read the catalog at all. A runtime user without DDL privileges, a busy
// table, or an unexpected DDL error all leave the database exactly as it was
// (or restored to it, see below) and log ERROR; the next boot, or a
// `tmi-dbtool --schema` run by an admin-privileged user, retries. The
// alternative -- crash-looping a server over an index it may not be
// privileged to touch -- is a larger outage than the missing constraint.
func EnsureUserProviderLookupUnique(db *gorm.DB) error {
	usersTable := (&models.User{}).TableName()
	present, err := requireMigrationTable(db, usersTable, "users provider-lookup unique-index upgrade (#732)")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}

	var exists, isUnique bool
	err = withMigrationRetry("users provider-lookup index state probe", func() error {
		var probeErr error
		exists, isUnique, probeErr = userProviderLookupIndexState(db, usersTable)
		return probeErr
	})
	if err != nil {
		return fmt.Errorf("failed to determine the state of %s: %w", userProviderLookupIndexName, err)
	}
	if exists && isUnique {
		return nil
	}

	logger := slogging.Get()

	// A wholly absent index is handled here too, not just a non-unique one.
	// AutoMigrate creates it from the model tag on a normal pass, but the
	// fingerprint fast path skips AutoMigrate, and a failed upgrade below can
	// leave the name free -- in both cases nothing else would ever put it
	// back.
	if !exists {
		logger.Warn("%s does not exist on %s; creating it as UNIQUE (#732)", userProviderLookupIndexName, usersTable)
	} else {
		logger.Warn("%s exists on %s but is NOT unique -- a pre-#701 index AutoMigrate cannot upgrade in place; dropping and recreating it as UNIQUE (#732)",
			userProviderLookupIndexName, usersTable)
	}

	dups, err := findDuplicateUserIdentities(db, usersTable)
	if err != nil {
		return err
	}
	if len(dups) > 0 {
		logger.Error(
			"cannot upgrade %s to UNIQUE: users table contains %d duplicate (provider, provider_user_id) identities: %s. "+
				"Manually merge or delete the duplicate rows (repointing any threat models, group memberships, and access grants they own); "+
				"the upgrade runs automatically on the next boot once they are gone. Startup is continuing with the constraint unenforced (#732)",
			userProviderLookupIndexName, len(dups), formatUserDupPairs(dups))
		return nil
	}

	dropDDL, createUniqueDDL, restoreDDL := userProviderLookupDDL(db.Name(), usersTable)

	if exists {
		if err := withDDLRetry("users provider-lookup index drop", func() error {
			return execMigrationDDL(db, dropDDL)
		}); err != nil {
			logger.Error(
				"failed to drop the non-unique %s ahead of recreating it as UNIQUE: %v. "+
					"The index is unchanged (still non-unique) and startup is continuing; retry on the next boot or run `tmi-dbtool --schema` with an admin-privileged database user (#732)",
				userProviderLookupIndexName, err)
			return nil
		}
	}

	// From here the index is gone. Any failure to recreate it leaves the
	// users table without its identity lookup index, which is a performance
	// problem on every login, so a failed CREATE UNIQUE is followed by an
	// attempt to put the original non-unique index back.
	if err := withDDLRetry("users provider-lookup unique index create", func() error {
		return execMigrationDDL(db, createUniqueDDL)
	}); err != nil {
		logger.Error("failed to create %s as UNIQUE: %v", userProviderLookupIndexName, err)
		if restoreErr := execMigrationDDL(db, restoreDDL); restoreErr != nil {
			logger.Error(
				"AND failed to restore the previous non-unique %s: %v. The users table now has NO (provider, provider_user_id) index -- "+
					"logins will table-scan. Recreate it manually: %s (#732)",
				userProviderLookupIndexName, restoreErr, createUniqueDDL)
		} else {
			logger.Error("restored the previous non-unique %s; the uniqueness constraint remains unenforced and the upgrade will retry on the next boot (#732)",
				userProviderLookupIndexName)
		}
		return nil
	}

	// Verify rather than assume: on PostgreSQL a CREATE UNIQUE INDEX IF NOT
	// EXISTS is a silent no-op against a same-named impostor, and on Oracle a
	// concurrent replica's create can win the race -- both produce a nil error
	// that says nothing about what is actually enforced now (the same
	// reasoning as verifySparseIndexEnforced).
	var nowExists, nowUnique bool
	if err := withMigrationRetry("users provider-lookup index post-upgrade verification", func() error {
		var probeErr error
		nowExists, nowUnique, probeErr = userProviderLookupIndexState(db, usersTable)
		return probeErr
	}); err != nil {
		return fmt.Errorf("failed to verify %s after upgrading it: %w", userProviderLookupIndexName, err)
	}
	if !nowExists || !nowUnique {
		logger.Error(
			"%s is still not a UNIQUE index after an apparently successful upgrade (exists=%t, unique=%t) -- "+
				"a concurrent replica may have recreated it non-unique, or a same-named index that is not TMI's occupies the name. "+
				"Duplicate user identities are NOT being prevented; startup is continuing (#732)",
			userProviderLookupIndexName, nowExists, nowUnique)
		return nil
	}

	logger.Info("%s is now a UNIQUE index on %s(provider, provider_user_id) (#732)", userProviderLookupIndexName, usersTable)
	return nil
}

// userProviderLookupDDL returns the drop, create-unique, and restore-non-unique
// statements for idx_users_provider_lookup on the given dialect.
//
// Oracle gets uppercase identifiers because TMI runs with
// SkipQuoteIdentifiers: true, so every object it created was folded to upper
// case; it also has no IF EXISTS / IF NOT EXISTS on index DDL, which is why
// the caller probes the catalog first rather than relying on the statement to
// be idempotent.
//
// There is a window between the drop and the create where the index does not
// exist. It cannot be avoided on Oracle -- a second index over the same column
// list raises ORA-01408, so the new index cannot be built alongside the old
// one -- and it is bounded: the whole sequence runs at startup under the
// cross-replica migration advisory lock, on a table whose index builds in
// seconds.
func userProviderLookupDDL(dialect, usersTable string) (drop, createUnique, restore string) {
	name := userProviderLookupIndexName
	table := usersTable
	if dialect == "oracle" {
		name = strings.ToUpper(name)
		table = strings.ToUpper(table)
		return fmt.Sprintf("DROP INDEX %s", name),
			fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (PROVIDER, PROVIDER_USER_ID)", name, table),
			fmt.Sprintf("CREATE INDEX %s ON %s (PROVIDER, PROVIDER_USER_ID)", name, table)
	}
	return fmt.Sprintf("DROP INDEX IF EXISTS %s", name),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (provider, provider_user_id)", name, table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (provider, provider_user_id)", name, table)
}

// userProviderLookupIndexState reports whether userProviderLookupIndexName
// exists on usersTable and, if so, whether it is UNIQUE.
//
// Uses dialect-specific catalog SQL for the same reason
// userProviderLookupIndexExists does -- gorm's HasIndex is unusable on Oracle
// (see that function) -- and filters to the session's CURRENT_SCHEMA rather
// than the connected user's own objects (#736).
func userProviderLookupIndexState(db *gorm.DB, usersTable string) (exists, unique bool, err error) {
	switch db.Name() {
	case "oracle":
		var uniqueness []string
		err = db.Raw(
			"SELECT UNIQUENESS FROM ALL_INDEXES WHERE INDEX_NAME = ? AND TABLE_NAME = ? AND TABLE_OWNER = "+oracleCurrentSchema,
			strings.ToUpper(userProviderLookupIndexName), strings.ToUpper(usersTable),
		).Scan(&uniqueness).Error
		if err != nil || len(uniqueness) == 0 {
			return false, false, err
		}
		return true, strings.EqualFold(uniqueness[0], "UNIQUE"), nil
	case "postgres":
		var defs []string
		err = db.Raw(
			"SELECT indexdef FROM pg_indexes WHERE indexname = ? AND tablename = ? AND schemaname = current_schema()",
			userProviderLookupIndexName, usersTable,
		).Scan(&defs).Error
		if err != nil || len(defs) == 0 {
			return false, false, err
		}
		return true, strings.HasPrefix(strings.ToUpper(defs[0]), "CREATE UNIQUE INDEX"), nil
	case "sqlite":
		// Test-fixture dialect only -- TMI's supported production dialects are
		// postgres and oracle.
		var defs []string
		err = db.Raw(
			"SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ?",
			userProviderLookupIndexName, usersTable,
		).Scan(&defs).Error
		if err != nil || len(defs) == 0 {
			return false, false, err
		}
		return true, strings.HasPrefix(strings.ToUpper(defs[0]), "CREATE UNIQUE INDEX"), nil
	default:
		// Unsupported dialect: report "exists and unique" so the caller does
		// nothing at all. Fail-safe in the direction of not issuing DDL this
		// probe cannot verify, rather than dropping an index it cannot read.
		slogging.Get().Warn("userProviderLookupIndexState: unsupported dialect %q, skipping the #732 upgrade", db.Name())
		return true, true, nil
	}
}

// findDuplicateUserIdentities returns every (provider, provider_user_id) pair
// that appears on more than one users row. Shared by
// CheckDuplicateUserProviderIdentities (#724), which reports them, and
// EnsureUserProviderLookupUnique (#732), which is gated on there being none.
func findDuplicateUserIdentities(db *gorm.DB, usersTable string) ([]userDupKey, error) {
	var dups []userDupKey
	err := withMigrationRetry("users duplicate-identity check", func() error {
		dups = dups[:0]
		return db.Table(usersTable).
			Select("provider, provider_user_id, COUNT(*) AS cnt").
			// A composite unique index never constrains a row where ANY key
			// column is NULL (Oracle and PostgreSQL both), so both halves of
			// the key must be excluded to mirror idx_users_provider_lookup's
			// actual enforcement exactly -- not just provider_user_id.
			// provider is tagged not-null in the model today, but this stays
			// correct against a legacy row that predates that tag.
			Where("provider IS NOT NULL AND provider_user_id IS NOT NULL").
			Group("provider, provider_user_id").
			Having("COUNT(*) > 1").
			Scan(&dups).Error
	})
	if err != nil {
		return nil, fmt.Errorf("failed to check users for duplicate provider identities: %w", err)
	}
	return dups, nil
}

// formatUserDupPairs renders duplicate identities for a log line or error
// message, capped at maxReportedPairs. provider_user_id is an IdP subject,
// which for several providers is an email address; an unbounded list would let
// a badly-merged database dump an arbitrarily long line of PII into the
// startup log.
func formatUserDupPairs(dups []userDupKey) string {
	const maxReportedPairs = 20
	shown := dups
	suffix := ""
	if len(dups) > maxReportedPairs {
		shown = dups[:maxReportedPairs]
		suffix = fmt.Sprintf(" (and %d more)", len(dups)-maxReportedPairs)
	}
	pairs := make([]string, len(shown))
	for i, d := range shown {
		pairs[i] = fmt.Sprintf("(provider=%s, provider_user_id=%s, rows=%d)", d.Provider, d.ProviderUserID, d.Cnt)
	}
	return strings.Join(pairs, "; ") + suffix
}
