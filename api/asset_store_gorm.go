package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormAssetStore implements AssetStore with GORM for database persistence and Redis caching
type GormAssetStore struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewGormAssetStore creates a new GORM-backed asset store with caching
func NewGormAssetStore(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormAssetStore {
	return &GormAssetStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new asset with write-through caching using GORM
func (s *GormAssetStore) Create(ctx context.Context, asset *Asset, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Creating asset: %s in threat model: %s", asset.Name, threatModelID)

	// Generate ID if not provided
	if asset.Id == nil {
		id := uuid.New()
		asset.Id = &id
	}

	// Validate threat model ID
	if _, err := uuid.Parse(threatModelID); err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Set timestamps
	now := time.Now().UTC()

	// Convert to GORM model
	gormAsset := s.toGormModel(asset, threatModelID)
	gormAsset.CreatedAt = now
	gormAsset.ModifiedAt = now

	// Insert into database
	if err := s.db.WithContext(ctx).Create(&gormAsset).Error; err != nil {
		logger.Error("Failed to create asset in database: %v", err)
		return fmt.Errorf("failed to create asset: %w", err)
	}

	// Save metadata if present
	if asset.Metadata != nil && len(*asset.Metadata) > 0 {
		if err := s.saveMetadata(ctx, asset.Id.String(), asset.Metadata); err != nil {
			logger.Error("Failed to save asset metadata: %v", err)
			// Don't fail the request if metadata saving fails
		}
	}

	// Cache the new asset
	if s.cache != nil {
		if cacheErr := s.cache.CacheAsset(ctx, asset); cacheErr != nil {
			logger.Error("Failed to cache new asset: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "asset",
			EntityID:      asset.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after asset creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created asset: %s", asset.Id)
	return nil
}

// Get retrieves an asset by ID with cache-first strategy using GORM
func (s *GormAssetStore) Get(ctx context.Context, id string) (*Asset, error) {
	logger := slogging.Get()
	logger.Debug("Getting asset: %s", id)

	// Try cache first
	if s.cache != nil {
		asset, err := s.cache.GetCachedAsset(ctx, id)
		if err != nil {
			logger.Error("Cache error when getting asset %s: %v", id, err)
		} else if asset != nil {
			logger.Debug("Cache hit for asset: %s", id)
			return asset, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for asset %s, querying database", id)

	var gormAsset models.Asset
	if err := s.db.WithContext(ctx).First(&gormAsset, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("asset not found: %s", id)
		}
		logger.Error("Failed to get asset from database: %v", err)
		return nil, fmt.Errorf("failed to get asset: %w", err)
	}

	// Convert to API model
	asset := s.toAPIModel(&gormAsset)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for asset %s: %v", id, err)
		metadata = []Metadata{}
	}
	asset.Metadata = &metadata

	// Cache the result for future requests
	if s.cache != nil {
		if cacheErr := s.cache.CacheAsset(ctx, asset); cacheErr != nil {
			logger.Error("Failed to cache asset after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved asset: %s", id)
	return asset, nil
}

// Update updates an existing asset with write-through caching using GORM
func (s *GormAssetStore) Update(ctx context.Context, asset *Asset, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Updating asset: %s", asset.Id)

	// Validate threat model ID
	if _, err := uuid.Parse(threatModelID); err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Update timestamp
	now := time.Now().UTC()

	// Convert to GORM model
	gormAsset := s.toGormModel(asset, threatModelID)
	gormAsset.ModifiedAt = now

	// Use struct-based Updates to ensure custom types (like StringArray) are properly
	// serialized via their Value() method. Map-based Updates bypasses custom type handling.
	result := s.db.WithContext(ctx).Model(&models.Asset{}).
		Where("id = ? AND threat_model_id = ?", asset.Id.String(), threatModelID).
		Updates(gormAsset)

	if result.Error != nil {
		logger.Error("Failed to update asset in database: %v", result.Error)
		return fmt.Errorf("failed to update asset: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("asset not found: %s", asset.Id)
	}

	// Save metadata if present
	if asset.Metadata != nil && len(*asset.Metadata) > 0 {
		if err := s.saveMetadata(ctx, asset.Id.String(), asset.Metadata); err != nil {
			logger.Error("Failed to save asset metadata: %v", err)
			// Don't fail the request if metadata saving fails
		}
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheAsset(ctx, asset); cacheErr != nil {
			logger.Error("Failed to update asset cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "asset",
			EntityID:      asset.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after asset update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated asset: %s", asset.Id)
	return nil
}

// Delete removes an asset and invalidates related caches using GORM
func (s *GormAssetStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting asset: %s", id)

	// Get the threat model ID for cache invalidation
	var gormAsset models.Asset
	if err := s.db.WithContext(ctx).Select("threat_model_id").First(&gormAsset, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("asset not found: %s", id)
		}
		logger.Error("Failed to get threat model ID for asset %s: %v", id, err)
		return fmt.Errorf("failed to get asset parent: %w", err)
	}

	// Delete from database
	result := s.db.WithContext(ctx).Delete(&models.Asset{}, "id = ?", id)
	if result.Error != nil {
		logger.Error("Failed to delete asset from database: %v", result.Error)
		return fmt.Errorf("failed to delete asset: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("asset not found: %s", id)
	}

	// Remove from cache
	if s.cache != nil {
		if cacheErr := s.cache.InvalidateEntity(ctx, "asset", id); cacheErr != nil {
			logger.Error("Failed to remove asset from cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "asset",
			EntityID:      id,
			ParentType:    "threat_model",
			ParentID:      gormAsset.ThreatModelID,
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after asset deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted asset: %s", id)
	return nil
}

// List retrieves assets for a threat model with pagination and caching using GORM
func (s *GormAssetStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Asset, error) {
	logger := slogging.Get()
	logger.Debug("Listing assets for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Validate threat model ID
	if _, err := uuid.Parse(threatModelID); err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return nil, fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Try cache first
	var assets []Asset
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "assets", threatModelID, offset, limit, &assets)
		if err == nil && assets != nil {
			logger.Debug("Cache hit for asset list %s [%d:%d]", threatModelID, offset, limit)
			return assets, nil
		}
		if err != nil {
			logger.Error("Cache error when getting asset list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for asset list, querying database")

	var gormAssets []models.Asset
	query := s.db.WithContext(ctx).
		Where("threat_model_id = ?", threatModelID).
		Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&gormAssets).Error; err != nil {
		logger.Error("Failed to query assets from database: %v", err)
		return nil, fmt.Errorf("failed to list assets: %w", err)
	}

	assets = make([]Asset, 0, len(gormAssets))
	for _, ga := range gormAssets {
		asset := s.toAPIModel(&ga)

		// Load metadata for this asset
		metadata, metaErr := s.loadMetadata(ctx, ga.ID)
		if metaErr != nil {
			logger.Error("Failed to load metadata for asset %s: %v", ga.ID, metaErr)
			metadata = []Metadata{}
		}
		asset.Metadata = &metadata

		assets = append(assets, *asset)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "assets", threatModelID, offset, limit, assets); cacheErr != nil {
			logger.Error("Failed to cache asset list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d assets", len(assets))
	return assets, nil
}

// BulkCreate creates multiple assets in a single transaction using GORM
func (s *GormAssetStore) BulkCreate(ctx context.Context, assets []Asset, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Bulk creating %d assets", len(assets))

	if len(assets) == 0 {
		return nil
	}

	// Validate threat model ID
	if _, err := uuid.Parse(threatModelID); err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	now := time.Now().UTC()
	gormAssets := make([]models.Asset, 0, len(assets))

	for i := range assets {
		asset := &assets[i]

		if asset.Id == nil {
			id := uuid.New()
			asset.Id = &id
		}

		gormAsset := s.toGormModel(asset, threatModelID)
		gormAsset.CreatedAt = now
		gormAsset.ModifiedAt = now

		gormAssets = append(gormAssets, *gormAsset)
	}

	// Create all in a transaction
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Create(&gormAssets).Error
	})

	if err != nil {
		logger.Error("Failed to bulk create assets: %v", err)
		return fmt.Errorf("failed to bulk create assets: %w", err)
	}

	// Cache each new asset
	if s.cache != nil {
		for i := range assets {
			if cacheErr := s.cache.CacheAsset(ctx, &assets[i]); cacheErr != nil {
				logger.Error("Failed to cache bulk-created asset: %v", cacheErr)
			}
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		for i := range assets {
			event := InvalidationEvent{
				EntityType:    "asset",
				EntityID:      assets[i].Id.String(),
				ParentType:    "threat_model",
				ParentID:      threatModelID,
				OperationType: "create",
				Strategy:      InvalidateImmediately,
			}
			if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
				logger.Error("Failed to invalidate caches after asset creation: %v", invErr)
			}
		}
	}

	logger.Debug("Successfully bulk created %d assets", len(assets))
	return nil
}

// Patch applies JSON patch operations to an asset using GORM
func (s *GormAssetStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Asset, error) {
	logger := slogging.Get()
	logger.Debug("Patching asset %s with %d operations", id, len(operations))

	// Get current asset
	asset, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	for _, op := range operations {
		if err := s.applyPatchOperation(asset, op); err != nil {
			logger.Error("Failed to apply patch operation %s to asset %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Get threat model ID for update
	threatModelID, err := s.getAssetThreatModelID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model ID: %w", err)
	}

	// Update the asset
	if err := s.Update(ctx, asset, threatModelID); err != nil {
		return nil, err
	}

	return asset, nil
}

// applyPatchOperation applies a single patch operation to an asset
func (s *GormAssetStore) applyPatchOperation(asset *Asset, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				asset.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/type":
		if op.Op == "replace" {
			if assetType, ok := op.Value.(string); ok {
				asset.Type = AssetType(assetType)
			} else {
				return fmt.Errorf("invalid value type for type: expected string")
			}
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				asset.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case "remove":
			asset.Description = nil
		}
	case "/classification":
		switch op.Op {
		case "replace", "add":
			if classArray, ok := op.Value.([]interface{}); ok {
				strArray := make([]string, len(classArray))
				for i, v := range classArray {
					if str, ok := v.(string); ok {
						strArray[i] = str
					} else {
						return fmt.Errorf("invalid value in classification array: expected string")
					}
				}
				asset.Classification = &strArray
			} else {
				return fmt.Errorf("invalid value type for classification: expected array of strings")
			}
		case "remove":
			asset.Classification = nil
		}
	case "/sensitivity":
		switch op.Op {
		case "replace", "add":
			if sens, ok := op.Value.(string); ok {
				asset.Sensitivity = &sens
			} else {
				return fmt.Errorf("invalid value type for sensitivity: expected string")
			}
		case "remove":
			asset.Sensitivity = nil
		}
	case "/criticality":
		switch op.Op {
		case "replace", "add":
			if criticality, ok := op.Value.(string); ok {
				asset.Criticality = &criticality
			} else {
				return fmt.Errorf("invalid value type for criticality: expected string")
			}
		case "remove":
			asset.Criticality = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getAssetThreatModelID retrieves the threat model ID for an asset using GORM
func (s *GormAssetStore) getAssetThreatModelID(ctx context.Context, assetID string) (string, error) {
	var gormAsset models.Asset
	if err := s.db.WithContext(ctx).Select("threat_model_id").First(&gormAsset, "id = ?", assetID).Error; err != nil {
		return "", fmt.Errorf("failed to get threat model ID for asset: %w", err)
	}
	return gormAsset.ThreatModelID, nil
}

// InvalidateCache invalidates the cache for a specific asset
func (s *GormAssetStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateEntity(ctx, "asset", id)
}

// WarmCache pre-loads assets for a threat model into the cache
func (s *GormAssetStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for assets in threat model: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of assets
	assets, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	logger.Debug("Warmed cache with %d assets for threat model %s", len(assets), threatModelID)
	return nil
}

// loadMetadata loads metadata for an asset using GORM
func (s *GormAssetStore) loadMetadata(ctx context.Context, assetID string) ([]Metadata, error) {
	var metadataEntries []models.Metadata
	if err := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", "asset", assetID).
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

// saveMetadata saves metadata for an asset using GORM
func (s *GormAssetStore) saveMetadata(ctx context.Context, assetID string, metadata *[]Metadata) error {
	logger := slogging.Get()

	// Delete existing metadata
	if err := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", "asset", assetID).
		Delete(&models.Metadata{}).Error; err != nil {
		logger.Error("Failed to delete existing metadata for asset %s: %v", assetID, err)
		return fmt.Errorf("failed to delete existing metadata: %w", err)
	}

	// Insert new metadata if present
	if metadata != nil && len(*metadata) > 0 {
		for _, m := range *metadata {
			entry := models.Metadata{
				ID:         uuid.New().String(),
				EntityType: "asset",
				EntityID:   assetID,
				Key:        m.Key,
				Value:      m.Value,
			}

			if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "modified_at"}),
			}).Create(&entry).Error; err != nil {
				logger.Error("Failed to insert metadata for asset %s (key: %s): %v", assetID, m.Key, err)
				return fmt.Errorf("failed to insert metadata: %w", err)
			}
		}
	}

	return nil
}

// Helper functions for model conversion

// toGormModel converts an API Asset to a GORM model
func (s *GormAssetStore) toGormModel(asset *Asset, threatModelID string) *models.Asset {
	gm := &models.Asset{
		ThreatModelID: threatModelID,
		Name:          asset.Name,
		Type:          string(asset.Type),
	}

	if asset.Id != nil {
		gm.ID = asset.Id.String()
	}
	if asset.Description != nil {
		gm.Description = asset.Description
	}
	if asset.Criticality != nil {
		gm.Criticality = asset.Criticality
	}
	if asset.Classification != nil {
		gm.Classification = models.StringArray(*asset.Classification)
	}
	if asset.Sensitivity != nil {
		gm.Sensitivity = asset.Sensitivity
	}

	return gm
}

// toAPIModel converts a GORM Asset model to an API model
func (s *GormAssetStore) toAPIModel(gm *models.Asset) *Asset {
	asset := &Asset{
		Name: gm.Name,
		Type: AssetType(gm.Type),
	}

	if gm.ID != "" {
		if id, err := uuid.Parse(gm.ID); err == nil {
			asset.Id = &id
		}
	}
	if gm.Description != nil {
		asset.Description = gm.Description
	}
	if gm.Criticality != nil {
		asset.Criticality = gm.Criticality
	}
	if len(gm.Classification) > 0 {
		classification := []string(gm.Classification)
		asset.Classification = &classification
	}
	if gm.Sensitivity != nil {
		asset.Sensitivity = gm.Sensitivity
	}

	return asset
}
