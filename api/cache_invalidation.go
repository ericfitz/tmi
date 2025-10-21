package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
)

// CacheInvalidator handles complex cache invalidation scenarios
type CacheInvalidator struct {
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder
	cache   *CacheService
}

// NewCacheInvalidator creates a new cache invalidator
func NewCacheInvalidator(redis *db.RedisDB, cache *CacheService) *CacheInvalidator {
	return &CacheInvalidator{
		redis:   redis,
		builder: db.NewRedisKeyBuilder(),
		cache:   cache,
	}
}

// InvalidationStrategy defines different cache invalidation approaches
type InvalidationStrategy int

const (
	// InvalidateImmediately removes cache entries immediately
	InvalidateImmediately InvalidationStrategy = iota
	// InvalidateAsync removes cache entries asynchronously
	InvalidateAsync
	// InvalidateWithDelay removes cache entries after a short delay
	InvalidateWithDelay
)

// InvalidationEvent represents a cache invalidation event
type InvalidationEvent struct {
	EntityType    string
	EntityID      string
	ParentType    string
	ParentID      string
	OperationType string // create, update, delete
	Strategy      InvalidationStrategy
}

// InvalidateSubResourceChange handles cache invalidation when a sub-resource changes
func (ci *CacheInvalidator) InvalidateSubResourceChange(ctx context.Context, event InvalidationEvent) error {
	logger := slogging.Get()
	logger.Debug("Processing cache invalidation event: %s %s:%s (parent: %s:%s)",
		event.OperationType, event.EntityType, event.EntityID, event.ParentType, event.ParentID)

	switch event.Strategy {
	case InvalidateImmediately:
		return ci.invalidateImmediately(ctx, event)
	case InvalidateAsync:
		go func() {
			if err := ci.invalidateImmediately(context.Background(), event); err != nil {
				logger.Error("Async cache invalidation failed: %v", err)
			}
		}()
		return nil
	case InvalidateWithDelay:
		// For now, implement as immediate - could be enhanced with Redis delays
		return ci.invalidateImmediately(ctx, event)
	default:
		return fmt.Errorf("unknown invalidation strategy: %d", event.Strategy)
	}
}

// invalidateImmediately performs immediate cache invalidation
func (ci *CacheInvalidator) invalidateImmediately(ctx context.Context, event InvalidationEvent) error {
	logger := slogging.Get()

	// Check if cache service is available
	if ci.cache == nil {
		logger.Debug("Cache service not available, skipping cache invalidation for %s:%s", event.EntityType, event.EntityID)
		return nil
	}

	// Invalidate the specific entity
	if err := ci.cache.InvalidateEntity(ctx, event.EntityType, event.EntityID); err != nil {
		logger.Error("Failed to invalidate entity cache %s:%s: %v", event.EntityType, event.EntityID, err)
		return err
	}

	// Invalidate metadata for the entity
	if err := ci.cache.InvalidateMetadata(ctx, event.EntityType, event.EntityID); err != nil {
		logger.Error("Failed to invalidate metadata cache %s:%s: %v", event.EntityType, event.EntityID, err)
		return err
	}

	// Invalidate related caches based on the entity type and operation
	switch event.EntityType {
	case "threat":
		return ci.invalidateThreatRelatedCaches(ctx, event)
	case "document":
		return ci.invalidateDocumentRelatedCaches(ctx, event)
	case "source":
		return ci.invalidateSourceRelatedCaches(ctx, event)
	case "cell":
		return ci.invalidateCellRelatedCaches(ctx, event)
	case "metadata":
		return ci.invalidateMetadataRelatedCaches(ctx, event)
	default:
		logger.Debug("No specific invalidation rules for entity type %s", event.EntityType)
		return nil
	}
}

// invalidateThreatRelatedCaches handles threat-specific cache invalidation
func (ci *CacheInvalidator) invalidateThreatRelatedCaches(ctx context.Context, event InvalidationEvent) error {
	logger := slogging.Get()

	// Check if cache service is available
	if ci.cache == nil {
		logger.Debug("Cache service not available, skipping threat-related cache invalidation")
		return nil
	}

	// Invalidate threat model cache if parent is specified
	if event.ParentType == "threat_model" && event.ParentID != "" {
		if err := ci.cache.InvalidateEntity(ctx, "threat_model", event.ParentID); err != nil {
			logger.Error("Failed to invalidate parent threat model cache %s: %v", event.ParentID, err)
			return err
		}

		// Invalidate authorization data cache for the threat model
		if err := ci.cache.InvalidateAuthData(ctx, event.ParentID); err != nil {
			logger.Error("Failed to invalidate auth data cache for threat model %s: %v", event.ParentID, err)
			return err
		}
	}

	// Invalidate paginated lists containing threats
	return ci.invalidatePaginatedLists(ctx, "threats", event.ParentID)
}

// invalidateDocumentRelatedCaches handles document-specific cache invalidation
func (ci *CacheInvalidator) invalidateDocumentRelatedCaches(ctx context.Context, event InvalidationEvent) error {
	logger := slogging.Get()

	// Check if cache service is available
	if ci.cache == nil {
		logger.Debug("Cache service not available, skipping document-related cache invalidation")
		return nil
	}

	// Invalidate threat model cache if parent is specified
	if event.ParentType == "threat_model" && event.ParentID != "" {
		if err := ci.cache.InvalidateEntity(ctx, "threat_model", event.ParentID); err != nil {
			logger.Error("Failed to invalidate parent threat model cache %s: %v", event.ParentID, err)
			return err
		}
	}

	// Invalidate paginated lists containing documents
	return ci.invalidatePaginatedLists(ctx, "documents", event.ParentID)
}

// invalidateSourceRelatedCaches handles source code-specific cache invalidation
func (ci *CacheInvalidator) invalidateSourceRelatedCaches(ctx context.Context, event InvalidationEvent) error {
	logger := slogging.Get()

	// Check if cache service is available
	if ci.cache == nil {
		logger.Debug("Cache service not available, skipping source-related cache invalidation")
		return nil
	}

	// Invalidate threat model cache if parent is specified
	if event.ParentType == "threat_model" && event.ParentID != "" {
		if err := ci.cache.InvalidateEntity(ctx, "threat_model", event.ParentID); err != nil {
			logger.Error("Failed to invalidate parent threat model cache %s: %v", event.ParentID, err)
			return err
		}
	}

	// Invalidate paginated lists containing sources
	return ci.invalidatePaginatedLists(ctx, "sources", event.ParentID)
}

// invalidateCellRelatedCaches handles cell-specific cache invalidation
func (ci *CacheInvalidator) invalidateCellRelatedCaches(ctx context.Context, event InvalidationEvent) error {
	logger := slogging.Get()

	// Check if cache service is available
	if ci.cache == nil {
		logger.Debug("Cache service not available, skipping cell-related cache invalidation")
		return nil
	}

	// Invalidate diagram cache if parent is specified
	if event.ParentType == "diagram" && event.ParentID != "" {
		if err := ci.cache.InvalidateEntity(ctx, "diagram", event.ParentID); err != nil {
			logger.Error("Failed to invalidate parent diagram cache %s: %v", event.ParentID, err)
			return err
		}

		// Invalidate cells collection cache
		if ci.redis != nil {
			key := ci.builder.CacheCellsKey(event.ParentID)
			if err := ci.redis.Del(ctx, key); err != nil {
				logger.Error("Failed to invalidate cells cache for diagram %s: %v", event.ParentID, err)
				return err
			}
		}
	}

	return nil
}

// invalidateMetadataRelatedCaches handles metadata-specific cache invalidation
func (ci *CacheInvalidator) invalidateMetadataRelatedCaches(ctx context.Context, event InvalidationEvent) error {
	logger := slogging.Get()

	// Check if cache service is available
	if ci.cache == nil {
		logger.Debug("Cache service not available, skipping metadata-related cache invalidation")
		return nil
	}

	// For metadata changes, we need to invalidate the parent entity's metadata cache
	// The parent info should be provided in the event
	if event.ParentType != "" && event.ParentID != "" {
		if err := ci.cache.InvalidateMetadata(ctx, event.ParentType, event.ParentID); err != nil {
			logger.Error("Failed to invalidate metadata cache %s:%s: %v", event.ParentType, event.ParentID, err)
			return err
		}

		// Also invalidate the parent entity itself if it caches metadata
		if err := ci.cache.InvalidateEntity(ctx, event.ParentType, event.ParentID); err != nil {
			logger.Error("Failed to invalidate parent entity cache %s:%s: %v", event.ParentType, event.ParentID, err)
			return err
		}
	}

	return nil
}

// invalidatePaginatedLists invalidates all paginated list caches for a given entity type and parent
func (ci *CacheInvalidator) invalidatePaginatedLists(ctx context.Context, entityType, parentID string) error {
	logger := slogging.Get()

	// Check if redis is available
	if ci.redis == nil {
		logger.Debug("Redis not available, skipping paginated list cache invalidation for %s:%s", entityType, parentID)
		return nil
	}

	// Build pattern for paginated list keys
	pattern := fmt.Sprintf("cache:list:%s:%s:*", entityType, parentID)

	// Use Redis SCAN to find matching keys
	client := ci.redis.GetClient()
	iter := client.Scan(ctx, 0, pattern, 0).Iterator()

	var keysToDelete []string
	for iter.Next(ctx) {
		keysToDelete = append(keysToDelete, iter.Val())
	}

	if err := iter.Err(); err != nil {
		logger.Error("Failed to scan for paginated list keys with pattern %s: %v", pattern, err)
		return fmt.Errorf("failed to scan for keys: %w", err)
	}

	// Delete found keys
	if len(keysToDelete) > 0 {
		if err := client.Del(ctx, keysToDelete...).Err(); err != nil {
			logger.Error("Failed to delete paginated list keys: %v", err)
			return fmt.Errorf("failed to delete keys: %w", err)
		}
		logger.Debug("Invalidated %d paginated list cache entries for %s:%s", len(keysToDelete), entityType, parentID)
	}

	return nil
}

// InvalidateAllRelatedCaches performs comprehensive cache invalidation for a threat model
func (ci *CacheInvalidator) InvalidateAllRelatedCaches(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Performing comprehensive cache invalidation for threat model %s", threatModelID)

	// Check if cache service is available
	if ci.cache == nil {
		logger.Debug("Cache service not available, skipping comprehensive cache invalidation for %s", threatModelID)
		return nil
	}

	// Invalidate the threat model itself
	if err := ci.cache.InvalidateEntity(ctx, "threat_model", threatModelID); err != nil {
		return fmt.Errorf("failed to invalidate threat model: %w", err)
	}

	// Invalidate authorization data
	if err := ci.cache.InvalidateAuthData(ctx, threatModelID); err != nil {
		return fmt.Errorf("failed to invalidate auth data: %w", err)
	}

	// Invalidate all sub-resource paginated lists
	subResourceTypes := []string{"threats", "documents", "sources"}
	for _, resourceType := range subResourceTypes {
		if err := ci.invalidatePaginatedLists(ctx, resourceType, threatModelID); err != nil {
			return fmt.Errorf("failed to invalidate %s lists: %w", resourceType, err)
		}
	}

	logger.Debug("Completed comprehensive cache invalidation for threat model %s", threatModelID)
	return nil
}

// InvalidatePermissionRelatedCaches invalidates caches when permissions change
func (ci *CacheInvalidator) InvalidatePermissionRelatedCaches(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Invalidating permission-related caches for threat model %s", threatModelID)

	// Check if cache service is available
	if ci.cache == nil {
		logger.Debug("Cache service not available, skipping permission-related cache invalidation for %s", threatModelID)
		return nil
	}

	// Invalidate authorization data cache
	if err := ci.cache.InvalidateAuthData(ctx, threatModelID); err != nil {
		return fmt.Errorf("failed to invalidate auth data: %w", err)
	}

	// Invalidate the threat model cache since it contains authorization info
	if err := ci.cache.InvalidateEntity(ctx, "threat_model", threatModelID); err != nil {
		return fmt.Errorf("failed to invalidate threat model: %w", err)
	}

	logger.Debug("Completed permission-related cache invalidation for threat model %s", threatModelID)
	return nil
}

// BulkInvalidate handles bulk cache invalidation for multiple entities
func (ci *CacheInvalidator) BulkInvalidate(ctx context.Context, events []InvalidationEvent) error {
	logger := slogging.Get()
	logger.Debug("Processing bulk cache invalidation for %d events", len(events))

	// Group events by strategy for efficient processing
	immediateEvents := make([]InvalidationEvent, 0)
	asyncEvents := make([]InvalidationEvent, 0)

	for _, event := range events {
		switch event.Strategy {
		case InvalidateImmediately, InvalidateWithDelay:
			immediateEvents = append(immediateEvents, event)
		case InvalidateAsync:
			asyncEvents = append(asyncEvents, event)
		}
	}

	// Process immediate events
	for _, event := range immediateEvents {
		if err := ci.invalidateImmediately(ctx, event); err != nil {
			logger.Error("Failed to process immediate invalidation event %s:%s: %v",
				event.EntityType, event.EntityID, err)
			return err
		}
	}

	// Process async events
	if len(asyncEvents) > 0 {
		go func() {
			for _, event := range asyncEvents {
				if err := ci.invalidateImmediately(context.Background(), event); err != nil {
					logger.Error("Failed to process async invalidation event %s:%s: %v",
						event.EntityType, event.EntityID, err)
				}
			}
		}()
	}

	logger.Debug("Completed bulk cache invalidation processing")
	return nil
}

// GetInvalidationPattern returns cache key patterns that would be affected by an entity change
func (ci *CacheInvalidator) GetInvalidationPattern(entityType, entityID, parentType, parentID string) []string {
	var patterns []string

	// Direct entity cache
	switch entityType {
	case "threat":
		patterns = append(patterns, ci.builder.CacheThreatKey(entityID))
	case "document":
		patterns = append(patterns, ci.builder.CacheDocumentKey(entityID))
	case "source":
		patterns = append(patterns, ci.builder.CacheRepositoryKey(entityID))
	case "diagram":
		patterns = append(patterns, ci.builder.CacheDiagramKey(entityID))
	case "threat_model":
		patterns = append(patterns, ci.builder.CacheThreatModelKey(entityID))
	}

	// Metadata cache
	patterns = append(patterns, ci.builder.CacheMetadataKey(entityType, entityID))

	// Parent entity caches
	if parentType != "" && parentID != "" {
		switch parentType {
		case "threat_model":
			patterns = append(patterns, ci.builder.CacheThreatModelKey(parentID))
			patterns = append(patterns, ci.builder.CacheAuthKey(parentID))
		case "diagram":
			patterns = append(patterns, ci.builder.CacheDiagramKey(parentID))
			patterns = append(patterns, ci.builder.CacheCellsKey(parentID))
		}

		// Paginated list patterns
		listPattern := fmt.Sprintf("cache:list:%s:%s:*",
			strings.TrimSuffix(entityType, "s")+"s", parentID) // Ensure plural
		patterns = append(patterns, listPattern)
	}

	return patterns
}
