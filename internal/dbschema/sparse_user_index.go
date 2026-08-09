// Package dbschema: unique sparse-email index for NULL provider_user_id rows
// (#720).
//
// idx_users_provider_lookup (provider, provider_user_id, #701) treats NULLs
// as distinct on both PostgreSQL and Oracle, so two concurrent email-only
// sparse inserts (performSparseUserInsert, api/authorization_enrichment.go)
// can both succeed and leave duplicate USERS rows for the same
// (provider, email). EnsureSparseUserEmailIndex closes that gap with a
// unique index restricted to sparse rows (provider_user_id IS NULL) only.
// This cannot be a GORM struct tag: the predicate/expression differs per
// dialect and AutoMigrate cannot express either portably, so it is raw DDL
// run from dbschema at the same three startup entry points as #704/#724.
package dbschema

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// sparseUserIndexName is the unique index EnsureSparseUserEmailIndex creates
// over users(provider, email) restricted to provider_user_id IS NULL rows.
const sparseUserIndexName = "idx_users_sparse_email"

// sparseDupKey is a raw scan target for the duplicate sparse-email query.
// Deliberately untagged (see userDupKey / groupDupKey): result-set labels
// come back UPPERCASE from Oracle and lowercase from Postgres, and an
// untagged field's DBName is derived from the active dialect's
// NamingStrategy, so the same struct matches both.
// SEM@0000000: hold a duplicate sparse-user (provider, email) key and its row count (pure)
type sparseDupKey struct {
	Provider string
	Email    string
	Cnt      int64
}

// EnsureSparseUserEmailIndex creates a unique index over (provider, email)
// restricted to rows with NULL provider_user_id, closing the duplicate-user
// gap idx_users_provider_lookup cannot cover on its own -- composite unique
// indexes treat NULLs as distinct on both PG and Oracle, so two concurrent
// email-only sparse inserts (#720) could otherwise both succeed. PG/SQLite
// use a partial index; Oracle a function-based index whose key expressions
// evaluate to NULL (hence excluded from the index) for every non-sparse row.
// email is not-null on models.User (size:320), so a NULL-email divergence
// between the two dialects' NULL handling is not reachable via the normal
// insert path (EnrichAuthorizationEntry already 400s an empty email).
//
// Idempotent; safe -- and required -- to call on every boot after
// AutoMigrate, including when the fingerprint fast path skips AutoMigrate
// entirely (this index is not part of the model fingerprint). Pre-existing
// sparse duplicates produce an actionable, named-rows error instead of being
// auto-merged, mirroring #724's operator-driven policy for user identities.
//
// Probes for the index by name first (oracle-db-admin review, 1.8.4): Oracle
// has no CREATE INDEX IF NOT EXISTS, so an unconditional Exec would re-issue
// the DDL -- and hit a swallowed-but-ERROR-logged ORA-00955 -- on every boot
// forever, exactly the failure mode ensureSchemaVersionTable
// (schema_version.go) already exists to avoid. Skipping both the duplicate
// scan and the DDL once the index is confirmed present also means this
// degrades to a single cheap catalog lookup in steady state instead of a
// full-table GROUP BY on every boot.
// SEM@0000000: build a dialect-appropriate unique index over sparse user emails (writes DB)
func EnsureSparseUserEmailIndex(db *gorm.DB) error {
	usersTable := (&models.User{}).TableName()
	if !db.Migrator().HasTable(usersTable) {
		return nil
	}

	var exists bool
	err := withMigrationRetry("sparse-user email index existence probe", func() error {
		var probeErr error
		exists, probeErr = sparseUserEmailIndexExists(db, usersTable)
		return probeErr
	})
	if err != nil {
		return fmt.Errorf("failed to check whether %s exists: %w", sparseUserIndexName, err)
	}
	if exists {
		return nil
	}

	// Fail with named rows, not an opaque ORA-01452/23505, if duplicates
	// already exist.
	var dups []sparseDupKey
	err = withMigrationRetry("sparse-user duplicate-email check", func() error {
		dups = dups[:0]
		return db.Table(usersTable).
			Select("provider, email, COUNT(*) AS cnt").
			Where("provider_user_id IS NULL").
			Group("provider, email").
			Having("COUNT(*) > 1").
			Scan(&dups).Error
	})
	if err != nil {
		return fmt.Errorf("failed to check users for duplicate sparse emails: %w", err)
	}
	if len(dups) > 0 {
		return fmt.Errorf("%s", formatSparseDupError(dups))
	}

	var ddl string
	switch db.Name() {
	case "oracle":
		// Function-based: non-sparse rows produce all-NULL keys and are
		// excluded from the index entirely, so uniqueness binds only rows
		// with provider_user_id IS NULL. No IF NOT EXISTS on Oracle -- the
		// existence probe above is the steady-state guard; ORA-00955 (name
		// already used) and ORA-01408 (identical column/expression list
		// already indexed under a different name) are swallowed below by
		// string match as belt-and-suspenders for a concurrent-create race,
		// the same idiom ensureSchemaVersionTable (schema_version.go) and
		// the alias-sequence installer (alias_sequence.go) already use to
		// avoid an unconditional godror import: godror is only ever imported
		// behind the `oracle` build tag (internal/dberrors/classify_oracle.go)
		// because it requires CGO and the Oracle Instant Client, and
		// dbschema has no such tag.
		ddl = fmt.Sprintf(
			`CREATE UNIQUE INDEX %s ON %s (CASE WHEN provider_user_id IS NULL THEN provider END, CASE WHEN provider_user_id IS NULL THEN email END)`,
			strings.ToUpper(sparseUserIndexName), usersTable)
	default: // postgres, sqlite
		ddl = fmt.Sprintf(
			`CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (provider, email) WHERE provider_user_id IS NULL`,
			sparseUserIndexName, usersTable)
	}

	// withMigrationRetry: CREATE INDEX takes a DDL lock on usersTable, which
	// is a hot table (login updates, sparse inserts). Against Oracle with
	// ddl_lock_timeout at its default of 0, a concurrent DML holder makes
	// this raise ORA-00054 (resource busy) immediately rather than waiting;
	// ORA-00054 is classified ErrTransient (internal/dberrors) and retried
	// here instead of hard-aborting startup (oracle-db-admin review, 1.8.4).
	err = withMigrationRetry("sparse-user email index create", func() error {
		return db.Exec(ddl).Error
	})
	if err != nil {
		if strings.Contains(err.Error(), "ORA-00955") {
			// A concurrent replica already won the create race; the index
			// exists and the invariant holds, so this is expected and quiet.
			return nil
		}
		if strings.Contains(err.Error(), "ORA-01408") {
			// A same-column-list index already exists under a different
			// name. Unlike ORA-00955 this doesn't guarantee the existing
			// index is UNIQUE, so it's worth a log line even though startup
			// continues (oracle-db-admin review, 1.8.4 round 2).
			slogging.Get().Warn("failed to create %s: an index over the same columns already exists under a different name (ORA-01408); continuing, but verify it is UNIQUE: %v", sparseUserIndexName, err)
			return nil
		}
		return fmt.Errorf("failed to create %s: %w", sparseUserIndexName, err)
	}
	return nil
}

// sparseUserEmailIndexExists reports whether a UNIQUE, valid index named
// sparseUserIndexName already exists on usersTable, probed with
// dialect-specific raw SQL rather than gorm.Migrator().HasIndex -- same
// pattern and reasoning as userProviderLookupIndexExists
// (user_identity_check.go): gorm-oracle's HasIndex binds the GORM tag's
// lowercase name as a bound query *parameter* against Oracle's
// USER_INDEXES.INDEX_NAME, which holds the uppercase name TMI's unquoted DDL
// actually created, so HasIndex always returns false for this index on
// Oracle.
//
// Matching by name alone would repeat #732's failure mode: an index named
// sparseUserIndexName that is non-unique, UNUSABLE, or (Oracle
// function-based) disabled would satisfy a bare existence check while
// enforcing nothing, and EnsureSparseUserEmailIndex would then skip the DDL
// forever. Not reachable today (the name is new in 1.8.4, so no deployed
// database can already hold a differently-defined index under it), but the
// stricter probe costs nothing and closes the gap before it can open
// (oracle-db-admin review, 1.8.4 round 2).
// SEM@0000000: probe whether a valid, unique sparse-user email index already exists, per dialect (reads DB)
func sparseUserEmailIndexExists(db *gorm.DB, usersTable string) (bool, error) {
	var cnt int64
	var err error
	switch db.Name() {
	case "oracle":
		err = db.Raw(
			"SELECT COUNT(*) FROM USER_INDEXES WHERE INDEX_NAME = ? AND TABLE_NAME = ? "+
				"AND UNIQUENESS = 'UNIQUE' AND STATUS = 'VALID' "+
				"AND (FUNCIDX_STATUS IS NULL OR FUNCIDX_STATUS = 'ENABLED')",
			strings.ToUpper(sparseUserIndexName), strings.ToUpper(usersTable),
		).Scan(&cnt).Error
	case "postgres":
		// pg_indexes spans every schema visible to the connecting role;
		// schema-qualify so a same-named index on a users table in another
		// schema can't produce a false positive (see #724 round 2).
		err = db.Raw(
			"SELECT COUNT(*) FROM pg_indexes WHERE indexname = ? AND tablename = ? "+
				"AND schemaname = current_schema() AND indexdef LIKE 'CREATE UNIQUE INDEX%'",
			sparseUserIndexName, usersTable,
		).Scan(&cnt).Error
	case "sqlite":
		// Test-fixture dialect only -- TMI's supported production dialects
		// are postgres and oracle.
		err = db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ? "+
				"AND sql LIKE 'CREATE UNIQUE INDEX%'",
			sparseUserIndexName, usersTable,
		).Scan(&cnt).Error
	default:
		// Unsupported dialect: fail closed (assume absent) so behavior falls
		// back to running the duplicate check and DDL rather than silently
		// skipping index creation on a dialect this probe doesn't understand.
		slogging.Get().Warn("sparseUserEmailIndexExists: unsupported dialect %q, assuming index absent", db.Name())
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// formatSparseDupError renders dups into an actionable startup error naming
// each duplicate (provider, email) pair and the index that cannot be
// created. maxReportedPairs caps the PII surfaced in the message/log: email
// is personal data, and an unbounded list would let a badly-merged database
// dump an arbitrarily long line of it into the startup log (same cap and
// reasoning as CheckDuplicateUserProviderIdentities, user_identity_check.go).
// SEM@0000000: render duplicate sparse-user rows into a capped, actionable error message (pure)
func formatSparseDupError(dups []sparseDupKey) string {
	const maxReportedPairs = 20
	shown := dups
	suffix := ""
	if len(dups) > maxReportedPairs {
		shown = dups[:maxReportedPairs]
		suffix = fmt.Sprintf(" (and %d more)", len(dups)-maxReportedPairs)
	}
	pairs := make([]string, len(shown))
	for i, d := range shown {
		pairs[i] = fmt.Sprintf("(provider=%s, email=%s, rows=%d)", d.Provider, d.Email, d.Cnt)
	}
	return fmt.Sprintf(
		"users table contains %d duplicate sparse identities (NULL provider_user_id) sharing (provider, email): %s%s. "+
			"The unique index %s cannot be created and startup cannot continue. Manually merge or "+
			"delete the duplicate user rows, then restart",
		len(dups), strings.Join(pairs, "; "), suffix, sparseUserIndexName)
}
