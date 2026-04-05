package api

import (
	"sync"
	"sync/atomic"
	"time"
)

// TimmyRateLimiter manages rate limits for Timmy operations
type TimmyRateLimiter struct {
	maxMessagesPerUserPerHour int
	maxSessionsPerTM          int
	maxConcurrentLLM          int

	mu                sync.Mutex
	userMessageCounts map[string]*slidingWindow

	concurrentLLM atomic.Int32
}

// slidingWindow tracks events in a 1-hour window
type slidingWindow struct {
	timestamps []time.Time
}

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

func (sw *slidingWindow) add() {
	sw.timestamps = append(sw.timestamps, time.Now())
}

// NewTimmyRateLimiter creates a rate limiter with the given limits
func NewTimmyRateLimiter(maxMessagesPerUserPerHour, maxSessionsPerTM, maxConcurrentLLM int) *TimmyRateLimiter {
	return &TimmyRateLimiter{
		maxMessagesPerUserPerHour: maxMessagesPerUserPerHour,
		maxSessionsPerTM:          maxSessionsPerTM,
		maxConcurrentLLM:          maxConcurrentLLM,
		userMessageCounts:         make(map[string]*slidingWindow),
	}
}

// AllowMessage checks if a user is within their hourly message limit
func (rl *TimmyRateLimiter) AllowMessage(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	sw, ok := rl.userMessageCounts[userID]
	if !ok {
		sw = &slidingWindow{}
		rl.userMessageCounts[userID] = sw
	}

	if sw.count(time.Hour) >= rl.maxMessagesPerUserPerHour {
		return false
	}
	sw.add()
	return true
}

// AcquireLLMSlot tries to acquire a concurrent LLM request slot
func (rl *TimmyRateLimiter) AcquireLLMSlot() bool {
	for {
		current := rl.concurrentLLM.Load()
		if int(current) >= rl.maxConcurrentLLM {
			return false
		}
		if rl.concurrentLLM.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

// ReleaseLLMSlot releases a concurrent LLM request slot
func (rl *TimmyRateLimiter) ReleaseLLMSlot() {
	rl.concurrentLLM.Add(-1)
}
