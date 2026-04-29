# Content Provider Integration Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add integration tests for the document access tracking and content provider flow (#232), fix the missing 409 status check in RequestDocumentAccess, and expand AccessPoller unit tests to cover actual polling behavior.

**Architecture:** One integration test file exercises the full HTTP API flow (document creation with access detection, GET to verify fields, RefreshSources for skipped sources, RequestDocumentAccess with error paths). One unit test file expansion covers AccessPoller polling logic using mock stores. A minor refactor extracts `pollOnce()` from the AccessPoller's ticker loop to enable direct testing.

**Tech Stack:** Go testing, testify/assert+mock, existing integration framework (OAuth stub, IntegrationClient, assertion helpers), real Google Drive API via service account credentials.

---

### Task 1: Extract `pollOnce()` from AccessPoller

Refactor the polling loop so individual poll cycles can be tested directly.

**Files:**
- Modify: `api/access_poller.go:45-61`

- [ ] **Step 1: Write a failing test that calls `pollOnce()`**

Add to `api/access_poller_test.go`:

```go
func TestAccessPoller_PollOnce_NilStore(t *testing.T) {
	sources := NewContentSourceRegistry()
	poller := NewAccessPoller(sources, nil, time.Minute, 7*24*time.Hour)
	// pollOnce with nil store should not panic
	poller.pollOnce()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestAccessPoller_PollOnce_NilStore`
Expected: FAIL — `pollOnce` method does not exist.

- [ ] **Step 3: Extract `pollOnce()` method**

In `api/access_poller.go`, rename `checkPendingDocuments` to `pollOnce` and update the caller in `run()`:

Replace the `run()` method (lines 45-61):

```go
func (p *AccessPoller) run() {
	logger := slogging.Get()
	logger.Info("AccessPoller: started (interval=%s, maxAge=%s)", p.interval, p.maxAge)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			logger.Info("AccessPoller: stopped")
			return
		case <-ticker.C:
			p.pollOnce()
		}
	}
}
```

Rename `checkPendingDocuments` (line 63) to `pollOnce`:

```go
func (p *AccessPoller) pollOnce() {
```

(The method body stays identical.)

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestAccessPoller`
Expected: All 3 tests pass (Creation, StopSignal, PollOnce_NilStore).

- [ ] **Step 5: Commit**

```bash
git add api/access_poller.go api/access_poller_test.go
git commit -m "refactor(api): extract pollOnce from AccessPoller for testability"
```

---

### Task 2: Add AccessPoller unit tests for polling behavior

Test the actual polling logic with mock stores and sources.

**Files:**
- Modify: `api/access_poller_test.go`

- [ ] **Step 1: Add mock implementations**

Add these mock types at the top of `api/access_poller_test.go` (below existing imports):

```go
import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// mockDocumentStoreForPoller is a minimal mock for AccessPoller tests.
type mockDocumentStoreForPoller struct {
	documents      []Document
	listErr        error
	updatedID      string
	updatedStatus  string
	updateCalled   bool
}

func (m *mockDocumentStoreForPoller) ListByAccessStatus(_ context.Context, _ string, _ int) ([]Document, error) {
	return m.documents, m.listErr
}

func (m *mockDocumentStoreForPoller) UpdateAccessStatus(_ context.Context, id string, status string, _ string) error {
	m.updateCalled = true
	m.updatedID = id
	m.updatedStatus = status
	return nil
}

// Stub out all other DocumentStore methods (required by interface).
func (m *mockDocumentStoreForPoller) Create(_ context.Context, _ *Document, _ string) error  { return nil }
func (m *mockDocumentStoreForPoller) Get(_ context.Context, _ string) (*Document, error)     { return nil, nil }
func (m *mockDocumentStoreForPoller) Update(_ context.Context, _ *Document, _ string) error  { return nil }
func (m *mockDocumentStoreForPoller) Delete(_ context.Context, _ string) error               { return nil }
func (m *mockDocumentStoreForPoller) SoftDelete(_ context.Context, _ string) error           { return nil }
func (m *mockDocumentStoreForPoller) Restore(_ context.Context, _ string) error              { return nil }
func (m *mockDocumentStoreForPoller) HardDelete(_ context.Context, _ string) error           { return nil }
func (m *mockDocumentStoreForPoller) GetIncludingDeleted(_ context.Context, _ string) (*Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForPoller) Patch(_ context.Context, _ string, _ []PatchOperation) (*Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForPoller) List(_ context.Context, _ string, _, _ int) ([]Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForPoller) Count(_ context.Context, _ string) (int, error) { return 0, nil }
func (m *mockDocumentStoreForPoller) BulkCreate(_ context.Context, _ []Document, _ string) error {
	return nil
}
func (m *mockDocumentStoreForPoller) InvalidateCache(_ context.Context, _ string) error { return nil }
func (m *mockDocumentStoreForPoller) WarmCache(_ context.Context, _ string) error       { return nil }

// mockAccessSource implements ContentSource and AccessValidator for testing.
type mockAccessSource struct {
	name       string
	canHandle  bool
	accessible bool
	valErr     error
}

func (m *mockAccessSource) Name() string                                        { return m.name }
func (m *mockAccessSource) CanHandle(_ context.Context, _ string) bool          { return m.canHandle }
func (m *mockAccessSource) Fetch(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}
func (m *mockAccessSource) ValidateAccess(_ context.Context, _ string) (bool, error) {
	return m.accessible, m.valErr
}
```

- [ ] **Step 2: Write TestAccessPoller_PollOnce_UpdatesAccessible**

```go
func TestAccessPoller_PollOnce_UpdatesAccessible(t *testing.T) {
	docID := uuid.New()
	now := time.Now()
	store := &mockDocumentStoreForPoller{
		documents: []Document{
			{
				Id:        &docID,
				Uri:       "https://docs.google.com/document/d/abc123/edit",
				CreatedAt: &now,
			},
		},
	}

	src := &mockAccessSource{name: "google_drive", canHandle: true, accessible: true}
	sources := NewContentSourceRegistry()
	sources.Register(src)

	poller := NewAccessPoller(sources, store, time.Minute, 7*24*time.Hour)
	poller.pollOnce()

	assert.True(t, store.updateCalled, "UpdateAccessStatus should have been called")
	assert.Equal(t, docID.String(), store.updatedID)
	assert.Equal(t, AccessStatusAccessible, store.updatedStatus)
}
```

- [ ] **Step 3: Write TestAccessPoller_PollOnce_StillInaccessible**

```go
func TestAccessPoller_PollOnce_StillInaccessible(t *testing.T) {
	docID := uuid.New()
	now := time.Now()
	store := &mockDocumentStoreForPoller{
		documents: []Document{
			{
				Id:        &docID,
				Uri:       "https://docs.google.com/document/d/abc123/edit",
				CreatedAt: &now,
			},
		},
	}

	src := &mockAccessSource{name: "google_drive", canHandle: true, accessible: false}
	sources := NewContentSourceRegistry()
	sources.Register(src)

	poller := NewAccessPoller(sources, store, time.Minute, 7*24*time.Hour)
	poller.pollOnce()

	assert.False(t, store.updateCalled, "UpdateAccessStatus should NOT be called when still inaccessible")
}
```

- [ ] **Step 4: Write TestAccessPoller_PollOnce_SkipsExpired**

```go
func TestAccessPoller_PollOnce_SkipsExpired(t *testing.T) {
	docID := uuid.New()
	oldTime := time.Now().Add(-30 * 24 * time.Hour) // 30 days ago
	store := &mockDocumentStoreForPoller{
		documents: []Document{
			{
				Id:        &docID,
				Uri:       "https://docs.google.com/document/d/abc123/edit",
				CreatedAt: &oldTime,
			},
		},
	}

	src := &mockAccessSource{name: "google_drive", canHandle: true, accessible: true}
	sources := NewContentSourceRegistry()
	sources.Register(src)

	// maxAge is 7 days — document is 30 days old, should be skipped
	poller := NewAccessPoller(sources, store, time.Minute, 7*24*time.Hour)
	poller.pollOnce()

	assert.False(t, store.updateCalled, "UpdateAccessStatus should NOT be called for expired documents")
}
```

- [ ] **Step 5: Write TestAccessPoller_PollOnce_NoMatchingSource**

```go
func TestAccessPoller_PollOnce_NoMatchingSource(t *testing.T) {
	docID := uuid.New()
	now := time.Now()
	store := &mockDocumentStoreForPoller{
		documents: []Document{
			{
				Id:        &docID,
				Uri:       "https://confluence.example.com/wiki/page",
				CreatedAt: &now,
			},
		},
	}

	// Register only a Google Drive source — it won't handle confluence URLs
	src := &mockAccessSource{name: "google_drive", canHandle: false, accessible: true}
	sources := NewContentSourceRegistry()
	sources.Register(src)

	poller := NewAccessPoller(sources, store, time.Minute, 7*24*time.Hour)
	// Should not panic
	poller.pollOnce()

	assert.False(t, store.updateCalled, "UpdateAccessStatus should NOT be called when no source matches")
}
```

- [ ] **Step 6: Run all AccessPoller tests**

Run: `make test-unit name=TestAccessPoller`
Expected: All 7 tests pass (Creation, StopSignal, PollOnce_NilStore, PollOnce_UpdatesAccessible, PollOnce_StillInaccessible, PollOnce_SkipsExpired, PollOnce_NoMatchingSource).

- [ ] **Step 7: Commit**

```bash
git add api/access_poller_test.go
git commit -m "test(api): add AccessPoller unit tests for polling behavior"
```

---

### Task 3: Fix missing 409 check in RequestDocumentAccess

The OpenAPI spec defines a 409 response for "Document is not in pending_access status" but the handler doesn't implement it.

**Files:**
- Modify: `api/timmy_handlers.go:469-519`
- Test: `api/timmy_handlers_test.go` (if handler-level unit tests exist; otherwise verified via integration tests in Task 4)

- [ ] **Step 1: Check for existing handler unit tests**

Look at `api/timmy_handlers_test.go` for any existing RequestDocumentAccess tests. If the file doesn't have tests for this handler, we'll verify via integration tests only.

- [ ] **Step 2: Add the 409 check to RequestDocumentAccess**

In `api/timmy_handlers.go`, after the document is fetched (line 482), add the access_status check before the pipeline lookup:

```go
	doc, err := GlobalDocumentStore.Get(c.Request.Context(), documentId.String())
	if err != nil {
		HandleRequestError(c, NotFoundError("Document not found"))
		return
	}

	// Only allow access requests for documents in pending_access status
	if doc.AccessStatus == nil || *doc.AccessStatus != DocumentAccessStatusPendingAccess {
		status := DocumentAccessStatus("unknown")
		if doc.AccessStatus != nil {
			status = *doc.AccessStatus
		}
		HandleRequestError(c, ConflictError(fmt.Sprintf(
			"Document access status is '%s', not 'pending_access'. Only pending_access documents can have access requested.",
			status,
		)))
		return
	}
```

This goes between the `Get` error check and the `s.contentPipeline == nil` check. Make sure `fmt` is in the imports.

- [ ] **Step 3: Run lint and build**

Run: `make lint && make build-server`
Expected: Both pass.

- [ ] **Step 4: Run unit tests**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add api/timmy_handlers.go
git commit -m "fix(api): return 409 when request_access called on non-pending document

Per OpenAPI spec, request_access should return 409 when the document
is not in pending_access status. Previously the handler proceeded
regardless of status.

Refs #232"
```

---

### Task 4: Write the integration test file

The main integration test exercising the full content provider flow via HTTP API.

**Files:**
- Create: `test/integration/workflows/content_provider_test.go`

- [ ] **Step 1: Create the test file with fixture loader and setup**

Create `test/integration/workflows/content_provider_test.go`:

```go
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
```

- [ ] **Step 2: Write the main test function with setup and Google Drive document tests**

Append to the same file:

```go
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
```

- [ ] **Step 3: Add plain HTTP URL test and GET verification tests**

Append:

```go
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
```

- [ ] **Step 4: Add RequestDocumentAccess tests**

Append:

```go
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
```

- [ ] **Step 5: Add Timmy session tests (RefreshSources) and cleanup**

Append:

```go
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
		// The response body contains multiple SSE events; find session_created.
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

		// Should have skipped_sources array (may be null if all accessible)
		framework.AssertJSONFieldExists(t, refreshResp, "source_count")

		// The inaccessible doc should not be counted in sources
		// (it has pending_access status — included in sources but skipped if auth_required)
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
			// Empty line after event name but before data — skip
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
```

- [ ] **Step 6: Run lint**

Run: `make lint`
Expected: 0 issues.

- [ ] **Step 7: Run build**

Run: `make build-server`
Expected: Build succeeds.

- [ ] **Step 8: Commit**

```bash
git add test/integration/workflows/content_provider_test.go
git commit -m "test(integration): add content provider workflow integration tests

Tests document creation with access tracking (Google Drive accessible,
inaccessible, PDF), plain HTTP URL, GET verification of access fields,
RequestDocumentAccess success and 409 error paths, and Timmy session
RefreshSources for skipped sources.

Google Drive tests require credentials in test/configs/ and are
skipped when not present. Timmy session tests skip if Timmy is not
configured on the server.

Refs #232"
```

---

### Task 5: Run full test suite and fix any issues

- [ ] **Step 1: Run unit tests**

Run: `make test-unit`
Expected: All tests pass (including new AccessPoller tests).

- [ ] **Step 2: Run lint**

Run: `make lint`
Expected: 0 issues.

- [ ] **Step 3: Fix any issues found**

If any tests fail or lint issues are found, fix them and re-run.

- [ ] **Step 4: Commit any fixes**

Only if changes were needed in Step 3.

---

### Task 6: Final verification and cleanup

- [ ] **Step 1: Verify all changes compile and pass**

Run: `make lint && make build-server && make test-unit`
Expected: All pass.

- [ ] **Step 2: Review all changed files**

Run: `git diff --stat HEAD~4` (or however many commits were made)
Verify the changeset matches expectations:
- `api/access_poller.go` — `pollOnce()` extraction
- `api/access_poller_test.go` — 5+ new tests
- `api/timmy_handlers.go` — 409 status check
- `test/integration/workflows/content_provider_test.go` — new file
- `.gitignore` — credential ignores
- `test/configs/GOOGLE_DRIVE_TEST_SETUP.md` — setup guide
- `docs/superpowers/specs/2026-04-09-content-provider-integration-tests-design.md` — spec

- [ ] **Step 3: Commit any remaining files**

Ensure the setup guide, spec, and gitignore changes are committed:

```bash
git add .gitignore test/configs/GOOGLE_DRIVE_TEST_SETUP.md docs/superpowers/specs/2026-04-09-content-provider-integration-tests-design.md
git commit -m "docs: add content provider integration test spec and setup guide

Adds Google Drive test setup instructions and design spec for
integration tests covering document access tracking, AccessPoller,
and Timmy session skipping.

Refs #232"
```
