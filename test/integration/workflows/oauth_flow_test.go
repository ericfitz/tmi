package workflows

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestOAuthFlow covers the following OpenAPI operations:
// - GET /oauth2/authorize (initiateOAuthAuthorization)
// - GET /oauth2/callback (handleOAuthCallback)
// - POST /oauth2/token (exchangeOAuthToken)
// - POST /oauth2/refresh (refreshAccessToken)
// - POST /oauth2/revoke (revokeToken)
// - GET /oauth2/userinfo (getUserInfo)
// - GET /oauth2/providers (listOAuthProviders)
// - GET /oauth2/providers/{idp}/groups (listProviderGroups)
// - POST /oauth2/introspect (introspectToken)
//
// Total: 9 operations
func TestOAuthFlow(t *testing.T) {
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

	t.Run("ListOAuthProviders", func(t *testing.T) {
		// Test listing OAuth providers without authentication
		// This is a public endpoint per OpenAPI spec
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/oauth2/providers",
		})
		framework.AssertNoError(t, err, "Failed to list OAuth providers")
		framework.AssertStatusOK(t, resp)

		// Validate response structure
		var providers []map[string]interface{}
		err = json.Unmarshal(resp.Body, &providers)
		framework.AssertNoError(t, err, "Failed to parse providers response")

		// Should have at least the 'tmi' provider
		found := false
		for _, provider := range providers {
			if name, ok := provider["name"].(string); ok && name == "tmi" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find 'tmi' provider in list")
		}

		t.Log("✓ Listed OAuth providers successfully")
	})

	t.Run("AuthorizationCodeFlowWithPKCE", func(t *testing.T) {
		// Test complete authorization code flow with PKCE
		userID := framework.UniqueUserID()

		// This internally tests:
		// - GET /oauth2/authorize
		// - GET /oauth2/callback
		// - POST /oauth2/token
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "PKCE authorization flow failed")

		// Validate token structure
		if tokens.AccessToken == "" {
			t.Error("Expected non-empty access_token")
		}
		if tokens.RefreshToken == "" {
			t.Error("Expected non-empty refresh_token")
		}
		if tokens.TokenType != "Bearer" {
			t.Errorf("Expected token_type 'Bearer', got '%s'", tokens.TokenType)
		}
		if tokens.ExpiresIn <= 0 {
			t.Errorf("Expected positive expires_in, got %d", tokens.ExpiresIn)
		}

		t.Logf("✓ Authorization code flow with PKCE completed for user: %s", userID)
	})

	t.Run("GetUserInfo", func(t *testing.T) {
		// Test getting user info with valid access token
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/oauth2/userinfo",
		})
		framework.AssertNoError(t, err, "Failed to get user info")
		framework.AssertStatusOK(t, resp)

		// Validate userinfo structure
		framework.AssertJSONFieldExists(t, resp, "sub")
		framework.AssertJSONFieldExists(t, resp, "email")
		framework.AssertJSONFieldExists(t, resp, "name")

		// Extract and validate email format
		var userInfo map[string]interface{}
		err = json.Unmarshal(resp.Body, &userInfo)
		framework.AssertNoError(t, err, "Failed to parse userinfo response")

		if email, ok := userInfo["email"].(string); ok {
			if email == "" {
				t.Error("Expected non-empty email in userinfo")
			}
			t.Logf("✓ User info retrieved for: %s", email)
		} else {
			t.Error("Email field not found or not a string")
		}
	})

	t.Run("TokenIntrospection", func(t *testing.T) {
		// Test token introspection endpoint
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/oauth2/introspect",
			Body: map[string]string{
				"token": tokens.AccessToken,
			},
		})
		framework.AssertNoError(t, err, "Failed to introspect token")
		framework.AssertStatusOK(t, resp)

		// Validate introspection response
		var introspection map[string]interface{}
		err = json.Unmarshal(resp.Body, &introspection)
		framework.AssertNoError(t, err, "Failed to parse introspection response")

		// Check that token is active
		if active, ok := introspection["active"].(bool); !ok || !active {
			t.Error("Expected token to be active")
		}

		t.Log("✓ Token introspection successful")
	})

	t.Run("TokenRefresh", func(t *testing.T) {
		// Test token refresh flow
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Initial authentication failed")

		originalAccessToken := tokens.AccessToken

		// Wait a moment to ensure new token will be different
		time.Sleep(100 * time.Millisecond)

		// Refresh the token
		newTokens, err := framework.RefreshToken(tokens.RefreshToken, userID)
		framework.AssertNoError(t, err, "Token refresh failed")

		// Validate new tokens
		if newTokens.AccessToken == "" {
			t.Error("Expected non-empty access_token after refresh")
		}
		if newTokens.RefreshToken == "" {
			t.Error("Expected non-empty refresh_token after refresh")
		}
		if newTokens.AccessToken == originalAccessToken {
			t.Error("Expected different access_token after refresh")
		}

		t.Logf("✓ Token refresh successful")
	})

	t.Run("TokenRevocation", func(t *testing.T) {
		// Test token revocation
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		// Revoke the access token
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/oauth2/revoke",
			Body: map[string]string{
				"token": tokens.AccessToken,
			},
		})
		framework.AssertNoError(t, err, "Failed to revoke token")
		framework.AssertStatusOK(t, resp)

		// Try to use the revoked token - should fail
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/oauth2/userinfo",
		})
		// Note: We don't assert error here because the request itself may succeed
		// but the server should return 401 Unauthorized
		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized when using revoked token, got %d", resp.StatusCode)
		}

		t.Log("✓ Token revocation successful")
	})

	t.Run("ListProviderGroups", func(t *testing.T) {
		// Test listing groups for a specific provider
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/oauth2/providers/test/groups",
		})
		framework.AssertNoError(t, err, "Failed to list provider groups")
		framework.AssertStatusOK(t, resp)

		// Validate response is an array
		var groups []map[string]interface{}
		err = json.Unmarshal(resp.Body, &groups)
		framework.AssertNoError(t, err, "Failed to parse groups response")

		t.Logf("✓ Listed %d groups for 'test' provider", len(groups))
	})

	t.Run("InvalidTokenHandling", func(t *testing.T) {
		// Test error handling with invalid token
		client, err := framework.NewClient(serverURL, &framework.OAuthTokens{
			AccessToken: "invalid-token-12345",
			TokenType:   "Bearer",
		})
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/oauth2/userinfo",
		})
		// Request itself should succeed, but response should be 401
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized with invalid token, got %d", resp.StatusCode)
		}

		t.Log("✓ Invalid token properly rejected")
	})

	t.Run("MissingTokenHandling", func(t *testing.T) {
		// Test error handling when no token is provided for protected endpoint
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/oauth2/userinfo",
		})
		// Request should succeed but return 401
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized with missing token, got %d", resp.StatusCode)
		}

		t.Log("✓ Missing token properly rejected")
	})

	t.Log("✓ All OAuth flow tests completed successfully")
}
