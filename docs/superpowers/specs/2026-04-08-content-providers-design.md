# Content Providers: OAuth and External Document Access

**Issue:** #232 — feat(timmy): OAuth and additional content providers (Phases 2 & 3)
**Date:** 2026-04-08
**Status:** Draft

## Overview

Extend TMI's content extraction pipeline to support authenticated access to external document providers (Google Drive, Confluence, OneDrive, Google Workspace). This design separates content sourcing (authentication + fetching bytes) from content extraction (bytes to plain text), introduces two provider categories (service-account and user-delegated), and adds document access tracking so the server knows whether it can read a document before a Timmy session needs it.

**Scope of this spec:** Infrastructure refactor + Google Drive as the proof-of-concept service provider. Delegated providers (Confluence, Google Workspace) and additional service providers (OneDrive) are future work that builds on this infrastructure.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture | Distinct provider types (service vs. delegated) | Two access models are fundamentally different; no need for strategy abstraction with only 2 cases |
| Google Drive auth | Regular bot account (share-with-account) | Least privilege; no domain-wide delegation. A threat modeling tool must be an exemplar of minimal access. |
| Per-user token storage | New `user_content_tokens` database table | Refresh tokens are long-lived (days to indefinite); must survive Redis flushes and server restarts |
| Token encryption | Dedicated AES-256-GCM key (`TMI_CONTENT_TOKEN_ENCRYPTION_KEY`) | Separate security domain from settings encryption |
| Document access validation | Hybrid sync/async at creation time | Try synchronously first (happy path); fall back to async with `pending_access` status and programmatic access request |
| Unconfigured provider | Reject with 422 | Honest and actionable; avoids silent failures during chat sessions |
| Content pipeline | Separate Source and Extractor layers | A source (Google Drive) can return any content type; extraction logic should be independent of sourcing |
| Proof-of-concept provider | Google Workspace / Google Drive | Clean OAuth, simple export API, good docs; validates infrastructure without provider-specific complexity |

## Architecture

### Two-Layer Content Pipeline

The current `ContentProvider` interface conflates authentication/fetching with text extraction. This design separates them into two layers:

**Content Sources** — authenticate and fetch raw bytes:

```
ContentSource interface:
    Name() string
    CanHandle(ctx context.Context, uri string) bool
    Fetch(ctx context.Context, uri string) (bytes []byte, contentType string, err error)
```

Sources that support pre-validation (checking access without downloading) implement `AccessValidator`:

```
AccessValidator interface:
    ValidateAccess(ctx context.Context, uri string) (accessible bool, err error)
```

Service content providers that can programmatically request access additionally implement `AccessRequester`:

```
AccessRequester interface:
    RequestAccess(ctx context.Context, uri string) error
```

`HTTPSource` implements only `ContentSource` — no pre-validation or access requests (any public URL is fetched on demand; failures are handled at extraction time). `GoogleDriveSource` implements all three interfaces.

**Content Extractors** — convert bytes to plain text:

```
ContentExtractor interface:
    Name() string
    CanHandle(contentType string) bool
    Extract(data []byte, contentType string) (ExtractedContent, error)
```

**Pipeline:**

```
Document URI
  -> URL Pattern Matcher (identify provider)
  -> Source.Fetch(uri) -> (bytes, content-type)
  -> Content-Type -> Extractor.Extract(bytes) -> plain text
```

DB-resident providers (`DirectTextProvider`, `JSONContentProvider`) bypass this pipeline entirely — they read directly from the database and are unchanged.

### Provider Categories

**Service Content Providers** — operator configures credentials for a bot/service account. No per-user tokens. The user shares documents with the bot account.

- Google Drive (this spec)
- OneDrive/SharePoint (future)

**Delegated Content Providers** — require per-user OAuth tokens. The user links their account to TMI once, granting content-access scopes.

- Confluence (future)
- Google Workspace non-Drive content (future)

### URL Pattern Matcher

A lightweight, always-active registry that maps URL patterns to provider names. This is separate from the source registry and runs even for disabled providers, enabling clear 422 error messages.

| Pattern | Provider |
|---------|----------|
| `docs.google.com/*`, `drive.google.com/*` | `google_drive` |
| `*.atlassian.net/wiki/*` | `confluence` |
| `*.sharepoint.com/*`, `onedrive.live.com/*` | `onedrive` |
| Everything else with `http://` or `https://` | `http` |

### Source and Extractor Inventory

**Sources:**

| Source | Auth Model | Handles |
|--------|------------|---------|
| `HTTPSource` | None (SSRF-validated) | Any HTTP/HTTPS URL not claimed by a specific provider |
| `GoogleDriveSource` | Service account | `docs.google.com/*`, `drive.google.com/*` |
| *(future)* `ConfluenceSource` | User OAuth token | Confluence URLs |
| *(future)* `OneDriveSource` | Service account | SharePoint/OneDrive URLs |

**Extractors:**

| Extractor | Content-Types |
|-----------|---------------|
| `PlainTextExtractor` | `text/plain`, `text/csv` |
| `HTMLExtractor` | `text/html` |
| `PDFExtractor` | `application/pdf` |
| `JSONExtractor` | `application/json` |
| *(future)* `DOCXExtractor` | `application/vnd.openxmlformats-officedocument.wordprocessingml.document` |

### Refactoring Existing Providers

| Current | Becomes |
|---------|---------|
| `HTTPContentProvider` | `HTTPSource` + `HTMLExtractor` |
| `PDFContentProvider` | Reuses `HTTPSource` + `PDFExtractor` |
| `DirectTextProvider` | Unchanged (DB-resident, bypasses pipeline) |
| `JSONContentProvider` | Unchanged (DB-resident, bypasses pipeline) |

No behavior change for existing users — same URLs work the same way.

## Data Model Changes

### Document Model Additions

| Field | Type | Values | Notes |
|-------|------|--------|-------|
| `access_status` | varchar(32) | `"accessible"`, `"pending_access"`, `"auth_required"`, `"unknown"` | Default `"unknown"` for existing documents and plain HTTP URLs |
| `content_source` | varchar(64), nullable | `"google_drive"`, `"confluence"`, `"http"`, `null` | Detected from URL pattern at creation time |

### New Table: `user_content_tokens`

Per-user OAuth tokens for delegated content providers.

| Column | Type | Notes |
|--------|------|-------|
| `id` | varchar(36) | Primary key, UUID |
| `user_id` | varchar(36) | FK to users, indexed |
| `provider_id` | varchar(64) | e.g., `"confluence"`, indexed |
| `access_token` | text | AES-256-GCM encrypted |
| `refresh_token` | text | AES-256-GCM encrypted |
| `scopes` | text | Space-separated scopes granted |
| `expires_at` | timestamp | Access token expiry |
| `created_at` | timestamp | |
| `modified_at` | timestamp | |

Unique constraint on `(user_id, provider_id)` — one token set per user per provider.

## Google Drive Source (Proof-of-Concept)

### Operator Configuration

```yaml
content_sources:
  google_drive:
    enabled: true
    service_account_email: "tmiserver@mycompany.com"
    credentials_file: "/path/to/credentials.json"
```

| Environment Variable | Purpose |
|---------------------|---------|
| `TMI_CONTENT_SOURCE_GOOGLE_DRIVE_ENABLED` | Enable Google Drive source |
| `TMI_CONTENT_SOURCE_GOOGLE_DRIVE_SERVICE_ACCOUNT_EMAIL` | Bot account email shown to users |
| `TMI_CONTENT_SOURCE_GOOGLE_DRIVE_CREDENTIALS_FILE` | Path to Google credentials JSON |

### URL Handling

`CanHandle()` matches: `drive.google.com`, `docs.google.com/document`, `docs.google.com/spreadsheets`, `docs.google.com/presentation`

`Fetch()`:
- Extracts file ID from URL
- Google Docs -> export as `text/plain`
- Google Sheets -> export as `text/csv`
- Google Slides -> export as `text/plain`
- Binary files (uploaded PDFs, etc.) -> download raw bytes, return actual content-type

`ValidateAccess()`: Metadata-only read (cheaper than full download) to confirm the service account can see the file.

`RequestAccess()`: Google Drive API to create an access request, generating a "TMI Server wants to view this document" email to the document owner.

### Document Creation Flow

1. `POST /threat_models/{id}/documents` with `uri: "https://docs.google.com/document/d/abc123/edit"`
2. URL pattern matcher identifies: `google_drive`
3. Check source registry: is `GoogleDriveSource` registered (enabled + configured)?
   - **Not registered**: 422 — "Google Drive document access is not configured on this server. Contact your administrator."
4. Source implements `AccessValidator`, so call `ValidateAccess(uri)` — tries metadata read
   - **Can read**: `access_status: "accessible"`, `content_source: "google_drive"`, 201 Created
   - **Cannot read**: Source also implements `AccessRequester`, so call `RequestAccess(uri)` to send share request email -> `access_status: "pending_access"`, `content_source: "google_drive"`, 201 Created

For sources that don't implement `AccessValidator` (e.g., `HTTPSource`): `access_status: "unknown"`, validated at extraction time.

### Background Access Poller

- Configurable interval (default 5 minutes)
- Queries documents where `access_status = "pending_access"`
- Calls `ValidateAccess()` for each
- On success: updates to `"accessible"`
- Stops retrying after configurable window (default 7 days)

## Delegated Content Provider Infrastructure

> **Deferred to #249.** The delegated provider infrastructure (per-user token table, encryption, account linking endpoints) will be implemented alongside the first delegated provider (Confluence). The design below is retained for reference.

### Token Storage

Tokens are encrypted with AES-256-GCM using `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` (hex-encoded, 32-byte key). This is a separate key from settings encryption — different security domain.

If `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` is not set and any delegated provider is enabled, the server refuses to start.

### Token Lifecycle

1. User links account via OAuth consent flow (account linking endpoints)
2. On `Fetch()`, delegated source looks up user's token, refreshes if expired, fetches content
3. If no token exists: returns `ErrAuthRequired`
4. Document creation with a delegated-provider URL when user has no token: 422 with message indicating the user needs to link their account first
5. User can revoke via `DELETE /me/content_tokens/{provider_id}`

### Account Linking Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/me/content_tokens` | List linked content providers (no secrets exposed) |
| `POST` | `/me/content_tokens/{provider_id}/authorize` | Initiate OAuth consent, returns authorization URL |
| `GET` | `/oauth2/content_callback` | OAuth callback, stores encrypted token (`x-public-endpoint: true`) |
| `DELETE` | `/me/content_tokens/{provider_id}` | Revoke and delete linked account |

### Delegated Provider Configuration

```yaml
content_sources:
  confluence:
    enabled: false
    auth_provider_id: "confluence"
    required_scopes: ["read:confluence-content.all"]
```

| Environment Variable | Purpose |
|---------------------|---------|
| `TMI_CONTENT_SOURCE_CONFLUENCE_ENABLED` | Enable Confluence source |
| `TMI_CONTENT_SOURCE_CONFLUENCE_AUTH_PROVIDER_ID` | Which OAuth provider config to use |
| `TMI_CONTENT_SOURCE_CONFLUENCE_REQUIRED_SCOPES` | Scopes for content access |

## Timmy Session Integration

### Session Creation

During `snapshotSources()`, for each document with `timmy_enabled = true`:

| `access_status` | Behavior |
|-----------------|----------|
| `"accessible"` | Include in session, extract content normally |
| `"pending_access"` | Skip, report via `progress` SSE event |
| `"auth_required"` | Skip, report via `progress` SSE event |
| `"unknown"` (plain HTTP) | Attempt extraction as today; skip and report on failure |

The `session_created` SSE event includes a `skipped_sources` array listing what was skipped and why.

No new SSE event types needed. The document's `access_status` field (set at creation time) is the primary mechanism — not discovery at session time.

### Refresh Sources

New endpoint: `POST /threat_models/{id}/timmy/sessions/{session_id}/refresh_sources`

Re-runs `snapshotSources()` and re-indexes any newly-accessible documents into the existing session's vector index. Useful when a user grants access to a pending document after the session was created.

### Request Access (Re-request)

New endpoint: `POST /threat_models/{id}/documents/{document_id}/request_access`

Re-sends the access request for a `pending_access` document. Useful when the document owner missed or ignored the initial request email.

## OpenAPI Spec Changes

### New Schemas

| Schema | Fields |
|--------|--------|
| `SkippedSource` | `entity_id`, `name`, `reason` |

### Modified Schemas

| Schema | Change |
|--------|--------|
| `Document` | Add `access_status` (enum), `content_source` (string, nullable) |
| `TimmySession` | Add `skipped_sources` array to creation response |

### New Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/threat_models/{id}/timmy/sessions/{session_id}/refresh_sources` | Re-scan sources |
| `POST` | `/threat_models/{id}/documents/{document_id}/request_access` | Re-request document access |

### Deferred to #249

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/me/content_tokens` | List linked content providers |
| `POST` | `/me/content_tokens/{provider_id}/authorize` | Initiate OAuth consent |
| `DELETE` | `/me/content_tokens/{provider_id}` | Revoke linked account |
| `GET` | `/oauth2/content_callback` | OAuth callback for content providers |

New schemas deferred: `ContentTokenInfo`, `ContentAuthorizationURL`

## Startup Validation

- Source enabled but missing required config (e.g., no credentials file): log error, skip that source (don't crash)
- Log active sources at startup: `"Content sources enabled: google_drive, http"`
- *(Deferred to #249)* Delegated provider enabled without `TMI_CONTENT_TOKEN_ENCRYPTION_KEY`: refuse to start
- URL pattern matcher always active for all known providers (even disabled ones) to enable clear 422 errors

## Implementation Phases

1. **Source/Extractor refactor** — Split existing providers into Source + Extractor layers; introduce orchestrator. No behavior change.
2. **Document access tracking** — Add `access_status` and `content_source` fields to Document model; URL pattern matcher; creation-time detection; 422 for unconfigured providers.
3. **Google Drive source** — Operator config, service account auth, validate/request access, background poller.
4. **Timmy session integration** — Skip inaccessible documents, `skipped_sources` in session response, `refresh_sources` endpoint.
5. **OpenAPI spec** — New schemas, endpoints, modified schemas.

Each phase is independently deployable and testable.

## Deferred to #249

- **Delegated provider infrastructure** — `user_content_tokens` table, AES-256-GCM encryption, account linking endpoints
- Confluence provider (uses delegated infrastructure)
- OneDrive/SharePoint provider (uses service provider pattern from phase 3)
- Google Workspace delegated access (uses delegated infrastructure)
- `ContentTokenInfo` and `ContentAuthorizationURL` schemas
- Account linking endpoints (`/me/content_tokens/*`, `/oauth2/content_callback`)

## Out of Scope

- Admin UI for provider configuration (client issue to be filed against tmi-ux)
- DOCX/PPTX extractors (future, adds to extractor registry)
