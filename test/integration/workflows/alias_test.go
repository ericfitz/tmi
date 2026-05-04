package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestAliasAssignedOnThreatModelCreate verifies that a newly created ThreatModel
// is assigned a server-assigned integer alias >= 1.
func TestAliasAssignedOnThreatModelCreate(t *testing.T) {
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

	tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   framework.NewThreatModelFixture().WithName("Alias Test"),
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCode(t, resp, 201)

	var tm map[string]any
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &tm), "Failed to decode threat model response")

	alias, ok := tm["alias"].(float64)
	if !ok {
		t.Fatalf("alias field missing or not numeric: %v", tm["alias"])
	}
	if alias < 1 {
		t.Fatalf("alias must be >= 1, got %v", alias)
	}

	t.Logf("✓ ThreatModel created with alias=%v", alias)
}

// TestAliasMonotonicAcrossSubObjects verifies that notes within a ThreatModel
// receive monotonically increasing aliases (1, 2, 3) in creation order.
func TestAliasMonotonicAcrossSubObjects(t *testing.T) {
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

	tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Create ThreatModel.
	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   framework.NewThreatModelFixture().WithName("Alias Sub Test"),
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCode(t, resp, 201)

	var tm map[string]any
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &tm), "Failed to decode threat model response")
	tmID := tm["id"].(string)

	// Create 3 notes; expect alias 1, 2, 3.
	for i := 1; i <= 3; i++ {
		noteResp, noteErr := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/notes",
			Body:   map[string]any{"name": "n", "content": "x"},
		})
		framework.AssertNoError(t, noteErr, fmt.Sprintf("Failed to create note %d", i))
		framework.AssertStatusCode(t, noteResp, 201)

		var note map[string]any
		framework.AssertNoError(t, json.Unmarshal(noteResp.Body, &note), fmt.Sprintf("Failed to decode note %d response", i))

		alias, _ := note["alias"].(float64)
		if int(alias) != i {
			t.Fatalf("expected note alias %d, got %v", i, alias)
		}
		t.Logf("✓ Note %d created with alias=%v", i, alias)
	}
}

// TestAliasIsImmutableViaPut verifies that attempting to change the alias via PUT
// is rejected with HTTP 400.
func TestAliasIsImmutableViaPut(t *testing.T) {
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

	tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Create ThreatModel.
	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   framework.NewThreatModelFixture().WithName("Alias PUT Test"),
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCode(t, resp, 201)

	var tm map[string]any
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &tm), "Failed to decode threat model response")

	// PUT with alias field set to 999; should be rejected.
	tm["alias"] = 999
	putResp, putErr := client.Do(framework.Request{
		Method: "PUT",
		Path:   "/threat_models/" + tm["id"].(string),
		Body:   tm,
	})
	framework.AssertNoError(t, putErr, "PUT request failed unexpectedly")
	framework.AssertStatusCode(t, putResp, 400)

	t.Logf("✓ PUT with alias=999 correctly rejected with 400")
}

// TestAliasNoReuseAfterDelete verifies that after deleting a note, the next
// created note receives a new alias (no gap-filling / alias reuse).
func TestAliasNoReuseAfterDelete(t *testing.T) {
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

	tokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Create ThreatModel.
	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   framework.NewThreatModelFixture().WithName("Alias Delete Test"),
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCode(t, resp, 201)

	var tm map[string]any
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &tm), "Failed to decode threat model response")
	tmID := tm["id"].(string)

	// Create 3 notes (aliases 1, 2, 3).
	noteIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		noteResp, noteErr := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/notes",
			Body:   map[string]any{"name": "n", "content": "x"},
		})
		framework.AssertNoError(t, noteErr, fmt.Sprintf("Failed to create note %d", i+1))
		framework.AssertStatusCode(t, noteResp, 201)

		var n map[string]any
		framework.AssertNoError(t, json.Unmarshal(noteResp.Body, &n), fmt.Sprintf("Failed to decode note %d response", i+1))
		noteIDs[i] = n["id"].(string)
	}

	// Delete note #2 (alias 2).
	delResp, delErr := client.Do(framework.Request{
		Method: "DELETE",
		Path:   "/threat_models/" + tmID + "/notes/" + noteIDs[1],
	})
	framework.AssertNoError(t, delErr, "Failed to delete note 2")
	framework.AssertStatusCode(t, delResp, 204)

	// Create a 4th note; should be alias 4 (no reuse of alias 2).
	n4Resp, n4Err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models/" + tmID + "/notes",
		Body:   map[string]any{"name": "n4", "content": "x"},
	})
	framework.AssertNoError(t, n4Err, "Failed to create note 4")
	framework.AssertStatusCode(t, n4Resp, 201)

	var n4 map[string]any
	framework.AssertNoError(t, json.Unmarshal(n4Resp.Body, &n4), "Failed to decode note 4 response")

	if int(n4["alias"].(float64)) != 4 {
		t.Fatalf("expected alias 4 after delete-then-create, got %v", n4["alias"])
	}

	t.Logf("✓ After deleting note with alias 2, new note correctly received alias=4 (no reuse)")
}
