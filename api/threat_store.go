package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/google/uuid"
)

// ThreatStore defines the interface for threat operations with caching support
type ThreatStore interface {
	// CRUD operations
	Create(ctx context.Context, threat *Threat) error
	Get(ctx context.Context, id string) (*Threat, error)
	Update(ctx context.Context, threat *Threat) error
	Delete(ctx context.Context, id string) error

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Threat, error)

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
	logger := logging.Get()
	logger.Debug("Creating threat: %s in threat model: %s", threat.Name, threat.ThreatModelId)

	// Generate ID if not provided
	if threat.Id == nil {
		id := uuid.New()
		threat.Id = &id
	}

	// Set timestamps
	now := time.Now().UTC()
	threat.CreatedAt = now
	threat.ModifiedAt = now

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

	// Insert into database
	query := `
		INSERT INTO threats (
			id, threat_model_id, name, description, severity, 
			likelihood, risk_level, mitigation, threat_type, 
			status, priority, mitigated, score, issue_url, 
			diagram_id, cell_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
	`

	_, err := s.db.ExecContext(ctx, query,
		threat.Id,
		threat.ThreatModelId,
		threat.Name,
		threat.Description,
		string(threat.Severity),
		nil, // likelihood - not in current schema
		nil, // risk_level - not in current schema
		threat.Mitigation,
		threat.ThreatType,
		threat.Status,
		threat.Priority,
		threat.Mitigated,
		threat.Score,
		threat.IssueUrl,
		threat.DiagramId,
		threat.CellId,
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
	logger := logging.Get()
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
			   score, issue_url, diagram_id, cell_id, created_at, updated_at
		FROM threats 
		WHERE id = $1
	`

	var threat Threat
	var description, mitigation, issueUrl sql.NullString
	var score sql.NullFloat64
	var diagramId, cellId sql.NullString

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
		threat.IssueUrl = &issueUrl.String
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
	logger := logging.Get()
	logger.Debug("Updating threat: %s", threat.Id)

	// Update modified timestamp
	threat.ModifiedAt = time.Now().UTC()

	query := `
		UPDATE threats SET
			name = $2, description = $3, severity = $4, mitigation = $5,
			threat_type = $6, status = $7, priority = $8, mitigated = $9,
			score = $10, issue_url = $11, diagram_id = $12, cell_id = $13,
			updated_at = $14
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
		threat.IssueUrl,
		threat.DiagramId,
		threat.CellId,
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
	logger := logging.Get()
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

// List retrieves threats for a threat model with pagination and caching
func (s *DatabaseThreatStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Threat, error) {
	logger := logging.Get()
	logger.Debug("Listing threats for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Try cache first
	var threats []Threat
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "threats", threatModelID, offset, limit, &threats)
		if err == nil && threats != nil {
			logger.Debug("Cache hit for threat list %s [%d:%d]", threatModelID, offset, limit)
			return threats, nil
		}
		if err != nil {
			logger.Error("Cache error when getting threat list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for threat list, querying database")

	query := `
		SELECT id, threat_model_id, name, description, severity,
			   mitigation, threat_type, status, priority, mitigated,
			   score, issue_url, diagram_id, cell_id, created_at, updated_at
		FROM threats 
		WHERE threat_model_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.QueryContext(ctx, query, threatModelID, limit, offset)
	if err != nil {
		logger.Error("Failed to query threats from database: %v", err)
		return nil, fmt.Errorf("failed to list threats: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	threats = make([]Threat, 0)
	for rows.Next() {
		var threat Threat
		var description, mitigation, issueUrl sql.NullString
		var score sql.NullFloat64
		var diagramId, cellId sql.NullString

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
			&threat.CreatedAt,
			&threat.ModifiedAt,
		)

		if err != nil {
			logger.Error("Failed to scan threat row: %v", err)
			return nil, fmt.Errorf("failed to scan threat: %w", err)
		}

		// Handle nullable fields
		if description.Valid {
			threat.Description = &description.String
		}
		if mitigation.Valid {
			threat.Mitigation = &mitigation.String
		}
		if issueUrl.Valid {
			threat.IssueUrl = &issueUrl.String
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

		threats = append(threats, threat)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating threat rows: %v", err)
		return nil, fmt.Errorf("error iterating threats: %w", err)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "threats", threatModelID, offset, limit, threats); cacheErr != nil {
			logger.Error("Failed to cache threat list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d threats", len(threats))
	return threats, nil
}

// Patch applies JSON patch operations to a threat
func (s *DatabaseThreatStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error) {
	logger := logging.Get()
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
				threat.Severity = ThreatSeverity(sev)
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
	logger := logging.Get()
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
			score, issue_url, diagram_id, cell_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
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
		threat.CreatedAt = now
		threat.ModifiedAt = now

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
			threat.IssueUrl,
			threat.DiagramId,
			threat.CellId,
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
	logger := logging.Get()
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
			score = $10, issue_url = $11, diagram_id = $12, cell_id = $13,
			updated_at = $14
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
		threat.ModifiedAt = now

		// Track parent for cache invalidation
		if parentThreatModelID == "" {
			parentThreatModelID = threat.ThreatModelId.String()
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
			threat.IssueUrl,
			threat.DiagramId,
			threat.CellId,
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
	logger := logging.Get()
	logger.Debug("Warming cache for threat model: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of threats
	threats, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	// Individual threats are already cached by List(), so we're done
	logger.Debug("Warmed cache with %d threats for threat model %s", len(threats), threatModelID)
	return nil
}
