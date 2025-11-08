package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// AddonInvocationWorker handles delivery of add-on invocations to webhooks
type AddonInvocationWorker struct {
	httpClient *http.Client
	running    bool
	stopChan   chan struct{}
	workChan   chan uuid.UUID // Channel for invocation IDs to process
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
	}
}

// Start begins processing invocations
func (w *AddonInvocationWorker) Start(ctx context.Context) error {
	logger := slogging.Get()

	w.running = true
	logger.Info("addon invocation worker started")

	// Start processing in a goroutine
	go w.processLoop(ctx)

	return nil
}

// Stop gracefully stops the worker
func (w *AddonInvocationWorker) Stop() {
	logger := slogging.Get()
	if w.running {
		w.running = false
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

	for w.running {
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

	// Build callback URL
	// TODO: Get base URL from configuration
	callbackURL := fmt.Sprintf("https://tmi.example.com/invocations/%s/status", invocationID)

	// Build payload
	payload := AddonInvocationPayload{
		EventType:     "addon_invocation",
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
	req.Header.Set("X-Webhook-Event", "addon_invocation")
	req.Header.Set("X-Invocation-Id", invocationID.String())
	req.Header.Set("X-Addon-Id", invocation.AddonID.String())
	req.Header.Set("User-Agent", "TMI-Addon-Worker/1.0")

	// Add HMAC signature
	if webhook.Secret != "" {
		signature := w.generateSignature(payloadBytes, webhook.Secret)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// Send request (no retries for now - webhook can call back with failures)
	resp, err := w.httpClient.Do(req)
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

		// Mark as in_progress (webhook will update to completed/failed via callback)
		invocation.Status = InvocationStatusInProgress
		invocation.StatusMessage = "Invocation sent to webhook"
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

// generateSignature generates HMAC-SHA256 signature for the payload
func (w *AddonInvocationWorker) generateSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature verifies the HMAC signature of a request
func VerifySignature(payload []byte, signature string, secret string) bool {
	expectedSignature := generateHMACSignature(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// generateHMACSignature generates an HMAC signature (helper for verification)
func generateHMACSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// GlobalAddonInvocationWorker is the global singleton for the invocation worker
var GlobalAddonInvocationWorker *AddonInvocationWorker
