package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// WebhookChallengeWorker handles webhook subscription verification challenges
type WebhookChallengeWorker struct {
	baseWorker
	httpClient *http.Client
}

// NewWebhookChallengeWorker creates a new challenge verification worker
func NewWebhookChallengeWorker() *WebhookChallengeWorker {
	w := &WebhookChallengeWorker{
		httpClient: webhookHTTPClient(10 * time.Second),
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

	// Get all subscriptions pending verification
	subscriptions, err := GlobalWebhookSubscriptionStore.ListPendingVerification()
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
			// Continue with other subscriptions
		}
	}

	return nil
}

// verifySubscription sends a challenge to a webhook URL and verifies the response
func (w *WebhookChallengeWorker) verifySubscription(ctx context.Context, sub DBWebhookSubscription) error {
	logger := slogging.Get()

	// Check if max challenges reached
	const maxChallenges = 3
	if sub.ChallengesSent >= maxChallenges {
		logger.Warn("subscription %s exceeded max challenges (%d), marking for deletion", sub.Id, maxChallenges)
		// Mark for deletion (cleanup worker will handle it)
		if err := GlobalWebhookSubscriptionStore.UpdateStatus(sub.Id.String(), "pending_delete"); err != nil {
			logger.Error("failed to mark subscription %s for deletion: %v", sub.Id, err)
		}
		return nil
	}

	// Generate challenge if not present
	challenge := sub.Challenge
	if challenge == "" {
		challenge = generateRandomHex(32)
	}

	// Send challenge to webhook URL
	logger.Debug("sending challenge to %s (attempt %d/%d)", sub.Url, sub.ChallengesSent+1, maxChallenges)

	// Create JSON payload with challenge
	payload := map[string]string{
		"type":      "webhook.challenge",
		"challenge": challenge,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal challenge payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", sub.Url, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers (no X-Webhook-Challenge header)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", "webhook.challenge")
	req.Header.Set("X-Webhook-Subscription-Id", sub.Id.String())
	req.Header.Set("User-Agent", "TMI-Webhook/1.0")

	// Send request
	resp, err := w.httpClient.Do(req)
	if err != nil {
		logger.Warn("challenge request failed for %s: %v", sub.Url, err)
		// Update challenges sent count
		if updateErr := GlobalWebhookSubscriptionStore.UpdateChallenge(sub.Id.String(), challenge, sub.ChallengesSent+1); updateErr != nil {
			logger.Error("failed to update challenge count: %v", updateErr)
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024)) // Limit to 1KB
	if err != nil {
		logger.Warn("failed to read challenge response: %v", err)
		return err
	}

	// Parse JSON response
	var response map[string]string
	if err := json.Unmarshal(body, &response); err != nil {
		logger.Warn("challenge response is not valid JSON for %s: %v", sub.Url, err)
		// Update challenge count and continue
		if updateErr := GlobalWebhookSubscriptionStore.UpdateChallenge(sub.Id.String(), challenge, sub.ChallengesSent+1); updateErr != nil {
			logger.Error("failed to update challenge count: %v", updateErr)
		}
		return fmt.Errorf("invalid JSON response: %w", err)
	}

	// Verify response contains matching challenge
	if resp.StatusCode == http.StatusOK && response["challenge"] == challenge {
		logger.Info("subscription %s verified successfully", sub.Id)
		// Mark as active
		if err := GlobalWebhookSubscriptionStore.UpdateStatus(sub.Id.String(), "active"); err != nil {
			return fmt.Errorf("failed to activate subscription: %w", err)
		}
		return nil
	}

	// Challenge failed
	logger.Warn("challenge verification failed for %s: status=%d, expected=%s, got=%s",
		sub.Url, resp.StatusCode, challenge, response["challenge"])

	// Update challenge count
	if err := GlobalWebhookSubscriptionStore.UpdateChallenge(sub.Id.String(), challenge, sub.ChallengesSent+1); err != nil {
		logger.Error("failed to update challenge count: %v", err)
	}

	return fmt.Errorf("challenge verification failed: status=%d", resp.StatusCode)
}
