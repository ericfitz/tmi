package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestSurveyCRUD covers the following OpenAPI operations:
// - POST /admin/surveys (CreateAdminSurvey)
// - GET /admin/surveys (ListAdminSurveys)
// - GET /admin/surveys/{survey_id} (GetAdminSurvey)
// - PUT /admin/surveys/{survey_id} (UpdateAdminSurvey)
// - PATCH /admin/surveys/{survey_id} (PatchAdminSurvey)
// - DELETE /admin/surveys/{survey_id} (DeleteAdminSurvey)
// - GET /intake/surveys (ListIntakeSurveys)
// - GET /intake/surveys/{survey_id} (GetIntakeSurvey)
//
// Total: 8 operations
func TestSurveyCRUD(t *testing.T) {
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

	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	var surveyID string

	t.Run("CreateSurvey", func(t *testing.T) {
		fixture := framework.NewSurveyFixture().
			WithName("Integration Test Survey").
			WithDescription("Created by integration test suite").
			WithVersion("1.0").
			WithStatus("active")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/surveys",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create survey")
		framework.AssertStatusCreated(t, resp)

		surveyID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")
		framework.AssertJSONField(t, resp, "name", "Integration Test Survey")
		framework.AssertJSONField(t, resp, "description", "Created by integration test suite")
		framework.AssertJSONField(t, resp, "version", "1.0")
		framework.AssertJSONField(t, resp, "status", "active")
		framework.AssertJSONFieldExists(t, resp, "created_at")
		framework.AssertJSONFieldExists(t, resp, "modified_at")
		framework.AssertJSONFieldExists(t, resp, "survey_json")
		framework.AssertValidTimestamp(t, resp, "created_at")
		framework.AssertValidTimestamp(t, resp, "modified_at")

		client.SaveState("survey_id", surveyID)
		t.Logf("Created survey: %s", surveyID)
	})

	t.Run("GetSurvey", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/surveys/" + surveyID,
		})
		framework.AssertNoError(t, err, "Failed to get survey")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", surveyID)
		framework.AssertJSONField(t, resp, "name", "Integration Test Survey")
		framework.AssertJSONField(t, resp, "status", "active")
		framework.AssertValidTimestamp(t, resp, "created_at")

		t.Logf("Retrieved survey: %s", surveyID)
	})

	t.Run("ListAdminSurveys", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/surveys",
		})
		framework.AssertNoError(t, err, "Failed to list surveys")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Surveys []map[string]interface{} `json:"surveys"`
			Total   int                      `json:"total"`
			Limit   int                      `json:"limit"`
			Offset  int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse surveys response")

		found := false
		for _, s := range response.Surveys {
			if id, ok := s["id"].(string); ok && id == surveyID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find survey %s in list", surveyID)
		}

		t.Logf("Listed %d surveys (total: %d)", len(response.Surveys), response.Total)
	})

	t.Run("ListIntakeSurveys", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/intake/surveys",
		})
		framework.AssertNoError(t, err, "Failed to list intake surveys")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Surveys []map[string]interface{} `json:"surveys"`
			Total   int                      `json:"total"`
			Limit   int                      `json:"limit"`
			Offset  int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse intake surveys response")

		// Our active survey should appear in intake list
		found := false
		for _, s := range response.Surveys {
			if id, ok := s["id"].(string); ok && id == surveyID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find active survey %s in intake list", surveyID)
		}

		t.Logf("Listed %d intake surveys (total: %d)", len(response.Surveys), response.Total)
	})

	t.Run("GetIntakeSurvey", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/intake/surveys/" + surveyID,
		})
		framework.AssertNoError(t, err, "Failed to get intake survey")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", surveyID)
		framework.AssertJSONField(t, resp, "status", "active")

		t.Logf("Retrieved intake survey: %s", surveyID)
	})

	t.Run("UpdateSurvey", func(t *testing.T) {
		updateFixture := framework.NewSurveyFixture().
			WithName("Updated Survey").
			WithDescription("Updated via PUT").
			WithVersion("2.0").
			WithStatus("active")

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/admin/surveys/" + surveyID,
			Body:   updateFixture,
		})
		framework.AssertNoError(t, err, "Failed to update survey")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", surveyID)
		framework.AssertJSONField(t, resp, "name", "Updated Survey")
		framework.AssertJSONField(t, resp, "description", "Updated via PUT")
		framework.AssertJSONField(t, resp, "version", "2.0")

		t.Logf("Updated survey with PUT: %s", surveyID)
	})

	t.Run("PatchSurvey", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/description",
				"value": "Patched via PATCH operation",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/admin/surveys/" + surveyID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch survey")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "description", "Patched via PATCH operation")
		framework.AssertJSONField(t, resp, "name", "Updated Survey")

		t.Logf("Patched survey: %s", surveyID)
	})

	t.Run("PatchSurveyStatus", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/status",
				"value": "inactive",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/admin/surveys/" + surveyID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch survey status")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "status", "inactive")

		t.Logf("Patched survey status to inactive: %s", surveyID)
	})

	t.Run("InactiveSurveyNotInIntake", func(t *testing.T) {
		// Inactive survey should return 404 on intake endpoint
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/intake/surveys/" + surveyID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusNotFound(t, resp)

		t.Log("Inactive survey correctly not found in intake")
	})

	t.Run("DeleteSurvey", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/surveys/" + surveyID,
		})
		framework.AssertNoError(t, err, "Failed to delete survey")
		framework.AssertStatusNoContent(t, resp)

		// Verify deleted
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/surveys/" + surveyID,
		})
		framework.AssertStatusNotFound(t, resp)

		t.Logf("Deleted survey: %s", surveyID)
	})

	t.Run("ErrorHandling_NotFound", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/surveys/00000000-0000-0000-0000-000000000000",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("404 handling validated")
	})

	t.Run("ErrorHandling_BadRequest", func(t *testing.T) {
		// Missing required survey_json field
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/surveys",
			Body:   map[string]string{"name": "Bad Survey"},
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusBadRequest(t, resp)

		t.Log("400 handling validated")
	})

	t.Log("All survey CRUD tests completed successfully")
}
