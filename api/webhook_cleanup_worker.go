package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// WebhookCleanupWorker handles cleanup of old deliveries, idle subscriptions, and broken subscriptions
type WebhookCleanupWorker struct {
	baseWorker
}

// NewWebhookCleanupWorker creates a new cleanup worker
func NewWebhookCleanupWorker() *WebhookCleanupWorker {
	w := &WebhookCleanupWorker{}
	w.baseWorker = newBaseWorker("webhook cleanup worker", 1*time.Hour, true, w.performCleanup)
	return w
}

// performCleanup performs all cleanup operations
func (w *WebhookCleanupWorker) performCleanup(_ context.Context) error {
	logger := slogging.Get()

	if GlobalWebhookDeliveryStore == nil || GlobalWebhookSubscriptionStore == nil {
		logger.Warn("webhook stores not available for cleanup")
		return nil
	}

	logger.Debug("starting webhook cleanup operations")

	// 1. Delete old delivery records (keep for 30 days)
	if count, err := w.cleanupOldDeliveries(30); err != nil {
		logger.Error("failed to cleanup old deliveries: %v", err)
	} else if count > 0 {
		logger.Info("cleaned up %d old delivery records", count)
	}

	// 2. Mark idle subscriptions for deletion (no successful delivery in 90 days)
	if count, err := w.markIdleSubscriptions(90); err != nil {
		logger.Error("failed to mark idle subscriptions: %v", err)
	} else if count > 0 {
		logger.Info("marked %d idle subscriptions for deletion", count)
	}

	// 3. Mark broken subscriptions for deletion (10+ failures, no success in 7 days)
	if count, err := w.markBrokenSubscriptions(10, 7); err != nil {
		logger.Error("failed to mark broken subscriptions: %v", err)
	} else if count > 0 {
		logger.Info("marked %d broken subscriptions for deletion", count)
	}

	// 4. Delete subscriptions marked for deletion
	if count, err := w.deletePendingSubscriptions(); err != nil {
		logger.Error("failed to delete pending subscriptions: %v", err)
	} else if count > 0 {
		logger.Info("deleted %d subscriptions marked for deletion", count)
	}

	logger.Debug("webhook cleanup operations completed")
	return nil
}

// cleanupOldDeliveries deletes delivery records older than specified days
func (w *WebhookCleanupWorker) cleanupOldDeliveries(daysOld int) (int, error) {
	count, err := GlobalWebhookDeliveryStore.DeleteOld(daysOld)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old deliveries: %w", err)
	}
	return count, nil
}

// markIdleSubscriptions marks subscriptions with no recent activity for deletion
func (w *WebhookCleanupWorker) markIdleSubscriptions(daysIdle int) (int, error) {
	logger := slogging.Get()

	subscriptions, err := GlobalWebhookSubscriptionStore.ListIdle(daysIdle)
	if err != nil {
		return 0, fmt.Errorf("failed to list idle subscriptions: %w", err)
	}

	count := 0
	for _, sub := range subscriptions {
		// Only mark active subscriptions (don't re-mark already pending_delete)
		if sub.Status == "active" {
			logger.Debug("marking idle subscription %s for deletion (last use: %v)", sub.Id, sub.LastSuccessfulUse)
			if err := GlobalWebhookSubscriptionStore.UpdateStatus(sub.Id.String(), "pending_delete"); err != nil {
				logger.Error("failed to mark subscription %s for deletion: %v", sub.Id, err)
				continue
			}
			count++
		}
	}

	return count, nil
}

// markBrokenSubscriptions marks subscriptions with too many failures for deletion
func (w *WebhookCleanupWorker) markBrokenSubscriptions(minFailures, daysSinceSuccess int) (int, error) {
	logger := slogging.Get()

	subscriptions, err := GlobalWebhookSubscriptionStore.ListBroken(minFailures, daysSinceSuccess)
	if err != nil {
		return 0, fmt.Errorf("failed to list broken subscriptions: %w", err)
	}

	count := 0
	for _, sub := range subscriptions {
		// Only mark active subscriptions (don't re-mark already pending_delete)
		if sub.Status == "active" {
			logger.Debug("marking broken subscription %s for deletion (failures: %d, last success: %v)",
				sub.Id, sub.PublicationFailures, sub.LastSuccessfulUse)
			if err := GlobalWebhookSubscriptionStore.UpdateStatus(sub.Id.String(), "pending_delete"); err != nil {
				logger.Error("failed to mark subscription %s for deletion: %v", sub.Id, err)
				continue
			}
			count++
		}
	}

	return count, nil
}

// deletePendingSubscriptions deletes subscriptions marked for deletion
func (w *WebhookCleanupWorker) deletePendingSubscriptions() (int, error) {
	logger := slogging.Get()

	subscriptions, err := GlobalWebhookSubscriptionStore.ListPendingDelete()
	if err != nil {
		return 0, fmt.Errorf("failed to list pending delete subscriptions: %w", err)
	}

	count := 0
	for _, sub := range subscriptions {
		logger.Debug("deleting subscription %s (status: %s)", sub.Id, sub.Status)
		if err := GlobalWebhookSubscriptionStore.Delete(sub.Id.String()); err != nil {
			logger.Error("failed to delete subscription %s: %v", sub.Id, err)
			continue
		}
		count++
	}

	return count, nil
}
