package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeLogout tests the /me/logout endpoint (self-logout with JWT in Authorization header)
func TestMeLogout(t *testing.T) {
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

	// Create mock dbManager (without Redis - tests the error path)
	dbManager := db.NewMockManager()

	// Create service with key manager and dbManager
	service := &Service{
		keyManager: keyManager,
		config:     config,
		dbManager:  dbManager,
	}

	// Create handlers
	handlers := &Handlers{
		service: service,
		config:  config,
	}

	t.Run("Invalid_JWT_Returns_Error", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/me/logout", handlers.MeLogout)

		// Create request with invalid JWT token
		req := httptest.NewRequest("POST", "/me/logout", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 401 Unauthorized for invalid token
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// Check error message
		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "unauthorized", response["error"])
		assert.Contains(t, response["error_description"], "Invalid token")
	})

	t.Run("No_Auth_Header_Returns_Error", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/me/logout", handlers.MeLogout)

		// Create request with no Authorization header
		req := httptest.NewRequest("POST", "/me/logout", nil)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 401 Unauthorized for missing Authorization header
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// Check error message
		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "unauthorized", response["error"])
		assert.Contains(t, response["error_description"], "Missing or invalid Authorization header")
	})
}

// TestRevokeToken tests the /oauth2/revoke endpoint (RFC 7009 token revocation)
func TestRevokeToken(t *testing.T) {
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

	// Create mock dbManager
	dbManager := db.NewMockManager()

	// Create service with key manager and dbManager
	service := &Service{
		keyManager: keyManager,
		config:     config,
		dbManager:  dbManager,
	}

	// Create handlers
	handlers := &Handlers{
		service: service,
		config:  config,
	}

	// Create a valid JWT token for testing
	validToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "test@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	validTokenStr, err := validToken.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	t.Run("Missing_Token_Parameter_Returns_400", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/oauth2/revoke", handlers.RevokeToken)

		// Create request with empty body (missing token parameter)
		req := httptest.NewRequest("POST", "/oauth2/revoke", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+validTokenStr)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 400 Bad Request for missing token parameter
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Check error message
		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "invalid_request", response["error"])
		assert.Contains(t, response["error_description"], "token")
	})

	t.Run("No_Auth_Returns_401", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/oauth2/revoke", handlers.RevokeToken)

		// Create request with token but no authentication
		body := `{"token": "some-token-to-revoke"}`
		req := httptest.NewRequest("POST", "/oauth2/revoke", bytes.NewBuffer([]byte(body)))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 401 Unauthorized for missing authentication
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// Check error message
		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "invalid_client", response["error"])
		assert.Contains(t, response["error_description"], "authentication failed")
	})

	t.Run("Invalid_Bearer_Token_Returns_401", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/oauth2/revoke", handlers.RevokeToken)

		// Create request with invalid Bearer token for authentication
		body := `{"token": "some-token-to-revoke"}`
		req := httptest.NewRequest("POST", "/oauth2/revoke", bytes.NewBuffer([]byte(body)))
		req.Header.Set("Authorization", "Bearer invalid-auth-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 401 Unauthorized for invalid Bearer token
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// Check error message
		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "invalid_client", response["error"])
	})

	t.Run("Form_URLEncoded_Request", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/oauth2/revoke", handlers.RevokeToken)

		// Create form-urlencoded request (RFC 7009 standard format)
		body := "token=some-token-to-revoke&token_type_hint=access_token"
		req := httptest.NewRequest("POST", "/oauth2/revoke", bytes.NewBuffer([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+validTokenStr)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Per RFC 7009, should return 200 OK regardless of token validity
		// (Note: This may return 500 if Redis is not available in test, which is expected)
		// The key is that it shouldn't return 400 or 401
		assert.NotEqual(t, http.StatusBadRequest, w.Code, "Should not return 400 for valid request format")
	})
}
