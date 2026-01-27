package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTokenGeneration(t *testing.T) {
	// Skip this test as it requires database manager and Redis
	t.Skip("Token generation requires database manager and Redis - test structure only")

	// Create service with test config
	config := Config{
		JWT: JWTConfig{
			Secret:            "test-secret-key-for-testing",
			ExpirationSeconds: 3600,
		},
	}

	service := &Service{
		config: config,
		// dbManager is nil - will cause panic
	}

	// Create test user
	user := User{
		InternalUUID:   "test-user-internal-uuid",
		Provider:       "tmi",
		ProviderUserID: "test-user-provider-id",
		Email:          "test@example.com",
		Name:           "Test User",
	}

	// Test token generation
	ctx := context.Background()
	tokens, err := service.GenerateTokens(ctx, user)

	// Note: This will fail without Redis, but tests the core logic
	if err != nil {
		t.Logf("Expected error without Redis: %v", err)
		assert.Contains(t, err.Error(), "failed to store refresh token")
		return
	}

	// If Redis is available, validate tokens
	assert.NotEmpty(t, tokens.AccessToken)
	assert.NotEmpty(t, tokens.RefreshToken)
	assert.Equal(t, "Bearer", tokens.TokenType)
	assert.Equal(t, 3600, tokens.ExpiresIn)
}

func TestTokenValidation(t *testing.T) {
	config := Config{
		JWT: JWTConfig{
			Secret:            "test-secret-key-for-testing",
			ExpirationSeconds: 3600,
			SigningMethod:     "HS256",
		},
	}

	// Initialize key manager
	keyManager, err := NewJWTKeyManager(config.JWT)
	if err != nil {
		t.Fatalf("Failed to create key manager: %v", err)
	}

	service := &Service{
		config:     config,
		keyManager: keyManager,
	}

	tests := []struct {
		name        string
		token       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Empty token",
			token:       "",
			expectError: true,
			errorMsg:    "token contains an invalid number of segments",
		},
		{
			name:        "Invalid format",
			token:       "invalid.token.format",
			expectError: true,
			errorMsg:    "failed to verify token",
		},
		{
			name:        "Wrong signing method",
			token:       "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWV9.invalid",
			expectError: true,
			errorMsg:    "unexpected signing method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.ValidateToken(tt.token)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUserProviderLinking(t *testing.T) {
	// Skip this test as it requires database
	t.Skip("Requires database connection - testing structure only")

	// This test would verify provider linking logic
	t.Logf("Would test LinkUserProvider, GetUserByProviderID, UnlinkUserProvider methods")
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid config",
			config: Config{
				Database: DatabaseConfig{
					URL: "postgres://postgres@localhost:5432/test?sslmode=disable",
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: "6379",
				},
				JWT: JWTConfig{
					SigningMethod:     "HS256",
					Secret:            "valid-secret-key",
					ExpirationSeconds: 3600,
				},
				OAuth: OAuthConfig{
					CallbackURL: "http://localhost:8080/oauth2/callback",
					Providers: map[string]OAuthProviderConfig{
						"google": {
							ID:      "google",
							Enabled: true,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "Missing JWT secret",
			config: Config{
				Database: DatabaseConfig{
					URL: "postgres://postgres@localhost:5432/test?sslmode=disable",
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: "6379",
				},
				JWT: JWTConfig{
					SigningMethod:     "HS256",
					Secret:            "", // Missing secret
					ExpirationSeconds: 3600,
				},
				OAuth: OAuthConfig{
					CallbackURL: "http://localhost:8080/oauth2/callback",
					Providers: map[string]OAuthProviderConfig{
						"google": {
							ID:      "google",
							Enabled: true,
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "jwt secret is required",
		},
		{
			name: "Invalid expiration",
			config: Config{
				Database: DatabaseConfig{
					URL: "postgres://postgres@localhost:5432/test?sslmode=disable",
				},
				Redis: RedisConfig{
					Host: "localhost",
					Port: "6379",
				},
				JWT: JWTConfig{
					Secret:            "valid-secret",
					ExpirationSeconds: 0, // Invalid expiration
				},
				OAuth: OAuthConfig{
					CallbackURL: "http://localhost:8080/oauth2/callback",
					Providers: map[string]OAuthProviderConfig{
						"google": {
							ID:      "google",
							Enabled: true,
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "jwt expiration must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ValidateConfig()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProviderConfiguration(t *testing.T) {
	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:               "google",
					Name:             "Google",
					Enabled:          true,
					ClientID:         "google-client-id",
					ClientSecret:     "google-client-secret",
					AuthorizationURL: "https://accounts.google.com/o/oauth2/auth",
					TokenURL:         "https://oauth2.googleapis.com/token",
					UserInfo: []UserInfoEndpoint{
						{
							URL:    "https://www.googleapis.com/oauth2/v3/userinfo",
							Claims: map[string]string{},
						},
					},
					Icon: "google",
				},
				"disabled": {
					ID:      "disabled",
					Name:    "Disabled Provider",
					Enabled: false,
				},
			},
		},
	}

	// Test getting enabled providers
	enabledProviders := config.GetEnabledProviders()
	assert.Len(t, enabledProviders, 1)
	assert.Equal(t, "google", enabledProviders[0].ID)

	// Test getting specific provider
	provider, exists := config.GetProvider("google")
	assert.True(t, exists)
	assert.Equal(t, "Google", provider.Name)

	// Test getting non-existent provider
	_, exists = config.GetProvider("nonexistent")
	assert.False(t, exists)

	// Test getting disabled provider
	_, exists = config.GetProvider("disabled")
	assert.False(t, exists) // Should not return disabled providers
}

func TestJWTDuration(t *testing.T) {
	config := Config{
		JWT: JWTConfig{
			ExpirationSeconds: 3600,
		},
	}

	duration := config.GetJWTDuration()
	assert.Equal(t, time.Hour, duration)

	// Test zero value
	config.JWT.ExpirationSeconds = 0
	duration = config.GetJWTDuration()
	assert.Equal(t, 0*time.Second, duration) // GetJWTDuration doesn't provide defaults
}
