package dbschema

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestInstallThreatModelAliasSequence_SqliteNoOp verifies the installer is a
// clean no-op on SQLite (which keeps the row-counter allocator) and does not
// require the threat_models / alias_counters tables to exist.
func TestInstallThreatModelAliasSequence_SqliteNoOp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, InstallThreatModelAliasSequence(context.Background(), db))
	// Idempotent: a second call is still a clean no-op.
	require.NoError(t, InstallThreatModelAliasSequence(context.Background(), db))
}
