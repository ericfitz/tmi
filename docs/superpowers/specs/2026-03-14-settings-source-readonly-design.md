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

These are additive, non-breaking changes to the API.

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

Add fields to `MigratableSetting`:

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

### Environment Override Tracking

Add `envOverrides map[string]bool` to `Config` struct. During `overrideWithEnv`, when an env var is found and applied, record the setting key path (e.g., `"server.port"`). `GetMigratableSettings` consults this map to set `Source` to `"environment"` vs `"config"`.

### Expanded Config Exposure

All configuration is exposed through the settings API. Settings fall into two categories:

**Non-secret settings** (actual value shown):
- `server.*` — port, interface, base_url, timeouts, TLS settings (not key file contents), CORS
- `auth.build_mode`, `auth.auto_promote_first_user`, `auth.everyone_is_a_reviewer`
- `auth.jwt.expiration_seconds`, `auth.jwt.signing_method`, `auth.jwt.refresh_token_days`, `auth.jwt.session_lifetime_days`
- `auth.oauth.callback_url`
- `auth.oauth.providers.*` — enabled, id, name, icon, authorization_url, token_url, issuer, jwks_url, client_id, scopes, auth_header_format, accept_header, userinfo (as JSON)
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

### URL Sanitization

A utility function in `internal/config`:

```go
func sanitizeURL(rawURL string) string
```

- Parses with `net/url.Parse`
- If a password exists in the userinfo, replaces it with `****`
- Returns the re-serialized URL
- If parsing fails, returns `"<invalid URL>"` to avoid leaking unparsable credential strings

Applied to `database.url` and `database.redis.url` inside `GetMigratableSettings`. The actual config values are never modified.

### API Handler Changes

**`ListSystemSettings` and `GetSystemSetting`:**
1. Load all database settings into a map keyed by setting key
2. Load all config settings via `configProvider.GetMigratableSettings()`
3. Merge: for each config setting, if the key also exists in the database, the config value wins (existing priority behavior). Database-only settings pass through as-is.
4. Enrich each setting with `source` and `read_only`
5. If `Secret` is true, replace value with `"<configured>"` or `"<not configured>"`
6. Return merged list sorted by key

**`UpdateSystemSetting` (PUT):**
Check if the setting's effective source is `"config"` or `"environment"`. If so, return **409 Conflict** with message: `"Setting '{key}' is controlled by {source} and cannot be modified via the API"`.

**`DeleteSystemSetting`:**
Same 409 guard for config/environment-sourced settings.

### Files Changed

| File | Change |
|---|---|
| `api-schema/tmi-openapi.json` | Add `source` (enum) and `read_only` (bool) to `SystemSetting`, both `readOnly: true` |
| `api/models/system_setting.go` | Add `Source` and `ReadOnly` fields with `gorm:"-"` |
| `internal/config/config.go` | Add `envOverrides map[string]bool` to `Config`, populate during `overrideWithEnv` |
| `internal/config/migratable_settings.go` | Add `Secret` and `Source` fields to `MigratableSetting`, expand to expose all config, add URL sanitization, use `envOverrides` for source |
| `api/server.go` | Add `Secret`, `Source` fields to `api.MigratableSetting` |
| `api/config_provider_adapter.go` | Pass through new `Secret`, `Source` fields |
| `api/config_handlers.go` | Merge+enrich in List/Get handlers; 409 rejection in Update/Delete |
| Tests | Update existing, add tests for source/read_only, secret masking, URL sanitization, 409 rejection |

### Non-Goals

- No database migration
- No new endpoints
- No nested/hierarchical config representation
- No runtime modification of config/environment-sourced settings
- No vault integration implementation (enum value reserved for future)
