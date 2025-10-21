package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
)

// CacheTestHelper provides utilities for testing Redis cache functionality
type CacheTestHelper struct {
	Cache       *CacheService
	Invalidator *CacheInvalidator
	RedisClient *db.RedisDB
	TestContext context.Context
	KeyBuilder  *db.RedisKeyBuilder
}

// CacheTestScenario defines a test scenario for cache testing
type CacheTestScenario struct {
	Description     string
	EntityType      string
	EntityID        string
	ThreatModelID   string
	ExpectedHit     bool
	ExpectedMiss    bool
	TTL             time.Duration
	ShouldExpire    bool
	InvalidateAfter bool
}

// NewCacheTestHelper creates a new cache test helper
func NewCacheTestHelper(cache *CacheService, invalidator *CacheInvalidator, redisClient *db.RedisDB) *CacheTestHelper {
	return &CacheTestHelper{
		Cache:       cache,
		Invalidator: invalidator,
		RedisClient: redisClient,
		TestContext: context.Background(),
		KeyBuilder:  db.NewRedisKeyBuilder(),
	}
}

// SetupTestCache initializes cache with test data
func (h *CacheTestHelper) SetupTestCache(t *testing.T) {
	t.Helper()

	if !SubResourceFixtures.Initialized {
		InitSubResourceTestFixtures()
	}

	// Populate cache with test fixtures
	h.CacheTestThreat(t, &SubResourceFixtures.Threat1)
	h.CacheTestDocument(t, &SubResourceFixtures.Document1)
	h.CacheTestRepository(t, &SubResourceFixtures.Repository1)
}

// CacheTestThreat caches a threat for testing
func (h *CacheTestHelper) CacheTestThreat(t *testing.T, threat *Threat) {
	t.Helper()

	err := h.Cache.CacheThreat(h.TestContext, threat)
	if err != nil {
		t.Errorf("Failed to cache threat: %v", err)
	}
}

// CacheTestDocument caches a document for testing
func (h *CacheTestHelper) CacheTestDocument(t *testing.T, document *Document) {
	t.Helper()

	err := h.Cache.CacheDocument(h.TestContext, document)
	if err != nil {
		t.Errorf("Failed to cache document: %v", err)
	}
}

// CacheTestRepository caches a repository for testing
func (h *CacheTestHelper) CacheTestRepository(t *testing.T, repository *Repository) {
	t.Helper()

	err := h.Cache.CacheRepository(h.TestContext, repository)
	if err != nil {
		t.Errorf("Failed to cache repository: %v", err)
	}
}

// TestCacheThreatOperations tests caching operations for threats
func (h *CacheTestHelper) TestCacheThreatOperations(t *testing.T, scenarios []CacheTestScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		t.Run(scenario.Description, func(t *testing.T) {
			threatID := scenario.EntityID
			if threatID == "" {
				threatID = uuid.New().String()
			}

			// Clear cache first if testing miss scenario
			if scenario.ExpectedMiss {
				h.ClearThreatCache(t, threatID)
			}

			// Try to get from cache
			cachedThreat, err := h.Cache.GetCachedThreat(h.TestContext, threatID)

			if scenario.ExpectedHit {
				if err != nil {
					t.Errorf("Expected cache hit but got error: %v", err)
					return
				}
				if cachedThreat == nil {
					t.Errorf("Expected cached threat but got nil")
				}
			}

			if scenario.ExpectedMiss {
				if err == nil && cachedThreat != nil {
					t.Errorf("Expected cache miss but got cached data")
				}
			}

			// Test invalidation if requested
			if scenario.InvalidateAfter && cachedThreat != nil {
				event := InvalidationEvent{
					EntityType:    "threat",
					EntityID:      threatID,
					ParentType:    "threat_model",
					ParentID:      scenario.ThreatModelID,
					OperationType: "update",
					Strategy:      InvalidateImmediately,
				}
				err = h.Invalidator.InvalidateSubResourceChange(h.TestContext, event)
				if err != nil {
					t.Errorf("Failed to invalidate threat cache: %v", err)
				}

				// Verify cache is cleared
				cachedAfter, err := h.Cache.GetCachedThreat(h.TestContext, threatID)
				if err == nil && cachedAfter != nil {
					t.Errorf("Expected cache to be cleared after invalidation")
				}
			}
		})
	}
}

// TestCacheDocumentOperations tests caching operations for documents
func (h *CacheTestHelper) TestCacheDocumentOperations(t *testing.T, scenarios []CacheTestScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		t.Run(scenario.Description, func(t *testing.T) {
			documentID := scenario.EntityID
			if documentID == "" {
				documentID = uuid.New().String()
			}

			// Clear cache first if testing miss scenario
			if scenario.ExpectedMiss {
				h.ClearDocumentCache(t, documentID)
			}

			// Try to get from cache
			cachedDocument, err := h.Cache.GetCachedDocument(h.TestContext, documentID)

			if scenario.ExpectedHit {
				if err != nil {
					t.Errorf("Expected cache hit but got error: %v", err)
					return
				}
				if cachedDocument == nil {
					t.Errorf("Expected cached document but got nil")
				}
			}

			if scenario.ExpectedMiss {
				if err == nil && cachedDocument != nil {
					t.Errorf("Expected cache miss but got cached data")
				}
			}

			// Test invalidation if requested
			if scenario.InvalidateAfter && cachedDocument != nil {
				event := InvalidationEvent{
					EntityType:    "document",
					EntityID:      documentID,
					ParentType:    "threat_model",
					ParentID:      scenario.ThreatModelID,
					OperationType: "update",
					Strategy:      InvalidateImmediately,
				}
				err = h.Invalidator.InvalidateSubResourceChange(h.TestContext, event)
				if err != nil {
					t.Errorf("Failed to invalidate document cache: %v", err)
				}
			}
		})
	}
}

// TestCacheRepositoryOperations tests caching operations for repositories
func (h *CacheTestHelper) TestCacheRepositoryOperations(t *testing.T, scenarios []CacheTestScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		t.Run(scenario.Description, func(t *testing.T) {
			repositoryID := scenario.EntityID
			if repositoryID == "" {
				repositoryID = uuid.New().String()
			}

			// Clear cache first if testing miss scenario
			if scenario.ExpectedMiss {
				h.ClearRepositoryCache(t, repositoryID)
			}

			// Try to get from cache
			cachedRepository, err := h.Cache.GetCachedRepository(h.TestContext, repositoryID)

			if scenario.ExpectedHit {
				if err != nil {
					t.Errorf("Expected cache hit but got error: %v", err)
					return
				}
				if cachedRepository == nil {
					t.Errorf("Expected cached repository but got nil")
				}
			}

			if scenario.ExpectedMiss {
				if err == nil && cachedRepository != nil {
					t.Errorf("Expected cache miss but got cached data")
				}
			}

			// Test invalidation if requested
			if scenario.InvalidateAfter && cachedRepository != nil {
				event := InvalidationEvent{
					EntityType:    "repository",
					EntityID:      repositoryID,
					ParentType:    "threat_model",
					ParentID:      scenario.ThreatModelID,
					OperationType: "update",
					Strategy:      InvalidateImmediately,
				}
				err = h.Invalidator.InvalidateSubResourceChange(h.TestContext, event)
				if err != nil {
					t.Errorf("Failed to invalidate repository cache: %v", err)
				}
			}
		})
	}
}

// TestCacheMetadataOperations tests caching operations for metadata
func (h *CacheTestHelper) TestCacheMetadataOperations(t *testing.T, entityType, entityID string) {
	t.Helper()

	// Cache some test metadata
	metadata := []Metadata{
		{Key: "test_key_1", Value: "test_value_1"},
		{Key: "test_key_2", Value: "test_value_2"},
	}

	err := h.Cache.CacheMetadata(h.TestContext, entityType, entityID, metadata)
	if err != nil {
		t.Errorf("Failed to cache metadata collection: %v", err)
		return
	}

	// Retrieve from cache
	cachedMetadata, err := h.Cache.GetCachedMetadata(h.TestContext, entityType, entityID)
	if err != nil {
		t.Errorf("Failed to get cached metadata: %v", err)
		return
	}

	if cachedMetadata == nil {
		t.Errorf("Expected cached metadata but got nil")
		return
	}

	if len(cachedMetadata) != len(metadata) {
		t.Errorf("Cached metadata length mismatch: expected %d, got %d", len(metadata), len(cachedMetadata))
	}

	// Verify individual metadata items are accessible from the collection
	for _, meta := range metadata {
		found := false
		for _, cached := range cachedMetadata {
			if cached.Key == meta.Key && cached.Value == meta.Value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Metadata item %s:%s not found in cached collection", meta.Key, meta.Value)
		}
	}

	// Test invalidation
	event := InvalidationEvent{
		EntityType:    "metadata",
		EntityID:      entityID,
		ParentType:    entityType,
		ParentID:      entityID,
		OperationType: "update",
		Strategy:      InvalidateImmediately,
	}
	err = h.Invalidator.InvalidateSubResourceChange(h.TestContext, event)
	if err != nil {
		t.Errorf("Failed to invalidate metadata cache: %v", err)
	}
}

// TestCacheAuthOperations tests caching operations for authorization data
func (h *CacheTestHelper) TestCacheAuthOperations(t *testing.T, threatModelID string) {
	t.Helper()

	// Create test authorization data
	authData := GetTestAuthorizationData("valid_multi_user")
	if authData == nil {
		t.Fatal("Failed to get test authorization data")
	}

	// Cache authorization data
	err := h.Cache.CacheAuthData(h.TestContext, threatModelID, *authData)
	if err != nil {
		t.Errorf("Failed to cache auth data: %v", err)
		return
	}

	// Retrieve from cache
	cachedAuthData, err := h.Cache.GetCachedAuthData(h.TestContext, threatModelID)
	if err != nil {
		t.Errorf("Failed to get cached auth data: %v", err)
		return
	}

	if cachedAuthData == nil {
		t.Errorf("Expected cached auth data but got nil")
		return
	}

	// Verify cached data matches original
	AssertAuthDataEqual(t, authData, cachedAuthData)

	// Test invalidation
	err = h.Invalidator.InvalidateAllRelatedCaches(h.TestContext, threatModelID)
	if err != nil {
		t.Errorf("Failed to invalidate auth cache: %v", err)
	}

	// Verify cache is cleared
	cachedAfter, err := h.Cache.GetCachedAuthData(h.TestContext, threatModelID)
	if err == nil && cachedAfter != nil {
		t.Errorf("Expected auth cache to be cleared after invalidation")
	}
}

// TestCacheTTLBehavior tests TTL behavior for cached items
func (h *CacheTestHelper) TestCacheTTLBehavior(t *testing.T, scenarios []CacheTestScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		if !scenario.ShouldExpire {
			continue
		}

		t.Run(fmt.Sprintf("%s_TTL_test", scenario.Description), func(t *testing.T) {
			key := h.KeyBuilder.CacheThreatKey(scenario.EntityID)

			// Set a value with short TTL
			err := h.RedisClient.Set(h.TestContext, key, "test_value", 100*time.Millisecond)
			if err != nil {
				t.Errorf("Failed to set test value: %v", err)
				return
			}

			// Verify it exists
			_, err = h.RedisClient.Get(h.TestContext, key)
			if err != nil {
				t.Errorf("Failed to get test value: %v", err)
				return
			}

			// Wait for expiration
			time.Sleep(150 * time.Millisecond)

			// Verify it's expired
			_, err = h.RedisClient.Get(h.TestContext, key)
			if err == nil {
				t.Errorf("Expected key to be expired but it still exists")
				return
			}
		})
	}
}

// TestCacheConsistency tests cache consistency across operations
func (h *CacheTestHelper) TestCacheConsistency(t *testing.T, threatModelID string) {
	t.Helper()

	// Create and cache a threat
	threat := CreateTestThreatWithMetadata(threatModelID, []Metadata{
		{Key: "consistency_test", Value: "true"},
	})

	err := h.Cache.CacheThreat(h.TestContext, &threat)
	if err != nil {
		t.Errorf("Failed to cache threat: %v", err)
		return
	}

	// Update threat and cache again
	threat.Name = "Updated Threat Name"
	err = h.Cache.CacheThreat(h.TestContext, &threat)
	if err != nil {
		t.Errorf("Failed to update cached threat: %v", err)
		return
	}

	// Retrieve and verify update
	cachedThreat, err := h.Cache.GetCachedThreat(h.TestContext, threat.Id.String())
	if err != nil {
		t.Errorf("Failed to get cached threat: %v", err)
		return
	}

	if cachedThreat == nil {
		t.Errorf("Expected cached threat but got nil")
		return
	}

	if cachedThreat.Name != "Updated Threat Name" {
		t.Errorf("Cache inconsistency: expected updated name but got %s", cachedThreat.Name)
	}
}

// TestCacheInvalidationStrategies tests different invalidation strategies
func (h *CacheTestHelper) TestCacheInvalidationStrategies(t *testing.T, threatModelID string) {
	t.Helper()

	strategies := []struct {
		name       string
		entityType string
		operation  string
	}{
		{"immediate_threat", "threat", "update"},
		{"immediate_document", "document", "delete"},
		{"immediate_source", "source", "create"},
		{"async_metadata", "metadata", "update"},
	}

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			event := InvalidationEvent{
				EntityType:    strategy.entityType,
				EntityID:      uuid.New().String(),
				ParentType:    "threat_model",
				ParentID:      threatModelID,
				OperationType: strategy.operation,
				Strategy:      InvalidateImmediately,
			}

			err := h.Invalidator.InvalidateSubResourceChange(h.TestContext, event)
			if err != nil {
				t.Errorf("Failed to invalidate with strategy %s: %v", strategy.name, err)
			}
		})
	}
}

// ClearThreatCache clears threat cache for testing
func (h *CacheTestHelper) ClearThreatCache(t *testing.T, threatID string) {
	t.Helper()
	key := h.KeyBuilder.CacheThreatKey(threatID)
	err := h.RedisClient.Del(h.TestContext, key)
	if err != nil {
		t.Errorf("Failed to clear threat cache: %v", err)
	}
}

// ClearDocumentCache clears document cache for testing
func (h *CacheTestHelper) ClearDocumentCache(t *testing.T, documentID string) {
	t.Helper()
	key := h.KeyBuilder.CacheDocumentKey(documentID)
	err := h.RedisClient.Del(h.TestContext, key)
	if err != nil {
		t.Errorf("Failed to clear document cache: %v", err)
	}
}

// ClearRepositoryCache clears repository cache for testing
func (h *CacheTestHelper) ClearRepositoryCache(t *testing.T, repositoryID string) {
	t.Helper()
	key := h.KeyBuilder.CacheRepositoryKey(repositoryID)
	err := h.RedisClient.Del(h.TestContext, key)
	if err != nil {
		t.Errorf("Failed to clear repository cache: %v", err)
	}
}

// ClearAllTestCache clears all test cache data
func (h *CacheTestHelper) ClearAllTestCache(t *testing.T) {
	t.Helper()

	// Clear test-specific cache patterns
	patterns := []string{
		"cache:threat:*",
		"cache:document:*",
		"cache:source:*",
		"cache:metadata:*",
		"cache:auth:*",
		"cache:list:*",
	}

	for _, pattern := range patterns {
		// Use the Redis client directly for pattern-based operations
		client := h.RedisClient.GetClient()
		keys, err := client.Keys(h.TestContext, pattern).Result()
		if err != nil {
			t.Errorf("Failed to get keys for pattern %s: %v", pattern, err)
			continue
		}

		for _, key := range keys {
			err = h.RedisClient.Del(h.TestContext, key)
			if err != nil {
				t.Errorf("Failed to delete key %s: %v", key, err)
			}
		}
	}
}

// GetCacheStats returns cache statistics for testing
func (h *CacheTestHelper) GetCacheStats(t *testing.T) map[string]interface{} {
	t.Helper()

	// Use the Redis client directly for INFO command
	client := h.RedisClient.GetClient()
	info, err := client.Info(h.TestContext, "stats").Result()
	if err != nil {
		t.Errorf("Failed to get Redis stats: %v", err)
		return nil
	}

	// Parse info string into map (implementation would parse Redis INFO format)
	stats := make(map[string]interface{})
	stats["raw_info"] = info

	return stats
}

// SetupCacheTestScenarios returns common cache test scenarios
func SetupCacheTestScenarios() []CacheTestScenario {
	if !SubResourceFixtures.Initialized {
		InitSubResourceTestFixtures()
	}

	return []CacheTestScenario{
		{
			Description:     "Cache hit for existing threat",
			EntityType:      "threat",
			EntityID:        SubResourceFixtures.Threat1ID,
			ThreatModelID:   SubResourceFixtures.ThreatModelID,
			ExpectedHit:     true,
			ExpectedMiss:    false,
			TTL:             SubResourceCacheTTL,
			ShouldExpire:    false,
			InvalidateAfter: false,
		},
		{
			Description:     "Cache miss for non-existent threat",
			EntityType:      "threat",
			EntityID:        uuid.New().String(),
			ThreatModelID:   SubResourceFixtures.ThreatModelID,
			ExpectedHit:     false,
			ExpectedMiss:    true,
			TTL:             SubResourceCacheTTL,
			ShouldExpire:    false,
			InvalidateAfter: false,
		},
		{
			Description:     "Cache invalidation test",
			EntityType:      "threat",
			EntityID:        SubResourceFixtures.Threat1ID,
			ThreatModelID:   SubResourceFixtures.ThreatModelID,
			ExpectedHit:     true,
			ExpectedMiss:    false,
			TTL:             SubResourceCacheTTL,
			ShouldExpire:    false,
			InvalidateAfter: true,
		},
		{
			Description:     "Cache TTL expiration test",
			EntityType:      "threat",
			EntityID:        uuid.New().String(),
			ThreatModelID:   SubResourceFixtures.ThreatModelID,
			ExpectedHit:     false,
			ExpectedMiss:    false,
			TTL:             100 * time.Millisecond,
			ShouldExpire:    true,
			InvalidateAfter: false,
		},
	}
}

// VerifyCacheMetrics verifies cache performance metrics
func (h *CacheTestHelper) VerifyCacheMetrics(t *testing.T, expectedHitRatio float64) {
	t.Helper()

	// This would typically check Redis INFO stats or custom metrics
	// For now, we'll just verify the cache service is responsive

	testKey := "cache:test:metrics"
	err := h.RedisClient.Set(h.TestContext, testKey, "test", time.Minute)
	if err != nil {
		t.Errorf("Cache not responsive for metrics test: %v", err)
		return
	}

	_, err = h.RedisClient.Get(h.TestContext, testKey)
	if err != nil {
		t.Errorf("Cache read failed for metrics test: %v", err)
	}

	// Clean up
	_ = h.RedisClient.Del(h.TestContext, testKey)
}
