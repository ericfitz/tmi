package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/crypto"
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/ericfitz/tmi/internal/slogging"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// WebhookDeliveryWorker handles delivery of webhook events to subscribed endpoints.
// All outbound requests go through SafeHTTPClient which pins the validated IP at
// dial time, defending against DNS rebinding between subscription validation and
// per-delivery dispatch.
type WebhookDeliveryWorker struct {
	baseWorker
	client *SafeHTTPClient
}

// NewWebhookDeliveryWorker creates a new delivery worker. The validator
// controls the SSRF blocklist and URL schemes used for outbound calls.
func NewWebhookDeliveryWorker(validator *URIValidator) *WebhookDeliveryWorker {
	w := &WebhookDeliveryWorker{
		client: NewSafeHTTPClient(
			validator,
			WithUserAgent("TMI-Webhook/1.0"),
			WithTransportWrapper(func(rt http.RoundTripper) http.RoundTripper {
				return otelhttp.NewTransport(rt)
			}),
			WithDefaultTimeouts(30*time.Second, 5*time.Second, 1*1024*1024),
		),
	}
	w.baseWorker = newBaseWorker("webhook delivery worker", 2*time.Second, false, w.processPendingDeliveries)
	return w
}

// processPendingDeliveries processes all pending deliveries
func (w *WebhookDeliveryWorker) processPendingDeliveries(ctx context.Context) error {
	logger := slogging.Get()

	if GlobalWebhookDeliveryRedisStore == nil || GlobalWebhookSubscriptionStore == nil {
		logger.Warn("webhook stores not available")
		return nil
	}

	// Get pending deliveries (limit to 100 per batch)
	deliveries, err := GlobalWebhookDeliveryRedisStore.ListPending(ctx, 100)
	if err != nil {
		return fmt.Errorf("failed to list pending deliveries: %w", err)
	}

	// Also get deliveries ready for retry
	retryDeliveries, err := GlobalWebhookDeliveryRedisStore.ListReadyForRetry(ctx)
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
			logger.Error("failed to deliver webhook %s: %v", delivery.ID, err)
			// Continue with other deliveries
		}
	}

	return nil
}

// deliverWebhook attempts to deliver a webhook to its endpoint
func (w *WebhookDeliveryWorker) deliverWebhook(ctx context.Context, delivery WebhookDeliveryRecord) error {
	logger := slogging.Get()

	// Get subscription details
	subscription, err := GlobalWebhookSubscriptionStore.Get(ctx, delivery.SubscriptionID.String())
	if err != nil {
		logger.Error("failed to get subscription %s: %v", delivery.SubscriptionID, err)
		now := time.Now().UTC()
		_ = GlobalWebhookDeliveryRedisStore.UpdateStatus(ctx, delivery.ID, DeliveryStatusFailed, &now)
		return err
	}

	// Check if subscription is active
	if subscription.Status != string(WebhookSubscriptionStatusActive) {
		logger.Warn("subscription %s is not active (status: %s), skipping delivery", subscription.Id, subscription.Status)
		now := time.Now().UTC()
		_ = GlobalWebhookDeliveryRedisStore.UpdateStatus(ctx, delivery.ID, DeliveryStatusFailed, &now)
		return nil
	}

	logger.Debug("delivering webhook to %s (attempt %d)", subscription.Url, delivery.Attempts+1)

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Webhook-Event", delivery.EventType)
	headers.Set("X-Webhook-Delivery-Id", delivery.ID.String())
	headers.Set("X-Webhook-Subscription-Id", subscription.Id.String())

	if subscription.Secret != "" {
		signature := crypto.GenerateHMACSignature([]byte(delivery.Payload), subscription.Secret)
		headers.Set("X-Webhook-Signature", signature)
	}

	result, err := w.client.Fetch(ctx, subscription.Url, SafeFetchOptions{
		Method:                http.MethodPost,
		Body:                  bytes.NewReader([]byte(delivery.Payload)),
		Headers:               headers,
		Timeout:               30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		MaxBodyBytes:          10 * 1024,
	})
	if err != nil {
		return w.handleDeliveryFailure(ctx, delivery, fmt.Sprintf("request failed: %v", err))
	}

	if result.StatusCode >= 200 && result.StatusCode < 300 {
		logger.Info("webhook delivered successfully to %s (delivery: %s, status: %d)",
			subscription.Url, delivery.ID, result.StatusCode)
		if m := tmiotel.GlobalMetrics; m != nil {
			m.WebhookDeliveries.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "success")))
		}

		now := time.Now().UTC()
		callbackMode := result.Header.Get("X-TMI-Callback")
		if callbackMode == "async" {
			if err := GlobalWebhookDeliveryRedisStore.UpdateStatus(ctx, delivery.ID, DeliveryStatusInProgress, nil); err != nil {
				logger.Error("failed to update delivery status to in_progress: %v", err)
			}
		} else {
			if err := GlobalWebhookDeliveryRedisStore.UpdateStatus(ctx, delivery.ID, DeliveryStatusDelivered, &now); err != nil {
				logger.Error("failed to update delivery status: %v", err)
			}
		}

		if err := GlobalWebhookSubscriptionStore.UpdatePublicationStats(ctx, subscription.Id.String(), true); err != nil {
			logger.Error("failed to update subscription stats: %v", err)
		}

		return nil
	}

	errorMsg := fmt.Sprintf("HTTP %d: %s", result.StatusCode, string(result.Body))
	return w.handleDeliveryFailure(ctx, delivery, errorMsg)
}

// handleDeliveryFailure handles a failed delivery attempt
func (w *WebhookDeliveryWorker) handleDeliveryFailure(ctx context.Context, delivery WebhookDeliveryRecord, errorMsg string) error {
	logger := slogging.Get()

	const maxAttempts = 5
	newAttempts := delivery.Attempts + 1

	logger.Warn("delivery %s failed (attempt %d/%d): %s", delivery.ID, newAttempts, maxAttempts, errorMsg)

	if newAttempts >= maxAttempts {
		now := time.Now().UTC()
		if err := GlobalWebhookDeliveryRedisStore.UpdateStatus(ctx, delivery.ID, DeliveryStatusFailed, &now); err != nil {
			logger.Error("failed to update delivery status: %v", err)
		}

		if err := GlobalWebhookSubscriptionStore.UpdatePublicationStats(ctx, delivery.SubscriptionID.String(), false); err != nil {
			logger.Error("failed to update subscription stats: %v", err)
		}

		logger.Error("delivery %s permanently failed after %d attempts", delivery.ID, maxAttempts)
		if m := tmiotel.GlobalMetrics; m != nil {
			m.WebhookDeliveries.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "failure")))
		}
		return fmt.Errorf("max attempts reached: %s", errorMsg)
	}

	backoffMinutes := []int{1, 5, 15, 30}
	backoffIndex := newAttempts - 1
	if backoffIndex >= len(backoffMinutes) {
		backoffIndex = len(backoffMinutes) - 1
	}
	nextRetry := time.Now().UTC().Add(time.Duration(backoffMinutes[backoffIndex]) * time.Minute)

	if err := GlobalWebhookDeliveryRedisStore.UpdateRetry(ctx, delivery.ID, newAttempts, &nextRetry, errorMsg); err != nil {
		logger.Error("failed to update retry info: %v", err)
	}

	logger.Debug("delivery %s scheduled for retry at %s", delivery.ID, nextRetry.Format(time.RFC3339))
	return fmt.Errorf("delivery failed, will retry: %s", errorMsg)
}
