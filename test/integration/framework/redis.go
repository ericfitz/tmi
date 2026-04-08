package framework

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// ClearRateLimits clears all rate limit keys from both dev and test Redis.
// The workflow tests run against the dev server (which uses dev Redis on port 6379 DB 0),
// so we must clear rate limit keys from the dev Redis, not the test Redis.
// Errors are intentionally ignored — if Redis is unavailable, tests may hit rate limits.
func ClearRateLimits() error {
	ctx := context.Background()

	// Clear from dev Redis (port 6379, DB 0) — this is what the dev server uses
	devClient := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", getEnvOrDefault("TEST_REDIS_HOST", "localhost"), "6379"),
		DB:   0,
	})
	clearRateLimitKeys(ctx, devClient)
	devClient.Close()

	// Also clear from test Redis (port 6380, DB 1) — used by in-process API tests
	testPort := getEnvOrDefault("TEST_REDIS_PORT", "6380")
	testClient := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", getEnvOrDefault("TEST_REDIS_HOST", "localhost"), testPort),
		DB:   1,
	})
	clearRateLimitKeys(ctx, testClient)
	testClient.Close()

	return nil
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
