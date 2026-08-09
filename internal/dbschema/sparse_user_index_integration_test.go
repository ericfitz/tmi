//go:build dev || test || integration

package dbschema

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestSparseUserEmailIndex_Integration verifies EnsureSparseUserEmailIndex
// (#720) against real PostgreSQL: the index must exist in pg_indexes after
// creation, and a second email-only sparse insert sharing (provider, email)
// with an existing sparse row must fail with a unique violation. Reuses the
// DSN/bootstrap helpers from
// TestInstallPostgresDefaultIsolation_Integration (postgres_isolation_integration_test.go).
// SEM@87d1696b4bf3edbe042353cf7586a60de78c2028: validate the sparse user-email unique index against a live PostgreSQL schema (reads/writes DB)
func TestSparseUserEmailIndex_Integration(t *testing.T) {
	dsn := pgIsolationDSN(t)
	db := openPG(t, dsn)

	require.NoError(t, EnsureSparseUserEmailIndex(db), "creating the index must succeed against a live schema")
	require.NoError(t, EnsureSparseUserEmailIndex(db), "must be idempotent")

	usersTable := (&models.User{}).TableName()
	var indexExists bool
	require.NoError(t, db.Raw(
		"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE indexname = ? AND tablename = ? AND schemaname = current_schema())",
		sparseUserIndexName, usersTable,
	).Scan(&indexExists).Error)
	require.True(t, indexExists, "%s must exist in pg_indexes after creation", sparseUserIndexName)

	// A unique provider per run avoids colliding with real data in the
	// shared test database; ProviderUserID is left at its zero value
	// (NullableDBVarchar{Valid: false}), which serializes to NULL -- a sparse
	// row.
	provider := models.DBVarchar("sparse-index-itest-" + uuid.NewString())
	email := models.DBVarchar("sparse-index-itest@example.com")

	first := models.User{Provider: provider, Email: email, Name: "Sparse Test User 1"}
	require.NoError(t, db.Create(&first).Error)
	t.Cleanup(func() {
		require.NoError(t, db.Where("provider = ?", provider).Delete(&models.User{}).Error)
	})

	second := models.User{Provider: provider, Email: email, Name: "Sparse Test User 2"}
	err := db.Create(&second).Error
	require.Error(t, err, "a second sparse row sharing (provider, email) must violate %s", sparseUserIndexName)
}
