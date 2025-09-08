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

// SourceStore defines the interface for source operations with caching support
// Note: Sources do not support PATCH operations per the implementation plan
type SourceStore interface {
	// CRUD operations (no PATCH support)
	Create(ctx context.Context, source *Source, threatModelID string) error
	Get(ctx context.Context, id string) (*Source, error)
	Update(ctx context.Context, source *Source, threatModelID string) error
	Delete(ctx context.Context, id string) error

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Source, error)

	// Bulk operations
	BulkCreate(ctx context.Context, sources []Source, threatModelID string) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}

// ExtendedSource includes database fields not in the API model
type ExtendedSource struct {
	Source
	ThreatModelId uuid.UUID `json:"threat_model_id"`
	CreatedAt     time.Time `json:"created_at"`
	ModifiedAt    time.Time `json:"modified_at"`
}

// DatabaseSourceStore implements SourceStore with database persistence and Redis caching
type DatabaseSourceStore struct {
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDatabaseSourceStore creates a new database-backed source store with caching
func NewDatabaseSourceStore(db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DatabaseSourceStore {
	return &DatabaseSourceStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// extendedToSource converts an ExtendedSource to Source
func extendedToSource(extSrc *ExtendedSource) *Source {
	return &extSrc.Source
}

// Create creates a new source with write-through caching
func (s *DatabaseSourceStore) Create(ctx context.Context, source *Source, threatModelID string) error {
	logger := logging.Get()
	logger.Debug("Creating source: %s in threat model: %s", source.Url, threatModelID)

	// Generate ID if not provided
	if source.Id == nil {
		id := uuid.New()
		source.Id = &id
	}

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Set timestamps
	now := time.Now().UTC()

	// Serialize parameters if present
	var parametersJSON sql.NullString
	if source.Parameters != nil {
		paramBytes, err := json.Marshal(source.Parameters)
		if err != nil {
			logger.Error("Failed to marshal source parameters: %v", err)
			return fmt.Errorf("failed to marshal parameters: %w", err)
		}
		parametersJSON.String = string(paramBytes)
		parametersJSON.Valid = true
	}

	// Convert type to string
	var sourceType sql.NullString
	if source.Type != nil {
		sourceType.String = string(*source.Type)
		sourceType.Valid = true
	}

	// Insert into database
	query := `
		INSERT INTO sources (
			id, threat_model_id, name, url, description, type, parameters, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	_, err = s.db.ExecContext(ctx, query,
		source.Id,
		tmID,
		source.Name,
		source.Url,
		source.Description,
		sourceType,
		parametersJSON,
		now,
		now,
	)

	if err != nil {
		logger.Error("Failed to create source in database: %v", err)
		return fmt.Errorf("failed to create source: %w", err)
	}

	// Cache the new source
	if s.cache != nil {
		if cacheErr := s.cache.CacheSource(ctx, source); cacheErr != nil {
			logger.Error("Failed to cache new source: %v", cacheErr)
			// Don't fail the request if caching fails
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "source",
			EntityID:      source.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after source creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created source: %s", source.Id)
	return nil
}

// Get retrieves a source by ID with cache-first strategy
func (s *DatabaseSourceStore) Get(ctx context.Context, id string) (*Source, error) {
	logger := logging.Get()
	logger.Debug("Getting source: %s", id)

	// Try cache first
	if s.cache != nil {
		source, err := s.cache.GetCachedSource(ctx, id)
		if err != nil {
			logger.Error("Cache error when getting source %s: %v", id, err)
		} else if source != nil {
			logger.Debug("Cache hit for source: %s", id)
			return source, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for source %s, querying database", id)

	query := `
		SELECT id, threat_model_id, name, url, description, type, parameters, created_at, modified_at
		FROM sources 
		WHERE id = $1
	`

	var extSrc ExtendedSource
	var name, description, sourceType, parametersJSON sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&extSrc.Id,
		&extSrc.ThreatModelId,
		&name,
		&extSrc.Url,
		&description,
		&sourceType,
		&parametersJSON,
		&extSrc.CreatedAt,
		&extSrc.ModifiedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("source not found: %s", id)
		}
		logger.Error("Failed to get source from database: %v", err)
		return nil, fmt.Errorf("failed to get source: %w", err)
	}

	// Handle nullable fields
	if name.Valid {
		extSrc.Name = &name.String
	}
	if description.Valid {
		extSrc.Description = &description.String
	}
	if sourceType.Valid {
		srcType := SourceType(sourceType.String)
		extSrc.Type = &srcType
	}
	if parametersJSON.Valid {
		var params struct {
			RefType  SourceParametersRefType `json:"refType"`
			RefValue string                  `json:"refValue"`
			SubPath  *string                 `json:"subPath,omitempty"`
		}
		if err := json.Unmarshal([]byte(parametersJSON.String), &params); err == nil {
			extSrc.Parameters = &params
		}
	}

	source := extendedToSource(&extSrc)

	// Cache the result for future requests
	if s.cache != nil {
		if cacheErr := s.cache.CacheSource(ctx, source); cacheErr != nil {
			logger.Error("Failed to cache source after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved source: %s", id)
	return source, nil
}

// Update updates an existing source with write-through caching
func (s *DatabaseSourceStore) Update(ctx context.Context, source *Source, threatModelID string) error {
	logger := logging.Get()
	logger.Debug("Updating source: %s", source.Id)

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Update timestamp
	now := time.Now().UTC()

	// Serialize parameters if present
	var parametersJSON sql.NullString
	if source.Parameters != nil {
		paramBytes, err := json.Marshal(source.Parameters)
		if err != nil {
			logger.Error("Failed to marshal source parameters: %v", err)
			return fmt.Errorf("failed to marshal parameters: %w", err)
		}
		parametersJSON.String = string(paramBytes)
		parametersJSON.Valid = true
	}

	// Convert type to string
	var sourceType sql.NullString
	if source.Type != nil {
		sourceType.String = string(*source.Type)
		sourceType.Valid = true
	}

	query := `
		UPDATE sources SET
			name = $2, url = $3, description = $4, type = $5, parameters = $6, modified_at = $7
		WHERE id = $1 AND threat_model_id = $8
	`

	result, err := s.db.ExecContext(ctx, query,
		source.Id,
		source.Name,
		source.Url,
		source.Description,
		sourceType,
		parametersJSON,
		now,
		tmID,
	)

	if err != nil {
		logger.Error("Failed to update source in database: %v", err)
		return fmt.Errorf("failed to update source: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify update: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("source not found: %s", source.Id)
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheSource(ctx, source); cacheErr != nil {
			logger.Error("Failed to update source cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "source",
			EntityID:      source.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after source update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated source: %s", source.Id)
	return nil
}

// Delete removes a source and invalidates related caches
func (s *DatabaseSourceStore) Delete(ctx context.Context, id string) error {
	logger := logging.Get()
	logger.Debug("Deleting source: %s", id)

	// Get the threat model ID from database for cache invalidation
	// We need this since the Source struct doesn't contain the threat_model_id field
	var threatModelID uuid.UUID
	query := `SELECT threat_model_id FROM sources WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, id).Scan(&threatModelID)
	if err != nil {
		logger.Error("Failed to get threat model ID for source %s: %v", id, err)
		return fmt.Errorf("failed to get source parent: %w", err)
	}

	// Delete from database
	deleteQuery := `DELETE FROM sources WHERE id = $1`
	result, err := s.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		logger.Error("Failed to delete source from database: %v", err)
		return fmt.Errorf("failed to delete source: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify deletion: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("source not found: %s", id)
	}

	// Remove from cache
	if s.cache != nil {
		if cacheErr := s.cache.InvalidateEntity(ctx, "source", id); cacheErr != nil {
			logger.Error("Failed to remove source from cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "source",
			EntityID:      id,
			ParentType:    "threat_model",
			ParentID:      threatModelID.String(),
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after source deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted source: %s", id)
	return nil
}

// List retrieves sources for a threat model with pagination and caching
func (s *DatabaseSourceStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Source, error) {
	logger := logging.Get()
	logger.Debug("Listing sources for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Try cache first
	var sources []Source
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "sources", threatModelID, offset, limit, &sources)
		if err == nil && sources != nil {
			logger.Debug("Cache hit for source list %s [%d:%d]", threatModelID, offset, limit)
			return sources, nil
		}
		if err != nil {
			logger.Error("Cache error when getting source list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for source list, querying database")

	query := `
		SELECT id, threat_model_id, name, url, description, type, parameters, created_at, modified_at
		FROM sources 
		WHERE threat_model_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.QueryContext(ctx, query, threatModelID, limit, offset)
	if err != nil {
		logger.Error("Failed to query sources from database: %v", err)
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	sources = make([]Source, 0)
	for rows.Next() {
		var extSrc ExtendedSource
		var name, description, sourceType, parametersJSON sql.NullString

		err := rows.Scan(
			&extSrc.Id,
			&extSrc.ThreatModelId,
			&name,
			&extSrc.Url,
			&description,
			&sourceType,
			&parametersJSON,
			&extSrc.CreatedAt,
			&extSrc.ModifiedAt,
		)

		if err != nil {
			logger.Error("Failed to scan source row: %v", err)
			return nil, fmt.Errorf("failed to scan source: %w", err)
		}

		// Handle nullable fields
		if name.Valid {
			extSrc.Name = &name.String
		}
		if description.Valid {
			extSrc.Description = &description.String
		}
		if sourceType.Valid {
			srcType := SourceType(sourceType.String)
			extSrc.Type = &srcType
		}
		if parametersJSON.Valid {
			var params struct {
				RefType  SourceParametersRefType `json:"refType"`
				RefValue string                  `json:"refValue"`
				SubPath  *string                 `json:"subPath,omitempty"`
			}
			if err := json.Unmarshal([]byte(parametersJSON.String), &params); err == nil {
				extSrc.Parameters = &params
			}
		}

		source := extendedToSource(&extSrc)
		sources = append(sources, *source)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating source rows: %v", err)
		return nil, fmt.Errorf("error iterating sources: %w", err)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "sources", threatModelID, offset, limit, sources); cacheErr != nil {
			logger.Error("Failed to cache source list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d sources", len(sources))
	return sources, nil
}

// BulkCreate creates multiple sources in a single transaction
func (s *DatabaseSourceStore) BulkCreate(ctx context.Context, sources []Source, threatModelID string) error {
	logger := logging.Get()
	logger.Debug("Bulk creating %d sources", len(sources))

	if len(sources) == 0 {
		return nil
	}

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
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
		INSERT INTO sources (
			id, threat_model_id, name, url, description, type, parameters, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
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

	for i := range sources {
		source := &sources[i]

		// Generate ID if not provided
		if source.Id == nil {
			id := uuid.New()
			source.Id = &id
		}

		// Serialize parameters if present
		var parametersJSON sql.NullString
		if source.Parameters != nil {
			paramBytes, err := json.Marshal(source.Parameters)
			if err != nil {
				logger.Error("Failed to marshal parameters for source %d: %v", i, err)
				return fmt.Errorf("failed to marshal parameters for source %d: %w", i, err)
			}
			parametersJSON.String = string(paramBytes)
			parametersJSON.Valid = true
		}

		// Convert type to string
		var sourceType sql.NullString
		if source.Type != nil {
			sourceType.String = string(*source.Type)
			sourceType.Valid = true
		}

		_, err = stmt.ExecContext(ctx,
			source.Id,
			tmID,
			source.Name,
			source.Url,
			source.Description,
			sourceType,
			parametersJSON,
			now,
			now,
		)

		if err != nil {
			logger.Error("Failed to execute bulk insert for source %d: %v", i, err)
			return fmt.Errorf("failed to insert source %d: %w", i, err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		logger.Error("Failed to commit bulk create transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		if invErr := s.cacheInvalidator.InvalidateAllRelatedCaches(ctx, threatModelID); invErr != nil {
			logger.Error("Failed to invalidate caches after bulk source creation: %v", invErr)
		}
	}

	logger.Debug("Successfully bulk created %d sources", len(sources))
	return nil
}

// InvalidateCache removes source-related cache entries
func (s *DatabaseSourceStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}

	return s.cache.InvalidateEntity(ctx, "source", id)
}

// WarmCache preloads sources for a threat model into cache
func (s *DatabaseSourceStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := logging.Get()
	logger.Debug("Warming cache for threat model sources: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of sources
	sources, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	// Individual sources are already cached by List(), so we're done
	logger.Debug("Warmed cache with %d sources for threat model %s", len(sources), threatModelID)
	return nil
}
