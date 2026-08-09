package dbschema

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// dedupeTestUser mirrors the column shape of api/models.User closely enough
// to exercise CheckDuplicateUserProviderIdentities in isolation, matching
// group_dedupe_test.go's convention of a test-local struct rather than
// importing the real model.
// SEM@bb40881560ec43c848a818a906635c7d26b0b603: represent a minimal users row for exercising the provider-identity duplicate check (pure)
type dedupeTestUser struct {
	ID             string  `gorm:"column:id;primaryKey"`
	Provider       string  `gorm:"column:provider"`
	ProviderUserID *string `gorm:"column:provider_user_id"`
}

// SEM@bb40881560ec43c848a818a906635c7d26b0b603: map the test user struct to the users table name (pure)
func (dedupeTestUser) TableName() string { return "users" }

// SEM@bb40881560ec43c848a818a906635c7d26b0b603: build an in-memory SQLite DB migrated with the test users table
func newUserIdentityCheckTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dedupeTestUser{}))
	return db
}

// SEM@bb40881560ec43c848a818a906635c7d26b0b603: validate distinct provider identities and multiple NULL provider_user_id rows pass the duplicate check
func TestCheckDuplicateUserProviderIdentities_Clean(t *testing.T) {
	db := newUserIdentityCheckTestDB(t)

	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: strPtr("alice"),
	}).Error)
	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: strPtr("bob"),
	}).Error)
	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "okta", ProviderUserID: strPtr("alice"),
	}).Error)
	// Multiple NULL provider_user_id rows are NOT duplicates for this check.
	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: nil,
	}).Error)
	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: nil,
	}).Error)

	err := CheckDuplicateUserProviderIdentities(db)
	assert.NoError(t, err)
}

// SEM@bb40881560ec43c848a818a906635c7d26b0b603: validate duplicate provider identities are detected and reported with an actionable error
func TestCheckDuplicateUserProviderIdentities_Duplicates(t *testing.T) {
	db := newUserIdentityCheckTestDB(t)

	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "okta", ProviderUserID: strPtr("dupuser"),
	}).Error)
	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "okta", ProviderUserID: strPtr("dupuser"),
	}).Error)
	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "okta", ProviderUserID: strPtr("dupuser"),
	}).Error)

	err := CheckDuplicateUserProviderIdentities(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "okta")
	assert.Contains(t, err.Error(), "dupuser")
	assert.Contains(t, err.Error(), "3")
	assert.Contains(t, err.Error(), "idx_users_provider_lookup")
}

// SEM@bb40881560ec43c848a818a906635c7d26b0b603: validate the duplicate provider-identity check is a no-op when the users table is absent
func TestCheckDuplicateUserProviderIdentities_NoTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = CheckDuplicateUserProviderIdentities(db)
	assert.NoError(t, err)
}

// TestCheckDuplicateUserProviderIdentities_DuplicatesWithExistingIndex covers
// the #732 case a plain oracle-db-admin BLOCKING review surfaced: a database
// that already has (a pre-#701, non-unique) idx_users_provider_lookup was
// never actually going to hit AutoMigrate's CREATE UNIQUE INDEX abort, so
// duplicates found alongside an existing index of that name must NOT abort
// startup -- only a genuinely-absent index does.
// SEM@bb40881560ec43c848a818a906635c7d26b0b603: validate duplicates found alongside a pre-existing non-unique lookup index do not abort startup
func TestCheckDuplicateUserProviderIdentities_DuplicatesWithExistingIndex(t *testing.T) {
	db := newUserIdentityCheckTestDB(t)

	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "okta", ProviderUserID: strPtr("dupuser"),
	}).Error)
	require.NoError(t, db.Create(&dedupeTestUser{
		ID: uuid.NewString(), Provider: "okta", ProviderUserID: strPtr("dupuser"),
	}).Error)

	// Pre-create a NON-unique index with the exact production name, standing
	// in for a pre-#701 deployment whose index AutoMigrate cannot upgrade to
	// unique in place.
	require.NoError(t, db.Exec("CREATE INDEX idx_users_provider_lookup ON users(provider, provider_user_id)").Error)

	err := CheckDuplicateUserProviderIdentities(db)
	assert.NoError(t, err, "an existing (non-unique) index means AutoMigrate was never going to abort, so startup must continue")
}
