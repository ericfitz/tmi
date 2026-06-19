package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// OAuthTokenLockout is a Redis-backed exponential-backoff lockout for the
// /oauth2/token endpoint. It counts failed grant attempts per key (typically
// the client_id) and emits a Retry-After hint that grows with the failure
// count. Closes T15 (brute-force of client_credentials) — a per-IP rate
// limiter does not catch an attacker rotating IPs against a single client.
//
// The counter is stored as a plain integer at key
// `oauth_token_failures:{key}` with a 1h TTL. A successful grant deletes
// the key; a quiet period also expires it.
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: Redis-backed exponential-backoff lockout tracker for OAuth token grant failures (mutates shared state)
type OAuthTokenLockout struct {
	client *redis.Client
	now    func() time.Time // injected for tests
}

// NewOAuthTokenLockout constructs a lockout backed by the given Redis
// client. A nil client returns a no-op lockout.
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: build a Redis-backed OAuth token lockout tracker (pure)
func NewOAuthTokenLockout(client *redis.Client) *OAuthTokenLockout {
	return &OAuthTokenLockout{client: client, now: time.Now}
}

// LockoutDecision is the result of a Check call.
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: result of a lockout check: locked status, Retry-After duration, and failure count (pure)
type LockoutDecision struct {
	Locked     bool          // true when the caller should be rejected with 429
	RetryAfter time.Duration // Retry-After hint to surface in HTTP headers
	Count      int64         // current failure count (0 if no lock)
}

// counterKey builds the Redis key for the given lockout subject.
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: build the Redis key for a lockout subject's failure counter (pure)
func counterKey(key string) string {
	return "oauth_token_failures:" + key
}

// failureTTL is how long the counter persists with no further activity.
// A quiet hour effectively resets the lockout.
const failureTTL = time.Hour

// retryAfterFor returns the Retry-After hint for the given failure count.
// Mirrors the schedule from #350: 0–4 → 0; 5 → 1s; 10 → 30s; 20 → 5min;
// 50+ → 1h (hard lock, surfaces 429 until the counter expires).
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: compute the exponential Retry-After backoff duration for a given failure count (pure)
func retryAfterFor(count int64) time.Duration {
	switch {
	case count >= 50:
		return time.Hour
	case count >= 20:
		return 5 * time.Minute
	case count >= 10:
		return 30 * time.Second
	case count >= 5:
		return time.Second
	default:
		return 0
	}
}

// Check returns the current lockout state for the given subject. Returns
// {Locked: false} if Redis is unavailable — failing open is safer than
// rejecting valid clients during a Redis outage.
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: fetch the current lockout state for a subject; fails open when Redis is unavailable (reads DB)
func (l *OAuthTokenLockout) Check(ctx context.Context, key string) LockoutDecision {
	if l == nil || l.client == nil || key == "" {
		return LockoutDecision{}
	}
	val, err := l.client.Get(ctx, counterKey(key)).Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return LockoutDecision{}
		}
		return LockoutDecision{}
	}
	wait := retryAfterFor(val)
	return LockoutDecision{
		Locked:     wait > 0,
		RetryAfter: wait,
		Count:      val,
	}
}

// RecordFailure increments the counter and (re)applies the TTL. Returns
// the post-increment count and the new lockout decision so the caller can
// surface the updated Retry-After to the client.
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: increment the failure counter and refresh its TTL; return the updated lockout decision (mutates shared state)
func (l *OAuthTokenLockout) RecordFailure(ctx context.Context, key string) (LockoutDecision, error) {
	if l == nil || l.client == nil || key == "" {
		return LockoutDecision{}, nil
	}
	rkey := counterKey(key)
	count, err := l.client.Incr(ctx, rkey).Result()
	if err != nil {
		return LockoutDecision{}, fmt.Errorf("oauth lockout incr: %w", err)
	}
	// Refresh TTL on every failure so the lockout decays only after a
	// genuine quiet period.
	if err := l.client.Expire(ctx, rkey, failureTTL).Err(); err != nil {
		return LockoutDecision{}, fmt.Errorf("oauth lockout expire: %w", err)
	}
	wait := retryAfterFor(count)
	return LockoutDecision{
		Locked:     wait > 0,
		RetryAfter: wait,
		Count:      count,
	}, nil
}

// Reset clears the failure counter for the given subject. Called on a
// successful grant.
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: delete the failure counter for a subject on successful grant (mutates shared state)
func (l *OAuthTokenLockout) Reset(ctx context.Context, key string) {
	if l == nil || l.client == nil || key == "" {
		return
	}
	_ = l.client.Del(ctx, counterKey(key)).Err()
}
