package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLockoutWithMiniredis(t *testing.T) (*OAuthTokenLockout, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewOAuthTokenLockout(client), mr
}

// TestOAuthTokenLockout_TierThresholds pins the exponential-backoff
// schedule from #350. If a future change loosens the schedule, this test
// fails — preventing accidental regressions of the brute-force defense.
func TestOAuthTokenLockout_TierThresholds(t *testing.T) {
	cases := []struct {
		count int64
		want  time.Duration
	}{
		{0, 0},
		{1, 0},
		{4, 0},
		{5, time.Second},
		{9, time.Second},
		{10, 30 * time.Second},
		{19, 30 * time.Second},
		{20, 5 * time.Minute},
		{49, 5 * time.Minute},
		{50, time.Hour},
		{1000, time.Hour},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, retryAfterFor(tc.count), "count=%d", tc.count)
	}
}

// TestOAuthTokenLockout_RecordFailureLocksAt5 confirms that 5 failures
// crosses the threshold into the locked state.
func TestOAuthTokenLockout_RecordFailureLocksAt5(t *testing.T) {
	l, _ := newLockoutWithMiniredis(t)
	ctx := context.Background()

	for i := 1; i <= 4; i++ {
		d, err := l.RecordFailure(ctx, "client:test")
		require.NoError(t, err)
		assert.False(t, d.Locked, "should not be locked at count=%d", i)
	}

	d, err := l.RecordFailure(ctx, "client:test")
	require.NoError(t, err)
	assert.True(t, d.Locked, "should be locked at count=5")
	assert.Equal(t, time.Second, d.RetryAfter)
}

// TestOAuthTokenLockout_AfterFiftyFailuresHardLock pins the Acceptance
// Criteria from #350: "50 failed requests for the same client_id from 50
// different IPs and asserts the 51st returns 429 with Retry-After".
//
// The lockout itself does not care about IPs (the per-client counter is
// the whole point), so we don't simulate IPs here; we simulate 50
// failures and assert the 51st check returns Locked with a 1h
// Retry-After.
func TestOAuthTokenLockout_AfterFiftyFailuresHardLock(t *testing.T) {
	l, _ := newLockoutWithMiniredis(t)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		_, err := l.RecordFailure(ctx, "client:abc")
		require.NoError(t, err)
	}

	d := l.Check(ctx, "client:abc")
	assert.True(t, d.Locked)
	assert.Equal(t, time.Hour, d.RetryAfter)
	assert.Equal(t, int64(50), d.Count)
}

// TestOAuthTokenLockout_ResetClearsCounter pins the AC: "A successful
// login resets the counter."
func TestOAuthTokenLockout_ResetClearsCounter(t *testing.T) {
	l, _ := newLockoutWithMiniredis(t)
	ctx := context.Background()

	for i := 0; i < 6; i++ {
		_, err := l.RecordFailure(ctx, "client:reset-me")
		require.NoError(t, err)
	}
	assert.True(t, l.Check(ctx, "client:reset-me").Locked)

	l.Reset(ctx, "client:reset-me")

	d := l.Check(ctx, "client:reset-me")
	assert.False(t, d.Locked, "Reset must clear the lockout")
	assert.Equal(t, int64(0), d.Count)
}

// TestOAuthTokenLockout_TTLExpiresCounter confirms the counter decays
// after a quiet period (TTL).
func TestOAuthTokenLockout_TTLExpiresCounter(t *testing.T) {
	l, mr := newLockoutWithMiniredis(t)
	ctx := context.Background()

	for i := 0; i < 6; i++ {
		_, err := l.RecordFailure(ctx, "client:ttl")
		require.NoError(t, err)
	}
	assert.True(t, l.Check(ctx, "client:ttl").Locked)

	mr.FastForward(failureTTL + time.Minute)

	d := l.Check(ctx, "client:ttl")
	assert.False(t, d.Locked, "lockout should decay after TTL")
}

// TestOAuthTokenLockout_NilClientIsNoOp ensures the lockout never panics
// or hard-locks when Redis is unavailable; failing open is the correct
// behavior during a Redis outage.
func TestOAuthTokenLockout_NilClientIsNoOp(t *testing.T) {
	l := NewOAuthTokenLockout(nil)
	ctx := context.Background()

	d := l.Check(ctx, "client:any")
	assert.False(t, d.Locked)

	d2, err := l.RecordFailure(ctx, "client:any")
	assert.NoError(t, err)
	assert.False(t, d2.Locked)

	l.Reset(ctx, "client:any") // must not panic
}

// TestOAuthTokenLockout_PerClientIsolation pins that two different
// client_ids do not share a counter. If a future refactor accidentally
// keys on something global, this test fails.
func TestOAuthTokenLockout_PerClientIsolation(t *testing.T) {
	l, _ := newLockoutWithMiniredis(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		_, err := l.RecordFailure(ctx, "client:victim")
		require.NoError(t, err)
	}
	assert.True(t, l.Check(ctx, "client:victim").Locked)
	assert.False(t, l.Check(ctx, "client:innocent").Locked)
}
