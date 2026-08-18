//go:build oracle

package dbschema_test

import (
	"os"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openOracleDB opens a real Oracle ADB connection from the same environment the
// other *_oracle_test.go files use (TMI_DATABASE_URL=oracle://…, ORACLE_PASSWORD,
// wallet from TMI_ORACLE_WALLET_LOCATION/TNS_ADMIN). Skips when unset.
//
// Run via `make test-integration-oci` (with scripts/oci-env.sh sourced and
// TMI_DATABASE_URL exported).
func openOracleDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbURL := os.Getenv("TMI_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TMI_DATABASE_URL not set; run under `make test-integration-oci` with scripts/oci-env.sh sourced")
	}

	cfg, err := authdb.ParseDatabaseURL(dbURL)
	require.NoError(t, err, "parse TMI_DATABASE_URL")
	require.Equal(t, authdb.DatabaseTypeOracle, cfg.Type,
		"this test requires an oracle:// TMI_DATABASE_URL (got %q)", cfg.Type)

	if cfg.OracleWalletLocation == "" {
		if w := os.Getenv("TMI_ORACLE_WALLET_LOCATION"); w != "" {
			cfg.OracleWalletLocation = w
		} else if w := os.Getenv("TNS_ADMIN"); w != "" {
			cfg.OracleWalletLocation = w
		}
	}

	gormDB, err := authdb.NewGormDB(*cfg)
	require.NoError(t, err, "open Oracle ADB connection")
	t.Cleanup(func() { _ = gormDB.Close() })
	return gormDB.DB()
}

// TestSchemaFingerprintOracleIntegration verifies the #480 schema-fingerprint
// machinery against real Oracle ADB — the cases the oracle-db-admin review
// flagged as Oracle-specific: that creating the stamp table is idempotent
// across calls (no fatal ORA-00955), that the unquoted/case-folded
// TMI_SCHEMA_VERSIONS identifiers resolve, and that the portable
// update-then-insert UPSERT and the read-back round-trip work on Oracle.
func TestSchemaFingerprintOracleIntegration(t *testing.T) {
	db := openOracleDB(t)

	// Start from a clean slate so the test is idempotent across runs. The table
	// may or may not exist yet; ignore a missing-table error (ORA-00942).
	_ = db.Exec("DELETE FROM TMI_SCHEMA_VERSIONS").Error

	fp := dbschema.ComputeModelsFingerprint(&models.User{}, &models.ThreatModel{})
	require.Len(t, fp, 64)

	// No row yet -> not current (safe fallback to full AutoMigrate). This also
	// exercises ensureSchemaVersionTable's CREATE TABLE path on Oracle.
	require.False(t, dbschema.SchemaFingerprintCurrent(db, fp),
		"no stamp row yet -> must report not current")
	// The stamp table must exist after the currency check. Oracle folds the
	// unquoted identifier to upper case, so query the catalog directly rather
	// than db.Migrator().HasTable (which matches the literal lower-case name).
	// ALL_TABLES scoped to CURRENT_SCHEMA rather than USER_TABLES, matching
	// the convention #736 established for the production probes: USER_TABLES
	// is scoped to the connected user's own objects and reports nothing on a
	// schema-owner-separated deployment.
	var tableExists int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM ALL_TABLES WHERE TABLE_NAME = 'TMI_SCHEMA_VERSIONS' "+
			"AND OWNER = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')").Scan(&tableExists).Error)
	require.Equal(t, int64(1), tableExists, "stamp table must exist after the currency check")

	// Record -> read-back must match (INSERT branch of the upsert).
	require.NoError(t, dbschema.RecordSchemaFingerprint(db, fp))
	require.True(t, dbschema.SchemaFingerprintCurrent(db, fp),
		"recorded fingerprint must read back as current on Oracle")

	// Re-record a different value (UPDATE branch of the upsert) and confirm the
	// single row is updated in place, not duplicated.
	require.NoError(t, dbschema.RecordSchemaFingerprint(db, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"))
	require.True(t, dbschema.SchemaFingerprintCurrent(db, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"))
	require.False(t, dbschema.SchemaFingerprintCurrent(db, fp), "old fingerprint must no longer be current")

	var count int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM TMI_SCHEMA_VERSIONS").Scan(&count).Error)
	require.Equal(t, int64(1), count, "upsert must keep exactly one stamp row on Oracle")

	// Idempotency: ensuring the table again (via another currency check) must
	// not raise a fatal ORA-00955 on the already-existing table.
	require.True(t, dbschema.SchemaFingerprintCurrent(db,
		"deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		"second currency check on an existing table must succeed (no fatal ORA-00955)")

	// Leaves the stamp table empty. Harmless but worth knowing now that
	// `make test-integration-oci` actually runs this package (#735): the next
	// server boot against this schema finds no fingerprint row and takes the
	// full AutoMigrate path instead of the #480 fast path, then re-stamps.
	t.Cleanup(func() { _ = db.Exec("DELETE FROM TMI_SCHEMA_VERSIONS").Error })
}
