package api

import (
	"net/url"
	"sync"
	"time"
)

// webhookCircuitBreaker tracks per-target failure history for webhook
// delivery. After threshold consecutive failures the circuit opens for
// an exponentially growing window; deliveries to that target are
// skipped (and rescheduled) while the circuit is open. A successful
// delivery — or the half-open probe one performed when the open window
// elapses — resets the breaker.
//
// Targets are keyed by URL host (case-insensitive). Hostless URLs are
// keyed by raw URL.
//
// Thread-safe; called from the delivery worker on every attempt.
// SEM@0aee687bf1c2b4e1819bf1c183575104459a14d4: per-target circuit breaker that blocks webhook deliveries after consecutive failures with exponential backoff (mutates shared state)
type webhookCircuitBreaker struct {
	mu        sync.Mutex
	threshold int
	backoffs  []time.Duration
	now       func() time.Time
	state     map[string]*webhookTargetState
}

// SEM@0aee687bf1c2b4e1819bf1c183575104459a14d4: tracks consecutive failure count and current open-window state for a webhook target (pure)
type webhookTargetState struct {
	consecutiveFailures int
	openWindowIdx       int
	openUntil           time.Time
}

// newWebhookCircuitBreaker constructs a breaker with the given failure
// threshold and progressive open-window backoff schedule.
// SEM@0aee687bf1c2b4e1819bf1c183575104459a14d4: build a webhook circuit breaker with a failure threshold and progressive backoff schedule (pure)
func newWebhookCircuitBreaker(threshold int, backoffs []time.Duration) *webhookCircuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if len(backoffs) == 0 {
		backoffs = []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute}
	}
	return &webhookCircuitBreaker{
		threshold: threshold,
		backoffs:  backoffs,
		now:       time.Now,
		state:     make(map[string]*webhookTargetState),
	}
}

// targetKey extracts a stable key for a webhook URL.
// SEM@0aee687bf1c2b4e1819bf1c183575104459a14d4: convert a webhook URL to a stable per-host circuit-breaker key (pure)
func targetKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Hostname()
}

// allow reports whether a delivery to target may proceed now. When the
// circuit is open, it returns the time at which the next probe is
// allowed (the second return value); the caller should reschedule the
// delivery to that time without consuming a retry attempt.
// SEM@0aee687bf1c2b4e1819bf1c183575104459a14d4: check whether delivery to a target is permitted or circuit-blocked (pure)
func (b *webhookCircuitBreaker) allow(target string) (allowed bool, openUntil time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.state[target]
	if !ok {
		return true, time.Time{}
	}
	if st.openUntil.IsZero() {
		return true, time.Time{}
	}
	if !b.now().Before(st.openUntil) {
		return true, time.Time{}
	}
	return false, st.openUntil
}

// recordSuccess clears the failure history for target.
// SEM@0aee687bf1c2b4e1819bf1c183575104459a14d4: clear failure history for a target after a successful delivery (mutates shared state)
func (b *webhookCircuitBreaker) recordSuccess(target string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, target)
}

// recordFailure increments the consecutive-failure count and opens the
// circuit if the threshold is reached. Each successive open round uses
// the next backoff in the schedule (capped at the longest entry).
// SEM@0aee687bf1c2b4e1819bf1c183575104459a14d4: increment failure count and open the circuit when threshold is reached (mutates shared state)
func (b *webhookCircuitBreaker) recordFailure(target string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.state[target]
	if !ok {
		st = &webhookTargetState{}
		b.state[target] = st
	}
	st.consecutiveFailures++
	if st.consecutiveFailures < b.threshold {
		return
	}
	idx := st.openWindowIdx
	if idx >= len(b.backoffs) {
		idx = len(b.backoffs) - 1
	}
	st.openUntil = b.now().Add(b.backoffs[idx])
	st.openWindowIdx++
	st.consecutiveFailures = 0
}
