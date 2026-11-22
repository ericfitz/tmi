package config

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// startupOnlyKeys are settings that come from config/env only (#1167): the
// auth service reads the struct once at startup, so a database row could never
// take effect.
var startupOnlyKeys = []string{"auth.oauth_callback_url", "features.saml_enabled"}

func TestStartupOnlySettings_AreBootstrapStatic(t *testing.T) {
	for _, key := range startupOnlyKeys {
		t.Run(key, func(t *testing.T) {
			c := classificationFor(key)
			assert.Equal(t, CategoryBootstrap, c.Category, "file/env only, never database-backed")
			assert.Equal(t, MutabilityStatic, c.Mutability)
			assert.Nil(t, c.Delivery)
			d, ok := DefFor(key)
			require.True(t, ok)
			assert.False(t, d.Transitional)
			assert.False(t, d.Seeded, "a startup-only key must not be seeded into system_settings")
			assert.Equal(t, MutabilityStatic, d.Class.Mutability)
			assert.Equal(t, CategoryBootstrap, d.Class.Category)
		})
	}
	// tmi-ux reads features.saml_enabled from the public client config.
	c := classificationFor("features.saml_enabled")
	assert.Equal(t, VisibilityPublic, c.Visibility)
	assert.Contains(t, c.Consumers, ConsumerTMIUX)
}

func TestStartupOnlySettings_NotInDefaultOperationalSettings(t *testing.T) {
	for _, s := range DefaultOperationalSettings() {
		for _, key := range startupOnlyKeys {
			assert.NotEqual(t, key, s.Key, "startup-only key must not be seeded")
		}
	}
}

// OperationalKeysInFile must classify by setting key, not by YAML path (#1167):
// the two startup-only keys are non-operational and stay in the file, while
// extraction.async_enabled (YAML path content_extractors.async_enabled) is
// still operational.
func TestOperationalKeysInFile_ClassifiesBySettingKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yml")
	yamlText := "auth:\n  oauth:\n    callback_url: https://x.test/cb\n  saml:\n    enabled: true\n" +
		"content_extractors:\n  async_enabled: true\n"
	require.NoError(t, os.WriteFile(p, []byte(yamlText), 0o600))

	keys, err := OperationalKeysInFile(p)
	require.NoError(t, err)
	sort.Strings(keys)
	assert.Equal(t, []string{"content_extractors.async_enabled"}, keys)
}

func TestSettingKeyForYAMLPath(t *testing.T) {
	assert.Equal(t, "auth.oauth_callback_url", SettingKeyForYAMLPath("auth.oauth.callback_url"))
	assert.Equal(t, "features.saml_enabled", SettingKeyForYAMLPath("auth.saml.enabled"))
	assert.Equal(t, "extraction.async_enabled", SettingKeyForYAMLPath("content_extractors.async_enabled"))
	// A path that is already a key, and generated provider paths, map to themselves.
	assert.Equal(t, "server.port", SettingKeyForYAMLPath("server.port"))
	assert.Equal(t, "auth.oauth.providers.google.client_id", SettingKeyForYAMLPath("auth.oauth.providers.google.client_id"))
}

// The generated example file must place the startup-only keys at the YAML
// paths config.Load reads, not at their setting keys.
func TestGenerateExampleConfig_StartupOnlyKeysUseYAMLPaths(t *testing.T) {
	out, err := GenerateExampleConfig()
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(out, &parsed))

	auth, _ := parsed["auth"].(map[string]any)
	require.NotNil(t, auth)
	oauth, _ := auth["oauth"].(map[string]any)
	require.NotNil(t, oauth, "auth.oauth.callback_url must be emitted at its YAML path")
	assert.NotEmpty(t, oauth["callback_url"])
	saml, _ := auth["saml"].(map[string]any)
	require.NotNil(t, saml, "auth.saml.enabled must be emitted at its YAML path")
	assert.Contains(t, saml, "enabled")
	assert.NotContains(t, auth, "oauth_callback_url")
	assert.NotContains(t, parsed, "features")
}
