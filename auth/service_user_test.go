package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for utility functions and helpers in the auth package
// Tests that require database mocking are structured for future implementation

func TestService_DeriveIssuer(t *testing.T) {
	tests := []struct {
		name           string
		callbackURL    string
		expectedIssuer string
	}{
		{
			name:           "derives from callback URL",
			callbackURL:    "https://app.example.com/oauth2/callback",
			expectedIssuer: "https://app.example.com",
		},
		{
			name:           "returns localhost default when callback URL empty",
			callbackURL:    "",
			expectedIssuer: "http://localhost:8080",
		},
		{
			name:           "handles callback URL without path",
			callbackURL:    "https://auth.example.com",
			expectedIssuer: "https://auth.example.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := &Service{
				config: Config{
					OAuth: OAuthConfig{
						CallbackURL: tc.callbackURL,
					},
				},
			}

			issuer := service.deriveIssuer()
			assert.Equal(t, tc.expectedIssuer, issuer)
		})
	}
}

func TestService_GetIssuer(t *testing.T) {
	t.Run("derives issuer from callback URL", func(t *testing.T) {
		service := &Service{
			config: Config{
				OAuth: OAuthConfig{
					CallbackURL: "https://auth.example.com/oauth2/callback",
				},
			},
		}

		issuer := service.getIssuer()
		assert.Equal(t, "https://auth.example.com", issuer)
	})

	t.Run("returns localhost default when callback URL empty", func(t *testing.T) {
		service := &Service{
			config: Config{
				OAuth: OAuthConfig{
					CallbackURL: "",
				},
			},
		}

		issuer := service.getIssuer()
		assert.Equal(t, "http://localhost:8080", issuer)
	})

	t.Run("recalculates when config changes", func(t *testing.T) {
		service := &Service{
			config: Config{
				OAuth: OAuthConfig{
					CallbackURL: "https://auth.example.com/oauth2/callback",
				},
			},
		}

		issuer1 := service.getIssuer()
		assert.Equal(t, "https://auth.example.com", issuer1)

		// Modify config - getIssuer recalculates
		service.config.OAuth.CallbackURL = "https://different.example.com/oauth2/callback"

		issuer2 := service.getIssuer()
		assert.Equal(t, "https://different.example.com", issuer2)
	})
}

func TestService_ValidateToken(t *testing.T) {
	keyManager, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret-key-for-unit-tests",
	})
	require.NoError(t, err)

	service := &Service{
		config: Config{
			JWT: JWTConfig{
				Secret:            "test-secret-key-for-unit-tests",
				ExpirationSeconds: 3600,
				SigningMethod:     "HS256",
			},
		},
		keyManager: keyManager,
	}

	t.Run("rejects empty token", func(t *testing.T) {
		_, err := service.ValidateToken("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid number of segments")
	})

	t.Run("rejects malformed token", func(t *testing.T) {
		_, err := service.ValidateToken("not.a.valid.jwt.token")
		assert.Error(t, err)
	})

	t.Run("rejects token with wrong signing method", func(t *testing.T) {
		// RS256 signed token will fail HS256 verification
		// #nosec G101 -- This is a test token, not a real credential
		rsaToken := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.invalid"
		_, err := service.ValidateToken(rsaToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected signing method")
	})
}

// Test helper functions

func TestCreateTestUser(t *testing.T) {
	t.Run("creates user with correct defaults", func(t *testing.T) {
		user := CreateTestUser("google", "test@gmail.com")

		assert.NotEmpty(t, user.InternalUUID)
		assert.Equal(t, "google", user.Provider)
		assert.Equal(t, "test@gmail.com", user.Email)
		assert.Equal(t, "test@gmail.com-provider-id", user.ProviderUserID)
		assert.Equal(t, "Test User", user.Name)
		assert.True(t, user.EmailVerified)
		assert.False(t, user.IsAdmin)
		assert.NotNil(t, user.CreatedAt)
		assert.NotNil(t, user.ModifiedAt)
		assert.NotNil(t, user.LastLogin)
	})
}

func TestCreateTestUserWithRole(t *testing.T) {
	t.Run("creates admin user", func(t *testing.T) {
		user := CreateTestUserWithRole("tmi", "admin@example.com", true)
		assert.True(t, user.IsAdmin)
	})

	t.Run("creates non-admin user", func(t *testing.T) {
		user := CreateTestUserWithRole("tmi", "user@example.com", false)
		assert.False(t, user.IsAdmin)
	})
}

func TestCreateTestUserWithGroups(t *testing.T) {
	t.Run("creates user with groups", func(t *testing.T) {
		groups := []string{"developers", "admins", "security"}
		user := CreateTestUserWithGroups("tmi", "user@example.com", groups)

		assert.Equal(t, groups, user.Groups)
	})
}

func TestTestUsers(t *testing.T) {
	t.Run("Admin user has correct properties", func(t *testing.T) {
		assert.True(t, TestUsers.Admin.IsAdmin)
		assert.Equal(t, "admin@example.com", TestUsers.Admin.Email)
		assert.Contains(t, TestUsers.Admin.Groups, "admins")
	})

	t.Run("Regular user has correct properties", func(t *testing.T) {
		assert.False(t, TestUsers.Regular.IsAdmin)
		assert.Equal(t, "user@example.com", TestUsers.Regular.Email)
	})

	t.Run("External user has correct properties", func(t *testing.T) {
		assert.Equal(t, "google", TestUsers.External.Provider)
		assert.Equal(t, "external@gmail.com", TestUsers.External.Email)
	})
}

func TestValidateTokenClaims(t *testing.T) {
	t.Run("validates matching claims", func(t *testing.T) {
		user := User{
			InternalUUID: "test-uuid",
			Email:        "test@example.com",
			Name:         "Test User",
		}

		claims := &Claims{
			Email: "test@example.com",
			Name:  "Test User",
		}
		claims.Subject = "test-uuid"

		// Should not fail
		ValidateTokenClaims(t, claims, user)
	})
}

func TestCreateTestClientCredential(t *testing.T) {
	t.Run("creates credential with correct defaults", func(t *testing.T) {
		ownerUUID := uuid.New()
		cred := CreateTestClientCredential(ownerUUID, "Test Credential")

		assert.NotNil(t, cred)
		assert.NotEqual(t, uuid.Nil, cred.ID)
		assert.True(t, len(cred.ClientID) > 0)
		assert.Contains(t, cred.ClientID, "tmi_cc_")
		assert.Equal(t, "Test Credential", cred.Name)
		assert.Equal(t, "Test client credential", cred.Description)
		assert.Equal(t, ownerUUID, cred.OwnerUUID)
		assert.True(t, cred.IsActive)
		assert.NotNil(t, cred.CreatedAt)
		assert.NotNil(t, cred.ModifiedAt)
		assert.Nil(t, cred.LastUsedAt)
		assert.Nil(t, cred.ExpiresAt)
	})
}

func TestCreateTestUserInfo(t *testing.T) {
	t.Run("creates user info with correct values", func(t *testing.T) {
		groups := []string{"group1", "group2"}
		userInfo := CreateTestUserInfo("user@example.com", "Test User", "google", groups)

		assert.Equal(t, "user@example.com-id", userInfo.ID)
		assert.Equal(t, "user@example.com", userInfo.Email)
		assert.True(t, userInfo.EmailVerified)
		assert.Equal(t, "Test User", userInfo.Name)
		assert.Equal(t, "google", userInfo.IdP)
		assert.Equal(t, groups, userInfo.Groups)
	})
}

func TestTestHelper(t *testing.T) {
	t.Run("creates helper with all components", func(t *testing.T) {
		helper := NewTestHelper(t)
		defer helper.Cleanup()

		assert.NotNil(t, helper.DB)
		assert.NotNil(t, helper.Mock)
		assert.NotNil(t, helper.Redis)
		assert.NotNil(t, helper.MiniRedis)
		assert.NotNil(t, helper.KeyManager)
		assert.NotNil(t, helper.StateStore)
		assert.NotNil(t, helper.TestContext)
	})

	t.Run("Redis operations work", func(t *testing.T) {
		helper := NewTestHelper(t)
		defer helper.Cleanup()

		// Set a key
		err := helper.SetRedisKey("test-key", "test-value", time.Minute)
		require.NoError(t, err)

		// Get the key
		value, err := helper.GetRedisKey("test-key")
		require.NoError(t, err)
		assert.Equal(t, "test-value", value)

		// Flush Redis
		helper.FlushRedis()

		// Key should be gone
		_, err = helper.GetRedisKey("test-key")
		assert.Error(t, err)
	})

	t.Run("FastForwardRedis affects TTL", func(t *testing.T) {
		helper := NewTestHelper(t)
		defer helper.Cleanup()

		// Set a key with 1 minute TTL
		err := helper.SetRedisKey("ttl-test", "value", time.Minute)
		require.NoError(t, err)

		// Key should exist
		_, err = helper.GetRedisKey("ttl-test")
		require.NoError(t, err)

		// Fast forward past TTL
		helper.FastForwardRedis(2 * time.Minute)

		// Key should be expired
		_, err = helper.GetRedisKey("ttl-test")
		assert.Error(t, err)
	})
}

func TestSetupMockDB(t *testing.T) {
	db, mock := SetupMockDB(t)
	defer func() { _ = db.Close() }()

	assert.NotNil(t, db)
	assert.NotNil(t, mock)
}

func TestSetupMockRedis(t *testing.T) {
	rdb, mr := SetupMockRedis(t)
	defer mr.Close()
	defer func() { _ = rdb.Close() }()

	assert.NotNil(t, rdb)
	assert.NotNil(t, mr)
}

func TestSetupTestKeyManager(t *testing.T) {
	km := SetupTestKeyManager(t)
	assert.NotNil(t, km)
	assert.Equal(t, "HS256", km.GetSigningMethod())
}

func TestUser_Struct(t *testing.T) {
	t.Run("user struct fields are accessible", func(t *testing.T) {
		now := time.Now()
		user := User{
			InternalUUID:   uuid.New().String(),
			Provider:       "tmi",
			ProviderUserID: "user@example.com",
			Email:          "user@example.com",
			Name:           "Test User",
			EmailVerified:  true,
			Groups:         []string{"developers", "testers"},
			IsAdmin:        false,
			CreatedAt:      now,
			ModifiedAt:     now,
			LastLogin:      &now,
		}

		assert.NotEmpty(t, user.InternalUUID)
		assert.Equal(t, "tmi", user.Provider)
		assert.Equal(t, "user@example.com", user.Email)
		assert.Equal(t, "Test User", user.Name)
		assert.True(t, user.EmailVerified)
		assert.False(t, user.IsAdmin)
		assert.Len(t, user.Groups, 2)
	})
}

func TestClientCredential_Struct(t *testing.T) {
	t.Run("client credential struct fields are accessible", func(t *testing.T) {
		now := time.Now()
		expiresAt := now.Add(30 * 24 * time.Hour)
		cred := ClientCredential{
			ID:               uuid.New(),
			ClientID:         "tmi_cc_test123",
			Name:             "Test API Client",
			Description:      "A test client credential",
			OwnerUUID:        uuid.New(),
			ClientSecretHash: "$2a$10$hash_here",
			IsActive:         true,
			CreatedAt:        now,
			ModifiedAt:       now,
			LastUsedAt:       &now,
			ExpiresAt:        &expiresAt,
		}

		assert.NotEqual(t, uuid.Nil, cred.ID)
		assert.Equal(t, "tmi_cc_test123", cred.ClientID)
		assert.Equal(t, "Test API Client", cred.Name)
		assert.True(t, cred.IsActive)
		assert.NotNil(t, cred.LastUsedAt)
		assert.NotNil(t, cred.ExpiresAt)
	})
}

func TestUserInfo_Struct(t *testing.T) {
	t.Run("user info struct fields are accessible", func(t *testing.T) {
		userInfo := UserInfo{
			ID:            "google-12345",
			Email:         "user@gmail.com",
			EmailVerified: true,
			Name:          "Test User",
			GivenName:     "Test",
			FamilyName:    "User",
			Picture:       "https://example.com/picture.jpg",
			Locale:        "en-US",
			IdP:           "google",
			Groups:        []string{"users", "developers"},
		}

		assert.Equal(t, "google-12345", userInfo.ID)
		assert.Equal(t, "user@gmail.com", userInfo.Email)
		assert.True(t, userInfo.EmailVerified)
		assert.Equal(t, "Test", userInfo.GivenName)
		assert.Equal(t, "User", userInfo.FamilyName)
		assert.Equal(t, "google", userInfo.IdP)
		assert.Len(t, userInfo.Groups, 2)
	})
}

func TestClaims_Struct(t *testing.T) {
	t.Run("claims struct fields are accessible", func(t *testing.T) {
		claims := Claims{
			Email:            "user@example.com",
			Name:             "Test User",
			IdentityProvider: "tmi",
			Groups:           []string{"admins"},
		}
		claims.Subject = uuid.New().String()
		claims.Issuer = "https://auth.example.com"

		assert.Equal(t, "user@example.com", claims.Email)
		assert.Equal(t, "Test User", claims.Name)
		assert.Equal(t, "tmi", claims.IdentityProvider)
		assert.NotEmpty(t, claims.Subject)
		assert.Len(t, claims.Groups, 1)
	})
}
