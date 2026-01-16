package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// Database defaults
	assert.Equal(t, "localhost", config.Database.Postgres.Host)
	assert.Equal(t, "5432", config.Database.Postgres.Port)
	assert.Equal(t, "postgres", config.Database.Postgres.User)
	assert.Equal(t, "tmi", config.Database.Postgres.Database)
	assert.Equal(t, "disable", config.Database.Postgres.SSLMode)

	assert.Equal(t, "localhost", config.Database.Redis.Host)
	assert.Equal(t, "6379", config.Database.Redis.Port)
	assert.Equal(t, 0, config.Database.Redis.DB)

	// Auth defaults
	assert.Equal(t, 3600, config.Auth.JWT.ExpirationSeconds)
	assert.Equal(t, "HS256", config.Auth.JWT.SigningMethod)
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

func TestGetDefaultOAuthProviders(t *testing.T) {
	providers := getDefaultOAuthProviders()

	assert.NotNil(t, providers)
	assert.Len(t, providers, 3)

	// Google provider
	google, ok := providers["google"]
	require.True(t, ok)
	assert.Equal(t, "google", google.ID)
	assert.Equal(t, "Google", google.Name)
	assert.True(t, google.Enabled)
	assert.Equal(t, "fa-brands fa-google", google.Icon)
	assert.Equal(t, "https://accounts.google.com/o/oauth2/auth", google.AuthorizationURL)
	assert.Equal(t, "https://oauth2.googleapis.com/token", google.TokenURL)
	assert.Contains(t, google.Scopes, "openid")

	// GitHub provider
	github, ok := providers["github"]
	require.True(t, ok)
	assert.Equal(t, "github", github.ID)
	assert.Equal(t, "GitHub", github.Name)
	assert.True(t, github.Enabled)
	assert.Equal(t, "token %s", github.AuthHeaderFormat)

	// Microsoft provider
	microsoft, ok := providers["microsoft"]
	require.True(t, ok)
	assert.Equal(t, "microsoft", microsoft.ID)
	assert.Equal(t, "Microsoft", microsoft.Name)
	assert.True(t, microsoft.Enabled)
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
func getReflectField(s interface{}, fieldName string) reflect.Value {
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
	t.Run("ValidConfig", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				Postgres: PostgresConfig{
					Host:     "localhost",
					Port:     "5432",
					User:     "postgres",
					Database: "tmi",
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: "6379",
				},
			},
		}
		err := config.validateDatabase()
		assert.NoError(t, err)
	})

	t.Run("MissingPostgresHost", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				Postgres: PostgresConfig{
					Host: "",
					Port: "5432",
					User: "postgres",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "postgres host is required")
	})

	t.Run("MissingPostgresPort", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				Postgres: PostgresConfig{
					Host: "localhost",
					Port: "",
					User: "postgres",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "postgres port is required")
	})

	t.Run("MissingPostgresUser", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				Postgres: PostgresConfig{
					Host: "localhost",
					Port: "5432",
					User: "",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "postgres user is required")
	})

	t.Run("MissingPostgresDatabase", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				Postgres: PostgresConfig{
					Host:     "localhost",
					Port:     "5432",
					User:     "postgres",
					Database: "",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "postgres database is required")
	})

	t.Run("MissingRedisHost", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				Postgres: PostgresConfig{
					Host:     "localhost",
					Port:     "5432",
					User:     "postgres",
					Database: "tmi",
				},
				Redis: RedisConfig{
					Host: "",
					Port: "6379",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "redis host is required")
	})

	t.Run("MissingRedisPort", func(t *testing.T) {
		config := &Config{
			Database: DatabaseConfig{
				Postgres: PostgresConfig{
					Host:     "localhost",
					Port:     "5432",
					User:     "postgres",
					Database: "tmi",
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: "",
				},
			},
		}
		err := config.validateDatabase()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "redis port is required")
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
  postgres:
    host: "db.example.com"
    port: "5433"
    user: "testuser"
    password: "testpass"
    database: "testdb"
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
		assert.Equal(t, "db.example.com", config.Database.Postgres.Host)
		assert.Equal(t, "5433", config.Database.Postgres.Port)
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
		t.Setenv("SERVER_PORT", "9999")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, "9999", config.Server.Port)
	})

	t.Run("OverridePostgresHost", func(t *testing.T) {
		t.Setenv("POSTGRES_HOST", "remote-db.example.com")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, "remote-db.example.com", config.Database.Postgres.Host)
	})

	t.Run("OverrideBooleanField", func(t *testing.T) {
		t.Setenv("SERVER_TLS_ENABLED", "true")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.True(t, config.Server.TLSEnabled)
	})

	t.Run("OverrideIntField", func(t *testing.T) {
		t.Setenv("JWT_EXPIRATION_SECONDS", "7200")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, 7200, config.Auth.JWT.ExpirationSeconds)
	})

	t.Run("OverrideDurationField", func(t *testing.T) {
		t.Setenv("SERVER_READ_TIMEOUT", "30s")

		config := getDefaultConfig()
		err := overrideWithEnv(config)

		assert.NoError(t, err)
		assert.Equal(t, 30*time.Second, config.Server.ReadTimeout)
	})
}

// =============================================================================
// Full Load Tests with Environment Variables
// =============================================================================

func TestLoadWithEnvAdministrator(t *testing.T) {
	// This test requires careful setup to avoid validation errors
	// The test is skipped as it requires a full valid config
	t.Skip("Requires full configuration setup")
}
