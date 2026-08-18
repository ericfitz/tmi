//go:build oracle

package dbschema_test

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Identifiers are spelled uppercase here because TMI runs Oracle with
// SkipQuoteIdentifiers: true, so every object it created was folded to upper
// case. The index name is duplicated from dbschema's unexported
// sparseUserIndexName deliberately: this file is an external test package, and
// a catalog test should assert against the name Oracle actually holds rather
// than re-derive it from the code under test.
const (
	oracleSparseIndexName = "IDX_USERS_SPARSE_EMAIL"
	oracleUsersTable      = "USERS"
	// oracleSparseDupIndexName is a throwaway index this test creates over the
	// same expression list as the real one, to provoke ORA-01408.
	oracleSparseDupIndexName = "IDX_TMI_ITEST_SPARSE_DUP"
	// oracleSparseKeyExprs is the CASE-expression key list
	// EnsureSparseUserEmailIndex builds on Oracle: non-sparse rows produce
	// all-NULL keys and are excluded from the index entirely.
	oracleSparseKeyExprs = "(CASE WHEN PROVIDER_USER_ID IS NULL THEN PROVIDER END, CASE WHEN PROVIDER_USER_ID IS NULL THEN EMAIL END)"
)

// oracleIndexState is a raw scan target for the ALL_INDEXES probe.
// Deliberately untagged: Oracle returns UPPERCASE result-set labels and an
// untagged field's DBName is derived from the active dialect's
// NamingStrategy, so the mapping holds (the same convention as dbschema's own
// sparseDupKey; see also #699).
type oracleIndexState struct {
	Uniqueness    string
	Status        string
	IndexType     string
	FuncidxStatus string
}

// readOracleIndexState reports what Oracle's catalog actually holds for an
// index name, scoped to this session's CURRENT_SCHEMA the same way the
// production probes are (#736).
func readOracleIndexState(t *testing.T, db *gorm.DB, indexName string) (oracleIndexState, bool) {
	t.Helper()
	var rows []oracleIndexState
	// Pins OWNER as well as TABLE_OWNER, matching the production probes: this
	// is the assertion that would otherwise fail to notice the foreign-owned
	// same-named index those probes now guard against (oracle-db-admin
	// re-review, #736).
	require.NoError(t, db.Raw(
		"SELECT UNIQUENESS, STATUS, INDEX_TYPE, NVL(FUNCIDX_STATUS, 'NONE') AS FUNCIDX_STATUS FROM ALL_INDEXES "+
			"WHERE INDEX_NAME = ? AND TABLE_NAME = ? "+
			"AND OWNER = SYS_CONTEXT('USERENV','CURRENT_SCHEMA') AND TABLE_OWNER = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')",
		indexName, oracleUsersTable,
	).Scan(&rows).Error)
	if len(rows) == 0 {
		return oracleIndexState{}, false
	}
	return rows[0], true
}

// TestSparseUserEmailIndexOracleIntegration covers EnsureSparseUserEmailIndex
// (#720) against real Oracle ADB — the paths that only exist on Oracle and
// that no SQLite or PostgreSQL test can reach (#735):
//
//   - the ALL_INDEXES probe, including the UNIQUENESS / STATUS / FUNCIDX_STATUS
//     predicates that decide whether the invariant is actually enforced;
//   - the CASE-expression function-based DDL, issued through #734's pinned
//     session with DDL_LOCK_TIMEOUT set, and the sparse-only uniqueness
//     semantics it is supposed to produce;
//   - ORA-00955 handling on a second call (idempotency);
//   - the impostor-index verify path — a same-named index that is not #720's
//     own must be left in place and reported, not silently trusted;
//   - the ORA-01408 branch — an identical expression list already indexed
//     under a different name must warn and continue, not abort startup.
//
// Named ...OracleIntegration so `make test-integration-oci`'s oracle-tagged
// phase (-run OracleIntegration) selects it.
//
// WARNING: this test drops and recreates real indexes on the real USERS
// table, and between a DROP and its restore those rows are neither unique-
// constrained nor indexed. Every phase restores what it changed and t.Cleanup
// re-ensures the production index at the end, but run it only against a
// dedicated test ADB -- never one shared with real users.
func TestSparseUserEmailIndexOracleIntegration(t *testing.T) {
	db := openOracleDB(t)

	// Whatever this test does to the index, the database must end up with the
	// real one back. Registered before the first mutation so it also runs if
	// an assertion fails partway through.
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX " + oracleSparseDupIndexName).Error
		if state, found := readOracleIndexState(t, db, oracleSparseIndexName); found && state.Uniqueness != "UNIQUE" {
			_ = db.Exec("DROP INDEX " + oracleSparseIndexName).Error
		}
		require.NoError(t, dbschema.EnsureSparseUserEmailIndex(db),
			"the production sparse-email index must be restored after this test")
	})

	// --- Phase 1: create, and confirm what Oracle actually built -----------
	require.NoError(t, dbschema.EnsureSparseUserEmailIndex(db),
		"creating the sparse-email index must succeed against a live Oracle schema")

	state, found := readOracleIndexState(t, db, oracleSparseIndexName)
	require.True(t, found, "%s must exist in ALL_INDEXES after creation", oracleSparseIndexName)
	assert.Equal(t, "UNIQUE", state.Uniqueness, "the index must be UNIQUE or it enforces nothing")
	assert.Equal(t, "VALID", state.Status, "an UNUSABLE index enforces nothing")
	assert.Equal(t, "ENABLED", state.FuncidxStatus,
		"the CASE-expression index must be function-based and enabled; a DISABLED function-based index enforces nothing")

	// --- Phase 2: idempotency (the ORA-00955 swallow + verify path) --------
	require.NoError(t, dbschema.EnsureSparseUserEmailIndex(db), "must be idempotent")
	state, found = readOracleIndexState(t, db, oracleSparseIndexName)
	require.True(t, found)
	assert.Equal(t, "UNIQUE", state.Uniqueness, "a second call must not have degraded the index")

	// --- Phase 3: the uniqueness semantics the CASE expressions encode ----
	// A unique provider per run keeps this from colliding with real data in
	// the shared ADB schema.
	provider := models.DBVarchar("sparse-oracle-itest-" + uuid.NewString())
	email := models.DBVarchar("sparse-oracle-itest@example.com")
	t.Cleanup(func() {
		require.NoError(t, db.Where("provider = ?", provider).Delete(&models.User{}).Error)
	})

	// ProviderUserID left at its zero value (NullableDBVarchar{Valid: false})
	// serializes to NULL -- a sparse row, which the index constrains.
	require.NoError(t, db.Create(&models.User{Provider: provider, Email: email, Name: "Oracle Sparse 1"}).Error)
	require.Error(t, db.Create(&models.User{Provider: provider, Email: email, Name: "Oracle Sparse 2"}).Error,
		"a second sparse row sharing (provider, email) must violate %s", oracleSparseIndexName)

	// Non-sparse rows produce all-NULL index keys and are excluded from the
	// index, so they may share (provider, email) freely.
	puid1, puid2 := "oracle-itest-subject-1", "oracle-itest-subject-2"
	require.NoError(t, db.Create(&models.User{
		Provider: provider, Email: email, Name: "Oracle NonSparse 1",
		ProviderUserID: models.NewNullableDBVarchar(&puid1),
	}).Error)
	require.NoError(t, db.Create(&models.User{
		Provider: provider, Email: email, Name: "Oracle NonSparse 2",
		ProviderUserID: models.NewNullableDBVarchar(&puid2),
	}).Error, "rows with a non-NULL provider_user_id must not be constrained by the sparse index")

	// --- Phase 4: the impostor-index verify path --------------------------
	// A same-named index that is not #720's own: EnsureSparseUserEmailIndex
	// must hit ORA-00955, verify, discover the invariant is NOT enforced, log
	// it, and continue -- without dropping an index it did not create.
	require.NoError(t, db.Exec("DROP INDEX "+oracleSparseIndexName).Error)
	// The impostor is built over (NAME), not (EMAIL): models.User already
	// carries index:idx_users_email over exactly (EMAIL), and Oracle rejects a
	// second index over an identical column list with ORA-01408 regardless of
	// its name -- which is Phase 5's subject, not this one. (NAME) is the only
	// User column with no index of its own (oracle-db-admin review, #735).
	require.NoError(t, db.Exec("CREATE INDEX "+oracleSparseIndexName+" ON "+oracleUsersTable+" (NAME)").Error)

	require.NoError(t, dbschema.EnsureSparseUserEmailIndex(db),
		"an impostor index must not abort startup")
	state, found = readOracleIndexState(t, db, oracleSparseIndexName)
	require.True(t, found, "the impostor must still be there")
	assert.Equal(t, "NONUNIQUE", state.Uniqueness,
		"the impostor must be left in place -- startup code must not drop an index it did not create")

	require.NoError(t, db.Exec("DROP INDEX "+oracleSparseIndexName).Error)
	require.NoError(t, dbschema.EnsureSparseUserEmailIndex(db))
	state, found = readOracleIndexState(t, db, oracleSparseIndexName)
	require.True(t, found)
	require.Equal(t, "UNIQUE", state.Uniqueness, "the real index must be restored before the next phase")

	// --- Phase 5: the ORA-01408 branch ------------------------------------
	// The same expression list already indexed under a different name. Oracle
	// refuses the create with ORA-01408 rather than ORA-00955, which must warn
	// and continue rather than abort.
	require.NoError(t, db.Exec("DROP INDEX "+oracleSparseIndexName).Error)
	require.NoError(t, db.Exec(
		"CREATE UNIQUE INDEX "+oracleSparseDupIndexName+" ON "+oracleUsersTable+" "+oracleSparseKeyExprs).Error)

	require.NoError(t, dbschema.EnsureSparseUserEmailIndex(db),
		"ORA-01408 (same column/expression list under another name) must warn and continue, not abort startup")
	_, found = readOracleIndexState(t, db, oracleSparseIndexName)
	assert.False(t, found, "the real index name must still be free -- ORA-01408 means nothing was created under it")

	require.NoError(t, db.Exec("DROP INDEX "+oracleSparseDupIndexName).Error)
}

// TestUserProviderLookupUniqueOracleIntegration covers #732 against real
// Oracle: a pre-#701 non-unique idx_users_provider_lookup must be detected
// from the catalog (gorm's HasIndex cannot see it at all on Oracle) and
// upgraded in place to a genuine UNIQUE index.
//
// This is the case that motivated #732 -- it cannot be reproduced anywhere
// but Oracle, because the detection failure is specific to gorm-oracle
// binding a lowercase index name against the uppercase names Oracle folded
// TMI's unquoted DDL into.
//
// Carries the same shared-database warning as
// TestSparseUserEmailIndexOracleIntegration. It also fails, correctly and for
// environmental reasons, if the target schema already holds duplicate
// (provider, provider_user_id) rows: the upgrade refuses to run over
// duplicates, so the final assertion sees a still-non-unique index. Clean the
// duplicates the log names, then re-run.
func TestUserProviderLookupUniqueOracleIntegration(t *testing.T) {
	const lookupIndexName = "IDX_USERS_PROVIDER_LOOKUP"
	db := openOracleDB(t)

	// Restore a UNIQUE index no matter how this test exits.
	t.Cleanup(func() {
		require.NoError(t, dbschema.EnsureUserProviderLookupUnique(db),
			"the production provider-lookup index must be UNIQUE after this test")
	})

	require.NoError(t, dbschema.EnsureUserProviderLookupUnique(db))
	state, found := readOracleIndexState(t, db, lookupIndexName)
	require.True(t, found, "%s must exist", lookupIndexName)
	require.Equal(t, "UNIQUE", state.Uniqueness, "baseline: the index must be unique before the downgrade below")

	// Recreate the pre-#701 shape: same name, no uniqueness. This is exactly
	// what every database created before #701 still carries, and what
	// AutoMigrate silently fails to upgrade.
	require.NoError(t, db.Exec("DROP INDEX "+lookupIndexName).Error)
	require.NoError(t, db.Exec(
		"CREATE INDEX "+lookupIndexName+" ON "+oracleUsersTable+" (PROVIDER, PROVIDER_USER_ID)").Error)
	state, found = readOracleIndexState(t, db, lookupIndexName)
	require.True(t, found)
	require.Equal(t, "NONUNIQUE", state.Uniqueness, "fixture must be in the pre-#701 state")

	// gorm's own migrator cannot even see this index on Oracle -- the bug
	// that let the non-unique index survive every AutoMigrate pass.
	assert.False(t, db.Migrator().HasIndex(&models.User{}, "idx_users_provider_lookup"),
		"documents the #732 root cause: gorm-oracle's HasIndex binds the lowercase tag name against uppercase USER_INDEXES rows and never matches")

	require.NoError(t, dbschema.EnsureUserProviderLookupUnique(db), "the upgrade must run and must not abort startup")

	state, found = readOracleIndexState(t, db, lookupIndexName)
	require.True(t, found, "the index must exist after the upgrade")
	assert.Equal(t, "UNIQUE", state.Uniqueness, "the upgrade must leave a genuinely UNIQUE index")
	assert.Equal(t, "VALID", state.Status)
	// #760: it must be function-based, not a plain column-list unique index.
	// A plain one reports UNIQUE and VALID while enforcing stricter NULL
	// semantics than PostgreSQL -- which is the bug, not the fix.
	assert.Equal(t, "FUNCTION-BASED NORMAL", state.IndexType,
		"the index must be function-based so sparse rows key as all-NULL and drop out (#760)")
	assert.Equal(t, "ENABLED", state.FuncidxStatus,
		"a DISABLED function-based index enforces nothing and breaks DML on the table")

	// And it must actually bind: a duplicate (provider, provider_user_id) is
	// now rejected by the database, not merely reported as unique by a
	// catalog view.
	provider := models.DBVarchar("lookup-oracle-itest-" + uuid.NewString())
	subject := "lookup-oracle-itest-subject"
	t.Cleanup(func() {
		require.NoError(t, db.Where("provider = ?", provider).Delete(&models.User{}).Error)
	})
	require.NoError(t, db.Create(&models.User{
		Provider: provider, Email: models.DBVarchar("lookup-1@example.com"), Name: "Lookup 1",
		ProviderUserID: models.NewNullableDBVarchar(&subject),
	}).Error)
	require.Error(t, db.Create(&models.User{
		Provider: provider, Email: models.DBVarchar("lookup-2@example.com"), Name: "Lookup 2",
		ProviderUserID: models.NewNullableDBVarchar(&subject),
	}).Error, "a duplicate (provider, provider_user_id) must be rejected once the index is UNIQUE")

	// --- #760 regression --------------------------------------------------
	// The constraint must stop at rows that actually carry an identity. Two
	// sparse rows sharing a provider are two different people who have not
	// logged in yet, and PostgreSQL has always allowed them; a plain unique
	// index on Oracle rejected the second with ORA-00001, which
	// performSparseUserInsert then turned into an HTTP 500 for every
	// email-only user after the first.
	//
	// Distinct emails, because #720's idx_users_sparse_email legitimately
	// rejects sparse rows that share (provider, email) -- that is a different
	// invariant and it stays enforced.
	sparseProvider := models.DBVarchar("lookup-oracle-itest-sparse-" + uuid.NewString())
	t.Cleanup(func() {
		require.NoError(t, db.Where("provider = ?", sparseProvider).Delete(&models.User{}).Error)
	})
	require.NoError(t, db.Create(&models.User{
		Provider: sparseProvider, Email: models.DBVarchar("sparse-a@example.com"), Name: "Sparse A",
	}).Error, "the first sparse row must insert")
	require.NoError(t, db.Create(&models.User{
		Provider: sparseProvider, Email: models.DBVarchar("sparse-b@example.com"), Name: "Sparse B",
	}).Error, "a second sparse row under the same provider must NOT collide -- this is #760")
}
