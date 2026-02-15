package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// WebhookDeliveryWorker handles delivery of webhook events to subscribed endpoints
type WebhookDeliveryWorker struct {
	baseWorker
	httpClient *http.Client
}

// NewWebhookDeliveryWorker creates a new delivery worker
func NewWebhookDeliveryWorker() *WebhookDeliveryWorker {
	w := &WebhookDeliveryWorker{
		httpClient: webhookHTTPClient(30 * time.Second),
	}
	w.baseWorker = newBaseWorker("webhook delivery worker", 2*time.Second, false, w.processPendingDeliveries)
	return w
}

// processPendingDeliveries processes all pending deliveries
func (w *WebhookDeliveryWorker) processPendingDeliveries(ctx context.Context) error {
	logger := slogging.Get()

	if GlobalWebhookDeliveryStore == nil || GlobalWebhookSubscriptionStore == nil {
		logger.Warn("webhook stores not available")
		return nil
	}

	// Get pending deliveries (limit to 100 per batch)
	deliveries, err := GlobalWebhookDeliveryStore.ListPending(100)
	if err != nil {
		return fmt.Errorf("failed to list pending deliveries: %w", err)
	}

	// Also get deliveries ready for retry
	retryDeliveries, err := GlobalWebhookDeliveryStore.ListReadyForRetry()
	if err != nil {
		logger.Error("failed to list retry deliveries: %v", err)
	} else {
		deliveries = append(deliveries, retryDeliveries...)
	}

	if len(deliveries) == 0 {
		return nil
	}

	logger.Debug("processing %d pending deliveries", len(deliveries))

	for _, delivery := range deliveries {
		if err := w.deliverWebhook(ctx, delivery); err != nil {
			logger.Error("failed to deliver webhook %s: %v", delivery.Id, err)
			// Continue with other deliveries
		}
	}

	return nil
}

// deliverWebhook attempts to deliver a webhook to its endpoint
func (w *WebhookDeliveryWorker) deliverWebhook(ctx context.Context, delivery DBWebhookDelivery) error {
	logger := slogging.Get()

	// Get subscription details
	subscription, err := GlobalWebhookSubscriptionStore.Get(delivery.SubscriptionId.String())
	if err != nil {
		logger.Error("failed to get subscription %s: %v", delivery.SubscriptionId, err)
		// Mark delivery as failed
		now := time.Now().UTC()
		_ = GlobalWebhookDeliveryStore.UpdateStatus(delivery.Id.String(), "failed", &now)
		return err
	}

	// Check if subscription is active
	if subscription.Status != "active" {
		logger.Warn("subscription %s is not active (status: %s), skipping delivery", subscription.Id, subscription.Status)
		// Mark delivery as failed
		now := time.Now().UTC()
		_ = GlobalWebhookDeliveryStore.UpdateStatus(delivery.Id.String(), "failed", &now)
		return nil
	}

	logger.Debug("delivering webhook to %s (attempt %d)", subscription.Url, delivery.Attempts+1)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", subscription.Url, bytes.NewReader([]byte(delivery.Payload)))
	if err != nil {
		return w.handleDeliveryFailure(delivery, fmt.Sprintf("failed to create request: %v", err))
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", delivery.EventType)
	req.Header.Set("X-Webhook-Delivery-Id", delivery.Id.String())
	req.Header.Set("X-Webhook-Subscription-Id", subscription.Id.String())
	req.Header.Set("User-Agent", "TMI-Webhook/1.0")

	// Add HMAC signature if secret is configured
	if subscription.Secret != "" {
		signature := w.generateSignature([]byte(delivery.Payload), subscription.Secret)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// Send request
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return w.handleDeliveryFailure(delivery, fmt.Sprintf("request failed: %v", err))
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response (limit to 10KB for logging)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024))

	// Check response status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		logger.Info("webhook delivered successfully to %s (delivery: %s, status: %d)",
			subscription.Url, delivery.Id, resp.StatusCode)

		// Mark as delivered
		now := time.Now().UTC()
		if err := GlobalWebhookDeliveryStore.UpdateStatus(delivery.Id.String(), "delivered", &now); err != nil {
			logger.Error("failed to update delivery status: %v", err)
		}

		// Update subscription stats (success)
		if err := GlobalWebhookSubscriptionStore.UpdatePublicationStats(subscription.Id.String(), true); err != nil {
			logger.Error("failed to update subscription stats: %v", err)
		}

		return nil
	}

	// Delivery failed
	errorMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
	return w.handleDeliveryFailure(delivery, errorMsg)
}

// handleDeliveryFailure handles a failed delivery attempt
func (w *WebhookDeliveryWorker) handleDeliveryFailure(delivery DBWebhookDelivery, errorMsg string) error {
	logger := slogging.Get()

	const maxAttempts = 5
	newAttempts := delivery.Attempts + 1

	logger.Warn("delivery %s failed (attempt %d/%d): %s", delivery.Id, newAttempts, maxAttempts, errorMsg)

	if newAttempts >= maxAttempts {
		// Max attempts reached, mark as failed
		now := time.Now().UTC()
		if err := GlobalWebhookDeliveryStore.UpdateStatus(delivery.Id.String(), "failed", &now); err != nil {
			logger.Error("failed to update delivery status: %v", err)
		}

		// Update subscription stats (failure)
		if err := GlobalWebhookSubscriptionStore.UpdatePublicationStats(delivery.SubscriptionId.String(), false); err != nil {
			logger.Error("failed to update subscription stats: %v", err)
		}

		logger.Error("delivery %s permanently failed after %d attempts", delivery.Id, maxAttempts)
		return fmt.Errorf("max attempts reached: %s", errorMsg)
	}

	// Calculate exponential backoff: 1min, 5min, 15min, 30min
	backoffMinutes := []int{1, 5, 15, 30}
	backoffIndex := newAttempts - 1
	if backoffIndex >= len(backoffMinutes) {
		backoffIndex = len(backoffMinutes) - 1
	}
	nextRetry := time.Now().UTC().Add(time.Duration(backoffMinutes[backoffIndex]) * time.Minute)

	// Update retry information
	if err := GlobalWebhookDeliveryStore.UpdateRetry(delivery.Id.String(), newAttempts, &nextRetry, errorMsg); err != nil {
		logger.Error("failed to update retry info: %v", err)
	}

	logger.Debug("delivery %s scheduled for retry at %s", delivery.Id, nextRetry.Format(time.RFC3339))
	return fmt.Errorf("delivery failed, will retry: %s", errorMsg)
}

// generateSignature generates HMAC-SHA256 signature for the payload
func (w *WebhookDeliveryWorker) generateSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
