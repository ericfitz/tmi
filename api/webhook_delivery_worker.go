package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/crypto"
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DelegationTokenHeader is the HTTP header that carries the per-attempt
// scoped delegation JWT for addon.invoked deliveries (T18, #358). Addons
// MUST use this token (not their own service-account credentials) when
// performing write-backs attributed to the invocation. The token's TTL
// matches the addon-invocation budget; if it expires, the addon's
// write-back is rejected and the invocation must be re-attempted.
// #nosec G101 — header name only; the token value comes from the
// delegation-token issuer at delivery time.
const DelegationTokenHeader = "X-TMI-Delegation-Token"

// WebhookDeliveryWorker handles delivery of webhook events to subscribed endpoints.
// All outbound requests go through SafeHTTPClient which pins the validated IP at
// dial time, defending against DNS rebinding between subscription validation and
// per-delivery dispatch.
type WebhookDeliveryWorker struct {
	baseWorker
	client  *SafeHTTPClient
	breaker *webhookCircuitBreaker
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
		breaker: newWebhookCircuitBreaker(5, nil),
	}
	w.baseWorker = newBaseWorker("webhook delivery worker", 2*time.Second, false, w.processPendingDeliveries)
	return w
}

// hardResponseBodyCap is the absolute ceiling we will read from a
// webhook response. The 10 KiB MaxBodyBytes used at the call site is
// the truncation cap for logging; this is the cap above which a
// declared Content-Length triggers fast-fail before the body is read.
const hardResponseBodyCap = 1 * 1024 * 1024

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

	target := targetKey(subscription.Url)
	if allowed, openUntil := w.breaker.allow(target); !allowed {
		logger.Warn("circuit open for %s — deferring delivery %s until %s",
			target, delivery.ID, openUntil.Format(time.RFC3339))
		if m := tmiotel.GlobalMetrics; m != nil {
			m.WebhookCircuitOpen.Add(ctx, 1, metric.WithAttributes(attribute.String("target", target)))
		}
		// Reschedule without consuming a retry attempt.
		retryAt := openUntil
		if err := GlobalWebhookDeliveryRedisStore.UpdateRetry(ctx, delivery.ID, delivery.Attempts, &retryAt, "circuit open"); err != nil {
			logger.Error("failed to update retry info: %v", err)
		}
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

	// For addon.invoked deliveries, mint a per-attempt scoped delegation
	// JWT so the addon can write back as the invoker rather than as its
	// own service-account (T18, #358). On any error we proceed without
	// the header — the addon's write-back will fail with its current
	// authority just as it does today, which is the existing behavior
	// rather than a regression. Operators see a Warn line and can
	// triage from there.
	attachAddonDelegationToken(ctx, &delivery, &headers)

	result, err := w.client.Fetch(ctx, subscription.Url, SafeFetchOptions{
		Method:                 http.MethodPost,
		Body:                   bytes.NewReader([]byte(delivery.Payload)),
		Headers:                headers,
		Timeout:                30 * time.Second,
		ResponseHeaderTimeout:  5 * time.Second,
		MaxBodyBytes:           hardResponseBodyCap,
		RejectIfBodyExceedsMax: true,
	})
	if err != nil {
		w.recordTransportFailureMetrics(ctx, target, err)
		w.breaker.recordFailure(target)
		return w.handleDeliveryFailure(ctx, delivery, fmt.Sprintf("request failed: %v", err))
	}

	if result.StatusCode >= 200 && result.StatusCode < 300 {
		w.breaker.recordSuccess(target)
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

	w.breaker.recordFailure(target)
	errorMsg := fmt.Sprintf("HTTP %d: %s", result.StatusCode, truncateForLog(result.Body, logBodyCap))
	return w.handleDeliveryFailure(ctx, delivery, errorMsg)
}

// recordTransportFailureMetrics classifies a transport-level error and
// increments the matching counter so operators can distinguish hostile
// targets (oversize body, slowloris on headers) from generic failures.
func (w *WebhookDeliveryWorker) recordTransportFailureMetrics(ctx context.Context, target string, err error) {
	m := tmiotel.GlobalMetrics
	if m == nil {
		return
	}
	switch {
	case errors.Is(err, ErrSafeHTTPBodyTooLarge):
		m.WebhookResponseTooLarge.Add(ctx, 1, metric.WithAttributes(attribute.String("target", target)))
	case isResponseHeaderTimeout(err):
		m.WebhookResponseHeaderTO.Add(ctx, 1, metric.WithAttributes(attribute.String("target", target)))
	}
}

// isResponseHeaderTimeout reports whether err is the timeout produced
// by net/http when ResponseHeaderTimeout fires before headers arrive.
func isResponseHeaderTimeout(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "timeout awaiting response headers")
}

// truncateForLog returns at most n bytes of body for inclusion in a log
// or error message. Anything above n is replaced with a marker so we
// never spill a 1 MiB upstream response into our logs.
func truncateForLog(body []byte, n int) string {
	if len(body) <= n {
		return string(body)
	}
	return string(body[:n]) + "...[truncated]"
}

// logBodyCap is the maximum number of upstream-response bytes ever
// included in a TMI log line or error string. The wire-level read can
// be much larger (see hardResponseBodyCap) but only this much is
// retained for diagnostics.
const logBodyCap = 10 * 1024

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

// attachAddonDelegationToken mints a scoped delegation JWT for an
// addon.invoked delivery and adds it to the outbound HTTP headers as
// X-TMI-Delegation-Token (T18, #358). For non-addon deliveries it is a
// no-op. On any failure to mint, it logs at Warn and leaves the header
// unset — the delivery still goes out, and the addon's write-back will
// fail with its current authority (the legacy pre-#358 behavior) rather
// than blocking the entire delivery on token-issuance trouble.
//
// The threat-model ID is parsed out of the JSON envelope payload so the
// delegation token can scope to that specific TM; we don't store the TM
// ID on the WebhookDeliveryRecord directly to avoid a Redis-schema
// migration of in-flight records during deployment.
func attachAddonDelegationToken(
	ctx context.Context,
	delivery *WebhookDeliveryRecord,
	headers *http.Header,
) {
	if delivery == nil || headers == nil {
		return
	}
	if delivery.EventType != string(WebhookEventTypeAddonInvoked) {
		return
	}
	if delivery.AddonID == nil || delivery.InvokedByUUID == nil {
		return
	}
	if GlobalDelegationTokenIssuer == nil {
		slogging.Get().Warn(
			"webhook delivery worker: addon.invoked delivery %s has no delegation-token issuer configured; addon write-backs will use legacy SA authority",
			delivery.ID,
		)
		return
	}

	tmID := extractThreatModelIDFromEnvelope(delivery.Payload)
	if tmID == uuid.Nil {
		slogging.Get().Warn(
			"webhook delivery worker: addon.invoked delivery %s has no parseable threat_model_id in envelope; skipping delegation token",
			delivery.ID,
		)
		return
	}

	token, err := GlobalDelegationTokenIssuer.IssueForInvocation(
		ctx,
		delivery.InvokedByUUID.String(),
		*delivery.AddonID,
		delivery.ID,
		tmID,
	)
	if err != nil {
		slogging.Get().Warn(
			"webhook delivery worker: failed to mint delegation token for delivery %s (addon=%s, invoker=%s): %v",
			delivery.ID, delivery.AddonID, delivery.InvokedByUUID, err,
		)
		return
	}

	headers.Set(DelegationTokenHeader, token)
}

// extractThreatModelIDFromEnvelope parses the WebhookDeliveryPayload
// envelope JSON and returns the ThreatModelID, or uuid.Nil on any error.
// Used by attachAddonDelegationToken; tolerant of additional/missing
// fields so envelope-shape changes don't break delegation issuance.
func extractThreatModelIDFromEnvelope(payload string) uuid.UUID {
	if payload == "" {
		return uuid.Nil
	}
	var env struct {
		ThreatModelID uuid.UUID `json:"threat_model_id"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return uuid.Nil
	}
	return env.ThreatModelID
}
