//go:build dev || test || integration

package api

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openExtractionJobIntegrationDB opens the real configured database for the
// emit-once test. It uses PostgreSQL when TEST_DB_* are set (so `make
// test-integration` exercises the genuine ON CONFLICT DO NOTHING RowsAffected
// path), otherwise falls back to in-memory SQLite. The Oracle godror MERGE
// row-count path is covered separately by the Oracle-gated suite — see #441.
func openExtractionJobIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	port := os.Getenv("TEST_DB_PORT")
	user := os.Getenv("TEST_DB_USER")
	password := os.Getenv("TEST_DB_PASSWORD")
	dbname := os.Getenv("TEST_DB_NAME")

	var db *gorm.DB
	var err error
	if host == "" || port == "" || user == "" || dbname == "" {
		t.Log("TEST_DB_* vars not set; falling back to SQLite")
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		require.NoError(t, err, "open SQLite fallback")
	} else {
		dsn := fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
			host, port, user, password, dbname,
		)
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		require.NoError(t, err, "open PostgreSQL integration DB")
	}

	require.NoError(t, db.AutoMigrate(&models.ExtractionJob{}))
	return db
}

// TestExtractionJobStore_MarkTerminal_EmitOnce_Integration verifies the #438
// emit-once contract against the real configured database (PostgreSQL under
// `make test-integration`). It pins that only the first terminal transition
// reports true; a redelivery of the same terminal result reports false, so the
// result-consumer emits the document.extraction_* webhook exactly once. This is
// the integration-level guard for the RowsAffected behavior the oracle-db-admin
// review flagged as load-bearing.
func TestExtractionJobStore_MarkTerminal_EmitOnce_Integration(t *testing.T) {
	db := openExtractionJobIntegrationDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()

	// `make test-integration` may run against a persistent dev database, and the
	// SQLite fallback is fresh each run, but the PostgreSQL path is not: leftover
	// rows from a prior run would collide on the job_id primary key (InsertQueued)
	// or leave a terminal row that makes the *first* MarkTerminal report false.
	// Hard-delete each job_id before and after its subtest (ExtractionJob has no
	// soft-delete column, so a plain Delete is a hard delete) so the test is
	// idempotently re-runnable without a DB reset. Mirrors the Oracle sibling.
	cleanup := func(jobID string) {
		require.NoError(t, db.Where("job_id = ?", jobID).Delete(&models.ExtractionJob{}).Error)
	}

	t.Run("queued row transitions once", func(t *testing.T) {
		const jobID = "it-emit-once-queued"
		cleanup(jobID)
		t.Cleanup(func() { cleanup(jobID) })

		require.NoError(t, store.InsertQueued(ctx, jobID, "doc-emit-once-1"))

		first, err := store.MarkTerminal(ctx, jobID, models.ExtractionStatusCompleted, "")
		require.NoError(t, err)
		assert.True(t, first, "first terminal transition must report true")

		second, err := store.MarkTerminal(ctx, jobID, models.ExtractionStatusCompleted, "")
		require.NoError(t, err)
		assert.False(t, second, "redelivery of an already-terminal row must report false")

		// The original document_ref survives the terminal flip.
		ref, err := store.GetDocumentRef(ctx, jobID)
		require.NoError(t, err)
		assert.Equal(t, "doc-emit-once-1", ref)
	})

	t.Run("bare insert transitions once", func(t *testing.T) {
		const jobID = "it-emit-once-bare"
		cleanup(jobID)
		t.Cleanup(func() { cleanup(jobID) })

		first, err := store.MarkTerminal(ctx, jobID, models.ExtractionStatusFailed, "extraction_limit:timeout")
		require.NoError(t, err)
		assert.True(t, first, "bare insert of a terminal row is a first transition")

		second, err := store.MarkTerminal(ctx, jobID, models.ExtractionStatusFailed, "extraction_limit:timeout")
		require.NoError(t, err)
		assert.False(t, second, "redelivery after a bare insert must report false")

		var count int64
		require.NoError(t, db.Model(&models.ExtractionJob{}).Where("job_id = ?", jobID).Count(&count).Error)
		assert.Equal(t, int64(1), count, "bare-insert redelivery must not create a duplicate row")
	})
}
