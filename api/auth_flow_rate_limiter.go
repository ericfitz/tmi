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

// envForceAuthFlowRateLimiting, when set to "true", forces the auth-flow rate
// limiter to enforce limits even in build_mode=test. It exists so the
// integration suite can exercise the multi-scope limiter against a server that
// must otherwise run in build_mode=test for the built-in tmi OAuth provider.
// The override only ever makes limiting MORE restrictive and has no effect
// outside build_mode=test, so it is inert in production.
const envForceAuthFlowRateLimiting = "TMI_TEST_FORCE_AUTH_FLOW_RATE_LIMITING"

// AuthFlowRateLimiter implements multi-scope rate limiting for OAuth/SAML auth flows
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: enforce sliding-window rate limits on OAuth/SAML auth flows across session, IP, and user scopes (mutates shared state)
type AuthFlowRateLimiter struct {
	SlidingWindowRateLimiter
}

// NewAuthFlowRateLimiter creates a new auth flow rate limiter
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: build an AuthFlowRateLimiter backed by a Redis client (pure)
func NewAuthFlowRateLimiter(redisClient *redis.Client) *AuthFlowRateLimiter {
	return &AuthFlowRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: redisClient},
	}
}

// RateLimitResult represents the result of a rate limit check
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: carry the outcome of a rate limit check including scope, remaining quota, and retry timing (pure)
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
// SEM@c70d49ed2d6089c24d05f8bc287ba5711c73abde: clear the user-scoped rate limit counter after a successful login (mutates shared state)
func (r *AuthFlowRateLimiter) ResetUserRateLimit(ctx context.Context, userIdentifier string) {
	if userIdentifier == "" || r.RedisClient == nil {
		return
	}
	logger := slogging.Get()
	userKey := fmt.Sprintf("auth:ratelimit:user:60s:%s", userIdentifier)
	if err := r.RedisClient.Del(ctx, userKey).Err(); err != nil {
		logger.Error("failed to reset user rate limit for %s: %v", userIdentifier, err)
	}
}

// CheckRateLimit checks all three scopes and returns the most restrictive result
// Scopes: session (100/min), IP (100/min), user identifier (100/min)
// SEM@2ba330fcb59eb085d8f877fe8f75f90af9b69071: check all three auth-flow rate limit scopes and return the most restrictive result (reads DB)
func (r *AuthFlowRateLimiter) CheckRateLimit(ctx context.Context, sessionID string, ipAddress string, userIdentifier string) (*RateLimitResult, error) {
	return r.checkRateLimitWithIPLimit(ctx, sessionID, ipAddress, userIdentifier, 100)
}

// CheckRateLimitForTokenEndpoint checks rate limits for the token endpoint
// Uses the same 100 requests/minute per IP limit as other auth endpoints
// SEM@c70d49ed2d6089c24d05f8bc287ba5711c73abde: check rate limits for the token endpoint using the standard per-IP limit (reads DB)
func (r *AuthFlowRateLimiter) CheckRateLimitForTokenEndpoint(ctx context.Context, sessionID string, ipAddress string, userIdentifier string) (*RateLimitResult, error) {
	return r.checkRateLimitWithIPLimit(ctx, sessionID, ipAddress, userIdentifier, 100)
}

// checkRateLimitWithIPLimit implements multi-scope rate limiting with a configurable IP limit
// SEM@c70d49ed2d6089c24d05f8bc287ba5711c73abde: check session, IP, and user rate limit scopes with a configurable IP limit (reads DB)
func (r *AuthFlowRateLimiter) checkRateLimitWithIPLimit(ctx context.Context, sessionID string, ipAddress string, userIdentifier string, ipLimit int) (*RateLimitResult, error) {
	logger := slogging.Get()

	// Skip rate limiting in test mode to avoid false failures during
	// integration tests that perform many OAuth flows from localhost. A
	// dedicated test-only override (TMI_TEST_FORCE_AUTH_FLOW_RATE_LIMITING=true)
	// forces the limiter on so the multi-scope behavior can be exercised against
	// the integration server, which must otherwise run in build_mode=test for
	// the built-in tmi provider. The override is inert outside build_mode=test
	// and only ever makes limiting more restrictive.
	if os.Getenv("TMI_BUILD_MODE") == buildModeTest &&
		os.Getenv(envForceAuthFlowRateLimiting) != "true" {
		return &RateLimitResult{Allowed: true}, nil
	}

	if r.RedisClient == nil {
		logger.Warn("Redis not available, skipping auth flow rate limit check")
		return &RateLimitResult{Allowed: true}, nil
	}

	// Check session scope (100 requests/minute)
	if sessionID != "" {
		sessionKey := fmt.Sprintf("auth:ratelimit:session:60s:%s", sessionID)
		allowed, retryAfter, err := r.CheckSlidingWindow(ctx, sessionKey, 100, 60)
		if err != nil {
			logger.Error("failed to check session rate limit: %v", err)
			return nil, fmt.Errorf("session rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.GetRateLimitInfo(ctx, sessionKey, 100, 60)
			return &RateLimitResult{
				Allowed:        false,
				BlockedByScope: "session",
				RetryAfter:     retryAfter,
				Limit:          100,
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

	// Check user identifier scope (100 attempts/minute)
	if userIdentifier != "" {
		userKey := fmt.Sprintf("auth:ratelimit:user:60s:%s", userIdentifier)
		allowed, retryAfter, err := r.CheckSlidingWindow(ctx, userKey, 100, 60)
		if err != nil {
			logger.Error("failed to check user identifier rate limit: %v", err)
			return nil, fmt.Errorf("user identifier rate limit check failed: %w", err)
		}
		if !allowed {
			remaining, resetAt, _ := r.GetRateLimitInfo(ctx, userKey, 100, 60)
			return &RateLimitResult{
				Allowed:        false,
				BlockedByScope: "user",
				RetryAfter:     retryAfter,
				Limit:          100,
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
		remaining, resetAt, _ = r.GetRateLimitInfo(ctx, sessionKey, 100, 60)
	} else {
		remaining = 100
		resetAt = time.Now().Unix() + 60
	}

	return &RateLimitResult{
		Allowed:   true,
		Limit:     100,
		Remaining: remaining,
		ResetAt:   resetAt,
	}, nil
}
