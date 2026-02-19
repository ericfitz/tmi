package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// AddonInvocationWorker handles delivery of add-on invocations to webhooks
type AddonInvocationWorker struct {
	httpClient *http.Client
	running    atomic.Bool
	stopChan   chan struct{}
	workChan   chan uuid.UUID // Channel for invocation IDs to process
	baseURL    string         // Server base URL for callback URLs
}

// AddonInvocationPayload represents the payload sent to webhook endpoints
type AddonInvocationPayload struct {
	EventType     string          `json:"event_type"`
	InvocationID  uuid.UUID       `json:"invocation_id"`
	AddonID       uuid.UUID       `json:"addon_id"`
	ThreatModelID uuid.UUID       `json:"threat_model_id"`
	ObjectType    string          `json:"object_type,omitempty"`
	ObjectID      *uuid.UUID      `json:"object_id,omitempty"`
	Timestamp     time.Time       `json:"timestamp"`
	Payload       json.RawMessage `json:"payload"`
	CallbackURL   string          `json:"callback_url"`
}

// NewAddonInvocationWorker creates a new invocation worker
func NewAddonInvocationWorker() *AddonInvocationWorker {
	return &AddonInvocationWorker{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		},
		stopChan: make(chan struct{}),
		workChan: make(chan uuid.UUID, 100), // Buffer up to 100 pending invocations
		baseURL:  "http://localhost:8080",   // Default, should be set via SetBaseURL
	}
}

// SetBaseURL sets the server's base URL for callback URLs
func (w *AddonInvocationWorker) SetBaseURL(baseURL string) {
	w.baseURL = baseURL
}

// Start begins processing invocations
func (w *AddonInvocationWorker) Start(ctx context.Context) error {
	logger := slogging.Get()

	w.running.Store(true)
	logger.Info("addon invocation worker started")

	// Start processing in a goroutine
	go w.processLoop(ctx)

	return nil
}

// Stop gracefully stops the worker
func (w *AddonInvocationWorker) Stop() {
	logger := slogging.Get()
	if w.running.CompareAndSwap(true, false) {
		close(w.stopChan)
		logger.Info("addon invocation worker stopped")
	}
}

// QueueInvocation queues an invocation for processing
func (w *AddonInvocationWorker) QueueInvocation(invocationID uuid.UUID) {
	select {
	case w.workChan <- invocationID:
		// Successfully queued
	default:
		logger := slogging.Get()
		logger.Warn("addon invocation worker queue full, dropping invocation: %s", invocationID)
	}
}

// processLoop continuously processes invocations from the work queue
func (w *AddonInvocationWorker) processLoop(ctx context.Context) {
	logger := slogging.Get()

	for w.running.Load() {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping invocation worker")
			return
		case <-w.stopChan:
			logger.Info("stop signal received, stopping invocation worker")
			return
		case invocationID := <-w.workChan:
			if err := w.processInvocation(ctx, invocationID); err != nil {
				logger.Error("error processing invocation %s: %v", invocationID, err)
			}
		}
	}
}

// processInvocation processes a single invocation
func (w *AddonInvocationWorker) processInvocation(ctx context.Context, invocationID uuid.UUID) error {
	logger := slogging.Get()

	// Get invocation
	invocation, err := GlobalAddonInvocationStore.Get(ctx, invocationID)
	if err != nil {
		logger.Error("failed to get invocation %s: %v", invocationID, err)
		return err
	}

	// Get add-on details
	addon, err := GlobalAddonStore.Get(ctx, invocation.AddonID)
	if err != nil {
		logger.Error("failed to get add-on %s: %v", invocation.AddonID, err)
		return err
	}

	// Get webhook subscription details
	webhook, err := GlobalWebhookSubscriptionStore.Get(addon.WebhookID.String())
	if err != nil {
		logger.Error("failed to get webhook %s: %v", addon.WebhookID, err)
		// Mark invocation as failed
		invocation.Status = InvocationStatusFailed
		invocation.StatusMessage = fmt.Sprintf("Webhook not found: %v", err)
		_ = GlobalAddonInvocationStore.Update(ctx, invocation)
		return err
	}

	// Check if webhook is active
	if webhook.Status != "active" {
		logger.Warn("webhook %s is not active (status: %s), failing invocation", webhook.Id, webhook.Status)
		invocation.Status = InvocationStatusFailed
		invocation.StatusMessage = fmt.Sprintf("Webhook not active (status: %s)", webhook.Status)
		_ = GlobalAddonInvocationStore.Update(ctx, invocation)
		return nil
	}

	logger.Debug("sending addon invocation to %s (invocation: %s)", webhook.Url, invocationID)

	// Build callback URL using configured base URL
	callbackURL := fmt.Sprintf("%s/invocations/%s/status", w.baseURL, invocationID)

	// Build payload
	payload := AddonInvocationPayload{
		EventType:     "addon.invoked",
		InvocationID:  invocation.ID,
		AddonID:       invocation.AddonID,
		ThreatModelID: invocation.ThreatModelID,
		ObjectType:    invocation.ObjectType,
		ObjectID:      invocation.ObjectID,
		Timestamp:     invocation.CreatedAt,
		Payload:       json.RawMessage(invocation.Payload),
		CallbackURL:   callbackURL,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to marshal invocation payload: %v", err)
		invocation.Status = InvocationStatusFailed
		invocation.StatusMessage = fmt.Sprintf("Failed to marshal payload: %v", err)
		_ = GlobalAddonInvocationStore.Update(ctx, invocation)
		return err
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", webhook.Url, bytes.NewReader(payloadBytes))
	if err != nil {
		invocation.Status = InvocationStatusFailed
		invocation.StatusMessage = fmt.Sprintf("Failed to create request: %v", err)
		_ = GlobalAddonInvocationStore.Update(ctx, invocation)
		return err
	}

	// Add headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", "addon.invoked")
	req.Header.Set("X-Invocation-Id", invocationID.String())
	req.Header.Set("X-Addon-Id", invocation.AddonID.String())
	req.Header.Set("User-Agent", "TMI-Addon-Worker/1.0")

	// Add HMAC signature
	if webhook.Secret != "" {
		signature := crypto.GenerateHMACSignature(payloadBytes, webhook.Secret)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// Send request (no retries for now - webhook can call back with failures)
	resp, err := w.httpClient.Do(req) //nolint:gosec // G704 - URL is from admin-configured addon callback
	if err != nil {
		logger.Error("addon invocation request failed for %s: %v", invocationID, err)
		invocation.Status = InvocationStatusFailed
		invocation.StatusMessage = fmt.Sprintf("Request failed: %v", err)
		_ = GlobalAddonInvocationStore.Update(ctx, invocation)
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response (limit to 10KB for logging)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024))

	// Check response status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		logger.Info("addon invocation sent successfully to %s (invocation: %s, status: %d)",
			webhook.Url, invocationID, resp.StatusCode)

		// Check if webhook wants to use async callbacks
		// If X-TMI-Callback: async is set, the webhook will call back with status updates
		// Otherwise, auto-complete the invocation (webhook handles work internally)
		callbackMode := resp.Header.Get("X-TMI-Callback")

		if callbackMode == "async" {
			// Webhook will call back with status updates
			invocation.Status = InvocationStatusInProgress
			invocation.StatusMessage = "Invocation sent to webhook, awaiting callback"
			logger.Debug("webhook requested async callback mode for invocation %s", invocationID)
		} else {
			// Auto-complete: webhook accepted and will handle internally
			invocation.Status = InvocationStatusCompleted
			invocation.StatusMessage = "Invocation delivered successfully"
			invocation.StatusPercent = 100
			logger.Debug("auto-completing invocation %s (no async callback requested)", invocationID)
		}

		if err := GlobalAddonInvocationStore.Update(ctx, invocation); err != nil {
			logger.Error("failed to update invocation status: %v", err)
		}

		return nil
	}

	// Request failed
	errorMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
	logger.Error("addon invocation failed for %s: %s", invocationID, errorMsg)

	invocation.Status = InvocationStatusFailed
	invocation.StatusMessage = errorMsg
	_ = GlobalAddonInvocationStore.Update(ctx, invocation)

	return fmt.Errorf("invocation failed: %s", errorMsg)
}

// VerifySignature verifies the HMAC signature of a request.
// Delegates to the consolidated crypto package.
func VerifySignature(payload []byte, signature string, secret string) bool {
	return crypto.VerifyHMACSignature(payload, signature, secret)
}

// GlobalAddonInvocationWorker is the global singleton for the invocation worker
var GlobalAddonInvocationWorker *AddonInvocationWorker
