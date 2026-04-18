# Google Workspace Delegated Picker Access

**Issue:** #249 — feat(timmy): Confluence and OneDrive content providers (sub-project 4 of 5)
**Date:** 2026-04-18
**Status:** Draft (pending user review)
**Builds on:** #249 sub-project 1 (delegated content provider infrastructure, landed on `dev/1.4.0` commit `74d36522`)

## Overview

This spec covers sub-project 4 of #249: a delegated Google Workspace content source that uses Google's per-file authorization model (`drive.file` scope + Google Picker) to fetch Drive documents under the user's own identity rather than the operator's service account.

The existing service-account `GoogleDriveSource` from #232 stays in place and handles Drive URIs that have been explicitly shared with the service account. The new `DelegatedGoogleWorkspaceSource` handles Drive documents the user has selected through the Google Picker. The two coexist in the `ContentSourceRegistry`; at fetch time, a document-aware dispatch routine picks the right source based on whether the document was picker-registered.

This sub-project is server-side only. The tmi-ux picker UI is a separate follow-up issue.

## Background

#232 shipped a content pipeline with `ContentSource` / `AccessValidator` / `AccessRequester` interfaces and a service-account Google Drive source. #249 sub-project 1 shipped the delegated infrastructure: per-user OAuth tokens (`user_content_tokens`), `DelegatedSource` helper, `ContentOAuthProviderRegistry`, account-linking endpoints under `/me/content_tokens/*`, Redis OAuth state, PKCE S256, and `RefreshWithLock` for concurrent-refresh serialization.

What remains for Google Workspace delegated access:

- A concrete `DelegatedGoogleWorkspaceSource` that consumes the `DelegatedSource` helper.
- A picker-token endpoint that mints a short-lived Google access token for browser-side Google Picker JS.
- A document-attach extension allowing the client to supply a picker registration (`file_id`, `mime_type`) at attach time.
- Picker metadata columns on the `documents` table so fetch time can route to the delegated source.
- Structured access diagnostics on the document API so tmi-ux can render user-appropriate remediation UI.

## Why Google Picker + `drive.file`

The Google OAuth scope most intuitively matching "read the user's documents" (`drive.readonly`) is a **restricted** scope: apps requesting it must complete Google's CASA Tier 2 annual security assessment (~$15k–$75k) and face Workspace-admin allow-list gating. For TMI's use case — reading specific user-chosen documents — this cost is disproportionate.

The `drive.file` scope avoids CASA-2. It grants the app access only to files the user has **picked via Google Picker** (or files the app created). This is the pattern used by Slack, Notion, Zapier, Trello, Asana, Linear, Miro, and most "read specific user-chosen files" applications. User consent is implicit in the picker interaction: selecting a file in the picker equals granting the app access to that file.

Tradeoff: picker-based flows require a browser-side UI step (tmi-ux) because the Picker library runs in the browser. We accept that cost and factor the tmi-ux work into a separate follow-up issue.

Alternative paths considered and rejected:

- `drive.readonly` — CASA-2 cost; admin-blockable; disproportionate scope.
- `documents.readonly` + `spreadsheets.readonly` + `presentations.readonly` (narrow per-service scopes) — no CASA-2 but still admin-blockable, requires verification review, and covers only Workspace-native docs (not PDFs/images).
- Programmatic access-request via Drive `accessproposals` — **does not exist**. Drive's `accessproposals` resource has only `get`/`list`/`resolve` methods for the owner side; there is no API to create a request.
- TMI-sent email to document owners — would require building outbound email infrastructure; out of scope.
- Service-account-only (status quo) — known friction point; users must discover the service account email and share manually.

## Decomposition of #249 (recap)

| Order | Sub-project | Status |
|-------|-------------|--------|
| 1 | Delegated provider infrastructure | Shipped on `dev/1.4.0` |
| 2 | Confluence provider (delegated) | Pending |
| 3 | OneDrive / SharePoint provider (service) | Pending |
| **4** | **Google Workspace delegated access** (this spec) | **Draft** |
| 5 | OOXML extractors (DOCX + PPTX) | Pending |

#249 stays open as the tracking issue until all five land.

## Design decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Coexistence with service-account source | Delegated wins when user has linked token; service-account is fallback | Users expect "their view"; service-account preserves pre-linking capability |
| Provider identifier | `google_workspace` (distinct from existing `google_drive`) | Keeps audit trails clear; matches issue-title phrasing and cross-sub-project naming |
| OAuth scope | `drive.file` | Avoids CASA-2 audit; industry-standard pattern |
| Authorization UX | Google Picker (browser-side, in tmi-ux) | Required by `drive.file`; `file_id` is recorded at pick time and used at every subsequent fetch |
| Picker registration transport | Inline in document-attach payload | One atomic round trip; no orphan registrations |
| Picker metadata storage | Columns on `documents` row (not separate table) | 1:1 with document; cascade-delete is automatic; no joins |
| Un-link cascade | Null picker columns for affected documents before token deletion | Affected docs naturally transition to `auth_required` on next fetch |
| Pre-existing Drive documents | Stay on service-account path; no retroactive upgrade | Keeps the invariant "fetch path decided at attach time"; upgrade = delete + re-add |
| Picker-token endpoint | Dedicated `POST /me/picker_tokens/{provider_id}` | State-effectful (may trigger refresh); audit-loggable; non-cacheable |
| Export format (phase 0) | Plain text / CSV (matches current service-account behavior) | Keeps scope tight; fidelity upgrade is a separate follow-up |
| Access diagnostics on document | `reason_code` + `reason_detail` + `remediations[]`; computed at read time | Per-viewer correctness for multi-user threat models; stable machine-readable codes for tmi-ux localization |
| `reason_code` / `remediation.action` | Enumerated with `other` escape hatch | tmi-ux localizes by code; no free-form text in API |
| Content-type reported to pipeline | Exported type only | Current extractors look up by content-type; no interface churn |

## Architecture

### New components

1. `DelegatedGoogleWorkspaceSource` (`api/content_source_google_workspace.go`) — concrete delegated source, embeds the `DelegatedSource` helper from sub-project 1.
2. Picker-token handler (`api/picker_token_handler.go`) — serves `POST /me/picker_tokens/{provider_id}`.
3. `FindSourceForDocument` — new method on `ContentSourceRegistry` that picks the right source for fetching a specific document (honors picker metadata + user's linked tokens).
4. Access-diagnostics builder — read-time assembly of the `access_diagnostics` object from stored `reason_code` + caller context.
5. Migration adding picker metadata + diagnostics columns to `documents`.
6. `scripts/google-picker-harness/index.html` — minimal standalone picker-test harness for manual testing only.
7. `test/integration/manual/google_workspace_delegated_test.go` (`//go:build manual`) — human-driven end-to-end test.

### Unchanged components

- `ContentSource`, `AccessValidator`, `AccessRequester` interfaces from #232.
- Existing service-account `GoogleDriveSource`.
- `DelegatedSource`, `ContentOAuthProviderRegistry`, `ContentTokenRepository`, `/me/content_tokens/*` endpoints from sub-project 1.
- Content extractors (plain-text / HTML).
- Access poller (`api/access_poller.go`) — upgraded only to use the new dispatch.

### Fetch dispatch

The existing `ContentSourceRegistry.FindSource(ctx, uri)` routes by URI alone — insufficient when two sources match the same URI and we want delegated to win only for picker-registered documents under the owning user.

New method:

```go
func (r *ContentSourceRegistry) FindSourceForDocument(
    ctx context.Context,
    doc *Document,
    userID string,
) (ContentSource, bool)
```

Logic:

1. If `doc.PickerProviderID` is set **and** the provider is registered **and** `userID` matches `doc.OwnerID` **and** the user has an active (not `failed_refresh`) linked token for the provider → return the delegated source with that name.
2. Otherwise, fall through to `FindSource(ctx, doc.Uri)`.

Callers:

- Document-attach handler (initial `ValidateAccess` call).
- Access poller (per-document retry loop).
- Any future document-fetch code path.

The legacy `FindSource(ctx, uri)` remains for URI-only dispatch (e.g., validation that a URI is handleable at all, before a document row exists).

### File layout

```
api/
  content_source_google_workspace.go       (new, ~150 lines)
  content_source_google_workspace_test.go  (new)
  picker_token_handler.go                   (new, ~100 lines)
  picker_token_handler_test.go              (new)
  access_diagnostics.go                     (new — read-time builder)
  access_diagnostics_test.go                (new)
  content_source_registry.go                (extended: FindSourceForDocument)
  document_sub_resource_handlers.go         (extended: picker_registration validation + new dispatch)
  document_store_gorm.go                    (extended: new columns, update methods)
  access_poller.go                          (extended: new dispatch + per-doc user context)
internal/config/
  content_sources.go                        (extended: GoogleWorkspaceConfig)
auth/migrations/
  NNNN_add_picker_and_diagnostics_to_documents.sql (new)
scripts/google-picker-harness/
  index.html                                (new, ~100 lines)
test/integration/manual/
  google_workspace_delegated_test.go        (new, build-tag: manual)
test/integration/workflows/
  google_workspace_delegated_test.go        (new)
```

## Data model

### New columns on `documents`

**Picker metadata** (set at attach time when `picker_registration` is supplied):

| Column | Type | Notes |
|---|---|---|
| `picker_provider_id` | `varchar(64)` | e.g. `"google_workspace"`; NULL for non-picker docs |
| `picker_file_id` | `varchar(255)` | Drive file id returned by Picker |
| `picker_mime_type` | `varchar(128)` | Recorded at pick time |

Invariant: all three NULL or all three non-NULL. Enforced via CHECK constraint.

**Access diagnostics** (populated/refreshed by pipeline state transitions):

| Column | Type | Notes |
|---|---|---|
| `access_reason_code` | `varchar(64)` | NULL when `access_status IN ('accessible', 'unknown')` |
| `access_reason_detail` | `text` | ≤512 chars; NULL unless `reason_code = 'other'` |
| `access_status_updated_at` | `timestamptz` | Updated on every `access_status` or diagnostic change |

**Not stored**: `remediation_action` and `remediation_params`. These are derived at read time from `reason_code` + caller context (see Access Diagnostics section).

### Indexes

- Partial composite index `(picker_provider_id, picker_file_id) WHERE picker_provider_id IS NOT NULL` — supports un-link cascade queries and future picker-file lookups.

### Migration shape

```sql
ALTER TABLE documents
  ADD COLUMN picker_provider_id varchar(64),
  ADD COLUMN picker_file_id varchar(255),
  ADD COLUMN picker_mime_type varchar(128),
  ADD COLUMN access_reason_code varchar(64),
  ADD COLUMN access_reason_detail text,
  ADD COLUMN access_status_updated_at timestamptz,
  ADD CONSTRAINT documents_picker_metadata_consistency CHECK (
    (picker_provider_id IS NULL AND picker_file_id IS NULL AND picker_mime_type IS NULL)
    OR
    (picker_provider_id IS NOT NULL AND picker_file_id IS NOT NULL AND picker_mime_type IS NOT NULL)
  );

CREATE INDEX idx_documents_picker_lookup
  ON documents (picker_provider_id, picker_file_id)
  WHERE picker_provider_id IS NOT NULL;
```

Down migration drops the index, the constraint, and the six columns.

### Un-link cascade

When `DELETE /me/content_tokens/google_workspace` fires, the existing handler is extended with a transactional step run **before** the token-delete + provider-revoke logic:

```sql
UPDATE documents
   SET picker_provider_id = NULL,
       picker_file_id = NULL,
       picker_mime_type = NULL,
       access_status = 'unknown',
       access_reason_code = NULL,
       access_reason_detail = NULL,
       access_status_updated_at = NOW()
 WHERE owner_id = :user_id
   AND picker_provider_id = :provider_id;
```

On next fetch attempt, affected documents hit URL-based dispatch (no picker metadata) and fall through to service-account. If that too fails, the pipeline transitions them to `pending_access` with `no_accessible_source` reason.

Re-linking the same Google account does not restore access: Google invalidates all previously-granted picker scopes when the OAuth token is revoked. User must re-pick affected files.

## HTTP API

### New operation: `POST /me/picker_tokens/{provider_id}`

Mints a short-lived Google OAuth access token for tmi-ux to hand to Google Picker JS.

**Request**:
- Path param: `provider_id` (currently only `google_workspace` is valid).
- Body: empty.
- Auth: JWT bearer.

**Response 200**:
```json
{
  "access_token": "ya29.a0...",
  "expires_at": "2026-04-18T14:32:00Z",
  "developer_key": "AIzaSy...",
  "app_id": "123456789012"
}
```

**Response codes**:

| Code | Meaning |
|---|---|
| 200 | Token minted (possibly after refresh) |
| 401 | Not authenticated, or token is `failed_refresh` |
| 404 | No linked token for this provider |
| 422 | Provider not registered / not enabled, or `picker_developer_key` / `picker_app_id` unconfigured |
| 429 | Rate limited |
| 503 | Transient refresh failure |

**Handler logic**:

1. Validate provider registered + enabled. 422 if not.
2. Validate `picker_developer_key` and `picker_app_id` are non-empty config values. 422 if missing.
3. `ContentTokenRepository.GetByUserAndProvider(ctx, userID, providerID)`. `ErrContentTokenNotFound` → 404. Status `failed_refresh` → 401.
4. If token is expired (within 30s skew), run the `DelegatedSource.refresh`-equivalent. Permanent failure → 401; transient → 503.
5. Return `{access_token, expires_at, developer_key, app_id}` from the (possibly refreshed) token + config.
6. Emit structured log `picker_token_minted` with `user_id`, `provider_id`, `expires_at`.

**Security notes**:

- This endpoint is the single documented exception to sub-project 1's "plaintext tokens never leave the repository layer" rule.
- Response is non-cacheable (`x-cacheable-endpoint: false`). Marked appropriately in OpenAPI.
- Existing per-user rate-limit middleware applies.

### Document-attach extension

Existing document-attach request bodies gain an optional `picker_registration` field:

```json
{
  "url": "https://docs.google.com/document/d/1a2b.../edit",
  "picker_registration": {
    "provider_id": "google_workspace",
    "file_id": "1a2b...",
    "mime_type": "application/vnd.google-apps.document"
  }
}
```

**Semantics**:

- `picker_registration` is optional. Absent → existing behavior (no picker metadata stored; fetch goes through URL-based dispatch).
- Present → server validates:
  1. `provider_id` is a registered, enabled delegated provider.
  2. Caller has a linked, active token for `provider_id`.
  3. `file_id` is non-empty.
  4. `file_id` matches `extractGoogleDriveFileID(url)`. Mismatch → 400.
  5. `mime_type` is non-empty (stored as-is; not validated against an allow-list).
- On success, the document row is created with `picker_provider_id`, `picker_file_id`, `picker_mime_type` populated. Write is atomic with the document row insert.

**Additional response codes for the picker path**:

| Code | Meaning |
|---|---|
| 400 | `file_id` mismatched `url`, `file_id` empty, or `url` unparseable |
| 401 | No linked token for caller |
| 422 | `provider_id` not registered / not enabled |

### OpenAPI schemas

```yaml
PickerRegistration:
  type: object
  required: [provider_id, file_id, mime_type]
  properties:
    provider_id: { type: string, enum: [google_workspace] }
    file_id: { type: string, minLength: 1, maxLength: 255 }
    mime_type: { type: string, minLength: 1, maxLength: 128 }

PickerTokenResponse:
  type: object
  required: [access_token, expires_at, developer_key, app_id]
  properties:
    access_token: { type: string }
    expires_at: { type: string, format: date-time }
    developer_key: { type: string }
    app_id: { type: string }

DocumentAccessDiagnostics:
  type: object
  required: [reason_code, remediations]
  properties:
    reason_code:
      type: string
      enum:
        - token_not_linked
        - token_refresh_failed
        - token_transient_failure
        - picker_registration_invalid
        - no_accessible_source
        - source_not_found
        - fetch_error
        - other
    reason_detail:
      type: string
      nullable: true
      maxLength: 512
    remediations:
      type: array
      items: { $ref: '#/components/schemas/AccessRemediation' }

AccessRemediation:
  type: object
  required: [action, params]
  properties:
    action:
      type: string
      enum:
        - link_account
        - relink_account
        - repick_file
        - share_with_service_account
        - repick_after_share
        - retry
        - contact_owner
    params:
      type: object
      additionalProperties: true
```

The document schema gains:

- Optional `picker_registration` on attach request bodies (via the existing document-attach operations).
- Optional `access_diagnostics: DocumentAccessDiagnostics` on the response.
- Optional `access_status_updated_at: string (date-time)`.

Regeneration via `make generate-api`.

## Access diagnostics

`access_diagnostics` on the document response is assembled at read time from `access_reason_code` + `access_reason_detail` + **caller context** (the user making the GET request — who may or may not be the document owner).

### `reason_code` → default `remediations` mapping

| `reason_code` | Default `remediations` |
|---|---|
| `token_not_linked` | `[{action: link_account, params: {provider_id}}]` |
| `token_refresh_failed` | `[{action: relink_account, params: {provider_id}}]` |
| `token_transient_failure` | `[{action: retry, params: {}}]` |
| `picker_registration_invalid` | `[{action: repick_file, params: {provider_id}}]` |
| `no_accessible_source` | `[{action: share_with_service_account, params: {service_account_email}}]`, plus `{action: repick_after_share, params: {provider_id, user_email}}` appended when caller has a linked Google Workspace token |
| `source_not_found` | `[]` |
| `fetch_error` | `[{action: retry, params: {}}]` |
| `other` | `[]` |

### Caller-context assembly for `no_accessible_source`

Per-viewer remediation assembly:

1. Always include `{action: share_with_service_account, params: {service_account_email: <from config>}}` first.
2. If caller has an active linked token for `google_workspace`, append `{action: repick_after_share, params: {provider_id: "google_workspace", user_email: <from caller's user record>}}`.
3. Serialize the array in order into the response.

This is recomputed on every GET — the `remediations` array is never stored. The document row only stores `access_reason_code` + `access_reason_detail`.

### `other` escape hatch discipline

`reason_code = "other"` is set only when the pipeline encounters a non-retryable error that cannot be mapped to any other enum value. Every occurrence triggers a structured warning log so operators can identify new reasons that should become enum values. Unit tests assert that known error paths produce specific enum codes, never `other`.

### Pipeline write points

The following code paths write `(access_status, access_reason_code, access_reason_detail, access_status_updated_at)` together:

- Document attach handler — after initial `ValidateAccess`.
- Access poller — per iteration.
- `DelegatedSource` error translation — `ErrAuthRequired` → `token_refresh_failed` or `token_not_linked` depending on the cause; `ErrTransient` → `token_transient_failure`.
- Un-link cascade — clears diagnostics and resets to `unknown`.

Writes are through a new document-store method `UpdateAccessStatusWithDiagnostics` that extends the existing `UpdateAccessStatus` signature.

## `DelegatedGoogleWorkspaceSource`

### Interface

Embeds `DelegatedSource` from sub-project 1. Provides:

- `Name()` returns `"google_workspace"`.
- `CanHandle(ctx, uri)` returns true for `docs.google.com` / `drive.google.com` host URIs. Same predicate as `GoogleDriveSource`; the dispatch logic in `FindSourceForDocument` handles picking the right source.
- `Fetch(ctx, uri)` delegates to `DelegatedSource.FetchForUser(ctx, userID, uri)` with a `DoFetch` callback that:
  1. Extracts the file id from `uri` via `extractGoogleDriveFileID` (reused from the existing Google Drive source).
  2. Constructs a Drive API client authorized with the plaintext access token supplied by the helper.
  3. Runs the same MIME-type branching as the service-account source: Docs → export `text/plain`, Sheets → export `text/csv`, Slides → export `text/plain`, other → direct download.
  4. Returns bytes + content-type.
- `ValidateAccess(ctx, uri)` — Drive `files.get(fileId).Fields("id")` with the user's token. Maps 4xx to `(false, nil)`; 5xx or network errors surface as `ErrTransient`.
- `RequestAccess(ctx, uri)` — no Drive API call. Calls `ValidateAccess` first; if it now succeeds, returns nil. Otherwise returns a clear error. Actionable remediation is surfaced via `access_diagnostics` at the pipeline level, not via this method directly.

### Scope

`https://www.googleapis.com/auth/drive.file` only. Configured via `content_oauth.providers.google_workspace.required_scopes` in sub-project 1's provider registry.

### Export fidelity (phase 0)

This sub-project keeps the current plain-text / CSV export behavior, matching what the service-account source does today. The follow-up issue (filed after spec approval) will upgrade both Google sources to DOCX/PPTX/XLSX export once sub-project 5 lands the OOXML extractors.

### Content-type reporting

The source reports only the exported content type (`text/plain`, `text/csv`), never the original Google MIME (`application/vnd.google-apps.document`). This matches current service-account behavior and avoids coupling this sub-project to extractor changes.

### Single-user discipline

`Fetch`, `ValidateAccess`, and `RequestAccess` require `UserIDFromContext(ctx)` to return a non-empty user id. Missing user context → clear error (inherits from `DelegatedSource.FetchForUser`).

## Configuration

### New config subsection `content_sources.google_workspace`

```yaml
content_sources:
  google_drive:
    enabled: true
    service_account_email: indexer@tmi.iam.gserviceaccount.com
    credentials_file: /etc/tmi/google-service-account.json
  google_workspace:
    enabled: false
    picker_developer_key: ""
    picker_app_id: ""

content_oauth:
  callback_url: "https://tmi.example.com/oauth2/content_callback"
  allowed_client_callbacks:
    - "https://tmi-ux.example.com/*"
  providers:
    google_workspace:
      enabled: false
      client_id: "..."
      client_secret: "..."
      auth_url: "https://accounts.google.com/o/oauth2/v2/auth"
      token_url: "https://oauth2.googleapis.com/token"
      userinfo_url: "https://www.googleapis.com/oauth2/v2/userinfo"
      revocation_url: "https://oauth2.googleapis.com/revoke"
      required_scopes: ["https://www.googleapis.com/auth/drive.file"]
```

### Environment variables

| Env var | Config path |
|---|---|
| `TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_ENABLED` | `content_sources.google_workspace.enabled` |
| `TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_DEVELOPER_KEY` | `content_sources.google_workspace.picker_developer_key` |
| `TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_APP_ID` | `content_sources.google_workspace.picker_app_id` |
| `TMI_CONTENT_OAUTH_PROVIDERS_GOOGLE_WORKSPACE_*` | Handled by sub-project 1's provider-discovery scheme |

### Startup validation

When `content_sources.google_workspace.enabled = true`:

- `content_oauth.providers.google_workspace.enabled` must also be `true`. Violation → refuse to start with a clear error.
- `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` must be present (already enforced by sub-project 1 whenever any delegated provider is enabled).
- `picker_developer_key` missing → log structured warning; source stays registered but `POST /me/picker_tokens` returns 422.
- `picker_app_id` missing → same as above.

Missing OAuth provider fields (`client_id`, `auth_url`, `token_url`) → handled by sub-project 1's existing validator (disables the provider with a structured error log; non-fatal).

### Operator setup (documented in the wiki)

Not shipped code; captured here for the operator docs:

- Google Cloud project with Drive API + Google Picker API enabled.
- OAuth 2.0 client of type "Web application" with TMI's `content_oauth.callback_url` in Authorized redirect URIs.
- API key with Drive API + Picker API access.
- Google OAuth verification review for production deployments (one-time per project; standard verification, not CASA-2).

## Testing

### Unit tests

- **`DelegatedGoogleWorkspaceSource`**: `Name`, `CanHandle`, `Fetch` (via mocked `DelegatedSource.DoFetch`), `Fetch` without user context, `ValidateAccess`, `RequestAccess`.
- **Picker-token handler**: happy path, refresh-on-expiry, 404/401/422/503 branches, rate-limit wiring.
- **Document-attach picker-registration extension**: success path with valid registration, `file_id` mismatch, empty `file_id`, unknown provider, no linked token, non-picker path unchanged.
- **Access-diagnostics builder**: each `reason_code` → default `remediations`; `no_accessible_source` with and without caller's linked token; `other` surfaces `reason_detail`; absence of diagnostics on `accessible` / `unknown` documents.
- **Un-link cascade**: documents with matching `owner_id` + `picker_provider_id` have columns cleared; others untouched; `access_status` resets to `unknown`.
- **`FindSourceForDocument`**: delegated selected when picker metadata + matching user + active token; fall-through otherwise.

### Integration tests (`test/integration/workflows/`)

Real Postgres + stub OAuth provider (reusing sub-project 1's fixtures parameterized for `google_workspace`):

- Attach with picker registration → document row has `picker_*` columns set.
- GET document → `access_diagnostics` absent when `accessible`; present with correct shape when `pending_access`.
- `reason_code = no_accessible_source` + caller has linked account → `remediations` contains both elements in the specified order.
- Un-link cascade at the API level: authorize → delete token → GET prior document shows cleared picker columns and transitioned `access_status`.
- Picker-token endpoint end-to-end: authorize → mint → response contains a valid bearer token (refreshed if needed).
- Multi-user view: user A attaches picker-registered doc; user B views same threat model → per-viewer diagnostic assembly produces correct `remediations`.

No stub Drive API. Delegated Fetch is validated at unit-test granularity with mocked `DoFetch`. This matches the existing service-account source's testing discipline (no Drive API stub exists for it either).

### Manual integration test (`//go:build manual`)

Location: `test/integration/manual/google_workspace_delegated_test.go`. Make target: `make test-manual-google-workspace`.

Prerequisites (documented in file header):

- TMI server running locally with `content_sources.google_workspace.enabled = true` and full `content_oauth.providers.google_workspace` config against a real Google Cloud project.
- `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` set.
- Tester has a real Google account with at least one Google Doc they own.
- TMI's existing test service account is configured (used for the parity check).

Flow:

1. `POST /me/content_tokens/google_workspace/authorize` with `client_callback` pointing at the oauth-client-callback-stub.
2. Test prints the authorization URL. Tester consents in a browser.
3. Test polls the callback stub until tokens are saved.
4. Test invokes `POST /me/picker_tokens/google_workspace`.
5. Test starts a small local HTTP server that serves `scripts/google-picker-harness/index.html` with picker inputs in query params. Tester opens the harness URL.
6. Harness runs Google Picker JS. Tester selects a file. Harness renders the picker result.
7. Tester supplies the picked file info to the test (stdin, or harness POST back).
8. Test calls document-attach with the `picker_registration` payload.
9. Test triggers the fetch pipeline against the document and asserts non-empty bytes with expected content-type.
10. **Parity check**: for a file also shared with the service account, fetch via service-account source (`content_source = "google_drive"`) and verify both succeed.
11. Cleanup: `DELETE /me/content_tokens/google_workspace` revokes at Google and clears local state.

### Picker harness (`scripts/google-picker-harness/index.html`)

Self-contained static HTML file (~100 lines). Loads Google Picker JS from Google's CDN. Reads `access_token`, `developer_key`, `app_id` from query params. "Pick files" button opens the picker. Result area renders the picker's JSON output for copy-paste into the manual test.

No build step. Served by a stdlib `net/http` server inside the manual test binary.

### What is not tested

- Real Google Drive API calls in CI — the existing service-account source tests don't stub Drive either; we stay consistent.
- Real picker JS behavior in CI — covered only by the manual test.
- Google OAuth verification flow — infrastructural, once per Google Cloud project at deployment time.

### CATS fuzzing

New endpoints (`POST /me/picker_tokens/{provider_id}`) and the extended document-attach request body are picked up by CATS on the next scheduled release-gate run. No per-change requirement.

## Implementation phases

Phases land as independent commits. Phases 1–8 are strictly sequential; phase 9 overlaps 5–8; phase 10 runs last.

1. **Data model and schema migration.** Add columns + constraint + partial index. GORM models updated. `UpdateAccessStatusWithDiagnostics` added to the document store. Unit tests for the repository layer.
2. **Access-diagnostics assembly.** Read-time builder with `reason_code` → `remediations` mapping. Per-viewer overlay for `no_accessible_source`. GET document handler wires the builder into response serialization. Unit tests.
3. **`DelegatedGoogleWorkspaceSource`.** Source type, `DoFetch` callback, registry registration gated on `content_sources.google_workspace.enabled`. Unit tests.
4. **`FindSourceForDocument` dispatch.** Registry extension, access-poller upgrade with per-document owner context, document-attach handler upgrade for initial validation. Unit tests + poller test updates.
5. **Picker-token endpoint.** Handler, config section, startup validation. Unit tests.
6. **Document-attach `picker_registration` extension.** OpenAPI schema additions, handler validation, atomic write. Unit tests.
7. **Un-link cascade.** Extend `DELETE /me/content_tokens/{provider_id}` handler with transactional picker-column clearing. Unit tests.
8. **OpenAPI regeneration.** `make generate-api`, `make validate-openapi`. Commit regenerated `api/api.go`.
9. **Integration tests and manual harness.** Workflow integration tests, picker harness, manual test, `make test-manual-google-workspace` target.
10. **Documentation.** Wiki pages covering operator setup (Google Cloud project, OAuth client, Picker API, verification review) and the user-facing flow.

## Acceptance criteria

- All new endpoints pass handler + integration tests.
- Schema migrations apply cleanly and roll back cleanly.
- Un-link cascade correctly NULLs picker metadata for the affected owner's documents.
- Diagnostics assembly produces the correct `remediations` array for each `reason_code` + caller-context combination.
- Manual test harness completes the full authorize → pick → attach → fetch flow against a real Google account.
- Parity check in manual test: for a file both accounts can read, delegated and service-account fetches succeed (bytes may differ in format; both must be non-empty and parseable).
- `make lint`, `make build-server`, `make test-unit`, `make test-integration`, `make validate-openapi` all pass.
- No behavior change for non-picker document paths (`access_status` state machine and service-account fetch unchanged in character).
- Follow-up issues (tmi-ux picker UI; OOXML upgrade) filed before this sub-project's final commit.

## Out of scope (explicit)

- Other delegated providers (Confluence is sub-project 2; OneDrive is sub-project 3).
- OOXML extractors (sub-project 5).
- OOXML export upgrade for Google Drive sources (follow-up issue).
- tmi-ux picker UI (follow-up issue).
- Background refresh / proactive token warming.
- Dynamic scope upgrades (scope changes require re-linking).
- Admin endpoints beyond what sub-project 1 already shipped.
- Encryption-key rotation.
- Drive "Open with" entry flow (external entry to TMI from Drive UI).
- Retroactive upgrade of pre-existing Drive documents to delegated (users must delete and re-add).

## Follow-up issues (to be filed after spec approval)

1. **tmi-ux — Google Drive picker integration for delegated document attachments.** Adds the "Add from Google Drive" button, picker JS integration, and access-diagnostics UI rendering (localized per `reason_code`). Blocks on this sub-project landing.
2. **tmi — Upgrade Google Drive sources to OOXML export.** Updates both `GoogleDriveSource` and `DelegatedGoogleWorkspaceSource` to export Docs → DOCX, Slides → PPTX, Sheets → XLSX. Blocks on sub-project 5 landing.

## Tracking

This sub-project closes the Google-Workspace-delegated portion of #249. #249 stays open; the tracking comment on that issue will be updated to note completion of sub-project 4 and the dependencies between sub-projects.
