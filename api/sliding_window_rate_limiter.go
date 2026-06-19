package api

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// SlidingWindowRateLimiter provides shared sliding window rate limiting using Redis sorted sets.
// Domain-specific rate limiters embed this struct and add their own key-building and quota logic.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: shared Redis-backed sliding window rate limiter embeddable by domain limiters (mutates shared state)
type SlidingWindowRateLimiter struct {
	RedisClient *redis.Client
}

// CheckSlidingWindow implements sliding window rate limiting using Redis sorted sets.
// Returns allowed (bool), retryAfter (seconds), and error.
// SEM@914adca66ed5ce0bcfa6a1233361a298648ccf00: enforce a sliding window rate limit and return allowed status with retry-after seconds (mutates shared state)
func (sw *SlidingWindowRateLimiter) CheckSlidingWindow(ctx context.Context, key string, limit int, windowSeconds int) (bool, int, error) {
	now := time.Now().Unix()
	windowStart := now - int64(windowSeconds)

	pipe := sw.RedisClient.Pipeline()

	// Remove old entries outside the window
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))

	// Count entries in current window
	countCmd := pipe.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), "+inf")

	// Get oldest entry for retry-after calculation
	oldestCmd := pipe.ZRangeWithScores(ctx, key, 0, 0)

	// Set expiration to window + 60 seconds
	pipe.Expire(ctx, key, time.Duration(windowSeconds+60)*time.Second)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, 0, err
	}

	count := countCmd.Val()

	// Check if we're at or over the limit
	if count >= int64(limit) {
		// Calculate retry-after based on oldest entry
		retryAfter := windowSeconds
		if oldest := oldestCmd.Val(); len(oldest) > 0 {
			oldestTime := int64(oldest[0].Score)
			retryAfter = int(oldestTime + int64(windowSeconds) - now)
			if retryAfter < 0 {
				retryAfter = 1
			}
		}
		return false, retryAfter, nil
	}

	// Add current request only if under limit
	_, err = sw.RedisClient.ZAdd(ctx, key, redis.Z{
		Score:  float64(now),
		Member: fmt.Sprintf("%d:%d", now, time.Now().UnixNano()),
	}).Result()
	if err != nil {
		return false, 0, err
	}

	return true, 0, nil
}

// CheckSlidingWindowSimple is a simplified variant that only returns allowed/not-allowed without retryAfter.
// Used by webhook rate limiting where retry-after info is not needed in the check itself.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: check a sliding window rate limit returning only allowed/denied without retry-after (mutates shared state)
func (sw *SlidingWindowRateLimiter) CheckSlidingWindowSimple(ctx context.Context, key string, limit int, windowSeconds int) (bool, error) {
	allowed, _, err := sw.CheckSlidingWindow(ctx, key, limit, windowSeconds)
	return allowed, err
}

// GetRateLimitInfo returns current rate limit status for a key.
// Returns remaining count, resetAt timestamp, and error.
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: fetch remaining quota and window reset timestamp for a rate limit key (reads DB)
func (sw *SlidingWindowRateLimiter) GetRateLimitInfo(ctx context.Context, key string, limit int, windowSeconds int) (int, int64, error) {
	now := time.Now().Unix()
	windowStart := now - int64(windowSeconds)

	count, err := sw.RedisClient.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), "+inf").Result()
	if err != nil {
		return limit, now + int64(windowSeconds), err
	}

	remaining := max(limit-int(count), 0)

	// Calculate reset time
	oldestScore, err := sw.RedisClient.ZRangeWithScores(ctx, key, 0, 0).Result()
	resetAt := now + int64(windowSeconds)
	if err == nil && len(oldestScore) > 0 {
		resetAt = int64(oldestScore[0].Score) + int64(windowSeconds)
	}

	return remaining, resetAt, nil
}

// RecordInWindow records a timestamp in a sliding window without checking limits.
// SEM@914adca66ed5ce0bcfa6a1233361a298648ccf00: record a timestamped event in a sliding window without enforcing limits (mutates shared state)
func (sw *SlidingWindowRateLimiter) RecordInWindow(ctx context.Context, key string, timestamp int64, ttlSeconds int) error {
	pipe := sw.RedisClient.Pipeline()

	pipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(timestamp),
		Member: fmt.Sprintf("%d:%d", timestamp, time.Now().UnixNano()),
	})

	pipe.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second)

	_, err := pipe.Exec(ctx)
	return err
}
