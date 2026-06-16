package dbschema

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestInstallPostgresDefaultIsolation_SqliteNoOp verifies the installer is a
// clean no-op on non-PostgreSQL dialects (the per-transaction wrapper is the
// only isolation lever there).
func TestInstallPostgresDefaultIsolation_SqliteNoOp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, InstallPostgresDefaultIsolation(context.Background(), db))
	// Idempotent: still a clean no-op on re-run.
	require.NoError(t, InstallPostgresDefaultIsolation(context.Background(), db))
}
