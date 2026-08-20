package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newMetadataBulkTestRepo creates an in-memory SQLite-backed GormMetadataRepository
// for testing the batched bulk write paths (#666) in isolation. Metadata has no
// FK to other tables, so no fixture rows beyond the entity_type/entity_id pair
// under test are needed.
func newMetadataBulkTestRepo(t *testing.T) (*GormMetadataRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Metadata{}))
	return NewGormMetadataRepository(db, nil, nil), db
}

// TestGormMetadataRepository_BulkCreate_ConflictNamesExistingKey guards the
// batched BulkCreate's pre-insert existing-keys probe: when one key among
// several requested entries already exists for the entity, the call must be
// rejected with a MetadataConflictError naming that key rather than any
// generic duplicate error, and must not create the non-conflicting entries.
func TestGormMetadataRepository_BulkCreate_ConflictNamesExistingKey(t *testing.T) {
	repo, db := newMetadataBulkTestRepo(t)
	ctx := context.Background()
	entityType := "threat_model"
	entityID := uuid.New().String()

	now := time.Now().UTC()
	require.NoError(t, db.Create(&models.Metadata{
		ID:         models.DBVarchar(uuid.New().String()),
		EntityType: models.DBVarchar(entityType),
		EntityID:   models.DBVarchar(entityID),
		Key:        models.DBVarchar("color"),
		Value:      models.DBVarchar("red"),
		CreatedAt:  now,
		ModifiedAt: now,
	}).Error)

	err := repo.BulkCreate(ctx, entityType, entityID, []Metadata{
		{Key: "color", Value: "blue"},
		{Key: "size", Value: "large"},
		{Key: "shape", Value: "round"},
	})

	require.Error(t, err)
	var conflictErr *MetadataConflictError
	require.True(t, errors.As(err, &conflictErr), "expected *MetadataConflictError, got %T: %v", err, err)
	assert.Equal(t, []string{"color"}, conflictErr.ConflictingKeys)

	// None of the requested entries should have been created, including the
	// non-conflicting ones -- BulkCreate is all-or-nothing.
	var count int64
	require.NoError(t, db.Model(&models.Metadata{}).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Count(&count).Error)
	assert.Equal(t, int64(1), count, "only the originally seeded row should remain")
}

// TestGormMetadataRepository_BulkCreate_RaceNamesKeyViaProbe guards the new
// post-batch-insert conflict path added when BulkCreate moved from per-row
// tx.Create to a single tx.CreateInBatches call: a duplicate-key error
// surfacing from the batch insert itself (e.g. a concurrent writer winning
// the race between the pre-insert probe and the insert) must still be
// resolved to the actual conflicting key via a second probe, not reported as
// a bare/unnamed conflict. Simulated with a GORM callback injection, the same
// technique used in TestUpsertGroup_ToleratesDuplicateRace (group_repository_upsert_test.go)
// since SQLite's own constraint checking can't itself reproduce a genuine
// concurrent race.
func TestGormMetadataRepository_BulkCreate_RaceNamesKeyViaProbe(t *testing.T) {
	repo, db := newMetadataBulkTestRepo(t)
	ctx := context.Background()
	entityType := "threat_model"
	entityID := uuid.New().String()

	// The "concurrent" winner: inserted with raw SQL (bypassing GORM's create
	// callbacks, to avoid recursing into this same hook) inside the batch
	// insert's own transaction, right before that insert fails. This puts it
	// exactly where a real concurrent winner would be: invisible to
	// BulkCreate's pre-insert probe (which already ran and found nothing) but
	// visible to the post-failure race-recovery probe that runs afterward in
	// the same transaction.
	winnerNow := time.Now().UTC()
	winner := models.Metadata{
		ID:         models.DBVarchar(uuid.New().String()),
		EntityType: models.DBVarchar(entityType),
		EntityID:   models.DBVarchar(entityID),
		Key:        models.DBVarchar("owner"),
		Value:      models.DBVarchar("alice"),
		CreatedAt:  winnerNow,
		ModifiedAt: winnerNow,
	}

	injected := false
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:inject_bulkcreate_duplicate", func(tx *gorm.DB) {
		if injected {
			return
		}
		if _, ok := tx.Statement.Dest.(*[]models.Metadata); !ok {
			return
		}
		injected = true
		require.NoError(t, tx.Exec(
			"INSERT INTO metadata (id, entity_type, entity_id, key, value, created_at, modified_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			winner.ID, winner.EntityType, winner.EntityID, winner.Key, winner.Value, winner.CreatedAt, winner.ModifiedAt,
		).Error)
		tx.Error = errors.New("UNIQUE constraint failed: metadata.entity_type, metadata.entity_id, metadata.key")
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:inject_bulkcreate_duplicate") })

	err := repo.BulkCreate(ctx, entityType, entityID, []Metadata{
		{Key: "owner", Value: "bob"},
		{Key: "priority", Value: "high"},
	})

	require.Error(t, err)
	require.True(t, injected, "the synthetic duplicate error must actually have fired")
	var conflictErr *MetadataConflictError
	require.True(t, errors.As(err, &conflictErr), "expected *MetadataConflictError, got %T: %v", err, err)
	assert.Equal(t, []string{"owner"}, conflictErr.ConflictingKeys, "the race-recovery probe must name the actual conflicting key")
}

// TestGormMetadataRepository_BulkReplace_RoundTripsAtCap proves the batched
// BulkReplace path (tx.CreateInBatches, batch size 100) works correctly at
// the spec-documented cap: all 100 new entries land, and everything that
// existed before the replace is gone.
func TestGormMetadataRepository_BulkReplace_RoundTripsAtCap(t *testing.T) {
	repo, db := newMetadataBulkTestRepo(t)
	ctx := context.Background()
	entityType := "diagram"
	entityID := uuid.New().String()

	// Seed pre-existing metadata that BulkReplace must remove.
	require.NoError(t, repo.BulkCreate(ctx, entityType, entityID, []Metadata{
		{Key: "stale-1", Value: "old"},
		{Key: "stale-2", Value: "old"},
	}))

	const capSize = 100
	replacement := make([]Metadata, capSize)
	for i := 0; i < capSize; i++ {
		replacement[i] = Metadata{
			Key:   uuid.New().String(),
			Value: "v",
		}
	}

	require.NoError(t, repo.BulkReplace(ctx, entityType, entityID, replacement))

	var stored []models.Metadata
	require.NoError(t, db.Where("entity_type = ? AND entity_id = ?", entityType, entityID).Find(&stored).Error)
	require.Len(t, stored, capSize, "all %d replacement entries must be present", capSize)

	storedKeys := make(map[string]bool, len(stored))
	for _, m := range stored {
		storedKeys[string(m.Key)] = true
	}
	for _, meta := range replacement {
		assert.True(t, storedKeys[meta.Key], "replacement key %q must be present after replace", meta.Key)
	}
	assert.False(t, storedKeys["stale-1"], "pre-existing key must be gone after replace")
	assert.False(t, storedKeys["stale-2"], "pre-existing key must be gone after replace")
}

// TestGormMetadataRepository_BulkUpdate_UpsertsNewAndExistingMix guards the
// batched BulkUpdate's ON CONFLICT DoUpdates clause applied across
// tx.CreateInBatches: a single call mixing brand-new keys with keys that
// already exist must create the new ones, update the value/modified_at of the
// existing ones, and leave created_at untouched on the updated rows.
func TestGormMetadataRepository_BulkUpdate_UpsertsNewAndExistingMix(t *testing.T) {
	repo, db := newMetadataBulkTestRepo(t)
	ctx := context.Background()
	entityType := "asset"
	entityID := uuid.New().String()

	originalCreatedAt := time.Now().UTC().Add(-24 * time.Hour)
	require.NoError(t, db.Create(&models.Metadata{
		ID:         models.DBVarchar(uuid.New().String()),
		EntityType: models.DBVarchar(entityType),
		EntityID:   models.DBVarchar(entityID),
		Key:        models.DBVarchar("status"),
		Value:      models.DBVarchar("draft"),
		CreatedAt:  originalCreatedAt,
		ModifiedAt: originalCreatedAt,
	}).Error)

	require.NoError(t, repo.BulkUpdate(ctx, entityType, entityID, []Metadata{
		{Key: "status", Value: "published"}, // existing: update
		{Key: "reviewer", Value: "carol"},   // new: create
	}))

	var updated models.Metadata
	require.NoError(t, db.Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, "status").First(&updated).Error)
	assert.Equal(t, "published", string(updated.Value))
	assert.True(t, updated.CreatedAt.Equal(originalCreatedAt), "created_at must be preserved on an upserted-existing row")
	assert.True(t, updated.ModifiedAt.After(originalCreatedAt), "modified_at must advance on an upserted-existing row")

	var created models.Metadata
	require.NoError(t, db.Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, "reviewer").First(&created).Error)
	assert.Equal(t, "carol", string(created.Value))
}
