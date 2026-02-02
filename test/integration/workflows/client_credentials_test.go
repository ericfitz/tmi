package workflows

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestClientCredentialsCRUD covers the following OpenAPI operations:
// - POST /me/client_credentials (createCurrentUserClientCredential)
// - GET /me/client_credentials (listCurrentUserClientCredentials)
// - DELETE /me/client_credentials/{id} (deleteCurrentUserClientCredential)
//
// Total: 3 operations
func TestClientCredentialsCRUD(t *testing.T) {
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

	var credentialID string
	var clientID string

	t.Run("ListEmptyCredentials", func(t *testing.T) {
		// Test that listing returns 200 OK with empty array for new user
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Failed to list credentials for new user")
		framework.AssertStatusOK(t, resp)

		// Validate response is a wrapped object with pagination
		var response struct {
			Credentials []map[string]interface{} `json:"credentials"`
			Total       int                      `json:"total"`
			Limit       int                      `json:"limit"`
			Offset      int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse credentials response")

		// Should be empty for a fresh user
		if len(response.Credentials) != 0 {
			t.Logf("Note: User already has %d credentials from previous tests", len(response.Credentials))
		}

		t.Log("✓ Empty credential list returns 200 OK with wrapped response")
	})

	t.Run("CreateClientCredential", func(t *testing.T) {
		// Set expiration 1 year from now
		expiresAt := time.Now().AddDate(1, 0, 0).UTC().Format(time.RFC3339)

		fixture := map[string]interface{}{
			"name":        "Integration Test Credential",
			"description": "Created by integration test suite",
			"expires_at":  expiresAt,
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/client_credentials",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create client credential")
		framework.AssertStatusCreated(t, resp)

		// Parse response
		var credential map[string]interface{}
		err = json.Unmarshal(resp.Body, &credential)
		framework.AssertNoError(t, err, "Failed to parse credential response")

		// Extract and validate ID
		credentialID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")

		// Validate client_id is present
		if cid, ok := credential["client_id"].(string); ok {
			clientID = cid
			if clientID == "" {
				t.Error("client_id is empty")
			}
		} else {
			t.Error("client_id not found in response")
		}

		// Validate client_secret is present (only returned on creation)
		if secret, ok := credential["client_secret"].(string); ok {
			if secret == "" {
				t.Error("client_secret is empty")
			}
		} else {
			t.Error("client_secret not found in response")
		}

		// Validate other fields
		framework.AssertJSONField(t, resp, "name", "Integration Test Credential")
		framework.AssertJSONFieldExists(t, resp, "created_at")
		framework.AssertValidTimestamp(t, resp, "created_at")

		// Save to workflow state
		client.SaveState("credential_id", credentialID)
		client.SaveState("client_id", clientID)

		t.Logf("✓ Created client credential: %s (client_id: %s)", credentialID, clientID)
	})

	t.Run("ListClientCredentials", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Failed to list client credentials")
		framework.AssertStatusOK(t, resp)

		// Validate response is a wrapped object with pagination
		var response struct {
			Credentials []map[string]interface{} `json:"credentials"`
			Total       int                      `json:"total"`
			Limit       int                      `json:"limit"`
			Offset      int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse credentials response")

		// Should contain our created credential
		found := false
		for _, cred := range response.Credentials {
			if id, ok := cred["id"].(string); ok && id == credentialID {
				found = true

				// Verify client_secret is NOT returned in list
				if _, hasSecret := cred["client_secret"]; hasSecret {
					t.Error("client_secret should not be returned in list response")
				}

				// Verify other fields are present
				if _, ok := cred["client_id"]; !ok {
					t.Error("client_id should be present in list response")
				}
				if _, ok := cred["name"]; !ok {
					t.Error("name should be present in list response")
				}
				if _, ok := cred["is_active"]; !ok {
					t.Error("is_active should be present in list response")
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected to find credential %s in list", credentialID)
		}

		t.Logf("✓ Listed %d client credentials (total: %d, limit: %d, offset: %d)", len(response.Credentials), response.Total, response.Limit, response.Offset)
	})

	t.Run("CreateSecondCredential", func(t *testing.T) {
		// Create a second credential to test multiple credentials
		fixture := map[string]interface{}{
			"name": "Second Test Credential",
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/client_credentials",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create second credential")
		framework.AssertStatusCreated(t, resp)

		secondID := framework.ExtractID(t, resp, "id")
		client.SaveState("second_credential_id", secondID)

		t.Logf("✓ Created second credential: %s", secondID)
	})

	t.Run("ListShowsMultipleCredentials", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Failed to list credentials")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Credentials []map[string]interface{} `json:"credentials"`
			Total       int                      `json:"total"`
			Limit       int                      `json:"limit"`
			Offset      int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse credentials response")

		if len(response.Credentials) < 2 {
			t.Errorf("Expected at least 2 credentials, got %d", len(response.Credentials))
		}

		t.Logf("✓ Listed %d credentials (multiple)", len(response.Credentials))
	})

	t.Run("DeleteSecondCredential", func(t *testing.T) {
		secondID, err := client.GetStateString("second_credential_id")
		framework.AssertNoError(t, err, "Failed to get second credential ID from state")

		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/me/client_credentials/" + secondID,
		})
		framework.AssertNoError(t, err, "Failed to delete second credential")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("✓ Deleted second credential: %s", secondID)
	})

	t.Run("DeleteClientCredential", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/me/client_credentials/" + credentialID,
		})
		framework.AssertNoError(t, err, "Failed to delete client credential")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("✓ Deleted client credential: %s", credentialID)
	})

	t.Run("VerifyCredentialDeleted", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Failed to list credentials after deletion")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Credentials []map[string]interface{} `json:"credentials"`
			Total       int                      `json:"total"`
			Limit       int                      `json:"limit"`
			Offset      int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse credentials response")

		// Verify deleted credential is not in list
		for _, cred := range response.Credentials {
			if id, ok := cred["id"].(string); ok && id == credentialID {
				t.Errorf("Deleted credential %s should not be in list", credentialID)
			}
		}

		t.Log("✓ Verified credential was deleted")
	})

	t.Run("DeleteNonExistentCredential", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/me/client_credentials/00000000-0000-0000-0000-000000000000",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("✓ 404 handling validated for non-existent credential")
	})

	t.Run("CreateCredential_MassAssignmentPrevention", func(t *testing.T) {
		// Try to send extra fields that should be rejected
		fixture := map[string]interface{}{
			"name":           "Mass Assignment Test",
			"organizationId": "attacker-org", // This should be rejected
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/client_credentials",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		// Should return 400 Bad Request due to unknown field
		if resp.StatusCode != 400 {
			t.Errorf("Expected 400 for mass assignment attempt, got %d", resp.StatusCode)
		}

		t.Log("✓ Mass assignment prevention validated")
	})

	t.Run("Unauthorized_NoToken", func(t *testing.T) {
		// Test without authentication token
		noAuthClient, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := noAuthClient.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized without token, got %d", resp.StatusCode)
		}

		t.Log("✓ Unauthorized access properly rejected")
	})

	t.Log("✓ All client credentials tests completed successfully")
}
