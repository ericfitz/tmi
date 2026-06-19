package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestHelper provides utilities for testing auth package functionality
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: test fixture bundling mocked DB, Redis, JWT key manager, and state store for auth tests
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a test helper with mocked SQL DB, Redis, and JWT key manager (pure)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: release Redis client, miniredis, and DB connections held by the test helper
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a mock SQL database and sqlmock controller for unit tests (pure)
func SetupMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return db, mock
}

// SetupMockRedis creates a mock Redis client using miniredis
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build an in-process miniredis server and Redis client for unit tests (pure)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a JWT key manager with a fixed HMAC secret for unit tests (pure)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a User value with default fields for a given provider and email (pure)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a test user with a specific admin role flag (pure)
func CreateTestUserWithRole(provider, email string, isAdmin bool) User {
	user := CreateTestUser(provider, email)
	user.IsAdmin = isAdmin
	return user
}

// CreateTestUserWithGroups creates a test user with specific groups
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a test user assigned to specific groups (pure)
func CreateTestUserWithGroups(provider, email string, groups []string) User {
	user := CreateTestUser(provider, email)
	user.Groups = groups
	return user
}

// CreateTestUserInfo creates UserInfo for testing OAuth responses
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a UserInfo value representing an OAuth provider's identity response (pure)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build a ClientCredential fixture with a placeholder bcrypt hash (pure)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build sqlmock rows representing a single user record (pure)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: build an empty sqlmock row set for user queries that return no results (pure)
func MockEmptyUserRows(mock sqlmock.Sqlmock) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"internal_uuid", "provider", "provider_user_id", "email", "name",
		"email_verified", "is_admin", "created_at", "modified_at", "last_login",
	})
}

// ExpectUserQuery sets up mock expectation for a user query by email
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: register a sqlmock expectation for a user lookup by email (mutates shared state)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: register a sqlmock expectation for an INSERT INTO users statement (mutates shared state)
func ExpectUserCreate(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT INTO users").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

// ExpectUserUpdate sets up mock expectation for user update
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: register a sqlmock expectation for an UPDATE users statement (mutates shared state)
func ExpectUserUpdate(mock sqlmock.Sqlmock) {
	mock.ExpectExec("UPDATE users").
		WillReturnResult(sqlmock.NewResult(0, 1))
}

// ExpectUserDelete sets up mock expectation for user deletion
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: register a sqlmock expectation for a DELETE FROM users statement by user ID (mutates shared state)
func ExpectUserDelete(mock sqlmock.Sqlmock, userID string) {
	mock.ExpectExec("DELETE FROM users").
		WithArgs(userID).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

// TokenTestCase represents a test case for token operations
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: data type grouping token operation inputs and expected outcomes for table-driven tests (pure)
type TokenTestCase struct {
	Name           string
	User           User
	ExpectedError  bool
	ExpectedClaims func(*testing.T, *Claims)
}

// ClientCredentialTestCase represents a test case for client credential operations
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: data type grouping client credential inputs, mock setup, and expected outcomes for table-driven tests (pure)
type ClientCredentialTestCase struct {
	Name          string
	ClientID      string
	ClientSecret  string
	ExpectSuccess bool
	ExpectedError string
	SetupMock     func(sqlmock.Sqlmock)
	VerifyResult  func(*testing.T, *TokenPair)
}

// AuthScenario represents an authorization test scenario
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: data type grouping a user, required role, and expected access decision for authorization tests (pure)
type AuthScenario struct {
	Name           string
	User           User
	RequiredRole   string
	ExpectedAccess bool
	SetupContext   func(context.Context) context.Context
}

// ValidateTokenClaims is a helper to validate common JWT claims
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: assert that JWT claims match the expected user's subject, email, and name (pure)
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
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: advance miniredis internal clock to trigger TTL expiry in tests (mutates shared state)
func (h *TestHelper) FastForwardRedis(duration time.Duration) {
	h.MiniRedis.FastForward(duration)
}

// SetRedisKey sets a key in miniredis for testing
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: store a key-value pair with expiration in the test Redis instance
func (h *TestHelper) SetRedisKey(key, value string, expiration time.Duration) error {
	return h.Redis.Set(h.TestContext, key, value, expiration).Err()
}

// GetRedisKey gets a key from miniredis for testing
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: fetch a string value by key from the test Redis instance
func (h *TestHelper) GetRedisKey(key string) (string, error) {
	return h.Redis.Get(h.TestContext, key).Result()
}

// FlushRedis clears all keys in miniredis
// SEM@ac74bec7c763b2f6486d3fe0a6731458c37e43c5: delete all keys from the test Redis instance (mutates shared state)
func (h *TestHelper) FlushRedis() {
	h.MiniRedis.FlushAll()
}
