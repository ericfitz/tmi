package api

import (
	"fmt"
	"os"
)

// Test infrastructure configuration helpers.
// These read from environment variables with dev defaults, matching the pattern
// used by test/integration/framework/database.go.

func testGetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// testDatabaseURL returns the database connection URL for tests.
// Reads from TEST_DATABASE_URL, or constructs from individual TEST_DB_* env vars,
// falling back to the dev configuration defaults.
func testDatabaseURL() string {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		return url
	}
	host := testGetEnvOrDefault("TEST_DB_HOST", "127.0.0.1")
	port := testGetEnvOrDefault("TEST_DB_PORT", "5432")
	user := testGetEnvOrDefault("TEST_DB_USER", "tmi_dev")
	password := testGetEnvOrDefault("TEST_DB_PASSWORD", "dev123")
	dbname := testGetEnvOrDefault("TEST_DB_NAME", "tmi_dev")
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, dbname)
}

// testRedisHost returns the Redis host for tests.
func testRedisHost() string {
	return testGetEnvOrDefault("TEST_REDIS_HOST", "127.0.0.1")
}

// testRedisPort returns the Redis port for tests.
func testRedisPort() string {
	return testGetEnvOrDefault("TEST_REDIS_PORT", "6379")
}

// testRedisPassword returns the Redis password for tests.
func testRedisPassword() string {
	return testGetEnvOrDefault("TEST_REDIS_PASSWORD", "")
}
