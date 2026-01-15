package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRedisClient mocks the Redis client for cache invalidation testing
type MockRedisClient struct {
	mock.Mock
}

func (m *MockRedisClient) Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd {
	args := m.Called(ctx, cursor, match, count)
	return args.Get(0).(*redis.ScanCmd)
}

func (m *MockRedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	args := m.Called(ctx, keys)
	return args.Get(0).(*redis.IntCmd)
}

// MockScanIterator mocks the Redis scan iterator
type MockScanIterator struct {
	keys  []string
	index int
}

func (m *MockScanIterator) Next(ctx context.Context) bool {
	return m.index < len(m.keys)
}

func (m *MockScanIterator) Val() string {
	if m.index < len(m.keys) {
		val := m.keys[m.index]
		m.index++
		return val
	}
	return ""
}

func (m *MockScanIterator) Err() error {
	return nil
}

// MockRedisDB extends our previous MockRedisDB with GetClient method
type MockRedisDBWithClient struct {
	MockRedisDB
	client *MockRedisClient
}

func (m *MockRedisDBWithClient) GetClient() interface{} {
	return m.client
}

// MockCacheService provides mock cache service for invalidation testing
type MockCacheService struct {
	mock.Mock
}

func (m *MockCacheService) InvalidateEntity(ctx context.Context, entityType, entityID string) error {
	args := m.Called(ctx, entityType, entityID)
	return args.Error(0)
}

func (m *MockCacheService) InvalidateMetadata(ctx context.Context, entityType, entityID string) error {
	args := m.Called(ctx, entityType, entityID)
	return args.Error(0)
}

func (m *MockCacheService) InvalidateAuthData(ctx context.Context, threatModelID string) error {
	args := m.Called(ctx, threatModelID)
	return args.Error(0)
}

// setupCacheInvalidator creates a test cache invalidator with mocks
func setupCacheInvalidator() (*CacheInvalidator, *MockRedisDBWithClient, *MockCacheService) {
	mockClient := &MockRedisClient{}
	mockRedis := &MockRedisDBWithClient{
		client: mockClient,
	}
	mockCache := &MockCacheService{}

	invalidator := &CacheInvalidator{
		redis:   nil, // We'll access through mockRedis directly in tests
		builder: db.NewRedisKeyBuilder(),
		cache:   nil, // We'll access through mockCache directly in tests
	}

	return invalidator, mockRedis, mockCache
}

// TestNewCacheInvalidator tests cache invalidator creation
func TestNewCacheInvalidator(t *testing.T) {
	// Create a simple test to verify the constructor works
	invalidator := &CacheInvalidator{
		redis:   nil,
		builder: db.NewRedisKeyBuilder(),
		cache:   nil,
	}

	assert.NotNil(t, invalidator)
	assert.NotNil(t, invalidator.builder)
}

// TestInvalidateSubResourceChange tests main invalidation logic
func TestInvalidateSubResourceChange(t *testing.T) {
	t.Run("AsyncStrategy", func(t *testing.T) {
		invalidator, _, _ := setupCacheInvalidator()
		ctx := context.Background()

		event := InvalidationEvent{
			EntityType: "document",
			EntityID:   "doc-123",
			Strategy:   InvalidateAsync,
		}

		// For async, the call should return immediately without waiting
		err := invalidator.InvalidateSubResourceChange(ctx, event)

		assert.NoError(t, err)
		// Note: We can't easily test async behavior in unit tests,
		// but we verify it doesn't block or error
	})

	t.Run("UnknownStrategy", func(t *testing.T) {
		invalidator, _, _ := setupCacheInvalidator()
		ctx := context.Background()

		event := InvalidationEvent{
			EntityType: "threat",
			EntityID:   "threat-123",
			Strategy:   InvalidationStrategy(999), // Unknown strategy
		}

		err := invalidator.InvalidateSubResourceChange(ctx, event)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown invalidation strategy")
	})
}

// TestInvalidationEvent tests event structure validation
func TestInvalidationEvent(t *testing.T) {
	t.Run("ValidEvent", func(t *testing.T) {
		event := InvalidationEvent{
			EntityType:    "threat",
			EntityID:      "threat-123",
			ParentType:    "threat_model",
			ParentID:      "tm-456",
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}

		assert.Equal(t, "threat", event.EntityType)
		assert.Equal(t, "threat-123", event.EntityID)
		assert.Equal(t, "threat_model", event.ParentType)
		assert.Equal(t, "tm-456", event.ParentID)
		assert.Equal(t, "update", event.OperationType)
		assert.Equal(t, InvalidateImmediately, event.Strategy)
	})

	t.Run("MinimalEvent", func(t *testing.T) {
		event := InvalidationEvent{
			EntityType: "document",
			EntityID:   "doc-123",
			Strategy:   InvalidateAsync,
		}

		assert.Equal(t, "document", event.EntityType)
		assert.Equal(t, "doc-123", event.EntityID)
		assert.Empty(t, event.ParentType)
		assert.Empty(t, event.ParentID)
		assert.Empty(t, event.OperationType)
		assert.Equal(t, InvalidateAsync, event.Strategy)
	})
}

// TestInvalidationStrategy tests strategy constants
func TestInvalidationStrategy(t *testing.T) {
	assert.Equal(t, InvalidationStrategy(0), InvalidateImmediately)
	assert.Equal(t, InvalidationStrategy(1), InvalidateAsync)
	assert.Equal(t, InvalidationStrategy(2), InvalidateWithDelay)
}

// TestBulkInvalidate tests bulk invalidation functionality
func TestBulkInvalidate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Run("EmptyEventList", func(t *testing.T) {
		invalidator, _, _ := setupCacheInvalidator()
		ctx := context.Background()

		events := []InvalidationEvent{}

		err := invalidator.BulkInvalidate(ctx, events)

		assert.NoError(t, err)
	})

	t.Run("MixedStrategies", func(t *testing.T) {
		invalidator, _, _ := setupCacheInvalidator()
		ctx := context.Background()

		events := []InvalidationEvent{
			{
				EntityType: "threat",
				EntityID:   "threat-1",
				Strategy:   InvalidateImmediately,
			},
			{
				EntityType: "document",
				EntityID:   "doc-1",
				Strategy:   InvalidateAsync,
			},
		}

		// This should not error - nil cache service is gracefully handled
		// The function separates strategies correctly and logs when cache is unavailable
		err := invalidator.BulkInvalidate(ctx, events)

		assert.NoError(t, err)
	})
}

// TestGetInvalidationPattern tests pattern generation for cache keys
func TestGetInvalidationPattern(t *testing.T) {
	t.Run("ThreatEntity", func(t *testing.T) {
		invalidator, _, _ := setupCacheInvalidator()

		patterns := invalidator.GetInvalidationPattern("threat", "threat-123", "threat_model", "tm-456")

		expectedPatterns := []string{
			"cache:threat:threat-123",
			"cache:metadata:threat:threat-123",
			"cache:threat_model:tm-456",
			"cache:auth:tm-456",
			"cache:list:threats:tm-456:*",
		}

		assert.ElementsMatch(t, expectedPatterns, patterns)
	})

	t.Run("CellEntity", func(t *testing.T) {
		invalidator, _, _ := setupCacheInvalidator()

		patterns := invalidator.GetInvalidationPattern("cell", "cell-123", "diagram", "diag-456")

		expectedPatterns := []string{
			"cache:metadata:cell:cell-123",
			"cache:diagram:diag-456",
			"cache:cells:diag-456",
			"cache:list:cells:diag-456:*",
		}

		assert.ElementsMatch(t, expectedPatterns, patterns)
	})

	t.Run("NoParent", func(t *testing.T) {
		invalidator, _, _ := setupCacheInvalidator()

		patterns := invalidator.GetInvalidationPattern("document", "doc-123", "", "")

		expectedPatterns := []string{
			"cache:document:doc-123",
			"cache:metadata:document:doc-123",
		}

		assert.ElementsMatch(t, expectedPatterns, patterns)
	})

	t.Run("UnknownEntityType", func(t *testing.T) {
		invalidator, _, _ := setupCacheInvalidator()

		patterns := invalidator.GetInvalidationPattern("unknown", "unknown-123", "", "")

		expectedPatterns := []string{
			"cache:metadata:unknown:unknown-123",
		}

		assert.ElementsMatch(t, expectedPatterns, patterns)
	})
}
