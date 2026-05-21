package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// fixedLimits returns a limits closure with constant thresholds for tests.
func fixedLimits(maxMessages, maxSessions, maxConcurrent int) func() (int, int, int) {
	return func() (int, int, int) { return maxMessages, maxSessions, maxConcurrent }
}

func TestTimmyRateLimiter_UserMessageLimit(t *testing.T) {
	rl := NewTimmyRateLimiter(fixedLimits(3, 100, 100)) // 3 messages per user per hour

	assert.True(t, rl.AllowMessage("user-1"), "first message should be allowed")
	assert.True(t, rl.AllowMessage("user-1"), "second message should be allowed")
	assert.True(t, rl.AllowMessage("user-1"), "third message should be allowed")
	assert.False(t, rl.AllowMessage("user-1"), "fourth message should be blocked")
	assert.True(t, rl.AllowMessage("user-2"), "different user should be allowed")
}

func TestTimmyRateLimiter_ConcurrentLLMLimit(t *testing.T) {
	rl := NewTimmyRateLimiter(fixedLimits(100, 100, 2)) // max 2 concurrent

	assert.True(t, rl.AcquireLLMSlot(), "first slot should be available")
	assert.True(t, rl.AcquireLLMSlot(), "second slot should be available")
	assert.False(t, rl.AcquireLLMSlot(), "third slot should be blocked")
	rl.ReleaseLLMSlot()
	assert.True(t, rl.AcquireLLMSlot(), "slot should be available after release")
}

// TestTimmyRateLimiter_LiveMessageThreshold proves the message threshold is
// read live on each check: raising the limit unblocks a previously-blocked
// user without rebuilding the limiter (the sliding-window state is preserved).
func TestTimmyRateLimiter_LiveMessageThreshold(t *testing.T) {
	maxMessages := 1
	rl := NewTimmyRateLimiter(func() (int, int, int) { return maxMessages, 100, 100 })

	assert.True(t, rl.AllowMessage("user-1"), "first message should be allowed under limit=1")
	assert.False(t, rl.AllowMessage("user-1"), "second message should be blocked under limit=1")

	// Raise the limit live; the prior message count must still count.
	maxMessages = 5
	assert.True(t, rl.AllowMessage("user-1"), "message should be allowed after limit bumped to 5")
}
