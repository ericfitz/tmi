package api

import (
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestContentOAuthRegistry_RegisterAndLookup(t *testing.T) {
	r := NewContentOAuthProviderRegistry()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{})
	r.Register(p)
	got, ok := r.Get("mock")
	assert.True(t, ok)
	assert.Equal(t, "mock", got.ID())
	_, ok = r.Get("missing")
	assert.False(t, ok)
}

func TestContentOAuthRegistry_LoadFromConfig_OnlyEnabled(t *testing.T) {
	cfg := config.ContentOAuthConfig{
		Providers: map[string]config.ContentOAuthProviderConfig{
			"on":  {Enabled: true, ClientID: "c", AuthURL: "http://a", TokenURL: "http://t"},
			"off": {Enabled: false},
		},
	}
	r, err := LoadContentOAuthRegistryFromConfig(cfg)
	assert.NoError(t, err)
	_, ok := r.Get("on")
	assert.True(t, ok)
	_, ok = r.Get("off")
	assert.False(t, ok)
}
