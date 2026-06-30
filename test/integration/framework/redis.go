package framework

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// ClearRateLimits clears all rate limit keys from Redis.
// Tests run against the isolated test server's Redis logical DB (TEST_REDIS_DB,
// default 0). dev uses DB 0; the test path sets TEST_REDIS_DB=1 so dev and test
// never share a keyspace (#477).
// Errors are intentionally ignored — if Redis is unavailable, tests may hit rate limits.
func ClearRateLimits() error {
	ctx := context.Background()

	redisPort := getEnvOrDefault("TEST_REDIS_PORT", "6379")
	client := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", getEnvOrDefault("TEST_REDIS_HOST", "localhost"), redisPort),
		DB:   TestRedisDB(),
	})
	clearRateLimitKeys(ctx, client)
	client.Close()

	return nil
}

// TestRedisDB returns the Redis logical DB index for integration tests from
// TEST_REDIS_DB, defaulting to 0 (the dev keyspace) for backward compatibility.
// The test runner sets TEST_REDIS_DB=1 so dev and test never share a keyspace (#477).
func TestRedisDB() int {
	if v := os.Getenv("TEST_REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func clearRateLimitKeys(ctx context.Context, client *redis.Client) {
	patterns := []string{
		"auth:ratelimit:*",    // OAuth auth flow rate limits
		"ip:ratelimit:*",      // IP-based rate limits
		"webhook:ratelimit:*", // Webhook subscription CRUD rate limits
		"addon:ratelimit:*",   // Addon invocation rate limits
		"addon:dedup:*",       // Addon deduplication keys
		"api:ratelimit:*",     // API rate limits
	}
	for _, pattern := range patterns {
		iter := client.Scan(ctx, 0, pattern, 100).Iterator()
		for iter.Next(ctx) {
			client.Del(ctx, iter.Val())
		}
	}
}
