package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("skips rate limiting when no rate limiter configured", func(t *testing.T) {
		server := &Server{
			apiRateLimiter: nil,
		}

		router := gin.New()
		router.Use(RateLimitMiddleware(server))
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("skips rate limiting for public endpoints", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		quotaStore := &mockAPIQuotaStore{
			quotas: make(map[string]UserAPIQuota),
		}
		server := &Server{
			apiRateLimiter: NewAPIRateLimiter(client, quotaStore),
		}

		router := gin.New()
		router.Use(RateLimitMiddleware(server))
		router.GET("/", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		router.GET("/.well-known/openid-configuration", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Test root endpoint
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Test .well-known endpoint
		req = httptest.NewRequest("GET", "/.well-known/openid-configuration", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("skips rate limiting for auth flow endpoints", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		quotaStore := &mockAPIQuotaStore{
			quotas: make(map[string]UserAPIQuota),
		}
		server := &Server{
			apiRateLimiter: NewAPIRateLimiter(client, quotaStore),
		}

		authEndpoints := []string{
			"/oauth2/authorize",
			"/oauth2/callback",
			"/oauth2/token",
			"/oauth2/refresh",
			"/saml/login",
		}

		router := gin.New()
		router.Use(RateLimitMiddleware(server))
		for _, endpoint := range authEndpoints {
			router.GET(endpoint, func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"status": "ok"})
			})
		}

		for _, endpoint := range authEndpoints {
			req := httptest.NewRequest("GET", endpoint, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Endpoint %s should be skipped", endpoint)
		}
	})

	t.Run("skips rate limiting when user not authenticated", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		quotaStore := &mockAPIQuotaStore{
			quotas: make(map[string]UserAPIQuota),
		}
		server := &Server{
			apiRateLimiter: NewAPIRateLimiter(client, quotaStore),
		}

		router := gin.New()
		router.Use(RateLimitMiddleware(server))
		router.GET("/api/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// No user_id in context
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("allows requests within rate limit", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		userUUID := uuid.New()
		userID := userUUID.String()
		quotaStore := &mockAPIQuotaStore{
			quotas: map[string]UserAPIQuota{
				userID: {
					UserId:               userUUID,
					MaxRequestsPerMinute: 10,
				},
			},
		}
		server := &Server{
			apiRateLimiter: NewAPIRateLimiter(client, quotaStore),
		}

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("user_id", userID)
			c.Next()
		})
		router.Use(RateLimitMiddleware(server))
		router.GET("/api/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Make 5 requests (under limit of 10)
		for i := range 5 {
			req := httptest.NewRequest("GET", "/api/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i+1)
			assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"))
			assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"))
		}
	})

	t.Run("rejects requests when rate limit exceeded", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		userUUID := uuid.New()
		userID := userUUID.String()
		quotaStore := &mockAPIQuotaStore{
			quotas: map[string]UserAPIQuota{
				userID: {
					UserId:               userUUID,
					MaxRequestsPerMinute: 3,
				},
			},
		}
		server := &Server{
			apiRateLimiter: NewAPIRateLimiter(client, quotaStore),
		}

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("user_id", userID)
			c.Next()
		})
		router.Use(RateLimitMiddleware(server))
		router.GET("/api/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Make 3 requests to exhaust the limit
		for i := range 3 {
			req := httptest.NewRequest("GET", "/api/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i+1)
		}

		// 4th request should be blocked
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.NotEmpty(t, w.Header().Get("Retry-After"))
		assert.Contains(t, w.Body.String(), "rate_limit_exceeded")
	})

	t.Run("returns rate limit headers on 429", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		userUUID := uuid.New()
		userID := userUUID.String()
		quotaStore := &mockAPIQuotaStore{
			quotas: map[string]UserAPIQuota{
				userID: {
					UserId:               userUUID,
					MaxRequestsPerMinute: 1,
				},
			},
		}
		server := &Server{
			apiRateLimiter: NewAPIRateLimiter(client, quotaStore),
		}

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("user_id", userID)
			c.Next()
		})
		router.Use(RateLimitMiddleware(server))
		router.GET("/api/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// First request allowed
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Second request blocked
		req = httptest.NewRequest("GET", "/api/test", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Equal(t, "1", w.Header().Get("X-RateLimit-Limit"))
		assert.Equal(t, "0", w.Header().Get("X-RateLimit-Remaining"))
		assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
		assert.NotEmpty(t, w.Header().Get("Retry-After"))
	})
}

func TestIPRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("skips rate limiting when no IP rate limiter configured", func(t *testing.T) {
		server := &Server{
			ipRateLimiter: nil,
		}

		router := gin.New()
		router.Use(IPRateLimitMiddleware(server))
		router.GET("/", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("only applies to public endpoints", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		server := &Server{
			ipRateLimiter: NewIPRateLimiter(client),
		}

		router := gin.New()
		router.Use(IPRateLimitMiddleware(server))
		router.GET("/api/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Non-public endpoint should pass without rate limiting
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// No rate limit headers for non-public endpoints
		assert.Empty(t, w.Header().Get("X-RateLimit-Limit"))
	})

	t.Run("rate limits public endpoints by IP", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		server := &Server{
			ipRateLimiter: NewIPRateLimiter(client),
		}

		router := gin.New()
		router.Use(IPRateLimitMiddleware(server))
		router.GET("/", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Make 10 requests to exhaust the limit
		for i := range 10 {
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i+1)
		}

		// 11th request should be blocked
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), "IP rate limit exceeded")
	})

	t.Run("extracts IP from X-Forwarded-For header", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		server := &Server{
			ipRateLimiter: NewIPRateLimiter(client),
		}

		router := gin.New()
		router.Use(IPRateLimitMiddleware(server))
		router.GET("/", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Request with X-Forwarded-For
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "10", w.Header().Get("X-RateLimit-Limit"))
	})
}

func TestAuthFlowRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("skips rate limiting when no auth flow rate limiter configured", func(t *testing.T) {
		server := &Server{
			authFlowRateLimiter: nil,
		}

		router := gin.New()
		router.Use(AuthFlowRateLimitMiddleware(server))
		router.GET("/oauth2/authorize", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/oauth2/authorize", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("only applies to auth flow endpoints", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		server := &Server{
			authFlowRateLimiter: NewAuthFlowRateLimiter(client),
		}

		router := gin.New()
		router.Use(AuthFlowRateLimitMiddleware(server))
		router.GET("/api/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Non-auth endpoint should pass without rate limiting
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Header().Get("X-RateLimit-Limit"))
	})

	t.Run("rate limits by session scope", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		server := &Server{
			authFlowRateLimiter: NewAuthFlowRateLimiter(client),
		}

		router := gin.New()
		router.Use(AuthFlowRateLimitMiddleware(server))
		router.GET("/oauth2/authorize", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		sessionState := uuid.New().String()

		// Make 5 requests to exhaust session limit
		for i := range 5 {
			req := httptest.NewRequest("GET", "/oauth2/authorize?state="+sessionState, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i+1)
		}

		// 6th request should be blocked
		req := httptest.NewRequest("GET", "/oauth2/authorize?state="+sessionState, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), "session")
	})

	t.Run("rate limits by user identifier scope", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		server := &Server{
			authFlowRateLimiter: NewAuthFlowRateLimiter(client),
		}

		router := gin.New()
		router.Use(AuthFlowRateLimitMiddleware(server))
		router.GET("/oauth2/authorize", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		loginHint := "testuser@example.com"

		// Make 10 requests to exhaust user limit (10/hour)
		for i := range 10 {
			// Use different state each time to avoid session limit
			req := httptest.NewRequest("GET", "/oauth2/authorize?state="+uuid.New().String()+"&login_hint="+loginHint, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Request %d should be allowed", i+1)
		}

		// 11th request should be blocked by user scope
		req := httptest.NewRequest("GET", "/oauth2/authorize?state="+uuid.New().String()+"&login_hint="+loginHint, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), "user")
	})
}

func TestIsPublicEndpoint(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/", true},
		{"/.well-known/openid-configuration", true},
		{"/.well-known/jwks.json", true},
		{"/api/test", false},
		{"/threat-models", false},
		{"/oauth2/authorize", false}, // Auth flow, not public
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := isPublicEndpoint(tc.path)
			assert.Equal(t, tc.expected, result, "isPublicEndpoint(%q)", tc.path)
		})
	}
}

func TestIsAuthFlowEndpoint(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/oauth2/authorize", true},
		{"/oauth2/callback", true},
		{"/oauth2/token", true},
		{"/oauth2/refresh", true},
		{"/oauth2/introspect", true},
		{"/saml/login", true},
		{"/saml/acs", true},
		{"/saml/slo", true},
		{"/", false},
		{"/api/test", false},
		{"/threat-models", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := isAuthFlowEndpoint(tc.path)
			assert.Equal(t, tc.expected, result, "isAuthFlowEndpoint(%q)", tc.path)
		})
	}
}

func TestExtractIPAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("extracts from X-Forwarded-For", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")

		ip := extractIPAddress(c)
		assert.Equal(t, "203.0.113.195", ip)
	})

	t.Run("extracts from X-Real-IP", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.Header.Set("X-Real-IP", "192.168.1.100")

		ip := extractIPAddress(c)
		assert.Equal(t, "192.168.1.100", ip)
	})

	t.Run("falls back to ClientIP", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.RemoteAddr = "10.0.0.1:12345"

		ip := extractIPAddress(c)
		assert.NotEmpty(t, ip)
	})
}

func TestExtractSessionID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("extracts from state parameter", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/oauth2/authorize?state=abc123", nil)

		sessionID := extractSessionID(c)
		assert.Equal(t, "abc123", sessionID)
	})

	t.Run("extracts from RelayState parameter", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/saml/acs?RelayState=saml123", nil)

		sessionID := extractSessionID(c)
		assert.Equal(t, "saml123", sessionID)
	})

	t.Run("extracts from code parameter", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/oauth2/token?code=authcode123", nil)

		sessionID := extractSessionID(c)
		assert.Equal(t, "authcode123", sessionID)
	})

	t.Run("returns empty when no session identifier found", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/oauth2/authorize", nil)

		sessionID := extractSessionID(c)
		assert.Empty(t, sessionID)
	})
}

func TestExtractUserIdentifier(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("extracts from login_hint parameter", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/oauth2/authorize?login_hint=User@Example.com", nil)

		userID := extractUserIdentifier(c)
		assert.Equal(t, "user@example.com", userID)
	})

	t.Run("returns empty when no login_hint", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/oauth2/authorize", nil)

		userID := extractUserIdentifier(c)
		assert.Empty(t, userID)
	})
}

func TestAPIRateLimiter(t *testing.T) {
	t.Run("allows when redis not available", func(t *testing.T) {
		quotaStore := &mockAPIQuotaStore{
			quotas: make(map[string]UserAPIQuota),
		}
		limiter := NewAPIRateLimiter(nil, quotaStore)

		allowed, retryAfter, err := limiter.CheckRateLimit(context.Background(), "user123")
		require.NoError(t, err)
		assert.True(t, allowed)
		assert.Equal(t, 0, retryAfter)
	})

	t.Run("returns default info when redis not available", func(t *testing.T) {
		quotaStore := &mockAPIQuotaStore{
			quotas: make(map[string]UserAPIQuota),
		}
		limiter := NewAPIRateLimiter(nil, quotaStore)

		limit, remaining, resetAt, err := limiter.GetRateLimitInfo(context.Background(), "user123")
		require.NoError(t, err)
		assert.Equal(t, DefaultMaxRequestsPerMinute, limit)
		assert.Equal(t, DefaultMaxRequestsPerMinute, remaining)
		assert.Greater(t, resetAt, int64(0))
	})

	t.Run("enforces per-minute rate limit", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		userUUID := uuid.New()
		userID := userUUID.String()
		quotaStore := &mockAPIQuotaStore{
			quotas: map[string]UserAPIQuota{
				userID: {
					UserId:               userUUID,
					MaxRequestsPerMinute: 3,
				},
			},
		}
		limiter := NewAPIRateLimiter(client, quotaStore)

		// First 3 requests should succeed
		for i := range 3 {
			allowed, _, err := limiter.CheckRateLimit(context.Background(), userID)
			require.NoError(t, err)
			assert.True(t, allowed, "Request %d should be allowed", i+1)
		}

		// 4th request should be blocked
		allowed, retryAfter, err := limiter.CheckRateLimit(context.Background(), userID)
		require.NoError(t, err)
		assert.False(t, allowed)
		assert.Greater(t, retryAfter, 0)
	})

	t.Run("enforces per-hour rate limit", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		userUUID := uuid.New()
		userID := userUUID.String()
		hourlyLimit := 2
		quotaStore := &mockAPIQuotaStore{
			quotas: map[string]UserAPIQuota{
				userID: {
					UserId:               userUUID,
					MaxRequestsPerMinute: 100, // High per-minute
					MaxRequestsPerHour:   &hourlyLimit,
				},
			},
		}
		limiter := NewAPIRateLimiter(client, quotaStore)

		// First 2 requests should succeed
		for i := range 2 {
			allowed, _, err := limiter.CheckRateLimit(context.Background(), userID)
			require.NoError(t, err)
			assert.True(t, allowed, "Request %d should be allowed", i+1)
		}

		// 3rd request should be blocked by hourly limit
		allowed, _, err := limiter.CheckRateLimit(context.Background(), userID)
		require.NoError(t, err)
		assert.False(t, allowed)
	})
}

func TestIPRateLimiter(t *testing.T) {
	t.Run("allows when redis not available", func(t *testing.T) {
		limiter := NewIPRateLimiter(nil)

		allowed, retryAfter, err := limiter.CheckRateLimit(context.Background(), "192.168.1.1", 10, 60)
		require.NoError(t, err)
		assert.True(t, allowed)
		assert.Equal(t, 0, retryAfter)
	})

	t.Run("returns default info when redis not available", func(t *testing.T) {
		limiter := NewIPRateLimiter(nil)

		remaining, resetAt, err := limiter.GetRateLimitInfo(context.Background(), "192.168.1.1", 10, 60)
		require.NoError(t, err)
		assert.Equal(t, 10, remaining)
		assert.Greater(t, resetAt, int64(0))
	})

	t.Run("enforces rate limit by IP", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		limiter := NewIPRateLimiter(client)
		ipAddress := "192.168.1.100"
		limit := 3
		windowSeconds := 60

		// First 3 requests should succeed
		for i := range 3 {
			allowed, _, err := limiter.CheckRateLimit(context.Background(), ipAddress, limit, windowSeconds)
			require.NoError(t, err)
			assert.True(t, allowed, "Request %d should be allowed", i+1)
		}

		// 4th request should be blocked
		allowed, retryAfter, err := limiter.CheckRateLimit(context.Background(), ipAddress, limit, windowSeconds)
		require.NoError(t, err)
		assert.False(t, allowed)
		assert.Greater(t, retryAfter, 0)
	})
}

func TestAuthFlowRateLimiter(t *testing.T) {
	t.Run("allows when redis not available", func(t *testing.T) {
		limiter := NewAuthFlowRateLimiter(nil)

		result, err := limiter.CheckRateLimit(context.Background(), "session1", "192.168.1.1", "user@example.com")
		require.NoError(t, err)
		assert.True(t, result.Allowed)
	})

	t.Run("enforces session scope limit", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		limiter := NewAuthFlowRateLimiter(client)
		sessionID := uuid.New().String()

		// First 5 requests should succeed
		for i := range 5 {
			result, err := limiter.CheckRateLimit(context.Background(), sessionID, "", "")
			require.NoError(t, err)
			assert.True(t, result.Allowed, "Request %d should be allowed", i+1)
		}

		// 6th request should be blocked
		result, err := limiter.CheckRateLimit(context.Background(), sessionID, "", "")
		require.NoError(t, err)
		assert.False(t, result.Allowed)
		assert.Equal(t, "session", result.BlockedByScope)
	})

	t.Run("enforces IP scope limit", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		limiter := NewAuthFlowRateLimiter(client)
		ipAddress := "192.168.1.100"

		// Make 100 requests to exhaust IP limit (use different sessions to avoid session limit)
		for i := range 100 {
			result, err := limiter.CheckRateLimit(context.Background(), uuid.New().String(), ipAddress, "")
			require.NoError(t, err)
			assert.True(t, result.Allowed, "Request %d should be allowed", i+1)
		}

		// 101st request should be blocked by IP scope
		result, err := limiter.CheckRateLimit(context.Background(), uuid.New().String(), ipAddress, "")
		require.NoError(t, err)
		assert.False(t, result.Allowed)
		assert.Equal(t, "ip", result.BlockedByScope)
	})

	t.Run("enforces user identifier scope limit", func(t *testing.T) {
		client, mr := setupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		limiter := NewAuthFlowRateLimiter(client)
		userIdentifier := "testuser@example.com"

		// Make 10 requests to exhaust user limit (use different sessions and IPs)
		for i := range 10 {
			result, err := limiter.CheckRateLimit(context.Background(), uuid.New().String(), uuid.New().String(), userIdentifier)
			require.NoError(t, err)
			assert.True(t, result.Allowed, "Request %d should be allowed", i+1)
		}

		// 11th request should be blocked by user scope
		result, err := limiter.CheckRateLimit(context.Background(), uuid.New().String(), uuid.New().String(), userIdentifier)
		require.NoError(t, err)
		assert.False(t, result.Allowed)
		assert.Equal(t, "user", result.BlockedByScope)
	})
}

// mockAPIQuotaStore implements UserAPIQuotaStoreInterface for testing
type mockAPIQuotaStore struct {
	quotas map[string]UserAPIQuota
}

func (m *mockAPIQuotaStore) Get(userID string) (UserAPIQuota, error) {
	if quota, ok := m.quotas[userID]; ok {
		return quota, nil
	}
	return UserAPIQuota{}, nil
}

func (m *mockAPIQuotaStore) GetOrDefault(userID string) UserAPIQuota {
	if quota, ok := m.quotas[userID]; ok {
		return quota
	}
	userUUID, _ := uuid.Parse(userID)
	return UserAPIQuota{
		UserId:               userUUID,
		MaxRequestsPerMinute: DefaultMaxRequestsPerMinute,
	}
}

func (m *mockAPIQuotaStore) Create(item UserAPIQuota) (UserAPIQuota, error) {
	m.quotas[item.UserId.String()] = item
	return item, nil
}

func (m *mockAPIQuotaStore) Update(userID string, item UserAPIQuota) error {
	m.quotas[userID] = item
	return nil
}

func (m *mockAPIQuotaStore) Delete(userID string) error {
	delete(m.quotas, userID)
	return nil
}

func (m *mockAPIQuotaStore) List(offset, limit int) ([]UserAPIQuota, error) {
	var result []UserAPIQuota
	for _, quota := range m.quotas {
		result = append(result, quota)
	}
	return result, nil
}

func (m *mockAPIQuotaStore) Count() (int, error) {
	return len(m.quotas), nil
}
