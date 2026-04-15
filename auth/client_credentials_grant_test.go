package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/auth/repository"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// stubUserRepo implements repository.UserRepository for testing
type stubUserRepo struct {
	users map[string]*repository.User // keyed by InternalUUID
}

func (r *stubUserRepo) GetByID(_ context.Context, id string) (*repository.User, error) {
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}

func (r *stubUserRepo) GetByEmail(context.Context, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *stubUserRepo) GetByProviderID(context.Context, string, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *stubUserRepo) GetByProviderAndEmail(context.Context, string, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *stubUserRepo) GetByAnyProviderID(context.Context, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *stubUserRepo) GetProviders(context.Context, string) ([]repository.UserProvider, error) {
	return nil, nil
}
func (r *stubUserRepo) GetPrimaryProviderID(context.Context, string) (string, error) {
	return "", repository.ErrUserNotFound
}
func (r *stubUserRepo) Create(context.Context, *repository.User) (*repository.User, error) {
	return nil, nil
}
func (r *stubUserRepo) Update(context.Context, *repository.User) error { return nil }
func (r *stubUserRepo) Delete(context.Context, string) error           { return nil }

// stubCredRepo implements repository.ClientCredentialRepository for testing
type stubCredRepo struct {
	creds map[string]*repository.ClientCredential // keyed by ClientID
}

func (r *stubCredRepo) GetByClientID(_ context.Context, clientID string) (*repository.ClientCredential, error) {
	if c, ok := r.creds[clientID]; ok {
		return c, nil
	}
	return nil, repository.ErrClientCredentialNotFound
}

func (r *stubCredRepo) Create(context.Context, repository.ClientCredentialCreateParams) (*repository.ClientCredential, error) {
	return nil, nil
}
func (r *stubCredRepo) ListByOwner(context.Context, uuid.UUID) ([]*repository.ClientCredential, error) {
	return nil, nil
}
func (r *stubCredRepo) UpdateLastUsed(context.Context, uuid.UUID) error        { return nil }
func (r *stubCredRepo) Deactivate(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *stubCredRepo) Delete(context.Context, uuid.UUID, uuid.UUID) error     { return nil }

// setupTestServiceWithRepos creates a Service with stub repos and a real miniredis for cache.
func setupTestServiceWithRepos(t *testing.T, userRepo repository.UserRepository, credRepo repository.ClientCredentialRepository) (*Service, func()) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)

	parts := strings.SplitN(mr.Addr(), ":", 2)
	host := parts[0]
	port := parts[1]

	dbManager := db.NewManager()
	err = dbManager.InitRedis(db.RedisConfig{
		Host: host,
		Port: port,
	})
	require.NoError(t, err)

	keyManager, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret-key-for-ccg-tests",
	})
	require.NoError(t, err)

	svc := &Service{
		dbManager:  dbManager,
		keyManager: keyManager,
		config: Config{
			JWT: JWTConfig{
				Secret:            "test-secret-key-for-ccg-tests",
				ExpirationSeconds: 3600,
				SigningMethod:     "HS256",
			},
			OAuth: OAuthConfig{
				CallbackURL: "http://localhost:8080/oauth2/callback",
			},
		},
		userRepo: userRepo,
		credRepo: credRepo,
	}

	cleanup := func() {
		_ = dbManager.Close()
		mr.Close()
	}

	return svc, cleanup
}

// stubClaimsEnricher implements ClaimsEnricher for testing
type stubClaimsEnricher struct {
	isAdmin     bool
	isSecReview bool
	tmiGroups   []string
	enrichError error
}

func (e *stubClaimsEnricher) EnrichClaims(_ context.Context, _ string, _ string, _ []string) (bool, bool, []string, error) {
	return e.isAdmin, e.isSecReview, e.tmiGroups, e.enrichError
}

// newTestCredential creates a bcrypt-hashed credential for testing.
func newTestCredential(t *testing.T, clientID string, ownerUUID, credID uuid.UUID) (string, *repository.ClientCredential) {
	t.Helper()
	plainSecret := "test-client-secret-value"
	hash, err := bcrypt.GenerateFromPassword([]byte(plainSecret), bcrypt.MinCost)
	require.NoError(t, err)
	return plainSecret, &repository.ClientCredential{
		ID:               credID,
		OwnerUUID:        ownerUUID,
		ClientID:         clientID,
		ClientSecretHash: string(hash),
		Name:             "Test Credential",
		Description:      "For testing",
		IsActive:         true,
		CreatedAt:        time.Now(),
		ModifiedAt:       time.Now(),
	}
}

func TestHandleClientCredentialsGrant_OwnerProviderInJWT(t *testing.T) {
	// Verifies that the JWT contains the owner's actual OAuth provider (e.g. "google"),
	// not a hardcoded "tmi". Bug: #260.

	ownerUUID := uuid.New()
	credID := uuid.New()
	clientID := "tmi_cc_test123"
	plainSecret, cred := newTestCredential(t, clientID, ownerUUID, credID)

	tests := []struct {
		name             string
		ownerProvider    string
		ownerProviderUID string
	}{
		{
			name:             "google provider preserved in JWT",
			ownerProvider:    "google",
			ownerProviderUID: "google-uid-12345",
		},
		{
			name:             "github provider preserved in JWT",
			ownerProvider:    "github",
			ownerProviderUID: "github-uid-67890",
		},
		{
			name:             "tmi provider still works",
			ownerProvider:    "tmi",
			ownerProviderUID: "alice@tmi.local",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userRepo := &stubUserRepo{
				users: map[string]*repository.User{
					ownerUUID.String(): {
						InternalUUID:   ownerUUID.String(),
						Provider:       tc.ownerProvider,
						ProviderUserID: tc.ownerProviderUID,
						Email:          "owner@example.com",
						Name:           "Test Owner",
						EmailVerified:  true,
						CreatedAt:      time.Now(),
						ModifiedAt:     time.Now(),
					},
				},
			}

			credRepo := &stubCredRepo{
				creds: map[string]*repository.ClientCredential{clientID: cred},
			}

			svc, cleanup := setupTestServiceWithRepos(t, userRepo, credRepo)
			defer cleanup()

			tokenPair, err := svc.HandleClientCredentialsGrant(context.Background(), clientID, plainSecret)
			require.NoError(t, err)
			require.NotNil(t, tokenPair)
			require.NotEmpty(t, tokenPair.AccessToken)

			parser := jwt.NewParser(jwt.WithoutClaimsValidation())
			token, _, err := parser.ParseUnverified(tokenPair.AccessToken, jwt.MapClaims{})
			require.NoError(t, err)

			claims, ok := token.Claims.(jwt.MapClaims)
			require.True(t, ok)

			assert.Equal(t, tc.ownerProvider, claims["idp"], "JWT idp claim must match owner's provider")

			sub, ok := claims["sub"].(string)
			require.True(t, ok)
			assert.True(t, strings.HasPrefix(sub, "sa:"), "subject should have sa: prefix")
			assert.Contains(t, sub, tc.ownerProviderUID, "subject should contain owner's provider user ID")
		})
	}
}

func TestHandleClientCredentialsGrant_AdminExcluded(t *testing.T) {
	// Verifies that client credential tokens never carry administrator privileges,
	// even when the owner is an administrator. Admin operations require PKCE.

	ownerUUID := uuid.New()
	credID := uuid.New()
	clientID := "tmi_cc_admin_test"
	plainSecret, cred := newTestCredential(t, clientID, ownerUUID, credID)

	userRepo := &stubUserRepo{
		users: map[string]*repository.User{
			ownerUUID.String(): {
				InternalUUID:   ownerUUID.String(),
				Provider:       "google",
				ProviderUserID: "admin-user-id",
				Email:          "admin@example.com",
				Name:           "Admin Owner",
				EmailVerified:  true,
				CreatedAt:      time.Now(),
				ModifiedAt:     time.Now(),
			},
		},
	}

	credRepo := &stubCredRepo{
		creds: map[string]*repository.ClientCredential{clientID: cred},
	}

	svc, cleanup := setupTestServiceWithRepos(t, userRepo, credRepo)
	defer cleanup()

	// Simulate an admin user: enricher returns isAdmin=true with administrators group
	svc.claimsEnricher = &stubClaimsEnricher{
		isAdmin:     true,
		isSecReview: true,
		tmiGroups:   []string{"administrators", "security-reviewers", "developers"},
	}

	tokenPair, err := svc.HandleClientCredentialsGrant(context.Background(), clientID, plainSecret)
	require.NoError(t, err)
	require.NotNil(t, tokenPair)

	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(tokenPair.AccessToken, jwt.MapClaims{})
	require.NoError(t, err)

	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)

	// tmi_is_administrator must be false
	isAdmin, hasAdmin := claims["tmi_is_administrator"]
	assert.True(t, hasAdmin, "tmi_is_administrator claim should be present")
	assert.Equal(t, false, isAdmin, "tmi_is_administrator must be false for client credentials")

	// tmi_is_security_reviewer should still be true (only admin is restricted)
	isSecRev, hasSecRev := claims["tmi_is_security_reviewer"]
	assert.True(t, hasSecRev, "tmi_is_security_reviewer claim should be present")
	assert.Equal(t, true, isSecRev, "tmi_is_security_reviewer should be preserved")

	// groups must not contain "administrators" but should contain other groups
	if groupsRaw, hasGroups := claims["groups"]; hasGroups {
		groupsArr, ok := groupsRaw.([]interface{})
		require.True(t, ok)
		groupNames := make([]string, 0, len(groupsArr))
		for _, g := range groupsArr {
			groupNames = append(groupNames, g.(string))
		}
		assert.NotContains(t, groupNames, "administrators", "administrators group must not appear in client credential tokens")
		assert.Contains(t, groupNames, "security-reviewers", "non-admin groups should be preserved")
		assert.Contains(t, groupNames, "developers", "non-admin groups should be preserved")
	}
}
