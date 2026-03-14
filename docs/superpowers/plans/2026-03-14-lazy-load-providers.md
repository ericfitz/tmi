# Lazy Load OAuth/SAML Providers from Database — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow OAuth/SAML providers to be configured at runtime via the database settings API, in addition to the existing config file and environment variable sources.

**Architecture:** A `ProviderRegistry` in the `auth` package provides unified provider lookup, merging immutable config/env providers with mutable DB-sourced providers assembled from `system_settings` rows. The registry uses lazy cache invalidation with TTL safety net. The `api.SettingsService` satisfies a minimal `ProviderSettingsReader` interface defined in `auth` to avoid circular imports.

**Tech Stack:** Go, GORM, Redis (optional), testify

**Spec:** `docs/superpowers/specs/2026-03-14-lazy-load-providers-design.md`

---

## Chunk 1: ProviderRegistry Core

### Task 1: Add `ListByPrefix` to SettingsServiceInterface and implement

**Files:**
- Modify: `api/server.go:16-26` (add method to interface)
- Modify: `api/settings_service.go` (add implementation)
- Modify: `api/config_handlers_test.go:19-88` (add method to MockSettingsService)
- Test: `api/settings_service_test.go`

- [ ] **Step 1: Write failing test for ListByPrefix**

In `api/settings_service_test.go`, add:

```go
func TestSettingsService_ListByPrefix(t *testing.T) {
	service := &SettingsService{
		memCache:    make(map[string]settingsCacheEntry),
		memCacheTTL: 60 * time.Second,
		useMemCache: true,
	}

	t.Run("returns empty slice when no matches", func(t *testing.T) {
		// No GORM DB = no results
		result, err := service.ListByPrefix(context.Background(), "auth.oauth.providers.")
		assert.NoError(t, err)
		assert.Empty(t, result)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestSettingsService_ListByPrefix`
Expected: FAIL — `ListByPrefix` not defined

- [ ] **Step 3: Add ListByPrefix to SettingsServiceInterface**

In `api/server.go`, add to the `SettingsServiceInterface` interface (after line 24, before the closing brace):

```go
	ListByPrefix(ctx context.Context, prefix string) ([]models.SystemSetting, error)
```

- [ ] **Step 4: Implement ListByPrefix on SettingsService**

In `api/settings_service.go`, add after the `List` method (after line 289):

```go
// ListByPrefix returns all database settings whose key starts with the given prefix.
// Results are decrypted if an encryptor is configured.
func (s *SettingsService) ListByPrefix(ctx context.Context, prefix string) ([]models.SystemSetting, error) {
	if s.gormDB == nil {
		return nil, nil
	}

	var settings []models.SystemSetting
	result := s.gormDB.WithContext(ctx).Where("setting_key LIKE ?", prefix+"%").Find(&settings)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to list settings by prefix %q: %w", prefix, result.Error)
	}

	// Decrypt values if encryptor is configured
	if s.encryptor != nil {
		for i := range settings {
			decrypted, err := s.encryptor.Decrypt(settings[i].Value)
			if err != nil {
				// Value may not be encrypted (e.g., migrated from before encryption was enabled)
				continue
			}
			settings[i].Value = decrypted
		}
	}

	return settings, nil
}
```

- [ ] **Step 5: Add ListByPrefix to MockSettingsService**

In `api/config_handlers_test.go`, add to `MockSettingsService` (after line 80):

```go
func (m *MockSettingsService) ListByPrefix(ctx context.Context, prefix string) ([]models.SystemSetting, error) {
	result := make([]models.SystemSetting, 0)
	for _, s := range m.settings {
		if strings.HasPrefix(s.SettingKey, prefix) {
			result = append(result, *s)
		}
	}
	return result, nil
}
```

Ensure `"strings"` is in the import block (it already is at line 8).

- [ ] **Step 6: Run tests to verify they pass**

Run: `make test-unit name=TestSettingsService_ListByPrefix`
Expected: PASS

- [ ] **Step 7: Run all settings tests to check for regressions**

Run: `make test-unit name=TestSettingsService`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add api/server.go api/settings_service.go api/config_handlers_test.go api/settings_service_test.go
git commit -m "feat(api): add ListByPrefix to SettingsService for prefix-based key scanning"
```

---

### Task 2: Create ProviderRegistry interface and types in auth package

**Files:**
- Create: `auth/provider_registry.go`
- Test: `auth/provider_registry_test.go`

- [ ] **Step 1: Write failing test for ProviderRegistry interface**

Create `auth/provider_registry_test.go`:

```go
package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockProviderSettingsReader implements ProviderSettingsReader for testing
type mockProviderSettingsReader struct {
	settings []ProviderSetting
}

func (m *mockProviderSettingsReader) ListByPrefix(ctx context.Context, prefix string) ([]ProviderSetting, error) {
	var result []ProviderSetting
	for _, s := range m.settings {
		if len(s.Key) >= len(prefix) && s.Key[:len(prefix)] == prefix {
			result = append(result, s)
		}
	}
	return result, nil
}

func TestNewDefaultProviderRegistry(t *testing.T) {
	configOAuth := map[string]OAuthProviderConfig{
		"google": {
			ID:      "google",
			Name:    "Google",
			Enabled: true,
		},
	}
	configSAML := map[string]SAMLProviderConfig{}
	reader := &mockProviderSettingsReader{}

	registry := NewDefaultProviderRegistry(configOAuth, configSAML, reader)
	assert.NotNil(t, registry)

	// Config providers should be immediately available
	providers := registry.GetEnabledOAuthProviders()
	assert.Len(t, providers, 1)
	assert.Equal(t, "Google", providers["google"].Name)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestNewDefaultProviderRegistry`
Expected: FAIL — types not defined

- [ ] **Step 3: Create provider_registry.go with interface and types**

Create `auth/provider_registry.go`:

```go
package auth

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// providerIDPattern validates provider IDs: lowercase alphanumeric and hyphens
var providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ProviderSettingsReader is a minimal interface defined in the auth package
// to avoid a circular dependency on the api package. The api.SettingsService
// satisfies this interface via the ProviderSettingsReaderAdapter.
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

// ProviderRegistry provides unified access to OAuth and SAML provider
// configurations from all sources (config, environment, database).
type ProviderRegistry interface {
	// GetOAuthProvider returns an OAuth provider by ID regardless of enabled state.
	GetOAuthProvider(id string) (OAuthProviderConfig, bool)

	// GetEnabledOAuthProviders returns all enabled OAuth providers.
	GetEnabledOAuthProviders() map[string]OAuthProviderConfig

	// GetSAMLProvider returns a SAML provider by ID regardless of enabled state.
	GetSAMLProvider(id string) (SAMLProviderConfig, bool)

	// GetEnabledSAMLProviders returns all enabled SAML providers.
	GetEnabledSAMLProviders() map[string]SAMLProviderConfig

	// InvalidateCache marks the DB provider cache as dirty.
	InvalidateCache()
}

// DefaultProviderRegistry merges immutable config/env providers with
// mutable database-sourced providers assembled from system_settings rows.
type DefaultProviderRegistry struct {
	// Immutable: loaded once at startup from config/env
	configOAuth map[string]OAuthProviderConfig
	configSAML  map[string]SAMLProviderConfig

	// Database-sourced providers (cached, refreshable)
	dbOAuth     map[string]OAuthProviderConfig
	dbSAML      map[string]SAMLProviderConfig
	dbCacheMu   sync.RWMutex
	dbCacheTime time.Time
	cacheTTL    time.Duration
	dirty       bool

	// Settings reader for DB provider keys
	settings ProviderSettingsReader
}

// DefaultProviderCacheTTL is the default cache TTL for DB-sourced providers.
const DefaultProviderCacheTTL = 60 * time.Second

// NewDefaultProviderRegistry creates a new registry with config-sourced providers
// and a settings reader for database-sourced providers.
func NewDefaultProviderRegistry(
	configOAuth map[string]OAuthProviderConfig,
	configSAML map[string]SAMLProviderConfig,
	settings ProviderSettingsReader,
) *DefaultProviderRegistry {
	// Deep copy config maps so they are truly immutable
	oauth := make(map[string]OAuthProviderConfig, len(configOAuth))
	for k, v := range configOAuth {
		oauth[k] = v
	}
	saml := make(map[string]SAMLProviderConfig, len(configSAML))
	for k, v := range configSAML {
		saml[k] = v
	}

	return &DefaultProviderRegistry{
		configOAuth: oauth,
		configSAML:  saml,
		dbOAuth:     make(map[string]OAuthProviderConfig),
		dbSAML:      make(map[string]SAMLProviderConfig),
		cacheTTL:    DefaultProviderCacheTTL,
		dirty:       true, // Force initial load
		settings:    settings,
	}
}

// GetOAuthProvider returns an OAuth provider by ID regardless of enabled state.
// Config providers take precedence over DB providers.
func (r *DefaultProviderRegistry) GetOAuthProvider(id string) (OAuthProviderConfig, bool) {
	// Check config first (immutable, no lock needed)
	if p, ok := r.configOAuth[id]; ok {
		return p, true
	}

	// Check DB providers (may need refresh)
	r.ensureDBCacheFresh()
	r.dbCacheMu.RLock()
	defer r.dbCacheMu.RUnlock()
	p, ok := r.dbOAuth[id]
	return p, ok
}

// GetEnabledOAuthProviders returns all enabled OAuth providers from all sources.
func (r *DefaultProviderRegistry) GetEnabledOAuthProviders() map[string]OAuthProviderConfig {
	r.ensureDBCacheFresh()

	result := make(map[string]OAuthProviderConfig)

	// Add enabled config providers
	for id, p := range r.configOAuth {
		if p.Enabled {
			result[id] = p
		}
	}

	// Add enabled DB providers (skip if ID exists in config)
	r.dbCacheMu.RLock()
	defer r.dbCacheMu.RUnlock()
	for id, p := range r.dbOAuth {
		if _, inConfig := r.configOAuth[id]; inConfig {
			continue
		}
		if p.Enabled {
			result[id] = p
		}
	}

	return result
}

// GetSAMLProvider returns a SAML provider by ID regardless of enabled state.
func (r *DefaultProviderRegistry) GetSAMLProvider(id string) (SAMLProviderConfig, bool) {
	if p, ok := r.configSAML[id]; ok {
		return p, true
	}

	r.ensureDBCacheFresh()
	r.dbCacheMu.RLock()
	defer r.dbCacheMu.RUnlock()
	p, ok := r.dbSAML[id]
	return p, ok
}

// GetEnabledSAMLProviders returns all enabled SAML providers from all sources.
func (r *DefaultProviderRegistry) GetEnabledSAMLProviders() map[string]SAMLProviderConfig {
	r.ensureDBCacheFresh()

	result := make(map[string]SAMLProviderConfig)

	for id, p := range r.configSAML {
		if p.Enabled {
			result[id] = p
		}
	}

	r.dbCacheMu.RLock()
	defer r.dbCacheMu.RUnlock()
	for id, p := range r.dbSAML {
		if _, inConfig := r.configSAML[id]; inConfig {
			continue
		}
		if p.Enabled {
			result[id] = p
		}
	}

	return result
}

// InvalidateCache marks the DB provider cache as dirty. The next Get* call
// will trigger a refresh from the database.
func (r *DefaultProviderRegistry) InvalidateCache() {
	r.dbCacheMu.Lock()
	defer r.dbCacheMu.Unlock()
	r.dirty = true
}

// ensureDBCacheFresh checks if the DB cache needs refreshing and does so if needed.
// Uses double-checked locking to avoid redundant refreshes.
func (r *DefaultProviderRegistry) ensureDBCacheFresh() {
	r.dbCacheMu.RLock()
	needsRefresh := r.dirty || time.Since(r.dbCacheTime) > r.cacheTTL
	r.dbCacheMu.RUnlock()

	if !needsRefresh {
		return
	}

	r.dbCacheMu.Lock()
	defer r.dbCacheMu.Unlock()

	// Double-check after acquiring write lock
	if !r.dirty && time.Since(r.dbCacheTime) <= r.cacheTTL {
		return
	}

	r.refreshDBProviders()
}

// refreshDBProviders loads providers from database settings. Caller must hold write lock.
func (r *DefaultProviderRegistry) refreshDBProviders() {
	logger := slogging.Get()
	ctx := context.Background()

	// Load OAuth providers from DB
	oauthSettings, err := r.settings.ListByPrefix(ctx, "auth.oauth.providers.")
	if err != nil {
		logger.Error("Failed to load OAuth providers from database: %v", err)
		return
	}
	r.dbOAuth = AssembleOAuthProviders(oauthSettings)

	// Load SAML providers from DB
	samlSettings, err := r.settings.ListByPrefix(ctx, "auth.saml.providers.")
	if err != nil {
		logger.Error("Failed to load SAML providers from database: %v", err)
		return
	}
	r.dbSAML = AssembleSAMLProviders(samlSettings)

	r.dbCacheTime = time.Now()
	r.dirty = false

	if len(r.dbOAuth) > 0 || len(r.dbSAML) > 0 {
		logger.Info("Loaded %d OAuth and %d SAML providers from database",
			len(r.dbOAuth), len(r.dbSAML))
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestNewDefaultProviderRegistry`
Expected: FAIL — `assembleOAuthProviders` and `assembleSAMLProviders` not defined yet. That's expected; we'll implement those in Task 3.

- [ ] **Step 5: Add stub assembly functions to unblock compilation**

Add to `auth/provider_registry.go`:

```go
// AssembleOAuthProviders groups settings by provider ID and assembles OAuthProviderConfig structs.
// Exported so the api package can use it for enable-validation.
func AssembleOAuthProviders(settings []ProviderSetting) map[string]OAuthProviderConfig {
	// Stub — implemented in Task 3
	return make(map[string]OAuthProviderConfig)
}

// AssembleSAMLProviders groups settings by provider ID and assembles SAMLProviderConfig structs.
// Exported so the api package can use it for enable-validation.
func AssembleSAMLProviders(settings []ProviderSetting) map[string]SAMLProviderConfig {
	// Stub — implemented in Task 3
	return make(map[string]SAMLProviderConfig)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `make test-unit name=TestNewDefaultProviderRegistry`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add auth/provider_registry.go auth/provider_registry_test.go
git commit -m "feat(auth): add ProviderRegistry interface and DefaultProviderRegistry skeleton"
```

---

### Task 3: Implement provider assembly from settings keys

**Files:**
- Modify: `auth/provider_registry.go` (replace stub assembly functions)
- Test: `auth/provider_registry_test.go`

- [ ] **Step 1: Write failing tests for OAuth provider assembly**

Add to `auth/provider_registry_test.go`:

```go
func TestAssembleOAuthProviders(t *testing.T) {
	t.Run("assembles complete provider from settings", func(t *testing.T) {
		settings := []ProviderSetting{
			{Key: "auth.oauth.providers.azure.client_id", Value: "azure-client-123"},
			{Key: "auth.oauth.providers.azure.client_secret", Value: "secret-456"},
			{Key: "auth.oauth.providers.azure.authorization_url", Value: "https://login.microsoft.com/authorize"},
			{Key: "auth.oauth.providers.azure.token_url", Value: "https://login.microsoft.com/token"},
			{Key: "auth.oauth.providers.azure.enabled", Value: "true"},
			{Key: "auth.oauth.providers.azure.name", Value: "Azure AD"},
			{Key: "auth.oauth.providers.azure.icon", Value: "fa-brands fa-microsoft"},
			{Key: "auth.oauth.providers.azure.issuer", Value: "https://login.microsoft.com"},
			{Key: "auth.oauth.providers.azure.jwks_url", Value: "https://login.microsoft.com/jwks"},
			{Key: "auth.oauth.providers.azure.auth_header_format", Value: "Bearer %s"},
			{Key: "auth.oauth.providers.azure.accept_header", Value: "application/json"},
			{Key: "auth.oauth.providers.azure.scopes", Value: `["openid","profile","email"]`},
			{Key: "auth.oauth.providers.azure.userinfo", Value: `[{"url":"https://graph.microsoft.com/me","claims":["email","name"]}]`},
		}

		providers := AssembleOAuthProviders(settings)
		assert.Len(t, providers, 1)

		p, ok := providers["azure"]
		require.True(t, ok)
		assert.Equal(t, "azure", p.ID)
		assert.Equal(t, "azure-client-123", p.ClientID)
		assert.Equal(t, "secret-456", p.ClientSecret)
		assert.Equal(t, "https://login.microsoft.com/authorize", p.AuthorizationURL)
		assert.Equal(t, "https://login.microsoft.com/token", p.TokenURL)
		assert.True(t, p.Enabled)
		assert.Equal(t, "Azure AD", p.Name)
		assert.Equal(t, "fa-brands fa-microsoft", p.Icon)
		assert.Equal(t, "https://login.microsoft.com", p.Issuer)
		assert.Equal(t, "https://login.microsoft.com/jwks", p.JWKSURL)
		assert.Equal(t, "Bearer %s", p.AuthHeaderFormat)
		assert.Equal(t, "application/json", p.AcceptHeader)
		assert.Equal(t, []string{"openid", "profile", "email"}, p.Scopes)
		assert.Len(t, p.UserInfo, 1)
		assert.Equal(t, "https://graph.microsoft.com/me", p.UserInfo[0].URL)
	})

	t.Run("ignores invalid provider IDs", func(t *testing.T) {
		settings := []ProviderSetting{
			{Key: "auth.oauth.providers.INVALID!.client_id", Value: "test"},
		}
		providers := AssembleOAuthProviders(settings)
		assert.Empty(t, providers)
	})

	t.Run("ignores unrecognized field suffixes", func(t *testing.T) {
		settings := []ProviderSetting{
			{Key: "auth.oauth.providers.test.client_id", Value: "id"},
			{Key: "auth.oauth.providers.test.unknown_field", Value: "ignored"},
		}
		providers := AssembleOAuthProviders(settings)
		assert.Len(t, providers, 1)
		assert.Equal(t, "id", providers["test"].ClientID)
	})

	t.Run("assembles multiple providers", func(t *testing.T) {
		settings := []ProviderSetting{
			{Key: "auth.oauth.providers.google.client_id", Value: "g-id"},
			{Key: "auth.oauth.providers.google.enabled", Value: "true"},
			{Key: "auth.oauth.providers.github.client_id", Value: "gh-id"},
			{Key: "auth.oauth.providers.github.enabled", Value: "false"},
		}
		providers := AssembleOAuthProviders(settings)
		assert.Len(t, providers, 2)
		assert.True(t, providers["google"].Enabled)
		assert.False(t, providers["github"].Enabled)
	})
}

func TestAssembleSAMLProviders(t *testing.T) {
	t.Run("assembles complete SAML provider", func(t *testing.T) {
		settings := []ProviderSetting{
			{Key: "auth.saml.providers.entra.entity_id", Value: "https://tmi.example.com"},
			{Key: "auth.saml.providers.entra.metadata_url", Value: "https://login.microsoft.com/metadata"},
			{Key: "auth.saml.providers.entra.acs_url", Value: "https://tmi.example.com/saml/entra/acs"},
			{Key: "auth.saml.providers.entra.enabled", Value: "true"},
			{Key: "auth.saml.providers.entra.name", Value: "Entra ID"},
			{Key: "auth.saml.providers.entra.sign_requests", Value: "true"},
			{Key: "auth.saml.providers.entra.email_attribute", Value: "email"},
			{Key: "auth.saml.providers.entra.name_attribute", Value: "displayName"},
			{Key: "auth.saml.providers.entra.groups_attribute", Value: "groups"},
		}

		providers := AssembleSAMLProviders(settings)
		assert.Len(t, providers, 1)

		p, ok := providers["entra"]
		require.True(t, ok)
		assert.Equal(t, "entra", p.ID)
		assert.Equal(t, "https://tmi.example.com", p.EntityID)
		assert.Equal(t, "https://login.microsoft.com/metadata", p.MetadataURL)
		assert.True(t, p.Enabled)
		assert.True(t, p.SignRequests)
		assert.Equal(t, "email", p.EmailAttribute)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestAssembleOAuthProviders`
Expected: FAIL — stub `AssembleOAuthProviders` returns empty map

- [ ] **Step 3: Implement assembly functions**

Replace the stub functions in `auth/provider_registry.go` with the full implementation (keep them exported):

```go
const (
	oauthPrefix = "auth.oauth.providers."
	samlPrefix  = "auth.saml.providers."
)

// AssembleOAuthProviders groups settings by provider ID and assembles OAuthProviderConfig structs.
func AssembleOAuthProviders(settings []ProviderSetting) map[string]OAuthProviderConfig {
	logger := slogging.Get()

	// Group settings by provider ID
	grouped := groupSettingsByProvider(settings, oauthPrefix)

	providers := make(map[string]OAuthProviderConfig)
	for id, fields := range grouped {
		if !providerIDPattern.MatchString(id) {
			logger.Warn("Ignoring OAuth provider with invalid ID: %q", id)
			continue
		}

		p := OAuthProviderConfig{ID: id}
		for field, value := range fields {
			switch field {
			case "client_id":
				p.ClientID = value
			case "client_secret":
				p.ClientSecret = value
			case "authorization_url":
				p.AuthorizationURL = value
			case "token_url":
				p.TokenURL = value
			case "issuer":
				p.Issuer = value
			case "jwks_url":
				p.JWKSURL = value
			case "enabled":
				p.Enabled = value == "true"
			case "name":
				p.Name = value
			case "icon":
				p.Icon = value
			case "auth_header_format":
				p.AuthHeaderFormat = value
			case "accept_header":
				p.AcceptHeader = value
			case "scopes":
				var scopes []string
				if err := json.Unmarshal([]byte(value), &scopes); err != nil {
					logger.Warn("Failed to parse scopes for OAuth provider %q: %v", id, err)
				} else {
					p.Scopes = scopes
				}
			case "userinfo":
				var userInfo []UserInfoEndpoint
				if err := json.Unmarshal([]byte(value), &userInfo); err != nil {
					logger.Warn("Failed to parse userinfo for OAuth provider %q: %v", id, err)
				} else {
					p.UserInfo = userInfo
				}
			case "additional_params":
				var params map[string]string
				if err := json.Unmarshal([]byte(value), &params); err != nil {
					logger.Warn("Failed to parse additional_params for OAuth provider %q: %v", id, err)
				} else {
					p.AdditionalParams = params
				}
			default:
				logger.Debug("Ignoring unrecognized OAuth provider field %q.%q", id, field)
			}
		}
		providers[id] = p
	}

	return providers
}

// AssembleSAMLProviders groups settings by provider ID and assembles SAMLProviderConfig structs.
func AssembleSAMLProviders(settings []ProviderSetting) map[string]SAMLProviderConfig {
	logger := slogging.Get()

	grouped := groupSettingsByProvider(settings, samlPrefix)

	providers := make(map[string]SAMLProviderConfig)
	for id, fields := range grouped {
		if !providerIDPattern.MatchString(id) {
			logger.Warn("Ignoring SAML provider with invalid ID: %q", id)
			continue
		}

		p := SAMLProviderConfig{ID: id}
		for field, value := range fields {
			switch field {
			case "entity_id":
				p.EntityID = value
			case "metadata_url":
				p.MetadataURL = value
			case "metadata_xml":
				p.MetadataXML = value
			case "acs_url":
				p.ACSURL = value
			case "slo_url":
				p.SLOURL = value
			case "sp_private_key":
				p.SPPrivateKey = value
			case "sp_private_key_path":
				p.SPPrivateKeyPath = value
			case "sp_certificate":
				p.SPCertificate = value
			case "sp_certificate_path":
				p.SPCertificatePath = value
			case "idp_metadata_url":
				p.IDPMetadataURL = value
			case "idp_metadata_b64xml":
				p.IDPMetadataB64XML = value
			case "enabled":
				p.Enabled = value == "true"
			case "name":
				p.Name = value
			case "icon":
				p.Icon = value
			case "allow_idp_initiated":
				p.AllowIDPInitiated = value == "true"
			case "force_authn":
				p.ForceAuthn = value == "true"
			case "sign_requests":
				p.SignRequests = value == "true"
			case "name_id_attribute":
				p.NameIDAttribute = value
			case "email_attribute":
				p.EmailAttribute = value
			case "name_attribute":
				p.NameAttribute = value
			case "groups_attribute":
				p.GroupsAttribute = value
			default:
				logger.Debug("Ignoring unrecognized SAML provider field %q.%q", id, field)
			}
		}
		providers[id] = p
	}

	return providers
}

// groupSettingsByProvider parses settings keys of the form "<prefix><id>.<field>"
// and groups them into a map of id -> field -> value.
func groupSettingsByProvider(settings []ProviderSetting, prefix string) map[string]map[string]string {
	grouped := make(map[string]map[string]string)
	for _, s := range settings {
		// Strip prefix, leaving "<id>.<field>"
		remainder := s.Key[len(prefix):]
		dotIdx := strings.Index(remainder, ".")
		if dotIdx <= 0 {
			continue // No field part
		}
		id := remainder[:dotIdx]
		field := remainder[dotIdx+1:]
		if field == "" {
			continue
		}

		if _, ok := grouped[id]; !ok {
			grouped[id] = make(map[string]string)
		}
		grouped[id][field] = s.Value
	}
	return grouped
}
```

Add `"encoding/json"` and `"strings"` to the imports in `auth/provider_registry.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestAssembleOAuthProviders`
Then: `make test-unit name=TestAssembleSAMLProviders`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add auth/provider_registry.go auth/provider_registry_test.go
git commit -m "feat(auth): implement provider assembly from settings keys"
```

---

### Task 4: Add validation logic for enable gate

**Files:**
- Modify: `auth/provider_registry.go` (add validation functions)
- Test: `auth/provider_registry_test.go`

- [ ] **Step 1: Write failing tests for validation**

Add to `auth/provider_registry_test.go`:

```go
func TestValidateOAuthProvider(t *testing.T) {
	t.Run("valid provider passes", func(t *testing.T) {
		p := OAuthProviderConfig{
			ClientID:         "id",
			AuthorizationURL: "https://auth.example.com",
			TokenURL:         "https://token.example.com",
			UserInfo:         []UserInfoEndpoint{{URL: "https://info.example.com"}},
		}
		missing := ValidateOAuthProvider(p)
		assert.Empty(t, missing)
	})

	t.Run("missing required fields reported", func(t *testing.T) {
		p := OAuthProviderConfig{
			ClientID: "id",
			// Missing authorization_url, token_url, userinfo
		}
		missing := ValidateOAuthProvider(p)
		assert.Contains(t, missing, "authorization_url")
		assert.Contains(t, missing, "token_url")
		assert.Contains(t, missing, "userinfo")
		assert.NotContains(t, missing, "client_id")
	})

	t.Run("completely empty provider", func(t *testing.T) {
		missing := ValidateOAuthProvider(OAuthProviderConfig{})
		assert.Len(t, missing, 4) // client_id, authorization_url, token_url, userinfo
	})
}

func TestValidateSAMLProvider(t *testing.T) {
	t.Run("valid with metadata_url", func(t *testing.T) {
		p := SAMLProviderConfig{
			EntityID:    "https://tmi.example.com",
			MetadataURL: "https://idp.example.com/metadata",
		}
		missing := ValidateSAMLProvider(p)
		assert.Empty(t, missing)
	})

	t.Run("valid with idp_metadata_url", func(t *testing.T) {
		p := SAMLProviderConfig{
			EntityID:       "https://tmi.example.com",
			IDPMetadataURL: "https://idp.example.com/metadata",
		}
		missing := ValidateSAMLProvider(p)
		assert.Empty(t, missing)
	})

	t.Run("valid with idp_metadata_b64xml", func(t *testing.T) {
		p := SAMLProviderConfig{
			EntityID:          "https://tmi.example.com",
			IDPMetadataB64XML: "PHNhbWw=",
		}
		missing := ValidateSAMLProvider(p)
		assert.Empty(t, missing)
	})

	t.Run("missing entity_id and metadata", func(t *testing.T) {
		missing := ValidateSAMLProvider(SAMLProviderConfig{})
		assert.Contains(t, missing, "entity_id")
		assert.Contains(t, missing, "metadata_url or idp_metadata_url or idp_metadata_b64xml")
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestValidateOAuthProvider`
Expected: FAIL — `ValidateOAuthProvider` not defined

- [ ] **Step 3: Implement validation functions**

Add to `auth/provider_registry.go`:

```go
// ValidateOAuthProvider checks that required fields are present for an enabled OAuth provider.
// Returns a list of missing field names, or nil if valid.
func ValidateOAuthProvider(p OAuthProviderConfig) []string {
	var missing []string
	if p.ClientID == "" {
		missing = append(missing, "client_id")
	}
	if p.AuthorizationURL == "" {
		missing = append(missing, "authorization_url")
	}
	if p.TokenURL == "" {
		missing = append(missing, "token_url")
	}
	if len(p.UserInfo) == 0 {
		missing = append(missing, "userinfo")
	}
	return missing
}

// ValidateSAMLProvider checks that required fields are present for an enabled SAML provider.
// Returns a list of missing field names, or nil if valid.
func ValidateSAMLProvider(p SAMLProviderConfig) []string {
	var missing []string
	if p.EntityID == "" {
		missing = append(missing, "entity_id")
	}
	if p.MetadataURL == "" && p.IDPMetadataURL == "" && p.IDPMetadataB64XML == "" {
		missing = append(missing, "metadata_url or idp_metadata_url or idp_metadata_b64xml")
	}
	return missing
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestValidateOAuthProvider`
Then: `make test-unit name=TestValidateSAMLProvider`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add auth/provider_registry.go auth/provider_registry_test.go
git commit -m "feat(auth): add provider validation for enable gate"
```

---

### Task 5: Add ProviderSettingsReaderAdapter to bridge api → auth

**Files:**
- Create: `api/provider_settings_adapter.go`
- Test: `api/provider_settings_adapter_test.go`

- [ ] **Step 1: Write failing test**

Create `api/provider_settings_adapter_test.go`:

```go
package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check that adapter satisfies the auth interface
var _ auth.ProviderSettingsReader = (*ProviderSettingsReaderAdapter)(nil)

func TestProviderSettingsReaderAdapter(t *testing.T) {
	mock := NewMockSettingsService()
	mock.AddSetting("auth.oauth.providers.azure.client_id", "az-id", "string")
	mock.AddSetting("auth.oauth.providers.azure.enabled", "true", "bool")
	mock.AddSetting("rate_limit.requests_per_minute", "100", "int")

	adapter := NewProviderSettingsReaderAdapter(mock)

	t.Run("returns matching settings", func(t *testing.T) {
		result, err := adapter.ListByPrefix(context.Background(), "auth.oauth.providers.")
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("excludes non-matching settings", func(t *testing.T) {
		result, err := adapter.ListByPrefix(context.Background(), "rate_limit.")
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "rate_limit.requests_per_minute", result[0].Key)
	})

	t.Run("returns empty for no matches", func(t *testing.T) {
		result, err := adapter.ListByPrefix(context.Background(), "nonexistent.")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestProviderSettingsReaderAdapter`
Expected: FAIL — type not defined

- [ ] **Step 3: Implement the adapter**

Create `api/provider_settings_adapter.go`:

```go
package api

import (
	"context"

	"github.com/ericfitz/tmi/auth"
)

// ProviderSettingsReaderAdapter adapts SettingsServiceInterface to auth.ProviderSettingsReader.
// This avoids a circular import from auth to api.
type ProviderSettingsReaderAdapter struct {
	settings SettingsServiceInterface
}

// NewProviderSettingsReaderAdapter creates a new adapter.
func NewProviderSettingsReaderAdapter(settings SettingsServiceInterface) *ProviderSettingsReaderAdapter {
	return &ProviderSettingsReaderAdapter{settings: settings}
}

// ListByPrefix returns all settings whose key starts with the given prefix,
// converted to auth.ProviderSetting.
func (a *ProviderSettingsReaderAdapter) ListByPrefix(ctx context.Context, prefix string) ([]auth.ProviderSetting, error) {
	dbSettings, err := a.settings.ListByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}

	result := make([]auth.ProviderSetting, len(dbSettings))
	for i, s := range dbSettings {
		result[i] = auth.ProviderSetting{
			Key:   s.SettingKey,
			Value: s.Value,
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestProviderSettingsReaderAdapter`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/provider_settings_adapter.go api/provider_settings_adapter_test.go
git commit -m "feat(api): add ProviderSettingsReaderAdapter bridging api to auth.ProviderSettingsReader"
```

---

### Task 6: Add registry integration tests (cache invalidation, TTL, merge)

**Files:**
- Modify: `auth/provider_registry_test.go`

- [ ] **Step 1: Write tests for cache behavior and merge rules**

Add to `auth/provider_registry_test.go`:

```go
func TestProviderRegistry_ConfigPrecedence(t *testing.T) {
	configOAuth := map[string]OAuthProviderConfig{
		"google": {ID: "google", Name: "Config Google", Enabled: true, ClientID: "config-id"},
	}

	// DB has same provider ID — should be ignored
	reader := &mockProviderSettingsReader{
		settings: []ProviderSetting{
			{Key: "auth.oauth.providers.google.client_id", Value: "db-id"},
			{Key: "auth.oauth.providers.google.enabled", Value: "true"},
			{Key: "auth.oauth.providers.google.name", Value: "DB Google"},
			{Key: "auth.oauth.providers.azure.client_id", Value: "azure-id"},
			{Key: "auth.oauth.providers.azure.enabled", Value: "true"},
		},
	}

	registry := NewDefaultProviderRegistry(configOAuth, nil, reader)

	t.Run("config provider wins over DB for same ID", func(t *testing.T) {
		p, ok := registry.GetOAuthProvider("google")
		assert.True(t, ok)
		assert.Equal(t, "config-id", p.ClientID)
		assert.Equal(t, "Config Google", p.Name)
	})

	t.Run("DB provider available for new IDs", func(t *testing.T) {
		p, ok := registry.GetOAuthProvider("azure")
		assert.True(t, ok)
		assert.Equal(t, "azure-id", p.ClientID)
	})

	t.Run("GetEnabledOAuthProviders merges both sources", func(t *testing.T) {
		enabled := registry.GetEnabledOAuthProviders()
		assert.Len(t, enabled, 2)
		assert.Equal(t, "config-id", enabled["google"].ClientID) // Config wins
		assert.Equal(t, "azure-id", enabled["azure"].ClientID)   // DB fills gap
	})
}

func TestProviderRegistry_CacheInvalidation(t *testing.T) {
	reader := &mockProviderSettingsReader{
		settings: []ProviderSetting{
			{Key: "auth.oauth.providers.test.client_id", Value: "original"},
			{Key: "auth.oauth.providers.test.enabled", Value: "true"},
		},
	}

	registry := NewDefaultProviderRegistry(nil, nil, reader)

	// First read loads from DB
	p, ok := registry.GetOAuthProvider("test")
	assert.True(t, ok)
	assert.Equal(t, "original", p.ClientID)

	// Simulate settings update
	reader.settings = []ProviderSetting{
		{Key: "auth.oauth.providers.test.client_id", Value: "updated"},
		{Key: "auth.oauth.providers.test.enabled", Value: "true"},
	}

	// Without invalidation, still returns cached value
	p, _ = registry.GetOAuthProvider("test")
	assert.Equal(t, "original", p.ClientID)

	// After invalidation, next read gets fresh data
	registry.InvalidateCache()
	p, _ = registry.GetOAuthProvider("test")
	assert.Equal(t, "updated", p.ClientID)
}

func TestProviderRegistry_TTLExpiry(t *testing.T) {
	reader := &mockProviderSettingsReader{
		settings: []ProviderSetting{
			{Key: "auth.oauth.providers.test.client_id", Value: "v1"},
			{Key: "auth.oauth.providers.test.enabled", Value: "true"},
		},
	}

	registry := NewDefaultProviderRegistry(nil, nil, reader)
	registry.cacheTTL = 1 * time.Millisecond // Very short TTL for testing

	// First read
	p, _ := registry.GetOAuthProvider("test")
	assert.Equal(t, "v1", p.ClientID)

	// Update underlying data
	reader.settings = []ProviderSetting{
		{Key: "auth.oauth.providers.test.client_id", Value: "v2"},
		{Key: "auth.oauth.providers.test.enabled", Value: "true"},
	}

	// Wait for TTL to expire
	time.Sleep(5 * time.Millisecond)

	// Should get fresh data via TTL expiry
	p, _ = registry.GetOAuthProvider("test")
	assert.Equal(t, "v2", p.ClientID)
}

func TestProviderRegistry_DisabledNotInEnabled(t *testing.T) {
	reader := &mockProviderSettingsReader{
		settings: []ProviderSetting{
			{Key: "auth.oauth.providers.disabled.client_id", Value: "id"},
			{Key: "auth.oauth.providers.disabled.enabled", Value: "false"},
			{Key: "auth.oauth.providers.active.client_id", Value: "id2"},
			{Key: "auth.oauth.providers.active.enabled", Value: "true"},
		},
	}

	registry := NewDefaultProviderRegistry(nil, nil, reader)

	t.Run("disabled excluded from GetEnabledOAuthProviders", func(t *testing.T) {
		enabled := registry.GetEnabledOAuthProviders()
		assert.Len(t, enabled, 1)
		_, ok := enabled["disabled"]
		assert.False(t, ok)
	})

	t.Run("disabled still accessible via GetOAuthProvider", func(t *testing.T) {
		p, ok := registry.GetOAuthProvider("disabled")
		assert.True(t, ok)
		assert.False(t, p.Enabled)
	})
}
```

- [ ] **Step 2: Run tests**

Run: `make test-unit name=TestProviderRegistry`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add auth/provider_registry_test.go
git commit -m "test(auth): add registry integration tests for cache, merge, and TTL"
```

---

### Task 7: Lint and build check for Chunk 1

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: PASS (no new warnings from our code)

- [ ] **Step 2: Run build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 3: Run all unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 4: Fix any issues found, commit if needed**

---

## Chunk 2: Wire Registry into Auth Handlers

### Task 8: Add registry field and setter to Handlers and Service

**Files:**
- Modify: `auth/handlers.go:72-78` (add registry field)
- Modify: `auth/service.go:36-46` (add registry field)

- [ ] **Step 1: Add registry to Handlers struct**

In `auth/handlers.go`, add to the `Handlers` struct (after the `cookieOpts` field):

```go
	registry          ProviderRegistry
```

Add a setter method (after `Service()` at line 106):

```go
// SetProviderRegistry sets the provider registry for unified provider lookup.
func (h *Handlers) SetProviderRegistry(registry ProviderRegistry) {
	h.registry = registry
}
```

- [ ] **Step 2: Add registry to Service struct**

In `auth/service.go`, add to the `Service` struct (after `claimsEnricher`):

```go
	registry       ProviderRegistry
```

Add a setter method (after `SetClaimsEnricher`):

```go
// SetProviderRegistry sets the provider registry for unified provider lookup.
func (s *Service) SetProviderRegistry(registry ProviderRegistry) {
	s.registry = registry
}
```

- [ ] **Step 3: Build to verify compilation**

Run: `make build-server`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add auth/handlers.go auth/service.go
git commit -m "feat(auth): add ProviderRegistry field and setter to Handlers and Service"
```

---

### Task 9: Update handlers_providers.go to use registry

**Files:**
- Modify: `auth/handlers_providers.go`
- Test: `auth/handlers_test.go`

- [ ] **Step 1: Write test for GetProviders using registry**

Add to `auth/handlers_test.go`:

```go
func TestGetProvidersWithRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
		},
	}

	// Create a registry with providers (instead of putting them in config)
	configProviders := map[string]OAuthProviderConfig{
		"google": {
			ID:       "google",
			Name:     "Google",
			Enabled:  true,
			Icon:     "fa-brands fa-google",
			ClientID: "test-google-client-id",
		},
	}
	registry := NewDefaultProviderRegistry(configProviders, nil, &mockSettingsReader{})

	handlers := &Handlers{
		config:   config,
		registry: registry,
	}

	router.GET("/oauth2/providers", handlers.GetProviders)

	req := httptest.NewRequest("GET", "/oauth2/providers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string][]map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response["providers"], 1)
	assert.Equal(t, "google", response["providers"][0]["id"])
}

// mockSettingsReader is a no-op ProviderSettingsReader for handler tests
type mockSettingsReader struct{}

func (m *mockSettingsReader) ListByPrefix(ctx context.Context, prefix string) ([]ProviderSetting, error) {
	return nil, nil
}
```

Add `"context"` to imports if not already present.

- [ ] **Step 2: Update GetProviders to use registry**

In `auth/handlers_providers.go`, replace `GetProviders` (lines 35-75):

```go
// GetProviders returns the available OAuth providers
func (h *Handlers) GetProviders(c *gin.Context) {
	var enabledProviders map[string]OAuthProviderConfig

	if h.registry != nil {
		enabledProviders = h.registry.GetEnabledOAuthProviders()
	} else {
		// Fallback for tests or when registry not yet wired
		enabledProviders = make(map[string]OAuthProviderConfig)
		for id, p := range h.config.OAuth.Providers {
			if p.Enabled {
				enabledProviders[id] = p
			}
		}
	}

	providers := make([]ProviderInfo, 0, len(enabledProviders))

	for id, providerConfig := range enabledProviders {
		name := providerConfig.Name
		if name == "" {
			name = id
		}
		icon := providerConfig.Icon
		if icon == "" {
			icon = id
		}

		authURL := fmt.Sprintf("%s/oauth2/authorize?idp=%s", getBaseURL(c), id)
		tokenURL := fmt.Sprintf("%s/oauth2/token?idp=%s", getBaseURL(c), id)

		providers = append(providers, ProviderInfo{
			ID:          id,
			Name:        name,
			Icon:        icon,
			AuthURL:     authURL,
			TokenURL:    tokenURL,
			RedirectURI: h.config.OAuth.CallbackURL,
			ClientID:    providerConfig.ClientID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"providers": providers,
	})
}
```

- [ ] **Step 3: Update GetSAMLProviders to use registry**

In `auth/handlers_providers.go`, replace `GetSAMLProviders` (lines 78-135):

```go
// GetSAMLProviders returns the available SAML providers
func (h *Handlers) GetSAMLProviders(c *gin.Context) {
	// SAML global kill switch
	if !h.config.SAML.Enabled {
		c.JSON(http.StatusOK, gin.H{"providers": []SAMLProviderInfo{}})
		return
	}

	var samlManager *SAMLManager
	if h.service != nil {
		samlManager = h.service.GetSAMLManager()
	}

	var enabledProviders map[string]SAMLProviderConfig
	if h.registry != nil {
		enabledProviders = h.registry.GetEnabledSAMLProviders()
	} else {
		enabledProviders = make(map[string]SAMLProviderConfig)
		for id, p := range h.config.SAML.Providers {
			if p.Enabled {
				enabledProviders[id] = p
			}
		}
	}

	providers := make([]SAMLProviderInfo, 0, len(enabledProviders))
	baseURL := getBaseURL(c)

	for id, providerConfig := range enabledProviders {
		initialized := samlManager != nil && samlManager.IsProviderInitialized(id)

		name := providerConfig.Name
		if name == "" {
			name = id
		}
		icon := providerConfig.Icon
		if icon == "" {
			icon = "fa-solid fa-key"
		}

		authURL := fmt.Sprintf("%s/saml/%s/login", baseURL, id)
		metadataURL := fmt.Sprintf("%s/saml/%s/metadata", baseURL, id)

		providers = append(providers, SAMLProviderInfo{
			ID:          id,
			Name:        name,
			Icon:        icon,
			AuthURL:     authURL,
			MetadataURL: metadataURL,
			EntityID:    providerConfig.EntityID,
			ACSURL:      providerConfig.ACSURL,
			SLOURL:      providerConfig.SLOURL,
			Initialized: initialized,
		})
	}

	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, gin.H{"providers": providers})
}
```

- [ ] **Step 4: Update getProvider to use registry**

In `auth/handlers_providers.go`, replace `getProvider` (lines 137-145):

```go
// getProvider returns a Provider instance for the given provider ID
func (h *Handlers) getProvider(providerID string) (Provider, error) {
	var providerConfig OAuthProviderConfig
	var exists bool

	if h.registry != nil {
		providerConfig, exists = h.registry.GetOAuthProvider(providerID)
	} else {
		providerConfig, exists = h.config.OAuth.Providers[providerID]
	}

	if !exists {
		return nil, fmt.Errorf("provider %s not found", providerID)
	}

	return NewProvider(providerConfig, h.config.OAuth.CallbackURL)
}
```

- [ ] **Step 5: Run all auth handler tests**

Run: `make test-unit name=TestGetProviders`
Expected: PASS (both old and new tests)

- [ ] **Step 6: Commit**

```bash
git add auth/handlers_providers.go auth/handlers_test.go
git commit -m "refactor(auth): update provider handlers to use ProviderRegistry"
```

---

### Task 10: Update auth/config.go to delegate to registry

**Files:**
- Modify: `auth/config.go:501-519`

- [ ] **Step 1: This is a passthrough change — no new tests needed since existing tests cover it**

Update `GetProvider` and `GetEnabledProviders` in `auth/config.go`. These methods are on `Config`, not on the registry-aware structs. They remain as-is for backward compatibility — callers that need registry support will use the registry directly. No changes needed here after all, since:
- `Handlers.getProvider()` already delegates to registry (Task 9)
- `Config.GetProvider()` is only used in tests (`auth/service_test.go:353`)

Skip this task — no code changes needed.

---

### Task 11: Add providerRegistry field to API Server

**Files:**
- Modify: `api/server.go:29-70` (add field)

- [ ] **Step 1: Add field and setter to Server struct**

In `api/server.go`, add to the `Server` struct (after `configProvider`, around line 66):

```go
	providerRegistry auth.ProviderRegistry
```

Add import `"github.com/ericfitz/tmi/auth"` if not already present.

Add a setter method (after `SetConfigProvider`):

```go
// SetProviderRegistry sets the provider registry for cache invalidation from settings handlers.
func (s *Server) SetProviderRegistry(registry auth.ProviderRegistry) {
	s.providerRegistry = registry
}
```

- [ ] **Step 2: Build to verify**

Run: `make build-server`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add api/server.go
git commit -m "feat(api): add providerRegistry field to Server for settings handler integration"
```

---

### Task 12: Lint and build check for Chunk 2

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: PASS

- [ ] **Step 2: Run full unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Fix any issues, commit if needed**

---

## Chunk 3: Settings Handler Integration

### Task 13: Add cache invalidation to settings handlers

**Files:**
- Modify: `api/config_handlers.go` (UpdateSystemSetting, DeleteSystemSetting)
- Test: `api/config_handlers_test.go`

- [ ] **Step 1: Write test for cache invalidation on provider key write**

Add to `api/config_handlers_test.go`:

```go
func TestUpdateSystemSetting_InvalidatesProviderCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockSettings := NewMockSettingsService()
	mockRegistry := &mockProviderRegistry{invalidated: false}

	server := &Server{
		settingsService:  mockSettings,
		providerRegistry: mockRegistry,
	}

	// Call the handler directly since it uses OpenAPI-generated signature (key as parameter)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("userUUID", uuid.New().String())
	c.Set("isAdmin", true)
	c.Request = httptest.NewRequest("PUT", "/admin/settings/auth.oauth.providers.azure.client_id",
		strings.NewReader(`{"value": "azure-client-123", "setting_type": "string"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	server.UpdateSystemSetting(c, "auth.oauth.providers.azure.client_id")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, mockRegistry.invalidated, "provider cache should be invalidated")
}

// mockProviderRegistry tracks InvalidateCache calls
type mockProviderRegistry struct {
	invalidated bool
}

func (m *mockProviderRegistry) GetOAuthProvider(id string) (auth.OAuthProviderConfig, bool) {
	return auth.OAuthProviderConfig{}, false
}
func (m *mockProviderRegistry) GetEnabledOAuthProviders() map[string]auth.OAuthProviderConfig {
	return nil
}
func (m *mockProviderRegistry) GetSAMLProvider(id string) (auth.SAMLProviderConfig, bool) {
	return auth.SAMLProviderConfig{}, false
}
func (m *mockProviderRegistry) GetEnabledSAMLProviders() map[string]auth.SAMLProviderConfig {
	return nil
}
func (m *mockProviderRegistry) InvalidateCache() {
	m.invalidated = true
}
```

Add `"github.com/ericfitz/tmi/auth"` to imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestUpdateSystemSetting_InvalidatesProviderCache`
Expected: FAIL — no invalidation logic yet

- [ ] **Step 3: Add invalidation logic to UpdateSystemSetting**

In `api/config_handlers.go`, in the `UpdateSystemSetting` method, after the successful `s.settingsService.Set(ctx, &setting)` call and before the response is written, add:

```go
	// Invalidate provider cache if this is a provider-related key
	s.invalidateProviderCacheIfNeeded(key)
```

Add the helper method to `api/config_handlers.go`:

```go
// providerKeyPrefixes are the settings key prefixes for auth providers.
var providerKeyPrefixes = []string{
	"auth.oauth.providers.",
	"auth.saml.providers.",
}

// providerSecretSuffixes are the settings key suffixes for provider secrets.
var providerSecretSuffixes = []string{
	".client_secret",
	".sp_private_key",
	".sp_certificate",
	".idp_metadata_b64xml",
}

// invalidateProviderCacheIfNeeded checks if the key is a provider-related setting
// and invalidates the provider registry cache if so.
func (s *Server) invalidateProviderCacheIfNeeded(key string) {
	if s.providerRegistry == nil {
		return
	}
	for _, prefix := range providerKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			s.providerRegistry.InvalidateCache()
			return
		}
	}
}

// isProviderSecretKey returns true if the key is a provider secret that should be masked.
func isProviderSecretKey(key string) bool {
	isProviderKey := false
	for _, prefix := range providerKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			isProviderKey = true
			break
		}
	}
	if !isProviderKey {
		return false
	}
	for _, suffix := range providerSecretSuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}
```

Ensure `"strings"` is imported.

- [ ] **Step 4: Add invalidation to DeleteSystemSetting too**

In `DeleteSystemSetting`, after the successful `s.settingsService.Delete(ctx, key)` call:

```go
	s.invalidateProviderCacheIfNeeded(key)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `make test-unit name=TestUpdateSystemSetting_InvalidatesProviderCache`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go
git commit -m "feat(api): add provider cache invalidation on settings writes"
```

---

### Task 14: Add enable-validation gate

**Files:**
- Modify: `api/config_handlers.go` (in UpdateSystemSetting)
- Test: `api/config_handlers_test.go`

- [ ] **Step 1: Write tests for enable validation**

Add to `api/config_handlers_test.go`:

```go
func TestUpdateSystemSetting_EnableValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("rejects enabled=true with missing required fields", func(t *testing.T) {
		mockSettings := NewMockSettingsService()
		// Only client_id exists, missing authorization_url, token_url, userinfo
		mockSettings.AddSetting("auth.oauth.providers.azure.client_id", "az-id", "string")

		server := &Server{
			settingsService:  mockSettings,
			providerRegistry: &mockProviderRegistry{},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userUUID", uuid.New().String())
		c.Set("isAdmin", true)
		c.Request = httptest.NewRequest("PUT", "/admin/settings/auth.oauth.providers.azure.enabled",
			strings.NewReader(`{"value": "true", "setting_type": "bool"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateSystemSetting(c, "auth.oauth.providers.azure.enabled")

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "authorization_url")
		assert.Contains(t, w.Body.String(), "token_url")
		assert.Contains(t, w.Body.String(), "userinfo")
	})

	t.Run("accepts enabled=true with all required fields", func(t *testing.T) {
		mockSettings := NewMockSettingsService()
		mockSettings.AddSetting("auth.oauth.providers.azure.client_id", "az-id", "string")
		mockSettings.AddSetting("auth.oauth.providers.azure.authorization_url", "https://auth", "string")
		mockSettings.AddSetting("auth.oauth.providers.azure.token_url", "https://token", "string")
		mockSettings.AddSetting("auth.oauth.providers.azure.userinfo", `[{"url":"https://me","claims":["email"]}]`, "json")

		server := &Server{
			settingsService:  mockSettings,
			providerRegistry: &mockProviderRegistry{},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userUUID", uuid.New().String())
		c.Set("isAdmin", true)
		c.Request = httptest.NewRequest("PUT", "/admin/settings/auth.oauth.providers.azure.enabled",
			strings.NewReader(`{"value": "true", "setting_type": "bool"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateSystemSetting(c, "auth.oauth.providers.azure.enabled")

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("accepts enabled=false without validation", func(t *testing.T) {
		mockSettings := NewMockSettingsService()

		server := &Server{
			settingsService:  mockSettings,
			providerRegistry: &mockProviderRegistry{},
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userUUID", uuid.New().String())
		c.Set("isAdmin", true)
		c.Request = httptest.NewRequest("PUT", "/admin/settings/auth.oauth.providers.azure.enabled",
			strings.NewReader(`{"value": "false", "setting_type": "bool"}`))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateSystemSetting(c, "auth.oauth.providers.azure.enabled")

		assert.Equal(t, http.StatusOK, w.Code)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestUpdateSystemSetting_EnableValidation`
Expected: FAIL — no validation logic yet

- [ ] **Step 3: Implement enable-validation gate**

In `api/config_handlers.go`, add the validation function:

```go
// validateProviderEnableKey checks if the key is an enable key for a provider
// and validates required fields if the value is "true".
// Returns an error message if validation fails, or empty string if OK.
func (s *Server) validateProviderEnableKey(ctx context.Context, key, value string) string {
	if value != "true" {
		return ""
	}

	// Check if this is an OAuth provider enable key
	if strings.HasPrefix(key, "auth.oauth.providers.") && strings.HasSuffix(key, ".enabled") {
		providerID := extractProviderID(key, "auth.oauth.providers.")
		if providerID == "" {
			return ""
		}

		// Read all sibling keys for this provider
		settings, err := s.settingsService.ListByPrefix(ctx, "auth.oauth.providers."+providerID+".")
		if err != nil {
			return "failed to read provider settings"
		}

		// Assemble provider from settings
		providerSettings := make([]auth.ProviderSetting, len(settings))
		for i, s := range settings {
			providerSettings[i] = auth.ProviderSetting{Key: s.SettingKey, Value: s.Value}
		}
		providers := auth.AssembleOAuthProviders(providerSettings)
		p, ok := providers[providerID]
		if !ok {
			p = auth.OAuthProviderConfig{}
		}

		missing := auth.ValidateOAuthProvider(p)
		if len(missing) > 0 {
			return fmt.Sprintf("Cannot enable OAuth provider %q: missing required fields: %s",
				providerID, strings.Join(missing, ", "))
		}
	}

	// Check if this is a SAML provider enable key
	if strings.HasPrefix(key, "auth.saml.providers.") && strings.HasSuffix(key, ".enabled") {
		providerID := extractProviderID(key, "auth.saml.providers.")
		if providerID == "" {
			return ""
		}

		settings, err := s.settingsService.ListByPrefix(ctx, "auth.saml.providers."+providerID+".")
		if err != nil {
			return "failed to read provider settings"
		}

		providerSettings := make([]auth.ProviderSetting, len(settings))
		for i, s := range settings {
			providerSettings[i] = auth.ProviderSetting{Key: s.SettingKey, Value: s.Value}
		}
		providers := auth.AssembleSAMLProviders(providerSettings)
		p, ok := providers[providerID]
		if !ok {
			p = auth.SAMLProviderConfig{}
		}

		missing := auth.ValidateSAMLProvider(p)
		if len(missing) > 0 {
			return fmt.Sprintf("Cannot enable SAML provider %q: missing required fields: %s",
				providerID, strings.Join(missing, ", "))
		}
	}

	return ""
}

// extractProviderID extracts the provider ID from a settings key.
// For key "auth.oauth.providers.azure.enabled" with prefix "auth.oauth.providers.",
// returns "azure".
func extractProviderID(key, prefix string) string {
	remainder := key[len(prefix):]
	dotIdx := strings.Index(remainder, ".")
	if dotIdx <= 0 {
		return ""
	}
	return remainder[:dotIdx]
}
```

In `UpdateSystemSetting`, add the validation check before the `s.settingsService.Set(ctx, &setting)` call:

```go
	// Enable-validation gate: validate required fields when enabling a provider
	if validationErr := s.validateProviderEnableKey(ctx, key, setting.Value); validationErr != "" {
		c.JSON(http.StatusConflict, gin.H{"error": validationErr})
		return
	}
```

Add import `"github.com/ericfitz/tmi/auth"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestUpdateSystemSetting_EnableValidation`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go auth/provider_registry.go
git commit -m "feat(api): add enable-validation gate for provider settings writes"
```

---

### Task 15: Add secret suffix detection for DB provider keys in settings list

**Files:**
- Modify: `api/config_handlers.go` (in mergeSettingsWithConfig or ListSystemSettings)
- Test: `api/config_handlers_test.go`

- [ ] **Step 1: Write test for secret masking of DB provider keys**

Add to `api/config_handlers_test.go`:

```go
func TestIsProviderSecretKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"auth.oauth.providers.azure.client_secret", true},
		{"auth.oauth.providers.azure.client_id", false},
		{"auth.saml.providers.entra.sp_private_key", true},
		{"auth.saml.providers.entra.sp_certificate", true},
		{"auth.saml.providers.entra.idp_metadata_b64xml", true},
		{"auth.saml.providers.entra.entity_id", false},
		{"rate_limit.requests_per_minute", false},
		{"auth.jwt.secret", false}, // Not a provider key
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			assert.Equal(t, tt.expected, isProviderSecretKey(tt.key))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `make test-unit name=TestIsProviderSecretKey`
Expected: PASS (function was already implemented in Task 13)

- [ ] **Step 3: Integrate secret masking into ListSystemSettings**

In `api/config_handlers.go`, in the `ListSystemSettings` handler, after the merge step and before returning the response, add secret masking for DB provider keys. In the loop that builds the response array, add:

```go
		// Mask DB-sourced provider secrets
		if setting.Source == "database" && isProviderSecretKey(setting.SettingKey) {
			if setting.Value != "" {
				setting.Value = "<configured>"
			} else {
				setting.Value = "<not configured>"
			}
		}
```

Do the same in `GetSystemSetting` for single-key retrieval.

- [ ] **Step 4: Run settings handler tests**

Run: `make test-unit name=TestListSystemSettings`
Run: `make test-unit name=TestIsProviderSecretKey`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go
git commit -m "feat(api): add secret masking for DB provider keys in settings API"
```

---

### Task 16: Lint and build check for Chunk 3

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: PASS

- [ ] **Step 2: Run build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 3: Run all unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 4: Fix any issues, commit if needed**

---

## Chunk 4: Startup Wiring and SAML Integration

### Task 17: Wire registry in cmd/server/main.go

**Files:**
- Modify: `cmd/server/main.go` (Phase 4, after settings service initialization)

- [ ] **Step 1: Add registry creation and wiring**

In `cmd/server/main.go`, after the `settingsService.SetConfigProvider(configProvider)` line and before the `Server` struct creation, add:

```go
	// Create provider registry for unified auth provider lookup
	authConfig := auth.ConfigFromUnified(config)
	providerSettingsReader := api.NewProviderSettingsReaderAdapter(settingsService)
	providerRegistry := auth.NewDefaultProviderRegistry(
		authConfig.OAuth.Providers,
		authConfig.SAML.Providers,
		providerSettingsReader,
	)
	apiServer.SetProviderRegistry(providerRegistry)
	logger.Info("Provider registry initialized for lazy-loading database providers")
```

Note: `authConfig` may already exist in scope from the earlier `auth.InitAuthWithDB` call. Check and reuse if so; otherwise use a different variable name like `authConfigForRegistry`.

After the `authHandlers` are wired (in the `if authServiceAdapter != nil` block), add:

```go
		authHandlers.SetProviderRegistry(providerRegistry)
		authHandlers.Service().SetProviderRegistry(providerRegistry)
```

- [ ] **Step 2: Build to verify**

Run: `make build-server`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): wire ProviderRegistry at startup for lazy DB provider loading"
```

---

### Task 18: Add EnsureProvider to SAMLManager

**Files:**
- Modify: `auth/saml_manager.go`
- Test: `auth/saml_manager_test.go` (new file)

- [ ] **Step 1: Write test for EnsureProvider**

Create `auth/saml_manager_test.go`:

```go
package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSAMLManager_EnsureProvider_Idempotent(t *testing.T) {
	manager := NewSAMLManager(nil)

	config := SAMLProviderConfig{
		ID:             "test",
		Name:           "Test IDP",
		EntityID:       "https://tmi.example.com",
		IDPMetadataURL: "https://idp.example.com/metadata",
		ACSURL:         "https://tmi.example.com/saml/test/acs",
	}

	// First call should attempt initialization (will fail without real IDP, but shouldn't panic)
	err := manager.EnsureProvider("test", config)
	// Expected: error because we can't actually fetch IDP metadata in tests
	// But the method should exist and not panic
	if err != nil {
		// This is expected in unit tests — metadata fetch fails
		assert.Contains(t, err.Error(), "test")
	}
}

func TestSAMLManager_IsProviderInitialized_NotInitialized(t *testing.T) {
	manager := NewSAMLManager(nil)
	assert.False(t, manager.IsProviderInitialized("nonexistent"))
}
```

- [ ] **Step 2: Implement EnsureProvider**

In `auth/saml_manager.go`, add after `IsProviderInitialized`:

```go
// EnsureProvider lazily initializes a SAML provider if not already initialized.
// Idempotent: if the provider is already initialized, returns immediately.
// Thread-safe: uses the manager's mutex to prevent concurrent initialization.
func (m *SAMLManager) EnsureProvider(id string, config SAMLProviderConfig) error {
	m.mu.RLock()
	_, exists := m.providers[id]
	m.mu.RUnlock()

	if exists {
		return nil // Already initialized
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if _, exists := m.providers[id]; exists {
		return nil
	}

	logger := slogging.Get()
	logger.Info("Lazily initializing SAML provider %q from database configuration", id)

	// Convert to SAML library config (matches pattern in InitializeProviders)
	samlConfig := &saml.SAMLConfig{
		ID:                 id,
		Name:               config.Name,
		Enabled:            config.Enabled,
		Icon:               config.Icon,
		EntityID:           config.EntityID,
		ACSURL:             config.ACSURL,
		SLOURL:             config.SLOURL,
		SPPrivateKey:       config.SPPrivateKey,
		SPPrivateKeyPath:   config.SPPrivateKeyPath,
		SPCertificate:      config.SPCertificate,
		SPCertificatePath:  config.SPCertificatePath,
		IDPMetadataURL:     config.IDPMetadataURL,
		IDPMetadataB64XML:  config.IDPMetadataB64XML,
		AllowIDPInitiated:  config.AllowIDPInitiated,
		ForceAuthn:         config.ForceAuthn,
		SignRequests:       config.SignRequests,
		GroupAttributeName: config.GroupsAttribute,
		AttributeMapping: map[string]string{
			"email": config.EmailAttribute,
			"name":  config.NameAttribute,
		},
	}
	// If MetadataURL is set but IDPMetadataURL is not, use MetadataURL
	if samlConfig.IDPMetadataURL == "" && config.MetadataURL != "" {
		samlConfig.IDPMetadataURL = config.MetadataURL
	}

	provider, err := saml.NewSAMLProvider(samlConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize SAML provider %q: %w", id, err)
	}

	m.providers[id] = provider
	logger.Info("SAML provider %q initialized successfully from database", id)
	return nil
}
```

Add `"fmt"` to imports if not already present.

- [ ] **Step 3: Run tests**

Run: `make test-unit name=TestSAMLManager`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add auth/saml_manager.go auth/saml_manager_test.go
git commit -m "feat(auth): add EnsureProvider for lazy SAML provider initialization"
```

---

### Task 19: Final lint, build, and test

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: PASS

- [ ] **Step 2: Run build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 3: Run all unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 4: Fix any issues, commit if needed**

- [ ] **Step 5: Run integration tests**

Run: `make test-integration`
Expected: PASS

- [ ] **Step 6: Commit any final fixes**

```bash
git commit -m "chore: fix issues from final integration testing"
```
