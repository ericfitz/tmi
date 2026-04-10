package framework

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// ClearRateLimits clears all rate limit keys from Redis.
// Tests run against the dev server (Redis on port 6379 DB 0).
// Errors are intentionally ignored — if Redis is unavailable, tests may hit rate limits.
func ClearRateLimits() error {
	ctx := context.Background()

	redisPort := getEnvOrDefault("TEST_REDIS_PORT", "6379")
	client := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", getEnvOrDefault("TEST_REDIS_HOST", "localhost"), redisPort),
		DB:   0,
	})
	clearRateLimitKeys(ctx, client)
	client.Close()

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
