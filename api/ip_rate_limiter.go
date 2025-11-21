package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// IPRateLimiter implements rate limiting based on IP address
type IPRateLimiter struct {
	redisClient *redis.Client
}

// NewIPRateLimiter creates a new IP-based rate limiter
func NewIPRateLimiter(redisClient *redis.Client) *IPRateLimiter {
	return &IPRateLimiter{
		redisClient: redisClient,
	}
}

// CheckRateLimit checks if an IP has exceeded its rate limit
// Returns allowed (bool), retryAfter (seconds), and error
func (r *IPRateLimiter) CheckRateLimit(ctx context.Context, ipAddress string, limit int, windowSeconds int) (bool, int, error) {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, skipping IP rate limit check")
		return true, 0, nil
	}

	key := fmt.Sprintf("ip:ratelimit:%ds:%s", windowSeconds, ipAddress)
	allowed, retryAfter, err := r.checkSlidingWindow(ctx, key, limit, windowSeconds)
	if err != nil {
		logger.Error("failed to check IP rate limit for %s: %v", ipAddress, err)
		return false, 0, fmt.Errorf("rate limit check failed: %w", err)
	}

	return allowed, retryAfter, nil
}

// GetRateLimitInfo returns current rate limit status for an IP
func (r *IPRateLimiter) GetRateLimitInfo(ctx context.Context, ipAddress string, limit int, windowSeconds int) (remaining int, resetAt int64, err error) {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, returning default IP rate limit info")
		return limit, time.Now().Unix() + int64(windowSeconds), nil
	}

	now := time.Now().Unix()
	windowStart := now - int64(windowSeconds)
	key := fmt.Sprintf("ip:ratelimit:%ds:%s", windowSeconds, ipAddress)

	count, err := r.redisClient.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), "+inf").Result()
	if err != nil {
		logger.Error("failed to get IP rate limit count for %s: %v", ipAddress, err)
		return limit, now + int64(windowSeconds), nil
	}

	remaining = limit - int(count)
	if remaining < 0 {
		remaining = 0
	}

	// Calculate reset time (oldest entry + window duration)
	oldestScore, err := r.redisClient.ZRangeWithScores(ctx, key, 0, 0).Result()
	resetAt = now + int64(windowSeconds) // Default
	if err == nil && len(oldestScore) > 0 {
		resetAt = int64(oldestScore[0].Score) + int64(windowSeconds)
	}

	return remaining, resetAt, nil
}

// checkSlidingWindow implements sliding window rate limiting using Redis sorted sets
func (r *IPRateLimiter) checkSlidingWindow(ctx context.Context, key string, limit int, windowSeconds int) (bool, int, error) {
	now := time.Now().Unix()
	windowStart := now - int64(windowSeconds)

	pipe := r.redisClient.Pipeline()

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
	_, err = r.redisClient.ZAdd(ctx, key, &redis.Z{
		Score:  float64(now),
		Member: fmt.Sprintf("%d:%d", now, time.Now().UnixNano()),
	}).Result()
	if err != nil {
		return false, 0, err
	}

	return true, 0, nil
}
