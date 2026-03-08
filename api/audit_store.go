package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// Default retention configuration
const (
	defaultAuditRetentionDays     = 365
	defaultVersionRetentionCount  = 50
	defaultVersionRetentionDays   = 90
	defaultTombstoneRetentionDays = 30
)

// GormAuditService implements AuditServiceInterface using GORM.
type GormAuditService struct {
	db                     *gorm.DB
	auditRetentionDays     int
	versionRetentionCount  int
	versionRetentionDays   int
	tombstoneRetentionDays int
}

// NewGormAuditService creates a new GormAuditService with configuration from environment.
func NewGormAuditService(db *gorm.DB) *GormAuditService {
	return &GormAuditService{
		db:                     db,
		auditRetentionDays:     getEnvInt("AUDIT_RETENTION_DAYS", defaultAuditRetentionDays),
		versionRetentionCount:  getEnvInt("VERSION_RETENTION_COUNT", defaultVersionRetentionCount),
		versionRetentionDays:   getEnvInt("VERSION_RETENTION_DAYS", defaultVersionRetentionDays),
		tombstoneRetentionDays: getEnvInt("TOMBSTONE_RETENTION_DAYS", defaultTombstoneRetentionDays),
	}
}

// getEnvInt reads an integer from an environment variable with a default fallback.
func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		slogging.Get().Warn("invalid value for %s=%q, using default %d", key, val, defaultVal)
		return defaultVal
	}
	return n
}

// RecordMutation records a mutation in the audit trail and creates a version snapshot.
func (s *GormAuditService) RecordMutation(ctx context.Context, params AuditParams) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Assign next version number for this object
		var maxVersion *int
		err := tx.Model(&models.AuditEntry{}).
			Where("object_type = ? AND object_id = ?", params.ObjectType, params.ObjectID).
			Select("MAX(version)").
			Scan(&maxVersion).Error
		if err != nil {
			return fmt.Errorf("failed to get max version: %w", err)
		}

		nextVersion := 1
		if maxVersion != nil {
			nextVersion = *maxVersion + 1
		}

		// Create audit entry
		entry := models.AuditEntry{
			ThreatModelID:    params.ThreatModelID,
			ObjectType:       params.ObjectType,
			ObjectID:         params.ObjectID,
			Version:          &nextVersion,
			ChangeType:       params.ChangeType,
			ActorEmail:       params.Actor.Email,
			ActorProvider:    params.Actor.Provider,
			ActorProviderID:  params.Actor.ProviderID,
			ActorDisplayName: params.Actor.DisplayName,
			ChangeSummary:    models.NewNullableDBText(params.ChangeSummary),
		}

		if err := tx.Create(&entry).Error; err != nil {
			return fmt.Errorf("failed to create audit entry: %w", err)
		}

		// Create version snapshot
		if err := s.createVersionSnapshot(tx, entry, params, nextVersion); err != nil {
			return fmt.Errorf("failed to create version snapshot: %w", err)
		}

		return nil
	})
}

// createVersionSnapshot creates the appropriate version snapshot (checkpoint or diff).
func (s *GormAuditService) createVersionSnapshot(tx *gorm.DB, entry models.AuditEntry, params AuditParams, version int) error {
	snapshot := models.VersionSnapshot{
		AuditEntryID: entry.ID,
		ObjectType:   params.ObjectType,
		ObjectID:     params.ObjectID,
		Version:      version,
	}

	switch params.ChangeType {
	case models.ChangeTypeCreated:
		// For creates, store the initial state as a checkpoint
		if params.CurrentState != nil {
			snapshot.SnapshotType = models.SnapshotTypeCheckpoint
			snapshot.Data = models.NullableDBText{String: string(params.CurrentState), Valid: true}
		} else {
			return nil // no state to snapshot
		}

	case models.ChangeTypeDeleted:
		// For deletes, store the previous state as a checkpoint (needed for undelete)
		if params.PreviousState != nil {
			snapshot.SnapshotType = models.SnapshotTypeCheckpoint
			snapshot.Data = models.NullableDBText{String: string(params.PreviousState), Valid: true}
		} else {
			return nil
		}

	default:
		// For updates/patches/rollbacks: store diff or checkpoint
		if params.PreviousState == nil || params.CurrentState == nil {
			return nil
		}

		isCheckpoint := version%models.CheckpointInterval == 0 || version == 1

		if isCheckpoint {
			// Store full snapshot of the state BEFORE this mutation
			snapshot.SnapshotType = models.SnapshotTypeCheckpoint
			snapshot.Data = models.NullableDBText{String: string(params.PreviousState), Valid: true}
		} else {
			// Store reverse diff: patch that transforms current state back to previous state
			reverseDiff, err := ComputeReverseDiff(params.PreviousState, params.CurrentState)
			if err != nil {
				// Fall back to checkpoint if diff computation fails
				slogging.Get().Warn("failed to compute reverse diff, storing checkpoint: %v", err)
				snapshot.SnapshotType = models.SnapshotTypeCheckpoint
				snapshot.Data = models.NullableDBText{String: string(params.PreviousState), Valid: true}
			} else {
				snapshot.SnapshotType = models.SnapshotTypeDiff
				snapshot.Data = models.NullableDBText{String: string(reverseDiff), Valid: true}
			}
		}
	}

	return tx.Create(&snapshot).Error
}

// GetThreatModelAuditTrail retrieves all audit entries for a threat model.
func (s *GormAuditService) GetThreatModelAuditTrail(ctx context.Context, threatModelID string, offset, limit int, filters *AuditFilters) ([]AuditEntryResponse, int, error) {
	query := s.db.WithContext(ctx).Model(&models.AuditEntry{}).Where("threat_model_id = ?", threatModelID)
	query = applyAuditFilters(query, filters)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count audit entries: %w", err)
	}

	var entries []models.AuditEntry
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&entries).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to list audit entries: %w", err)
	}

	return toAuditEntryResponses(entries), int(total), nil
}

// GetObjectAuditTrail retrieves audit entries for a specific object.
func (s *GormAuditService) GetObjectAuditTrail(ctx context.Context, objectType, objectID string, offset, limit int) ([]AuditEntryResponse, int, error) {
	query := s.db.WithContext(ctx).Model(&models.AuditEntry{}).
		Where("object_type = ? AND object_id = ?", objectType, objectID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count audit entries: %w", err)
	}

	var entries []models.AuditEntry
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&entries).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to list audit entries: %w", err)
	}

	return toAuditEntryResponses(entries), int(total), nil
}

// GetAuditEntry retrieves a single audit entry by ID.
func (s *GormAuditService) GetAuditEntry(ctx context.Context, entryID string) (*AuditEntryResponse, error) {
	var entry models.AuditEntry
	if err := s.db.WithContext(ctx).Where("id = ?", entryID).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get audit entry: %w", err)
	}
	resp := toAuditEntryResponse(entry)
	return &resp, nil
}

// GetSnapshot reconstructs the full entity state at a given audit entry's version.
// It finds the nearest checkpoint and applies reverse diffs to reconstruct the state.
func (s *GormAuditService) GetSnapshot(ctx context.Context, entryID string) ([]byte, error) {
	// Get the audit entry to find object info and version
	var entry models.AuditEntry
	if err := s.db.WithContext(ctx).Where("id = ?", entryID).First(&entry).Error; err != nil {
		return nil, fmt.Errorf("failed to get audit entry: %w", err)
	}

	if entry.Version == nil {
		return nil, fmt.Errorf("version snapshot has been pruned")
	}

	targetVersion := *entry.Version

	// Get the version snapshot for this entry
	var targetSnapshot models.VersionSnapshot
	if err := s.db.WithContext(ctx).
		Where("audit_entry_id = ?", entryID).
		First(&targetSnapshot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("version snapshot has been pruned")
		}
		return nil, fmt.Errorf("failed to get version snapshot: %w", err)
	}

	// If it's a checkpoint, return directly
	if targetSnapshot.SnapshotType == models.SnapshotTypeCheckpoint {
		return []byte(targetSnapshot.Data.String), nil
	}

	// For diffs, we need to reconstruct from the nearest checkpoint
	return s.reconstructFromCheckpoint(ctx, entry.ObjectType, entry.ObjectID, targetVersion)
}

// reconstructFromCheckpoint finds the nearest checkpoint and applies diffs to reach the target version.
func (s *GormAuditService) reconstructFromCheckpoint(ctx context.Context, objectType, objectID string, targetVersion int) ([]byte, error) {
	// Find the nearest checkpoint AT or AFTER the target version
	// (Checkpoints store the state BEFORE a mutation, so we work forward from a later checkpoint)
	// Actually, we need to think about this differently:
	//
	// Version N's snapshot stores the state as it was BEFORE version N's mutation was applied.
	// For "created" (version 1), it stores the state AFTER creation.
	// For diffs, it stores a reverse patch: applying it to the CURRENT state gives the PREVIOUS state.
	//
	// To reconstruct version N's snapshot (the state before mutation N):
	// 1. Find the nearest checkpoint at version >= targetVersion
	// 2. Get all diffs between targetVersion and that checkpoint
	// 3. Apply diffs in reverse order from checkpoint down to targetVersion

	// Find nearest checkpoint at or after target version
	var checkpoint models.VersionSnapshot
	err := s.db.WithContext(ctx).
		Where("object_type = ? AND object_id = ? AND snapshot_type = ? AND version >= ?",
			objectType, objectID, models.SnapshotTypeCheckpoint, targetVersion).
		Order("version ASC").
		First(&checkpoint).Error

	if err != nil {
		// No checkpoint after target; try to use the most recent checkpoint before target
		// and work forward using the current entity state
		return nil, fmt.Errorf("cannot reconstruct version %d: no checkpoint available", targetVersion)
	}

	state := []byte(checkpoint.Data.String)

	if checkpoint.Version == targetVersion {
		return state, nil
	}

	// Get all diffs between targetVersion (exclusive) and checkpoint version (exclusive)
	// and apply them in descending order to walk backward from checkpoint to target
	var diffs []models.VersionSnapshot
	err = s.db.WithContext(ctx).
		Where("object_type = ? AND object_id = ? AND version > ? AND version < ?",
			objectType, objectID, targetVersion, checkpoint.Version).
		Order("version DESC").
		Find(&diffs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get version diffs: %w", err)
	}

	// Apply each diff in descending version order
	for _, diff := range diffs {
		if diff.SnapshotType == models.SnapshotTypeCheckpoint {
			// If we hit another checkpoint, use it directly
			state = []byte(diff.Data.String)
			continue
		}
		state, err = ApplyDiff(state, []byte(diff.Data.String))
		if err != nil {
			return nil, fmt.Errorf("failed to apply diff at version %d: %w", diff.Version, err)
		}
	}

	// Apply the target version's own diff if it exists and is a diff type
	var targetSnapshot models.VersionSnapshot
	err = s.db.WithContext(ctx).
		Where("object_type = ? AND object_id = ? AND version = ?",
			objectType, objectID, targetVersion).
		First(&targetSnapshot).Error
	if err == nil && targetSnapshot.SnapshotType == models.SnapshotTypeDiff {
		state, err = ApplyDiff(state, []byte(targetSnapshot.Data.String))
		if err != nil {
			return nil, fmt.Errorf("failed to apply target diff at version %d: %w", targetVersion, err)
		}
	}

	return state, nil
}

// DeleteThreatModelAudit deletes all audit entries and version snapshots for a threat model,
// except the "threat model deleted" entry.
func (s *GormAuditService) DeleteThreatModelAudit(ctx context.Context, threatModelID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get IDs of audit entries to delete (all except TM deletion record)
		var entryIDs []string
		err := tx.Model(&models.AuditEntry{}).
			Where("threat_model_id = ? AND NOT (change_type = ? AND object_type = ?)",
				threatModelID, models.ChangeTypeDeleted, models.ObjectTypeThreatModel).
			Pluck("id", &entryIDs).Error
		if err != nil {
			return fmt.Errorf("failed to find audit entries for deletion: %w", err)
		}

		if len(entryIDs) == 0 {
			return nil
		}

		// Delete version snapshots for these entries
		if err := tx.Where("audit_entry_id IN ?", entryIDs).Delete(&models.VersionSnapshot{}).Error; err != nil {
			return fmt.Errorf("failed to delete version snapshots: %w", err)
		}

		// Delete the audit entries themselves
		if err := tx.Where("id IN ?", entryIDs).Delete(&models.AuditEntry{}).Error; err != nil {
			return fmt.Errorf("failed to delete audit entries: %w", err)
		}

		return nil
	})
}

// PruneAuditEntries removes audit entries older than the configured retention period.
func (s *GormAuditService) PruneAuditEntries(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -s.auditRetentionDays)

	// First delete associated version snapshots
	var entryIDs []string
	err := s.db.WithContext(ctx).Model(&models.AuditEntry{}).
		Where("created_at < ? AND NOT (change_type = ? AND object_type = ?)",
			cutoff, models.ChangeTypeDeleted, models.ObjectTypeThreatModel).
		Pluck("id", &entryIDs).Error
	if err != nil {
		return 0, fmt.Errorf("failed to find prunable audit entries: %w", err)
	}

	if len(entryIDs) == 0 {
		return 0, nil
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("audit_entry_id IN ?", entryIDs).Delete(&models.VersionSnapshot{}).Error; err != nil {
			return fmt.Errorf("failed to delete version snapshots during audit prune: %w", err)
		}

		if err := tx.Where("id IN ?", entryIDs).Delete(&models.AuditEntry{}).Error; err != nil {
			return fmt.Errorf("failed to prune audit entries: %w", err)
		}
		return nil
	})

	return len(entryIDs), err
}

// PruneVersionSnapshots removes version snapshots outside the retention window.
// Always stops at a checkpoint boundary so remaining diffs can be reconstructed.
func (s *GormAuditService) PruneVersionSnapshots(ctx context.Context) (int, error) {
	totalPruned := 0

	// Find distinct (object_type, object_id) pairs that have version snapshots
	type objectKey struct {
		ObjectType string
		ObjectID   string
	}

	var keys []objectKey
	err := s.db.WithContext(ctx).Model(&models.VersionSnapshot{}).
		Select("DISTINCT object_type, object_id").
		Scan(&keys).Error
	if err != nil {
		return 0, fmt.Errorf("failed to find objects with version snapshots: %w", err)
	}

	timeCutoff := time.Now().UTC().AddDate(0, 0, -s.versionRetentionDays)

	for _, key := range keys {
		pruned, err := s.pruneObjectVersions(ctx, key.ObjectType, key.ObjectID, timeCutoff)
		if err != nil {
			slogging.Get().Error("failed to prune versions for %s/%s: %v", key.ObjectType, key.ObjectID, err)
			continue
		}
		totalPruned += pruned
	}

	return totalPruned, nil
}

// pruneObjectVersions prunes version snapshots for a single object.
func (s *GormAuditService) pruneObjectVersions(ctx context.Context, objectType, objectID string, timeCutoff time.Time) (int, error) {
	// Get all version snapshots for this object, ordered by version
	var snapshots []models.VersionSnapshot
	err := s.db.WithContext(ctx).
		Where("object_type = ? AND object_id = ?", objectType, objectID).
		Order("version ASC").
		Find(&snapshots).Error
	if err != nil {
		return 0, err
	}

	if len(snapshots) <= 1 {
		return 0, nil // never prune the only snapshot
	}

	// Determine the oldest version to keep based on count and time retention
	// Keep: versions within count limit OR within time limit (whichever keeps more)
	keepByCount := len(snapshots) - s.versionRetentionCount
	if keepByCount < 0 {
		keepByCount = 0
	}

	// Find count-based prune boundary
	countBoundaryVersion := 0
	if keepByCount > 0 {
		countBoundaryVersion = snapshots[keepByCount-1].Version
	}

	// Find time-based prune boundary
	timeBoundaryVersion := 0
	for _, snap := range snapshots {
		if snap.CreatedAt.Before(timeCutoff) {
			timeBoundaryVersion = snap.Version
		}
	}

	// Use the SMALLER boundary (prune fewer, keep more)
	pruneBoundary := countBoundaryVersion
	if timeBoundaryVersion < pruneBoundary {
		pruneBoundary = timeBoundaryVersion
	}

	if pruneBoundary <= 0 {
		return 0, nil // nothing to prune
	}

	// Critical: find the nearest checkpoint AT or BEFORE the prune boundary
	// We must stop at a checkpoint so remaining diffs have a valid starting point
	actualBoundary := 0
	for _, snap := range snapshots {
		if snap.Version > pruneBoundary {
			break
		}
		if snap.SnapshotType == models.SnapshotTypeCheckpoint {
			actualBoundary = snap.Version
		}
	}

	// Never prune version 1 (always a checkpoint for the created state)
	if actualBoundary <= 1 {
		return 0, nil
	}

	// Delete snapshots with version < actualBoundary
	return s.executePrune(ctx, objectType, objectID, actualBoundary)
}

// executePrune deletes version snapshots below the boundary and nulls audit entry versions.
func (s *GormAuditService) executePrune(ctx context.Context, objectType, objectID string, boundary int) (int, error) {
	var pruned int

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get IDs of snapshots to delete
		var snapshotIDs []string
		err := tx.Model(&models.VersionSnapshot{}).
			Where("object_type = ? AND object_id = ? AND version < ?", objectType, objectID, boundary).
			Pluck("id", &snapshotIDs).Error
		if err != nil {
			return err
		}

		if len(snapshotIDs) == 0 {
			return nil
		}

		// Get associated audit entry IDs
		var auditEntryIDs []string
		err = tx.Model(&models.VersionSnapshot{}).
			Where("id IN ?", snapshotIDs).
			Pluck("audit_entry_id", &auditEntryIDs).Error
		if err != nil {
			return err
		}

		// Delete version snapshots
		if err := tx.Where("id IN ?", snapshotIDs).Delete(&models.VersionSnapshot{}).Error; err != nil {
			return err
		}

		// Null out version on corresponding audit entries
		if len(auditEntryIDs) > 0 {
			if err := tx.Model(&models.AuditEntry{}).
				Where("id IN ?", auditEntryIDs).
				Update("version", nil).Error; err != nil {
				return err
			}
		}

		pruned = len(snapshotIDs)
		return nil
	})

	return pruned, err
}

// applyAuditFilters adds WHERE clauses based on the provided filters.
func applyAuditFilters(query *gorm.DB, filters *AuditFilters) *gorm.DB {
	if filters == nil {
		return query
	}
	if filters.ObjectType != nil {
		query = query.Where("object_type = ?", *filters.ObjectType)
	}
	if filters.ObjectID != nil {
		query = query.Where("object_id = ?", *filters.ObjectID)
	}
	if filters.ChangeType != nil {
		query = query.Where("change_type = ?", *filters.ChangeType)
	}
	if filters.ActorEmail != nil {
		query = query.Where("actor_email = ?", *filters.ActorEmail)
	}
	if filters.After != nil {
		query = query.Where("created_at >= ?", *filters.After)
	}
	if filters.Before != nil {
		query = query.Where("created_at <= ?", *filters.Before)
	}
	return query
}

// toAuditEntryResponse converts a GORM model to an API response.
func toAuditEntryResponse(entry models.AuditEntry) AuditEntryResponse {
	resp := AuditEntryResponse{
		ID:            entry.ID,
		ThreatModelID: entry.ThreatModelID,
		ObjectType:    entry.ObjectType,
		ObjectID:      entry.ObjectID,
		Version:       entry.Version,
		ChangeType:    entry.ChangeType,
		Actor: InternalAuditActor{
			Email:       entry.ActorEmail,
			Provider:    entry.ActorProvider,
			ProviderID:  entry.ActorProviderID,
			DisplayName: entry.ActorDisplayName,
		},
		CreatedAt: entry.CreatedAt,
	}
	if entry.ChangeSummary.Valid {
		resp.ChangeSummary = &entry.ChangeSummary.String
	}
	return resp
}

// toAuditEntryResponses converts a slice of GORM models to API responses.
func toAuditEntryResponses(entries []models.AuditEntry) []AuditEntryResponse {
	responses := make([]AuditEntryResponse, len(entries))
	for i, e := range entries {
		responses[i] = toAuditEntryResponse(e)
	}
	return responses
}

// PurgeTombstones hard-deletes entities that have been soft-deleted longer than the retention period.
func (s *GormAuditService) PurgeTombstones(ctx context.Context) (int, error) {
	logger := slogging.Get()
	cutoff := time.Now().UTC().Add(-time.Duration(s.tombstoneRetentionDays) * 24 * time.Hour)
	totalPurged := 0

	// Purge expired threat models (cascading hard-delete handles children)
	var expiredTMs []models.ThreatModel
	if err := s.db.WithContext(ctx).Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).Find(&expiredTMs).Error; err != nil {
		return 0, fmt.Errorf("failed to query expired threat model tombstones: %w", err)
	}

	for _, tm := range expiredTMs {
		tmID := tm.ID
		// Use HardDelete on the ThreatModelStore (which cascades to children)
		if ThreatModelStore != nil {
			if err := ThreatModelStore.HardDelete(tmID); err != nil {
				logger.Error("failed to hard-delete expired threat model %s: %v", tmID, err)
				continue
			}
		}
		// Note: audit entries are append-only and are never deleted
		totalPurged++
	}

	// Purge orphaned sub-resources (soft-deleted children of non-deleted parents)
	type subResource struct {
		table string
		name  string
	}
	subResources := []subResource{
		{"diagrams", "diagram"},
		{"threats", "threat"},
		{"assets", "asset"},
		{"documents", "document"},
		{"notes", "note"},
		{"repositories", "repository"},
	}

	for _, sr := range subResources {
		// Query expired sub-resource IDs first to clean up associated metadata
		var expiredIDs []string
		if err := s.db.WithContext(ctx).
			Table(sr.table).
			Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).
			Pluck("id", &expiredIDs).Error; err != nil {
			logger.Error("failed to query expired %s tombstones: %v", sr.name, err)
			continue
		}
		if len(expiredIDs) == 0 {
			continue
		}

		// Clean up metadata for these sub-resources
		if metaResult := s.db.WithContext(ctx).
			Exec("DELETE FROM metadata WHERE entity_type = ? AND entity_id IN ?", sr.name, expiredIDs); metaResult.Error != nil {
			logger.Error("failed to clean up metadata for expired %s tombstones: %v", sr.name, metaResult.Error)
		}

		// Delete the sub-resources themselves
		result := s.db.WithContext(ctx).
			Exec(fmt.Sprintf("DELETE FROM %s WHERE id IN ?", sr.table), expiredIDs)
		if result.Error != nil {
			logger.Error("failed to purge expired %s tombstones: %v", sr.name, result.Error)
			continue
		}
		if result.RowsAffected > 0 {
			logger.Info("purged %d expired %s tombstones (with metadata)", result.RowsAffected, sr.name)
			totalPurged += int(result.RowsAffected)
		}
	}

	return totalPurged, nil
}

// Ensure GormAuditService implements AuditServiceInterface at compile time
var _ AuditServiceInterface = (*GormAuditService)(nil)

// MarshalAuditEntryResponse is a helper to serialize an AuditEntryResponse to JSON.
func MarshalAuditEntryResponse(resp AuditEntryResponse) ([]byte, error) {
	return json.Marshal(resp)
}
