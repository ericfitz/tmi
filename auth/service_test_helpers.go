package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestHelper provides utilities for testing auth package functionality
type TestHelper struct {
	DB          *sql.DB
	Mock        sqlmock.Sqlmock
	Redis       *redis.Client
	MiniRedis   *miniredis.Miniredis
	KeyManager  *JWTKeyManager
	StateStore  StateStore
	TestContext context.Context
}

// NewTestHelper creates a new test helper with mocked dependencies
func NewTestHelper(t *testing.T) *TestHelper {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	mr, err := miniredis.Run()
	require.NoError(t, err)

	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	keyManager, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret-key-for-unit-tests",
	})
	require.NoError(t, err)

	return &TestHelper{
		DB:          db,
		Mock:        mock,
		Redis:       rdb,
		MiniRedis:   mr,
		KeyManager:  keyManager,
		StateStore:  NewInMemoryStateStore(),
		TestContext: context.Background(),
	}
}

// Cleanup releases all resources held by the test helper
func (h *TestHelper) Cleanup() {
	if h.Redis != nil {
		_ = h.Redis.Close()
	}
	if h.MiniRedis != nil {
		h.MiniRedis.Close()
	}
	if h.DB != nil {
		_ = h.DB.Close()
	}
}

// SetupMockDB creates a mock SQL database for testing
func SetupMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return db, mock
}

// SetupMockRedis creates a mock Redis client using miniredis
func SetupMockRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)

	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return rdb, mr
}

// SetupTestKeyManager creates a key manager for testing
func SetupTestKeyManager(t *testing.T) *JWTKeyManager {
	t.Helper()
	km, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret-key-for-unit-tests",
	})
	require.NoError(t, err)
	return km
}

// CreateTestUser creates a test user with default values
func CreateTestUser(provider, email string) User {
	now := time.Now()
	return User{
		InternalUUID:   uuid.New().String(),
		Provider:       provider,
		ProviderUserID: email + "-provider-id",
		Email:          email,
		Name:           "Test User",
		EmailVerified:  true,
		Groups:         []string{},
		IsAdmin:        false,
		CreatedAt:      now,
		ModifiedAt:     now,
		LastLogin:      &now,
	}
}

// CreateTestUserWithRole creates a test user with specific admin status
func CreateTestUserWithRole(provider, email string, isAdmin bool) User {
	user := CreateTestUser(provider, email)
	user.IsAdmin = isAdmin
	return user
}

// CreateTestUserWithGroups creates a test user with specific groups
func CreateTestUserWithGroups(provider, email string, groups []string) User {
	user := CreateTestUser(provider, email)
	user.Groups = groups
	return user
}

// CreateTestUserInfo creates UserInfo for testing OAuth responses
func CreateTestUserInfo(email, name, idp string, groups []string) *UserInfo {
	return &UserInfo{
		ID:            email + "-id",
		Email:         email,
		EmailVerified: true,
		Name:          name,
		IdP:           idp,
		Groups:        groups,
	}
}

// CreateTestClientCredential creates a test client credential
func CreateTestClientCredential(ownerUUID uuid.UUID, name string) *ClientCredential {
	now := time.Now()
	return &ClientCredential{
		ID:               uuid.New(),
		ClientID:         "tmi_cc_" + uuid.New().String()[:16],
		Name:             name,
		Description:      "Test client credential",
		OwnerUUID:        ownerUUID,
		ClientSecretHash: "$2a$10$placeholder_hash", // Placeholder bcrypt hash
		IsActive:         true,
		CreatedAt:        now,
		ModifiedAt:       now,
		LastUsedAt:       nil,
		ExpiresAt:        nil,
	}
}

// TestUsers provides standard test user identities for auth testing
var TestUsers = struct {
	Admin    User
	Regular  User
	External User
}{
	Admin: User{
		InternalUUID:   "admin-internal-uuid",
		Provider:       "tmi",
		ProviderUserID: "admin@example.com",
		Email:          "admin@example.com",
		Name:           "Admin User",
		EmailVerified:  true,
		Groups:         []string{"admins"},
		IsAdmin:        true,
		CreatedAt:      time.Now(),
		ModifiedAt:     time.Now(),
	},
	Regular: User{
		InternalUUID:   "regular-internal-uuid",
		Provider:       "tmi",
		ProviderUserID: "user@example.com",
		Email:          "user@example.com",
		Name:           "Regular User",
		EmailVerified:  true,
		Groups:         []string{},
		IsAdmin:        false,
		CreatedAt:      time.Now(),
		ModifiedAt:     time.Now(),
	},
	External: User{
		InternalUUID:   "external-internal-uuid",
		Provider:       "google",
		ProviderUserID: "external@gmail.com",
		Email:          "external@gmail.com",
		Name:           "External User",
		EmailVerified:  true,
		Groups:         []string{},
		IsAdmin:        false,
		CreatedAt:      time.Now(),
		ModifiedAt:     time.Now(),
	},
}

// MockUserRow returns mock SQL rows for a user query
func MockUserRow(mock sqlmock.Sqlmock, user User) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"internal_uuid", "provider", "provider_user_id", "email", "name",
		"email_verified", "is_admin", "created_at", "modified_at", "last_login",
	})
	rows.AddRow(
		user.InternalUUID, user.Provider, user.ProviderUserID, user.Email, user.Name,
		user.EmailVerified, user.IsAdmin, user.CreatedAt, user.ModifiedAt, user.LastLogin,
	)
	return rows
}

// MockEmptyUserRows returns empty rows for user queries
func MockEmptyUserRows(mock sqlmock.Sqlmock) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"internal_uuid", "provider", "provider_user_id", "email", "name",
		"email_verified", "is_admin", "created_at", "modified_at", "last_login",
	})
}

// ExpectUserQuery sets up mock expectation for a user query by email
func ExpectUserQuery(mock sqlmock.Sqlmock, email string, user *User) {
	if user != nil {
		mock.ExpectQuery("SELECT .* FROM users WHERE email").
			WithArgs(email).
			WillReturnRows(MockUserRow(mock, *user))
	} else {
		mock.ExpectQuery("SELECT .* FROM users WHERE email").
			WithArgs(email).
			WillReturnRows(MockEmptyUserRows(mock))
	}
}

// ExpectUserCreate sets up mock expectation for user creation
func ExpectUserCreate(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT INTO users").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

// ExpectUserUpdate sets up mock expectation for user update
func ExpectUserUpdate(mock sqlmock.Sqlmock) {
	mock.ExpectExec("UPDATE users").
		WillReturnResult(sqlmock.NewResult(0, 1))
}

// ExpectUserDelete sets up mock expectation for user deletion
func ExpectUserDelete(mock sqlmock.Sqlmock, userID string) {
	mock.ExpectExec("DELETE FROM users").
		WithArgs(userID).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

// TokenTestCase represents a test case for token operations
type TokenTestCase struct {
	Name           string
	User           User
	ExpectedError  bool
	ExpectedClaims func(*testing.T, *Claims)
}

// ClientCredentialTestCase represents a test case for client credential operations
type ClientCredentialTestCase struct {
	Name          string
	ClientID      string
	ClientSecret  string //nolint:gosec // G117 - test helper struct for client credentials
	ExpectSuccess bool
	ExpectedError string
	SetupMock     func(sqlmock.Sqlmock)
	VerifyResult  func(*testing.T, *TokenPair)
}

// AuthScenario represents an authorization test scenario
type AuthScenario struct {
	Name           string
	User           User
	RequiredRole   string
	ExpectedAccess bool
	SetupContext   func(context.Context) context.Context
}

// ValidateTokenClaims is a helper to validate common JWT claims
func ValidateTokenClaims(t *testing.T, claims *Claims, user User) {
	t.Helper()
	if claims.Subject != user.InternalUUID {
		t.Errorf("Expected subject %q, got %q", user.InternalUUID, claims.Subject)
	}
	if claims.Email != user.Email {
		t.Errorf("Expected email %q, got %q", user.Email, claims.Email)
	}
	if claims.Name != user.Name {
		t.Errorf("Expected name %q, got %q", user.Name, claims.Name)
	}
}

// FastForwardRedis advances time in miniredis for TTL testing
func (h *TestHelper) FastForwardRedis(duration time.Duration) {
	h.MiniRedis.FastForward(duration)
}

// SetRedisKey sets a key in miniredis for testing
func (h *TestHelper) SetRedisKey(key, value string, expiration time.Duration) error {
	return h.Redis.Set(h.TestContext, key, value, expiration).Err()
}

// GetRedisKey gets a key from miniredis for testing
func (h *TestHelper) GetRedisKey(key string) (string, error) {
	return h.Redis.Get(h.TestContext, key).Result()
}

// FlushRedis clears all keys in miniredis
func (h *TestHelper) FlushRedis() {
	h.MiniRedis.FlushAll()
}
