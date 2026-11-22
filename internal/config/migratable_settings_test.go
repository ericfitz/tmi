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

func TestMigratableSettings_IncludesExtractionAsyncEnabled(t *testing.T) {
	cfg := getDefaultConfig()
	settings := cfg.GetMigratableSettings()
	var found *MigratableSetting
	for i := range settings {
		if settings[i].Key == "extraction.async_enabled" {
			found = &settings[i]
			break
		}
	}
	if found == nil {
		t.Fatal("extraction.async_enabled not registered")
	}
	if found.Type != "bool" {
		t.Errorf("Type = %q, want bool", found.Type)
	}
	if found.Value != "false" {
		t.Errorf("default Value = %q, want false", found.Value)
	}
	if found.EnvVar != "TMI_EXTRACTION_ASYNC_ENABLED" {
		t.Errorf("EnvVar = %q, want TMI_EXTRACTION_ASYNC_ENABLED", found.EnvVar)
	}
}

func TestDefaultOperationalSettings_OnlyOperational(t *testing.T) {
	ops := DefaultOperationalSettings()
	if len(ops) == 0 {
		t.Fatal("DefaultOperationalSettings returned nothing")
	}
	for _, s := range ops {
		if s.Class.Category != CategoryOperational {
			t.Errorf("DefaultOperationalSettings included non-operational key %q (category %v)", s.Key, s.Class.Category)
		}
	}
	// A representative operational key must be present.
	found := false
	for _, s := range ops {
		if s.Key == "websocket.inactivity_timeout_seconds" {
			found = true
		}
		// No bootstrap key may appear.
		if s.Key == "database.url" {
			t.Errorf("bootstrap key %q must not be in DefaultOperationalSettings", s.Key)
		}
	}
	if !found {
		t.Error("expected websocket.inactivity_timeout_seconds among operational settings")
	}
}

// A callback URL supplied via TMI_OAUTH_CALLBACK_URL must be reported as
// Source "environment" and Explicit, so the admin API reports where the
// startup-only value came from (#1167). The setting used to hardcode
// Source: "config", unlike the 130 others that route through settingSource().
//
// History: before it was startup-only, a stale seeded
// http://localhost:8080/oauth2/callback row beat a correctly-set env value on
// AWS (2026-08-20), and every provider's redirect_uri was advertised as
// localhost. Startup-only removes the database from this read altogether.
func TestGetMigratableSettings_OAuthCallbackURLHonorsEnvironment(t *testing.T) {
	t.Setenv("TMI_OAUTH_CALLBACK_URL", "https://api.example.test/oauth2/callback")
	cfg := &Config{Auth: AuthConfig{OAuth: OAuthConfig{
		CallbackURL: "https://api.example.test/oauth2/callback",
	}}}
	found := findSetting(cfg.GetMigratableSettings(), "auth.oauth_callback_url")
	require.NotNil(t, found)
	assert.Equal(t, "environment", found.Source)
	assert.True(t, found.Explicit, "env-supplied callback URL must outrank the database row")
	assert.Equal(t, "TMI_OAUTH_CALLBACK_URL", found.EnvVar)
}

// With no env var set, the struct default must stay non-Explicit so the admin
// API can tell an operator-supplied value from the default.
func TestGetMigratableSettings_OAuthCallbackURLDefaultDefersToDatabase(t *testing.T) {
	t.Setenv("TMI_OAUTH_CALLBACK_URL", "")
	t.Setenv("OAUTH_CALLBACK_URL", "")
	cfg := &Config{Auth: AuthConfig{OAuth: OAuthConfig{
		CallbackURL: "http://localhost:8080/oauth2/callback",
	}}}
	found := findSetting(cfg.GetMigratableSettings(), "auth.oauth_callback_url")
	require.NotNil(t, found)
	assert.Equal(t, "config", found.Source)
	assert.False(t, found.Explicit, "an unset callback URL is the default, not an operator-supplied value")
}

// The legacy OAUTH_CALLBACK_URL is honored by auth/config.go but is NOT bound by
// this package's OAuthConfig.CallbackURL struct tag. It must therefore NOT count
// as "environment" here: doing so would report Explicit while Value was still the
// localhost struct default, and an Explicit default outranks the database — the
// fallback would become an override, which is a worse failure than the bug this
// fix addresses.
func TestGetMigratableSettings_OAuthCallbackURLIgnoresUnboundLegacyEnvName(t *testing.T) {
	t.Setenv("TMI_OAUTH_CALLBACK_URL", "")
	t.Setenv("OAUTH_CALLBACK_URL", "https://legacy.example.test/oauth2/callback")
	// Value is the struct default, because this package never reads the legacy name.
	cfg := &Config{Auth: AuthConfig{OAuth: OAuthConfig{
		CallbackURL: "http://localhost:8080/oauth2/callback",
	}}}
	found := findSetting(cfg.GetMigratableSettings(), "auth.oauth_callback_url")
	require.NotNil(t, found)
	assert.Equal(t, "config", found.Source)
	assert.False(t, found.Explicit,
		"an env name this package does not bind must not make the default Explicit")
}
