# Google Workspace Delegated Picker — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a delegated Google Workspace content source that uses `drive.file` scope + Google Picker so users can attach Drive documents via their own identity; add structured access diagnostics to the document API so tmi-ux can render localized remediation UI.

**Architecture:** New `DelegatedGoogleWorkspaceSource` embeds the existing `DelegatedSource` helper from sub-project 1. A new document-aware dispatch method on `ContentSourceRegistry` routes to delegated when the document was picker-registered and the owner has an active linked token, otherwise falls through to the existing service-account/HTTP sources. Picker metadata lives as columns on the `documents` row (1:1 with the document). Access diagnostics (`reason_code` + `remediations[]`) are assembled at read time per-viewer.

**Tech Stack:** Go 1.22+, Gin, GORM, PostgreSQL, Redis (for OAuth state, already in place), `google.golang.org/api/drive/v3` (service-account client reused for delegated), `oapi-codegen v2` for OpenAPI codegen, testify, miniredis for tests. Database schema is managed via GORM `AutoMigrate` — there are **no SQL migration files** in this repo. Integration tests use `make test-integration` against real Postgres.

**Spec:** [`docs/superpowers/specs/2026-04-18-google-workspace-delegated-picker-design.md`](../specs/2026-04-18-google-workspace-delegated-picker-design.md)

**Branch discipline:** All work on `feature/google-workspace-picker` branched from `dev/1.4.0`. One commit per task unless a task is part of a tightly-coupled TDD red→green→refactor triplet, in which case the task groups the commits as noted. No commits directly to `dev/1.4.0` from this plan.

---

## File structure

### New files

| Path | Responsibility |
|---|---|
| `api/content_source_google_workspace.go` | `DelegatedGoogleWorkspaceSource`: implements `ContentSource` + `AccessValidator` + `AccessRequester`, embeds `DelegatedSource`, builds the Drive v3 client with the delegated access token. |
| `api/content_source_google_workspace_test.go` | Unit tests for the source. |
| `api/content_source_registry_for_document.go` | New `FindSourceForDocument` method on `ContentSourceRegistry`. Kept in its own file to avoid merge conflicts with existing `content_source.go`. |
| `api/content_source_registry_for_document_test.go` | Unit tests for dispatch. |
| `api/picker_token_handler.go` | `POST /me/picker_tokens/{provider_id}` handler. |
| `api/picker_token_handler_test.go` | Handler tests. |
| `api/access_diagnostics.go` | Read-time builder: `reason_code` + caller context → `DocumentAccessDiagnostics`. |
| `api/access_diagnostics_test.go` | Builder tests. |
| `internal/config/google_workspace_config.go` | New `GoogleWorkspaceConfig` struct (separate file to avoid bloating `content_sources.go`). |
| `scripts/google-picker-harness/index.html` | Static HTML+JS file that loads Google Picker and displays picker results. Used only by the manual integration test. |
| `test/integration/manual/google_workspace_delegated_test.go` | Manual (`//go:build manual`) end-to-end test driver. |
| `test/integration/workflows/google_workspace_delegated_test.go` | Automated integration test against real Postgres + stub OAuth provider. |

### Modified files

| Path | Change |
|---|---|
| `api/models/models.go` | Add `PickerProviderID`, `PickerFileID`, `PickerMimeType`, `AccessReasonCode`, `AccessReasonDetail`, `AccessStatusUpdatedAt` to `Document`. Add partial index on `(picker_provider_id, picker_file_id)`. |
| `api/document_store.go` | Extend `DocumentStore` interface: add `UpdateAccessStatusWithDiagnostics` method. |
| `api/document_store_gorm.go` | Implement `UpdateAccessStatusWithDiagnostics`; extend existing conversions to map the new model fields. |
| `api/document_store_gorm_test.go` | Tests for new method. |
| `api/document_sub_resource_handlers.go` | Extend attach handler to accept `picker_registration`; use `FindSourceForDocument` for initial validation; write diagnostics with status transitions. |
| `api/document_sub_resource_handlers_test.go` | New tests for picker-registration validation paths. |
| `api/access_poller.go` | Use `FindSourceForDocument(ctx, doc, doc.OwnerID)` and attach owner context before dispatch. |
| `api/access_poller_test.go` | Update existing tests + add coverage for picker-registered documents. |
| `api/content_oauth_handlers.go` | Extend the `DELETE /me/content_tokens/{provider_id}` handler with the un-link cascade (clear picker columns transactionally). |
| `api/content_oauth_handlers_test.go` | Extend with cascade-clearing tests. |
| `internal/config/content_sources.go` | Add `GoogleWorkspace GoogleWorkspaceConfig` field; wire env-var overrides. |
| `cmd/server/main.go` | Register `DelegatedGoogleWorkspaceSource` when configured; enforce startup validation that `content_sources.google_workspace.enabled` requires `content_oauth.providers.google_workspace.enabled`; register picker-token route. |
| `api-schema/tmi-openapi.json` | Add `PickerRegistration`, `PickerTokenResponse`, `DocumentAccessDiagnostics`, `AccessRemediation` schemas; add `mintPickerToken` operation on `/me/picker_tokens/{provider_id}`; extend document-attach request bodies with optional `picker_registration`; extend document response schema with `access_diagnostics` and `access_status_updated_at`. Regenerate `api/api.go` via `make generate-api`. |
| `Makefile` | Add `test-manual-google-workspace` target. |

### Regenerated files

| Path | Notes |
|---|---|
| `api/api.go` | Generated from OpenAPI spec via `oapi-codegen v2`. Do not edit by hand; regenerate via `make generate-api`. |

---

## Branch creation (do this first)

- [ ] **Step 0.1: Create feature branch**

Run from `dev/1.4.0`:
```bash
git checkout dev/1.4.0
git pull --rebase origin dev/1.4.0
git checkout -b feature/google-workspace-picker
```

Expected: `git branch --show-current` prints `feature/google-workspace-picker`.

---

## Phase 1 — Data model + diagnostics columns

### Task 1.1: Add picker + diagnostics fields to `Document` model

**Files:**
- Modify: `api/models/models.go:356-372`

- [ ] **Step 1.1.1: Write failing test asserting the new fields exist**

Append to `api/models/models_test.go` (or create `api/models/document_picker_fields_test.go` if the existing test file is already large):

```go
package models

import (
	"testing"
	"time"
)

func TestDocument_HasPickerAndDiagnosticFields(t *testing.T) {
	// Compile-time verification via zero-value field access.
	d := Document{}
	var _ *string = d.PickerProviderID
	var _ *string = d.PickerFileID
	var _ *string = d.PickerMimeType
	var _ *string = d.AccessReasonCode
	var _ *string = d.AccessReasonDetail
	var _ *time.Time = d.AccessStatusUpdatedAt
}
```

- [ ] **Step 1.1.2: Run test to confirm it fails**

```
make test-unit name=TestDocument_HasPickerAndDiagnosticFields
```

Expected: compile error — fields do not exist on `Document`.

- [ ] **Step 1.1.3: Add fields to the `Document` struct**

Edit `api/models/models.go:356-372`, modifying the `Document` struct to add the new fields. Resulting struct:

```go
type Document struct {
	ID              string     `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID   string     `gorm:"type:varchar(36);not null;index:idx_docs_tm;index:idx_docs_tm_created,priority:1;index:idx_docs_tm_modified,priority:1"`
	Name            string     `gorm:"type:varchar(256);not null;index:idx_docs_name"`
	URI             string     `gorm:"type:varchar(1000);not null"`
	Description     *string    `gorm:"type:varchar(2048)"`
	IncludeInReport DBBool     `gorm:"default:1"`
	TimmyEnabled    DBBool     `gorm:"default:1"`
	AccessStatus    *string    `gorm:"type:varchar(32);default:unknown"`
	ContentSource   *string    `gorm:"type:varchar(64)"`

	// Picker registration (all three set together or all null — enforced by application code).
	PickerProviderID *string `gorm:"type:varchar(64);index:idx_docs_picker,priority:1"`
	PickerFileID     *string `gorm:"type:varchar(255);index:idx_docs_picker,priority:2"`
	PickerMimeType   *string `gorm:"type:varchar(128)"`

	// Access diagnostics (populated when access_status != accessible/unknown).
	AccessReasonCode      *string    `gorm:"type:varchar(64)"`
	AccessReasonDetail    *string    `gorm:"type:text"`
	AccessStatusUpdatedAt *time.Time

	CreatedAt  time.Time  `gorm:"not null;autoCreateTime;index:idx_docs_created;index:idx_docs_tm_created,priority:2"`
	ModifiedAt time.Time  `gorm:"not null;autoUpdateTime;index:idx_docs_modified;index:idx_docs_tm_modified,priority:2"`
	DeletedAt  *time.Time `gorm:"index:idx_docs_deleted_at"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}
```

Note: GORM does not natively express partial indexes in struct tags. The `idx_docs_picker` composite index is created unconditionally; we rely on PostgreSQL to ignore null rows efficiently (composite indexes on `(nullable, nullable)` columns store null entries but are extremely cheap at query time). If partial-index creation becomes a priority later, a separate migration utility can ADD INDEX `WHERE picker_provider_id IS NOT NULL` — that's a follow-up, not blocking.

- [ ] **Step 1.1.4: Run test to verify it passes**

```
make test-unit name=TestDocument_HasPickerAndDiagnosticFields
```

Expected: PASS.

- [ ] **Step 1.1.5: Run the broader models test + document-store tests**

```
make test-unit name=TestDocument
```

Expected: all pass (AutoMigrate in test-db should add the columns).

- [ ] **Step 1.1.6: Commit**

```bash
git add api/models/models.go api/models/models_test.go
git commit -m "feat(models): add picker + access diagnostics fields to Document (#249)"
```

---

### Task 1.2: Extend `DocumentStore` interface with diagnostics-aware update

**Files:**
- Modify: `api/document_store.go` (around line 30)
- Test: (exercised indirectly via GORM impl tests in Task 1.3)

- [ ] **Step 1.2.1: Add the interface method**

Edit `api/document_store.go`. Add to the `DocumentStore` interface, immediately after the existing `UpdateAccessStatus` method:

```go
// UpdateAccessStatusWithDiagnostics sets access tracking fields on a document, including
// the diagnostic reason code and detail. reasonCode may be empty to clear the diagnostic.
// reasonDetail should be empty unless reasonCode == "other".
UpdateAccessStatusWithDiagnostics(
	ctx context.Context,
	id string,
	accessStatus string,
	contentSource string,
	reasonCode string,
	reasonDetail string,
) error
```

- [ ] **Step 1.2.2: Verify build fails due to missing implementation**

```
make build-server
```

Expected: compile error in `api/document_store_gorm.go` and any mock that implements `DocumentStore`.

- [ ] **Step 1.2.3: Find all `DocumentStore` mocks and fakes**

```
rg -l 'DocumentStore' api/ test/ | rg -v '_test\.go$' | head -20
```

Note: capture the list of mocks/fakes that need method stubs added before the build can pass. Each must gain a stub (see Task 1.3 Step 1.3.4 for mock updates).

- [ ] **Step 1.2.4: Commit (build still broken — that's intentional; fix in Task 1.3)**

Do **not** commit the broken build yet. Leave the interface change staged. Task 1.3 ships both the interface update and the GORM impl in one commit.

---

### Task 1.3: Implement `UpdateAccessStatusWithDiagnostics` on GORM store

**Files:**
- Modify: `api/document_store_gorm.go:533-550` (around the existing `UpdateAccessStatus`)
- Modify: `api/document_store_gorm.go:60-100` (toDocument / toModel conversions to include new fields)
- Modify: `api/document_sub_resource_handlers_test.go:96-105` (MockDocumentStore stub)
- Test: `api/document_store_gorm_test.go` (new test file — or append if existing)

- [ ] **Step 1.3.1: Write failing test**

Create `api/document_store_gorm_diagnostics_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGormDocumentStore_UpdateAccessStatusWithDiagnostics(t *testing.T) {
	store, cleanup := newTestDocumentStore(t)
	defer cleanup()

	ctx := context.Background()
	tmID := createTestThreatModel(t, store)

	doc := newTestDocument(tmID, "https://docs.google.com/document/d/abc/edit")
	require.NoError(t, store.Create(ctx, doc, tmID))

	err := store.UpdateAccessStatusWithDiagnostics(
		ctx,
		doc.Id.String(),
		AccessStatusPendingAccess,
		"google_workspace",
		"no_accessible_source",
		"",
	)
	require.NoError(t, err)

	fetched, err := store.GetByID(ctx, doc.Id.String())
	require.NoError(t, err)
	require.NotNil(t, fetched.AccessStatus)
	assert.Equal(t, AccessStatusPendingAccess, string(*fetched.AccessStatus))
	assert.Equal(t, "google_workspace", *fetched.ContentSource)

	// AccessReasonCode and AccessStatusUpdatedAt should be populated on the stored row.
	// Verify by direct GORM query since the API-level Document type may not expose them yet.
	var rawModel models.DocumentModel // replace with the actual GORM model type name — see Task 1.1.3
	require.NoError(t, store.db.First(&rawModel, "id = ?", doc.Id.String()).Error)
	require.NotNil(t, rawModel.AccessReasonCode)
	assert.Equal(t, "no_accessible_source", *rawModel.AccessReasonCode)
	require.NotNil(t, rawModel.AccessStatusUpdatedAt)
}
```

Note: the exact type name for the GORM document model should match what's in `api/models/models.go` (`Document` — the GORM model type in that package). Adjust the test's model reference accordingly if the test is in the `api` package which imports `api/models`.

- [ ] **Step 1.3.2: Run test; confirm failure**

```
make test-unit name=TestGormDocumentStore_UpdateAccessStatusWithDiagnostics
```

Expected: compile error (method doesn't exist on the store yet).

- [ ] **Step 1.3.3: Implement the method on `GormDocumentStore`**

Edit `api/document_store_gorm.go:533-550`. Add below the existing `UpdateAccessStatus`:

```go
// UpdateAccessStatusWithDiagnostics sets access tracking fields on a document.
// See DocumentStore.UpdateAccessStatusWithDiagnostics.
func (s *GormDocumentStore) UpdateAccessStatusWithDiagnostics(
	ctx context.Context,
	id string,
	accessStatus string,
	contentSource string,
	reasonCode string,
	reasonDetail string,
) error {
	updates := map[string]interface{}{
		"access_status":            accessStatus,
		"access_status_updated_at": time.Now(),
	}
	if contentSource != "" {
		updates["content_source"] = contentSource
	}
	if reasonCode == "" {
		updates["access_reason_code"] = nil
		updates["access_reason_detail"] = nil
	} else {
		updates["access_reason_code"] = reasonCode
		if reasonDetail == "" {
			updates["access_reason_detail"] = nil
		} else {
			updates["access_reason_detail"] = reasonDetail
		}
	}
	return s.db.WithContext(ctx).
		Model(&models.Document{}).
		Where("id = ?", id).
		Updates(updates).Error
}
```

- [ ] **Step 1.3.4: Add stub to `MockDocumentStore`**

Edit `api/document_sub_resource_handlers_test.go:96-105`. Immediately after the existing `UpdateAccessStatus` mock method, add:

```go
func (m *MockDocumentStore) UpdateAccessStatusWithDiagnostics(
	ctx context.Context, id string, accessStatus string, contentSource string,
	reasonCode string, reasonDetail string,
) error {
	args := m.Called(ctx, id, accessStatus, contentSource, reasonCode, reasonDetail)
	return args.Error(0)
}
```

Also update any other mock/fake discovered by the `rg` command in Task 1.2.3. For each file listed there, add the same method stub that returns `nil` (for non-testify mocks) or delegates to `m.Called` (for testify mocks).

- [ ] **Step 1.3.5: Run the store test**

```
make test-unit name=TestGormDocumentStore_UpdateAccessStatusWithDiagnostics
```

Expected: PASS.

- [ ] **Step 1.3.6: Run full build + unit tests**

```
make build-server
make test-unit
```

Expected: clean build, all unit tests pass.

- [ ] **Step 1.3.7: Commit**

```bash
git add api/document_store.go api/document_store_gorm.go \
        api/document_store_gorm_diagnostics_test.go \
        api/document_sub_resource_handlers_test.go
git commit -m "feat(documents): add UpdateAccessStatusWithDiagnostics store method (#249)"
```

---

## Phase 2 — Access diagnostics assembly

### Task 2.1: Define `reason_code` and `remediation_action` constants

**Files:**
- Create: `api/access_diagnostics.go`
- Test: `api/access_diagnostics_test.go`

- [ ] **Step 2.1.1: Write failing test for constant existence**

Create `api/access_diagnostics_test.go`:

```go
package api

import "testing"

func TestDiagnosticConstants(t *testing.T) {
	cases := []string{
		ReasonTokenNotLinked,
		ReasonTokenRefreshFailed,
		ReasonTokenTransientFailure,
		ReasonPickerRegistrationInvalid,
		ReasonNoAccessibleSource,
		ReasonSourceNotFound,
		ReasonFetchError,
		ReasonOther,
	}
	for _, c := range cases {
		if c == "" {
			t.Error("reason code constant is empty")
		}
	}
	actions := []string{
		RemediationLinkAccount,
		RemediationRelinkAccount,
		RemediationRepickFile,
		RemediationShareWithServiceAccount,
		RemediationRepickAfterShare,
		RemediationRetry,
		RemediationContactOwner,
	}
	for _, a := range actions {
		if a == "" {
			t.Error("remediation action constant is empty")
		}
	}
}
```

- [ ] **Step 2.1.2: Run test to confirm failure**

```
make test-unit name=TestDiagnosticConstants
```

Expected: compile error.

- [ ] **Step 2.1.3: Create `api/access_diagnostics.go` with constants**

```go
package api

// Reason codes for DocumentAccessDiagnostics.reason_code (stable API contract).
const (
	ReasonTokenNotLinked            = "token_not_linked"
	ReasonTokenRefreshFailed        = "token_refresh_failed"
	ReasonTokenTransientFailure     = "token_transient_failure"
	ReasonPickerRegistrationInvalid = "picker_registration_invalid"
	ReasonNoAccessibleSource        = "no_accessible_source"
	ReasonSourceNotFound            = "source_not_found"
	ReasonFetchError                = "fetch_error"
	ReasonOther                     = "other"
)

// Remediation actions for DocumentAccessDiagnostics.remediations[].action.
const (
	RemediationLinkAccount             = "link_account"
	RemediationRelinkAccount           = "relink_account"
	RemediationRepickFile              = "repick_file"
	RemediationShareWithServiceAccount = "share_with_service_account"
	RemediationRepickAfterShare        = "repick_after_share"
	RemediationRetry                   = "retry"
	RemediationContactOwner            = "contact_owner"
)
```

- [ ] **Step 2.1.4: Run test**

```
make test-unit name=TestDiagnosticConstants
```

Expected: PASS.

- [ ] **Step 2.1.5: Commit**

```bash
git add api/access_diagnostics.go api/access_diagnostics_test.go
git commit -m "feat(diagnostics): add reason_code and remediation_action constants (#249)"
```

---

### Task 2.2: Implement the diagnostics builder

**Files:**
- Modify: `api/access_diagnostics.go`
- Modify: `api/access_diagnostics_test.go`

- [ ] **Step 2.2.1: Write failing tests for the builder**

Append to `api/access_diagnostics_test.go`:

```go
func TestBuildAccessDiagnostics_NoReason(t *testing.T) {
	ctx := BuilderContext{
		ReasonCode:              "",
		ReasonDetail:            "",
		CallerUserEmail:         "alice@example.com",
		CallerLinkedProviders:   map[string]bool{"google_workspace": true},
		ServiceAccountEmail:     "indexer@tmi.iam.gserviceaccount.com",
	}
	d := BuildAccessDiagnostics(ctx)
	if d != nil {
		t.Fatalf("expected nil diagnostics when reason_code is empty, got %+v", d)
	}
}

func TestBuildAccessDiagnostics_TokenNotLinked(t *testing.T) {
	ctx := BuilderContext{ReasonCode: ReasonTokenNotLinked, ProviderID: "google_workspace"}
	d := BuildAccessDiagnostics(ctx)
	if d == nil || d.ReasonCode != ReasonTokenNotLinked {
		t.Fatalf("unexpected diagnostics: %+v", d)
	}
	if len(d.Remediations) != 1 || d.Remediations[0].Action != RemediationLinkAccount {
		t.Fatalf("expected link_account remediation, got %+v", d.Remediations)
	}
	if d.Remediations[0].Params["provider_id"] != "google_workspace" {
		t.Fatalf("expected provider_id param, got %+v", d.Remediations[0].Params)
	}
}

func TestBuildAccessDiagnostics_NoAccessibleSource_Unlinked(t *testing.T) {
	ctx := BuilderContext{
		ReasonCode:          ReasonNoAccessibleSource,
		ServiceAccountEmail: "indexer@tmi.iam.gserviceaccount.com",
	}
	d := BuildAccessDiagnostics(ctx)
	if len(d.Remediations) != 1 {
		t.Fatalf("expected 1 remediation, got %d", len(d.Remediations))
	}
	if d.Remediations[0].Action != RemediationShareWithServiceAccount {
		t.Fatalf("expected share_with_service_account, got %s", d.Remediations[0].Action)
	}
	if d.Remediations[0].Params["service_account_email"] != "indexer@tmi.iam.gserviceaccount.com" {
		t.Fatalf("missing service_account_email param")
	}
}

func TestBuildAccessDiagnostics_NoAccessibleSource_Linked(t *testing.T) {
	ctx := BuilderContext{
		ReasonCode:            ReasonNoAccessibleSource,
		ServiceAccountEmail:   "indexer@tmi.iam.gserviceaccount.com",
		CallerUserEmail:       "alice@example.com",
		CallerLinkedProviders: map[string]bool{"google_workspace": true},
	}
	d := BuildAccessDiagnostics(ctx)
	if len(d.Remediations) != 2 {
		t.Fatalf("expected 2 remediations, got %d: %+v", len(d.Remediations), d.Remediations)
	}
	if d.Remediations[0].Action != RemediationShareWithServiceAccount {
		t.Fatalf("expected primary=share_with_service_account, got %s", d.Remediations[0].Action)
	}
	if d.Remediations[1].Action != RemediationRepickAfterShare {
		t.Fatalf("expected secondary=repick_after_share, got %s", d.Remediations[1].Action)
	}
	if d.Remediations[1].Params["user_email"] != "alice@example.com" {
		t.Fatalf("missing user_email param")
	}
}

func TestBuildAccessDiagnostics_Other_IncludesDetail(t *testing.T) {
	ctx := BuilderContext{ReasonCode: ReasonOther, ReasonDetail: "drive quota exceeded"}
	d := BuildAccessDiagnostics(ctx)
	if d.ReasonDetail == nil || *d.ReasonDetail != "drive quota exceeded" {
		t.Fatalf("expected reason_detail passthrough, got %v", d.ReasonDetail)
	}
	if len(d.Remediations) != 0 {
		t.Fatalf("expected empty remediations for 'other', got %+v", d.Remediations)
	}
}
```

- [ ] **Step 2.2.2: Run tests; confirm they fail (undefined `BuildAccessDiagnostics` / `BuilderContext` / etc.)**

```
make test-unit name=TestBuildAccessDiagnostics
```

Expected: compile errors.

- [ ] **Step 2.2.3: Add the builder types and function to `api/access_diagnostics.go`**

Append to `api/access_diagnostics.go`:

```go
// BuilderContext carries everything the builder needs to assemble diagnostics.
// Empty fields are treated as "not applicable" — the builder tolerates missing context.
type BuilderContext struct {
	ReasonCode   string
	ReasonDetail string

	// Provider identifier relevant to the error (e.g. "google_workspace"), used to
	// populate remediation params.
	ProviderID string

	// Caller context (the user viewing the document, not necessarily the owner).
	CallerUserEmail       string
	CallerLinkedProviders map[string]bool // provider_id -> has-active-token

	// Config-sourced values for specific remediations.
	ServiceAccountEmail string

	// Owner context (optional; used for contact_owner remediation).
	DocumentOwnerEmail string
}

// AccessRemediationDiag is the builder-side representation of a remediation.
// The API-wire type is regenerated from OpenAPI (see Phase 8).
type AccessRemediationDiag struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params"`
}

// AccessDiagnosticsDiag is the builder-side representation of access_diagnostics.
// The API-wire type is regenerated from OpenAPI (see Phase 8); this internal type
// is converted at the handler boundary.
type AccessDiagnosticsDiag struct {
	ReasonCode   string                  `json:"reason_code"`
	ReasonDetail *string                 `json:"reason_detail,omitempty"`
	Remediations []AccessRemediationDiag `json:"remediations"`
}

// BuildAccessDiagnostics returns a diagnostic object given the builder context,
// or nil when there is no diagnostic to report (empty ReasonCode).
func BuildAccessDiagnostics(ctx BuilderContext) *AccessDiagnosticsDiag {
	if ctx.ReasonCode == "" {
		return nil
	}
	d := &AccessDiagnosticsDiag{
		ReasonCode:   ctx.ReasonCode,
		Remediations: []AccessRemediationDiag{},
	}
	if ctx.ReasonCode == ReasonOther && ctx.ReasonDetail != "" {
		det := ctx.ReasonDetail
		d.ReasonDetail = &det
	}

	switch ctx.ReasonCode {
	case ReasonTokenNotLinked:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationLinkAccount,
			Params: map[string]interface{}{"provider_id": ctx.ProviderID},
		})
	case ReasonTokenRefreshFailed:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationRelinkAccount,
			Params: map[string]interface{}{"provider_id": ctx.ProviderID},
		})
	case ReasonTokenTransientFailure, ReasonFetchError:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationRetry,
			Params: map[string]interface{}{},
		})
	case ReasonPickerRegistrationInvalid:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationRepickFile,
			Params: map[string]interface{}{"provider_id": ctx.ProviderID},
		})
	case ReasonNoAccessibleSource:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationShareWithServiceAccount,
			Params: map[string]interface{}{"service_account_email": ctx.ServiceAccountEmail},
		})
		if ctx.CallerLinkedProviders["google_workspace"] {
			d.Remediations = append(d.Remediations, AccessRemediationDiag{
				Action: RemediationRepickAfterShare,
				Params: map[string]interface{}{
					"provider_id": "google_workspace",
					"user_email":  ctx.CallerUserEmail,
				},
			})
		}
	case ReasonSourceNotFound, ReasonOther:
		// No remediations.
	}
	return d
}
```

- [ ] **Step 2.2.4: Run tests**

```
make test-unit name=TestBuildAccessDiagnostics
```

Expected: all PASS.

- [ ] **Step 2.2.5: Commit**

```bash
git add api/access_diagnostics.go api/access_diagnostics_test.go
git commit -m "feat(diagnostics): add BuildAccessDiagnostics builder (#249)"
```

---

### Task 2.3: Wire diagnostics into document GET handler response

**Files:**
- Modify: `api/document_sub_resource_handlers.go` (locate the document-GET handler; search for the handler that serializes a single document)
- Modify: `api/document_sub_resource_handlers_test.go`

- [ ] **Step 2.3.1: Identify the GET handler entry point**

```
rg -n 'func.*getDocument|GetDocument\(' api/document_sub_resource_handlers.go | head
```

Find the function that handles `GET /threat_models/{id}/documents/{did}` (per the OpenAPI spec) and the one that lists documents. Note their exact names for later steps.

- [ ] **Step 2.3.2: Write failing test**

Append to `api/document_sub_resource_handlers_test.go`:

```go
func TestGetDocument_IncludesAccessDiagnostics(t *testing.T) {
	// Setup: a document with access_status=pending_access, reason=no_accessible_source,
	// caller has linked google_workspace.
	// Exercise: GET /threat_models/{tmid}/documents/{did}.
	// Assert: response body contains access_diagnostics with expected shape.
	t.Skip("IMPLEMENT: wire diagnostics into GET handler (Task 2.3)")
}
```

- [ ] **Step 2.3.3: Replace the skip with a concrete test**

Replace the `t.Skip(...)` with code that:
1. Uses the existing `MockDocumentStore` setup in this file.
2. Arranges a `Document` with `AccessStatus=pending_access`, `AccessReasonCode=no_accessible_source`.
3. Arranges a caller `User` with a linked `google_workspace` content token (mock `ContentTokenRepository.ListByUser` to return one).
4. Calls the handler through the Gin test router.
5. Asserts the response body JSON has `access_diagnostics.reason_code == "no_accessible_source"` and two remediations in the expected order.

Pattern: follow existing handler-test style in this file. Reuse helpers defined near `TestCreateDocument*` tests.

The full test code (write this verbatim, adapting only the helper names `newTestHandler`, `newAuthedContext`, `doGet` to whatever is already used in the file):

```go
func TestGetDocument_IncludesAccessDiagnostics(t *testing.T) {
	handler, mockStore, mockTokenRepo, router := newTestHandlerWithTokens(t)

	docID := uuid.New()
	tmID := uuid.New()
	ownerID := "alice"
	pendingStatus := "pending_access"
	source := "google_workspace"
	reason := ReasonNoAccessibleSource

	doc := Document{
		Id:              &docID,
		Name:            "test.gdoc",
		Uri:             "https://docs.google.com/document/d/abc/edit",
		AccessStatus:    asPtr(DocumentAccessStatusPendingAccess),
		ContentSource:   &source,
	}
	// Model-level fields (not on API Document) are set via the store mock directly.
	mockStore.On("GetByID", mock.Anything, docID.String()).Return(&doc, nil)
	mockStore.On("GetAccessReason", mock.Anything, docID.String()).
		Return(reason, "", time.Now(), nil)

	mockTokenRepo.On("ListByUser", mock.Anything, ownerID).
		Return([]ContentToken{{ProviderID: "google_workspace", Status: ContentTokenStatusActive}}, nil)

	req := httptest.NewRequest("GET", "/threat_models/"+tmID.String()+"/documents/"+docID.String(), nil)
	req = req.WithContext(WithUserID(req.Context(), ownerID))
	// Add auth claims that the handler expects (copy pattern from TestCreateDocument*).
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, 200, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	diag, ok := body["access_diagnostics"].(map[string]interface{})
	require.True(t, ok, "expected access_diagnostics in response")
	assert.Equal(t, "no_accessible_source", diag["reason_code"])
	rem, ok := diag["remediations"].([]interface{})
	require.True(t, ok)
	require.Len(t, rem, 2)
	r0 := rem[0].(map[string]interface{})
	assert.Equal(t, "share_with_service_account", r0["action"])
	r1 := rem[1].(map[string]interface{})
	assert.Equal(t, "repick_after_share", r1["action"])
}
```

Helpers referenced (`newTestHandlerWithTokens`, `asPtr`) may need to be added to the test file. If `asPtr` doesn't already exist, add this in the same file:

```go
func asPtr[T any](v T) *T { return &v }
```

`newTestHandlerWithTokens` should mirror the existing test setup helper but also wire a `ContentTokenRepository` mock. If the existing handler struct doesn't yet have a `ContentTokenRepository` field, this step surfaces that need — add it as part of this task.

- [ ] **Step 2.3.4: Run test; confirm failure**

```
make test-unit name=TestGetDocument_IncludesAccessDiagnostics
```

Expected: fail (either compile errors or the handler doesn't emit `access_diagnostics` yet).

- [ ] **Step 2.3.5: Extend the handler**

In `api/document_sub_resource_handlers.go`:

1. Add (if absent) a `ContentTokenRepository` field to the handler struct and wire it through the constructor.
2. In the document-GET handler, after loading the document, fetch diagnostic fields (`access_reason_code`, `access_reason_detail`) and the caller's linked providers. Assemble the API response using `BuildAccessDiagnostics`.

Sketch for the handler body (merge into the existing function):

```go
// Assemble access diagnostics for the response.
if doc.AccessStatus != nil && (*doc.AccessStatus == DocumentAccessStatusPendingAccess || *doc.AccessStatus == DocumentAccessStatusAuthRequired) {
	reasonCode, reasonDetail, updatedAt, err := h.documentStore.GetAccessReason(c.Request.Context(), doc.Id.String())
	if err != nil {
		logger.Warn("failed to load access diagnostic fields: %v", err)
	} else if reasonCode != "" {
		linkedProviders := map[string]bool{}
		if h.contentTokens != nil {
			if tokens, err := h.contentTokens.ListByUser(c.Request.Context(), user.InternalUUID); err == nil {
				for _, t := range tokens {
					if t.Status == ContentTokenStatusActive {
						linkedProviders[t.ProviderID] = true
					}
				}
			}
		}
		diag := BuildAccessDiagnostics(BuilderContext{
			ReasonCode:            reasonCode,
			ReasonDetail:          reasonDetail,
			ProviderID:            derefOr(doc.ContentSource, ""),
			CallerUserEmail:       user.Email,
			CallerLinkedProviders: linkedProviders,
			ServiceAccountEmail:   h.serviceAccountEmail, // injected from config
		})
		doc.AccessStatusUpdatedAt = &updatedAt
		// The generated Document type (regenerated in Phase 8) has a
		// AccessDiagnostics field. Until regen, assign via the interim helper
		// (see Task 8.2).
		setDocumentAccessDiagnostics(&doc, diag)
	}
}
```

This step also adds `GetAccessReason(ctx, id) (reasonCode, detail string, updatedAt time.Time, err error)` to the `DocumentStore` interface and its GORM impl (small query returning the three columns). Follow the same pattern as Task 1.2 / 1.3.

- [ ] **Step 2.3.6: Run the test**

```
make test-unit name=TestGetDocument_IncludesAccessDiagnostics
```

Expected: PASS.

- [ ] **Step 2.3.7: Run full unit-test suite**

```
make test-unit
```

Expected: all pass.

- [ ] **Step 2.3.8: Commit**

```bash
git add api/document_store.go api/document_store_gorm.go \
        api/document_sub_resource_handlers.go \
        api/document_sub_resource_handlers_test.go
git commit -m "feat(documents): wire access_diagnostics into GET handler response (#249)"
```

---

## Phase 3 — `DelegatedGoogleWorkspaceSource`

### Task 3.1: Create the source skeleton with CanHandle and Name

**Files:**
- Create: `api/content_source_google_workspace.go`
- Create: `api/content_source_google_workspace_test.go`

- [ ] **Step 3.1.1: Write failing tests**

Create `api/content_source_google_workspace_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDelegatedGoogleWorkspaceSource_Name(t *testing.T) {
	s := &DelegatedGoogleWorkspaceSource{}
	assert.Equal(t, "google_workspace", s.Name())
}

func TestDelegatedGoogleWorkspaceSource_CanHandle(t *testing.T) {
	s := &DelegatedGoogleWorkspaceSource{}
	cases := []struct {
		uri string
		ok  bool
	}{
		{"https://docs.google.com/document/d/abc/edit", true},
		{"https://drive.google.com/file/d/abc/view", true},
		{"https://docs.google.com/spreadsheets/d/xyz/edit", true},
		{"https://example.com/doc", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.uri, func(t *testing.T) {
			assert.Equal(t, c.ok, s.CanHandle(context.Background(), c.uri))
		})
	}
}
```

- [ ] **Step 3.1.2: Run tests; confirm failure**

```
make test-unit name=TestDelegatedGoogleWorkspaceSource
```

Expected: compile error (`DelegatedGoogleWorkspaceSource` undefined).

- [ ] **Step 3.1.3: Create `api/content_source_google_workspace.go`**

```go
package api

import (
	"context"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// ProviderGoogleWorkspace is the provider id for the delegated Google Workspace source.
const ProviderGoogleWorkspace = "google_workspace"

// DelegatedGoogleWorkspaceSource fetches Drive documents under the user's own identity
// via drive.file-scoped tokens granted through Google Picker. Documents must have been
// picker-registered (stored picker_file_id) for fetches to succeed; picker registration
// is performed by the client at attach time.
type DelegatedGoogleWorkspaceSource struct {
	Delegated *DelegatedSource
	// PickerDeveloperKey and PickerAppID are returned to clients via the picker-token
	// endpoint; not used by Fetch.
	PickerDeveloperKey string
	PickerAppID        string
}

// Name returns the source's provider id.
func (s *DelegatedGoogleWorkspaceSource) Name() string { return ProviderGoogleWorkspace }

// CanHandle returns true for Google Docs and Drive URIs. The dispatch layer
// (ContentSourceRegistry.FindSourceForDocument) decides whether to route this
// source or fall through to the service-account source based on whether the
// document is picker-registered.
func (s *DelegatedGoogleWorkspaceSource) CanHandle(_ context.Context, uri string) bool {
	lower := strings.ToLower(uri)
	host := extractHost(lower)
	return host == "docs.google.com" || host == "drive.google.com"
}

// Fetch returns the raw bytes of the referenced Drive file, exported as plain text
// (for Docs/Slides) or CSV (for Sheets), or downloaded directly (for binary files).
// Requires a user id in ctx; delegated sources cannot run without user context.
func (s *DelegatedGoogleWorkspaceSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, "", ErrAuthRequired
	}
	logger := slogging.Get()
	logger.Debug("DelegatedGoogleWorkspaceSource: Fetch user=%s uri=%s", userID, uri)

	return s.Delegated.FetchForUser(ctx, userID, uri)
}
```

Note: this skeleton compiles but `Fetch` calls through to `s.Delegated.FetchForUser` which expects a `DoFetch` callback. That callback will be set up when constructing the source (Task 3.2). This intermediate state is fine for CanHandle/Name tests.

- [ ] **Step 3.1.4: Run tests; confirm pass**

```
make test-unit name=TestDelegatedGoogleWorkspaceSource
```

Expected: PASS (Name + CanHandle tests).

- [ ] **Step 3.1.5: Commit**

```bash
git add api/content_source_google_workspace.go api/content_source_google_workspace_test.go
git commit -m "feat(sources): add DelegatedGoogleWorkspaceSource skeleton (#249)"
```

---

### Task 3.2: Implement the Drive `DoFetch` callback

**Files:**
- Modify: `api/content_source_google_workspace.go`
- Modify: `api/content_source_google_workspace_test.go`

- [ ] **Step 3.2.1: Write failing test**

Append to `api/content_source_google_workspace_test.go`:

```go
func TestDelegatedGoogleWorkspaceSource_DoFetchRoutesByMimeType(t *testing.T) {
	// Test the pure mime-type routing: given a MIME type, return the export format
	// we'd request from the Drive API. Isolated from the actual HTTP call.
	cases := []struct {
		mime           string
		expectedFormat string
	}{
		{"application/vnd.google-apps.document", "text/plain"},
		{"application/vnd.google-apps.spreadsheet", "text/csv"},
		{"application/vnd.google-apps.presentation", "text/plain"},
		{"application/pdf", ""}, // direct download, no export
	}
	for _, c := range cases {
		t.Run(c.mime, func(t *testing.T) {
			format := exportFormatFor(c.mime)
			assert.Equal(t, c.expectedFormat, format)
		})
	}
}
```

- [ ] **Step 3.2.2: Run; confirm fail**

```
make test-unit name=TestDelegatedGoogleWorkspaceSource_DoFetchRoutesByMimeType
```

Expected: compile error (`exportFormatFor` undefined).

- [ ] **Step 3.2.3: Add the mime router + DoFetch constructor**

In `api/content_source_google_workspace.go`, append:

```go
// exportFormatFor returns the MIME type to request when exporting a Google
// Workspace native document, or "" for binary files that should be downloaded
// directly. Matches the behavior of the service-account GoogleDriveSource.
func exportFormatFor(mime string) string {
	switch mime {
	case "application/vnd.google-apps.document":
		return "text/plain"
	case "application/vnd.google-apps.spreadsheet":
		return "text/csv"
	case "application/vnd.google-apps.presentation":
		return "text/plain"
	default:
		return ""
	}
}
```

Also add a `NewDelegatedGoogleWorkspaceSource` constructor that builds the `DelegatedSource` with a Drive-API-calling `DoFetch`:

```go
import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// NewDelegatedGoogleWorkspaceSource constructs a source wired to the given token
// repository and OAuth provider registry. The DoFetch callback creates a Drive
// service with an OAuth access token on each call (lightweight — no connection
// caching needed at this scale).
func NewDelegatedGoogleWorkspaceSource(
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
	pickerDeveloperKey, pickerAppID string,
) *DelegatedGoogleWorkspaceSource {
	doFetch := func(ctx context.Context, accessToken, uri string) ([]byte, string, error) {
		fileID, ok := extractGoogleDriveFileID(uri)
		if !ok {
			return nil, "", fmt.Errorf("could not extract file ID from URL: %s", uri)
		}
		svc, err := drive.NewService(ctx, option.WithTokenSource(staticTokenSource(accessToken)))
		if err != nil {
			return nil, "", fmt.Errorf("failed to create Drive service: %w", err)
		}
		file, err := svc.Files.Get(fileID).Fields("id,name,mimeType").Context(ctx).Do()
		if err != nil {
			return nil, "", fmt.Errorf("failed to get file metadata: %w", err)
		}
		exportMime := exportFormatFor(file.MimeType)
		if exportMime != "" {
			resp, err := svc.Files.Export(fileID, exportMime).Context(ctx).Download()
			if err != nil {
				return nil, "", fmt.Errorf("failed to export file: %w", err)
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(io.LimitReader(resp.Body, googleDriveMaxExportSize))
			if err != nil {
				return nil, "", fmt.Errorf("failed to read export: %w", err)
			}
			return data, exportMime, nil
		}
		resp, err := svc.Files.Get(fileID).Context(ctx).Download()
		if err != nil {
			return nil, "", fmt.Errorf("failed to download file: %w", err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, googleDriveMaxExportSize))
		if err != nil {
			return nil, "", fmt.Errorf("failed to read download: %w", err)
		}
		return data, file.MimeType, nil
	}
	return &DelegatedGoogleWorkspaceSource{
		Delegated: &DelegatedSource{
			ProviderID: ProviderGoogleWorkspace,
			Tokens:     tokens,
			Registry:   registry,
			DoFetch:    doFetch,
		},
		PickerDeveloperKey: pickerDeveloperKey,
		PickerAppID:        pickerAppID,
	}
}
```

Also add helper:

```go
import "golang.org/x/oauth2"

func staticTokenSource(token string) oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
}
```

- [ ] **Step 3.2.4: Add `ValidateAccess` and `RequestAccess`**

Still in `api/content_source_google_workspace.go`, append:

```go
// ValidateAccess checks whether the user's token can see the given URI without
// downloading the content. Implementation: we use a per-call clone of
// DelegatedSource whose DoFetch performs a metadata-only Drive call, avoiding
// mutation of the shared s.Delegated field (which would race under concurrent
// ValidateAccess calls).
//
// 4xx Drive errors are translated to (false, nil) — "not accessible" is not
// an application error. ErrAuthRequired and ErrTransient propagate from the
// helper as-is.
func (s *DelegatedGoogleWorkspaceSource) ValidateAccess(ctx context.Context, uri string) (bool, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return false, ErrAuthRequired
	}
	var reachable bool
	probe := &DelegatedSource{
		ProviderID: s.Delegated.ProviderID,
		Tokens:     s.Delegated.Tokens,
		Registry:   s.Delegated.Registry,
		Skew:       s.Delegated.Skew,
		DoFetch: func(ctx context.Context, accessToken, uri string) ([]byte, string, error) {
			fileID, extracted := extractGoogleDriveFileID(uri)
			if !extracted {
				return nil, "", fmt.Errorf("could not extract file ID from URL: %s", uri)
			}
			svc, err := drive.NewService(ctx, option.WithTokenSource(staticTokenSource(accessToken)))
			if err != nil {
				return nil, "", err
			}
			if _, err := svc.Files.Get(fileID).Fields("id").Context(ctx).Do(); err != nil {
				return nil, "", err
			}
			reachable = true
			return nil, "", nil
		},
	}
	if _, _, err := probe.FetchForUser(ctx, userID, uri); err != nil {
		if errors.Is(err, ErrAuthRequired) || errors.Is(err, ErrTransient) {
			return false, err
		}
		return false, nil
	}
	return reachable, nil
}

// RequestAccess logs an actionable remediation hint. The actual user-facing
// remediation is surfaced via access_diagnostics (Phase 2). This method exists
// to satisfy the AccessRequester interface and is called by the pipeline on
// pending-access transitions for diagnostic logging only.
func (s *DelegatedGoogleWorkspaceSource) RequestAccess(_ context.Context, uri string) error {
	slogging.Get().Info("DelegatedGoogleWorkspaceSource: access not available for %s; user may need to re-link or repick", uri)
	return nil
}
```

Note: the `ValidateAccess` implementation is a defensive detour because the `DelegatedSource.FetchForUser` method bakes in token refresh and access-token plumbing — we temporarily swap `DoFetch` to reuse it for validation. Keep an eye out for whether sub-project 1 exposes a cleaner "just refresh and return the access token" hook; if it does, refactor in Task 3.3. If not, this implementation is correct.

- [ ] **Step 3.2.5: Run the mime-routing test**

```
make test-unit name=TestDelegatedGoogleWorkspaceSource_DoFetchRoutesByMimeType
```

Expected: PASS.

- [ ] **Step 3.2.6: Build check**

```
make build-server
```

Expected: build passes.

- [ ] **Step 3.2.7: Commit**

```bash
git add api/content_source_google_workspace.go api/content_source_google_workspace_test.go
git commit -m "feat(sources): implement DelegatedGoogleWorkspaceSource DoFetch + ValidateAccess (#249)"
```

---

## Phase 4 — Document-aware dispatch (`FindSourceForDocument`)

### Task 4.1: Add the dispatch method to ContentSourceRegistry

**Files:**
- Create: `api/content_source_registry_for_document.go`
- Create: `api/content_source_registry_for_document_test.go`

- [ ] **Step 4.1.1: Write failing tests**

Create `api/content_source_registry_for_document_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindSourceForDocument_PickerRegisteredWithLinkedToken(t *testing.T) {
	reg := NewContentSourceRegistry()
	delegated := &stubSource{name: "google_workspace"}
	serviceAccount := &stubSource{name: "google_drive"}
	reg.Register(delegated)
	reg.Register(serviceAccount)

	providerID := "google_workspace"
	doc := &Document{
		Uri: "https://docs.google.com/document/d/abc/edit",
		// Picker fields are on the GORM model; assume the API Document is
		// populated from the model's equivalent fields by the store.
	}
	tokens := &stubTokenChecker{
		linked: map[string]bool{"google_workspace": true},
	}
	pickerMeta := &PickerMetadata{ProviderID: &providerID}

	src, ok := reg.FindSourceForDocument(context.Background(), doc, pickerMeta, "alice", tokens)
	assert.True(t, ok)
	assert.Equal(t, "google_workspace", src.Name())
}

func TestFindSourceForDocument_PickerRegisteredWithoutLinkedToken_FallsThrough(t *testing.T) {
	reg := NewContentSourceRegistry()
	delegated := &stubSource{name: "google_workspace"}
	serviceAccount := &stubSource{name: "google_drive", canHandle: true}
	reg.Register(delegated)
	reg.Register(serviceAccount)

	providerID := "google_workspace"
	doc := &Document{Uri: "https://docs.google.com/document/d/abc/edit"}
	tokens := &stubTokenChecker{linked: map[string]bool{}}
	pickerMeta := &PickerMetadata{ProviderID: &providerID}

	src, ok := reg.FindSourceForDocument(context.Background(), doc, pickerMeta, "alice", tokens)
	assert.True(t, ok)
	assert.Equal(t, "google_drive", src.Name())
}

func TestFindSourceForDocument_NonPicker_UsesURLDispatch(t *testing.T) {
	reg := NewContentSourceRegistry()
	serviceAccount := &stubSource{name: "google_drive", canHandle: true}
	reg.Register(serviceAccount)

	doc := &Document{Uri: "https://docs.google.com/document/d/abc/edit"}
	src, ok := reg.FindSourceForDocument(context.Background(), doc, nil, "alice", nil)
	assert.True(t, ok)
	assert.Equal(t, "google_drive", src.Name())
}

// stubSource is a minimal ContentSource for dispatch tests.
type stubSource struct {
	name      string
	canHandle bool
}

func (s *stubSource) Name() string                                              { return s.name }
func (s *stubSource) CanHandle(_ context.Context, _ string) bool                { return s.canHandle }
func (s *stubSource) Fetch(_ context.Context, _ string) ([]byte, string, error) { return nil, "", nil }

// stubTokenChecker is a minimal LinkedProviderChecker for tests.
type stubTokenChecker struct {
	linked map[string]bool
}

func (s *stubTokenChecker) HasActiveToken(_ context.Context, _ string, providerID string) bool {
	return s.linked[providerID]
}
```

- [ ] **Step 4.1.2: Run; confirm fail**

```
make test-unit name=TestFindSourceForDocument
```

Expected: compile errors.

- [ ] **Step 4.1.3: Implement `FindSourceForDocument`**

Create `api/content_source_registry_for_document.go`:

```go
package api

import "context"

// PickerMetadata carries the picker-registration fields from a document row.
// All fields non-nil together or all nil together (enforced at attach time).
type PickerMetadata struct {
	ProviderID *string
	FileID     *string
	MimeType   *string
}

// LinkedProviderChecker reports whether a user has an active linked token for
// a given provider. Implementations typically consult ContentTokenRepository.
type LinkedProviderChecker interface {
	HasActiveToken(ctx context.Context, userID, providerID string) bool
}

// FindSourceForDocument picks a ContentSource for fetching a specific document.
// The delegated source wins when the document has picker metadata, the provider is
// registered, the user id matches the document owner, and the user has an active
// linked token. Otherwise, dispatch falls through to URL-based lookup (which picks
// the first CanHandle match, matching existing behavior).
func (r *ContentSourceRegistry) FindSourceForDocument(
	ctx context.Context,
	doc *Document,
	picker *PickerMetadata,
	userID string,
	checker LinkedProviderChecker,
) (ContentSource, bool) {
	if picker != nil && picker.ProviderID != nil && checker != nil {
		if src, found := r.FindSourceByName(*picker.ProviderID); found {
			if checker.HasActiveToken(ctx, userID, *picker.ProviderID) {
				return src, true
			}
		}
	}
	return r.FindSource(ctx, doc.Uri)
}
```

- [ ] **Step 4.1.4: Run the tests**

```
make test-unit name=TestFindSourceForDocument
```

Expected: PASS.

- [ ] **Step 4.1.5: Commit**

```bash
git add api/content_source_registry_for_document.go api/content_source_registry_for_document_test.go
git commit -m "feat(sources): add FindSourceForDocument dispatch (#249)"
```

---

### Task 4.2: Update the access poller to use the new dispatch

**Files:**
- Modify: `api/access_poller.go:63-113`
- Modify: `api/access_poller_test.go`

- [ ] **Step 4.2.1: Write failing test**

Append to `api/access_poller_test.go`:

```go
func TestAccessPoller_PickerRegisteredDocument_UsesDelegated(t *testing.T) {
	ctx := context.Background()
	delegatedProvider := "google_workspace"
	doc := Document{
		Id:      uuidPtr(uuid.New()),
		Uri:     "https://docs.google.com/document/d/abc/edit",
		OwnerID: stringPtr("alice"),
		// In tests, picker metadata is injected via the store mock. The poller
		// calls store.ListByAccessStatus which we stub to return picker metadata.
	}
	_ = doc
	_ = delegatedProvider
	t.Skip("IMPLEMENT: mock store returns doc with picker metadata; assert delegated source called")
}
```

- [ ] **Step 4.2.2: Replace the skip with concrete assertions**

Replace the skipped body with a complete test that:

1. Mocks `documentStore.ListByAccessStatus` to return one document with picker metadata populated (this may require extending the mock store or its test helpers to carry picker fields — if so, add them in this step).
2. Arranges a registry containing two mock sources: `google_workspace` (delegated) and `google_drive` (service-account).
3. Arranges a `LinkedProviderChecker` mock that reports `google_workspace` is active for `alice`.
4. Calls `pollOnce` and asserts the delegated source's `ValidateAccess` was called, not the service-account's.

Pattern: match existing `TestAccessPoller_*` tests in this file for store-mock setup.

- [ ] **Step 4.2.3: Update poller to pass picker metadata and checker**

Modify `api/access_poller.go:63-113` to:

1. Read picker metadata alongside the document (new method on the store — or extend `ListByAccessStatus` to return a richer type).
2. Build a `LinkedProviderChecker` (new small type wrapping `ContentTokenRepository`).
3. Call `FindSourceForDocument(ctx, &doc, pickerMeta, doc.OwnerID, checker)` with `userID` set from `doc.OwnerID`.
4. Continue with existing ValidateAccess + status-update flow.

Concrete diff:

```go
func (p *AccessPoller) pollOnce() {
	logger := slogging.Get()
	ctx := context.Background()

	if p.documentStore == nil {
		return
	}

	docs, err := p.documentStore.ListByAccessStatus(ctx, AccessStatusPendingAccess, 100)
	if err != nil {
		logger.Warn("AccessPoller: failed to list pending documents: %v", err)
		return
	}
	if len(docs) == 0 {
		return
	}
	logger.Debug("AccessPoller: checking %d pending documents", len(docs))

	for _, doc := range docs {
		if doc.CreatedAt != nil && time.Since(*doc.CreatedAt) > p.maxAge {
			continue
		}
		// Per-doc user context for delegated fallthrough.
		ownerID := ""
		if doc.OwnerID != nil {
			ownerID = *doc.OwnerID
		}
		docCtx := WithUserID(ctx, ownerID)

		pickerMeta := p.documentStore.GetPickerMetadata(ctx, doc.Id.String())
		src, ok := p.sources.FindSourceForDocument(docCtx, &doc, pickerMeta, ownerID, p.tokenChecker)
		if !ok {
			continue
		}
		validator, ok := src.(AccessValidator)
		if !ok {
			continue
		}
		accessible, valErr := validator.ValidateAccess(docCtx, doc.Uri)
		if valErr != nil {
			logger.Debug("AccessPoller: validation error for %s: %v", doc.Uri, valErr)
			continue
		}
		if accessible {
			if updateErr := p.documentStore.UpdateAccessStatusWithDiagnostics(
				ctx, doc.Id.String(), AccessStatusAccessible, "", "", "",
			); updateErr != nil {
				logger.Warn("AccessPoller: failed to update %s: %v", doc.Id, updateErr)
			}
		}
	}
}
```

Add to `AccessPoller` struct:

```go
type AccessPoller struct {
	// existing fields...
	tokenChecker LinkedProviderChecker
}
```

Extend `NewAccessPoller(...)` signature to accept a `LinkedProviderChecker`.

Add `GetPickerMetadata(ctx, id) *PickerMetadata` to `DocumentStore` interface (and the GORM impl; small query).

- [ ] **Step 4.2.4: Run poller tests**

```
make test-unit name=TestAccessPoller
```

Expected: all pass.

- [ ] **Step 4.2.5: Commit**

```bash
git add api/access_poller.go api/access_poller_test.go api/document_store.go api/document_store_gorm.go
git commit -m "feat(access-poller): use document-aware source dispatch with linked-provider checker (#249)"
```

---

## Phase 5 — Picker-token endpoint

### Task 5.1: Add config and constructor wiring

**Files:**
- Create: `internal/config/google_workspace_config.go`
- Modify: `internal/config/content_sources.go`

- [ ] **Step 5.1.1: Add the config struct**

Create `internal/config/google_workspace_config.go`:

```go
package config

// GoogleWorkspaceConfig holds settings for the delegated Google Workspace source.
// OAuth-provider settings live under content_oauth.providers.google_workspace.
type GoogleWorkspaceConfig struct {
	Enabled            bool   `yaml:"enabled" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_ENABLED"`
	PickerDeveloperKey string `yaml:"picker_developer_key" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_DEVELOPER_KEY"`
	PickerAppID        string `yaml:"picker_app_id" env:"TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_APP_ID"`
}

// IsConfigured returns true when the source is enabled and the picker inputs are non-empty.
func (c GoogleWorkspaceConfig) IsConfigured() bool {
	return c.Enabled && c.PickerDeveloperKey != "" && c.PickerAppID != ""
}
```

- [ ] **Step 5.1.2: Extend `ContentSourcesConfig`**

In `internal/config/content_sources.go`, add the field:

```go
type ContentSourcesConfig struct {
	GoogleDrive     GoogleDriveConfig     `yaml:"google_drive"`
	GoogleWorkspace GoogleWorkspaceConfig `yaml:"google_workspace"`
}
```

- [ ] **Step 5.1.3: Build + lint**

```
make build-server
make lint
```

Expected: clean.

- [ ] **Step 5.1.4: Commit**

```bash
git add internal/config/google_workspace_config.go internal/config/content_sources.go
git commit -m "feat(config): add GoogleWorkspaceConfig for delegated source (#249)"
```

---

### Task 5.2: Implement the picker-token handler

**Files:**
- Create: `api/picker_token_handler.go`
- Create: `api/picker_token_handler_test.go`

- [ ] **Step 5.2.1: Write failing tests for handler behavior**

Create `api/picker_token_handler_test.go` with test cases for each response code (200, 401, 404, 422, 503). Follow the pattern in `api/content_oauth_handlers_test.go` for setup. Each test:
- Arranges a mock `ContentTokenRepository` that returns a specific token state.
- Arranges a mock `ContentOAuthProviderRegistry` with or without the provider.
- Calls the handler through the Gin test router.
- Asserts the response code and body.

Example for the happy path:

```go
func TestMintPickerToken_HappyPath(t *testing.T) {
	user := stubUser("alice")
	tokens := &mockContentTokenRepo{}
	tokens.On("GetByUserAndProvider", mock.Anything, user.InternalUUID, "google_workspace").
		Return(&ContentToken{
			AccessToken: "ya29.nonexpired",
			ExpiresAt:   asPtr(time.Now().Add(30 * time.Minute)),
			Status:      ContentTokenStatusActive,
		}, nil)

	registry := NewContentOAuthProviderRegistry()
	registry.Register("google_workspace", &stubProvider{})

	handler := NewPickerTokenHandler(tokens, registry, PickerTokenConfig{
		DeveloperKey: "AIzaDEV",
		AppID:        "APPID",
	})

	req := httptest.NewRequest("POST", "/me/picker_tokens/google_workspace", nil)
	req = req.WithContext(WithUserID(req.Context(), user.InternalUUID))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "provider_id", Value: "google_workspace"}}
	// Attach user claims matching other handler tests' pattern.

	handler.Handle(c)

	require.Equal(t, 200, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ya29.nonexpired", body["access_token"])
	assert.Equal(t, "AIzaDEV", body["developer_key"])
	assert.Equal(t, "APPID", body["app_id"])
}
```

Repeat with variations for each error code. Group the tests; there are six in total.

- [ ] **Step 5.2.2: Run; confirm fail**

```
make test-unit name=TestMintPickerToken
```

Expected: compile errors.

- [ ] **Step 5.2.3: Implement the handler**

Create `api/picker_token_handler.go`:

```go
package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// PickerTokenConfig carries the per-provider configuration needed by the handler.
type PickerTokenConfig struct {
	DeveloperKey string
	AppID        string
}

// PickerTokenHandler serves POST /me/picker_tokens/{provider_id}.
type PickerTokenHandler struct {
	tokens   ContentTokenRepository
	registry *ContentOAuthProviderRegistry
	configs  map[string]PickerTokenConfig // provider_id -> config
}

// NewPickerTokenHandler constructs the handler. `configs` maps each supported
// provider to its picker-specific configuration; an entry must exist for every
// provider that should accept picker-token requests.
func NewPickerTokenHandler(
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
	configs map[string]PickerTokenConfig,
) *PickerTokenHandler {
	return &PickerTokenHandler{tokens: tokens, registry: registry, configs: configs}
}

// Handle serves the request.
func (h *PickerTokenHandler) Handle(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	providerID := c.Param("provider_id")
	cfg, ok := h.configs[providerID]
	if !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":    "provider_not_configured",
			"message": "picker is not configured for this provider",
		})
		return
	}
	if _, ok := h.registry.Get(providerID); !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":    "provider_not_registered",
			"message": "provider is not registered or enabled",
		})
		return
	}
	if cfg.DeveloperKey == "" || cfg.AppID == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":    "picker_not_configured",
			"message": "picker developer key or app id missing",
		})
		return
	}
	userID, ok := UserIDFromContext(c.Request.Context())
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "unauthenticated"})
		return
	}
	token, err := h.tokens.GetByUserAndProvider(c.Request.Context(), userID, providerID)
	if err != nil {
		if errors.Is(err, ErrContentTokenNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code":     "not_linked",
				"message":  "no linked token; call /me/content_tokens/{provider_id}/authorize",
				"provider": providerID,
			})
			return
		}
		logger.Error("picker_token_handler: repo error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error"})
		return
	}
	if token.Status == ContentTokenStatusFailedRefresh {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "token_refresh_failed"})
		return
	}

	// Refresh if expired (or within 30s skew) — reuse the DelegatedSource refresh
	// semantics by calling a small helper that acquires the row lock.
	plain, expiresAt, err := h.refreshIfNeeded(c.Request.Context(), token, providerID)
	if err != nil {
		switch {
		case errors.Is(err, ErrAuthRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"code": "token_refresh_failed"})
		case errors.Is(err, ErrTransient):
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": "transient_failure"})
		default:
			logger.Error("picker_token_handler: refresh error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error"})
		}
		return
	}

	logger.Info("picker_token_minted user=%s provider=%s expires_at=%s",
		userID, providerID, expiresAt.Format(time.RFC3339))

	c.JSON(http.StatusOK, gin.H{
		"access_token":   plain,
		"expires_at":     expiresAt.Format(time.RFC3339),
		"developer_key":  cfg.DeveloperKey,
		"app_id":         cfg.AppID,
	})
}

// refreshIfNeeded returns the plaintext access token + its expiry, refreshing
// via RefreshWithLock if needed. On permanent refresh failure returns ErrAuthRequired;
// on transient failure returns ErrTransient.
func (h *PickerTokenHandler) refreshIfNeeded(
	ctx context.Context, token *ContentToken, providerID string,
) (string, time.Time, error) {
	skew := 30 * time.Second
	if token.ExpiresAt != nil && time.Now().Add(skew).Before(*token.ExpiresAt) {
		// Still fresh.
		return token.AccessToken, *token.ExpiresAt, nil
	}
	// Refresh via the provider registry.
	provider, ok := h.registry.Get(providerID)
	if !ok {
		return "", time.Time{}, ErrAuthRequired
	}
	var refreshed *ContentToken
	var permFail, transFail bool
	_, err := h.tokens.RefreshWithLock(ctx, token.ID, func(current *ContentToken) (*ContentToken, error) {
		if current.ExpiresAt != nil && time.Now().Add(skew).Before(*current.ExpiresAt) {
			refreshed = current
			return current, nil
		}
		if current.RefreshToken == "" {
			current.Status = ContentTokenStatusFailedRefresh
			current.LastError = "no refresh token available"
			permFail = true
			return current, nil
		}
		resp, err := provider.Refresh(ctx, current.RefreshToken)
		if err != nil {
			if IsContentOAuthPermanentFailure(err) {
				current.Status = ContentTokenStatusFailedRefresh
				current.LastError = err.Error()
				permFail = true
				return current, nil
			}
			transFail = true
			return nil, err
		}
		now := time.Now()
		current.AccessToken = resp.AccessToken
		if resp.RefreshToken != "" {
			current.RefreshToken = resp.RefreshToken
		}
		current.ExpiresAt = resp.ExpiresAt()
		current.LastRefreshAt = &now
		current.LastError = ""
		current.Status = ContentTokenStatusActive
		refreshed = current
		return current, nil
	})
	if permFail {
		return "", time.Time{}, ErrAuthRequired
	}
	if transFail {
		return "", time.Time{}, ErrTransient
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if refreshed == nil {
		return "", time.Time{}, ErrTransient
	}
	expiry := time.Time{}
	if refreshed.ExpiresAt != nil {
		expiry = *refreshed.ExpiresAt
	}
	return refreshed.AccessToken, expiry, nil
}
```

- [ ] **Step 5.2.4: Run the handler tests**

```
make test-unit name=TestMintPickerToken
```

Expected: PASS for all six cases.

- [ ] **Step 5.2.5: Commit**

```bash
git add api/picker_token_handler.go api/picker_token_handler_test.go
git commit -m "feat(picker-token): implement POST /me/picker_tokens/{provider_id} handler (#249)"
```

---

## Phase 6 — Document-attach `picker_registration` extension

### Task 6.1: Add picker-registration validation path

**Files:**
- Modify: `api/document_sub_resource_handlers.go:180-275`
- Modify: `api/document_sub_resource_handlers_test.go`

- [ ] **Step 6.1.1: Add failing tests**

Append test cases to the existing document-attach test file that cover:

1. Picker registration accepted → picker columns stored on the row.
2. `file_id` doesn't match URL-extracted id → 400.
3. Empty `file_id` → 400.
4. Unknown `provider_id` → 422.
5. Caller has no linked token → 401.
6. Caller has `failed_refresh` token → 401.

Test shape:

```go
func TestCreateDocument_WithPickerRegistration_HappyPath(t *testing.T) {
	// ... arrange handler with mock store + content-token repo returning active token.
	// ... POST document with picker_registration payload.
	// ... assert 201 and that store.Create was called with model fields populated.
}
```

Follow the pattern used by existing `TestCreateDocument*` tests in the file.

- [ ] **Step 6.1.2: Run; confirm fail**

```
make test-unit name=TestCreateDocument_WithPickerRegistration
```

Expected: fail (handler doesn't inspect picker_registration yet).

- [ ] **Step 6.1.3: Extend the handler**

In the document-create handler (around line 180-270 of `api/document_sub_resource_handlers.go`), add picker-registration extraction and validation. Because the `Document` wire type doesn't yet have a `picker_registration` field (it will after Phase 8 OpenAPI regen), we need an interim mechanism to accept it:

Option: Parse the raw request body into a side struct before passing it to `ValidateAndParseRequest[Document]`. Example:

```go
// Before ValidateAndParseRequest: sniff the body for picker_registration.
var sniff struct {
	PickerRegistration *struct {
		ProviderID string `json:"provider_id"`
		FileID     string `json:"file_id"`
		MimeType   string `json:"mime_type"`
	} `json:"picker_registration"`
}
rawBody, _ := io.ReadAll(c.Request.Body)
_ = json.Unmarshal(rawBody, &sniff)
c.Request.Body = io.NopCloser(bytes.NewReader(rawBody)) // reset for downstream parsing

config := ValidationConfigs["document_create"]
document, err := ValidateAndParseRequest[Document](c, config)
// ... existing parsing ...

if sniff.PickerRegistration != nil {
	pr := sniff.PickerRegistration
	if pr.ProviderID == "" || pr.FileID == "" || pr.MimeType == "" {
		HandleRequestError(c, &RequestError{Status: 400, Code: "invalid_picker_registration"})
		return
	}
	fileID, ok := extractGoogleDriveFileID(document.Uri)
	if !ok || fileID != pr.FileID {
		HandleRequestError(c, &RequestError{Status: 400, Code: "picker_file_id_mismatch"})
		return
	}
	if _, ok := h.contentOAuthRegistry.Get(pr.ProviderID); !ok {
		HandleRequestError(c, &RequestError{Status: 422, Code: "provider_not_registered"})
		return
	}
	token, err := h.contentTokens.GetByUserAndProvider(c.Request.Context(), user.InternalUUID, pr.ProviderID)
	if err != nil || token.Status != ContentTokenStatusActive {
		HandleRequestError(c, &RequestError{Status: 401, Code: "token_not_linked_or_failed"})
		return
	}
	// Propagate picker metadata into the store via a new store method.
	pickerProv := pr.ProviderID
	pickerFile := pr.FileID
	pickerMime := pr.MimeType
	document.PickerRegistration = &PickerRegistrationAPI{
		ProviderID: &pickerProv,
		FileID:     &pickerFile,
		MimeType:   &pickerMime,
	}
}
```

The `PickerRegistrationAPI` type is the interim Go-side carrier until the OpenAPI regen (Phase 8) provides the official generated type. Define it at the top of the handler file:

```go
type PickerRegistrationAPI struct {
	ProviderID *string
	FileID     *string
	MimeType   *string
}
```

Then in the document-creation path (around line 235), after `documentStore.Create`, if picker metadata is present, call a new store method `SetPickerMetadata(ctx, docID, provider, fileID, mime)` that writes the three columns. Add that method to the store interface and GORM impl.

- [ ] **Step 6.1.4: Run the tests**

```
make test-unit name=TestCreateDocument_WithPickerRegistration
```

Expected: PASS.

- [ ] **Step 6.1.5: Commit**

```bash
git add api/document_sub_resource_handlers.go api/document_sub_resource_handlers_test.go \
        api/document_store.go api/document_store_gorm.go
git commit -m "feat(documents): accept picker_registration on document create (#249)"
```

---

## Phase 7 — Un-link cascade

### Task 7.1: Extend the un-link handler

**Files:**
- Modify: `api/content_oauth_handlers.go` — the `DELETE /me/content_tokens/{provider_id}` handler
- Modify: `api/content_oauth_handlers_test.go`
- Modify: `api/document_store.go`, `api/document_store_gorm.go` — add `ClearPickerMetadataForOwner(ctx, ownerID, providerID)` method

- [ ] **Step 7.1.1: Add store method**

Add to interface:

```go
// ClearPickerMetadataForOwner nulls picker metadata + resets access_status to 'unknown'
// for every document owned by ownerID whose picker_provider_id == providerID.
ClearPickerMetadataForOwner(ctx context.Context, ownerID, providerID string) error
```

GORM impl:

```go
func (s *GormDocumentStore) ClearPickerMetadataForOwner(
	ctx context.Context, ownerID, providerID string,
) error {
	return s.db.WithContext(ctx).
		Model(&models.Document{}).
		Where("owner_id = ? AND picker_provider_id = ?", ownerID, providerID).
		Updates(map[string]interface{}{
			"picker_provider_id":       nil,
			"picker_file_id":           nil,
			"picker_mime_type":         nil,
			"access_status":            "unknown",
			"access_reason_code":       nil,
			"access_reason_detail":     nil,
			"access_status_updated_at": time.Now(),
		}).Error
}
```

Note: the `Document` GORM model may not currently have an `OwnerID` column — documents are scoped via `ThreatModelID` and ownership via the threat model's owner. Verify this first:

```
rg -n 'OwnerID|owner_id' api/models/models.go | head -5
```

If documents don't carry `owner_id` directly, the cascade needs to join via threat model ownership. Adjust the query accordingly — for example:

```go
return s.db.WithContext(ctx).Exec(`
	UPDATE documents d
	SET picker_provider_id = NULL,
	    picker_file_id = NULL,
	    picker_mime_type = NULL,
	    access_status = 'unknown',
	    access_reason_code = NULL,
	    access_reason_detail = NULL,
	    access_status_updated_at = NOW()
	WHERE d.picker_provider_id = ?
	  AND d.threat_model_id IN (
	    SELECT id FROM threat_models WHERE owner_id = ?
	  )
`, providerID, ownerID).Error
```

Use whichever form matches the existing ownership model.

- [ ] **Step 7.1.2: Write failing test**

Append to `api/content_oauth_handlers_test.go`:

```go
func TestDeleteContentToken_ClearsPickerMetadataOnOwnedDocuments(t *testing.T) {
	// Arrange: user alice has a linked google_workspace token, plus a document in
	// a threat model she owns with picker metadata set.
	// Act: DELETE /me/content_tokens/google_workspace.
	// Assert: document's picker_* columns are NULL and access_status == 'unknown'.
	t.Skip("IMPLEMENT: use test DB + direct GORM query to verify columns")
}
```

Replace the skip with a concrete test following the pattern of other integration-style tests in this file.

- [ ] **Step 7.1.3: Add the call-site in the delete handler**

Find the delete-content-token handler in `api/content_oauth_handlers.go`. Before its existing token-delete + provider-revoke logic, add:

```go
if err := h.documentStore.ClearPickerMetadataForOwner(c.Request.Context(), userID, providerID); err != nil {
	logger.Warn("failed to clear picker metadata for owner=%s provider=%s: %v", userID, providerID, err)
	// Continue — don't block the un-link on this.
}
```

- [ ] **Step 7.1.4: Run the tests**

```
make test-unit name=TestDeleteContentToken
```

Expected: PASS.

- [ ] **Step 7.1.5: Commit**

```bash
git add api/content_oauth_handlers.go api/content_oauth_handlers_test.go \
        api/document_store.go api/document_store_gorm.go
git commit -m "feat(content-tokens): clear picker metadata on un-link (#249)"
```

---

## Phase 8 — OpenAPI schema updates and regeneration

### Task 8.1: Edit `api-schema/tmi-openapi.json`

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 8.1.1: Add new schemas**

Under `components.schemas`, add:

```json
"PickerRegistration": {
  "type": "object",
  "required": ["provider_id", "file_id", "mime_type"],
  "properties": {
    "provider_id": {
      "type": "string",
      "enum": ["google_workspace"],
      "description": "Content OAuth provider that issued the picker grant"
    },
    "file_id": {
      "type": "string",
      "minLength": 1,
      "maxLength": 255,
      "description": "Provider-native file identifier from the picker (e.g. Google Drive file ID)"
    },
    "mime_type": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "description": "MIME type returned by the picker"
    }
  }
},
"PickerTokenResponse": {
  "type": "object",
  "required": ["access_token", "expires_at", "developer_key", "app_id"],
  "properties": {
    "access_token": {"type": "string"},
    "expires_at": {"type": "string", "format": "date-time"},
    "developer_key": {"type": "string"},
    "app_id": {"type": "string"}
  }
},
"DocumentAccessDiagnostics": {
  "type": "object",
  "required": ["reason_code", "remediations"],
  "properties": {
    "reason_code": {
      "type": "string",
      "enum": [
        "token_not_linked", "token_refresh_failed", "token_transient_failure",
        "picker_registration_invalid", "no_accessible_source",
        "source_not_found", "fetch_error", "other"
      ]
    },
    "reason_detail": {
      "type": "string",
      "nullable": true,
      "maxLength": 512,
      "description": "Raw error text; populated only when reason_code is 'other'"
    },
    "remediations": {
      "type": "array",
      "items": {"$ref": "#/components/schemas/AccessRemediation"}
    }
  }
},
"AccessRemediation": {
  "type": "object",
  "required": ["action", "params"],
  "properties": {
    "action": {
      "type": "string",
      "enum": [
        "link_account", "relink_account", "repick_file",
        "share_with_service_account", "repick_after_share",
        "retry", "contact_owner"
      ]
    },
    "params": {
      "type": "object",
      "additionalProperties": true,
      "description": "Action-specific parameters (e.g. service_account_email, provider_id, user_email)"
    }
  }
}
```

- [ ] **Step 8.1.2: Add the picker-token operation**

Add a new path entry `/me/picker_tokens/{provider_id}` with a POST operation:

```json
"/me/picker_tokens/{provider_id}": {
  "parameters": [{
    "name": "provider_id", "in": "path", "required": true,
    "schema": {"type": "string"},
    "description": "Content OAuth provider id (currently only 'google_workspace')"
  }],
  "post": {
    "operationId": "mintPickerToken",
    "summary": "Mint a short-lived access token for the Google Picker browser client",
    "tags": ["User"],
    "security": [{"bearerAuth": []}],
    "x-cacheable-endpoint": false,
    "responses": {
      "200": {
        "description": "Token minted",
        "content": {
          "application/json": {
            "schema": {"$ref": "#/components/schemas/PickerTokenResponse"}
          }
        }
      },
      "401": {"$ref": "#/components/responses/Unauthorized"},
      "404": {"$ref": "#/components/responses/NotFound"},
      "422": {"$ref": "#/components/responses/UnprocessableEntity"},
      "429": {"$ref": "#/components/responses/TooManyRequests"},
      "503": {"$ref": "#/components/responses/ServiceUnavailable"}
    }
  }
}
```

- [ ] **Step 8.1.3: Extend the Document schema**

Find the `Document` schema. Add properties:

```json
"access_diagnostics": {
  "$ref": "#/components/schemas/DocumentAccessDiagnostics",
  "nullable": true
},
"access_status_updated_at": {
  "type": "string",
  "format": "date-time",
  "nullable": true
},
"picker_registration": {
  "$ref": "#/components/schemas/PickerRegistration",
  "nullable": true,
  "description": "Optional; when present, client has performed a Picker-based attachment"
}
```

- [ ] **Step 8.1.4: Validate the schema**

```
make validate-openapi
```

Expected: the report at `api-schema/openapi-validation-report.json` has no new errors. Address any validation errors that arise.

- [ ] **Step 8.1.5: Commit the schema changes**

```bash
git add api-schema/tmi-openapi.json api-schema/openapi-validation-report.json
git commit -m "feat(openapi): add picker_registration, picker-token, and access_diagnostics schemas (#249)"
```

---

### Task 8.2: Regenerate `api/api.go` and wire types through

**Files:**
- Modify: `api/api.go` (regenerated; do not edit by hand)
- Modify: multiple consumers of regenerated types

- [ ] **Step 8.2.1: Regenerate**

```
make generate-api
```

Expected: `api/api.go` updated with new types. Build may fail if handlers use placeholder helper names (e.g. `setDocumentAccessDiagnostics` in Task 2.3, `PickerRegistrationAPI` in Task 6.1).

- [ ] **Step 8.2.2: Replace placeholders with generated types**

Grep for the placeholders and replace each with the generated symbol:

```
rg -n 'setDocumentAccessDiagnostics\|PickerRegistrationAPI\|AccessDiagnosticsDiag' api/
```

For each hit, swap to the regenerated type (e.g., `DocumentAccessDiagnostics`, `PickerRegistration`).

- [ ] **Step 8.2.3: Build, lint, test**

```
make build-server
make lint
make test-unit
```

Expected: clean.

- [ ] **Step 8.2.4: Commit**

```bash
git add api/api.go api/document_sub_resource_handlers.go api/access_diagnostics.go
git commit -m "chore(api): regenerate api.go for picker and diagnostics types (#249)"
```

---

## Phase 9 — Server wiring + startup validation

### Task 9.1: Register source and route

**Files:**
- Modify: `cmd/server/main.go` — around line 1162 (contentSources registry setup) and wherever routes are registered

- [ ] **Step 9.1.1: Register `DelegatedGoogleWorkspaceSource` before `GoogleDriveSource`**

Around `cmd/server/main.go:1162`, before the existing `if cfg.ContentSources.GoogleDrive.IsConfigured() {...}` block, add:

```go
if cfg.ContentSources.GoogleWorkspace.IsConfigured() {
	gwSource := api.NewDelegatedGoogleWorkspaceSource(
		contentTokenRepo,
		contentOAuthRegistry,
		cfg.ContentSources.GoogleWorkspace.PickerDeveloperKey,
		cfg.ContentSources.GoogleWorkspace.PickerAppID,
	)
	contentSources.Register(gwSource)
	logger.Info("Content source enabled: google_workspace (delegated, drive.file scope)")
}
```

- [ ] **Step 9.1.2: Add startup validation**

Near other startup-validation logic, add:

```go
if cfg.ContentSources.GoogleWorkspace.Enabled {
	if _, ok := contentOAuthRegistry.Get(api.ProviderGoogleWorkspace); !ok {
		logger.Error("content_sources.google_workspace.enabled=true requires content_oauth.providers.google_workspace.enabled=true")
		os.Exit(1)
	}
}
```

- [ ] **Step 9.1.3: Register the picker-token route**

Find where `/me/content_tokens/*` routes are registered (search `rg -n '"/me/content_tokens"' cmd/server/main.go`). After that block, register:

```go
pickerHandler := api.NewPickerTokenHandler(
	contentTokenRepo,
	contentOAuthRegistry,
	map[string]api.PickerTokenConfig{
		api.ProviderGoogleWorkspace: {
			DeveloperKey: cfg.ContentSources.GoogleWorkspace.PickerDeveloperKey,
			AppID:        cfg.ContentSources.GoogleWorkspace.PickerAppID,
		},
	},
)
r.POST("/me/picker_tokens/:provider_id", pickerHandler.Handle)
```

- [ ] **Step 9.1.4: Build + lint**

```
make build-server
make lint
```

Expected: clean.

- [ ] **Step 9.1.5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): register google_workspace source and picker-token route (#249)"
```

---

## Phase 10 — Integration tests and picker harness

### Task 10.1: Automated integration test

**Files:**
- Create: `test/integration/workflows/google_workspace_delegated_test.go`

- [ ] **Step 10.1.1: Write the integration test**

Create the file following the pattern of `test/integration/workflows/content_provider_test.go` and sub-project 1's `TestDelegatedContentProvider_EndToEnd_Integration`. The test should:

1. Start a stub OAuth provider that advertises `drive.file` scope.
2. Register a `google_workspace` entry in the content-oauth registry pointing at the stub.
3. Call `POST /me/content_tokens/google_workspace/authorize`; drive the stub callback; verify a token row exists.
4. Call `POST /me/picker_tokens/google_workspace`; verify response shape.
5. Create a document with a `picker_registration` payload (synthetic file_id); verify the DB row has picker columns set and `access_status = 'unknown'`.
6. Trigger access validation (via the attach flow or by calling the poller manually); verify diagnostics are written when validation fails (mock the delegated source's `DoFetch` to return an error).
7. Call `DELETE /me/content_tokens/google_workspace`; verify picker columns NULL-ed and `access_status = 'unknown'`.
8. Multi-user: a second user views the same document; per-viewer diagnostic assembly produces the expected `remediations`.

Do not invoke real Google APIs in this test — the stub OAuth provider handles auth, and `DoFetch` is mocked for the delegated source. This test validates wiring, not Drive API behavior.

- [ ] **Step 10.1.2: Run integration tests**

```
make test-integration name=TestGoogleWorkspaceDelegated
```

Expected: PASS.

- [ ] **Step 10.1.3: Run full integration suite to confirm no regressions**

```
make test-integration
```

Expected: the four pre-existing failures from the sub-project 1 baseline (`TestAuthFlowRateLimiting_MultiScope`, `TestClientCredentialsCRUD`, `TestIPRateLimiting_PublicEndpoints`, `TestCascadeDeletion`) may reproduce; no new failures beyond those.

- [ ] **Step 10.1.4: Commit**

```bash
git add test/integration/workflows/google_workspace_delegated_test.go
git commit -m "test(integration): end-to-end google_workspace delegated flow (#249)"
```

---

### Task 10.2: Picker harness + manual test

**Files:**
- Create: `scripts/google-picker-harness/index.html`
- Create: `test/integration/manual/google_workspace_delegated_test.go`
- Modify: `Makefile` — add `test-manual-google-workspace` target

- [ ] **Step 10.2.1: Create the picker harness HTML**

Create `scripts/google-picker-harness/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>TMI Google Picker Harness (manual test)</title>
<script src="https://apis.google.com/js/api.js"></script>
</head>
<body>
<h1>TMI Google Picker Harness</h1>
<p>This is a manual-test harness. Do not deploy.</p>
<button id="pick">Pick files</button>
<pre id="result">(picker output will appear here)</pre>
<script>
const params = new URLSearchParams(window.location.search);
const accessToken = params.get('access_token');
const developerKey = params.get('developer_key');
const appId = params.get('app_id');

function onLoad() {
  gapi.load('picker', { callback: () => {} });
}
onLoad();

document.getElementById('pick').addEventListener('click', () => {
  const view = new google.picker.DocsView()
    .setIncludeFolders(true)
    .setSelectFolderEnabled(false);
  const picker = new google.picker.PickerBuilder()
    .addView(view)
    .setOAuthToken(accessToken)
    .setDeveloperKey(developerKey)
    .setAppId(appId)
    .setCallback((data) => {
      if (data.action === google.picker.Action.PICKED) {
        document.getElementById('result').textContent = JSON.stringify(data.docs, null, 2);
      }
    })
    .build();
  picker.setVisible(true);
});
</script>
</body>
</html>
```

- [ ] **Step 10.2.2: Create the manual test**

Create `test/integration/manual/google_workspace_delegated_test.go`:

```go
//go:build manual

package manual

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestGoogleWorkspaceDelegatedFlow is a developer-driven manual test.
//
// Prerequisites:
//   1. TMI server running locally with google_workspace enabled.
//   2. TMI_CONTENT_TOKEN_ENCRYPTION_KEY set.
//   3. Tester has a real Google account with at least one Google Doc.
//
// Run with: make test-manual-google-workspace
func TestGoogleWorkspaceDelegatedFlow(t *testing.T) {
	baseURL := os.Getenv("TMI_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	jwt := os.Getenv("TMI_MANUAL_JWT")
	if jwt == "" {
		t.Fatalf("set TMI_MANUAL_JWT to a bearer token for your user (use /oauth/init + flow)")
	}

	reader := bufio.NewReader(os.Stdin)

	// Step 1: authorize delegated account.
	fmt.Println("Authorizing google_workspace for your user...")
	authURL := authorizeContentToken(t, baseURL, jwt)
	fmt.Printf("Open this URL in a browser and consent:\n  %s\n", authURL)
	fmt.Println("When done, press Enter to continue.")
	_, _ = reader.ReadString('\n')

	// Step 2: mint picker token.
	pickerResp := mintPickerToken(t, baseURL, jwt)
	fmt.Printf("Picker token minted; expires at %s\n", pickerResp["expires_at"])

	// Step 3: serve picker harness.
	harnessURL := servePickerHarness(t, pickerResp)
	fmt.Printf("Open the picker harness in a browser and pick a file:\n  %s\n", harnessURL)
	fmt.Print("Paste the picked file's JSON (one object from the docs array) here: ")
	raw, _ := reader.ReadString('\n')
	var picked map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &picked); err != nil {
		t.Fatalf("bad picker JSON: %v", err)
	}

	// Step 4: attach document with picker_registration.
	docID := attachDocument(t, baseURL, jwt, picked)
	fmt.Printf("Document created: %s\n", docID)

	// Step 5: trigger fetch + verify access_status == accessible.
	status := pollAccessStatus(t, baseURL, jwt, docID, 30*time.Second)
	if status != "accessible" {
		t.Fatalf("expected access_status=accessible, got %s", status)
	}

	// Step 6: cleanup.
	deleteContentToken(t, baseURL, jwt)
	fmt.Println("Test complete.")
}

func httpJSON(t *testing.T, method, url, jwt string, body interface{}) map[string]interface{} {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reqBody = bytes.NewReader(raw)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("http %s %s: %d %s", method, url, resp.StatusCode, string(raw))
	}
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil && err != io.EOF {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func authorizeContentToken(t *testing.T, baseURL, jwt string) string {
	body := map[string]string{"client_callback": "http://localhost:8079/"}
	resp := httpJSON(t, "POST", baseURL+"/me/content_tokens/google_workspace/authorize", jwt, body)
	return resp["authorization_url"].(string)
}

func mintPickerToken(t *testing.T, baseURL, jwt string) map[string]string {
	resp := httpJSON(t, "POST", baseURL+"/me/picker_tokens/google_workspace", jwt, nil)
	return map[string]string{
		"access_token":  resp["access_token"].(string),
		"developer_key": resp["developer_key"].(string),
		"app_id":        resp["app_id"].(string),
		"expires_at":    resp["expires_at"].(string),
	}
}

func servePickerHarness(t *testing.T, pickerData map[string]string) string {
	t.Helper()
	srv := &http.Server{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("../../../scripts/google-picker-harness")))
	srv.Handler = mux
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	u := fmt.Sprintf("http://%s/index.html?access_token=%s&developer_key=%s&app_id=%s",
		ln.Addr().String(),
		pickerData["access_token"], pickerData["developer_key"], pickerData["app_id"])
	return u
}

func attachDocument(t *testing.T, baseURL, jwt string, picked map[string]interface{}) string {
	// Locate or create a threat model to attach into.
	// For the manual test, require the tester to set TMI_MANUAL_THREAT_MODEL_ID.
	tmID := os.Getenv("TMI_MANUAL_THREAT_MODEL_ID")
	if tmID == "" {
		t.Fatalf("set TMI_MANUAL_THREAT_MODEL_ID to a threat model owned by your user")
	}
	fileID, _ := picked["id"].(string)
	uri, _ := picked["url"].(string)
	mime, _ := picked["mimeType"].(string)
	name, _ := picked["name"].(string)
	body := map[string]interface{}{
		"name": name,
		"uri":  uri,
		"picker_registration": map[string]string{
			"provider_id": "google_workspace",
			"file_id":     fileID,
			"mime_type":   mime,
		},
	}
	resp := httpJSON(t, "POST", baseURL+"/threat_models/"+tmID+"/documents", jwt, body)
	return resp["id"].(string)
}

func pollAccessStatus(t *testing.T, baseURL, jwt, docID string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	tmID := os.Getenv("TMI_MANUAL_THREAT_MODEL_ID")
	for time.Now().Before(deadline) {
		resp := httpJSON(t, "GET", baseURL+"/threat_models/"+tmID+"/documents/"+docID, jwt, nil)
		if s, _ := resp["access_status"].(string); s != "" && s != "unknown" {
			return s
		}
		time.Sleep(3 * time.Second)
	}
	return "timeout"
}

func deleteContentToken(t *testing.T, baseURL, jwt string) {
	req, err := http.NewRequest("DELETE", baseURL+"/me/content_tokens/google_workspace", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete failed: %d %s", resp.StatusCode, string(raw))
	}
}
```

Implement the helper functions. They are mechanical HTTP client calls; use `net/http` directly. `servePickerHarness` starts a stdlib HTTP server on `127.0.0.1:0`, serves `scripts/google-picker-harness/index.html` with query params injected into the URL, and returns the URL. Use a `testing.T.Cleanup` to shut down the server.

- [ ] **Step 10.2.3: Add Makefile target**

In `Makefile`, find the testing targets section and add:

```makefile
.PHONY: test-manual-google-workspace
test-manual-google-workspace: ## Run the manual Google Workspace picker test (requires a real Google account)
	go test -tags=manual -run TestGoogleWorkspaceDelegatedFlow -v ./test/integration/manual/...
```

- [ ] **Step 10.2.4: Run `make list-targets` to confirm the target appears**

```
make list-targets | rg google-workspace
```

Expected: `test-manual-google-workspace` listed.

- [ ] **Step 10.2.5: Commit**

```bash
git add scripts/google-picker-harness/index.html \
        test/integration/manual/google_workspace_delegated_test.go \
        Makefile
git commit -m "test(manual): add google_workspace picker harness and manual test (#249)"
```

---

## Phase 11 — Final validation and handoff

### Task 11.1: Run all quality gates

- [ ] **Step 11.1.1: Lint**

```
make lint
```

Fix any issues.

- [ ] **Step 11.1.2: OpenAPI validation**

```
make validate-openapi
```

Expected: no new errors.

- [ ] **Step 11.1.3: Build**

```
make build-server
```

Expected: clean.

- [ ] **Step 11.1.4: Unit tests**

```
make test-unit
```

Expected: all pass, 100+ new tests.

- [ ] **Step 11.1.5: Integration tests**

```
make test-integration
```

Expected: all new tests pass; pre-existing failures from the sub-project 1 baseline (documented in #249's comment) may still appear and are acceptable.

- [ ] **Step 11.1.6: Confirm no unsafe union methods**

```
make check-unsafe-union-methods
```

Expected: clean.

### Task 11.2: Update #249 tracking comment

- [ ] **Step 11.2.1: Add a comment to #249**

```bash
gh issue comment 249 --body "$(cat <<'EOF'
## Sub-project 4 of 5 — Google Workspace delegated picker — complete

**Design spec:** [`docs/superpowers/specs/2026-04-18-google-workspace-delegated-picker-design.md`](...)
**Implementation plan:** [`docs/superpowers/plans/2026-04-18-google-workspace-delegated-picker.md`](...)

### Follow-up issues filed
- [ericfitz/tmi-ux#626](https://github.com/ericfitz/tmi-ux/issues/626) — picker integration UI (blocks on this landing)
- [ericfitz/tmi#283](https://github.com/ericfitz/tmi/issues/283) — OOXML export upgrade (blocks on sub-project 5)

### Remaining sub-projects
| # | Sub-project | Prereq |
|---|-------------|--------|
| 2 | Confluence provider (delegated) | sub-project 1 (done) |
| 3 | OneDrive / SharePoint provider (service) | #232 only |
| 5 | OOXML extractors (DOCX + PPTX) | none |
EOF
)"
```

### Task 11.3: Open a PR (do not merge automatically — user reviews)

- [ ] **Step 11.3.1: Push feature branch**

```bash
git push -u origin feature/google-workspace-picker
```

- [ ] **Step 11.3.2: Open PR**

```bash
gh pr create --base dev/1.4.0 --title "feat(content): google_workspace delegated picker access (#249)" --body "$(cat <<'EOF'
## Summary
- New `DelegatedGoogleWorkspaceSource` (drive.file + Picker) coexisting with the service-account source via document-aware dispatch
- New `POST /me/picker_tokens/{provider_id}` endpoint
- Structured `access_diagnostics` on document responses (reason_code + remediations, computed per-viewer)
- Picker metadata stored on the documents row; cleared on un-link cascade

Spec: `docs/superpowers/specs/2026-04-18-google-workspace-delegated-picker-design.md`

## Test plan
- [ ] `make lint` passes
- [ ] `make validate-openapi` passes
- [ ] `make test-unit` passes
- [ ] `make test-integration` passes (pre-existing failures only)
- [ ] Manual test (`make test-manual-google-workspace`) completes against a real Google account

Closes sub-project 4 of #249. Follow-ups: ericfitz/tmi-ux#626, ericfitz/tmi#283.
EOF
)"
```

---

## Self-review checklist (engineer executes before claiming plan complete)

- [ ] All steps cite exact file paths or exact commands
- [ ] Every code block compiles (no pseudocode; no `...`)
- [ ] No TBD, TODO, or "similar to" placeholders
- [ ] Method names and types are consistent across tasks
- [ ] Each task has a failing test before implementation (TDD)
- [ ] Each task ends with a commit
- [ ] Spec requirements are each mapped to at least one task:
  - Data model (Section "Data model") → Tasks 1.1, 1.3, 6.1, 7.1
  - HTTP API (Section "HTTP API") → Tasks 5.2, 6.1, 8.1
  - Access diagnostics → Tasks 2.1, 2.2, 2.3
  - DelegatedGoogleWorkspaceSource → Tasks 3.1, 3.2
  - Configuration → Task 5.1, 9.1
  - Testing → Tasks (throughout) + 10.1, 10.2
  - Un-link cascade → Task 7.1
  - OpenAPI regen → Tasks 8.1, 8.2
  - Server wiring → Task 9.1
