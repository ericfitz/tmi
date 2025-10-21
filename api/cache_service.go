package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// CacheService provides caching functionality for sub-resources
type CacheService struct {
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder
}

// NewCacheService creates a new cache service instance
func NewCacheService(redis *db.RedisDB) *CacheService {
	return &CacheService{
		redis:   redis,
		builder: db.NewRedisKeyBuilder(),
	}
}

// Cache TTL configurations based on the implementation plan
const (
	ThreatModelCacheTTL  = 10 * time.Minute // 10-15 minutes for threat models
	DiagramCacheTTL      = 2 * time.Minute  // 2-3 minutes for diagrams
	SubResourceCacheTTL  = 5 * time.Minute  // 5-10 minutes for sub-resources
	AuthCacheTTL         = 15 * time.Minute // 15 minutes for authorization data
	MetadataCacheTTL     = 7 * time.Minute  // 5-10 minutes for metadata
	ListCacheTTL         = 5 * time.Minute  // 5 minutes for paginated lists
)

// CacheThreat caches an individual threat with write-through strategy
func (cs *CacheService) CacheThreat(ctx context.Context, threat *Threat) error {
	logger := slogging.Get()
	key := cs.builder.CacheThreatKey(threat.Id.String())

	data, err := json.Marshal(threat)
	if err != nil {
		logger.Error("Failed to marshal threat for cache: %v", err)
		return fmt.Errorf("failed to marshal threat: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, SubResourceCacheTTL)
	if err != nil {
		logger.Error("Failed to cache threat %s: %v", threat.Id, err)
		return fmt.Errorf("failed to cache threat: %w", err)
	}

	logger.Debug("Cached threat %s with TTL %v", threat.Id, SubResourceCacheTTL)
	return nil
}

// GetCachedThreat retrieves a cached threat
func (cs *CacheService) GetCachedThreat(ctx context.Context, threatID string) (*Threat, error) {
	logger := slogging.Get()
	key := cs.builder.CacheThreatKey(threatID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("Cache miss for threat %s", threatID)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached threat %s: %v", threatID, err)
		return nil, fmt.Errorf("failed to get cached threat: %w", err)
	}

	var threat Threat
	err = json.Unmarshal([]byte(data), &threat)
	if err != nil {
		logger.Error("Failed to unmarshal cached threat %s: %v", threatID, err)
		return nil, fmt.Errorf("failed to unmarshal cached threat: %w", err)
	}

	logger.Debug("Cache hit for threat %s", threatID)
	return &threat, nil
}

// CacheDocument caches a document
func (cs *CacheService) CacheDocument(ctx context.Context, document *Document) error {
	logger := slogging.Get()
	key := cs.builder.CacheDocumentKey(document.Id.String())

	data, err := json.Marshal(document)
	if err != nil {
		logger.Error("Failed to marshal document for cache: %v", err)
		return fmt.Errorf("failed to marshal document: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, SubResourceCacheTTL)
	if err != nil {
		logger.Error("Failed to cache document %s: %v", document.Id, err)
		return fmt.Errorf("failed to cache document: %w", err)
	}

	logger.Debug("Cached document %s with TTL %v", document.Id, SubResourceCacheTTL)
	return nil
}

// GetCachedDocument retrieves a cached document
func (cs *CacheService) GetCachedDocument(ctx context.Context, documentID string) (*Document, error) {
	logger := slogging.Get()
	key := cs.builder.CacheDocumentKey(documentID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("Cache miss for document %s", documentID)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached document %s: %v", documentID, err)
		return nil, fmt.Errorf("failed to get cached document: %w", err)
	}

	var document Document
	err = json.Unmarshal([]byte(data), &document)
	if err != nil {
		logger.Error("Failed to unmarshal cached document %s: %v", documentID, err)
		return nil, fmt.Errorf("failed to unmarshal cached document: %w", err)
	}

	logger.Debug("Cache hit for document %s", documentID)
	return &document, nil
}

// CacheRepository caches a repository code entry
func (cs *CacheService) CacheRepository(ctx context.Context, repository *Repository) error {
	logger := slogging.Get()
	key := cs.builder.CacheRepositoryKey(repository.Id.String())

	data, err := json.Marshal(repository)
	if err != nil {
		logger.Error("Failed to marshal repository for cache: %v", err)
		return fmt.Errorf("failed to marshal repository: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, SubResourceCacheTTL)
	if err != nil {
		logger.Error("Failed to cache repository %s: %v", repository.Id, err)
		return fmt.Errorf("failed to cache repository: %w", err)
	}

	logger.Debug("Cached repository %s with TTL %v", repository.Id, SubResourceCacheTTL)
	return nil
}

// GetCachedRepository retrieves a cached repository code entry
func (cs *CacheService) GetCachedRepository(ctx context.Context, repositoryID string) (*Repository, error) {
	logger := slogging.Get()
	key := cs.builder.CacheRepositoryKey(repositoryID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("Cache miss for repository %s", repositoryID)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached repository %s: %v", repositoryID, err)
		return nil, fmt.Errorf("failed to get cached repository: %w", err)
	}

	var repository Repository
	err = json.Unmarshal([]byte(data), &repository)
	if err != nil {
		logger.Error("Failed to unmarshal cached repository %s: %v", repositoryID, err)
		return nil, fmt.Errorf("failed to unmarshal cached repository: %w", err)
	}

	logger.Debug("Cache hit for repository %s", repositoryID)
	return &repository, nil
}

// CacheMetadata caches metadata collection for an entity
func (cs *CacheService) CacheMetadata(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	logger := slogging.Get()
	key := cs.builder.CacheMetadataKey(entityType, entityID)

	data, err := json.Marshal(metadata)
	if err != nil {
		logger.Error("Failed to marshal metadata for cache: %v", err)
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, MetadataCacheTTL)
	if err != nil {
		logger.Error("Failed to cache metadata for %s:%s: %v", entityType, entityID, err)
		return fmt.Errorf("failed to cache metadata: %w", err)
	}

	logger.Debug("Cached metadata for %s:%s with TTL %v", entityType, entityID, MetadataCacheTTL)
	return nil
}

// GetCachedMetadata retrieves cached metadata for an entity
func (cs *CacheService) GetCachedMetadata(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	logger := slogging.Get()
	key := cs.builder.CacheMetadataKey(entityType, entityID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("Cache miss for metadata %s:%s", entityType, entityID)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached metadata %s:%s: %v", entityType, entityID, err)
		return nil, fmt.Errorf("failed to get cached metadata: %w", err)
	}

	var metadata []Metadata
	err = json.Unmarshal([]byte(data), &metadata)
	if err != nil {
		logger.Error("Failed to unmarshal cached metadata %s:%s: %v", entityType, entityID, err)
		return nil, fmt.Errorf("failed to unmarshal cached metadata: %w", err)
	}

	logger.Debug("Cache hit for metadata %s:%s", entityType, entityID)
	return metadata, nil
}

// CacheCells caches diagram cells collection
func (cs *CacheService) CacheCells(ctx context.Context, diagramID string, cells []Cell) error {
	logger := slogging.Get()
	key := cs.builder.CacheCellsKey(diagramID)

	data, err := json.Marshal(cells)
	if err != nil {
		logger.Error("Failed to marshal cells for cache: %v", err)
		return fmt.Errorf("failed to marshal cells: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, DiagramCacheTTL)
	if err != nil {
		logger.Error("Failed to cache cells for diagram %s: %v", diagramID, err)
		return fmt.Errorf("failed to cache cells: %w", err)
	}

	logger.Debug("Cached cells for diagram %s with TTL %v", diagramID, DiagramCacheTTL)
	return nil
}

// GetCachedCells retrieves cached diagram cells
func (cs *CacheService) GetCachedCells(ctx context.Context, diagramID string) ([]Cell, error) {
	logger := slogging.Get()
	key := cs.builder.CacheCellsKey(diagramID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("Cache miss for cells %s", diagramID)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached cells %s: %v", diagramID, err)
		return nil, fmt.Errorf("failed to get cached cells: %w", err)
	}

	var cells []Cell
	err = json.Unmarshal([]byte(data), &cells)
	if err != nil {
		logger.Error("Failed to unmarshal cached cells %s: %v", diagramID, err)
		return nil, fmt.Errorf("failed to unmarshal cached cells: %w", err)
	}

	logger.Debug("Cache hit for cells %s", diagramID)
	return cells, nil
}

// CacheAuthData caches authorization data for a threat model
func (cs *CacheService) CacheAuthData(ctx context.Context, threatModelID string, authData AuthorizationData) error {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID)

	data, err := json.Marshal(authData)
	if err != nil {
		logger.Error("Failed to marshal auth data for cache: %v", err)
		return fmt.Errorf("failed to marshal auth data: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, AuthCacheTTL)
	if err != nil {
		logger.Error("Failed to cache auth data for threat model %s: %v", threatModelID, err)
		return fmt.Errorf("failed to cache auth data: %w", err)
	}

	logger.Debug("Cached auth data for threat model %s with TTL %v", threatModelID, AuthCacheTTL)
	return nil
}

// GetCachedAuthData retrieves cached authorization data
func (cs *CacheService) GetCachedAuthData(ctx context.Context, threatModelID string) (*AuthorizationData, error) {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("Cache miss for auth data %s", threatModelID)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached auth data %s: %v", threatModelID, err)
		return nil, fmt.Errorf("failed to get cached auth data: %w", err)
	}

	var authData AuthorizationData
	err = json.Unmarshal([]byte(data), &authData)
	if err != nil {
		logger.Error("Failed to unmarshal cached auth data %s: %v", threatModelID, err)
		return nil, fmt.Errorf("failed to unmarshal cached auth data: %w", err)
	}

	logger.Debug("Cache hit for auth data %s", threatModelID)
	return &authData, nil
}

// CacheList caches a paginated list result
func (cs *CacheService) CacheList(ctx context.Context, entityType, parentID string, offset, limit int, data interface{}) error {
	logger := slogging.Get()
	key := cs.builder.CacheListKey(entityType, parentID, offset, limit)

	jsonData, err := json.Marshal(data)
	if err != nil {
		logger.Error("Failed to marshal list for cache: %v", err)
		return fmt.Errorf("failed to marshal list: %w", err)
	}

	err = cs.redis.Set(ctx, key, jsonData, ListCacheTTL)
	if err != nil {
		logger.Error("Failed to cache list %s:%s [%d:%d]: %v", entityType, parentID, offset, limit, err)
		return fmt.Errorf("failed to cache list: %w", err)
	}

	logger.Debug("Cached list %s:%s [%d:%d] with TTL %v", entityType, parentID, offset, limit, ListCacheTTL)
	return nil
}

// GetCachedList retrieves a cached paginated list result
func (cs *CacheService) GetCachedList(ctx context.Context, entityType, parentID string, offset, limit int, result interface{}) error {
	logger := slogging.Get()
	key := cs.builder.CacheListKey(entityType, parentID, offset, limit)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if err == redis.Nil {
			logger.Debug("Cache miss for list %s:%s [%d:%d]", entityType, parentID, offset, limit)
			return nil // Cache miss
		}
		logger.Error("Failed to get cached list %s:%s [%d:%d]: %v", entityType, parentID, offset, limit, err)
		return fmt.Errorf("failed to get cached list: %w", err)
	}

	err = json.Unmarshal([]byte(data), result)
	if err != nil {
		logger.Error("Failed to unmarshal cached list %s:%s [%d:%d]: %v", entityType, parentID, offset, limit, err)
		return fmt.Errorf("failed to unmarshal cached list: %w", err)
	}

	logger.Debug("Cache hit for list %s:%s [%d:%d]", entityType, parentID, offset, limit)
	return nil
}

// InvalidateEntity removes an entity from cache
func (cs *CacheService) InvalidateEntity(ctx context.Context, entityType, entityID string) error {
	logger := slogging.Get()

	var key string
	switch entityType {
	case "threat":
		key = cs.builder.CacheThreatKey(entityID)
	case "document":
		key = cs.builder.CacheDocumentKey(entityID)
	case "repository":
		key = cs.builder.CacheRepositoryKey(entityID)
	case "diagram":
		key = cs.builder.CacheDiagramKey(entityID)
	case "threat_model":
		key = cs.builder.CacheThreatModelKey(entityID)
	default:
		return fmt.Errorf("unknown entity type: %s", entityType)
	}

	err := cs.redis.Del(ctx, key)
	if err != nil {
		logger.Error("Failed to invalidate cache for %s:%s: %v", entityType, entityID, err)
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}

	logger.Debug("Invalidated cache for %s:%s", entityType, entityID)
	return nil
}

// InvalidateMetadata removes metadata cache for an entity
func (cs *CacheService) InvalidateMetadata(ctx context.Context, entityType, entityID string) error {
	logger := slogging.Get()
	key := cs.builder.CacheMetadataKey(entityType, entityID)

	err := cs.redis.Del(ctx, key)
	if err != nil {
		logger.Error("Failed to invalidate metadata cache for %s:%s: %v", entityType, entityID, err)
		return fmt.Errorf("failed to invalidate metadata cache: %w", err)
	}

	logger.Debug("Invalidated metadata cache for %s:%s", entityType, entityID)
	return nil
}

// InvalidateAuthData removes authorization data cache
func (cs *CacheService) InvalidateAuthData(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID)

	err := cs.redis.Del(ctx, key)
	if err != nil {
		logger.Error("Failed to invalidate auth data cache for threat model %s: %v", threatModelID, err)
		return fmt.Errorf("failed to invalidate auth data cache: %w", err)
	}

	logger.Debug("Invalidated auth data cache for threat model %s", threatModelID)
	return nil
}
