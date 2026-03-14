package auth

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
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

const (
	oauthPrefix = "auth.oauth.providers."
	samlPrefix  = "auth.saml.providers."
)

// groupSettingsByProvider parses settings keys of the form "<prefix><id>.<field>"
// and groups them into a map of id -> field -> value.
func groupSettingsByProvider(settings []ProviderSetting, prefix string) map[string]map[string]string {
	grouped := make(map[string]map[string]string)
	for _, s := range settings {
		if !strings.HasPrefix(s.Key, prefix) {
			continue
		}
		remainder := s.Key[len(prefix):]
		dotIdx := strings.Index(remainder, ".")
		if dotIdx <= 0 {
			continue
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

// AssembleOAuthProviders groups settings by provider ID and assembles OAuthProviderConfig structs.
// Exported so the api package can use it for enable-validation.
func AssembleOAuthProviders(settings []ProviderSetting) map[string]OAuthProviderConfig {
	logger := slogging.Get()
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
				p.Enabled = value == literalTrue
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
// Exported so the api package can use it for enable-validation.
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
				p.Enabled = value == literalTrue
			case "name":
				p.Name = value
			case "icon":
				p.Icon = value
			case "allow_idp_initiated":
				p.AllowIDPInitiated = value == literalTrue
			case "force_authn":
				p.ForceAuthn = value == literalTrue
			case "sign_requests":
				p.SignRequests = value == literalTrue
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
