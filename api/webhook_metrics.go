package api

import (
	"github.com/ericfitz/tmi/internal/slogging"
)

// WebhookMetrics provides observability for webhook operations
// NOTE: These are stubs for future integration with observability systems
type WebhookMetrics struct{}

// NewWebhookMetrics creates a new metrics collector
func NewWebhookMetrics() *WebhookMetrics {
	return &WebhookMetrics{}
}

// RecordSubscriptionCreated records a subscription creation event
func (m *WebhookMetrics) RecordSubscriptionCreated(ownerID string) {
	logger := slogging.Get()
	logger.Debug("[METRICS] subscription_created owner=%s", ownerID)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: subscriptionCreatedCounter.Inc()
}

// RecordSubscriptionDeleted records a subscription deletion event
func (m *WebhookMetrics) RecordSubscriptionDeleted(ownerID string, reason string) {
	logger := slogging.Get()
	logger.Debug("[METRICS] subscription_deleted owner=%s reason=%s", ownerID, reason)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: subscriptionDeletedCounter.WithLabelValues(reason).Inc()
}

// RecordSubscriptionVerified records a successful subscription verification
func (m *WebhookMetrics) RecordSubscriptionVerified(subscriptionID string) {
	logger := slogging.Get()
	logger.Debug("[METRICS] subscription_verified id=%s", subscriptionID)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: subscriptionVerifiedCounter.Inc()
}

// RecordSubscriptionVerificationFailed records a failed verification attempt
func (m *WebhookMetrics) RecordSubscriptionVerificationFailed(subscriptionID string, attempts int) {
	logger := slogging.Get()
	logger.Debug("[METRICS] subscription_verification_failed id=%s attempts=%d", subscriptionID, attempts)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: subscriptionVerificationFailedCounter.Inc()
}

// RecordEventEmitted records an event emission
func (m *WebhookMetrics) RecordEventEmitted(eventType string, ownerID string) {
	logger := slogging.Get()
	logger.Debug("[METRICS] event_emitted type=%s owner=%s", eventType, ownerID)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: eventEmittedCounter.WithLabelValues(eventType).Inc()
}

// RecordEventDeduplication records a deduplicated event
func (m *WebhookMetrics) RecordEventDeduplication(eventType string) {
	logger := slogging.Get()
	logger.Debug("[METRICS] event_deduplicated type=%s", eventType)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: eventDeduplicatedCounter.WithLabelValues(eventType).Inc()
}

// RecordDeliveryCreated records a delivery creation
func (m *WebhookMetrics) RecordDeliveryCreated(subscriptionID string, eventType string) {
	logger := slogging.Get()
	logger.Debug("[METRICS] delivery_created subscription=%s event=%s", subscriptionID, eventType)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: deliveryCreatedCounter.WithLabelValues(eventType).Inc()
}

// RecordDeliverySuccess records a successful delivery
func (m *WebhookMetrics) RecordDeliverySuccess(subscriptionID string, eventType string, attempts int, latencyMs int64) {
	logger := slogging.Get()
	logger.Debug("[METRICS] delivery_success subscription=%s event=%s attempts=%d latency_ms=%d",
		subscriptionID, eventType, attempts, latencyMs)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example:
	// deliverySuccessCounter.WithLabelValues(eventType).Inc()
	// deliveryLatencyHistogram.WithLabelValues(eventType).Observe(float64(latencyMs))
	// deliveryAttemptsHistogram.WithLabelValues(eventType).Observe(float64(attempts))
}

// RecordDeliveryFailure records a failed delivery
func (m *WebhookMetrics) RecordDeliveryFailure(subscriptionID string, eventType string, attempts int, permanent bool) {
	logger := slogging.Get()
	logger.Debug("[METRICS] delivery_failure subscription=%s event=%s attempts=%d permanent=%t",
		subscriptionID, eventType, attempts, permanent)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example:
	// if permanent {
	//     deliveryPermanentFailureCounter.WithLabelValues(eventType).Inc()
	// } else {
	//     deliveryRetryCounter.WithLabelValues(eventType).Inc()
	// }
}

// RecordRateLimitHit records a rate limit violation
func (m *WebhookMetrics) RecordRateLimitHit(ownerID string, limitType string) {
	logger := slogging.Get()
	logger.Debug("[METRICS] rate_limit_hit owner=%s type=%s", ownerID, limitType)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: rateLimitHitCounter.WithLabelValues(limitType).Inc()
}

// RecordCleanupOperation records a cleanup operation
func (m *WebhookMetrics) RecordCleanupOperation(operationType string, count int) {
	logger := slogging.Get()
	logger.Debug("[METRICS] cleanup_operation type=%s count=%d", operationType, count)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: cleanupOperationCounter.WithLabelValues(operationType).Add(float64(count))
}

// RecordActiveSubscriptions records the current number of active subscriptions
func (m *WebhookMetrics) RecordActiveSubscriptions(count int) {
	logger := slogging.Get()
	logger.Debug("[METRICS] active_subscriptions count=%d", count)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: activeSubscriptionsGauge.Set(float64(count))
}

// RecordPendingDeliveries records the current number of pending deliveries
func (m *WebhookMetrics) RecordPendingDeliveries(count int) {
	logger := slogging.Get()
	logger.Debug("[METRICS] pending_deliveries count=%d", count)
	// TODO: Integrate with OpenTelemetry/Prometheus
	// Example: pendingDeliveriesGauge.Set(float64(count))
}

// Global metrics instance
var GlobalWebhookMetrics *WebhookMetrics

// InitializeWebhookMetrics initializes the global metrics collector
func InitializeWebhookMetrics() {
	GlobalWebhookMetrics = NewWebhookMetrics()
	slogging.Get().Info("webhook metrics initialized (stub mode)")
}
