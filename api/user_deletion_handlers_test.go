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
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupUserDeletionTest creates a test environment with authenticated user
func setupUserDeletionTest(t *testing.T) (*gin.Engine, *auth.Service, *auth.User, string) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping user deletion integration test in short mode")
	}

	// Initialize database manager with GORM using DATABASE_URL
	databaseURL := "postgres://tmi_dev:dev123@localhost:5432/tmi_dev?sslmode=disable" //nolint:gosec // G101: Test credentials for local development only
	gormConfig, err := db.ParseDatabaseURL(databaseURL)
	require.NoError(t, err, "Failed to parse DATABASE_URL")

	redisConfig := db.RedisConfig{
		Host:     "localhost",
		Port:     "6379",
		Password: "",
		DB:       3, // Use DB 3 for user deletion testing
	}

	dbManager := db.NewManager()
	err = dbManager.InitGorm(*gormConfig)
	require.NoError(t, err, "Failed to initialize GORM")
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
		Database: auth.DatabaseConfig{
			URL: databaseURL,
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
		Provider:       "test",
		ProviderUserID: fmt.Sprintf("delete-test-%d", timestamp),
		Email:          fmt.Sprintf("delete-test-%d@example.com", timestamp),
		Name:           "Delete Test User",
	}

	user, err := authService.CreateUser(ctx, testUser)
	require.NoError(t, err)

	// Note: LinkUserProvider is deprecated and no longer needed
	// Provider info is now stored directly on the User struct during CreateUser

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
			c.Set("userID", user.ProviderUserID) // JWT sub claim contains provider user ID
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
	req := httptest.NewRequest(http.MethodDelete, "/me", nil)
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
	req := httptest.NewRequest(http.MethodDelete, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var challenge auth.DeletionChallenge
	err := json.Unmarshal(w.Body.Bytes(), &challenge)
	require.NoError(t, err)

	// Step 2: Delete with challenge
	req = httptest.NewRequest(http.MethodDelete, "/me?challenge="+challenge.ChallengeText, nil)
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
	req := httptest.NewRequest(http.MethodDelete, "/me?challenge=wrong-challenge", nil)
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
	timestamp2 := time.Now().Unix()
	user2 := auth.User{
		Provider:       "test",
		ProviderUserID: fmt.Sprintf("alternate-owner-%d", timestamp2),
		Email:          fmt.Sprintf("alternate-owner-%d@example.com", timestamp2),
		Name:           "Alternate Owner",
	}
	user2Created, err := authService.CreateUser(ctx, user2)
	require.NoError(t, err)

	// Create User objects for Owner field
	owner1User := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "test",
		ProviderId:    user1.Email,
		DisplayName:   user1.Name,
		Email:         openapi_types.Email(user1.Email),
	}

	// Create threat model owned by user1 with user2 as co-owner
	tm := ThreatModel{
		Name:  "Shared Threat Model",
		Owner: owner1User,
		Authorization: []Authorization{
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    user1.Email,
				Role:          RoleOwner,
			},
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    user2Created.Email,
				Role:          RoleOwner,
			},
		},
	}

	createdTM, err := ThreatModelStore.Create(tm, func(tm ThreatModel, id string) ThreatModel {
		parsedID, _ := uuid.Parse(id)
		tm.Id = &parsedID
		return tm
	})
	require.NoError(t, err)

	// Step 1: Get deletion challenge
	req := httptest.NewRequest(http.MethodDelete, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken1)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var challenge auth.DeletionChallenge
	err = json.Unmarshal(w.Body.Bytes(), &challenge)
	require.NoError(t, err)

	// Step 2: Delete user1 with challenge
	req = httptest.NewRequest(http.MethodDelete, "/me?challenge="+challenge.ChallengeText, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken1)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify threat model still exists with transferred ownership
	tmAfterDeletion, err := ThreatModelStore.Get(createdTM.Id.String())
	require.NoError(t, err)
	assert.Equal(t, user2Created.Email, tmAfterDeletion.Owner.ProviderId, "Ownership should transfer to user2")

	// Verify user1 is deleted
	_, err = authService.GetUserByEmail(ctx, user1.Email)
	assert.Error(t, err, "User1 should be deleted")
}

// TestUserDeletion_ThreatModelDeletion tests threat model deletion when no alternate owner
func TestUserDeletion_ThreatModelDeletion(t *testing.T) {
	router, authService, user, accessToken := setupUserDeletionTest(t)
	ctx := context.Background()

	// Create User object for Owner field
	ownerUser := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "test",
		ProviderId:    user.Email,
		DisplayName:   user.Name,
		Email:         openapi_types.Email(user.Email),
	}

	// Create threat model owned only by user
	tm := ThreatModel{
		Name:  "Solo Owned Threat Model",
		Owner: ownerUser,
		Authorization: []Authorization{
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    user.Email,
				Role:          RoleOwner,
			},
		},
	}

	createdTM, err := ThreatModelStore.Create(tm, func(tm ThreatModel, id string) ThreatModel {
		parsedID, _ := uuid.Parse(id)
		tm.Id = &parsedID
		return tm
	})
	require.NoError(t, err)

	// Step 1: Get deletion challenge
	req := httptest.NewRequest(http.MethodDelete, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var challenge auth.DeletionChallenge
	err = json.Unmarshal(w.Body.Bytes(), &challenge)
	require.NoError(t, err)

	// Step 2: Delete user with challenge
	req = httptest.NewRequest(http.MethodDelete, "/me?challenge="+challenge.ChallengeText, nil)
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
