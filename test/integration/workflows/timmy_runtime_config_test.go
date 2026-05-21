package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestTimmyRuntimeToggle verifies that disabling Timmy via the settings API
// makes the chat endpoint return 404 without a server restart. The enable gate
// is evaluated per request in TimmyEnabledMiddleware, so a PUT to timmy.enabled
// takes effect (after cache invalidation) without restarting the server.
//
// Covers the runtime behavior of:
//   - PUT /admin/settings/timmy.enabled (updateSystemSetting) - admin only
//   - POST /threat_models/{threat_model_id}/chat/sessions (createTimmyChatSession)
//     gated by the Timmy enable middleware
func TestTimmyRuntimeToggle(t *testing.T) {
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

	tokens, err := framework.AuthenticateUser("charlie")
	framework.AssertNoError(t, err, "auth failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "client failed")

	meResp, err := client.Do(framework.Request{Method: "GET", Path: "/me"})
	framework.AssertNoError(t, err, "GET /me failed")

	var me map[string]interface{}
	if err := json.Unmarshal(meResp.Body, &me); err != nil {
		t.Fatalf("parse /me: %v", err)
	}
	isAdmin, _ := me["is_admin"].(bool)
	if !isAdmin {
		t.Skip("test user is not admin in this instance; cannot drive /admin/settings")
	}

	// Disable Timmy via settings.
	putResp, err := client.Do(framework.Request{
		Method: "PUT", Path: "/admin/settings/timmy.enabled",
		Body: map[string]interface{}{"value": "false", "type": "bool"},
	})
	framework.AssertNoError(t, err, "disable timmy failed")
	if putResp.StatusCode != 200 {
		t.Fatalf("expected 200 from PUT timmy.enabled, got %d: %s", putResp.StatusCode, string(putResp.Body))
	}

	// Re-enable Timmy so the test leaves the instance as it found it, regardless
	// of what the assertion below does.
	defer func() {
		_, _ = client.Do(framework.Request{
			Method: "PUT", Path: "/admin/settings/timmy.enabled",
			Body: map[string]interface{}{"value": "true", "type": "bool"},
		})
	}()

	// The chat path must now 404 (enable gate). Use a syntactically valid TM id;
	// the gate fires in middleware before the handler, so the id need not exist.
	chatResp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models/00000000-0000-0000-0000-000000000000/chat/sessions",
		Body:   map[string]interface{}{},
	})
	framework.AssertNoError(t, err, "chat call failed")
	if chatResp.StatusCode != 404 {
		t.Errorf("expected 404 when Timmy disabled, got %d: %s", chatResp.StatusCode, string(chatResp.Body))
	}
}
