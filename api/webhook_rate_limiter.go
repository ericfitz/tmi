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
	SlidingWindowRateLimiter
}

// NewWebhookRateLimiter creates a new rate limiter
func NewWebhookRateLimiter(redisClient *redis.Client) *WebhookRateLimiter {
	return &WebhookRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: redisClient},
	}
}

// CheckSubscriptionLimit checks if owner can create a new subscription
func (r *WebhookRateLimiter) CheckSubscriptionLimit(ctx context.Context, ownerID string) error {
	logger := slogging.Get()

	if r.RedisClient == nil {
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

	if r.RedisClient == nil {
		logger.Warn("Redis not available, skipping subscription request rate limit")
		return nil
	}

	// Get quota for owner
	quota := GlobalWebhookQuotaStore.GetOrDefault(ownerID)

	// Check per-minute rate limit
	perMinuteKey := fmt.Sprintf("webhook:ratelimit:sub:minute:%s", ownerID)
	allowed, err := r.CheckSlidingWindowSimple(ctx, perMinuteKey, quota.MaxSubscriptionRequestsPerMinute, 60)
	if err != nil {
		logger.Error("failed to check per-minute rate limit: %v", err)
		return fmt.Errorf("rate limit check failed: %w", err)
	}
	if !allowed {
		return fmt.Errorf("subscription request rate limit exceeded: %d requests per minute", quota.MaxSubscriptionRequestsPerMinute)
	}

	// Check per-day rate limit
	perDayKey := fmt.Sprintf("webhook:ratelimit:sub:day:%s", ownerID)
	allowed, err = r.CheckSlidingWindowSimple(ctx, perDayKey, quota.MaxSubscriptionRequestsPerDay, 86400)
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

	if r.RedisClient == nil {
		logger.Warn("Redis not available, skipping event publication rate limit")
		return nil
	}

	// Get quota for owner
	quota := GlobalWebhookQuotaStore.GetOrDefault(ownerID)

	// Check per-minute event publication limit
	perMinuteKey := fmt.Sprintf("webhook:ratelimit:events:minute:%s", ownerID)
	allowed, err := r.CheckSlidingWindowSimple(ctx, perMinuteKey, quota.MaxEventsPerMinute, 60)
	if err != nil {
		logger.Error("failed to check event publication rate limit: %v", err)
		return fmt.Errorf("rate limit check failed: %w", err)
	}
	if !allowed {
		return fmt.Errorf("event publication rate limit exceeded: %d events per minute", quota.MaxEventsPerMinute)
	}

	return nil
}

// RecordSubscriptionRequest records a subscription creation request for rate limiting
func (r *WebhookRateLimiter) RecordSubscriptionRequest(ctx context.Context, ownerID string) error {
	if r.RedisClient == nil {
		return nil
	}

	now := time.Now().Unix()

	// Record in per-minute window
	perMinuteKey := fmt.Sprintf("webhook:ratelimit:sub:minute:%s", ownerID)
	if err := r.RecordInWindow(ctx, perMinuteKey, now, 120); err != nil {
		return err
	}

	// Record in per-day window
	perDayKey := fmt.Sprintf("webhook:ratelimit:sub:day:%s", ownerID)
	if err := r.RecordInWindow(ctx, perDayKey, now, 86460); err != nil {
		return err
	}

	return nil
}

// RecordEventPublication records an event publication for rate limiting
func (r *WebhookRateLimiter) RecordEventPublication(ctx context.Context, ownerID string) error {
	if r.RedisClient == nil {
		return nil
	}

	now := time.Now().Unix()

	perMinuteKey := fmt.Sprintf("webhook:ratelimit:events:minute:%s", ownerID)
	return r.RecordInWindow(ctx, perMinuteKey, now, 120)
}

// GetSubscriptionRateLimitInfo returns current subscription request rate limit status
func (r *WebhookRateLimiter) GetSubscriptionRateLimitInfo(ctx context.Context, ownerID string) (limit int, remaining int, resetAt int64, err error) {
	logger := slogging.Get()

	if r.RedisClient == nil {
		logger.Warn("Redis not available, returning default subscription rate limit info")
		return DefaultMaxSubscriptionRequestsPerMinute, DefaultMaxSubscriptionRequestsPerMinute, time.Now().Unix() + 60, nil
	}

	// Get quota for owner
	quota := GlobalWebhookQuotaStore.GetOrDefault(ownerID)

	perMinuteKey := fmt.Sprintf("webhook:ratelimit:sub:minute:%s", ownerID)
	remaining, resetAt, err = r.GetRateLimitInfo(ctx, perMinuteKey, quota.MaxSubscriptionRequestsPerMinute, 60)
	if err != nil {
		logger.Error("failed to get subscription rate limit count for owner %s: %v", ownerID, err)
		return quota.MaxSubscriptionRequestsPerMinute, quota.MaxSubscriptionRequestsPerMinute, time.Now().Unix() + 60, nil
	}

	return quota.MaxSubscriptionRequestsPerMinute, remaining, resetAt, nil
}
