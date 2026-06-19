package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// GlobalCacheService is the package-level cache service instance, set during server initialization.
// Used by middleware and store methods that don't have dependency-injected cache references.
// Nil-safe: all callers check for nil before use.
var GlobalCacheService *CacheService

// MiddlewareAuthData holds authorization data needed by middleware.
// Cached separately from full threat model to avoid loading sub-resources.
// SEM@2ec29b7908cd546e20f3bbf1ad51b2c76e52c70d: lightweight owner and authorization data cached separately for middleware auth checks (pure)
type MiddlewareAuthData struct {
	Owner         User            `json:"owner"`
	Authorization []Authorization `json:"authorization"`
}

// CacheService provides caching functionality for sub-resources
// SEM@6a25ed41f4450e7eba44de39fb07a07cac216f26: Redis-backed cache for threat model sub-resources and authorization data (pure)
type CacheService struct {
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder
}

// NewCacheService creates a new cache service instance
// SEM@6a25ed41f4450e7eba44de39fb07a07cac216f26: build a CacheService wired to a Redis connection and key builder (pure)
func NewCacheService(redis *db.RedisDB) *CacheService {
	return &CacheService{
		redis:   redis,
		builder: db.NewRedisKeyBuilder(),
	}
}

// Cache TTL configurations based on the implementation plan
const (
	ThreatModelCacheTTL = 10 * time.Minute // 10-15 minutes for threat models
	DiagramCacheTTL     = 2 * time.Minute  // 2-3 minutes for diagrams
	SubResourceCacheTTL = 5 * time.Minute  // 5-10 minutes for sub-resources
	AuthCacheTTL        = 15 * time.Minute // 15 minutes for authorization data
	MetadataCacheTTL    = 7 * time.Minute  // 5-10 minutes for metadata
	ListCacheTTL        = 5 * time.Minute  // 5 minutes for paginated lists
)

// CacheThreat caches an individual threat with write-through strategy
// SEM@98c83c6a9092288eead710533517e486c44239b2: store a threat in Redis with sub-resource TTL (mutates shared state)
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
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch a cached threat from Redis, returning nil on cache miss (reads DB)
func (cs *CacheService) GetCachedThreat(ctx context.Context, threatID string) (*Threat, error) {
	logger := slogging.Get()
	key := cs.builder.CacheThreatKey(threatID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for threat %s", threatID)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "threat")))
			}
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
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "threat")))
	}
	return &threat, nil
}

// CacheDocument caches a document
// SEM@98c83c6a9092288eead710533517e486c44239b2: store a document in Redis with sub-resource TTL (mutates shared state)
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
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch a cached document from Redis, returning nil on cache miss (reads DB)
func (cs *CacheService) GetCachedDocument(ctx context.Context, documentID string) (*Document, error) {
	logger := slogging.Get()
	key := cs.builder.CacheDocumentKey(documentID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for document %s", documentID)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "document")))
			}
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
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "document")))
	}
	return &document, nil
}

// CacheNote caches a note
// SEM@bc24b01d8fe51390e6178a0cbe35e701f76556ce: store a note in Redis with sub-resource TTL (mutates shared state)
func (cs *CacheService) CacheNote(ctx context.Context, note *Note) error {
	logger := slogging.Get()
	key := cs.builder.CacheNoteKey(note.Id.String())

	data, err := json.Marshal(note)
	if err != nil {
		logger.Error("Failed to marshal note for cache: %v", err)
		return fmt.Errorf("failed to marshal note: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, SubResourceCacheTTL)
	if err != nil {
		logger.Error("Failed to cache note %s: %v", note.Id, err)
		return fmt.Errorf("failed to cache note: %w", err)
	}

	logger.Debug("Cached note %s with TTL %v", note.Id, SubResourceCacheTTL)
	return nil
}

// GetCachedNote retrieves a cached note
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch a cached note from Redis, returning nil on cache miss (reads DB)
func (cs *CacheService) GetCachedNote(ctx context.Context, noteID string) (*Note, error) {
	logger := slogging.Get()
	key := cs.builder.CacheNoteKey(noteID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for note %s", noteID)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "note")))
			}
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached note %s: %v", noteID, err)
		return nil, fmt.Errorf("failed to get cached note: %w", err)
	}

	var note Note
	err = json.Unmarshal([]byte(data), &note)
	if err != nil {
		logger.Error("Failed to unmarshal cached note %s: %v", noteID, err)
		return nil, fmt.Errorf("failed to unmarshal cached note: %w", err)
	}

	logger.Debug("Cache hit for note %s", noteID)
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "note")))
	}
	return &note, nil
}

// CacheRepository caches a repository code entry
// SEM@98c83c6a9092288eead710533517e486c44239b2: store a repository entry in Redis with sub-resource TTL (mutates shared state)
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
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch a cached repository entry from Redis, returning nil on cache miss (reads DB)
func (cs *CacheService) GetCachedRepository(ctx context.Context, repositoryID string) (*Repository, error) {
	logger := slogging.Get()
	key := cs.builder.CacheRepositoryKey(repositoryID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for repository %s", repositoryID)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "repository")))
			}
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
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "repository")))
	}
	return &repository, nil
}

// CacheAsset caches an asset
// SEM@f2c738b899d06c4246bd8283b568260c596d5168: store an asset in Redis with sub-resource TTL (mutates shared state)
func (cs *CacheService) CacheAsset(ctx context.Context, asset *Asset) error {
	logger := slogging.Get()
	key := cs.builder.CacheAssetKey(asset.Id.String())

	data, err := json.Marshal(asset)
	if err != nil {
		logger.Error("Failed to marshal asset for cache: %v", err)
		return fmt.Errorf("failed to marshal asset: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, SubResourceCacheTTL)
	if err != nil {
		logger.Error("Failed to cache asset %s: %v", asset.Id.String(), err)
		return fmt.Errorf("failed to cache asset: %w", err)
	}

	logger.Debug("Cached asset %s with TTL %v", asset.Id.String(), SubResourceCacheTTL)
	return nil
}

// GetCachedAsset retrieves a cached asset
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch a cached asset from Redis, returning nil on cache miss (reads DB)
func (cs *CacheService) GetCachedAsset(ctx context.Context, assetID string) (*Asset, error) {
	logger := slogging.Get()
	key := cs.builder.CacheAssetKey(assetID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for asset %s", assetID)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "asset")))
			}
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached asset %s: %v", assetID, err)
		return nil, fmt.Errorf("failed to get cached asset: %w", err)
	}

	var asset Asset
	err = json.Unmarshal([]byte(data), &asset)
	if err != nil {
		logger.Error("Failed to unmarshal cached asset %s: %v", assetID, err)
		return nil, fmt.Errorf("failed to unmarshal cached asset: %w", err)
	}

	logger.Debug("Cache hit for asset %s", assetID)
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "asset")))
	}
	return &asset, nil
}

// CacheMetadata caches metadata collection for an entity
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: store a metadata collection for an entity in Redis with metadata TTL (mutates shared state)
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
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch cached metadata for an entity from Redis, returning nil on cache miss (reads DB)
func (cs *CacheService) GetCachedMetadata(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	logger := slogging.Get()
	key := cs.builder.CacheMetadataKey(entityType, entityID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for metadata %s:%s", entityType, entityID)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "metadata")))
			}
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
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "metadata")))
	}
	return metadata, nil
}

// CacheCells caches diagram cells collection
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: store a diagram's cells collection in Redis with diagram TTL (mutates shared state)
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
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch cached diagram cells from Redis, returning nil on cache miss (reads DB)
func (cs *CacheService) GetCachedCells(ctx context.Context, diagramID string) ([]Cell, error) {
	logger := slogging.Get()
	key := cs.builder.CacheCellsKey(diagramID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for cells %s", diagramID)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "cells")))
			}
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
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "cells")))
	}
	return cells, nil
}

// CacheAuthData caches authorization data for a threat model
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: store authorization data for a threat model in Redis with auth TTL (mutates shared state)
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
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch cached authorization data for a threat model from Redis, returning nil on miss (reads DB)
func (cs *CacheService) GetCachedAuthData(ctx context.Context, threatModelID string) (*AuthorizationData, error) {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for auth data %s", threatModelID)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "auth_data")))
			}
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
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "auth_data")))
	}
	return &authData, nil
}

// CacheList caches a paginated list result
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: store a paginated list result in Redis keyed by entity type, parent, and page params (mutates shared state)
func (cs *CacheService) CacheList(ctx context.Context, entityType, parentID string, offset, limit int, data any) error {
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
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch a cached paginated list from Redis, deserializing into result; nil error on miss (reads DB)
func (cs *CacheService) GetCachedList(ctx context.Context, entityType, parentID string, offset, limit int, result any) error {
	logger := slogging.Get()
	key := cs.builder.CacheListKey(entityType, parentID, offset, limit)

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			logger.Debug("Cache miss for list %s:%s [%d:%d]", entityType, parentID, offset, limit)
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "list")))
			}
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
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "list")))
	}
	return nil
}

// InvalidateEntity removes an entity from cache
// SEM@cdbe48c974fb76e1161972733b30bb0d1c02c3b1: delete a typed entity from Redis cache by entity type and ID (mutates shared state)
func (cs *CacheService) InvalidateEntity(ctx context.Context, entityType, entityID string) error {
	logger := slogging.Get()

	var key string
	switch entityType {
	case string(CreateAddonRequestObjectsThreat):
		key = cs.builder.CacheThreatKey(entityID)
	case string(CreateAddonRequestObjectsDocument):
		key = cs.builder.CacheDocumentKey(entityID)
	case string(CreateAddonRequestObjectsRepository):
		key = cs.builder.CacheRepositoryKey(entityID)
	case string(CreateAddonRequestObjectsAsset):
		key = cs.builder.CacheAssetKey(entityID)
	case string(CreateAddonRequestObjectsDiagram):
		key = cs.builder.CacheDiagramKey(entityID)
	case string(CreateAddonRequestObjectsThreatModel):
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
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: delete metadata cache for an entity from Redis (mutates shared state)
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
// SEM@2ec29b7908cd546e20f3bbf1ad51b2c76e52c70d: delete authorization and middleware auth cache entries for a threat model from Redis (mutates shared state)
func (cs *CacheService) InvalidateAuthData(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID)

	err := cs.redis.Del(ctx, key)
	if err != nil {
		logger.Error("Failed to invalidate auth data cache for threat model %s: %v", threatModelID, err)
		return fmt.Errorf("failed to invalidate auth data cache: %w", err)
	}

	logger.Debug("Invalidated auth data cache for threat model %s", threatModelID)

	// Also invalidate middleware auth cache (separate key)
	mwKey := cs.builder.CacheAuthKey(threatModelID) + ":mw"
	_ = cs.redis.Del(ctx, mwKey)

	return nil
}

// CacheMiddlewareAuth caches lightweight auth data for middleware
// SEM@2ec29b7908cd546e20f3bbf1ad51b2c76e52c70d: store lightweight middleware auth data for a threat model in Redis with auth TTL (mutates shared state)
func (cs *CacheService) CacheMiddlewareAuth(ctx context.Context, threatModelID string, data MiddlewareAuthData) error {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID) + ":mw"

	jsonData, err := json.Marshal(data)
	if err != nil {
		logger.Error("Failed to marshal middleware auth data: %v", err)
		return fmt.Errorf("failed to marshal middleware auth data: %w", err)
	}

	err = cs.redis.Set(ctx, key, jsonData, AuthCacheTTL)
	if err != nil {
		logger.Error("Failed to cache middleware auth data for %s: %v", threatModelID, err)
		return fmt.Errorf("failed to cache middleware auth data: %w", err)
	}

	logger.Debug("Cached middleware auth data for %s with TTL %v", threatModelID, AuthCacheTTL)
	return nil
}

// GetCachedMiddlewareAuth retrieves cached middleware auth data
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch cached middleware auth data for a threat model from Redis, returning nil on miss (reads DB)
func (cs *CacheService) GetCachedMiddlewareAuth(ctx context.Context, threatModelID string) (*MiddlewareAuthData, error) {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID) + ":mw"

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "middleware_auth")))
			}
			return nil, nil // Cache miss
		}
		return nil, fmt.Errorf("failed to get cached middleware auth data: %w", err)
	}

	var authData MiddlewareAuthData
	if err := json.Unmarshal([]byte(data), &authData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached middleware auth data: %w", err)
	}

	logger.Debug("Cache hit for middleware auth data %s", threatModelID)
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "middleware_auth")))
	}
	return &authData, nil
}

// InvalidateMiddlewareAuth invalidates middleware auth cache for a threat model
// SEM@2ec29b7908cd546e20f3bbf1ad51b2c76e52c70d: delete middleware auth cache entry for a threat model from Redis (mutates shared state)
func (cs *CacheService) InvalidateMiddlewareAuth(ctx context.Context, threatModelID string) error {
	key := cs.builder.CacheAuthKey(threatModelID) + ":mw"
	return cs.redis.Del(ctx, key)
}

// CacheThreatModelResponse caches a full threat model API response
// SEM@b226389b316426e5d229ed94aa3a29dff80e46b1: store a full threat model API response in Redis with threat-model TTL (mutates shared state)
func (cs *CacheService) CacheThreatModelResponse(ctx context.Context, id string, tm *ThreatModel) error {
	logger := slogging.Get()
	key := cs.builder.CacheThreatModelKey(id) + ":response"

	data, err := json.Marshal(tm)
	if err != nil {
		logger.Error("Failed to marshal threat model response for cache: %v", err)
		return fmt.Errorf("failed to marshal threat model response: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, ThreatModelCacheTTL)
	if err != nil {
		logger.Error("Failed to cache threat model response %s: %v", id, err)
		return fmt.Errorf("failed to cache threat model response: %w", err)
	}

	logger.Debug("Cached threat model response %s with TTL %v", id, ThreatModelCacheTTL)
	return nil
}

// GetCachedThreatModelResponse retrieves a cached threat model response
// SEM@1f8a861705b8907dc184e3db47d54cbe24222ef9: fetch a cached full threat model response from Redis, returning nil on cache miss (reads DB)
func (cs *CacheService) GetCachedThreatModelResponse(ctx context.Context, id string) (*ThreatModel, error) {
	logger := slogging.Get()
	key := cs.builder.CacheThreatModelKey(id) + ":response"

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			if m := tmiotel.GlobalMetrics; m != nil {
				m.CacheMisses.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "threat_model_response")))
			}
			return nil, nil // Cache miss
		}
		return nil, fmt.Errorf("failed to get cached threat model response: %w", err)
	}

	var tm ThreatModel
	if err := json.Unmarshal([]byte(data), &tm); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached threat model response: %w", err)
	}

	logger.Debug("Cache hit for threat model response %s", id)
	if m := tmiotel.GlobalMetrics; m != nil {
		m.CacheHits.Add(ctx, 1, metric.WithAttributes(attribute.String("entity_type", "threat_model_response")))
	}
	return &tm, nil
}

// InvalidateThreatModelResponse invalidates the response cache for a threat model
// SEM@b226389b316426e5d229ed94aa3a29dff80e46b1: delete the response cache entry for a threat model from Redis (mutates shared state)
func (cs *CacheService) InvalidateThreatModelResponse(ctx context.Context, id string) error {
	key := cs.builder.CacheThreatModelKey(id) + ":response"
	return cs.redis.Del(ctx, key)
}
