package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestUsabilityFeedback_PostThenAdminGet verifies the end-to-end flow where a regular
// user submits usability feedback and an admin can list and find it.
//
// Operations covered:
//   - POST /usability_feedback (any authenticated user)
//   - GET /usability_feedback (admin only)
func TestUsabilityFeedback_PostThenAdminGet(t *testing.T) {
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

	// Authenticate as a regular user (alice) to post feedback
	aliceTokens, err := framework.AuthenticateUser("alice")
	framework.AssertNoError(t, err, "Authentication failed for alice")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create client for alice")

	// Authenticate as admin (test-admin) to list feedback
	adminTokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed for admin")

	adminClient, err := framework.NewClient(serverURL, adminTokens)
	framework.AssertNoError(t, err, "Failed to create client for admin")

	var feedbackID string

	t.Run("Alice_PostFeedback", func(t *testing.T) {
		body := map[string]interface{}{
			"sentiment": "up",
			"surface":   "tm_list",
			"client_id": "tmi-ux",
			"verbatim":  "Integration test feedback from alice",
		}

		resp, err := aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   "/usability_feedback",
			Body:   body,
		})
		framework.AssertNoError(t, err, "Failed to POST usability_feedback")
		framework.AssertStatusCreated(t, resp)

		feedbackID = framework.ExtractID(t, resp, "id")
		framework.AssertJSONField(t, resp, "sentiment", "up")
		framework.AssertJSONField(t, resp, "surface", "tm_list")
		framework.AssertJSONField(t, resp, "client_id", "tmi-ux")
		framework.AssertValidTimestamp(t, resp, "created_at")

		t.Logf("Alice posted feedback: %s", feedbackID)
	})

	t.Run("Admin_ListFeedback_FindsAlicesRow", func(t *testing.T) {
		if feedbackID == "" {
			t.Skip("No feedback ID from previous subtest")
		}

		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/usability_feedback",
		})
		framework.AssertNoError(t, err, "Admin failed to list usability_feedback")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Items []map[string]interface{} `json:"items"`
			Total int                      `json:"total"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse usability_feedback list response")

		found := false
		for _, item := range response.Items {
			if id, ok := item["id"].(string); ok && id == feedbackID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Admin list did not contain alice's feedback %s (total=%d)", feedbackID, response.Total)
		}

		t.Logf("Admin found alice's feedback in list of %d items", response.Total)
	})
}

// TestUsabilityFeedback_NonAdminCannotList verifies that a non-admin user receives 403
// when attempting to list usability feedback.
//
// Operations covered:
//   - GET /usability_feedback (admin only — non-admin should be rejected)
func TestUsabilityFeedback_NonAdminCannotList(t *testing.T) {
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

	// Authenticate as a plain (non-admin) user
	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	// WithValidation(false) because OpenAPI validator rejects 403 bodies on this route
	client, err := framework.NewClient(serverURL, tokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create client")

	resp, err := client.Do(framework.Request{
		Method: "GET",
		Path:   "/usability_feedback",
	})
	framework.AssertNoError(t, err, "Request failed unexpectedly")
	framework.AssertStatusForbidden(t, resp)

	t.Logf("Non-admin correctly received 403 for GET /usability_feedback")
}

// TestUsabilityFeedback_PostUnauthenticated verifies that an unauthenticated request
// to POST /usability_feedback is rejected with 401.
//
// Operations covered:
//   - POST /usability_feedback (unauthenticated — should be 401)
func TestUsabilityFeedback_PostUnauthenticated(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// No OAuth stub check needed — we're not authenticating
	client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create unauthenticated client")

	body := map[string]interface{}{
		"sentiment": "up",
		"surface":   "tm_list",
		"client_id": "tmi-ux",
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/usability_feedback",
		Body:   body,
	})
	framework.AssertNoError(t, err, "Request failed unexpectedly")
	framework.AssertStatusUnauthorized(t, resp)

	t.Logf("Unauthenticated POST correctly received %d", resp.StatusCode)
}
