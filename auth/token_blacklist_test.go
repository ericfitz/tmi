package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
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

	// Create test key manager
	testKeyManager, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret",
	})
	require.NoError(t, err)

	// Create token blacklist
	tb := NewTokenBlacklist(rdb, testKeyManager)
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

		// Try to blacklist the expired token (should fail with validation error)
		err = tb.BlacklistToken(ctx, tokenString)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token is expired")
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

	// Create test key manager
	testKeyManager, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret",
	})
	require.NoError(t, err)

	// Create token blacklist
	tb := NewTokenBlacklist(rdb, testKeyManager)

	assert.NotNil(t, tb)
	assert.Equal(t, rdb, tb.redis)
}

// TestIsRetryableRedisError pins which Redis failures the blacklist lookup
// retries. Only connectivity failures qualify: an application-level error will
// answer identically on the next attempt, so retrying it just adds latency to a
// request that is going to fail anyway. See issue #660.
func TestIsRetryableRedisError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"redis.Nil is a miss, not a failure", redis.Nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{"application-level error", errors.New("WRONGTYPE Operation against a key"), false},
		{"i/o timeout (the observed failure)", errors.New("dial tcp 10.1.2.3:6379: i/o timeout"), true},
		{"connection refused", errors.New("dial tcp 10.1.2.3:6379: connect: connection refused"), true},
		{"connection reset", errors.New("read tcp: connection reset by peer"), true},
		{"broken pipe", errors.New("write tcp: broken pipe"), true},
		{"EOF", errors.New("EOF"), true},
		{"pool timeout", errors.New("redis: connection pool timeout"), true},
		{"wrapped net.Error", fmt.Errorf("checking key: %w", &net.OpError{
			Op: "dial", Err: errors.New("no route to host"),
		}), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isRetryableRedisError(tt.err))
		})
	}
}

// TestIsTokenBlacklistedRetriesTransientFailure verifies the lookup does not
// give up on the first connectivity error. With Redis down the call must still
// fail, but only after exhausting its retries -- measured via the mandatory
// backoff between attempts.
func TestIsTokenBlacklistedRetriesTransientFailure(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	addr := mr.Addr()
	mr.Close() // Redis is now unreachable at this address.

	rdb := redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1, // disable go-redis' own retries so we measure ours
	})
	defer func() { _ = rdb.Close() }()

	testKeyManager, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret",
	})
	require.NoError(t, err)
	tb := NewTokenBlacklist(rdb, testKeyManager)

	// Minimum time spent sleeping between attempts: 20ms + 40ms.
	var minBackoff time.Duration
	for attempt := 0; attempt < blacklistCheckRetries; attempt++ {
		minBackoff += blacklistCheckBackoff << attempt
	}

	start := time.Now()
	isBlacklisted, err := tb.IsTokenBlacklisted(context.Background(), "some-token")
	elapsed := time.Since(start)

	require.Error(t, err, "an unreachable Redis must still surface as an error")
	assert.False(t, isBlacklisted, "must not report a token as clean when the check failed")
	assert.GreaterOrEqual(t, elapsed, minBackoff,
		"expected %d retries totalling at least %v of backoff, took %v",
		blacklistCheckRetries, minBackoff, elapsed)
}

// TestIsTokenBlacklistedHonoursCancelledContext verifies a cancelled request
// aborts the retry loop instead of sleeping out the full backoff.
func TestIsTokenBlacklistedHonoursCancelledContext(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	addr := mr.Addr()
	mr.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:        addr,
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	defer func() { _ = rdb.Close() }()

	testKeyManager, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret",
	})
	require.NoError(t, err)
	tb := NewTokenBlacklist(rdb, testKeyManager)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tb.IsTokenBlacklisted(ctx, "some-token")
	require.Error(t, err)
}
