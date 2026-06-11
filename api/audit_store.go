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
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// Default retention configuration
const (
	defaultAuditRetentionDays     = 365
	defaultVersionRetentionCount  = 50
	defaultVersionRetentionDays   = 90
	defaultTombstoneRetentionDays = 30

	defaultSystemAuditRetentionDays = 365
	// minSystemAuditRetentionDays is the documented evidence minimum: system
	// audit rows are T7 evidence and accidentally-aggressive pruning destroys
	// investigative value (#400).
	minSystemAuditRetentionDays = 90
)

// oracleMaxInListSize is Oracle's hard cap on IN expression lists
// (ORA-01795); all bulk deletes chunk their ID slices to stay under it.
const oracleMaxInListSize = 1000

// chunkIDs splits ids into slices of at most size elements.
func chunkIDs(ids []string, size int) [][]string {
	if len(ids) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}

// GormAuditService implements AuditServiceInterface using GORM.
type GormAuditService struct {
	db                       *gorm.DB
	auditRetentionDays       int
	versionRetentionCount    int
	versionRetentionDays     int
	tombstoneRetentionDays   int
	systemAuditRetentionDays int
}

// NewGormAuditService creates a new GormAuditService with configuration from environment.
func NewGormAuditService(db *gorm.DB) *GormAuditService {
	return &GormAuditService{
		db:                       db,
		auditRetentionDays:       AuditRetentionDays(),
		versionRetentionCount:    getEnvInt("VERSION_RETENTION_COUNT", defaultVersionRetentionCount),
		versionRetentionDays:     VersionRetentionDays(),
		tombstoneRetentionDays:   TombstoneRetentionDays(),
		systemAuditRetentionDays: SystemAuditRetentionDays(),
	}
}

// AuditRetentionDays returns the configured audit-entry retention in days
// (AUDIT_RETENTION_DAYS, default 365). Exported because the append-only
// trigger installation derives its delete age floor from the same value,
// so the pruner cutoff and the trigger floor cannot drift (#453).
func AuditRetentionDays() int {
	return getEnvInt("AUDIT_RETENTION_DAYS", defaultAuditRetentionDays)
}

// VersionRetentionDays returns the configured version-snapshot retention in
// days (VERSION_RETENTION_DAYS, default 90). See AuditRetentionDays.
func VersionRetentionDays() int {
	return getEnvInt("VERSION_RETENTION_DAYS", defaultVersionRetentionDays)
}

// TombstoneRetentionDays returns the configured tombstone retention in days
// (TOMBSTONE_RETENTION_DAYS, default 30). See AuditRetentionDays.
func TombstoneRetentionDays() int {
	return getEnvInt("TOMBSTONE_RETENTION_DAYS", defaultTombstoneRetentionDays)
}

// SystemAuditRetentionDays returns the configured system-audit retention in
// days (SYSTEM_AUDIT_RETENTION_DAYS, default 365), clamped to a 90-day
// minimum. Exported because the append-only trigger installation derives its
// delete age floor from the same value (#400).
func SystemAuditRetentionDays() int {
	days := getEnvInt("SYSTEM_AUDIT_RETENTION_DAYS", defaultSystemAuditRetentionDays)
	if days < minSystemAuditRetentionDays {
		slogging.Get().Warn("SYSTEM_AUDIT_RETENTION_DAYS=%d is below the %d-day evidence minimum; using %d",
			days, minSystemAuditRetentionDays, minSystemAuditRetentionDays)
		return minSystemAuditRetentionDays
	}
	return days
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
	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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
			ThreatModelID:    models.DBVarchar(params.ThreatModelID),
			ObjectType:       models.DBVarchar(params.ObjectType),
			ObjectID:         models.DBVarchar(params.ObjectID),
			Version:          &nextVersion,
			ChangeType:       models.DBVarchar(params.ChangeType),
			ActorEmail:       models.DBVarchar(params.Actor.Email),
			ActorProvider:    models.DBVarchar(params.Actor.Provider),
			ActorProviderID:  models.DBVarchar(params.Actor.ProviderID),
			ActorDisplayName: models.DBVarchar(params.Actor.DisplayName),
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
		ObjectType:   models.DBVarchar(params.ObjectType),
		ObjectID:     models.DBVarchar(params.ObjectID),
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
	return s.reconstructFromCheckpoint(ctx, string(entry.ObjectType), string(entry.ObjectID), targetVersion)
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

	err = authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		for _, chunk := range chunkIDs(entryIDs, oracleMaxInListSize) {
			if err := tx.Where("audit_entry_id IN ?", chunk).Delete(&models.VersionSnapshot{}).Error; err != nil {
				return fmt.Errorf("failed to delete version snapshots during audit prune: %w", err)
			}
		}

		for _, chunk := range chunkIDs(entryIDs, oracleMaxInListSize) {
			if err := tx.Where("id IN ?", chunk).Delete(&models.AuditEntry{}).Error; err != nil {
				return fmt.Errorf("failed to prune audit entries: %w", err)
			}
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

// executePrune deletes version snapshots below the boundary. The parent
// audit entries are immutable and keep their version numbers; a missing
// snapshot means the content was pruned and rollback returns 410 Gone.
func (s *GormAuditService) executePrune(ctx context.Context, objectType, objectID string, boundary int) (int, error) {
	var pruned int

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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

		// Delete version snapshots in chunks to stay under Oracle's 1000-element IN-list cap (ORA-01795).
		for _, chunk := range chunkIDs(snapshotIDs, oracleMaxInListSize) {
			if err := tx.Where("id IN ?", chunk).Delete(&models.VersionSnapshot{}).Error; err != nil {
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
		ID:            string(entry.ID),
		ThreatModelID: string(entry.ThreatModelID),
		ObjectType:    string(entry.ObjectType),
		ObjectID:      string(entry.ObjectID),
		Version:       entry.Version,
		ChangeType:    string(entry.ChangeType),
		Actor: InternalAuditActor{
			Email:       string(entry.ActorEmail),
			Provider:    string(entry.ActorProvider),
			ProviderID:  string(entry.ActorProviderID),
			DisplayName: string(entry.ActorDisplayName),
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
			if err := ThreatModelStore.HardDelete(string(tmID)); err != nil {
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

		// Clean up metadata for these sub-resources.
		// Chunk ID slices to stay under Oracle's 1000-element IN-list cap (ORA-01795).
		for _, chunk := range chunkIDs(expiredIDs, oracleMaxInListSize) {
			if metaResult := s.db.WithContext(ctx).
				Exec("DELETE FROM metadata WHERE entity_type = ? AND entity_id IN ?", sr.name, chunk); metaResult.Error != nil {
				logger.Error("%s", pruneFailureMessage("metadata for expired "+sr.name+" tombstones", metaResult.Error))
			}
		}

		// Clean up version snapshots for these sub-resources.
		// Note: audit entries are append-only and are never deleted.
		for _, chunk := range chunkIDs(expiredIDs, oracleMaxInListSize) {
			if vsResult := s.db.WithContext(ctx).
				Exec("DELETE FROM version_snapshots WHERE object_type = ? AND object_id IN ?", sr.name, chunk); vsResult.Error != nil {
				logger.Error("%s", pruneFailureMessage("version snapshots for expired "+sr.name+" tombstones", vsResult.Error))
			}
		}

		// Delete the sub-resources themselves, accumulating rows affected across chunks.
		var subResourceRowsAffected int64
		var subResourceErr error
		for _, chunk := range chunkIDs(expiredIDs, oracleMaxInListSize) {
			result := s.db.WithContext(ctx).
				Exec(fmt.Sprintf("DELETE FROM %s WHERE id IN ?", sr.table), chunk)
			if result.Error != nil {
				subResourceErr = result.Error
				break
			}
			subResourceRowsAffected += result.RowsAffected
		}
		if subResourceErr != nil {
			logger.Error("failed to purge expired %s tombstones: %v", sr.name, subResourceErr)
			continue
		}
		if subResourceRowsAffected > 0 {
			logger.Info("purged %d expired %s tombstones (with metadata and version snapshots)", subResourceRowsAffected, sr.name)
			totalPurged += int(subResourceRowsAffected)
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
