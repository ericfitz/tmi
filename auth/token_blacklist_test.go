package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenBlacklist(t *testing.T) {
	// Start miniredis for testing
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = rdb.Close() }()

	// Create token blacklist
	tb := NewTokenBlacklist(rdb)
	ctx := context.Background()

	t.Run("BlacklistValidToken", func(t *testing.T) {
		// Create a valid JWT token
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user123",
			"exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(),
		})
		tokenString, err := token.SignedString([]byte("test-secret"))
		require.NoError(t, err)

		// Blacklist the token
		err = tb.BlacklistToken(ctx, tokenString)
		assert.NoError(t, err)

		// Check if token is blacklisted
		isBlacklisted, err := tb.IsTokenBlacklisted(ctx, tokenString)
		assert.NoError(t, err)
		assert.True(t, isBlacklisted)
	})

	t.Run("NonBlacklistedToken", func(t *testing.T) {
		// Create a valid JWT token
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user456",
			"exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(),
		})
		tokenString, err := token.SignedString([]byte("test-secret"))
		require.NoError(t, err)

		// Check if token is blacklisted (should not be)
		isBlacklisted, err := tb.IsTokenBlacklisted(ctx, tokenString)
		assert.NoError(t, err)
		assert.False(t, isBlacklisted)
	})

	t.Run("ExpiredToken", func(t *testing.T) {
		// Create an expired JWT token
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user789",
			"exp": time.Now().Add(-time.Hour).Unix(), // Expired 1 hour ago
			"iat": time.Now().Add(-2 * time.Hour).Unix(),
		})
		tokenString, err := token.SignedString([]byte("test-secret"))
		require.NoError(t, err)

		// Try to blacklist the expired token (should succeed but not store)
		err = tb.BlacklistToken(ctx, tokenString)
		assert.NoError(t, err)

		// Check if token is blacklisted (should not be since it's expired)
		isBlacklisted, err := tb.IsTokenBlacklisted(ctx, tokenString)
		assert.NoError(t, err)
		assert.False(t, isBlacklisted)
	})

	t.Run("InvalidToken", func(t *testing.T) {
		invalidJWT := "invalid.jwt.token"

		// Try to blacklist invalid token
		err = tb.BlacklistToken(ctx, invalidJWT)
		assert.Error(t, err)
	})

	t.Run("TokenTTL", func(t *testing.T) {
		// Create a token that expires in 2 seconds
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user_ttl",
			"exp": time.Now().Add(2 * time.Second).Unix(),
			"iat": time.Now().Unix(),
		})
		tokenString, err := token.SignedString([]byte("test-secret"))
		require.NoError(t, err)

		// Blacklist the token
		err = tb.BlacklistToken(ctx, tokenString)
		assert.NoError(t, err)

		// Check if token is blacklisted
		isBlacklisted, err := tb.IsTokenBlacklisted(ctx, tokenString)
		assert.NoError(t, err)
		assert.True(t, isBlacklisted)

		// Fast forward time in miniredis
		mr.FastForward(3 * time.Second)

		// Check if token is still blacklisted (should not be due to TTL)
		isBlacklisted, err = tb.IsTokenBlacklisted(ctx, tokenString)
		assert.NoError(t, err)
		assert.False(t, isBlacklisted)
	})

	t.Run("HashingConsistency", func(t *testing.T) {
		// Test that the same token produces the same hash
		testJWT := "test.jwt.token"

		hash1 := tb.hashToken(testJWT)
		hash2 := tb.hashToken(testJWT)

		assert.Equal(t, hash1, hash2)
		assert.NotEmpty(t, hash1)
	})
}

func TestNewTokenBlacklist(t *testing.T) {
	// Start miniredis for testing
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = rdb.Close() }()

	// Create token blacklist
	tb := NewTokenBlacklist(rdb)

	assert.NotNil(t, tb)
	assert.Equal(t, rdb, tb.redis)
}
