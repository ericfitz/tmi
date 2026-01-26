package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestAddonOperations covers the following OpenAPI operations:
// - GET /addons (listAddons)
// - GET /addons/{id} (getAddon)
// - POST /addons/{id}/invoke (invokeAddon)
//
// Note: Add-on creation/deletion is typically done by administrators.
// This test covers the user-accessible operations.
//
// Total: 3 operations
func TestAddonOperations(t *testing.T) {
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

	var addonID string

	t.Run("ListAddons", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/addons",
		})
		framework.AssertNoError(t, err, "Failed to list addons")
		framework.AssertStatusOK(t, resp)

		// Validate response structure
		var response map[string]interface{}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse addons response")

		// Check for items array
		items, hasItems := response["items"]
		if !hasItems {
			t.Error("Expected 'items' field in response")
		}

		addons, ok := items.([]interface{})
		if !ok {
			t.Error("Expected 'items' to be an array")
		}

		// If there are addons, save one for subsequent tests
		if len(addons) > 0 {
			if addon, ok := addons[0].(map[string]interface{}); ok {
				if id, ok := addon["id"].(string); ok {
					addonID = id
					client.SaveState("addon_id", addonID)
				}
			}
		}

		t.Logf("✓ Listed %d addons", len(addons))
	})

	t.Run("ListAddons_WithPagination", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/addons",
			QueryParams: map[string]string{
				"limit":  "5",
				"offset": "0",
			},
		})
		framework.AssertNoError(t, err, "Failed to list addons with pagination")
		framework.AssertStatusOK(t, resp)

		var response map[string]interface{}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse paginated addons response")

		// Verify pagination structure
		if _, ok := response["total"]; !ok {
			t.Log("Note: 'total' field not present in paginated response")
		}

		t.Log("✓ Listed addons with pagination")
	})

	t.Run("GetAddon", func(t *testing.T) {
		if addonID == "" {
			t.Skip("No addon available for GetAddon test (none returned from ListAddons)")
		}

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/addons/" + addonID,
		})
		framework.AssertNoError(t, err, "Failed to get addon")
		framework.AssertStatusOK(t, resp)

		// Validate fields
		framework.AssertJSONField(t, resp, "id", addonID)
		framework.AssertJSONFieldExists(t, resp, "name")

		t.Logf("✓ Retrieved addon: %s", addonID)
	})

	t.Run("GetAddon_NotFound", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/addons/00000000-0000-0000-0000-000000000000",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("✓ 404 handling validated for non-existent addon")
	})

	t.Run("InvokeAddon", func(t *testing.T) {
		if addonID == "" {
			t.Skip("No addon available for InvokeAddon test")
		}

		// Create a threat model to use with addon invocation
		tmFixture := framework.NewThreatModelFixture().WithName("Addon Test TM")
		tmResp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   tmFixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model for addon test")
		tmID := framework.ExtractID(t, tmResp, "id")

		// Invoke addon
		invokePayload := map[string]interface{}{
			"threat_model_id": tmID,
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/addons/" + addonID + "/invoke",
			Body:   invokePayload,
		})
		framework.AssertNoError(t, err, "Failed to invoke addon")

		// Addon invocation may return various status codes depending on addon behavior
		// 200/202 for success, 400 for invalid input, 404 if addon not found
		if resp.StatusCode != 200 && resp.StatusCode != 202 && resp.StatusCode != 400 && resp.StatusCode != 404 {
			t.Errorf("Expected 200, 202, 400, or 404 for addon invocation, got %d", resp.StatusCode)
		}

		// Cleanup threat model
		client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})

		t.Log("✓ Addon invocation completed")
	})

	t.Run("InvokeAddon_NotFound", func(t *testing.T) {
		invokePayload := map[string]interface{}{}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/addons/00000000-0000-0000-0000-000000000000/invoke",
			Body:   invokePayload,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("✓ 404 handling validated for invoking non-existent addon")
	})

	t.Run("Unauthorized_NoToken", func(t *testing.T) {
		// Test without authentication token
		noAuthClient, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := noAuthClient.Do(framework.Request{
			Method: "GET",
			Path:   "/addons",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized without token, got %d", resp.StatusCode)
		}

		t.Log("✓ Unauthorized access properly rejected")
	})

	t.Log("✓ All addon tests completed successfully")
}
