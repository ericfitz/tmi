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
type dedupeTestUser struct {
	ID             string  `gorm:"column:id;primaryKey"`
	Provider       string  `gorm:"column:provider"`
	ProviderUserID *string `gorm:"column:provider_user_id"`
}

func (dedupeTestUser) TableName() string { return "users" }

func newUserIdentityCheckTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dedupeTestUser{}))
	return db
}

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
