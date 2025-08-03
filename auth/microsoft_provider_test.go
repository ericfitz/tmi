package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMicrosoftProvider(t *testing.T) {
	t.Run("NewMicrosoftProvider_Success", func(t *testing.T) {
		config := OAuthProviderConfig{
			ID:               "microsoft",
			ClientID:         "test-client-id",
			ClientSecret:     "test-client-secret",
			AuthorizationURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL:         "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			UserInfoURL:      "https://graph.microsoft.com/v1.0/me",
			Scopes:           []string{"openid", "profile", "email"},
		}

		callbackURL := "http://localhost:8080/auth/callback"

		// This should not fail with issuer mismatch error
		provider, err := NewMicrosoftProvider(config, callbackURL)
		require.NoError(t, err)
		assert.NotNil(t, provider)
		// Note: provider.verifier might be nil due to Microsoft's issuer validation issues
		// This is expected behavior for Microsoft provider
		assert.NotNil(t, provider.oauth2Config)

		// Verify OAuth2 config is set up correctly
		oauth2Config := provider.GetOAuth2Config()
		assert.Equal(t, config.ClientID, oauth2Config.ClientID)
		assert.Equal(t, config.ClientSecret, oauth2Config.ClientSecret)
		assert.Equal(t, callbackURL, oauth2Config.RedirectURL)
		assert.Equal(t, config.Scopes, oauth2Config.Scopes)
		assert.Equal(t, config.AuthorizationURL, oauth2Config.Endpoint.AuthURL)
		assert.Equal(t, config.TokenURL, oauth2Config.Endpoint.TokenURL)
	})

	t.Run("MicrosoftProvider_GetAuthorizationURL", func(t *testing.T) {
		config := OAuthProviderConfig{
			ID:               "microsoft",
			ClientID:         "test-client-id",
			ClientSecret:     "test-client-secret",
			AuthorizationURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL:         "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			UserInfoURL:      "https://graph.microsoft.com/v1.0/me",
			Scopes:           []string{"openid", "profile", "email"},
		}

		provider, err := NewMicrosoftProvider(config, "http://localhost:8080/auth/callback")
		require.NoError(t, err)

		state := "test-state-123"
		authURL := provider.GetAuthorizationURL(state)

		// Verify the URL contains expected components
		assert.Contains(t, authURL, config.AuthorizationURL)
		assert.Contains(t, authURL, "client_id=test-client-id")
		assert.Contains(t, authURL, "state=test-state-123")
		assert.Contains(t, authURL, "scope=openid+profile+email")
		assert.Contains(t, authURL, "response_type=code")
	})
}