package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// AddonInvocationCleanupWorker handles cleanup of stale addon invocations
type AddonInvocationCleanupWorker struct {
	running  bool
	stopChan chan struct{}
}

// NewAddonInvocationCleanupWorker creates a new cleanup worker
func NewAddonInvocationCleanupWorker() *AddonInvocationCleanupWorker {
	return &AddonInvocationCleanupWorker{
		stopChan: make(chan struct{}),
	}
}

// Start begins cleanup operations
func (w *AddonInvocationCleanupWorker) Start(ctx context.Context) error {
	logger := slogging.Get()

	w.running = true
	logger.Info("addon invocation cleanup worker started")

	// Start processing in a goroutine
	go w.processLoop(ctx)

	return nil
}

// Stop gracefully stops the worker
func (w *AddonInvocationCleanupWorker) Stop() {
	logger := slogging.Get()
	if w.running {
		w.running = false
		close(w.stopChan)
		logger.Info("addon invocation cleanup worker stopped")
	}
}

// processLoop continuously performs cleanup operations
func (w *AddonInvocationCleanupWorker) processLoop(ctx context.Context) {
	logger := slogging.Get()
	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	// Run cleanup immediately on start
	if err := w.performCleanup(ctx); err != nil {
		logger.Error("initial addon invocation cleanup failed: %v", err)
	}

	for w.running {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping addon invocation cleanup worker")
			return
		case <-w.stopChan:
			logger.Info("stop signal received, stopping addon invocation cleanup worker")
			return
		case <-ticker.C:
			if err := w.performCleanup(ctx); err != nil {
				logger.Error("addon invocation cleanup failed: %v", err)
			}
		}
	}
}

// performCleanup performs all cleanup operations
func (w *AddonInvocationCleanupWorker) performCleanup(ctx context.Context) error {
	logger := slogging.Get()

	if GlobalAddonInvocationStore == nil {
		logger.Warn("addon invocation store not available for cleanup")
		return nil
	}

	logger.Debug("starting addon invocation cleanup operations")

	// Find stale invocations (no activity for 15 minutes)
	staleInvocations, err := GlobalAddonInvocationStore.ListStale(ctx, AddonInvocationTimeout)
	if err != nil {
		return fmt.Errorf("failed to list stale invocations: %w", err)
	}

	if len(staleInvocations) == 0 {
		logger.Debug("no stale invocations found")
		return nil
	}

	logger.Info("found %d stale invocations to timeout", len(staleInvocations))

	// Mark each stale invocation as failed
	for _, invocation := range staleInvocations {
		if err := w.timeoutInvocation(ctx, &invocation); err != nil {
			logger.Error("failed to timeout invocation %s: %v", invocation.ID, err)
			// Continue with other invocations
		}
	}

	logger.Debug("addon invocation cleanup operations completed")
	return nil
}

// timeoutInvocation marks an invocation as failed due to timeout
func (w *AddonInvocationCleanupWorker) timeoutInvocation(ctx context.Context, invocation *AddonInvocation) error {
	logger := slogging.Get()

	// Get addon details to find the webhook
	addon, err := GlobalAddonStore.Get(ctx, invocation.AddonID)
	if err != nil {
		logger.Error("failed to get addon %s for timeout: %v", invocation.AddonID, err)
		// Continue with timeout anyway
	}

	logger.Warn("timing out stale invocation: invocation_id=%s, addon_id=%s, user=%s, last_activity=%s, status=%s",
		invocation.ID,
		invocation.AddonID,
		invocation.InvokedByID,
		invocation.LastActivityAt.Format(time.RFC3339),
		invocation.Status)

	// Update invocation status to failed
	invocation.Status = InvocationStatusFailed
	invocation.StatusMessage = fmt.Sprintf("Invocation timed out after %v of inactivity", AddonInvocationTimeout)

	if err := GlobalAddonInvocationStore.Update(ctx, invocation); err != nil {
		return fmt.Errorf("failed to update invocation status: %w", err)
	}

	// Increment timeout count on webhook subscription if we have addon details
	if addon != nil && GlobalWebhookSubscriptionStore != nil {
		if err := GlobalWebhookSubscriptionStore.IncrementTimeouts(addon.WebhookID.String()); err != nil {
			logger.Error("failed to increment timeout count for webhook %s: %v", addon.WebhookID, err)
			// Don't fail the timeout operation for this
		}
	}

	logger.Info("invocation timed out: invocation_id=%s, addon_id=%s", invocation.ID, invocation.AddonID)

	return nil
}

// GlobalAddonInvocationCleanupWorker is the global singleton for the cleanup worker
var GlobalAddonInvocationCleanupWorker *AddonInvocationCleanupWorker
