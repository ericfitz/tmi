package workflows

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestSubResourceWritesReturnStoredTimestampsIntegration verifies that POST,
// PUT, PATCH and bulk POST on repositories, documents and assets return the
// created_at and modified_at the database holds, matching a subsequent GET
// (#1150: these responses omitted created_at, and bulk omitted both).
func TestSubResourceWritesReturnStoredTimestampsIntegration(t *testing.T) {
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
		Body:   framework.NewThreatModelFixture().WithName("Sub-resource Timestamps Test"),
	})
	framework.AssertNoError(t, err, "Failed to create threat model")
	framework.AssertStatusCode(t, resp, 201)
	tmPath := "/threat_models/" + decodeObject(t, resp.Body)["id"].(string)

	kinds := []struct {
		name  string
		path  string
		body  func(label string) map[string]any
		patch []map[string]any
	}{
		{
			name: "repository",
			path: tmPath + "/repositories",
			body: func(label string) map[string]any {
				return map[string]any{"name": label, "uri": "https://github.com/example/" + label}
			},
			patch: []map[string]any{{"op": "replace", "path": "/description", "value": "patched"}},
		},
		{
			name: "document",
			path: tmPath + "/documents",
			body: func(label string) map[string]any {
				return map[string]any{"name": label, "uri": "https://example.com/docs/" + label + ".pdf"}
			},
			patch: []map[string]any{{"op": "replace", "path": "/description", "value": "patched"}},
		},
		{
			name: "asset",
			path: tmPath + "/assets",
			body: func(label string) map[string]any {
				return map[string]any{"name": label, "type": "data"}
			},
			patch: []map[string]any{{"op": "replace", "path": "/description", "value": "patched"}},
		},
	}

	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			// POST
			resp, err := client.Do(framework.Request{Method: "POST", Path: k.path, Body: k.body("single")})
			framework.AssertNoError(t, err, "POST failed")
			framework.AssertStatusCode(t, resp, 201)
			created := decodeObject(t, resp.Body)
			itemPath := k.path + "/" + created["id"].(string)
			createdAt := parseTimestamp(t, created, "created_at")
			lastModified := parseTimestamp(t, created, "modified_at")
			assertMatchesGet(t, client, "POST", itemPath, created)

			// PUT then PATCH: modified_at advances, created_at stays put.
			updates := []struct {
				name string
				req  framework.Request
			}{
				{"PUT", framework.Request{Method: "PUT", Path: itemPath, Body: k.body("single")}},
				{"PATCH", framework.Request{Method: "PATCH", Path: itemPath, Body: k.patch}},
			}
			for _, u := range updates {
				// Stored timestamps have microsecond resolution; make sure the clock moves.
				time.Sleep(5 * time.Millisecond)
				resp, err := client.Do(u.req)
				framework.AssertNoError(t, err, u.name+" failed")
				framework.AssertStatusCode(t, resp, 200)
				updated := decodeObject(t, resp.Body)

				gotModified := parseTimestamp(t, updated, "modified_at")
				if !gotModified.After(lastModified) {
					t.Fatalf("%s: response modified_at %v did not advance past %v", u.name, gotModified, lastModified)
				}
				if got := parseTimestamp(t, updated, "created_at"); !got.Equal(createdAt) {
					t.Fatalf("%s: response created_at %v, want %v", u.name, got, createdAt)
				}
				assertMatchesGet(t, client, u.name, itemPath, updated)
				lastModified = gotModified
			}

			// Bulk POST
			resp, err = client.Do(framework.Request{
				Method: "POST",
				Path:   k.path + "/bulk",
				Body:   []map[string]any{k.body("bulk-a"), k.body("bulk-b")},
			})
			framework.AssertNoError(t, err, "bulk POST failed")
			framework.AssertStatusCode(t, resp, 201)
			var items []map[string]any
			framework.AssertNoError(t, json.Unmarshal(resp.Body, &items), "Failed to decode bulk response")
			if len(items) != 2 {
				t.Fatalf("bulk POST returned %d items, want 2", len(items))
			}
			for _, item := range items {
				assertMatchesGet(t, client, "bulk POST", k.path+"/"+item["id"].(string), item)
			}
			t.Logf("✓ %s POST/PUT/PATCH/bulk returned stored timestamps", k.name)
		})
	}
}

// assertMatchesGet fails unless a write response's created_at and modified_at
// equal what a GET of the same item returns.
func assertMatchesGet(t *testing.T, client *framework.IntegrationClient, op, itemPath string, written map[string]any) {
	t.Helper()
	resp, err := client.Do(framework.Request{Method: "GET", Path: itemPath})
	framework.AssertNoError(t, err, "GET after "+op+" failed")
	framework.AssertStatusCode(t, resp, 200)
	fetched := decodeObject(t, resp.Body)
	for _, field := range []string{"created_at", "modified_at"} {
		got := parseTimestamp(t, written, field)
		if want := parseTimestamp(t, fetched, field); !got.Equal(want) {
			t.Fatalf("%s: response %s %v, GET returns %v", op, field, got, want)
		}
	}
}
