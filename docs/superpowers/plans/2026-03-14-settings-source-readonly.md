# Settings Source, Read-Only, and Full Config Exposure Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `source` and `read_only` properties to settings API responses, expose all server configuration through the settings API, and mask secret values.

**Architecture:** Compute-on-read approach. Config settings are merged with database settings at response time in the handler layer. `source` and `read_only` are never persisted — they're derived from where the effective value originates. Secret values are masked before reaching the API response.

**Tech Stack:** Go, OpenAPI 3.0.3, oapi-codegen, Gin, GORM, PostgreSQL

**Spec:** `docs/superpowers/specs/2026-03-14-settings-source-readonly-design.md`

---

## Chunk 0: Preparatory — Extract SettingsService Interface

### Task 0: Extract SettingsServiceInterface for Testability

The `Server.settingsService` field is currently a concrete `*SettingsService`. Tests cannot assign `MockSettingsService` to it. Extract an interface so both real and mock implementations satisfy it.

**Files:**
- Modify: `api/server.go:50-51`
- Modify: `api/config_handlers_test.go:19-91`

- [ ] **Step 1: Define SettingsServiceInterface in api/server.go**

Add above the `Server` struct:

```go
// SettingsServiceInterface defines the operations needed by handlers on settings.
type SettingsServiceInterface interface {
    Get(ctx context.Context, key string) (*models.SystemSetting, error)
    GetString(ctx context.Context, key string) (string, error)
    GetInt(ctx context.Context, key string) (int, error)
    GetBool(ctx context.Context, key string) (bool, error)
    List(ctx context.Context) ([]models.SystemSetting, error)
    Set(ctx context.Context, setting *models.SystemSetting) error
    Delete(ctx context.Context, key string) error
    SeedDefaults(ctx context.Context) error
}
```

Add `"context"` and `"github.com/ericfitz/tmi/api/models"` to the imports.

- [ ] **Step 2: Change Server.settingsService to use the interface**

In the `Server` struct, change:

```go
settingsService SettingsServiceInterface
```

Also update `SetSettingsService` to accept the interface:

```go
func (s *Server) SetSettingsService(settingsService SettingsServiceInterface) {
    s.settingsService = settingsService
}
```

- [ ] **Step 3: Verify MockSettingsService satisfies the interface**

The existing `MockSettingsService` in `api/config_handlers_test.go` already has all the required methods. Add a compile-time check:

```go
var _ SettingsServiceInterface = (*MockSettingsService)(nil)
```

- [ ] **Step 4: Build and test**

Run: `make build-server && make test-unit`
Expected: PASS — `*SettingsService` already satisfies the interface implicitly

- [ ] **Step 5: Commit**

```bash
git add api/server.go api/config_handlers_test.go
git commit -m "refactor(api): extract SettingsServiceInterface for testability"
```

---

## Chunk 1: Foundation — Schema, Model, and MigratableSetting Changes

### Task 1: Add `source` and `read_only` to OpenAPI Schema

**Files:**
- Modify: `api-schema/tmi-openapi.json:6468-6523` (SystemSetting schema)

- [ ] **Step 1: Add source and read_only properties to SystemSetting**

In `api-schema/tmi-openapi.json`, add two properties to the `SystemSetting` schema (inside `"properties": {}`), after `"modified_by"`:

```json
"source": {
  "type": "string",
  "description": "Where the effective value of this setting comes from. Server-managed, not writable.",
  "enum": ["database", "config", "environment", "vault"],
  "readOnly": true
},
"read_only": {
  "type": "boolean",
  "description": "Whether this setting can be modified via the API. True when source is not database.",
  "readOnly": true
}
```

Update the `"example"` to include the new fields:

```json
"example": {
  "key": "rate_limit.requests_per_minute",
  "value": "100",
  "type": "int",
  "description": "Maximum API requests per minute per user",
  "modified_at": "2026-01-15T10:30:00Z",
  "modified_by": "550e8400-e29b-41d4-a716-446655440000",
  "source": "database",
  "read_only": false
}
```

- [ ] **Step 2: Validate the OpenAPI spec**

Run: `make validate-openapi`
Expected: PASS (no new validation errors)

- [ ] **Step 3: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` regenerated with new `Source` and `ReadOnly` fields on `SystemSetting` struct

- [ ] **Step 4: Verify generated code has new fields**

Check that `api/api.go` now has `Source` and `ReadOnly` in the `SystemSetting` struct:
```go
type SystemSetting struct {
    // ... existing fields ...
    ReadOnly *bool                  `json:"read_only,omitempty"`
    Source   *SystemSettingSource   `json:"source,omitempty"`
}
```

Also check that a new `SystemSettingSource` type and constants were generated.

- [ ] **Step 5: Build to check for compilation errors**

Run: `make build-server`
Expected: May fail — the generated `SystemSetting` struct now has new pointer fields (`Source *SystemSettingSource`, `ReadOnly *bool`) that are not populated by `modelToAPISystemSetting` or any test code that constructs `SystemSetting` literals. This is expected and will be resolved in Task 6 when the merge/enrich logic is added. If compilation errors occur, note them but proceed to Task 2.

- [ ] **Step 6: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add source and read_only properties to SystemSetting schema"
```

### Task 2: Add Source and ReadOnly to GORM Model

**Files:**
- Modify: `api/models/system_setting.go:11-23`

- [ ] **Step 1: Write test for new model fields**

Create test in `api/models/system_setting_test.go` (or add to existing test file if one exists):

```go
package models

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestSystemSetting_SourceAndReadOnly(t *testing.T) {
    setting := SystemSetting{
        SettingKey:  "test.key",
        Value:       "test-value",
        SettingType: SystemSettingTypeString,
        Source:      "config",
        ReadOnly:    true,
    }

    assert.Equal(t, "config", setting.Source)
    assert.True(t, setting.ReadOnly)
}

func TestSystemSetting_SourceAndReadOnly_Defaults(t *testing.T) {
    setting := SystemSetting{
        SettingKey:  "test.key",
        Value:       "test-value",
        SettingType: SystemSettingTypeString,
    }

    // Zero values
    assert.Equal(t, "", setting.Source)
    assert.False(t, setting.ReadOnly)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestSystemSetting_SourceAndReadOnly`
Expected: FAIL — `Source` and `ReadOnly` fields don't exist yet

- [ ] **Step 3: Add fields to SystemSetting model**

In `api/models/system_setting.go`, add after the `ModifiedBy` field:

```go
// Source indicates where the effective value comes from: "database", "config", "environment", "vault"
// This is computed at response time and not stored in the database.
Source string `gorm:"-" json:"source"`
// ReadOnly indicates whether this setting can be modified via the API.
// True when source is not "database". Computed at response time.
ReadOnly bool `gorm:"-" json:"read_only"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestSystemSetting_SourceAndReadOnly`
Expected: PASS

- [ ] **Step 5: Run full unit tests to check for regressions**

Run: `make test-unit`
Expected: PASS (gorm:"-" means no DB schema change)

- [ ] **Step 6: Commit**

```bash
git add api/models/system_setting.go api/models/system_setting_test.go
git commit -m "feat(models): add Source and ReadOnly fields to SystemSetting (gorm:- non-persisted)"
```

### Task 3: Expand MigratableSetting Struct

**Files:**
- Modify: `internal/config/migratable_settings.go:7-13` (MigratableSetting struct)
- Modify: `api/server.go:62-67` (api.MigratableSetting struct)
- Modify: `api/config_provider_adapter.go:16-28` (adapter mapping)

- [ ] **Step 1: Add Secret and Source fields to config.MigratableSetting**

In `internal/config/migratable_settings.go`, update the struct:

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

- [ ] **Step 2: Add Secret and Source fields to api.MigratableSetting**

In `api/server.go`, update the struct:

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

- [ ] **Step 3: Update ConfigProviderAdapter to pass through new fields**

In `api/config_provider_adapter.go`, update the mapping:

```go
func (a *ConfigProviderAdapter) GetMigratableSettings() []MigratableSetting {
    configSettings := a.cfg.GetMigratableSettings()
    settings := make([]MigratableSetting, len(configSettings))
    for i, s := range configSettings {
        settings[i] = MigratableSetting{
            Key:         s.Key,
            Value:       s.Value,
            Type:        s.Type,
            Description: s.Description,
            Secret:      s.Secret,
            Source:      s.Source,
        }
    }
    return settings
}
```

- [ ] **Step 4: Update existing getMigratable* methods to set Source**

In `internal/config/migratable_settings.go`, update existing settings to include `Source: "config"`. For example in `getMigratableFeatureFlags`:

```go
func (c *Config) getMigratableFeatureFlags() []MigratableSetting {
    return []MigratableSetting{
        {
            Key:         "features.saml_enabled",
            Value:       strconv.FormatBool(c.Auth.SAML.Enabled),
            Type:        "bool",
            Description: "Enable SAML authentication",
            Source:      "config",
        },
    }
}
```

Apply the same pattern to all existing settings in:
- `getMigratableFeatureFlags`
- `getMigratableOAuthSettings` / `getMigratableOAuthProviderSettings`
- `getMigratableSAMLSettings` / `getMigratableSAMLProviderSettings`
- `getMigratableRuntimeSettings`

For each setting, check if the corresponding env var is set using `os.Getenv`. If so, set `Source: "environment"` instead. Use the env var names from the struct tags in `config.go`. Example:

```go
source := "config"
if os.Getenv("TMI_SAML_ENABLED") != "" {
    source = "environment"
}
```

Remove the `"(from config)"` suffix from all descriptions — the `source` field now conveys this.

- [ ] **Step 5: Update test mocks**

Update `MockConfigProvider` in `api/config_handlers_test.go:385-392` and `api/settings_service_test.go` to include the new fields:

```go
type MockConfigProvider struct {
    settings []MigratableSetting
}

func (m *MockConfigProvider) GetMigratableSettings() []MigratableSetting {
    return m.settings
}
```

Update all test data that creates `MigratableSetting` to include `Source: "config"` (and optionally `Secret: false`).

- [ ] **Step 6: Build and test**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/migratable_settings.go api/server.go api/config_provider_adapter.go api/config_handlers_test.go api/settings_service_test.go
git commit -m "feat(config): add Secret and Source fields to MigratableSetting"
```

## Chunk 2: URL Sanitization and Expanded Config Exposure

### Task 4: URL Sanitization Utility

**Files:**
- Create: `internal/config/sanitize_url.go`
- Create: `internal/config/sanitize_url_test.go`

- [ ] **Step 1: Write tests for URL sanitization**

Create `internal/config/sanitize_url_test.go`:

```go
package config

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestSanitizeURL_PostgresWithPassword(t *testing.T) {
    result := sanitizeURL("postgres://tmi_dev:dev123@localhost:5432/tmi_dev?sslmode=disable")
    assert.Equal(t, "postgres://tmi_dev:****@localhost:5432/tmi_dev?sslmode=disable", result)
}

func TestSanitizeURL_RedisWithPassword(t *testing.T) {
    result := sanitizeURL("redis://:secretpass@redis.example.com:6379/0")
    assert.Equal(t, "redis://:****@redis.example.com:6379/0", result)
}

func TestSanitizeURL_NoPassword(t *testing.T) {
    result := sanitizeURL("postgres://tmi_dev@localhost:5432/tmi_dev")
    assert.Equal(t, "postgres://tmi_dev@localhost:5432/tmi_dev", result)
}

func TestSanitizeURL_NoUserInfo(t *testing.T) {
    result := sanitizeURL("postgres://localhost:5432/tmi_dev")
    assert.Equal(t, "postgres://localhost:5432/tmi_dev", result)
}

func TestSanitizeURL_EmptyString(t *testing.T) {
    result := sanitizeURL("")
    assert.Equal(t, "", result)
}

func TestSanitizeURL_BareHostPort(t *testing.T) {
    // No scheme — not a real URL, pass through unchanged
    result := sanitizeURL("myhost:6379")
    assert.Equal(t, "myhost:6379", result)
}

func TestSanitizeURL_InvalidURL(t *testing.T) {
    result := sanitizeURL("://bad-url")
    assert.Equal(t, "<invalid URL>", result)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestSanitizeURL`
Expected: FAIL — function doesn't exist

- [ ] **Step 3: Implement sanitizeURL**

Create `internal/config/sanitize_url.go`:

```go
package config

import (
    "net/url"
)

// sanitizeURL parses a URL and replaces the password with "****".
// Returns the original string for empty input or bare host:port (no scheme).
// Returns "<invalid URL>" if parsing fails with a scheme present.
func sanitizeURL(rawURL string) string {
    if rawURL == "" {
        return ""
    }

    parsed, err := url.Parse(rawURL)
    if err != nil {
        return "<invalid URL>"
    }

    // If no scheme was detected, this is likely a bare host:port — pass through
    if parsed.Scheme == "" || parsed.Host == "" {
        return rawURL
    }

    // If there's a password, replace it
    if parsed.User != nil {
        if _, hasPassword := parsed.User.Password(); hasPassword {
            parsed.User = url.UserPassword(parsed.User.Username(), "****")
        }
    }

    return parsed.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestSanitizeURL`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/sanitize_url.go internal/config/sanitize_url_test.go
git commit -m "feat(config): add URL sanitization utility for database connection strings"
```

### Task 5: Expand GetMigratableSettings — Server, Auth, JWT, Cookie

**Files:**
- Modify: `internal/config/migratable_settings.go`
- Create: `internal/config/migratable_settings_test.go`

- [ ] **Step 1: Write tests for new server settings**

Create `internal/config/migratable_settings_test.go`:

```go
package config

import (
    "os"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestGetMigratableSettings_IncludesServerSettings(t *testing.T) {
    cfg := &Config{
        Server: ServerConfig{
            Port:      "8080",
            Interface: "localhost",
        },
    }
    settings := cfg.GetMigratableSettings()
    found := findSetting(settings, "server.port")
    require.NotNil(t, found)
    assert.Equal(t, "8080", found.Value)
    assert.Equal(t, "string", found.Type)
    assert.Equal(t, "config", found.Source)
    assert.False(t, found.Secret)
}

func TestGetMigratableSettings_IncludesAuthFlags(t *testing.T) {
    cfg := &Config{
        Auth: AuthConfig{
            AutoPromoteFirstUser: true,
            EveryoneIsAReviewer:  false,
            BuildMode:            "dev",
        },
    }
    settings := cfg.GetMigratableSettings()

    found := findSetting(settings, "auth.auto_promote_first_user")
    require.NotNil(t, found)
    assert.Equal(t, "true", found.Value)
    assert.Equal(t, "bool", found.Type)

    found = findSetting(settings, "auth.build_mode")
    require.NotNil(t, found)
    assert.Equal(t, "dev", found.Value)
}

func TestGetMigratableSettings_JWTSecretMasked(t *testing.T) {
    cfg := &Config{
        Auth: AuthConfig{
            JWT: JWTConfig{
                Secret: "super-secret",
            },
        },
    }
    settings := cfg.GetMigratableSettings()
    found := findSetting(settings, "auth.jwt.secret")
    require.NotNil(t, found)
    assert.True(t, found.Secret)
}

func TestGetMigratableSettings_EnvironmentSource(t *testing.T) {
    t.Setenv("TMI_SERVER_PORT", "9090")
    cfg := &Config{
        Server: ServerConfig{
            Port: "9090",
        },
    }
    settings := cfg.GetMigratableSettings()
    found := findSetting(settings, "server.port")
    require.NotNil(t, found)
    assert.Equal(t, "environment", found.Source)
}

func TestGetMigratableSettings_DatabaseURLSanitized(t *testing.T) {
    cfg := &Config{
        Database: DatabaseConfig{
            URL: "postgres://user:secret@localhost:5432/db",
        },
    }
    settings := cfg.GetMigratableSettings()
    found := findSetting(settings, "database.url")
    require.NotNil(t, found)
    assert.Equal(t, "postgres://user:****@localhost:5432/db", found.Value)
    assert.False(t, found.Secret)
}

func TestGetMigratableSettings_OAuthClientSecretMasked(t *testing.T) {
    cfg := &Config{
        Auth: AuthConfig{
            OAuth: OAuthConfig{
                Providers: map[string]OAuthProviderConfig{
                    "github": {Enabled: true, ClientSecret: "ghsecret"},
                },
            },
        },
    }
    settings := cfg.GetMigratableSettings()
    found := findSetting(settings, "auth.oauth.providers.github.client_secret")
    require.NotNil(t, found)
    assert.True(t, found.Secret)
}

// findSetting is a test helper to find a setting by key
func findSetting(settings []MigratableSetting, key string) *MigratableSetting {
    for i := range settings {
        if settings[i].Key == key {
            return &settings[i]
        }
    }
    return nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestGetMigratableSettings`
Expected: FAIL — new settings don't exist yet

- [ ] **Step 3: Add helper function for source detection**

Add to `internal/config/migratable_settings.go`:

```go
// settingSource returns "environment" if the given env var is set, otherwise "config".
func settingSource(envVar string) string {
    if os.Getenv(envVar) != "" {
        return "environment"
    }
    return "config"
}
```

Add `"os"` to the imports.

- [ ] **Step 4: Add getMigratableServerSettings**

Add to `internal/config/migratable_settings.go`:

```go
func (c *Config) getMigratableServerSettings() []MigratableSetting {
    settings := []MigratableSetting{
        {Key: "server.port", Value: c.Server.Port, Type: "string", Description: "HTTP server port", Source: settingSource("TMI_SERVER_PORT")},
        {Key: "server.interface", Value: c.Server.Interface, Type: "string", Description: "Network interface to bind to", Source: settingSource("TMI_SERVER_INTERFACE")},
        {Key: "server.tls_enabled", Value: strconv.FormatBool(c.Server.TLSEnabled), Type: "bool", Description: "TLS enabled", Source: settingSource("TMI_SERVER_TLS_ENABLED")},
        {Key: "server.tls_subject_name", Value: c.Server.TLSSubjectName, Type: "string", Description: "TLS certificate subject name", Source: settingSource("TMI_SERVER_TLS_SUBJECT_NAME")},
        {Key: "server.http_to_https_redirect", Value: strconv.FormatBool(c.Server.HTTPToHTTPSRedirect), Type: "bool", Description: "HTTP to HTTPS redirect", Source: settingSource("TMI_SERVER_HTTP_TO_HTTPS_REDIRECT")},
        {Key: "server.read_timeout", Value: c.Server.ReadTimeout.String(), Type: "string", Description: "HTTP read timeout", Source: settingSource("TMI_SERVER_READ_TIMEOUT")},
        {Key: "server.write_timeout", Value: c.Server.WriteTimeout.String(), Type: "string", Description: "HTTP write timeout", Source: settingSource("TMI_SERVER_WRITE_TIMEOUT")},
        {Key: "server.idle_timeout", Value: c.Server.IdleTimeout.String(), Type: "string", Description: "HTTP idle timeout", Source: settingSource("TMI_SERVER_IDLE_TIMEOUT")},
    }

    if c.Server.BaseURL != "" {
        settings = append(settings, MigratableSetting{Key: "server.base_url", Value: c.Server.BaseURL, Type: "string", Description: "Public base URL for callbacks", Source: settingSource("TMI_SERVER_BASE_URL")})
    }

    // TLS file paths — not secret, but only relevant when TLS is enabled
    if c.Server.TLSEnabled {
        settings = append(settings,
            MigratableSetting{Key: "server.tls_cert_file", Value: c.Server.TLSCertFile, Type: "string", Description: "TLS certificate file path", Source: settingSource("TMI_SERVER_TLS_CERT_FILE")},
            MigratableSetting{Key: "server.tls_key_file", Value: c.Server.TLSKeyFile, Type: "string", Description: "TLS key file path", Source: settingSource("TMI_SERVER_TLS_KEY_FILE")},
        )
    }

    // CORS
    if len(c.Server.CORS.AllowedOrigins) > 0 {
        originsJSON, _ := json.Marshal(c.Server.CORS.AllowedOrigins)
        settings = append(settings, MigratableSetting{Key: "server.cors.allowed_origins", Value: string(originsJSON), Type: "json", Description: "CORS allowed origins", Source: settingSource("TMI_CORS_ALLOWED_ORIGINS")})
    }

    return settings
}
```

Add `"encoding/json"` to the imports.

- [ ] **Step 5: Add getMigratableAuthSettings**

```go
func (c *Config) getMigratableAuthSettings() []MigratableSetting {
    settings := []MigratableSetting{
        {Key: "auth.build_mode", Value: c.Auth.BuildMode, Type: "string", Description: "Build mode (dev, test, production)", Source: settingSource("TMI_BUILD_MODE")},
        {Key: "auth.auto_promote_first_user", Value: strconv.FormatBool(c.Auth.AutoPromoteFirstUser), Type: "bool", Description: "Auto-promote first user to admin", Source: settingSource("TMI_AUTH_AUTO_PROMOTE_FIRST_USER")},
        {Key: "auth.everyone_is_a_reviewer", Value: strconv.FormatBool(c.Auth.EveryoneIsAReviewer), Type: "bool", Description: "Auto-add all users to Security Reviewers group", Source: settingSource("TMI_AUTH_EVERYONE_IS_A_REVIEWER")},
    }

    // JWT settings
    settings = append(settings,
        MigratableSetting{Key: "auth.jwt.secret", Value: c.Auth.JWT.Secret, Type: "string", Description: "JWT signing secret", Source: settingSource("TMI_JWT_SECRET"), Secret: true},
        MigratableSetting{Key: "auth.jwt.expiration_seconds", Value: strconv.Itoa(c.Auth.JWT.ExpirationSeconds), Type: "int", Description: "JWT token expiration in seconds", Source: settingSource("TMI_JWT_EXPIRATION_SECONDS")},
        MigratableSetting{Key: "auth.jwt.signing_method", Value: c.Auth.JWT.SigningMethod, Type: "string", Description: "JWT signing method", Source: settingSource("TMI_JWT_SIGNING_METHOD")},
        MigratableSetting{Key: "auth.jwt.refresh_token_days", Value: strconv.Itoa(c.Auth.JWT.RefreshTokenDays), Type: "int", Description: "Refresh token TTL in days", Source: settingSource("TMI_REFRESH_TOKEN_DAYS")},
        MigratableSetting{Key: "auth.jwt.session_lifetime_days", Value: strconv.Itoa(c.Auth.JWT.SessionLifetimeDays), Type: "int", Description: "Absolute session lifetime in days", Source: settingSource("TMI_SESSION_LIFETIME_DAYS")},
    )

    // Cookie settings
    settings = append(settings,
        MigratableSetting{Key: "auth.cookie.enabled", Value: strconv.FormatBool(c.Auth.Cookie.Enabled), Type: "bool", Description: "HttpOnly cookie-based auth enabled", Source: settingSource("TMI_COOKIE_ENABLED")},
        MigratableSetting{Key: "auth.cookie.domain", Value: c.Auth.Cookie.Domain, Type: "string", Description: "Cookie domain", Source: settingSource("TMI_COOKIE_DOMAIN")},
        MigratableSetting{Key: "auth.cookie.secure", Value: strconv.FormatBool(c.Auth.Cookie.Secure), Type: "bool", Description: "Require HTTPS for cookies", Source: settingSource("TMI_COOKIE_SECURE")},
    )

    return settings
}
```

- [ ] **Step 6: Add getMigratableDatabaseSettings**

```go
func (c *Config) getMigratableDatabaseSettings() []MigratableSetting {
    settings := []MigratableSetting{
        {Key: "database.url", Value: sanitizeURL(c.Database.URL), Type: "string", Description: "Database connection URL (password redacted)", Source: settingSource("TMI_DATABASE_URL")},
    }

    // Connection pool
    settings = append(settings,
        MigratableSetting{Key: "database.connection_pool.max_open_conns", Value: strconv.Itoa(c.Database.ConnectionPool.MaxOpenConns), Type: "int", Description: "Maximum open database connections", Source: settingSource("TMI_DB_MAX_OPEN_CONNS")},
        MigratableSetting{Key: "database.connection_pool.max_idle_conns", Value: strconv.Itoa(c.Database.ConnectionPool.MaxIdleConns), Type: "int", Description: "Maximum idle database connections", Source: settingSource("TMI_DB_MAX_IDLE_CONNS")},
        MigratableSetting{Key: "database.connection_pool.conn_max_lifetime", Value: strconv.Itoa(c.Database.ConnectionPool.ConnMaxLifetime), Type: "int", Description: "Max connection lifetime in seconds", Source: settingSource("TMI_DB_CONN_MAX_LIFETIME")},
        MigratableSetting{Key: "database.connection_pool.conn_max_idle_time", Value: strconv.Itoa(c.Database.ConnectionPool.ConnMaxIdleTime), Type: "int", Description: "Max connection idle time in seconds", Source: settingSource("TMI_DB_CONN_MAX_IDLE_TIME")},
    )

    // Redis
    if c.Database.Redis.URL != "" {
        settings = append(settings, MigratableSetting{Key: "database.redis.url", Value: sanitizeURL(c.Database.Redis.URL), Type: "string", Description: "Redis connection URL (password redacted)", Source: settingSource("TMI_REDIS_URL")})
    }
    settings = append(settings,
        MigratableSetting{Key: "database.redis.host", Value: c.Database.Redis.Host, Type: "string", Description: "Redis host", Source: settingSource("TMI_REDIS_HOST")},
        MigratableSetting{Key: "database.redis.port", Value: c.Database.Redis.Port, Type: "string", Description: "Redis port", Source: settingSource("TMI_REDIS_PORT")},
        MigratableSetting{Key: "database.redis.password", Value: c.Database.Redis.Password, Type: "string", Description: "Redis password", Source: settingSource("TMI_REDIS_PASSWORD"), Secret: true},
        MigratableSetting{Key: "database.redis.db", Value: strconv.Itoa(c.Database.Redis.DB), Type: "int", Description: "Redis database number", Source: settingSource("TMI_REDIS_DB")},
    )

    return settings
}
```

- [ ] **Step 7: Add getMigratableLoggingSettings**

```go
func (c *Config) getMigratableLoggingSettings() []MigratableSetting {
    return []MigratableSetting{
        {Key: "logging.level", Value: c.Logging.Level, Type: "string", Description: "Log level", Source: settingSource("TMI_LOG_LEVEL")},
        {Key: "logging.is_dev", Value: strconv.FormatBool(c.Logging.IsDev), Type: "bool", Description: "Development mode logging", Source: settingSource("TMI_LOG_IS_DEV")},
        {Key: "logging.is_test", Value: strconv.FormatBool(c.Logging.IsTest), Type: "bool", Description: "Test mode logging", Source: settingSource("TMI_LOG_IS_TEST")},
        {Key: "logging.log_dir", Value: c.Logging.LogDir, Type: "string", Description: "Log directory", Source: settingSource("TMI_LOG_DIR")},
        {Key: "logging.max_age_days", Value: strconv.Itoa(c.Logging.MaxAgeDays), Type: "int", Description: "Log max age in days", Source: settingSource("TMI_LOG_MAX_AGE_DAYS")},
        {Key: "logging.max_size_mb", Value: strconv.Itoa(c.Logging.MaxSizeMB), Type: "int", Description: "Log max size in MB", Source: settingSource("TMI_LOG_MAX_SIZE_MB")},
        {Key: "logging.max_backups", Value: strconv.Itoa(c.Logging.MaxBackups), Type: "int", Description: "Log max backup count", Source: settingSource("TMI_LOG_MAX_BACKUPS")},
        {Key: "logging.also_log_to_console", Value: strconv.FormatBool(c.Logging.AlsoLogToConsole), Type: "bool", Description: "Also log to console", Source: settingSource("TMI_LOG_ALSO_LOG_TO_CONSOLE")},
        {Key: "logging.log_api_requests", Value: strconv.FormatBool(c.Logging.LogAPIRequests), Type: "bool", Description: "Log API requests", Source: settingSource("TMI_LOG_API_REQUESTS")},
        {Key: "logging.log_api_responses", Value: strconv.FormatBool(c.Logging.LogAPIResponses), Type: "bool", Description: "Log API responses", Source: settingSource("TMI_LOG_API_RESPONSES")},
        {Key: "logging.log_websocket_messages", Value: strconv.FormatBool(c.Logging.LogWebSocketMsg), Type: "bool", Description: "Log WebSocket messages", Source: settingSource("TMI_LOG_WEBSOCKET_MESSAGES")},
        {Key: "logging.redact_auth_tokens", Value: strconv.FormatBool(c.Logging.RedactAuthTokens), Type: "bool", Description: "Redact auth tokens in logs", Source: settingSource("TMI_LOG_REDACT_AUTH_TOKENS")},
        {Key: "logging.suppress_unauthenticated_logs", Value: strconv.FormatBool(c.Logging.SuppressUnauthenticatedLogs), Type: "bool", Description: "Suppress unauthenticated request logs", Source: settingSource("TMI_LOG_SUPPRESS_UNAUTH_LOGS")},
    }
}
```

- [ ] **Step 8: Add getMigratableSecretsSettings and getMigratableAdministratorsSettings**

```go
func (c *Config) getMigratableSecretsSettings() []MigratableSetting {
    settings := []MigratableSetting{
        {Key: "secrets.provider", Value: c.Secrets.Provider, Type: "string", Description: "Secret provider type", Source: settingSource("TMI_SECRETS_PROVIDER")},
    }

    // Non-secret provider config
    stringFields := []struct {
        key, value, env, desc string
    }{
        {"secrets.vault_address", c.Secrets.VaultAddress, "TMI_VAULT_ADDRESS", "HashiCorp Vault address"},
        {"secrets.vault_path", c.Secrets.VaultPath, "TMI_VAULT_PATH", "HashiCorp Vault path"},
        {"secrets.aws_region", c.Secrets.AWSRegion, "TMI_AWS_REGION", "AWS region"},
        {"secrets.aws_secret_name", c.Secrets.AWSSecretName, "TMI_AWS_SECRET_NAME", "AWS secret name"},
        {"secrets.azure_vault_url", c.Secrets.AzureVaultURL, "TMI_AZURE_VAULT_URL", "Azure Key Vault URL"},
        {"secrets.gcp_project_id", c.Secrets.GCPProjectID, "TMI_GCP_PROJECT_ID", "GCP project ID"},
        {"secrets.gcp_secret_name", c.Secrets.GCPSecretName, "TMI_GCP_SECRET_NAME", "GCP secret name"},
        {"secrets.oci_compartment_id", c.Secrets.OCICompartmentID, "TMI_OCI_COMPARTMENT_ID", "OCI compartment ID"},
        {"secrets.oci_vault_id", c.Secrets.OCIVaultID, "TMI_OCI_VAULT_ID", "OCI vault ID"},
        {"secrets.oci_secret_name", c.Secrets.OCISecretName, "TMI_OCI_SECRET_NAME", "OCI secret name"},
    }
    for _, f := range stringFields {
        if f.value != "" {
            settings = append(settings, MigratableSetting{Key: f.key, Value: f.value, Type: "string", Description: f.desc, Source: settingSource(f.env)})
        }
    }

    // Secret: vault token
    settings = append(settings, MigratableSetting{Key: "secrets.vault_token", Value: c.Secrets.VaultToken, Type: "string", Description: "HashiCorp Vault token", Source: settingSource("TMI_VAULT_TOKEN"), Secret: true})

    return settings
}

func (c *Config) getMigratableAdministratorsSettings() []MigratableSetting {
    if len(c.Administrators) == 0 {
        return nil
    }
    adminsJSON, err := json.Marshal(c.Administrators)
    if err != nil {
        return nil
    }
    return []MigratableSetting{
        {Key: "administrators", Value: string(adminsJSON), Type: "json", Description: "Configured administrators", Source: "config"},
    }
}
```

- [ ] **Step 9: Update GetMigratableSettings to call new methods**

Replace the body of `GetMigratableSettings`:

```go
func (c *Config) GetMigratableSettings() []MigratableSetting {
    settings := []MigratableSetting{}

    settings = append(settings, c.getMigratableServerSettings()...)
    settings = append(settings, c.getMigratableDatabaseSettings()...)
    settings = append(settings, c.getMigratableAuthSettings()...)
    settings = append(settings, c.getMigratableFeatureFlags()...)
    settings = append(settings, c.getMigratableOAuthSettings()...)
    settings = append(settings, c.getMigratableSAMLSettings()...)
    settings = append(settings, c.getMigratableRuntimeSettings()...)
    settings = append(settings, c.getMigratableLoggingSettings()...)
    settings = append(settings, c.getMigratableSecretsSettings()...)
    settings = append(settings, c.getMigratableAdministratorsSettings()...)

    return settings
}
```

Also update `getMigratableRuntimeSettings` to remove settings that are now covered by other methods (e.g., `operator.*` stays there, but `session.timeout_minutes` and `websocket.inactivity_timeout_seconds` can stay too since they use different keys than the JWT settings).

- [ ] **Step 10: Update existing OAuth/SAML provider methods to include secret fields**

In `getMigratableOAuthProviderSettings`, add the client_secret as a secret and scopes/userinfo as JSON. Per-provider settings don't have individual env var overrides — they are always `"config"` sourced:

```go
// Client secret — masked in API responses
settings = append(settings, MigratableSetting{
    Key: prefix + ".client_secret", Value: p.ClientSecret, Type: "string",
    Description: "OAuth client secret", Source: "config", Secret: true,
})

// Scopes as JSON array
if len(p.Scopes) > 0 {
    scopesJSON, _ := json.Marshal(p.Scopes)
    settings = append(settings, MigratableSetting{
        Key: prefix + ".scopes", Value: string(scopesJSON), Type: "json",
        Description: "OAuth scopes", Source: "config",
    })
}

// UserInfo endpoints as JSON
if len(p.UserInfo) > 0 {
    userInfoJSON, _ := json.Marshal(p.UserInfo)
    settings = append(settings, MigratableSetting{
        Key: prefix + ".userinfo", Value: string(userInfoJSON), Type: "json",
        Description: "OAuth userinfo endpoints", Source: "config",
    })
}

// Auth header format and accept header
if p.AuthHeaderFormat != "" {
    settings = append(settings, MigratableSetting{
        Key: prefix + ".auth_header_format", Value: p.AuthHeaderFormat, Type: "string",
        Description: "OAuth auth header format", Source: "config",
    })
}
if p.AcceptHeader != "" {
    settings = append(settings, MigratableSetting{
        Key: prefix + ".accept_header", Value: p.AcceptHeader, Type: "string",
        Description: "OAuth accept header", Source: "config",
    })
}
```

In `getMigratableSAMLProviderSettings`, add secret fields, attribute_mapping, and group settings. Per-provider SAML settings are also always `"config"` sourced:

```go
// Secret fields
settings = append(settings,
    MigratableSetting{Key: prefix + ".sp_private_key", Value: p.SPPrivateKey, Type: "string", Description: "SAML SP private key", Source: "config", Secret: true},
    MigratableSetting{Key: prefix + ".sp_certificate", Value: p.SPCertificate, Type: "string", Description: "SAML SP certificate", Source: "config", Secret: true},
    MigratableSetting{Key: prefix + ".idp_metadata_b64xml", Value: p.IDPMetadataB64XML, Type: "string", Description: "IdP metadata (base64 XML)", Source: "config", Secret: true},
)

// Encrypt assertions flag
settings = append(settings, MigratableSetting{
    Key: prefix + ".encrypt_assertions", Value: strconv.FormatBool(p.EncryptAssertions),
    Type: "bool", Description: "Require encrypted SAML assertions", Source: "config",
})

// Attribute mapping as JSON
if len(p.AttributeMapping) > 0 {
    mapJSON, _ := json.Marshal(p.AttributeMapping)
    settings = append(settings, MigratableSetting{
        Key: prefix + ".attribute_mapping", Value: string(mapJSON), Type: "json",
        Description: "SAML attribute mapping", Source: "config",
    })
}

// Group settings
if p.GroupAttributeName != "" {
    settings = append(settings, MigratableSetting{
        Key: prefix + ".group_attribute_name", Value: p.GroupAttributeName, Type: "string",
        Description: "SAML group attribute name", Source: "config",
    })
}
if p.GroupPrefix != "" {
    settings = append(settings, MigratableSetting{
        Key: prefix + ".group_prefix", Value: p.GroupPrefix, Type: "string",
        Description: "SAML group prefix filter", Source: "config",
    })
}
```

**Note:** The `EncryptAssertions`, `AttributeMapping`, `GroupAttributeName`, and `GroupPrefix` fields are on `SAMLProviderConfig` in `auth/saml/config.go`, not on `internal/config.SAMLProviderConfig`. If these fields are not accessible from the config package, you will need to add them to `internal/config.SAMLProviderConfig` first or skip them and file a follow-up issue.

- [ ] **Step 11: Run tests**

Run: `make test-unit name=TestGetMigratableSettings`
Expected: PASS

- [ ] **Step 12: Run full unit tests and lint**

Run: `make lint && make build-server && make test-unit`
Expected: PASS

- [ ] **Step 13: Commit**

```bash
git add internal/config/migratable_settings.go internal/config/migratable_settings_test.go
git commit -m "feat(config): expand GetMigratableSettings to expose all configuration"
```

## Chunk 3: Handler Layer — Merge, Enrich, and Protect

**Prerequisites:** Task 0 (interface extraction) must be complete before this chunk. Tests in this chunk assign `MockSettingsService` to `Server.settingsService`, which requires the interface type.

### Task 6: Update ListSystemSettings Handler with Merge Logic

**Files:**
- Modify: `api/config_handlers.go:143-188`
- Modify: `api/config_handlers.go:594-611` (modelToAPISystemSetting)

- [ ] **Step 1: Write test for merged list with source and read_only**

Add to `api/config_handlers_test.go`:

```go
func TestListSystemSettings_MergedWithConfigSettings(t *testing.T) {
    originalAdminStore := GlobalGroupMemberStore
    defer restoreConfigStores(originalAdminStore)

    gin.SetMode(gin.TestMode)
    r := gin.New()

    mockSettings := NewMockSettingsService()
    mockSettings.AddSetting("rate_limit.requests_per_minute", "100", "int")

    server := &Server{
        settingsService: mockSettings,
        configProvider: &MockConfigProvider{
            settings: []MigratableSetting{
                {Key: "server.port", Value: "8080", Type: "string", Description: "HTTP port", Source: "config"},
                {Key: "rate_limit.requests_per_minute", Value: "200", Type: "int", Description: "Rate limit from config", Source: "config"},
            },
        },
    }

    GlobalGroupMemberStore = &mockGroupMemberStoreForAdmin{isAdminResult: true}
    userUUID := uuid.New()

    r.GET("/admin/settings", func(c *gin.Context) {
        c.Set("userInternalUUID", userUUID.String())
        server.ListSystemSettings(c)
    })

    req, _ := http.NewRequest("GET", "/admin/settings", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)

    var settings []map[string]interface{}
    err := json.Unmarshal(w.Body.Bytes(), &settings)
    require.NoError(t, err)

    // Should have both config-only and database settings
    assert.GreaterOrEqual(t, len(settings), 2)

    // Config setting should have source=config, read_only=true
    var serverPort map[string]interface{}
    for _, s := range settings {
        if s["key"] == "server.port" {
            serverPort = s
            break
        }
    }
    require.NotNil(t, serverPort)
    assert.Equal(t, "config", serverPort["source"])
    assert.Equal(t, true, serverPort["read_only"])

    // Config-overridden DB setting should show config value
    var rateLimit map[string]interface{}
    for _, s := range settings {
        if s["key"] == "rate_limit.requests_per_minute" {
            rateLimit = s
            break
        }
    }
    require.NotNil(t, rateLimit)
    assert.Equal(t, "200", rateLimit["value"]) // config wins
    assert.Equal(t, "config", rateLimit["source"])
    assert.Equal(t, true, rateLimit["read_only"])
}

func TestListSystemSettings_SecretMasking(t *testing.T) {
    originalAdminStore := GlobalGroupMemberStore
    defer restoreConfigStores(originalAdminStore)

    gin.SetMode(gin.TestMode)
    r := gin.New()

    server := &Server{
        settingsService: NewMockSettingsService(),
        configProvider: &MockConfigProvider{
            settings: []MigratableSetting{
                {Key: "auth.jwt.secret", Value: "super-secret", Type: "string", Description: "JWT secret", Source: "config", Secret: true},
                {Key: "auth.jwt.empty_secret", Value: "", Type: "string", Description: "Empty secret", Source: "config", Secret: true},
            },
        },
    }

    GlobalGroupMemberStore = &mockGroupMemberStoreForAdmin{isAdminResult: true}
    userUUID := uuid.New()

    r.GET("/admin/settings", func(c *gin.Context) {
        c.Set("userInternalUUID", userUUID.String())
        server.ListSystemSettings(c)
    })

    req, _ := http.NewRequest("GET", "/admin/settings", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)

    var settings []map[string]interface{}
    err := json.Unmarshal(w.Body.Bytes(), &settings)
    require.NoError(t, err)

    for _, s := range settings {
        if s["key"] == "auth.jwt.secret" {
            assert.Equal(t, "<configured>", s["value"])
        }
        if s["key"] == "auth.jwt.empty_secret" {
            assert.Equal(t, "<not configured>", s["value"])
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestListSystemSettings_Merged`
Expected: FAIL — merge logic not implemented yet

- [ ] **Step 3: Add helper function to build merged settings list**

Add to `api/config_handlers.go`:

```go
// mergeSettingsWithConfig builds a merged view of database and config settings.
// Config settings take priority over database settings for the same key.
// Returns the merged list sorted by key.
func (s *Server) mergeSettingsWithConfig(dbSettings []models.SystemSetting) []SystemSetting {
    // Build map of config settings
    configMap := make(map[string]MigratableSetting)
    if s.configProvider != nil {
        for _, cs := range s.configProvider.GetMigratableSettings() {
            configMap[cs.Key] = cs
        }
    }

    // Build map of DB settings
    dbMap := make(map[string]models.SystemSetting)
    for _, ds := range dbSettings {
        dbMap[ds.SettingKey] = ds
    }

    // Collect all unique keys
    allKeys := make(map[string]bool)
    for k := range configMap {
        allKeys[k] = true
    }
    for k := range dbMap {
        allKeys[k] = true
    }

    // Build merged list
    result := make([]SystemSetting, 0, len(allKeys))
    for key := range allKeys {
        configSetting, inConfig := configMap[key]
        dbSetting, inDB := dbMap[key]

        var apiSetting SystemSetting
        if inConfig {
            // Config wins — use config value
            apiSetting = configSettingToAPI(configSetting)
        } else if inDB {
            // DB only
            apiSetting = modelToAPISystemSetting(dbSetting)
            source := SystemSettingSource("database")
            apiSetting.Source = &source
            readOnly := false
            apiSetting.ReadOnly = &readOnly
        }
        result = append(result, apiSetting)
    }

    // Sort by key
    sort.Slice(result, func(i, j int) bool {
        return result[i].Key < result[j].Key
    })

    return result
}

// configSettingToAPI converts a MigratableSetting to an API SystemSetting with source/read_only.
func configSettingToAPI(cs MigratableSetting) SystemSetting {
    source := SystemSettingSource(cs.Source)
    readOnly := true

    value := cs.Value
    if cs.Secret {
        if value != "" {
            value = "<configured>"
        } else {
            value = "<not configured>"
        }
    }

    setting := SystemSetting{
        Key:      cs.Key,
        Value:    value,
        Type:     SystemSettingType(cs.Type),
        Source:   &source,
        ReadOnly: &readOnly,
    }
    if cs.Description != "" {
        setting.Description = &cs.Description
    }
    return setting
}
```

Add `"sort"` to the imports.

- [ ] **Step 4: Update modelToAPISystemSetting to include source/read_only**

Update in `api/config_handlers.go`:

```go
func modelToAPISystemSetting(m models.SystemSetting) SystemSetting {
    setting := SystemSetting{
        Key:        m.SettingKey,
        Value:      m.Value,
        Type:       SystemSettingType(m.SettingType),
        ModifiedAt: &m.ModifiedAt,
    }
    if m.Description != nil {
        setting.Description = m.Description
    }
    if m.ModifiedBy != nil {
        if parsedUUID, err := uuid.Parse(*m.ModifiedBy); err == nil {
            setting.ModifiedBy = &parsedUUID
        }
    }
    // Set source and read_only from model (if populated)
    if m.Source != "" {
        source := SystemSettingSource(m.Source)
        setting.Source = &source
        readOnly := m.ReadOnly
        setting.ReadOnly = &readOnly
    }
    return setting
}
```

- [ ] **Step 5: Update ListSystemSettings handler**

Replace the body of `ListSystemSettings` (after the admin/service checks) with:

```go
    settings, err := s.settingsService.List(ctx)
    if err != nil {
        logger.Error("Failed to list system settings: %v", err)
        HandleRequestError(c, &RequestError{
            Status:  http.StatusInternalServerError,
            Code:    "internal_error",
            Message: "Failed to list settings",
        })
        return
    }

    // Merge database settings with config settings
    response := s.mergeSettingsWithConfig(settings)

    logger.Info("Listed %d system settings (merged)", len(response))
    c.JSON(http.StatusOK, response)
```

- [ ] **Step 6: Run tests**

Run: `make test-unit name=TestListSystemSettings`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go
git commit -m "feat(api): merge config settings into ListSystemSettings with source/read_only"
```

### Task 7: Update GetSystemSetting Handler with Config Lookup

**Files:**
- Modify: `api/config_handlers.go:191-252`

- [ ] **Step 1: Write test for getting a config-sourced setting**

Add to `api/config_handlers_test.go`:

```go
func TestGetSystemSetting_ConfigSourced(t *testing.T) {
    originalAdminStore := GlobalGroupMemberStore
    defer restoreConfigStores(originalAdminStore)

    gin.SetMode(gin.TestMode)
    r := gin.New()

    server := &Server{
        settingsService: NewMockSettingsService(),
        configProvider: &MockConfigProvider{
            settings: []MigratableSetting{
                {Key: "server.port", Value: "8080", Type: "string", Description: "HTTP port", Source: "config"},
            },
        },
    }

    GlobalGroupMemberStore = &mockGroupMemberStoreForAdmin{isAdminResult: true}
    userUUID := uuid.New()

    r.GET("/admin/settings/:key", func(c *gin.Context) {
        c.Set("userInternalUUID", userUUID.String())
        key := c.Param("key")
        server.GetSystemSetting(c, key)
    })

    req, _ := http.NewRequest("GET", "/admin/settings/server.port", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)

    var setting map[string]interface{}
    err := json.Unmarshal(w.Body.Bytes(), &setting)
    require.NoError(t, err)

    assert.Equal(t, "server.port", setting["key"])
    assert.Equal(t, "8080", setting["value"])
    assert.Equal(t, "config", setting["source"])
    assert.Equal(t, true, setting["read_only"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestGetSystemSetting_ConfigSourced`
Expected: FAIL — returns 404 (config settings not checked)

- [ ] **Step 3: Update GetSystemSetting handler**

In the `GetSystemSetting` handler, after the service nil check, replace the database lookup with:

```go
    // Check config provider first
    if s.configProvider != nil {
        for _, cs := range s.configProvider.GetMigratableSettings() {
            if cs.Key == key {
                c.JSON(http.StatusOK, configSettingToAPI(cs))
                return
            }
        }
    }

    // Fall back to database
    setting, err := s.settingsService.Get(ctx, key)
    if err != nil {
        logger.Error("Failed to get system setting %s: %v", key, err)
        HandleRequestError(c, &RequestError{
            Status:  http.StatusInternalServerError,
            Code:    "internal_error",
            Message: "Failed to get setting",
        })
        return
    }

    if setting == nil {
        logger.Debug("System setting not found: %s", key)
        HandleRequestError(c, &RequestError{
            Status:  http.StatusNotFound,
            Code:    "not_found",
            Message: "Setting not found",
        })
        return
    }

    apiSetting := modelToAPISystemSetting(*setting)
    source := SystemSettingSource("database")
    apiSetting.Source = &source
    readOnly := false
    apiSetting.ReadOnly = &readOnly
    c.JSON(http.StatusOK, apiSetting)
```

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestGetSystemSetting`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go
git commit -m "feat(api): add config provider lookup to GetSystemSetting"
```

### Task 8: Add 409 Conflict Guard to UpdateSystemSetting

**Files:**
- Modify: `api/config_handlers.go:254-336`

- [ ] **Step 1: Write test for 409 on config-sourced setting**

Add to `api/config_handlers_test.go`:

```go
func TestUpdateSystemSetting_409_ConfigSourced(t *testing.T) {
    originalAdminStore := GlobalGroupMemberStore
    defer restoreConfigStores(originalAdminStore)

    gin.SetMode(gin.TestMode)
    r := gin.New()

    server := &Server{
        settingsService: NewMockSettingsService(),
        configProvider: &MockConfigProvider{
            settings: []MigratableSetting{
                {Key: "server.port", Value: "8080", Type: "string", Source: "config"},
            },
        },
    }

    GlobalGroupMemberStore = &mockGroupMemberStoreForAdmin{isAdminResult: true}
    userUUID := uuid.New()

    r.PUT("/admin/settings/:key", func(c *gin.Context) {
        c.Set("userInternalUUID", userUUID.String())
        key := c.Param("key")
        server.UpdateSystemSetting(c, key)
    })

    body := `{"value": "9090", "type": "string"}`
    req, _ := http.NewRequest("PUT", "/admin/settings/server.port", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    assert.Equal(t, http.StatusConflict, w.Code)

    var errResp map[string]interface{}
    err := json.Unmarshal(w.Body.Bytes(), &errResp)
    require.NoError(t, err)
    assert.Equal(t, "conflict", errResp["error"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestUpdateSystemSetting_409`
Expected: FAIL — returns 200 (no guard yet)

- [ ] **Step 3: Add config source guard to UpdateSystemSetting**

In `UpdateSystemSetting`, after the service nil check and before parsing the request body, add:

```go
    // Check if setting is controlled by config/environment (cannot be modified via API)
    if s.configProvider != nil {
        for _, cs := range s.configProvider.GetMigratableSettings() {
            if cs.Key == key {
                logger.Warn("Attempted to update config-controlled setting: %s (source: %s)", key, cs.Source)
                HandleRequestError(c, &RequestError{
                    Status:  http.StatusConflict,
                    Code:    "conflict",
                    Message: "Setting '" + key + "' is controlled by " + cs.Source + " and cannot be modified via the API",
                })
                return
            }
        }
    }
```

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestUpdateSystemSetting`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go
git commit -m "feat(api): reject PUT to config/environment-controlled settings with 409"
```

### Task 9: Update DeleteSystemSetting Handler

**Files:**
- Modify: `api/config_handlers.go:339-411`

- [ ] **Step 1: Write tests for delete behavior**

Add to `api/config_handlers_test.go`:

```go
func TestDeleteSystemSetting_404_ConfigOnlyNoDB(t *testing.T) {
    originalAdminStore := GlobalGroupMemberStore
    defer restoreConfigStores(originalAdminStore)

    gin.SetMode(gin.TestMode)
    r := gin.New()

    server := &Server{
        settingsService: NewMockSettingsService(), // no DB entry for server.port
        configProvider: &MockConfigProvider{
            settings: []MigratableSetting{
                {Key: "server.port", Value: "8080", Type: "string", Source: "config"},
            },
        },
    }

    GlobalGroupMemberStore = &mockGroupMemberStoreForAdmin{isAdminResult: true}
    userUUID := uuid.New()

    r.DELETE("/admin/settings/:key", func(c *gin.Context) {
        c.Set("userInternalUUID", userUUID.String())
        key := c.Param("key")
        server.DeleteSystemSetting(c, key)
    })

    req, _ := http.NewRequest("DELETE", "/admin/settings/server.port", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    // Config-only setting with no DB row → 404
    assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteSystemSetting_AllowDeleteDualSource(t *testing.T) {
    originalAdminStore := GlobalGroupMemberStore
    defer restoreConfigStores(originalAdminStore)

    gin.SetMode(gin.TestMode)
    r := gin.New()

    mockSettings := NewMockSettingsService()
    mockSettings.AddSetting("server.port", "9090", "string") // DB has override

    server := &Server{
        settingsService: mockSettings,
        configProvider: &MockConfigProvider{
            settings: []MigratableSetting{
                {Key: "server.port", Value: "8080", Type: "string", Source: "config"},
            },
        },
    }

    GlobalGroupMemberStore = &mockGroupMemberStoreForAdmin{isAdminResult: true}
    userUUID := uuid.New()

    r.DELETE("/admin/settings/:key", func(c *gin.Context) {
        c.Set("userInternalUUID", userUUID.String())
        key := c.Param("key")
        server.DeleteSystemSetting(c, key)
    })

    req, _ := http.NewRequest("DELETE", "/admin/settings/server.port", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    // Dual-source: allow delete of DB row (config value becomes effective)
    assert.Equal(t, http.StatusNoContent, w.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestDeleteSystemSetting_404_ConfigOnly`
Expected: FAIL

- [ ] **Step 3: Update DeleteSystemSetting handler**

Replace the existing existence check in `DeleteSystemSetting` with logic that checks the database directly:

```go
    // Check if setting exists in database (we only delete DB rows)
    setting, err := s.settingsService.Get(ctx, key)
    if err != nil {
        logger.Error("Failed to check system setting %s: %v", key, err)
        HandleRequestError(c, &RequestError{
            Status:  http.StatusInternalServerError,
            Code:    "internal_error",
            Message: "Failed to check setting",
        })
        return
    }

    if setting == nil {
        // No DB row — even if it exists in config, there's nothing to delete
        logger.Debug("System setting not found in database for deletion: %s", key)
        HandleRequestError(c, &RequestError{
            Status:  http.StatusNotFound,
            Code:    "not_found",
            Message: "Setting not found in database",
        })
        return
    }

    // Delete the DB row
    if err := s.settingsService.Delete(ctx, key); err != nil {
        logger.Error("Failed to delete system setting %s: %v", key, err)
        HandleRequestError(c, &RequestError{
            Status:  http.StatusInternalServerError,
            Code:    "internal_error",
            Message: "Failed to delete setting",
        })
        return
    }

    logger.Info("Deleted system setting: %s", key)
    c.Status(http.StatusNoContent)
```

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestDeleteSystemSetting`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go
git commit -m "feat(api): update DeleteSystemSetting for config-aware behavior"
```

## Chunk 4: Final Integration and Verification

### Task 10: Full Build, Lint, and Test

**Files:** All modified files

- [ ] **Step 1: Lint**

Run: `make lint`
Expected: PASS (fix any issues)

- [ ] **Step 2: Build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 3: Unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 4: Integration tests**

Run: `make test-integration`
Expected: PASS

- [ ] **Step 5: Commit any remaining fixes**

Only if there are actual fixes:
```bash
git add <specific files that were fixed>
git commit -m "fix: address lint and test issues from settings source/read_only feature"
```

### Task 11: Update OpenAPI Validation and Verify

- [ ] **Step 1: Validate OpenAPI spec**

Run: `make validate-openapi`
Expected: PASS

- [ ] **Step 2: Verify generated code matches**

Run: `make generate-api`
Verify no diff: `git diff api/api.go` should show no changes

- [ ] **Step 3: Final commit if needed**

```bash
git status
# If clean, no commit needed
```
