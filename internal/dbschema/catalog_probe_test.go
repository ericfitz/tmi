package dbschema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestMigrationTableExists covers #736's non-Oracle path: the helper must
// agree with gorm's own HasTable on the dialects where HasTable is already
// current-schema-scoped. The Oracle branch (ALL_TABLES filtered to
// CURRENT_SCHEMA) is exercised by the live-Oracle integration suite; it
// cannot be reached from a SQLite fixture.
func TestMigrationTableExists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	present, err := migrationTableExists(db, "users")
	require.NoError(t, err)
	assert.False(t, present, "no users table has been created yet")

	require.NoError(t, db.AutoMigrate(&sparseIndexTestUser{}))

	present, err = migrationTableExists(db, "users")
	require.NoError(t, err)
	assert.True(t, present, "users table exists after AutoMigrate")
}

// TestRequireMigrationTable covers the shared gate the startup invariant
// checks call: present -> (true, nil) and the caller proceeds; absent ->
// (false, nil) plus a WARN, so the skip is visible in the startup log instead
// of being a silent no-op (#736).
func TestRequireMigrationTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	present, err := requireMigrationTable(db, "users", "test step")
	require.NoError(t, err, "an absent table is not an error, only a skip")
	assert.False(t, present)

	require.NoError(t, db.AutoMigrate(&sparseIndexTestUser{}))

	present, err = requireMigrationTable(db, "users", "test step")
	require.NoError(t, err)
	assert.True(t, present)
}
