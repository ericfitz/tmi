package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormThreatStore implements ThreatStore with GORM for database persistence and Redis caching
type GormThreatStore struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewGormThreatStore creates a new GORM-backed threat store with caching
func NewGormThreatStore(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormThreatStore {
	return &GormThreatStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new threat with write-through caching using GORM
func (s *GormThreatStore) Create(ctx context.Context, threat *Threat) error {
	logger := slogging.Get()
	logger.Debug("Creating threat: %s in threat model: %s", threat.Name, threat.ThreatModelId)

	// Generate UUIDv7 ID if not provided (for better index locality)
	if threat.Id == nil {
		id := uuidgen.MustNewForEntity(uuidgen.EntityTypeThreat)
		threat.Id = &id
	}

	// Normalize severity
	if threat.Severity != nil {
		normalized := normalizeSeverity(*threat.Severity)
		threat.Severity = &normalized
	}

	// Convert API model to GORM model
	gormThreat := s.toGormModelForCreate(threat)

	// Log the gormThreat for debugging
	logger.Debug("GORM Threat model before insert: ID=%s, ThreatModelID=%s, Name=%s",
		gormThreat.ID, gormThreat.ThreatModelID, gormThreat.Name)

	// Use GORM's standard Create - this handles all type conversions correctly
	// (StringArray, OracleBool, etc.) across different database dialects.
	// Note: The Threat model now has FieldsWithDefaultDBValue=0 (autoCreateTime/autoUpdateTime
	// tags removed, timestamps set explicitly in toGormModelForCreate), so the dzwvip/oracle
	// driver's RETURNING INTO bug should not be triggered.
	if err := s.db.WithContext(ctx).Create(gormThreat).Error; err != nil {
		logger.Error("Failed to create threat in database: %v", err)
		return fmt.Errorf("failed to create threat: %w", err)
	}

	// Update API model with timestamps set by GORM
	threat.CreatedAt = &gormThreat.CreatedAt
	threat.ModifiedAt = &gormThreat.ModifiedAt

	// Cache the new threat
	if s.cache != nil {
		if cacheErr := s.cache.CacheThreat(ctx, threat); cacheErr != nil {
			logger.Error("Failed to cache new threat: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "threat",
			EntityID:      threat.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threat.ThreatModelId.String(),
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after threat creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created threat: %s", threat.Id)
	return nil
}

// Get retrieves a threat by ID with cache-first strategy using GORM
func (s *GormThreatStore) Get(ctx context.Context, id string) (*Threat, error) {
	logger := slogging.Get()
	logger.Debug("Getting threat: %s", id)

	// Try cache first
	if s.cache != nil {
		threat, err := s.cache.GetCachedThreat(ctx, id)
		if err != nil {
			logger.Error("Cache error when getting threat %s: %v", id, err)
		} else if threat != nil {
			logger.Debug("Cache hit for threat: %s", id)
			return threat, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for threat %s, querying database", id)

	var gormThreat models.Threat
	if err := s.db.WithContext(ctx).First(&gormThreat, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("threat not found: %s", id)
		}
		logger.Error("Failed to get threat from database: %v", err)
		return nil, fmt.Errorf("failed to get threat: %w", err)
	}

	// Convert GORM model to API model
	threat := s.toAPIModel(&gormThreat)

	// Load metadata from the metadata table
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for threat %s: %v", id, err)
		metadata = []Metadata{}
	}
	threat.Metadata = &metadata

	// Cache the result for future requests
	if s.cache != nil {
		if cacheErr := s.cache.CacheThreat(ctx, threat); cacheErr != nil {
			logger.Error("Failed to cache threat after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved threat: %s", id)
	return threat, nil
}

// Update updates an existing threat with write-through caching using GORM
func (s *GormThreatStore) Update(ctx context.Context, threat *Threat) error {
	logger := slogging.Get()
	logger.Debug("Updating threat: %s", threat.Id)

	// Update modified timestamp
	now := time.Now().UTC()
	threat.ModifiedAt = &now

	// Normalize severity
	if threat.Severity != nil {
		normalized := normalizeSeverity(*threat.Severity)
		threat.Severity = &normalized
	}

	// Convert to GORM model and update
	// Use struct-based Updates to ensure custom types (like StringArray for ThreatType)
	// are properly serialized via their Value() method. Map-based Updates bypasses custom type handling.
	gormThreat := s.toGormModel(threat)

	result := s.db.WithContext(ctx).Model(&models.Threat{}).Where("id = ?", threat.Id.String()).Updates(gormThreat)

	if result.Error != nil {
		logger.Error("Failed to update threat in database: %v", result.Error)
		return fmt.Errorf("failed to update threat: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("threat not found: %s", threat.Id)
	}

	// Save metadata to separate table
	if err := s.saveMetadata(ctx, threat.Id.String(), threat.Metadata); err != nil {
		logger.Error("Failed to save metadata for threat %s: %v", threat.Id, err)
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheThreat(ctx, threat); cacheErr != nil {
			logger.Error("Failed to update threat cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "threat",
			EntityID:      threat.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threat.ThreatModelId.String(),
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after threat update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated threat: %s", threat.Id)
	return nil
}

// Delete removes a threat and invalidates related caches using GORM
func (s *GormThreatStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting threat: %s", id)

	// Get the threat first to get parent info for cache invalidation
	threat, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete from database
	result := s.db.WithContext(ctx).Delete(&models.Threat{}, "id = ?", id)
	if result.Error != nil {
		logger.Error("Failed to delete threat from database: %v", result.Error)
		return fmt.Errorf("failed to delete threat: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("threat not found: %s", id)
	}

	// Remove from cache
	if s.cache != nil {
		if cacheErr := s.cache.InvalidateEntity(ctx, "threat", id); cacheErr != nil {
			logger.Error("Failed to remove threat from cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "threat",
			EntityID:      id,
			ParentType:    "threat_model",
			ParentID:      threat.ThreatModelId.String(),
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after threat deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted threat: %s", id)
	return nil
}

// List retrieves threats for a threat model with advanced filtering, sorting and pagination using GORM
func (s *GormThreatStore) List(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
	logger := slogging.Get()
	logger.Debug("Listing threats for threat model %s with advanced filters", threatModelID)

	// Check if we should use cache
	useCache := s.shouldUseCache(filter)

	// Try cache first for simple queries
	if useCache {
		if threats, err := s.tryGetFromCache(ctx, threatModelID, filter); err == nil && threats != nil {
			return threats, nil
		}
	}

	// Build and execute query
	threats, err := s.executeListQuery(ctx, threatModelID, filter)
	if err != nil {
		return nil, err
	}

	// Cache the result only for simple queries
	if useCache && s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "threats", threatModelID, filter.Offset, filter.Limit, threats); cacheErr != nil {
			logger.Error("Failed to cache threat list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d threats", len(threats))
	return threats, nil
}

// executeListQuery builds and executes the GORM query for listing threats
func (s *GormThreatStore) executeListQuery(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.Threat{}).Where("threat_model_id = ?", threatModelID)

	// Apply filters
	query = s.applyFilters(query, filter)

	// Apply sorting
	orderBy := "created_at DESC"
	if filter.Sort != nil {
		orderBy = s.buildOrderBy(*filter.Sort)
	}
	query = query.Order(orderBy)

	// Apply pagination
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var gormThreats []models.Threat
	if err := query.Find(&gormThreats).Error; err != nil {
		logger.Error("Failed to query threats from database: %v", err)
		return nil, fmt.Errorf("failed to list threats: %w", err)
	}

	// Convert to API models
	threats := make([]Threat, 0, len(gormThreats))
	for _, gt := range gormThreats {
		threats = append(threats, *s.toAPIModel(&gt))
	}

	return threats, nil
}

// applyFilters applies the filter conditions to the GORM query
func (s *GormThreatStore) applyFilters(query *gorm.DB, filter ThreatFilter) *gorm.DB {
	// Text filters - use LOWER() for cross-database case-insensitive search
	if filter.Name != nil {
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+*filter.Name+"%")
	}

	if filter.Description != nil {
		query = query.Where("LOWER(description) LIKE LOWER(?)", "%"+*filter.Description+"%")
	}

	// Enum filters
	if len(filter.ThreatType) > 0 {
		// For JSON array field - use database-specific JSON contains
		// This works with both PostgreSQL JSONB and Oracle JSON
		for _, tt := range filter.ThreatType {
			query = query.Where("threat_type LIKE ?", "%\""+tt+"\"%")
		}
	}

	if filter.Severity != nil {
		query = query.Where("severity = ?", *filter.Severity)
	}

	if filter.Priority != nil {
		query = query.Where("priority = ?", *filter.Priority)
	}

	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}

	// UUID filters
	if filter.DiagramID != nil {
		query = query.Where("diagram_id = ?", filter.DiagramID.String())
	}

	if filter.CellID != nil {
		query = query.Where("cell_id = ?", filter.CellID.String())
	}

	// Score filters
	if filter.ScoreGT != nil {
		query = query.Where("score > ?", *filter.ScoreGT)
	}

	if filter.ScoreLT != nil {
		query = query.Where("score < ?", *filter.ScoreLT)
	}

	if filter.ScoreEQ != nil {
		query = query.Where("score = ?", *filter.ScoreEQ)
	}

	if filter.ScoreGE != nil {
		query = query.Where("score >= ?", *filter.ScoreGE)
	}

	if filter.ScoreLE != nil {
		query = query.Where("score <= ?", *filter.ScoreLE)
	}

	// Date filters
	if filter.CreatedAfter != nil {
		query = query.Where("created_at > ?", *filter.CreatedAfter)
	}

	if filter.CreatedBefore != nil {
		query = query.Where("created_at < ?", *filter.CreatedBefore)
	}

	if filter.ModifiedAfter != nil {
		query = query.Where("modified_at > ?", *filter.ModifiedAfter)
	}

	if filter.ModifiedBefore != nil {
		query = query.Where("modified_at < ?", *filter.ModifiedBefore)
	}

	return query
}

// buildOrderBy constructs a safe ORDER BY clause from sort parameter
func (s *GormThreatStore) buildOrderBy(sort string) string {
	validColumns := map[string]string{
		"name":        "name",
		"created_at":  "created_at",
		"modified_at": "modified_at",
		"severity":    "severity",
		"priority":    "priority",
		"status":      "status",
		"score":       "score",
		"threat_type": "threat_type",
	}

	parts := strings.Split(sort, ":")
	if len(parts) != 2 {
		return "created_at DESC"
	}

	column, direction := parts[0], strings.ToUpper(parts[1])

	safeColumn, exists := validColumns[column]
	if !exists {
		return "created_at DESC"
	}

	if direction != "ASC" && direction != "DESC" {
		direction = "DESC"
	}

	return safeColumn + " " + direction
}

// Patch applies JSON patch operations to a threat using GORM
func (s *GormThreatStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error) {
	logger := slogging.Get()
	logger.Debug("Patching threat %s with %d operations", id, len(operations))

	// Get current threat
	threat, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations (reuse the same patch logic)
	for _, op := range operations {
		if err := s.applyPatchOperation(threat, op); err != nil {
			logger.Error("Failed to apply patch operation %s to threat %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Update the threat
	if err := s.Update(ctx, threat); err != nil {
		return nil, err
	}

	return threat, nil
}

// applyPatchOperation applies a single patch operation to a threat
func (s *GormThreatStore) applyPatchOperation(threat *Threat, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				threat.Name = name
				return nil
			}
			return fmt.Errorf("invalid value type for name: expected string")
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				threat.Description = &desc
				return nil
			}
			return fmt.Errorf("invalid value type for description: expected string")
		case "remove":
			threat.Description = nil
		}
	case "/severity":
		if op.Op == "replace" {
			if sev, ok := op.Value.(string); ok {
				normalized := normalizeSeverity(sev)
				threat.Severity = &normalized
				return nil
			}
			return fmt.Errorf("invalid value type for severity: expected string")
		}
	case "/mitigation":
		switch op.Op {
		case "replace", "add":
			if mit, ok := op.Value.(string); ok {
				threat.Mitigation = &mit
				return nil
			}
			return fmt.Errorf("invalid value type for mitigation: expected string")
		case "remove":
			threat.Mitigation = nil
		}
	case "/status":
		if op.Op == "replace" {
			if status, ok := op.Value.(string); ok {
				threat.Status = &status
				return nil
			}
			return fmt.Errorf("invalid value type for status: expected string")
		}
	case "/priority":
		if op.Op == "replace" {
			if priority, ok := op.Value.(string); ok {
				threat.Priority = &priority
				return nil
			}
			return fmt.Errorf("invalid value type for priority: expected string")
		}
	case "/mitigated":
		if op.Op == "replace" {
			if mitigated, ok := op.Value.(bool); ok {
				threat.Mitigated = &mitigated
				return nil
			}
			return fmt.Errorf("invalid value type for mitigated: expected boolean")
		}
	case "/score":
		switch op.Op {
		case "replace", "add":
			if score, ok := op.Value.(float64); ok {
				score32 := float32(score)
				threat.Score = &score32
				return nil
			}
			return fmt.Errorf("invalid value type for score: expected number")
		case "remove":
			threat.Score = nil
		}
	case "/threat_type":
		return s.patchThreatTypeGorm(threat, op)
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

func (s *GormThreatStore) patchThreatTypeGorm(threat *Threat, op PatchOperation) error {
	switch op.Op {
	case "replace":
		if types, ok := op.Value.([]interface{}); ok {
			stringTypes := make([]string, 0, len(types))
			for _, t := range types {
				if str, ok := t.(string); ok {
					stringTypes = append(stringTypes, str)
				} else {
					return fmt.Errorf("invalid type in threat_type array")
				}
			}
			threat.ThreatType = stringTypes
			return nil
		}
		return fmt.Errorf("threat_type replace requires array")
	case "add":
		if newType, ok := op.Value.(string); ok {
			for _, existing := range threat.ThreatType {
				if existing == newType {
					return nil
				}
			}
			threat.ThreatType = append(threat.ThreatType, newType)
			return nil
		}
		return fmt.Errorf("threat_type add requires string value")
	case "remove":
		if removeType, ok := op.Value.(string); ok {
			filtered := make([]string, 0, len(threat.ThreatType))
			for _, t := range threat.ThreatType {
				if t != removeType {
					filtered = append(filtered, t)
				}
			}
			threat.ThreatType = filtered
			return nil
		}
		return fmt.Errorf("threat_type remove requires string value")
	}
	return nil
}

// BulkCreate creates multiple threats in a single transaction using GORM
func (s *GormThreatStore) BulkCreate(ctx context.Context, threats []Threat) error {
	logger := slogging.Get()
	logger.Debug("Bulk creating %d threats", len(threats))

	if len(threats) == 0 {
		return nil
	}

	// Convert to GORM models (timestamps set explicitly in toGormModelForCreate)
	var parentThreatModelID string
	gormThreats := make([]models.Threat, 0, len(threats))

	for i := range threats {
		threat := &threats[i]

		if threat.Id == nil {
			id := uuid.New()
			threat.Id = &id
		}

		if threat.Severity != nil {
			normalized := normalizeSeverity(*threat.Severity)
			threat.Severity = &normalized
		}

		if parentThreatModelID == "" {
			parentThreatModelID = threat.ThreatModelId.String()
		}

		gormThreats = append(gormThreats, *s.toGormModelForCreate(threat))
	}

	// Create all in a transaction
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Create(&gormThreats).Error
	})

	if err != nil {
		logger.Error("Failed to bulk create threats: %v", err)
		return fmt.Errorf("failed to bulk create threats: %w", err)
	}

	// Update API models with timestamps set by GORM
	for i := range threats {
		threats[i].CreatedAt = &gormThreats[i].CreatedAt
		threats[i].ModifiedAt = &gormThreats[i].ModifiedAt
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil && parentThreatModelID != "" {
		if invErr := s.cacheInvalidator.InvalidateAllRelatedCaches(ctx, parentThreatModelID); invErr != nil {
			logger.Error("Failed to invalidate caches after bulk threat creation: %v", invErr)
		}
	}

	logger.Debug("Successfully bulk created %d threats", len(threats))
	return nil
}

// BulkUpdate updates multiple threats in a single transaction using GORM
func (s *GormThreatStore) BulkUpdate(ctx context.Context, threats []Threat) error {
	logger := slogging.Get()
	logger.Debug("Bulk updating %d threats", len(threats))

	if len(threats) == 0 {
		return nil
	}

	now := time.Now().UTC()
	var parentThreatModelID string

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range threats {
			threat := &threats[i]
			threat.ModifiedAt = &now

			if threat.Severity != nil {
				normalized := normalizeSeverity(*threat.Severity)
				threat.Severity = &normalized
			}

			if parentThreatModelID == "" {
				parentThreatModelID = threat.ThreatModelId.String()
			}

			gormThreat := s.toGormModel(threat)
			if err := tx.Model(&models.Threat{}).Where("id = ?", threat.Id.String()).Updates(gormThreat).Error; err != nil {
				return err
			}

			// Save metadata
			if err := s.saveMetadataTx(tx, threat.Id.String(), threat.Metadata); err != nil {
				logger.Error("Failed to save metadata for threat %s: %v", threat.Id, err)
			}
		}
		return nil
	})

	if err != nil {
		logger.Error("Failed to bulk update threats: %v", err)
		return fmt.Errorf("failed to bulk update threats: %w", err)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil && parentThreatModelID != "" {
		if invErr := s.cacheInvalidator.InvalidateAllRelatedCaches(ctx, parentThreatModelID); invErr != nil {
			logger.Error("Failed to invalidate caches after bulk threat update: %v", invErr)
		}
	}

	logger.Debug("Successfully bulk updated %d threats", len(threats))
	return nil
}

// InvalidateCache removes threat-related cache entries
func (s *GormThreatStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateEntity(ctx, "threat", id)
}

// WarmCache preloads threats for a threat model into cache
func (s *GormThreatStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	filter := ThreatFilter{Offset: 0, Limit: 50}
	_, err := s.List(ctx, threatModelID, filter)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	logger.Debug("Warmed cache for threat model %s", threatModelID)
	return nil
}

// loadMetadata loads metadata for a threat using GORM
func (s *GormThreatStore) loadMetadata(ctx context.Context, threatID string) ([]Metadata, error) {
	var metadataEntries []models.Metadata
	if err := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", "threat", threatID).
		Order("key ASC").
		Find(&metadataEntries).Error; err != nil {
		return nil, err
	}

	metadata := make([]Metadata, 0, len(metadataEntries))
	for _, entry := range metadataEntries {
		metadata = append(metadata, Metadata{
			Key:   entry.Key,
			Value: entry.Value,
		})
	}

	return metadata, nil
}

// saveMetadata saves metadata for a threat using GORM
func (s *GormThreatStore) saveMetadata(ctx context.Context, threatID string, metadata *[]Metadata) error {
	return s.saveMetadataTx(s.db.WithContext(ctx), threatID, metadata)
}

// saveMetadataTx saves metadata within a transaction
func (s *GormThreatStore) saveMetadataTx(tx *gorm.DB, threatID string, metadata *[]Metadata) error {
	logger := slogging.Get()

	// Delete existing metadata
	if err := tx.Where("entity_type = ? AND entity_id = ?", "threat", threatID).Delete(&models.Metadata{}).Error; err != nil {
		logger.Error("Failed to delete existing metadata for threat %s: %v", threatID, err)
		return fmt.Errorf("failed to delete existing metadata: %w", err)
	}

	// Insert new metadata if present
	if metadata != nil && len(*metadata) > 0 {
		for _, m := range *metadata {
			entry := models.Metadata{
				ID:         uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata).String(),
				EntityType: "threat",
				EntityID:   threatID,
				Key:        m.Key,
				Value:      m.Value,
			}

			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "modified_at"}),
			}).Create(&entry).Error; err != nil {
				logger.Error("Failed to insert metadata for threat %s (key: %s): %v", threatID, m.Key, err)
				return fmt.Errorf("failed to insert metadata: %w", err)
			}
		}
	}

	return nil
}

// Helper functions

// shouldUseCache determines if the query is simple enough to use caching
func (s *GormThreatStore) shouldUseCache(filter ThreatFilter) bool {
	return filter.Name == nil && filter.Description == nil && len(filter.ThreatType) == 0 &&
		filter.Severity == nil && filter.Priority == nil && filter.Status == nil &&
		filter.DiagramID == nil && filter.CellID == nil &&
		filter.ScoreGT == nil && filter.ScoreLT == nil && filter.ScoreEQ == nil &&
		filter.ScoreGE == nil && filter.ScoreLE == nil &&
		filter.CreatedAfter == nil && filter.CreatedBefore == nil &&
		filter.ModifiedAfter == nil && filter.ModifiedBefore == nil &&
		filter.Sort == nil
}

// tryGetFromCache attempts to retrieve threats from cache
func (s *GormThreatStore) tryGetFromCache(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
	if s.cache == nil {
		return nil, fmt.Errorf("cache not available")
	}

	logger := slogging.Get()
	var threats []Threat
	err := s.cache.GetCachedList(ctx, "threats", threatModelID, filter.Offset, filter.Limit, &threats)
	if err == nil && threats != nil {
		logger.Debug("Cache hit for threat list %s [%d:%d]", threatModelID, filter.Offset, filter.Limit)
		return threats, nil
	}
	if err != nil {
		logger.Error("Cache error when getting threat list: %v", err)
	}
	return nil, err
}

// toGormModelForCreate converts an API Threat to a GORM model for CREATE operations.
// Timestamps are set explicitly to ensure compatibility across all database backends.
// The dzwvip/oracle driver has issues with RETURNING INTO clause when relying on
// GORM's autoCreateTime/autoUpdateTime - by setting timestamps explicitly, we avoid
// the RETURNING INTO clause and the associated Oracle driver bugs.
func (s *GormThreatStore) toGormModelForCreate(threat *Threat) *models.Threat {
	var id string
	var threatModelID string
	if threat.Id != nil {
		id = threat.Id.String()
	}
	if threat.ThreatModelId != nil {
		threatModelID = threat.ThreatModelId.String()
	}

	// Set timestamps explicitly to avoid Oracle RETURNING INTO clause issues
	now := time.Now().UTC()

	gm := &models.Threat{
		ID:            id,
		ThreatModelID: threatModelID,
		Name:          threat.Name,
		ThreatType:    models.StringArray(threat.ThreatType),
		CreatedAt:     now,
		ModifiedAt:    now,
	}
	if threat.Description != nil {
		gm.Description = threat.Description
	}
	if threat.Severity != nil {
		gm.Severity = threat.Severity
	}
	if threat.Mitigation != nil {
		gm.Mitigation = threat.Mitigation
	}
	if threat.Status != nil {
		gm.Status = threat.Status
	}
	if threat.Priority != nil {
		gm.Priority = threat.Priority
	}
	if threat.Mitigated != nil {
		gm.Mitigated = models.OracleBool(*threat.Mitigated)
	}
	if threat.Score != nil {
		score64 := float64(*threat.Score)
		gm.Score = &score64
	}
	if threat.IssueUri != nil {
		gm.IssueURI = threat.IssueUri
	}
	if threat.DiagramId != nil {
		diagID := threat.DiagramId.String()
		gm.DiagramID = &diagID
	}
	if threat.CellId != nil {
		cellID := threat.CellId.String()
		gm.CellID = &cellID
	}
	if threat.AssetId != nil {
		assetID := threat.AssetId.String()
		gm.AssetID = &assetID
	}

	return gm
}

// toGormModel converts an API Threat to a GORM model for UPDATE operations.
// This version sets timestamps from the API model for updates.
func (s *GormThreatStore) toGormModel(threat *Threat) *models.Threat {
	gm := s.toGormModelForCreate(threat)
	if threat.CreatedAt != nil {
		gm.CreatedAt = *threat.CreatedAt
	}
	if threat.ModifiedAt != nil {
		gm.ModifiedAt = *threat.ModifiedAt
	}
	return gm
}

// toAPIModel converts a GORM Threat model to an API model
func (s *GormThreatStore) toAPIModel(gm *models.Threat) *Threat {
	mitigatedBool := gm.Mitigated.Bool()
	threat := &Threat{
		Name:       gm.Name,
		ThreatType: []string(gm.ThreatType),
		Mitigated:  &mitigatedBool,
		CreatedAt:  &gm.CreatedAt,
		ModifiedAt: &gm.ModifiedAt,
	}

	if gm.ID != "" {
		if id, err := uuid.Parse(gm.ID); err == nil {
			threat.Id = &id
		}
	}
	if gm.ThreatModelID != "" {
		if tmID, err := uuid.Parse(gm.ThreatModelID); err == nil {
			threat.ThreatModelId = &tmID
		}
	}
	if gm.Description != nil {
		threat.Description = gm.Description
	}
	if gm.Severity != nil {
		threat.Severity = gm.Severity
	}
	if gm.Mitigation != nil {
		threat.Mitigation = gm.Mitigation
	}
	if gm.Status != nil {
		threat.Status = gm.Status
	}
	if gm.Priority != nil {
		threat.Priority = gm.Priority
	}
	if gm.Score != nil {
		score32 := float32(*gm.Score)
		threat.Score = &score32
	}
	if gm.IssueURI != nil {
		threat.IssueUri = gm.IssueURI
	}
	if gm.DiagramID != nil {
		if diagID, err := uuid.Parse(*gm.DiagramID); err == nil {
			threat.DiagramId = &diagID
		}
	}
	if gm.CellID != nil {
		if cellID, err := uuid.Parse(*gm.CellID); err == nil {
			threat.CellId = &cellID
		}
	}
	if gm.AssetID != nil {
		if assetID, err := uuid.Parse(*gm.AssetID); err == nil {
			threat.AssetId = &assetID
		}
	}

	return threat
}
