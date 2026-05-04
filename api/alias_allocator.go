package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AllocateNextAlias atomically reserves the next alias value for the given
// (parentID, objectType) scope. MUST be called inside a transaction; the
// caller's transaction holds the lock until commit. The returned value is
// guaranteed unique within the scope so long as the calling transaction
// commits successfully.
//
// For the ThreatModel global counter, pass parentID="__global__" and
// objectType="threat_model". For sub-objects, parentID is the parent
// ThreatModel UUID and objectType is one of "diagram", "threat", "asset",
// "repository", "note", "document".
//
// Note: if the calling transaction rolls back, the counter UPDATE rolls back
// too — the alias is "released" and reused by the next caller. High-water-mark
// semantics apply only to committed inserts.
func AllocateNextAlias(ctx context.Context, tx *gorm.DB, parentID, objectType string) (int32, error) {
	logger := slogging.Get()

	// Insert counter row if missing. ON CONFLICT DO NOTHING is idempotent.
	row := models.AliasCounter{ParentID: parentID, ObjectType: objectType, NextAlias: 1}
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		logger.Error("alias_counters upsert failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters upsert: %w", err)
	}

	// Lock the row and read the current value.
	var counter models.AliasCounter
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("parent_id = ? AND object_type = ?", parentID, objectType).
		First(&counter).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Should be impossible after the upsert above.
		return 0, fmt.Errorf("alias_counters row missing after upsert: parent=%s type=%s", parentID, objectType)
	}
	if err != nil {
		logger.Error("alias_counters lock failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters lock: %w", err)
	}

	allocated := counter.NextAlias

	// Bump the counter atomically (still inside the same transaction & lock).
	if err := tx.WithContext(ctx).
		Model(&models.AliasCounter{}).
		Where("parent_id = ? AND object_type = ?", parentID, objectType).
		Update("next_alias", counter.NextAlias+1).Error; err != nil {
		logger.Error("alias_counters bump failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters bump: %w", err)
	}

	return allocated, nil
}
