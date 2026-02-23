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
	SlidingWindowRateLimiter
	quotaStore UserAPIQuotaStoreInterface
}

// NewAPIRateLimiter creates a new API rate limiter
func NewAPIRateLimiter(redisClient *redis.Client, quotaStore UserAPIQuotaStoreInterface) *APIRateLimiter {
	return &APIRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: redisClient},
		quotaStore:               quotaStore,
	}
}

// CheckRateLimit checks if a user has exceeded their rate limit
// Returns allowed (bool), retryAfter (seconds), and error
func (r *APIRateLimiter) CheckRateLimit(ctx context.Context, userID string) (bool, int, error) {
	logger := slogging.Get()

	if r.RedisClient == nil {
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
	allowed, retryAfter, err := r.CheckSlidingWindow(ctx, perMinuteKey, quota.MaxRequestsPerMinute, 60)
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
		allowed, retryAfter, err := r.CheckSlidingWindow(ctx, perHourKey, *quota.MaxRequestsPerHour, 3600)
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

	if r.RedisClient == nil {
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

	perMinuteKey := fmt.Sprintf("api:ratelimit:minute:%s", userID)
	remaining, resetAt, err = r.SlidingWindowRateLimiter.GetRateLimitInfo(ctx, perMinuteKey, quota.MaxRequestsPerMinute, 60)
	if err != nil {
		logger.Error("failed to get rate limit count for user %s: %v", userID, err)
		return quota.MaxRequestsPerMinute, quota.MaxRequestsPerMinute, time.Now().Unix() + 60, nil
	}

	return quota.MaxRequestsPerMinute, remaining, resetAt, nil
}
