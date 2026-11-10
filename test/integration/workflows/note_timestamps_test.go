package workflows

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestNoteUpdateReturnsStoredTimestampsIntegration verifies that POST, PUT and PATCH
// on a note return the created_at and modified_at the database holds, matching
// a subsequent GET (#1149: the update response echoed the pre-update value).
func TestNoteUpdateReturnsStoredTimestampsIntegration(t *testing.T) {
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
		Body:   framework.NewThreatModelFixture().WithName("Note Timestamps Test"),
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCode(t, resp, 201)
	tmID := decodeObject(t, resp.Body)["id"].(string)

	resp, err = client.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models/" + tmID + "/notes",
		Body:   map[string]any{"name": "n", "content": "original"},
	})
	framework.AssertNoError(t, err, "Failed to create note")
	framework.AssertStatusCode(t, resp, 201)
	created := decodeObject(t, resp.Body)
	notePath := "/threat_models/" + tmID + "/notes/" + created["id"].(string)
	createdAt := parseTimestamp(t, created, "created_at")
	lastModified := parseTimestamp(t, created, "modified_at")

	// The create response must carry what was stored (no sub-microsecond
	// residue the database would round away).
	resp, err = client.Do(framework.Request{Method: "GET", Path: notePath})
	framework.AssertNoError(t, err, "GET after create failed")
	framework.AssertStatusCode(t, resp, 200)
	fetched := decodeObject(t, resp.Body)
	for field, got := range map[string]time.Time{"created_at": createdAt, "modified_at": lastModified} {
		if want := parseTimestamp(t, fetched, field); !got.Equal(want) {
			t.Fatalf("POST: response %s %v, GET returns %v", field, got, want)
		}
	}

	updates := []struct {
		name string
		req  framework.Request
	}{
		{"PUT", framework.Request{Method: "PUT", Path: notePath,
			Body: map[string]any{"name": "n", "content": "after put"}}},
		{"PATCH", framework.Request{Method: "PATCH", Path: notePath,
			Body: []map[string]any{{"op": "replace", "path": "/content", "value": "after patch"}}}},
	}

	for _, u := range updates {
		// Stored timestamps have microsecond resolution; make sure the clock moves.
		time.Sleep(5 * time.Millisecond)

		resp, err = client.Do(u.req)
		framework.AssertNoError(t, err, u.name+" failed")
		framework.AssertStatusCode(t, resp, 200)
		updated := decodeObject(t, resp.Body)

		resp, err = client.Do(framework.Request{Method: "GET", Path: notePath})
		framework.AssertNoError(t, err, "GET after "+u.name+" failed")
		framework.AssertStatusCode(t, resp, 200)
		fetched := decodeObject(t, resp.Body)

		gotModified := parseTimestamp(t, updated, "modified_at")
		if !gotModified.After(lastModified) {
			t.Fatalf("%s: response modified_at %v did not advance past %v", u.name, gotModified, lastModified)
		}
		if want := parseTimestamp(t, fetched, "modified_at"); !gotModified.Equal(want) {
			t.Fatalf("%s: response modified_at %v, GET returns %v", u.name, gotModified, want)
		}
		if got := parseTimestamp(t, updated, "created_at"); !got.Equal(createdAt) {
			t.Fatalf("%s: response created_at %v, want %v", u.name, got, createdAt)
		}
		lastModified = gotModified
		t.Logf("✓ %s returned stored modified_at=%v", u.name, gotModified)
	}
}

// decodeObject unmarshals a JSON object response body.
func decodeObject(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	framework.AssertNoError(t, json.Unmarshal(body, &obj), "Failed to decode response body")
	return obj
}

// parseTimestamp reads an RFC 3339 timestamp field, failing if it is absent.
func parseTimestamp(t *testing.T, obj map[string]any, field string) time.Time {
	t.Helper()
	s, ok := obj[field].(string)
	if !ok {
		t.Fatalf("%s missing or not a string: %v", field, obj[field])
	}
	ts, err := time.Parse(time.RFC3339Nano, s)
	framework.AssertNoError(t, err, "Failed to parse "+field)
	return ts
}
