//go:build dev || test || integration

package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedSnapshot inserts a version snapshot with an explicit created_at. INSERT
// is never blocked by the append-only trigger (it guards UPDATE/DELETE only),
// so this works whether or not the trigger is installed, and autoCreateTime
// preserves a non-zero CreatedAt.
func seedSnapshot(t *testing.T, db *gorm.DB, objectType, objectID string, ageDays int) string {
	t.Helper()
	snap := models.VersionSnapshot{
		AuditEntryID: models.DBVarchar(uuid.New().String()),
		ObjectType:   models.DBVarchar(objectType),
		ObjectID:     models.DBVarchar(objectID),
		Version:      1,
		SnapshotType: models.DBVarchar(models.SnapshotTypeCheckpoint),
		CreatedAt:    time.Now().UTC().AddDate(0, 0, -ageDays),
	}
	require.NoError(t, db.Create(&snap).Error)
	return string(snap.ID)
}

func snapshotExists(t *testing.T, db *gorm.DB, id string) bool {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&models.VersionSnapshot{}).Where("id = ?", id).Count(&count).Error)
	return count > 0
}

// TestPruneOrphanedVersionSnapshotsIntegration proves the orphan sweep (#458):
// a snapshot whose referenced entity no longer exists (orphaned by the
// threat-model hard-delete cascade) is removed once it ages past the delete
// floor, while a young orphan and a snapshot of a live entity are both spared.
// The sweep runs THROUGH the installed append-only trigger, proving the aged
// delete is permitted by the floor.
func TestPruneOrphanedVersionSnapshotsIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	ctx := context.Background()

	// Remove any pre-existing trigger so seeding and cleanup are unencumbered.
	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.ThreatModel{}, &models.VersionSnapshot{}))

	// A live threat model whose snapshot must be spared (entity still exists).
	owner := models.User{
		InternalUUID: models.DBVarchar(uuid.New().String()),
		Provider:     "tmi",
		Email:        models.DBVarchar(fmt.Sprintf("orphan-sweep-%s@tmi.local", uuid.New().String()[:8])),
		Name:         "Orphan Sweep Owner",
	}
	require.NoError(t, db.Create(&owner).Error)
	liveTM := models.ThreatModel{
		ID:                    models.DBVarchar(uuid.New().String()),
		OwnerInternalUUID:     owner.InternalUUID,
		Name:                  "orphan-sweep-live-tm",
		CreatedByInternalUUID: owner.InternalUUID,
		// Explicit unique alias avoids the store-side allocator (not a model hook).
		Alias: 2_000_000_000 + int32(time.Now().UnixNano()%1_000_000),
	}
	require.NoError(t, db.Create(&liveTM).Error)

	orphanOldID := seedSnapshot(t, db, models.ObjectTypeThreatModel, uuid.New().String(), 60)  // orphan, past floor+cutoff
	orphanYoungID := seedSnapshot(t, db, models.ObjectTypeThreatModel, uuid.New().String(), 3) // orphan, younger than cutoff
	liveOldID := seedSnapshot(t, db, models.ObjectTypeThreatModel, string(liveTM.ID), 60)      // live entity, must be spared

	// Install with a 7-day snapshot floor so the 60-day-old orphan delete is
	// permitted by the trigger.
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:     90,
		VersionRetentionDays:   8,
		TombstoneRetentionDays: 8,
	}))
	t.Cleanup(func() {
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error
		_ = db.Exec("DELETE FROM version_snapshots WHERE id IN ?", []string{orphanOldID, orphanYoungID, liveOldID}).Error
		_ = db.Exec("DELETE FROM threat_models WHERE id = ?", string(liveTM.ID)).Error
		_ = db.Exec("DELETE FROM users WHERE internal_uuid = ?", string(owner.InternalUUID)).Error
	})

	// Service cutoff = now - 30d: orphanOld (60d) is past it; orphanYoung (3d) is not.
	t.Setenv("VERSION_RETENTION_DAYS", "30")
	svc := NewGormAuditService(db)

	pruned, err := svc.PruneOrphanedVersionSnapshots(ctx)
	require.NoError(t, err, "orphan sweep must succeed through the age-floored trigger")
	assert.GreaterOrEqual(t, pruned, 1, "the aged orphan should have been swept")

	assert.False(t, snapshotExists(t, db, orphanOldID), "aged orphan snapshot should be deleted")
	assert.True(t, snapshotExists(t, db, orphanYoungID), "young orphan should be spared (age floor)")
	assert.True(t, snapshotExists(t, db, liveOldID), "snapshot of a live entity must never be swept")
}
