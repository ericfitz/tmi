package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogoutWithJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a test service with proper JWT configuration
	config := Config{
		JWT: JWTConfig{
			Secret:            "test-secret",
			ExpirationSeconds: 3600,
			SigningMethod:     "HS256",
		},
	}

	// Create JWT key manager
	keyManager, err := NewJWTKeyManager(config.JWT)
	require.NoError(t, err)

	// Create minimal service with key manager
	service := &Service{
		keyManager: keyManager,
		config:     config,
	}

	// Create handlers
	handlers := &Handlers{
		service: service,
		config:  config,
	}

	// Create a valid JWT token for testing
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "test@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	tokenString, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	t.Run("JWT_Based_Logout_Success", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/oauth2/revoke", handlers.Logout)

		// Create request with JWT token in Authorization header
		req := httptest.NewRequest("POST", "/oauth2/revoke", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+tokenString)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 200 OK (since we don't have Redis for blacklisting in this test)
		assert.Equal(t, http.StatusOK, w.Code)

		// Check response message
		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Logged out successfully", response["message"])
	})

	t.Run("Invalid_JWT_Falls_Back_To_Refresh_Token", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/oauth2/revoke", handlers.Logout)

		// Create request with invalid JWT token and no refresh token
		req := httptest.NewRequest("POST", "/oauth2/revoke", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Authorization", "Bearer invalid-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 400 Bad Request since no refresh_token provided
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Check error message
		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response["error"], "missing refresh_token")
	})

	t.Run("No_Auth_Header_Requires_Refresh_Token", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/oauth2/revoke", handlers.Logout)

		// Create request with no Authorization header and no refresh token
		req := httptest.NewRequest("POST", "/oauth2/revoke", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 400 Bad Request
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Check error message
		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response["error"], "missing refresh_token")
	})
}
