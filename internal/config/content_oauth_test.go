package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestContentOAuthConfig_YAML_Decode(t *testing.T) {
	raw := `
callback_url: "http://localhost:8080/oauth2/content_callback"
allowed_client_callbacks:
  - "http://localhost:8079/"
  - "http://localhost:4200/*"
providers:
  confluence:
    enabled: true
    client_id: "cid"
    client_secret: "sec"
    auth_url: "https://auth.example.com/authorize"
    token_url: "https://auth.example.com/token"
    userinfo_url: "https://api.example.com/me"
    revocation_url: "https://auth.example.com/revoke"
    required_scopes: ["read:a", "read:b"]
`
	var c ContentOAuthConfig
	require.NoError(t, yaml.Unmarshal([]byte(raw), &c))
	assert.Equal(t, "http://localhost:8080/oauth2/content_callback", c.CallbackURL)
	assert.Len(t, c.AllowedClientCallbacks, 2)
	p := c.Providers["confluence"]
	assert.True(t, p.Enabled)
	assert.Equal(t, []string{"read:a", "read:b"}, p.RequiredScopes)
}

func TestContentOAuthConfig_Validate_RequiresKeyWhenEnabled(t *testing.T) {
	c := ContentOAuthConfig{
		Providers: map[string]ContentOAuthProviderConfig{
			"confluence": {Enabled: true, ClientID: "c", ClientSecret: "s",
				AuthURL: "https://a", TokenURL: "https://t", RequiredScopes: []string{"read"}},
		},
	}
	err := c.Validate("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TMI_CONTENT_TOKEN_ENCRYPTION_KEY")
}

func TestContentOAuthConfig_Validate_AcceptsKeyWhenEnabled(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	c := ContentOAuthConfig{
		Providers: map[string]ContentOAuthProviderConfig{
			"confluence": {Enabled: true, ClientID: "c", ClientSecret: "s",
				AuthURL: "https://a", TokenURL: "https://t", RequiredScopes: []string{"read"}},
		},
	}
	assert.NoError(t, c.Validate(key))
}

func TestContentOAuthConfig_Validate_DisabledIsFine(t *testing.T) {
	c := ContentOAuthConfig{Providers: map[string]ContentOAuthProviderConfig{
		"confluence": {Enabled: false},
	}}
	assert.NoError(t, c.Validate(""))
}

func TestContentOAuthProvider_RequiresAuthAndTokenURLs(t *testing.T) {
	c := ContentOAuthConfig{
		Providers: map[string]ContentOAuthProviderConfig{
			"bad": {Enabled: true, ClientID: "c"},
		},
	}
	err := c.Validate("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.Error(t, err)
}
