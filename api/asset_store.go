package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// AssetStore defines the interface for asset operations with caching support
type AssetStore interface {
	// CRUD operations
	Create(ctx context.Context, asset *Asset, threatModelID string) error
	Get(ctx context.Context, id string) (*Asset, error)
	Update(ctx context.Context, asset *Asset, threatModelID string) error
	Delete(ctx context.Context, id string) error
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Asset, error)

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Asset, error)
	// Count returns total number of assets for a threat model
	Count(ctx context.Context, threatModelID string) (int, error)

	// Bulk operations
	BulkCreate(ctx context.Context, assets []Asset, threatModelID string) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}

// Note: ExtendedAsset is now generated from OpenAPI spec in api.go
// It includes all Asset fields plus ThreatModelId, CreatedAt, ModifiedAt

// DatabaseAssetStore implements AssetStore with database persistence and Redis caching
type DatabaseAssetStore struct {
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDatabaseAssetStore creates a new database-backed asset store with caching
func NewDatabaseAssetStore(db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DatabaseAssetStore {
	return &DatabaseAssetStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// extendedToAsset converts an ExtendedAsset to Asset
func extendedToAsset(extAsset *ExtendedAsset) *Asset {
	return &Asset{
		Id:             extAsset.Id,
		Name:           extAsset.Name,
		Description:    extAsset.Description,
		Type:           AssetType(extAsset.Type),
		Criticality:    extAsset.Criticality,
		Classification: extAsset.Classification,
		Sensitivity:    extAsset.Sensitivity,
		Metadata:       extAsset.Metadata,
	}
}

// Create creates a new asset with write-through caching
func (s *DatabaseAssetStore) Create(ctx context.Context, asset *Asset, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Creating asset: %s in threat model: %s", asset.Name, threatModelID)

	// Generate ID if not provided
	if asset.Id == nil {
		id := uuid.New()
		asset.Id = &id
	}

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Set timestamps
	now := time.Now().UTC()

	// Convert arrays to PostgreSQL format
	var classification []string
	if asset.Classification != nil {
		classification = *asset.Classification
	}
	var sensitivity *string
	if asset.Sensitivity != nil {
		sensitivity = asset.Sensitivity
	}

	// Insert into database
	query := `
		INSERT INTO assets (
			id, threat_model_id, name, description, type, criticality, classification, sensitivity, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
	`

	_, err = s.db.ExecContext(ctx, query,
		asset.Id,
		tmID,
		asset.Name,
		asset.Description,
		asset.Type,
		asset.Criticality,
		pq.Array(classification),
		sensitivity,
		now,
		now,
	)

	if err != nil {
		logger.Error("Failed to create asset in database: %v", err)
		return fmt.Errorf("failed to create asset: %w", err)
	}

	// Cache the new asset
	if s.cache != nil {
		if cacheErr := s.cache.CacheAsset(ctx, asset); cacheErr != nil {
			logger.Error("Failed to cache new asset: %v", cacheErr)
			// Don't fail the request if caching fails
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

// Get retrieves an asset by ID with cache-first strategy
func (s *DatabaseAssetStore) Get(ctx context.Context, id string) (*Asset, error) {
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

	query := `
		SELECT id, threat_model_id, name, description, type, criticality, classification, sensitivity, created_at, modified_at
		FROM assets
		WHERE id = $1
	`

	var extAsset ExtendedAsset
	var description, criticality, sensitivity sql.NullString
	var classification []string

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&extAsset.Id,
		&extAsset.ThreatModelId,
		&extAsset.Name,
		&description,
		&extAsset.Type,
		&criticality,
		pq.Array(&classification),
		&sensitivity,
		&extAsset.CreatedAt,
		&extAsset.ModifiedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("asset not found: %s", id)
		}
		logger.Error("Failed to get asset from database: %v", err)
		return nil, fmt.Errorf("failed to get asset: %w", err)
	}

	// Handle nullable fields
	if description.Valid {
		extAsset.Description = &description.String
	}
	if criticality.Valid {
		extAsset.Criticality = &criticality.String
	}
	if len(classification) > 0 {
		extAsset.Classification = &classification
	}
	if sensitivity.Valid {
		extAsset.Sensitivity = &sensitivity.String
	}

	asset := extendedToAsset(&extAsset)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for asset %s: %v", id, err)
		// Don't fail the request if metadata loading fails, just set empty metadata
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

// Update updates an existing asset with write-through caching
func (s *DatabaseAssetStore) Update(ctx context.Context, asset *Asset, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Updating asset: %s", asset.Id)

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
		logger.Error("Invalid threat model ID: %s", threatModelID)
		return fmt.Errorf("invalid threat model ID: %w", err)
	}

	// Update timestamp
	now := time.Now().UTC()

	// Convert arrays to PostgreSQL format
	var classification []string
	if asset.Classification != nil {
		classification = *asset.Classification
	}
	var sensitivity *string
	if asset.Sensitivity != nil {
		sensitivity = asset.Sensitivity
	}

	query := `
		UPDATE assets SET
			name = $2, description = $3, type = $4, criticality = $5, classification = $6, sensitivity = $7, modified_at = $8
		WHERE id = $1 AND threat_model_id = $9
	`

	result, err := s.db.ExecContext(ctx, query,
		asset.Id,
		asset.Name,
		asset.Description,
		asset.Type,
		asset.Criticality,
		pq.Array(classification),
		sensitivity,
		now,
		tmID,
	)

	if err != nil {
		logger.Error("Failed to update asset in database: %v", err)
		return fmt.Errorf("failed to update asset: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify update: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("asset not found: %s", asset.Id)
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

// Delete removes an asset and invalidates related caches
func (s *DatabaseAssetStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting asset: %s", id)

	// Get the threat model ID from database for cache invalidation
	var threatModelID uuid.UUID
	query := `SELECT threat_model_id FROM assets WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, id).Scan(&threatModelID)
	if err != nil {
		logger.Error("Failed to get threat model ID for asset %s: %v", id, err)
		return fmt.Errorf("failed to get asset parent: %w", err)
	}

	// Delete from database
	deleteQuery := `DELETE FROM assets WHERE id = $1`
	result, err := s.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		logger.Error("Failed to delete asset from database: %v", err)
		return fmt.Errorf("failed to delete asset: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify deletion: %w", err)
	}

	if rowsAffected == 0 {
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
			ParentID:      threatModelID.String(),
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

// List retrieves assets for a threat model with pagination and caching
func (s *DatabaseAssetStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Asset, error) {
	logger := slogging.Get()
	logger.Debug("Listing assets for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Parse threat model ID
	tmID, err := uuid.Parse(threatModelID)
	if err != nil {
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

	query := `
		SELECT id, threat_model_id, name, description, type, criticality, classification, sensitivity, created_at, modified_at
		FROM assets
		WHERE threat_model_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.QueryContext(ctx, query, tmID, limit, offset)
	if err != nil {
		logger.Error("Failed to query assets from database: %v", err)
		return nil, fmt.Errorf("failed to list assets: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	assets = make([]Asset, 0)
	for rows.Next() {
		var extAsset ExtendedAsset
		var description, criticality, sensitivity sql.NullString
		var classification []string

		err := rows.Scan(
			&extAsset.Id,
			&extAsset.ThreatModelId,
			&extAsset.Name,
			&description,
			&extAsset.Type,
			&criticality,
			pq.Array(&classification),
			&sensitivity,
			&extAsset.CreatedAt,
			&extAsset.ModifiedAt,
		)

		if err != nil {
			logger.Error("Failed to scan asset row: %v", err)
			return nil, fmt.Errorf("failed to scan asset: %w", err)
		}

		// Handle nullable fields
		if description.Valid {
			extAsset.Description = &description.String
		}
		if criticality.Valid {
			extAsset.Criticality = &criticality.String
		}
		if len(classification) > 0 {
			extAsset.Classification = &classification
		}
		if sensitivity.Valid {
			extAsset.Sensitivity = &sensitivity.String
		}

		asset := extendedToAsset(&extAsset)

		// Load metadata for this asset
		metadata, metaErr := s.loadMetadata(ctx, extAsset.Id.String())
		if metaErr != nil {
			logger.Error("Failed to load metadata for asset %s: %v", extAsset.Id.String(), metaErr)
			// Don't fail the request if metadata loading fails, just set empty metadata
			metadata = []Metadata{}
		}
		asset.Metadata = &metadata

		assets = append(assets, *asset)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating asset rows: %v", err)
		return nil, fmt.Errorf("error iterating assets: %w", err)
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

// BulkCreate creates multiple assets in a single transaction
func (s *DatabaseAssetStore) BulkCreate(ctx context.Context, assets []Asset, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Bulk creating %d assets", len(assets))

	if len(assets) == 0 {
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
		INSERT INTO assets (
			id, threat_model_id, name, description, type, criticality, classification, sensitivity, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
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

	for i := range assets {
		asset := &assets[i]

		// Generate ID if not provided
		if asset.Id == nil {
			id := uuid.New()
			asset.Id = &id
		}

		// Convert arrays to PostgreSQL format
		var classification []string
		if asset.Classification != nil {
			classification = *asset.Classification
		}
		var sensitivity *string
		if asset.Sensitivity != nil {
			sensitivity = asset.Sensitivity
		}

		_, err = stmt.ExecContext(ctx,
			asset.Id,
			tmID,
			asset.Name,
			asset.Description,
			asset.Type,
			asset.Criticality,
			pq.Array(classification),
			sensitivity,
			now,
			now,
		)

		if err != nil {
			logger.Error("Failed to insert asset %s: %v", asset.Name, err)
			return fmt.Errorf("failed to insert asset: %w", err)
		}

		// Cache each new asset
		if s.cache != nil {
			if cacheErr := s.cache.CacheAsset(ctx, asset); cacheErr != nil {
				logger.Error("Failed to cache bulk-created asset: %v", cacheErr)
			}
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		logger.Error("Failed to commit transaction: %v", err)
		return fmt.Errorf("failed to commit bulk create: %w", err)
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

// Patch applies JSON patch operations to an asset
func (s *DatabaseAssetStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Asset, error) {
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
func (s *DatabaseAssetStore) applyPatchOperation(asset *Asset, op PatchOperation) error {
	switch op.Path {
	case PatchPathName:
		if op.Op == string(Replace) {
			if name, ok := op.Value.(string); ok {
				asset.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case PatchPathType:
		if op.Op == string(Replace) {
			if assetType, ok := op.Value.(string); ok {
				asset.Type = AssetType(assetType)
			} else {
				return fmt.Errorf("invalid value type for type: expected string")
			}
		}
	case PatchPathDescription:
		switch op.Op {
		case string(Replace), string(Add):
			if desc, ok := op.Value.(string); ok {
				asset.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case string(Remove):
			asset.Description = nil
		}
	case "/classification":
		switch op.Op {
		case string(Replace), string(Add):
			if classArray, ok := op.Value.([]interface{}); ok {
				strArray := make([]string, len(classArray))
				for i, v := range classArray {
					if s, ok := v.(string); ok {
						strArray[i] = s
					} else {
						return fmt.Errorf("invalid value in classification array: expected string")
					}
				}
				asset.Classification = &strArray
			} else {
				return fmt.Errorf("invalid value type for classification: expected array of strings")
			}
		case string(Remove):
			asset.Classification = nil
		}
	case "/sensitivity":
		switch op.Op {
		case string(Replace), string(Add):
			if sens, ok := op.Value.(string); ok {
				asset.Sensitivity = &sens
			} else {
				return fmt.Errorf("invalid value type for sensitivity: expected string")
			}
		case string(Remove):
			asset.Sensitivity = nil
		}
	case "/criticality":
		switch op.Op {
		case string(Replace), string(Add):
			if criticality, ok := op.Value.(string); ok {
				asset.Criticality = &criticality
			} else {
				return fmt.Errorf("invalid value type for criticality: expected string")
			}
		case string(Remove):
			asset.Criticality = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getAssetThreatModelID retrieves the threat model ID for an asset
func (s *DatabaseAssetStore) getAssetThreatModelID(ctx context.Context, assetID string) (string, error) {
	query := `SELECT threat_model_id FROM assets WHERE id = $1`
	var threatModelID string
	err := s.db.QueryRowContext(ctx, query, assetID).Scan(&threatModelID)
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for asset: %w", err)
	}
	return threatModelID, nil
}

// Count returns the total number of assets for a threat model
func (s *DatabaseAssetStore) Count(ctx context.Context, threatModelID string) (int, error) {
	logger := slogging.Get()
	logger.Debug("Counting assets for threat model %s", threatModelID)

	query := `SELECT COUNT(*) FROM assets WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRowContext(ctx, query, threatModelID).Scan(&count)
	if err != nil {
		logger.Error("Failed to count assets: %v", err)
		return 0, fmt.Errorf("failed to count assets: %w", err)
	}

	return count, nil
}

// InvalidateCache invalidates the cache for a specific asset
func (s *DatabaseAssetStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}

	return s.cache.InvalidateEntity(ctx, "asset", id)
}

// WarmCache pre-loads assets for a threat model into the cache
func (s *DatabaseAssetStore) WarmCache(ctx context.Context, threatModelID string) error {
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

	// Individual assets are already cached by List(), so we're done
	logger.Debug("Warmed cache with %d assets for threat model %s", len(assets), threatModelID)
	return nil
}

// loadMetadata loads metadata for an asset from the metadata table
func (s *DatabaseAssetStore) loadMetadata(ctx context.Context, assetID string) ([]Metadata, error) {
	query := `
		SELECT key, value
		FROM metadata
		WHERE entity_type = 'asset' AND entity_id = $1
		ORDER BY key ASC
	`

	rows, err := s.db.QueryContext(ctx, query, assetID)
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
