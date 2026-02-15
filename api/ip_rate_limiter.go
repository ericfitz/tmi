package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// IPRateLimiter implements rate limiting based on IP address
type IPRateLimiter struct {
	SlidingWindowRateLimiter
}

// NewIPRateLimiter creates a new IP-based rate limiter
func NewIPRateLimiter(redisClient *redis.Client) *IPRateLimiter {
	return &IPRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: redisClient},
	}
}

// CheckRateLimit checks if an IP has exceeded its rate limit
// Returns allowed (bool), retryAfter (seconds), and error
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
