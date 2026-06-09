package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupExtractionJobTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ExtractionJob{}))
	return db
}

func TestExtractionJobStore_InsertQueued_Idempotent(t *testing.T) {
	db := setupExtractionJobTestDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()
	require.NoError(t, store.InsertQueued(ctx, "job-1", "doc-1"))
	require.NoError(t, store.InsertQueued(ctx, "job-1", "doc-1")) // no error on duplicate
	var count int64
	require.NoError(t, db.Model(&models.ExtractionJob{}).Where("job_id = ?", "job-1").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestExtractionJobStore_MarkTerminal_Upsert(t *testing.T) {
	db := setupExtractionJobTestDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()
	require.NoError(t, store.InsertQueued(ctx, "job-2", "doc-2"))
	transitioned, err := store.MarkTerminal(ctx, "job-2", models.ExtractionStatusFailed, "extraction_limit:timeout")
	require.NoError(t, err)
	assert.True(t, transitioned, "first terminal transition of a queued row must report true")
	var job models.ExtractionJob
	require.NoError(t, db.Where("job_id = ?", "job-2").First(&job).Error)
	assert.Equal(t, models.ExtractionStatusFailed, string(job.Status))
	// NullableDBVarchar exposes the value via the .String field
	assert.True(t, job.ReasonCode.Valid)
	assert.Equal(t, "extraction_limit:timeout", job.ReasonCode.String)
	require.NotNil(t, job.CompletedAt)
	// document_ref of the pre-existing queued row is preserved (never overwritten
	// with the sentinel by the guarded UPDATE path).
	assert.Equal(t, "doc-2", string(job.DocumentRef))
}

func TestExtractionJobStore_MarkTerminal_WithoutQueuedRow_Inserts(t *testing.T) {
	db := setupExtractionJobTestDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()
	transitioned, err := store.MarkTerminal(ctx, "job-3", models.ExtractionStatusCompleted, "")
	require.NoError(t, err)
	assert.True(t, transitioned, "bare-insert of a terminal row is a first transition")
	var job models.ExtractionJob
	require.NoError(t, db.Where("job_id = ?", "job-3").First(&job).Error)
	assert.Equal(t, models.ExtractionStatusCompleted, string(job.Status))
	// document_ref must be a non-empty sentinel, never "". On Oracle '' == NULL,
	// and document_ref is NOT NULL, so an empty value would raise ORA-01400 and
	// trap the result message in a redelivery loop. (oracle-db-admin blocking fix.)
	assert.Equal(t, unknownDocumentRef, string(job.DocumentRef))
	assert.NotEmpty(t, string(job.DocumentRef))
}

// TestExtractionJobStore_MarkTerminal_EmitOnce pins the #438 contract: only the
// first call that moves a row to terminal reports true; subsequent calls (a
// JetStream redelivery of the same result) report false so the consumer emits
// the webhook exactly once.
func TestExtractionJobStore_MarkTerminal_EmitOnce(t *testing.T) {
	db := setupExtractionJobTestDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()

	t.Run("queued row: first true, redelivery false", func(t *testing.T) {
		require.NoError(t, store.InsertQueued(ctx, "job-a", "doc-a"))

		first, err := store.MarkTerminal(ctx, "job-a", models.ExtractionStatusCompleted, "")
		require.NoError(t, err)
		assert.True(t, first)

		second, err := store.MarkTerminal(ctx, "job-a", models.ExtractionStatusCompleted, "")
		require.NoError(t, err)
		assert.False(t, second, "redelivery of an already-terminal row must report false")
	})

	t.Run("bare insert: first true, redelivery false", func(t *testing.T) {
		first, err := store.MarkTerminal(ctx, "job-b", models.ExtractionStatusFailed, "extraction_limit:timeout")
		require.NoError(t, err)
		assert.True(t, first)

		second, err := store.MarkTerminal(ctx, "job-b", models.ExtractionStatusFailed, "extraction_limit:timeout")
		require.NoError(t, err)
		assert.False(t, second)

		// Still exactly one row, still terminal.
		var count int64
		require.NoError(t, db.Model(&models.ExtractionJob{}).Where("job_id = ?", "job-b").Count(&count).Error)
		assert.Equal(t, int64(1), count)
	})
}
