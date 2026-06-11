package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestAdminAuditQuery covers the four admin audit query endpoints added in #398:
//   - GET /admin/audit/system        (listSystemAuditEntries)
//   - GET /admin/audit/system/{id}   (getSystemAuditEntry)
//   - GET /admin/audit/threat_models (listAdminThreatModelAuditEntries)
//   - GET /admin/audit/threat_models/{id} (getAdminThreatModelAuditEntry)
//
// Flow:
//  1. As admin (charlie): PUT /admin/settings/test.auditquery twice to generate ≥2
//     system audit rows via the #355 admin-audit middleware.
//  2. GET /admin/audit/system with actor_email + limit=1: assert first-page content,
//     follow next_cursor, verify no duplicates.
//  3. GET /admin/audit/system with path_prefix filter: verify all returned rows match.
//  4. GET /admin/audit/system/{entry_id}: 200 for known id, 404 for random UUID;
//     GET /admin/audit/system?cursor=!!! → 400.
//  5. Create + delete a threat model to generate threat-model audit rows.
//     GET /admin/audit/threat_models?threat_model_id=<id> → rows present;
//     change_type=deleted filter narrows to just the deletion entry.
//  6. GET /admin/audit/threat_models/{entry_id}: 200 / random UUID → 404.
//  7. Non-admin user gets 403 on all four endpoints.
func TestAdminAuditQuery(t *testing.T) {
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

	// Authenticate as admin (charlie). The admin audit middleware records system
	// audit rows for all successful /admin/* writes, so we need an admin token.
	adminTokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Admin authentication failed")

	// Use WithValidation(false): the new list endpoints declare the response body in the
	// OpenAPI spec, but the OpenAPI validator in the test framework may not have loaded
	// the regenerated spec yet (spec is embedded at build time). Disabling validation
	// avoids false failures while still asserting all concrete JSON fields we care about.
	adminClient, err := framework.NewClient(serverURL, adminTokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create admin integration client")

	// --- discover charlie's email from a known /admin/settings GET response -----------
	// We need the actor email to filter system audit entries. We derive it from the
	// admin JWT by doing a settings lookup which records a system audit row; the
	// actor.email in that row is charlie's canonical email.
	//
	// The login hint "test-admin" → email "test-admin@tmi.local" per the TMI OAuth stub
	// convention. Hardcode it so the test does not depend on any extra API endpoint.
	charlieEmail := "test-admin@tmi.local"

	// Use a unique key so parallel runs don't collide.
	settingKey := "test.auditquery." + uuid.New().String()[:8]

	// Step 1: Two PUT /admin/settings/{key} writes → ≥2 system audit rows for this key.
	t.Run("Setup_GenerateSystemAuditRows", func(t *testing.T) {
		for i, val := range []string{"first-value", "second-value"} {
			resp, err := adminClient.Do(framework.Request{
				Method: "PUT",
				Path:   "/admin/settings/" + settingKey,
				Body: map[string]interface{}{
					"value": val,
					"type":  "string",
				},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("PUT settings write %d failed", i+1))
			if resp.StatusCode != 200 {
				t.Fatalf("PUT /admin/settings/%s: got %d, want 200; body: %s", settingKey, resp.StatusCode, string(resp.Body))
			}
		}
		t.Logf("Generated 2 system audit rows for key %s", settingKey)
	})

	// Cleanup: delete the test setting when the test is done (best-effort).
	t.Cleanup(func() {
		_, _ = adminClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/settings/" + settingKey,
		})
	})

	// Step 2: Cursor iteration on GET /admin/audit/system.
	var firstEntryID string

	t.Run("SystemAudit_CursorIteration", func(t *testing.T) {
		// Page 1: filter by actor_email + limit=1.
		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/system",
			QueryParams: map[string]string{
				"actor_email": charlieEmail,
				"limit":       "1",
			},
		})
		framework.AssertNoError(t, err, "GET /admin/audit/system page 1 failed")
		if resp.StatusCode != 200 {
			t.Fatalf("GET /admin/audit/system: got %d, want 200; body: %s", resp.StatusCode, string(resp.Body))
		}

		var page1 struct {
			Entries []map[string]interface{} `json:"entries"`
			Total   float64                  `json:"total"`
			Limit   float64                  `json:"limit"`
			NextCursor *string               `json:"next_cursor"`
		}
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &page1), "parse page 1")

		if len(page1.Entries) == 0 {
			t.Fatalf("Expected ≥1 system audit entry for actor %s, got 0 (body: %s)", charlieEmail, string(resp.Body))
		}
		if page1.Total < 2 {
			t.Errorf("Expected total ≥ 2 audit entries for %s, got %.0f", charlieEmail, page1.Total)
		}
		if page1.NextCursor == nil || *page1.NextCursor == "" {
			t.Fatalf("Expected next_cursor on a full page (limit=1, total=%.0f)", page1.Total)
		}

		entry := page1.Entries[0]
		// Verify actor.email field
		actor, ok := entry["actor"].(map[string]interface{})
		if !ok {
			t.Fatalf("entry.actor is not an object: %v", entry["actor"])
		}
		if actor["email"] != charlieEmail {
			t.Errorf("actor.email: got %v, want %s", actor["email"], charlieEmail)
		}
		// Verify http_method = PUT
		if entry["http_method"] != "PUT" {
			t.Errorf("http_method: got %v, want PUT", entry["http_method"])
		}
		// Verify field_path is populated (non-empty string)
		if fp, _ := entry["field_path"].(string); fp == "" {
			t.Errorf("field_path should be non-empty, got %q", fp)
		}

		firstEntryID = entry["id"].(string)
		t.Logf("Page 1: entry_id=%s next_cursor=%s", firstEntryID, *page1.NextCursor)

		// Page 2: follow the cursor.
		resp2, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/system",
			QueryParams: map[string]string{
				"actor_email": charlieEmail,
				"limit":       "1",
				"cursor":      *page1.NextCursor,
			},
		})
		framework.AssertNoError(t, err, "GET /admin/audit/system page 2 failed")
		if resp2.StatusCode != 200 {
			t.Fatalf("GET /admin/audit/system page 2: got %d; body: %s", resp2.StatusCode, string(resp2.Body))
		}

		var page2 struct {
			Entries []map[string]interface{} `json:"entries"`
		}
		framework.AssertNoError(t, json.Unmarshal(resp2.Body, &page2), "parse page 2")

		if len(page2.Entries) == 0 {
			t.Fatalf("Expected ≥1 entry on page 2, got 0")
		}
		page2ID := page2.Entries[0]["id"].(string)
		if page2ID == firstEntryID {
			t.Errorf("Cursor iteration returned duplicate entry: %s appears on both pages", firstEntryID)
		}
		t.Logf("Page 2: entry_id=%s (different from page 1 — no duplicates)", page2ID)
	})

	// Step 3: path_prefix filter — all returned rows must match.
	t.Run("SystemAudit_PathPrefixFilter", func(t *testing.T) {
		settingPath := "/admin/settings/" + settingKey
		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/system",
			QueryParams: map[string]string{
				"path_prefix": settingPath,
			},
		})
		framework.AssertNoError(t, err, "GET /admin/audit/system?path_prefix failed")
		if resp.StatusCode != 200 {
			t.Fatalf("GET /admin/audit/system?path_prefix=%s: got %d; body: %s", settingPath, resp.StatusCode, string(resp.Body))
		}

		var result struct {
			Entries []map[string]interface{} `json:"entries"`
			Total   float64                  `json:"total"`
		}
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &result), "parse path_prefix result")

		// Every returned entry must have http_path starting with our prefix.
		for _, e := range result.Entries {
			path, _ := e["http_path"].(string)
			if !strings.HasPrefix(path, settingPath) {
				t.Errorf("path_prefix filter leak: entry http_path=%q does not start with %q", path, settingPath)
			}
		}
		t.Logf("path_prefix filter: %d entries, total=%.0f, all match prefix %s", len(result.Entries), result.Total, settingPath)
	})

	// Step 4a: GET /admin/audit/system/{entry_id} — known id returns 200, same row.
	t.Run("SystemAudit_GetByID_Found", func(t *testing.T) {
		if firstEntryID == "" {
			t.Skip("firstEntryID not set (CursorIteration subtest likely failed)")
		}
		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/system/" + firstEntryID,
		})
		framework.AssertNoError(t, err, "GET /admin/audit/system/{id} failed")
		if resp.StatusCode != 200 {
			t.Fatalf("GET /admin/audit/system/%s: got %d; body: %s", firstEntryID, resp.StatusCode, string(resp.Body))
		}
		var entry map[string]interface{}
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &entry), "parse single entry")
		if entry["id"] != firstEntryID {
			t.Errorf("id mismatch: got %v, want %s", entry["id"], firstEntryID)
		}
		t.Logf("GetByID: confirmed entry %s", firstEntryID)
	})

	// Step 4b: Random UUID → 404.
	t.Run("SystemAudit_GetByID_NotFound", func(t *testing.T) {
		randomID := uuid.New().String()
		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/system/" + randomID,
		})
		framework.AssertNoError(t, err, "GET /admin/audit/system/{random} failed")
		if resp.StatusCode != 404 {
			t.Errorf("Expected 404 for unknown entry_id, got %d; body: %s", resp.StatusCode, string(resp.Body))
		}
	})

	// Step 4c: invalid cursor → 400.
	t.Run("SystemAudit_InvalidCursor_400", func(t *testing.T) {
		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/system",
			QueryParams: map[string]string{
				"cursor": "!!!invalid-base64!!!",
			},
		})
		framework.AssertNoError(t, err, "GET /admin/audit/system?cursor=!!! failed")
		if resp.StatusCode != 400 {
			t.Errorf("Expected 400 for invalid cursor, got %d; body: %s", resp.StatusCode, string(resp.Body))
		}
	})

	// Step 5: Create + delete a threat model to generate TM audit rows.
	var tmID string
	var deletedEntryID string

	t.Run("Setup_ThreatModelAuditRows", func(t *testing.T) {
		// Create threat model.
		createResp, err := adminClient.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   framework.NewThreatModelFixture().WithName("Audit Query Test TM"),
		})
		framework.AssertNoError(t, err, "POST /threat_models failed")
		if createResp.StatusCode != 201 {
			t.Fatalf("POST /threat_models: got %d, want 201; body: %s", createResp.StatusCode, string(createResp.Body))
		}
		tmID = framework.ExtractID(t, createResp, "id")
		t.Logf("Created threat model: %s", tmID)

		// Delete threat model (generates a "deleted" audit row).
		deleteResp, err := adminClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "DELETE /threat_models failed")
		if deleteResp.StatusCode != 204 {
			t.Fatalf("DELETE /threat_models/%s: got %d, want 204; body: %s", tmID, deleteResp.StatusCode, string(deleteResp.Body))
		}
		t.Logf("Deleted threat model: %s", tmID)
	})

	// Step 5a: GET /admin/audit/threat_models?threat_model_id=<id> — entries present.
	//
	// The audit debouncer delays writes by DefaultRESTDebounceDelay (10 s), so
	// we poll up to 15 s for the entries to appear rather than asserting
	// immediately after the create+delete.
	t.Run("ThreatModelAudit_FilterByThreatModelID", func(t *testing.T) {
		if tmID == "" {
			t.Skip("tmID not set (Setup subtest failed)")
		}

		var result struct {
			Entries []map[string]interface{} `json:"entries"`
			Total   float64                  `json:"total"`
		}

		deadline := time.Now().Add(15 * time.Second)
		for {
			resp, err := adminClient.Do(framework.Request{
				Method: "GET",
				Path:   "/admin/audit/threat_models",
				QueryParams: map[string]string{
					"threat_model_id": tmID,
				},
			})
			framework.AssertNoError(t, err, "GET /admin/audit/threat_models?threat_model_id failed")
			if resp.StatusCode != 200 {
				t.Fatalf("GET /admin/audit/threat_models?threat_model_id=%s: got %d; body: %s", tmID, resp.StatusCode, string(resp.Body))
			}
			framework.AssertNoError(t, json.Unmarshal(resp.Body, &result), "parse TM audit list")

			if result.Total >= 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("Expected ≥1 audit entries for TM %s after 15 s; got 0. "+
					"Audit debouncer may not have flushed (DefaultRESTDebounceDelay = 10 s).", tmID)
			}
			// Poll every 2 seconds.
			time.Sleep(2 * time.Second)
		}

		// Collect an entry ID for the by-ID lookup test (step 6).
		// Also grab the "deleted" entry ID if present.
		for _, e := range result.Entries {
			if ct, _ := e["change_type"].(string); ct == "deleted" {
				if id, _ := e["id"].(string); id != "" {
					deletedEntryID = id
				}
			}
		}
		t.Logf("TM audit entries for %s: total=%.0f, deletedEntryID=%s", tmID, result.Total, deletedEntryID)
	})

	// Step 5b: change_type=deleted filter narrows to deletion entry only.
	t.Run("ThreatModelAudit_ChangeTypeFilter", func(t *testing.T) {
		if tmID == "" {
			t.Skip("tmID not set (Setup subtest failed)")
		}
		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/threat_models",
			QueryParams: map[string]string{
				"threat_model_id": tmID,
				"change_type":     "deleted",
			},
		})
		framework.AssertNoError(t, err, "GET /admin/audit/threat_models?change_type=deleted failed")
		if resp.StatusCode != 200 {
			t.Fatalf("GET /admin/audit/threat_models?change_type=deleted: got %d; body: %s", resp.StatusCode, string(resp.Body))
		}

		var result struct {
			Entries []map[string]interface{} `json:"entries"`
			Total   float64                  `json:"total"`
		}
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &result), "parse change_type=deleted result")

		// Every entry returned must have change_type == "deleted".
		for _, e := range result.Entries {
			if ct, _ := e["change_type"].(string); ct != "deleted" {
				t.Errorf("change_type filter leak: entry has change_type=%q, expected 'deleted'", ct)
			}
		}
		t.Logf("change_type=deleted filter: %d entries, all correctly typed", len(result.Entries))
	})

	// Step 6a: GET /admin/audit/threat_models/{entry_id} — 200 for known id.
	t.Run("ThreatModelAudit_GetByID_Found", func(t *testing.T) {
		if deletedEntryID == "" {
			t.Skip("deletedEntryID not set (ThreatModelAudit_FilterByThreatModelID likely failed or no deleted entry)")
		}
		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/threat_models/" + deletedEntryID,
		})
		framework.AssertNoError(t, err, "GET /admin/audit/threat_models/{id} failed")
		if resp.StatusCode != 200 {
			t.Fatalf("GET /admin/audit/threat_models/%s: got %d; body: %s", deletedEntryID, resp.StatusCode, string(resp.Body))
		}
		var entry map[string]interface{}
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &entry), "parse TM audit entry")
		if entry["id"] != deletedEntryID {
			t.Errorf("id mismatch: got %v, want %s", entry["id"], deletedEntryID)
		}
		t.Logf("ThreatModelAudit GetByID: confirmed entry %s", deletedEntryID)
	})

	// Step 6b: Random UUID → 404.
	t.Run("ThreatModelAudit_GetByID_NotFound", func(t *testing.T) {
		randomID := uuid.New().String()
		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/audit/threat_models/" + randomID,
		})
		framework.AssertNoError(t, err, "GET /admin/audit/threat_models/{random} failed")
		if resp.StatusCode != 404 {
			t.Errorf("Expected 404 for unknown entry_id, got %d; body: %s", resp.StatusCode, string(resp.Body))
		}
	})

	// Step 7: Non-admin user gets 403 on all four endpoints.
	t.Run("NonAdmin_403_AllEndpoints", func(t *testing.T) {
		nonAdminUserID := framework.UniqueUserID()
		nonAdminTokens, err := framework.AuthenticateUser(nonAdminUserID)
		framework.AssertNoError(t, err, "non-admin authentication failed")

		// Disable validation: 403 error responses use our standard Error schema which
		// may differ from the generated ListSystemAuditEntriesResponse schema. The status
		// code assertion is what matters here.
		nonAdminClient, err := framework.NewClient(serverURL, nonAdminTokens, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create non-admin client")

		endpoints := []struct {
			method string
			path   string
		}{
			{"GET", "/admin/audit/system"},
			{"GET", "/admin/audit/system/" + uuid.New().String()},
			{"GET", "/admin/audit/threat_models"},
			{"GET", "/admin/audit/threat_models/" + uuid.New().String()},
		}

		for _, ep := range endpoints {
			ep := ep // capture
			t.Run(ep.method+"_"+ep.path, func(t *testing.T) {
				resp, err := nonAdminClient.Do(framework.Request{
					Method: ep.method,
					Path:   ep.path,
				})
				framework.AssertNoError(t, err, "request failed unexpectedly")
				if resp.StatusCode != 403 {
					t.Errorf("%s %s: expected 403 for non-admin, got %d; body: %s",
						ep.method, ep.path, resp.StatusCode, string(resp.Body))
				}
			})
		}
	})
}
