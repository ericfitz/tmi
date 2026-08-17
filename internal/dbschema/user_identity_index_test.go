package dbschema

import (
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
