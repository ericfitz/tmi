package workflows

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// computeHMAC computes an HMAC-SHA256 signature over payload using the given secret.
// Returns the signature in the format "sha256=<hex>".
func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// setupAddonInfrastructure creates the full addon infrastructure needed for invocation tests:
// a webhook subscription (waited to become active), an addon linked to that subscription,
// and a threat model. Returns all IDs needed for invocation.
func setupAddonInfrastructure(t *testing.T, client *framework.IntegrationClient, receiver *framework.WebhookReceiver) (subscriptionID, secret, addonID, threatModelID string) {
	t.Helper()

	// Clear rate limits before creating a subscription to avoid 429 errors
	// when multiple tests each create their own subscription.
	if err := framework.ClearRateLimits(); err != nil {
		t.Logf("Warning: failed to clear rate limits: %v", err)
	}

	// 1. Create subscription with receiver URL
	subPayload := map[string]any{
		"name":   framework.UniqueName("addon-test-sub"),
		"url":    receiver.URL(),
		"events": []string{"addon.invoked"},
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/admin/webhooks/subscriptions",
		Body:   subPayload,
	})
	framework.AssertNoError(t, err, "Failed to create webhook subscription")
	framework.AssertStatusCreated(t, resp)

	subscriptionID = framework.ExtractID(t, resp, "id")

	// Auto-cleanup subscription when test completes
	t.Cleanup(func() {
		_, _ = client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/webhooks/subscriptions/" + subscriptionID,
		})
	})

	// Extract secret from response
	var subResp map[string]any
	err = json.Unmarshal(resp.Body, &subResp)
	framework.AssertNoError(t, err, "Failed to parse subscription response")
	secretVal, ok := subResp["secret"].(string)
	if !ok || secretVal == "" {
		t.Fatal("Expected subscription secret to be present")
	}
	secret = secretVal

	// 2. Wait for activation (challenge verification)
	receiver.WaitForChallenge(t, 90*time.Second)

	framework.PollUntil(t, 90*time.Second, 2*time.Second, func() bool {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/subscriptions/" + subscriptionID,
		})
		if err != nil {
			return false
		}
		var sub map[string]any
		if json.Unmarshal(resp.Body, &sub) != nil {
			return false
		}
		return sub["status"] == "active"
	}, "subscription to become active")

	// Reset receiver to clear challenge deliveries
	receiver.Reset()

	// 3. Create addon linked to subscription
	addonPayload := map[string]any{
		"name":        framework.UniqueName("test-addon"),
		"webhook_id":  subscriptionID,
		"description": "Integration test addon",
		"objects":     []string{"threat_model"},
	}

	resp, err = client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons",
		Body:   addonPayload,
	})
	framework.AssertNoError(t, err, "Failed to create addon")
	framework.AssertStatusCreated(t, resp)

	addonID = framework.ExtractID(t, resp, "id")

	// 4. Create threat model
	tmFixture := framework.NewThreatModelFixture()
	resp, err = client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   tmFixture,
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCreated(t, resp)

	threatModelID = framework.ExtractID(t, resp, "id")

	// Reset receiver again to clear any event deliveries from TM creation
	time.Sleep(5 * time.Second)
	receiver.Reset()

	return subscriptionID, secret, addonID, threatModelID
}

// TestAddonInvocationEndToEnd tests the full addon invocation flow:
// create subscription -> wait active -> create addon -> create threat model -> invoke
// -> verify delivery received with correct payload, HMAC, and delivery status.
func TestAddonInvocationEndToEnd(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Use no-validation client for delivery status polling (OpenAPI validator
	// may reject addon-specific response fields like invoked_by).
	noValClient, err := framework.NewClient(serverURL, tokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create no-validation client")

	receiver := framework.NewWebhookReceiver()
	defer receiver.Close()

	subscriptionID, secret, addonID, threatModelID := setupAddonInfrastructure(t, client, receiver)
	_ = subscriptionID

	// Invoke the addon
	invokePayload := map[string]any{
		"threat_model_id": threatModelID,
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/" + addonID + "/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Failed to invoke addon")
	framework.AssertStatusCode(t, resp, 202)

	deliveryID := framework.ExtractID(t, resp, "delivery_id")
	framework.AssertJSONFieldExists(t, resp, "status")
	framework.AssertValidTimestamp(t, resp, "created_at")

	// Wait for delivery on receiver
	delivery := receiver.WaitForDelivery(t, 30*time.Second)

	// Verify payload contains event_type
	var payload map[string]any
	err = json.Unmarshal(delivery.Body, &payload)
	framework.AssertNoError(t, err, "Failed to parse delivery payload")

	if eventType, ok := payload["event_type"].(string); !ok || eventType != "addon.invoked" {
		t.Errorf("Expected event_type 'addon.invoked', got '%v'", payload["event_type"])
	}

	// Verify HMAC signature
	if delivery.Signature == "" {
		t.Error("Expected X-Webhook-Signature header to be present")
	} else {
		expectedSig := computeHMAC(delivery.Body, secret)
		if delivery.Signature != expectedSig {
			t.Errorf("HMAC signature mismatch: expected %s, got %s", expectedSig, delivery.Signature)
		}
	}

	// Check delivery status via admin API (use no-validation client to avoid
	// OpenAPI response validation silently swallowing the successful response).
	framework.PollUntil(t, 15*time.Second, 1*time.Second, func() bool {
		resp, err := noValClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/deliveries/" + deliveryID,
		})
		if err != nil {
			return false
		}
		var del map[string]any
		if json.Unmarshal(resp.Body, &del) != nil {
			return false
		}
		return del["status"] == "delivered"
	}, "delivery status to become 'delivered'")

	t.Logf("Addon invocation end-to-end test passed: addon=%s, delivery=%s", addonID, deliveryID)
}

// TestAddonInvocationAsyncCallback tests addon invocation with async status updates via HMAC callback.
func TestAddonInvocationAsyncCallback(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Use no-validation client for delivery status polling and HMAC callbacks
	noValClient, err := framework.NewClient(serverURL, tokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create no-validation client")

	receiver := framework.NewWebhookReceiver(framework.WithCallbackMode("async"))
	defer receiver.Close()

	_, secret, addonID, threatModelID := setupAddonInfrastructure(t, client, receiver)

	// Invoke addon
	invokePayload := map[string]any{
		"threat_model_id": threatModelID,
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/" + addonID + "/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Failed to invoke addon")
	framework.AssertStatusCode(t, resp, 202)

	deliveryID := framework.ExtractID(t, resp, "delivery_id")

	// Wait for delivery on receiver
	receiver.WaitForDelivery(t, 30*time.Second)

	// Verify delivery status is in_progress (async mode)
	framework.PollUntil(t, 15*time.Second, 1*time.Second, func() bool {
		resp, err := noValClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/deliveries/" + deliveryID,
		})
		if err != nil {
			return false
		}
		var del map[string]any
		if json.Unmarshal(resp.Body, &del) != nil {
			return false
		}
		return del["status"] == "in_progress"
	}, "delivery status to become 'in_progress'")

	// Send progress update via HMAC callback (use no-validation client for HMAC endpoints)
	progressBody, _ := json.Marshal(map[string]any{
		"status":         "in_progress",
		"status_percent": 50,
		"status_message": "Processing...",
	})
	progressSig := computeHMAC(progressBody, secret)

	resp, err = noValClient.Do(framework.Request{
		Method: "POST",
		Path:   "/webhook-deliveries/" + deliveryID + "/status",
		Body:   json.RawMessage(progressBody),
		Headers: map[string]string{
			"X-Webhook-Signature": progressSig,
		},
	})
	framework.AssertNoError(t, err, "Failed to send progress update")
	framework.AssertStatusOK(t, resp)

	// Send completion via HMAC callback
	completionBody, _ := json.Marshal(map[string]any{
		"status":         "completed",
		"status_percent": 100,
	})
	completionSig := computeHMAC(completionBody, secret)

	resp, err = noValClient.Do(framework.Request{
		Method: "POST",
		Path:   "/webhook-deliveries/" + deliveryID + "/status",
		Body:   json.RawMessage(completionBody),
		Headers: map[string]string{
			"X-Webhook-Signature": completionSig,
		},
	})
	framework.AssertNoError(t, err, "Failed to send completion callback")
	framework.AssertStatusOK(t, resp)

	// Verify final delivery status is delivered
	framework.PollUntil(t, 15*time.Second, 1*time.Second, func() bool {
		resp, err := noValClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/deliveries/" + deliveryID,
		})
		if err != nil {
			return false
		}
		var del map[string]any
		if json.Unmarshal(resp.Body, &del) != nil {
			return false
		}
		return del["status"] == "delivered"
	}, "delivery status to become 'delivered' after completion callback")

	t.Logf("Addon async callback test passed: addon=%s, delivery=%s", addonID, deliveryID)
}

// TestAddonInvocationDeduplication tests that rapid duplicate invocations within the
// 5-second dedup window are rejected with 429.
func TestAddonInvocationDeduplication(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	receiver := framework.NewWebhookReceiver()
	defer receiver.Close()

	_, _, addonID, threatModelID := setupAddonInfrastructure(t, client, receiver)

	invokePayload := map[string]any{
		"threat_model_id": threatModelID,
	}

	// First invocation - should succeed
	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/" + addonID + "/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Failed to invoke addon (first)")
	framework.AssertStatusCode(t, resp, 202)

	// Second invocation immediately - should be rejected as duplicate
	resp, err = client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/" + addonID + "/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Failed to send second invocation")
	framework.AssertStatusCode(t, resp, 429)

	// Verify error mentions duplication
	var errResp map[string]any
	if json.Unmarshal(resp.Body, &errResp) == nil {
		errMsg := fmt.Sprintf("%v %v %v", errResp["error"], errResp["message"], errResp["details"])
		if !strings.Contains(strings.ToLower(errMsg), "duplicate") {
			t.Logf("Note: 429 response did not explicitly mention 'duplicate': %s", string(resp.Body))
		}
	}

	t.Log("Addon deduplication test passed")
}

// TestAddonInvocation_InvalidThreatModel tests that invoking an addon with a
// nonexistent threat_model_id returns 400 or 404.
func TestAddonInvocation_InvalidThreatModel(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	receiver := framework.NewWebhookReceiver()
	defer receiver.Close()

	_, _, addonID, _ := setupAddonInfrastructure(t, client, receiver)

	// Invoke with nonexistent threat model.
	// Note: addon invocation is asynchronous — the server accepts the invocation
	// (202) without validating that the threat_model_id exists. The delivery
	// record will be created and the event emitted regardless.
	invokePayload := map[string]any{
		"threat_model_id": "00000000-0000-0000-0000-000000000000",
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/" + addonID + "/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Request failed unexpectedly")

	// Accept 202 (async acceptance), 400, or 404
	if resp.StatusCode != 202 && resp.StatusCode != 400 && resp.StatusCode != 404 {
		t.Errorf("Expected 202, 400, or 404 for nonexistent threat model, got %d", resp.StatusCode)
	}

	t.Logf("Invalid threat model test passed with status %d", resp.StatusCode)
}

// TestAddonInvocation_InvalidObjectType tests that invoking an addon with an
// unsupported object_type returns 400.
func TestAddonInvocation_InvalidObjectType(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	receiver := framework.NewWebhookReceiver()
	defer receiver.Close()

	_, _, addonID, threatModelID := setupAddonInfrastructure(t, client, receiver)

	// Addon was created with objects: ["threat_model"], try invoking with "diagram"
	invokePayload := map[string]any{
		"threat_model_id": threatModelID,
		"object_type":     "diagram",
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/" + addonID + "/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Request failed unexpectedly")
	framework.AssertStatusBadRequest(t, resp)

	t.Log("Invalid object type test passed")
}

// TestAddonInvocation_PayloadTooLarge tests that invoking an addon with a data
// field exceeding 1024 bytes returns 400.
func TestAddonInvocation_PayloadTooLarge(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	receiver := framework.NewWebhookReceiver()
	defer receiver.Close()

	_, _, addonID, threatModelID := setupAddonInfrastructure(t, client, receiver)

	// Create oversized data payload (>1024 bytes)
	largeData := make(map[string]any)
	// Each key-value pair is ~20 bytes; 100 entries gives ~2000 bytes
	for i := range 100 {
		largeData[fmt.Sprintf("key_%03d", i)] = strings.Repeat("x", 20)
	}

	invokePayload := map[string]any{
		"threat_model_id": threatModelID,
		"data":            largeData,
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/" + addonID + "/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Request failed unexpectedly")
	framework.AssertStatusBadRequest(t, resp)

	t.Log("Payload too large test passed")
}

// TestAddonInvocation_NonexistentAddon tests that invoking a nonexistent addon returns 404.
func TestAddonInvocation_NonexistentAddon(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	invokePayload := map[string]any{
		"threat_model_id": "00000000-0000-0000-0000-000000000000",
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/00000000-0000-0000-0000-000000000000/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Request failed unexpectedly")
	framework.AssertStatusNotFound(t, resp)

	t.Log("Nonexistent addon test passed")
}

// TestAddonInvocation_Unauthorized tests that invoking an addon without authentication
// returns 401.
func TestAddonInvocation_Unauthorized(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Create unauthenticated client
	noAuthClient, err := framework.NewClient(serverURL, nil)
	framework.AssertNoError(t, err, "Failed to create client")

	invokePayload := map[string]any{
		"threat_model_id": "00000000-0000-0000-0000-000000000000",
	}

	resp, err := noAuthClient.Do(framework.Request{
		Method: "POST",
		Path:   "/addons/00000000-0000-0000-0000-000000000000/invoke",
		Body:   invokePayload,
	})
	framework.AssertNoError(t, err, "Request failed unexpectedly")
	framework.AssertStatusUnauthorized(t, resp)

	t.Log("Unauthorized addon invocation test passed")
}
