package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// googleDriveTestDocs holds file IDs and URLs for Google Drive test documents.
type googleDriveTestDocs struct {
	ServiceAccountEmail string `json:"service_account_email"`
	AccessibleDoc       struct {
		FileID string `json:"file_id"`
		URL    string `json:"url"`
	} `json:"accessible_doc"`
	InaccessibleDoc struct {
		FileID string `json:"file_id"`
		URL    string `json:"url"`
	} `json:"inaccessible_doc"`
	AccessiblePDF struct {
		FileID string `json:"file_id"`
		URL    string `json:"url"`
	} `json:"accessible_pdf"`
}

// loadGoogleDriveTestDocs loads the test document fixture file.
// Returns nil if the file does not exist (caller should skip).
func loadGoogleDriveTestDocs(t *testing.T) *googleDriveTestDocs {
	t.Helper()

	// Find project root relative to this test file
	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	docsPath := filepath.Join(projectRoot, "test", "configs", "google-drive-test-docs.json")

	data, err := os.ReadFile(docsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("Failed to read %s: %v", docsPath, err)
	}

	var docs googleDriveTestDocs
	if err := json.Unmarshal(data, &docs); err != nil {
		t.Fatalf("Failed to parse %s: %v", docsPath, err)
	}
	return &docs
}

// googleDriveCredentialsExist checks if the credentials file is present.
func googleDriveCredentialsExist() bool {
	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	credsPath := filepath.Join(projectRoot, "test", "configs", "google-drive-credentials.json")
	_, err := os.Stat(credsPath)
	return err == nil
}

// TestContentProviderWorkflow tests the document access tracking pipeline end-to-end.
// Requires:
// - INTEGRATION_TESTS=true
// - TMI server running with Google Drive source configured
// - test/configs/google-drive-credentials.json (service account key)
// - test/configs/google-drive-test-docs.json (test document URLs)
func TestContentProviderWorkflow(t *testing.T) {
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

	gdriveDocs := loadGoogleDriveTestDocs(t)
	hasGDrive := gdriveDocs != nil && googleDriveCredentialsExist()
	if !hasGDrive {
		t.Log("Google Drive test docs/credentials not found — Google Drive tests will be skipped")
	}

	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	var threatModelID string

	// --- Setup ---

	t.Run("Setup_CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture()
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Create threat model request failed")
		framework.AssertStatusCreated(t, resp)
		threatModelID = framework.ExtractID(t, resp, "id")
		client.SaveState("threat_model_id", threatModelID)
		t.Logf("Created threat model: %s", threatModelID)
	})

	// --- Google Drive document tests ---

	t.Run("CreateDocument_AccessibleGoogleDoc", func(t *testing.T) {
		if !hasGDrive {
			t.Skip("Google Drive not configured")
		}
		fixture := map[string]interface{}{
			"name": "Accessible Google Doc",
			"uri":  gdriveDocs.AccessibleDoc.URL,
		}
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents", threatModelID),
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Create document request failed")
		framework.AssertStatusCreated(t, resp)

		framework.AssertJSONField(t, resp, "access_status", "accessible")
		framework.AssertJSONField(t, resp, "content_source", "google_drive")

		docID := framework.ExtractID(t, resp, "id")
		client.SaveState("accessible_doc_id", docID)
		t.Logf("Created accessible Google Doc document: %s", docID)
	})

	t.Run("CreateDocument_InaccessibleGoogleDoc", func(t *testing.T) {
		if !hasGDrive {
			t.Skip("Google Drive not configured")
		}
		fixture := map[string]interface{}{
			"name": "Inaccessible Google Doc",
			"uri":  gdriveDocs.InaccessibleDoc.URL,
		}
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents", threatModelID),
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Create document request failed")
		framework.AssertStatusCreated(t, resp)

		framework.AssertJSONField(t, resp, "access_status", "pending_access")
		framework.AssertJSONField(t, resp, "content_source", "google_drive")

		docID := framework.ExtractID(t, resp, "id")
		client.SaveState("inaccessible_doc_id", docID)
		t.Logf("Created inaccessible Google Doc document: %s", docID)
	})

	t.Run("CreateDocument_AccessiblePDF", func(t *testing.T) {
		if !hasGDrive {
			t.Skip("Google Drive not configured")
		}
		fixture := map[string]interface{}{
			"name": "Accessible PDF",
			"uri":  gdriveDocs.AccessiblePDF.URL,
		}
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents", threatModelID),
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Create document request failed")
		framework.AssertStatusCreated(t, resp)

		framework.AssertJSONField(t, resp, "access_status", "accessible")
		framework.AssertJSONField(t, resp, "content_source", "google_drive")

		docID := framework.ExtractID(t, resp, "id")
		client.SaveState("accessible_pdf_id", docID)
		t.Logf("Created accessible PDF document: %s", docID)
	})

	// --- Non-Google-Drive tests (always run) ---

	t.Run("CreateDocument_PlainHTTPURL", func(t *testing.T) {
		fixture := map[string]interface{}{
			"name": "Plain HTTP Document",
			"uri":  "https://example.com/docs/security-policy.pdf",
		}
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents", threatModelID),
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Create document request failed")
		framework.AssertStatusCreated(t, resp)

		framework.AssertJSONField(t, resp, "access_status", "unknown")

		docID := framework.ExtractID(t, resp, "id")
		client.SaveState("http_doc_id", docID)
		t.Logf("Created plain HTTP document: %s", docID)
	})

	// --- GET verification ---

	t.Run("GetDocument_AccessibleGoogleDoc_ShowsAccessFields", func(t *testing.T) {
		if !hasGDrive {
			t.Skip("Google Drive not configured")
		}
		docID, _ := client.GetState("accessible_doc_id")
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", threatModelID, docID),
		})
		framework.AssertNoError(t, err, "Get document request failed")
		framework.AssertStatusOK(t, resp)
		framework.AssertJSONField(t, resp, "access_status", "accessible")
		framework.AssertJSONField(t, resp, "content_source", "google_drive")
	})

	t.Run("GetDocument_InaccessibleGoogleDoc_ShowsAccessFields", func(t *testing.T) {
		if !hasGDrive {
			t.Skip("Google Drive not configured")
		}
		docID, _ := client.GetState("inaccessible_doc_id")
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", threatModelID, docID),
		})
		framework.AssertNoError(t, err, "Get document request failed")
		framework.AssertStatusOK(t, resp)
		framework.AssertJSONField(t, resp, "access_status", "pending_access")
		framework.AssertJSONField(t, resp, "content_source", "google_drive")
	})

	t.Run("GetDocument_PlainHTTP_ShowsAccessFields", func(t *testing.T) {
		docID, _ := client.GetState("http_doc_id")
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", threatModelID, docID),
		})
		framework.AssertNoError(t, err, "Get document request failed")
		framework.AssertStatusOK(t, resp)
		framework.AssertJSONField(t, resp, "access_status", "unknown")
	})

	// --- RequestDocumentAccess ---

	t.Run("RequestAccess_PendingDoc_Success", func(t *testing.T) {
		if !hasGDrive {
			t.Skip("Google Drive not configured")
		}
		docID, _ := client.GetState("inaccessible_doc_id")
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s/request_access", threatModelID, docID),
		})
		framework.AssertNoError(t, err, "Request access failed")
		framework.AssertStatusOK(t, resp)
		framework.AssertJSONField(t, resp, "status", "access_requested")
	})

	t.Run("RequestAccess_AccessibleDoc_Returns409", func(t *testing.T) {
		if !hasGDrive {
			t.Skip("Google Drive not configured")
		}
		docID, _ := client.GetState("accessible_doc_id")
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s/request_access", threatModelID, docID),
		})
		framework.AssertNoError(t, err, "Request access failed")
		framework.AssertStatusCode(t, resp, 409)
	})

	t.Run("RequestAccess_PlainHTTPDoc_Returns409", func(t *testing.T) {
		docID, _ := client.GetState("http_doc_id")
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s/request_access", threatModelID, docID),
		})
		framework.AssertNoError(t, err, "Request access failed")
		// access_status is "unknown", not "pending_access", so 409
		framework.AssertStatusCode(t, resp, 409)
	})

	// --- Timmy session tests ---
	// These require Timmy to be configured on the server.
	// Session creation uses SSE, so we test skipped sources via RefreshSources (regular JSON).

	t.Run("TimmySession_RefreshSources_ShowsSkipped", func(t *testing.T) {
		if !hasGDrive {
			t.Skip("Google Drive not configured")
		}

		// Create a session via SSE — we just need the session ID.
		// If Timmy is not configured, the server returns 503 and we skip.
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
			Body:   map[string]string{"title": "Integration Test Session"},
		})
		framework.AssertNoError(t, err, "Create session request failed")

		if resp.StatusCode == 503 {
			t.Skip("Timmy is not configured on this server — skipping session tests")
		}

		// SSE response — parse the session_created event to extract session ID.
		sessionID := extractSSESessionID(t, resp.Body)
		if sessionID == "" {
			t.Skip("Could not extract session ID from SSE response — Timmy may not be configured")
		}
		client.SaveState("session_id", sessionID)
		t.Logf("Created Timmy session: %s", sessionID)

		// Now call RefreshSources to get regular JSON with skipped_sources
		refreshResp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s/refresh_sources", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Refresh sources request failed")
		framework.AssertStatusOK(t, refreshResp)

		// Should have source_count field
		framework.AssertJSONFieldExists(t, refreshResp, "source_count")

		t.Logf("Refresh sources response: %s", string(refreshResp.Body))
	})

	// --- Cleanup ---

	t.Run("Cleanup", func(t *testing.T) {
		// Delete the session if created
		if sessionID, ok := client.GetState("session_id"); ok && sessionID != nil {
			_, _ = client.Do(framework.Request{
				Method: "DELETE",
				Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionID),
			})
		}

		// Delete threat model (cascades to documents)
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s", threatModelID),
		})
		framework.AssertNoError(t, err, "Delete threat model failed")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Cleaned up threat model: %s", threatModelID)
	})
}

// extractSSESessionID parses an SSE response body to find the session ID
// from the "session_created" event.
func extractSSESessionID(t *testing.T, body []byte) string {
	t.Helper()

	// SSE format: "event: session_created\ndata: {json}\n\n"
	lines := string(body)
	var nextIsSessionData bool
	for _, line := range splitLines(lines) {
		if line == "event: session_created" {
			nextIsSessionData = true
			continue
		}
		if nextIsSessionData && len(line) > 6 && line[:6] == "data: " {
			var session map[string]interface{}
			if err := json.Unmarshal([]byte(line[6:]), &session); err != nil {
				t.Logf("Failed to parse session_created data: %v", err)
				return ""
			}
			if id, ok := session["id"].(string); ok {
				return id
			}
			return ""
		}
		if nextIsSessionData && line == "" {
			continue
		}
	}
	return ""
}

// splitLines splits a string into lines, handling both \n and \r\n.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
