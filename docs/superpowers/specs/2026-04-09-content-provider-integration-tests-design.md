# Content Provider Integration Tests Design

**Issue:** #232 (feat(timmy): content provider infrastructure and Google Drive source)
**Date:** 2026-04-09
**Scope:** Integration tests for document access tracking, AccessPoller, and Timmy session skipping; improved AccessPoller unit tests.

## Motivation

Issue #232 is feature-complete across all 5 implementation phases, but the acceptance criterion "Integration tests for Google Drive access flow" is not yet met. Additionally, AccessPoller unit tests only cover creation and stop — not the actual polling logic. This spec defines both test suites.

## Test Infrastructure

### Credentials & Fixtures

Tests depend on two files in `test/configs/` (both gitignored):

| File | Contents |
|------|----------|
| `google-drive-credentials.json` | GCP service account key (JSON) |
| `google-drive-test-docs.json` | File IDs and URLs for 3 test documents |

Setup instructions: `test/configs/GOOGLE_DRIVE_TEST_SETUP.md`

### Skip Behavior

- If `google-drive-credentials.json` is missing, all Google Drive integration tests are **skipped** (not failed)
- Non-Google-Drive tests (plain HTTP URL, request_access error paths) run regardless
- AccessPoller unit tests have no external dependencies

### Test Documents

| Document | Shared? | Expected access_status | Tests |
|----------|---------|----------------------|-------|
| Google Doc (text) | Yes | `accessible` | Workspace export, content fetch |
| Google Doc (text) | No | `pending_access` | Auth detection, session skipping, request_access |
| PDF (binary) | Yes | `accessible` | Binary download path |

## Integration Test: `test/integration/workflows/content_provider_test.go`

Follows existing workflow test patterns: `INTEGRATION_TESTS` env gate, OAuth stub authentication, framework client, fixture builders, assertion helpers.

### Test Flow

```
TestContentProviderWorkflow(t)
├── Setup_CreateThreatModel
│   └── POST /threat_models → save threat_model_id
├── CreateDocument_AccessibleGoogleDoc
│   └── POST .../documents (shared Google Doc URL)
│       → assert 201, access_status="accessible", content_source="google_drive"
├── CreateDocument_InaccessibleGoogleDoc
│   └── POST .../documents (unshared Google Doc URL)
│       → assert 201, access_status="pending_access", content_source="google_drive"
├── CreateDocument_AccessiblePDF
│   └── POST .../documents (shared PDF URL)
│       → assert 201, access_status="accessible", content_source="google_drive"
├── CreateDocument_PlainHTTPURL
│   └── POST .../documents (plain HTTP URL)
│       → assert 201, access_status="unknown", content_source=null
├── GetDocument_ShowsAccessFields
│   └── GET .../documents/{id} for each created doc
│       → assert access_status and content_source match creation response
├── TimmySession_SkipsInaccessible
│   └── POST .../chat/sessions
│       → assert skipped_sources contains inaccessible doc
│       → assert skipped_sources does NOT contain accessible docs
├── RefreshSources_ReturnsSkipped
│   └── POST .../chat/sessions/{id}/refresh_sources
│       → assert response includes skipped_sources
│       → assert source_count reflects accessible docs only
├── RequestAccess_PendingDoc
│   └── POST .../documents/{inaccessible_id}/request_access
│       → assert 200, status="access_requested"
├── RequestAccess_WrongStatus
│   └── POST .../documents/{accessible_id}/request_access
│       → assert 409
└── Cleanup
    └── DELETE /threat_models/{id}
```

### Fixture Loading

A helper function loads `test/configs/google-drive-test-docs.json` and returns a struct:

```go
type GoogleDriveTestDocs struct {
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
```

### Timmy Session Sub-tests

The Timmy session and refresh_sources tests require a running Timmy configuration (LLM inference). If Timmy is not configured on the test server, these sub-tests should be **skipped** rather than failed. Check by attempting session creation and skipping on 503 or configuration error responses.

## AccessPoller Unit Tests: `api/access_poller_test.go`

Expanded from current 2 tests (creation, stop) to cover actual polling behavior using mock stores and sources.

### New Tests

**TestAccessPoller_PollCycle_UpdatesAccessible**
- Setup: Mock document store returns 1 document with `pending_access` status
- Mock source registry returns a source whose `ValidateAccess()` returns true
- Trigger one poll cycle
- Assert: `UpdateAccessStatus` called with `accessible`

**TestAccessPoller_PollCycle_StillInaccessible**
- Setup: Mock document store returns 1 document with `pending_access` status
- Mock source returns `ValidateAccess()` = false
- Trigger one poll cycle
- Assert: `UpdateAccessStatus` NOT called (status unchanged)

**TestAccessPoller_SkipsExpiredDocuments**
- Setup: Mock document store returns 1 document with `pending_access` status, created_at older than maxAge
- Trigger one poll cycle
- Assert: `ValidateAccess` NOT called for expired document

**TestAccessPoller_NoMatchingSource**
- Setup: Mock document store returns 1 document with `content_source="confluence"` (no confluence source registered)
- Trigger one poll cycle
- Assert: No panic, document skipped gracefully

**TestAccessPoller_StopDuringPoll**
- Start poller with short interval
- Call Stop() immediately
- Assert: goroutine exits without error or hang (use timeout)

### Implementation Approach

The AccessPoller currently runs its logic inside a `select` loop in `Start()`. To test individual poll cycles without timers, extract the per-cycle logic into a `pollOnce()` method that can be called directly from tests. This is a minor refactor — move the body of the `case <-ticker.C:` branch into `pollOnce()`.

## Files Changed

| File | Change |
|------|--------|
| `test/integration/workflows/content_provider_test.go` | NEW: integration test |
| `api/access_poller.go` | Extract `pollOnce()` method |
| `api/access_poller_test.go` | Add 5 new unit tests |
| `.gitignore` | Add credential file ignores (already done) |
| `test/configs/GOOGLE_DRIVE_TEST_SETUP.md` | Setup guide (already done) |

## Out of Scope

- Confluence, OneDrive providers (issue #249)
- Delegated provider token infrastructure (issue #249)
- Load/performance testing of the access poller
- Testing with actual Google Drive API failures (rate limiting, transient errors)
