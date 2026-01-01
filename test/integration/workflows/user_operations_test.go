package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestUserOperations covers the following OpenAPI operations:
// - GET /users/me (getCurrentUser)
// - DELETE /users/me (deleteCurrentUser)
//
// Total: 2 operations
func TestUserOperations(t *testing.T) {
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

	t.Run("GetCurrentUser", func(t *testing.T) {
		// Authenticate as a unique user
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		// Create client
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create integration client")

		// Get current user
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/users/me",
		})
		framework.AssertNoError(t, err, "Failed to get current user")
		framework.AssertStatusOK(t, resp)

		// Validate user structure
		var user map[string]interface{}
		err = json.Unmarshal(resp.Body, &user)
		framework.AssertNoError(t, err, "Failed to parse user response")

		// Check for expected fields
		requiredFields := []string{"id", "email", "name", "provider", "created_at"}
		for _, field := range requiredFields {
			if _, ok := user[field]; !ok {
				t.Errorf("Expected field '%s' in user response", field)
			}
		}

		// Validate specific field types
		framework.AssertValidUUID(t, resp, "id")
		framework.AssertValidTimestamp(t, resp, "created_at")

		// Validate email format (should be from TMI provider)
		if email, ok := user["email"].(string); ok {
			if email == "" {
				t.Error("Expected non-empty email")
			}
			t.Logf("✓ Retrieved current user: %s", email)
		} else {
			t.Error("Email field not found or not a string")
		}

		// Validate provider
		if provider, ok := user["provider"].(string); ok {
			if provider != "tmi" {
				t.Errorf("Expected provider 'tmi', got '%s'", provider)
			}
		}
	})

	t.Run("GetCurrentUser_Unauthorized", func(t *testing.T) {
		// Test without authentication token
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/users/me",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		// Should return 401 Unauthorized
		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized without token, got %d", resp.StatusCode)
		}

		t.Log("✓ Unauthorized access properly rejected")
	})

	t.Run("GetCurrentUser_InvalidToken", func(t *testing.T) {
		// Test with invalid authentication token
		client, err := framework.NewClient(serverURL, &framework.OAuthTokens{
			AccessToken: "invalid-token-xyz",
			TokenType:   "Bearer",
		})
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/users/me",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		// Should return 401 Unauthorized
		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized with invalid token, got %d", resp.StatusCode)
		}

		t.Log("✓ Invalid token properly rejected")
	})

	t.Run("DeleteCurrentUser", func(t *testing.T) {
		// Create a dedicated user for deletion test
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		// Create client
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create integration client")

		// Get current user first to confirm it exists
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/users/me",
		})
		framework.AssertNoError(t, err, "Failed to get user before deletion")
		framework.AssertStatusOK(t, resp)

		var userBeforeDelete map[string]interface{}
		err = json.Unmarshal(resp.Body, &userBeforeDelete)
		framework.AssertNoError(t, err, "Failed to parse user response")

		userEmail := userBeforeDelete["email"].(string)
		t.Logf("Deleting user: %s", userEmail)

		// Delete current user
		resp, err = client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/users/me",
		})
		framework.AssertNoError(t, err, "Failed to delete current user")
		framework.AssertStatusNoContent(t, resp)

		// Verify user is deleted - token should no longer work
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/users/me",
		})
		// Should return 401 since the user (and associated tokens) no longer exist
		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 after user deletion, got %d", resp.StatusCode)
		}

		t.Logf("✓ Successfully deleted user: %s", userEmail)
	})

	t.Run("DeleteCurrentUser_Unauthorized", func(t *testing.T) {
		// Test deletion without authentication
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/users/me",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		// Should return 401 Unauthorized
		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized without token, got %d", resp.StatusCode)
		}

		t.Log("✓ Unauthorized deletion properly rejected")
	})

	t.Run("UserLifecycle_Complete", func(t *testing.T) {
		// Test complete user lifecycle: authenticate -> get user -> create resource -> delete user
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		// 1. Get user info
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/users/me",
		})
		framework.AssertNoError(t, err, "Failed to get user")
		framework.AssertStatusOK(t, resp)

		var user map[string]interface{}
		json.Unmarshal(resp.Body, &user)
		t.Logf("User lifecycle test for: %s", user["email"])

		// 2. Create a resource (threat model) to verify user can perform actions
		tmFixture := framework.NewThreatModelFixture().
			WithName("User Lifecycle Test TM")

		resp, err = client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   tmFixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)

		tmID := framework.ExtractID(t, resp, "id")
		t.Logf("Created threat model: %s", tmID)

		// 3. Delete the user (this should cascade delete all user resources)
		resp, err = client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/users/me",
		})
		framework.AssertNoError(t, err, "Failed to delete user")
		framework.AssertStatusNoContent(t, resp)

		// 4. Verify user and resources are gone
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/users/me",
		})
		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 after user deletion, got %d", resp.StatusCode)
		}

		t.Log("✓ Complete user lifecycle validated")
	})

	t.Log("✓ All user operations tests completed successfully")
}
