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
	assert.False(t, found.Secret)
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
