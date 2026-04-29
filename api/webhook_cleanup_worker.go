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
func (w *WebhookCleanupWorker) performCleanup(ctx context.Context) error {
	logger := slogging.Get()

	if GlobalWebhookSubscriptionStore == nil {
		logger.Warn("webhook subscription store not available for cleanup")
		return nil
	}

	logger.Debug("starting webhook cleanup operations")

	// 0. Clean up stale in_progress deliveries in Redis
	if err := w.cleanupStaleDeliveries(ctx); err != nil {
		logger.Error("failed to cleanup stale deliveries: %v", err)
	}

	// 1. Mark idle subscriptions for deletion (no successful delivery in 90 days)
	if count, err := w.markIdleSubscriptions(ctx, 90); err != nil {
		logger.Error("failed to mark idle subscriptions: %v", err)
	} else if count > 0 {
		logger.Info("marked %d idle subscriptions for deletion", count)
	}

	// 3. Mark broken subscriptions for deletion (10+ failures, no success in 7 days)
	if count, err := w.markBrokenSubscriptions(ctx, 10, 7); err != nil {
		logger.Error("failed to mark broken subscriptions: %v", err)
	} else if count > 0 {
		logger.Info("marked %d broken subscriptions for deletion", count)
	}

	// 4. Delete subscriptions marked for deletion
	if count, err := w.deletePendingSubscriptions(ctx); err != nil {
		logger.Error("failed to delete pending subscriptions: %v", err)
	} else if count > 0 {
		logger.Info("deleted %d subscriptions marked for deletion", count)
	}

	logger.Debug("webhook cleanup operations completed")
	return nil
}

// markIdleSubscriptions marks subscriptions with no recent activity for deletion
func (w *WebhookCleanupWorker) markIdleSubscriptions(ctx context.Context, daysIdle int) (int, error) {
	logger := slogging.Get()

	subscriptions, err := GlobalWebhookSubscriptionStore.ListIdle(ctx, daysIdle)
	if err != nil {
		return 0, fmt.Errorf("failed to list idle subscriptions: %w", err)
	}

	count := 0
	for _, sub := range subscriptions {
		// Only mark active subscriptions (don't re-mark already pending_delete)
		if sub.Status == string(WebhookSubscriptionStatusActive) {
			logger.Debug("marking idle subscription %s for deletion (last use: %v)", sub.Id, sub.LastSuccessfulUse)
			if err := GlobalWebhookSubscriptionStore.UpdateStatus(ctx, sub.Id.String(), "pending_delete"); err != nil {
				logger.Error("failed to mark subscription %s for deletion: %v", sub.Id, err)
				continue
			}
			count++
		}
	}

	return count, nil
}

// markBrokenSubscriptions marks subscriptions with too many failures for deletion
func (w *WebhookCleanupWorker) markBrokenSubscriptions(ctx context.Context, minFailures, daysSinceSuccess int) (int, error) {
	logger := slogging.Get()

	subscriptions, err := GlobalWebhookSubscriptionStore.ListBroken(ctx, minFailures, daysSinceSuccess)
	if err != nil {
		return 0, fmt.Errorf("failed to list broken subscriptions: %w", err)
	}

	count := 0
	for _, sub := range subscriptions {
		// Only mark active subscriptions (don't re-mark already pending_delete)
		if sub.Status == string(WebhookSubscriptionStatusActive) {
			logger.Debug("marking broken subscription %s for deletion (failures: %d, last success: %v)",
				sub.Id, sub.PublicationFailures, sub.LastSuccessfulUse)
			if err := GlobalWebhookSubscriptionStore.UpdateStatus(ctx, sub.Id.String(), "pending_delete"); err != nil {
				logger.Error("failed to mark subscription %s for deletion: %v", sub.Id, err)
				continue
			}
			count++
		}
	}

	return count, nil
}

// cleanupStaleDeliveries finds in_progress Redis delivery records with no activity
// for longer than DeliveryStaleTimeout and marks them as failed.
func (w *WebhookCleanupWorker) cleanupStaleDeliveries(ctx context.Context) error {
	logger := slogging.Get()

	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Debug("webhook delivery Redis store not available, skipping stale delivery cleanup")
		return nil
	}

	staleRecords, err := GlobalWebhookDeliveryRedisStore.ListStale(ctx, DeliveryStaleTimeout)
	if err != nil {
		return fmt.Errorf("failed to list stale deliveries: %w", err)
	}

	if len(staleRecords) == 0 {
		return nil
	}

	logger.Info("found %d stale in_progress deliveries to clean up", len(staleRecords))

	for _, record := range staleRecords {
		now := time.Now().UTC()
		if err := GlobalWebhookDeliveryRedisStore.UpdateStatus(ctx, record.ID, DeliveryStatusFailed, &now); err != nil {
			logger.Error("failed to mark stale delivery %s as failed: %v", record.ID, err)
			continue
		}

		logger.Info("marked stale delivery %s as failed (last activity: %s)", record.ID, record.LastActivityAt.Format(time.RFC3339))

		// If this delivery is for an addon, update subscription failure stats
		if record.AddonID != nil && GlobalAddonStore != nil {
			addon, addonErr := GlobalAddonStore.Get(ctx, *record.AddonID)
			if addonErr != nil {
				logger.Error("failed to look up addon %s for stale delivery %s: %v", record.AddonID, record.ID, addonErr)
				continue
			}
			if err := GlobalWebhookSubscriptionStore.UpdatePublicationStats(ctx, addon.WebhookID.String(), false); err != nil {
				logger.Error("failed to update subscription stats for addon %s webhook %s: %v", addon.ID, addon.WebhookID, err)
			}
		}
	}

	return nil
}

// deletePendingSubscriptions deletes subscriptions marked for deletion,
// including associated deliveries and addons that have foreign key constraints.
func (w *WebhookCleanupWorker) deletePendingSubscriptions(ctx context.Context) (int, error) {
	logger := slogging.Get()

	subscriptions, err := GlobalWebhookSubscriptionStore.ListPendingDelete(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list pending delete subscriptions: %w", err)
	}

	count := 0
	for _, sub := range subscriptions {
		logger.Debug("deleting subscription %s (status: %s)", sub.Id, sub.Status)

		// Delete associated addons (foreign key constraint)
		if GlobalAddonStore != nil {
			if delCount, delErr := GlobalAddonStore.DeleteByWebhookID(ctx, sub.Id); delErr != nil {
				logger.Error("failed to delete addons for subscription %s: %v", sub.Id, delErr)
				continue
			} else if delCount > 0 {
				logger.Info("cascade deleted %d addons for subscription %s", delCount, sub.Id)
			}
		}

		if err := GlobalWebhookSubscriptionStore.Delete(ctx, sub.Id.String()); err != nil {
			logger.Error("failed to delete subscription %s: %v", sub.Id, err)
			continue
		}
		count++
	}

	return count, nil
}
