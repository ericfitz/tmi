package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormThreatRepository implements ThreatStore with GORM for database persistence and Redis caching
type GormThreatRepository struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewGormThreatRepository creates a new GORM-backed threat repository with caching
func NewGormThreatRepository(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormThreatRepository {
	return &GormThreatRepository{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new threat with write-through caching using GORM
func (s *GormThreatRepository) Create(ctx context.Context, threat *Threat) error {
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
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		alias, err := AllocateNextAlias(ctx, tx, gormThreat.ThreatModelID, "threat")
		if err != nil {
			return fmt.Errorf("allocate threat alias: %w", err)
		}
		gormThreat.Alias = alias
		if err := tx.Create(gormThreat).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		logger.Error("Failed to create threat in database: %v", err)
		return err
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
func (s *GormThreatRepository) Get(ctx context.Context, id string) (*Threat, error) {
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
	if err := s.db.WithContext(ctx).First(&gormThreat, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrThreatNotFound
		}
		logger.Error("Failed to get threat from database: %v", err)
		return nil, dberrors.Classify(err)
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
func (s *GormThreatRepository) Update(ctx context.Context, threat *Threat) error {
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

	// Build update map with ALL fields included unconditionally.
	// Map-based Updates() writes nil values as NULL, unlike struct-based Updates()
	// which skips Go zero-value fields. Custom types are serialized explicitly.
	updates := s.buildThreatUpdateMap(threat, now)

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.Threat{}).Where("id = ?", threat.Id.String()).Updates(updates)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrThreatNotFound
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrThreatNotFound) {
			logger.Error("Failed to update threat in database: %v", err)
		}
		return err
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

// Delete soft-deletes a threat by setting deleted_at
func (s *GormThreatRepository) Delete(ctx context.Context, id string) error {
	return s.SoftDelete(ctx, id)
}

// hardDeleteThreat permanently removes a threat and invalidates related caches using GORM
func (s *GormThreatRepository) hardDeleteThreat(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting threat: %s", id)

	// Get the threat first to get parent info for cache invalidation
	threat, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete from database (with retry)
	err = authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Delete(&models.Threat{}, "id = ?", id)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrThreatNotFound
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrThreatNotFound) {
			logger.Error("Failed to delete threat from database: %v", err)
		}
		return err
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
// Returns: items, total count (before pagination), error
func (s *GormThreatRepository) List(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, int, error) {
	logger := slogging.Get()
	logger.Debug("Listing threats for threat model %s with advanced filters", threatModelID)

	// Get total count first (before pagination)
	total, err := s.countWithFilter(ctx, threatModelID, filter)
	if err != nil {
		logger.Warn("Failed to count threats: %v", err)
		total = 0 // Continue with items, total will be 0
	}

	// Check if we should use cache
	useCache := s.shouldUseCache(filter)

	// Try cache first for simple queries
	if useCache {
		if threats, err := s.tryGetFromCache(ctx, threatModelID, filter); err == nil && threats != nil {
			return threats, total, nil
		}
	}

	// Build and execute query
	threats, err := s.executeListQuery(ctx, threatModelID, filter)
	if err != nil {
		return nil, 0, err
	}

	// Cache the result only for simple queries
	if useCache && s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "threats", threatModelID, filter.Offset, filter.Limit, threats); cacheErr != nil {
			logger.Error("Failed to cache threat list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d threats (total: %d)", len(threats), total)
	return threats, total, nil
}

// countWithFilter counts threats matching the filter (without pagination)
func (s *GormThreatRepository) countWithFilter(ctx context.Context, threatModelID string, filter ThreatFilter) (int, error) {
	query := s.db.WithContext(ctx).Model(&models.Threat{})
	if includeDeletedFromContext(ctx) {
		query = query.Where("threat_model_id = ?", threatModelID)
	} else {
		query = query.Where("threat_model_id = ? AND deleted_at IS NULL", threatModelID)
	}
	query = s.applyFilters(query, filter)

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

// executeListQuery builds and executes the GORM query for listing threats
func (s *GormThreatRepository) executeListQuery(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.Threat{})
	if includeDeletedFromContext(ctx) {
		query = query.Where("threat_model_id = ?", threatModelID)
	} else {
		query = query.Where("threat_model_id = ? AND deleted_at IS NULL", threatModelID)
	}

	// Apply filters
	query = s.applyFilters(query, filter)

	// Apply sorting
	orderBy := DefaultSortOrderCreatedAtDesc
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
		return nil, dberrors.Classify(err)
	}

	// Convert to API models
	threats := make([]Threat, 0, len(gormThreats))
	for _, gt := range gormThreats {
		threats = append(threats, *s.toAPIModel(&gt))
	}

	return threats, nil
}

// applyFilters applies the filter conditions to the GORM query
func (s *GormThreatRepository) applyFilters(query *gorm.DB, filter ThreatFilter) *gorm.DB {
	// Text filters - use LOWER() for cross-database case-insensitive search
	if filter.Name != nil {
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+*filter.Name+"%")
	}

	if filter.Description != nil {
		query = query.Where("LOWER(description) LIKE LOWER(?)", "%"+*filter.Description+"%")
	}

	// Enum filters
	if len(filter.ThreatType) > 0 {
		// OR logic: return threats matching ANY of the specified types
		orConditions := make([]string, len(filter.ThreatType))
		orArgs := make([]interface{}, len(filter.ThreatType))
		for i, tt := range filter.ThreatType {
			orConditions[i] = "threat_type LIKE ?"
			orArgs[i] = "%\"" + tt + "\"%"
		}
		query = query.Where(strings.Join(orConditions, " OR "), orArgs...)
	}

	if len(filter.Severity) == 1 {
		query = query.Where("severity = ?", filter.Severity[0])
	} else if len(filter.Severity) > 1 {
		query = query.Where("severity IN ?", filter.Severity)
	}

	if len(filter.Priority) == 1 {
		query = query.Where("priority = ?", filter.Priority[0])
	} else if len(filter.Priority) > 1 {
		query = query.Where("priority IN ?", filter.Priority)
	}

	if len(filter.Status) == 1 {
		query = query.Where("status = ?", filter.Status[0])
	} else if len(filter.Status) > 1 {
		query = query.Where("status IN ?", filter.Status)
	}

	if filter.Mitigated != nil {
		query = query.Where("mitigated = ?", *filter.Mitigated)
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

// severityOrder maps severity values to their semantic rank for sorting.
// Higher rank = more severe. Unknown values sort to 0 (lowest).
var severityOrder = map[string]int{
	"unknown":       0,
	"informational": 1,
	"low":           2,
	"medium":        3,
	"high":          4,
	"critical":      5,
}

// priorityOrder maps priority values to their semantic rank for sorting.
// Higher rank = more urgent.
var priorityOrder = map[string]int{
	"deferred":  0,
	"low":       1,
	"medium":    2,
	"high":      3,
	"immediate": 4,
}

// statusOrder maps threat status values to their workflow progression for sorting.
var statusOrder = map[string]int{
	"identified":     0,
	"investigating":  1,
	"in_progress":    2,
	"mitigated":      3,
	"resolved":       4,
	"accepted":       5,
	"false_positive": 6,
}

// semanticOrderMaps maps column names to their ordinal ranking maps.
var semanticOrderMaps = map[string]map[string]int{
	"severity": severityOrder,
	"priority": priorityOrder,
	"status":   statusOrder,
}

// buildSemanticOrderExpr builds a CASE WHEN SQL expression for semantic sorting.
// Values not in the map sort to -1 (before all known values).
func buildSemanticOrderExpr(column string, orderMap map[string]int, dialectName string) string {
	col := ColumnName(dialectName, column)
	var b strings.Builder
	b.WriteString("CASE")
	for value, rank := range orderMap {
		fmt.Fprintf(&b, " WHEN LOWER(%s) = '%s' THEN %d", col, value, rank)
	}
	b.WriteString(" ELSE -1 END")
	return b.String()
}

// buildOrderBy constructs a safe ORDER BY clause from sort parameter.
// For severity, priority, and status fields, it generates a CASE WHEN
// expression that sorts by semantic rank instead of alphabetical order.
func (s *GormThreatRepository) buildOrderBy(sort string) string {
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
		return DefaultSortOrderCreatedAtDesc
	}

	column, direction := parts[0], strings.ToUpper(parts[1])

	safeColumn, exists := validColumns[column]
	if !exists {
		return DefaultSortOrderCreatedAtDesc
	}

	if direction != SortDirectionASC && direction != SortDirectionDESC {
		direction = SortDirectionDESC
	}

	// Use semantic ordering for enum-like fields
	if orderMap, ok := semanticOrderMaps[safeColumn]; ok {
		dialectName := GetDialectName(s.db)
		expr := buildSemanticOrderExpr(safeColumn, orderMap, dialectName)
		return expr + " " + direction
	}

	return ColumnName(GetDialectName(s.db), safeColumn) + " " + direction
}

// Patch applies JSON patch operations to a threat using GORM
func (s *GormThreatRepository) Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error) {
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
func (s *GormThreatRepository) applyPatchOperation(threat *Threat, op PatchOperation) error {
	switch op.Path {
	case PatchPathName:
		if op.Op == string(Replace) {
			if name, ok := op.Value.(string); ok {
				threat.Name = name
				return nil
			}
			return fmt.Errorf("invalid value type for name: expected string")
		}
	case PatchPathDescription:
		switch op.Op {
		case string(Replace), string(Add):
			if desc, ok := op.Value.(string); ok {
				threat.Description = &desc
				return nil
			}
			return fmt.Errorf("invalid value type for description: expected string")
		case string(Remove):
			threat.Description = nil
		}
	case "/severity":
		if op.Op == string(Replace) {
			if sev, ok := op.Value.(string); ok {
				normalized := normalizeSeverity(sev)
				threat.Severity = &normalized
				return nil
			}
			return fmt.Errorf("invalid value type for severity: expected string")
		}
	case "/mitigation":
		switch op.Op {
		case string(Replace), string(Add):
			if mit, ok := op.Value.(string); ok {
				threat.Mitigation = &mit
				return nil
			}
			return fmt.Errorf("invalid value type for mitigation: expected string")
		case string(Remove):
			threat.Mitigation = nil
		}
	case PatchPathStatus:
		if op.Op == string(Replace) {
			if status, ok := op.Value.(string); ok {
				threat.Status = &status
				return nil
			}
			return fmt.Errorf("invalid value type for status: expected string")
		}
	case "/priority":
		if op.Op == string(Replace) {
			if priority, ok := op.Value.(string); ok {
				threat.Priority = &priority
				return nil
			}
			return fmt.Errorf("invalid value type for priority: expected string")
		}
	case "/mitigated":
		if op.Op == string(Replace) {
			if mitigated, ok := op.Value.(bool); ok {
				threat.Mitigated = &mitigated
				return nil
			}
			return fmt.Errorf("invalid value type for mitigated: expected boolean")
		}
	case "/score":
		switch op.Op {
		case string(Replace), string(Add):
			if score, ok := op.Value.(float64); ok {
				score32 := float32(score)
				threat.Score = &score32
				return nil
			}
			return fmt.Errorf("invalid value type for score: expected number")
		case string(Remove):
			threat.Score = nil
		}
	case "/threat_type":
		return s.patchThreatTypeGorm(threat, op)
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

func (s *GormThreatRepository) patchThreatTypeGorm(threat *Threat, op PatchOperation) error {
	switch op.Op {
	case string(Replace):
		if types, ok := op.Value.([]any); ok {
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
	case string(Add):
		if newType, ok := op.Value.(string); ok {
			if slices.Contains(threat.ThreatType, newType) {
				return nil
			}
			threat.ThreatType = append(threat.ThreatType, newType)
			return nil
		}
		return fmt.Errorf("threat_type add requires string value")
	case string(Remove):
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
func (s *GormThreatRepository) BulkCreate(ctx context.Context, threats []Threat) error {
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

	// Create all in a transaction (with retry)
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(&gormThreats).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})

	if err != nil {
		logger.Error("Failed to bulk create threats: %v", err)
		return err
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
func (s *GormThreatRepository) BulkUpdate(ctx context.Context, threats []Threat) error {
	logger := slogging.Get()
	logger.Debug("Bulk updating %d threats", len(threats))

	if len(threats) == 0 {
		return nil
	}

	now := time.Now().UTC()
	var parentThreatModelID string

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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

			updates := s.buildThreatUpdateMap(threat, now)
			if err := tx.Model(&models.Threat{}).Where("id = ?", threat.Id.String()).Updates(updates).Error; err != nil {
				return dberrors.Classify(err)
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
		return err
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
func (s *GormThreatRepository) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateEntity(ctx, "threat", id)
}

// WarmCache preloads threats for a threat model into cache
func (s *GormThreatRepository) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	filter := ThreatFilter{Offset: 0, Limit: 50}
	_, _, err := s.List(ctx, threatModelID, filter)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	logger.Debug("Warmed cache for threat model %s", threatModelID)
	return nil
}

// loadMetadata loads metadata for a threat using GORM
func (s *GormThreatRepository) loadMetadata(ctx context.Context, threatID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db.WithContext(ctx), "threat", threatID)
}

// saveMetadata saves metadata for a threat using GORM
func (s *GormThreatRepository) saveMetadata(ctx context.Context, threatID string, metadata *[]Metadata) error {
	return s.saveMetadataTx(s.db.WithContext(ctx), threatID, metadata)
}

// saveMetadataTx saves metadata within a transaction
func (s *GormThreatRepository) saveMetadataTx(tx *gorm.DB, threatID string, metadata *[]Metadata) error {
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

			// Use Col()/ColumnName() so the Oracle GORM driver receives
			// uppercase column identifiers when emitting MERGE INTO.
			dialect := tx.Name()
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					Col(dialect, "entity_type"),
					Col(dialect, "entity_id"),
					Col(dialect, "key"),
				},
				DoUpdates: clause.AssignmentColumns([]string{
					ColumnName(dialect, "value"),
					ColumnName(dialect, "modified_at"),
				}),
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
func (s *GormThreatRepository) shouldUseCache(filter ThreatFilter) bool {
	return filter.Name == nil && filter.Description == nil && len(filter.ThreatType) == 0 &&
		len(filter.Severity) == 0 && len(filter.Priority) == 0 && len(filter.Status) == 0 &&
		filter.Mitigated == nil && filter.DiagramID == nil && filter.CellID == nil &&
		filter.ScoreGT == nil && filter.ScoreLT == nil && filter.ScoreEQ == nil &&
		filter.ScoreGE == nil && filter.ScoreLE == nil &&
		filter.CreatedAfter == nil && filter.CreatedBefore == nil &&
		filter.ModifiedAfter == nil && filter.ModifiedBefore == nil &&
		filter.Sort == nil
}

// tryGetFromCache attempts to retrieve threats from cache
func (s *GormThreatRepository) tryGetFromCache(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
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

// convertScore converts a *float32 to *float64 for database storage
func (s *GormThreatRepository) convertScore(score *float32) *float64 {
	if score == nil {
		return nil
	}
	s64 := float64(*score)
	return &s64
}

// convertUUIDToString converts a *uuid.UUID to *string, returning nil if the UUID is nil
func (s *GormThreatRepository) convertUUIDToString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	str := id.String()
	return &str
}

// buildThreatUpdateMap builds a map[string]any with ALL fields included unconditionally
// for use with GORM map-based Updates(). This ensures nil/zero-value fields are written
// as NULL to the database, unlike struct-based Updates() which skips zero values.
// Custom types (StringArray, CVSSArray, DBBool) are handled explicitly since map-based
// Updates() bypasses GORM's Value() methods.
func (s *GormThreatRepository) buildThreatUpdateMap(threat *Threat, now time.Time) map[string]any {
	// Handle boolean fields: default to false if nil
	mitigated := models.DBBool(false)
	if threat.Mitigated != nil {
		mitigated = models.DBBool(*threat.Mitigated)
	}
	includeInReport := models.DBBool(false)
	if threat.IncludeInReport != nil {
		includeInReport = models.DBBool(*threat.IncludeInReport)
	}
	timmyEnabled := models.DBBool(false)
	if threat.TimmyEnabled != nil {
		timmyEnabled = models.DBBool(*threat.TimmyEnabled)
	}

	// Handle array types: use empty arrays for nil/empty values
	threatType := models.StringArray(threat.ThreatType)
	if threatType == nil {
		threatType = models.StringArray{}
	}

	var cweID models.StringArray
	if threat.CweId != nil && len(*threat.CweId) > 0 {
		cweID = models.StringArray(*threat.CweId)
	} else {
		cweID = models.StringArray{}
	}

	var cvss models.CVSSArray
	if threat.Cvss != nil && len(*threat.Cvss) > 0 {
		cvss = make(models.CVSSArray, len(*threat.Cvss))
		for i, c := range *threat.Cvss {
			cvss[i] = models.CVSSScore{
				Vector: c.Vector,
				Score:  float64(c.Score),
			}
		}
	} else {
		cvss = models.CVSSArray{}
	}

	// Handle SSVC: convert to NullableSSVC for serialization
	ssvc := models.NullableSSVC{}
	if threat.Ssvc != nil {
		ssvc = models.NullableSSVC{
			SSVCScore: models.SSVCScore{
				Vector:      threat.Ssvc.Vector,
				Decision:    string(threat.Ssvc.Decision),
				Methodology: threat.Ssvc.Methodology,
			},
			Valid: true,
		}
	}

	// Serialize custom types manually since map-based Updates() bypasses Value() methods
	threatTypeVal, _ := threatType.Value()
	cweIDVal, _ := cweID.Value()
	cvssVal, _ := cvss.Value()
	mitigatedVal, _ := mitigated.Value()
	includeInReportVal, _ := includeInReport.Value()
	timmyEnabledVal, _ := timmyEnabled.Value()
	ssvcVal, _ := ssvc.Value()

	return map[string]any{
		"name":              threat.Name,
		"threat_model_id":   threat.ThreatModelId.String(),
		"description":       threat.Description,                      // nil writes NULL
		"severity":          threat.Severity,                         // nil writes NULL
		"mitigation":        threat.Mitigation,                       // nil writes NULL
		"status":            threat.Status,                           // nil writes NULL
		"priority":          threat.Priority,                         // nil writes NULL
		"issue_uri":         threat.IssueUri,                         // nil writes NULL
		"score":             s.convertScore(threat.Score),            // nil writes NULL
		"diagram_id":        s.convertUUIDToString(threat.DiagramId), // nil writes NULL
		"cell_id":           s.convertUUIDToString(threat.CellId),    // nil writes NULL
		"asset_id":          s.convertUUIDToString(threat.AssetId),   // nil writes NULL
		"threat_type":       threatTypeVal,
		"cwe_id":            cweIDVal,
		"cvss":              cvssVal,
		"ssvc":              ssvcVal, // nil writes NULL
		"mitigated":         mitigatedVal,
		"include_in_report": includeInReportVal,
		"timmy_enabled":     timmyEnabledVal,
		"modified_at":       now,
	}
}

// toGormModelForCreate converts an API Threat to a GORM model for CREATE operations.
// Timestamps are set explicitly to ensure compatibility across all database backends.
func (s *GormThreatRepository) toGormModelForCreate(threat *Threat) *models.Threat {
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
		gm.Mitigated = models.DBBool(*threat.Mitigated)
	}
	if threat.IncludeInReport != nil {
		gm.IncludeInReport = models.DBBool(*threat.IncludeInReport)
	}
	if threat.TimmyEnabled != nil {
		gm.TimmyEnabled = models.DBBool(*threat.TimmyEnabled)
	}
	if threat.AutoGenerated != nil {
		gm.AutoGenerated = models.DBBool(*threat.AutoGenerated)
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
	if threat.CweId != nil && len(*threat.CweId) > 0 {
		gm.CweID = models.StringArray(*threat.CweId)
	}
	if threat.Cvss != nil && len(*threat.Cvss) > 0 {
		cvssArray := make(models.CVSSArray, len(*threat.Cvss))
		for i, c := range *threat.Cvss {
			cvssArray[i] = models.CVSSScore{
				Vector: c.Vector,
				Score:  float64(c.Score),
			}
		}
		gm.Cvss = cvssArray
	}
	if threat.Ssvc != nil {
		gm.Ssvc = models.NullableSSVC{
			SSVCScore: models.SSVCScore{
				Vector:      threat.Ssvc.Vector,
				Decision:    string(threat.Ssvc.Decision),
				Methodology: threat.Ssvc.Methodology,
			},
			Valid: true,
		}
	}

	return gm
}

// toAPIModel converts a GORM Threat model to an API model
func (s *GormThreatRepository) toAPIModel(gm *models.Threat) *Threat {
	mitigatedBool := gm.Mitigated.Bool()
	includeInReport := gm.IncludeInReport.Bool()
	timmyEnabled := gm.TimmyEnabled.Bool()
	autoGenerated := gm.AutoGenerated.Bool()
	alias := gm.Alias
	threat := &Threat{
		Name:            gm.Name,
		ThreatType:      []string(gm.ThreatType),
		Mitigated:       &mitigatedBool,
		IncludeInReport: &includeInReport,
		TimmyEnabled:    &timmyEnabled,
		AutoGenerated:   &autoGenerated,
		CreatedAt:       &gm.CreatedAt,
		ModifiedAt:      &gm.ModifiedAt,
		Alias:           &alias,
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
	if len(gm.CweID) > 0 {
		cweSlice := []string(gm.CweID)
		threat.CweId = &cweSlice
	}
	if len(gm.Cvss) > 0 {
		cvssSlice := make([]CVSSScore, len(gm.Cvss))
		for i, c := range gm.Cvss {
			cvssSlice[i] = CVSSScore{
				Vector: c.Vector,
				Score:  float32(c.Score),
			}
		}
		threat.Cvss = &cvssSlice
	}
	if gm.Ssvc.Valid {
		decision := SSVCScoreDecision(gm.Ssvc.Decision)
		threat.Ssvc = &SSVCScore{
			Vector:      gm.Ssvc.Vector,
			Decision:    decision,
			Methodology: gm.Ssvc.Methodology,
		}
	}

	return threat
}
