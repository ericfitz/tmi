# IP Rate Limit Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden IP-based rate limiting for public discovery endpoints (Tier 1) with configurable rate limits, trusted proxy support, integration tests, and documentation.

**Architecture:** Add two config fields (`TMI_TRUSTED_PROXIES`, `TMI_RATELIMIT_PUBLIC_RPM`) to `ServerConfig`, wire them through to the Gin engine and IP rate limiter, update the middleware to use configured values instead of hardcoded ones, add TODO comments for future observability, write integration tests against a real server, and update wiki docs.

**Tech Stack:** Go, Gin, Redis, go-redis/v8

**Spec:** `docs/superpowers/specs/2026-04-08-ip-rate-limit-hardening-design.md`
**Issue:** [#235](https://github.com/ericfitz/tmi/issues/235)

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/config/config.go` | Modify (lines 60-72) | Add `TrustedProxies` and `RateLimitPublicRPM` to `ServerConfig` |
| `cmd/server/main.go` | Modify (lines 452, 779) | Call `SetTrustedProxies()`, pass RPM to rate limiter |
| `api/ip_rate_limiter.go` | Modify | Add `DefaultLimit` field, use it in middleware |
| `api/ip_and_auth_rate_limit_middleware.go` | Modify | Use config-driven limit, proxy-aware IP extraction, TODO comments |
| `api/rate_limit_middleware_test.go` | Modify | Update unit tests for configurable limit |
| `test/integration/workflows/ip_rate_limit_test.go` | Create | Integration tests (7 test cases) |
| `tmi.wiki/Configuration-Reference.md` | Modify | Add Rate Limiting section |
| `tmi.wiki/API-Rate-Limiting.md` | Modify | Update Tier 1 configurability, add trusted proxy docs |

---

### Task 1: Add Config Fields

**Files:**
- Modify: `internal/config/config.go:60-72`

- [ ] **Step 1: Write failing test for new config fields**

Open `internal/config/config_test.go` and add a test that verifies the new fields are parsed from YAML and env vars. Find the existing test pattern first:

```bash
make test-unit name=TestConfig
```

Look at how existing fields like `AllowedOrigins` (a `[]string` with env tag) are tested, then add:

```go
func TestServerConfig_TrustedProxies(t *testing.T) {
	// Test YAML parsing
	yamlData := []byte(`
server:
  trusted_proxies:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
  ratelimit_public_rpm: 20
`)
	var cfg Config
	err := yaml.Unmarshal(yamlData, &cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.0/8", "172.16.0.0/12"}, cfg.Server.TrustedProxies)
	assert.Equal(t, 20, cfg.Server.RateLimitPublicRPM)
}

func TestServerConfig_RateLimitPublicRPM_Default(t *testing.T) {
	yamlData := []byte(`
server:
  port: "8080"
`)
	var cfg Config
	err := yaml.Unmarshal(yamlData, &cfg)
	require.NoError(t, err)
	// Zero value — caller must apply default of 10
	assert.Equal(t, 0, cfg.Server.RateLimitPublicRPM)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
make test-unit name=TestServerConfig_TrustedProxies
```

Expected: FAIL — `TrustedProxies` and `RateLimitPublicRPM` fields don't exist yet.

- [ ] **Step 3: Add the config fields**

In `internal/config/config.go`, add to `ServerConfig` (after `HTTPToHTTPSRedirect` on line 71, before `CORS` on line 72):

```go
// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port                string        `yaml:"port" env:"TMI_SERVER_PORT"`
	Interface           string        `yaml:"interface" env:"TMI_SERVER_INTERFACE"`
	BaseURL             string        `yaml:"base_url" env:"TMI_SERVER_BASE_URL"` // Public base URL for callbacks (auto-inferred if empty)
	ReadTimeout         time.Duration `yaml:"read_timeout" env:"TMI_SERVER_READ_TIMEOUT"`
	WriteTimeout        time.Duration `yaml:"write_timeout" env:"TMI_SERVER_WRITE_TIMEOUT"`
	IdleTimeout         time.Duration `yaml:"idle_timeout" env:"TMI_SERVER_IDLE_TIMEOUT"`
	TLSEnabled          bool          `yaml:"tls_enabled" env:"TMI_SERVER_TLS_ENABLED"`
	TLSCertFile         string        `yaml:"tls_cert_file" env:"TMI_SERVER_TLS_CERT_FILE"`
	TLSKeyFile          string        `yaml:"tls_key_file" env:"TMI_SERVER_TLS_KEY_FILE"`
	TLSSubjectName      string        `yaml:"tls_subject_name" env:"TMI_SERVER_TLS_SUBJECT_NAME"`
	HTTPToHTTPSRedirect bool          `yaml:"http_to_https_redirect" env:"TMI_SERVER_HTTP_TO_HTTPS_REDIRECT"`
	TrustedProxies      []string      `yaml:"trusted_proxies" env:"TMI_TRUSTED_PROXIES"` // Comma-separated CIDRs/IPs for X-Forwarded-For validation
	RateLimitPublicRPM  int           `yaml:"ratelimit_public_rpm" env:"TMI_RATELIMIT_PUBLIC_RPM"` // Requests/min per IP for public endpoints (default: 10)
	CORS                CORSConfig    `yaml:"cors"`
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
make test-unit name=TestServerConfig_TrustedProxies
make test-unit name=TestServerConfig_RateLimitPublicRPM_Default
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add TMI_TRUSTED_PROXIES and TMI_RATELIMIT_PUBLIC_RPM settings

Closes #235 (partial)

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Add DefaultLimit to IPRateLimiter

**Files:**
- Modify: `api/ip_rate_limiter.go`
- Modify: `api/rate_limit_middleware_test.go`

- [ ] **Step 1: Write failing test for configurable limit**

In `api/rate_limit_middleware_test.go`, add a test inside the existing `TestIPRateLimitMiddleware` function:

```go
t.Run("uses configured rate limit instead of hardcoded value", func(t *testing.T) {
	client, mr := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	limiter := NewIPRateLimiter(client)
	limiter.DefaultLimit = 3 // Very low for testing
	limiter.DefaultWindowSeconds = 60
	server := &Server{
		ipRateLimiter: limiter,
	}

	router := gin.New()
	router.Use(IPRateLimitMiddleware(server))
	router.GET("/.well-known/openid-configuration", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"issuer": "test"})
	})

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/.well-known/openid-configuration", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed", i+1)
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/.well-known/openid-configuration", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "3", w.Header().Get("X-RateLimit-Limit"))
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
make test-unit name=TestIPRateLimitMiddleware
```

Expected: FAIL — `DefaultLimit` and `DefaultWindowSeconds` fields don't exist on `IPRateLimiter`.

- [ ] **Step 3: Add DefaultLimit and DefaultWindowSeconds to IPRateLimiter**

In `api/ip_rate_limiter.go`, modify the struct and constructor:

```go
// IPRateLimiter implements rate limiting based on IP address
type IPRateLimiter struct {
	SlidingWindowRateLimiter
	DefaultLimit         int // Requests per window (default: 10)
	DefaultWindowSeconds int // Window size in seconds (default: 60)
}

// NewIPRateLimiter creates a new IP-based rate limiter
func NewIPRateLimiter(redisClient *redis.Client) *IPRateLimiter {
	return &IPRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: redisClient},
		DefaultLimit:             10,
		DefaultWindowSeconds:     60,
	}
}
```

- [ ] **Step 4: Update IPRateLimitMiddleware to use configured values**

In `api/ip_and_auth_rate_limit_middleware.go`, replace the hardcoded `10` and `60` values in `IPRateLimitMiddleware` (lines 39, 48, 51):

```go
// IPRateLimitMiddleware creates middleware for IP-based rate limiting (Tier 1 - public discovery)
func IPRateLimitMiddleware(server *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Skip if no IP rate limiter configured
		if server.ipRateLimiter == nil {
			c.Next()
			return
		}

		// Only apply to public discovery endpoints
		path := c.Request.URL.Path
		if !isPublicEndpoint(path) {
			c.Next()
			return
		}

		// Extract IP address (proxy-aware when trusted proxies configured)
		ipAddress := extractIPAddress(c)
		if ipAddress == "" {
			logger.Warn("Could not extract IP address for rate limiting")
			c.Next()
			return
		}

		limit := server.ipRateLimiter.DefaultLimit
		window := server.ipRateLimiter.DefaultWindowSeconds

		// Check rate limit
		allowed, retryAfter, err := server.ipRateLimiter.CheckRateLimit(c.Request.Context(), ipAddress, limit, window)
		if err != nil {
			logger.Error("IP rate limit check failed for %s: %v", ipAddress, err)
			// Fail open
			c.Next()
			return
		}

		// Get rate limit info for headers
		remaining, resetAt, _ := server.ipRateLimiter.GetRateLimitInfo(c.Request.Context(), ipAddress, limit, window)

		// Add rate limit headers
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))

		if !allowed {
			// TODO: emit structured log event with IP, endpoint, and remaining count on rate limit block
			// TODO: emit rate_limit_blocked metric counter with labels {tier: "public-discovery", ip: extractedIP}
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			c.JSON(http.StatusTooManyRequests, Error{
				Error:            "rate_limit_exceeded",
				ErrorDescription: "IP rate limit exceeded. Please retry after the specified time.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
make test-unit name=TestIPRateLimitMiddleware
```

Expected: ALL pass, including new configurable limit test.

- [ ] **Step 6: Commit**

```bash
git add api/ip_rate_limiter.go api/ip_and_auth_rate_limit_middleware.go api/rate_limit_middleware_test.go
git commit -m "feat(api): make IP rate limit configurable via DefaultLimit field

Replace hardcoded 10 req/min with IPRateLimiter.DefaultLimit (default: 10).
Add TODO comments for future metrics and structured logging.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Wire Config to Startup

**Files:**
- Modify: `cmd/server/main.go:452, 779`

- [ ] **Step 1: Apply trusted proxy config at Gin engine startup**

In `cmd/server/main.go`, in the `setupRouter` function, after `r := gin.New()` (line 452) and the gin mode configuration (around line 459), add:

```go
	// Configure trusted proxies for X-Forwarded-For validation
	if len(config.Server.TrustedProxies) > 0 {
		if err := r.SetTrustedProxies(config.Server.TrustedProxies); err != nil {
			slogging.Get().Error("Failed to set trusted proxies: %v", err)
		} else {
			slogging.Get().Info("Trusted proxies configured: %v", config.Server.TrustedProxies)
		}
	}
```

- [ ] **Step 2: Pass configured RPM to IPRateLimiter**

In `cmd/server/main.go`, where `NewIPRateLimiter` is called (line 779), set the configured limit:

```go
		logger.Info("Initializing IP rate limiter")
		ipLimiter := api.NewIPRateLimiter(dbManager.Redis().GetClient())
		if config.Server.RateLimitPublicRPM > 0 {
			ipLimiter.DefaultLimit = config.Server.RateLimitPublicRPM
		}
		apiServer.SetIPRateLimiter(ipLimiter)
```

- [ ] **Step 3: Update extractIPAddress to be proxy-aware**

In `api/ip_and_auth_rate_limit_middleware.go`, update `extractIPAddress` to accept a boolean indicating whether trusted proxies are configured. When trusted proxies are configured, use `c.ClientIP()` directly (Gin validates the chain). When not configured, keep current manual extraction.

First, add a field to `Server` in `api/server.go`:

```go
	// Trusted proxy configuration
	trustedProxiesConfigured bool
```

Add a setter method next to the existing rate limiter setters:

```go
// SetTrustedProxiesConfigured marks whether trusted proxies have been configured
func (s *Server) SetTrustedProxiesConfigured(configured bool) {
	s.trustedProxiesConfigured = configured
}
```

Then update `extractIPAddress` to accept the flag:

```go
// extractIPAddress extracts the client IP address from the request.
// When trusted proxies are configured, uses Gin's ClientIP() which validates
// the X-Forwarded-For chain. Otherwise, extracts from headers directly.
func extractIPAddress(c *gin.Context, trustedProxiesConfigured bool) string {
	if trustedProxiesConfigured {
		// Gin's ClientIP() validates X-Forwarded-For against trusted proxy list
		return c.ClientIP()
	}

	// Manual extraction when no trusted proxies configured (backward compatible)
	// Try X-Forwarded-For first (for proxied requests)
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Try X-Real-IP
	if xri := c.GetHeader("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return c.ClientIP()
}
```

Update all callers of `extractIPAddress` in the same file. There are two call sites:

1. In `IPRateLimitMiddleware` (line 31): `extractIPAddress(c)` → `extractIPAddress(c, server.trustedProxiesConfigured)`
2. In `AuthFlowRateLimitMiddleware` (line 89): `extractIPAddress(c)` → `extractIPAddress(c, server.trustedProxiesConfigured)`

- [ ] **Step 4: Set the flag in main.go**

In `cmd/server/main.go`, after the `SetTrustedProxies` block added in Step 1:

```go
	if len(config.Server.TrustedProxies) > 0 {
		if err := r.SetTrustedProxies(config.Server.TrustedProxies); err != nil {
			slogging.Get().Error("Failed to set trusted proxies: %v", err)
		} else {
			slogging.Get().Info("Trusted proxies configured: %v", config.Server.TrustedProxies)
		}
	}
```

Then later, after `apiServer` is created (look for where other `apiServer.Set*` calls are made), add:

```go
	apiServer.SetTrustedProxiesConfigured(len(config.Server.TrustedProxies) > 0)
```

- [ ] **Step 5: Update existing unit tests for new extractIPAddress signature**

In `api/rate_limit_middleware_test.go`, find all calls to `extractIPAddress` in test helper functions or direct calls. The tests in `TestExtractIPAddress` call `extractIPAddress(c)` — update them to `extractIPAddress(c, false)` (testing the backward-compatible path).

Add additional tests for the trusted-proxy path:

```go
t.Run("uses ClientIP when trusted proxies configured", func(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	c.Request.RemoteAddr = "10.0.0.1:12345"

	// With trusted proxies, should use Gin's ClientIP()
	ip := extractIPAddress(c, true)
	// Gin's ClientIP() without SetTrustedProxies returns RemoteAddr IP
	assert.NotEmpty(t, ip)
})
```

- [ ] **Step 6: Build and run all unit tests**

```bash
make build-server
make test-unit
```

Expected: Build succeeds, all tests pass.

- [ ] **Step 7: Commit**

```bash
git add cmd/server/main.go api/server.go api/ip_and_auth_rate_limit_middleware.go api/rate_limit_middleware_test.go
git commit -m "feat: wire trusted proxies and configurable RPM to server startup

SetTrustedProxies called at Gin engine init when TMI_TRUSTED_PROXIES is set.
extractIPAddress uses Gin's validated ClientIP() when proxies configured,
falls back to manual header extraction otherwise (backward compatible).
RateLimitPublicRPM passed through to IPRateLimiter.DefaultLimit.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Write Integration Tests

**Files:**
- Create: `test/integration/workflows/ip_rate_limit_test.go`

- [ ] **Step 1: Create the integration test file**

Create `test/integration/workflows/ip_rate_limit_test.go`:

```go
package workflows

import (
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestIPRateLimiting_PublicEndpoints verifies IP-based rate limiting
// on public discovery endpoints (Tier 1).
// Requires: running TMI server + Redis (via make start-dev)
func TestIPRateLimiting_PublicEndpoints(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Clear rate limit keys before tests to avoid pollution
	_ = framework.ClearRateLimits()

	t.Run("rate limits public discovery endpoints", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Default limit is 10 req/min — send 10 requests, all should succeed
		for i := 0; i < 10; i++ {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/.well-known/openid-configuration",
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				t.Fatalf("Request %d was rate limited unexpectedly", i+1)
			}
		}

		// 11th request should be rate limited
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/.well-known/openid-configuration",
		})
		framework.AssertNoError(t, err, "11th request failed")

		if resp.StatusCode != 429 {
			t.Errorf("Expected 429 on 11th request, got %d", resp.StatusCode)
		}

		// Verify rate limit headers
		if resp.Headers.Get("X-RateLimit-Limit") == "" {
			t.Error("Missing X-RateLimit-Limit header")
		}
		if resp.Headers.Get("X-RateLimit-Remaining") == "" {
			t.Error("Missing X-RateLimit-Remaining header")
		}
		if resp.Headers.Get("X-RateLimit-Reset") == "" {
			t.Error("Missing X-RateLimit-Reset header")
		}
		if resp.Headers.Get("Retry-After") == "" {
			t.Error("Missing Retry-After header on 429 response")
		}
	})

	t.Run("health check endpoint excluded from rate limiting", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Send 15 requests to GET / (more than the 10/min limit)
		for i := 0; i < 15; i++ {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/",
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			if resp.StatusCode == 429 {
				t.Fatalf("Health check request %d was rate limited — GET / should be excluded", i+1)
			}
		}
	})

	t.Run("rate limit headers present on successful responses", func(t *testing.T) {
		_ = framework.ClearRateLimits()
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/.well-known/openid-configuration",
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		if resp.Headers.Get("X-RateLimit-Limit") == "" {
			t.Error("Missing X-RateLimit-Limit header on 200 response")
		}
		if resp.Headers.Get("X-RateLimit-Remaining") == "" {
			t.Error("Missing X-RateLimit-Remaining header on 200 response")
		}
		if resp.Headers.Get("X-RateLimit-Reset") == "" {
			t.Error("Missing X-RateLimit-Reset header on 200 response")
		}
	})

	t.Run("different IPs are rate limited independently", func(t *testing.T) {
		_ = framework.ClearRateLimits()

		// This test verifies that X-Forwarded-For headers with different IPs
		// result in independent rate limit counters. We send requests with
		// two different forwarded IPs, exceeding the limit for one but not the other.
		client, err := framework.NewUnauthenticatedClient(serverURL, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create client")

		// Exhaust limit for IP "198.51.100.1"
		for i := 0; i < 11; i++ {
			resp, err := client.Do(framework.Request{
				Method:  "GET",
				Path:    "/.well-known/openid-configuration",
				Headers: map[string]string{"X-Forwarded-For": "198.51.100.1"},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			_ = resp
		}

		// Request from different IP "198.51.100.2" should still succeed
		resp, err := client.Do(framework.Request{
			Method:  "GET",
			Path:    "/.well-known/openid-configuration",
			Headers: map[string]string{"X-Forwarded-For": "198.51.100.2"},
		})
		framework.AssertNoError(t, err, "Request from different IP failed")
		if resp.StatusCode == 429 {
			t.Error("Request from different IP was rate limited — IPs should have independent counters")
		}
	})

	t.Run("non-public endpoints not affected by IP rate limiter", func(t *testing.T) {
		_ = framework.ClearRateLimits()

		// Authenticate a user for accessing protected endpoints
		if err := framework.EnsureOAuthStubRunning(); err != nil {
			t.Skipf("OAuth stub not running, skipping: %v", err)
		}
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Failed to authenticate user")

		client, err := framework.NewClient(serverURL, tokens, framework.WithValidation(false))
		framework.AssertNoError(t, err, "Failed to create authenticated client")

		// Send 15 requests to an authenticated endpoint (exceeds IP limit of 10)
		for i := 0; i < 15; i++ {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   "/threat_models",
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request %d failed", i+1))
			// Should not get 429 from IP rate limiter
			// (may get 429 from API rate limiter at 1000/min, but not at 15 requests)
			if resp.StatusCode == 429 {
				t.Fatalf("Authenticated endpoint request %d was IP rate limited — only public endpoints should be affected", i+1)
			}
		}
	})
}
```

- [ ] **Step 2: Run integration tests to verify they work**

Start the dev environment if not running:

```bash
make start-dev
```

Then run:

```bash
make test-integration
```

Expected: All existing tests pass, plus the new `TestIPRateLimiting_PublicEndpoints` tests pass.

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/ip_rate_limit_test.go
git commit -m "test(integration): add IP rate limiting tests for public endpoints

Tests: rate limit enforcement, health check exclusion, headers on success,
independent IP counters, non-public endpoint passthrough.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Add Unit Tests for Fail-Open and Configurable Limit

**Files:**
- Modify: `api/rate_limit_middleware_test.go`

The spec calls for "Redis unavailable — fail open" and "configurable rate limit applied" integration tests. These require server restart or Redis teardown, which can't be done from workflow tests. Cover them as unit tests instead.

- [ ] **Step 1: Add fail-open unit test**

In `api/rate_limit_middleware_test.go`, inside `TestIPRateLimitMiddleware`, add:

```go
t.Run("fails open when Redis is unavailable", func(t *testing.T) {
	// Create limiter with nil Redis client to simulate unavailability
	limiter := &IPRateLimiter{
		SlidingWindowRateLimiter: SlidingWindowRateLimiter{RedisClient: nil},
		DefaultLimit:             10,
		DefaultWindowSeconds:     60,
	}
	server := &Server{
		ipRateLimiter: limiter,
	}

	router := gin.New()
	router.Use(IPRateLimitMiddleware(server))
	router.GET("/.well-known/openid-configuration", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"issuer": "test"})
	})

	req := httptest.NewRequest("GET", "/.well-known/openid-configuration", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should allow request through (fail-open behavior)
	assert.Equal(t, http.StatusOK, w.Code)
})
```

- [ ] **Step 2: Add configurable limit unit test with non-default value**

This test is already added in Task 2 Step 1 ("uses configured rate limit instead of hardcoded value"). Verify it exists and covers the case of `DefaultLimit = 3`.

- [ ] **Step 3: Run tests**

```bash
make test-unit name=TestIPRateLimitMiddleware
```

Expected: ALL pass.

- [ ] **Step 4: Commit**

```bash
git add api/rate_limit_middleware_test.go
git commit -m "test: add fail-open unit test for IP rate limiter without Redis

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Update Wiki Documentation

**Files:**
- Modify: `/Users/efitz/Projects/tmi.wiki/Configuration-Reference.md`
- Modify: `/Users/efitz/Projects/tmi.wiki/API-Rate-Limiting.md`

- [ ] **Step 1: Add Rate Limiting section to Configuration-Reference.md**

In `/Users/efitz/Projects/tmi.wiki/Configuration-Reference.md`, after the "### Server Settings" table (which ends around the `TMI_SERVER_INTERFACE` row), add a new subsection:

```markdown
### Rate Limiting

| Variable | Default | Description |
|----------|---------|-------------|
| `TMI_TRUSTED_PROXIES` | _(none)_ | Comma-separated list of trusted proxy CIDRs or IP addresses for `X-Forwarded-For` validation (e.g., `10.0.0.0/8,172.16.0.0/12`) |
| `TMI_RATELIMIT_PUBLIC_RPM` | 10 | Maximum requests per minute per IP address for public discovery endpoints (Tier 1) |

**Trusted Proxy Behavior:**

When `TMI_TRUSTED_PROXIES` is set, TMI uses Gin's trusted proxy validation to verify the `X-Forwarded-For` header chain. Only requests forwarded through listed proxies will have their `X-Forwarded-For` headers trusted for IP extraction. This prevents IP spoofing when TMI is deployed behind a load balancer or reverse proxy.

When not set (default), TMI extracts the client IP directly from request headers without validation. This is the backward-compatible behavior and is acceptable for defense-in-depth since the Tier 1 rate limit (10 req/min) is lenient.

**Example configurations:**

```yaml
# YAML configuration
server:
  trusted_proxies:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
  ratelimit_public_rpm: 20

# Environment variables
TMI_TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12
TMI_RATELIMIT_PUBLIC_RPM=20
```
```

- [ ] **Step 2: Update API-Rate-Limiting.md Tier 1 section**

In `/Users/efitz/Projects/tmi.wiki/API-Rate-Limiting.md`, find the Tier 1 rate limit configuration block (around line 63) and update `configurable: false` to `configurable: true`:

```yaml
scope: ip
tier: public-discovery
limits:
  - type: requests_per_minute
    default: 10
    configurable: true
    env_var: TMI_RATELIMIT_PUBLIC_RPM
    tracking_method: Source IP address
```

Also update the Tier 1 table row at the top of the file (around line 30). Change:

```
| 1 | Public Discovery | IP | No | 4 |
```

to:

```
| 1 | Public Discovery | IP | Yes | 4 |
```

- [ ] **Step 3: Add Trusted Proxy Configuration section to API-Rate-Limiting.md**

In the "Implementation Notes" section (around line 740), add a new subsection before "### Technology Stack":

```markdown
### Trusted Proxy Configuration

TMI supports configurable trusted proxies for accurate client IP extraction when deployed behind load balancers or reverse proxies.

**Configuration:** `TMI_TRUSTED_PROXIES` (comma-separated CIDRs/IPs)

**Behavior:**
- **When set:** Gin validates the `X-Forwarded-For` header chain against the trusted proxy list. Only the rightmost untrusted IP is used for rate limiting, preventing clients from spoofing their IP via the header.
- **When not set (default):** The first IP in `X-Forwarded-For` is used directly, falling back to `X-Real-IP`, then `RemoteAddr`. This is backward-compatible but allows IP spoofing.

**Common configurations:**

| Environment | Trusted Proxies |
|------------|-----------------|
| AWS ALB | ALB subnet CIDRs (e.g., `10.0.0.0/16`) |
| Kubernetes | Pod network CIDR (e.g., `10.244.0.0/16`) |
| Cloudflare | [Cloudflare IP ranges](https://www.cloudflare.com/ips/) |
| Local dev | Not needed (direct connections) |

**Security note:** If your deployment uses a reverse proxy and you do not configure `TMI_TRUSTED_PROXIES`, clients can bypass IP-based rate limiting by setting a fake `X-Forwarded-For` header. For production deployments behind a proxy, always configure this setting.
```

- [ ] **Step 4: Commit wiki changes**

```bash
cd /Users/efitz/Projects/tmi.wiki
git add Configuration-Reference.md API-Rate-Limiting.md
git commit -m "docs: add trusted proxy and configurable rate limit documentation

Add TMI_TRUSTED_PROXIES and TMI_RATELIMIT_PUBLIC_RPM to Configuration Reference.
Update Tier 1 to configurable=true in API Rate Limiting.
Add Trusted Proxy Configuration section with deployment examples.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Final Verification

- [ ] **Step 1: Run linter**

```bash
make lint
```

Expected: No new lint errors.

- [ ] **Step 2: Build**

```bash
make build-server
```

Expected: Build succeeds.

- [ ] **Step 3: Run all unit tests**

```bash
make test-unit
```

Expected: All tests pass.

- [ ] **Step 4: Run integration tests**

```bash
make start-dev
make test-integration
```

Expected: All tests pass including new IP rate limit tests.

- [ ] **Step 5: Push both repos**

```bash
cd /Users/efitz/Projects/tmi
git push

cd /Users/efitz/Projects/tmi.wiki
git push
```

- [ ] **Step 6: Close issue**

```bash
gh issue close 235 --repo ericfitz/tmi --comment "Completed: configurable IP rate limits (TMI_RATELIMIT_PUBLIC_RPM), trusted proxy support (TMI_TRUSTED_PROXIES), integration tests, and wiki documentation."
```
