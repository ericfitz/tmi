package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/google/uuid"
)

// ThreatFilter defines filtering criteria for threats
type ThreatFilter struct {
	// Basic filters
	Name        *string
	Description *string
	ThreatType  *string
	Severity    *ThreatSeverity
	Priority    *string
	Status      *string
	DiagramID   *uuid.UUID
	CellID      *uuid.UUID

	// Score comparison filters
	ScoreGT *float32
	ScoreLT *float32
	ScoreEQ *float32
	ScoreGE *float32
	ScoreLE *float32

	// Date filters
	CreatedAfter   *time.Time
	CreatedBefore  *time.Time
	ModifiedAfter  *time.Time
	ModifiedBefore *time.Time

	// Sorting and pagination
	Sort   *string
	Offset int
	Limit  int
}

// normalizeSeverity converts severity values to the standardized case-sensitive format
func normalizeSeverity(severity ThreatSeverity) ThreatSeverity {
	switch strings.ToLower(string(severity)) {
	case "low":
		return "Low"
	case "medium":
		return "Medium"
	case "high":
		return "High"
	case "critical":
		return "Critical"
	case "unknown":
		return "Unknown"
	case "none":
		return "None"
	default:
		// If no match, return as-is (likely already in correct format)
		return severity
	}
}

// ThreatStore defines the interface for threat operations with caching support
type ThreatStore interface {
	// CRUD operations
	Create(ctx context.Context, threat *Threat) error
	Get(ctx context.Context, id string) (*Threat, error)
	Update(ctx context.Context, threat *Threat) error
	Delete(ctx context.Context, id string) error

	// List operations with filtering, sorting and pagination
	List(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error)

	// PATCH operations for granular updates
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error)

	// Bulk operations
	BulkCreate(ctx context.Context, threats []Threat) error
	BulkUpdate(ctx context.Context, threats []Threat) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}

// DatabaseThreatStore implements ThreatStore with database persistence and Redis caching
type DatabaseThreatStore struct {
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDatabaseThreatStore creates a new database-backed threat store with caching
func NewDatabaseThreatStore(db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DatabaseThreatStore {
	return &DatabaseThreatStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new threat with write-through caching
func (s *DatabaseThreatStore) Create(ctx context.Context, threat *Threat) error {
	logger := slogging.Get()
	logger.Debug("Creating threat: %s in threat model: %s", threat.Name, threat.ThreatModelId)

	// Generate UUIDv7 ID if not provided (for better index locality)
	if threat.Id == nil {
		id := uuidgen.MustNewForEntity(uuidgen.EntityTypeThreat)
		threat.Id = &id
	}

	// Set timestamps
	now := time.Now().UTC()
	threat.CreatedAt = &now
	threat.ModifiedAt = &now

	// Normalize severity to standardized case
	threat.Severity = normalizeSeverity(threat.Severity)

	// Insert into database
	query := `
		INSERT INTO threats (
			id, threat_model_id, name, description, severity,
			mitigation, threat_type, status, priority, mitigated,
			score, issue_uri, diagram_id, cell_id, asset_id,
			created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)
	`

	_, err := s.db.ExecContext(ctx, query,
		threat.Id,
		threat.ThreatModelId,
		threat.Name,
		threat.Description,
		string(threat.Severity),
		threat.Mitigation,
		threat.ThreatType,
		threat.Status,
		threat.Priority,
		threat.Mitigated,
		threat.Score,
		threat.IssueUri,
		threat.DiagramId,
		threat.CellId,
		threat.AssetId,
		threat.CreatedAt,
		threat.ModifiedAt,
	)

	if err != nil {
		logger.Error("Failed to create threat in database: %v", err)
		return fmt.Errorf("failed to create threat: %w", err)
	}

	// Cache the new threat
	if s.cache != nil {
		if cacheErr := s.cache.CacheThreat(ctx, threat); cacheErr != nil {
			logger.Error("Failed to cache new threat: %v", cacheErr)
			// Don't fail the request if caching fails
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

// Get retrieves a threat by ID with cache-first strategy
func (s *DatabaseThreatStore) Get(ctx context.Context, id string) (*Threat, error) {
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

	query := `
		SELECT id, threat_model_id, name, description, severity,
			   mitigation, threat_type, status, priority, mitigated,
			   score, issue_uri, diagram_id, cell_id, asset_id, created_at, modified_at
		FROM threats
		WHERE id = $1
	`

	var threat Threat
	var description, mitigation, issueUrl sql.NullString
	var score sql.NullFloat64
	var diagramId, cellId, assetId sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&threat.Id,
		&threat.ThreatModelId,
		&threat.Name,
		&description,
		&threat.Severity,
		&mitigation,
		&threat.ThreatType,
		&threat.Status,
		&threat.Priority,
		&threat.Mitigated,
		&score,
		&issueUrl,
		&diagramId,
		&cellId,
		&assetId,
		&threat.CreatedAt,
		&threat.ModifiedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("threat not found: %s", id)
		}
		logger.Error("Failed to get threat from database: %v", err)
		return nil, fmt.Errorf("failed to get threat: %w", err)
	}

	// Handle nullable fields
	if description.Valid {
		threat.Description = &description.String
	}
	if mitigation.Valid {
		threat.Mitigation = &mitigation.String
	}
	if issueUrl.Valid {
		threat.IssueUri = &issueUrl.String
	}
	if score.Valid {
		score32 := float32(score.Float64)
		threat.Score = &score32
	}
	if diagramId.Valid {
		if diagID, err := uuid.Parse(diagramId.String); err == nil {
			threat.DiagramId = &diagID
		}
	}
	if cellId.Valid {
		if cID, err := uuid.Parse(cellId.String); err == nil {
			threat.CellId = &cID
		}
	}
	if assetId.Valid {
		if aID, err := uuid.Parse(assetId.String); err == nil {
			threat.AssetId = &aID
		}
	}
	// Load metadata from the metadata table
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for threat %s: %v", id, err)
		// Don't fail the request if metadata loading fails, just set empty metadata
		metadata = []Metadata{}
	}
	threat.Metadata = &metadata

	// Cache the result for future requests
	if s.cache != nil {
		if cacheErr := s.cache.CacheThreat(ctx, &threat); cacheErr != nil {
			logger.Error("Failed to cache threat after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved threat: %s", id)
	return &threat, nil
}

// Update updates an existing threat with write-through caching
func (s *DatabaseThreatStore) Update(ctx context.Context, threat *Threat) error {
	logger := slogging.Get()
	logger.Debug("Updating threat: %s", threat.Id)

	// Update modified timestamp
	now := time.Now().UTC()
	threat.ModifiedAt = &now

	// Normalize severity to standardized case
	threat.Severity = normalizeSeverity(threat.Severity)

	// Serialize metadata if present
	var metadataJSON sql.NullString
	if threat.Metadata != nil && len(*threat.Metadata) > 0 {
		metadataBytes, err := json.Marshal(*threat.Metadata)
		if err != nil {
			logger.Error("Failed to marshal threat metadata: %v", err)
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON.String = string(metadataBytes)
		metadataJSON.Valid = true
	}

	query := `
		UPDATE threats SET
			name = $2, description = $3, severity = $4, mitigation = $5,
			threat_type = $6, status = $7, priority = $8, mitigated = $9,
			score = $10, issue_uri = $11, diagram_id = $12, cell_id = $13,
			asset_id = $14, metadata = $15, modified_at = $16
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query,
		threat.Id,
		threat.Name,
		threat.Description,
		string(threat.Severity),
		threat.Mitigation,
		threat.ThreatType,
		threat.Status,
		threat.Priority,
		threat.Mitigated,
		threat.Score,
		threat.IssueUri,
		threat.DiagramId,
		threat.CellId,
		threat.AssetId,
		metadataJSON,
		threat.ModifiedAt,
	)

	if err != nil {
		logger.Error("Failed to update threat in database: %v", err)
		return fmt.Errorf("failed to update threat: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify update: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("threat not found: %s", threat.Id)
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

// Delete removes a threat and invalidates related caches
func (s *DatabaseThreatStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting threat: %s", id)

	// Get the threat first to get parent info for cache invalidation
	threat, err := s.Get(ctx, id)
	if err != nil {
		return err // Threat not found or database error
	}

	// Delete from database
	query := `DELETE FROM threats WHERE id = $1`
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		logger.Error("Failed to delete threat from database: %v", err)
		return fmt.Errorf("failed to delete threat: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify deletion: %w", err)
	}

	if rowsAffected == 0 {
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

// List retrieves threats for a threat model with advanced filtering, sorting and pagination
func (s *DatabaseThreatStore) List(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
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

// executeListQuery builds and executes the database query for listing threats
func (s *DatabaseThreatStore) executeListQuery(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
	logger := slogging.Get()

	// Build query
	query, args := s.buildListQuery(threatModelID, filter)

	logger.Debug("Executing threat query: %s", query)
	logger.Debug("With args: %v", args)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		logger.Error("Failed to query threats from database: %v", err)
		return nil, fmt.Errorf("failed to list threats: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	return s.scanThreatRows(rows)
}

// buildOrderBy constructs a safe ORDER BY clause from sort parameter
func (s *DatabaseThreatStore) buildOrderBy(sort string) string {
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

	// Parse sort parameter (e.g., "created_at:desc" or "name:asc")
	parts := strings.Split(sort, ":")
	if len(parts) != 2 {
		return "created_at DESC" // fallback to default
	}

	column, direction := parts[0], strings.ToUpper(parts[1])

	// Validate column name
	safeColumn, exists := validColumns[column]
	if !exists {
		return "created_at DESC" // fallback to default
	}

	// Validate direction
	if direction != "ASC" && direction != "DESC" {
		direction = "DESC"
	}

	return safeColumn + " " + direction
}

// Patch applies JSON patch operations to a threat
func (s *DatabaseThreatStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error) {
	logger := slogging.Get()
	logger.Debug("Patching threat %s with %d operations", id, len(operations))

	// Get current threat
	threat, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
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
func (s *DatabaseThreatStore) applyPatchOperation(threat *Threat, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				threat.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				threat.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case "remove":
			threat.Description = nil
		}
	case "/severity":
		if op.Op == "replace" {
			if sev, ok := op.Value.(string); ok {
				threat.Severity = normalizeSeverity(ThreatSeverity(sev))
			} else {
				return fmt.Errorf("invalid value type for severity: expected string")
			}
		}
	case "/mitigation":
		switch op.Op {
		case "replace", "add":
			if mit, ok := op.Value.(string); ok {
				threat.Mitigation = &mit
			} else {
				return fmt.Errorf("invalid value type for mitigation: expected string")
			}
		case "remove":
			threat.Mitigation = nil
		}
	case "/status":
		if op.Op == "replace" {
			if status, ok := op.Value.(string); ok {
				threat.Status = status
			} else {
				return fmt.Errorf("invalid value type for status: expected string")
			}
		}
	case "/priority":
		if op.Op == "replace" {
			if priority, ok := op.Value.(string); ok {
				threat.Priority = priority
			} else {
				return fmt.Errorf("invalid value type for priority: expected string")
			}
		}
	case "/mitigated":
		if op.Op == "replace" {
			if mitigated, ok := op.Value.(bool); ok {
				threat.Mitigated = mitigated
			} else {
				return fmt.Errorf("invalid value type for mitigated: expected boolean")
			}
		}
	case "/score":
		switch op.Op {
		case "replace", "add":
			if score, ok := op.Value.(float64); ok {
				score32 := float32(score)
				threat.Score = &score32
			} else {
				return fmt.Errorf("invalid value type for score: expected number")
			}
		case "remove":
			threat.Score = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}

	return nil
}

// BulkCreate creates multiple threats in a single transaction
func (s *DatabaseThreatStore) BulkCreate(ctx context.Context, threats []Threat) error {
	logger := slogging.Get()
	logger.Debug("Bulk creating %d threats", len(threats))

	if len(threats) == 0 {
		return nil
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		logger.Error("Failed to begin transaction: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logger.Error("Failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	query := `
		INSERT INTO threats (
			id, threat_model_id, name, description, severity,
			mitigation, threat_type, status, priority, mitigated,
			score, issue_uri, diagram_id, cell_id, asset_id, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		logger.Error("Failed to prepare bulk insert statement: %v", err)
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			logger.Error("Failed to close statement: %v", closeErr)
		}
	}()

	now := time.Now().UTC()
	var parentThreatModelID string

	for i := range threats {
		threat := &threats[i]

		// Generate ID if not provided
		if threat.Id == nil {
			id := uuid.New()
			threat.Id = &id
		}

		// Set timestamps
		threat.CreatedAt = &now
		threat.ModifiedAt = &now

		// Normalize severity to standardized case
		threat.Severity = normalizeSeverity(threat.Severity)

		// Track parent for cache invalidation
		if parentThreatModelID == "" {
			parentThreatModelID = threat.ThreatModelId.String()
		}

		_, err = stmt.ExecContext(ctx,
			threat.Id,
			threat.ThreatModelId,
			threat.Name,
			threat.Description,
			string(threat.Severity),
			threat.Mitigation,
			threat.ThreatType,
			threat.Status,
			threat.Priority,
			threat.Mitigated,
			threat.Score,
			threat.IssueUri,
			threat.DiagramId,
			threat.CellId,
			threat.AssetId,
			threat.CreatedAt,
			threat.ModifiedAt,
		)

		if err != nil {
			logger.Error("Failed to execute bulk insert for threat %d: %v", i, err)
			return fmt.Errorf("failed to insert threat %d: %w", i, err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		logger.Error("Failed to commit bulk create transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
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

// BulkUpdate updates multiple threats in a single transaction
func (s *DatabaseThreatStore) BulkUpdate(ctx context.Context, threats []Threat) error {
	logger := slogging.Get()
	logger.Debug("Bulk updating %d threats", len(threats))

	if len(threats) == 0 {
		return nil
	}

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		logger.Error("Failed to begin transaction: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logger.Error("Failed to rollback transaction: %v", rollbackErr)
			}
		}
	}()

	query := `
		UPDATE threats SET
			name = $2, description = $3, severity = $4, mitigation = $5,
			threat_type = $6, status = $7, priority = $8, mitigated = $9,
			score = $10, issue_uri = $11, diagram_id = $12, cell_id = $13,
			asset_id = $14, metadata = $15, modified_at = $16
		WHERE id = $1
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		logger.Error("Failed to prepare bulk update statement: %v", err)
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			logger.Error("Failed to close statement: %v", closeErr)
		}
	}()

	now := time.Now().UTC()
	var parentThreatModelID string

	for i := range threats {
		threat := &threats[i]
		threat.ModifiedAt = &now

		// Normalize severity to standardized case
		threat.Severity = normalizeSeverity(threat.Severity)

		// Track parent for cache invalidation
		if parentThreatModelID == "" {
			parentThreatModelID = threat.ThreatModelId.String()
		}

		// Serialize metadata if present
		var metadataJSON sql.NullString
		if threat.Metadata != nil && len(*threat.Metadata) > 0 {
			if metadataBytes, err := json.Marshal(*threat.Metadata); err == nil {
				metadataJSON.String = string(metadataBytes)
				metadataJSON.Valid = true
			}
		}

		_, err = stmt.ExecContext(ctx,
			threat.Id,
			threat.Name,
			threat.Description,
			string(threat.Severity),
			threat.Mitigation,
			threat.ThreatType,
			threat.Status,
			threat.Priority,
			threat.Mitigated,
			threat.Score,
			threat.IssueUri,
			threat.DiagramId,
			threat.CellId,
			threat.AssetId,
			metadataJSON,
			threat.ModifiedAt,
		)

		if err != nil {
			logger.Error("Failed to execute bulk update for threat %d: %v", i, err)
			return fmt.Errorf("failed to update threat %d: %w", i, err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		logger.Error("Failed to commit bulk update transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
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
func (s *DatabaseThreatStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}

	return s.cache.InvalidateEntity(ctx, "threat", id)
}

// WarmCache preloads threats for a threat model into cache
func (s *DatabaseThreatStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of threats
	filter := ThreatFilter{Offset: 0, Limit: 50}
	threats, err := s.List(ctx, threatModelID, filter)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	// Individual threats are already cached by List(), so we're done
	logger.Debug("Warmed cache with %d threats for threat model %s", len(threats), threatModelID)
	return nil
}

// loadMetadata loads metadata for a threat from the metadata table
func (s *DatabaseThreatStore) loadMetadata(ctx context.Context, threatID string) ([]Metadata, error) {
	query := `
		SELECT key, value 
		FROM metadata 
		WHERE entity_type = 'threat' AND entity_id = $1
		ORDER BY key ASC
	`

	rows, err := s.db.QueryContext(ctx, query, threatID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	var metadata []Metadata
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		metadata = append(metadata, Metadata{
			Key:   key,
			Value: value,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating metadata: %w", err)
	}

	return metadata, nil
}

// Helper functions for DatabaseThreatStore to reduce cyclomatic complexity

// shouldUseCache determines if the query is simple enough to use caching
func (s *DatabaseThreatStore) shouldUseCache(filter ThreatFilter) bool {
	return filter.Name == nil && filter.Description == nil && filter.ThreatType == nil &&
		filter.Severity == nil && filter.Priority == nil && filter.Status == nil &&
		filter.DiagramID == nil && filter.CellID == nil &&
		filter.ScoreGT == nil && filter.ScoreLT == nil && filter.ScoreEQ == nil &&
		filter.ScoreGE == nil && filter.ScoreLE == nil &&
		filter.CreatedAfter == nil && filter.CreatedBefore == nil &&
		filter.ModifiedAfter == nil && filter.ModifiedBefore == nil &&
		filter.Sort == nil
}

// tryGetFromCache attempts to retrieve threats from cache
func (s *DatabaseThreatStore) tryGetFromCache(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, error) {
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

// buildListQuery constructs the SQL query with filters
func (s *DatabaseThreatStore) buildListQuery(threatModelID string, filter ThreatFilter) (string, []interface{}) {
	query := `
		SELECT id, threat_model_id, name, description, severity,
			   mitigation, threat_type, status, priority, mitigated,
			   score, issue_uri, diagram_id, cell_id, asset_id, metadata, created_at, modified_at
		FROM threats
		WHERE threat_model_id = $1`

	args := []interface{}{threatModelID}
	argIndex := 2

	// Build WHERE clause
	whereClause, newArgs, newIndex := s.buildWhereClause(filter, argIndex)
	query += whereClause
	args = append(args, newArgs...)
	argIndex = newIndex

	// Add ORDER BY clause
	orderBy := "created_at DESC"
	if filter.Sort != nil {
		orderBy = s.buildOrderBy(*filter.Sort)
	}
	query += " ORDER BY " + orderBy

	// Add LIMIT and OFFSET
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
	args = append(args, filter.Limit, filter.Offset)

	return query, args
}

// buildWhereClause builds the WHERE clause conditions
func (s *DatabaseThreatStore) buildWhereClause(filter ThreatFilter, startIndex int) (string, []interface{}, int) {
	var conditions []string
	var args []interface{}
	argIndex := startIndex

	// Text filters
	if filter.Name != nil {
		conditions = append(conditions, fmt.Sprintf(" AND name ILIKE $%d", argIndex))
		args = append(args, "%"+*filter.Name+"%")
		argIndex++
	}

	if filter.Description != nil {
		conditions = append(conditions, fmt.Sprintf(" AND description ILIKE $%d", argIndex))
		args = append(args, "%"+*filter.Description+"%")
		argIndex++
	}

	// Enum filters
	if filter.ThreatType != nil {
		conditions = append(conditions, fmt.Sprintf(" AND threat_type = $%d", argIndex))
		args = append(args, *filter.ThreatType)
		argIndex++
	}

	if filter.Severity != nil {
		conditions = append(conditions, fmt.Sprintf(" AND severity = $%d", argIndex))
		args = append(args, string(*filter.Severity))
		argIndex++
	}

	if filter.Priority != nil {
		conditions = append(conditions, fmt.Sprintf(" AND priority = $%d", argIndex))
		args = append(args, *filter.Priority)
		argIndex++
	}

	if filter.Status != nil {
		conditions = append(conditions, fmt.Sprintf(" AND status = $%d", argIndex))
		args = append(args, *filter.Status)
		argIndex++
	}

	// UUID filters
	if filter.DiagramID != nil {
		conditions = append(conditions, fmt.Sprintf(" AND diagram_id = $%d", argIndex))
		args = append(args, filter.DiagramID.String())
		argIndex++
	}

	if filter.CellID != nil {
		conditions = append(conditions, fmt.Sprintf(" AND cell_id = $%d", argIndex))
		args = append(args, filter.CellID.String())
		argIndex++
	}

	// Score filters
	scoreConditions, scoreArgs, newIndex := s.buildScoreConditions(filter, argIndex)
	conditions = append(conditions, scoreConditions...)
	args = append(args, scoreArgs...)
	argIndex = newIndex

	// Date filters
	dateConditions, dateArgs, newIndex := s.buildDateConditions(filter, argIndex)
	conditions = append(conditions, dateConditions...)
	args = append(args, dateArgs...)
	argIndex = newIndex

	return strings.Join(conditions, ""), args, argIndex
}

// buildScoreConditions builds score-related WHERE conditions
func (s *DatabaseThreatStore) buildScoreConditions(filter ThreatFilter, startIndex int) ([]string, []interface{}, int) {
	var conditions []string
	var args []interface{}
	argIndex := startIndex

	if filter.ScoreGT != nil {
		conditions = append(conditions, fmt.Sprintf(" AND score > $%d", argIndex))
		args = append(args, *filter.ScoreGT)
		argIndex++
	}

	if filter.ScoreLT != nil {
		conditions = append(conditions, fmt.Sprintf(" AND score < $%d", argIndex))
		args = append(args, *filter.ScoreLT)
		argIndex++
	}

	if filter.ScoreEQ != nil {
		conditions = append(conditions, fmt.Sprintf(" AND score = $%d", argIndex))
		args = append(args, *filter.ScoreEQ)
		argIndex++
	}

	if filter.ScoreGE != nil {
		conditions = append(conditions, fmt.Sprintf(" AND score >= $%d", argIndex))
		args = append(args, *filter.ScoreGE)
		argIndex++
	}

	if filter.ScoreLE != nil {
		conditions = append(conditions, fmt.Sprintf(" AND score <= $%d", argIndex))
		args = append(args, *filter.ScoreLE)
		argIndex++
	}

	return conditions, args, argIndex
}

// buildDateConditions builds date-related WHERE conditions
func (s *DatabaseThreatStore) buildDateConditions(filter ThreatFilter, startIndex int) ([]string, []interface{}, int) {
	var conditions []string
	var args []interface{}
	argIndex := startIndex

	if filter.CreatedAfter != nil {
		conditions = append(conditions, fmt.Sprintf(" AND created_at > $%d", argIndex))
		args = append(args, *filter.CreatedAfter)
		argIndex++
	}

	if filter.CreatedBefore != nil {
		conditions = append(conditions, fmt.Sprintf(" AND created_at < $%d", argIndex))
		args = append(args, *filter.CreatedBefore)
		argIndex++
	}

	if filter.ModifiedAfter != nil {
		conditions = append(conditions, fmt.Sprintf(" AND modified_at > $%d", argIndex))
		args = append(args, *filter.ModifiedAfter)
		argIndex++
	}

	if filter.ModifiedBefore != nil {
		conditions = append(conditions, fmt.Sprintf(" AND modified_at < $%d", argIndex))
		args = append(args, *filter.ModifiedBefore)
		argIndex++
	}

	return conditions, args, argIndex
}

// scanThreatRows scans database rows into Threat objects
func (s *DatabaseThreatStore) scanThreatRows(rows *sql.Rows) ([]Threat, error) {
	logger := slogging.Get()
	threats := make([]Threat, 0)

	for rows.Next() {
		threat, err := s.scanSingleThreat(rows)
		if err != nil {
			logger.Error("Failed to scan threat row: %v", err)
			return nil, fmt.Errorf("failed to scan threat: %w", err)
		}
		threats = append(threats, threat)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Error iterating threats: %v", err)
		return nil, fmt.Errorf("error iterating threats: %w", err)
	}

	return threats, nil
}

// scanSingleThreat scans a single row into a Threat object
func (s *DatabaseThreatStore) scanSingleThreat(rows *sql.Rows) (Threat, error) {
	var threat Threat
	var description, mitigation, issueUrl sql.NullString
	var score sql.NullFloat64
	var diagramId, cellId, assetId sql.NullString
	var metadataJSON sql.NullString

	err := rows.Scan(
		&threat.Id,
		&threat.ThreatModelId,
		&threat.Name,
		&description,
		&threat.Severity,
		&mitigation,
		&threat.ThreatType,
		&threat.Status,
		&threat.Priority,
		&threat.Mitigated,
		&score,
		&issueUrl,
		&diagramId,
		&cellId,
		&assetId,
		&metadataJSON,
		&threat.CreatedAt,
		&threat.ModifiedAt,
	)

	if err != nil {
		return threat, err
	}

	// Handle nullable fields
	s.populateNullableFields(&threat, description, mitigation, issueUrl, score, diagramId, cellId, assetId, metadataJSON)

	return threat, nil
}

// populateNullableFields sets the nullable fields on a Threat
func (s *DatabaseThreatStore) populateNullableFields(threat *Threat, description, mitigation, issueUrl sql.NullString,
	score sql.NullFloat64, diagramId, cellId, assetId sql.NullString, metadataJSON sql.NullString) {

	if description.Valid {
		threat.Description = &description.String
	}
	if mitigation.Valid {
		threat.Mitigation = &mitigation.String
	}
	if issueUrl.Valid {
		threat.IssueUri = &issueUrl.String
	}
	if score.Valid {
		score32 := float32(score.Float64)
		threat.Score = &score32
	}
	if diagramId.Valid {
		if diagID, err := uuid.Parse(diagramId.String); err == nil {
			threat.DiagramId = &diagID
		}
	}
	if cellId.Valid {
		if cID, err := uuid.Parse(cellId.String); err == nil {
			threat.CellId = &cID
		}
	}
	if assetId.Valid {
		if aID, err := uuid.Parse(assetId.String); err == nil {
			threat.AssetId = &aID
		}
	}
	if metadataJSON.Valid && metadataJSON.String != "" {
		var metadata []Metadata
		if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err == nil {
			threat.Metadata = &metadata
		}
	}
}
