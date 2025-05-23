package db

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisKeyValidator(t *testing.T) {
	validator := NewRedisKeyValidator()

	tests := []struct {
		name    string
		key     string
		valid   bool
		pattern string
	}{
		// Valid keys
		{
			name:    "valid session key",
			key:     "session:550e8400-e29b-41d4-a716-446655440000:660e8400-e29b-41d4-a716-446655440001",
			valid:   true,
			pattern: "session:{user_id}:{session_id}",
		},
		{
			name:    "valid auth token key",
			key:     "auth:token:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			valid:   true,
			pattern: "auth:token:{token_id}",
		},
		{
			name:    "valid rate limit global key",
			key:     "rate_limit:global:192.168.1.1:api_v1_users",
			valid:   true,
			pattern: "rate_limit:global:{ip}:{endpoint}",
		},
		{
			name:    "valid cache user key",
			key:     "cache:user:550e8400-e29b-41d4-a716-446655440000",
			valid:   true,
			pattern: "cache:user:{user_id}",
		},
		{
			name:    "valid lock key",
			key:     "lock:threat_model:550e8400-e29b-41d4-a716-446655440000",
			valid:   true,
			pattern: "lock:{resource}:{id}",
		},
		// Invalid keys
		{
			name:  "invalid key - wrong namespace",
			key:   "invalid:key:pattern",
			valid: false,
		},
		{
			name:  "invalid key - spaces",
			key:   "session:user id:session id",
			valid: false,
		},
		{
			name:  "invalid key - missing parts",
			key:   "session:only-one-part",
			valid: false,
		},
		{
			name:  "invalid key - uppercase in UUID",
			key:   "session:550E8400-E29B-41D4-A716-446655440000:660e8400-e29b-41d4-a716-446655440001",
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateKey(tt.key)
			if tt.valid {
				assert.NoError(t, err, "Expected key %s to be valid", tt.key)

				pattern, err := validator.GetPatternForKey(tt.key)
				assert.NoError(t, err)
				assert.Equal(t, tt.pattern, pattern.Pattern)
			} else {
				assert.Error(t, err, "Expected key %s to be invalid", tt.key)
			}
		})
	}
}

func TestRedisKeyValidatorWithTTL(t *testing.T) {
	// Start miniredis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() {
		_ = client.Close()
	}()

	ctx := context.Background()
	validator := NewRedisKeyValidator()

	// Test key with proper TTL
	sessionKey := "session:550e8400-e29b-41d4-a716-446655440000:660e8400-e29b-41d4-a716-446655440001"
	err = client.HSet(ctx, sessionKey, "user_id", "test-user").Err()
	require.NoError(t, err)
	err = client.Expire(ctx, sessionKey, 1*time.Hour).Err()
	require.NoError(t, err)

	err = validator.ValidateKeyWithTTL(ctx, client, sessionKey)
	assert.NoError(t, err)

	// Test key without TTL
	cacheKey := "cache:user:550e8400-e29b-41d4-a716-446655440000"
	err = client.HSet(ctx, cacheKey, "name", "Test User").Err()
	require.NoError(t, err)

	err = validator.ValidateKeyWithTTL(ctx, client, cacheKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "has no TTL set")

	// Test key with excessive TTL
	authKey := "auth:state:test-state-123"
	err = client.HSet(ctx, authKey, "provider", "google").Err()
	require.NoError(t, err)
	err = client.Expire(ctx, authKey, 1*time.Hour).Err() // Max is 10 minutes
	require.NoError(t, err)

	err = validator.ValidateKeyWithTTL(ctx, client, authKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max TTL")
}

func TestRedisKeyValidatorDataType(t *testing.T) {
	// Start miniredis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() {
		_ = client.Close()
	}()

	ctx := context.Background()
	validator := NewRedisKeyValidator()

	// Test correct data type (hash)
	sessionKey := "session:550e8400-e29b-41d4-a716-446655440000:660e8400-e29b-41d4-a716-446655440001"
	err = client.HSet(ctx, sessionKey, "user_id", "test-user").Err()
	require.NoError(t, err)

	err = validator.ValidateDataType(ctx, client, sessionKey)
	assert.NoError(t, err)

	// Test incorrect data type (string instead of hash)
	cacheKey := "cache:user:550e8400-e29b-41d4-a716-446655440000"
	err = client.Set(ctx, cacheKey, "wrong-type", 0).Err()
	require.NoError(t, err)

	err = validator.ValidateDataType(ctx, client, cacheKey)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected 'hash'")
}

func TestRedisKeyBuilder(t *testing.T) {
	builder := NewRedisKeyBuilder()

	tests := []struct {
		name     string
		buildFn  func() string
		expected string
	}{
		{
			name:     "session key",
			buildFn:  func() string { return builder.SessionKey("user123", "session456") },
			expected: "session:user123:session456",
		},
		{
			name:     "auth token key",
			buildFn:  func() string { return builder.AuthTokenKey("token789") },
			expected: "auth:token:token789",
		},
		{
			name:     "rate limit global key",
			buildFn:  func() string { return builder.RateLimitGlobalKey("192.168.1.1", "/api/v1/users") },
			expected: "rate_limit:global:192.168.1.1:_api_v1_users",
		},
		{
			name:     "cache user key",
			buildFn:  func() string { return builder.CacheUserKey("user123") },
			expected: "cache:user:user123",
		},
		{
			name:     "lock key",
			buildFn:  func() string { return builder.LockKey("threat_model", "model123") },
			expected: "lock:threat_model:model123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.buildFn()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRedisKeyBuilderParsing(t *testing.T) {
	builder := NewRedisKeyBuilder()

	t.Run("parse session key", func(t *testing.T) {
		userID, sessionID, err := builder.ParseSessionKey("session:user123:session456")
		assert.NoError(t, err)
		assert.Equal(t, "user123", userID)
		assert.Equal(t, "session456", sessionID)

		_, _, err = builder.ParseSessionKey("invalid:key")
		assert.Error(t, err)
	})

	t.Run("parse rate limit key", func(t *testing.T) {
		limitType, identifier, action, err := builder.ParseRateLimitKey("rate_limit:global:192.168.1.1:login")
		assert.NoError(t, err)
		assert.Equal(t, "global", limitType)
		assert.Equal(t, "192.168.1.1", identifier)
		assert.Equal(t, "login", action)

		_, _, _, err = builder.ParseRateLimitKey("invalid:key")
		assert.Error(t, err)
	})
}

func TestPatternDocumentation(t *testing.T) {
	validator := NewRedisKeyValidator()
	docs := validator.GetPatternDocumentation()

	// Ensure we have documentation for all patterns
	assert.Greater(t, len(docs), 10, "Should have documentation for multiple patterns")

	// Check that each pattern has required fields
	for _, doc := range docs {
		assert.NotEmpty(t, doc.Name)
		assert.NotEmpty(t, doc.Pattern)
		assert.NotEmpty(t, doc.DataType)
		assert.NotEmpty(t, doc.Description)
		assert.Greater(t, doc.MaxTTL, time.Duration(0))
	}
}
