package framework

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/ericfitz/tmi/internal/tlsconfig"
	"github.com/redis/go-redis/v9"
)

// ClearRateLimits clears all rate limit keys from Redis.
// Tests run against the isolated test server's Redis logical DB (TEST_REDIS_DB,
// default 0). dev uses DB 0; the test path sets TEST_REDIS_DB=1 so dev and test
// never share a keyspace (#477).
// Errors are intentionally ignored — if Redis is unavailable, tests may hit rate limits.
// SEM@249dea6: delete rate-limit keys from the harness Redis; return error if client options are invalid (writes Redis)
func ClearRateLimits() error {
	ctx := context.Background()

	opts, err := RedisOptions()
	if err != nil {
		return err
	}
	client := redis.NewClient(opts)
	clearRateLimitKeys(ctx, client)
	client.Close()

	return nil
}

// RedisOptions returns client options for the harness Redis: TEST_REDIS_HOST/
// PORT/DB, TEST_REDIS_PASSWORD, and CA-pinned TLS when TEST_REDIS_TLS_CA_FILE
// is set (scripts/run-integration-tests.py sets all of them). Without the CA
// variable it is a plaintext client, for a developer pointing the tests at an
// ad-hoc Redis.
// SEM@249dea6: build go-redis options for the harness Redis from TEST_REDIS_* env (reads env)
func RedisOptions() (*redis.Options, error) {
	host := getEnvOrDefault("TEST_REDIS_HOST", "localhost")
	opts := &redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, getEnvOrDefault("TEST_REDIS_PORT", "6379")),
		Password: os.Getenv("TEST_REDIS_PASSWORD"),
		DB:       TestRedisDB(),
	}
	if ca := os.Getenv("TEST_REDIS_TLS_CA_FILE"); ca != "" {
		tlsCfg, err := tlsconfig.Load(ca, "", "")
		if err != nil {
			return nil, fmt.Errorf("harness redis tls: %w", err)
		}
		tlsCfg.ServerName = host
		opts.TLSConfig = tlsCfg
	}
	return opts, nil
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
