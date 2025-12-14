package workflows

import (
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestExample demonstrates the integration test framework
// This is a basic example showing how to use the framework components
func TestExample(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	// Server URL from environment or default
	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Ensure OAuth stub is running
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	// Authenticate test user
	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Failed to authenticate user")

	// Create integration client
	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Example: Create a threat model
	tmFixture := framework.NewThreatModelFixture().
		WithName("Example Threat Model").
		WithDescription("This is an example from integration test")

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   tmFixture,
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCreated(t, resp)

	// Extract threat model ID
	tmID := framework.ExtractID(t, resp, "id")
	t.Logf("Created threat model with ID: %s", tmID)

	// Save ID to workflow state for later steps
	client.SaveState("threat_model_id", tmID)

	// Example: Get the threat model
	resp, err = client.Do(framework.Request{
		Method: "GET",
		Path:   "/threat_models/" + tmID,
	})
	framework.AssertNoError(t, err, "Failed to get threat model")
	framework.AssertStatusOK(t, resp)

	// Validate fields
	framework.AssertJSONField(t, resp, "name", "Example Threat Model")
	framework.AssertJSONFieldExists(t, resp, "created_at")
	framework.AssertValidTimestamp(t, resp, "created_at")

	// Example: Delete the threat model (cleanup)
	resp, err = client.Do(framework.Request{
		Method: "DELETE",
		Path:   "/threat_models/" + tmID,
	})
	framework.AssertNoError(t, err, "Failed to delete threat model")
	framework.AssertStatusNoContent(t, resp)

	t.Log("✓ Example test completed successfully")
}
