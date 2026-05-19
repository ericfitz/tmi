package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findSetting(settings []MigratableSetting, key string) *MigratableSetting {
	for i := range settings {
		if settings[i].Key == key {
			return &settings[i]
		}
	}
	return nil
}

func TestGetMigratableSettings_IncludesServerSettings(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Port: "8080", Interface: "localhost"}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "server.port")
	require.NotNil(t, found)
	assert.Equal(t, "8080", found.Value)
	assert.Equal(t, "string", found.Type)
	assert.False(t, found.Secret)
}

func TestGetMigratableSettings_IncludesAuthFlags(t *testing.T) {
	cfg := &Config{Auth: AuthConfig{AutoPromoteFirstUser: true, BuildMode: "dev"}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "auth.auto_promote_first_user")
	require.NotNil(t, found)
	assert.Equal(t, "true", found.Value)
}

func TestGetMigratableSettings_JWTSecretMasked(t *testing.T) {
	cfg := &Config{Auth: AuthConfig{JWT: JWTConfig{Secret: "super-secret"}}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "auth.jwt.secret")
	require.NotNil(t, found)
	assert.True(t, found.Secret)
}

func TestGetMigratableSettings_DatabaseURLSanitized(t *testing.T) {
	cfg := &Config{Database: DatabaseConfig{URL: "postgres://user:secret@localhost:5432/db"}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "database.url")
	require.NotNil(t, found)
	assert.Equal(t, "postgres://user:****@localhost:5432/db", found.Value)
	// database.url is classified as a secret (bootstrap, secret=true) because it
	// can contain credentials; the value is sanitized but the field is still secret.
	assert.True(t, found.Secret)
}

func TestGetMigratableSettings_EnvironmentSource(t *testing.T) {
	t.Setenv("TMI_SERVER_PORT", "9090")
	cfg := &Config{Server: ServerConfig{Port: "9090"}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "server.port")
	require.NotNil(t, found)
	assert.Equal(t, "environment", found.Source)
}

func TestGetMigratableSettings_OAuthClientSecretMasked(t *testing.T) {
	cfg := &Config{Auth: AuthConfig{OAuth: OAuthConfig{Providers: map[string]OAuthProviderConfig{
		"github": {Enabled: true, ClientSecret: "ghsecret"},
	}}}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "auth.oauth.providers.github.client_secret")
	require.NotNil(t, found)
	assert.True(t, found.Secret)
}

// TestGetMigratableSettings_OAuthProviderSecretMasking guards against a
// regression where the Class.Secret->Secret sync in GetMigratableSettings
// would mask non-secret OAuth provider sub-keys. A provider subtree contains
// a mix: `.client_secret` is secret, but `.enabled`/`.name`/`.client_id` are
// not. The prefix Class.Secret must therefore be false, and per-setting Secret
// flags must remain the precise masking source.
func TestGetMigratableSettings_OAuthProviderSecretMasking(t *testing.T) {
	cfg := &Config{Auth: AuthConfig{OAuth: OAuthConfig{Providers: map[string]OAuthProviderConfig{
		"github": {
			Enabled:      true,
			ID:           "github",
			Name:         "GitHub",
			ClientID:     "client-id-visible-in-browser",
			ClientSecret: "ghsecret",
		},
	}}}}
	settings := cfg.GetMigratableSettings()

	// A non-secret provider sub-key must NOT be masked.
	clientID := findSetting(settings, "auth.oauth.providers.github.client_id")
	require.NotNil(t, clientID, "client_id sub-key should be emitted")
	assert.False(t, clientID.Secret, "client_id is semi-public (visible in browser) and must not be masked")

	enabled := findSetting(settings, "auth.oauth.providers.github.enabled")
	require.NotNil(t, enabled, "enabled sub-key should be emitted")
	assert.False(t, enabled.Secret, "enabled is not a secret and must not be masked")

	// The provider client_secret MUST be masked.
	clientSecret := findSetting(settings, "auth.oauth.providers.github.client_secret")
	require.NotNil(t, clientSecret, "client_secret sub-key should be emitted")
	assert.True(t, clientSecret.Secret, "client_secret must be masked")
}

func TestGetMigratableSettings_LoggingSettings(t *testing.T) {
	cfg := &Config{Logging: LoggingConfig{Level: "debug", IsDev: true, LogDir: "logs"}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "logging.level")
	require.NotNil(t, found)
	assert.Equal(t, "debug", found.Value)
	found = findSetting(settings, "logging.is_dev")
	require.NotNil(t, found)
	assert.Equal(t, "true", found.Value)
}

func TestGetMigratableSettings_RedisPasswordMasked(t *testing.T) {
	cfg := &Config{Database: DatabaseConfig{Redis: RedisConfig{Password: "secret"}}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "database.redis.password")
	require.NotNil(t, found)
	assert.True(t, found.Secret)
}

func TestGetMigratableSettings_Administrators(t *testing.T) {
	cfg := &Config{Administrators: []AdministratorConfig{
		{Provider: "google", Email: "admin@test.com", SubjectType: "user"},
	}}
	settings := cfg.GetMigratableSettings()
	found := findSetting(settings, "administrators")
	require.NotNil(t, found)
	assert.Equal(t, "json", found.Type)
	assert.Contains(t, found.Value, "google")
}
