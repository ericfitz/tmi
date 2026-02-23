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

// CheckActiveInvocationLimit checks if user has reached their concurrent invocation limit
func (rl *AddonRateLimiter) CheckActiveInvocationLimit(ctx context.Context, userID uuid.UUID) error {
	logger := slogging.Get()

	// Get quota
	quota, err := rl.quotaStore.GetOrDefault(ctx, userID)
	if err != nil {
		logger.Error("Failed to get quota for user %s: %v", userID, err)
		return fmt.Errorf("failed to check quota: %w", err)
	}

	// Get active invocations for user (fetch one more than limit to check if over)
	activeInvocations, err := GlobalAddonInvocationStore.ListActiveForUser(ctx, userID, quota.MaxActiveInvocations+1)
	if err != nil {
		logger.Error("Failed to check active invocations for user %s: %v", userID, err)
		return fmt.Errorf("failed to check active invocations: %w", err)
	}

	if len(activeInvocations) >= quota.MaxActiveInvocations {
		logger.Warn("User %s has %d active invocations (limit: %d)",
			userID, len(activeInvocations), quota.MaxActiveInvocations)

		// Build blocking invocation details for the error response
		blockingInvocations := make([]map[string]any, 0, len(activeInvocations))
		var earliestTimeout time.Time

		for _, inv := range activeInvocations {
			timeout := inv.LastActivityAt.Add(AddonInvocationTimeout)
			if earliestTimeout.IsZero() || timeout.Before(earliestTimeout) {
				earliestTimeout = timeout
			}

			blockingInvocations = append(blockingInvocations, map[string]any{
				"invocation_id":     inv.ID.String(),
				"addon_id":          inv.AddonID.String(),
				"status":            inv.Status,
				"created_at":        inv.CreatedAt.Format(time.RFC3339),
				"expires_at":        timeout.Format(time.RFC3339),
				"seconds_remaining": int(time.Until(timeout).Seconds()),
			})
		}

		// Calculate retry_after (time until oldest invocation times out)
		retryAfter := max(int(time.Until(earliestTimeout).Seconds()), 0)

		suggestion := fmt.Sprintf("Wait for an existing invocation to complete, or retry after %d seconds when the oldest will timeout.", retryAfter)

		return &RequestError{
			Status:  429,
			Code:    "rate_limit_exceeded",
			Message: fmt.Sprintf("Active invocation limit reached: %d/%d concurrent invocations.", len(activeInvocations), quota.MaxActiveInvocations),
			Details: &ErrorDetails{
				Context: map[string]any{
					"limit":                quota.MaxActiveInvocations,
					"current":              len(activeInvocations),
					"retry_after":          retryAfter,
					"blocking_invocations": blockingInvocations,
				},
				Suggestion: &suggestion,
			},
		}
	}

	logger.Debug("Active invocation check passed for user %s: %d/%d",
		userID, len(activeInvocations), quota.MaxActiveInvocations)
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
			retryAfter = max(int(oldestTime+3600-now), 0)
		}

		return &RequestError{
			Status:  429,
			Code:    "rate_limit_exceeded",
			Message: fmt.Sprintf("Hourly invocation limit exceeded: %d/%d. Retry after %d seconds.", count, quota.MaxInvocationsPerHour, retryAfter),
			Details: &ErrorDetails{
				Context: map[string]any{
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
