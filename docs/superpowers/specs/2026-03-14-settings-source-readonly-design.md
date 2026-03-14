# Settings API: Source, Read-Only, and Full Config Exposure

**Date:** 2026-03-14
**Status:** Approved

## Problem

The `/admin/settings` API currently exposes only a subset of server configuration, and provides no way for an admin to determine:
- Where a setting's effective value comes from (database, config file, environment variable)
- Whether a setting can be modified via the API
- Whether required secrets are configured without disclosing actual secret values

Many configuration values in `config-development.yml` (server settings, auth flags, logging, connection pool, etc.) are invisible to the admin API.

## Design

### Approach: Compute-on-Read

`source` and `read_only` are computed at response time in the handler layer, never stored in the database. The `List` and `Get` endpoints merge database settings with config-sourced settings, computing `source`/`read_only` for each. No database migration is required.

### OpenAPI Schema Changes

Add two properties to `SystemSetting` (response-only, not in `SystemSettingUpdate`):

- **`source`**: `string`, enum `[database, config, environment, vault]`, `readOnly: true`
  - Where the effective value comes from
  - `vault` is reserved for future external secret provider integration
- **`read_only`**: `boolean`, `readOnly: true`
  - Whether this setting can be modified via the API

**Note on `additionalProperties: false`:** The current `SystemSetting` schema sets `additionalProperties: false`. Adding new properties requires updating the schema and regenerating API code via `make generate-api`. Clients generated from the **previous** schema version with strict validation may reject the new fields. This is a minor version bump (additive properties), not a breaking change in practice — standard JSON parsers and most generated clients ignore unknown fields. However, any client performing strict `additionalProperties` validation against the old schema will need to update.

### Source and Read-Only Rules

| Source | `read_only` | Meaning |
|--------|------------|---------|
| `database` | `false` | Stored in DB, modifiable via API |
| `config` | `true` | From config file, API writes ineffective |
| `environment` | `true` | From env var, API writes ineffective |
| `vault` | `true` | From external secret provider (future) |

### GORM Model Changes

Add two fields to `SystemSetting` with `gorm:"-"` (not persisted):

```go
Source   string `gorm:"-" json:"source"`
ReadOnly bool   `gorm:"-" json:"read_only"`
```

No database migration required.

### MigratableSetting Expansion

Add fields to `MigratableSetting` in both `internal/config` and `api` packages:

**`internal/config/migratable_settings.go`:**
```go
type MigratableSetting struct {
    Key         string
    Value       string
    Type        string
    Description string
    Secret      bool   // true = mask value in API responses
    Source      string // "config" or "environment"
}
```

**`api/server.go`** (the `api.MigratableSetting` type):
```go
type MigratableSetting struct {
    Key         string
    Value       string
    Type        string
    Description string
    Secret      bool
    Source      string
}
```

**`api/config_provider_adapter.go`** must pass through the new `Secret` and `Source` fields when adapting between the two types.

The `ConfigProvider` interface (in `api/server.go`) returns `[]MigratableSetting` and is implicitly updated when the struct changes. All implementations — including test mocks — must be updated to populate the new fields.

### Environment Override Tracking

Rather than adding a generic reflection-based tracking mechanism to `overrideWithEnv` (which uses struct reflection and doesn't have access to setting key paths), each setting in `GetMigratableSettings` checks for its corresponding environment variable directly using `os.Getenv`. Each config field already has a known env var name from the struct's `env` tag. For example:

```go
source := "config"
if os.Getenv("TMI_SERVER_PORT") != "" {
    source = "environment"
}
settings = append(settings, MigratableSetting{
    Key: "server.port", Value: c.Server.Port, Type: "string",
    Description: "HTTP server port", Source: source,
})
```

This is explicit, simple, and avoids the complexity of mapping Go struct field paths to dotted setting key paths through reflection. Each setting knows its own env var name.

### Expanded Config Exposure

All configuration is exposed through the settings API. This is **new behavior** — the current `ListSystemSettings` handler only returns database settings, and the current `GetMigratableSettings` only returns a small subset of config values. After this change, the merged view will include all config entries.

**Estimated scope:** ~60-80 new settings added to `GetMigratableSettings` across new helper methods (server, auth, JWT, cookie, logging, database, secrets, administrators sections).

Settings fall into two categories:

**Non-secret settings** (actual value shown):
- `server.*` — port, interface, base_url, timeouts, TLS enabled/subject_name (not key file contents), http_to_https_redirect, CORS allowed_origins
- `auth.build_mode`, `auth.auto_promote_first_user`, `auth.everyone_is_a_reviewer`
- `auth.jwt.expiration_seconds`, `auth.jwt.signing_method`, `auth.jwt.refresh_token_days`, `auth.jwt.session_lifetime_days`
- `auth.oauth.callback_url`
- `auth.oauth.providers.*` — enabled, id, name, icon, authorization_url, token_url, issuer, jwks_url, client_id, scopes (as JSON), auth_header_format, accept_header, userinfo (as JSON)
- `auth.saml.enabled`
- `auth.saml.providers.*` — enabled, id, name, icon, entity_id, metadata_url, acs_url, slo_url, idp_metadata_url, attribute fields, behavior flags, encrypt_assertions, attribute_mapping (as JSON), group_attribute_name, group_prefix
- `auth.cookie.*` — enabled, domain, secure
- `logging.*` — all fields
- `websocket.*`
- `database.connection_pool.*`
- `database.url` — URL-sanitized (password replaced with `****`)
- `database.redis.url` — URL-sanitized (password replaced with `****`)
- `secrets.provider` and non-token fields (aws_region, aws_secret_name, vault_address, vault_path, azure_vault_url, gcp_project_id, gcp_secret_name, oci_compartment_id, oci_vault_id, oci_secret_name)
- `administrators` — as JSON array
- `operator.*`

**Secret settings** (value masked):
- `auth.jwt.secret` → `"<configured>"` or `"<not configured>"`
- `auth.oauth.providers.*.client_secret` → `"<configured>"` or `"<not configured>"`
- `auth.saml.providers.*.sp_private_key` → `"<configured>"` or `"<not configured>"`
- `auth.saml.providers.*.sp_certificate` → `"<configured>"` or `"<not configured>"`
- `auth.saml.providers.*.idp_metadata_b64xml` → `"<configured>"` or `"<not configured>"`
- `database.redis.password` → `"<configured>"` or `"<not configured>"`
- `secrets.vault_token` → `"<configured>"` or `"<not configured>"`

**Pagination:** The current OpenAPI spec allows `maxItems: 1000` on the List response. With full config exposure (~60-80 config settings + database settings), the total count stays well within this limit even with multiple OAuth/SAML providers configured.

**Config caching:** Config values are treated as immutable during server lifetime (config files and env vars do not change at runtime). The existing `configSettingsCache` in `SettingsService` will cache the expanded set; no cache invalidation changes are needed.

### URL Sanitization

A utility function in `internal/config`:

```go
func sanitizeURL(rawURL string) string
```

- Parses with `net/url.Parse`
- If a password exists in the userinfo, replaces it with `****`
- Returns the re-serialized URL
- If parsing fails, returns `"<invalid URL>"` to avoid leaking unparsable credential strings
- Handles bare `host:port` strings (no scheme): if `net/url.Parse` fails or produces unexpected results, returns the input unchanged (no credentials possible without userinfo)

Applied to `database.url` and `database.redis.url` inside `GetMigratableSettings`. The actual config values are never modified.

### API Handler Changes

**`ListSystemSettings`** (new merge behavior — currently returns database-only settings):
1. Load all database settings via `s.settingsService.List(ctx)` into a map keyed by setting key
2. Load all config settings via `s.configProvider.GetMigratableSettings()`
3. Merge: for each config setting, it becomes the effective value regardless of whether a database row exists. For database-only settings (no config counterpart), they pass through as-is.
4. Enrich each setting with `source` and `read_only`
5. If `Secret` is true, replace value with `"<configured>"` or `"<not configured>"`
6. Return merged list sorted by key

**`GetSystemSetting`** (new merge behavior — currently calls database-only `Get` method):
Same merge logic as List but for a single key. Check config provider first; if not found, fall back to database. Enrich with `source`/`read_only` and apply secret masking. This requires a new code path — either a new method on `SettingsService` (e.g., `GetEffective`) or inline logic in the handler.

**`UpdateSystemSetting` (PUT):**
Check if the setting's effective source is `"config"` or `"environment"`. If so, return **409 Conflict** with message: `"Setting '{key}' is controlled by {source} and cannot be modified via the API"`.

**`DeleteSystemSetting`:**
If the setting exists only in config/environment (no database row), return **404 Not Found** (nothing to delete). If the setting exists in both database and config, allow the delete — removing the database row reveals the config value as the new effective value. If the setting exists only in the database, delete as normal.

### Files Changed

| File | Change |
|---|---|
| `api-schema/tmi-openapi.json` | Add `source` (enum) and `read_only` (bool) to `SystemSetting`, both `readOnly: true` |
| `api/models/system_setting.go` | Add `Source` and `ReadOnly` fields with `gorm:"-"` |
| `internal/config/migratable_settings.go` | Add `Secret` and `Source` fields to `MigratableSetting`, expand to expose all config (~60-80 new settings), add URL sanitization, use `os.Getenv` checks for source detection |
| `api/server.go` | Add `Secret`, `Source` fields to `api.MigratableSetting` |
| `api/config_provider_adapter.go` | Pass through new `Secret`, `Source` fields |
| `api/config_handlers.go` | New merge+enrich logic in List/Get handlers; 409 rejection in Update; 404/delete logic in Delete |
| `api/settings_service.go` | Possibly add `GetEffective` method, or merge stays in handler layer |
| Tests | Update existing test mocks for new `MigratableSetting` fields, add tests for: source/read_only enrichment, secret masking, URL sanitization, 409 rejection on PUT, delete behavior for dual-source settings |

### Non-Goals

- No database migration
- No new endpoints
- No nested/hierarchical config representation
- No runtime modification of config/environment-sourced settings
- No vault integration implementation (enum value reserved for future)
