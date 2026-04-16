package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormMetadataRepository implements MetadataRepository using GORM
type GormMetadataRepository struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	mutex            sync.RWMutex
	logger           *slogging.Logger
}

// NewGormMetadataRepository creates a new GORM-backed metadata repository
func NewGormMetadataRepository(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormMetadataRepository {
	return &GormMetadataRepository{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
		logger:           slogging.Get(),
	}
}

// validateEntityType checks if the entity type is supported.
// Delegates to the canonical validation.ValidEntityTypes list to ensure consistency
// with the GORM BeforeSave hook in models/hooks.go.
func (r *GormMetadataRepository) validateEntityType(entityType string) error {
	return validation.ValidateEntityType(entityType)
}

// Create creates a new metadata entry
func (r *GormMetadataRepository) Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.logger.Debug("Creating metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
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

	err := authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Create(&model)
		if result.Error != nil {
			classified := dberrors.Classify(result.Error)
			if errors.Is(classified, dberrors.ErrDuplicate) {
				return &MetadataConflictError{ConflictingKeys: []string{metadata.Key}}
			}
			return classified
		}
		return nil
	})

	if err != nil {
		r.logger.Error("Failed to create metadata in database: %v", err)
		return err
	}

	// Invalidate related caches
	if r.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      model.ID,
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			r.logger.Error("Failed to invalidate caches after metadata creation: %v", invErr)
		}
	}

	r.logger.Debug("Successfully created metadata: %s=%s", metadata.Key, metadata.Value)
	return nil
}

// Get retrieves a specific metadata entry by key
func (r *GormMetadataRepository) Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	r.logger.Debug("Getting metadata: %s for %s:%s", key, entityType, entityID)

	// Try cache first
	if r.cache != nil {
		metadataList, err := r.cache.GetCachedMetadata(ctx, entityType, entityID)
		if err != nil {
			r.logger.Error("Cache error when getting metadata %s:%s: %v", entityType, entityID, err)
		} else if metadataList != nil {
			for _, meta := range metadataList {
				if meta.Key == key {
					r.logger.Debug("Cache hit for metadata: %s", key)
					return &meta, nil
				}
			}
			return nil, ErrMetadataNotFound
		}
	}

	// Cache miss - get from database
	r.logger.Debug("Cache miss for metadata %s, querying database", key)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return nil, err
	}

	var model models.Metadata
	result := r.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, key).
		First(&model)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrMetadataNotFound
		}
		r.logger.Error("Failed to get metadata from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	metadata := &Metadata{
		Key:   model.Key,
		Value: model.Value,
	}

	r.logger.Debug("Successfully retrieved metadata: %s=%s", metadata.Key, metadata.Value)
	return metadata, nil
}

// Update updates an existing metadata entry
func (r *GormMetadataRepository) Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.logger.Debug("Updating metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	err := authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Skip hooks to avoid validation errors on empty model struct.
		// Entity type is already validated above.
		// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
		result := tx.Session(&gorm.Session{SkipHooks: true}).Model(&models.Metadata{}).
			Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, metadata.Key).
			Updates(map[string]any{
				"value": metadata.Value,
			})

		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}

		if result.RowsAffected == 0 {
			return ErrMetadataNotFound
		}

		return nil
	})

	if err != nil {
		r.logger.Error("Failed to update metadata in database: %v", err)
		return err
	}

	// Invalidate related caches
	if r.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      fmt.Sprintf("%s:%s:%s", entityType, entityID, metadata.Key),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			r.logger.Error("Failed to invalidate caches after metadata update: %v", invErr)
		}
	}

	r.logger.Debug("Successfully updated metadata: %s=%s", metadata.Key, metadata.Value)
	return nil
}

// Delete removes a metadata entry
func (r *GormMetadataRepository) Delete(ctx context.Context, entityType, entityID, key string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.logger.Debug("Deleting metadata: %s for %s:%s", key, entityType, entityID)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	err := authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, key).
			Delete(&models.Metadata{})

		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}

		if result.RowsAffected == 0 {
			return ErrMetadataNotFound
		}

		return nil
	})

	if err != nil {
		r.logger.Error("Failed to delete metadata from database: %v", err)
		return err
	}

	// Invalidate related caches
	if r.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "metadata",
			EntityID:      fmt.Sprintf("%s:%s:%s", entityType, entityID, key),
			ParentType:    entityType,
			ParentID:      entityID,
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			r.logger.Error("Failed to invalidate caches after metadata deletion: %v", invErr)
		}
	}

	r.logger.Debug("Successfully deleted metadata: %s", key)
	return nil
}

// List retrieves all metadata for an entity
func (r *GormMetadataRepository) List(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	r.logger.Debug("Listing metadata for %s:%s", entityType, entityID)

	// Try cache first
	if r.cache != nil {
		metadataList, err := r.cache.GetCachedMetadata(ctx, entityType, entityID)
		if err != nil {
			r.logger.Error("Cache error when getting metadata list %s:%s: %v", entityType, entityID, err)
		} else if metadataList != nil {
			r.logger.Debug("Cache hit for metadata list %s:%s", entityType, entityID)
			return metadataList, nil
		}
	}

	// Cache miss - get from database
	r.logger.Debug("Cache miss for metadata list, querying database")

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return nil, err
	}

	var modelList []models.Metadata
	result := r.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("key ASC").
		Find(&modelList)

	if result.Error != nil {
		r.logger.Error("Failed to query metadata from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	metadataList := make([]Metadata, 0, len(modelList))
	for _, model := range modelList {
		metadataList = append(metadataList, Metadata{
			Key:   model.Key,
			Value: model.Value,
		})
	}

	// Cache the result
	if r.cache != nil {
		if cacheErr := r.cache.CacheMetadata(ctx, entityType, entityID, metadataList); cacheErr != nil {
			r.logger.Error("Failed to cache metadata list: %v", cacheErr)
		}
	}

	r.logger.Debug("Successfully retrieved %d metadata entries", len(metadataList))
	return metadataList, nil
}

// Post creates a new metadata entry using POST semantics
func (r *GormMetadataRepository) Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error {
	r.logger.Debug("Posting metadata: %s=%s for %s:%s", metadata.Key, metadata.Value, entityType, entityID)

	// POST semantics: create regardless of existing keys, let the database handle conflicts
	return r.Create(ctx, entityType, entityID, metadata)
}

// BulkCreate creates multiple metadata entries in a single transaction
func (r *GormMetadataRepository) BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.logger.Debug("Bulk creating %d metadata entries", len(metadata))

	if len(metadata) == 0 {
		return nil
	}

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Check for existing keys (create-only semantics)
		keys := make([]string, len(metadata))
		for i, meta := range metadata {
			keys[i] = meta.Key
		}

		var existingKeys []string
		if err := tx.Model(&models.Metadata{}).
			Where("entity_type = ? AND entity_id = ? AND key IN ?", entityType, entityID, keys).
			Pluck("key", &existingKeys).Error; err != nil {
			r.logger.Error("Failed to check existing keys: %v", err)
			return dberrors.Classify(err)
		}

		if len(existingKeys) > 0 {
			return &MetadataConflictError{ConflictingKeys: existingKeys}
		}

		// Insert new entries (no upsert)
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

			result := tx.Create(&model)

			if result.Error != nil {
				// Catch race condition
				classified := dberrors.Classify(result.Error)
				if errors.Is(classified, dberrors.ErrDuplicate) {
					return &MetadataConflictError{ConflictingKeys: []string{meta.Key}}
				}
				r.logger.Error("Failed to bulk create metadata: %v", result.Error)
				return classified
			}
		}

		// Invalidate related caches
		if r.cacheInvalidator != nil {
			event := InvalidationEvent{
				EntityType:    "metadata",
				EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
				ParentType:    entityType,
				ParentID:      entityID,
				OperationType: "create",
				Strategy:      InvalidateImmediately,
			}
			if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				r.logger.Error("Failed to invalidate caches after bulk metadata creation: %v", invErr)
			}
		}

		r.logger.Debug("Successfully bulk created %d metadata entries", len(metadata))
		return nil
	})
}

// BulkUpdate upserts multiple metadata entries in a single transaction.
// Keys present in the request are created or updated; keys not present are left untouched.
// This implements PATCH (merge/upsert) semantics.
func (r *GormMetadataRepository) BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.logger.Debug("Bulk upserting %d metadata entries", len(metadata))

	if len(metadata) == 0 {
		return nil
	}

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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
				r.logger.Error("Failed to bulk upsert metadata: %v", result.Error)
				return dberrors.Classify(result.Error)
			}
		}

		// Invalidate related caches
		if r.cacheInvalidator != nil {
			event := InvalidationEvent{
				EntityType:    "metadata",
				EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
				ParentType:    entityType,
				ParentID:      entityID,
				OperationType: "update",
				Strategy:      InvalidateImmediately,
			}
			if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				r.logger.Error("Failed to invalidate caches after bulk metadata upsert: %v", invErr)
			}
		}

		r.logger.Debug("Successfully bulk upserted %d metadata entries", len(metadata))
		return nil
	})
}

// BulkReplace replaces all metadata for an entity atomically.
// All existing metadata is deleted, then the provided entries are inserted.
// An empty metadata slice clears all metadata for the entity.
// This implements PUT (full replace) semantics.
func (r *GormMetadataRepository) BulkReplace(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.logger.Debug("Bulk replacing metadata for %s:%s with %d entries", entityType, entityID, len(metadata))

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	now := time.Now().UTC()

	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Delete all existing metadata for this entity
		if err := tx.Where("entity_type = ? AND entity_id = ?", entityType, entityID).
			Delete(&models.Metadata{}).Error; err != nil {
			r.logger.Error("Failed to delete existing metadata: %v", err)
			return dberrors.Classify(err)
		}

		// Insert new entries
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

			if err := tx.Create(&model).Error; err != nil {
				r.logger.Error("Failed to insert metadata during replace: %v", err)
				return dberrors.Classify(err)
			}
		}

		// Invalidate related caches
		if r.cacheInvalidator != nil {
			event := InvalidationEvent{
				EntityType:    "metadata",
				EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
				ParentType:    entityType,
				ParentID:      entityID,
				OperationType: "replace",
				Strategy:      InvalidateImmediately,
			}
			if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				r.logger.Error("Failed to invalidate caches after bulk metadata replace: %v", invErr)
			}
		}

		r.logger.Debug("Successfully bulk replaced metadata for %s:%s with %d entries", entityType, entityID, len(metadata))
		return nil
	})
}

// BulkDelete deletes multiple metadata entries by key in a single transaction
func (r *GormMetadataRepository) BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.logger.Debug("Bulk deleting %d metadata keys", len(keys))

	if len(keys) == 0 {
		return nil
	}

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return err
	}

	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		for _, key := range keys {
			result := tx.Where("entity_type = ? AND entity_id = ? AND key = ?", entityType, entityID, key).
				Delete(&models.Metadata{})
			if result.Error != nil {
				r.logger.Error("Failed to bulk delete metadata key %s: %v", key, result.Error)
				return dberrors.Classify(result.Error)
			}
		}

		// Invalidate related caches
		if r.cacheInvalidator != nil {
			event := InvalidationEvent{
				EntityType:    "metadata",
				EntityID:      fmt.Sprintf("%s:%s", entityType, entityID),
				ParentType:    entityType,
				ParentID:      entityID,
				OperationType: "delete",
				Strategy:      InvalidateImmediately,
			}
			if invErr := r.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				r.logger.Error("Failed to invalidate caches after bulk metadata deletion: %v", invErr)
			}
		}

		r.logger.Debug("Successfully bulk deleted %d metadata keys", len(keys))
		return nil
	})
}

// GetByKey retrieves all metadata entries with a specific key across all entities
func (r *GormMetadataRepository) GetByKey(ctx context.Context, key string) ([]Metadata, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	r.logger.Debug("Getting metadata by key: %s", key)

	var modelList []models.Metadata
	result := r.db.WithContext(ctx).
		Where("key = ?", key).
		Order("entity_type, entity_id").
		Find(&modelList)

	if result.Error != nil {
		r.logger.Error("Failed to query metadata by key from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	metadataList := make([]Metadata, 0, len(modelList))
	for _, model := range modelList {
		metadataList = append(metadataList, Metadata{
			Key:   model.Key,
			Value: model.Value,
		})
	}

	r.logger.Debug("Successfully retrieved %d metadata entries with key %s", len(metadataList), key)
	return metadataList, nil
}

// ListKeys retrieves all metadata keys for an entity
func (r *GormMetadataRepository) ListKeys(ctx context.Context, entityType, entityID string) ([]string, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	r.logger.Debug("Listing metadata keys for %s:%s", entityType, entityID)

	// Validate entity type
	if err := r.validateEntityType(entityType); err != nil {
		return nil, err
	}

	// Parse entity ID
	_, err := uuid.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("invalid entity ID: %w", err)
	}

	var keys []string
	result := r.db.WithContext(ctx).Model(&models.Metadata{}).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("key ASC").
		Distinct().
		Pluck("key", &keys)

	if result.Error != nil {
		r.logger.Error("Failed to query metadata keys from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	r.logger.Debug("Successfully retrieved %d metadata keys", len(keys))
	return keys, nil
}

// InvalidateCache removes metadata-related cache entries
func (r *GormMetadataRepository) InvalidateCache(ctx context.Context, entityType, entityID string) error {
	if r.cache == nil {
		return nil
	}
	return r.cache.InvalidateMetadata(ctx, entityType, entityID)
}

// WarmCache preloads metadata for an entity into cache
func (r *GormMetadataRepository) WarmCache(ctx context.Context, entityType, entityID string) error {
	r.logger.Debug("Warming cache for %s:%s metadata", entityType, entityID)

	if r.cache == nil {
		return nil
	}

	// Load metadata for the entity
	_, err := r.List(ctx, entityType, entityID)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	r.logger.Debug("Warmed cache for %s:%s metadata", entityType, entityID)
	return nil
}
