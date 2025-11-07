package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// WebhookChallengeWorker handles webhook subscription verification challenges
type WebhookChallengeWorker struct {
	httpClient *http.Client
	running    bool
	stopChan   chan struct{}
}

// NewWebhookChallengeWorker creates a new challenge verification worker
func NewWebhookChallengeWorker() *WebhookChallengeWorker {
	return &WebhookChallengeWorker{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		},
		stopChan: make(chan struct{}),
	}
}

// Start begins processing pending verification challenges
func (w *WebhookChallengeWorker) Start(ctx context.Context) error {
	logger := slogging.Get()

	w.running = true
	logger.Info("webhook challenge worker started")

	// Start processing in a goroutine
	go w.processLoop(ctx)

	return nil
}

// Stop gracefully stops the worker
func (w *WebhookChallengeWorker) Stop() {
	logger := slogging.Get()
	if w.running {
		w.running = false
		close(w.stopChan)
		logger.Info("webhook challenge worker stopped")
	}
}

// processLoop continuously processes pending verifications
func (w *WebhookChallengeWorker) processLoop(ctx context.Context) {
	logger := slogging.Get()
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	for w.running {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping challenge worker")
			return
		case <-w.stopChan:
			logger.Info("stop signal received, stopping challenge worker")
			return
		case <-ticker.C:
			if err := w.processPendingVerifications(ctx); err != nil {
				logger.Error("error processing pending verifications: %v", err)
			}
		}
	}
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
		challenge = generateChallenge()
	}

	// Send challenge to webhook URL
	logger.Debug("sending challenge to %s (attempt %d/%d)", sub.Url, sub.ChallengesSent+1, maxChallenges)

	req, err := http.NewRequestWithContext(ctx, "POST", sub.Url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add challenge headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Challenge", challenge)
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

	// Verify response
	if resp.StatusCode == http.StatusOK && string(body) == challenge {
		logger.Info("subscription %s verified successfully", sub.Id)
		// Mark as active
		if err := GlobalWebhookSubscriptionStore.UpdateStatus(sub.Id.String(), "active"); err != nil {
			return fmt.Errorf("failed to activate subscription: %w", err)
		}
		return nil
	}

	// Challenge failed
	logger.Warn("challenge verification failed for %s: status=%d, expected=%s, got=%s",
		sub.Url, resp.StatusCode, challenge, string(body))

	// Update challenge count
	if err := GlobalWebhookSubscriptionStore.UpdateChallenge(sub.Id.String(), challenge, sub.ChallengesSent+1); err != nil {
		logger.Error("failed to update challenge count: %v", err)
	}

	return fmt.Errorf("challenge verification failed: status=%d", resp.StatusCode)
}

// generateChallenge generates a random challenge string
func generateChallenge() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based challenge
		return fmt.Sprintf("challenge_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
