package workflows

import (
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestHEADMethodSupport tests that HEAD requests return correct status codes
// and empty bodies across public, protected, and nonexistent endpoints.
// HEAD is handled by HeadMethodMiddleware which converts HEAD→GET.
func TestHEADMethodSupport(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	t.Run("HEAD on public root endpoint returns 200 with no body", func(t *testing.T) {
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create unauthenticated client")

		resp, err := client.Do(framework.Request{
			Method: "HEAD",
			Path:   "/",
		})
		framework.AssertNoError(t, err, "HEAD / request failed")
		framework.AssertStatusOK(t, resp)

		if len(resp.Body) != 0 {
			t.Errorf("Expected empty body for HEAD /, got %d bytes", len(resp.Body))
		}
	})

	t.Run("HEAD on protected endpoint without auth returns 401", func(t *testing.T) {
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create unauthenticated client")

		resp, err := client.Do(framework.Request{
			Method: "HEAD",
			Path:   "/threat_models",
		})
		framework.AssertNoError(t, err, "HEAD /threat_models request failed")
		framework.AssertStatusUnauthorized(t, resp)
	})

	t.Run("HEAD on protected endpoint with auth returns 200 with empty body", func(t *testing.T) {
		if err := framework.EnsureOAuthStubRunning(); err != nil {
			t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
		}

		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Failed to authenticate user")

		client, err := framework.NewClient(serverURL, tokens, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create authenticated client")

		resp, err := client.Do(framework.Request{
			Method: "HEAD",
			Path:   "/threat_models",
		})
		framework.AssertNoError(t, err, "HEAD /threat_models with auth failed")
		framework.AssertStatusOK(t, resp)

		if len(resp.Body) != 0 {
			t.Errorf("Expected empty body for HEAD /threat_models, got %d bytes", len(resp.Body))
		}
	})

	t.Run("HEAD on nonexistent path returns 401 without auth", func(t *testing.T) {
		// Nonexistent paths are intercepted by JWT middleware first (401),
		// not by the router (404), because auth runs before route matching.
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create unauthenticated client")

		resp, err := client.Do(framework.Request{
			Method: "HEAD",
			Path:   "/nonexistent_path_that_does_not_exist",
		})
		framework.AssertNoError(t, err, "HEAD /nonexistent request failed")
		framework.AssertStatusUnauthorized(t, resp)
	})
}
