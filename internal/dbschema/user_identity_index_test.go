package dbschema

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// createNonUniqueLookupIndex installs the pre-#701 shape: a plain, non-unique
// idx_users_provider_lookup over (provider, provider_user_id). This is the
// state every database created before #701 is still in, and the state
// AutoMigrate cannot get out of.
func createNonUniqueLookupIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(
		"CREATE INDEX "+userProviderLookupIndexName+" ON users (provider, provider_user_id)").Error)
}

// TestEnsureUserProviderLookupUnique_UpgradesNonUniqueIndex is #732's core
// case: a pre-existing non-unique index must be dropped and recreated UNIQUE,
// and the constraint must actually bind afterwards.
func TestEnsureUserProviderLookupUnique_UpgradesNonUniqueIndex(t *testing.T) {
	db := newSparseIndexTestDB(t)
	createNonUniqueLookupIndex(t, db)

	exists, unique, err := userProviderLookupIndexState(db, "users")
	require.NoError(t, err)
	require.True(t, exists)
	require.False(t, unique, "fixture must start in the pre-#701 non-unique state")

	require.NoError(t, EnsureUserProviderLookupUnique(db))

	exists, unique, err = userProviderLookupIndexState(db, "users")
	require.NoError(t, err)
	assert.True(t, exists, "the index must still exist after the upgrade")
	assert.True(t, unique, "the index must be UNIQUE after the upgrade")

	// The constraint must genuinely bind now, not merely read as unique.
	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: strPtr("subject-1"), Email: "a@example.com",
	}).Error)
	require.Error(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: strPtr("subject-1"), Email: "b@example.com",
	}).Error, "a duplicate (provider, provider_user_id) must now be rejected")
}

// TestEnsureUserProviderLookupUnique_Idempotent covers the steady state: once
// the index is unique, repeated boots must leave it alone.
func TestEnsureUserProviderLookupUnique_Idempotent(t *testing.T) {
	db := newSparseIndexTestDB(t)
	createNonUniqueLookupIndex(t, db)

	require.NoError(t, EnsureUserProviderLookupUnique(db))
	require.NoError(t, EnsureUserProviderLookupUnique(db), "must be idempotent")

	exists, unique, err := userProviderLookupIndexState(db, "users")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.True(t, unique)
}

// TestEnsureUserProviderLookupUnique_CreatesMissingIndex covers the fast-path
// hole: when the #480 fingerprint check skips AutoMigrate, nothing else would
// ever create the index, so a wholly absent one must be created UNIQUE here.
func TestEnsureUserProviderLookupUnique_CreatesMissingIndex(t *testing.T) {
	db := newSparseIndexTestDB(t)

	exists, _, err := userProviderLookupIndexState(db, "users")
	require.NoError(t, err)
	require.False(t, exists, "fixture must start with no lookup index at all")

	require.NoError(t, EnsureUserProviderLookupUnique(db))

	exists, unique, err := userProviderLookupIndexState(db, "users")
	require.NoError(t, err)
	assert.True(t, exists, "a missing index must be created")
	assert.True(t, unique, "and created UNIQUE")
}

// TestEnsureUserProviderLookupUnique_DuplicatesBlockUpgrade covers the gate:
// duplicate identities make the unique index impossible, so the upgrade must
// be skipped (leaving the non-unique index in place) rather than attempted and
// failed -- and startup must continue, since merging identities is an operator
// decision (#724's policy).
func TestEnsureUserProviderLookupUnique_DuplicatesBlockUpgrade(t *testing.T) {
	db := newSparseIndexTestDB(t)
	createNonUniqueLookupIndex(t, db)

	for range 2 {
		require.NoError(t, db.Create(&sparseIndexTestUser{
			ID: uuid.NewString(), Provider: "okta", ProviderUserID: strPtr("shared-subject"), Email: uuid.NewString() + "@example.com",
		}).Error)
	}

	require.NoError(t, EnsureUserProviderLookupUnique(db), "duplicates must not abort startup")

	exists, unique, err := userProviderLookupIndexState(db, "users")
	require.NoError(t, err)
	assert.True(t, exists, "the existing index must be left in place, not dropped")
	assert.False(t, unique, "the upgrade must not have been attempted over duplicate rows")
}

// TestEnsureUserProviderLookupUnique_NoTable covers a database whose users
// table does not exist yet (a first-ever provisioning run reaching this before
// AutoMigrate): nothing to do, no error.
func TestEnsureUserProviderLookupUnique_NoTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, EnsureUserProviderLookupUnique(db))
}

// TestUserProviderLookupDDL_OracleUsesUppercase pins the Oracle identifier
// casing: TMI runs with SkipQuoteIdentifiers, so every object it created was
// folded to upper case and a lowercase DROP INDEX would raise ORA-01418
// ("specified index does not exist").
func TestUserProviderLookupDDL_OracleUsesUppercase(t *testing.T) {
	drop, createUnique, restore := userProviderLookupDDL("oracle", "USERS")
	assert.Equal(t, "DROP INDEX IDX_USERS_PROVIDER_LOOKUP", drop)
	assert.Contains(t, createUnique, "CREATE UNIQUE INDEX IDX_USERS_PROVIDER_LOOKUP ON USERS")
	assert.Contains(t, restore, "CREATE INDEX IDX_USERS_PROVIDER_LOOKUP ON USERS")
	assert.NotContains(t, createUnique, "IF NOT EXISTS", "Oracle has no IF NOT EXISTS on index DDL")

	drop, createUnique, _ = userProviderLookupDDL("postgres", "users")
	assert.Contains(t, drop, "IF EXISTS")
	assert.Contains(t, createUnique, "CREATE UNIQUE INDEX IF NOT EXISTS idx_users_provider_lookup ON users")
}

// TestUserProviderLookupDDL_OracleExcludesSparseRows pins the #760 fix: the
// Oracle index must be function-based so that a row with a NULL
// provider_user_id keys as all-NULL and is left out of the index entirely.
//
// A plain (PROVIDER, PROVIDER_USER_ID) unique index looks correct and is not:
// Oracle omits a b-tree entry only when EVERY key column is NULL, so with
// PROVIDER non-NULL two sparse rows under one provider collide with ORA-00001
// where PostgreSQL allows both. That divergence 500ed every email-only user
// after the first on Oracle.
func TestUserProviderLookupDDL_OracleExcludesSparseRows(t *testing.T) {
	_, createUnique, restore := userProviderLookupDDL("oracle", "USERS")

	assert.Contains(t, createUnique, "CASE WHEN PROVIDER_USER_ID IS NOT NULL THEN PROVIDER END",
		"PROVIDER must be wrapped so a sparse row's key is entirely NULL and Oracle skips it")

	// Key order is irrelevant to uniqueness but decides whether the hot auth
	// lookup gets an index range scan: a bare leading column is usable as an
	// access predicate, a leading CASE expression is not.
	subjectAt := strings.Index(createUnique, "(PROVIDER_USER_ID,")
	caseAt := strings.Index(createUnique, "CASE WHEN")
	assert.Positive(t, subjectAt, "PROVIDER_USER_ID must lead the key list")
	assert.Less(t, subjectAt, caseAt, "the bare column must precede the CASE expression")

	// The restore fallback is deliberately a plain non-unique index: its only
	// job is to keep the lookup path off a table scan, and a non-unique index
	// imposes no NULL semantics, so #760 cannot re-enter through it.
	assert.NotContains(t, restore, "CASE WHEN")
	assert.Contains(t, restore, "(PROVIDER, PROVIDER_USER_ID)")

	// PostgreSQL needs no such trick -- NULLS DISTINCT is the default there.
	_, pgCreateUnique, _ := userProviderLookupDDL("postgres", "users")
	assert.NotContains(t, pgCreateUnique, "CASE WHEN")
	assert.Contains(t, pgCreateUnique, "(provider, provider_user_id)")
}

// TestUserProviderLookupIndexState_WrongColumnListReadsAsNotOurs covers #756:
// a same-named UNIQUE index over a different column list must not satisfy the
// probe — name-matching alone is not a strong enough identity check for an
// index whose whole job is enforcing a constraint. The Ensure flow must then
// repair it to the intended definition.
func TestUserProviderLookupIndexState_WrongColumnListReadsAsNotOurs(t *testing.T) {
	db := newSparseIndexTestDB(t)
	require.NoError(t, db.Exec(
		"CREATE UNIQUE INDEX "+userProviderLookupIndexName+" ON users (email)").Error)

	exists, unique, err := userProviderLookupIndexState(db, "users")
	require.NoError(t, err)
	assert.True(t, exists, "the impostor occupies the name, so it exists")
	assert.False(t, unique, "a UNIQUE index over the wrong column list must not read as the intended definition")

	// The Ensure flow must drop the impostor and recreate the intended index.
	require.NoError(t, EnsureUserProviderLookupUnique(db))
	exists, unique, err = userProviderLookupIndexState(db, "users")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.True(t, unique, "after repair the intended (provider, provider_user_id) unique index must be in place")

	// And the intended constraint must genuinely bind.
	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: strPtr("subject-756"), Email: "a756@example.com",
	}).Error)
	require.Error(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: strPtr("subject-756"), Email: "b756@example.com",
	}).Error, "a duplicate (provider, provider_user_id) must be rejected after the repair")
}
