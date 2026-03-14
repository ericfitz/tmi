package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			{Key: "auth.oauth.providers.azure.userinfo", Value: `[{"url":"https://graph.microsoft.com/me","claims":{"email":"email","name":"name"}}]`},
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
		}
		missing := ValidateOAuthProvider(p)
		assert.Contains(t, missing, "authorization_url")
		assert.Contains(t, missing, "token_url")
		assert.Contains(t, missing, "userinfo")
		assert.NotContains(t, missing, "client_id")
	})

	t.Run("completely empty provider", func(t *testing.T) {
		missing := ValidateOAuthProvider(OAuthProviderConfig{})
		assert.Len(t, missing, 4)
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

	providers := registry.GetEnabledOAuthProviders()
	assert.Len(t, providers, 1)
	assert.Equal(t, "Google", providers["google"].Name)
}
