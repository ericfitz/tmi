package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// RepositoryStore defines the interface for repository operations with caching support
type RepositoryStore interface {
	// CRUD operations
	Create(ctx context.Context, repository *Repository, threatModelID string) error
	Get(ctx context.Context, id string) (*Repository, error)
	Update(ctx context.Context, repository *Repository, threatModelID string) error
	Delete(ctx context.Context, id string) error
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Repository, error)

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Repository, error)
	// Count returns total number of repositories for a threat model
	Count(ctx context.Context, threatModelID string) (int, error)

	// Bulk operations
	BulkCreate(ctx context.Context, repositorys []Repository, threatModelID string) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}

// ExtendedRepository includes database fields not in the API model
type ExtendedRepository struct {
	Repository
	ThreatModelId uuid.UUID `json:"threat_model_id"`
	CreatedAt     time.Time `json:"created_at"`
	ModifiedAt    time.Time `json:"modified_at"`
}

// DatabaseRepositoryStore implements RepositoryStore with database persistence and Redis caching
type DatabaseRepositoryStore struct {
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
}

// NewDatabaseRepositoryStore creates a new database-backed repository store with caching
func NewDatabaseRepositoryStore(db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DatabaseRepositoryStore {
	return &DatabaseRepositoryStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// extendedToRepository converts an ExtendedRepository to Repository
func extendedToRepository(extSrc *ExtendedRepository) *Repository {
	return &extSrc.Repository
}

// Create creates a new repository with write-through caching
func (s *DatabaseRepositoryStore) Create(ctx context.Context, repository *Repository, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Creating repository: %s in threat model: %s", repository.Uri, threatModelID)

	// Generate ID if not provided
	if repository.Id == nil {
		id := uuid.New()
		repository.Id = &id
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
	if repository.Parameters != nil {
		paramBytes, err := json.Marshal(repository.Parameters)
		if err != nil {
			logger.Error("Failed to marshal repository parameters: %v", err)
			return fmt.Errorf("failed to marshal parameters: %w", err)
		}
		parametersJSON.String = string(paramBytes)
		parametersJSON.Valid = true
	}

	// Convert type to string
	var repositoryType sql.NullString
	if repository.Type != nil {
		repositoryType.String = string(*repository.Type)
		repositoryType.Valid = true
	}

	// Insert into database
	query := `
		INSERT INTO repositories (
			id, threat_model_id, name, uri, description, type, parameters, created_at, modified_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	_, err = s.db.ExecContext(ctx, query,
		repository.Id,
		tmID,
		repository.Name,
		repository.Uri,
		repository.Description,
		repositoryType,
		parametersJSON,
		now,
		now,
	)

	if err != nil {
		logger.Error("Failed to create repository in database: %v", err)
		return fmt.Errorf("failed to create repository: %w", err)
	}

	// Cache the new repository
	if s.cache != nil {
		if cacheErr := s.cache.CacheRepository(ctx, repository); cacheErr != nil {
			logger.Error("Failed to cache new repository: %v", cacheErr)
			// Don't fail the request if caching fails
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

// Get retrieves a repository by ID with cache-first strategy
func (s *DatabaseRepositoryStore) Get(ctx context.Context, id string) (*Repository, error) {
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

	query := `
		SELECT id, threat_model_id, name, uri, description, type, parameters, created_at, modified_at
		FROM repositories 
		WHERE id = $1
	`

	var extSrc ExtendedRepository
	var name, description, repositoryType, parametersJSON sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&extSrc.Id,
		&extSrc.ThreatModelId,
		&name,
		&extSrc.Uri,
		&description,
		&repositoryType,
		&parametersJSON,
		&extSrc.CreatedAt,
		&extSrc.ModifiedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("repository not found: %s", id)
		}
		logger.Error("Failed to get repository from database: %v", err)
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}

	// Handle nullable fields
	if name.Valid {
		extSrc.Name = &name.String
	}
	if description.Valid {
		extSrc.Description = &description.String
	}
	if repositoryType.Valid {
		srcType := RepositoryType(repositoryType.String)
		extSrc.Type = &srcType
	}
	if parametersJSON.Valid {
		var params struct {
			RefType  RepositoryParametersRefType `json:"refType"`
			RefValue string                      `json:"refValue"`
			SubPath  *string                     `json:"subPath,omitempty"`
		}
		if err := json.Unmarshal([]byte(parametersJSON.String), &params); err == nil {
			extSrc.Parameters = &params
		}
	}

	repository := extendedToRepository(&extSrc)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for repository %s: %v", id, err)
		// Don't fail the request if metadata loading fails, just set empty metadata
		metadata = []Metadata{}
	}
	repository.Metadata = &metadata

	// Cache the result for future requests
	if s.cache != nil {
		if cacheErr := s.cache.CacheRepository(ctx, repository); cacheErr != nil {
			logger.Error("Failed to cache repository after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved repository: %s", id)
	return repository, nil
}

// Update updates an existing repository with write-through caching
func (s *DatabaseRepositoryStore) Update(ctx context.Context, repository *Repository, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Updating repository: %s", repository.Id)

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
	if repository.Parameters != nil {
		paramBytes, err := json.Marshal(repository.Parameters)
		if err != nil {
			logger.Error("Failed to marshal repository parameters: %v", err)
			return fmt.Errorf("failed to marshal parameters: %w", err)
		}
		parametersJSON.String = string(paramBytes)
		parametersJSON.Valid = true
	}

	// Convert type to string
	var repositoryType sql.NullString
	if repository.Type != nil {
		repositoryType.String = string(*repository.Type)
		repositoryType.Valid = true
	}

	query := `
		UPDATE repositories SET
			name = $2, uri = $3, description = $4, type = $5, parameters = $6, modified_at = $7
		WHERE id = $1 AND threat_model_id = $8
	`

	result, err := s.db.ExecContext(ctx, query,
		repository.Id,
		repository.Name,
		repository.Uri,
		repository.Description,
		repositoryType,
		parametersJSON,
		now,
		tmID,
	)

	if err != nil {
		logger.Error("Failed to update repository in database: %v", err)
		return fmt.Errorf("failed to update repository: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify update: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("repository not found: %s", repository.Id)
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

// Delete removes a repository and invalidates related caches
func (s *DatabaseRepositoryStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()
	logger.Debug("Deleting repository: %s", id)

	// Get the threat model ID from database for cache invalidation
	// We need this since the Repository struct doesn't contain the threat_model_id field
	var threatModelID uuid.UUID
	query := `SELECT threat_model_id FROM repositories WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, id).Scan(&threatModelID)
	if err != nil {
		logger.Error("Failed to get threat model ID for repository %s: %v", id, err)
		return fmt.Errorf("failed to get repository parent: %w", err)
	}

	// Delete from database
	deleteQuery := `DELETE FROM repositories WHERE id = $1`
	result, err := s.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		logger.Error("Failed to delete repository from database: %v", err)
		return fmt.Errorf("failed to delete repository: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected: %v", err)
		return fmt.Errorf("failed to verify deletion: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("repository not found: %s", id)
	}

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
			ParentID:      threatModelID.String(),
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

// List retrieves repositorys for a threat model with pagination and caching
func (s *DatabaseRepositoryStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Repository, error) {
	logger := slogging.Get()
	logger.Debug("Listing repositorys for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Try cache first
	var repositorys []Repository
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "repositorys", threatModelID, offset, limit, &repositorys)
		if err == nil && repositorys != nil {
			logger.Debug("Cache hit for repository list %s [%d:%d]", threatModelID, offset, limit)
			return repositorys, nil
		}
		if err != nil {
			logger.Error("Cache error when getting repository list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for repository list, querying database")

	query := `
		SELECT id, threat_model_id, name, uri, description, type, parameters, created_at, modified_at
		FROM repositories 
		WHERE threat_model_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.QueryContext(ctx, query, threatModelID, limit, offset)
	if err != nil {
		logger.Error("Failed to query repositorys from database: %v", err)
		return nil, fmt.Errorf("failed to list repositorys: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	repositorys = make([]Repository, 0)
	for rows.Next() {
		var extSrc ExtendedRepository
		var name, description, repositoryType, parametersJSON sql.NullString

		err := rows.Scan(
			&extSrc.Id,
			&extSrc.ThreatModelId,
			&name,
			&extSrc.Uri,
			&description,
			&repositoryType,
			&parametersJSON,
			&extSrc.CreatedAt,
			&extSrc.ModifiedAt,
		)

		if err != nil {
			logger.Error("Failed to scan repository row: %v", err)
			return nil, fmt.Errorf("failed to scan repository: %w", err)
		}

		// Handle nullable fields
		if name.Valid {
			extSrc.Name = &name.String
		}
		if description.Valid {
			extSrc.Description = &description.String
		}
		if repositoryType.Valid {
			srcType := RepositoryType(repositoryType.String)
			extSrc.Type = &srcType
		}
		if parametersJSON.Valid {
			var params struct {
				RefType  RepositoryParametersRefType `json:"refType"`
				RefValue string                      `json:"refValue"`
				SubPath  *string                     `json:"subPath,omitempty"`
			}
			if err := json.Unmarshal([]byte(parametersJSON.String), &params); err == nil {
				extSrc.Parameters = &params
			}
		}

		repository := extendedToRepository(&extSrc)

		// Load metadata for this repository
		metadata, metaErr := s.loadMetadata(ctx, extSrc.Id.String())
		if metaErr != nil {
			logger.Error("Failed to load metadata for repository %s: %v", extSrc.Id.String(), metaErr)
			// Don't fail the request if metadata loading fails, just set empty metadata
			metadata = []Metadata{}
		}
		repository.Metadata = &metadata

		repositorys = append(repositorys, *repository)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating repository rows: %v", err)
		return nil, fmt.Errorf("error iterating repositorys: %w", err)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "repositorys", threatModelID, offset, limit, repositorys); cacheErr != nil {
			logger.Error("Failed to cache repository list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d repositorys", len(repositorys))
	return repositorys, nil
}

// BulkCreate creates multiple repositorys in a single transaction
func (s *DatabaseRepositoryStore) BulkCreate(ctx context.Context, repositorys []Repository, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Bulk creating %d repositorys", len(repositorys))

	if len(repositorys) == 0 {
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
		INSERT INTO repositories (
			id, threat_model_id, name, uri, description, type, parameters, created_at, modified_at
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

	for i := range repositorys {
		repository := &repositorys[i]

		// Generate ID if not provided
		if repository.Id == nil {
			id := uuid.New()
			repository.Id = &id
		}

		// Serialize parameters if present
		var parametersJSON sql.NullString
		if repository.Parameters != nil {
			paramBytes, err := json.Marshal(repository.Parameters)
			if err != nil {
				logger.Error("Failed to marshal parameters for repository %d: %v", i, err)
				return fmt.Errorf("failed to marshal parameters for repository %d: %w", i, err)
			}
			parametersJSON.String = string(paramBytes)
			parametersJSON.Valid = true
		}

		// Convert type to string
		var repositoryType sql.NullString
		if repository.Type != nil {
			repositoryType.String = string(*repository.Type)
			repositoryType.Valid = true
		}

		_, err = stmt.ExecContext(ctx,
			repository.Id,
			tmID,
			repository.Name,
			repository.Uri,
			repository.Description,
			repositoryType,
			parametersJSON,
			now,
			now,
		)

		if err != nil {
			logger.Error("Failed to execute bulk insert for repository %d: %v", i, err)
			return fmt.Errorf("failed to insert repository %d: %w", i, err)
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
			logger.Error("Failed to invalidate caches after bulk repository creation: %v", invErr)
		}
	}

	logger.Debug("Successfully bulk created %d repositorys", len(repositorys))
	return nil
}

// Count returns the total number of repositories for a threat model
func (s *DatabaseRepositoryStore) Count(ctx context.Context, threatModelID string) (int, error) {
	logger := slogging.Get()
	logger.Debug("Counting repositories for threat model %s", threatModelID)

	query := `SELECT COUNT(*) FROM repositories WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRowContext(ctx, query, threatModelID).Scan(&count)
	if err != nil {
		logger.Error("Failed to count repositories: %v", err)
		return 0, fmt.Errorf("failed to count repositories: %w", err)
	}

	return count, nil
}

// InvalidateCache removes repository-related cache entries
func (s *DatabaseRepositoryStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}

	return s.cache.InvalidateEntity(ctx, "repository", id)
}

// WarmCache preloads repositorys for a threat model into cache
func (s *DatabaseRepositoryStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model repositorys: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of repositorys
	repositorys, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	// Individual repositorys are already cached by List(), so we're done
	logger.Debug("Warmed cache with %d repositorys for threat model %s", len(repositorys), threatModelID)
	return nil
}

// loadMetadata loads metadata for a repository from the metadata table
func (s *DatabaseRepositoryStore) loadMetadata(ctx context.Context, repositoryID string) ([]Metadata, error) {
	query := `
		SELECT key, value 
		FROM metadata 
		WHERE entity_type = 'repository' AND entity_id = $1
		ORDER BY key ASC
	`

	rows, err := s.db.QueryContext(ctx, query, repositoryID)
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

// Patch applies JSON patch operations to a repository
func (s *DatabaseRepositoryStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Repository, error) {
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

// applyPatchOperation applies a single patch operation to a repository
func (s *DatabaseRepositoryStore) applyPatchOperation(repository *Repository, op PatchOperation) error {
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
func (s *DatabaseRepositoryStore) getRepositoryThreatModelID(ctx context.Context, repositoryID string) (string, error) {
	query := `SELECT threat_model_id FROM repositories WHERE id = $1`
	var threatModelID string
	err := s.db.QueryRowContext(ctx, query, repositoryID).Scan(&threatModelID)
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for repository: %w", err)
	}
	return threatModelID, nil
}
