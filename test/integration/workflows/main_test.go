package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestMain runs before all tests in the workflows package.
// It authenticates the admin user and prepares the test environment.
func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION_TESTS") == "true" {
		if err := framework.EnsureOAuthStubRunning(); err == nil {
			// Authenticate admin — creates the user and promotes via DB
			tokens, _ := framework.AuthenticateAdmin()

			// Allow localhost webhook URLs for test receivers
			framework.AllowLocalhostWebhooks()

			// Clean up leftover subscriptions from previous test runs
			// to prevent subscription count quota (default 10) from blocking
			if tokens != nil {
				serverURL := os.Getenv("TMI_SERVER_URL")
				if serverURL == "" {
					serverURL = "http://localhost:8080"
				}
				if client, err := framework.NewClient(serverURL, tokens); err == nil {
					deleteAllSubscriptions(client)
				}
			}

			// Clear all rate limits
			_ = framework.ClearRateLimits()
		}
	}
	os.Exit(m.Run())
}

// deleteAllSubscriptions removes all webhook subscriptions for the admin user.
func deleteAllSubscriptions(client *framework.IntegrationClient) {
	resp, err := client.Do(framework.Request{
		Method: "GET",
		Path:   "/admin/webhooks/subscriptions",
	})
	if err != nil || resp.StatusCode != 200 {
		return
	}
	var result map[string]any
	if json.Unmarshal(resp.Body, &result) != nil {
		return
	}
	subs, ok := result["subscriptions"].([]any)
	if !ok {
		return
	}
	for _, s := range subs {
		sub, ok := s.(map[string]any)
		if !ok {
			continue
		}
		id, ok := sub["id"].(string)
		if !ok {
			continue
		}
		_, _ = client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/webhooks/subscriptions/" + id,
		})
	}
}
