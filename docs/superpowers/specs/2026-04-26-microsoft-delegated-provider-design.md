# Microsoft Delegated Content Provider for OneDrive-for-Business and SharePoint

**Issue:** #286
**Date:** 2026-04-26
**Status:** Draft
**Builds on:**
- #232 — content provider infrastructure + Google Drive (the service-mode reference)
- #258 / #261 — repository pattern + typed errors
- #285 — Confluence delegated content provider (the most recent delegated implementation; reuses the same `DelegatedSource` helper)

## Overview

Implement TMI's content provider for documents in **Microsoft OneDrive-for-Business and SharePoint Online** under a Microsoft Entra ID tenant. Runs in **delegated mode** with **per-file permission grants**, so the TMI app never has blanket read access to a tenant. Both required end-user experiences ship together:

- **Experience 1 (paste URL):** user supplies a SharePoint/OneDrive URL; if not yet permissioned, TMI returns a copy-pasteable Graph snippet the file owner runs to grant the TMI app per-file access.
- **Experience 2 (picker):** user clicks a picker button in tmi-ux; Microsoft File Picker v3 opens; user picks a file; TMI server-side mediates a `POST /drives/.../permissions` grant call on the user's behalf using their delegated `Files.ReadWrite` token; document becomes accessible immediately.

Personal Microsoft accounts (consumer OneDrive at `onedrive.live.com` / `1drv.ms`) are out of scope; tracked in a separate sibling issue.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Auth model | Delegated (per-user OAuth) | App-only `client_credentials` requires per-customer-tenant admin consent (#296 was closed for this reason); delegated keeps consent in the operator's own tenant only |
| Tenant model | Single-org self-hosted (one Entra app, one operator tenant) | Mirrors Google service-account-per-deployment model; SaaS-multi-tenant out of scope |
| Read scope | `Files.SelectedOperations.Selected` (delegated) | Zero blanket read access; per-file grants enable specific files. Closest Microsoft analog of Google's `drive.file` |
| Grant scope | `Files.ReadWrite` (delegated) | Required to call `POST /drives/.../permissions` from the picker-grant endpoint on the user's behalf. The operator's corp-security team consents to this once, in their own tenant |
| Refresh tokens | `offline_access` | Long-lived refresh; standard for delegated providers |
| Profile | `User.Read` | Account label/email for the linked-token row, parallel to Google Workspace and Confluence |
| Verified Publisher | Not required | Self-hosted operators are unlikely MPN-verified; admin consent in the operator's own tenant sidesteps the publisher-verification gate |
| Picker SDK | Microsoft File Picker v3 (hosted at `{tenant}.sharepoint.com/_layouts/15/FilePicker.aspx`) | Microsoft's unified picker for OneDrive + SharePoint; supersedes legacy OneDrive Picker SDK |
| Picker grant call | Server-mediated (TMI calls Graph from a server endpoint, not the browser) | Centralizes secrets, keeps the call auditable in TMI logs, doesn't ship the user's bearer token through additional client-side code paths |
| Provider name | `microsoft` | Forward-compatible (covers OneDrive-for-Business + SharePoint, possibly Outlook/Teams later); supersedes the placeholder `onedrive` constant currently in `URLPatternMatcher` |
| URL → drive item resolution | Graph `/shares/{share-id}/driveItem` (`u!{base64url(url)}` encoding) | Single endpoint that handles every SharePoint and OneDrive sharing-URL variant uniformly; no fragile path parsing |
| Picker metadata storage | Encode `(driveId, itemId)` as `driveId:itemId` in the existing `picker_file_id` column | No schema migration; matches the existing `PickerMetadata` shape |
| Tenant identification at fetch time | Parse host from URL (`contoso.sharepoint.com` → tenant `contoso`); use Graph `/v1.0/sites/{host}` only if needed for cross-site routing | The user's delegated token is already scoped to their home tenant; no operator-side tenant routing required |

## Architecture

### URL pattern matcher

`api/content_pipeline.go`'s `URLPatternMatcher` already routes:

- `*.sharepoint.com/*` → `ProviderOneDrive`
- `onedrive.live.com` → `ProviderOneDrive`

Update: rename `ProviderOneDrive` to `ProviderMicrosoft = "microsoft"` for forward-compatibility. `*.sharepoint.com` and `*-my.sharepoint.com` (per-user OneDrive-for-Business) both route to `microsoft`. `onedrive.live.com` and `1drv.ms` continue to route to a `microsoft_personal` value reserved for the future personal-account sibling sub-project; until that lands, those URLs hit the 422 "no provider configured" path the pipeline already handles.

### Components

**New:**

1. `api/content_oauth_provider_microsoft.go` — `MicrosoftContentOAuthProvider`. Optional thin wrapper over `BaseContentOAuthProvider` (only needed if Graph's `/me` userinfo response differs from what `BaseContentOAuthProvider.FetchAccountInfo` already understands; Graph returns `id` + `mail` + `displayName` which the base implementation handles correctly, so this wrapper may collapse to a no-op constructor that just uses the base directly).
2. `api/content_source_microsoft_graph.go` — `DelegatedMicrosoftSource`. Implements `ContentSource` + `AccessValidator` + `AccessRequester`. Embeds `*DelegatedSource`; provides Microsoft-specific `DoFetch` callback.
3. `api/microsoft_picker_grant_handler.go` — `POST /me/microsoft/picker_grants` endpoint. Takes `(driveId, itemId)`; uses the user's delegated `Files.ReadWrite` token to call `POST /drives/{driveId}/items/{itemId}/permissions` granting the TMI Entra app's identity `read` access to that file; returns the created permission ID (audit) and success/failure.
4. `internal/config` — extend with `MicrosoftConfig` block (Enabled, TenantID, ClientID, ClientSecret, ApplicationObjectID for picker-grant call payload).

**Updated:**

- `cmd/server/main.go` — register the OAuth provider and source; configure `PickerTokenConfig[ProviderMicrosoft]` with Microsoft-specific picker payload; wire the picker-grant handler.
- `api/picker_token_handler.go` — `PickerTokenConfig` and `pickerTokenResponse` need to carry provider-agnostic config. Today they hold Google-specific `developer_key` + `app_id`. Generalize to a `provider_config map[string]string` (or add Microsoft-specific fields) so the picker token response can carry whatever the browser needs to initialize the picker for that provider. **OpenAPI schema update required** — see below.
- `api-schema/tmi-openapi.json` — update `PickerTokenResponse` schema; add `POST /me/microsoft/picker_grants` operation.
- `api/api.go` — regenerated.

**Unchanged:**

- `ContentSource`, `AccessValidator`, `AccessRequester` interfaces.
- `DelegatedSource` (the shared helper).
- `ContentTokenRepository`, `user_content_tokens` table, encryption.
- Document model (`access_status`, `content_source`, `picker_*` fields).
- Background access poller.
- Pipeline orchestrator.

### `DelegatedMicrosoftSource`

```go
type DelegatedMicrosoftSource struct {
    Delegated *DelegatedSource

    // GraphBaseURL is "https://graph.microsoft.com/v1.0" by default;
    // overridable for tests.
    GraphBaseURL string

    // PickerConfig holds picker-token-endpoint-relevant fields (passed
    // through to the browser, not used by Fetch).
    PickerConfig MicrosoftPickerConfig
}

type MicrosoftPickerConfig struct {
    ClientID  string
    TenantID  string
}
```

`DoFetch` callback responsibilities:

1. If document has picker metadata: parse `driveId:itemId` from `picker_file_id`. Call `GET /drives/{driveId}/items/{itemId}` for metadata (`mimeType`, `name`, `size`); then `GET /drives/{driveId}/items/{itemId}/content` for bytes.
2. Otherwise (paste-URL flow): resolve URL to drive item via `GET /shares/{shareId}/driveItem` where `shareId = "u!" + base64url(url) (no padding)`. Then fetch as in step 1.
3. Apply `googleDriveMaxExportSize`-equivalent (10 MiB) limit via `io.LimitReader` on the body stream.
4. Return `(bytes, contentType, err)`.

`ValidateAccess` callback: same dispatch as `DoFetch`, but only the metadata `GET` (no content download). 4xx response → `(false, nil)`. 5xx → `(false, ErrTransient)`. Auth errors → propagate `ErrAuthRequired`.

`RequestAccess` (Experience 1's "no access yet" hook): logs an informational entry and returns nil. The actual user-facing remediation lives in the document's `access_diagnostics` payload (handler-level), surfacing the "share with TMI" snippet — see "Diagnostics payload" below.

### Picker-grant endpoint

`POST /me/microsoft/picker_grants`

Request:
```json
{ "drive_id": "...", "item_id": "..." }
```

Behavior:
1. Authenticate the caller via JWT middleware (existing); resolve user.
2. Look up linked Microsoft token for the user. Refuse with 401/`not_linked` if missing or in `failed_refresh`.
3. Refresh token if expired (reuse existing `pickerTokenExpired` / `refreshIfNeeded` patterns).
4. Get the TMI Entra app's object ID from config.
5. Call `POST /drives/{driveId}/items/{itemId}/permissions` to Graph with the user's bearer token:
   ```json
   {
     "roles": ["read"],
     "grantedToIdentities": [
       { "application": { "id": "<tmi-app-object-id>", "displayName": "TMI" } }
     ]
   }
   ```
6. On 2xx: return `{ "permission_id": "...", "drive_id": "...", "item_id": "..." }` (200).
7. On 4xx from Graph: surface the Graph error (sanitized) as 422 with reason code (`grant_failed`).
8. On 5xx from Graph: 503 with `transient_failure`.

Audit logging: every grant attempt logged with `user_id`, `drive_id`, `item_id`, success/failure, Graph status code. Operator can correlate against Graph audit logs.

### Picker token endpoint generalization

Existing endpoint: `POST /me/picker_tokens/{provider_id}`. Today returns `{access_token, expires_at, developer_key, app_id}` — Google-shaped.

Generalization: introduce a `provider_config` field carrying provider-specific picker initialization values. For Microsoft:
```json
{
  "access_token": "...",
  "expires_at": "...",
  "provider_config": {
    "client_id": "...",
    "tenant_id": "...",
    "redirect_uri": "...",
    "picker_origin": "https://contoso.sharepoint.com"
  }
}
```

For Google, current fields move into `provider_config` too:
```json
{
  "access_token": "...",
  "expires_at": "...",
  "provider_config": {
    "developer_key": "...",
    "app_id": "..."
  }
}
```

Backward-compat: keep the legacy `developer_key` and `app_id` top-level fields on the response for Google, populated alongside the new map, until tmi-ux migrates. Drop them in a follow-on cleanup.

### Two end-to-end flows

#### Experience 1 — paste URL

```
User                          tmi-ux                         TMI server                       Microsoft Graph
──┬─                          ──┬───                         ──┬───────                       ──┬──────────────
  │ paste SharePoint URL        │                              │                                 │
  ├────────────────────────────▶│                              │                                 │
  │                             │ POST /threat_models/.../    │                                 │
  │                             │   documents { uri, ... }     │                                 │
  │                             ├─────────────────────────────▶│                                 │
  │                             │                              │ ValidateAccess(uri)             │
  │                             │                              │   → resolve via /shares/{id}    │
  │                             │                              ├────────────────────────────────▶│
  │                             │                              │   ← 403 Forbidden               │
  │                             │                              │◀────────────────────────────────┤
  │                             │                              │ document.access_status =        │
  │                             │                              │   pending_access                │
  │                             │  201 Created + diagnostics   │                                 │
  │                             │◀─────────────────────────────┤                                 │
  │ display "share with TMI"    │                              │                                 │
  │   snippet from diagnostics  │                              │                                 │
  │◀────────────────────────────┤                              │                                 │
  │                                                                                              │
  │ runs Graph snippet to grant TMI app read on this file (uses user's own Files.ReadWrite)      │
  ├──────────────────────────────────────────────────────────────────────────────────────────────▶│
  │                                                                                              │
  │           [...background access poller re-checks every 5 min...]                              │
  │                             │                              │ ValidateAccess again            │
  │                             │                              ├────────────────────────────────▶│
  │                             │                              │   ← 200 OK                      │
  │                             │                              │◀────────────────────────────────┤
  │                             │                              │ document.access_status =        │
  │                             │                              │   accessible                    │
```

The "share with TMI" snippet is a one-line PowerShell or curl call:
```
POST https://graph.microsoft.com/v1.0/drives/{driveId}/items/{itemId}/permissions
Authorization: Bearer <user's own token>
{ "roles": ["read"],
  "grantedToIdentities": [{ "application": {"id": "<tmi-app-id>", "displayName": "TMI"} }] }
```

The diagnostics payload includes the `driveId`, `itemId` (resolved from the URL via `/shares/`), and the TMI app object ID, so the user can copy-paste with no manual parameter substitution.

#### Experience 2 — picker

```
User             tmi-ux                  TMI server               Microsoft Graph             SharePoint Picker
──┬──            ──┬───                  ──┬───────               ──┬──────────────             ──┬─────────────
  │ click          │                       │                         │                            │
  │ "browse"       │                       │                         │                            │
  ├───────────────▶│                       │                         │                            │
  │                │ POST /me/picker_tokens/microsoft                │                            │
  │                ├──────────────────────▶│                         │                            │
  │                │                       │ refresh user token      │                            │
  │                │                       ├────────────────────────▶│                            │
  │                │   {access_token, ...} │                         │                            │
  │                │◀──────────────────────┤                         │                            │
  │                │ embed picker iframe with token                                              │
  │                ├──────────────────────────────────────────────────────────────────────────────▶│
  │  pick file     │                                                                              │
  │ ──────────────▶│ ◀── postMessage with {driveId, itemId, url, mimeType, ...}                  │
  │                │                       │                         │                            │
  │                │ POST /me/microsoft/picker_grants                                             │
  │                │   {drive_id, item_id} │                         │                            │
  │                ├──────────────────────▶│                         │                            │
  │                │                       │ refresh user token      │                            │
  │                │                       ├────────────────────────▶│                            │
  │                │                       │ POST /drives/{driveId}/ │                            │
  │                │                       │   items/{itemId}/       │                            │
  │                │                       │   permissions           │                            │
  │                │                       ├────────────────────────▶│                            │
  │                │                       │   ← 201 created         │                            │
  │                │   {permission_id, ...}│                         │                            │
  │                │◀──────────────────────┤                         │                            │
  │                │ POST /threat_models/.../documents { uri, picker_*, ... }                    │
  │                ├──────────────────────▶│                         │                            │
  │                │                       │ ValidateAccess          │                            │
  │                │                       │   (succeeds — granted)  │                            │
  │                │                       ├────────────────────────▶│                            │
  │                │   201 Created         │                         │                            │
  │                │   accessible          │                         │                            │
  │                │◀──────────────────────┤                         │                            │
```

### Diagnostics payload extension

Existing `DocumentAccessDiagnostics` struct (`api/document_diagnostics.go` if it exists, else `document_sub_resource_handlers.go`) already returns a structured `reason_code` + `remediations[]` shape. Add Microsoft-specific reason codes:

- `microsoft_share_with_app` — file owner needs to grant the TMI app per-file read access. Remediation includes the Graph API call payload and human instructions.
- `microsoft_user_not_linked` — user has no linked Microsoft token. Remediation links to the account-link UI.
- `microsoft_token_failed` — refresh token failed; user needs to re-link.

### Picker integration with tmi-ux

Out-of-band (not in this server-side issue), but documented here for coordination:

- tmi-ux loads the Microsoft File Picker SDK script from `https://res-1.cdn.office.net/files/odsp-web-prod_*.js` (URL pattern; current Microsoft CDN).
- Picker iframe origin: `{tenant}.sharepoint.com`. The picker sends postMessage events; tmi-ux registers handlers for `command` events including `pick`.
- Token-passing: tmi-ux receives `{access_token, provider_config}` from `POST /me/picker_tokens/microsoft`; passes `access_token` into the picker config plus `provider_config.client_id`, `provider_config.tenant_id`, `provider_config.picker_origin`.
- On pick, tmi-ux POSTs to `/me/microsoft/picker_grants` with `{drive_id, item_id}`, then to `/threat_models/{id}/documents` with full document payload including picker fields.

The tmi-ux developer is on call to coordinate iframe details, postMessage protocol, and SDK version selection.

## Data model

No schema changes required. Reuse existing fields:

- `user_content_tokens` row with `provider_id='microsoft'`. Encryption, scopes, expiry handled by the existing repository.
- Document row picker fields: `picker_provider_id='microsoft'`, `picker_file_id='{driveId}:{itemId}'`, `picker_mime_type='<mime>'`.

Helper functions in `api/content_source_microsoft_graph.go`:

```go
func encodeMicrosoftPickerFileID(driveID, itemID string) string
func decodeMicrosoftPickerFileID(s string) (driveID, itemID string, ok bool)
```

## Configuration

```yaml
content_oauth:
  providers:
    microsoft:
      enabled: true
      client_id: "..."
      client_secret: "..."
      tenant_id: "common"  # or operator's tenant GUID for single-tenant restriction
      auth_url: "https://login.microsoftonline.com/{tenant_id}/oauth2/v2.0/authorize"
      token_url: "https://login.microsoftonline.com/{tenant_id}/oauth2/v2.0/token"
      revocation_url: ""  # Microsoft Graph has no public RFC 7009 revocation endpoint
      userinfo_url: "https://graph.microsoft.com/v1.0/me"
      required_scopes:
        - "Files.SelectedOperations.Selected"
        - "Files.ReadWrite"
        - "offline_access"
        - "User.Read"

content_sources:
  microsoft:
    enabled: true
    tenant_id: "..."             # operator's Entra tenant GUID
    application_object_id: "..." # for picker-grant call's grantedToIdentities.application.id
    picker_origin: "https://contoso.sharepoint.com"  # for picker iframe
```

`tenant_id` placeholder: when `tenant_id` is `"common"`, the OAuth endpoints accept work-or-school accounts from any Entra tenant. When set to a specific tenant GUID, OAuth is restricted to that one tenant. For single-org deployments the operator typically pins to their own tenant GUID; `"common"` is the alternative for organizations whose users span multiple Entra tenants.

Environment variable equivalents follow the existing `TMI_*` naming convention.

## Operator setup (one-time)

1. **Register Entra app** in operator's tenant:
   - Type: Web application (single-tenant or multi-tenant per operator's choice)
   - Redirect URI: `https://{tmi-host}/oauth2/content_callback`
   - API permissions (delegated, Microsoft Graph):
     - `Files.SelectedOperations.Selected`
     - `Files.ReadWrite`
     - `offline_access`
     - `User.Read`
   - Click "Grant admin consent for {tenant}" (corp-security-team action)
   - Generate a client secret; record it
   - Record the application's Object ID (not the App ID — needed for `grantedToIdentities.application.id` in the picker-grant call)

2. **Configure TMI server** with `client_id`, `client_secret`, `tenant_id`, `application_object_id`.

3. **Restart TMI server.** Startup validation refuses to start if `content_oauth.providers.microsoft.enabled=true` and `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` is unset (existing infrastructure check).

Total operator-side cost: ~10 minutes; one click of admin-consent in Entra portal.

## Security considerations

- **Token encryption.** All stored access and refresh tokens use the existing `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` AES-256-GCM scheme.
- **Scope minimization.** Read scope is `Files.SelectedOperations.Selected` (zero default access). The broader `Files.ReadWrite` is requested only because Microsoft has no narrower scope that allows a user to grant another app per-file access.
- **Per-file grant audit.** Every picker-grant call is logged with user/file/grant ID. Microsoft Graph also retains its own audit log of permission changes, so corp security has dual visibility.
- **Tenant isolation.** The user's delegated token is scoped to their home tenant; cross-tenant data never appears in TMI.
- **SSRF.** All Graph URL constructions go through the existing `URIValidator` (Timmy SSRF policy from #232) before being fetched.
- **Permission revocation.** When a user unlinks their Microsoft account, existing user-delete cascade revokes the token at the provider where possible (Microsoft has no public RFC 7009 revoke endpoint, but the `Files.SelectedOperations.Selected` scope means revoking the user's session naturally invalidates the picker-mediated grants).
- **Token expiry & refresh.** Existing `DelegatedSource` handles lazy refresh, `failed_refresh` status transitions, concurrent-refresh serialization.
- **Picker-grant idempotency.** Re-granting the same app to the same file is a no-op at Graph (idempotent on the permission set). Safe for retries.

## Testing plan

### Unit tests

- `api/content_source_microsoft_graph_test.go`:
  - URL → shareId encoding (`encodeMicrosoftShareID`).
  - Picker file ID encode/decode round-trips.
  - `CanHandle` matrix (sharepoint.com, *-my.sharepoint.com, sharepoint subdomains, non-Microsoft URLs).
  - `Name` returns `"microsoft"`.
- `api/microsoft_picker_grant_handler_test.go`:
  - Authenticated path: refresh-as-needed, Graph 2xx, returns permission ID.
  - Auth required: no token, returns 401.
  - Failed refresh: returns 401.
  - Graph 4xx: returns 422 with sanitized reason.
  - Graph 5xx: returns 503.
- Mocked Graph HTTP using `httptest.Server`.

### Integration tests

- `api/microsoft_delegated_integration_test.go` (parallel to `google_workspace_delegated_integration_test.go`):
  - End-to-end OAuth-link → picker-grant → fetch flow against a stub Microsoft Graph (mock server) and a stub OAuth provider.
  - Cover both Experience 1 (paste URL → pending → grant → poll → accessible → fetch) and Experience 2 (link → picker-token → grant-handler → fetch).
- Reuse the `MockDelegatedSource` infrastructure where possible.

### Negative tests

- Token refresh failure (4xx → permanent → `failed_refresh` status).
- Token refresh transient (5xx → `ErrTransient` → caller retries).
- Graph rate-limit (429) → propagate as transient.
- Picker-grant when user lacks `Files.ReadWrite` (provider returned narrower scope set than requested) → 422 with `insufficient_scope`.
- URL pointing to a file not shared with the user (validate returns false, no token error) → `pending_access`.

### CATS API fuzzing

- New picker-grant endpoint added to fuzz target list.
- Check for 500-error path; mark `is_oauth_false_positive` for legitimate 401/403 cases.

## Out of scope

- Personal Microsoft accounts (consumer OneDrive, `1drv.ms`, `onedrive.live.com`). Sibling tracking issue to be filed.
- Multi-customer-tenant SaaS-style operation (cross-tenant in a single deployment).
- Editing or write access to documents — read-only fetch (despite the `Files.ReadWrite` scope, which is used solely for the per-file grant call).
- Microsoft Teams / Stream / OneNote / Outlook content.
- OOXML extractors (DOCX/PPTX) — separate sub-project per #287; this provider works with whatever extractors are registered.
- Microsoft Verified Publisher status — neither required nor pursued.
- SharePoint embedded sites or Microsoft Lists content.

## Open questions / future work

1. **Picker SDK version pinning.** Microsoft updates File Picker URL endpoints periodically. tmi-ux should isolate the SDK URL behind a config value the operator can override if Microsoft changes endpoints.
2. **Cross-tenant guest access.** A user in tenant A linking their account, then trying to access a SharePoint URL in tenant B where they're a B2B guest. Should work (the user's token can act as them in B2B contexts) but needs verification.
3. **Tenant `"common"` vs pinned.** Default config recommendation: pin to the operator's tenant for tightest scope. `"common"` is the escape hatch for orgs with users spanning multiple Entra tenants.
4. **Personal-account sibling.** Once this provider is shipped, the personal-account variant is mostly: same Entra app registered with `signInAudience: PersonalMicrosoftAccount`, OAuth endpoints under `login.live.com`, URL pattern matcher routes for `onedrive.live.com` / `1drv.ms`. Estimated half the size of this issue.

## Decision log (carry-over from brainstorm)

| Question | Decision |
|---|---|
| Service mode (#296) vs delegated (#286) | Delegated; #296 closed as superseded |
| Picker UX path (Path 1, 2, 3) | Path 1 — picker-mediated grant via TMI server endpoint |
| Verified Microsoft Publisher | Not required (admin consent in operator's own tenant) |
| `Files.ReadWrite` trust ask | Accepted — required for the picker-grant call; Microsoft has no narrower scope |
| Server-mediated vs client-side grant | Server-mediated |
| Personal Microsoft accounts | Out of scope; sibling issue |
| Provider name | `microsoft` (not `onedrive`, not `microsoft_graph`) |
