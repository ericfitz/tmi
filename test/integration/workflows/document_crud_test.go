package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestDocumentCRUD covers the following OpenAPI operations:
// - POST /threat_models/{threat_model_id}/documents (createDocument)
// - GET /threat_models/{threat_model_id}/documents (listDocuments)
// - GET /threat_models/{threat_model_id}/documents/{document_id} (getDocument)
// - PUT /threat_models/{threat_model_id}/documents/{document_id} (updateDocument)
// - PATCH /threat_models/{threat_model_id}/documents/{document_id} (patchDocument)
// - DELETE /threat_models/{threat_model_id}/documents/{document_id} (deleteDocument)
//
// Total: 6 operations
func TestDocumentCRUD(t *testing.T) {
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
	var documentID string

	// Setup: Create a threat model for document tests
	t.Run("Setup_CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture().
			WithName("Document Test Threat Model").
			WithDescription("Container for document tests")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)

		threatModelID = framework.ExtractID(t, resp, "id")
		client.SaveState("threat_model_id", threatModelID)

		t.Logf("✓ Setup: Created threat model %s", threatModelID)
	})

	t.Run("CreateDocument", func(t *testing.T) {
		documentFixture := map[string]interface{}{
			"name":        "Security Policy Document",
			"description": "Main security policy for the application",
			"uri":         "https://example.com/docs/security-policy.pdf",
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents", threatModelID),
			Body:   documentFixture,
		})
		framework.AssertNoError(t, err, "Failed to create document")
		framework.AssertStatusCreated(t, resp)

		// Extract document ID
		documentID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")

		// Validate fields
		framework.AssertJSONField(t, resp, "name", "Security Policy Document")
		framework.AssertJSONField(t, resp, "uri", "https://example.com/docs/security-policy.pdf")
		framework.AssertValidTimestamp(t, resp, "created_at")

		// Save to workflow state
		client.SaveState("document_id", documentID)

		t.Logf("✓ Created document: %s", documentID)
	})

	t.Run("GetDocument", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", threatModelID, documentID),
		})
		framework.AssertNoError(t, err, "Failed to get document")
		framework.AssertStatusOK(t, resp)

		// Validate fields
		framework.AssertJSONField(t, resp, "id", documentID)
		framework.AssertJSONField(t, resp, "name", "Security Policy Document")
		framework.AssertJSONField(t, resp, "uri", "https://example.com/docs/security-policy.pdf")
		framework.AssertValidTimestamp(t, resp, "created_at")

		t.Logf("✓ Retrieved document: %s", documentID)
	})

	t.Run("ListDocuments", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to list documents")
		framework.AssertStatusOK(t, resp)

		// Validate response is a wrapped object with pagination
		var response struct {
			Documents []map[string]interface{} `json:"documents"`
			Total     int                      `json:"total"`
			Limit     int                      `json:"limit"`
			Offset    int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse documents response")

		// Should contain our created document
		found := false
		for _, doc := range response.Documents {
			if id, ok := doc["id"].(string); ok && id == documentID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find document %s in list", documentID)
		}

		t.Logf("✓ Listed %d documents (total: %d, limit: %d, offset: %d)", len(response.Documents), response.Total, response.Limit, response.Offset)
	})

	t.Run("UpdateDocument", func(t *testing.T) {
		updatePayload := map[string]interface{}{
			"name":        "Updated Security Policy",
			"description": "Updated security policy document",
			"uri":         "https://example.com/docs/security-policy-v2.pdf",
		}

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", threatModelID, documentID),
			Body:   updatePayload,
		})
		framework.AssertNoError(t, err, "Failed to update document")
		framework.AssertStatusOK(t, resp)

		// Validate updated fields
		framework.AssertJSONField(t, resp, "name", "Updated Security Policy")
		framework.AssertJSONField(t, resp, "description", "Updated security policy document")
		framework.AssertJSONField(t, resp, "uri", "https://example.com/docs/security-policy-v2.pdf")

		t.Logf("✓ Updated document with PUT: %s", documentID)
	})

	t.Run("PatchDocument", func(t *testing.T) {
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
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", threatModelID, documentID),
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch document")
		framework.AssertStatusOK(t, resp)

		// Validate patched field
		framework.AssertJSONField(t, resp, "description", "Patched via PATCH operation")
		// Name should remain unchanged
		framework.AssertJSONField(t, resp, "name", "Updated Security Policy")

		t.Logf("✓ Patched document: %s", documentID)
	})

	t.Run("DeleteDocument", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", threatModelID, documentID),
		})
		framework.AssertNoError(t, err, "Failed to delete document")
		framework.AssertStatusNoContent(t, resp)

		// Verify document is deleted
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", threatModelID, documentID),
		})
		framework.AssertStatusNotFound(t, resp)

		t.Logf("✓ Deleted document: %s", documentID)
	})

	// Cleanup: Delete threat model
	t.Run("Cleanup_DeleteThreatModel", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + threatModelID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("✓ Cleanup: Deleted threat model %s", threatModelID)
	})

	t.Run("ErrorHandling_DocumentNotFound", func(t *testing.T) {
		// Need a valid threat model for this test
		tmFixture := framework.NewThreatModelFixture().WithName("Error Test TM")
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   tmFixture,
		})
		framework.AssertNoError(t, err, "Failed to create test threat model")
		testTMID := framework.ExtractID(t, resp, "id")

		// Test 404 for non-existent document
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents/00000000-0000-0000-0000-000000000000", testTMID),
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		// Cleanup
		client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + testTMID,
		})

		t.Log("✓ 404 handling validated for document")
	})

	t.Log("✓ All document CRUD tests completed successfully")
}
