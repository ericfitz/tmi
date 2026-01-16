package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormMetadataStore implements MetadataStore using GORM
type GormMetadataStore struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	mutex            sync.RWMutex
}

// NewGormMetadataStore creates a new GORM-backed metadata store
func NewGormMetadataStore(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormMetadataStore {
	return &GormMetadataStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// validateEntityType checks if the entity type is supported
func (s *GormMetadataStore) validateEntityType(entityType string) error {
	validTypes := []string{"threat_model", "threat", "diagram", "document", "repository", "note", "cell", "asset"}
	for _, valid := range validTypes {
		if entityType == valid {
			return nil
		}
	}
	return fmt.Errorf("unsupported entity type: %s", entityType)
}

// Create creates a new metadata entry
func (s *GormMetadataStore) Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Creating metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	model := models.Metadata{
		ID:         uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata).String(),
		EntityType: entityType,
		EntityID:   entityID,
		Key:        metadata.Key,
		Value:      metadata.Value,
		CreatedAt:  now,
		ModifiedAt: now,
	}

	// Use OnConflict to handle upsert
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "modified_at"}),
	}).Create(&model)

	if result.Error != nil {
		logger.Error("Failed to create metadata in database: %v", result.Error)
		return fmt.Errorf("failed to create metadata: %w", result.Error)
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      model.ID,
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

// Get retrieves a specific metadata entry by key
func (s *GormMetadataStore) Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting metadata: %s for %s:%s", key, entityType, entityID)

	// Try cache first
	if s.cache != nil {
		metadataList, err := s.cache.GetCachedMetadata(ctx, entityType, entityID)
		if err != nil {
			logger.Error("Cache error when getting metadata %s:%s: %v", entityType, entityID, err)
		} else if metadataList != nil {
			for _, meta := range metadataList {
				if meta.Key == key {
					logger.Debug("Cache hit for metadata: %s", key)
					return &meta, nil
				}
			}
			return nil, fmt.Errorf("metadata key not found: %s", key)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for metadata %s, querying database", key)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return nil, err
	}

	var model models.Metadata
	result := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, key).
		First(&model)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("metadata key not found: %s", key)
		}
		logger.Error("Failed to get metadata from database: %v", result.Error)
		return nil, fmt.Errorf("failed to get metadata: %w", result.Error)
	}

	metadata := &Metadata{
		Key:   model.Key,
		Value: model.Value,
	}

	logger.Debug("Successfully retrieved metadata: %s=%s", metadata.Key, metadata.Value)
	return metadata, nil
}

// Update updates an existing metadata entry
func (s *GormMetadataStore) Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Updating metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	result := s.db.WithContext(ctx).Model(&models.Metadata{}).
		Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, metadata.Key).
		Updates(map[string]interface{}{
			"value":       metadata.Value,
			"modified_at": now,
		})

	if result.Error != nil {
		logger.Error("Failed to update metadata in database: %v", result.Error)
		return fmt.Errorf("failed to update metadata: %w", result.Error)
	}

	if result.RowsAffected == 0 {
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

// Delete removes a metadata entry
func (s *GormMetadataStore) Delete(ctx context.Context, entityType, entityID, key string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting metadata: %s for %s:%s", key, entityType, entityID)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	result := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, key).
		Delete(&models.Metadata{})

	if result.Error != nil {
		logger.Error("Failed to delete metadata from database: %v", result.Error)
		return fmt.Errorf("failed to delete metadata: %w", result.Error)
	}

	if result.RowsAffected == 0 {
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

// List retrieves all metadata for an entity
func (s *GormMetadataStore) List(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

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

	var modelList []models.Metadata
	result := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("key ASC").
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to query metadata from database: %v", result.Error)
		return nil, fmt.Errorf("failed to list metadata: %w", result.Error)
	}

	metadataList := make([]Metadata, 0, len(modelList))
	for _, model := range modelList {
		metadataList = append(metadataList, Metadata{
			Key:   model.Key,
			Value: model.Value,
		})
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

// Post creates a new metadata entry using POST semantics
func (s *GormMetadataStore) Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	logger := slogging.Get()
	logger.Debug("Posting metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// POST semantics: create regardless of existing keys, let the database handle conflicts
	return s.Create(ctx, entityType, entityID, metadata)
}

// BulkCreate creates multiple metadata entries in a single transaction
func (s *GormMetadataStore) BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Bulk creating %d metadata entries", len(metadata))

	if len(metadata) == 0 {
		return nil
	}

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, meta := range metadata {
			model := models.Metadata{
				ID:         uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata).String(),
				EntityType: entityType,
				EntityID:   entityID,
				Key:        meta.Key,
				Value:      meta.Value,
				CreatedAt:  now,
				ModifiedAt: now,
			}

			result := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "modified_at"}),
			}).Create(&model)

			if result.Error != nil {
				logger.Error("Failed to bulk create metadata: %v", result.Error)
				return fmt.Errorf("failed to create metadata: %w", result.Error)
			}
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
	})
}

// BulkUpdate updates multiple metadata entries in a single transaction
func (s *GormMetadataStore) BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	logger := slogging.Get()
	logger.Debug("Bulk updating %d metadata entries", len(metadata))

	// Use BulkCreate with upsert semantics
	return s.BulkCreate(ctx, entityType, entityID, metadata)
}

// BulkDelete deletes multiple metadata entries by key in a single transaction
func (s *GormMetadataStore) BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Bulk deleting %d metadata keys", len(keys))

	if len(keys) == 0 {
		return nil
	}

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, key := range keys {
			result := tx.Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, key).
				Delete(&models.Metadata{})
			if result.Error != nil {
				logger.Error("Failed to bulk delete metadata key %s: %v", key, result.Error)
				return fmt.Errorf("failed to delete key %s: %w", key, result.Error)
			}
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
	})
}

// GetByKey retrieves all metadata entries with a specific key across all entities
func (s *GormMetadataStore) GetByKey(ctx context.Context, key string) ([]Metadata, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting metadata by key: %s", key)

	var modelList []models.Metadata
	result := s.db.WithContext(ctx).
		Where("key = ?", key).
		Order("entity_type, entity_id").
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to query metadata by key from database: %v", result.Error)
		return nil, fmt.Errorf("failed to get metadata by key: %w", result.Error)
	}

	metadataList := make([]Metadata, 0, len(modelList))
	for _, model := range modelList {
		metadataList = append(metadataList, Metadata{
			Key:   model.Key,
			Value: model.Value,
		})
	}

	logger.Debug("Successfully retrieved %d metadata entries with key %s", len(metadataList), key)
	return metadataList, nil
}

// ListKeys retrieves all metadata keys for an entity
func (s *GormMetadataStore) ListKeys(ctx context.Context, entityType, entityID string) ([]string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing metadata keys for %s:%s", entityType, entityID)

	// Validate entity type
	if err := s.validateEntityType(entityType); err != nil {
		return nil, err
	}

	// Parse entity ID
	_, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid entity ID: %w", err)
	}

	var keys []string
	result := s.db.WithContext(ctx).Model(&models.Metadata{}).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("key ASC").
		Distinct().
		Pluck("key", &keys)

	if result.Error != nil {
		logger.Error("Failed to query metadata keys from database: %v", result.Error)
		return nil, fmt.Errorf("failed to list metadata keys: %w", result.Error)
	}

	logger.Debug("Successfully retrieved %d metadata keys", len(keys))
	return keys, nil
}

// InvalidateCache removes metadata-related cache entries
func (s *GormMetadataStore) InvalidateCache(ctx context.Context, entityType, entityID string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateMetadata(ctx, entityType, entityID)
}

// WarmCache preloads metadata for an entity into cache
func (s *GormMetadataStore) WarmCache(ctx context.Context, entityType, entityID string) error {
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
