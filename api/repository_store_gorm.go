package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormRepositoryRepository implements RepositoryStore using GORM
type GormRepositoryRepository struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	mutex            sync.RWMutex
}

// NewGormRepositoryRepository creates a new GORM-backed repository store with optional caching
func NewGormRepositoryRepository(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormRepositoryRepository {
	return &GormRepositoryRepository{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new repository
func (s *GormRepositoryRepository) Create(ctx context.Context, repository *Repository, threatModelID string) error {
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
	if repository.IncludeInReport != nil {
		model.IncludeInReport = models.DBBool(*repository.IncludeInReport)
	}
	if repository.TimmyEnabled != nil {
		model.TimmyEnabled = models.DBBool(*repository.TimmyEnabled)
	}

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		alias, err := AllocateNextAlias(ctx, tx, threatModelID, "repository")
		if err != nil {
			return fmt.Errorf("allocate repository alias: %w", err)
		}
		model.Alias = alias
		if err := tx.Create(&model).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		logger.Error("Failed to create repository in database: %v", err)
		return err
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
func (s *GormRepositoryRepository) Get(ctx context.Context, id string) (*Repository, error) {
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
	result := s.db.WithContext(ctx).First(&model, "id = ? AND deleted_at IS NULL", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		logger.Error("Failed to get repository from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
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
func (s *GormRepositoryRepository) Update(ctx context.Context, repository *Repository, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Updating repository: %s", repository.Id)

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

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	updates := map[string]any{
		"name":        repository.Name,
		"uri":         repository.Uri,
		"description": repository.Description,
		"type":        repoType,
		"parameters":  params,
	}
	if repository.IncludeInReport != nil {
		updates["include_in_report"] = models.DBBool(*repository.IncludeInReport)
	} else {
		updates["include_in_report"] = models.DBBool(false)
	}
	if repository.TimmyEnabled != nil {
		updates["timmy_enabled"] = models.DBBool(*repository.TimmyEnabled)
	} else {
		updates["timmy_enabled"] = models.DBBool(false)
	}

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.Repository{}).
			Where("id = ? AND threat_model_id = ?", repository.Id.String(), threatModelID).
			Updates(updates)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrRepositoryNotFound
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrRepositoryNotFound) {
			logger.Error("Failed to update repository in database: %v", err)
		}
		return err
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

// Delete soft-deletes a repository by setting deleted_at
func (s *GormRepositoryRepository) Delete(ctx context.Context, id string) error {
	return s.SoftDelete(ctx, id)
}

// hardDeleteRepository permanently removes a repository and its metadata from the database
func (s *GormRepositoryRepository) hardDeleteRepository(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting repository: %s", id)

	// Get threat model ID for cache invalidation
	var model models.Repository
	if err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRepositoryNotFound
		}
		logger.Error("Failed to get threat model ID for repository %s: %v", id, err)
		return dberrors.Classify(err)
	}

	// Delete from database (with retry)
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Delete(&models.Repository{}, "id = ?", id)
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrRepositoryNotFound
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrRepositoryNotFound) {
			logger.Error("Failed to delete repository from database: %v", err)
		}
		return err
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
func (s *GormRepositoryRepository) List(ctx context.Context, threatModelID string, offset, limit int) ([]Repository, error) {
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
	query := s.db.WithContext(ctx)
	if includeDeletedFromContext(ctx) {
		query = query.Where("threat_model_id = ?", threatModelID)
	} else {
		query = query.Where("threat_model_id = ? AND deleted_at IS NULL", threatModelID)
	}
	result := query.
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to query repositories from database: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
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
func (s *GormRepositoryRepository) BulkCreate(ctx context.Context, repositories []Repository, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Bulk creating %d repositories", len(repositories))

	if len(repositories) == 0 {
		return nil
	}

	now := time.Now().UTC()

	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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
			if repository.IncludeInReport != nil {
				model.IncludeInReport = models.DBBool(*repository.IncludeInReport)
			}
			if repository.TimmyEnabled != nil {
				model.TimmyEnabled = models.DBBool(*repository.TimmyEnabled)
			}

			if err := tx.Create(&model).Error; err != nil {
				logger.Error("Failed to bulk create repository %d: %v", i, err)
				return dberrors.Classify(err)
			}
		}

		return nil
	})
}

// Patch applies JSON patch operations to a repository
func (s *GormRepositoryRepository) Patch(ctx context.Context, id string, operations []PatchOperation) (*Repository, error) {
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
		return nil, err
	}

	// Update the repository
	if err := s.Update(ctx, repository, threatModelID); err != nil {
		return nil, err
	}

	return repository, nil
}

// Count returns the total number of repositories for a threat model
func (s *GormRepositoryRepository) Count(ctx context.Context, threatModelID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Counting repositories for threat model %s", threatModelID)

	var count int64
	query := s.db.WithContext(ctx).Model(&models.Repository{})
	if includeDeletedFromContext(ctx) {
		query = query.Where("threat_model_id = ?", threatModelID)
	} else {
		query = query.Where("threat_model_id = ? AND deleted_at IS NULL", threatModelID)
	}
	result := query.Count(&count)

	if result.Error != nil {
		logger.Error("Failed to count repositories: %v", result.Error)
		return 0, dberrors.Classify(result.Error)
	}

	return int(count), nil
}

// InvalidateCache removes repository-related cache entries
func (s *GormRepositoryRepository) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateEntity(ctx, "repository", id)
}

// WarmCache preloads repositories for a threat model into cache
func (s *GormRepositoryRepository) WarmCache(ctx context.Context, threatModelID string) error {
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
func (s *GormRepositoryRepository) modelToAPI(model *models.Repository) *Repository {
	id, _ := uuid.Parse(model.ID)

	includeInReport := model.IncludeInReport.Bool()
	timmyEnabled := model.TimmyEnabled.Bool()
	repo := &Repository{
		Id:              &id,
		Name:            model.Name,
		Uri:             model.URI,
		Description:     model.Description,
		IncludeInReport: &includeInReport,
		TimmyEnabled:    &timmyEnabled,
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
func (s *GormRepositoryRepository) loadMetadata(ctx context.Context, repositoryID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db.WithContext(ctx), "repository", repositoryID)
}

// saveMetadata saves metadata for a repository
func (s *GormRepositoryRepository) saveMetadata(ctx context.Context, repositoryID string, metadata []Metadata) error {
	return saveEntityMetadata(s.db.WithContext(ctx), "repository", repositoryID, metadata)
}

// updateMetadata updates metadata for a repository
func (s *GormRepositoryRepository) updateMetadata(ctx context.Context, repositoryID string, metadata []Metadata) error {
	return deleteAndSaveEntityMetadata(s.db.WithContext(ctx), "repository", repositoryID, metadata)
}

// applyPatchOperation applies a single patch operation to a repository
func (s *GormRepositoryRepository) applyPatchOperation(repository *Repository, op PatchOperation) error {
	switch op.Path {
	case PatchPathName:
		if op.Op == string(Replace) {
			if name, ok := op.Value.(string); ok {
				repository.Name = &name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case PatchPathType:
		if op.Op == string(Replace) {
			if repoType, ok := op.Value.(string); ok {
				rt := RepositoryType(repoType)
				repository.Type = &rt
			} else {
				return fmt.Errorf("invalid value type for type: expected string")
			}
		}
	case PatchPathURI:
		if op.Op == string(Replace) {
			if uri, ok := op.Value.(string); ok {
				repository.Uri = uri
			} else {
				return fmt.Errorf("invalid value type for uri: expected string")
			}
		}
	case PatchPathDescription:
		switch op.Op {
		case string(Replace), string(Add):
			if desc, ok := op.Value.(string); ok {
				repository.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case string(Remove):
			repository.Description = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getRepositoryThreatModelID retrieves the threat model ID for a repository
func (s *GormRepositoryRepository) getRepositoryThreatModelID(ctx context.Context, repositoryID string) (string, error) {
	var model models.Repository
	err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", repositoryID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrRepositoryNotFound
		}
		return "", dberrors.Classify(err)
	}
	return model.ThreatModelID, nil
}
