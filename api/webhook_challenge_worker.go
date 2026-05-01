package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// WebhookChallengeWorker handles webhook subscription verification challenges.
// All outbound requests go through SafeHTTPClient (DNS-pinned, SSRF-checked).
type WebhookChallengeWorker struct {
	baseWorker
	client *SafeHTTPClient
}

// NewWebhookChallengeWorker creates a new challenge verification worker.
// The validator controls the SSRF blocklist and URL schemes used for outbound calls.
func NewWebhookChallengeWorker(validator *URIValidator) *WebhookChallengeWorker {
	w := &WebhookChallengeWorker{
		client: NewSafeHTTPClient(
			validator,
			WithUserAgent("TMI-Webhook/1.0"),
			WithTransportWrapper(func(rt http.RoundTripper) http.RoundTripper {
				return otelhttp.NewTransport(rt)
			}),
			WithDefaultTimeouts(10*time.Second, 5*time.Second, 1*1024),
		),
	}
	w.baseWorker = newBaseWorker("webhook challenge worker", 30*time.Second, false, w.processPendingVerifications)
	return w
}

// processPendingVerifications processes all subscriptions pending verification
func (w *WebhookChallengeWorker) processPendingVerifications(ctx context.Context) error {
	logger := slogging.Get()

	if GlobalWebhookSubscriptionStore == nil {
		logger.Warn("webhook subscription store not available")
		return nil
	}

	subscriptions, err := GlobalWebhookSubscriptionStore.ListPendingVerification(ctx)
	if err != nil {
		return fmt.Errorf("failed to list pending verifications: %w", err)
	}

	if len(subscriptions) == 0 {
		return nil
	}

	logger.Debug("processing %d pending verifications", len(subscriptions))

	for _, sub := range subscriptions {
		if err := w.verifySubscription(ctx, sub); err != nil {
			logger.Error("failed to verify subscription %s: %v", sub.Id, err)
		}
	}

	return nil
}

// verifySubscription sends a challenge to a webhook URL and verifies the response
func (w *WebhookChallengeWorker) verifySubscription(ctx context.Context, sub DBWebhookSubscription) error {
	logger := slogging.Get()

	const maxChallenges = 3
	if sub.ChallengesSent >= maxChallenges {
		logger.Warn("subscription %s exceeded max challenges (%d), marking for deletion", sub.Id, maxChallenges)
		if err := GlobalWebhookSubscriptionStore.UpdateStatus(ctx, sub.Id.String(), "pending_delete"); err != nil {
			logger.Error("failed to mark subscription %s for deletion: %v", sub.Id, err)
		}
		return nil
	}

	challenge := sub.Challenge
	if challenge == "" {
		challenge = generateRandomHex(32)
	}

	logger.Debug("sending challenge to %s (attempt %d/%d)", sub.Url, sub.ChallengesSent+1, maxChallenges)

	payload := map[string]string{
		"type":      "webhook.challenge",
		"challenge": challenge,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal challenge payload: %w", err)
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Webhook-Event", "webhook.challenge")
	headers.Set("X-Webhook-Subscription-Id", sub.Id.String())

	result, err := w.client.Fetch(ctx, sub.Url, SafeFetchOptions{
		Method:                http.MethodPost,
		Body:                  bytes.NewReader(payloadBytes),
		Headers:               headers,
		Timeout:               10 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		MaxBodyBytes:          1024,
	})
	if err != nil {
		logger.Warn("challenge request failed for %s: %v", sub.Url, err)
		if updateErr := GlobalWebhookSubscriptionStore.UpdateChallenge(ctx, sub.Id.String(), challenge, sub.ChallengesSent+1); updateErr != nil {
			logger.Error("failed to update challenge count: %v", updateErr)
		}
		return err
	}

	var response map[string]string
	if err := json.Unmarshal(result.Body, &response); err != nil {
		logger.Warn("challenge response is not valid JSON for %s: %v", sub.Url, err)
		if updateErr := GlobalWebhookSubscriptionStore.UpdateChallenge(ctx, sub.Id.String(), challenge, sub.ChallengesSent+1); updateErr != nil {
			logger.Error("failed to update challenge count: %v", updateErr)
		}
		return fmt.Errorf("invalid JSON response: %w", err)
	}

	if result.StatusCode == http.StatusOK && response["challenge"] == challenge {
		logger.Info("subscription %s verified successfully", sub.Id)
		if err := GlobalWebhookSubscriptionStore.UpdateStatus(ctx, sub.Id.String(), "active"); err != nil {
			return fmt.Errorf("failed to activate subscription: %w", err)
		}
		return nil
	}

	logger.Warn("challenge verification failed for %s: status=%d, expected=%s, got=%s",
		sub.Url, result.StatusCode, challenge, response["challenge"])

	if err := GlobalWebhookSubscriptionStore.UpdateChallenge(ctx, sub.Id.String(), challenge, sub.ChallengesSent+1); err != nil {
		logger.Error("failed to update challenge count: %v", err)
	}

	return fmt.Errorf("challenge verification failed: status=%d", result.StatusCode)
}
