package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestCircuitBreaker_OpensAfterThresholdFailures pins that the breaker
// stays closed below the failure threshold and opens at it.
func TestCircuitBreaker_OpensAfterThresholdFailures(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	b := newWebhookCircuitBreaker(3, []time.Duration{1 * time.Minute, 5 * time.Minute})
	b.now = func() time.Time { return now }

	target := "example.com"
	for i := 0; i < 2; i++ {
		b.recordFailure(target)
		ok, _ := b.allow(target)
		assert.True(t, ok, "circuit should remain closed after %d failure(s)", i+1)
	}
	b.recordFailure(target)
	ok, openUntil := b.allow(target)
	assert.False(t, ok, "circuit should open at threshold")
	assert.Equal(t, now.Add(1*time.Minute), openUntil)
}

// TestCircuitBreaker_ProgressiveBackoff pins that successive open
// rounds use the next entry in the schedule, capping at the last.
func TestCircuitBreaker_ProgressiveBackoff(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	b := newWebhookCircuitBreaker(2, []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute})
	b.now = func() time.Time { return now }

	target := "x.example"

	// First open: 1m.
	b.recordFailure(target)
	b.recordFailure(target)
	_, until := b.allow(target)
	assert.Equal(t, now.Add(1*time.Minute), until)

	// Move past the open window, then fail twice more → 5m.
	now = now.Add(2 * time.Minute)
	b.recordFailure(target)
	b.recordFailure(target)
	_, until = b.allow(target)
	assert.Equal(t, now.Add(5*time.Minute), until)

	// Third round caps at 15m.
	now = now.Add(10 * time.Minute)
	b.recordFailure(target)
	b.recordFailure(target)
	_, until = b.allow(target)
	assert.Equal(t, now.Add(15*time.Minute), until)

	// Fourth round stays at 15m (last entry).
	now = now.Add(20 * time.Minute)
	b.recordFailure(target)
	b.recordFailure(target)
	_, until = b.allow(target)
	assert.Equal(t, now.Add(15*time.Minute), until)
}

// TestCircuitBreaker_SuccessResets pins that a successful delivery
// clears the failure history so a target that recovers does not stay
// half-broken in the breaker's memory.
func TestCircuitBreaker_SuccessResets(t *testing.T) {
	b := newWebhookCircuitBreaker(3, nil)
	target := "y.example"

	b.recordFailure(target)
	b.recordFailure(target)
	b.recordSuccess(target)

	// Fresh start: two more failures must NOT open (would need 3 from zero).
	b.recordFailure(target)
	b.recordFailure(target)
	ok, _ := b.allow(target)
	assert.True(t, ok, "two failures from a fresh start must not open the circuit")
}

// TestCircuitBreaker_AllowAfterOpenWindow pins that once the open
// window elapses, allow returns true without explicit close — the
// next attempt is the half-open probe.
func TestCircuitBreaker_AllowAfterOpenWindow(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	b := newWebhookCircuitBreaker(1, []time.Duration{1 * time.Minute})
	b.now = func() time.Time { return now }

	target := "z.example"
	b.recordFailure(target)

	ok, _ := b.allow(target)
	assert.False(t, ok, "circuit should be open during the window")

	now = now.Add(2 * time.Minute)
	ok, _ = b.allow(target)
	assert.True(t, ok, "circuit should allow a probe after the open window")
}

// TestTargetKey_HostnameNormalisation pins that the key used for the
// breaker is the hostname (case-insensitive) so http and https URLs
// to the same host share state.
func TestTargetKey_HostnameNormalisation(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://example.com/hook", "example.com"},
		{"http://example.com:8080/hook", "example.com"},
		{"https://Example.com/hook", "Example.com"}, // url.Parse preserves case
		{"not a url", "not a url"},
	}
	for _, tc := range cases {
		got := targetKey(tc.url)
		assert.Equal(t, tc.want, got)
	}
}
