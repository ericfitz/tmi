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
			ID:               "tmi",
			ClientID:         "tmi-client-id",
			ClientSecret:     "tmi-oauth-secret-12345",
			AuthorizationURL: "http://localhost:8080/oauth2/authorize?idp=tmi",
			TokenURL:         "http://localhost:8080/oauth2/token?idp=tmi",
			UserInfoURL:      "",
			Scopes:           []string{"profile", "email"},
		}

		callbackURL := "http://localhost:8080/oauth2/callback"

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
			ID:               "tmi",
			ClientID:         "tmi-client-id",
			AuthorizationURL: "http://localhost:8080/oauth2/authorize?idp=tmi",
			TokenURL:         "http://localhost:8080/oauth2/token?idp=tmi",
			Scopes:           []string{"profile", "email"},
		}

		provider := NewTestProvider(config, "http://localhost:8080/oauth2/callback")
		state := "test-state-123"
		authURL := provider.GetAuthorizationURL(state)

		// For TMI provider, it should return a direct callback URL with auth code
		assert.Contains(t, authURL, "http://localhost:8080/oauth2/callback")
		assert.Contains(t, authURL, "state=test-state-123")
		assert.Contains(t, authURL, "code=test_auth_code_")
		// Verify it's a valid URL
		parsedURL, err := url.Parse(authURL)
		assert.NoError(t, err)
		assert.Equal(t, "/oauth2/callback", parsedURL.Path)
		assert.Equal(t, "test-state-123", parsedURL.Query().Get("state"))
		assert.NotEmpty(t, parsedURL.Query().Get("code"))
	})
}
