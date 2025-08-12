//go:build dev || test

package auth

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestProviderURLs(t *testing.T) {
	t.Run("TestProvider_Uses_Config_URLs", func(t *testing.T) {
		config := OAuthProviderConfig{
			ID:               "test",
			ClientID:         "test-client-id",
			ClientSecret:     "test-oauth-secret-12345",
			AuthorizationURL: "http://localhost:8080/auth/login/test",
			TokenURL:         "http://localhost:8080/auth/token/test",
			UserInfoURL:      "",
			Scopes:           []string{"profile", "email"},
		}

		callbackURL := "http://localhost:8080/auth/callback"

		provider := NewTestProvider(config, callbackURL)
		require.NotNil(t, provider)

		// Verify OAuth2 config uses the config URLs, not hardcoded ones
		oauth2Config := provider.GetOAuth2Config()
		assert.Equal(t, config.AuthorizationURL, oauth2Config.Endpoint.AuthURL)
		assert.Equal(t, config.TokenURL, oauth2Config.Endpoint.TokenURL)
		assert.Equal(t, config.Scopes, oauth2Config.Scopes)
		assert.Equal(t, config.ClientID, oauth2Config.ClientID)
		assert.Equal(t, callbackURL, oauth2Config.RedirectURL)
	})

	t.Run("TestProvider_GetAuthorizationURL", func(t *testing.T) {
		config := OAuthProviderConfig{
			ID:               "test",
			ClientID:         "test-client-id",
			AuthorizationURL: "http://localhost:8080/auth/login/test",
			TokenURL:         "http://localhost:8080/auth/token/test",
			Scopes:           []string{"profile", "email"},
		}

		provider := NewTestProvider(config, "http://localhost:8080/auth/callback")
		state := "test-state-123"
		authURL := provider.GetAuthorizationURL(state)

		// For test provider, it should return a direct callback URL with auth code
		assert.Contains(t, authURL, "http://localhost:8080/auth/callback")
		assert.Contains(t, authURL, "state=test-state-123")
		assert.Contains(t, authURL, "code=test_auth_code_")
		// Verify it's a valid URL
		parsedURL, err := url.Parse(authURL)
		assert.NoError(t, err)
		assert.Equal(t, "/auth/callback", parsedURL.Path)
		assert.Equal(t, "test-state-123", parsedURL.Query().Get("state"))
		assert.NotEmpty(t, parsedURL.Query().Get("code"))
	})
}