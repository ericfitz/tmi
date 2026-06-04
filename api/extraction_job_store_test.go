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
	require.NoError(t, store.MarkTerminal(ctx, "job-2", models.ExtractionStatusFailed, "extraction_limit:timeout"))
	var job models.ExtractionJob
	require.NoError(t, db.Where("job_id = ?", "job-2").First(&job).Error)
	assert.Equal(t, models.ExtractionStatusFailed, string(job.Status))
	// NullableDBVarchar exposes the value via the .String field
	assert.True(t, job.ReasonCode.Valid)
	assert.Equal(t, "extraction_limit:timeout", job.ReasonCode.String)
	require.NotNil(t, job.CompletedAt)
}

func TestExtractionJobStore_MarkTerminal_WithoutQueuedRow_Inserts(t *testing.T) {
	db := setupExtractionJobTestDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()
	require.NoError(t, store.MarkTerminal(ctx, "job-3", models.ExtractionStatusCompleted, ""))
	var job models.ExtractionJob
	require.NoError(t, db.Where("job_id = ?", "job-3").First(&job).Error)
	assert.Equal(t, models.ExtractionStatusCompleted, string(job.Status))
}
