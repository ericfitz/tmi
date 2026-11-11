package slogging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// cloudWatchdog polls a configured CloudLogWriter at a fixed interval and
// surfaces (a) health-check transitions and (b) bursts of consecutive write
// failures as Warn/Info entries via the provided slogger.
//
// The watchdog's own state (lastHealthy, lastObservedErrorCount,
// accumulatedFailures, failureWarnEmitted) is goroutine-local in run(), so
// no internal locking is required. Cross-goroutine reads of the cloud
// handler's counters use ErrorCount(), which handles its own synchronization.
type cloudWatchdog struct {
	cloudHandler   *CloudLogHandler
	cloudWriter    CloudLogWriter
	slogger        *slog.Logger
	pollInterval   time.Duration
	errorThreshold int
	done           chan struct{}
	once           sync.Once
}

// newCloudWatchdog constructs the watchdog and starts its goroutine. The
// caller MUST call Stop. If errorThreshold <= 0, the failure-rate alarm is
// disabled but health-check transitions are still observed.
func newCloudWatchdog(
	cloudHandler *CloudLogHandler,
	cloudWriter CloudLogWriter,
	slogger *slog.Logger,
	pollInterval time.Duration,
	errorThreshold int,
) *cloudWatchdog {
	w := &cloudWatchdog{
		cloudHandler:   cloudHandler,
		cloudWriter:    cloudWriter,
		slogger:        slogger,
		pollInterval:   pollInterval,
		errorThreshold: errorThreshold,
		done:           make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *cloudWatchdog) run() {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	// Seed with current observed state.
	lastHealthy := w.safeIsHealthy()
	var lastObservedErrorCount int64
	var accumulatedFailures int
	var failureWarnEmitted bool

	for {
		select {
		case <-ticker.C:
			// Health-check transition.
			healthy := w.safeIsHealthy()
			if healthy != lastHealthy {
				if healthy {
					w.slogger.Info("cloud log sink healthy", "provider", w.cloudWriter.Name())
				} else {
					w.slogger.Warn("cloud log sink unhealthy", "provider", w.cloudWriter.Name())
				}
				lastHealthy = healthy
			}

			// Error-rate alarm.
			if w.errorThreshold > 0 {
				current := w.cloudHandler.ErrorCount()
				delta := current - lastObservedErrorCount
				if delta > 0 {
					accumulatedFailures += int(delta)
					if accumulatedFailures >= w.errorThreshold && !failureWarnEmitted {
						w.slogger.Warn("cloud log sink failing writes",
							"provider", w.cloudWriter.Name(),
							"threshold", w.errorThreshold,
							"recent_errors", accumulatedFailures)
						failureWarnEmitted = true
					}
				} else if accumulatedFailures > 0 {
					if failureWarnEmitted {
						w.slogger.Info("cloud log sink writes recovered",
							"provider", w.cloudWriter.Name())
					}
					accumulatedFailures = 0
					failureWarnEmitted = false
				}
				lastObservedErrorCount = current
			}

		case <-w.done:
			return
		}
	}
}

// safeIsHealthy calls cloudWriter.IsHealthy with a short timeout and recovers
// from panics, returning false on any failure mode.
func (w *cloudWatchdog) safeIsHealthy() (healthy bool) {
	defer func() {
		if r := recover(); r != nil {
			w.slogger.Warn("cloud log sink IsHealthy panicked",
				"provider", w.cloudWriter.Name(),
				"recover", r)
			healthy = false
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.cloudWriter.IsHealthy(ctx)
}

// Stop signals the goroutine to exit. Safe to call multiple times.
func (w *cloudWatchdog) Stop() {
	w.once.Do(func() { close(w.done) })
}
