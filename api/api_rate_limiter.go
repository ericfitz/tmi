package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// APIRateLimiter implements rate limiting for general API operations using Redis
type APIRateLimiter struct {
	redisClient *redis.Client
	quotaStore  UserAPIQuotaStoreInterface
}

// NewAPIRateLimiter creates a new API rate limiter
func NewAPIRateLimiter(redisClient *redis.Client, quotaStore UserAPIQuotaStoreInterface) *APIRateLimiter {
	return &APIRateLimiter{
		redisClient: redisClient,
		quotaStore:  quotaStore,
	}
}

// CheckRateLimit checks if a user has exceeded their rate limit
// Returns allowed (bool), retryAfter (seconds), and error
func (r *APIRateLimiter) CheckRateLimit(ctx context.Context, userID string) (bool, int, error) {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, skipping API rate limit check")
		return true, 0, nil
	}

	// Get quota for user (with caching if available)
	var quota UserAPIQuota
	if GlobalQuotaCache != nil {
		quota = GlobalQuotaCache.GetUserAPIQuota(userID, r.quotaStore)
	} else {
		quota = r.quotaStore.GetOrDefault(userID)
	}

	// Check per-minute rate limit
	perMinuteKey := fmt.Sprintf("api:ratelimit:minute:%s", userID)
	allowed, retryAfter, err := r.checkSlidingWindow(ctx, perMinuteKey, quota.MaxRequestsPerMinute, 60)
	if err != nil {
		logger.Error("failed to check per-minute rate limit for user %s: %v", userID, err)
		return false, 0, fmt.Errorf("rate limit check failed: %w", err)
	}
	if !allowed {
		return false, retryAfter, nil
	}

	// Check per-hour rate limit if configured
	if quota.MaxRequestsPerHour != nil {
		perHourKey := fmt.Sprintf("api:ratelimit:hour:%s", userID)
		allowed, retryAfter, err := r.checkSlidingWindow(ctx, perHourKey, *quota.MaxRequestsPerHour, 3600)
		if err != nil {
			logger.Error("failed to check per-hour rate limit for user %s: %v", userID, err)
			return false, 0, fmt.Errorf("rate limit check failed: %w", err)
		}
		if !allowed {
			return false, retryAfter, nil
		}
	}

	return true, 0, nil
}

// GetRateLimitInfo returns current rate limit status for a user
func (r *APIRateLimiter) GetRateLimitInfo(ctx context.Context, userID string) (limit int, remaining int, resetAt int64, err error) {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, returning default rate limit info")
		return DefaultMaxRequestsPerMinute, DefaultMaxRequestsPerMinute, time.Now().Unix() + 60, nil
	}

	// Get quota for user (with caching if available)
	var quota UserAPIQuota
	if GlobalQuotaCache != nil {
		quota = GlobalQuotaCache.GetUserAPIQuota(userID, r.quotaStore)
	} else {
		quota = r.quotaStore.GetOrDefault(userID)
	}

	// Get current count in the minute window
	now := time.Now().Unix()
	windowStart := now - 60
	perMinuteKey := fmt.Sprintf("api:ratelimit:minute:%s", userID)

	count, err := r.redisClient.ZCount(ctx, perMinuteKey, fmt.Sprintf("%d", windowStart), "+inf").Result()
	if err != nil {
		logger.Error("failed to get rate limit count for user %s: %v", userID, err)
		return quota.MaxRequestsPerMinute, quota.MaxRequestsPerMinute, now + 60, nil
	}

	remaining = quota.MaxRequestsPerMinute - int(count)
	if remaining < 0 {
		remaining = 0
	}

	// Calculate reset time (oldest entry + window duration)
	oldestScore, err := r.redisClient.ZRangeWithScores(ctx, perMinuteKey, 0, 0).Result()
	resetAt = now + 60 // Default to 60 seconds from now
	if err == nil && len(oldestScore) > 0 {
		resetAt = int64(oldestScore[0].Score) + 60
	}

	return quota.MaxRequestsPerMinute, remaining, resetAt, nil
}

// checkSlidingWindow implements sliding window rate limiting using Redis sorted sets
// Returns allowed (bool), retryAfter (seconds), and error
func (r *APIRateLimiter) checkSlidingWindow(ctx context.Context, key string, limit int, windowSeconds int) (bool, int, error) {
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
