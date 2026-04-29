# Microsoft picker_registration on document creation — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `POST /threat_models/{id}/documents` accept `picker_registration` payloads with `provider_id: "microsoft"`, closing the gap that blocks tmi-ux #643.

**Architecture:** Inline `switch pr.ProviderID` in the existing `validatePickerRegistration` helper in `api/document_sub_resource_handlers.go`. Google branch keeps existing `extractGoogleDriveFileID` check; Microsoft branch verifies `*.sharepoint.com` host suffix and `decodeMicrosoftPickerFileID` parse. Widen the OpenAPI `PickerRegistration.provider_id` enum and regenerate. Add an integration test that exercises the Microsoft path end-to-end.

**Tech Stack:** Go, Gin, oapi-codegen v2, GORM (untouched), `github.com/stretchr/testify`. No new dependencies.

**Spec:** [`docs/superpowers/specs/2026-04-28-microsoft-picker-registration-design.md`](../specs/2026-04-28-microsoft-picker-registration-design.md)

---

## File Structure

| File | Disposition | Purpose |
|---|---|---|
| `api-schema/tmi-openapi.json` | Modify | Widen `PickerRegistration.provider_id.enum`; generalize descriptions |
| `api/api.go` | Regenerate (do NOT hand-edit) | Output of `make generate-api` |
| `api/document_sub_resource_handlers.go` | Modify | Provider-dispatch in `validatePickerRegistration` (~lines 88–146) |
| `api/document_sub_resource_handlers_test.go` | Modify | Unit tests for the new provider switch |
| `api/microsoft_picker_registration_integration_test.go` | Create | End-to-end test for `provider_id: "microsoft"` document creation |

---

## Pre-flight

- [ ] **Step 1: Confirm working tree is clean and on dev/1.4.0**

```bash
git status
git branch --show-current
```

Expected: `nothing to commit, working tree clean`; branch `dev/1.4.0`.

If anything is dirty, stash before starting.

---

## Task 1: Widen OpenAPI enum and regenerate

**Files:**
- Modify: `api-schema/tmi-openapi.json` (component `PickerRegistration`, ~line 11040)
- Regenerate: `api/api.go`

- [ ] **Step 1: Edit the schema**

Open `api-schema/tmi-openapi.json` and locate `components.schemas.PickerRegistration` (around line 11040). Apply three edits:

1. `provider_id.enum`: change `["google_workspace"]` to `["google_workspace", "microsoft"]`.
2. `provider_id.description`: keep as-is (`"Content OAuth provider that issued the picker grant"`).
3. `file_id.description`: change `"Provider-native file identifier from the picker (e.g. Google Drive file ID)"` to `"Provider-native file identifier from the picker (e.g. Google Drive file ID, or Microsoft \"{driveId}:{itemId}\")"`.
4. Schema-level `description`: change

   `"Client-provided registration for a Google Workspace Picker attachment. Supplied when a user attaches a file to a threat model via the Google Picker flow; the server stores these fields on the document and uses them to dispatch fetch and access-validation operations through the delegated Google Workspace source."`

   to

   `"Client-provided registration for a picker-mediated provider attachment. Supplied when a user attaches a file to a threat model via a picker flow (Google Picker, Microsoft File Picker); the server stores these fields on the document and uses them to dispatch fetch and access-validation operations through the matching delegated source."`

After edits, the section should read:

```json
"PickerRegistration": {
  "type": "object",
  "required": ["provider_id", "file_id", "mime_type"],
  "properties": {
    "provider_id": {
      "type": "string",
      "enum": ["google_workspace", "microsoft"],
      "description": "Content OAuth provider that issued the picker grant"
    },
    "file_id": {
      "type": "string",
      "minLength": 1,
      "maxLength": 255,
      "description": "Provider-native file identifier from the picker (e.g. Google Drive file ID, or Microsoft \"{driveId}:{itemId}\")"
    },
    "mime_type": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "description": "MIME type returned by the picker"
    }
  },
  "description": "Client-provided registration for a picker-mediated provider attachment. Supplied when a user attaches a file to a threat model via a picker flow (Google Picker, Microsoft File Picker); the server stores these fields on the document and uses them to dispatch fetch and access-validation operations through the matching delegated source."
}
```

- [ ] **Step 2: Validate the OpenAPI spec**

```bash
make validate-openapi
```

Expected: `0 errors / 0 warnings / 0 info`. If new warnings appear, fix the descriptions until clean — the spec was clean before this change.

- [ ] **Step 3: Regenerate API code**

```bash
make generate-api
```

Expected: succeeds. `api/api.go` is rewritten. No manual edits to `api/api.go`.

- [ ] **Step 4: Verify the diff is small and contains only enum-related changes**

```bash
git diff --stat api/api.go api-schema/tmi-openapi.json
git diff api/api.go | head -60
```

Expected: changes are only in `PickerRegistration` description, `PickerRegistrationProviderId`-related lines, and `enum` constants. No unrelated handler signatures or struct shapes shifted.

- [ ] **Step 5: Build to confirm everything compiles**

```bash
make build-server
```

Expected: clean build (no compile errors).

- [ ] **Step 6: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): widen PickerRegistration provider_id enum to include microsoft (#307)"
```

---

## Task 2: Unit-test the new provider dispatch (TDD)

We have an existing test file `api/document_sub_resource_handlers_test.go`. Add focused unit tests for `validatePickerRegistration` covering the Microsoft path before changing the handler.

**Files:**
- Modify: `api/document_sub_resource_handlers_test.go`

First, find the existing tests for `validatePickerRegistration` so the new ones follow the same style.

- [ ] **Step 1: Locate existing tests for validatePickerRegistration**

```bash
rg -n "validatePickerRegistration|TestValidatePickerRegistration" api/document_sub_resource_handlers_test.go
```

If matches exist, read the surrounding test functions to match style (mock setup, gin context construction). If no direct tests exist (the function may have been covered only via integration tests), pattern-match on existing handler-helper tests in the same file.

- [ ] **Step 2: Write Microsoft happy-path unit test (failing)**

Append the following test at the end of `api/document_sub_resource_handlers_test.go`. **Do not invent struct fields or constructors** — read the existing tests in the same file and use the same `DocumentSubResourceHandler` construction pattern (likely uses `NewDocumentSubResourceHandler` plus `Set*` setters). Use the existing test helpers for gin context if any are visible (e.g., `httptest.NewRecorder` + `gin.CreateTestContext`).

```go
func TestValidatePickerRegistration_Microsoft_HappyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDocumentSubResourceHandlerForTest(t) // existing test factory; mirror the helper used by other tests
	// Stand up a content-OAuth registry that has "microsoft" registered, plus
	// a content-token repo that returns an active token for our user.
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubContentOAuthProvider{id: ProviderMicrosoft})
	h.SetContentOAuthRegistry(registry)
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, userID, providerID string) (*ContentToken, error) {
			if userID == "u1" && providerID == ProviderMicrosoft {
				return &ContentToken{ID: "t1", UserID: "u1", ProviderID: ProviderMicrosoft, Status: ContentTokenStatusActive}, nil
			}
			return nil, ErrContentTokenNotFound
		},
	}
	h.SetContentTokens(repo)

	c, rec := newGinContextForTest(t) // mirror the existing helper used by other tests in this file

	uri := "https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/Doc.docx"
	sniff := pickerRegistrationSniff{}
	sniff.PickerRegistration = &struct {
		ProviderID string `json:"provider_id"`
		FileID     string `json:"file_id"`
		MimeType   string `json:"mime_type"`
	}{ProviderID: ProviderMicrosoft, FileID: "b!abc:01XYZ", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}

	ok := h.validatePickerRegistration(c, uri, sniff, "u1")
	require.True(t, ok)
	require.Equal(t, http.StatusOK, rec.Code) // no error response written
}
```

If the existing tests use a different helper name than `newDocumentSubResourceHandlerForTest` or `newGinContextForTest`, replace those calls with whatever the file actually uses. **Do not introduce new helpers — reuse what's there.** If neither helper exists, inline the setup using the same idioms as other tests in this file (e.g., direct struct construction + `httptest.NewRecorder` + `gin.CreateTestContext`).

- [ ] **Step 3: Write Microsoft negative-path unit tests (failing)**

Append:

```go
func TestValidatePickerRegistration_Microsoft_NonSharePointURI_Rejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDocumentSubResourceHandlerForTest(t)
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubContentOAuthProvider{id: ProviderMicrosoft})
	h.SetContentOAuthRegistry(registry)
	h.SetContentTokens(&mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return &ContentToken{Status: ContentTokenStatusActive}, nil
		},
	})

	c, rec := newGinContextForTest(t)
	sniff := pickerRegistrationSniff{}
	sniff.PickerRegistration = &struct {
		ProviderID string `json:"provider_id"`
		FileID     string `json:"file_id"`
		MimeType   string `json:"mime_type"`
	}{ProviderID: ProviderMicrosoft, FileID: "b!abc:01XYZ", MimeType: "text/plain"}

	ok := h.validatePickerRegistration(c, "https://example.com/doc.txt", sniff, "u1")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "picker_file_id_mismatch")
}

func TestValidatePickerRegistration_Microsoft_MalformedFileID_Rejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDocumentSubResourceHandlerForTest(t)
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubContentOAuthProvider{id: ProviderMicrosoft})
	h.SetContentOAuthRegistry(registry)
	h.SetContentTokens(&mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return &ContentToken{Status: ContentTokenStatusActive}, nil
		},
	})

	c, rec := newGinContextForTest(t)
	sniff := pickerRegistrationSniff{}
	sniff.PickerRegistration = &struct {
		ProviderID string `json:"provider_id"`
		FileID     string `json:"file_id"`
		MimeType   string `json:"mime_type"`
	}{ProviderID: ProviderMicrosoft, FileID: "no-colon-here", MimeType: "text/plain"}

	ok := h.validatePickerRegistration(c, "https://contoso.sharepoint.com/sites/M/Doc.docx", sniff, "u1")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "picker_file_id_mismatch")
}

func TestValidatePickerRegistration_UnknownProvider_Rejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newDocumentSubResourceHandlerForTest(t)
	registry := NewContentOAuthProviderRegistry()
	h.SetContentOAuthRegistry(registry) // empty registry — provider not registered
	h.SetContentTokens(&mockContentTokenRepo{})

	c, rec := newGinContextForTest(t)
	sniff := pickerRegistrationSniff{}
	sniff.PickerRegistration = &struct {
		ProviderID string `json:"provider_id"`
		FileID     string `json:"file_id"`
		MimeType   string `json:"mime_type"`
	}{ProviderID: "made_up_provider", FileID: "abc", MimeType: "text/plain"}

	ok := h.validatePickerRegistration(c, "https://example.com/foo", sniff, "u1")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "picker_file_id_mismatch")
}
```

- [ ] **Step 4: Run the new tests — expect FAIL**

```bash
make test-unit name=TestValidatePickerRegistration_Microsoft
make test-unit name=TestValidatePickerRegistration_UnknownProvider
```

Expected: the four tests compile, the existing happy-path Google test (if any) passes, and the four new tests **fail** because the handler still hardcodes `extractGoogleDriveFileID`. The Microsoft happy-path will fail with `picker_file_id_mismatch` because today's code can't extract a Google file ID from a SharePoint URL. The unknown-provider test may already pass (depends on registry-not-found handling order) — that's OK.

If the test file refuses to compile because helpers (`newDocumentSubResourceHandlerForTest`, `newGinContextForTest`, `mockContentTokenRepo`, `stubContentOAuthProvider`) don't exist, replace them with whatever this test file actually uses. The names are placeholders; the goal is to mirror the existing patterns in `api/document_sub_resource_handlers_test.go`.

- [ ] **Step 5: Commit the failing tests**

```bash
git add api/document_sub_resource_handlers_test.go
git commit -m "test(api): add failing unit tests for microsoft picker_registration validation (#307)"
```

---

## Task 3: Implement provider dispatch in validatePickerRegistration

**Files:**
- Modify: `api/document_sub_resource_handlers.go` (function `validatePickerRegistration`, ~lines 88–146)

- [ ] **Step 1: Add `net/url` and `strings` imports if not already present**

```bash
rg -n '"net/url"|"strings"' api/document_sub_resource_handlers.go | head -5
```

If `net/url` or `strings` is missing from the import block, add them. Verify with:

```bash
goimports -l api/document_sub_resource_handlers.go
```

(If `goimports` reports the file, run `goimports -w` on it after editing.)

- [ ] **Step 2: Replace the unconditional Google extraction with a provider switch**

In `api/document_sub_resource_handlers.go`, locate this block (currently around lines 103–110):

```go
	fileID, ok := extractGoogleDriveFileID(uri)
	if !ok || fileID != pr.FileID {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "picker_file_id_mismatch",
			Message: "picker_registration.file_id does not match the file id in uri",
		})
		return false
	}
```

Replace it with:

```go
	// Per-provider URI ↔ file_id consistency check. Each branch validates
	// that the provider_id, the URI shape, and the file_id format are
	// internally consistent. Adding a third pickered provider should at
	// that point be refactored into a per-provider validator interface;
	// with two providers, an inline switch is clearer.
	switch pr.ProviderID {
	case ProviderGoogleWorkspace:
		fileID, ok := extractGoogleDriveFileID(uri)
		if !ok || fileID != pr.FileID {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "picker_file_id_mismatch",
				Message: "picker_registration.file_id does not match the file id in uri",
			})
			return false
		}
	case ProviderMicrosoft:
		// SharePoint webUrls do not deterministically encode (driveId, itemId);
		// the picker grant call already vouched for the binding before we got
		// here. We verify only that the URL is a SharePoint host and that the
		// file_id is a syntactically valid {driveId}:{itemId} tuple.
		parsed, err := url.Parse(uri)
		if err != nil || !strings.HasSuffix(strings.ToLower(parsed.Host), ".sharepoint.com") {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "picker_file_id_mismatch",
				Message: "picker_registration.file_id does not match the file id in uri",
			})
			return false
		}
		if _, _, ok := decodeMicrosoftPickerFileID(pr.FileID); !ok {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "picker_file_id_mismatch",
				Message: "picker_registration.file_id does not match the file id in uri",
			})
			return false
		}
	default:
		// Unknown provider_id — rejected here even before the registry
		// lookup, with the same error code clients already handle. The
		// OpenAPI middleware does not gate this field because the
		// typed-parse target (Document) does not include picker_registration;
		// the field is read via a body-sniff in CreateDocument.
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "picker_file_id_mismatch",
			Message: "picker_registration.file_id does not match the file id in uri",
		})
		return false
	}
```

The downstream registry lookup and linked-token checks remain unchanged.

- [ ] **Step 3: Run the unit tests — expect PASS**

```bash
make test-unit name=TestValidatePickerRegistration
```

Expected: all four new tests pass. Any pre-existing tests for `validatePickerRegistration` (Google branch) still pass.

If the Google happy-path test in the existing file fails, the issue is in the new code's `case ProviderGoogleWorkspace` branch — read the diff carefully and ensure the existing logic was preserved verbatim.

- [ ] **Step 4: Run the full unit-test suite**

```bash
make test-unit
```

Expected: all tests pass (no regressions). If anything else in `api/` fails, investigate — the change is narrow, regressions should be rare.

- [ ] **Step 5: Lint**

```bash
make lint
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add api/document_sub_resource_handlers.go
git commit -m "feat(api): dispatch picker_registration validation per provider (#307)"
```

---

## Task 4: Integration test — Microsoft document creation end-to-end

**Files:**
- Create: `api/microsoft_picker_registration_integration_test.go`

The new test exercises `POST /threat_models/{id}/documents` with a Microsoft picker_registration, verifies the document is stored and returned with picker fields populated. Build on the harness in `api/microsoft_delegated_integration_test.go` (stub Graph server, mock token repo).

- [ ] **Step 1: Read the existing integration test for harness patterns**

```bash
sed -n '1,250p' api/microsoft_delegated_integration_test.go
```

Note the build tag (`//go:build dev || test || integration`) and the `stubMicrosoftGraphServer` constructor — the new test reuses these.

- [ ] **Step 2: Read the document-creation integration test (if any) for the threat-model fixture pattern**

```bash
rg -ln "POST.*threat_models.*documents" api/*_integration_test.go
```

If a document-creation integration test exists (e.g. `google_workspace_delegated_integration_test.go` referenced in the spec), read it for the threat-model fixture and HTTP request idioms — copy that pattern instead of inventing one.

```bash
sed -n '1,200p' api/google_workspace_delegated_integration_test.go 2>/dev/null || echo "(file not present; pattern-match other integration tests)"
```

- [ ] **Step 3: Write the integration test file**

Create `api/microsoft_picker_registration_integration_test.go` with:

```go
//go:build dev || test || integration

package api

// TestMicrosoftPickerRegistration_DocumentCreate exercises the contract gap
// closed by issue #307: POST /threat_models/{id}/documents accepts a
// picker_registration payload with provider_id=microsoft. Sub-tests cover the
// happy path plus the three handler-level rejection paths.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMicrosoftPickerRegistration_DocumentCreate_HappyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Reuse the Microsoft delegated harness: stub Graph + seeded token repo.
	// The fixture is identical to TestMicrosoftDelegated_PickerGrantThenFetch
	// for the parts we care about (token + provider registration).
	env := newMicrosoftDocumentCreateEnv(t) // helper defined below

	body := map[string]any{
		"name":        "Marketing Doc",
		"description": "Picker-attached SharePoint document",
		"uri":         "https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/Doc.docx",
		"picker_registration": map[string]string{
			"provider_id": "microsoft",
			"file_id":     "b!abc:01XYZ",
			"mime_type":   "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+env.threatModelID+"/documents", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "Marketing Doc", got["name"])
	require.Equal(t, "https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/Doc.docx", got["uri"])
	// access_status is set to "unknown" until the access poller runs; we just
	// confirm the document was accepted.
	require.NotEmpty(t, got["id"])
}

func TestMicrosoftPickerRegistration_DocumentCreate_NonSharePointURI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newMicrosoftDocumentCreateEnv(t)

	body := map[string]any{
		"name": "Bad URL",
		"uri":  "https://example.com/whatever.docx",
		"picker_registration": map[string]string{
			"provider_id": "microsoft",
			"file_id":     "b!abc:01XYZ",
			"mime_type":   "text/plain",
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+env.threatModelID+"/documents", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "picker_file_id_mismatch")
}

func TestMicrosoftPickerRegistration_DocumentCreate_MalformedFileID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newMicrosoftDocumentCreateEnv(t)

	body := map[string]any{
		"name": "Bad file_id",
		"uri":  "https://contoso.sharepoint.com/sites/Marketing/Doc.docx",
		"picker_registration": map[string]string{
			"provider_id": "microsoft",
			"file_id":     "no-colon-here",
			"mime_type":   "text/plain",
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+env.threatModelID+"/documents", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "picker_file_id_mismatch")
}

func TestMicrosoftPickerRegistration_DocumentCreate_UnknownProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newMicrosoftDocumentCreateEnv(t)

	body := map[string]any{
		"name": "Unknown provider",
		"uri":  "https://example.com/foo",
		"picker_registration": map[string]string{
			"provider_id": "made_up_provider",
			"file_id":     "abc",
			"mime_type":   "text/plain",
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+env.threatModelID+"/documents", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "picker_file_id_mismatch")
}
```

- [ ] **Step 4: Provide `newMicrosoftDocumentCreateEnv` test helper**

This helper sets up a minimal HTTP environment that routes `POST /threat_models/{id}/documents` to the real `DocumentSubResourceHandler` with a stub Graph server, an in-memory document store, a seeded threat model, an authenticated user, and a registered Microsoft content-OAuth provider with an active token.

**Read the existing test helpers first** — there's likely already a function that builds a routed `*gin.Engine` with the document handler attached. Search for it:

```bash
rg -n "func.*newDocumentTestEnv|setupDocumentHandlerTest|TestThreatModelDocuments_" api/*_test.go | head -10
```

If a similar helper exists (it likely does, given the existing google_workspace integration test), reuse it and parameterize for Microsoft. **Do not duplicate** infrastructure that already exists.

If no equivalent helper exists, add the helper to the new test file. The helper must:

1. Construct an in-memory `DocumentRepository` (use the existing `InMemoryDocumentStore` if present, or a sqlite in-memory backed `GormDocumentRepository`).
2. Pre-create a threat model that the test user owns. Capture its UUID into `env.threatModelID`.
3. Build a `DocumentSubResourceHandler`, call all the relevant `Set*` setters: `SetContentOAuthRegistry` (with Microsoft registered), `SetContentTokens` (with an active token for the test user), `SetDocumentURIValidator` (an SSRF validator that allows `*.sharepoint.com` and `example.com` — match what the existing google integration test does), and the cache invalidator.
4. Attach an authentication middleware that injects a fixed test user (`alice` or `u1`).
5. Register the route `POST /threat_models/:threat_model_id/documents` → `handler.CreateDocument`.
6. Return `env := struct { router *gin.Engine; threatModelID string }`.

**Concretely, the function signature**:

```go
type microsoftDocumentCreateEnv struct {
	router        *gin.Engine
	threatModelID string
}

func newMicrosoftDocumentCreateEnv(t *testing.T) *microsoftDocumentCreateEnv {
	t.Helper()
	// ... mirror api/google_workspace_delegated_integration_test.go's env factory
	// but register ProviderMicrosoft instead of ProviderGoogleWorkspace.
}
```

**If the patterns in google_workspace_delegated_integration_test.go are too divergent to fit a "mirror it" approach, stop and read that whole file before continuing — the design assumes the env factory is generalizable. If it isn't, the cheapest move is to lift the env construction inline into each sub-test.**

- [ ] **Step 5: Run the integration test — expect PASS**

```bash
make test-integration name=TestMicrosoftPickerRegistration_DocumentCreate
```

Expected: all four sub-tests pass.

If the integration setup (DB, redis) is not running, start it first:

```bash
make start-database
make start-redis
```

If the helper construction fails because of missing infrastructure (e.g., the existing google env factory uses real Postgres), you have two options:
1. Use the same real-Postgres harness — the integration target already starts it.
2. Use sqlite-in-memory if the existing tests do that.

Match what the closest existing integration test does. **Do not invent a new harness pattern.**

- [ ] **Step 6: Run the full integration suite to check for regressions**

```bash
make test-integration
```

Expected: only pre-existing failures (`TestAuthFlowRateLimiting_MultiScope`, `TestClientCredentialsCRUD`, `TestIPRateLimiting_PublicEndpoints`, `TestCascadeDeletion` — listed as known failures in #249's tracking comments) remain. No new failures.

- [ ] **Step 7: Lint**

```bash
make lint
```

- [ ] **Step 8: Commit**

```bash
git add api/microsoft_picker_registration_integration_test.go
git commit -m "test(api): integration test for microsoft picker_registration on document create (#307)"
```

---

## Task 5: Final verification and ship

- [ ] **Step 1: Run the complete quality gate**

```bash
make lint
make validate-openapi
make build-server
make test-unit
make test-integration
```

All clean (modulo pre-existing integration failures noted above).

- [ ] **Step 2: Oracle DB review check**

This change does not touch DB code (no migrations, no GORM models, no repository methods, no SQL). The picker fields it interacts with were already added on `dev/1.4.0`. Skip the oracle-db-admin dispatch and note this in the close-out comment.

- [ ] **Step 3: Review the full branch diff**

```bash
git log --oneline f7d829c2..HEAD
git diff f7d829c2..HEAD --stat
```

Expected: 4 commits (one per task), changes limited to the files listed in the file-structure table at the top of this plan.

- [ ] **Step 4: Push**

```bash
git pull --rebase
git push
git status
```

Expected: `up to date with origin`.

- [ ] **Step 5: Close the issue**

Per the per-branch closure rule (memory: feature/dev branches don't auto-close):

```bash
gh issue comment 307 --body "Resolved on branch dev/1.4.0 in commits ...

- OpenAPI \`PickerRegistration.provider_id\` enum widened to include \`microsoft\`; descriptions generalized.
- \`validatePickerRegistration\` dispatches per provider: Google keeps \`extractGoogleDriveFileID\` check; Microsoft verifies \`*.sharepoint.com\` host suffix + \`decodeMicrosoftPickerFileID\` parse.
- Integration test \`TestMicrosoftPickerRegistration_DocumentCreate_*\` covers happy path + 3 negative cases.

Pre-existing finding noted: \`BulkCreateDocuments\` does not process \`picker_registration\` (binds to []Document, never sniffs). Not introduced by #307, file separately if bulk needs picker support."
gh issue close 307
```

Replace the `commits ...` placeholder with the actual SHAs from `git log`.

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| OpenAPI enum widening | Task 1 |
| OpenAPI description generalization | Task 1 |
| Per-provider switch in validator | Task 3 |
| Microsoft host check + decodeMicrosoftPickerFileID | Task 3 |
| Default case for unknown provider_id | Task 3 |
| Error code preservation (picker_file_id_mismatch) | Task 3 |
| Integration test (4 sub-tests) | Task 4 |
| Bulk endpoint stays out of scope | Documented in plan; no task |

All spec requirements have a matching task. ✓

**Placeholder scan:** No "TBD" / "implement appropriately" / "handle edge cases" / "similar to Task N". Each step has runnable commands or full code blocks. ✓

**Type consistency:** `validatePickerRegistration` signature, `pickerRegistrationSniff` shape, `ProviderMicrosoft` / `ProviderGoogleWorkspace` constants, `decodeMicrosoftPickerFileID` signature — all match what's in the codebase as of the exploration done before writing the plan. ✓

One acknowledged risk: the test helpers `newDocumentSubResourceHandlerForTest` / `newGinContextForTest` / `newMicrosoftDocumentCreateEnv` are placeholders for whatever the existing integration tests use. The plan explicitly tells the executor to read existing tests first and reuse their patterns. If the existing harness can't be reused, the executor inlines the setup — see Task 4 Step 4. This is a deliberate choice: writing fully-runnable test setup code requires reading files the plan author can't fully predict; the alternative (inventing helpers) would produce code that may not compile against actual repository state.
