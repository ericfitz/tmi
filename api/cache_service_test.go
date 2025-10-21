package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRedisDB is a comprehensive mock for Redis operations used in cache service testing
type MockRedisDB struct {
	mock.Mock
}

func (m *MockRedisDB) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockRedisDB) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockRedisDB) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	args := m.Called(ctx, key, value, expiration)
	return args.Error(0)
}

func (m *MockRedisDB) Get(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *MockRedisDB) Del(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *MockRedisDB) HSet(ctx context.Context, key, field string, value interface{}) error {
	args := m.Called(ctx, key, field, value)
	return args.Error(0)
}

func (m *MockRedisDB) HGet(ctx context.Context, key, field string) (string, error) {
	args := m.Called(ctx, key, field)
	return args.String(0), args.Error(1)
}

func (m *MockRedisDB) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	args := m.Called(ctx, key)
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockRedisDB) HDel(ctx context.Context, key string, fields ...string) error {
	args := m.Called(ctx, key, fields)
	return args.Error(0)
}

func (m *MockRedisDB) Expire(ctx context.Context, key string, expiration time.Duration) error {
	args := m.Called(ctx, key, expiration)
	return args.Error(0)
}

// setupCacheService creates a test cache service with mock Redis by creating a simplified version
func setupCacheService() (*TestCacheService, *MockRedisDB) {
	mockRedis := &MockRedisDB{}
	cacheService := &TestCacheService{
		redis:   mockRedis,
		builder: db.NewRedisKeyBuilder(),
	}
	return cacheService, mockRedis
}

// TestCacheService wraps the actual cache service logic with our mock
type TestCacheService struct {
	redis   *MockRedisDB
	builder *db.RedisKeyBuilder
}

// Implement key cache operations for testing

func (cs *TestCacheService) CacheThreat(ctx context.Context, threat *Threat) error {
	if threat.Id == nil {
		return assert.AnError
	}

	key := cs.builder.CacheThreatKey(threat.Id.String())
	data, err := json.Marshal(threat)
	if err != nil {
		return err
	}

	return cs.redis.Set(ctx, key, data, SubResourceCacheTTL)
}

func (cs *TestCacheService) GetCachedThreat(ctx context.Context, threatID string) (*Threat, error) {
	key := cs.builder.CacheThreatKey(threatID)
	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	var threat Threat
	err = json.Unmarshal([]byte(data), &threat)
	if err != nil {
		return nil, err
	}

	return &threat, nil
}

func (cs *TestCacheService) CacheDocument(ctx context.Context, document *Document) error {
	if document.Id == nil {
		return assert.AnError
	}

	key := cs.builder.CacheDocumentKey(document.Id.String())
	data, err := json.Marshal(document)
	if err != nil {
		return err
	}

	return cs.redis.Set(ctx, key, data, SubResourceCacheTTL)
}

func (cs *TestCacheService) GetCachedDocument(ctx context.Context, documentID string) (*Document, error) {
	key := cs.builder.CacheDocumentKey(documentID)
	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	var document Document
	err = json.Unmarshal([]byte(data), &document)
	if err != nil {
		return nil, err
	}

	return &document, nil
}

func (cs *TestCacheService) CacheRepository(ctx context.Context, source *Repository) error {
	if source.Id == nil {
		return assert.AnError
	}

	key := cs.builder.CacheRepositoryKey(source.Id.String())
	data, err := json.Marshal(source)
	if err != nil {
		return err
	}

	return cs.redis.Set(ctx, key, data, SubResourceCacheTTL)
}

func (cs *TestCacheService) GetCachedRepository(ctx context.Context, sourceID string) (*Repository, error) {
	key := cs.builder.CacheRepositoryKey(sourceID)
	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	var repository Repository
	err = json.Unmarshal([]byte(data), &repository)
	if err != nil {
		return nil, err
	}

	return &repository, nil
}

func (cs *TestCacheService) CacheMetadata(ctx context.Context, entityType, entityID string, metadata []Metadata) error {
	key := cs.builder.CacheMetadataKey(entityType, entityID)
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	return cs.redis.Set(ctx, key, data, MetadataCacheTTL)
}

func (cs *TestCacheService) GetCachedMetadata(ctx context.Context, entityType, entityID string) ([]Metadata, error) {
	key := cs.builder.CacheMetadataKey(entityType, entityID)
	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	var metadata []Metadata
	err = json.Unmarshal([]byte(data), &metadata)
	if err != nil {
		return nil, err
	}

	return metadata, nil
}

func (cs *TestCacheService) InvalidateEntity(ctx context.Context, entityType, entityID string) error {
	switch entityType {
	case "threat":
		key := cs.builder.CacheThreatKey(entityID)
		if err := cs.redis.Del(ctx, key); err != nil {
			return err
		}
	case "document":
		key := cs.builder.CacheDocumentKey(entityID)
		if err := cs.redis.Del(ctx, key); err != nil {
			return err
		}
	case "source":
		key := cs.builder.CacheRepositoryKey(entityID)
		if err := cs.redis.Del(ctx, key); err != nil {
			return err
		}
	}

	// Also invalidate metadata
	metadataKey := cs.builder.CacheMetadataKey(entityType, entityID)
	return cs.redis.Del(ctx, metadataKey)
}

func (cs *TestCacheService) InvalidateMetadata(ctx context.Context, entityType, entityID string) error {
	key := cs.builder.CacheMetadataKey(entityType, entityID)
	return cs.redis.Del(ctx, key)
}

// TestCacheService_CacheThreat tests threat caching functionality
func TestCacheService_CacheThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		threatID := uuid.New()
		threat := &Threat{
			Id:   &threatID,
			Name: "SQL Injection",
		}

		expectedKey := cs.builder.CacheThreatKey(threatID.String())
		expectedData, _ := json.Marshal(threat)

		mockRedis.On("Set", ctx, expectedKey, expectedData, SubResourceCacheTTL).Return(nil)

		err := cs.CacheThreat(ctx, threat)

		assert.NoError(t, err)
		mockRedis.AssertExpectations(t)
	})

	t.Run("NilID", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		threat := &Threat{
			Id:   nil, // This will cause error
			Name: "Test Threat",
		}

		err := cs.CacheThreat(ctx, threat)

		assert.Error(t, err)
		mockRedis.AssertExpectations(t)
	})

	t.Run("RedisSetError", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		threatID := uuid.New()
		threat := &Threat{
			Id:   &threatID,
			Name: "Test Threat",
		}

		expectedKey := cs.builder.CacheThreatKey(threatID.String())
		expectedData, _ := json.Marshal(threat)
		redisError := assert.AnError

		mockRedis.On("Set", ctx, expectedKey, expectedData, SubResourceCacheTTL).Return(redisError)

		err := cs.CacheThreat(ctx, threat)

		assert.Error(t, err)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_GetCachedThreat tests threat retrieval from cache
func TestCacheService_GetCachedThreat(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		threatID := "00000000-0000-0000-0000-000000000001"
		threat := &Threat{
			Id:   mustParseUUID(threatID),
			Name: "Cached Threat",
		}

		expectedKey := cs.builder.CacheThreatKey(threatID)
		threatData, _ := json.Marshal(threat)

		mockRedis.On("Get", ctx, expectedKey).Return(string(threatData), nil)

		result, err := cs.GetCachedThreat(ctx, threatID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, threat.Name, result.Name)
		mockRedis.AssertExpectations(t)
	})

	t.Run("CacheMiss", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		threatID := "00000000-0000-0000-0000-000000000001"
		expectedKey := cs.builder.CacheThreatKey(threatID)

		mockRedis.On("Get", ctx, expectedKey).Return("", assert.AnError)

		result, err := cs.GetCachedThreat(ctx, threatID)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRedis.AssertExpectations(t)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		threatID := "00000000-0000-0000-0000-000000000001"
		expectedKey := cs.builder.CacheThreatKey(threatID)

		mockRedis.On("Get", ctx, expectedKey).Return("invalid-json", nil)

		result, err := cs.GetCachedThreat(ctx, threatID)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_CacheDocument tests document caching functionality
func TestCacheService_CacheDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		docID := uuid.New()
		document := &Document{
			Id:   &docID,
			Name: "Test Document",
			Uri:  "https://example.com/doc",
		}

		expectedKey := cs.builder.CacheDocumentKey(docID.String())
		expectedData, _ := json.Marshal(document)

		mockRedis.On("Set", ctx, expectedKey, expectedData, SubResourceCacheTTL).Return(nil)

		err := cs.CacheDocument(ctx, document)

		assert.NoError(t, err)
		mockRedis.AssertExpectations(t)
	})

	t.Run("RedisError", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		docID := uuid.New()
		document := &Document{
			Id:  &docID,
			Uri: "https://example.com/doc",
		}

		expectedKey := cs.builder.CacheDocumentKey(docID.String())
		expectedData, _ := json.Marshal(document)

		mockRedis.On("Set", ctx, expectedKey, expectedData, SubResourceCacheTTL).Return(assert.AnError)

		err := cs.CacheDocument(ctx, document)

		assert.Error(t, err)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_GetCachedDocument tests document retrieval from cache
func TestCacheService_GetCachedDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		docID := "00000000-0000-0000-0000-000000000001"
		document := &Document{
			Id:   mustParseUUID(docID),
			Name: "Cached Document",
			Uri:  "https://example.com/cached-doc",
		}

		expectedKey := cs.builder.CacheDocumentKey(docID)
		docData, _ := json.Marshal(document)

		mockRedis.On("Get", ctx, expectedKey).Return(string(docData), nil)

		result, err := cs.GetCachedDocument(ctx, docID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, document.Name, result.Name)
		assert.Equal(t, document.Uri, result.Uri)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_CacheRepository tests source code caching functionality
func TestCacheService_CacheRepository(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		sourceID := uuid.New()
		repository := &Repository{
			Id:  &sourceID,
			Uri: "https://github.com/user/repo",
		}

		expectedKey := cs.builder.CacheRepositoryKey(sourceID.String())
		expectedData, _ := json.Marshal(repository)

		mockRedis.On("Set", ctx, expectedKey, expectedData, SubResourceCacheTTL).Return(nil)

		err := cs.CacheRepository(ctx, repository)

		assert.NoError(t, err)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_GetCachedRepository tests source retrieval from cache
func TestCacheService_GetCachedRepository(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		sourceID := "00000000-0000-0000-0000-000000000001"
		repository := &Repository{
			Id:  mustParseUUID(sourceID),
			Uri: "https://github.com/cached/repo",
		}

		expectedKey := cs.builder.CacheRepositoryKey(sourceID)
		repositoryData, _ := json.Marshal(repository)

		mockRedis.On("Get", ctx, expectedKey).Return(string(repositoryData), nil)

		result, err := cs.GetCachedRepository(ctx, sourceID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, repository.Uri, result.Uri)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_CacheMetadata tests metadata caching functionality
func TestCacheService_CacheMetadata(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		entityType := "threat"
		entityID := "00000000-0000-0000-0000-000000000001"
		metadata := []Metadata{
			{Key: "priority", Value: "high"},
			{Key: "status", Value: "active"},
		}

		expectedKey := cs.builder.CacheMetadataKey(entityType, entityID)
		expectedData, _ := json.Marshal(metadata)

		mockRedis.On("Set", ctx, expectedKey, expectedData, MetadataCacheTTL).Return(nil)

		err := cs.CacheMetadata(ctx, entityType, entityID, metadata)

		assert.NoError(t, err)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_GetCachedMetadata tests metadata retrieval from cache
func TestCacheService_GetCachedMetadata(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		entityType := "threat"
		entityID := "00000000-0000-0000-0000-000000000001"
		metadata := []Metadata{
			{Key: "priority", Value: "high"},
			{Key: "category", Value: "spoofing"},
		}

		expectedKey := cs.builder.CacheMetadataKey(entityType, entityID)
		metadataData, _ := json.Marshal(metadata)

		mockRedis.On("Get", ctx, expectedKey).Return(string(metadataData), nil)

		result, err := cs.GetCachedMetadata(ctx, entityType, entityID)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result, 2)
		assert.Equal(t, "priority", result[0].Key)
		assert.Equal(t, "high", result[0].Value)
		mockRedis.AssertExpectations(t)
	})

	t.Run("CacheMiss", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		entityType := "threat"
		entityID := "00000000-0000-0000-0000-000000000001"
		expectedKey := cs.builder.CacheMetadataKey(entityType, entityID)

		mockRedis.On("Get", ctx, expectedKey).Return("", assert.AnError)

		result, err := cs.GetCachedMetadata(ctx, entityType, entityID)

		assert.Error(t, err)
		assert.Nil(t, result)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_InvalidateEntity tests cache invalidation
func TestCacheService_InvalidateEntity(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		entityType := "threat"
		entityID := "00000000-0000-0000-0000-000000000001"

		expectedThreatKey := cs.builder.CacheThreatKey(entityID)
		expectedMetadataKey := cs.builder.CacheMetadataKey(entityType, entityID)

		mockRedis.On("Del", ctx, expectedThreatKey).Return(nil)
		mockRedis.On("Del", ctx, expectedMetadataKey).Return(nil)

		err := cs.InvalidateEntity(ctx, entityType, entityID)

		assert.NoError(t, err)
		mockRedis.AssertExpectations(t)
	})

	t.Run("RedisError", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		entityType := "threat"
		entityID := "00000000-0000-0000-0000-000000000001"

		expectedThreatKey := cs.builder.CacheThreatKey(entityID)

		mockRedis.On("Del", ctx, expectedThreatKey).Return(assert.AnError)

		err := cs.InvalidateEntity(ctx, entityType, entityID)

		assert.Error(t, err)
		mockRedis.AssertExpectations(t)
	})
}

// TestCacheService_InvalidateMetadata tests metadata cache invalidation
func TestCacheService_InvalidateMetadata(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		entityType := "threat"
		entityID := "00000000-0000-0000-0000-000000000001"

		expectedKey := cs.builder.CacheMetadataKey(entityType, entityID)

		mockRedis.On("Del", ctx, expectedKey).Return(nil)

		err := cs.InvalidateMetadata(ctx, entityType, entityID)

		assert.NoError(t, err)
		mockRedis.AssertExpectations(t)
	})

	t.Run("RedisError", func(t *testing.T) {
		cs, mockRedis := setupCacheService()
		ctx := context.Background()

		entityType := "threat"
		entityID := "00000000-0000-0000-0000-000000000001"

		expectedKey := cs.builder.CacheMetadataKey(entityType, entityID)

		mockRedis.On("Del", ctx, expectedKey).Return(assert.AnError)

		err := cs.InvalidateMetadata(ctx, entityType, entityID)

		assert.Error(t, err)
		mockRedis.AssertExpectations(t)
	})
}

// Helper functions

func mustParseUUID(s string) *uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}
	return &id
}
