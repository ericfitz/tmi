package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestAuditAlertSinkE2E covers the end-to-end acceptance criterion for #395:
// a PUT /admin/settings write must BOTH:
//   (a) persist an in-band system audit row (GET /admin/audit/system?field_path=<key>), AND
//   (b) fire a system_audit.admin_write webhook event delivered to every active
//       subscription that declared the event type (convenience-feed path).
//
// This test cannot reconfigure the running server's operator config, so it
// covers the T7 pinned-subscription path at the unit level (Tasks 4/5) and
// exercises the full pipeline here via an admin-created subscription.
//
// Flow:
//  1. Authenticate as charlie (admin).
//  2. Start a local webhook receiver; subscribe it to system_audit.admin_write.
//  3. Wait for the subscription to become active (challenge handshake).
//  4. PUT /admin/settings/<unique-key> → 200.
//  5. Poll receiver ≤30 s; assert envelope fields.
//  6. Verify X-Webhook-Signature HMAC with the subscription secret.
//  7. GET /admin/audit/system?field_path=<key> → ≥1 row.
//  8. Cleanup: delete subscription, delete setting.
func TestAuditAlertSinkE2E(t *testing.T) {
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

	// Authenticate as charlie (admin). Admin access is required to:
	// - POST /admin/webhooks/subscriptions
	// - PUT /admin/settings/<key>
	// - GET /admin/audit/system
	adminTokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Admin authentication failed")

	// Disable OpenAPI response validation: the new audit list endpoints may
	// not be reflected in the embedded spec of the running binary if it was
	// built before the most recent codegen. Status-code and field assertions
	// cover correctness; response-body schema validation adds no value here.
	client, err := framework.NewClient(serverURL, adminTokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Use a unique key so parallel/repeated runs don't collide in audit logs.
	settingKey := fmt.Sprintf("test.alertsink.%s", uuid.New().String()[:8])
	// The admin-audit middleware constructs the field_path as "system_settings.<key>"
	// (see api/admin_audit_descriptors.go). The webhook payload and the audit list
	// filter both use this prefixed form.
	auditFieldPath := "system_settings." + settingKey

	// Clean up: delete the test setting when the test completes.
	t.Cleanup(func() {
		_, _ = client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/settings/" + settingKey,
		})
	})

	// ----------------------------------------------------------------
	// Step 1: Verify admin access.
	// ----------------------------------------------------------------
	t.Run("CheckAdminAccess", func(t *testing.T) {
		resp, err := client.Do(framework.Request{Method: "GET", Path: "/me"})
		framework.AssertNoError(t, err, "GET /me failed")
		framework.AssertStatusOK(t, resp)

		var user map[string]any
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &user), "parse /me")
		if isAdmin, _ := user["is_admin"].(bool); !isAdmin {
			t.Fatal("Authenticated user is not admin — test requires admin role")
		}
		t.Log("User is admin")
	})

	// ----------------------------------------------------------------
	// Step 2: Create a webhook receiver and subscribe to system_audit.admin_write.
	// The receiver uses the standard framework helper which:
	//   - binds on 0.0.0.0 (reachable from k8s pod via host.docker.internal)
	//   - defaults to ChallengeAutoRespond
	//   - advertises http://host.docker.internal:<PORT> (works because the k8s
	//     deployment sets TMI_WEBHOOK_ALLOW_HTTP_TARGETS=true and
	//     TMI_SSRF_WEBHOOK_ALLOWLIST=host.docker.internal)
	// ----------------------------------------------------------------
	if err := framework.ClearRateLimits(); err != nil {
		t.Logf("Warning: failed to clear rate limits: %v", err)
	}

	receiver := framework.NewWebhookReceiver()
	t.Cleanup(func() { receiver.Close() })

	// setupActiveSubscription (defined in webhook_delivery_test.go, same package)
	// creates the subscription, extracts the secret, and polls until active.
	subID, secret := setupActiveSubscription(t, client, receiver.URL(), []string{"system_audit.admin_write"})
	t.Logf("Subscription %s active, receiver at %s", subID, receiver.URL())

	// ----------------------------------------------------------------
	// Step 3: Trigger a system audit write — PUT /admin/settings/<key>.
	// The admin-audit middleware will call SystemAuditRepository.Create,
	// and the alerting decorator (Task 3) will emit a
	// system_audit.admin_write event onto the Redis Stream. The webhook
	// event consumer picks it up and creates a delivery record; the
	// delivery worker sends the POST to our receiver.
	// ----------------------------------------------------------------
	t.Run("TriggerAdminWrite", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/admin/settings/" + settingKey,
			Body: map[string]any{
				"value": "alert-sink-test-value",
				"type":  "string",
			},
		})
		framework.AssertNoError(t, err, "PUT /admin/settings failed")
		if resp.StatusCode != 200 {
			t.Fatalf("PUT /admin/settings/%s: got %d, want 200; body: %s",
				settingKey, resp.StatusCode, string(resp.Body))
		}
		t.Logf("PUT /admin/settings/%s → 200", settingKey)
	})

	// ----------------------------------------------------------------
	// Step 4: Poll the receiver for the webhook delivery (≤30 s).
	// ----------------------------------------------------------------
	t.Run("WebhookDeliveryReceived", func(t *testing.T) {
		// WaitForDelivery already fails the test on timeout.
		delivery := receiver.WaitForDelivery(t, 30*time.Second)

		// --- 4a: event type header ---
		if delivery.EventType != "system_audit.admin_write" {
			t.Errorf("X-Webhook-Event: got %q, want %q", delivery.EventType, "system_audit.admin_write")
		}

		// --- 4b: parse envelope ---
		var envelope map[string]any
		if err := json.Unmarshal(delivery.Body, &envelope); err != nil {
			t.Fatalf("failed to parse webhook envelope JSON: %v", err)
		}

		// event_type in the body
		if et, _ := envelope["event_type"].(string); et != "system_audit.admin_write" {
			t.Errorf("envelope.event_type: got %q, want %q", et, "system_audit.admin_write")
		}

		// data sub-object
		data, ok := envelope["data"].(map[string]any)
		if !ok || data == nil {
			t.Fatalf("envelope.data is missing or not an object: %v", envelope["data"])
		}

		// entry_id must be a non-empty string (UUID from the decorator)
		entryID, _ := data["entry_id"].(string)
		if entryID == "" {
			t.Errorf("envelope.data.entry_id is empty (expected a UUID)")
		}
		t.Logf("entry_id: %s", entryID)

		// actor_email — charlie's email per the login-hint convention
		charlieEmail := "test-admin@tmi.local"
		if ae, _ := data["actor_email"].(string); ae != charlieEmail {
			t.Errorf("envelope.data.actor_email: got %q, want %q", ae, charlieEmail)
		}

		// field_path must be the prefixed form "system_settings.<key>"
		// (the admin-audit middleware prepends "system_settings." to the setting key)
		if fp, _ := data["field_path"].(string); fp != auditFieldPath {
			t.Errorf("envelope.data.field_path: got %q, want %q", fp, auditFieldPath)
		}

		// --- 4c: HMAC signature verification ---
		if delivery.Signature == "" {
			t.Error("X-Webhook-Signature header is missing")
		} else {
			expectedSig := generateHMACSignature(delivery.Body, secret)
			if delivery.Signature != expectedSig {
				t.Errorf("HMAC mismatch: header=%q expected=%q", delivery.Signature, expectedSig)
			} else {
				t.Log("HMAC signature verified")
			}
			// Verify that a wrong secret fails.
			if verifyHMACSignature(delivery.Body, delivery.Signature, "wrong-secret") {
				t.Error("HMAC should NOT verify with wrong secret")
			}
		}

		t.Logf("Webhook delivery verified: event_type=%s entry_id=%s actor=%s field_path=%s",
			delivery.EventType, entryID, data["actor_email"], data["field_path"])
	})

	// ----------------------------------------------------------------
	// Step 5: Verify the in-band audit row also exists (both paths must
	// fire per the #395 acceptance criterion).
	// GET /admin/audit/system?field_path=<key> → ≥1 row.
	// ----------------------------------------------------------------
	t.Run("InBandAuditRowExists", func(t *testing.T) {
		var result struct {
			Entries []map[string]any `json:"entries"`
			Total   float64          `json:"total"`
		}

		// The admin-audit middleware writes synchronously, so the row should
		// already be present. Poll briefly (5 s) in case of any write delay.
		deadline := time.Now().Add(5 * time.Second)
		for {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/admin/audit/system",
				QueryParams: map[string]string{
					"field_path": auditFieldPath,
				},
			})
			framework.AssertNoError(t, err, "GET /admin/audit/system failed")
			if resp.StatusCode != 200 {
				t.Fatalf("GET /admin/audit/system?field_path=%s: got %d, want 200; body: %s",
					auditFieldPath, resp.StatusCode, string(resp.Body))
			}
			framework.AssertNoError(t, json.Unmarshal(resp.Body, &result), "parse audit list")

			if result.Total >= 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("Expected ≥1 system audit entry for field_path=%s after 5 s; got 0", auditFieldPath)
			}
			time.Sleep(500 * time.Millisecond)
		}

		// Verify the returned entry looks correct.
		if len(result.Entries) == 0 {
			t.Fatal("No entries returned despite total >= 1")
		}
		entry := result.Entries[0]

		if fp, _ := entry["field_path"].(string); fp != auditFieldPath {
			t.Errorf("audit entry field_path: got %q, want %q", fp, auditFieldPath)
		}
		if hm, _ := entry["http_method"].(string); hm != "PUT" {
			t.Errorf("audit entry http_method: got %q, want PUT", hm)
		}

		t.Logf("In-band audit row verified: field_path=%s http_method=%s total=%.0f",
			auditFieldPath, entry["http_method"], result.Total)
		t.Log("#395 acceptance criterion satisfied: BOTH the audit row and the webhook alert fired")
	})
}
