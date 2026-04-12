package api

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/redis/go-redis/v9"
)

const buildModeTest = "test"

// AuthFlowRateLimiter implements multi-scope rate limiting for OAuth/SAML auth flows
type AuthFlowRateLimiter struct {
	SlidingWindowRateLimiter
}

// NewAuthFlowRateLimiter creates a new auth flow rate limiter
func NewAuthFlowRateLimiter(redisClient *redis.Client) *AuthFlowRateLimiter {
	return &AuthFlowRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: redisClient},
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

// ResetUserRateLimit clears the user identifier rate limit counter.
// Called after a successful login so that prior failed/exploratory attempts
// don't lock a legitimate user out for the remainder of the hour window.
func (r *AuthFlowRateLimiter) ResetUserRateLimit(ctx context.Context, userIdentifier string) {
	if userIdentifier == "" || r.RedisClient == nil {
		return
	}
	logger := slogging.Get()
	userKey := fmt.Sprintf("auth:ratelimit:user:3600s:%s", userIdentifier)
	if err := r.RedisClient.Del(ctx, userKey).Err(); err != nil {
		logger.Error("failed to reset user rate limit for %s: %v", userIdentifier, err)
	}
}

// CheckRateLimit checks all three scopes and returns the most restrictive result
// Scopes: session (5/min), IP (20/min for token endpoint, 100/min otherwise), user identifier (10/hour)
func (r *AuthFlowRateLimiter) CheckRateLimit(ctx context.Context, sessionID string, ipAddress string, userIdentifier string) (*RateLimitResult, error) {
	return r.checkRateLimitWithIPLimit(ctx, sessionID, ipAddress, userIdentifier, 100)
}

// CheckRateLimitForTokenEndpoint checks rate limits with a stricter IP limit for the token endpoint
// Token endpoint uses 20 requests/minute per IP to mitigate authorization code brute force
func (r *AuthFlowRateLimiter) CheckRateLimitForTokenEndpoint(ctx context.Context, sessionID string, ipAddress string, userIdentifier string) (*RateLimitResult, error) {
	return r.checkRateLimitWithIPLimit(ctx, sessionID, ipAddress, userIdentifier, 20)
}

// checkRateLimitWithIPLimit implements multi-scope rate limiting with a configurable IP limit
func (r *AuthFlowRateLimiter) checkRateLimitWithIPLimit(ctx context.Context, sessionID string, ipAddress string, userIdentifier string, ipLimit int) (*RateLimitResult, error) {
	logger := slogging.Get()

	// Skip rate limiting in test mode to avoid false failures during
	// integration tests that perform many OAuth flows from localhost.
	if os.Getenv("TMI_BUILD_MODE") == buildModeTest {
		return &RateLimitResult{Allowed: true}, nil
	}

	if r.RedisClient == nil {
		logger.Warn("Redis not available, skipping auth flow rate limit check")
		return &RateLimitResult{Allowed: true}, nil
	}

	// Check session scope (5 requests/minute)
	if sessionID != "" {
		sessionKey := fmt.Sprintf("auth:ratelimit:session:60s:%s", sessionID)
		allowed, retryAfter, err := r.CheckSlidingWindow(ctx, sessionKey, 5, 60)
		if err != nil {
			logger.Error("failed to check session rate limit: %v", err)
			return nil, fmt.Errorf("session rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.GetRateLimitInfo(ctx, sessionKey, 5, 60)
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

	// Check IP scope (configurable requests/minute)
	if ipAddress != "" {
		ipKey := fmt.Sprintf("auth:ratelimit:ip:60s:%s", ipAddress)
		allowed, retryAfter, err := r.CheckSlidingWindow(ctx, ipKey, ipLimit, 60)
		if err != nil {
			logger.Error("failed to check IP rate limit: %v", err)
			return nil, fmt.Errorf("IP rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.GetRateLimitInfo(ctx, ipKey, ipLimit, 60)
			return &RateLimitResult{
				Allowed:        false,
				BlockedByScope: "ip",
				RetryAfter:     retryAfter,
				Limit:          ipLimit,
				Remaining:      remaining,
				ResetAt:        resetAt,
			}, nil
		}
	}

	// Check user identifier scope (10 attempts/hour)
	if userIdentifier != "" {
		userKey := fmt.Sprintf("auth:ratelimit:user:3600s:%s", userIdentifier)
		allowed, retryAfter, err := r.CheckSlidingWindow(ctx, userKey, 10, 3600)
		if err != nil {
			logger.Error("failed to check user identifier rate limit: %v", err)
			return nil, fmt.Errorf("user identifier rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.GetRateLimitInfo(ctx, userKey, 10, 3600)
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
		remaining, resetAt, _ = r.GetRateLimitInfo(ctx, sessionKey, 5, 60)
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
