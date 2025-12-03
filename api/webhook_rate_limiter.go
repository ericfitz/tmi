package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// WebhookRateLimiter implements rate limiting for webhook operations using Redis
type WebhookRateLimiter struct {
	redisClient *redis.Client
}

// NewWebhookRateLimiter creates a new rate limiter
func NewWebhookRateLimiter(redisClient *redis.Client) *WebhookRateLimiter {
	return &WebhookRateLimiter{
		redisClient: redisClient,
	}
}

// CheckSubscriptionLimit checks if owner can create a new subscription
func (r *WebhookRateLimiter) CheckSubscriptionLimit(ctx context.Context, ownerID string) error {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, skipping subscription limit check")
		return nil
	}

	// Get quota for owner
	quota := GlobalWebhookQuotaStore.GetOrDefault(ownerID)

	// Count current subscriptions
	count, err := GlobalWebhookSubscriptionStore.CountByOwner(ownerID)
	if err != nil {
		logger.Error("failed to count subscriptions for %s: %v", ownerID, err)
		return fmt.Errorf("failed to check subscription limit: %w", err)
	}

	if count >= quota.MaxSubscriptions {
		return fmt.Errorf("subscription limit exceeded: %d/%d", count, quota.MaxSubscriptions)
	}

	return nil
}

// CheckSubscriptionRequestLimit checks rate limit for subscription creation requests
func (r *WebhookRateLimiter) CheckSubscriptionRequestLimit(ctx context.Context, ownerID string) error {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, skipping subscription request rate limit")
		return nil
	}

	// Get quota for owner
	quota := GlobalWebhookQuotaStore.GetOrDefault(ownerID)

	// Check per-minute rate limit
	perMinuteKey := fmt.Sprintf("webhook:ratelimit:sub:minute:%s", ownerID)
	allowed, err := r.checkSlidingWindow(ctx, perMinuteKey, quota.MaxSubscriptionRequestsPerMinute, 60)
	if err != nil {
		logger.Error("failed to check per-minute rate limit: %v", err)
		return fmt.Errorf("rate limit check failed: %w", err)
	}
	if !allowed {
		return fmt.Errorf("subscription request rate limit exceeded: %d requests per minute", quota.MaxSubscriptionRequestsPerMinute)
	}

	// Check per-day rate limit
	perDayKey := fmt.Sprintf("webhook:ratelimit:sub:day:%s", ownerID)
	allowed, err = r.checkSlidingWindow(ctx, perDayKey, quota.MaxSubscriptionRequestsPerDay, 86400)
	if err != nil {
		logger.Error("failed to check per-day rate limit: %v", err)
		return fmt.Errorf("rate limit check failed: %w", err)
	}
	if !allowed {
		return fmt.Errorf("subscription request rate limit exceeded: %d requests per day", quota.MaxSubscriptionRequestsPerDay)
	}

	return nil
}

// CheckEventPublicationLimit checks rate limit for event publications
func (r *WebhookRateLimiter) CheckEventPublicationLimit(ctx context.Context, ownerID string) error {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, skipping event publication rate limit")
		return nil
	}

	// Get quota for owner
	quota := GlobalWebhookQuotaStore.GetOrDefault(ownerID)

	// Check per-minute event publication limit
	perMinuteKey := fmt.Sprintf("webhook:ratelimit:events:minute:%s", ownerID)
	allowed, err := r.checkSlidingWindow(ctx, perMinuteKey, quota.MaxEventsPerMinute, 60)
	if err != nil {
		logger.Error("failed to check event publication rate limit: %v", err)
		return fmt.Errorf("rate limit check failed: %w", err)
	}
	if !allowed {
		return fmt.Errorf("event publication rate limit exceeded: %d events per minute", quota.MaxEventsPerMinute)
	}

	return nil
}

// checkSlidingWindow implements sliding window rate limiting using Redis sorted sets
func (r *WebhookRateLimiter) checkSlidingWindow(ctx context.Context, key string, limit int, windowSeconds int) (bool, error) {
	now := time.Now().Unix()
	windowStart := now - int64(windowSeconds)

	pipe := r.redisClient.Pipeline()

	// Remove old entries outside the window
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))

	// Count entries in current window
	countCmd := pipe.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), "+inf")

	// Set expiration to window + 60 seconds
	pipe.Expire(ctx, key, time.Duration(windowSeconds+60)*time.Second)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	count := countCmd.Val()

	// Check if we're at or over the limit
	if count >= int64(limit) {
		return false, nil
	}

	// Add current request only if under limit
	_, err = r.redisClient.ZAdd(ctx, key, &redis.Z{
		Score:  float64(now),
		Member: fmt.Sprintf("%d:%d", now, time.Now().UnixNano()),
	}).Result()
	if err != nil {
		return false, err
	}

	return true, nil
}

// RecordSubscriptionRequest records a subscription creation request for rate limiting
func (r *WebhookRateLimiter) RecordSubscriptionRequest(ctx context.Context, ownerID string) error {
	if r.redisClient == nil {
		return nil
	}

	now := time.Now().Unix()

	// Record in per-minute window
	perMinuteKey := fmt.Sprintf("webhook:ratelimit:sub:minute:%s", ownerID)
	if err := r.recordInWindow(ctx, perMinuteKey, now, 120); err != nil {
		return err
	}

	// Record in per-day window
	perDayKey := fmt.Sprintf("webhook:ratelimit:sub:day:%s", ownerID)
	if err := r.recordInWindow(ctx, perDayKey, now, 86460); err != nil {
		return err
	}

	return nil
}

// RecordEventPublication records an event publication for rate limiting
func (r *WebhookRateLimiter) RecordEventPublication(ctx context.Context, ownerID string) error {
	if r.redisClient == nil {
		return nil
	}

	now := time.Now().Unix()

	perMinuteKey := fmt.Sprintf("webhook:ratelimit:events:minute:%s", ownerID)
	return r.recordInWindow(ctx, perMinuteKey, now, 120)
}

// recordInWindow records a timestamp in a sliding window
func (r *WebhookRateLimiter) recordInWindow(ctx context.Context, key string, timestamp int64, ttlSeconds int) error {
	pipe := r.redisClient.Pipeline()

	pipe.ZAdd(ctx, key, &redis.Z{
		Score:  float64(timestamp),
		Member: fmt.Sprintf("%d:%d", timestamp, time.Now().UnixNano()),
	})

	pipe.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second)

	_, err := pipe.Exec(ctx)
	return err
}

// GetSubscriptionRateLimitInfo returns current subscription request rate limit status
func (r *WebhookRateLimiter) GetSubscriptionRateLimitInfo(ctx context.Context, ownerID string) (limit int, remaining int, resetAt int64, err error) {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, returning default subscription rate limit info")
		return DefaultMaxSubscriptionRequestsPerMinute, DefaultMaxSubscriptionRequestsPerMinute, time.Now().Unix() + 60, nil
	}

	// Get quota for owner
	quota := GlobalWebhookQuotaStore.GetOrDefault(ownerID)

	// Get current count in the minute window
	now := time.Now().Unix()
	windowStart := now - 60
	perMinuteKey := fmt.Sprintf("webhook:ratelimit:sub:minute:%s", ownerID)

	count, err := r.redisClient.ZCount(ctx, perMinuteKey, fmt.Sprintf("%d", windowStart), "+inf").Result()
	if err != nil {
		logger.Error("failed to get subscription rate limit count for owner %s: %v", ownerID, err)
		return quota.MaxSubscriptionRequestsPerMinute, quota.MaxSubscriptionRequestsPerMinute, now + 60, nil
	}

	remaining = quota.MaxSubscriptionRequestsPerMinute - int(count)
	if remaining < 0 {
		remaining = 0
	}

	// Calculate reset time (oldest entry + window duration)
	oldestScore, err := r.redisClient.ZRangeWithScores(ctx, perMinuteKey, 0, 0).Result()
	resetAt = now + 60 // Default to 60 seconds from now
	if err == nil && len(oldestScore) > 0 {
		resetAt = int64(oldestScore[0].Score) + 60
	}

	return quota.MaxSubscriptionRequestsPerMinute, remaining, resetAt, nil
}
