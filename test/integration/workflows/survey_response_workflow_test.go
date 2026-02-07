package workflows

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestSurveyResponseWorkflow covers the full survey response lifecycle:
// - POST /admin/surveys (setup: create survey)
// - POST /intake/survey_responses (CreateIntakeSurveyResponse)
// - GET /intake/survey_responses (ListIntakeSurveyResponses)
// - GET /intake/survey_responses/{survey_response_id} (GetIntakeSurveyResponse)
// - PUT /intake/survey_responses/{survey_response_id} (UpdateIntakeSurveyResponse)
// - PATCH /intake/survey_responses/{survey_response_id} (PatchIntakeSurveyResponse)
// - DELETE /intake/survey_responses/{survey_response_id} (DeleteIntakeSurveyResponse)
// - GET /triage/survey_responses (ListTriageSurveyResponses)
// - GET /triage/survey_responses/{survey_response_id} (GetTriageSurveyResponse)
// - PATCH /triage/survey_responses/{survey_response_id} (PatchTriageSurveyResponse)
//
// Total: 10 operations
func TestSurveyResponseWorkflow(t *testing.T) {
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

	// Connect to database to set up admin access
	db, err := framework.NewTestDatabase()
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer db.Close()

	// Clear administrators table so first authenticated user gets auto-promoted to admin
	err = db.TruncateTable("administrators")
	if err != nil {
		t.Fatalf("Failed to truncate administrators table: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Trigger auto-promotion to admin by making first request
	meResp, err := client.Do(framework.Request{Method: "GET", Path: "/me"})
	framework.AssertNoError(t, err, "Failed to get user profile")
	framework.AssertStatusOK(t, meResp)

	// Extract user email for authorization entries
	var meData map[string]interface{}
	err = json.Unmarshal(meResp.Body, &meData)
	framework.AssertNoError(t, err, "Failed to parse user profile")
	userEmail, _ := meData["email"].(string)
	userProvider, _ := meData["provider"].(string)
	t.Logf("User auto-promoted to admin: %s (%s)", userEmail, userProvider)

	var surveyID string
	var responseID string

	// Setup: create a survey to respond to
	t.Run("Setup_CreateSurvey", func(t *testing.T) {
		fixture := framework.NewSurveyFixture().
			WithName(framework.UniqueName("Response Test Survey")).
			WithStatus("active")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/surveys",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create survey")
		framework.AssertStatusCreated(t, resp)

		surveyID = framework.ExtractID(t, resp, "id")
		client.SaveState("survey_id", surveyID)

		t.Logf("Setup: created survey %s", surveyID)
	})

	// Build authorization entry for the authenticated user
	userAuth := []map[string]interface{}{
		{
			"principal_type": "user",
			"provider":       userProvider,
			"provider_id":    userEmail,
			"role":           "owner",
		},
	}

	t.Run("CreateResponse", func(t *testing.T) {
		fixture := framework.NewSurveyResponseFixture(surveyID).
			WithAuthorization(userAuth)

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/intake/survey_responses",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create survey response")
		framework.AssertStatusCreated(t, resp)

		responseID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")
		framework.AssertJSONField(t, resp, "survey_id", surveyID)
		framework.AssertJSONField(t, resp, "status", "draft")
		framework.AssertJSONFieldExists(t, resp, "created_at")
		framework.AssertJSONFieldExists(t, resp, "modified_at")
		framework.AssertJSONFieldExists(t, resp, "survey_json")
		framework.AssertJSONFieldExists(t, resp, "survey_version")

		client.SaveState("response_id", responseID)

		t.Logf("Created survey response: %s", responseID)
	})

	t.Run("GetResponse", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/intake/survey_responses/" + responseID,
		})
		framework.AssertNoError(t, err, "Failed to get survey response")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", responseID)
		framework.AssertJSONField(t, resp, "survey_id", surveyID)
		framework.AssertJSONField(t, resp, "status", "draft")
		framework.AssertJSONFieldExists(t, resp, "authorization")

		t.Logf("Retrieved survey response: %s", responseID)
	})

	t.Run("ListResponses", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/intake/survey_responses",
		})
		framework.AssertNoError(t, err, "Failed to list survey responses")
		framework.AssertStatusOK(t, resp)

		var response struct {
			SurveyResponses []map[string]interface{} `json:"survey_responses"`
			Total           int                      `json:"total"`
			Limit           int                      `json:"limit"`
			Offset          int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse survey responses")

		found := false
		for _, r := range response.SurveyResponses {
			if id, ok := r["id"].(string); ok && id == responseID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find response %s in list", responseID)
		}

		t.Logf("Listed %d survey responses (total: %d)", len(response.SurveyResponses), response.Total)
	})

	t.Run("UpdateResponse", func(t *testing.T) {
		updatePayload := map[string]interface{}{
			"survey_id":     surveyID,
			"authorization": userAuth,
			"answers": map[string]interface{}{
				"project_name":        "Updated Project Name",
				"project_description": "Updated project description",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/intake/survey_responses/" + responseID,
			Body:   updatePayload,
		})
		framework.AssertNoError(t, err, "Failed to update survey response")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", responseID)

		t.Logf("Updated survey response: %s", responseID)
	})

	t.Run("PatchResponse_UpdateAnswers", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":   "replace",
				"path": "/answers",
				"value": map[string]interface{}{
					"project_name":        "Patched Project",
					"project_description": "Patched via JSON Patch",
				},
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/intake/survey_responses/" + responseID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch survey response")
		framework.AssertStatusOK(t, resp)

		t.Logf("Patched survey response answers: %s", responseID)
	})

	t.Run("PatchResponse_SubmitForReview", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/status",
				"value": "submitted",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/intake/survey_responses/" + responseID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to submit survey response")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "status", "submitted")

		t.Logf("Submitted survey response for review: %s", responseID)
	})

	t.Run("ListTriageResponses", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/triage/survey_responses",
		})
		framework.AssertNoError(t, err, "Failed to list triage survey responses")
		framework.AssertStatusOK(t, resp)

		var response struct {
			SurveyResponses []map[string]interface{} `json:"survey_responses"`
			Total           int                      `json:"total"`
			Limit           int                      `json:"limit"`
			Offset          int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse triage responses")

		t.Logf("Listed %d triage survey responses (total: %d)", len(response.SurveyResponses), response.Total)
	})

	t.Run("GetTriageResponse", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/triage/survey_responses/" + responseID,
		})
		framework.AssertNoError(t, err, "Failed to get triage survey response")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", responseID)
		framework.AssertJSONField(t, resp, "status", "submitted")

		t.Logf("Retrieved triage survey response: %s", responseID)
	})

	t.Run("PatchTriageResponse_MarkReadyForReview", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/status",
				"value": "ready_for_review",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/triage/survey_responses/" + responseID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch triage survey response")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "status", "ready_for_review")

		t.Logf("Marked survey response as ready_for_review: %s", responseID)
	})

	// Create a second response for delete testing (only draft responses can be deleted)
	var draftResponseID string

	t.Run("CreateDraftResponse_ForDelete", func(t *testing.T) {
		fixture := framework.NewSurveyResponseFixture(surveyID).
			WithAuthorization(userAuth)

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/intake/survey_responses",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create draft response")
		framework.AssertStatusCreated(t, resp)

		draftResponseID = framework.ExtractID(t, resp, "id")

		t.Logf("Created draft response for delete test: %s", draftResponseID)
	})

	t.Run("DeleteDraftResponse", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/intake/survey_responses/" + draftResponseID,
		})
		framework.AssertNoError(t, err, "Failed to delete draft response")
		framework.AssertStatusNoContent(t, resp)

		// Verify deleted
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/intake/survey_responses/" + draftResponseID,
		})
		framework.AssertStatusNotFound(t, resp)

		t.Logf("Deleted draft response: %s", draftResponseID)
	})

	t.Run("ErrorHandling_InvalidSurveyId", func(t *testing.T) {
		fixture := map[string]interface{}{
			"survey_id":     "00000000-0000-0000-0000-000000000000",
			"authorization": userAuth,
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/intake/survey_responses",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusBadRequest(t, resp)

		t.Log("Invalid survey_id correctly rejected")
	})

	// Cleanup
	t.Run("Cleanup_DeleteSurvey", func(t *testing.T) {
		// Note: survey with responses cannot be deleted, so this tests the conflict case
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/surveys/" + surveyID,
		})
		framework.AssertNoError(t, err, "Request failed")
		// Should get 409 since the submitted response still exists
		framework.AssertStatusCode(t, resp, 409)

		t.Log("Survey with responses correctly cannot be deleted")
	})

	t.Log("All survey response workflow tests completed successfully")
}
