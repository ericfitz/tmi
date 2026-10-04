package main

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// On Oracle ADB a single attempt can exceed the whole budget: wallet tnsnames
// entries carry retry_count=20/retry_delay=3, so Oracle Net itself retries a
// stopped ADB for about a minute. Then this loop makes one attempt and stops.
const (
	startupRetryBudget   = 30 * time.Second
	startupRetryInitial  = 500 * time.Millisecond
	startupRetryMaxDelay = 5 * time.Second
)

// retryConnect retries connect with exponential backoff until it succeeds, the
// budget (wall-clock: time inside attempts plus backoff) runs out, the context
// is cancelled, or isPermanent (optional) says the error cannot be fixed by retrying.
// SEM@ab6a7ff21a80d96c9f7c490184f44a06dc5607b9: retry a connect function with bounded backoff, stopping early on non-retryable errors
func retryConnect(ctx context.Context, name string, budget time.Duration, connect func() error, isPermanent func(error) bool, sleep func(context.Context, time.Duration) error) error {
	logger := slogging.Get()
	delay := startupRetryInitial
	var elapsed time.Duration
	for attempt := 1; ; attempt++ {
		start := time.Now()
		err := connect()
		elapsed += time.Since(start)
		if err == nil {
			return nil
		}
		if isPermanent != nil && isPermanent(err) {
			logger.Warn("%s connection attempt %d failed with a non-retryable error; not retrying", name, attempt)
			return err
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

// SEM@ab6a7ff21a80d96c9f7c490184f44a06dc5607b9: wait for a duration or until the context is cancelled
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
