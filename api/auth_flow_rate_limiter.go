package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// AuthFlowRateLimiter implements multi-scope rate limiting for OAuth/SAML auth flows
type AuthFlowRateLimiter struct {
	redisClient *redis.Client
}

// NewAuthFlowRateLimiter creates a new auth flow rate limiter
func NewAuthFlowRateLimiter(redisClient *redis.Client) *AuthFlowRateLimiter {
	return &AuthFlowRateLimiter{
		redisClient: redisClient,
	}
}

// RateLimitResult represents the result of a rate limit check
type RateLimitResult struct {
	Allowed        bool
	BlockedByScope string // "session", "ip", or "user"
	RetryAfter     int    // seconds
	Limit          int
	Remaining      int
	ResetAt        int64
}

// CheckRateLimit checks all three scopes and returns the most restrictive result
// Scopes: session (5/min), IP (100/min), user identifier (10/hour)
func (r *AuthFlowRateLimiter) CheckRateLimit(ctx context.Context, sessionID string, ipAddress string, userIdentifier string) (*RateLimitResult, error) {
	logger := slogging.Get()

	if r.redisClient == nil {
		logger.Warn("Redis not available, skipping auth flow rate limit check")
		return &RateLimitResult{Allowed: true}, nil
	}

	// Check session scope (5 requests/minute)
	if sessionID != "" {
		sessionKey := fmt.Sprintf("auth:ratelimit:session:60s:%s", sessionID)
		allowed, retryAfter, err := r.checkSlidingWindow(ctx, sessionKey, 5, 60)
		if err != nil {
			logger.Error("failed to check session rate limit: %v", err)
			return nil, fmt.Errorf("session rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.getRateLimitInfo(ctx, sessionKey, 5, 60)
			return &RateLimitResult{
				Allowed:        false,
				BlockedByScope: "session",
				RetryAfter:     retryAfter,
				Limit:          5,
				Remaining:      remaining,
				ResetAt:        resetAt,
			}, nil
		}
	}

	// Check IP scope (100 requests/minute)
	if ipAddress != "" {
		ipKey := fmt.Sprintf("auth:ratelimit:ip:60s:%s", ipAddress)
		allowed, retryAfter, err := r.checkSlidingWindow(ctx, ipKey, 100, 60)
		if err != nil {
			logger.Error("failed to check IP rate limit: %v", err)
			return nil, fmt.Errorf("IP rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.getRateLimitInfo(ctx, ipKey, 100, 60)
			return &RateLimitResult{
				Allowed:        false,
				BlockedByScope: "ip",
				RetryAfter:     retryAfter,
				Limit:          100,
				Remaining:      remaining,
				ResetAt:        resetAt,
			}, nil
		}
	}

	// Check user identifier scope (10 attempts/hour)
	if userIdentifier != "" {
		userKey := fmt.Sprintf("auth:ratelimit:user:3600s:%s", userIdentifier)
		allowed, retryAfter, err := r.checkSlidingWindow(ctx, userKey, 10, 3600)
		if err != nil {
			logger.Error("failed to check user identifier rate limit: %v", err)
			return nil, fmt.Errorf("user identifier rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.getRateLimitInfo(ctx, userKey, 10, 3600)
			return &RateLimitResult{
				Allowed:        false,
				BlockedByScope: "user",
				RetryAfter:     retryAfter,
				Limit:          10,
				Remaining:      remaining,
				ResetAt:        resetAt,
			}, nil
		}
	}

	// All scopes passed - return session scope info (most restrictive window)
	var remaining int
	var resetAt int64
	if sessionID != "" {
		sessionKey := fmt.Sprintf("auth:ratelimit:session:60s:%s", sessionID)
		remaining, resetAt, _ = r.getRateLimitInfo(ctx, sessionKey, 5, 60)
	} else {
		remaining = 5
		resetAt = time.Now().Unix() + 60
	}

	return &RateLimitResult{
		Allowed:   true,
		Limit:     5,
		Remaining: remaining,
		ResetAt:   resetAt,
	}, nil
}

// getRateLimitInfo returns current rate limit status for a key
func (r *AuthFlowRateLimiter) getRateLimitInfo(ctx context.Context, key string, limit int, windowSeconds int) (int, int64, error) {
	now := time.Now().Unix()
	windowStart := now - int64(windowSeconds)

	count, err := r.redisClient.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), "+inf").Result()
	if err != nil {
		return limit, now + int64(windowSeconds), err
	}

	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}

	// Calculate reset time
	oldestScore, err := r.redisClient.ZRangeWithScores(ctx, key, 0, 0).Result()
	resetAt := now + int64(windowSeconds)
	if err == nil && len(oldestScore) > 0 {
		resetAt = int64(oldestScore[0].Score) + int64(windowSeconds)
	}

	return remaining, resetAt, nil
}

// checkSlidingWindow implements sliding window rate limiting using Redis sorted sets
func (r *AuthFlowRateLimiter) checkSlidingWindow(ctx context.Context, key string, limit int, windowSeconds int) (bool, int, error) {
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
