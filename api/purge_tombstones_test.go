package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupPurgeTestDB creates an in-memory SQLite database with the tables needed for PurgeTombstones.
func setupPurgeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.ThreatModel{},
		&models.Diagram{},
		&models.Threat{},
		&models.Asset{},
		&models.Document{},
		&models.Note{},
		&models.Repository{},
		&models.Metadata{},
		&models.AuditEntry{},
		&models.VersionSnapshot{},
	))
	return db
}

func TestPurgeTombstones_CleansUpVersionSnapshotsForOrphanedSubResources(t *testing.T) {
	db := setupPurgeTestDB(t)

	// Create a non-deleted threat model (parent is active)
	tmID := uuid.New().String()
	require.NoError(t, db.Create(&models.ThreatModel{
		ID:   tmID,
		Name: "Active TM",
	}).Error)

	// Create a soft-deleted diagram (orphaned: parent is active but diagram is deleted)
	diagramID := uuid.New().String()
	expiredTime := time.Now().UTC().Add(-60 * 24 * time.Hour) // 60 days ago, well past 30-day retention
	require.NoError(t, db.Create(&models.Diagram{
		ID:            diagramID,
		ThreatModelID: tmID,
		Name:          "Deleted Diagram",
		DeletedAt:     &expiredTime,
	}).Error)

	// Create metadata for the diagram
	require.NoError(t, db.Create(&models.Metadata{
		ID:         uuid.New().String(),
		EntityType: "diagram",
		EntityID:   diagramID,
		Key:        "test-key",
		Value:      "test-value",
	}).Error)

	// Create an audit entry for the diagram
	auditID := uuid.New().String()
	require.NoError(t, db.Create(&models.AuditEntry{
		ID:               auditID,
		ThreatModelID:    tmID,
		ObjectType:       "diagram",
		ObjectID:         diagramID,
		ChangeType:       "created",
		ActorEmail:       "alice@tmi.local",
		ActorProvider:    "tmi",
		ActorProviderID:  "alice",
		ActorDisplayName: "Alice",
	}).Error)

	// Create a version snapshot for the diagram
	snapshotID := uuid.New().String()
	require.NoError(t, db.Create(&models.VersionSnapshot{
		ID:           snapshotID,
		AuditEntryID: auditID,
		ObjectType:   "diagram",
		ObjectID:     diagramID,
		Version:      1,
		SnapshotType: "checkpoint",
	}).Error)

	// Run PurgeTombstones with 0-day retention so everything expired qualifies
	svc := &GormAuditService{
		db:                     db,
		tombstoneRetentionDays: 30,
	}
	purged, err := svc.PurgeTombstones(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, purged, "should purge 1 orphaned diagram")

	// Verify diagram is deleted
	var diagramCount int64
	db.Model(&models.Diagram{}).Where("id = ?", diagramID).Count(&diagramCount)
	assert.Equal(t, int64(0), diagramCount, "diagram should be purged")

	// Verify metadata is deleted
	var metaCount int64
	db.Model(&models.Metadata{}).Where("entity_type = 'diagram' AND entity_id = ?", diagramID).Count(&metaCount)
	assert.Equal(t, int64(0), metaCount, "metadata should be cleaned up")

	// Verify version snapshot is deleted
	var snapshotCount int64
	db.Model(&models.VersionSnapshot{}).Where("object_type = 'diagram' AND object_id = ?", diagramID).Count(&snapshotCount)
	assert.Equal(t, int64(0), snapshotCount, "version snapshots should be cleaned up")

	// Verify audit entry is PRESERVED (append-only, never deleted)
	var auditCount int64
	db.Model(&models.AuditEntry{}).Where("object_type = 'diagram' AND object_id = ?", diagramID).Count(&auditCount)
	assert.Equal(t, int64(1), auditCount, "audit entries must be preserved (append-only)")
}

func TestPurgeTombstones_PreservesNonExpiredTombstones(t *testing.T) {
	db := setupPurgeTestDB(t)

	tmID := uuid.New().String()
	require.NoError(t, db.Create(&models.ThreatModel{
		ID:   tmID,
		Name: "Active TM",
	}).Error)

	// Create a recently soft-deleted note (within retention period)
	noteID := uuid.New().String()
	recentDelete := time.Now().UTC().Add(-5 * 24 * time.Hour) // 5 days ago, within 30-day retention
	require.NoError(t, db.Create(&models.Note{
		ID:            noteID,
		ThreatModelID: tmID,
		Name:          "Test Note",
		Content:       models.DBText("test content"),
		DeletedAt:     &recentDelete,
	}).Error)

	svc := &GormAuditService{
		db:                     db,
		tombstoneRetentionDays: 30,
	}
	purged, err := svc.PurgeTombstones(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, purged, "should not purge recently deleted entities")

	var noteCount int64
	db.Model(&models.Note{}).Where("id = ?", noteID).Count(&noteCount)
	assert.Equal(t, int64(1), noteCount, "note should still exist")
}

func TestPurgeTombstones_MultipleSubResourceTypes(t *testing.T) {
	db := setupPurgeTestDB(t)

	tmID := uuid.New().String()
	require.NoError(t, db.Create(&models.ThreatModel{
		ID:   tmID,
		Name: "Active TM",
	}).Error)

	expiredTime := time.Now().UTC().Add(-60 * 24 * time.Hour)

	// Create expired tombstones for multiple sub-resource types
	threatID := uuid.New().String()
	now := time.Now().UTC()
	require.NoError(t, db.Create(&models.Threat{
		ID:            threatID,
		ThreatModelID: tmID,
		Name:          "Test Threat",
		ThreatType:    models.StringArray{"spoofing"},
		CreatedAt:     now,
		ModifiedAt:    now,
		DeletedAt:     &expiredTime,
	}).Error)

	assetID := uuid.New().String()
	require.NoError(t, db.Create(&models.Asset{
		ID:            assetID,
		ThreatModelID: tmID,
		Name:          "Test Asset",
		Type:          "data",
		DeletedAt:     &expiredTime,
	}).Error)

	// Create version snapshots for both
	for _, pair := range []struct{ objType, objID string }{
		{"threat", threatID},
		{"asset", assetID},
	} {
		auditID := uuid.New().String()
		require.NoError(t, db.Create(&models.AuditEntry{
			ID:               auditID,
			ThreatModelID:    tmID,
			ObjectType:       pair.objType,
			ObjectID:         pair.objID,
			ChangeType:       "created",
			ActorEmail:       "alice@tmi.local",
			ActorProvider:    "tmi",
			ActorProviderID:  "alice",
			ActorDisplayName: "Alice",
		}).Error)
		require.NoError(t, db.Create(&models.VersionSnapshot{
			ID:           uuid.New().String(),
			AuditEntryID: auditID,
			ObjectType:   pair.objType,
			ObjectID:     pair.objID,
			Version:      1,
			SnapshotType: "checkpoint",
		}).Error)
	}

	svc := &GormAuditService{
		db:                     db,
		tombstoneRetentionDays: 30,
	}
	purged, err := svc.PurgeTombstones(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, purged, "should purge both expired sub-resources")

	// Verify version snapshots are cleaned up
	var snapshotCount int64
	db.Model(&models.VersionSnapshot{}).Count(&snapshotCount)
	assert.Equal(t, int64(0), snapshotCount, "all version snapshots should be cleaned up")

	// Verify audit entries are preserved
	var auditCount int64
	db.Model(&models.AuditEntry{}).Count(&auditCount)
	assert.Equal(t, int64(2), auditCount, "all audit entries must be preserved")
}
