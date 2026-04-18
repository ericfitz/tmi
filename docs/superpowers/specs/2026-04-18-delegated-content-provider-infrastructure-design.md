# Delegated Content Provider Infrastructure

**Issue:** #249 — feat(timmy): Confluence and OneDrive content providers (sub-project 1 of 5)
**Date:** 2026-04-18
**Status:** Approved
**Builds on:** #232 (content provider infrastructure + Google Drive)

## Overview

This spec covers the first sub-project of #249: the per-user OAuth token infrastructure needed by any *delegated* content provider (Confluence, Google Workspace per-user access, etc.). It ships without a real delegated provider — a build-tagged `MockDelegatedSource` is included so the full authorize → callback → fetch → refresh → revoke flow is exercised end-to-end before Confluence lands.

The content pipeline from #232 already separates sources (auth + fetch bytes) from extractors (bytes → text) and already distinguishes service providers (operator credentials, used by Google Drive) from delegated providers (per-user tokens). This sub-project builds the delegated side of that distinction.

## Decomposition of #249

#249 bundles six workstreams. They are being tracked as independent sub-projects:

| Order | Sub-project | Notes |
|-------|-------------|-------|
| 1 | **Delegated provider infrastructure** (this spec) | Unblocks #2 and #4 |
| 2 | Confluence provider (delegated) | First real consumer |
| 3 | OneDrive / SharePoint provider (service) | Reuses #232 service-provider pattern; independent of this spec |
| 4 | Google Workspace delegated access | Extends existing Google Drive source with per-user OAuth |
| 5 | OOXML extractors (DOCX + PPTX) | Content-type-scoped; usable by any source. Independent of this spec. |

#249 stays open as the tracking issue until all five land.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| User-auth OAuth configs vs content-access OAuth configs | Separate (new `content_oauth` config section) | Consumer-login apps and workspace/content-scope apps are frequently distinct OAuth client registrations; separation prevents accidental scope bleed |
| Token refresh | Lazy, on Fetch | One code path for refresh success/failure; no background worker; ~100–300 ms first-fetch-after-sleep latency is tolerable for a document-indexing pipeline |
| Callback completion UX | Client-callback 302 with `status=success` or `status=error` | Matches the existing TMI user-auth OAuth flow; testable via oauth-client-callback-stub |
| Revocation on DELETE | Provider revocation (RFC 7009 where supported) + local delete; best-effort on provider failure | "Disconnect" must actually disconnect at the provider; outage cannot block local cleanup |
| Admin endpoints & user-delete cascade | Admin list + revoke endpoints; user-delete sweeps revocations before FK cascade | Off-boarding with dangling grants is a real security concern |
| OAuth state storage | Redis, 10-minute TTL | Ephemeral, TTL-native, no schema churn, already deployed |
| PKCE | S256 always | RFC 7636; avoids reliance on client secret alone for code-exchange leg |
| Concurrent refresh serialization | `SELECT ... FOR UPDATE` on the token row | Correct across replicas; minor latency cost |
| Token encryption key | Dedicated `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` (AES-256-GCM) | Separate security domain from settings encryption; startup refuses without it when any delegated provider is enabled |
| Mock delegated source | Build-tagged (`dev`/`test`) `MockDelegatedSource` + stub OAuth provider in integration tests | Enables full-flow integration tests without coupling this sub-project to Confluence |

## Architecture

### Components

**New:**

1. `user_content_tokens` table — encrypted per-user OAuth token storage.
2. `ContentTokenRepository` — repository abstraction with typed errors (pattern from #258/#261).
3. `ContentOAuthProviderRegistry` + `content_oauth` config section — parallel to `auth.ProviderRegistry` / `oauth` config, scoped to content access.
4. Account-linking HTTP handlers — `/me/content_tokens/*`, `/oauth2/content_callback`, `/admin/users/{user_id}/content_tokens/*`.
5. `DelegatedSource` helper — embedded by concrete delegated sources; handles token lookup, lazy refresh, status updates.
6. `MockDelegatedSource` (build-tagged) — test-only source used by integration tests.
7. Startup validation — refuse to start if any delegated provider is enabled without `TMI_CONTENT_TOKEN_ENCRYPTION_KEY`.
8. User-delete cascade hook — best-effort revocation sweep before existing FK cascade deletes rows.

**Unchanged:**

- `ContentSource`, `AccessValidator`, `AccessRequester` interfaces from #232.
- Service providers (Google Drive).
- Document model (`access_status`, `content_source` fields from #232 are reused unchanged).
- Timmy session integration (`SkippedSource`, session-created SSE payload from #232 are reused unchanged).

### Non-goals

- No real delegated provider in this sub-project (Confluence, Google Workspace delegated, OneDrive are separate sub-projects).
- No changes to service-provider code paths.
- No DOCX/PPTX extractor work.
- No Admin UI (a tmi-ux issue will be filed separately once the API is in place).
- No dynamic / incremental scope upgrades — scope changes require re-linking.
- No background refresh worker — lazy refresh only.
- No shared/multi-user tokens (each link is `(user_id, provider_id)` unique).

### Definition of done

An integration test can, against a running server:

1. Call `POST /me/content_tokens/mock/authorize` with a `client_callback` URL → receive an authorization URL pointing at a stub provider.
2. Drive the provider's authorization response to `/oauth2/content_callback` with a code.
3. End up with an encrypted token row in `user_content_tokens`.
4. Issue a `MockDelegatedSource.Fetch(...)` through the pipeline.
5. Observe a refresh when the stored access token is expired, with `last_refresh_at` and `status='active'` updated.
6. Call `DELETE /me/content_tokens/mock` → confirm the row is gone **and** the stub provider saw a revocation call.

## Data Model

### New table: `user_content_tokens`

| Column | Type | Notes |
|--------|------|-------|
| `id` | `varchar(36)` | PK, UUID |
| `user_id` | `varchar(36)` | FK → `users.id`, `ON DELETE CASCADE`, indexed |
| `provider_id` | `varchar(64)` | e.g. `"confluence"`, `"google_workspace"`, `"mock"` |
| `access_token` | `bytea` | AES-256-GCM ciphertext (nonce prepended) |
| `refresh_token` | `bytea` | AES-256-GCM ciphertext; nullable |
| `scopes` | `text` | Space-separated scopes actually granted |
| `expires_at` | `timestamptz` | Access-token expiry; nullable |
| `status` | `varchar(16)` | `'active'` \| `'failed_refresh'`; default `'active'` |
| `last_refresh_at` | `timestamptz` | Set on successful refresh; null until first refresh |
| `last_error` | `text` | Most recent refresh failure reason (truncated to 1024 chars); cleared on success |
| `provider_account_id` | `varchar(255)` | External account identifier (e.g. Atlassian account id); nullable |
| `provider_account_label` | `varchar(255)` | Human-readable label (email / username); shown in list responses |
| `created_at` | `timestamptz` | |
| `modified_at` | `timestamptz` | |

**Status note:** `revoked` was considered and dropped; revocation deletes the row rather than marking it, so the status field only needs to distinguish active tokens from tokens whose refresh failed at the provider.

### Constraints & indexes

- `UNIQUE (user_id, provider_id)` — one token per user per provider.
- Secondary index on `(status, expires_at)` — cheap scans for ops/alerting (no consumer in this sub-project, but essentially free).
- FK with `ON DELETE CASCADE` from `user_id` — row removal runs *after* the cascade hook performs revocations (see User-delete cascade below).

### Migration

New migration file under `auth/migrations/` following the existing numbered pattern.

### Encryption

- **Key:** `TMI_CONTENT_TOKEN_ENCRYPTION_KEY`, 32-byte hex-encoded (64 chars), read at startup.
- **Algorithm:** AES-256-GCM, 96-bit random nonce per encryption, nonce prepended to ciphertext (matches the settings-encryption idiom in the codebase).
- **Startup rule:** If *any* entry under `content_oauth.providers.*` has `enabled: true` and the key is missing or malformed, the server refuses to start with a clear config-validation error.
- **Key rotation:** Out of scope for this sub-project. When needed later, a versioned-nonce scheme can be added without schema change.
- **Separate from settings encryption** — different security domain.

### `ContentTokenRepository`

Typed errors via `internal/dberrors`. Plaintext tokens never leave the repository layer — callers get a `DecryptedContentToken` value struct from a dedicated decrypt step.

```go
type ContentTokenRepository interface {
    GetByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error)
    ListByUser(ctx context.Context, userID string) ([]ContentToken, error)
    Upsert(ctx context.Context, token *ContentToken) error
    UpdateStatus(ctx context.Context, id, status, lastError string) error
    Delete(ctx context.Context, id string) error
    DeleteByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error)
    // RefreshWithLock locks the row via SELECT ... FOR UPDATE, re-checks expiry,
    // and invokes the callback to mint a new token. See Fetch/Refresh flow.
    RefreshWithLock(ctx context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error)
}
```

### API schemas (no secrets exposed)

| Schema | Fields |
|--------|--------|
| `ContentTokenInfo` | `provider_id`, `provider_account_id`, `provider_account_label`, `scopes[]`, `status`, `expires_at`, `last_refresh_at`, `created_at` |
| `ContentAuthorizationURL` | `authorization_url`, `expires_at` (state TTL) |

## HTTP Flow

### Endpoints (user-scoped)

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/me/content_tokens` | List caller's linked providers (`ContentTokenInfo[]`) |
| `POST` | `/me/content_tokens/{provider_id}/authorize` | Body: `{ "client_callback": "…" }` → returns `ContentAuthorizationURL` |
| `GET` | `/oauth2/content_callback` | Public (`x-public-endpoint: true`). Exchanges code, stores token, 302 to client_callback |
| `DELETE` | `/me/content_tokens/{provider_id}` | Revoke at provider (where supported), delete row. 204 on success or when row already absent |

### Endpoints (admin-scoped)

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/admin/users/{user_id}/content_tokens` | List a target user's linked providers (reuses `ContentTokenInfo`) |
| `DELETE` | `/admin/users/{user_id}/content_tokens/{provider_id}` | Revoke + delete a target user's token |

Admin endpoints use the existing admin-role middleware. Both run the same revocation path as the me-versions.

### OAuth state storage

Redis key: `content_oauth_state:{nonce}`, TTL 10 minutes, payload:

```json
{
  "user_id": "…",
  "provider_id": "…",
  "client_callback": "…",
  "pkce_code_verifier": "…",
  "created_at": "…"
}
```

Single-use — the callback handler deletes the key immediately after reading.

### PKCE

Always S256. Server-generated verifier, stored in state payload, consumed during code exchange.

### Client-callback allow-list

The `client_callback` URL on `authorize` is validated against a list:

- If the existing user-auth flow already exposes an allow-list abstraction, reuse it.
- Otherwise, a new `content_oauth.allowed_client_callbacks` config entry is added (simple prefix/glob matching).

The final choice is a code-time discovery — either path is acceptable.

### Authorize flow

1. Client → `POST /me/content_tokens/{provider_id}/authorize` with `client_callback`.
2. Server validates: provider is registered + enabled; `client_callback` matches the allow-list.
3. Server generates nonce (`state`) and PKCE verifier/challenge.
4. State payload written to Redis.
5. Server constructs provider authorization URL with `response_type=code`, `redirect_uri={TMI}/oauth2/content_callback`, `scope={provider.RequiredScopes}`, `state={nonce}`, `code_challenge={S256}`.
6. Response: `{ authorization_url, expires_at }`.

### Callback flow (`GET /oauth2/content_callback`)

1. Validate `state` exists in Redis. If not → render minimal error page (no client_callback to 302 to); log.
2. Read + delete state payload (single-use).
3. If `error` query param is set (user denied, provider error) → 302 to `{client_callback}?status=error&error={code}&provider_id={…}`.
4. Exchange `code` + `pkce_code_verifier` at the provider's token endpoint.
5. Hit provider's "who am I" endpoint (provider-specific hook) for `provider_account_id` + `provider_account_label`.
6. Encrypt tokens, upsert into `user_content_tokens` (re-linking replaces prior row).
7. 302 to `{client_callback}?status=success&provider_id={…}`.

Any failure at steps 4–5: 302 to client_callback with `status=error&error={code}` and **no** row stored.

### Revoke flow (`DELETE` on me or admin endpoint)

1. Repository lookup — if no row, return 204 (idempotent).
2. Decrypt tokens.
3. If provider config declares a `revocation_url`, call RFC 7009 revoke. Any failure → `Warn` log, continue.
4. Delete the row.
5. Return 204.

### OpenAPI

New operations:

| operationId | Path | Method |
|-------------|------|--------|
| `listMyContentTokens` | `/me/content_tokens` | GET |
| `authorizeContentToken` | `/me/content_tokens/{provider_id}/authorize` | POST |
| `deleteMyContentToken` | `/me/content_tokens/{provider_id}` | DELETE |
| `contentOAuthCallback` | `/oauth2/content_callback` | GET (public) |
| `adminListUserContentTokens` | `/admin/users/{user_id}/content_tokens` | GET |
| `adminDeleteUserContentToken` | `/admin/users/{user_id}/content_tokens/{provider_id}` | DELETE |

The public callback is marked `x-public-endpoint: true`; responses are not cacheable (`x-cacheable-endpoint: false`).

New schemas: `ContentTokenInfo`, `ContentAuthorizationURL`.

### Rate limiting

Authorize + callback run through the existing per-user rate-limit middleware. No new limits introduced here.

## `DelegatedSource` helper

Lives alongside other content-source code in the same package as `ContentSource` (`api/content_source_delegated.go`), so concrete sources import from one place.

### Shape

```
(s *DelegatedSource) FetchForUser(ctx, userID, uri) (bytes []byte, contentType string, err error):
    token := tokenRepo.GetByUserAndProvider(ctx, userID, s.ProviderID)
      -> ErrNotFound → return ErrAuthRequired
      -> token.Status == "failed_refresh" → return ErrAuthRequired
    if token.ExpiresAt is in the past (or within 30-second skew):
        token = refresh(ctx, token)   // see Refresh flow below
    return s.DoFetch(ctx, token.AccessToken, uri)
```

Concrete delegated sources must implement:

- `Name() string`
- `CanHandle(ctx, uri) bool`
- `DoFetch(ctx, accessToken, uri) ([]byte, string, error)`
- Optionally `AccessValidator`, `AccessRequester` as usual.

The helper handles token lookup, expiry detection, refresh orchestration, status transitions, and concurrent-refresh serialization.

### Refresh flow (lazy)

1. Decrypt current refresh token; if none, fail with `ErrAuthRequired`.
2. `RefreshWithLock` opens a short transaction, `SELECT … FOR UPDATE` on the row, re-checks expiry (another goroutine may have refreshed already), and if still expired:
   a. POST `grant_type=refresh_token` to provider token endpoint.
   b. On success (200): encrypt new access token + (if rotated) new refresh token, update `expires_at`, `last_refresh_at`, `last_error=NULL`, `status='active'`, commit.
   c. On 400-class failure (invalid/revoked): set `status='failed_refresh'`, populate `last_error` (truncated), commit, then return `ErrAuthRequired` to caller.
   d. On 5xx or network failure: no status change, rollback, return `ErrTransient` wrapping the underlying error.

### Concurrent-refresh safety

`SELECT … FOR UPDATE` inside the refresh transaction. Two goroutines seeing the token expired both attempt the refresh; one acquires the lock, refreshes, commits; the second acquires the lock, finds the token no longer expired, and skips the provider call. This is correct across multiple server replicas.

### Error propagation into the pipeline

- `ErrAuthRequired` — documents transition to `access_status='auth_required'`; the Timmy session skips them and lists them under `skipped_sources` with the appropriate reason (using the machinery already shipped in #232).
- `ErrTransient` — treated as a general Fetch failure by the pipeline orchestrator; document retains its previous `access_status`.

No new Timmy events or document-model fields.

### Single-user-context discipline

`FetchForUser` requires a `user_id`. Delegated sources deliberately cannot run on behalf of no user (e.g., in anonymous background jobs). If a code path reaches a delegated source without user context, it returns a clear error rather than falling back to any kind of "service" identity.

## User-delete cascade

Before the existing `ON DELETE CASCADE` FK removes `user_content_tokens` rows:

1. List the user's `user_content_tokens` rows.
2. For each: run the same revocation path used by `DELETE /me/content_tokens/{provider_id}` — best-effort; failures logged (`Warn`), loop continues.
3. Then the existing DB cascade removes the rows.

Best-effort is deliberate — a provider outage must not block user deletion.

## Mock Delegated Source (test-build-only)

File: `api/content_source_mock_delegated.go`, build-tag `//go:build dev || test`.

### Stub OAuth provider

An HTTP test server stood up inside integration tests, speaking minimal:

- Authorization endpoint (redirects to our callback with `code`+`state`).
- Token endpoint (handles `grant_type=authorization_code` and `grant_type=refresh_token`; supports PKCE verification).
- Revocation endpoint (RFC 7009).
- Whoami endpoint (returns fake account id + email).

Knobs per test:

- Access-token lifetime (for fast expiry tests).
- Whether refresh succeeds, rotates refresh token, or fails 400 (to cover the `failed_refresh` path).
- Whether revocation succeeds (to exercise best-effort DELETE behavior).

### `MockDelegatedSource`

Embeds `DelegatedSource`. Handles URIs of the form `mock://doc/{id}`. `DoFetch` returns a canned blob so the fetch path runs end-to-end with a known URI scheme. Registered only when a `mock` entry is present in the `ContentOAuthProviderRegistry` — fixture-installed by integration tests; not exposed via env vars.

## Configuration

### New top-level config section: `content_oauth`

```yaml
content_oauth:
  callback_url: "http://localhost:8080/oauth2/content_callback"
  allowed_client_callbacks:
    - "http://localhost:8079/"
    - "http://localhost:4200/*"
  providers:
    confluence:
      enabled: false
      client_id: "..."
      client_secret: "..."
      auth_url: "https://auth.atlassian.com/authorize"
      token_url: "https://auth.atlassian.com/oauth/token"
      userinfo_url: "https://api.atlassian.com/me"
      revocation_url: ""          # optional; empty means no RFC 7009 support
      required_scopes: ["read:confluence-content.all"]
```

Note: only the `mock` provider will be wired for the initial landing of this sub-project; Confluence / Google Workspace entries are added by their respective sub-projects. The config schema above is the target shape.

### Environment variables

Parallels `OAUTH_PROVIDERS_*`, using the same dynamic provider discovery helper (`envutil.DiscoverProviders`):

- `TMI_CONTENT_OAUTH_CALLBACK_URL`
- `TMI_CONTENT_OAUTH_ALLOWED_CLIENT_CALLBACKS` (comma-separated)
- `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_ENABLED`
- `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_CLIENT_ID`
- `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_CLIENT_SECRET`
- `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_AUTH_URL`
- `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_TOKEN_URL`
- `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_USERINFO_URL`
- `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_REVOCATION_URL`
- `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_REQUIRED_SCOPES` (space-separated)
- `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` (required when any delegated provider is enabled)

### Startup validation

- Malformed / missing encryption key while any delegated provider is enabled → refuse to start with a clear config-validation error.
- Delegated provider enabled but missing required fields (client_id, auth_url, token_url) → log structured error and disable that provider (non-fatal).
- Log the registered delegated provider ids at startup (alongside the existing service-source log from #232).

## Testing

### Unit tests

- `ContentTokenRepository`: CRUD, encryption round-trip, typed-error returns, unique-constraint enforcement.
- `DelegatedSource.FetchForUser`: lookup, skew-aware expiry, refresh success/rotation/failure, status transitions, concurrent-refresh serialization.
- State payload Redis codec (round-trip + TTL).
- PKCE verifier generation + S256 challenge derivation.
- Client-callback allow-list matcher.

### Handler tests (with mocked repository)

For each endpoint (4 user + 2 admin + 1 public callback):

- Happy path.
- Provider not registered / not enabled → 422.
- Unauthenticated → 401 (user-scoped endpoints).
- Cross-user access denied → 403 (admin endpoints require admin role).
- Invalid / expired OAuth state → error page / logged.
- Provider-side user denial → client_callback redirect with `status=error`.
- `client_callback` not in allow-list → 400.

### Integration tests

Stub OAuth provider + real Postgres:

- Full authorize → callback → store → fetch → refresh → revoke.
- User-delete cascade: tokens revoked at provider *before* rows removed.
- Concurrent Fetch calls: exactly one refresh call at provider, both callers observe the new token.
- Refresh rotation: new refresh token persisted.
- Refresh failure (400): token status → `failed_refresh`, `last_error` populated, subsequent Fetch → `ErrAuthRequired` without provider call.
- Revocation failure: row still deleted, warn log emitted.

### CATS fuzzing

- `/me/content_tokens/*` + `/admin/users/{id}/content_tokens/*` fuzzed on the next release-gate run.
- `/oauth2/content_callback` marked `x-public-endpoint: true`.

## Implementation Phases

Each phase is an independent commit/PR candidate.

1. **Schema + repository.** Migration for `user_content_tokens`, `ContentToken` model, `ContentTokenRepository` + typed errors, encryption helper, unit tests. No HTTP wiring.
2. **Content OAuth provider registry + config.** `content_oauth` config section, env-var overrides, `ContentOAuthProviderRegistry`, startup validation for encryption key, parsing tests.
3. **Account-linking endpoints (me-scoped) + OAuth callback.** The four `/me/content_tokens/*` handlers + Redis state store + PKCE + client-callback allow-list + callback exchange → encrypted upsert → 302 redirect.
4. **`DelegatedSource` helper + `MockDelegatedSource` + stub OAuth provider.** Base helper with lazy refresh + `SELECT … FOR UPDATE` + status transitions; mock source; stub OAuth provider test harness; full authorize → fetch → refresh → revoke integration test.
5. **Admin endpoints + user-delete cascade.** `/admin/users/{id}/content_tokens/*`; pre-delete hook on user deletion that sweeps revocations before FK cascade runs.
6. **OpenAPI spec + regen + documentation.** Add schemas and operations, `make validate-openapi`, `make generate-api`, update the relevant GitHub wiki pages.

Phase 4 is the gate where end-to-end wiring lights up — if any infrastructure decision is wrong, it surfaces here before any real provider is built.

## Acceptance criteria

- All new endpoints pass handler + integration tests.
- Encryption round-trip verified; server refuses to start when the key is missing and a delegated provider is enabled.
- Concurrent-refresh test passes (two goroutines → exactly one provider refresh).
- User-delete triggers best-effort revocation before row removal.
- `MockDelegatedSource` enables the full flow without a real provider.
- OpenAPI spec validates; regenerated code builds; `make lint`, `make test-unit`, `make test-integration` all pass.
- CATS fuzzing run on the branch completes with no new true-positive 500s.
- No behavior change for existing service-source code paths (Google Drive / HTTP).

## Out of Scope (explicit)

- Confluence, Google Workspace delegated, OneDrive/SharePoint, DOCX/PPTX extractors (separate sub-projects of #249).
- Dynamic / incremental scope upgrades — scope changes require re-linking.
- Admin UI for provider config — tracked separately against tmi-ux.
- Shared / multi-user tokens (service-provider model, already handled by #232).
- Background refresh worker.
- Encryption-key rotation machinery.

## Tracking

This sub-project closes the delegated-infrastructure portion of #249. #249 stays open; a comment will be added listing the five sub-projects and linking this spec.
