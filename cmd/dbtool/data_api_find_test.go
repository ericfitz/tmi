package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestAPIClient returns an apiClient pointed at a stub server.
func newTestAPIClient(t *testing.T, handler http.HandlerFunc) *apiClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newAPIClient(srv.URL, "test-token")
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode stub response: %v", err)
	}
}

// The /admin/groups case: matches on group_name, returns internal_uuid.
func TestFindExistingByFieldHTTP_UsesMatchKeyAndIDKey(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"groups": []any{
			map[string]any{"group_name": "other", "internal_uuid": "uuid-other"},
			map[string]any{"group_name": "administrators", "internal_uuid": "uuid-admin"},
		}})
	})

	got := c.findExistingByFieldHTTP("/admin/groups", "groups", "group_name", "administrators", "internal_uuid")
	if got != "uuid-admin" {
		t.Fatalf("got %q, want %q", got, "uuid-admin")
	}
}

func TestFindExistingByFieldHTTP_NoMatchReturnsEmpty(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"groups": []any{
			map[string]any{"group_name": "other", "internal_uuid": "uuid-other"},
		}})
	})

	if got := c.findExistingByFieldHTTP("/admin/groups", "groups", "group_name", "absent", "internal_uuid"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestFindExistingByFieldHTTP_ErrorStatusReturnsEmpty(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	if got := c.findExistingByFieldHTTP("/teams", "teams", "name", "anything", "id"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// Auth previously came from the SDK's configured DefaultHeader; it must still
// be sent now that these lookups go through apiRequest.
func TestFindExistingByFieldHTTP_SendsBearerToken(t *testing.T) {
	var gotAuth string
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(t, w, map[string]any{"teams": []any{}})
	})

	c.findExistingByFieldHTTP("/teams", "teams", "name", "x", "id")
	if gotAuth != "Bearer test-token" {
		t.Fatalf("got %q, want %q", gotAuth, "Bearer test-token")
	}
}

func TestFindExistingByNameHTTP_DefaultsToNameAndID(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"teams": []any{
			map[string]any{"name": "platform", "id": "team-1"},
		}})
	})

	if got := c.findExistingByNameHTTP("/teams", "teams", "platform"); got != "team-1" {
		t.Fatalf("got %q, want %q", got, "team-1")
	}
}

// Each former SDK helper must hit the right path and read the right array key.
func TestFindExistingHelpers_UseCorrectPathAndItemsKey(t *testing.T) {
	cases := []struct {
		name     string
		wantPath string
		itemsKey string
		idKey    string
		nameKey  string
		call     func(c *apiClient) string
	}{
		{"threat model", "/threat_models", "threat_models", "id", "name",
			func(c *apiClient) string { return c.findExistingTM("target") }},
		{"team", "/teams", "teams", "id", "name",
			func(c *apiClient) string { return c.findExistingTeam("target") }},
		{"project", "/projects", "projects", "id", "name",
			func(c *apiClient) string { return c.findExistingProject("target") }},
		{"webhook", "/admin/webhooks/subscriptions", "subscriptions", "id", "name",
			func(c *apiClient) string { return c.findExistingWebhook("target") }},
		{"group", "/admin/groups", "groups", "internal_uuid", "group_name",
			func(c *apiClient) string { return c.findExistingGroup("target") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				writeJSON(t, w, map[string]any{tc.itemsKey: []any{
					map[string]any{tc.nameKey: "target", tc.idKey: "found-id"},
				}})
			})

			if got := tc.call(c); got != "found-id" {
				t.Fatalf("got %q, want %q", got, "found-id")
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}
