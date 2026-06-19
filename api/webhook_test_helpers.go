package api

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// setupTestRedis creates a miniredis instance and returns a Redis client for testing
// SEM@60d232adaccc3526d92dfabc62a0aeb78bbe07ae: start a miniredis instance and return a Redis client for use in tests (pure)
func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return client, mr
}
