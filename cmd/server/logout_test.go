package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogoutEndpoint(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Start miniredis for testing
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis client and token blacklist
	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = rdb.Close() }()

	tokenBlacklist := auth.NewTokenBlacklist(rdb)

	// Create test configuration
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWT: config.JWTConfig{
				Secret: "test-secret-key",
			},
		},
	}

	// Create test server
	server := &Server{
		config:         cfg,
		tokenBlacklist: tokenBlacklist,
	}

	// Create a valid JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "testuser@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	tokenString, err := token.SignedString([]byte(cfg.Auth.JWT.Secret))
	require.NoError(t, err)

	t.Run("PostAuthLogout_Success", func(t *testing.T) {
		// Create Gin router with minimal setup
		r := gin.New()
		r.POST("/auth/logout", server.PostAuthLogout)

		// Create request with valid token
		req := httptest.NewRequest("POST", "/auth/logout", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+tokenString)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 204 No Content
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Token should now be blacklisted
		isBlacklisted, err := tokenBlacklist.IsTokenBlacklisted(req.Context(), tokenString)
		assert.NoError(t, err)
		assert.True(t, isBlacklisted)
	})

	t.Run("PostAuthLogout_MissingToken", func(t *testing.T) {
		// Create Gin router with minimal setup
		r := gin.New()
		r.POST("/auth/logout", server.PostAuthLogout)

		// Create request without token
		req := httptest.NewRequest("POST", "/auth/logout", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 401 Unauthorized
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("PostAuthLogout_InvalidToken", func(t *testing.T) {
		// Create Gin router with minimal setup
		r := gin.New()
		r.POST("/auth/logout", server.PostAuthLogout)

		// Create request with invalid token
		req := httptest.NewRequest("POST", "/auth/logout", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Authorization", "Bearer invalid-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 401 Unauthorized
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("JWT_Middleware_BlacklistedToken", func(t *testing.T) {
		// Create a new token for this test
		newToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "testuser2@example.com",
			"exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(),
		})
		newTokenString, err := newToken.SignedString([]byte(cfg.Auth.JWT.Secret))
		require.NoError(t, err)

		// Blacklist the token first
		err = tokenBlacklist.BlacklistToken(context.Background(), newTokenString)
		require.NoError(t, err)

		// Create Gin router with JWT middleware
		r := gin.New()
		r.Use(JWTMiddleware(cfg, tokenBlacklist, nil)) // nil authHandlers for this test
		r.GET("/protected", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		// Create request with blacklisted token
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+newTokenString)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should return 401 Unauthorized due to blacklisted token
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestServerWithoutTokenBlacklist(t *testing.T) {
	// Test server behavior when token blacklist is not available
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWT: config.JWTConfig{
				Secret: "test-secret-key",
			},
		},
	}

	server := &Server{
		config:         cfg,
		tokenBlacklist: nil, // No token blacklist
	}

	// Create a valid JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "testuser@example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	tokenString, err := token.SignedString([]byte(cfg.Auth.JWT.Secret))
	require.NoError(t, err)

	t.Run("Logout_WithoutBlacklist", func(t *testing.T) {
		// Create Gin router
		r := gin.New()
		r.POST("/auth/logout", server.PostAuthLogout)

		// Create request with valid token
		req := httptest.NewRequest("POST", "/auth/logout", bytes.NewBuffer([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+tokenString)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Should still return 204 No Content (but token won't be blacklisted)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}
