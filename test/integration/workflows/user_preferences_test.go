package workflows

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestUserPreferences covers the following OpenAPI operations:
// - GET /me/preferences (getCurrentUserPreferences)
// - POST /me/preferences (createCurrentUserPreferences)
// - PUT /me/preferences (updateCurrentUserPreferences)
//
// Total: 3 operations
func TestUserPreferences(t *testing.T) {
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

	t.Run("GetPreferences_Empty", func(t *testing.T) {
		// Authenticate as a unique user
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		// Create client
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create integration client")

		// Get preferences - should return empty object for new user
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/preferences",
		})
		framework.AssertNoError(t, err, "Failed to get preferences")
		framework.AssertStatusOK(t, resp)

		// Validate empty object
		var prefs map[string]interface{}
		err = json.Unmarshal(resp.Body, &prefs)
		framework.AssertNoError(t, err, "Failed to parse preferences response")

		if len(prefs) != 0 {
			t.Errorf("Expected empty preferences, got %d entries", len(prefs))
		}
	})

	t.Run("CreatePreferences", func(t *testing.T) {
		// Authenticate as a unique user
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		// Create client
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create integration client")

		// Create preferences - use map, not []byte (bytes get base64-encoded by json.Marshal)
		prefsBody := map[string]interface{}{
			"tmi-ux": map[string]interface{}{
				"theme":  "dark",
				"locale": "en-US",
			},
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/preferences",
			Body:   prefsBody,
		})
		framework.AssertNoError(t, err, "Failed to create preferences")
		framework.AssertStatusCreated(t, resp)

		// Validate response
		var prefs map[string]interface{}
		err = json.Unmarshal(resp.Body, &prefs)
		framework.AssertNoError(t, err, "Failed to parse preferences response")

		if _, ok := prefs["tmi-ux"]; !ok {
			t.Error("Expected 'tmi-ux' in preferences response")
		}

		// Verify we can retrieve the preferences
		getResp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/preferences",
		})
		framework.AssertNoError(t, err, "Failed to get preferences")
		framework.AssertStatusOK(t, getResp)

		var savedPrefs map[string]interface{}
		err = json.Unmarshal(getResp.Body, &savedPrefs)
		framework.AssertNoError(t, err, "Failed to parse saved preferences")

		tmiux, ok := savedPrefs["tmi-ux"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected 'tmi-ux' to be an object")
		}
		if tmiux["theme"] != "dark" {
			t.Errorf("Expected theme 'dark', got %v", tmiux["theme"])
		}
	})

	t.Run("CreatePreferences_Conflict", func(t *testing.T) {
		// Authenticate as a unique user
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		// Create client
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create integration client")

		// Create preferences first time
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/preferences",
			Body: map[string]interface{}{
				"tmi-ux": map[string]interface{}{"theme": "light"},
			},
		})
		framework.AssertNoError(t, err, "Failed to create preferences")
		framework.AssertStatusCreated(t, resp)

		// Try to create again - should fail with 409 Conflict
		resp, err = client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/preferences",
			Body: map[string]interface{}{
				"tmi-cli": map[string]interface{}{"color": true},
			},
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusCode(t, resp, 409)
	})

	t.Run("UpdatePreferences_Create", func(t *testing.T) {
		// Authenticate as a unique user
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		// Create client
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create integration client")

		// PUT should create if not exists
		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/me/preferences",
			Body: map[string]interface{}{
				"tmi-ux": map[string]interface{}{"theme": "dark"},
			},
		})
		framework.AssertNoError(t, err, "Failed to update preferences")
		framework.AssertStatusOK(t, resp)

		// Verify preferences were created
		getResp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/preferences",
		})
		framework.AssertNoError(t, err, "Failed to get preferences")
		framework.AssertStatusOK(t, getResp)

		var prefs map[string]interface{}
		err = json.Unmarshal(getResp.Body, &prefs)
		framework.AssertNoError(t, err, "Failed to parse preferences")

		if _, ok := prefs["tmi-ux"]; !ok {
			t.Error("Expected 'tmi-ux' in preferences")
		}
	})

	t.Run("UpdatePreferences_Replace", func(t *testing.T) {
		// Authenticate as a unique user
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		// Create client
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create integration client")

		// Create initial preferences
		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/me/preferences",
			Body: map[string]interface{}{
				"tmi-ux": map[string]interface{}{"theme": "light", "sidebar": true},
			},
		})
		framework.AssertNoError(t, err, "Failed to create preferences")
		framework.AssertStatusOK(t, resp)

		// Replace with new preferences (completely different)
		resp, err = client.Do(framework.Request{
			Method: "PUT",
			Path:   "/me/preferences",
			Body: map[string]interface{}{
				"tmi-cli": map[string]interface{}{"output": "json"},
			},
		})
		framework.AssertNoError(t, err, "Failed to replace preferences")
		framework.AssertStatusOK(t, resp)

		// Verify old preferences are gone
		getResp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/preferences",
		})
		framework.AssertNoError(t, err, "Failed to get preferences")

		var prefs map[string]interface{}
		err = json.Unmarshal(getResp.Body, &prefs)
		framework.AssertNoError(t, err, "Failed to parse preferences")

		if _, ok := prefs["tmi-ux"]; ok {
			t.Error("Expected 'tmi-ux' to be removed after PUT replace")
		}
		if _, ok := prefs["tmi-cli"]; !ok {
			t.Error("Expected 'tmi-cli' in preferences after PUT replace")
		}
	})

	t.Run("Preferences_ValidationErrors", func(t *testing.T) {
		// Authenticate as a unique user
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		if err != nil {
			t.Skipf("Skipping test due to authentication timeout (may be transient): %v", err)
		}

		// Create client
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create integration client")

		// Note: InvalidJSON test removed - framework json.Marshal's the body,
		// so we can't test raw invalid JSON strings without framework changes

		t.Run("InvalidClientKey", func(t *testing.T) {
			// Client keys must be alphanumeric + underscore/hyphen
			resp, err := client.Do(framework.Request{
				Method: "PUT",
				Path:   "/me/preferences",
				Body: map[string]interface{}{
					"invalid.key": map[string]interface{}{"theme": "dark"},
				},
			})
			framework.AssertNoError(t, err, "Failed to make request")
			framework.AssertStatusBadRequest(t, resp)
		})

		t.Run("ExceedsSizeLimit", func(t *testing.T) {
			// Create payload larger than 1KB
			largeValue := strings.Repeat("x", 2000)
			resp, err := client.Do(framework.Request{
				Method: "PUT",
				Path:   "/me/preferences",
				Body: map[string]interface{}{
					"tmi-ux": map[string]interface{}{"data": largeValue},
				},
			})
			framework.AssertNoError(t, err, "Failed to make request")
			framework.AssertStatusBadRequest(t, resp)
		})
	})

	t.Run("Preferences_Unauthorized", func(t *testing.T) {
		// Create client without auth
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create integration client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/preferences",
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusUnauthorized(t, resp)
	})
}
