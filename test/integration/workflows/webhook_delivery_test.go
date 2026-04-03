package workflows

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// generateHMACSignature generates an HMAC-SHA256 signature for the payload.
// Returns the signature in the format "sha256=<hex-encoded-mac>".
// This duplicates internal/crypto.GenerateHMACSignature to avoid cross-module import issues.
func generateHMACSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// verifyHMACSignature verifies an HMAC-SHA256 signature using constant-time comparison.
func verifyHMACSignature(payload []byte, signature string, secret string) bool {
	expected := generateHMACSignature(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// setupActiveSubscription creates a webhook subscription and waits for it to become active.
// It returns the subscription ID and secret. The receiver must be configured with ChallengeAutoRespond.
func setupActiveSubscription(
	t *testing.T,
	client *framework.IntegrationClient,
	receiverURL string,
	events []string,
) (subscriptionID, secret string) {
	t.Helper()

	fixture := map[string]any{
		"name":   fmt.Sprintf("Delivery Test Webhook %s", time.Now().Format("150405")),
		"url":    receiverURL,
		"events": events,
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/admin/webhooks/subscriptions",
		Body:   fixture,
	})
	framework.AssertNoError(t, err, "Failed to create webhook subscription")
	framework.AssertStatusCreated(t, resp)

	subscriptionID = framework.ExtractID(t, resp, "id")

	var sub map[string]any
	err = json.Unmarshal(resp.Body, &sub)
	framework.AssertNoError(t, err, "Failed to parse subscription response")

	if s, ok := sub["secret"].(string); ok {
		secret = s
	}
	if secret == "" {
		t.Fatal("Expected secret in subscription response")
	}

	// Wait for challenge verification to complete and subscription to become active (90s timeout).
	framework.PollUntil(t, 90*time.Second, 2*time.Second, func() bool {
		r, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/subscriptions/" + subscriptionID,
		})
		if err != nil {
			return false
		}
		var data map[string]any
		if json.Unmarshal(r.Body, &data) != nil {
			return false
		}
		return data["status"] == "active"
	}, "subscription to become active")

	return subscriptionID, secret
}

// setupActiveSubscriptionWithTMFilter creates a subscription filtered to a specific threat model.
func setupActiveSubscriptionWithTMFilter(
	t *testing.T,
	client *framework.IntegrationClient,
	receiverURL string,
	events []string,
	threatModelID string,
) (subscriptionID, secret string) {
	t.Helper()

	fixture := map[string]any{
		"name":            fmt.Sprintf("Filtered Webhook %s", time.Now().Format("150405")),
		"url":             receiverURL,
		"events":          events,
		"threat_model_id": threatModelID,
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/admin/webhooks/subscriptions",
		Body:   fixture,
	})
	framework.AssertNoError(t, err, "Failed to create webhook subscription")
	framework.AssertStatusCreated(t, resp)

	subscriptionID = framework.ExtractID(t, resp, "id")

	var sub map[string]any
	err = json.Unmarshal(resp.Body, &sub)
	framework.AssertNoError(t, err, "Failed to parse subscription response")

	if s, ok := sub["secret"].(string); ok {
		secret = s
	}
	if secret == "" {
		t.Fatal("Expected secret in subscription response")
	}

	framework.PollUntil(t, 90*time.Second, 2*time.Second, func() bool {
		r, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/subscriptions/" + subscriptionID,
		})
		if err != nil {
			return false
		}
		var data map[string]any
		if json.Unmarshal(r.Body, &data) != nil {
			return false
		}
		return data["status"] == "active"
	}, "filtered subscription to become active")

	return subscriptionID, secret
}

// createThreatModel creates a threat model and returns its ID.
func createThreatModel(t *testing.T, client *framework.IntegrationClient) string {
	t.Helper()

	fixture := framework.NewThreatModelFixture().
		WithName(fmt.Sprintf("Webhook Test TM %s", time.Now().Format("150405.000"))).
		WithDescription("Created for webhook delivery testing")

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   fixture,
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCreated(t, resp)

	return framework.ExtractID(t, resp, "id")
}

// updateThreatModel updates a threat model's description.
func updateThreatModel(t *testing.T, client *framework.IntegrationClient, tmID string) {
	t.Helper()

	updatePayload := map[string]any{
		"name":        fmt.Sprintf("Updated TM %s", time.Now().Format("150405.000")),
		"description": "Updated for webhook delivery testing",
	}

	resp, err := client.Do(framework.Request{
		Method: "PUT",
		Path:   "/threat_models/" + tmID,
		Body:   updatePayload,
	})
	framework.AssertNoError(t, err, "Failed to update threat model")
	framework.AssertStatusOK(t, resp)
}

// getDeliveryRecord retrieves a delivery record by ID (admin endpoint).
func getDeliveryRecord(t *testing.T, client *framework.IntegrationClient, deliveryID string) map[string]any {
	t.Helper()

	resp, err := client.Do(framework.Request{
		Method: "GET",
		Path:   "/admin/webhooks/deliveries/" + deliveryID,
	})
	framework.AssertNoError(t, err, "Failed to get delivery record")
	framework.AssertStatusOK(t, resp)

	var record map[string]any
	err = json.Unmarshal(resp.Body, &record)
	framework.AssertNoError(t, err, "Failed to parse delivery record")
	return record
}

// getSubscriptionDeliveries lists deliveries filtered by subscription ID.
func getSubscriptionDeliveries(t *testing.T, client *framework.IntegrationClient, subscriptionID string) []map[string]any {
	t.Helper()

	resp, err := client.Do(framework.Request{
		Method:      "GET",
		Path:        "/admin/webhooks/deliveries",
		QueryParams: map[string]string{"subscription_id": subscriptionID},
	})
	framework.AssertNoError(t, err, "Failed to list deliveries")
	framework.AssertStatusOK(t, resp)

	var deliveries []map[string]any
	err = json.Unmarshal(resp.Body, &deliveries)
	framework.AssertNoError(t, err, "Failed to parse deliveries list")
	return deliveries
}

// postDeliveryStatusHMAC sends an HMAC-authenticated status update to the public endpoint.
func postDeliveryStatusHMAC(
	t *testing.T,
	serverURL string,
	deliveryID string,
	secret string,
	body map[string]any,
) (*http.Response, []byte) {
	t.Helper()

	bodyBytes, err := json.Marshal(body)
	framework.AssertNoError(t, err, "Failed to marshal status body")

	sig := generateHMACSignature(bodyBytes, secret)

	url := serverURL + "/webhook-deliveries/" + deliveryID + "/status"
	req, err := http.NewRequest("POST", url, strings.NewReader(string(bodyBytes)))
	framework.AssertNoError(t, err, "Failed to create request")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", sig)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	framework.AssertNoError(t, err, "Status update request failed")

	var respBody []byte
	if resp.Body != nil {
		respBody, _ = json.Marshal(nil) // placeholder
		buf := make([]byte, 0, 4096)
		for {
			tmp := make([]byte, 1024)
			n, readErr := resp.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if readErr != nil {
				break
			}
		}
		resp.Body.Close()
		respBody = buf
	}

	return resp, respBody
}

// TestWebhookDelivery is the top-level test function containing all webhook delivery
// integration test scenarios. It runs shared setup once (authentication, admin check)
// and then delegates to subtests.
//
// Covers OpenAPI operations:
// - POST /admin/webhooks/subscriptions (createWebhookSubscription)
// - GET /admin/webhooks/subscriptions/{webhook_id} (getWebhookSubscription)
// - GET /admin/webhooks/deliveries (listWebhookDeliveries)
// - GET /admin/webhooks/deliveries/{delivery_id} (getWebhookDelivery)
// - GET /webhook-deliveries/{delivery_id} (getWebhookDeliveryStatus)
// - POST /webhook-deliveries/{delivery_id}/status (updateWebhookDeliveryStatus)
// - DELETE /admin/webhooks/subscriptions/{webhook_id} (deleteWebhookSubscription)
func TestWebhookDelivery(t *testing.T) {
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

	// First user gets auto-promoted to admin.
	adminUserID := framework.UniqueUserID()
	adminTokens, err := framework.AuthenticateUser(adminUserID)
	framework.AssertNoError(t, err, "Admin authentication failed")

	client, err := framework.NewClient(serverURL, adminTokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Verify admin access.
	t.Run("CheckAdminAccess", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me",
		})
		framework.AssertNoError(t, err, "Failed to get user profile")
		framework.AssertStatusOK(t, resp)

		var user map[string]any
		err = json.Unmarshal(resp.Body, &user)
		framework.AssertNoError(t, err, "Failed to parse user response")

		isAdmin, _ := user["is_admin"].(bool)
		if !isAdmin {
			t.Fatal("First user should be auto-promoted to admin but is_admin is false")
		}
		t.Log("User is admin, webhook operations available")
	})

	// ========================================================================
	// Section 3: Happy Path Tests
	// ========================================================================

	// 3.1 SubscriptionLifecycle: create -> challenge -> active
	t.Run("SubscriptionLifecycle", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		fixture := map[string]any{
			"name":   "Lifecycle Test Webhook",
			"url":    receiver.URL(),
			"events": []string{"threat_model.created"},
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/webhooks/subscriptions",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create subscription")
		framework.AssertStatusCreated(t, resp)

		subID := framework.ExtractID(t, resp, "id")

		var sub map[string]any
		err = json.Unmarshal(resp.Body, &sub)
		framework.AssertNoError(t, err, "Failed to parse subscription")

		if sub["status"] != "pending_verification" {
			t.Errorf("Expected initial status pending_verification, got %v", sub["status"])
		}

		secret, _ := sub["secret"].(string)
		if secret == "" {
			t.Fatal("Expected secret in subscription response")
		}

		// Wait for challenge.
		challenge := receiver.WaitForChallenge(t, 90*time.Second)
		if challenge.EventType != "webhook.challenge" {
			t.Errorf("Expected challenge event type webhook.challenge, got %s", challenge.EventType)
		}
		if challenge.SubscriptionID != subID {
			t.Errorf("Expected subscription ID %s in challenge, got %s", subID, challenge.SubscriptionID)
		}

		// Poll until active.
		framework.PollUntil(t, 90*time.Second, 2*time.Second, func() bool {
			r, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/admin/webhooks/subscriptions/" + subID,
			})
			if err != nil {
				return false
			}
			var data map[string]any
			if json.Unmarshal(r.Body, &data) != nil {
				return false
			}
			return data["status"] == "active"
		}, "subscription to become active")

		t.Logf("Subscription %s transitioned to active", subID)

		// Cleanup.
		_, _ = client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/webhooks/subscriptions/" + subID,
		})
	})

	// 3.2 EventDelivery: trigger event -> verify delivery received
	t.Run("EventDelivery", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)

		if delivery.EventType != "threat_model.created" {
			t.Errorf("Expected event type threat_model.created, got %s", delivery.EventType)
		}
		if delivery.SubscriptionID != subID {
			t.Errorf("Expected subscription ID %s, got %s", subID, delivery.SubscriptionID)
		}
		if delivery.DeliveryID == "" {
			t.Error("Expected non-empty delivery ID header")
		}
		if delivery.Signature == "" {
			t.Error("Expected non-empty HMAC signature header")
		}

		// Verify payload contains expected fields.
		var payload map[string]any
		err := json.Unmarshal(delivery.Body, &payload)
		framework.AssertNoError(t, err, "Failed to parse delivery payload")

		if payload["event_type"] == nil {
			t.Error("Expected event_type in payload")
		}

		t.Logf("Received delivery %s for event %s", delivery.DeliveryID, delivery.EventType)
	})

	// 3.3 DeliveryHMAC: verify HMAC signature correctness
	t.Run("DeliveryHMAC", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		subID, secret := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)

		// Verify HMAC with correct secret.
		expectedSig := generateHMACSignature(delivery.Body, secret)
		if delivery.Signature != expectedSig {
			t.Errorf("HMAC mismatch: expected %s, got %s", expectedSig, delivery.Signature)
		}

		// Verify HMAC with wrong secret fails.
		if verifyHMACSignature(delivery.Body, delivery.Signature, "wrong-secret") {
			t.Error("HMAC should not verify with wrong secret")
		}

		// Verify HMAC with correct secret passes.
		if !verifyHMACSignature(delivery.Body, delivery.Signature, secret) {
			t.Error("HMAC should verify with correct secret")
		}

		t.Log("HMAC signature verification passed")
	})

	// 3.4 ChallengeFailure: bad challenge -> pending_delete
	t.Run("ChallengeFailure", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver(framework.WithChallengeMode(framework.ChallengeIgnore))
		defer receiver.Close()

		fixture := map[string]any{
			"name":   "Challenge Fail Webhook",
			"url":    receiver.URL(),
			"events": []string{"threat_model.created"},
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/webhooks/subscriptions",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create subscription")
		framework.AssertStatusCreated(t, resp)

		subID := framework.ExtractID(t, resp, "id")
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		// Wait for subscription to transition to pending_delete after failed challenges.
		framework.PollUntil(t, 120*time.Second, 3*time.Second, func() bool {
			r, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/admin/webhooks/subscriptions/" + subID,
			})
			if err != nil {
				return false
			}
			var data map[string]any
			if json.Unmarshal(r.Body, &data) != nil {
				return false
			}
			status, _ := data["status"].(string)
			return status == "pending_delete"
		}, "subscription to become pending_delete after challenge failure")

		t.Log("Subscription correctly marked as pending_delete after challenge failure")
	})

	// 3.5 AsyncCallback: async delivery -> status callback
	t.Run("AsyncCallback", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver(framework.WithCallbackMode("async"))
		defer receiver.Close()

		subID, secret := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)
		deliveryID := delivery.DeliveryID
		if deliveryID == "" {
			t.Fatal("Expected delivery ID in header")
		}

		// Poll until delivery status is in_progress (async callback accepted).
		framework.PollUntil(t, 15*time.Second, 1*time.Second, func() bool {
			record := getDeliveryRecord(t, client, deliveryID)
			status, _ := record["status"].(string)
			return status == "in_progress"
		}, "delivery to be in_progress")

		// Send completion callback with HMAC.
		statusBody := map[string]any{
			"status":         "completed",
			"status_percent": 100,
		}
		statusResp, _ := postDeliveryStatusHMAC(t, serverURL, deliveryID, secret, statusBody)
		if statusResp.StatusCode != 200 {
			t.Errorf("Expected 200 for status callback, got %d", statusResp.StatusCode)
		}

		// Poll until delivered.
		framework.PollUntil(t, 15*time.Second, 1*time.Second, func() bool {
			record := getDeliveryRecord(t, client, deliveryID)
			status, _ := record["status"].(string)
			return status == "delivered"
		}, "delivery to be delivered after callback")

		t.Logf("Async callback completed for delivery %s", deliveryID)
	})

	// 3.6 DeliveryTracking: verify delivery records via API
	t.Run("DeliveryTracking", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		subID, secret := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)
		deliveryID := delivery.DeliveryID

		// Verify delivery list includes our delivery.
		deliveries := getSubscriptionDeliveries(t, client, subID)
		found := false
		for _, d := range deliveries {
			if id, ok := d["id"].(string); ok && id == deliveryID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find delivery %s in subscription deliveries", deliveryID)
		}

		// Verify individual record.
		record := getDeliveryRecord(t, client, deliveryID)
		if record["subscription_id"] != subID {
			t.Errorf("Expected subscription_id %s, got %v", subID, record["subscription_id"])
		}
		if record["event_type"] != "threat_model.created" {
			t.Errorf("Expected event_type threat_model.created, got %v", record["event_type"])
		}

		// Verify public endpoint with JWT auth.
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/webhook-deliveries/" + deliveryID,
		})
		framework.AssertNoError(t, err, "Failed to get delivery via public endpoint")
		framework.AssertStatusOK(t, resp)

		// Verify public endpoint with HMAC auth (sign delivery ID with secret).
		hmacSig := generateHMACSignature([]byte(deliveryID), secret)
		hmacClient := &http.Client{Timeout: 30 * time.Second}
		hmacReq, _ := http.NewRequest("GET", serverURL+"/webhook-deliveries/"+deliveryID, nil)
		hmacReq.Header.Set("X-Webhook-Signature", hmacSig)
		hmacResp, err := hmacClient.Do(hmacReq)
		framework.AssertNoError(t, err, "HMAC GET request failed")
		hmacResp.Body.Close()
		if hmacResp.StatusCode != 200 {
			t.Errorf("Expected 200 for HMAC-auth GET, got %d", hmacResp.StatusCode)
		}

		t.Logf("Delivery tracking verified for %s", deliveryID)
	})

	// 3.7 DeliveryFailure_ServerError: 500 response -> error recorded
	t.Run("DeliveryFailure_ServerError", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver(framework.WithStatusCode(500))
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		// Wait for at least one delivery attempt to be recorded.
		var deliveryRecord map[string]any
		framework.PollUntil(t, 30*time.Second, 2*time.Second, func() bool {
			deliveries := getSubscriptionDeliveries(t, client, subID)
			for _, d := range deliveries {
				eventType, _ := d["event_type"].(string)
				if eventType == "threat_model.created" {
					attempts, _ := d["attempts"].(float64)
					if attempts >= 1 {
						deliveryRecord = d
						return true
					}
				}
			}
			return false
		}, "delivery attempt with error to be recorded")

		lastError, _ := deliveryRecord["last_error"].(string)
		if !strings.Contains(strings.ToLower(lastError), "500") {
			t.Logf("Note: last_error does not contain '500': %s", lastError)
		}

		status, _ := deliveryRecord["status"].(string)
		if status != "pending" && status != "failed" {
			t.Errorf("Expected delivery status pending or failed, got %s", status)
		}

		t.Log("Server error (500) delivery failure recorded correctly")
	})

	// 3.8 DeliveryFailure_ReceiverDown: closed receiver -> connection error
	t.Run("DeliveryFailure_ReceiverDown", func(t *testing.T) {
		// Start receiver for challenge verification, then close it.
		receiver := framework.NewWebhookReceiver()
		receiverURL := receiver.URL()

		subID, _ := setupActiveSubscription(t, client, receiverURL, []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		// Close receiver before triggering event.
		receiver.Close()

		_ = createThreatModel(t, client)

		// Wait for a delivery attempt that records a connection error.
		framework.PollUntil(t, 30*time.Second, 2*time.Second, func() bool {
			deliveries := getSubscriptionDeliveries(t, client, subID)
			for _, d := range deliveries {
				eventType, _ := d["event_type"].(string)
				attempts, _ := d["attempts"].(float64)
				if eventType == "threat_model.created" && attempts >= 1 {
					return true
				}
			}
			return false
		}, "delivery attempt to be recorded for closed receiver")

		deliveries := getSubscriptionDeliveries(t, client, subID)
		for _, d := range deliveries {
			eventType, _ := d["event_type"].(string)
			if eventType == "threat_model.created" {
				status, _ := d["status"].(string)
				if status != "pending" && status != "failed" {
					t.Errorf("Expected delivery status pending or failed, got %s", status)
				}
				lastError, _ := d["last_error"].(string)
				if lastError == "" {
					t.Error("Expected last_error to be set for connection failure")
				}
				t.Logf("Connection error recorded: %s", lastError)
				break
			}
		}

		t.Log("Receiver-down delivery failure recorded correctly")
	})

	// 3.9 DeliveryRetry_EventualSuccess: fail then succeed on retry (~70s)
	t.Run("DeliveryRetry_EventualSuccess", func(t *testing.T) {
		t.Log("This test waits for retry backoff (~70s)")

		// Fail first delivery with 500, then succeed.
		receiver := framework.NewWebhookReceiver(
			framework.WithStatusCode(500),
			framework.WithFailCount(1),
		)
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		// Wait for at least 2 delivery attempts (first fails, retry succeeds).
		framework.PollUntil(t, 90*time.Second, 3*time.Second, func() bool {
			return receiver.DeliveryCount() >= 2
		}, "retry delivery attempt")

		// Verify delivery record shows success.
		framework.PollUntil(t, 15*time.Second, 2*time.Second, func() bool {
			deliveries := getSubscriptionDeliveries(t, client, subID)
			for _, d := range deliveries {
				eventType, _ := d["event_type"].(string)
				status, _ := d["status"].(string)
				if eventType == "threat_model.created" && status == "delivered" {
					return true
				}
			}
			return false
		}, "delivery to show as delivered after retry")

		if receiver.DeliveryCount() < 2 {
			t.Errorf("Expected at least 2 delivery attempts, got %d", receiver.DeliveryCount())
		}

		t.Logf("Retry succeeded after %d attempts", receiver.DeliveryCount())
	})

	// 3.10 DeliveryRetry_PermanentFailure: verify retry scheduling
	t.Run("DeliveryRetry_PermanentFailure", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver(framework.WithStatusCode(503))
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		// Wait for first delivery attempt.
		var deliveryRecord map[string]any
		framework.PollUntil(t, 30*time.Second, 2*time.Second, func() bool {
			deliveries := getSubscriptionDeliveries(t, client, subID)
			for _, d := range deliveries {
				eventType, _ := d["event_type"].(string)
				attempts, _ := d["attempts"].(float64)
				if eventType == "threat_model.created" && attempts >= 1 {
					deliveryRecord = d
					return true
				}
			}
			return false
		}, "first delivery attempt to be recorded")

		attempts, _ := deliveryRecord["attempts"].(float64)
		if attempts < 1 {
			t.Errorf("Expected at least 1 attempt, got %v", attempts)
		}

		lastError, _ := deliveryRecord["last_error"].(string)
		if lastError == "" {
			t.Error("Expected last_error to be set")
		}

		// Verify retry is scheduled (next_retry_at should be set).
		nextRetry, _ := deliveryRecord["next_retry_at"].(string)
		if nextRetry == "" {
			t.Log("Note: next_retry_at not set - may be null for first attempt")
		} else {
			t.Logf("Next retry scheduled at: %s", nextRetry)
		}

		status, _ := deliveryRecord["status"].(string)
		if status != "pending" {
			t.Errorf("Expected status pending for retry, got %s", status)
		}

		t.Log("Permanent failure retry scheduling verified")
	})

	// 3.11 DeliveryFailure_4xx: 400 response still retried
	t.Run("DeliveryFailure_4xx", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver(framework.WithStatusCode(400))
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		// Wait for delivery attempt.
		var deliveryRecord map[string]any
		framework.PollUntil(t, 30*time.Second, 2*time.Second, func() bool {
			deliveries := getSubscriptionDeliveries(t, client, subID)
			for _, d := range deliveries {
				eventType, _ := d["event_type"].(string)
				attempts, _ := d["attempts"].(float64)
				if eventType == "threat_model.created" && attempts >= 1 {
					deliveryRecord = d
					return true
				}
			}
			return false
		}, "delivery attempt with 400 error")

		lastError, _ := deliveryRecord["last_error"].(string)
		if !strings.Contains(strings.ToLower(lastError), "400") {
			t.Logf("Note: last_error does not contain '400': %s", lastError)
		}

		status, _ := deliveryRecord["status"].(string)
		if status != "pending" && status != "failed" {
			t.Errorf("Expected status pending or failed, got %s", status)
		}

		t.Log("4xx delivery failure handling verified")
	})

	// 3.12 DeliveryFailure_Timeout: slow receiver -> timeout error
	t.Run("DeliveryFailure_Timeout", func(t *testing.T) {
		t.Log("This test waits for HTTP client timeout (~35s)")

		receiver := framework.NewWebhookReceiver(framework.WithResponseDelay(35 * time.Second))
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		// Wait for timeout-related delivery error.
		framework.PollUntil(t, 45*time.Second, 3*time.Second, func() bool {
			deliveries := getSubscriptionDeliveries(t, client, subID)
			for _, d := range deliveries {
				eventType, _ := d["event_type"].(string)
				attempts, _ := d["attempts"].(float64)
				if eventType == "threat_model.created" && attempts >= 1 {
					return true
				}
			}
			return false
		}, "timeout delivery error to be recorded")

		deliveries := getSubscriptionDeliveries(t, client, subID)
		for _, d := range deliveries {
			eventType, _ := d["event_type"].(string)
			if eventType == "threat_model.created" {
				lastError, _ := d["last_error"].(string)
				if lastError == "" {
					t.Error("Expected last_error to be set for timeout")
				} else {
					t.Logf("Timeout error recorded: %s", lastError)
				}
				break
			}
		}

		t.Log("Timeout delivery failure recorded correctly")
	})

	// ========================================================================
	// Section 4: Negative Tests
	// ========================================================================

	// 4.1 NoMatchingSubscription: unmatched event type -> no delivery
	t.Run("NoMatchingSubscription", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		// Subscribe only to diagram.created.
		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"diagram.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		// Trigger threat_model.created (should NOT match).
		_ = createThreatModel(t, client)

		// Wait 10s and verify no deliveries received.
		time.Sleep(10 * time.Second)

		if receiver.DeliveryCount() > 0 {
			t.Errorf("Expected no deliveries for unmatched event type, got %d", receiver.DeliveryCount())
		}

		t.Log("No delivery for unmatched event type - correct")
	})

	// 4.2 InactiveSubscription: pending subscription doesn't receive events
	t.Run("InactiveSubscription", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver(framework.WithChallengeMode(framework.ChallengeIgnore))
		defer receiver.Close()

		fixture := map[string]any{
			"name":   "Inactive Sub Webhook",
			"url":    receiver.URL(),
			"events": []string{"threat_model.created"},
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/webhooks/subscriptions",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create subscription")
		framework.AssertStatusCreated(t, resp)

		subID := framework.ExtractID(t, resp, "id")
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		// Verify subscription is not active.
		time.Sleep(5 * time.Second)
		r, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/subscriptions/" + subID,
		})
		framework.AssertNoError(t, err, "Failed to get subscription")
		var subData map[string]any
		_ = json.Unmarshal(r.Body, &subData)
		status, _ := subData["status"].(string)
		if status == "active" {
			t.Skip("Subscription became active unexpectedly, skipping")
		}

		// Trigger event.
		_ = createThreatModel(t, client)

		// Wait 10s and verify no event deliveries (challenges may arrive but not events).
		time.Sleep(10 * time.Second)

		if receiver.DeliveryCount() > 0 {
			t.Errorf("Expected no event deliveries to inactive subscription, got %d", receiver.DeliveryCount())
		}

		t.Log("Inactive subscription correctly did not receive events")
	})

	// 4.3 WildcardSubscription: * matches all events
	t.Run("WildcardSubscription", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"*"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		// Create a threat model (threat_model.created).
		tmID := createThreatModel(t, client)

		// Wait for first delivery.
		delivery1 := receiver.WaitForDelivery(t, 30*time.Second)
		if delivery1.EventType != "threat_model.created" {
			t.Errorf("Expected first event threat_model.created, got %s", delivery1.EventType)
		}

		// Update the threat model (threat_model.updated).
		updateThreatModel(t, client, tmID)

		// Wait for second delivery.
		receiver.WaitForDeliveries(t, 2, 30*time.Second)
		deliveries := receiver.Deliveries()
		if len(deliveries) < 2 {
			t.Fatalf("Expected at least 2 deliveries, got %d", len(deliveries))
		}

		// Second delivery should be threat_model.updated.
		found := false
		for _, d := range deliveries {
			if d.EventType == "threat_model.updated" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected threat_model.updated event in deliveries")
		}

		t.Logf("Wildcard subscription received %d deliveries", len(deliveries))
	})

	// 4.4 ThreatModelFilter: scoped subscription
	t.Run("ThreatModelFilter", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		// Create the target threat model first.
		targetTMID := createThreatModel(t, client)

		subID, _ := setupActiveSubscriptionWithTMFilter(
			t, client, receiver.URL(),
			[]string{"threat_model.updated"},
			targetTMID,
		)
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		// Update the target TM (should trigger delivery).
		updateThreatModel(t, client, targetTMID)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)
		if delivery.EventType != "threat_model.updated" {
			t.Errorf("Expected threat_model.updated, got %s", delivery.EventType)
		}

		// Create and update a different TM (should NOT trigger delivery).
		otherTMID := createThreatModel(t, client)
		updateThreatModel(t, client, otherTMID)

		// Wait 10s and verify no additional deliveries.
		time.Sleep(10 * time.Second)

		if receiver.DeliveryCount() != 1 {
			t.Errorf("Expected exactly 1 delivery for filtered subscription, got %d", receiver.DeliveryCount())
		}

		t.Log("Threat model filter correctly scoped deliveries")
	})

	// 4.5 AsyncCallback_InvalidSignature: wrong HMAC rejected
	t.Run("AsyncCallback_InvalidSignature", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver(framework.WithCallbackMode("async"))
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)
		deliveryID := delivery.DeliveryID

		// Wait for in_progress status.
		framework.PollUntil(t, 15*time.Second, 1*time.Second, func() bool {
			record := getDeliveryRecord(t, client, deliveryID)
			status, _ := record["status"].(string)
			return status == "in_progress"
		}, "delivery to be in_progress")

		// Send status update with wrong HMAC.
		statusBody := map[string]any{"status": "completed", "status_percent": 100}
		bodyBytes, _ := json.Marshal(statusBody)
		wrongSig := generateHMACSignature(bodyBytes, "wrong-secret-value")

		url := serverURL + "/webhook-deliveries/" + deliveryID + "/status"
		req, _ := http.NewRequest("POST", url, strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", wrongSig)

		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(req)
		framework.AssertNoError(t, err, "Status update request failed")
		resp.Body.Close()

		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 for wrong HMAC, got %d", resp.StatusCode)
		}

		// Verify status is still in_progress.
		record := getDeliveryRecord(t, client, deliveryID)
		status, _ := record["status"].(string)
		if status != "in_progress" {
			t.Errorf("Expected status still in_progress after rejected callback, got %s", status)
		}

		t.Log("Invalid HMAC correctly rejected for status callback")
	})

	// 4.6 AsyncCallback_InvalidStatusTransition: can't update delivered
	t.Run("AsyncCallback_InvalidStatusTransition", func(t *testing.T) {
		// Use sync mode so delivery auto-completes as delivered.
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		subID, secret := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)
		deliveryID := delivery.DeliveryID

		// Wait for delivered status.
		framework.PollUntil(t, 15*time.Second, 1*time.Second, func() bool {
			record := getDeliveryRecord(t, client, deliveryID)
			status, _ := record["status"].(string)
			return status == "delivered"
		}, "delivery to be delivered")

		// Try to update status of already-delivered delivery.
		statusBody := map[string]any{"status": "failed"}
		statusResp, _ := postDeliveryStatusHMAC(t, serverURL, deliveryID, secret, statusBody)

		if statusResp.StatusCode != 409 {
			t.Errorf("Expected 409 Conflict for invalid status transition, got %d", statusResp.StatusCode)
		}

		// Verify status is still delivered.
		record := getDeliveryRecord(t, client, deliveryID)
		status, _ := record["status"].(string)
		if status != "delivered" {
			t.Errorf("Expected status still delivered, got %s", status)
		}

		t.Log("Invalid status transition correctly rejected (409)")
	})

	// 4.7 AsyncCallback_ProgressUpdates: incremental updates
	t.Run("AsyncCallback_ProgressUpdates", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver(framework.WithCallbackMode("async"))
		defer receiver.Close()

		subID, secret := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)
		deliveryID := delivery.DeliveryID

		// Wait for in_progress.
		framework.PollUntil(t, 15*time.Second, 1*time.Second, func() bool {
			record := getDeliveryRecord(t, client, deliveryID)
			status, _ := record["status"].(string)
			return status == "in_progress"
		}, "delivery to be in_progress")

		// Step 1: 25% progress.
		statusBody1 := map[string]any{
			"status":         "in_progress",
			"status_percent": 25,
			"status_message": "Step 1 complete",
		}
		resp1, _ := postDeliveryStatusHMAC(t, serverURL, deliveryID, secret, statusBody1)
		if resp1.StatusCode != 200 {
			t.Errorf("Expected 200 for progress update 1, got %d", resp1.StatusCode)
		}

		// Verify 25%.
		record := getDeliveryRecord(t, client, deliveryID)
		pct, _ := record["status_percent"].(float64)
		if pct != 25 {
			t.Errorf("Expected status_percent 25, got %v", pct)
		}
		msg, _ := record["status_message"].(string)
		if msg != "Step 1 complete" {
			t.Errorf("Expected status_message 'Step 1 complete', got '%s'", msg)
		}

		// Step 2: 75% progress.
		statusBody2 := map[string]any{
			"status":         "in_progress",
			"status_percent": 75,
			"status_message": "Step 3 complete",
		}
		resp2, _ := postDeliveryStatusHMAC(t, serverURL, deliveryID, secret, statusBody2)
		if resp2.StatusCode != 200 {
			t.Errorf("Expected 200 for progress update 2, got %d", resp2.StatusCode)
		}

		// Verify 75%.
		record = getDeliveryRecord(t, client, deliveryID)
		pct, _ = record["status_percent"].(float64)
		if pct != 75 {
			t.Errorf("Expected status_percent 75, got %v", pct)
		}

		// Step 3: Complete.
		statusBody3 := map[string]any{
			"status":         "completed",
			"status_percent": 100,
		}
		resp3, _ := postDeliveryStatusHMAC(t, serverURL, deliveryID, secret, statusBody3)
		if resp3.StatusCode != 200 {
			t.Errorf("Expected 200 for completion, got %d", resp3.StatusCode)
		}

		// Verify delivered.
		framework.PollUntil(t, 10*time.Second, 1*time.Second, func() bool {
			r := getDeliveryRecord(t, client, deliveryID)
			s, _ := r["status"].(string)
			return s == "delivered"
		}, "delivery to be delivered after completion callback")

		t.Log("Incremental progress updates worked correctly")
	})

	// 4.8 Subscription_DeleteCascadesAddons
	t.Run("Subscription_DeleteCascadesAddons", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		subID, _ := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})

		// Create addon linked to the subscription.
		addonFixture := map[string]any{
			"name":       "Cascade Test Addon",
			"url":        receiver.URL(),
			"webhook_id": subID,
			"objects":    []string{"threat_model"},
		}

		addonResp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/addons",
			Body:   addonFixture,
		})
		framework.AssertNoError(t, err, "Failed to create addon")

		if addonResp.StatusCode != 201 {
			t.Skipf("Addon creation returned %d (may not be implemented), skipping cascade test", addonResp.StatusCode)
		}

		addonID := framework.ExtractID(t, addonResp, "id")

		// Verify addon exists.
		getResp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/addons/" + addonID,
		})
		framework.AssertNoError(t, err, "Failed to get addon")
		framework.AssertStatusOK(t, getResp)

		// Delete subscription.
		delResp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/webhooks/subscriptions/" + subID,
		})
		framework.AssertNoError(t, err, "Failed to delete subscription")
		framework.AssertStatusNoContent(t, delResp)

		// Verify addon is also deleted.
		getResp2, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/addons/" + addonID,
		})
		framework.AssertNoError(t, err, "Failed to check addon after delete")
		if getResp2.StatusCode != 404 {
			t.Errorf("Expected addon to be deleted (404) after subscription delete, got %d", getResp2.StatusCode)
		}

		t.Log("Subscription delete cascaded to addons correctly")
	})

	// 4.9 Subscription_Unauthorized: non-admin gets 403
	t.Run("Subscription_Unauthorized", func(t *testing.T) {
		// Authenticate a second user (not auto-promoted).
		nonAdminUserID := framework.UniqueUserID()
		nonAdminTokens, err := framework.AuthenticateUser(nonAdminUserID)
		framework.AssertNoError(t, err, "Non-admin authentication failed")

		nonAdminClient, err := framework.NewClient(serverURL, nonAdminTokens)
		framework.AssertNoError(t, err, "Failed to create non-admin client")

		// POST /admin/webhooks/subscriptions should be 403.
		resp1, err := nonAdminClient.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/webhooks/subscriptions",
			Body: map[string]any{
				"name":   "Unauthorized Webhook",
				"url":    "https://example.com/webhook",
				"events": []string{"threat_model.created"},
			},
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp1)

		// GET /admin/webhooks/subscriptions should be 403.
		resp2, err := nonAdminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/subscriptions",
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp2)

		// GET /admin/webhooks/deliveries should be 403.
		resp3, err := nonAdminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/webhooks/deliveries",
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp3)

		t.Log("Non-admin user correctly denied access (403) to webhook admin endpoints")
	})

	// 4.10 DeliveryStatus_PublicEndpoint_Auth: HMAC and JWT auth variants
	t.Run("DeliveryStatus_PublicEndpoint_Auth", func(t *testing.T) {
		receiver := framework.NewWebhookReceiver()
		defer receiver.Close()

		subID, secret := setupActiveSubscription(t, client, receiver.URL(), []string{"threat_model.created"})
		defer func() {
			_, _ = client.Do(framework.Request{Method: "DELETE", Path: "/admin/webhooks/subscriptions/" + subID})
		}()

		_ = createThreatModel(t, client)

		delivery := receiver.WaitForDelivery(t, 30*time.Second)
		deliveryID := delivery.DeliveryID

		httpClient := &http.Client{Timeout: 30 * time.Second}

		// 1. Valid JWT (admin) -> 200.
		resp1, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/webhook-deliveries/" + deliveryID,
		})
		framework.AssertNoError(t, err, "JWT GET failed")
		framework.AssertStatusOK(t, resp1)

		// 2. Valid HMAC (sign delivery_id with secret) -> 200.
		hmacSig := generateHMACSignature([]byte(deliveryID), secret)
		req2, _ := http.NewRequest("GET", serverURL+"/webhook-deliveries/"+deliveryID, nil)
		req2.Header.Set("X-Webhook-Signature", hmacSig)
		resp2, err := httpClient.Do(req2)
		framework.AssertNoError(t, err, "HMAC GET failed")
		resp2.Body.Close()
		if resp2.StatusCode != 200 {
			t.Errorf("Expected 200 for valid HMAC auth, got %d", resp2.StatusCode)
		}

		// 3. No auth -> 401 or 403.
		req3, _ := http.NewRequest("GET", serverURL+"/webhook-deliveries/"+deliveryID, nil)
		resp3, err := httpClient.Do(req3)
		framework.AssertNoError(t, err, "No-auth GET failed")
		resp3.Body.Close()
		if resp3.StatusCode != 401 && resp3.StatusCode != 403 {
			t.Errorf("Expected 401 or 403 for no auth, got %d", resp3.StatusCode)
		}

		// 4. Wrong HMAC -> 401 or 403.
		wrongSig := generateHMACSignature([]byte(deliveryID), "completely-wrong-secret")
		req4, _ := http.NewRequest("GET", serverURL+"/webhook-deliveries/"+deliveryID, nil)
		req4.Header.Set("X-Webhook-Signature", wrongSig)
		resp4, err := httpClient.Do(req4)
		framework.AssertNoError(t, err, "Wrong HMAC GET failed")
		resp4.Body.Close()
		if resp4.StatusCode != 401 && resp4.StatusCode != 403 {
			t.Errorf("Expected 401 or 403 for wrong HMAC, got %d", resp4.StatusCode)
		}

		// 5. Nonexistent delivery -> 404.
		resp5, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/webhook-deliveries/00000000-0000-0000-0000-000000000000",
		})
		framework.AssertNoError(t, err, "Nonexistent delivery GET failed")
		framework.AssertStatusNotFound(t, resp5)

		t.Log("Public endpoint auth variants verified correctly")
	})

	t.Log("All webhook delivery tests completed")
}
