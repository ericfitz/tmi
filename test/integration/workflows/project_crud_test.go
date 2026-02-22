package workflows

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestProjectCRUD covers the following OpenAPI operations:
// - POST /projects (CreateProject)
// - GET /projects (ListProjects)
// - GET /projects/{project_id} (GetProject)
// - PUT /projects/{project_id} (UpdateProject)
// - PATCH /projects/{project_id} (PatchProject)
// - DELETE /projects/{project_id} (DeleteProject)
//
// Also verifies:
// - Projects require a valid team_id
// - Delete team blocked while projects reference it (409)
//
// Total: 6 operations
func TestProjectCRUD(t *testing.T) {
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
	t.Log("User auto-promoted to admin for project CRUD tests")

	var teamID string
	var projectID string
	projectName := framework.UniqueName("Integration Test Project")

	// Create a team first since projects require a team
	t.Run("SetupTeam", func(t *testing.T) {
		teamFixture := framework.NewTeamFixture().
			WithName(framework.UniqueName("Project Test Team")).
			WithDescription("Team for project CRUD tests").
			WithStatus("active")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/teams",
			Body:   teamFixture,
		})
		framework.AssertNoError(t, err, "Failed to create team for project tests")
		framework.AssertStatusCreated(t, resp)

		teamID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")

		client.SaveState("team_id", teamID)
		t.Logf("Created team for project tests: %s", teamID)
	})

	t.Run("CreateProject", func(t *testing.T) {
		fixture := framework.NewProjectFixture(teamID).
			WithName(projectName).
			WithDescription("Created by integration test suite").
			WithStatus("active")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/projects",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create project")
		framework.AssertStatusCreated(t, resp)

		projectID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")
		framework.AssertJSONField(t, resp, "name", projectName)
		framework.AssertJSONField(t, resp, "description", "Created by integration test suite")
		framework.AssertJSONField(t, resp, "status", "active")
		framework.AssertJSONFieldExists(t, resp, "created_at")
		framework.AssertJSONFieldExists(t, resp, "modified_at")
		framework.AssertValidTimestamp(t, resp, "created_at")
		framework.AssertValidTimestamp(t, resp, "modified_at")

		client.SaveState("project_id", projectID)
		t.Logf("Created project: %s", projectID)
	})

	t.Run("GetProject", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/projects/" + projectID,
		})
		framework.AssertNoError(t, err, "Failed to get project")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", projectID)
		framework.AssertJSONField(t, resp, "name", projectName)
		framework.AssertJSONField(t, resp, "status", "active")
		framework.AssertValidTimestamp(t, resp, "created_at")

		t.Logf("Retrieved project: %s", projectID)
	})

	t.Run("ListProjects", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/projects",
		})
		framework.AssertNoError(t, err, "Failed to list projects")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Projects []map[string]interface{} `json:"projects"`
			Total    int                      `json:"total"`
			Limit    int                      `json:"limit"`
			Offset   int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse projects response")

		found := false
		for _, project := range response.Projects {
			if id, ok := project["id"].(string); ok && id == projectID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find project %s in list", projectID)
		}

		t.Logf("Listed %d projects (total: %d)", len(response.Projects), response.Total)
	})

	t.Run("ListProjects_TeamFilter", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method:      "GET",
			Path:        "/projects",
			QueryParams: map[string]string{"team_id": teamID},
		})
		framework.AssertNoError(t, err, "Failed to list projects with team filter")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Projects []map[string]interface{} `json:"projects"`
			Total    int                      `json:"total"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse filtered projects response")

		if response.Total < 1 {
			t.Errorf("Expected at least 1 project matching team filter, got %d", response.Total)
		}

		t.Logf("Team filter returned %d projects", response.Total)
	})

	updatedProjectName := framework.UniqueName("Updated Project")

	t.Run("UpdateProject", func(t *testing.T) {
		updateFixture := framework.NewProjectFixture(teamID).
			WithName(updatedProjectName).
			WithDescription("Updated via PUT").
			WithStatus("active")

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/projects/" + projectID,
			Body:   updateFixture,
		})
		framework.AssertNoError(t, err, "Failed to update project")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", projectID)
		framework.AssertJSONField(t, resp, "name", updatedProjectName)
		framework.AssertJSONField(t, resp, "description", "Updated via PUT")

		t.Logf("Updated project with PUT: %s", projectID)
	})

	t.Run("PatchProject", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/description",
				"value": "Patched via PATCH operation",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/projects/" + projectID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch project")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "description", "Patched via PATCH operation")
		framework.AssertJSONField(t, resp, "name", updatedProjectName)

		t.Logf("Patched project: %s", projectID)
	})

	t.Run("PatchProject_ProhibitedField", func(t *testing.T) {
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/id",
				"value": "00000000-0000-0000-0000-000000000000",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/projects/" + projectID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusBadRequest(t, resp)

		t.Log("Prohibited field patch correctly rejected")
	})

	t.Run("DeleteTeam_BlockedByProject", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/teams/" + teamID,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		if resp.StatusCode != 409 {
			t.Errorf("Expected 409 Conflict when deleting team with projects, got %d", resp.StatusCode)
		}

		t.Log("Delete team correctly blocked by existing project (409)")
	})

	t.Run("DeleteProject", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/projects/" + projectID,
		})
		framework.AssertNoError(t, err, "Failed to delete project")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("Deleted project: %s", projectID)
	})

	t.Run("VerifyDeleted", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/projects/" + projectID,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("Project correctly not found after deletion")
	})

	t.Run("CleanupTeam", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/teams/" + teamID,
		})
		framework.AssertNoError(t, err, "Failed to delete team")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("Cleaned up team: %s", teamID)
	})

	t.Run("ErrorHandling_NotFound", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/projects/00000000-0000-0000-0000-000000000000",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("404 handling validated")
	})

	t.Run("ErrorHandling_BadRequest", func(t *testing.T) {
		// Missing required team_id field
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/projects",
			Body:   map[string]string{"name": "Bad Project"},
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusBadRequest(t, resp)

		t.Log("400 handling validated")
	})

	t.Log("All project CRUD tests completed successfully")
}
