package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupUserDeletionTest creates a test environment with authenticated user
func setupUserDeletionTest(t *testing.T) (*gin.Engine, *auth.Service, *auth.User, string) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping user deletion integration test in short mode")
	}

	// Initialize database manager
	postgresConfig := db.PostgresConfig{
		Host:     "localhost",
		Port:     "5432",
		User:     "tmi_dev",
		Password: "dev123",
		Database: "tmi_dev",
		SSLMode:  "disable",
	}

	redisConfig := db.RedisConfig{
		Host:     "localhost",
		Port:     "6379",
		Password: "",
		DB:       3, // Use DB 3 for user deletion testing
	}

	dbManager := db.NewManager()
	err := dbManager.InitPostgres(postgresConfig)
	require.NoError(t, err, "Failed to initialize PostgreSQL")
	err = dbManager.InitRedis(redisConfig)
	require.NoError(t, err, "Failed to initialize Redis")

	// Create auth service
	authConfig := auth.Config{
		JWT: auth.JWTConfig{
			Secret:            "test-secret-key-for-user-deletion-testing",
			ExpirationSeconds: 3600,
			SigningMethod:     "HS256",
		},
		OAuth: auth.OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
			Providers: map[string]auth.OAuthProviderConfig{
				"test": {
					ClientID:     "test-client-id",
					ClientSecret: "test-client-secret",
					Enabled:      true,
				},
			},
		},
		Postgres: auth.PostgresConfig{
			Host:     postgresConfig.Host,
			Port:     postgresConfig.Port,
			User:     postgresConfig.User,
			Password: postgresConfig.Password,
			Database: postgresConfig.Database,
			SSLMode:  postgresConfig.SSLMode,
		},
		Redis: auth.RedisConfig{
			Host:     redisConfig.Host,
			Port:     redisConfig.Port,
			Password: redisConfig.Password,
			DB:       redisConfig.DB,
		},
	}

	authService, err := auth.NewService(dbManager, authConfig)
	require.NoError(t, err)

	// Create test user
	ctx := context.Background()
	timestamp := time.Now().Unix()
	testUser := auth.User{
		Email: fmt.Sprintf("delete-test-%d@example.com", timestamp),
		Name:  "Delete Test User",
	}

	user, err := authService.CreateUser(ctx, testUser)
	require.NoError(t, err)

	// Link test provider to user
	err = authService.LinkUserProvider(ctx, user.ID, "test", "test-provider-user-id", user.Email)
	require.NoError(t, err)

	// Generate access token
	tokens, err := authService.GenerateTokens(ctx, user)
	require.NoError(t, err)

	// Setup router with auth
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authHandlers := auth.NewHandlers(authService, authConfig)
	authAdapter := NewAuthServiceAdapter(authHandlers)

	// Create server and set auth service
	server := NewServerForTests()
	server.SetAuthService(authAdapter)

	// Add authentication middleware
	router.Use(func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "Bearer "+tokens.AccessToken {
			c.Set("userEmail", user.Email)
			c.Set("userID", user.ID)
		}
		c.Next()
	})

	// Register the generated API routes
	RegisterHandlers(router, server)

	return router, authService, &user, tokens.AccessToken
}

// TestUserDeletion_ChallengeGeneration tests the first step of user deletion
func TestUserDeletion_ChallengeGeneration(t *testing.T) {
	router, _, user, accessToken := setupUserDeletionTest(t)

	// Make request without challenge parameter
	req := httptest.NewRequest(http.MethodDelete, "/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 200 with challenge
	assert.Equal(t, http.StatusOK, w.Code)

	var response auth.DeletionChallenge
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify challenge contains user email
	assert.Contains(t, response.ChallengeText, user.Email)
	assert.Contains(t, response.ChallengeText, "I want to delete all my data")
	assert.NotEmpty(t, response.ExpiresAt)
}

// TestUserDeletion_SuccessfulDeletion tests complete deletion flow
func TestUserDeletion_SuccessfulDeletion(t *testing.T) {
	router, authService, user, accessToken := setupUserDeletionTest(t)
	ctx := context.Background()

	// Step 1: Get challenge
	req := httptest.NewRequest(http.MethodDelete, "/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var challenge auth.DeletionChallenge
	err := json.Unmarshal(w.Body.Bytes(), &challenge)
	require.NoError(t, err)

	// Step 2: Delete with challenge
	req = httptest.NewRequest(http.MethodDelete, "/users/me?challenge="+challenge.ChallengeText, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return 204 No Content
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify user is deleted
	_, err = authService.GetUserByEmail(ctx, user.Email)
	assert.Error(t, err, "User should be deleted")
}

// TestUserDeletion_InvalidChallenge tests invalid challenge handling
func TestUserDeletion_InvalidChallenge(t *testing.T) {
	router, _, _, accessToken := setupUserDeletionTest(t)

	// Try to delete with wrong challenge
	req := httptest.NewRequest(http.MethodDelete, "/users/me?challenge=wrong-challenge", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestUserDeletion_OwnershipTransfer tests threat model ownership transfer
func TestUserDeletion_OwnershipTransfer(t *testing.T) {
	router, authService, user1, accessToken1 := setupUserDeletionTest(t)
	ctx := context.Background()

	// Create second user as alternate owner
	user2 := auth.User{
		Email: fmt.Sprintf("alternate-owner-%d@example.com", time.Now().Unix()),
		Name:  "Alternate Owner",
	}
	user2Created, err := authService.CreateUser(ctx, user2)
	require.NoError(t, err)

	// Create threat model owned by user1 with user2 as co-owner
	tm := ThreatModel{
		Name:  "Shared Threat Model",
		Owner: user1.Email,
		Authorization: []Authorization{
			{Subject: user1.Email, Role: RoleOwner},
			{Subject: user2Created.Email, Role: RoleOwner},
		},
	}

	createdTM, err := ThreatModelStore.Create(tm, func(tm ThreatModel, id string) ThreatModel {
		parsedID, _ := uuid.Parse(id)
		tm.Id = &parsedID
		return tm
	})
	require.NoError(t, err)

	// Step 1: Get deletion challenge
	req := httptest.NewRequest(http.MethodDelete, "/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken1)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var challenge auth.DeletionChallenge
	err = json.Unmarshal(w.Body.Bytes(), &challenge)
	require.NoError(t, err)

	// Step 2: Delete user1 with challenge
	req = httptest.NewRequest(http.MethodDelete, "/users/me?challenge="+challenge.ChallengeText, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken1)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify threat model still exists with transferred ownership
	tmAfterDeletion, err := ThreatModelStore.Get(createdTM.Id.String())
	require.NoError(t, err)
	assert.Equal(t, user2Created.Email, tmAfterDeletion.Owner, "Ownership should transfer to user2")

	// Verify user1 is deleted
	_, err = authService.GetUserByEmail(ctx, user1.Email)
	assert.Error(t, err, "User1 should be deleted")
}

// TestUserDeletion_ThreatModelDeletion tests threat model deletion when no alternate owner
func TestUserDeletion_ThreatModelDeletion(t *testing.T) {
	router, authService, user, accessToken := setupUserDeletionTest(t)
	ctx := context.Background()

	// Create threat model owned only by user
	tm := ThreatModel{
		Name:  "Solo Owned Threat Model",
		Owner: user.Email,
		Authorization: []Authorization{
			{Subject: user.Email, Role: RoleOwner},
		},
	}

	createdTM, err := ThreatModelStore.Create(tm, func(tm ThreatModel, id string) ThreatModel {
		parsedID, _ := uuid.Parse(id)
		tm.Id = &parsedID
		return tm
	})
	require.NoError(t, err)

	// Step 1: Get deletion challenge
	req := httptest.NewRequest(http.MethodDelete, "/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var challenge auth.DeletionChallenge
	err = json.Unmarshal(w.Body.Bytes(), &challenge)
	require.NoError(t, err)

	// Step 2: Delete user with challenge
	req = httptest.NewRequest(http.MethodDelete, "/users/me?challenge="+challenge.ChallengeText, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify threat model is deleted
	_, err = ThreatModelStore.Get(createdTM.Id.String())
	assert.Error(t, err, "Threat model should be deleted")

	// Verify user is deleted
	_, err = authService.GetUserByEmail(ctx, user.Email)
	assert.Error(t, err, "User should be deleted")
}
