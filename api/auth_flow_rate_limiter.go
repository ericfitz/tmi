package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// AuthFlowRateLimiterConfig holds configurable limits for the auth flow rate limiter
type AuthFlowRateLimiterConfig struct {
	// SessionRequestsPerMinute is the max auth-flow requests per minute per OAuth state/session
	SessionRequestsPerMinute int
	// IPRequestsPerMinute is the max auth-flow requests per minute per IP address
	IPRequestsPerMinute int
	// UserRequestsPerHour is the max auth-flow attempts per hour per login_hint value
	UserRequestsPerHour int
}

// DefaultAuthFlowRateLimiterConfig returns safe production defaults
func DefaultAuthFlowRateLimiterConfig() AuthFlowRateLimiterConfig {
	return AuthFlowRateLimiterConfig{
		SessionRequestsPerMinute: 5,
		IPRequestsPerMinute:      100,
		UserRequestsPerHour:      10,
	}
}

// AuthFlowRateLimiter implements multi-scope rate limiting for OAuth/SAML auth flows
type AuthFlowRateLimiter struct {
	SlidingWindowRateLimiter
	Config AuthFlowRateLimiterConfig
}

// NewAuthFlowRateLimiter creates a new auth flow rate limiter with the given config
func NewAuthFlowRateLimiter(redisClient *redis.Client, config AuthFlowRateLimiterConfig) *AuthFlowRateLimiter {
	return &AuthFlowRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: redisClient},
		Config:                   config,
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
// Scopes: session (Config.SessionRequestsPerMinute/min), IP (Config.IPRequestsPerMinute/min), user identifier (Config.UserRequestsPerHour/hour)
func (r *AuthFlowRateLimiter) CheckRateLimit(ctx context.Context, sessionID string, ipAddress string, userIdentifier string) (*RateLimitResult, error) {
	logger := slogging.Get()

	if r.RedisClient == nil {
		logger.Warn("Redis not available, skipping auth flow rate limit check")
		return &RateLimitResult{Allowed: true}, nil
	}

	sessionLimit := r.Config.SessionRequestsPerMinute
	ipLimit := r.Config.IPRequestsPerMinute
	userLimit := r.Config.UserRequestsPerHour

	// Check session scope
	if sessionID != "" {
		sessionKey := fmt.Sprintf("auth:ratelimit:session:60s:%s", sessionID)
		allowed, retryAfter, err := r.CheckSlidingWindow(ctx, sessionKey, sessionLimit, 60)
		if err != nil {
			logger.Error("failed to check session rate limit: %v", err)
			return nil, fmt.Errorf("session rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.GetRateLimitInfo(ctx, sessionKey, sessionLimit, 60)
			return &RateLimitResult{
				Allowed:        false,
				BlockedByScope: "session",
				RetryAfter:     retryAfter,
				Limit:          sessionLimit,
				Remaining:      remaining,
				ResetAt:        resetAt,
			}, nil
		}
	}

	// Check IP scope
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

	// Check user identifier scope
	if userIdentifier != "" {
		userKey := fmt.Sprintf("auth:ratelimit:user:3600s:%s", userIdentifier)
		allowed, retryAfter, err := r.CheckSlidingWindow(ctx, userKey, userLimit, 3600)
		if err != nil {
			logger.Error("failed to check user identifier rate limit: %v", err)
			return nil, fmt.Errorf("user identifier rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.GetRateLimitInfo(ctx, userKey, userLimit, 3600)
			return &RateLimitResult{
				Allowed:        false,
				BlockedByScope: "user",
				RetryAfter:     retryAfter,
				Limit:          userLimit,
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
		remaining, resetAt, _ = r.GetRateLimitInfo(ctx, sessionKey, sessionLimit, 60)
	} else {
		remaining = sessionLimit
		resetAt = time.Now().Unix() + 60
	}

	return &RateLimitResult{
		Allowed:   true,
		Limit:     sessionLimit,
		Remaining: remaining,
		ResetAt:   resetAt,
	}, nil
}
