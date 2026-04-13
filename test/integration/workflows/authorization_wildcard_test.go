package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestPatchAuthorizationWithWildcardProvider verifies that PATCH /threat_models/{id}
// succeeds when the client sends authorization entries with provider="*" for user
// principals. This was a regression (issue #254) where the enrichment layer failed
// to resolve users stored under a specific provider (e.g., "tmi") when the client
// used the wildcard provider.
func TestPatchAuthorizationWithWildcardProvider(t *testing.T) {
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

	// Create two users so both exist in the database
	ownerID := "alice"
	tokens, err := framework.AuthenticateUser(ownerID)
	framework.AssertNoError(t, err, "Authentication failed for owner")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create client for owner")

	// Ensure bob exists by authenticating as bob
	_, err = framework.AuthenticateUser("bob")
	framework.AssertNoError(t, err, "Authentication failed for bob")

	// Step 1: Create a threat model
	var threatModelID string

	t.Run("CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture().
			WithName("Wildcard Provider Test").
			WithDescription("Testing issue #254 fix")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)

		threatModelID = framework.AssertValidUUID(t, resp, "id")
		t.Logf("Created threat model: %s", threatModelID)
	})

	// Step 2: PATCH authorization with provider="*" for user entries
	t.Run("PatchAuthorizationWithWildcardProvider", func(t *testing.T) {
		if threatModelID == "" {
			t.Skip("No threat model created")
		}

		// This is the exact payload from issue #254: user entries with provider="*"
		patchPayload := []map[string]interface{}{
			{
				"op":   "replace",
				"path": "/authorization",
				"value": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "*",
						"provider_id":    ownerID,
						"role":           "owner",
					},
					{
						"principal_type": "group",
						"provider":       "*",
						"provider_id":    "security-reviewers",
						"role":           "owner",
					},
					{
						"principal_type": "group",
						"provider":       "*",
						"provider_id":    "tmi-automation",
						"role":           "writer",
					},
					{
						"principal_type": "user",
						"provider":       "*",
						"provider_id":    "bob",
						"role":           "writer",
					},
				},
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/threat_models/" + threatModelID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "PATCH with wildcard provider should not fail")
		framework.AssertStatusOK(t, resp)

		// Verify the response contains authorization entries
		authField := framework.AssertJSONFieldExists(t, resp, "authorization")
		authSlice, ok := authField.([]interface{})
		if !ok {
			t.Fatalf("authorization field is not an array")
		}

		// Should have 4 entries
		if len(authSlice) != 4 {
			t.Errorf("Expected 4 authorization entries, got %d", len(authSlice))
		}

		// Verify user entries had their provider resolved from "*" to the actual provider
		for _, entry := range authSlice {
			entryMap, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			if entryMap["principal_type"] == "user" {
				provider, _ := entryMap["provider"].(string)
				if provider == "*" {
					t.Errorf("User entry for %v still has provider='*' after enrichment, expected actual provider",
						entryMap["provider_id"])
				}
			}
		}

		t.Logf("PATCH with wildcard provider succeeded with %d authorization entries", len(authSlice))
	})

	// Step 3: Verify GET returns the data correctly (no serialization issues)
	t.Run("GetThreatModelAfterWildcardPatch", func(t *testing.T) {
		if threatModelID == "" {
			t.Skip("No threat model created")
		}

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + threatModelID,
		})
		framework.AssertNoError(t, err, "Failed to GET threat model after PATCH")
		framework.AssertStatusOK(t, resp)

		// Verify response body is not empty (regression: empty email caused serialization failure)
		var tmData map[string]interface{}
		err = json.Unmarshal(resp.Body, &tmData)
		framework.AssertNoError(t, err, "Failed to parse GET response (possible serialization issue with empty email)")

		framework.AssertJSONField(t, resp, "name", "Wildcard Provider Test")
		authField := framework.AssertJSONFieldExists(t, resp, "authorization")
		authSlice, ok := authField.([]interface{})
		if !ok {
			t.Fatalf("authorization field is not an array")
		}
		if len(authSlice) != 4 {
			t.Errorf("Expected 4 authorization entries on GET, got %d", len(authSlice))
		}

		t.Logf("GET after wildcard PATCH returned valid data with %d authorization entries", len(authSlice))
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		if threatModelID == "" {
			t.Skip("No threat model to clean up")
		}

		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + threatModelID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("Cleaned up threat model: %s", threatModelID)
	})
}
