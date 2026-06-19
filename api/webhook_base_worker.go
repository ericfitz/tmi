package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// baseWorker provides shared lifecycle management for ticker-based background workers.
// It handles Start/Stop/processLoop and fixes the data race on the running field
// by using atomic.Bool instead of a bare bool.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: shared lifecycle state for ticker-based background workers with atomic run flag (pure)
type baseWorker struct {
	name       string
	running    atomic.Bool
	stopChan   chan struct{}
	interval   time.Duration
	runOnStart bool                            // If true, run work function immediately on start
	work       func(ctx context.Context) error // The domain-specific work function
}

// newBaseWorker creates a new base worker with the given configuration.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: build a baseWorker with the given name, interval, run-on-start flag, and work function (pure)
func newBaseWorker(name string, interval time.Duration, runOnStart bool, work func(ctx context.Context) error) baseWorker {
	return baseWorker{
		name:       name,
		stopChan:   make(chan struct{}),
		interval:   interval,
		runOnStart: runOnStart,
		work:       work,
	}
}

// Start begins the worker's processing loop.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: start the background worker processing loop in a goroutine (mutates shared state)
func (bw *baseWorker) Start(ctx context.Context) error {
	logger := slogging.Get()

	bw.running.Store(true)
	logger.Info("%s started", bw.name)

	go bw.processLoop(ctx)

	return nil
}

// Stop gracefully stops the worker.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: gracefully stop the background worker by signaling the stop channel (mutates shared state)
func (bw *baseWorker) Stop() {
	logger := slogging.Get()
	if bw.running.CompareAndSwap(true, false) {
		close(bw.stopChan)
		logger.Info("%s stopped", bw.name)
	}
}

// processLoop continuously runs the work function on a ticker.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: dispatch the worker's task on a ticker, stopping on context cancellation or stop signal (mutates shared state)
func (bw *baseWorker) processLoop(ctx context.Context) {
	logger := slogging.Get()
	ticker := time.NewTicker(bw.interval)
	defer ticker.Stop()

	// Optionally run immediately on start
	if bw.runOnStart {
		if err := bw.work(ctx); err != nil {
			logger.Error("%s initial run failed: %v", bw.name, err)
		}
	}

	for bw.running.Load() {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping %s", bw.name)
			return
		case <-bw.stopChan:
			logger.Info("stop signal received, stopping %s", bw.name)
			return
		case <-ticker.C:
			if err := bw.work(ctx); err != nil {
				logger.Error("%s error: %v", bw.name, err)
			}
		}
	}
}

// generateRandomHex generates n random bytes and returns them as a hex string.
// Falls back to a timestamp-based value on error.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: generate a cryptographically random hex string of n bytes (pure)
func generateRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
