package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// AddonRateLimiter provides rate limiting for add-on invocations
type AddonRateLimiter struct {
	redis      *db.RedisDB
	builder    *db.RedisKeyBuilder
	quotaStore AddonInvocationQuotaStore
}

// NewAddonRateLimiter creates a new rate limiter
func NewAddonRateLimiter(redis *db.RedisDB, quotaStore AddonInvocationQuotaStore) *AddonRateLimiter {
	return &AddonRateLimiter{
		redis:      redis,
		builder:    db.NewRedisKeyBuilder(),
		quotaStore: quotaStore,
	}
}

// buildRateLimitKey creates the Redis key for hourly rate limit tracking
func (rl *AddonRateLimiter) buildRateLimitKey(userID uuid.UUID) string {
	return fmt.Sprintf("addon:ratelimit:hour:%s", userID.String())
}

// CheckActiveInvocationLimit checks if user has an active invocation (blocks if they do)
func (rl *AddonRateLimiter) CheckActiveInvocationLimit(ctx context.Context, userID uuid.UUID) error {
	logger := slogging.Get()

	// Get quota
	quota, err := rl.quotaStore.GetOrDefault(ctx, userID)
	if err != nil {
		logger.Error("Failed to get quota for user %s: %v", userID, err)
		return fmt.Errorf("failed to check quota: %w", err)
	}

	// Check if user has an active invocation
	activeInvocation, err := GlobalAddonInvocationStore.GetActiveForUser(ctx, userID)
	if err != nil {
		logger.Error("Failed to check active invocation for user %s: %v", userID, err)
		return fmt.Errorf("failed to check active invocation: %w", err)
	}

	if activeInvocation != nil {
		logger.Warn("User %s has active invocation (limit: %d): invocation_id=%s",
			userID, quota.MaxActiveInvocations, activeInvocation.ID)
		return &RequestError{
			Status:  429,
			Code:    "rate_limit_exceeded",
			Message: fmt.Sprintf("You already have an active add-on invocation. Please wait for it to complete. (Limit: %d concurrent invocation)", quota.MaxActiveInvocations),
		}
	}

	logger.Debug("Active invocation check passed for user %s", userID)
	return nil
}

// CheckHourlyRateLimit checks if user has exceeded hourly invocation limit using sliding window
func (rl *AddonRateLimiter) CheckHourlyRateLimit(ctx context.Context, userID uuid.UUID) error {
	logger := slogging.Get()

	// Get quota
	quota, err := rl.quotaStore.GetOrDefault(ctx, userID)
	if err != nil {
		logger.Error("Failed to get quota for user %s: %v", userID, err)
		return fmt.Errorf("failed to check quota: %w", err)
	}

	key := rl.buildRateLimitKey(userID)
	now := time.Now().Unix()
	windowStart := now - 3600 // 1 hour ago

	client := rl.redis.GetClient()

	// Remove old entries outside the window
	err = client.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart)).Err()
	if err != nil {
		logger.Error("Failed to clean old rate limit entries for user %s: %v", userID, err)
		// Continue despite error
	}

	// Count entries in current window
	count, err := client.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), fmt.Sprintf("%d", now)).Result()
	if err != nil {
		logger.Error("Failed to count rate limit entries for user %s: %v", userID, err)
		return fmt.Errorf("failed to check rate limit: %w", err)
	}

	if count >= int64(quota.MaxInvocationsPerHour) {
		logger.Warn("User %s exceeded hourly rate limit: %d/%d",
			userID, count, quota.MaxInvocationsPerHour)

		// Calculate retry-after (time until oldest entry expires)
		oldestScores, err := client.ZRangeWithScores(ctx, key, 0, 0).Result()
		retryAfter := 3600 // default 1 hour
		if err == nil && len(oldestScores) > 0 {
			oldestTime := int64(oldestScores[0].Score)
			retryAfter = int(oldestTime + 3600 - now)
			if retryAfter < 0 {
				retryAfter = 0
			}
		}

		return &RequestError{
			Status:  429,
			Code:    "rate_limit_exceeded",
			Message: fmt.Sprintf("Hourly invocation limit exceeded: %d/%d. Retry after %d seconds.", count, quota.MaxInvocationsPerHour, retryAfter),
			Details: &ErrorDetails{
				Context: map[string]interface{}{
					"retry_after": retryAfter,
					"limit":       quota.MaxInvocationsPerHour,
					"current":     count,
				},
			},
		}
	}

	logger.Debug("Hourly rate limit check passed for user %s: %d/%d",
		userID, count, quota.MaxInvocationsPerHour)

	return nil
}

// RecordInvocation records a new invocation in the sliding window
func (rl *AddonRateLimiter) RecordInvocation(ctx context.Context, userID uuid.UUID) error {
	logger := slogging.Get()

	key := rl.buildRateLimitKey(userID)
	now := time.Now()
	score := now.Unix()

	// Create unique member using timestamp + nanoseconds
	member := fmt.Sprintf("%d:%d", score, now.UnixNano())

	client := rl.redis.GetClient()

	// Add to sorted set
	err := client.ZAdd(ctx, key, &redis.Z{
		Score:  float64(score),
		Member: member,
	}).Err()
	if err != nil {
		logger.Error("Failed to record invocation for user %s: %v", userID, err)
		return fmt.Errorf("failed to record invocation: %w", err)
	}

	// Set TTL (window + buffer)
	err = rl.redis.Expire(ctx, key, 3660*time.Second) // 1 hour + 1 minute buffer
	if err != nil {
		logger.Error("Failed to set TTL for rate limit key for user %s: %v", userID, err)
		// Don't fail the operation for this
	}

	logger.Debug("Recorded invocation for user %s in sliding window", userID)

	return nil
}

// GlobalAddonRateLimiter is the global singleton for rate limiting
var GlobalAddonRateLimiter *AddonRateLimiter
