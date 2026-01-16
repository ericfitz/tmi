package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormRepositoryStore implements RepositoryStore using GORM
type GormRepositoryStore struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	mutex            sync.RWMutex
}

// NewGormRepositoryStore creates a new GORM-backed repository store with optional caching
func NewGormRepositoryStore(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormRepositoryStore {
	return &GormRepositoryStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new repository
func (s *GormRepositoryStore) Create(ctx context.Context, repository *Repository, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Creating repository: %s in threat model: %s", repository.Uri, threatModelID)

	// Generate ID if not provided
	if repository.Id == nil {
		id := uuid.New()
		repository.Id = &id
	}

	now := time.Now().UTC()

	// Convert type to string pointer
	var repoType *string
	if repository.Type != nil {
		t := string(*repository.Type)
		repoType = &t
	}

	// Convert parameters to JSONMap
	var params models.JSONMap
	if repository.Parameters != nil {
		params = models.JSONMap{
			"refType":  repository.Parameters.RefType,
			"refValue": repository.Parameters.RefValue,
		}
		if repository.Parameters.SubPath != nil {
			params["subPath"] = *repository.Parameters.SubPath
		}
	}

	model := models.Repository{
		ID:            repository.Id.String(),
		ThreatModelID: threatModelID,
		Name:          repository.Name,
		URI:           repository.Uri,
		Description:   repository.Description,
		Type:          repoType,
		Parameters:    params,
		CreatedAt:     now,
		ModifiedAt:    now,
	}

	if err := s.db.WithContext(ctx).Create(&model).Error; err != nil {
		logger.Error("Failed to create repository in database: %v", err)
		return fmt.Errorf("failed to create repository: %w", err)
	}

	// Save metadata if present
	if repository.Metadata != nil && len(*repository.Metadata) > 0 {
		if err := s.saveMetadata(ctx, repository.Id.String(), *repository.Metadata); err != nil {
			logger.Error("Failed to save repository metadata: %v", err)
		}
	}

	// Cache the new repository
	if s.cache != nil {
		if cacheErr := s.cache.CacheRepository(ctx, repository); cacheErr != nil {
			logger.Error("Failed to cache new repository: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "repository",
			EntityID:      repository.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after repository creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created repository: %s", repository.Id)
	return nil
}

// Get retrieves a repository by ID
func (s *GormRepositoryStore) Get(ctx context.Context, id string) (*Repository, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting repository: %s", id)

	// Try cache first
	if s.cache != nil {
		repository, err := s.cache.GetCachedRepository(ctx, id)
		if err != nil {
			logger.Error("Cache error when getting repository %s: %v", id, err)
		} else if repository != nil {
			logger.Debug("Cache hit for repository: %s", id)
			return repository, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for repository %s, querying database", id)

	var model models.Repository
	result := s.db.WithContext(ctx).First(&model, "id = ?", id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("repository not found: %s", id)
		}
		logger.Error("Failed to get repository from database: %v", result.Error)
		return nil, fmt.Errorf("failed to get repository: %w", result.Error)
	}

	repository := s.modelToAPI(&model)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for repository %s: %v", id, err)
		metadata = []Metadata{}
	}
	repository.Metadata = &metadata

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheRepository(ctx, repository); cacheErr != nil {
			logger.Error("Failed to cache repository after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved repository: %s", id)
	return repository, nil
}

// Update updates an existing repository
func (s *GormRepositoryStore) Update(ctx context.Context, repository *Repository, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Updating repository: %s", repository.Id)

	now := time.Now().UTC()

	// Convert type to string pointer
	var repoType *string
	if repository.Type != nil {
		t := string(*repository.Type)
		repoType = &t
	}

	// Convert parameters to JSONMap
	var params models.JSONMap
	if repository.Parameters != nil {
		params = models.JSONMap{
			"refType":  repository.Parameters.RefType,
			"refValue": repository.Parameters.RefValue,
		}
		if repository.Parameters.SubPath != nil {
			params["subPath"] = *repository.Parameters.SubPath
		}
	}

	updates := map[string]interface{}{
		"name":        repository.Name,
		"uri":         repository.Uri,
		"description": repository.Description,
		"type":        repoType,
		"parameters":  params,
		"modified_at": now,
	}

	result := s.db.WithContext(ctx).Model(&models.Repository{}).
		Where("id = ? AND threat_model_id = ?", repository.Id.String(), threatModelID).
		Updates(updates)

	if result.Error != nil {
		logger.Error("Failed to update repository in database: %v", result.Error)
		return fmt.Errorf("failed to update repository: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("repository not found: %s", repository.Id)
	}

	// Update metadata if present
	if repository.Metadata != nil {
		if err := s.updateMetadata(ctx, repository.Id.String(), *repository.Metadata); err != nil {
			logger.Error("Failed to update repository metadata: %v", err)
		}
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheRepository(ctx, repository); cacheErr != nil {
			logger.Error("Failed to update repository cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "repository",
			EntityID:      repository.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after repository update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated repository: %s", repository.Id)
	return nil
}

// Delete removes a repository
func (s *GormRepositoryStore) Delete(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting repository: %s", id)

	// Get threat model ID for cache invalidation
	var model models.Repository
	if err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("repository not found: %s", id)
		}
		logger.Error("Failed to get threat model ID for repository %s: %v", id, err)
		return fmt.Errorf("failed to get repository parent: %w", err)
	}

	// Delete from database
	result := s.db.WithContext(ctx).Delete(&models.Repository{}, "id = ?", id)
	if result.Error != nil {
		logger.Error("Failed to delete repository from database: %v", result.Error)
		return fmt.Errorf("failed to delete repository: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("repository not found: %s", id)
	}

	// Delete metadata
	s.db.WithContext(ctx).Where("entity_type = ? AND entity_id = ?", "repository", id).Delete(&models.Metadata{})

	// Remove from cache
	if s.cache != nil {
		if cacheErr := s.cache.InvalidateEntity(ctx, "repository", id); cacheErr != nil {
			logger.Error("Failed to remove repository from cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "repository",
			EntityID:      id,
			ParentType:    "threat_model",
			ParentID:      model.ThreatModelID,
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after repository deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted repository: %s", id)
	return nil
}

// List retrieves repositories for a threat model with pagination
func (s *GormRepositoryStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Repository, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing repositories for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Try cache first
	var repositories []Repository
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "repositories", threatModelID, offset, limit, &repositories)
		if err == nil && repositories != nil {
			logger.Debug("Cache hit for repository list %s [%d:%d]", threatModelID, offset, limit)
			return repositories, nil
		}
		if err != nil {
			logger.Error("Cache error when getting repository list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for repository list, querying database")

	var modelList []models.Repository
	result := s.db.WithContext(ctx).
		Where("threat_model_id = ?", threatModelID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to query repositories from database: %v", result.Error)
		return nil, fmt.Errorf("failed to list repositories: %w", result.Error)
	}

	repositories = make([]Repository, 0, len(modelList))
	for _, model := range modelList {
		repo := s.modelToAPI(&model)

		// Load metadata for this repository
		metadata, metaErr := s.loadMetadata(ctx, model.ID)
		if metaErr != nil {
			logger.Error("Failed to load metadata for repository %s: %v", model.ID, metaErr)
			metadata = []Metadata{}
		}
		repo.Metadata = &metadata

		repositories = append(repositories, *repo)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "repositories", threatModelID, offset, limit, repositories); cacheErr != nil {
			logger.Error("Failed to cache repository list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d repositories", len(repositories))
	return repositories, nil
}

// BulkCreate creates multiple repositories in a single transaction
func (s *GormRepositoryStore) BulkCreate(ctx context.Context, repositories []Repository, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Bulk creating %d repositories", len(repositories))

	if len(repositories) == 0 {
		return nil
	}

	now := time.Now().UTC()

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range repositories {
			repository := &repositories[i]

			// Generate ID if not provided
			if repository.Id == nil {
				id := uuid.New()
				repository.Id = &id
			}

			// Convert type to string pointer
			var repoType *string
			if repository.Type != nil {
				t := string(*repository.Type)
				repoType = &t
			}

			// Convert parameters to JSONMap
			var params models.JSONMap
			if repository.Parameters != nil {
				params = models.JSONMap{
					"refType":  repository.Parameters.RefType,
					"refValue": repository.Parameters.RefValue,
				}
				if repository.Parameters.SubPath != nil {
					params["subPath"] = *repository.Parameters.SubPath
				}
			}

			model := models.Repository{
				ID:            repository.Id.String(),
				ThreatModelID: threatModelID,
				Name:          repository.Name,
				URI:           repository.Uri,
				Description:   repository.Description,
				Type:          repoType,
				Parameters:    params,
				CreatedAt:     now,
				ModifiedAt:    now,
			}

			if err := tx.Create(&model).Error; err != nil {
				logger.Error("Failed to bulk create repository %d: %v", i, err)
				return fmt.Errorf("failed to insert repository %d: %w", i, err)
			}
		}

		return nil
	})
}

// Patch applies JSON patch operations to a repository
func (s *GormRepositoryStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Repository, error) {
	logger := slogging.Get()
	logger.Debug("Patching repository %s with %d operations", id, len(operations))

	// Get current repository
	repository, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	for _, op := range operations {
		if err := s.applyPatchOperation(repository, op); err != nil {
			logger.Error("Failed to apply patch operation %s to repository %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Get threat model ID for update
	threatModelID, err := s.getRepositoryThreatModelID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model ID: %w", err)
	}

	// Update the repository
	if err := s.Update(ctx, repository, threatModelID); err != nil {
		return nil, err
	}

	return repository, nil
}

// InvalidateCache removes repository-related cache entries
func (s *GormRepositoryStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateEntity(ctx, "repository", id)
}

// WarmCache preloads repositories for a threat model into cache
func (s *GormRepositoryStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model repositories: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of repositories
	repositories, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	logger.Debug("Warmed cache with %d repositories for threat model %s", len(repositories), threatModelID)
	return nil
}

// modelToAPI converts a GORM Repository model to the API Repository type
func (s *GormRepositoryStore) modelToAPI(model *models.Repository) *Repository {
	id, _ := uuid.Parse(model.ID)

	repo := &Repository{
		Id:          &id,
		Name:        model.Name,
		Uri:         model.URI,
		Description: model.Description,
	}

	// Convert type
	if model.Type != nil {
		repoType := RepositoryType(*model.Type)
		repo.Type = &repoType
	}

	// Convert parameters from JSONMap
	if len(model.Parameters) > 0 {
		params := &struct {
			RefType  RepositoryParametersRefType `json:"refType"`
			RefValue string                      `json:"refValue"`
			SubPath  *string                     `json:"subPath,omitempty"`
		}{}

		if refType, ok := model.Parameters["refType"].(string); ok {
			params.RefType = RepositoryParametersRefType(refType)
		}
		if refValue, ok := model.Parameters["refValue"].(string); ok {
			params.RefValue = refValue
		}
		if subPath, ok := model.Parameters["subPath"].(string); ok {
			params.SubPath = &subPath
		}

		repo.Parameters = params
	}

	return repo
}

// loadMetadata loads metadata for a repository
func (s *GormRepositoryStore) loadMetadata(ctx context.Context, repositoryID string) ([]Metadata, error) {
	var metadataEntries []models.Metadata
	result := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", "repository", repositoryID).
		Order("key ASC").
		Find(&metadataEntries)

	if result.Error != nil {
		return nil, result.Error
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

// saveMetadata saves metadata for a repository
func (s *GormRepositoryStore) saveMetadata(ctx context.Context, repositoryID string, metadata []Metadata) error {
	if len(metadata) == 0 {
		return nil
	}

	for _, meta := range metadata {
		entry := models.Metadata{
			ID:         uuid.New().String(),
			EntityType: "repository",
			EntityID:   repositoryID,
			Key:        meta.Key,
			Value:      meta.Value,
		}

		result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "modified_at"}),
		}).Create(&entry)

		if result.Error != nil {
			return fmt.Errorf("failed to save repository metadata: %w", result.Error)
		}
	}

	return nil
}

// updateMetadata updates metadata for a repository
func (s *GormRepositoryStore) updateMetadata(ctx context.Context, repositoryID string, metadata []Metadata) error {
	// Delete existing metadata
	result := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", "repository", repositoryID).
		Delete(&models.Metadata{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete existing repository metadata: %w", result.Error)
	}

	// Insert new metadata
	return s.saveMetadata(ctx, repositoryID, metadata)
}

// applyPatchOperation applies a single patch operation to a repository
func (s *GormRepositoryStore) applyPatchOperation(repository *Repository, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				repository.Name = &name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/type":
		if op.Op == "replace" {
			if repoType, ok := op.Value.(string); ok {
				rt := RepositoryType(repoType)
				repository.Type = &rt
			} else {
				return fmt.Errorf("invalid value type for type: expected string")
			}
		}
	case "/uri":
		if op.Op == "replace" {
			if uri, ok := op.Value.(string); ok {
				repository.Uri = uri
			} else {
				return fmt.Errorf("invalid value type for uri: expected string")
			}
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				repository.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case "remove":
			repository.Description = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getRepositoryThreatModelID retrieves the threat model ID for a repository
func (s *GormRepositoryStore) getRepositoryThreatModelID(ctx context.Context, repositoryID string) (string, error) {
	var model models.Repository
	err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", repositoryID).Error
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for repository: %w", err)
	}
	return model.ThreatModelID, nil
}
