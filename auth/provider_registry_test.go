package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
