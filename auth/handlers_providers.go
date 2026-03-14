package auth

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ProviderInfo contains information about an OAuth provider
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

// GetSAMLProviders returns the available SAML providers
func (h *Handlers) GetSAMLProviders(c *gin.Context) {
	// Return empty array if SAML disabled
	if !h.config.SAML.Enabled {
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

	for id, providerConfig := range enabledProviders {
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
