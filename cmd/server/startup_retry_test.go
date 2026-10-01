package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeSleep(total *time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error { *total += d; return nil }
}

func TestRetryConnect_SucceedsAfterFailures(t *testing.T) {
	calls := 0
	var slept time.Duration
	err := retryConnect(context.Background(), "x", startupRetryBudget, func() error {
		calls++
		if calls < 4 {
			return errors.New("refused")
		}
		return nil
	}, fakeSleep(&slept))
	require.NoError(t, err)
	assert.Equal(t, 4, calls)
	assert.Equal(t, 3500*time.Millisecond, slept) // 0.5+1+2
}

func TestRetryConnect_GivesUpAfterBudget(t *testing.T) {
	want := errors.New("refused")
	calls := 0
	var slept time.Duration
	err := retryConnect(context.Background(), "x", startupRetryBudget, func() error { calls++; return want }, fakeSleep(&slept))
	assert.ErrorIs(t, err, want)
	assert.LessOrEqual(t, slept, startupRetryBudget)
	assert.Greater(t, slept, startupRetryBudget-startupRetryMaxDelay)
	assert.Greater(t, calls, 3)
}

func TestRetryConnect_RespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryConnect(ctx, "x", startupRetryBudget, func() error { calls++; return errors.New("e") }, func(c context.Context, _ time.Duration) error {
		cancel()
		return c.Err()
	})
	assert.Error(t, err)
	assert.Equal(t, 1, calls)
}
