package auth

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"

	tmi "github.com/ericfitz/tmi" // Root package: embedded static sign-in icons
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// safeProviderIDForIcon constrains which provider ids may be interpolated into
// an embedded icon path, as defence-in-depth against path traversal even
// though provider ids come from trusted config.
var safeProviderIDForIcon = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// providerSignInIcon returns the served path of a sign-in icon for a provider
// whose config supplies no icon of its own. When an icon matching the provider
// id is embedded in the binary (static/provider-logos/signin/<id>.svg) it is
// used so built-in providers like "tmi" show their own branding; otherwise a
// generic OAuth icon is returned. Falling back to the bare provider id instead
// produced a malformed value the client resolved to a bogus URL (#498).
// SEM@f298766967f135dd9ae0e6697257535bbcc1946d: resolve a provider's default sign-in icon path, preferring an embedded brand icon
func providerSignInIcon(id string) string {
	const generic = "/static/provider-logos/signin/oauth.svg"
	if !safeProviderIDForIcon.MatchString(id) {
		return generic
	}
	embedded := "static/provider-logos/signin/" + id + ".svg"
	if _, err := fs.Stat(tmi.StaticFS, embedded); err == nil {
		return "/" + embedded
	}
	return generic
}

// ProviderInfo contains information about an OAuth provider
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: public OAuth provider metadata returned to clients
type ProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	AuthURL     string `json:"auth_url"`
	TokenURL    string `json:"token_url"`
	RedirectURI string `json:"redirect_uri"`
	ClientID    string `json:"client_id"`
}

// SAMLProviderInfo contains public information about a SAML provider
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: public SAML provider metadata returned to clients
type SAMLProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	AuthURL     string `json:"auth_url"`
	MetadataURL string `json:"metadata_url"`
	EntityID    string `json:"entity_id"`
	ACSURL      string `json:"acs_url"`
	SLOURL      string `json:"slo_url,omitempty"`
	Initialized bool   `json:"initialized"`
}

// GetProviders returns the available OAuth providers
// SEM@c1ae98795fcc480287e8ef03be0c86587e974cc5: list enabled OAuth providers with public endpoint URLs and resolved sign-in icons
func (h *Handlers) GetProviders(c *gin.Context) {
	var enabledProviders map[string]OAuthProviderConfig

	if h.registry != nil {
		enabledProviders = h.registry.GetEnabledOAuthProviders()
	} else {
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
		// Fall back to a valid sign-in icon when a provider has no icon
		// configured: the provider's own embedded icon if present (e.g. the
		// built-in "tmi" provider gets tmi.svg), else a generic OAuth icon.
		// Using the bare provider id produced a malformed value the client
		// resolved to a bogus URL like http://host/tmi (#498).
		icon := providerConfig.Icon
		if icon == "" {
			icon = providerSignInIcon(id)
		}

		authURL := fmt.Sprintf("%s/oauth2/authorize?idp=%s", getBaseURL(c), id)
		tokenURL := fmt.Sprintf("%s/oauth2/token?idp=%s", getBaseURL(c), id)

		providers = append(providers, ProviderInfo{
			ID:          id,
			Name:        name,
			Icon:        icon,
			AuthURL:     authURL,
			TokenURL:    tokenURL,
			RedirectURI: h.oauthCallbackURL(c.Request.Context()),
			ClientID:    providerConfig.ClientID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"providers": providers,
	})
}

// GetSAMLProviders returns the available SAML providers
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: list enabled SAML providers with initialization status and public URLs
func (h *Handlers) GetSAMLProviders(c *gin.Context) {
	// Return empty array if SAML disabled
	if !h.samlEnabled(c.Request.Context()) {
		c.JSON(http.StatusOK, gin.H{"providers": []SAMLProviderInfo{}})
		return
	}

	// Get SAML manager if available (may be nil in tests or if no providers initialized)
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

	for id := range enabledProviders {
		// Lazy-initialize DB-sourced SAML providers
		if samlManager != nil {
			if err := h.ensureSAMLProvider(samlManager, id); err != nil {
				logger := slogging.Get()
				logger.Warn("failed to initialize SAML provider %q: %v", id, err)
			}
		}

		providerConfig := enabledProviders[id]

		// Check if the provider was successfully initialized
		initialized := samlManager != nil && samlManager.IsProviderInitialized(id)

		// Use provider name or ID as fallback
		name := providerConfig.Name
		if name == "" {
			name = id
		}

		// Use provider icon or default SAML icon
		icon := providerConfig.Icon
		if icon == "" {
			icon = "fa-solid fa-key"
		}

		// Build public URLs (using path parameters)
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

	// Cache for 1 hour
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, gin.H{"providers": providers})
}

// ensureSAMLProvider lazy-initializes a DB-sourced SAML provider if needed,
// then returns it from the SAMLManager. Returns an error if the provider is
// not found in the registry or cannot be initialized.
// SEM@d398eb8185c6257f8aeade7ded4b474735df44ce: lazy-initialize a SAML provider from registry if not yet initialized
func (h *Handlers) ensureSAMLProvider(samlManager *SAMLManager, providerID string) error {
	if samlManager.IsProviderInitialized(providerID) {
		return nil
	}

	// Look up the provider config from the registry
	if h.registry == nil {
		// No registry; provider must already be initialized from config
		return nil
	}

	providerConfig, exists := h.registry.GetSAMLProvider(providerID)
	if !exists {
		return fmt.Errorf("SAML provider %q not found", providerID)
	}

	return samlManager.EnsureProvider(providerID, providerConfig)
}

// getProviderWithContext returns a Provider instance for the given provider
// ID. The OAuth callback URL is fetched from the runtime config reader
// (DB-backed) when wired, falling back to the YAML snapshot when not
// (e.g. in unit tests).
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: fetch an OAuth provider instance for a given provider ID using runtime config
func (h *Handlers) getProviderWithContext(ctx context.Context, providerID string) (Provider, error) {
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

	return NewProvider(providerConfig, h.oauthCallbackURL(ctx))
}
