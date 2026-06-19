package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/redis/go-redis/v9"
)

// IPRateLimiter implements rate limiting based on IP address
// SEM@96060a8db0ddd49240156ad864036a242325d82e: sliding-window rate limiter keyed by IP address with configurable limit and window (mutates shared state)
type IPRateLimiter struct {
	SlidingWindowRateLimiter
	DefaultLimit         int // Requests per window (default: 10)
	DefaultWindowSeconds int // Window size in seconds (default: 60)
}

// NewIPRateLimiter creates a new IP-based rate limiter
// SEM@96060a8db0ddd49240156ad864036a242325d82e: build an IP rate limiter backed by Redis with default 10 requests per 60-second window
func NewIPRateLimiter(redisClient *redis.Client) *IPRateLimiter {
	return &IPRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: redisClient},
		DefaultLimit:             10,
		DefaultWindowSeconds:     60,
	}
}

// CheckRateLimit checks if an IP has exceeded its rate limit
// Returns allowed (bool), retryAfter (seconds), and error
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: validate whether an IP address is within its rate limit and return retry-after seconds (reads DB)
func (r *IPRateLimiter) CheckRateLimit(ctx context.Context, ipAddress string, limit int, windowSeconds int) (bool, int, error) {
	logger := slogging.Get()

	if r.RedisClient == nil {
		logger.Warn("Redis not available, skipping IP rate limit check")
		return true, 0, nil
	}

	key := fmt.Sprintf("ip:ratelimit:%ds:%s", windowSeconds, ipAddress)
	allowed, retryAfter, err := r.CheckSlidingWindow(ctx, key, limit, windowSeconds)
	if err != nil {
		logger.Error("failed to check IP rate limit for %s: %v", ipAddress, err)
		return false, 0, fmt.Errorf("rate limit check failed: %w", err)
	}

	return allowed, retryAfter, nil
}

// GetRateLimitInfo returns current rate limit status for an IP
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: fetch remaining request quota and reset timestamp for an IP address (reads DB)
func (r *IPRateLimiter) GetRateLimitInfo(ctx context.Context, ipAddress string, limit int, windowSeconds int) (remaining int, resetAt int64, err error) {
	logger := slogging.Get()

	if r.RedisClient == nil {
		logger.Warn("Redis not available, returning default IP rate limit info")
		return limit, time.Now().Unix() + int64(windowSeconds), nil
	}

	key := fmt.Sprintf("ip:ratelimit:%ds:%s", windowSeconds, ipAddress)
	remaining, resetAt, err = r.SlidingWindowRateLimiter.GetRateLimitInfo(ctx, key, limit, windowSeconds)
	if err != nil {
		logger.Error("failed to get IP rate limit count for %s: %v", ipAddress, err)
		return limit, time.Now().Unix() + int64(windowSeconds), nil
	}

	return remaining, resetAt, nil
}
