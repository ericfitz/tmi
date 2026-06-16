//go:build dev || test || integration

package dbschema

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func pgIsolationDSN(t *testing.T) string {
	t.Helper()
	host := os.Getenv("TEST_DB_HOST")
	port := os.Getenv("TEST_DB_PORT")
	user := os.Getenv("TEST_DB_USER")
	password := os.Getenv("TEST_DB_PASSWORD")
	dbname := os.Getenv("TEST_DB_NAME")
	if host == "" || port == "" || user == "" || dbname == "" {
		t.Skip("TEST_DB_* not set; default-isolation test requires PostgreSQL")
	}
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		host, port, user, password, dbname,
	)
}

func openPG(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err, "open PostgreSQL")
	return db
}

// TestInstallPostgresDefaultIsolation_Integration verifies the #450
// defense-in-depth backstop: after the installer pins the role default, a
// freshly-opened (wrapper-less) connection starts its sessions at SERIALIZABLE.
func TestInstallPostgresDefaultIsolation_Integration(t *testing.T) {
	ctx := context.Background()
	dsn := pgIsolationDSN(t)

	installer := openPG(t, dsn)
	t.Cleanup(func() {
		// Revert the role-level setting so the shared DB role is left as found.
		_ = installer.Exec(`ALTER ROLE CURRENT_USER RESET default_transaction_isolation`).Error
	})

	require.NoError(t, InstallPostgresDefaultIsolation(ctx, installer))

	// A brand-new connection opens fresh sessions that must inherit the role
	// default. (The installer connection's already-open sessions need not.)
	fresh := openPG(t, dsn)
	var level string
	require.NoError(t, fresh.Raw(`SELECT current_setting('default_transaction_isolation')`).Scan(&level).Error)
	require.Equal(t, "serializable", level,
		"a wrapper-less connection must inherit the role's serializable default")
}
