package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/google/uuid"
)

// MetadataStore defines the interface for metadata operations with caching support
// Metadata supports POST operations and key-based access per the implementation plan
type MetadataStore interface {
	// CRUD operations
	Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error)
	Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	Delete(ctx context.Context, entityType, entityID, key string) error

	// Collection operations
	List(ctx context.Context, entityType, entityID string) ([]Metadata, error)

	// POST operations - adding metadata without specifying key upfront
	Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error

	// Bulk operations
	BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error

	// Key-based operations
	GetByKey(ctx context.Context, key string) ([]Metadata, error)
	ListKeys(ctx context.Context, entityType, entityID string) ([]string, error)

	// Cache management
	InvalidateCache(ctx context.Context, entityType, entityID string) error
	WarmCache(ctx context.Context, entityType, entityID string) error
}

// ExtendedMetadata includes database fields not in the API model
type ExtendedMetadata struct {
	Metadata
	ID         uuid.UUID `json:"id"`
	EntityType string    `json:"entity_type"`
	EntityID   uuid.UUID `json:"entity_id"`
	CreatedAt  time.Time `json:"created_at"`
	ModifiedAt time.Time `json:"modified_at"`
}

// DatabaseMetadataStore implements MetadataStore with database persistence and Redis caching
type DatabaseMetadataStore struct {
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDatabaseMetadataStore creates a new database-backed metadata store with caching
func NewDatabaseMetadataStore(db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DatabaseMetadataStore {
	return &DatabaseMetadataStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// validateEntityType checks if the entity type is supported
func (s *DatabaseMetadataStore) validateEntityType(entityType string) error {
	validTypes := []string{"threat_model", "threat", "diagram", "document", "source", "cell"}
	for _, valid := range validTypes {
		if entityType == valid {
			return nil
		}
	}
	return fmt.Errorf("unsupported entity type: %s", entityType)
}

// Create creates a new metadata entry with write-through caching
func (s *DatabaseMetadataStore) Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	logger := slogging.Get()
	logger.Debug("Creating metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	// Parse entity ID
	eID, err := uuid.Parse(entityID)
	if err != nil {
		logger.Error("Invalid entity ID: %s", entityID)
		return fmt.Errorf("invalid entity ID: %w", err)
	}

	// Set timestamps
	now := time.Now().UTC()

	// Insert into database
	query := `
		INSERT INTO metadata (
			id, entity_type, entity_id, key, value, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
		ON CONFLICT (entity_type, entity_id, key) 
		DO UPDATE SET 
			value = EXCLUDED.value,
			modified_at = EXCLUDED.modified_at
	`

	id := uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata)
	_, err = s.db.ExecContext(ctx, query,
		id,
		entityType,
		eID,
		metadata.Key,
		metadata.Value,
		now,
		now,
	)

	if err != nil {
		logger.Error("Failed to create metadata in database: %v", err)
		return fmt.Errorf("failed to create metadata: %w", err)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      id.String(),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after metadata creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created metadata: %s=%s", metadata.Key, metadata.Value)
	return nil
}

// Get retrieves a specific metadata entry by key with cache-first strategy
func (s *DatabaseMetadataStore) Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error) {
	logger := slogging.Get()
	logger.Debug("Getting metadata: %s for %s:%s", key, entityType, entityID)

	// Try cache first by getting the full metadata collection
	if s.cache != nil {
		metadataList, err := s.cache.GetCachedMetadata(ctx, entityType, entityID)
		if err != nil {
			logger.Error("Cache error when getting metadata %s:%s: %v", entityType, entityID, err)
		} else if metadataList != nil {
			// Find the specific key
			for _, meta := range metadataList {
				if meta.Key == key {
					logger.Debug("Cache hit for metadata: %s", key)
					return &meta, nil
				}
			}
			// Key not found in cached collection
			return nil, fmt.Errorf("metadata key not found: %s", key)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for metadata %s, querying database", key)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return nil, err
	}

	// Parse entity ID
	eID, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid entity ID: %w", err)
	}

	query := `
		SELECT key, value
		FROM metadata 
		WHERE entity_type = $1 AND entity_id = $2 AND key = $3
	`

	var metadata Metadata
	err = s.db.QueryRowContext(ctx, query, entityType, eID, key).Scan(
		&metadata.Key,
		&metadata.Value,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("metadata key not found: %s", key)
		}
		logger.Error("Failed to get metadata from database: %v", err)
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	logger.Debug("Successfully retrieved metadata: %s=%s", metadata.Key, metadata.Value)
	return &metadata, nil
}

// Update updates an existing metadata entry with write-through caching
func (s *DatabaseMetadataStore) Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	logger := slogging.Get()
	logger.Debug("Updating metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	// Parse entity ID
	eID, err := uuid.Parse(entityID)
	if err != nil {
		return fmt.Errorf("invalid entity ID: %w", err)
	}

	// Update timestamp
	now := time.Now().UTC()

	query := `
		UPDATE metadata SET
			value = $4, modified_at = $5
		WHERE entity_type = $1 AND entity_id = $2 AND key = $3
	`

	result, err := s.db.ExecContext(ctx, query,
		entityType,
		eID,
		metadata.Key,
		metadata.Value,
		now,
	)

	if err != nil {
		logger.Error("Failed to update metadata in database: %v", err)
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify update: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("metadata key not found: %s", metadata.Key)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      fmt.Sprintf("%s:%s:%s", entityType, entityID, metadata.Key),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after metadata update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated metadata: %s=%s", metadata.Key, metadata.Value)
	return nil
}

// Delete removes a metadata entry and invalidates related caches
func (s *DatabaseMetadataStore) Delete(ctx context.Context, entityType, entityID, key string) error {
	logger := slogging.Get()
	logger.Debug("Deleting metadata: %s for %s:%s", key, entityType, entityID)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	// Parse entity ID
	eID, err := uuid.Parse(entityID)
	if err != nil {
		return fmt.Errorf("invalid entity ID: %w", err)
	}

	// Delete from database
	query := `DELETE FROM metadata WHERE entity_type = $1 AND entity_id = $2 AND key = $3`
	result, err := s.db.ExecContext(ctx, query, entityType, eID, key)
	if err != nil {
		logger.Error("Failed to delete metadata from database: %v", err)
		return fmt.Errorf("failed to delete metadata: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify deletion: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("metadata key not found: %s", key)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      fmt.Sprintf("%s:%s:%s", entityType, entityID, key),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after metadata deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted metadata: %s", key)
	return nil
}

// List retrieves all metadata for an entity with caching
func (s *DatabaseMetadataStore) List(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	logger := slogging.Get()
	logger.Debug("Listing metadata for %s:%s", entityType, entityID)

	// Try cache first
	if s.cache != nil {
		metadataList, err := s.cache.GetCachedMetadata(ctx, entityType, entityID)
		if err != nil {
			logger.Error("Cache error when getting metadata list %s:%s: %v", entityType, entityID, err)
		} else if metadataList != nil {
			logger.Debug("Cache hit for metadata list %s:%s", entityType, entityID)
			return metadataList, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for metadata list, querying database")

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return nil, err
	}

	// Parse entity ID
	eID, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid entity ID: %w", err)
	}

	query := `
		SELECT key, value
		FROM metadata 
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY key ASC
	`

	rows, err := s.db.QueryContext(ctx, query, entityType, eID)
	if err != nil {
		logger.Error("Failed to query metadata from database: %v", err)
		return nil, fmt.Errorf("failed to list metadata: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var metadataList []Metadata
	for rows.Next() {
		var metadata Metadata
		err := rows.Scan(&metadata.Key, &metadata.Value)
		if err != nil {
			logger.Error("Failed to scan metadata row: %v", err)
			return nil, fmt.Errorf("failed to scan metadata: %w", err)
		}
		metadataList = append(metadataList, metadata)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating metadata rows: %v", err)
		return nil, fmt.Errorf("error iterating metadata: %w", err)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheMetadata(ctx, entityType, entityID, metadataList); cacheErr != nil {
			logger.Error("Failed to cache metadata list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d metadata entries", len(metadataList))
	return metadataList, nil
}

// Post creates a new metadata entry using POST semantics (allowing duplicates initially)
func (s *DatabaseMetadataStore) Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	logger := slogging.Get()
	logger.Debug("Posting metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// POST semantics: create regardless of existing keys, let the database handle conflicts
	return s.Create(ctx, entityType, entityID, metadata)
}

// BulkCreate creates multiple metadata entries in a single transaction
func (s *DatabaseMetadataStore) BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	logger := slogging.Get()
	logger.Debug("Bulk creating %d metadata entries", len(metadata))

	if len(metadata) == 0 {
		return nil
	}

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	// Parse entity ID
	eID, err := uuid.Parse(entityID)
	if err != nil {
		return fmt.Errorf("invalid entity ID: %w", err)
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
		INSERT INTO metadata (
			id, entity_type, entity_id, key, value, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
		ON CONFLICT (entity_type, entity_id, key) 
		DO UPDATE SET 
			value = EXCLUDED.value,
			modified_at = EXCLUDED.modified_at
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

	for i, meta := range metadata {
		id := uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata)
		_, err = stmt.ExecContext(ctx,
			id,
			entityType,
			eID,
			meta.Key,
			meta.Value,
			now,
			now,
		)

		if err != nil {
			logger.Error("Failed to execute bulk insert for metadata %d: %v", i, err)
			return fmt.Errorf("failed to insert metadata %d: %w", i, err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		logger.Error("Failed to commit bulk create transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after bulk metadata creation: %v", invErr)
		}
	}

	logger.Debug("Successfully bulk created %d metadata entries", len(metadata))
	return nil
}

// BulkUpdate updates multiple metadata entries in a single transaction
func (s *DatabaseMetadataStore) BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	logger := slogging.Get()
	logger.Debug("Bulk updating %d metadata entries", len(metadata))

	if len(metadata) == 0 {
		return nil
	}

	// Use BulkCreate with upsert semantics
	return s.BulkCreate(ctx, entityType, entityID, metadata)
}

// BulkDelete deletes multiple metadata entries by key in a single transaction
func (s *DatabaseMetadataStore) BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error {
	logger := slogging.Get()
	logger.Debug("Bulk deleting %d metadata keys", len(keys))

	if len(keys) == 0 {
		return nil
	}

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	// Parse entity ID
	eID, err := uuid.Parse(entityID)
	if err != nil {
		return fmt.Errorf("invalid entity ID: %w", err)
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

	query := `DELETE FROM metadata WHERE entity_type = $1 AND entity_id = $2 AND key = $3`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		logger.Error("Failed to prepare bulk delete statement: %v", err)
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil {
			logger.Error("Failed to close statement: %v", closeErr)
		}
	}()

	for _, key := range keys {
		_, err = stmt.ExecContext(ctx, entityType, eID, key)
		if err != nil {
			logger.Error("Failed to execute bulk delete for key %s: %v", key, err)
			return fmt.Errorf("failed to delete key %s: %w", key, err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		logger.Error("Failed to commit bulk delete transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after bulk metadata deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully bulk deleted %d metadata keys", len(keys))
	return nil
}

// GetByKey retrieves all metadata entries with a specific key across all entities
func (s *DatabaseMetadataStore) GetByKey(ctx context.Context, key string) ([]Metadata, error) {
	logger := slogging.Get()
	logger.Debug("Getting metadata by key: %s", key)

	query := `
		SELECT key, value
		FROM metadata 
		WHERE key = $1
		ORDER BY entity_type, entity_id
	`

	rows, err := s.db.QueryContext(ctx, query, key)
	if err != nil {
		logger.Error("Failed to query metadata by key from database: %v", err)
		return nil, fmt.Errorf("failed to get metadata by key: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var metadataList []Metadata
	for rows.Next() {
		var metadata Metadata
		err := rows.Scan(&metadata.Key, &metadata.Value)
		if err != nil {
			logger.Error("Failed to scan metadata row: %v", err)
			return nil, fmt.Errorf("failed to scan metadata: %w", err)
		}
		metadataList = append(metadataList, metadata)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating metadata rows: %v", err)
		return nil, fmt.Errorf("error iterating metadata: %w", err)
	}

	logger.Debug("Successfully retrieved %d metadata entries with key %s", len(metadataList), key)
	return metadataList, nil
}

// ListKeys retrieves all metadata keys for an entity
func (s *DatabaseMetadataStore) ListKeys(ctx context.Context, entityType, entityID string) ([]string, error) {
	logger := slogging.Get()
	logger.Debug("Listing metadata keys for %s:%s", entityType, entityID)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return nil, err
	}

	// Parse entity ID
	eID, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid entity ID: %w", err)
	}

	query := `
		SELECT DISTINCT key
		FROM metadata 
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY key ASC
	`

	rows, err := s.db.QueryContext(ctx, query, entityType, eID)
	if err != nil {
		logger.Error("Failed to query metadata keys from database: %v", err)
		return nil, fmt.Errorf("failed to list metadata keys: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var keys []string
	for rows.Next() {
		var key string
		err := rows.Scan(&key)
		if err != nil {
			logger.Error("Failed to scan metadata key row: %v", err)
			return nil, fmt.Errorf("failed to scan metadata key: %w", err)
		}
		keys = append(keys, key)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating metadata key rows: %v", err)
		return nil, fmt.Errorf("error iterating metadata keys: %w", err)
	}

	logger.Debug("Successfully retrieved %d metadata keys", len(keys))
	return keys, nil
}

// InvalidateCache removes metadata-related cache entries
func (s *DatabaseMetadataStore) InvalidateCache(ctx context.Context, entityType, entityID string) error {
	if s.cache == nil {
		return nil
	}

	return s.cache.InvalidateMetadata(ctx, entityType, entityID)
}

// WarmCache preloads metadata for an entity into cache
func (s *DatabaseMetadataStore) WarmCache(ctx context.Context, entityType, entityID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for %s:%s metadata", entityType, entityID)

	if s.cache == nil {
		return nil
	}

	// Load metadata for the entity
	_, err := s.List(ctx, entityType, entityID)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	logger.Debug("Warmed cache for %s:%s metadata", entityType, entityID)
	return nil
}
