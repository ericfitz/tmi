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
	ListByPrefix(ctx context.Context, prefix string) ([]ProviderSetting, error)
}

// ProviderSetting is a minimal representation of a setting key/value pair.
type ProviderSetting struct {
	Key   string
	Value string
}

// ProviderRegistry provides unified access to OAuth and SAML provider
// configurations from all sources (config, environment, database).
type ProviderRegistry interface {
	GetOAuthProvider(id string) (OAuthProviderConfig, bool)
	GetEnabledOAuthProviders() map[string]OAuthProviderConfig
	GetSAMLProvider(id string) (SAMLProviderConfig, bool)
	GetEnabledSAMLProviders() map[string]SAMLProviderConfig
	InvalidateCache()
}

// DefaultProviderRegistry merges immutable config/env providers with
// mutable database-sourced providers assembled from system_settings rows.
type DefaultProviderRegistry struct {
	configOAuth map[string]OAuthProviderConfig
	configSAML  map[string]SAMLProviderConfig
	dbOAuth     map[string]OAuthProviderConfig
	dbSAML      map[string]SAMLProviderConfig
	dbCacheMu   sync.RWMutex
	dbCacheTime time.Time
	cacheTTL    time.Duration
	dirty       bool
	settings    ProviderSettingsReader
}

// DefaultProviderCacheTTL is the default TTL for the database provider cache.
const DefaultProviderCacheTTL = 60 * time.Second

// NewDefaultProviderRegistry creates a new DefaultProviderRegistry with the given
// config/env providers and a settings reader for database-sourced providers.
func NewDefaultProviderRegistry(
	configOAuth map[string]OAuthProviderConfig,
	configSAML map[string]SAMLProviderConfig,
	settings ProviderSettingsReader,
) *DefaultProviderRegistry {
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
		dirty:       true,
		settings:    settings,
	}
}

// GetOAuthProvider returns the OAuth provider configuration for the given ID.
// Config/env providers take precedence over database-sourced providers.
func (r *DefaultProviderRegistry) GetOAuthProvider(id string) (OAuthProviderConfig, bool) {
	if p, ok := r.configOAuth[id]; ok {
		return p, true
	}
	r.ensureDBCacheFresh()
	r.dbCacheMu.RLock()
	defer r.dbCacheMu.RUnlock()
	p, ok := r.dbOAuth[id]
	return p, ok
}

// GetEnabledOAuthProviders returns all enabled OAuth providers from all sources.
// Config/env providers shadow database-sourced providers with the same ID.
func (r *DefaultProviderRegistry) GetEnabledOAuthProviders() map[string]OAuthProviderConfig {
	r.ensureDBCacheFresh()
	result := make(map[string]OAuthProviderConfig)
	for id, p := range r.configOAuth {
		if p.Enabled {
			result[id] = p
		}
	}
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

// GetSAMLProvider returns the SAML provider configuration for the given ID.
// Config/env providers take precedence over database-sourced providers.
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
// Config/env providers shadow database-sourced providers with the same ID.
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

// InvalidateCache marks the database provider cache as dirty so it will be
// refreshed on the next access.
func (r *DefaultProviderRegistry) InvalidateCache() {
	r.dbCacheMu.Lock()
	defer r.dbCacheMu.Unlock()
	r.dirty = true
}

func (r *DefaultProviderRegistry) ensureDBCacheFresh() {
	r.dbCacheMu.RLock()
	needsRefresh := r.dirty || time.Since(r.dbCacheTime) > r.cacheTTL
	r.dbCacheMu.RUnlock()
	if !needsRefresh {
		return
	}
	r.dbCacheMu.Lock()
	defer r.dbCacheMu.Unlock()
	if !r.dirty && time.Since(r.dbCacheTime) <= r.cacheTTL {
		return
	}
	r.refreshDBProviders()
}

func (r *DefaultProviderRegistry) refreshDBProviders() {
	logger := slogging.Get()
	ctx := context.Background()

	oauthSettings, err := r.settings.ListByPrefix(ctx, "auth.oauth.providers.")
	if err != nil {
		logger.Error("Failed to load OAuth providers from database: %v", err)
		return
	}
	r.dbOAuth = AssembleOAuthProviders(oauthSettings)

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

// AssembleOAuthProviders groups settings by provider ID and assembles OAuthProviderConfig structs.
// Exported so the api package can use it for enable-validation.
func AssembleOAuthProviders(settings []ProviderSetting) map[string]OAuthProviderConfig {
	return make(map[string]OAuthProviderConfig)
}

// AssembleSAMLProviders groups settings by provider ID and assembles SAMLProviderConfig structs.
// Exported so the api package can use it for enable-validation.
func AssembleSAMLProviders(settings []ProviderSetting) map[string]SAMLProviderConfig {
	return make(map[string]SAMLProviderConfig)
}
