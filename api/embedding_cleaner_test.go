package api

import (
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupCleanerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.ThreatModel{},
		&models.TimmyEmbedding{},
	))
	return db
}

func createTestTM(t *testing.T, db *gorm.DB, id string, status string, lastAccessed *time.Time, modifiedAt time.Time) {
	t.Helper()
	if status == "" {
		status = "not_started"
	}
	tm := models.ThreatModel{
		ID:                    id,
		OwnerInternalUUID:     "owner-uuid",
		Name:                  "Test TM " + id,
		CreatedByInternalUUID: "creator-uuid",
		Status:                status,
		StatusUpdated:         modifiedAt,
		LastAccessedAt:        lastAccessed,
	}
	require.NoError(t, db.Create(&tm).Error)
	// Override ModifiedAt (GORM autoUpdateTime sets it on create)
	require.NoError(t, db.Model(&models.ThreatModel{}).Where("id = ?", id).
		Update("modified_at", modifiedAt).Error)
}

func createTestEmbedding(t *testing.T, db *gorm.DB, tmID string, entityID string) {
	t.Helper()
	emb := models.TimmyEmbedding{
		ID:             "emb-" + tmID + "-" + entityID,
		ThreatModelID:  tmID,
		EntityType:     "document",
		EntityID:       entityID,
		ChunkIndex:     0,
		IndexType:      "text",
		ContentHash:    "hash-" + entityID,
		EmbeddingModel: "test-model",
		EmbeddingDim:   384,
		ChunkText:      "test chunk text",
	}
	require.NoError(t, db.Create(&emb).Error)
}

func countEmbeddings(t *testing.T, db *gorm.DB, tmID string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&models.TimmyEmbedding{}).
		Where("threat_model_id = ?", tmID).Count(&count).Error)
	return count
}

func TestEmbeddingCleaner_IdleActiveTMGetsCleaned(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	thirtyOneDaysAgo := time.Now().Add(-31 * 24 * time.Hour)
	createTestTM(t, db, "tm-idle-active", "", &thirtyOneDaysAgo, thirtyOneDaysAgo)
	createTestEmbedding(t, db, "tm-idle-active", "doc-1")
	createTestEmbedding(t, db, "tm-idle-active", "doc-2")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(2), deleted)
	assert.Equal(t, int64(0), countEmbeddings(t, db, "tm-idle-active"))
}

func TestEmbeddingCleaner_IdleClosedTMGetsCleanedSooner(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	eightDaysAgo := time.Now().Add(-8 * 24 * time.Hour)
	createTestTM(t, db, "tm-idle-closed", "closed", &eightDaysAgo, eightDaysAgo)
	createTestEmbedding(t, db, "tm-idle-closed", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(1), deleted)
	assert.Equal(t, int64(0), countEmbeddings(t, db, "tm-idle-closed"))
}

func TestEmbeddingCleaner_RecentlyAccessedTMPreserved(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	oneDayAgo := time.Now().Add(-1 * 24 * time.Hour)
	createTestTM(t, db, "tm-recent", "", &oneDayAgo, oneDayAgo)
	createTestEmbedding(t, db, "tm-recent", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(0), deleted)
	assert.Equal(t, int64(1), countEmbeddings(t, db, "tm-recent"))
}

func TestEmbeddingCleaner_TMWithNoEmbeddingsSkipped(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	thirtyOneDaysAgo := time.Now().Add(-31 * 24 * time.Hour)
	createTestTM(t, db, "tm-no-embeddings", "", &thirtyOneDaysAgo, thirtyOneDaysAgo)

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(0), deleted)
}

func TestEmbeddingCleaner_FallbackToModifiedAt_Old(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	thirtyFiveDaysAgo := time.Now().Add(-35 * 24 * time.Hour)
	// last_accessed_at is nil, should fall back to modified_at
	createTestTM(t, db, "tm-fallback-old", "", nil, thirtyFiveDaysAgo)
	createTestEmbedding(t, db, "tm-fallback-old", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(1), deleted)
	assert.Equal(t, int64(0), countEmbeddings(t, db, "tm-fallback-old"))
}

func TestEmbeddingCleaner_FallbackToModifiedAt_Recent(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	twoDaysAgo := time.Now().Add(-2 * 24 * time.Hour)
	// last_accessed_at is nil, modified_at is recent
	createTestTM(t, db, "tm-fallback-recent", "", nil, twoDaysAgo)
	createTestEmbedding(t, db, "tm-fallback-recent", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(0), deleted)
	assert.Equal(t, int64(1), countEmbeddings(t, db, "tm-fallback-recent"))
}

func TestEmbeddingCleaner_ClosedTMNotIdleLongEnough(t *testing.T) {
	db := setupCleanerTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)

	threeDaysAgo := time.Now().Add(-3 * 24 * time.Hour)
	createTestTM(t, db, "tm-closed-recent", "closed", &threeDaysAgo, threeDaysAgo)
	createTestEmbedding(t, db, "tm-closed-recent", "doc-1")

	cleaner := NewEmbeddingCleaner(store, db, time.Hour, 30, 7)
	deleted := cleaner.CleanOnce()

	assert.Equal(t, int64(0), deleted)
	assert.Equal(t, int64(1), countEmbeddings(t, db, "tm-closed-recent"))
}
