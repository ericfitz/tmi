# Confluence Delegated Content Provider

**Issue:** #285 — feat(timmy): Confluence delegated content provider (sub-project 2 of #249)
**Date:** 2026-04-25
**Status:** Draft
**Builds on:**
- #232 (content provider infrastructure + Google Drive)
- Sub-project 1 of #249 (delegated provider infrastructure: per-user OAuth tokens, encryption, account linking, `DelegatedSource` helper)
- Sub-project 4 of #249 (Google Workspace delegated picker — parallel reference, commit `906119e7`)

## Overview

Add a `confluence` delegated content source so users can paste Confluence Cloud page URLs into TMI documents and have Timmy index the page contents under the user's own identity. The infrastructure for delegated providers (token repository, encryption, OAuth callbacks, `DelegatedSource` helper, lazy refresh) is already in place; this sub-project plugs Confluence into it.

**Confluence is structurally simpler than the Google Workspace delegated source:** no picker UX, no per-document file_id metadata, no dual-source dispatch with a service-account fallback. URL pattern routing alone suffices. Most of the work is the OAuth provider quirks (Atlassian's `audience` parameter, the `accessible-resources` cloud-id discovery) and the page-content fetch.

## Decomposition

This is sub-project 2 of #249. It depends on sub-project 1 having landed (delegated provider infrastructure) and is independent of sub-projects 3 (OneDrive), 4 (Google Workspace, already done), and 5 (OOXML extractors).

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Page format | `body.view` (rendered HTML) | Already-rendered HTML is cleaner for `HTMLExtractor`; avoids parsing Confluence storage-format macros (`<ac:structured-macro>` etc.) |
| Cloud only | Yes (initial scope) | Issue scope explicitly excludes Server / Data Center; `*.atlassian.net/wiki/*` URL pattern only matches Cloud |
| URL forms supported | `/wiki/spaces/{SPACE}/pages/{id}/...` and `/wiki/spaces/{SPACE}/pages/{id}` | Modern page-id-bearing URL only; legacy `/display/` and short `/x/...` URLs return a clear `not supported` error rather than following redirects (out of scope) |
| `cloud_id` discovery | Per-fetch call to `accessible-resources`, match by URI host | Handles users with multiple linked Atlassian sites cleanly; the call is cheap and adds ≤1 hop on top of the lazy-refresh latency budget |
| `cloud_id` caching | None in this sub-project | Simplicity wins; future optimization can add a short-TTL Redis cache if profile data warrants it |
| Atlassian-specific OAuth params | Per-provider config field `extra_authorize_params` (map[string]string) on `ContentOAuthProviderConfig`, applied by `BaseContentOAuthProvider.AuthorizationURL` | Confluence requires `audience=api.atlassian.com`. Generalizing via config keeps `BaseContentOAuthProvider` reusable for future providers (e.g., Salesforce) without forking. |
| Refresh token | Operator must include `offline_access` in `required_scopes` | Atlassian only issues refresh tokens when this scope is granted; no refresh = `failed_refresh` after expiry. Documented in operator config notes and validated at startup with a warning. |
| Revocation URL | Empty (Atlassian 3LO has no public RFC 7009 endpoint) | DELETE `/me/content_tokens/confluence` deletes the row; user must disconnect the app from Atlassian's "Connected apps" UI to revoke at provider |
| Account label | `provider_account_label` = first accessible-resource site URL (e.g. `https://acme.atlassian.net`) | More useful than `email` from `/me` for multi-site users; identifies which Atlassian instance is linked |
| `RequestAccess` semantics | Log-only (no API call) | Confluence has no programmatic access-request equivalent for an external user. Page access is governed by space permissions managed by Confluence admins; we surface guidance via `access_diagnostics` instead. |
| 429 handling | Treat as `ErrTransient`, propagate; document remains in prior `access_status` | Built-in poller will retry naturally on its 5-minute cycle. No client-side backoff loop in this sub-project. |
| HTML extractor | Reuse existing `HTMLExtractor` | View-format HTML is well-formed; existing extractor already handles it |

## Architecture

### Components

**New:**

1. `api/content_source_confluence.go` — `DelegatedConfluenceSource` (mirrors `DelegatedGoogleWorkspaceSource` shape; embeds `*DelegatedSource`).
2. `api/content_source_confluence_test.go` — unit tests with `httptest.Server` mocks for Atlassian APIs.
3. `internal/config/confluence_config.go` — operator-side enable flag.
4. Optional `extra_authorize_params` field on `ContentOAuthProviderConfig` (small additive change to `internal/config/content_oauth.go` and `BaseContentOAuthProvider.AuthorizationURL`). Used by Confluence to pass `audience=api.atlassian.com` and `prompt=consent`.
5. `api/confluence_delegated_integration_test.go` — integration test mirroring `google_workspace_delegated_integration_test.go` shape.
6. Wiring in `cmd/server/main.go` to register the source when enabled.

**Reused (no changes):**

- `URLPatternMatcher` — already returns `confluence` for `*.atlassian.net/wiki/*`.
- `DelegatedSource`, `ContentTokenRepository`, `ContentOAuthProviderRegistry`, `BaseContentOAuthProvider`.
- `/me/content_tokens/*` and `/oauth2/content_callback` HTTP handlers (provider-id is dynamic).
- `HTMLExtractor`.
- `AccessPoller` (uses URL-pattern dispatch; will pick up the source automatically once registered).

### Non-goals

- Confluence Server / Data Center (separate auth model; out of scope).
- Atlassian-wide content other than Confluence pages (no Jira issues, no Confluence blog posts beyond what shares the `/pages/{id}/` path).
- Picker / attach UX (users paste page URLs).
- Storage-format extraction (we explicitly use view format).
- Multi-site selection UI; if a user has multiple linked sites, the URI host disambiguates at fetch time.
- `cloud_id` caching beyond the duration of a single fetch.
- Background refresh worker (still lazy refresh as established in sub-project 1).
- DOCX / PPTX extractors (sub-project 5).

### Definition of done

An integration test, against a running server with a stub Atlassian provider, exercises:

1. `POST /me/content_tokens/confluence/authorize` with a `client_callback` URL → returns an authorization URL pointing at the stub auth endpoint, with `audience=api.atlassian.com`, PKCE S256, and the configured scopes.
2. Stub authorize endpoint redirects to `/oauth2/content_callback` with code+state.
3. Stub token endpoint exchanges the code; row appears in `user_content_tokens` with encrypted access+refresh tokens, `provider_account_id` populated from `/me`, `provider_account_label` set to the matched site URL.
4. `POST /threat_models/{id}/documents` with a Confluence page URL flows through `ValidateAccess` → `accessible` (or `pending_access` / `auth_required` per token state).
5. `Fetch` against the document returns the page's view-format HTML (content-type `text/html`); `HTMLExtractor` produces plain text.
6. Expired access token triggers a refresh against the stub token endpoint; `last_refresh_at` updated, status remains `active`.
7. Refresh permanent failure (stub returns 400) flips status to `failed_refresh`; subsequent fetch returns `ErrAuthRequired` without further provider calls.
8. `DELETE /me/content_tokens/confluence` removes the row (revocation_url empty → no provider call, just local delete).
9. Multi-site user: stub `accessible-resources` returns two sites; fetch routes to the site matching the URI host.

## Data Model

No schema changes. Reuses `user_content_tokens` from sub-project 1.

`provider_account_id` stores the Atlassian `account_id` from `/me`. `provider_account_label` stores the first accessible site URL at link time (chosen because it identifies which Atlassian instance(s) the user is linked to; falls back to `email` from `/me` if `accessible-resources` returns no entries).

## URL parsing

Page id is extracted from the path component matching:

```
/wiki/spaces/{SPACE}/pages/{id}/...
/wiki/spaces/{SPACE}/pages/{id}
```

Other forms (`/wiki/display/...`, `/wiki/x/...`, REST URLs, draft URLs) are rejected at `Fetch` time with a clear error. Future expansion can resolve `/x/...` short links and `/display/` titles via additional API calls if user demand justifies it.

## Atlassian OAuth and API specifics

### Authorize URL

Confluence Cloud OAuth 2.0 (3LO) requires `audience=api.atlassian.com` on the authorize URL. Atlassian also recommends `prompt=consent` for predictable consent screens.

We add an optional `ExtraAuthorizeParams map[string]string` field to `ContentOAuthProviderConfig`. `BaseContentOAuthProvider.AuthorizationURL` applies these as additional query params. For Confluence the operator config is:

```yaml
content_oauth:
  providers:
    confluence:
      enabled: true
      client_id: "..."
      client_secret: "..."
      auth_url: "https://auth.atlassian.com/authorize"
      token_url: "https://auth.atlassian.com/oauth/token"
      userinfo_url: "https://api.atlassian.com/me"
      revocation_url: ""
      required_scopes:
        - "read:confluence-content.all"
        - "offline_access"
      extra_authorize_params:
        audience: "api.atlassian.com"
        prompt: "consent"
```

Env vars follow the existing pattern; `extra_authorize_params` is yaml-only at first (env var support deferred unless needed — operator-defined OAuth registrations are normally yaml/secret-store-managed anyway).

### Token endpoint

Standard `grant_type=authorization_code` and `grant_type=refresh_token` exchange. `BaseContentOAuthProvider.ExchangeCode` and `Refresh` work without modification.

### Userinfo

`GET https://api.atlassian.com/me` returns `{ account_id, name, email, ... }`. `BaseContentOAuthProvider.FetchAccountInfo` already prefers `sub`, `id`, `account_id` for the id field and `email`, `username`, `name` for the label, so the existing path produces a usable result. The Confluence source overrides `FetchAccountInfo` (or post-processes after callback) only to upgrade the label to the matched accessible-resources site URL when available.

The cleanest seam: `DelegatedConfluenceSource` does not need to override anything at link time — the existing callback handler will populate `provider_account_id` and `provider_account_label` from `/me`. We then **augment** the label by calling `accessible-resources` once at link time and writing the site URL into `provider_account_label`. To keep this non-invasive, the augmentation is performed in a small post-link hook registered on the OAuth callback path (same place where the existing `FetchAccountInfo` runs).

Alternative considered: skip the augmentation entirely and let `provider_account_label` remain the Atlassian email. Trade-off: simpler but less useful in the multi-site case. Choice: **do the augmentation** because it's one extra API call at link time and meaningfully improves the user-visible label.

### accessible-resources

`GET https://api.atlassian.com/oauth/token/accessible-resources` with the bearer token returns:

```json
[
  {
    "id": "11223344-...-...",
    "name": "Acme",
    "url": "https://acme.atlassian.net",
    "scopes": [...],
    "avatarUrl": "..."
  }
]
```

At fetch time, `DoFetch`:

1. Extracts the URI host (e.g. `acme.atlassian.net`).
2. Calls `accessible-resources` with the bearer token.
3. Picks the resource whose `url` host matches the URI host.
4. If no match → returns an error (the user has no access to that site through this token).
5. Uses the resource's `id` as `cloud_id` for the content fetch.

### Page content fetch

```
GET https://api.atlassian.com/ex/confluence/{cloud_id}/wiki/api/v2/pages/{id}?body-format=view
Authorization: Bearer {access_token}
Accept: application/json
```

Response (Confluence Cloud REST v2):

```json
{
  "id": "12345",
  "title": "Page Title",
  "spaceId": "...",
  "body": {
    "view": {
      "representation": "view",
      "value": "<p>Rendered HTML...</p>"
    }
  }
}
```

`DoFetch` returns `body.view.value` as bytes with content-type `text/html`. The `HTMLExtractor` consumes this transparently.

### ValidateAccess

Mirrors the Google Workspace pattern. A per-call probe `DelegatedSource` is constructed so concurrent `ValidateAccess` calls don't race on the source's `DoFetch`. The probe issues:

```
GET https://api.atlassian.com/ex/confluence/{cloud_id}/wiki/api/v2/pages/{id}
```

(no `body-format` parameter — we want metadata only). 200 → reachable. 4xx → not accessible (no error). 5xx / network → `ErrTransient`. No token → `ErrAuthRequired`.

### RequestAccess

Logs an actionable message and returns nil. The user-facing remediation surfaces via `access_diagnostics` ("This page is not accessible. Re-link your Confluence account or ask the page owner to grant view access."). No API call is made — Confluence has no equivalent to Google Drive's access-request emails for arbitrary pages.

### 429 / rate limiting

Atlassian returns 429 with `Retry-After`. The HTTP error is propagated to the pipeline as a generic Fetch failure. The document retains its prior `access_status`. The poller, when relevant, retries on its 5-minute cycle. No retry / backoff loop is added in this sub-project; if observability shows this is a frequent issue we can revisit.

## Configuration

### Operator yaml

```yaml
content_sources:
  confluence:
    enabled: true

content_oauth:
  providers:
    confluence:
      enabled: true
      client_id: "..."
      client_secret: "..."
      auth_url: "https://auth.atlassian.com/authorize"
      token_url: "https://auth.atlassian.com/oauth/token"
      userinfo_url: "https://api.atlassian.com/me"
      revocation_url: ""
      required_scopes:
        - "read:confluence-content.all"
        - "offline_access"
      extra_authorize_params:
        audience: "api.atlassian.com"
        prompt: "consent"
```

### Env vars

```
TMI_CONTENT_SOURCE_CONFLUENCE_ENABLED=true
TMI_CONTENT_OAUTH_PROVIDERS_CONFLUENCE_ENABLED=true
TMI_CONTENT_OAUTH_PROVIDERS_CONFLUENCE_CLIENT_ID=...
TMI_CONTENT_OAUTH_PROVIDERS_CONFLUENCE_CLIENT_SECRET=...
TMI_CONTENT_OAUTH_PROVIDERS_CONFLUENCE_AUTH_URL=https://auth.atlassian.com/authorize
TMI_CONTENT_OAUTH_PROVIDERS_CONFLUENCE_TOKEN_URL=https://auth.atlassian.com/oauth/token
TMI_CONTENT_OAUTH_PROVIDERS_CONFLUENCE_USERINFO_URL=https://api.atlassian.com/me
TMI_CONTENT_OAUTH_PROVIDERS_CONFLUENCE_REQUIRED_SCOPES=read:confluence-content.all offline_access
```

`extra_authorize_params` is yaml-only initially. (If this is a problem for Heroku-style env-var-only deploys we can add `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_EXTRA_AUTHORIZE_PARAMS` later as a comma-separated `k=v` list — additive change.)

### Startup validation

In `cmd/server/main.go`, mirroring the Google Workspace block:

```go
if cfg.ContentSources.Confluence.Enabled {
    if contentTokenRepo == nil || contentOAuthRegistry == nil {
        // refuse to start
    }
    if _, ok := contentOAuthRegistry.Get(api.ProviderConfluence); !ok {
        // refuse to start
    }
    src := api.NewDelegatedConfluenceSource(contentTokenRepo, contentOAuthRegistry)
    contentSources.Register(src)
    logger.Info("Content source enabled: confluence (delegated)")
}
```

Additionally, if Confluence is enabled but the OAuth provider's `required_scopes` does not contain `offline_access`, log a warning at startup ("refresh tokens will not be issued; users will need to re-link after access tokens expire"). Non-fatal.

## OpenAPI

No new endpoints. The existing `/me/content_tokens/{provider_id}/*` and `/oauth2/content_callback` operations accept any registered provider id; `confluence` becomes valid once the OAuth provider entry is enabled.

No schema changes.

## Pipeline registration order

`DelegatedConfluenceSource` is registered before `HTTPSource` (which matches all http/https URLs). `CanHandle` returns true only for hosts ending in `.atlassian.net` and paths under `/wiki/`. Because the URL pattern matcher already maps `*.atlassian.net/wiki/*` to `confluence`, both URL-based dispatch and the matcher-based 422 path are correct.

## Testing

### Unit tests (`api/content_source_confluence_test.go`)

- `extractConfluencePageID`: matrix of URL forms (modern, legacy, REST, malformed, non-Confluence) → expected page id or rejection.
- `CanHandle` matrix: Confluence URLs vs Jira URLs vs unrelated.
- `DoFetch` with `httptest.Server` mocking accessible-resources + page content endpoints:
  - Happy path → returns view HTML, content-type `text/html`.
  - URI host has no matching accessible resource → error.
  - Page id missing → error.
  - 404 from page endpoint → propagated.
  - 429 from page endpoint → propagated (no retry).
  - 5xx → propagated (becomes `ErrTransient` higher up).
- `ValidateAccess`: 200 / 404 / 5xx / no token in context.
- `RequestAccess`: returns nil, emits info log.
- Authorize URL augmentation: `BaseContentOAuthProvider.AuthorizationURL` includes `audience` and `prompt` from `extra_authorize_params`; PKCE + state still present and well-formed.

### Integration test (`api/confluence_delegated_integration_test.go`)

Stub Atlassian server (single `httptest.Server`) speaking:

- Authorization endpoint (302 redirect to callback with code+state).
- Token endpoint (`grant_type=authorization_code` + `grant_type=refresh_token`, supporting permanent/transient failure modes per test).
- `/me`.
- `accessible-resources` (configurable resource list).
- v2 page-content endpoint at `/ex/confluence/{cloud_id}/wiki/api/v2/pages/{id}`.

Test cases:

1. End-to-end authorize → callback → row stored → `ValidateAccess` accessible → `Fetch` returns view HTML.
2. Multi-site: two accessible resources, two URIs against different sites, both fetch correctly.
3. Refresh: stub access-token TTL is 1 second; fetch after expiry triggers refresh; `last_refresh_at` updated.
4. Permanent refresh failure: stub returns 400 on refresh; status flips to `failed_refresh`, subsequent fetch returns `ErrAuthRequired`.
5. Transient refresh failure: stub returns 502; row unchanged; subsequent fetch returns `ErrTransient`.
6. Revocation: `DELETE /me/content_tokens/confluence` removes the row; revocation endpoint not configured, no provider call.
7. URL parsing rejection: `/wiki/display/SPACE/Foo` returns a Fetch error without a provider call.

### CATS

`/me/content_tokens/{provider_id}/*` endpoints are already covered by sub-project 1's release-gate run. No new fuzzing surface in this sub-project.

## Implementation phases

1. **`extra_authorize_params` plumbing.** Add field to `ContentOAuthProviderConfig`; thread through `BaseContentOAuthProvider.AuthorizationURL`; unit test that custom params land in the URL alongside the standard ones; existing Google Workspace tests still pass.
2. **`internal/config/confluence_config.go` + `ContentSourcesConfig` field.** Mirror `GoogleWorkspaceConfig` minus picker keys.
3. **`api/content_source_confluence.go`.** `DelegatedConfluenceSource` implementing `ContentSource` + `AccessValidator` + `AccessRequester`. `extractConfluencePageID`, `extractConfluenceHost`. Unit tests with mocked Atlassian endpoints.
4. **Account-link augmentation** of `provider_account_label` with the matched site URL after the OAuth callback. This is a small post-`FetchAccountInfo` step on the Confluence-specific path — implement either as an override of `FetchAccountInfo` in a `ConfluenceContentOAuthProvider` wrapper, or inline in the callback handler keyed on `providerID == "confluence"`. Choose the wrapper path during code-time discovery — it keeps provider-specific knowledge out of the generic callback.
5. **Server wiring** in `cmd/server/main.go`. Register `DelegatedConfluenceSource` when `cfg.ContentSources.Confluence.Enabled` is true; reuse the same startup-validation pattern as Google Workspace.
6. **Integration test** (`api/confluence_delegated_integration_test.go`) covering the full flow, mirroring the Google Workspace integration test.
7. **Lint / build / unit / integration**, fix any issues.
8. **Operator documentation** (the GitHub wiki page for content providers gets a Confluence section).

Each phase is an independent commit candidate.

## Acceptance criteria (from issue #285)

- ✅ Provider registered behind operator config (enable + OAuth client id/secret + redirect URI).
- ✅ Account linking flow (`POST /me/content_tokens/confluence/authorize`, callback, `DELETE`) works end-to-end for Confluence.
- ✅ Per-user tokens stored encrypted; refresh works; un-link cascades correctly.
- ✅ Documents with Confluence URLs go through validate-access → fetch → extract pipeline.
- ✅ SSRF protection from `URIValidator` applies — note: delegated sources call hard-coded Atlassian API hosts; user-supplied URI host validation is enforced at URL-parse time (must end in `.atlassian.net`). General SSRF is not a concern for delegated providers because we never proxy arbitrary user URLs into untrusted networks.
- ✅ Unit tests with mocked Confluence HTTP responses.
- ✅ Integration test for the OAuth + fetch flow.
- ✅ OpenAPI spec updated if any provider-specific endpoints are added — none are added; the generic `/me/content_tokens/*` operations already cover Confluence.
- ✅ Design spec at `docs/superpowers/specs/2026-04-25-confluence-delegated-provider-design.md` (this file) written and reviewed before implementation.

## Out of scope (explicit)

- Confluence Server / Data Center.
- Jira, Bitbucket, Trello, or any non-Confluence Atlassian content.
- Picker-style attach UX.
- Storage-format extraction or macro rendering.
- `cloud_id` caching beyond the duration of a fetch.
- Background refresh worker.
- Retry/backoff on 429 (deferred until evidence justifies the complexity).
- Admin UI for provider configuration (tracked separately on tmi-ux).

## Tracking

Closes #285 when all acceptance criteria above are met. Parent #249 stays open; sub-projects 3 (OneDrive) and 5 (OOXML extractors) remain.
