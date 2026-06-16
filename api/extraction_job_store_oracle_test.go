//go:build oracle

package api

import (
	"context"
	"os"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openExtractionJobOracleDB opens a direct GORM connection to the Oracle ADB
// backend used by `make test-integration-oci`. It deliberately reuses the
// server's own connection setup (authdb.ParseDatabaseURL + authdb.NewGormDB) so
// the gorm-oracle (oracle-samples/gorm-oracle, godror) driver is configured
// exactly as in production — OracleNamingStrategy uppercasing plus
// SkipQuoteIdentifiers — which is what makes the MERGE INTO row-count behavior
// this test pins meaningful.
//
// It reads TMI_DATABASE_URL (oracle://…), ORACLE_PASSWORD (consumed inside
// ParseDatabaseURL), and the wallet directory from TMI_ORACLE_WALLET_LOCATION,
// falling back to TNS_ADMIN. scripts/oci-env.sh exports all of these. When
// TMI_DATABASE_URL is unset the test skips, so the (oracle-tagged) file is a
// no-op outside the OCI suite.
func openExtractionJobOracleDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbURL := os.Getenv("TMI_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TMI_DATABASE_URL not set; run under `make test-integration-oci` with scripts/oci-env.sh sourced")
	}

	cfg, err := authdb.ParseDatabaseURL(dbURL)
	require.NoError(t, err, "parse TMI_DATABASE_URL")
	require.Equal(t, authdb.DatabaseTypeOracle, cfg.Type,
		"this test requires an oracle:// TMI_DATABASE_URL (got %q)", cfg.Type)

	// The wallet location cannot be encoded in the URL; the server reads it from
	// TMI_ORACLE_WALLET_LOCATION. oci-env.sh also exports TNS_ADMIN to the same
	// wallet directory, so fall back to it.
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

	// extraction_jobs already exists on the shared ADB; gorm-oracle AutoMigrate
	// is not idempotent for it (ORA-01430 re-adding an existing column), so only
	// create it when genuinely absent.
	if !gormDB.DB().Migrator().HasTable(&models.ExtractionJob{}) {
		require.NoError(t, gormDB.AutoMigrate(&models.ExtractionJob{}))
	}
	return gormDB.DB()
}

// TestExtractionJobStore_MarkTerminal_EmitOnce_OracleIntegration is the
// Oracle-gated counterpart to the SQLite unit tests and the PostgreSQL/SQLite
// integration test (TestExtractionJobStore_MarkTerminal_EmitOnce_Integration).
// It closes the verification gap from #441: the #438 emit-once contract rests on
// MarkTerminal returning true on the first terminal transition and false on a
// redelivery, and on Oracle that false is derived from godror reporting
// RowsAffected==0 for a no-op MERGE. Until now that behavior was confirmed only
// by reading the gorm-oracle driver source; this test exercises it against a
// live Oracle ADB for both MarkTerminal paths.
//
// If the second MarkTerminal here returns true (godror reports a non-zero row
// count for the no-op MERGE), the emit-once guard is unsound on Oracle and the
// portable fallback from #441 applies: replace step 2's OnConflict-DoNothing
// insert in MarkTerminal with an explicit INSERT … SELECT … WHERE NOT EXISTS,
// whose RowsAffected is unambiguously 1/0 on every engine.
func TestExtractionJobStore_MarkTerminal_EmitOnce_OracleIntegration(t *testing.T) {
	db := openExtractionJobOracleDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()

	// Oracle ADB is a persistent, shared database; a previous run may have left
	// these job rows behind. A leftover terminal row would make the *first*
	// MarkTerminal report false and fail the contract, so hard-delete each job_id
	// before and after its subtest (ExtractionJob has no soft-delete column, so a
	// plain Delete is a hard delete). This keeps the subtests independent and the
	// whole test idempotently re-runnable.
	cleanup := func(jobID string) {
		require.NoError(t, db.Where("job_id = ?", jobID).Delete(&models.ExtractionJob{}).Error)
	}

	t.Run("queued row transitions once", func(t *testing.T) {
		const jobID = "oci-emit-once-queued"
		cleanup(jobID)
		t.Cleanup(func() { cleanup(jobID) })

		require.NoError(t, store.InsertQueued(ctx, jobID, "doc-oci-emit-1"))

		first, err := store.MarkTerminal(ctx, jobID, models.ExtractionStatusCompleted, "")
		require.NoError(t, err)
		assert.True(t, first, "first terminal transition of a queued row must report true")

		// Redelivery: the guarded UPDATE matches no non-terminal row, the
		// OnConflict-DoNothing insert MERGEs into the existing terminal row and
		// inserts nothing, so godror must report RowsAffected==0 ⇒ false. This is
		// the assertion previously verified only by source-reading the driver.
		second, err := store.MarkTerminal(ctx, jobID, models.ExtractionStatusCompleted, "")
		require.NoError(t, err)
		assert.False(t, second, "redelivery of an already-terminal queued row must report false")

		// The guarded UPDATE never touches document_ref, so the real ref survives
		// the terminal flip (it is never overwritten with the __unknown__ sentinel).
		ref, err := store.GetDocumentRef(ctx, jobID)
		require.NoError(t, err)
		assert.Equal(t, "doc-oci-emit-1", ref, "guarded UPDATE must preserve the real document_ref")
	})

	t.Run("bare insert transitions once", func(t *testing.T) {
		const jobID = "oci-emit-once-bare"
		cleanup(jobID)
		t.Cleanup(func() { cleanup(jobID) })

		// No prior queued row: step 1's guarded UPDATE matches nothing, so this
		// exercises step 2's OnConflict-DoNothing insert — a MERGE with no WHEN
		// MATCHED branch. First call inserts the bare terminal row:
		// RowsAffected==1 ⇒ true.
		first, err := store.MarkTerminal(ctx, jobID, models.ExtractionStatusFailed, "extraction_limit:timeout")
		require.NoError(t, err)
		assert.True(t, first, "bare insert of a terminal row is a first transition")

		// Redelivery: the MERGE matches the existing terminal row and inserts
		// nothing ⇒ godror RowsAffected==0 ⇒ false. A non-zero count here would
		// mean the no-op MERGE is miscounted on Oracle and the #441 portable
		// fallback is required.
		second, err := store.MarkTerminal(ctx, jobID, models.ExtractionStatusFailed, "extraction_limit:timeout")
		require.NoError(t, err)
		assert.False(t, second, "redelivery after a bare insert must report false")

		var count int64
		require.NoError(t, db.Model(&models.ExtractionJob{}).Where("job_id = ?", jobID).Count(&count).Error)
		assert.Equal(t, int64(1), count, "bare-insert redelivery must not create a duplicate row")
	})
}
