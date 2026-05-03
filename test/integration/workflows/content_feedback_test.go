package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestContentFeedback_PostThenGet_Reader verifies the end-to-end flow where a
// threat model owner creates a threat, posts feedback targeting it, then retrieves
// both the individual feedback record and the list.
//
// Operations covered:
//   - POST /threat_models
//   - POST /threat_models/{id}/threats
//   - POST /threat_models/{id}/feedback
//   - GET  /threat_models/{id}/feedback/{feedback_id}
//   - GET  /threat_models/{id}/feedback
func TestContentFeedback_PostThenGet_Reader(t *testing.T) {
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

	tokens, err := framework.AuthenticateUser("alice")
	framework.AssertNoError(t, err, "Authentication failed for alice")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create client for alice")

	var tmID, threatID, feedbackID string

	t.Run("Setup_CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture().
			WithName("Content Feedback Test TM").
			WithDescription("TM for content feedback integration tests")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		tmID = framework.ExtractID(t, resp, "id")
		t.Logf("Created threat model: %s", tmID)
	})

	t.Run("Setup_CreateThreat", func(t *testing.T) {
		if tmID == "" {
			t.Skip("No threat model ID from previous subtest")
		}

		fixture := framework.NewThreatFixture().WithName("SQL Injection via login form")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/threats",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat")
		framework.AssertStatusCreated(t, resp)
		threatID = framework.ExtractID(t, resp, "id")
		t.Logf("Created threat: %s", threatID)
	})

	t.Run("PostContentFeedback", func(t *testing.T) {
		if tmID == "" || threatID == "" {
			t.Skip("Missing prerequisite IDs from earlier subtests")
		}

		body := map[string]interface{}{
			"sentiment":   "up",
			"target_type": "threat",
			"target_id":   threatID,
			"client_id":   "tmi-ux",
			"verbatim":    "This threat is well described",
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/feedback",
			Body:   body,
		})
		framework.AssertNoError(t, err, "Failed to POST content feedback")
		framework.AssertStatusCreated(t, resp)

		feedbackID = framework.ExtractID(t, resp, "id")
		framework.AssertJSONField(t, resp, "sentiment", "up")
		framework.AssertJSONField(t, resp, "target_type", "threat")
		framework.AssertJSONField(t, resp, "client_id", "tmi-ux")
		framework.AssertValidTimestamp(t, resp, "created_at")
		t.Logf("Posted content feedback: %s", feedbackID)
	})

	t.Run("GetContentFeedback", func(t *testing.T) {
		if tmID == "" || feedbackID == "" {
			t.Skip("Missing prerequisite IDs from earlier subtests")
		}

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID + "/feedback/" + feedbackID,
		})
		framework.AssertNoError(t, err, "Failed to GET content feedback")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", feedbackID)
		framework.AssertJSONField(t, resp, "sentiment", "up")
		framework.AssertJSONField(t, resp, "target_type", "threat")
		framework.AssertJSONField(t, resp, "client_id", "tmi-ux")
		t.Logf("Retrieved content feedback: %s", feedbackID)
	})

	t.Run("ListContentFeedback_ReturnsOne", func(t *testing.T) {
		if tmID == "" || feedbackID == "" {
			t.Skip("Missing prerequisite IDs from earlier subtests")
		}

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID + "/feedback",
		})
		framework.AssertNoError(t, err, "Failed to list content feedback")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Items []map[string]interface{} `json:"items"`
			Total int                      `json:"total"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse content feedback list response")

		if response.Total < 1 {
			t.Errorf("Expected at least 1 feedback item, got total=%d", response.Total)
		}

		found := false
		for _, item := range response.Items {
			if id, ok := item["id"].(string); ok && id == feedbackID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find feedback %s in list (total=%d)", feedbackID, response.Total)
		}

		t.Logf("List returned %d item(s), found our feedback", response.Total)
	})

	t.Run("Cleanup_DeleteThreatModel", func(t *testing.T) {
		if tmID == "" {
			t.Skip("No threat model to delete")
		}

		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Deleted threat model: %s", tmID)
	})
}

// TestContentFeedback_NonMemberRejected verifies that a user with no role on a
// threat model is rejected when attempting to post feedback.
//
// TMI uses existence-leak prevention: a non-member may receive either 403 or 404.
// Both are acceptable per the TMI authorization pattern.
//
// Operations covered:
//   - POST /threat_models (alice)
//   - POST /threat_models/{id}/feedback (bob — no role → 403 or 404)
func TestContentFeedback_NonMemberRejected(t *testing.T) {
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

	// Alice creates a threat model
	aliceTokens, err := framework.AuthenticateUser("alice")
	framework.AssertNoError(t, err, "Authentication failed for alice")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create client for alice")

	// Bob is a separate user with no role on alice's TM
	bobID := framework.UniqueUserID()
	bobTokens, err := framework.AuthenticateUser(bobID)
	framework.AssertNoError(t, err, "Authentication failed for bob")

	// Bob uses WithValidation(false) because 403/404 responses may not match
	// the OpenAPI response schema for this endpoint.
	bobClient, err := framework.NewClient(serverURL, bobTokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create client for bob")

	var tmID string

	t.Run("Alice_CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture().
			WithName("NonMember Rejection Test TM")

		resp, err := aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		tmID = framework.ExtractID(t, resp, "id")
		t.Logf("Alice created threat model: %s", tmID)
	})

	t.Run("Bob_PostFeedback_Rejected", func(t *testing.T) {
		if tmID == "" {
			t.Skip("No threat model ID from previous subtest")
		}

		body := map[string]interface{}{
			"sentiment":   "up",
			"target_type": "threat",
			// Use a plausible but nonexistent target ID — the auth gate fires first
			"target_id": "00000000-0000-0000-0000-000000000099",
			"client_id": "tmi-ux",
		}

		resp, err := bobClient.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/feedback",
			Body:   body,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		// TMI uses existence-leak prevention: non-members get 403 or 404
		if resp.StatusCode != 403 && resp.StatusCode != 404 {
			t.Errorf("Expected 403 or 404 for non-member posting feedback, got %d\nBody: %s",
				resp.StatusCode, string(resp.Body))
		} else {
			t.Logf("Bob correctly received %d for POST to alice's TM feedback", resp.StatusCode)
		}
	})

	t.Run("Cleanup_DeleteThreatModel", func(t *testing.T) {
		if tmID == "" {
			t.Skip("No threat model to delete")
		}

		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
	})
}

// TestContentFeedback_TargetIDFromOtherTM_Rejected verifies that posting feedback
// with a target_id that belongs to a different threat model is rejected with 400.
//
// Operations covered:
//   - POST /threat_models (×2)
//   - POST /threat_models/{tmA}/threats
//   - POST /threat_models/{tmB}/feedback with target_id from tmA → 400
func TestContentFeedback_TargetIDFromOtherTM_Rejected(t *testing.T) {
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

	tokens, err := framework.AuthenticateUser("alice")
	framework.AssertNoError(t, err, "Authentication failed for alice")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create client for alice")

	var tmAID, tmBID, threatInAID string

	t.Run("Setup_CreateTMsAndThreat", func(t *testing.T) {
		// Create TM A
		respA, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   framework.NewThreatModelFixture().WithName("CrossTM Test TM-A"),
		})
		framework.AssertNoError(t, err, "Failed to create TM A")
		framework.AssertStatusCreated(t, respA)
		tmAID = framework.ExtractID(t, respA, "id")

		// Create TM B
		respB, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   framework.NewThreatModelFixture().WithName("CrossTM Test TM-B"),
		})
		framework.AssertNoError(t, err, "Failed to create TM B")
		framework.AssertStatusCreated(t, respB)
		tmBID = framework.ExtractID(t, respB, "id")

		// Create a threat in TM A
		respThreat, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmAID + "/threats",
			Body:   framework.NewThreatFixture().WithName("Threat in TM-A"),
		})
		framework.AssertNoError(t, err, "Failed to create threat in TM A")
		framework.AssertStatusCreated(t, respThreat)
		threatInAID = framework.ExtractID(t, respThreat, "id")

		t.Logf("Setup: TM-A=%s, TM-B=%s, threat-in-A=%s", tmAID, tmBID, threatInAID)
	})

	t.Run("PostFeedback_CrossTM_TargetID_Rejected", func(t *testing.T) {
		if tmAID == "" || tmBID == "" || threatInAID == "" {
			t.Skip("Missing prerequisite IDs from earlier subtest")
		}

		// Post to TM B's feedback endpoint but reference a threat that belongs to TM A
		body := map[string]interface{}{
			"sentiment":   "up",
			"target_type": "threat",
			"target_id":   threatInAID, // threat is in TM A, not TM B
			"client_id":   "tmi-ux",
		}

		// Use WithValidation(false) because the 400 error body may not match the
		// OpenAPI success schema for this endpoint.
		crossClient, err := framework.NewClient(serverURL, tokens, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create cross-TM client")

		resp, err := crossClient.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmBID + "/feedback",
			Body:   body,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusBadRequest(t, resp)

		t.Logf("Cross-TM target_id correctly rejected with 400")
	})

	t.Run("Cleanup", func(t *testing.T) {
		for _, id := range []string{tmAID, tmBID} {
			if id == "" {
				continue
			}
			client.Do(framework.Request{Method: "DELETE", Path: "/threat_models/" + id}) //nolint:errcheck
		}
	})
}

// TestContentFeedback_CascadeOnTMDelete verifies that deleting a threat model
// makes its feedback inaccessible (parent route returns 404), confirming the
// cascade semantics from the owning TM.
//
// Operations covered:
//   - POST /threat_models
//   - POST /threat_models/{id}/threats
//   - POST /threat_models/{id}/feedback
//   - DELETE /threat_models/{id}
//   - GET /threat_models/{id}/feedback → 404 (TM gone)
func TestContentFeedback_CascadeOnTMDelete(t *testing.T) {
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

	tokens, err := framework.AuthenticateUser("alice")
	framework.AssertNoError(t, err, "Authentication failed for alice")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create client for alice")

	// WithValidation(false) used for post-delete requests that return 404 — the
	// OpenAPI validator does not expect 404 on GET /threat_models/{id}/feedback.
	noValClient, err := framework.NewClient(serverURL, tokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create no-validation client")

	var tmID, threatID, feedbackID string

	t.Run("Setup_CreateResources", func(t *testing.T) {
		// Create TM
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   framework.NewThreatModelFixture().WithName("Cascade Delete Test TM"),
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		tmID = framework.ExtractID(t, resp, "id")

		// Create threat
		resp, err = client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/threats",
			Body:   framework.NewThreatFixture().WithName("Cascade test threat"),
		})
		framework.AssertNoError(t, err, "Failed to create threat")
		framework.AssertStatusCreated(t, resp)
		threatID = framework.ExtractID(t, resp, "id")

		// Post feedback
		resp, err = client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/feedback",
			Body: map[string]interface{}{
				"sentiment":   "down",
				"target_type": "threat",
				"target_id":   threatID,
				"client_id":   "tmi-ux",
				"verbatim":    "Cascade delete test",
			},
		})
		framework.AssertNoError(t, err, "Failed to post content feedback")
		framework.AssertStatusCreated(t, resp)
		feedbackID = framework.ExtractID(t, resp, "id")

		t.Logf("Setup: TM=%s threat=%s feedback=%s", tmID, threatID, feedbackID)
	})

	t.Run("DeleteThreatModel", func(t *testing.T) {
		if tmID == "" {
			t.Skip("No threat model to delete")
		}

		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Deleted threat model: %s", tmID)
	})

	t.Run("FeedbackRoute_Returns_404_After_TMDelete", func(t *testing.T) {
		if tmID == "" || feedbackID == "" {
			t.Skip("Missing prerequisite IDs from earlier subtests")
		}

		// The TM is deleted — the list endpoint should return 404 because the
		// TM no longer exists (and the middleware gate fires before the handler).
		resp, err := noValClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID + "/feedback",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Logf("GET /threat_models/%s/feedback correctly returns 404 after TM deletion", tmID)
	})
}
