package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimmyRateLimiter_UserMessageLimit(t *testing.T) {
	rl := NewTimmyRateLimiter(3, 100, 100) // 3 messages per user per hour

	assert.True(t, rl.AllowMessage("user-1"), "first message should be allowed")
	assert.True(t, rl.AllowMessage("user-1"), "second message should be allowed")
	assert.True(t, rl.AllowMessage("user-1"), "third message should be allowed")
	assert.False(t, rl.AllowMessage("user-1"), "fourth message should be blocked")
	assert.True(t, rl.AllowMessage("user-2"), "different user should be allowed")
}

func TestTimmyRateLimiter_ConcurrentLLMLimit(t *testing.T) {
	rl := NewTimmyRateLimiter(100, 100, 2) // max 2 concurrent

	assert.True(t, rl.AcquireLLMSlot(), "first slot should be available")
	assert.True(t, rl.AcquireLLMSlot(), "second slot should be available")
	assert.False(t, rl.AcquireLLMSlot(), "third slot should be blocked")
	rl.ReleaseLLMSlot()
	assert.True(t, rl.AcquireLLMSlot(), "slot should be available after release")
}
