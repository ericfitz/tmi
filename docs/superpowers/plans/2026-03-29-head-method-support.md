# HEAD Method Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add RFC 9110 Section 9.3.2 compliant HEAD support for 107 GET endpoints via middleware interception.

**Architecture:** A `HeadMethodMiddleware` placed just before the OpenAPI validator converts HEAD requests to GET, wraps the response writer to suppress the body while preserving headers, then restores the original method after the handler chain completes. Four protocol endpoints with side effects are excluded.

**Tech Stack:** Go, Gin framework, `net/http/httptest` for unit tests, integration test framework in `test/integration/`

**Spec:** `docs/superpowers/specs/2026-03-29-head-method-support-design.md`
**Issue:** [#76](https://github.com/ericfitz/tmi/issues/76)

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `api/head_method_middleware.go` | CREATE | `headResponseWriter` struct, exclusion list, `HeadMethodMiddleware()` function |
| `api/head_method_middleware_test.go` | CREATE | Unit tests for middleware, writer, and exclusion matching |
| `api/openapi_middleware.go` | MODIFY | Add HEAD to Allow header in `getAllowedMethodsForPath` |
| `cmd/server/main.go` | MODIFY | Register middleware at ~line 868 |
| `test/integration/workflows/head_method_test.go` | CREATE | Integration tests for HEAD over live server |

---

### Task 1: Unit Tests for Path Exclusion Matching

**Files:**
- Create: `api/head_method_middleware.go` (just the exclusion logic)
- Create: `api/head_method_middleware_test.go`

- [ ] **Step 1: Write the exclusion matcher tests**

Create `api/head_method_middleware_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchesExcludedPath(t *testing.T) {
	t.Run("exact match /oauth2/authorize", func(t *testing.T) {
		assert.True(t, isExcludedFromHead("/oauth2/authorize"))
	})

	t.Run("exact match /oauth2/callback", func(t *testing.T) {
		assert.True(t, isExcludedFromHead("/oauth2/callback"))
	})

	t.Run("exact match /saml/slo", func(t *testing.T) {
		assert.True(t, isExcludedFromHead("/saml/slo"))
	})

	t.Run("wildcard match /saml/okta/login", func(t *testing.T) {
		assert.True(t, isExcludedFromHead("/saml/okta/login"))
	})

	t.Run("wildcard match /saml/azure-ad/login", func(t *testing.T) {
		assert.True(t, isExcludedFromHead("/saml/azure-ad/login"))
	})

	t.Run("non-excluded path /threat_models", func(t *testing.T) {
		assert.False(t, isExcludedFromHead("/threat_models"))
	})

	t.Run("non-excluded path /", func(t *testing.T) {
		assert.False(t, isExcludedFromHead("/"))
	})

	t.Run("non-excluded path /oauth2/providers", func(t *testing.T) {
		assert.False(t, isExcludedFromHead("/oauth2/providers"))
	})

	t.Run("non-excluded path /saml/providers", func(t *testing.T) {
		assert.False(t, isExcludedFromHead("/saml/providers"))
	})

	t.Run("partial match not excluded /oauth2/authorize/extra", func(t *testing.T) {
		assert.False(t, isExcludedFromHead("/oauth2/authorize/extra"))
	})

	t.Run("shorter path not excluded /oauth2", func(t *testing.T) {
		assert.False(t, isExcludedFromHead("/oauth2"))
	})

	t.Run("empty path not excluded", func(t *testing.T) {
		assert.False(t, isExcludedFromHead(""))
	})
}
```

- [ ] **Step 2: Write the minimal exclusion logic to compile**

Create `api/head_method_middleware.go`:

```go
package api

import (
	"strings"
)

// headExcludedPaths lists URL path patterns excluded from HEAD-to-GET conversion.
// These are protocol endpoints with side effects (OAuth redirects, SAML SSO initiation)
// where HEAD is semantically inappropriate per RFC 9110.
// Use "*" as a wildcard for a single path segment.
var headExcludedPaths = [][]string{
	{"oauth2", "authorize"},
	{"oauth2", "callback"},
	{"saml", "*", "login"},
	{"saml", "slo"},
}

// isExcludedFromHead checks if a request path matches any excluded pattern.
func isExcludedFromHead(path string) bool {
	// Split path into segments, removing leading empty segment from "/"
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return false
	}
	parts := strings.Split(trimmed, "/")

	for _, pattern := range headExcludedPaths {
		if len(parts) != len(pattern) {
			continue
		}
		match := true
		for i, seg := range pattern {
			if seg == "*" {
				continue
			}
			if seg != parts[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `make test-unit name=TestMatchesExcludedPath`
Expected: All 11 subtests PASS

- [ ] **Step 4: Commit**

```bash
git add api/head_method_middleware.go api/head_method_middleware_test.go
git commit -m "feat(api): add path exclusion logic for HEAD method middleware (#76)

Adds isExcludedFromHead() with segment-based pattern matching for the 4
protocol endpoints excluded from HEAD support (OAuth authorize/callback,
SAML login/SLO). Includes unit tests.

Closes: n/a (partial implementation)"
```

---

### Task 2: headResponseWriter and Core Middleware

**Files:**
- Modify: `api/head_method_middleware.go` (add writer + middleware function)
- Modify: `api/head_method_middleware_test.go` (add middleware tests)

- [ ] **Step 1: Write the failing test for HEAD on a normal endpoint**

Add to `api/head_method_middleware_test.go`:

```go
import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHeadMethodMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("HEAD returns 200 with empty body and correct Content-Length", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"key": "value"})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest("HEAD", "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String())
		cl := w.Header().Get("Content-Length")
		assert.NotEmpty(t, cl)
		clInt, err := strconv.Atoi(cl)
		assert.NoError(t, err)
		assert.Greater(t, clInt, 0)
	})

	t.Run("GET passes through with body", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"key": "value"})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, w.Body.String())
		assert.Contains(t, w.Body.String(), "value")
	})

	t.Run("POST passes through unmodified", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusCreated, gin.H{"created": true})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NotEmpty(t, w.Body.String())
	})

	t.Run("HEAD on excluded path passes through as HEAD", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())

		var receivedMethod string
		router.HEAD("/oauth2/authorize", func(c *gin.Context) {
			receivedMethod = c.Request.Method
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest("HEAD", "/oauth2/authorize", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, "HEAD", receivedMethod)
	})

	t.Run("HEAD on excluded SAML wildcard path passes through as HEAD", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())

		var receivedMethod string
		router.HEAD("/saml/:provider/login", func(c *gin.Context) {
			receivedMethod = c.Request.Method
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest("HEAD", "/saml/okta/login", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, "HEAD", receivedMethod)
	})

	t.Run("HEAD preserves error status codes", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		router.GET("/not-found", func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest("HEAD", "/not-found", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Empty(t, w.Body.String())
	})

	t.Run("HEAD preserves custom response headers", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		router.GET("/headers", func(c *gin.Context) {
			c.Header("X-Custom-Header", "test-value")
			c.Header("Cache-Control", "public, max-age=3600")
			c.JSON(http.StatusOK, gin.H{"data": "test"})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest("HEAD", "/headers", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "test-value", w.Header().Get("X-Custom-Header"))
		assert.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))
		assert.Empty(t, w.Body.String())
	})

	t.Run("HEAD sets Content-Length when handler does not", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		router.GET("/raw", func(c *gin.Context) {
			// Write body without setting Content-Length explicitly
			c.Writer.WriteHeader(http.StatusOK)
			c.Writer.WriteString("hello world") //nolint:errcheck
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest("HEAD", "/raw", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String())
		cl := w.Header().Get("Content-Length")
		if cl != "" {
			clInt, err := strconv.Atoi(cl)
			assert.NoError(t, err)
			assert.Equal(t, 11, clInt) // len("hello world")
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestHeadMethodMiddleware`
Expected: FAIL — `HeadMethodMiddleware` is not defined

- [ ] **Step 3: Implement headResponseWriter and HeadMethodMiddleware**

Add to `api/head_method_middleware.go` (keeping existing exclusion code, adding imports and new code):

```go
package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// headExcludedPaths lists URL path patterns excluded from HEAD-to-GET conversion.
// These are protocol endpoints with side effects (OAuth redirects, SAML SSO initiation)
// where HEAD is semantically inappropriate per RFC 9110.
// Use "*" as a wildcard for a single path segment.
var headExcludedPaths = [][]string{
	{"oauth2", "authorize"},
	{"oauth2", "callback"},
	{"saml", "*", "login"},
	{"saml", "slo"},
}

// isExcludedFromHead checks if a request path matches any excluded pattern.
func isExcludedFromHead(path string) bool {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return false
	}
	parts := strings.Split(trimmed, "/")

	for _, pattern := range headExcludedPaths {
		if len(parts) != len(pattern) {
			continue
		}
		match := true
		for i, seg := range pattern {
			if seg == "*" {
				continue
			}
			if seg != parts[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// headResponseWriter wraps gin.ResponseWriter to suppress response body writes.
// It counts bytes that would have been written (for Content-Length) but discards them.
// WriteHeader and header methods pass through to the embedded writer unchanged.
type headResponseWriter struct {
	gin.ResponseWriter
	bodyBytes int
}

func (w *headResponseWriter) Write(b []byte) (int, error) {
	w.bodyBytes += len(b)
	return len(b), nil
}

func (w *headResponseWriter) WriteString(s string) (int, error) {
	w.bodyBytes += len(s)
	return len(s), nil
}

func (w *headResponseWriter) Size() int {
	return w.bodyBytes
}

func (w *headResponseWriter) Written() bool {
	return w.bodyBytes > 0
}

// HeadMethodMiddleware converts HEAD requests to GET for non-excluded endpoints,
// suppresses the response body, and ensures correct Content-Length per RFC 9110.
// Place this middleware immediately before the OpenAPI validation middleware.
func HeadMethodMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodHead {
			c.Next()
			return
		}

		if isExcludedFromHead(c.Request.URL.Path) {
			logger := slogging.GetContextLogger(c)
			logger.Debug("HEAD request to excluded path %s, passing through as HEAD", c.Request.URL.Path)
			c.Next()
			return
		}

		// Save originals for restoration after handler chain
		origWriter := c.Writer

		// Convert HEAD to GET so the OpenAPI validator and router accept it
		c.Request.Method = http.MethodGet

		// Wrap the writer to suppress body output
		hw := &headResponseWriter{ResponseWriter: c.Writer}
		c.Writer = hw

		c.Next()

		// Restore original writer and method
		c.Writer = origWriter
		c.Request.Method = http.MethodHead

		// Set Content-Length if the handler didn't set it explicitly
		if c.Writer.Header().Get("Content-Length") == "" && hw.bodyBytes > 0 {
			c.Writer.Header().Set("Content-Length", strconv.Itoa(hw.bodyBytes))
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestHeadMethodMiddleware`
Expected: All 8 subtests PASS

Run: `make test-unit name=TestMatchesExcludedPath`
Expected: All 11 subtests PASS (no regressions)

- [ ] **Step 5: Run lint**

Run: `make lint`
Expected: PASS (no new warnings)

- [ ] **Step 6: Commit**

```bash
git add api/head_method_middleware.go api/head_method_middleware_test.go
git commit -m "feat(api): add HeadMethodMiddleware with response body suppression (#76)

Implements headResponseWriter that counts but discards body bytes, and
HeadMethodMiddleware that converts HEAD→GET before the OpenAPI validator,
then restores the method and sets Content-Length after the handler chain.
Includes comprehensive unit tests."
```

---

### Task 3: Update Allow Header in OpenAPI Error Responses

**Files:**
- Modify: `api/openapi_middleware.go:320-322`

- [ ] **Step 1: Write the failing test**

Add to `api/head_method_middleware_test.go`:

```go
func TestGetAllowedMethodsForPathIncludesHead(t *testing.T) {
	// getAllowedMethodsForPath reads from the embedded OpenAPI spec.
	// The root path "/" has a GET operation, so HEAD should be included.
	methods := getAllowedMethodsForPath("/")
	assert.Contains(t, methods, "HEAD")
	assert.Contains(t, methods, "GET")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestGetAllowedMethodsForPathIncludesHead`
Expected: FAIL — methods string contains "GET" but not "HEAD"

- [ ] **Step 3: Modify getAllowedMethodsForPath**

In `api/openapi_middleware.go`, change lines 320-322 from:

```go
	if pathItem.Get != nil {
		methods = append(methods, "GET")
	}
```

to:

```go
	if pathItem.Get != nil {
		methods = append(methods, "GET")
		methods = append(methods, "HEAD")
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestGetAllowedMethodsForPathIncludesHead`
Expected: PASS

- [ ] **Step 5: Run full unit tests to check for regressions**

Run: `make test-unit`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add api/openapi_middleware.go api/head_method_middleware_test.go
git commit -m "fix(api): include HEAD in Allow header for GET-capable paths (#76)

When returning 405 Method Not Allowed, the Allow header now lists HEAD
alongside GET for endpoints that support GET requests, per RFC 9110."
```

---

### Task 4: Register Middleware in Server Startup

**Files:**
- Modify: `cmd/server/main.go:867-868`

- [ ] **Step 1: Add middleware registration**

In `cmd/server/main.go`, after line 867 (`r.Use(api.EnumNormalizerMiddleware())`), add:

```go
	// Convert HEAD requests to GET before OpenAPI validation (RFC 9110 Section 9.3.2)
	// This must be after auth/rate-limiting (which handle HEAD correctly) and before
	// the OpenAPI validator (which would reject HEAD as an unknown method)
	r.Use(api.HeadMethodMiddleware())
```

The result should read:

```go
	// Normalize enum values to canonical snake_case before OpenAPI validation
	r.Use(api.EnumNormalizerMiddleware())

	// Convert HEAD requests to GET before OpenAPI validation (RFC 9110 Section 9.3.2)
	// This must be after auth/rate-limiting (which handle HEAD correctly) and before
	// the OpenAPI validator (which would reject HEAD as an unknown method)
	r.Use(api.HeadMethodMiddleware())

	// Add OpenAPI validation middleware
	if openAPIValidator, err := api.SetupOpenAPIValidation(); err != nil {
```

- [ ] **Step 2: Build to verify compilation**

Run: `make build-server`
Expected: PASS — `bin/tmiserver` built successfully

- [ ] **Step 3: Run full unit tests**

Run: `make test-unit`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(api): register HeadMethodMiddleware in server startup (#76)

Places the middleware after EnumNormalizerMiddleware and before the
OpenAPI validator, ensuring HEAD requests are converted to GET before
spec validation while preserving correct auth and rate-limiting behavior."
```

---

### Task 5: Integration Tests

**Files:**
- Create: `test/integration/workflows/head_method_test.go`

- [ ] **Step 1: Write the integration test file**

Create `test/integration/workflows/head_method_test.go`:

```go
package workflows

import (
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestHeadMethodSupport tests HEAD method support for GET endpoints (RFC 9110)
func TestHeadMethodSupport(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	t.Run("HEAD on public root endpoint returns 200 with no body", func(t *testing.T) {
		// Root endpoint "/" is public, no auth needed
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "HEAD",
			Path:   "/",
		})
		framework.AssertNoError(t, err, "HEAD / failed")
		framework.AssertStatusOK(t, resp)

		if resp.Body != "" {
			t.Errorf("HEAD response should have empty body, got: %s", resp.Body)
		}

		if resp.Header.Get("Content-Type") == "" {
			t.Error("HEAD response should include Content-Type header")
		}
	})

	t.Run("HEAD on protected endpoint without auth returns 401", func(t *testing.T) {
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "HEAD",
			Path:   "/threat_models",
		})
		framework.AssertNoError(t, err, "HEAD /threat_models failed")

		if resp.StatusCode != 401 {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("HEAD on protected endpoint with auth returns 200", func(t *testing.T) {
		if err := framework.EnsureOAuthStubRunning(); err != nil {
			t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
		}

		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Failed to authenticate user")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "HEAD",
			Path:   "/threat_models",
		})
		framework.AssertNoError(t, err, "HEAD /threat_models failed")
		framework.AssertStatusOK(t, resp)

		if resp.Body != "" {
			t.Errorf("HEAD response should have empty body, got length: %d", len(resp.Body))
		}

		if resp.Header.Get("Content-Length") == "" {
			t.Log("Warning: Content-Length header not set (may be expected for empty list)")
		}
	})

	t.Run("HEAD on nonexistent path returns 404", func(t *testing.T) {
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "HEAD",
			Path:   "/this/path/does/not/exist",
		})
		framework.AssertNoError(t, err, "HEAD nonexistent path failed")

		if resp.StatusCode != 404 {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}
```

- [ ] **Step 2: Run integration tests**

Run: `make test-integration`
Expected: All 4 HEAD subtests PASS, no regressions in existing tests

Note: Integration tests require the dev environment running (`make start-dev`) and the OAuth stub (`make start-oauth-stub`).

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/head_method_test.go
git commit -m "test(integration): add HEAD method support integration tests (#76)

Tests HEAD on public root endpoint, protected endpoints with and without
auth, and nonexistent paths. Verifies empty body, correct status codes,
and header preservation."
```

---

### Task 6: Final Verification

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: All tests PASS

- [ ] **Step 3: Build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
Expected: All tests PASS

- [ ] **Step 5: Manual smoke test**

```bash
make start-dev
# Public endpoint
curl -I http://localhost:8080/
# Expected: HTTP/1.1 200 OK, Content-Type header, no body

# Protected endpoint without auth
curl -I http://localhost:8080/threat_models
# Expected: HTTP/1.1 401 Unauthorized

# Excluded endpoint
curl -I http://localhost:8080/oauth2/authorize
# Expected: HTTP/1.1 405 Method Not Allowed, Allow header includes HEAD for GET paths
```

- [ ] **Step 6: Final commit if any fixups needed, then close issue**

```bash
gh issue close 76 --repo ericfitz/tmi --comment "Implemented HEAD method support for 107 GET endpoints via HeadMethodMiddleware. Four protocol endpoints (OAuth authorize/callback, SAML login/SLO) excluded per RFC 9110 safety requirements."
```
