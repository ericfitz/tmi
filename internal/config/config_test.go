package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// =============================================================================
// Test Mode Detection Tests
// =============================================================================

func TestIsTestMode(t *testing.T) {
	// Create a config with IsTest set to false
	config := &Config{
		Logging: LoggingConfig{
			IsTest: false,
		},
	}

	// Should return true because we're running under 'go test'
	if !config.IsTestMode() {
		t.Error("Expected IsTestMode() to return true when running under 'go test'")
	}

	// Test explicit test flag
	config.Logging.IsTest = true
	if !config.IsTestMode() {
		t.Error("Expected IsTestMode() to return true when IsTest is explicitly set")
	}
}

func TestIsRunningInTest(t *testing.T) {
	// This should return true when running under 'go test'
	if !isRunningInTest() {
		t.Error("Expected isRunningInTest() to return true when running under 'go test'")
	}
}

// =============================================================================
// Default Config Tests
// =============================================================================

func TestGetDefaultConfig(t *testing.T) {
	config := getDefaultConfig()

	assert.NotNil(t, config)

	// Server defaults
	assert.Equal(t, "8080", config.Server.Port)
	assert.Equal(t, "0.0.0.0", config.Server.Interface)
	assert.Equal(t, 5*time.Second, config.Server.ReadTimeout)
	assert.Equal(t, 10*time.Second, config.Server.WriteTimeout)
	assert.Equal(t, 60*time.Second, config.Server.IdleTimeout)
	assert.False(t, config.Server.TLSEnabled)
	assert.True(t, config.Server.HTTPToHTTPSRedirect)

	// Database defaults - URL is required, defaults to empty
	assert.Equal(t, "", config.Database.URL)
	assert.Equal(t, "", config.Database.OracleWalletLocation)
	assert.Equal(t, 10, config.Database.ConnectionPool.MaxOpenConns)
	assert.Equal(t, 2, config.Database.ConnectionPool.MaxIdleConns)

	// Redis defaults
	assert.Equal(t, "localhost", config.Database.Redis.Host)
	assert.Equal(t, "6379", config.Database.Redis.Port)
	assert.Equal(t, 0, config.Database.Redis.DB)

	// Auth defaults
	assert.Equal(t, 3600, config.Auth.JWT.ExpirationSeconds)
	assert.Equal(t, "HS256", config.Auth.JWT.SigningMethod)
	assert.Equal(t, 7, config.Auth.JWT.RefreshTokenDays)
	assert.Equal(t, 7, config.Auth.JWT.SessionLifetimeDays)
	assert.Equal(t, "http://localhost:8080/oauth2/callback", config.Auth.OAuth.CallbackURL)

	// WebSocket defaults
	assert.Equal(t, 300, config.WebSocket.InactivityTimeoutSeconds)

	// Logging defaults
	assert.Equal(t, "info", config.Logging.Level)
	assert.True(t, config.Logging.IsDev)
	assert.False(t, config.Logging.IsTest)
	assert.Equal(t, 7, config.Logging.MaxAgeDays)
	assert.Equal(t, 100, config.Logging.MaxSizeMB)
	assert.True(t, config.Logging.SuppressUnauthenticatedLogs)
}

// =============================================================================
// Config Utility Method Tests
// =============================================================================

func TestGetJWTDuration(t *testing.T) {
	config := &Config{
		Auth: AuthConfig{
			JWT: JWTConfig{
				ExpirationSeconds: 3600,
			},
		},
	}

	duration := config.GetJWTDuration()
	assert.Equal(t, 3600*time.Second, duration)
	assert.Equal(t, time.Hour, duration)
}

func TestGetLogLevel(t *testing.T) {
	t.Run("DebugLevel", func(t *testing.T) {
		config := &Config{
			Logging: LoggingConfig{
				Level: "debug",
			},
		}
		assert.Equal(t, slogging.LogLevelDebug, config.GetLogLevel())
	})

	t.Run("InfoLevel", func(t *testing.T) {
		config := &Config{
			Logging: LoggingConfig{
				Level: "info",
			},
		}
		assert.Equal(t, slogging.LogLevelInfo, config.GetLogLevel())
	})

	t.Run("WarnLevel", func(t *testing.T) {
		config := &Config{
			Logging: LoggingConfig{
				Level: "warn",
			},
		}
		assert.Equal(t, slogging.LogLevelWarn, config.GetLogLevel())
	})

	t.Run("ErrorLevel", func(t *testing.T) {
		config := &Config{
			Logging: LoggingConfig{
				Level: "error",
			},
		}
		assert.Equal(t, slogging.LogLevelError, config.GetLogLevel())
	})
}

func TestGetWebSocketInactivityTimeout(t *testing.T) {
	config := &Config{
		WebSocket: WebSocketConfig{
			InactivityTimeoutSeconds: 600,
		},
	}

	timeout := config.GetWebSocketInactivityTimeout()
	assert.Equal(t, 600*time.Second, timeout)
	assert.Equal(t, 10*time.Minute, timeout)
}

func TestGetEnabledOAuthProviders(t *testing.T) {
	config := &Config{
		Auth: AuthConfig{
			OAuth: OAuthConfig{
				Providers: map[string]OAuthProviderConfig{
					"google": {
						ID:      "google",
						Name:    "Google",
						Enabled: true,
					},
					"github": {
						ID:      "github",
						Name:    "GitHub",
						Enabled: false,
					},
					"microsoft": {
						ID:      "microsoft",
						Name:    "Microsoft",
						Enabled: true,
					},
				},
			},
		},
	}

	enabled := config.GetEnabledOAuthProviders()

	assert.Len(t, enabled, 2)

	// Check that all returned providers are enabled
	for _, provider := range enabled {
		assert.True(t, provider.Enabled)
	}
}

func TestGetOAuthProvider(t *testing.T) {
	config := &Config{
		Auth: AuthConfig{
			OAuth: OAuthConfig{
				Providers: map[string]OAuthProviderConfig{
					"google": {
						ID:       "google",
						Name:     "Google",
						Enabled:  true,
						ClientID: "test-client-id",
					},
					"github": {
						ID:      "github",
						Name:    "GitHub",
						Enabled: false,
					},
				},
			},
		},
	}

	t.Run("ExistingEnabledProvider", func(t *testing.T) {
		provider, exists := config.GetOAuthProvider("google")
		assert.True(t, exists)
		assert.Equal(t, "google", provider.ID)
		assert.Equal(t, "test-client-id", provider.ClientID)
	})

	t.Run("ExistingDisabledProvider", func(t *testing.T) {
		_, exists := config.GetOAuthProvider("github")
		assert.False(t, exists)
	})

	t.Run("NonExistentProvider", func(t *testing.T) {
		_, exists := config.GetOAuthProvider("unknown")
		assert.False(t, exists)
	})
}

// =============================================================================
// SetFieldFromString Tests
// =============================================================================

func TestSetFieldFromString(t *testing.T) {
	t.Run("StringField", func(t *testing.T) {
		var s struct {
			Field string
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "test-value")
		assert.NoError(t, err)
		assert.Equal(t, "test-value", s.Field)
	})

	t.Run("BoolFieldTrue", func(t *testing.T) {
		var s struct {
			Field bool
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "true")
		assert.NoError(t, err)
		assert.True(t, s.Field)
	})

	t.Run("BoolFieldFalse", func(t *testing.T) {
		var s struct {
			Field bool
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "false")
		assert.NoError(t, err)
		assert.False(t, s.Field)
	})

	t.Run("BoolFieldInvalid", func(t *testing.T) {
		var s struct {
			Field bool
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "not-a-bool")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid bool value")
	})

	t.Run("IntField", func(t *testing.T) {
		var s struct {
			Field int
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "42")
		assert.NoError(t, err)
		assert.Equal(t, 42, s.Field)
	})

	t.Run("IntFieldInvalid", func(t *testing.T) {
		var s struct {
			Field int
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "not-an-int")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid int value")
	})

	t.Run("DurationField", func(t *testing.T) {
		var s struct {
			Field time.Duration
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "5s")
		assert.NoError(t, err)
		assert.Equal(t, 5*time.Second, s.Field)
	})

	t.Run("DurationFieldInvalid", func(t *testing.T) {
		var s struct {
			Field time.Duration
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "not-a-duration")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid duration value")
	})

	t.Run("StringSliceField", func(t *testing.T) {
		var s struct {
			Field []string
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "a, b, c")
		assert.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, s.Field)
	})

	t.Run("StringSliceFieldEmptyParts", func(t *testing.T) {
		var s struct {
			Field []string
		}
		field := getReflectField(&s, "Field")
		err := setFieldFromString(field, "a, , b, ,")
		assert.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, s.Field)
	})
}

// Helper function to get a reflect.Value for a struct field
func getReflectField(s any, fieldName string) reflect.Value {
	return reflect.ValueOf(s).Elem().FieldByName(fieldName)
}

// =============================================================================
// Validation Tests
// =============================================================================

func TestValidateServer(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		config := &Config{
			Server: ServerConfig{
				Port: "8080",
			},
		}
		err := config.validateServer()
		assert.NoError(t, err)
	})

	t.Run("MissingPort", func(t *testing.T) {
		config := &Config{
			Server: ServerConfig{
				Port: "",
			},
		}
		err := config.validateServer()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "port is required")
	})
}

func TestValidateDatabase(t *testing.T) {
	t.Run("ValidConfigWithURL", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				URL: "postgres://user:pass@localhost:5432/tmi?sslmode=disable",
				Redis: RedisConfig{
					Host: "localhost",
					Port: "6379",
				},
			},
		}
		err := config.validateDatabase()
		assert.NoError(t, err)
	})

	t.Run("ValidConfigWithRedisURL", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				URL: "postgres://user:pass@localhost:5432/tmi?sslmode=disable",
				Redis: RedisConfig{
					URL: "redis://localhost:6379/0",
				},
			},
		}
		err := config.validateDatabase()
		assert.NoError(t, err)
	})

	t.Run("MissingDatabaseURL", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				URL: "",
				Redis: RedisConfig{
					Host: "localhost",
					Port: "6379",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database url is required")
	})

	t.Run("MissingRedisConfig", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				URL: "postgres://user:pass@localhost:5432/tmi?sslmode=disable",
				Redis: RedisConfig{
					Host: "",
					Port: "",
					URL:  "",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "redis configuration is required")
	})

	t.Run("MissingRedisPortWhenNoURL", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				URL: "postgres://user:pass@localhost:5432/tmi?sslmode=disable",
				Redis: RedisConfig{
					Host: "localhost",
					Port: "",
					URL:  "",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "redis port is required when not using TMI_REDIS_URL")
	})
}

func TestValidateJWT(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		config := &Config{
			Auth: AuthConfig{
				JWT: JWTConfig{
					Secret:            "test-secret",
					ExpirationSeconds: 3600,
				},
			},
		}
		err := config.validateJWT()
		assert.NoError(t, err)
	})

	t.Run("MissingSecret", func(t *testing.T) {
		config := &Config{
			Auth: AuthConfig{
				JWT: JWTConfig{
					Secret:            "",
					ExpirationSeconds: 3600,
				},
			},
		}
		err := config.validateJWT()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "jwt secret is required")
	})

	t.Run("InvalidExpiration", func(t *testing.T) {
		config := &Config{
			Auth: AuthConfig{
				JWT: JWTConfig{
					Secret:            "test-secret",
					ExpirationSeconds: 0,
				},
			},
		}
		err := config.validateJWT()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "jwt expiration must be greater than 0")
	})

	t.Run("NegativeExpiration", func(t *testing.T) {
		config := &Config{
			Auth: AuthConfig{
				JWT: JWTConfig{
					Secret:            "test-secret",
					ExpirationSeconds: -100,
				},
			},
		}
		err := config.validateJWT()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "jwt expiration must be greater than 0")
	})
}

func TestValidateOAuth(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		config := &Config{
			Auth: AuthConfig{
				OAuth: OAuthConfig{
					CallbackURL: "http://localhost:8080/callback",
					Providers: map[string]OAuthProviderConfig{
						"google": {
							Enabled:      true,
							ClientID:     "test-client-id",
							ClientSecret: "test-client-secret",
						},
					},
				},
			},
		}
		err := config.validateOAuth()
		assert.NoError(t, err)
	})

	t.Run("MissingCallbackURL", func(t *testing.T) {
		config := &Config{
			Auth: AuthConfig{
				OAuth: OAuthConfig{
					CallbackURL: "",
				},
			},
		}
		err := config.validateOAuth()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "oauth callback url is required")
	})

	t.Run("NoEnabledProviders", func(t *testing.T) {
		config := &Config{
			Auth: AuthConfig{
				OAuth: OAuthConfig{
					CallbackURL: "http://localhost:8080/callback",
					Providers: map[string]OAuthProviderConfig{
						"google": {
							Enabled: false,
						},
					},
				},
			},
		}
		err := config.validateOAuth()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one oauth provider must be enabled")
	})

	t.Run("EnabledProviderMissingCredentials", func(t *testing.T) {
		config := &Config{
			Auth: AuthConfig{
				OAuth: OAuthConfig{
					CallbackURL: "http://localhost:8080/callback",
					Providers: map[string]OAuthProviderConfig{
						"google": {
							Enabled:  true,
							ClientID: "", // Missing
						},
					},
				},
			},
		}
		err := config.validateOAuth()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one oauth provider must be enabled")
	})
}

func TestValidateWebSocket(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		config := &Config{
			WebSocket: WebSocketConfig{
				InactivityTimeoutSeconds: 300,
			},
		}
		err := config.validateWebSocket()
		assert.NoError(t, err)
	})

	t.Run("MinimumTimeout", func(t *testing.T) {
		config := &Config{
			WebSocket: WebSocketConfig{
				InactivityTimeoutSeconds: 15,
			},
		}
		err := config.validateWebSocket()
		assert.NoError(t, err)
	})

	t.Run("TimeoutTooLow", func(t *testing.T) {
		config := &Config{
			WebSocket: WebSocketConfig{
				InactivityTimeoutSeconds: 14,
			},
		}
		err := config.validateWebSocket()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least 15 seconds")
	})

	t.Run("ZeroTimeout", func(t *testing.T) {
		config := &Config{
			WebSocket: WebSocketConfig{
				InactivityTimeoutSeconds: 0,
			},
		}
		err := config.validateWebSocket()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least 15 seconds")
	})
}

func TestValidateAdministrators(t *testing.T) {
	baseConfig := func() *Config {
		return &Config{
			Auth: AuthConfig{
				OAuth: OAuthConfig{
					Providers: map[string]OAuthProviderConfig{
						"google": {
							Enabled: true,
						},
					},
				},
			},
		}
	}

	t.Run("ValidUserAdmin", func(t *testing.T) {
		config := baseConfig()
		config.Administrators = []AdministratorConfig{
			{
				Provider:    "google",
				ProviderId:  "user@example.com",
				SubjectType: "user",
			},
		}
		err := config.validateAdministrators()
		assert.NoError(t, err)
	})

	t.Run("ValidUserAdminWithEmail", func(t *testing.T) {
		config := baseConfig()
		config.Administrators = []AdministratorConfig{
			{
				Provider:    "google",
				Email:       "user@example.com",
				SubjectType: "user",
			},
		}
		err := config.validateAdministrators()
		assert.NoError(t, err)
	})

	t.Run("ValidGroupAdmin", func(t *testing.T) {
		config := baseConfig()
		config.Administrators = []AdministratorConfig{
			{
				Provider:    "google",
				GroupName:   "admins",
				SubjectType: "group",
			},
		}
		err := config.validateAdministrators()
		assert.NoError(t, err)
	})

	t.Run("MissingProvider", func(t *testing.T) {
		config := baseConfig()
		config.Administrators = []AdministratorConfig{
			{
				Provider:    "",
				ProviderId:  "user@example.com",
				SubjectType: "user",
			},
		}
		err := config.validateAdministrators()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "provider is required")
	})

	t.Run("InvalidSubjectType", func(t *testing.T) {
		config := baseConfig()
		config.Administrators = []AdministratorConfig{
			{
				Provider:    "google",
				ProviderId:  "user@example.com",
				SubjectType: "invalid",
			},
		}
		err := config.validateAdministrators()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "subject_type must be 'user' or 'group'")
	})

	t.Run("UserMissingIdentifier", func(t *testing.T) {
		config := baseConfig()
		config.Administrators = []AdministratorConfig{
			{
				Provider:    "google",
				ProviderId:  "",
				Email:       "",
				SubjectType: "user",
			},
		}
		err := config.validateAdministrators()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user-type admin must have either provider_id or email")
	})

	t.Run("GroupMissingGroupName", func(t *testing.T) {
		config := baseConfig()
		config.Administrators = []AdministratorConfig{
			{
				Provider:    "google",
				GroupName:   "",
				SubjectType: "group",
			},
		}
		err := config.validateAdministrators()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "group-type admin must have group_name")
	})

	t.Run("UnconfiguredProvider", func(t *testing.T) {
		config := baseConfig()
		config.Administrators = []AdministratorConfig{
			{
				Provider:    "unknown-provider",
				ProviderId:  "user@example.com",
				SubjectType: "user",
			},
		}
		err := config.validateAdministrators()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "provider 'unknown-provider' is not configured or not enabled")
	})
}

func TestIsProviderConfigured(t *testing.T) {
	config := &Config{
		Auth: AuthConfig{
			OAuth: OAuthConfig{
				Providers: map[string]OAuthProviderConfig{
					"google": {
						Enabled: true,
					},
					"github": {
						Enabled: false,
					},
				},
			},
			SAML: SAMLConfig{
				Enabled: true,
				Providers: map[string]SAMLProviderConfig{
					"okta": {
						Enabled: true,
					},
					"azure": {
						Enabled: false,
					},
				},
			},
		},
	}

	t.Run("EnabledOAuthProvider", func(t *testing.T) {
		assert.True(t, config.isProviderConfigured("google"))
	})

	t.Run("DisabledOAuthProvider", func(t *testing.T) {
		assert.False(t, config.isProviderConfigured("github"))
	})

	t.Run("EnabledSAMLProvider", func(t *testing.T) {
		assert.True(t, config.isProviderConfigured("okta"))
	})

	t.Run("DisabledSAMLProvider", func(t *testing.T) {
		assert.False(t, config.isProviderConfigured("azure"))
	})

	t.Run("UnknownProvider", func(t *testing.T) {
		assert.False(t, config.isProviderConfigured("unknown"))
	})
}

// =============================================================================
// ServerConfig New Fields Tests
// =============================================================================

func TestServerConfig_TrustedProxies(t *testing.T) {
	// Test YAML parsing
	yamlData := []byte(`
server:
  trusted_proxies:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
  ratelimit_public_rpm: 20
`)
	var cfg Config
	err := yaml.Unmarshal(yamlData, &cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.0/8", "172.16.0.0/12"}, cfg.Server.TrustedProxies)
	assert.Equal(t, 20, cfg.Server.RateLimitPublicRPM)
}

func TestServerConfig_RateLimitPublicRPM_Default(t *testing.T) {
	yamlData := []byte(`
server:
  port: "8080"
`)
	var cfg Config
	err := yaml.Unmarshal(yamlData, &cfg)
	require.NoError(t, err)
	// Zero value — caller must apply default of 10
	assert.Equal(t, 0, cfg.Server.RateLimitPublicRPM)
}

// =============================================================================
// YAML Loading Tests
// =============================================================================

func TestLoadFromYAML(t *testing.T) {
	t.Run("ValidYAMLFile", func(t *testing.T) {
		// Create a temporary YAML file
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.yaml")

		yamlContent := `
server:
  port: "9090"
  interface: "127.0.0.1"
database:
  url: "postgres://testuser:testpass@db.example.com:5433/testdb?sslmode=disable"
  redis:
    host: "redis.example.com"
    port: "6380"
logging:
  level: "debug"
`
		err := os.WriteFile(configFile, []byte(yamlContent), 0600)
		require.NoError(t, err)

		config := getDefaultConfig()
		err = loadFromYAML(config, configFile)

		assert.NoError(t, err)
		assert.Equal(t, "9090", config.Server.Port)
		assert.Equal(t, "127.0.0.1", config.Server.Interface)
		assert.Equal(t, "postgres://testuser:testpass@db.example.com:5433/testdb?sslmode=disable", config.Database.URL)
		assert.Equal(t, "redis.example.com", config.Database.Redis.Host)
		assert.Equal(t, "6380", config.Database.Redis.Port)
		assert.Equal(t, "debug", config.Logging.Level)
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		config := getDefaultConfig()
		err := loadFromYAML(config, "/nonexistent/path/config.yaml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read config file")
	})

	t.Run("InvalidYAMLSyntax", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "invalid.yaml")

		invalidYAML := `
server:
  port: 9090
  interface: [invalid yaml
`
		err := os.WriteFile(configFile, []byte(invalidYAML), 0600)
		require.NoError(t, err)

		config := getDefaultConfig()
		err = loadFromYAML(config, configFile)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse YAML config")
	})
}

// =============================================================================
// Environment Variable Override Tests
// =============================================================================

func TestOverrideWithEnv(t *testing.T) {
	t.Run("OverrideServerPort", func(t *testing.T) {
		t.Setenv("TMI_SERVER_PORT", "9999")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, "9999", config.Server.Port)
	})

	t.Run("OverrideDatabaseURL", func(t *testing.T) {
		t.Setenv("TMI_DATABASE_URL", "postgres://user:pass@remote-db.example.com:5432/tmi?sslmode=require")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, "postgres://user:pass@remote-db.example.com:5432/tmi?sslmode=require", config.Database.URL)
	})

	t.Run("OverrideRedisURL", func(t *testing.T) {
		t.Setenv("TMI_REDIS_URL", "redis://redis.example.com:6379/1")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, "redis://redis.example.com:6379/1", config.Database.Redis.URL)
	})

	t.Run("OverrideBooleanField", func(t *testing.T) {
		t.Setenv("TMI_SERVER_TLS_ENABLED", "true")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.True(t, config.Server.TLSEnabled)
	})

	t.Run("OverrideIntField", func(t *testing.T) {
		t.Setenv("TMI_JWT_EXPIRATION_SECONDS", "7200")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, 7200, config.Auth.JWT.ExpirationSeconds)
	})

	t.Run("OverrideDurationField", func(t *testing.T) {
		t.Setenv("TMI_SERVER_READ_TIMEOUT", "30s")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, 30*time.Second, config.Server.ReadTimeout)
	})
}

// =============================================================================
// Heroku PORT Compatibility Tests
// =============================================================================

func TestHerokuPortFallback(t *testing.T) {
	// Helper to create a minimal valid config file for testing Load()
	createMinimalConfigFile := func(t *testing.T) string {
		t.Helper()
		content := `
server:
  port: "8080"
database:
  url: "postgres://test:test@localhost:5432/test"
  redis:
    url: "redis://localhost:6379/0"
auth:
  build_mode: "test"
  jwt:
    secret: "test-secret-for-jwt"
    expiration_seconds: 3600
  oauth:
    callback_url: "http://localhost:8080/oauth2/callback"
`
		tmpFile, err := os.CreateTemp(t.TempDir(), "config-*.yml")
		require.NoError(t, err)
		_, err = tmpFile.WriteString(content)
		require.NoError(t, err)
		require.NoError(t, tmpFile.Close())
		return tmpFile.Name()
	}

	t.Run("PortFallbackWhenTmiServerPortNotSet", func(t *testing.T) {
		// Set PORT (Heroku's env var) but not TMI_SERVER_PORT
		t.Setenv("PORT", "12345")

		// Set required OAuth provider for validation
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_ENABLED", "true")
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_CLIENT_ID", "test-client-id")
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET", "test-client-secret")

		configFile := createMinimalConfigFile(t)
		config, err := Load(configFile)

		assert.NoError(t, err)
		assert.Equal(t, "12345", config.Server.Port, "PORT should be used when TMI_SERVER_PORT is not set")
	})

	t.Run("TmiServerPortTakesPrecedenceOverPort", func(t *testing.T) {
		// Set both PORT and TMI_SERVER_PORT
		t.Setenv("PORT", "12345")
		t.Setenv("TMI_SERVER_PORT", "9999")

		// Set required OAuth provider for validation
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_ENABLED", "true")
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_CLIENT_ID", "test-client-id")
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET", "test-client-secret")

		configFile := createMinimalConfigFile(t)
		config, err := Load(configFile)

		assert.NoError(t, err)
		assert.Equal(t, "9999", config.Server.Port, "TMI_SERVER_PORT should take precedence over PORT")
	})

	t.Run("DefaultPortWhenNeitherSet", func(t *testing.T) {
		// Don't set PORT or TMI_SERVER_PORT - use defaults
		// Set required OAuth provider for validation
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_ENABLED", "true")
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_CLIENT_ID", "test-client-id")
		t.Setenv("OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET", "test-client-secret")

		configFile := createMinimalConfigFile(t)
		config, err := Load(configFile)

		assert.NoError(t, err)
		assert.Equal(t, "8080", config.Server.Port, "Default port should be 8080 when neither PORT nor TMI_SERVER_PORT is set")
	})
}

// =============================================================================
// Observability Config Tests
// =============================================================================

func TestObservabilityConfigDefaults(t *testing.T) {
	cfg := getDefaultConfig()
	assert.False(t, cfg.Observability.Enabled, "observability should be disabled by default")
	assert.Equal(t, 1.0, cfg.Observability.SamplingRate, "sampling rate should default to 1.0")
	assert.Equal(t, 0, cfg.Observability.PrometheusPort, "prometheus port should default to 0 (disabled)")
}

func TestObservabilityConfigEnvOverrides(t *testing.T) {
	t.Setenv("TMI_OTEL_ENABLED", "true")
	t.Setenv("TMI_OTEL_SAMPLING_RATE", "0.5")
	t.Setenv("TMI_OTEL_PROMETHEUS_PORT", "9090")

	cfg := getDefaultConfig()
	err := overrideWithEnv(cfg)
	assert.NoError(t, err)

	assert.True(t, cfg.Observability.Enabled)
	assert.Equal(t, 0.5, cfg.Observability.SamplingRate)
	assert.Equal(t, 9090, cfg.Observability.PrometheusPort)
}

// =============================================================================
// ContentOAuth Config Tests
// =============================================================================

// setBaselineEnv sets the minimum environment variables required for Load("")
// to succeed. As of #415, Load()-time Validate() checks only CategoryBootstrap
// config (server, database, JWT secret, CORS); operational config such as OAuth
// providers is DB-seeded and no longer validated at load time, so only the
// bootstrap env vars below are needed.
//
// TMI_BUILD_MODE is set because auth.build_mode is a Required bootstrap setting
// (enforced by Config.ValidateRequired in Load()); getDefaultConfig() supplies
// no default for it, so Load("") with no config file needs it from the env.
func setBaselineEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TMI_DATABASE_URL", "postgres://test:test@localhost:5432/test")
	t.Setenv("TMI_REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("TMI_JWT_SECRET", "test-secret-for-jwt")
	t.Setenv("TMI_BUILD_MODE", "test")
}

func TestConfig_ContentOAuth_EnvOverride_DiscoversProviders(t *testing.T) {
	setBaselineEnv(t)
	t.Setenv("TMI_CONTENT_OAUTH_CALLBACK_URL", "http://localhost:8080/cc")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_ENABLED", "true")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_CLIENT_ID", "cid")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_CLIENT_SECRET", "sec")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_AUTH_URL", "http://a")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_TOKEN_URL", "http://t")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_REQUIRED_SCOPES", "read write")
	t.Setenv("TMI_CONTENT_TOKEN_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	cfg, err := Load("")
	require.NoError(t, err)
	m, ok := cfg.ContentOAuth.Providers["mock"]
	require.True(t, ok)
	assert.True(t, m.Enabled)
	assert.Equal(t, "cid", m.ClientID)
	assert.Equal(t, []string{"read", "write"}, m.RequiredScopes)
}

// TestConfig_ContentOAuth_Load_DoesNotValidateOperationalConfig pins the #415
// contract: content OAuth provider config is CategoryOperational (DB-seeded and
// runtime-editable), so Load()-time Validate() must NOT reject a config where a
// content OAuth provider is enabled without an encryption key. Before #415 this
// test asserted the opposite (that Load() failed with a
// "TMI_CONTENT_TOKEN_ENCRYPTION_KEY" error) — that was load-time validation of
// operational config, which is wrong now that the config files are
// bootstrap-only. The validator-level rule "enabled provider requires a key" is
// still enforced and is covered directly by
// TestContentOAuthConfig_Validate_RequiresKeyWhenEnabled in content_oauth_test.go.
func TestConfig_ContentOAuth_Load_DoesNotValidateOperationalConfig(t *testing.T) {
	setBaselineEnv(t)
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_ENABLED", "true")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_CLIENT_ID", "c")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_AUTH_URL", "http://a")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_TOKEN_URL", "http://t")
	// TMI_CONTENT_TOKEN_ENCRYPTION_KEY intentionally unset: operational config
	// is no longer validated at load time, so Load() must still succeed.
	cfg, err := Load("")
	require.NoError(t, err)
	require.NotNil(t, cfg)
}

// =============================================================================
// MicrosoftConfig Tests
// =============================================================================

func TestMicrosoftConfig_IsConfigured(t *testing.T) {
	cases := []struct {
		name   string
		cfg    MicrosoftConfig
		wantOK bool
	}{
		{name: "complete", cfg: MicrosoftConfig{Enabled: true, TenantID: "t", ClientID: "c", ApplicationObjectID: "a"}, wantOK: true},
		{name: "disabled", cfg: MicrosoftConfig{Enabled: false, TenantID: "t", ClientID: "c", ApplicationObjectID: "a"}, wantOK: false},
		{name: "missing tenant", cfg: MicrosoftConfig{Enabled: true, ClientID: "c", ApplicationObjectID: "a"}, wantOK: false},
		{name: "missing client", cfg: MicrosoftConfig{Enabled: true, TenantID: "t", ApplicationObjectID: "a"}, wantOK: false},
		{name: "missing app object", cfg: MicrosoftConfig{Enabled: true, TenantID: "t", ClientID: "c"}, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantOK, tc.cfg.IsConfigured())
		})
	}
}

// =============================================================================
// Drift Detection Tests
// =============================================================================

func TestOperationalKeysInFile_DetectsDrift(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/c.yml"
	// websocket.inactivity_timeout_seconds is an operational key.
	yamlText := "server:\n  port: \"8080\"\nwebsocket:\n  inactivity_timeout_seconds: 300\n"
	if err := os.WriteFile(p, []byte(yamlText), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := OperationalKeysInFile(p)
	if err != nil {
		t.Fatalf("OperationalKeysInFile: %v", err)
	}
	found := false
	for _, k := range keys {
		if k == "websocket.inactivity_timeout_seconds" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected websocket.inactivity_timeout_seconds in drift list, got %v", keys)
	}
	for _, k := range keys {
		if k == "server.port" {
			t.Errorf("server.port is a bootstrap key and must not appear in the drift list, got %v", keys)
		}
	}
}

// --- Secret reference resolution wiring (Config.ResolveSecretReferences) ---

func TestResolveSecretReferences_EnvReference(t *testing.T) {
	t.Setenv("TEST_JWT", "resolved-secret")
	cfg := &Config{}
	cfg.Auth.JWT.Secret = "env://TEST_JWT"

	if err := cfg.ResolveSecretReferences(context.Background(), nil); err != nil {
		t.Fatalf("ResolveSecretReferences: %v", err)
	}
	if cfg.Auth.JWT.Secret != "resolved-secret" {
		t.Errorf("Auth.JWT.Secret = %q, want resolved-secret", cfg.Auth.JWT.Secret)
	}
}

func TestResolveSecretReferences_InlinePassThrough(t *testing.T) {
	cfg := &Config{}
	cfg.Auth.JWT.Secret = "a-plain-inline-jwt-secret"
	cfg.Database.URL = "postgres://u:p@h:5432/db"

	if err := cfg.ResolveSecretReferences(context.Background(), nil); err != nil {
		t.Fatalf("ResolveSecretReferences: %v", err)
	}
	if cfg.Auth.JWT.Secret != "a-plain-inline-jwt-secret" {
		t.Errorf("inline JWT secret changed: %q", cfg.Auth.JWT.Secret)
	}
	if cfg.Database.URL != "postgres://u:p@h:5432/db" {
		t.Errorf("inline DB URL changed: %q", cfg.Database.URL)
	}
}

func TestResolveSecretReferences_VaultReference(t *testing.T) {
	vault := &fakeVaultResolver{values: map[string]string{
		"kv/data/db": "postgres://vault-user:vault-pass@db:5432/tmi",
	}}
	cfg := &Config{}
	cfg.Database.URL = "vault://kv/data/db"
	cfg.Auth.JWT.Secret = "inline-stays"

	if err := cfg.ResolveSecretReferences(context.Background(), vault); err != nil {
		t.Fatalf("ResolveSecretReferences: %v", err)
	}
	if cfg.Database.URL != "postgres://vault-user:vault-pass@db:5432/tmi" {
		t.Errorf("Database.URL = %q, want vault-resolved value", cfg.Database.URL)
	}
	if cfg.Auth.JWT.Secret != "inline-stays" {
		t.Errorf("inline JWT secret changed: %q", cfg.Auth.JWT.Secret)
	}
}

func TestResolveSecretReferences_VaultWithoutResolver(t *testing.T) {
	cfg := &Config{}
	cfg.Auth.JWT.Secret = "vault://kv/data/jwt"

	err := cfg.ResolveSecretReferences(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for vault:// reference with nil resolver, got nil")
	}
}

func TestResolveSecretsConfigReferences_EnvToken(t *testing.T) {
	t.Setenv("TEST_VAULT_TOKEN", "resolved-token")
	cfg := &Config{}
	cfg.Secrets.VaultToken = "env://TEST_VAULT_TOKEN"

	if err := cfg.ResolveSecretsConfigReferences(context.Background()); err != nil {
		t.Fatalf("ResolveSecretsConfigReferences: %v", err)
	}
	if cfg.Secrets.VaultToken != "resolved-token" {
		t.Errorf("Secrets.VaultToken = %q, want resolved-token", cfg.Secrets.VaultToken)
	}
}

func TestResolveSecretsConfigReferences_VaultTokenRejected(t *testing.T) {
	cfg := &Config{}
	cfg.Secrets.VaultToken = "vault://kv/data/token"

	if err := cfg.ResolveSecretsConfigReferences(context.Background()); err == nil {
		t.Fatal("expected error: vault:// for secrets.vault_token must be rejected")
	}
}

// =============================================================================
// ValidateRequired Tests
// =============================================================================

// minimalValidConfig returns a Config that satisfies all 5 Required bootstrap
// settings (server.port, server.interface, database.url, auth.build_mode,
// auth.jwt.secret) by combining getDefaultConfig() defaults with explicit
// overrides for the fields that have no default.
func minimalValidConfig() *Config {
	cfg := getDefaultConfig()
	cfg.Database.URL = "postgres://test:test@localhost:5432/test"
	cfg.Auth.BuildMode = "dev"
	cfg.Auth.JWT.Secret = "test-jwt-secret"
	return cfg
}

func TestValidateRequired_PassesWhenAllRequiredFieldsPresent(t *testing.T) {
	cfg := minimalValidConfig()
	if err := cfg.ValidateRequired(); err != nil {
		t.Fatalf("ValidateRequired() should pass with all required fields set, got: %v", err)
	}
}

func TestValidateRequired_FailsWhenDatabaseURLMissing(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Database.URL = ""
	err := cfg.ValidateRequired()
	if err == nil {
		t.Fatal("ValidateRequired() should fail when database.url is empty")
	}
	if !strings.Contains(err.Error(), "database.url") {
		t.Errorf("error should mention database.url, got: %v", err)
	}
}

func TestValidateRequired_FailsWhenJWTSecretMissing(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.JWT.Secret = ""
	err := cfg.ValidateRequired()
	if err == nil {
		t.Fatal("ValidateRequired() should fail when auth.jwt.secret is empty")
	}
	if !strings.Contains(err.Error(), "auth.jwt.secret") {
		t.Errorf("error should mention auth.jwt.secret, got: %v", err)
	}
}

func TestValidateRequired_FailsWhenBuildModeMissing(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Auth.BuildMode = ""
	err := cfg.ValidateRequired()
	if err == nil {
		t.Fatal("ValidateRequired() should fail when auth.build_mode is empty")
	}
	if !strings.Contains(err.Error(), "auth.build_mode") {
		t.Errorf("error should mention auth.build_mode, got: %v", err)
	}
}

func TestValidateRequired_CollectsMultipleMissingKeys(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Database.URL = ""
	cfg.Auth.JWT.Secret = ""
	err := cfg.ValidateRequired()
	if err == nil {
		t.Fatal("ValidateRequired() should fail when multiple required fields are empty")
	}
	if !strings.Contains(err.Error(), "database.url") {
		t.Errorf("error should mention database.url, got: %v", err)
	}
	if !strings.Contains(err.Error(), "auth.jwt.secret") {
		t.Errorf("error should mention auth.jwt.secret, got: %v", err)
	}
}

func TestValidateRequired_LoadFailsWhenRequiredKeyMissing(t *testing.T) {
	// Verify that Load() calls ValidateRequired() by showing Load fails
	// when auth.build_mode (Required + Bootstrap) has no effective value.
	// We use Load("") (no file) + env vars that satisfy Validate() but
	// intentionally leave auth.build_mode empty (no TMI_BUILD_MODE set).
	t.Setenv("TMI_DATABASE_URL", "postgres://test:test@localhost:5432/test")
	t.Setenv("TMI_REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("TMI_JWT_SECRET", "test-jwt-secret")
	// TMI_BUILD_MODE intentionally not set — getDefaultConfig().Auth.BuildMode == ""
	_, err := Load("")
	if err == nil {
		t.Fatal("Load() should fail when auth.build_mode is empty (Required bootstrap key)")
	}
	if !strings.Contains(err.Error(), "auth.build_mode") {
		t.Errorf("error should mention auth.build_mode, got: %v", err)
	}
}

// TestGetCookieDomain verifies the cookie domain is taken ONLY from explicit
// config and is never inferred from the bind/listen address. Regression test
// for #497: deriving the domain from Server.Interface (default "0.0.0.0")
// produced Domain=0.0.0.0, which browsers reject, silently dropping the auth
// cookies.
func TestGetCookieDomain(t *testing.T) {
	t.Run("default config yields host-only cookie (empty domain)", func(t *testing.T) {
		config := getDefaultConfig()
		// Sanity: the default binds to the wildcard address that caused #497.
		assert.Equal(t, "0.0.0.0", config.Server.Interface)
		assert.Empty(t, config.GetCookieDomain(),
			"cookie domain must be empty (host-only) and never derived from the bind address")
	})

	t.Run("does not infer from base URL host", func(t *testing.T) {
		config := getDefaultConfig()
		config.Server.BaseURL = "https://api.example.com:8443"
		assert.Empty(t, config.GetCookieDomain(),
			"cookie domain must not be inferred from the base URL")
	})

	t.Run("returns explicitly configured domain", func(t *testing.T) {
		config := getDefaultConfig()
		config.Auth.Cookie.Domain = "example.com"
		assert.Equal(t, "example.com", config.GetCookieDomain())
	})
}
