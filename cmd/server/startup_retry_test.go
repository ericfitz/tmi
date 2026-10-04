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
	}, nil, fakeSleep(&slept))
	require.NoError(t, err)
	assert.Equal(t, 4, calls)
	assert.Equal(t, 3500*time.Millisecond, slept) // 0.5+1+2
}

func TestRetryConnect_GivesUpAfterBudget(t *testing.T) {
	want := errors.New("refused")
	calls := 0
	var slept time.Duration
	err := retryConnect(context.Background(), "x", startupRetryBudget, func() error { calls++; return want }, nil, fakeSleep(&slept))
	assert.ErrorIs(t, err, want)
	assert.LessOrEqual(t, slept, startupRetryBudget)
	assert.Greater(t, slept, startupRetryBudget-startupRetryMaxDelay)
	assert.Greater(t, calls, 3)
}

func TestRetryConnect_RespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryConnect(ctx, "x", startupRetryBudget, func() error { calls++; return errors.New("e") }, nil, func(c context.Context, _ time.Duration) error {
		cancel()
		return c.Err()
	})
	assert.Error(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetryConnect_StopsOnPermanentError(t *testing.T) {
	want := errors.New("bad password")
	calls := 0
	var slept time.Duration
	err := retryConnect(context.Background(), "x", startupRetryBudget, func() error { calls++; return want },
		func(err error) bool { return errors.Is(err, want) }, fakeSleep(&slept))
	assert.ErrorIs(t, err, want)
	assert.Equal(t, 1, calls, "a permanent error must not be retried (each retry is a login attempt)")
	assert.Zero(t, slept)
}

func TestRetryConnect_BudgetCountsTimeInsideAttempts(t *testing.T) {
	calls := 0
	var slept time.Duration
	// Each attempt blocks 20ms; with a 50ms budget and 500ms first delay, no retry fits.
	err := retryConnect(context.Background(), "x", 50*time.Millisecond, func() error {
		calls++
		time.Sleep(20 * time.Millisecond)
		return errors.New("timeout")
	}, nil, fakeSleep(&slept))
	assert.Error(t, err)
	assert.Equal(t, 1, calls)

	// Budget 600ms: attempt (20ms) + 500ms sleep fits once; second attempt (20ms) + 1s does not.
	calls, slept = 0, 0
	err = retryConnect(context.Background(), "x", 600*time.Millisecond, func() error {
		calls++
		time.Sleep(20 * time.Millisecond)
		return errors.New("timeout")
	}, nil, fakeSleep(&slept))
	assert.Error(t, err)
	assert.Equal(t, 2, calls)
}
