package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAuthMiddleware(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create a new Gin router
	r := gin.New()

	// Create a test configuration
	config := Config{
		JWT: JWTConfig{
			Secret:            "test-secret",
			ExpirationSeconds: 3600,
			SigningMethod:     "HS256",
		},
	}

	// Create a mock database manager
	dbManager := newMockDBManager()

	// Create the authentication service
	service, err := NewService(dbManager, config)
	assert.NoError(t, err)

	// Create the authentication middleware
	middleware := NewMiddleware(service)

	// Add the middleware to the router
	r.Use(middleware.AuthRequired())

	// Add a test route
	r.GET("/protected", func(c *gin.Context) {
		c.String(http.StatusOK, "protected")
	})

	// Create a test request
	req := httptest.NewRequest("GET", "/protected", nil)

	// Test without authorization header
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Test with invalid authorization header
	req.Header.Set("Authorization", "InvalidToken")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Test with valid token
	user := User{
		ID:        "test-id",
		Email:     "test@example.com",
		Name:      "Test User",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	tokenPair, err := service.GenerateTokens(context.Background(), user)
	assert.NoError(t, err)

	req.Header.Set("Authorization", "Bearer "+tokenPair.AccessToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "protected", w.Body.String())
}

// Mock database manager for testing
type mockDBManager struct{}

func newMockDBManager() *db.Manager {
	// For testing, we'll use a real db.Manager with a mock Redis
	manager := db.NewManager()

	// Initialize Redis with a dummy config
	// The actual Redis connection won't be used in tests
	_ = manager.InitRedis(db.RedisConfig{
		Host: "localhost",
		Port: "6379",
	})

	return manager
}

// Mock Redis implementation for the service
// This is used by the service, not by the db.Manager
type mockRedis struct{}

func (r *mockRedis) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return nil
}

func (r *mockRedis) Get(ctx context.Context, key string) (string, error) {
	return "", nil
}

func (r *mockRedis) Del(ctx context.Context, key string) error {
	return nil
}

func (r *mockRedis) HSet(ctx context.Context, key, field string, value interface{}) error {
	return nil
}

func (r *mockRedis) HGet(ctx context.Context, key, field string) (string, error) {
	return "", nil
}

func (r *mockRedis) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return nil, nil
}

func (r *mockRedis) HDel(ctx context.Context, key string, fields ...string) error {
	return nil
}

func (r *mockRedis) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return nil
}

func TestGenerateTokens(t *testing.T) {
	// Create a test configuration
	config := Config{
		JWT: JWTConfig{
			Secret:            "test-secret",
			ExpirationSeconds: 3600,
			SigningMethod:     "HS256",
		},
	}

	// Create a mock database manager
	dbManager := newMockDBManager()

	// Create the authentication service
	service, err := NewService(dbManager, config)
	assert.NoError(t, err)

	// Create a test user
	user := User{
		ID:        "test-id",
		Email:     "test@example.com",
		Name:      "Test User",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Generate tokens
	tokenPair, err := service.GenerateTokens(context.Background(), user)
	assert.NoError(t, err)
	assert.NotEmpty(t, tokenPair.AccessToken)
	assert.NotEmpty(t, tokenPair.RefreshToken)
	assert.Equal(t, 3600, tokenPair.ExpiresIn)
	assert.Equal(t, "Bearer", tokenPair.TokenType)

	// Validate the token
	claims, err := service.ValidateToken(tokenPair.AccessToken)
	assert.NoError(t, err)
	assert.Equal(t, user.Email, claims.Email)
	assert.Equal(t, user.Name, claims.Name)
	assert.Equal(t, user.Email, claims.Subject)
}
