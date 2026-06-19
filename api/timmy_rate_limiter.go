package api

import (
	"sync"
	"sync/atomic"
	"time"
)

// TimmyRateLimiter manages rate limits for Timmy operations
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: per-user sliding-window message limiter and LLM concurrency limiter for Timmy (mutates shared state)
type TimmyRateLimiter struct {
	// limits reads the current thresholds (maxMessages, maxSessions,
	// maxConcurrent) on each check, so limit changes take effect without
	// rebuilding the limiter (preserving the in-flight sliding-window and
	// concurrency state).
	limits func() (maxMessages, maxSessions, maxConcurrent int)

	mu                sync.Mutex
	userMessageCounts map[string]*slidingWindow

	concurrentLLM atomic.Int32
}

// slidingWindow tracks events in a 1-hour window
// SEM@c47068c629ce2c25efc48aa155d3fa2ba2ab7b57: tracks event timestamps within a rolling time window (mutates shared state)
type slidingWindow struct {
	timestamps []time.Time
}

// SEM@c47068c629ce2c25efc48aa155d3fa2ba2ab7b57: return the number of events within the given window, evicting expired entries (mutates shared state)
func (sw *slidingWindow) count(window time.Duration) int {
	cutoff := time.Now().Add(-window)
	// Remove expired entries
	valid := 0
	for _, ts := range sw.timestamps {
		if ts.After(cutoff) {
			sw.timestamps[valid] = ts
			valid++
		}
	}
	sw.timestamps = sw.timestamps[:valid]
	return valid
}

// SEM@c47068c629ce2c25efc48aa155d3fa2ba2ab7b57: record a new event timestamp in the sliding window (mutates shared state)
func (sw *slidingWindow) add() {
	sw.timestamps = append(sw.timestamps, time.Now())
}

// NewTimmyRateLimiter creates a rate limiter whose thresholds are read live
// via limitsFn on each check, so limit changes take effect without rebuilding
// the limiter (preserving the in-flight sliding-window + concurrency state).
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: build a Timmy rate limiter with live-readable thresholds and fresh sliding-window state (pure)
func NewTimmyRateLimiter(limitsFn func() (maxMessages, maxSessions, maxConcurrent int)) *TimmyRateLimiter {
	return &TimmyRateLimiter{
		limits:            limitsFn,
		userMessageCounts: make(map[string]*slidingWindow),
	}
}

// AllowMessage checks if a user is within their hourly message limit.
//
// limits() is read before taking rl.mu so the (potentially I/O-backed) live
// config lookup never extends the critical section guarding the per-user
// sliding-window state.
// SEM@67b94a899a1542320dc1780972f8e4c60ff217c5: validate that a user is within their hourly message quota and record the attempt (mutates shared state)
func (rl *TimmyRateLimiter) AllowMessage(userID string) bool {
	maxMessages, _, _ := rl.limits()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	sw, ok := rl.userMessageCounts[userID]
	if !ok {
		sw = &slidingWindow{}
		rl.userMessageCounts[userID] = sw
	}

	if sw.count(time.Hour) >= maxMessages {
		return false
	}
	sw.add()
	return true
}

// AcquireLLMSlot tries to acquire a concurrent LLM request slot
// SEM@63d2546d6591e57d65783c3032d4412409c2b328: atomically claim one concurrent LLM request slot, returning false if the cap is reached (mutates shared state)
func (rl *TimmyRateLimiter) AcquireLLMSlot() bool {
	_, _, maxConcurrent := rl.limits()
	for {
		current := rl.concurrentLLM.Load()
		if int(current) >= maxConcurrent {
			return false
		}
		if rl.concurrentLLM.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

// ReleaseLLMSlot releases a concurrent LLM request slot
// SEM@c47068c629ce2c25efc48aa155d3fa2ba2ab7b57: release a previously acquired LLM concurrency slot (mutates shared state)
func (rl *TimmyRateLimiter) ReleaseLLMSlot() {
	rl.concurrentLLM.Add(-1)
}
