package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ericfitz/tmi/auth/db"
)

func main() {
	// Example Redis configuration
	config := db.RedisConfig{
		Host:     "localhost",
		Port:     "6379",
		Password: "",
		DB:       0,
	}

	// Connect to Redis
	redisDB, err := db.NewRedisDB(config)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer func() {
		if err := redisDB.Close(); err != nil {
			log.Printf("Error closing Redis connection: %v", err)
		}
	}()

	client := redisDB.GetClient()
	ctx := context.Background()

	// Create key builder and validator
	keyBuilder := db.NewRedisKeyBuilder()
	validator := db.NewRedisKeyValidator()

	fmt.Println("Redis Schema Validation Demo")
	fmt.Println("============================")

	// 1. Create some valid keys
	fmt.Println("\n1. Creating valid keys:")

	// Session key
	sessionKey := keyBuilder.SessionKey("550e8400-e29b-41d4-a716-446655440000", "660e8400-e29b-41d4-a716-446655440001")
	err = redisDB.HSet(ctx, sessionKey, "user_id", "user123")
	if err == nil {
		if err := redisDB.Expire(ctx, sessionKey, 1*time.Hour); err != nil {
			log.Printf("Warning: Failed to set TTL on session key: %v", err)
		}
		fmt.Printf("✓ Created session key: %s\n", sessionKey)
	}

	// Rate limit key
	rateLimitKey := keyBuilder.RateLimitGlobalKey("192.168.1.1", "/api/v1/users")
	err = redisDB.Set(ctx, rateLimitKey, "5:1642598400", 1*time.Minute)
	if err == nil {
		fmt.Printf("✓ Created rate limit key: %s\n", rateLimitKey)
	}

	// Cache key
	cacheKey := keyBuilder.CacheUserKey("550e8400-e29b-41d4-a716-446655440000")
	err = redisDB.HSet(ctx, cacheKey, "name", "John Doe")
	if err == nil {
		if err := redisDB.Expire(ctx, cacheKey, 5*time.Minute); err != nil {
			log.Printf("Warning: Failed to set TTL on cache key: %v", err)
		}
		fmt.Printf("✓ Created cache key: %s\n", cacheKey)
	}

	// 2. Create some invalid keys (for demonstration)
	fmt.Println("\n2. Creating invalid keys (for testing):")

	// Invalid key pattern
	invalidKey1 := "invalid:pattern:key"
	client.Set(ctx, invalidKey1, "test", 0)
	fmt.Printf("✗ Created invalid pattern key: %s\n", invalidKey1)

	// Key without TTL
	invalidKey2 := "cache:user:no-ttl-test"
	client.HSet(ctx, invalidKey2, "test", "value")
	fmt.Printf("✗ Created key without TTL: %s\n", invalidKey2)

	// 3. Validate all keys
	fmt.Println("\n3. Validating all keys:")
	results, err := validator.ScanAndValidate(ctx, client)
	if err != nil {
		log.Printf("Error during validation: %v", err)
	}

	validCount := 0
	invalidCount := 0
	for _, result := range results {
		if result.Valid {
			validCount++
			fmt.Printf("✓ %s - Valid\n", result.Key)
		} else {
			invalidCount++
			fmt.Printf("✗ %s - Invalid: %v\n", result.Key, result.Errors)
		}
	}

	fmt.Printf("\nSummary: %d valid keys, %d invalid keys\n", validCount, invalidCount)

	// 4. Perform health check
	fmt.Println("\n4. Running Redis health check:")
	healthChecker := db.NewRedisHealthChecker(client)
	healthResult := healthChecker.CheckHealth(ctx)

	fmt.Printf("Health Status: %s\n", healthResult.Message)
	fmt.Printf("Response Time: %dms\n", healthResult.PerformanceMs)

	if len(healthResult.Errors) > 0 {
		fmt.Println("Errors:")
		for _, err := range healthResult.Errors {
			fmt.Printf("  - %s\n", err)
		}
	}

	if len(healthResult.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, warn := range healthResult.Warnings {
			fmt.Printf("  - %s\n", warn)
		}
	}

	// 5. Show key pattern documentation
	fmt.Println("\n5. Available key patterns:")
	docs := validator.GetPatternDocumentation()
	for _, doc := range docs {
		fmt.Printf("- %s (TTL: %v)\n  Pattern: %s\n  Type: %s\n  Description: %s\n\n",
			doc.Name, doc.MaxTTL, doc.Pattern, doc.DataType, doc.Description)
	}

	// Clean up test keys
	fmt.Println("\nCleaning up test keys...")
	client.Del(ctx, sessionKey, rateLimitKey, cacheKey, invalidKey1, invalidKey2)
}
