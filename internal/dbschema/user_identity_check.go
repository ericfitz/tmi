// Package dbschema: pre-migration duplicate-identity check for users
// (#724).
//
// api/models/models.go backs users(provider, provider_user_id) with the
// unique idx_users_provider_lookup (#701). On a database that never had this
// index before, AutoMigrate's CREATE UNIQUE INDEX genuinely aborts with
// ORA-01452 / PostgreSQL 23505 if duplicate rows already exist. But on a
// database that already had the pre-#701 *non-unique* index by that name,
// AutoMigrate does not reach that failure at all (see the index-existence
// probe below and #732) -- so CheckDuplicateUserProviderIdentities only
// hard-aborts when the index is genuinely about to be created; otherwise it
// logs the duplicates loudly and lets startup continue, since the constraint
// isn't being enforced regardless of what this check does. Unlike groups
// (#704), users get no automatic dedupe here: merging user identities
// repoints ownership and access rows across many tables and is a
// security-sensitive operator decision, not something startup code should do
// silently.
package dbschema

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// userProviderLookupIndexName is the unique index api/models.User tags onto
// (provider, provider_user_id) (#701). Referenced by name here because the
// existence probe below deliberately bypasses GORM's own index-existence
// check (see userProviderLookupIndexExists).
const userProviderLookupIndexName = "idx_users_provider_lookup"

// userDupKey is a raw scan target. Deliberately untagged: result-set labels
// come back UPPERCASE from Oracle and lowercase from Postgres, and an
// untagged field's DBName is derived from the active dialect's
// NamingStrategy, so the same struct matches both (see groupDupKey).
// SEM@0000000: hold a duplicate users(provider, provider_user_id) key and its row count (pure)
type userDupKey struct {
	Provider       string
	ProviderUserID string
	Cnt            int64
}

// CheckDuplicateUserProviderIdentities detects duplicate
// users(provider, provider_user_id) rows ahead of AutoMigrate (#724). If
// idx_users_provider_lookup is not yet present, AutoMigrate is about to
// attempt CREATE UNIQUE INDEX and would abort with ORA-01452 / SQLSTATE
// 23505 against these rows -- this returns an actionable, named-rows error
// instead of that opaque abort. If the index already exists (a pre-#701
// database whose non-unique index AutoMigrate cannot upgrade in place --
// #732), that abort was never actually going to happen, so this logs the
// duplicates at ERROR and returns nil so startup continues -- unchanged
// behavior for every deployment that currently boots. Unlike groups (#704),
// users get NO automatic dedupe: merging user identities repoints ownership
// and access rows and is a security decision an operator must make. NULL
// provider or provider_user_id rows are excluded -- a composite unique index
// never constrains a row where any key column is NULL.
// SEM@0000000: detect duplicate user provider identities and return an actionable startup error only when the unique index would genuinely fail to create (reads DB)
func CheckDuplicateUserProviderIdentities(db *gorm.DB) error {
	usersTable := (&models.User{}).TableName()
	if !db.Migrator().HasTable(usersTable) {
		return nil
	}

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
		return fmt.Errorf("failed to check users for duplicate provider identities: %w", err)
	}
	if len(dups) == 0 {
		return nil
	}

	var indexExists bool
	err = withMigrationRetry("users provider-lookup index existence probe", func() error {
		var probeErr error
		indexExists, probeErr = userProviderLookupIndexExists(db, usersTable)
		return probeErr
	})
	if err != nil {
		return fmt.Errorf("failed to check whether %s exists: %w", userProviderLookupIndexName, err)
	}

	// maxReportedPairs caps how many duplicate keys are named in the log/error.
	// provider_user_id is an IdP subject, which for several providers is an
	// email address; an unbounded list would let a badly-merged database dump
	// an arbitrarily long line of PII into the startup log.
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

	if indexExists {
		// The index predates #701 and AutoMigrate cannot upgrade a
		// non-unique index to unique in place (#732 tracks a real fix); no
		// CREATE UNIQUE INDEX is about to run, so there is no genuine
		// ORA-01452/23505 window to preempt. Every current deployment must
		// keep booting, so warn loudly instead of hard-aborting.
		slogging.Get().Error(
			"users table contains %d duplicate (provider, provider_user_id) identities that %s does NOT currently enforce as unique (the index predates #701 and AutoMigrate cannot upgrade a non-unique index in place -- see #732): %s%s. "+
				"Startup is continuing, but these rows should be manually merged or deleted (repointing any threat models, group memberships, and access grants they own). "+
				"Cleaning up the rows alone does not make the index unique: the index itself must also be dropped and recreated (tracked in #732) or every future AutoMigrate pass will keep leaving it non-unique",
			len(dups), userProviderLookupIndexName, strings.Join(pairs, "; "), suffix)
		return nil
	}

	return fmt.Errorf(
		"users table contains %d duplicate (provider, provider_user_id) identities: %s%s. "+
			"The unique index idx_users_provider_lookup cannot be created over these rows "+
			"(ORA-01452 / SQLSTATE 23505) and startup cannot continue. Manually merge or "+
			"delete the duplicate user rows (repointing any threat models, group "+
			"memberships, and access grants they own), then restart",
		len(dups), strings.Join(pairs, "; "), suffix)
}

// userProviderLookupIndexExists reports whether an index named
// userProviderLookupIndexName already exists on usersTable, probed with
// dialect-specific raw SQL rather than gorm.Migrator().HasIndex.
//
// HasIndex is deliberately NOT used here: gorm-oracle's implementation binds
// the GORM tag's index name verbatim (lowercase) as a query *parameter*
// against Oracle's USER_INDEXES.INDEX_NAME, which holds the uppercase name
// TMI's unquoted DDL (SkipQuoteIdentifiers: true) actually created. A bound
// parameter is never case-folded, so HasIndex always returns false for this
// index on Oracle -- which is exactly the bug this function exists to route
// around (oracle-db-admin review, #724). Querying the catalog view directly
// with the correctly-cased name avoids that trap.
// SEM@0000000: probe whether the users provider-lookup index already exists, per dialect (reads DB)
func userProviderLookupIndexExists(db *gorm.DB, usersTable string) (bool, error) {
	var cnt int64
	var err error
	switch db.Name() {
	case "oracle":
		err = db.Raw(
			"SELECT COUNT(*) FROM USER_INDEXES WHERE INDEX_NAME = ? AND TABLE_NAME = ?",
			strings.ToUpper(userProviderLookupIndexName), strings.ToUpper(usersTable),
		).Scan(&cnt).Error
	case "postgres":
		// pg_indexes spans every schema visible to the connecting role;
		// schema-qualify so a same-named index on a users table in another
		// schema can't produce a false positive (oracle-db-admin review,
		// #724 round 2). A false negative here just degrades to the
		// pre-#732 hard-abort, not silent data loss.
		err = db.Raw(
			"SELECT COUNT(*) FROM pg_indexes WHERE indexname = ? AND tablename = ? AND schemaname = current_schema()",
			userProviderLookupIndexName, usersTable,
		).Scan(&cnt).Error
	case "sqlite":
		// Test-fixture dialect only -- TMI's supported production dialects
		// are postgres and oracle (see InstallThreatModelAliasSequence).
		err = db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ?",
			userProviderLookupIndexName, usersTable,
		).Scan(&cnt).Error
	default:
		// Unsupported dialect: fail closed (assume absent) so behavior falls
		// back to the pre-#732 hard-abort rather than silently skipping
		// enforcement on a dialect this probe doesn't understand.
		slogging.Get().Warn("userProviderLookupIndexExists: unsupported dialect %q, assuming index absent", db.Name())
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}
