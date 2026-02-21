package api

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// SlidingWindowRateLimiter provides shared sliding window rate limiting using Redis sorted sets.
// Domain-specific rate limiters embed this struct and add their own key-building and quota logic.
type SlidingWindowRateLimiter struct {
	RedisClient *redis.Client
}

// CheckSlidingWindow implements sliding window rate limiting using Redis sorted sets.
// Returns allowed (bool), retryAfter (seconds), and error.
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
	_, err = sw.RedisClient.ZAdd(ctx, key, &redis.Z{
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
func (sw *SlidingWindowRateLimiter) CheckSlidingWindowSimple(ctx context.Context, key string, limit int, windowSeconds int) (bool, error) {
	allowed, _, err := sw.CheckSlidingWindow(ctx, key, limit, windowSeconds)
	return allowed, err
}

// GetRateLimitInfo returns current rate limit status for a key.
// Returns remaining count, resetAt timestamp, and error.
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
func (sw *SlidingWindowRateLimiter) RecordInWindow(ctx context.Context, key string, timestamp int64, ttlSeconds int) error {
	pipe := sw.RedisClient.Pipeline()

	pipe.ZAdd(ctx, key, &redis.Z{
		Score:  float64(timestamp),
		Member: fmt.Sprintf("%d:%d", timestamp, time.Now().UnixNano()),
	})

	pipe.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second)

	_, err := pipe.Exec(ctx)
	return err
}
