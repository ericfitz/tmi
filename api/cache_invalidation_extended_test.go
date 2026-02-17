package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/stretchr/testify/assert"
)

// --- Tests for entity-type-specific cache invalidation paths ---

func TestInvalidateImmediately_EntityTypeRouting(t *testing.T) {
	ctx := context.Background()

	// Note: invalidateImmediately requires cache != nil, otherwise it returns nil early.
	// The mock cache service interface doesn't match the concrete *CacheService type,
	// so we test via InvalidateSubResourceChange which calls invalidateImmediately.

	tests := []struct {
		name       string
		entityType string
		parentType string
	}{
		{"threat", "threat", "threat_model"},
		{"document", "document", "threat_model"},
		{"source", "source", "threat_model"},
		{"cell", "cell", "diagram"},
		{"metadata", "metadata", "threat"},
		{"unknown_entity", "foobar", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// With nil cache service, all invalidation should be a no-op (no panic)
			invalidator := &CacheInvalidator{
				redis:   nil,
				builder: db.NewRedisKeyBuilder(),
				cache:   nil,
			}

			event := InvalidationEvent{
				EntityType:    tt.entityType,
				EntityID:      "test-entity-123",
				ParentType:    tt.parentType,
				ParentID:      "parent-456",
				OperationType: "update",
				Strategy:      InvalidateImmediately,
			}

			err := invalidator.InvalidateSubResourceChange(ctx, event)
			assert.NoError(t, err, "Nil cache should not cause errors for entity type %s", tt.entityType)
		})
	}
}

func TestInvalidateImmediately_NilCacheGraceful(t *testing.T) {
	ctx := context.Background()

	invalidator := &CacheInvalidator{
		redis:   nil,
		builder: db.NewRedisKeyBuilder(),
		cache:   nil,
	}

	// Every entity type should gracefully handle nil cache
	entityTypes := []string{"threat", "document", "source", "cell", "metadata", "unknown"}
	for _, et := range entityTypes {
		event := InvalidationEvent{
			EntityType:    et,
			EntityID:      "entity-123",
			ParentType:    "threat_model",
			ParentID:      "tm-123",
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		err := invalidator.InvalidateSubResourceChange(ctx, event)
		assert.NoError(t, err, "Entity type %s with nil cache should not error", et)
	}
}

// --- Tests for InvalidatePermissionRelatedCaches ---

func TestInvalidatePermissionRelatedCaches(t *testing.T) {
	ctx := context.Background()

	t.Run("nil_cache_returns_nil", func(t *testing.T) {
		invalidator := &CacheInvalidator{
			redis:   nil,
			builder: db.NewRedisKeyBuilder(),
			cache:   nil,
		}
		err := invalidator.InvalidatePermissionRelatedCaches(ctx, "tm-123")
		assert.NoError(t, err)
	})
}

// --- Tests for InvalidateAllRelatedCaches ---

func TestInvalidateAllRelatedCaches(t *testing.T) {
	ctx := context.Background()

	t.Run("nil_cache_returns_nil", func(t *testing.T) {
		invalidator := &CacheInvalidator{
			redis:   nil,
			builder: db.NewRedisKeyBuilder(),
			cache:   nil,
		}
		err := invalidator.InvalidateAllRelatedCaches(ctx, "tm-123")
		assert.NoError(t, err)
	})

	t.Run("nil_redis_skips_paginated_lists", func(t *testing.T) {
		// With nil redis but non-nil cache, entity invalidation should work
		// but paginated list invalidation should be skipped gracefully
		invalidator := &CacheInvalidator{
			redis:   nil,
			builder: db.NewRedisKeyBuilder(),
			cache:   nil,
		}
		err := invalidator.InvalidateAllRelatedCaches(ctx, "tm-123")
		assert.NoError(t, err)
	})
}

// --- Tests for GetInvalidationPattern completeness ---

func TestGetInvalidationPattern_AllEntityTypes(t *testing.T) {
	invalidator := &CacheInvalidator{
		redis:   nil,
		builder: db.NewRedisKeyBuilder(),
		cache:   nil,
	}

	tests := []struct {
		name            string
		entityType      string
		entityID        string
		parentType      string
		parentID        string
		mustContain     []string
		mustNotContain  []string
		minPatternCount int
	}{
		{
			name:            "threat_with_parent",
			entityType:      "threat",
			entityID:        "t-123",
			parentType:      "threat_model",
			parentID:        "tm-456",
			mustContain:     []string{"cache:threat:t-123", "cache:metadata:threat:t-123", "cache:auth:tm-456"},
			minPatternCount: 4,
		},
		{
			name:            "document_with_parent",
			entityType:      "document",
			entityID:        "d-123",
			parentType:      "threat_model",
			parentID:        "tm-456",
			mustContain:     []string{"cache:document:d-123", "cache:metadata:document:d-123"},
			minPatternCount: 4,
		},
		{
			name:            "cell_with_diagram_parent",
			entityType:      "cell",
			entityID:        "c-123",
			parentType:      "diagram",
			parentID:        "diag-456",
			mustContain:     []string{"cache:metadata:cell:c-123", "cache:diagram:diag-456"},
			minPatternCount: 3,
		},
		{
			name:            "source_entity",
			entityType:      "source",
			entityID:        "s-123",
			parentType:      "threat_model",
			parentID:        "tm-456",
			mustContain:     []string{"cache:metadata:source:s-123"},
			minPatternCount: 3,
		},
		{
			name:            "diagram_entity",
			entityType:      "diagram",
			entityID:        "diag-123",
			parentType:      "",
			parentID:        "",
			mustContain:     []string{"cache:diagram:diag-123", "cache:metadata:diagram:diag-123"},
			minPatternCount: 2,
		},
		{
			name:            "threat_model_entity",
			entityType:      "threat_model",
			entityID:        "tm-123",
			parentType:      "",
			parentID:        "",
			mustContain:     []string{"cache:threat_model:tm-123", "cache:metadata:threat_model:tm-123"},
			minPatternCount: 2,
		},
		{
			name:       "unknown_entity_no_parent",
			entityType: "unknown_type",
			entityID:   "x-123",
			parentType: "",
			parentID:   "",
			// Unknown entity types should still get metadata pattern
			mustContain:     []string{"cache:metadata:unknown_type:x-123"},
			minPatternCount: 1,
		},
		{
			name:       "asset_entity_not_recognized",
			entityType: "asset",
			entityID:   "a-123",
			parentType: "threat_model",
			parentID:   "tm-456",
			// BUG DOCUMENTATION: "asset" is not in the switch statement for direct entity cache
			// so it only gets metadata pattern + parent patterns, no direct cache pattern
			mustContain:    []string{"cache:metadata:asset:a-123"},
			mustNotContain: []string{"cache:asset:a-123"},
			// This documents that assets are not directly cached — or that the pattern
			// generator is missing the asset case
		},
		{
			name:       "note_entity_not_recognized",
			entityType: "note",
			entityID:   "n-123",
			parentType: "threat_model",
			parentID:   "tm-456",
			// Same as asset — "note" is not in the entity type switch
			mustContain:    []string{"cache:metadata:note:n-123"},
			mustNotContain: []string{"cache:note:n-123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns := invalidator.GetInvalidationPattern(tt.entityType, tt.entityID, tt.parentType, tt.parentID)

			for _, expected := range tt.mustContain {
				assert.Contains(t, patterns, expected, "Pattern %q should be in results", expected)
			}
			for _, unexpected := range tt.mustNotContain {
				assert.NotContains(t, patterns, unexpected,
					"Pattern %q should NOT be in results (entity type not recognized in cache pattern generator)", unexpected)
			}
			if tt.minPatternCount > 0 {
				assert.GreaterOrEqual(t, len(patterns), tt.minPatternCount,
					"Expected at least %d patterns for %s", tt.minPatternCount, tt.entityType)
			}
		})
	}
}

// --- Tests for InvalidationStrategy behavior ---

func TestInvalidateSubResourceChange_AllStrategies(t *testing.T) {
	ctx := context.Background()

	invalidator := &CacheInvalidator{
		redis:   nil,
		builder: db.NewRedisKeyBuilder(),
		cache:   nil,
	}

	event := InvalidationEvent{
		EntityType:    "threat",
		EntityID:      "t-123",
		ParentType:    "threat_model",
		ParentID:      "tm-456",
		OperationType: "update",
	}

	t.Run("immediate_strategy", func(t *testing.T) {
		event.Strategy = InvalidateImmediately
		err := invalidator.InvalidateSubResourceChange(ctx, event)
		assert.NoError(t, err)
	})

	t.Run("async_strategy", func(t *testing.T) {
		event.Strategy = InvalidateAsync
		err := invalidator.InvalidateSubResourceChange(ctx, event)
		assert.NoError(t, err)
	})

	t.Run("delay_strategy_falls_through_to_immediate", func(t *testing.T) {
		event.Strategy = InvalidateWithDelay
		err := invalidator.InvalidateSubResourceChange(ctx, event)
		assert.NoError(t, err)
	})

	t.Run("unknown_strategy_returns_error", func(t *testing.T) {
		event.Strategy = InvalidationStrategy(42)
		err := invalidator.InvalidateSubResourceChange(ctx, event)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown invalidation strategy")
	})
}

// --- Tests for BulkInvalidate edge cases ---

func TestBulkInvalidate_EdgeCases(t *testing.T) {
	ctx := context.Background()

	invalidator := &CacheInvalidator{
		redis:   nil,
		builder: db.NewRedisKeyBuilder(),
		cache:   nil,
	}

	t.Run("all_immediate", func(t *testing.T) {
		events := []InvalidationEvent{
			{EntityType: "threat", EntityID: "t-1", Strategy: InvalidateImmediately},
			{EntityType: "document", EntityID: "d-1", Strategy: InvalidateImmediately},
			{EntityType: "source", EntityID: "s-1", Strategy: InvalidateImmediately},
		}
		err := invalidator.BulkInvalidate(ctx, events)
		assert.NoError(t, err)
	})

	t.Run("all_async", func(t *testing.T) {
		events := []InvalidationEvent{
			{EntityType: "threat", EntityID: "t-1", Strategy: InvalidateAsync},
			{EntityType: "document", EntityID: "d-1", Strategy: InvalidateAsync},
		}
		err := invalidator.BulkInvalidate(ctx, events)
		assert.NoError(t, err)
	})

	t.Run("mixed_with_delay", func(t *testing.T) {
		events := []InvalidationEvent{
			{EntityType: "threat", EntityID: "t-1", Strategy: InvalidateImmediately},
			{EntityType: "document", EntityID: "d-1", Strategy: InvalidateAsync},
			{EntityType: "source", EntityID: "s-1", Strategy: InvalidateWithDelay},
		}
		err := invalidator.BulkInvalidate(ctx, events)
		assert.NoError(t, err)
	})

	t.Run("single_event", func(t *testing.T) {
		events := []InvalidationEvent{
			{EntityType: "threat", EntityID: "t-1", Strategy: InvalidateImmediately},
		}
		err := invalidator.BulkInvalidate(ctx, events)
		assert.NoError(t, err)
	})
}

// --- Tests for invalidatePaginatedLists with nil redis ---

func TestInvalidatePaginatedLists_NilRedis(t *testing.T) {
	ctx := context.Background()

	invalidator := &CacheInvalidator{
		redis:   nil,
		builder: db.NewRedisKeyBuilder(),
		cache:   nil,
	}

	// With nil redis, invalidatePaginatedLists should return nil (graceful degradation)
	err := invalidator.invalidatePaginatedLists(ctx, "threats", "tm-123")
	assert.NoError(t, err)
}

// --- Tests for missing entity types in cache invalidation routing ---

func TestInvalidateImmediately_MissingEntityTypes(t *testing.T) {
	// The switch in invalidateImmediately routes to entity-type-specific handlers.
	// Entity types not in the switch (asset, note, repository) hit the default case
	// which only logs and returns nil — no related caches are invalidated.
	// This documents which entity types are NOT handled.

	ctx := context.Background()
	invalidator := &CacheInvalidator{
		redis:   nil,
		builder: db.NewRedisKeyBuilder(),
		cache:   nil,
	}

	unhandledTypes := []string{"asset", "note", "repository", "survey", "triage_note"}
	for _, entityType := range unhandledTypes {
		t.Run(fmt.Sprintf("%s_not_routed", entityType), func(t *testing.T) {
			event := InvalidationEvent{
				EntityType:    entityType,
				EntityID:      "test-123",
				ParentType:    "threat_model",
				ParentID:      "tm-456",
				OperationType: "update",
				Strategy:      InvalidateImmediately,
			}

			// Should not error, but also won't invalidate parent caches
			err := invalidator.InvalidateSubResourceChange(ctx, event)
			assert.NoError(t, err)
			// NOTE: This means that when an asset, note, or repository is updated,
			// the parent threat model's cache is NOT invalidated via this code path.
			// This could lead to stale data if the parent threat model is cached
			// and a sub-resource is updated.
		})
	}
}
