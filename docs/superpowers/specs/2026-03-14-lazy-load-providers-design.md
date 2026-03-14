# Lazy Load OAuth/SAML Provider Config from Database

**Date:** 2026-03-14
**Status:** Approved
**Issue:** [#175](https://github.com/ericfitz/tmi/issues/175)

## Problem

OAuth/SAML providers are currently loaded only from config files and environment variables at startup. In a hosted TMI scenario, one OAuth provider is configured via environment (to bootstrap and allow first admin login), but subsequent providers need to be configurable at runtime via the database. There is no mechanism to add, modify, or enable new auth providers without redeploying.

## Design Decisions

These decisions were made during the brainstorming phase:

1. **Config/env always wins** — If a provider ID exists in both config/env and database, the config/env version is used. Database version is skipped entirely. Consistent with the existing three-tier settings priority (env > config > database).
2. **Eager load at startup, on-demand refresh** — All providers (config + DB) loaded at boot. DB providers re-read on cache invalidation or TTL expiry.
3. **Use existing settings API** — No new endpoints. Admins compose providers by writing individual settings keys (e.g., `auth.oauth.providers.azure.client_id`) via the existing `/admin/settings` API.
4. **Validate on enable** — Individual settings writes are accepted freely. Validation of required fields occurs only when `auth.*.providers.<id>.enabled` is set to `"true"`.
5. **Use existing encryption** — Provider secrets stored in DB use the existing `SettingsEncryptor` at-rest encryption.
6. **Convention-based discovery** — DB providers discovered by scanning settings keys matching `auth.oauth.providers.<id>.*` and `auth.saml.providers.<id>.*`, grouped by provider ID.
7. **Immediate invalidation + TTL safety net** — Cache invalidated immediately on provider-related settings writes. TTL-based refresh (60s) as safety net for direct DB changes or multi-instance deployments.

## Design

### Approach: Provider Registry Layer

A new `ProviderRegistry` abstraction sits between the auth service/handlers and provider configuration sources. It merges immutable config/env providers with mutable DB-sourced providers, handles caching, validation, and invalidation.

### ProviderRegistry Interface

```go
// ProviderRegistry provides unified access to OAuth and SAML provider
// configurations from all sources (config, environment, database).
type ProviderRegistry interface {
    // GetOAuthProvider returns an OAuth provider by ID regardless of enabled state.
    // Callers that need only enabled providers should check the Enabled field.
    // This preserves the current behavior of getProvider() in handlers_providers.go,
    // which looks up providers by ID without checking Enabled.
    GetOAuthProvider(id string) (OAuthProviderConfig, bool)

    // GetEnabledOAuthProviders returns all enabled OAuth providers.
    GetEnabledOAuthProviders() map[string]OAuthProviderConfig

    // GetSAMLProvider returns a SAML provider by ID regardless of enabled state.
    GetSAMLProvider(id string) (SAMLProviderConfig, bool)

    // GetEnabledSAMLProviders returns all enabled SAML providers.
    GetEnabledSAMLProviders() map[string]SAMLProviderConfig

    // InvalidateCache marks the DB provider cache as dirty. The next Get* call
    // will trigger a refresh from the database.
    InvalidateCache()
}
```

**Behavioral note on `GetOAuthProvider`/`GetSAMLProvider`:** These methods return providers regardless of `Enabled` state. This preserves the current behavior of `h.getProvider()` in `handlers_providers.go`, which looks up by ID without checking `Enabled`. This matters for in-flight OAuth flows: if a provider is disabled while a user is mid-authorize, the token exchange can still complete. The `GetEnabled*` methods filter to `Enabled == true` and are used for listing available providers.

### DefaultProviderRegistry Implementation

```go
// ProviderSettingsReader is a minimal interface defined in the auth package
// to avoid a circular dependency on the api package. The api.SettingsService
// satisfies this interface.
type ProviderSettingsReader interface {
    // ListByPrefix returns all settings whose key starts with the given prefix.
    ListByPrefix(ctx context.Context, prefix string) ([]ProviderSetting, error)
}

// ProviderSetting is a minimal representation of a setting key/value pair,
// used by the registry to assemble provider configs from DB settings.
type ProviderSetting struct {
    Key   string
    Value string
}

type DefaultProviderRegistry struct {
    // Immutable: loaded once at startup from config/env
    configOAuth map[string]OAuthProviderConfig
    configSAML  map[string]SAMLProviderConfig

    // Database-sourced providers (cached, refreshable)
    dbOAuth     map[string]OAuthProviderConfig
    dbSAML      map[string]SAMLProviderConfig
    dbCacheMu   sync.RWMutex
    dbCacheTime time.Time
    cacheTTL    time.Duration  // default 60s, matches SettingsCacheTTL
    dirty       bool           // set by InvalidateCache, cleared on refresh

    // Settings reader for DB provider keys (satisfied by api.SettingsService)
    settings    ProviderSettingsReader
}
```

**Avoiding circular imports:** The `ProviderSettingsReader` interface is defined in the `auth` package with only the methods the registry needs. The `api.SettingsService` implements this interface. This avoids importing `api` from `auth`. A new `ListByPrefix(ctx, prefix)` method will be added to `SettingsService` to support efficient key scanning (rather than loading all settings and filtering in-memory).

### Provider Assembly from Settings Keys

The registry scans all settings keys matching `auth.oauth.providers.<id>.*` and `auth.saml.providers.<id>.*`. It groups by provider ID, then maps each recognized field name to the corresponding struct field using a static field mapping table (no reflection).

**Example key-to-field mappings (OAuth):**

| Settings Key Suffix | Struct Field | Type |
|---|---|---|
| `.client_id` | `ClientID` | string |
| `.client_secret` | `ClientSecret` | string |
| `.authorization_url` | `AuthorizationURL` | string |
| `.token_url` | `TokenURL` | string |
| `.issuer` | `Issuer` | string |
| `.jwks_url` | `JWKSURL` | string |
| `.enabled` | `Enabled` | bool |
| `.name` | `Name` | string |
| `.icon` | `Icon` | string |
| `.scopes` | `Scopes` | JSON array |
| `.userinfo` | `UserInfo` | JSON array |
| `.additional_params` | `AdditionalParams` | JSON object |
| `.auth_header_format` | `AuthHeaderFormat` | string |
| `.accept_header` | `AcceptHeader` | string |

**Example key-to-field mappings (SAML):**

| Settings Key Suffix | Struct Field | Type |
|---|---|---|
| `.entity_id` | `EntityID` | string |
| `.metadata_url` | `MetadataURL` | string |
| `.metadata_xml` | `MetadataXML` | string |
| `.acs_url` | `ACSURL` | string |
| `.slo_url` | `SLOURL` | string |
| `.sp_private_key` | `SPPrivateKey` | string |
| `.sp_private_key_path` | `SPPrivateKeyPath` | string |
| `.sp_certificate` | `SPCertificate` | string |
| `.sp_certificate_path` | `SPCertificatePath` | string |
| `.idp_metadata_url` | `IDPMetadataURL` | string |
| `.idp_metadata_b64xml` | `IDPMetadataB64XML` | string |
| `.enabled` | `Enabled` | bool |
| `.name` | `Name` | string |
| `.icon` | `Icon` | string |
| `.allow_idp_initiated` | `AllowIDPInitiated` | bool |
| `.force_authn` | `ForceAuthn` | bool |
| `.sign_requests` | `SignRequests` | bool |
| `.name_id_attribute` | `NameIDAttribute` | string |
| `.email_attribute` | `EmailAttribute` | string |
| `.name_attribute` | `NameAttribute` | string |
| `.groups_attribute` | `GroupsAttribute` | string |

Unrecognized field suffixes are ignored (logged at debug level).

### Provider ID Validation

Provider IDs extracted from settings keys must match the pattern `^[a-z0-9][a-z0-9-]*$` (lowercase alphanumeric and hyphens, starting with alphanumeric). Settings keys with invalid provider IDs are ignored with a warning log. This matches the convention used by env var discovery (provider IDs derived from env var names are lowercased).

### Merge Rules

1. Config/env providers are loaded at startup into `configOAuth`/`configSAML` (immutable).
2. DB providers are loaded from settings into `dbOAuth`/`dbSAML` (refreshable).
3. If a provider ID exists in config, the DB version for that same ID is skipped entirely.
4. Callers see the merged result: all config providers + non-overlapping DB providers.

### Validation on Enable

When assembling a DB provider where `enabled` is `true`, the registry validates required fields:

**OAuth required fields:**
- `client_id`
- `authorization_url`
- `token_url`
- At least one `userinfo` entry

**SAML required fields:**
- `entity_id`
- At least one of: `metadata_url`, `idp_metadata_url`, `idp_metadata_b64xml`

If validation fails, the provider is logged as a warning and excluded from `GetEnabled*` results. It remains accessible in settings for the admin to complete configuration.

### Cache Lifecycle

- **Lazy invalidation:** `InvalidateCache()` sets a `dirty` flag but does NOT immediately re-read from the database. The next `Get*` call sees the dirty flag and triggers a refresh. This avoids redundant DB reads when an admin writes multiple provider keys in quick succession (e.g., 10 keys to configure a provider results in 10 invalidations but only 1 DB read on the next access).
- **TTL expiry:** On any `Get*` call, if `time.Since(dbCacheTime) > cacheTTL`, re-read from settings service. This is the safety net for direct DB changes or multi-instance deployments.
- **Trigger:** The settings API handler calls `registry.InvalidateCache()` after any successful write or delete to a key starting with `auth.oauth.providers.` or `auth.saml.providers.`.
- **Concurrency:** On a `Get*` call that finds the cache dirty or TTL expired, the implementation uses double-checked locking: acquire read lock, check staleness, release, acquire write lock, re-check (another goroutine may have refreshed), then refresh if still needed. This matches the pattern used in `SettingsService.getConfigSetting`.

### Integration Points

**Startup wiring** (in `cmd/server/main.go`, Phase 3/4):

```go
registry := auth.NewDefaultProviderRegistry(
    authConfig.OAuth.Providers,   // immutable config-sourced OAuth
    authConfig.SAML.Providers,    // immutable config-sourced SAML
    settingsService,              // for DB provider reads (satisfies ProviderSettingsReader)
)
authHandlers.SetProviderRegistry(registry)
authHandlers.Service().SetProviderRegistry(registry)
server.SetProviderRegistry(registry)  // for cache invalidation from settings handlers
```

**Code migration from config maps to registry:**

| Current code | New code |
|---|---|
| `h.config.OAuth.Providers[id]` in `getProvider()` | `h.registry.GetOAuthProvider(id)` |
| `h.config.OAuth.Providers` iteration in `GetProviders()` | `h.registry.GetEnabledOAuthProviders()` |
| `h.config.SAML.Providers` iteration in `GetSAMLProviders()` | `h.registry.GetEnabledSAMLProviders()` |
| `config.GetProvider(id)` | `registry.GetOAuthProvider(id)` |
| `config.GetEnabledProviders()` | values of `registry.GetEnabledOAuthProviders()` |

The `Handlers` struct keeps its `config` field for non-provider settings (JWT config, callback URL, SAML enabled flag, build mode). Only provider map access moves to the registry.

**SAMLManager integration:**

The SAML manager currently initializes SAML providers eagerly at startup from the config map. With the registry:
- At startup, it initializes config-sourced SAML providers as before.
- A new method `samlManager.EnsureProvider(id string, config SAMLProviderConfig) error` lazily initializes a DB-sourced SAML provider (parse metadata, set up SP config) on first access.
- `GetSAMLProviders` handler calls `EnsureProvider` for each enabled DB-sourced provider before including it in the response.

**`EnsureProvider` behavior:**
- **Idempotent:** If the provider is already initialized with the same config, returns immediately (no-op).
- **Thread-safe:** Uses a per-provider mutex to prevent concurrent initialization of the same provider. Other providers can initialize concurrently.
- **Failure handling:** If metadata fetch fails (timeout, HTTP error, invalid XML), `EnsureProvider` returns an error. The calling handler logs the error and excludes the provider from the response (marks it as `Initialized: false`). The provider is NOT cached as failed — the next request will retry initialization. This allows transient failures (e.g., IDP temporarily down) to self-heal.
- **Config change detection:** If called with a different config than the previously initialized version (detected by comparing key fields), the existing provider is torn down and re-initialized. This handles config updates via the settings API.

**SAML global enable flag:** The `h.config.SAML.Enabled` flag (or `features.saml_enabled` setting) acts as a global kill switch for ALL SAML providers, including DB-sourced ones. If SAML is globally disabled, `GetSAMLProviders` returns an empty array regardless of DB provider state. DB-sourced SAML providers are only loaded and initialized when SAML is globally enabled.

### Encryption and Secret Masking

**At-rest encryption:** DB-sourced provider secrets flow through existing `SettingsService` encryption. Writing `auth.oauth.providers.azure.client_secret` encrypts via `SettingsEncryptor`. Reading decrypts transparently. No new encryption code.

**Secret masking in settings API:** A static set of known secret field suffixes determines which DB provider settings get masked in API responses:

```go
var providerSecretSuffixes = []string{
    ".client_secret",
    ".sp_private_key",
    ".sp_certificate",
    ".idp_metadata_b64xml",
}
```

Any DB setting whose key starts with `auth.oauth.providers.` or `auth.saml.providers.` and ends with one of these suffixes gets `Secret: true` and its value masked as `"<configured>"` or `"<not configured>"`.

### Settings Write Flow

Admin creates a new DB-sourced OAuth provider via individual settings writes:

1. `PUT /admin/settings` with key `auth.oauth.providers.azure.client_id` — accepted, stored in DB.
2. `PUT` key `auth.oauth.providers.azure.client_secret` — accepted, encrypted at rest.
3. `PUT` additional keys (authorization_url, token_url, userinfo, scopes, etc.) — build up config.
4. `PUT` key `auth.oauth.providers.azure.enabled` with value `"true"` — triggers validation. If required fields missing, **409 Conflict** with message listing missing fields. If valid, accepted, `registry.InvalidateCache()` called, provider becomes available.

**Write guard interaction:** The existing write guard rejects writes to keys that have a config-sourced value. A new provider ID's keys won't exist in config, so they pass through. Keys for config-sourced provider IDs remain read-only (409 on write attempt).

**Enable-validation gate:** When the key being written matches `auth.{oauth,saml}.providers.<id>.enabled` and value is `"true"`, the handler reads all sibling keys for that provider ID from both DB settings and config, assembles a provider struct, validates required fields, and rejects with 409 if incomplete. The 409 response body lists the missing required fields.

## Files Changed

| File | Change |
|---|---|
| `auth/provider_registry.go` | **New.** `ProviderRegistry` interface, `DefaultProviderRegistry` implementation, field mapping tables, assembly logic, validation |
| `auth/provider_registry_test.go` | **New.** Unit tests for registry |
| `auth/handlers.go` | Add `registry ProviderRegistry` field, `SetProviderRegistry` method |
| `auth/handlers_providers.go` | Replace `h.config.OAuth/SAML.Providers` with `h.registry.GetEnabled*` calls; update `getProvider` |
| `auth/service.go` | Add `registry ProviderRegistry` field, `SetProviderRegistry` method |
| `auth/config.go` | `GetProvider`/`GetEnabledProviders` — keep for backward compat but delegate to registry if set |
| `auth/saml_manager.go` | Add `EnsureProvider(id, config)` for lazy SAML provider initialization |
| `api/config_handlers.go` | Add enable-validation gate on `enabled=true` writes; add `InvalidateCache` call after provider key writes; add secret suffix detection for DB provider keys |
| `api/settings_service.go` | Add `ListByPrefix(ctx, prefix)` method returning settings with keys matching prefix |
| `api/server.go` | Add `providerRegistry` field, `SetProviderRegistry` method, wired during setup |
| `cmd/server/main.go` | Create and wire `DefaultProviderRegistry` in Phase 3/4 |
| Tests (existing) | Update mocks/test configs that access provider maps directly |

## Testing Strategy

1. **Registry unit tests** (`auth/provider_registry_test.go`):
   - Config-only providers returned correctly
   - DB-only providers assembled from settings keys
   - Config providers take precedence over DB (same ID skipped)
   - Providers with `enabled=false` excluded from `GetEnabled*`
   - Validation rejects incomplete providers on `enabled=true`
   - Cache invalidation forces re-read
   - TTL expiry triggers re-read
   - Unrecognized field names ignored

2. **Enable-validation gate tests** (`api/config_handlers_test.go`):
   - `enabled=true` with all required fields succeeds
   - `enabled=true` with missing required fields returns 409 with field list
   - Non-`enabled` provider key writes succeed without validation
   - `enabled=false` writes succeed without validation

3. **Secret suffix detection tests** (`api/config_handlers_test.go`):
   - DB provider settings with secret suffixes masked in list response
   - DB provider settings without secret suffixes show actual values

4. **Integration tests**:
   - Create provider entirely via settings API, enable it, verify it appears in `/oauth2/providers`
   - Modify a DB provider setting, verify change reflected after cache invalidation
   - Attempt to write a config-sourced provider key, verify 409
   - End-to-end OAuth authorize/callback/token flow with a DB-sourced provider
   - SAML global disable flag suppresses DB-sourced SAML providers
   - Provider ID validation rejects invalid IDs on settings write

## Non-Goals

- No new API endpoints (uses existing settings API)
- No new database tables or migrations (providers stored as individual `system_settings` rows)
- No nested/hierarchical settings representation
- No vault integration (reserved enum value only)
- No UI for provider management (API-only)
- No runtime modification of config/env-sourced providers
