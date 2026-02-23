package workflows

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestTeamCRUD covers the following OpenAPI operations:
// - POST /teams (CreateTeam)
// - GET /teams (ListTeams)
// - GET /teams/{team_id} (GetTeam)
// - PUT /teams/{team_id} (UpdateTeam)
// - PATCH /teams/{team_id} (PatchTeam)
// - DELETE /teams/{team_id} (DeleteTeam)
//
// Total: 6 operations
func TestTeamCRUD(t *testing.T) {
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

	// Clear Administrators group members so first authenticated user gets auto-promoted to admin
	err = db.ExecSQL("DELETE FROM group_members WHERE group_internal_uuid = '00000000-0000-0000-0000-000000000002'")
	if err != nil {
		t.Fatalf("Failed to clear Administrators group members: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Trigger auto-promotion to admin by making first request
	resp, err := client.Do(framework.Request{Method: "GET", Path: "/me"})
	framework.AssertNoError(t, err, "Failed to get user profile")
	framework.AssertStatusOK(t, resp)
	t.Log("User auto-promoted to admin for team CRUD tests")

	var teamID string
	teamName := framework.UniqueName("Integration Test Team")

	t.Run("CreateTeam", func(t *testing.T) {
		fixture := framework.NewTeamFixture().
			WithName(teamName).
			WithDescription("Created by integration test suite").
			WithStatus("active")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/teams",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create team")
		framework.AssertStatusCreated(t, resp)

		teamID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")
		framework.AssertJSONField(t, resp, "name", teamName)
		framework.AssertJSONField(t, resp, "description", "Created by integration test suite")
		framework.AssertJSONField(t, resp, "status", "active")
		framework.AssertJSONFieldExists(t, resp, "created_at")
		framework.AssertJSONFieldExists(t, resp, "modified_at")
		framework.AssertValidTimestamp(t, resp, "created_at")
		framework.AssertValidTimestamp(t, resp, "modified_at")

		client.SaveState("team_id", teamID)
		t.Logf("Created team: %s", teamID)
	})

	t.Run("GetTeam", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/teams/" + teamID,
		})
		framework.AssertNoError(t, err, "Failed to get team")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", teamID)
		framework.AssertJSONField(t, resp, "name", teamName)
		framework.AssertJSONField(t, resp, "status", "active")
		framework.AssertValidTimestamp(t, resp, "created_at")

		t.Logf("Retrieved team: %s", teamID)
	})

	t.Run("ListTeams", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/teams",
		})
		framework.AssertNoError(t, err, "Failed to list teams")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Teams  []map[string]interface{} `json:"teams"`
			Total  int                      `json:"total"`
			Limit  int                      `json:"limit"`
			Offset int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse teams response")

		found := false
		for _, team := range response.Teams {
			if id, ok := team["id"].(string); ok && id == teamID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find team %s in list", teamID)
		}

		t.Logf("Listed %d teams (total: %d)", len(response.Teams), response.Total)
	})

	t.Run("ListTeams_NameFilter", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method:      "GET",
			Path:        "/teams",
			QueryParams: map[string]string{"name": teamName},
		})
		framework.AssertNoError(t, err, "Failed to list teams with name filter")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Teams []map[string]interface{} `json:"teams"`
			Total int                      `json:"total"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse filtered teams response")

		if response.Total < 1 {
			t.Errorf("Expected at least 1 team matching name filter, got %d", response.Total)
		}

		t.Logf("Name filter returned %d teams", response.Total)
	})

	updatedTeamName := framework.UniqueName("Updated Team")

	t.Run("UpdateTeam", func(t *testing.T) {
		updateFixture := framework.NewTeamFixture().
			WithName(updatedTeamName).
			WithDescription("Updated via PUT").
			WithStatus("active")

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/teams/" + teamID,
			Body:   updateFixture,
		})
		framework.AssertNoError(t, err, "Failed to update team")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", teamID)
		framework.AssertJSONField(t, resp, "name", updatedTeamName)
		framework.AssertJSONField(t, resp, "description", "Updated via PUT")

		t.Logf("Updated team with PUT: %s", teamID)
	})

	t.Run("PatchTeam", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/description",
				"value": "Patched via PATCH operation",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/teams/" + teamID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch team")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "description", "Patched via PATCH operation")
		framework.AssertJSONField(t, resp, "name", updatedTeamName)

		t.Logf("Patched team: %s", teamID)
	})

	t.Run("PatchTeam_ProhibitedField", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/id",
				"value": "00000000-0000-0000-0000-000000000000",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/teams/" + teamID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusBadRequest(t, resp)

		t.Log("Prohibited field patch correctly rejected")
	})

	t.Run("DeleteTeam", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/teams/" + teamID,
		})
		framework.AssertNoError(t, err, "Failed to delete team")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("Deleted team: %s", teamID)
	})

	t.Run("VerifyDeleted", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/teams/" + teamID,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("Team correctly not found after deletion")
	})

	t.Run("ErrorHandling_NotFound", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/teams/00000000-0000-0000-0000-000000000000",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("404 handling validated")
	})

	t.Run("ErrorHandling_BadRequest", func(t *testing.T) {
		// Missing required name field
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/teams",
			Body:   map[string]string{"description": "Missing name"},
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusBadRequest(t, resp)

		t.Log("400 handling validated")
	})

	t.Log("All team CRUD tests completed successfully")
}
