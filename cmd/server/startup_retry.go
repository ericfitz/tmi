package main

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	startupRetryBudget   = 30 * time.Second
	startupRetryInitial  = 500 * time.Millisecond
	startupRetryMaxDelay = 5 * time.Second
)

// SEM@0000000: retry a connect function with bounded exponential backoff until success, budget, or cancel
func retryConnect(ctx context.Context, name string, budget time.Duration, connect func() error, sleep func(context.Context, time.Duration) error) error {
	logger := slogging.Get()
	delay := startupRetryInitial
	var elapsed time.Duration
	for attempt := 1; ; attempt++ {
		err := connect()
		if err == nil {
			return nil
		}
		if elapsed+delay > budget {
			return err
		}
		// Fixed message: driver error text is deliberately not logged (see #1005).
		logger.Warn("%s connection attempt %d failed; retrying in %s", name, attempt, delay)
		if serr := sleep(ctx, delay); serr != nil {
			return err
		}
		elapsed += delay
		delay = min(delay*2, startupRetryMaxDelay)
	}
}

// SEM@0000000: sleep for a duration or until the context is cancelled
func ctxSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
