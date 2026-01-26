package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestThreatModelCRUD covers the following OpenAPI operations:
// - POST /threat_models (createThreatModel)
// - GET /threat_models (listThreatModels)
// - GET /threat_models/{id} (getThreatModel)
// - PUT /threat_models/{id} (updateThreatModel)
// - PATCH /threat_models/{id} (patchThreatModel)
// - DELETE /threat_models/{id} (deleteThreatModel)
// - POST /threat_models/{id}/threats (createThreat)
// - GET /threat_models/{id}/threats (listThreats)
// - GET /threat_models/{id}/threats/{threat_id} (getThreat)
// - PUT /threat_models/{id}/threats/{threat_id} (updateThreat)
// - PATCH /threat_models/{id}/threats/{threat_id} (patchThreat)
// - DELETE /threat_models/{id}/threats/{threat_id} (deleteThreat)
// - POST /threat_models/{id}/threats/bulk (bulkCreateThreats)
// - PUT /threat_models/{id}/threats/bulk (bulkUpdateThreats)
// - PATCH /threat_models/{id}/threats/bulk (bulkPatchThreats)
//
// Total: 15 operations
func TestThreatModelCRUD(t *testing.T) {
	// Setup
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Ensure OAuth stub is running
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	// Authenticate
	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	// Create client
	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	var threatModelID string
	var threatID string

	t.Run("CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture().
			WithName("Integration Test Threat Model").
			WithDescription("Created by integration test suite")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)

		// Extract and validate ID
		threatModelID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")

		// Validate response fields
		framework.AssertJSONField(t, resp, "name", "Integration Test Threat Model")
		framework.AssertJSONField(t, resp, "description", "Created by integration test suite")
		framework.AssertJSONFieldExists(t, resp, "created_at")
		framework.AssertJSONFieldExists(t, resp, "modified_at")
		framework.AssertValidTimestamp(t, resp, "created_at")
		framework.AssertValidTimestamp(t, resp, "modified_at")

		// Save to workflow state
		client.SaveState("threat_model_id", threatModelID)

		t.Logf("✓ Created threat model: %s", threatModelID)
	})

	t.Run("GetThreatModel", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + threatModelID,
		})
		framework.AssertNoError(t, err, "Failed to get threat model")
		framework.AssertStatusOK(t, resp)

		// Validate fields match what we created
		framework.AssertJSONField(t, resp, "id", threatModelID)
		framework.AssertJSONField(t, resp, "name", "Integration Test Threat Model")
		framework.AssertJSONField(t, resp, "description", "Created by integration test suite")
		framework.AssertValidTimestamp(t, resp, "created_at")
		framework.AssertValidTimestamp(t, resp, "modified_at")

		t.Logf("✓ Retrieved threat model: %s", threatModelID)
	})

	t.Run("ListThreatModels", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models",
		})
		framework.AssertNoError(t, err, "Failed to list threat models")
		framework.AssertStatusOK(t, resp)

		// Validate response is an array
		var threatModels []map[string]interface{}
		err = json.Unmarshal(resp.Body, &threatModels)
		framework.AssertNoError(t, err, "Failed to parse threat models array")

		// Should contain at least our created threat model
		found := false
		for _, tm := range threatModels {
			if id, ok := tm["id"].(string); ok && id == threatModelID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find threat model %s in list", threatModelID)
		}

		t.Logf("✓ Listed %d threat models", len(threatModels))
	})

	t.Run("UpdateThreatModel", func(t *testing.T) {
		updateFixture := framework.NewThreatModelFixture().
			WithName("Updated Threat Model").
			WithDescription("Updated via PUT")

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + threatModelID,
			Body:   updateFixture,
		})
		framework.AssertNoError(t, err, "Failed to update threat model")
		framework.AssertStatusOK(t, resp)

		// Validate updated fields
		framework.AssertJSONField(t, resp, "id", threatModelID)
		framework.AssertJSONField(t, resp, "name", "Updated Threat Model")
		framework.AssertJSONField(t, resp, "description", "Updated via PUT")

		t.Logf("✓ Updated threat model with PUT: %s", threatModelID)
	})

	t.Run("PatchThreatModel", func(t *testing.T) {
		// JSON Patch format per RFC 6902
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/description",
				"value": "Patched via PATCH operation",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/threat_models/" + threatModelID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch threat model")
		framework.AssertStatusOK(t, resp)

		// Validate patched field
		framework.AssertJSONField(t, resp, "description", "Patched via PATCH operation")
		// Name should remain unchanged from PUT operation
		framework.AssertJSONField(t, resp, "name", "Updated Threat Model")

		t.Logf("✓ Patched threat model: %s", threatModelID)
	})

	t.Run("CreateThreat", func(t *testing.T) {
		threatFixture := map[string]interface{}{
			"name":        "SQL Injection",
			"description": "Database injection vulnerability",
			"severity":    "High",
			"status":      "Open",
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/threats", threatModelID),
			Body:   threatFixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat")
		framework.AssertStatusCreated(t, resp)

		// Extract threat ID
		threatID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")

		// Validate fields
		framework.AssertJSONField(t, resp, "name", "SQL Injection")
		framework.AssertJSONField(t, resp, "severity", "High")
		framework.AssertValidTimestamp(t, resp, "created_at")

		// Save to workflow state
		client.SaveState("threat_id", threatID)

		t.Logf("✓ Created threat: %s", threatID)
	})

	t.Run("GetThreat", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/threats/%s", threatModelID, threatID),
		})
		framework.AssertNoError(t, err, "Failed to get threat")
		framework.AssertStatusOK(t, resp)

		// Validate fields
		framework.AssertJSONField(t, resp, "id", threatID)
		framework.AssertJSONField(t, resp, "name", "SQL Injection")
		framework.AssertJSONField(t, resp, "severity", "High")

		t.Logf("✓ Retrieved threat: %s", threatID)
	})

	t.Run("ListThreats", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/threats", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to list threats")
		framework.AssertStatusOK(t, resp)

		// Validate response is an array
		var threats []map[string]interface{}
		err = json.Unmarshal(resp.Body, &threats)
		framework.AssertNoError(t, err, "Failed to parse threats array")

		// Should contain our created threat
		found := false
		for _, threat := range threats {
			if id, ok := threat["id"].(string); ok && id == threatID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find threat %s in list", threatID)
		}

		t.Logf("✓ Listed %d threats", len(threats))
	})

	t.Run("UpdateThreat", func(t *testing.T) {
		updatePayload := map[string]interface{}{
			"name":        "SQL Injection (Updated)",
			"description": "Updated threat description",
			"severity":    "Critical",
			"status":      "In Progress",
		}

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   fmt.Sprintf("/threat_models/%s/threats/%s", threatModelID, threatID),
			Body:   updatePayload,
		})
		framework.AssertNoError(t, err, "Failed to update threat")
		framework.AssertStatusOK(t, resp)

		// Validate updated fields
		framework.AssertJSONField(t, resp, "name", "SQL Injection (Updated)")
		framework.AssertJSONField(t, resp, "severity", "Critical")
		framework.AssertJSONField(t, resp, "status", "In Progress")

		t.Logf("✓ Updated threat with PUT: %s", threatID)
	})

	t.Run("PatchThreat", func(t *testing.T) {
		// JSON Patch format per RFC 6902
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/status",
				"value": "Resolved",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   fmt.Sprintf("/threat_models/%s/threats/%s", threatModelID, threatID),
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch threat")
		framework.AssertStatusOK(t, resp)

		// Validate patched field
		framework.AssertJSONField(t, resp, "status", "Resolved")
		// Name should remain unchanged
		framework.AssertJSONField(t, resp, "name", "SQL Injection (Updated)")

		t.Logf("✓ Patched threat: %s", threatID)
	})

	t.Run("BulkCreateThreats", func(t *testing.T) {
		bulkFixture := []map[string]interface{}{
			{
				"name":        "XSS Vulnerability",
				"description": "Cross-site scripting risk",
				"severity":    "Medium",
				"status":      "Open",
			},
			{
				"name":        "CSRF Attack",
				"description": "Cross-site request forgery",
				"severity":    "High",
				"status":      "Open",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/threats/bulk", threatModelID),
			Body:   bulkFixture,
		})
		framework.AssertNoError(t, err, "Failed to bulk create threats")
		framework.AssertStatusCreated(t, resp)

		// Validate response is an array with 2 items
		var createdThreats []map[string]interface{}
		err = json.Unmarshal(resp.Body, &createdThreats)
		framework.AssertNoError(t, err, "Failed to parse bulk create response")

		if len(createdThreats) != 2 {
			t.Errorf("Expected 2 threats created, got %d", len(createdThreats))
		}

		t.Logf("✓ Bulk created %d threats", len(createdThreats))
	})

	t.Run("BulkUpdateThreats", func(t *testing.T) {
		// First, list all threats to get IDs for bulk update
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/threats", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to list threats for bulk update")

		var threats []map[string]interface{}
		err = json.Unmarshal(resp.Body, &threats)
		framework.AssertNoError(t, err, "Failed to parse threats list")

		if len(threats) < 2 {
			t.Skip("Need at least 2 threats for bulk update test")
		}

		// Prepare bulk update payload with first 2 threats
		bulkUpdatePayload := []map[string]interface{}{
			{
				"id":       threats[0]["id"],
				"severity": "Low",
			},
			{
				"id":       threats[1]["id"],
				"severity": "Medium",
			},
		}

		resp, err = client.Do(framework.Request{
			Method: "PUT",
			Path:   fmt.Sprintf("/threat_models/%s/threats/bulk", threatModelID),
			Body:   bulkUpdatePayload,
		})
		framework.AssertNoError(t, err, "Failed to bulk update threats")
		framework.AssertStatusOK(t, resp)

		t.Log("✓ Bulk updated threats with PUT")
	})

	t.Run("BulkPatchThreats", func(t *testing.T) {
		// JSON Patch format per RFC 6902 - applied to all threats in the threat model
		bulkPatchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/status",
				"value": "Closed",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   fmt.Sprintf("/threat_models/%s/threats/bulk", threatModelID),
			Body:   bulkPatchPayload,
		})
		framework.AssertNoError(t, err, "Failed to bulk patch threats")
		framework.AssertStatusOK(t, resp)

		t.Log("✓ Bulk patched threats")
	})

	t.Run("DeleteThreat", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s/threats/%s", threatModelID, threatID),
		})
		framework.AssertNoError(t, err, "Failed to delete threat")
		framework.AssertStatusNoContent(t, resp)

		// Verify threat is deleted
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/threats/%s", threatModelID, threatID),
		})
		framework.AssertStatusNotFound(t, resp)

		t.Logf("✓ Deleted threat: %s", threatID)
	})

	t.Run("DeleteThreatModel", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + threatModelID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)

		// Verify threat model is deleted
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + threatModelID,
		})
		framework.AssertStatusNotFound(t, resp)

		t.Logf("✓ Deleted threat model: %s", threatModelID)
	})

	t.Run("ErrorHandling_NotFound", func(t *testing.T) {
		// Test 404 for non-existent threat model
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/00000000-0000-0000-0000-000000000000",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("✓ 404 handling validated")
	})

	t.Run("ErrorHandling_BadRequest", func(t *testing.T) {
		// Test 400 for invalid payload (missing required fields)
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   map[string]string{}, // Missing required 'name' field
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusBadRequest(t, resp)

		t.Log("✓ 400 handling validated")
	})

	t.Log("✓ All threat model CRUD tests completed successfully")
}
