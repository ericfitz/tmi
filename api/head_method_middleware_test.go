package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestMatchesExcludedPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"exact match /oauth2/authorize", "/oauth2/authorize", true},
		{"exact match /oauth2/callback", "/oauth2/callback", true},
		{"exact match /saml/slo", "/saml/slo", true},
		{"wildcard match /saml/okta/login", "/saml/okta/login", true},
		{"wildcard match /saml/azure-ad/login", "/saml/azure-ad/login", true},
		{"non-excluded path /threat_models", "/threat_models", false},
		{"non-excluded path /", "/", false},
		{"non-excluded path /oauth2/providers", "/oauth2/providers", false},
		{"non-excluded path /saml/providers", "/saml/providers", false},
		{"partial match not excluded /oauth2/authorize/extra", "/oauth2/authorize/extra", false},
		{"shorter path not excluded /oauth2", "/oauth2", false},
		{"empty path not excluded", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isExcludedFromHead(tt.path)
			assert.Equal(t, tt.expected, result, "isExcludedFromHead(%q)", tt.path)
		})
	}
}

// registerRoute registers both GET and HEAD handlers for a path, which mirrors
// how the real application works: HEAD routes must be registered alongside GET
// routes because Gin does not automatically serve HEAD from GET routes. The
// HeadMethodMiddleware then handles body suppression and method conversion.
func registerRoute(router *gin.Engine, path string, handler gin.HandlerFunc) {
	router.GET(path, handler)
	router.HEAD(path, handler)
}

func TestHeadMethodMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("HEAD returns 200 with empty body and correct Content-Length", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		registerRoute(router, "/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"key": "value"})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String(), "HEAD response body should be empty")
		assert.NotEmpty(t, w.Header().Get("Content-Length"), "Content-Length header should be set")
	})

	t.Run("GET passes through with body", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"key": "value"})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotEmpty(t, w.Body.String(), "GET response should have a body")
	})

	t.Run("POST passes through unmodified", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusCreated, gin.H{"created": true})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NotEmpty(t, w.Body.String(), "POST response should have a body")
	})

	t.Run("HEAD on excluded /oauth2/authorize passes through as HEAD", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())

		var capturedMethod string
		router.HEAD("/oauth2/authorize", func(c *gin.Context) {
			capturedMethod = c.Request.Method
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/oauth2/authorize", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.MethodHead, capturedMethod, "excluded path should keep HEAD method")
	})

	t.Run("HEAD on excluded /saml/:provider/login wildcard passes through as HEAD", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())

		var capturedMethod string
		router.HEAD("/saml/:provider/login", func(c *gin.Context) {
			capturedMethod = c.Request.Method
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/saml/okta/login", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.MethodHead, capturedMethod, "excluded wildcard path should keep HEAD method")
	})

	t.Run("HEAD preserves error status codes", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		registerRoute(router, "/notfound", func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/notfound", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Empty(t, w.Body.String(), "HEAD response body should be empty even for errors")
	})

	t.Run("HEAD preserves custom response headers", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		registerRoute(router, "/custom-headers", func(c *gin.Context) {
			c.Header("X-Custom-Header", "custom-value")
			c.Header("X-Request-Id", "abc-123")
			c.JSON(http.StatusOK, gin.H{"key": "value"})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/custom-headers", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "custom-value", w.Header().Get("X-Custom-Header"))
		assert.Equal(t, "abc-123", w.Header().Get("X-Request-Id"))
		assert.Empty(t, w.Body.String(), "HEAD response body should be empty")
	})

	t.Run("HEAD sets Content-Length when handler does not set it explicitly", func(t *testing.T) {
		router := gin.New()
		router.Use(HeadMethodMiddleware())
		registerRoute(router, "/no-content-length", func(c *gin.Context) {
			// Write body without explicitly setting Content-Length
			body := `{"message":"hello world"}`
			c.Writer.Header().Set("Content-Type", "application/json")
			c.Writer.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(c.Writer, body)
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodHead, "/no-content-length", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String(), "HEAD response body should be empty")
		assert.Equal(t, "25", w.Header().Get("Content-Length"),
			"Content-Length should be set to the byte count of the suppressed body")
	})
}

func TestGetAllowedMethodsForPathIncludesHead(t *testing.T) {
	// getAllowedMethodsForPath reads from the embedded OpenAPI spec.
	// The root path "/" has a GET operation, so HEAD should be included.
	methods := getAllowedMethodsForPath("/")
	assert.Contains(t, methods, "HEAD")
	assert.Contains(t, methods, "GET")
}
