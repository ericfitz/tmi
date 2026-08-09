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
// SEM@87d1696b4bf3edbe042353cf7586a60de78c2028: mirror the users table columns for sparse-email-index test fixtures (pure)
type sparseIndexTestUser struct {
	ID             string  `gorm:"column:id;primaryKey"`
	Provider       string  `gorm:"column:provider"`
	ProviderUserID *string `gorm:"column:provider_user_id"`
	Email          string  `gorm:"column:email"`
}

// SEM@87d1696b4bf3edbe042353cf7586a60de78c2028: map a sparse-index test user fixture to the users table name (pure)
func (sparseIndexTestUser) TableName() string { return "users" }

// SEM@87d1696b4bf3edbe042353cf7586a60de78c2028: build an in-memory SQLite DB migrated with the sparse-index test user schema (pure)
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
// SEM@87d1696b4bf3edbe042353cf7586a60de78c2028: validate creating the sparse user email index is idempotent and enforces uniqueness only for NULL provider_user_id rows
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
// SEM@df7fd289991cfd0c30ec2f8c8721a7b593f7d535: validate preexisting sparse duplicate rows abort index creation with an actionable error
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
	assert.Contains(t, err.Error(), "rows=2")
	assert.Contains(t, err.Error(), sparseUserIndexName)
}

// TestEnsureSparseUserEmailIndex_ImpostorIndexWarnsAndContinues covers the
// same-name-but-not-#720 case flagged in team lead review round 3: a
// NON-unique index already carrying sparseUserIndexName makes both DDL arms
// (PG/SQLite's CREATE ... IF NOT EXISTS, Oracle's ORA-00955 swallow) no-op
// without ever creating real enforcement. Startup must still continue (nil
// return, mirroring the #732/#724 warn-and-continue policy), but the
// invariant must NOT actually be enforced. There is no log-capture helper in
// this test file's conventions, so this proves the post-DDL verification
// path ran and reached the correct conclusion the only observable way
// available: two sparse rows sharing (provider, email) must BOTH succeed,
// because the impostor index never enforced uniqueness and
// EnsureSparseUserEmailIndex's IF NOT EXISTS DDL declined to replace it.
// SEM@df7fd289991cfd0c30ec2f8c8721a7b593f7d535: validate a non-unique impostor index leaves uniqueness unenforced but startup continues
func TestEnsureSparseUserEmailIndex_ImpostorIndexWarnsAndContinues(t *testing.T) {
	db := newSparseIndexTestDB(t)

	// A non-unique impostor occupying #720's index name -- e.g. left over
	// from a manual, non-#720 index or a partial/aborted migration.
	require.NoError(t, db.Exec(
		"CREATE INDEX "+sparseUserIndexName+" ON users(provider, email)",
	).Error)

	err := EnsureSparseUserEmailIndex(db)
	require.NoError(t, err, "an impostor index must not abort startup")

	// If the impostor had been silently treated as #720's own valid index,
	// this insert pair would fail exactly like
	// TestEnsureSparseUserEmailIndex_CreatesAndIsIdempotent's duplicate
	// case. It must NOT fail here: the impostor is non-unique, and CREATE
	// ... IF NOT EXISTS declined to replace it, so uniqueness is genuinely
	// unenforced.
	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: nil, Email: "impostor@example.com",
	}).Error)
	require.NoError(t, db.Create(&sparseIndexTestUser{
		ID: uuid.NewString(), Provider: "tmi", ProviderUserID: nil, Email: "impostor@example.com",
	}).Error, "impostor index must not silently be treated as enforcing uniqueness")
}

// TestEnsureSparseUserEmailIndex_LegacyNullProviderRowsIgnored covers a final
// review finding: the duplicate-sparse-email pre-check must filter out rows
// with a NULL provider (not just NULL provider_user_id), mirroring
// user_identity_check.go's "provider IS NOT NULL" discipline. Without that
// filter, two legacy rows sharing provider=NULL, email=X would GROUP BY as
// one duplicate group (SQL groups NULLs together) and false-positive the
// hard abort -- even though provider is NOT NULL on the current model, so
// this can only happen via pre-migration or manually-inserted data, and even
// though PG/Oracle unique indexes treat NULL key values as distinct (the
// actual index would never reject such rows), so the check would be aborting
// startup over rows the index itself does not consider duplicates.
// SEM@f89431d54295774a73b0813422dc99486a99df5c: validate legacy rows with a NULL provider are excluded from the sparse-duplicate pre-check
func TestEnsureSparseUserEmailIndex_LegacyNullProviderRowsIgnored(t *testing.T) {
	db := newSparseIndexTestDB(t)

	// Raw SQL, not the sparseIndexTestUser struct: Provider is a non-pointer
	// string there, so GORM would insert "" for its zero value, never SQL
	// NULL. A NULL provider can only be produced by bypassing GORM's normal
	// insert path -- exactly how such a row could only exist in practice
	// (pre-migration/manual data).
	require.NoError(t, db.Exec(
		"INSERT INTO users (id, provider, provider_user_id, email) VALUES (?, NULL, NULL, ?)",
		uuid.NewString(), "legacy@example.com",
	).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO users (id, provider, provider_user_id, email) VALUES (?, NULL, NULL, ?)",
		uuid.NewString(), "legacy@example.com",
	).Error)

	require.NoError(t, EnsureSparseUserEmailIndex(db), "NULL-provider rows must not false-positive the duplicate-sparse-email abort")
}

// SEM@87d1696b4bf3edbe042353cf7586a60de78c2028: validate sparse user email index creation is a no-op when the users table is absent
func TestEnsureSparseUserEmailIndex_NoTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = EnsureSparseUserEmailIndex(db)
	assert.NoError(t, err)
}
