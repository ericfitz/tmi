package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/redis/go-redis/v9"
)

// RedisHealthChecker performs health checks on Redis
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: Redis health checker holding a client, key validator, and logger (pure)
type RedisHealthChecker struct {
	client    *redis.Client
	validator *RedisKeyValidator
	logger    *slogging.Logger
}

// NewRedisHealthChecker creates a new Redis health checker
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: build a RedisHealthChecker with a default key validator (pure)
func NewRedisHealthChecker(client *redis.Client) *RedisHealthChecker {
	return &RedisHealthChecker{
		client:    client,
		validator: NewRedisKeyValidator(),
		logger:    slogging.Get(),
	}
}

// HealthCheckResult contains the results of a health check
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: aggregated Redis health check outcome with errors, warnings, details, and latency (pure)
type HealthCheckResult struct {
	Healthy       bool
	Message       string
	Details       map[string]any
	Errors        []string
	Warnings      []string
	PerformanceMs int64
}

// CheckHealth performs a comprehensive health check on Redis
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: run a full Redis health check covering connectivity, memory, key patterns, TTLs, and performance (reads DB)
func (h *RedisHealthChecker) CheckHealth(ctx context.Context) HealthCheckResult {
	start := time.Now()
	result := HealthCheckResult{
		Healthy:  true,
		Details:  make(map[string]any),
		Errors:   []string{},
		Warnings: []string{},
	}

	// 1. Check connectivity
	if err := h.checkConnectivity(ctx, &result); err != nil {
		result.Healthy = false
		result.Errors = append(result.Errors, fmt.Sprintf("Connectivity check failed: %v", err))
		result.PerformanceMs = time.Since(start).Milliseconds()
		return result
	}

	// 2. Check memory usage
	h.checkMemoryUsage(ctx, &result)

	// 3. Check key patterns
	h.checkKeyPatterns(ctx, &result)

	// 4. Check TTLs
	h.checkTTLs(ctx, &result)

	// 5. Check performance
	h.checkPerformance(ctx, &result)

	// Set overall health status
	switch {
	case len(result.Errors) > 0:
		result.Healthy = false
		result.Message = fmt.Sprintf("Redis health check failed with %d errors", len(result.Errors))
	case len(result.Warnings) > 0:
		result.Message = fmt.Sprintf("Redis is healthy with %d warnings", len(result.Warnings))
	default:
		result.Message = "Redis is healthy"
	}

	result.PerformanceMs = time.Since(start).Milliseconds()
	return result
}

// checkConnectivity verifies Redis is reachable
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: ping Redis and record latency; warn if ping exceeds 100ms (reads DB)
func (h *RedisHealthChecker) checkConnectivity(ctx context.Context, result *HealthCheckResult) error {
	pingStart := time.Now()
	err := h.client.Ping(ctx).Err()
	pingDuration := time.Since(pingStart)

	result.Details["ping_duration_ms"] = pingDuration.Milliseconds()

	if err != nil {
		return err
	}

	if pingDuration > 100*time.Millisecond {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("High ping latency: %v", pingDuration))
	}

	return nil
}

// checkMemoryUsage checks Redis memory usage
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: fetch Redis memory info and add error or warning when usage exceeds thresholds (reads DB)
func (h *RedisHealthChecker) checkMemoryUsage(ctx context.Context, result *HealthCheckResult) {
	info, err := h.client.Info(ctx, "memory").Result()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get memory info: %v", err))
		return
	}

	// Parse memory info (simplified - in production you'd parse more thoroughly)
	var usedMemory int64
	var maxMemory int64
	_, _ = fmt.Sscanf(info, "used_memory:%d", &usedMemory)
	_, _ = fmt.Sscanf(info, "maxmemory:%d", &maxMemory)

	result.Details["used_memory_bytes"] = usedMemory
	result.Details["max_memory_bytes"] = maxMemory

	if maxMemory > 0 {
		usagePercent := float64(usedMemory) / float64(maxMemory) * 100
		result.Details["memory_usage_percent"] = usagePercent

		if usagePercent > 90 {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Critical memory usage: %.1f%%", usagePercent))
		} else if usagePercent > 75 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("High memory usage: %.1f%%", usagePercent))
		}
	}
}

// checkKeyPatterns validates all keys match expected patterns
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: scan all Redis keys and warn on any that violate the registered key-pattern schema (reads DB)
func (h *RedisHealthChecker) checkKeyPatterns(ctx context.Context, result *HealthCheckResult) {
	// Sample keys to check patterns
	var cursor uint64
	var invalidKeys []string
	var totalKeys int

	for {
		keys, nextCursor, err := h.client.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to scan keys: %v", err))
			return
		}

		totalKeys += len(keys)

		for _, key := range keys {
			if err := h.validator.ValidateKey(key); err != nil {
				invalidKeys = append(invalidKeys, key)
				if len(invalidKeys) <= 10 { // Only report first 10
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("Invalid key pattern: %s", key))
				}
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	result.Details["total_keys"] = totalKeys
	result.Details["invalid_keys_count"] = len(invalidKeys)

	if len(invalidKeys) > 10 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Found %d invalid key patterns (showing first 10)", len(invalidKeys)))
	}
}

// checkTTLs ensures all temporary keys have TTLs
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: scan Redis keys and report those missing a required TTL or exceeding the maximum TTL (reads DB)
func (h *RedisHealthChecker) checkTTLs(ctx context.Context, result *HealthCheckResult) {
	var cursor uint64
	var keysWithoutTTL []string
	var keysWithExcessiveTTL []string

	for {
		keys, nextCursor, err := h.client.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to scan keys for TTL check: %v", err))
			return
		}

		for _, key := range keys {
			ttl, err := h.client.TTL(ctx, key).Result()
			if err != nil {
				continue
			}

			pattern, err := h.validator.GetPatternForKey(key)
			if err != nil {
				continue // Already reported in pattern check
			}

			// Check for missing TTL
			if ttl == -1 && pattern.MaxTTL > 0 {
				keysWithoutTTL = append(keysWithoutTTL, key)
				if len(keysWithoutTTL) <= 5 {
					result.Errors = append(result.Errors,
						fmt.Sprintf("Key without required TTL: %s", key))
				}
			}

			// Check for excessive TTL
			if ttl > 0 && ttl > pattern.MaxTTL {
				keysWithExcessiveTTL = append(keysWithExcessiveTTL, key)
				if len(keysWithExcessiveTTL) <= 5 {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("Key with excessive TTL: %s (TTL: %v, Max: %v)",
							key, ttl, pattern.MaxTTL))
				}
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	result.Details["keys_without_ttl"] = len(keysWithoutTTL)
	result.Details["keys_with_excessive_ttl"] = len(keysWithExcessiveTTL)

	if len(keysWithoutTTL) > 5 {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Found %d keys without required TTL (showing first 5)", len(keysWithoutTTL)))
	}

	if len(keysWithExcessiveTTL) > 5 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Found %d keys with excessive TTL (showing first 5)", len(keysWithExcessiveTTL)))
	}
}

// checkPerformance runs basic performance checks
// SEM@cdbe48c974fb76e1161972733b30bb0d1c02c3b1: measure Redis write and read latency with a probe key and warn when thresholds are exceeded (reads DB)
func (h *RedisHealthChecker) checkPerformance(ctx context.Context, result *HealthCheckResult) {
	// Test write performance
	writeStart := time.Now()
	testKey := "health:check:write:test"
	err := h.client.Set(ctx, testKey, "test", 1*time.Second).Err()
	writeDuration := time.Since(writeStart)

	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Write test failed: %v", err))
	} else {
		result.Details["write_latency_ms"] = writeDuration.Milliseconds()
		if writeDuration > 50*time.Millisecond {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("High write latency: %v", writeDuration))
		}
	}

	// Test read performance
	readStart := time.Now()
	_, err = h.client.Get(ctx, testKey).Result()
	readDuration := time.Since(readStart)

	if err != nil && !errors.Is(err, redis.Nil) {
		result.Errors = append(result.Errors, fmt.Sprintf("Read test failed: %v", err))
	} else {
		result.Details["read_latency_ms"] = readDuration.Milliseconds()
		if readDuration > 20*time.Millisecond {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("High read latency: %v", readDuration))
		}
	}

	// Clean up test key
	h.client.Del(ctx, testKey)
}

// GetKeyStatistics returns statistics about keys in Redis
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: aggregate Redis key counts by two-segment prefix and return the total DB size (reads DB)
func (h *RedisHealthChecker) GetKeyStatistics(ctx context.Context) (map[string]int, error) {
	stats := make(map[string]int)
	var cursor uint64

	// Count keys by pattern
	patternCounts := make(map[string]int)

	for {
		keys, nextCursor, err := h.client.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan keys: %w", err)
		}

		for _, key := range keys {
			// Extract pattern prefix
			prefix := getKeyPrefix(key)
			patternCounts[prefix]++
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	// Convert to stats map
	for prefix, count := range patternCounts {
		stats[fmt.Sprintf("keys:%s", prefix)] = count
	}

	// Get total key count
	dbSize, err := h.client.DBSize(ctx).Result()
	if err == nil {
		stats["total_keys"] = int(dbSize)
	}

	return stats, nil
}

// getKeyPrefix extracts the pattern prefix from a key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: extract the first two colon-delimited segments of a Redis key as its pattern prefix (pure)
func getKeyPrefix(key string) string {
	// Extract first two parts of the key as the pattern prefix
	parts := strings.Split(key, ":")
	if len(parts) >= 2 {
		return parts[0] + ":" + parts[1]
	}
	return parts[0]
}

// LogHealthCheck logs the health check results
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: log a health check result at the appropriate level with all errors, warnings, and details (mutates shared state)
func (h *RedisHealthChecker) LogHealthCheck(result HealthCheckResult) {
	if result.Healthy {
		h.logger.Info("Redis health check passed: %s (duration: %dms)",
			result.Message, result.PerformanceMs)
	} else {
		h.logger.Error("Redis health check failed: %s (duration: %dms)",
			result.Message, result.PerformanceMs)
	}

	// Log details
	for key, value := range result.Details {
		h.logger.Debug("Redis health detail - %s: %v", key, value)
	}

	// Log errors
	for _, err := range result.Errors {
		h.logger.Error("Redis health error: %s", err)
	}

	// Log warnings
	for _, warn := range result.Warnings {
		h.logger.Warn("Redis health warning: %s", warn)
	}
}
