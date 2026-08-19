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
// Note the deliberate asymmetry with EnsureSparseUserEmailIndex (#720), which
// hard-aborts startup on pre-existing duplicate sparse emails. That index is
// new, so a database carrying such duplicates has never had them tolerated and
// failing loudly is safe. This index predates #701 on every deployment that
// has the problem, so those databases boot today and must keep booting;
// aborting here would take down every pre-#701 deployment on upgrade
// (oracle-db-admin review, #732).
//
// This function never aborts startup on a DDL failure -- only on a failure to
// read the catalog at all. A runtime user without DDL privileges, a busy
// table, or an unexpected DDL error all leave the database exactly as it was
// (or restored to it, see below) and log ERROR; the next boot, or a
// `tmi-dbtool --schema` run by an admin-privileged user, retries. The
// alternative -- crash-looping a server over an index it may not be
// privileged to touch -- is a larger outage than the missing constraint.
// SEM@30424a23a3e8112b8be171d3d0fcb5cb63ca48a1: repair the users provider-lookup index to its intended unique definition, else warn and continue (mutates DB)
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
	switch {
	case !exists:
		logger.Warn("%s does not exist on %s; creating it as UNIQUE (#732)", userProviderLookupIndexName, usersTable)
	case db.Name() == "oracle":
		// Deliberately does not say "is NOT unique": on Oracle this branch is
		// reached both for a genuinely non-unique pre-#701 index and for the
		// plain UNIQUE index #732 created, which the catalog reports as
		// UNIQUE and which an operator would therefore read as already
		// correct. Naming the definition rather than the property keeps the
		// log honest against what they see in ALL_INDEXES (#760).
		logger.Warn("%s exists on %s but does not have the intended definition (a plain or non-unique index rather than the function-based unique index that matches PostgreSQL's NULL semantics); dropping and recreating it (#732, #760)",
			userProviderLookupIndexName, usersTable)
	default:
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
			// Oracle commits before and after every DDL statement, so a
			// transient failure can arrive *after* the DROP itself took
			// effect -- the retry then raises ORA-01418 for an index that is
			// genuinely gone. Ask the catalog what is actually true rather
			// than inferring it from the error (oracle-db-admin review, #732).
			// Retried: this probe runs at the single moment a transient
			// session error is most likely -- immediately after one.
			var stillPresent bool
			probeErr := withMigrationRetry("users provider-lookup index post-drop probe", func() error {
				var e error
				stillPresent, _, e = userProviderLookupIndexState(db, usersTable)
				return e
			})
			if probeErr != nil {
				// Can't tell what actually happened. Assume the drop did not
				// take effect: proceeding to CREATE on that assumption is the
				// branch that can leave the table index-less.
				logger.Error("re-probing %s after a failed drop also failed: %v", userProviderLookupIndexName, probeErr)
				stillPresent = true
			}
			if stillPresent {
				logger.Error(
					"failed to drop the non-unique %s ahead of recreating it as UNIQUE: %v. "+
						"The index is unchanged (still non-unique) and startup is continuing; retry on the next boot or run `tmi-dbtool --schema` with an admin-privileged database user (#732)",
					userProviderLookupIndexName, err)
				return nil
			}
			logger.Warn(
				"the DROP of %s reported an error (%v) but the index is gone -- Oracle commits around DDL, so the statement had already taken effect; continuing to the CREATE (#732)",
				userProviderLookupIndexName, err)
		}
	}

	// From here the index name is free. Any failure to recreate it leaves the
	// users table without its identity lookup index, which is a table scan on
	// every login, so a failed CREATE UNIQUE is followed by an attempt to put
	// the original non-unique index back.
	if err := withDDLRetry("users provider-lookup unique index create", func() error {
		return execMigrationDDL(db, createUniqueDDL)
	}); err != nil {
		// Same auto-commit reasoning as the DROP above, and it also covers the
		// ORA-00955 cases without string-matching the driver error: re-probe,
		// then decide. A concurrent replica winning the create race and this
		// session's own committed-then-disconnected create are indistinguishable
		// from here, and both are fine.
		// Retried like the post-drop probe: an unretried failure here falls
		// through to the restore, which then raises ORA-00955 if the unique
		// create had in fact committed -- producing the loudest error message
		// in this file for a database that is actually correct.
		var nowExists, nowUnique bool
		probeErr := withMigrationRetry("users provider-lookup index post-create probe", func() error {
			var e error
			nowExists, nowUnique, e = userProviderLookupIndexState(db, usersTable)
			return e
		})
		switch {
		case probeErr == nil && nowExists && nowUnique:
			logger.Warn(
				"the CREATE of %s reported an error (%v) but a valid UNIQUE index is present -- this session's DDL committed before the failure, or a concurrent replica won the race; treating as success (#732)",
				userProviderLookupIndexName, err)
			return nil
		case probeErr == nil && nowExists:
			logger.Error(
				"failed to create %s as UNIQUE (%v): an index of that name exists but is not a valid UNIQUE index. "+
					"Duplicate user identities are NOT being prevented; startup is continuing. Inspect that index and drop it if it is not TMI's (#732)",
				userProviderLookupIndexName, err)
			return nil
		}

		logger.Error("failed to create %s as UNIQUE: %v", userProviderLookupIndexName, err)
		if restoreErr := withDDLRetry("users provider-lookup index restore", func() error {
			return execMigrationDDL(db, restoreDDL)
		}); restoreErr != nil {
			logger.Error(
				"AND failed to restore the previous non-unique %s: %v. The users table may now have NO (provider, provider_user_id) index -- "+
					"logins will table-scan. Recreate it manually: %s. Two benign explanations to rule out first: ORA-00955 means the UNIQUE create "+
					"actually committed and only the verification failed (check the index and stop), and ORA-01408 means a differently-named index "+
					"already covers that exact column list (confirm it is UNIQUE). Note the restore DDL above is a plain column-list index "+
					"while the index this was trying to create is function-based on Oracle, so the two will not look alike in the catalog (#732, #760)",
				userProviderLookupIndexName, restoreErr, createUniqueDDL)
		} else {
			// On a database that already carried #732's plain UNIQUE index,
			// this restore puts back a NON-unique one -- a real, if narrow,
			// regression for the failure path. Accepted for the same reason
			// the whole function warns rather than aborting: the next boot
			// retries, and a missing constraint beats a crash loop (#760).
			logger.Error("restored the previous non-unique %s; the uniqueness constraint remains unenforced -- note this is weaker than what a #732-upgraded database had a moment ago -- and the upgrade will retry on the next boot (#732, #760)",
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

	logger.Info("%s is now a UNIQUE index on %s(provider, provider_user_id), enforced only for rows with a non-NULL provider_user_id (#732, #760)",
		userProviderLookupIndexName, usersTable)
	return nil
}

// UserProviderLookupIndexIsUnique reports whether idx_users_provider_lookup is
// currently a valid UNIQUE index with the definition this package intends, so
// a caller that needs the upgrade to have actually happened can say so.
//
// "The definition this package intends" is dialect-specific and is not the
// same as the catalog's UNIQUENESS column on Oracle: there it must also be
// function-based and ENABLED (#760), because a plain UNIQUE index over
// (provider, provider_user_id) reports as UNIQUE while enforcing stricter
// NULL semantics than PostgreSQL. See userProviderLookupDDL.
//
// EnsureUserProviderLookupUnique deliberately never aborts a server boot over
// a DDL failure -- crash-looping a server over an index it may not be
// privileged to touch is the larger outage. `tmi-dbtool --schema` is the
// opposite case: it is the admin-privileged remediation path, and exiting 0
// having silently failed to do the one thing the operator ran it for is the
// wrong outcome (oracle-db-admin review, #732). Returns false, not an error,
// when the users table does not exist.
// SEM@605e29546fe60dc8ac69862013475720a74dea8b: report whether the users provider-lookup index is currently valid and unique (reads DB)
func UserProviderLookupIndexIsUnique(db *gorm.DB) (bool, error) {
	usersTable := (&models.User{}).TableName()
	present, err := migrationTableExists(db, usersTable)
	if err != nil || !present {
		return false, err
	}
	exists, unique, err := userProviderLookupIndexState(db, usersTable)
	if err != nil {
		return false, err
	}
	return exists && unique, nil
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
// exist. It cannot be closed by building the replacement alongside the old one
// -- Oracle raises ORA-01408 for a second index over an identical column list
// -- and it is bounded: the whole sequence runs at startup under the
// cross-replica migration advisory lock, on a table whose index builds in
// seconds.
//
// A window-free Oracle alternative exists and was considered: a UNIQUE
// *constraint* can be enforced by an existing non-unique index, so
// `ALTER TABLE USERS ADD CONSTRAINT ... UNIQUE (PROVIDER, PROVIDER_USER_ID)`
// would bind immediately with no drop and no rebuild (validating existing data
// at DDL time, ORA-02299, which the duplicate gate above already prevents).
// It was not taken because it introduces an Oracle-only object with no
// PostgreSQL counterpart -- the probe would then have to consult
// ALL_CONSTRAINTS as well as ALL_INDEXES -- which cuts against
// api/models/models.go being the single source of truth for the schema
// (oracle-db-admin review, #732).
// SEM@30424a23a3e8112b8be171d3d0fcb5cb63ca48a1: build the drop, create-unique, and restore DDL for the provider-lookup index per dialect (pure)
func userProviderLookupDDL(dialect, usersTable string) (drop, createUnique, restore string) {
	name := userProviderLookupIndexName
	table := usersTable
	if dialect == "oracle" {
		name = strings.ToUpper(name)
		table = strings.ToUpper(table)
		// Function-based, not a plain (PROVIDER, PROVIDER_USER_ID) unique
		// index, because Oracle and PostgreSQL disagree about partially-NULL
		// keys (#760). Oracle omits a b-tree entry only when EVERY key column
		// is NULL; with PROVIDER non-NULL a sparse row IS indexed, so two
		// email-only users under one provider -- the normal output of
		// performSparseUserInsert -- collided with ORA-00001 and 500ed. On PG
		// (NULLS DISTINCT) they never collide. Wrapping PROVIDER in a CASE
		// that yields NULL for sparse rows makes the whole key NULL for them,
		// so Oracle skips the row entirely and the two engines agree. #720's
		// idx_users_sparse_email still covers duplicate sparse emails.
		//
		// PROVIDER_USER_ID leads deliberately. Key order is irrelevant to
		// uniqueness and to the all-NULL exclusion, but a bare leading column
		// keeps the hot auth lookup (provider = ? AND provider_user_id = ?,
		// api/authorization_enrichment.go) on an INDEX RANGE SCAN with a
		// near-unique access predicate; leading with the CASE expression
		// would leave the CBO no usable access path but idx_users_provider,
		// a range scan over every user of that provider (oracle-db-admin
		// review, #760).
		//
		// Residual divergence, accepted: a row with NULL PROVIDER and a
		// non-NULL subject keys as (subject, NULL) -- indexed, so two such
		// rows collide on Oracle and not on PG. models.User marks provider
		// not null and no code path writes a NULL provider, and the plain
		// index this replaces had the identical divergence.
		return fmt.Sprintf("DROP INDEX %s", name),
			fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (PROVIDER_USER_ID, CASE WHEN PROVIDER_USER_ID IS NOT NULL THEN PROVIDER END)", name, table),
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
// SEM@30424a23a3e8112b8be171d3d0fcb5cb63ca48a1: probe whether the users provider-lookup index exists with its intended definition, per dialect (reads DB)
func userProviderLookupIndexState(db *gorm.DB, usersTable string) (exists, unique bool, err error) {
	switch db.Name() {
	case "oracle":
		// STATUS is checked alongside UNIQUENESS for the same reason
		// sparseUserEmailIndexExists checks it: an UNUSABLE unique index
		// enforces nothing (and makes every USERS DML raise ORA-01502), so
		// treating it as "already unique" would skip the upgrade forever.
		// STATUS is 'N/A' for partitioned indexes; USERS is not partitioned.
		// Both OWNER and TABLE_OWNER are pinned -- an unqualified DROP/CREATE
		// INDEX resolves the index name in CURRENT_SCHEMA, so a foreign-owned
		// same-named index must not read as ours (oracle-db-admin review, #732).
		var state []struct {
			Uniqueness    string
			Status        string
			IndexType     string
			FuncidxStatus string
		}
		err = db.Raw(
			"SELECT UNIQUENESS, STATUS, INDEX_TYPE, FUNCIDX_STATUS FROM ALL_INDEXES WHERE INDEX_NAME = ? AND TABLE_NAME = ? "+
				"AND OWNER = "+oracleCurrentSchema+" AND TABLE_OWNER = "+oracleCurrentSchema,
			strings.ToUpper(userProviderLookupIndexName), strings.ToUpper(usersTable),
		).Scan(&state).Error
		if err != nil || len(state) == 0 {
			return false, false, err
		}
		// INDEX_TYPE is load-bearing, not belt-and-braces: a plain UNIQUE
		// index is what #732 itself created and what AutoMigrate builds from
		// the model tag on a fresh database, so it is the state every
		// affected deployment is actually in. Accepting it as correct would
		// short-circuit the caller and make the #760 fix a no-op on exactly
		// the databases that carry the bug (oracle-db-admin review).
		// FUNCIDX_STATUS is checked for the same reason STATUS is: a DISABLED
		// function-based index enforces nothing and makes DML on the table
		// fail, so it must read as "needs rebuilding", not "done".
		return true, strings.EqualFold(state[0].Uniqueness, "UNIQUE") &&
			strings.EqualFold(state[0].Status, "VALID") &&
			strings.EqualFold(state[0].IndexType, "FUNCTION-BASED NORMAL") &&
			strings.EqualFold(state[0].FuncidxStatus, "ENABLED"), nil
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
// SEM@637f0bdb33357d5c2d47d7c06b0e6e214c1962e6: fetch duplicate (provider, provider_user_id) pairs from the users table (reads DB)
func findDuplicateUserIdentities(db *gorm.DB, usersTable string) ([]userDupKey, error) {
	var dups []userDupKey
	err := withMigrationRetry("users duplicate-identity check", func() error {
		dups = dups[:0]
		return db.Table(usersTable).
			Select("provider, provider_user_id, COUNT(*) AS cnt").
			// Both halves of the key are excluded because the index does not
			// constrain a row with a NULL in either position -- but note the
			// two engines get there differently, and the difference is #760.
			// PostgreSQL treats NULLs as distinct, so a partially-NULL key is
			// never a duplicate. Oracle skips a b-tree entry only when EVERY
			// key column is NULL and would otherwise constrain a
			// partially-NULL key; the Oracle DDL wraps provider in a CASE
			// precisely so sparse rows key as all-NULL and drop out. The net
			// enforced row set is the same on both, which is what this
			// predicate mirrors. provider is tagged not-null in the model
			// today, but this stays correct against a legacy row that
			// predates that tag.
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
// SEM@637f0bdb33357d5c2d47d7c06b0e6e214c1962e6: format duplicate user identity pairs into a capped, PII-bounded log string (pure)
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
