package dbschema

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// sparseIndexTestUser mirrors the column shape of api/models.User closely
// enough to exercise EnsureSparseUserEmailIndex in isolation, matching
// dedupeTestUser's (user_identity_check_test.go) and dedupeTestGroup's
// (group_dedupe_test.go) convention of a test-local struct rather than
// importing the real model.
type sparseIndexTestUser struct {
	ID             string  `gorm:"column:id;primaryKey"`
	Provider       string  `gorm:"column:provider"`
	ProviderUserID *string `gorm:"column:provider_user_id"`
	Email          string  `gorm:"column:email"`
}

func (sparseIndexTestUser) TableName() string { return "users" }

func newSparseIndexTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sparseIndexTestUser{}))
	return db
}

// TestEnsureSparseUserEmailIndex_CreatesAndIsIdempotent covers #720: the
// index must be creatable twice without error, must reject a second sparse
// (NULL provider_user_id) row sharing (provider, email) with an existing
// sparse row, and must NOT constrain rows that carry a non-NULL
// provider_user_id even when they share (provider, email).
func TestEnsureSparseUserEmailIndex_CreatesAndIsIdempotent(t *testing.T) {
	db := newSparseIndexTestDB(t)

	require.NoError(t, EnsureSparseUserEmailIndex(db))
	require.NoError(t, EnsureSparseUserEmailIndex(db), "must be idempotent")

	// Two sparse (NULL provider_user_id) rows sharing (provider, email) must
	// collide on the new unique index.
	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: nil, Email: "alice@example.com",
	}).Error)
	err := db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: nil, Email: "alice@example.com",
	}).Error
	require.Error(t, err, "a second sparse row sharing (provider, email) must violate the unique index")

	// Two rows sharing (provider, email) but with distinct, non-NULL
	// provider_user_id are not sparse and must not collide.
	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: strPtr("user-1"), Email: "bob@example.com",
	}).Error)
	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: strPtr("user-2"), Email: "bob@example.com",
	}).Error)
}

// TestEnsureSparseUserEmailIndex_PreexistingSparseDuplicates covers #720's
// no-auto-merge policy (mirroring #724 for provider identities): duplicate
// sparse rows found ahead of index creation must abort with an actionable,
// named-rows error rather than being silently merged or dropped.
func TestEnsureSparseUserEmailIndex_PreexistingSparseDuplicates(t *testing.T) {
	db := newSparseIndexTestDB(t)

	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "okta", ProviderUserID: nil, Email: "dup@example.com",
	}).Error)
	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "okta", ProviderUserID: nil, Email: "dup@example.com",
	}).Error)

	err := EnsureSparseUserEmailIndex(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "okta")
	assert.Contains(t, err.Error(), "dup@example.com")
	assert.Contains(t, err.Error(), "2")
	assert.Contains(t, err.Error(), sparseUserIndexName)
}

func TestEnsureSparseUserEmailIndex_NoTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = EnsureSparseUserEmailIndex(db)
	assert.NoError(t, err)
}
