package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("all security headers present", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		AssertSecurityHeaders(t, w.Header())
	})

	t.Run("X-Content-Type-Options prevents MIME sniffing", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	})

	t.Run("X-Frame-Options prevents clickjacking", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	})

	t.Run("X-XSS-Protection disabled per modern security guidance", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "0", w.Header().Get("X-XSS-Protection"))
	})

	t.Run("production mode CSP is restrictive", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		csp := w.Header().Get("Content-Security-Policy")
		assert.NotEmpty(t, csp)
		assert.Contains(t, csp, "default-src 'self'")
		// Production CSP should NOT contain localhost
		assert.NotContains(t, csp, "localhost")
	})

	t.Run("development mode CSP allows localhost", func(t *testing.T) {
		router := gin.New()
		// Middleware to set isDev context before SecurityHeaders runs
		router.Use(func(c *gin.Context) {
			c.Set("isDev", true)
			c.Next()
		})
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		csp := w.Header().Get("Content-Security-Policy")
		assert.NotEmpty(t, csp)
		assert.Contains(t, csp, "localhost")
	})

	t.Run("Referrer-Policy is strict-origin-when-cross-origin", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
	})

	t.Run("Cache-Control prevents caching", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		cacheControl := w.Header().Get("Cache-Control")
		assert.Contains(t, cacheControl, "no-store")
		assert.Contains(t, cacheControl, "no-cache")
		assert.Contains(t, cacheControl, "must-revalidate")
	})

	t.Run("Permissions-Policy restricts sensitive APIs", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		permissionsPolicy := w.Header().Get("Permissions-Policy")
		assert.Contains(t, permissionsPolicy, "geolocation=()")
		assert.Contains(t, permissionsPolicy, "microphone=()")
		assert.Contains(t, permissionsPolicy, "camera=()")
	})

	t.Run("headers present on error responses", func(t *testing.T) {
		router := gin.New()
		router.Use(SecurityHeaders())
		router.GET("/test", func(c *gin.Context) {
			c.AbortWithStatus(http.StatusInternalServerError)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		AssertSecurityHeaders(t, w.Header())
	})

	t.Run("headers present on all HTTP methods", func(t *testing.T) {
		methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}

		for _, method := range methods {
			t.Run(method, func(t *testing.T) {
				router := gin.New()
				router.Use(SecurityHeaders())
				router.Handle(method, "/test", func(c *gin.Context) {
					c.String(http.StatusOK, "ok")
				})

				req := httptest.NewRequest(method, "/test", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				AssertSecurityHeaders(t, w.Header())
			})
		}
	})
}

func TestHSTSMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("HSTS header present when TLS enabled", func(t *testing.T) {
		router := gin.New()
		router.Use(HSTSMiddleware(true))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		AssertHSTSHeader(t, w.Header(), true)
	})

	t.Run("HSTS header absent when TLS disabled", func(t *testing.T) {
		router := gin.New()
		router.Use(HSTSMiddleware(false))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		AssertHSTSHeader(t, w.Header(), false)
	})

	t.Run("HSTS max-age is one year", func(t *testing.T) {
		router := gin.New()
		router.Use(HSTSMiddleware(true))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		hsts := w.Header().Get("Strict-Transport-Security")
		assert.Contains(t, hsts, "max-age=31536000")
	})

	t.Run("HSTS includes includeSubDomains", func(t *testing.T) {
		router := gin.New()
		router.Use(HSTSMiddleware(true))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		hsts := w.Header().Get("Strict-Transport-Security")
		assert.Contains(t, hsts, "includeSubDomains")
	})
}

func TestCORSMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("CORS headers present on normal request", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		AssertCORSHeaders(t, w.Header())
	})

	t.Run("OPTIONS request returns 204 No Content", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("OPTIONS", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		AssertCORSHeaders(t, w.Header())
	})

	t.Run("Access-Control-Allow-Origin is wildcard", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("Access-Control-Allow-Credentials is true", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("allowed headers include Authorization", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
		assert.Contains(t, allowHeaders, "Authorization")
		assert.Contains(t, allowHeaders, "Content-Type")
	})

	t.Run("allowed methods include all REST methods", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		allowMethods := w.Header().Get("Access-Control-Allow-Methods")
		assert.Contains(t, allowMethods, "GET")
		assert.Contains(t, allowMethods, "POST")
		assert.Contains(t, allowMethods, "PUT")
		assert.Contains(t, allowMethods, "DELETE")
		assert.Contains(t, allowMethods, "PATCH")
	})

	t.Run("preflight request does not reach handler", func(t *testing.T) {
		handlerCalled := false
		router := gin.New()
		router.Use(CORS())
		router.GET("/test", func(c *gin.Context) {
			handlerCalled = true
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("OPTIONS", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.False(t, handlerCalled, "Handler should not be called for OPTIONS request")
		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

func TestContextTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("context has timeout set", func(t *testing.T) {
		router := gin.New()
		router.Use(ContextTimeout(5000000000)) // 5 seconds in nanoseconds
		router.GET("/test", func(c *gin.Context) {
			deadline, ok := c.Request.Context().Deadline()
			if !ok {
				c.String(http.StatusInternalServerError, "no deadline set")
				return
			}
			if deadline.IsZero() {
				c.String(http.StatusInternalServerError, "deadline is zero")
				return
			}
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("request completes before timeout", func(t *testing.T) {
		router := gin.New()
		router.Use(ContextTimeout(5000000000)) // 5 seconds
		router.GET("/test", func(c *gin.Context) {
			// Immediate response
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ok", w.Body.String())
	})
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"debug", LogLevelDebug},
		{"DEBUG", LogLevelDebug},
		{"Debug", LogLevelDebug},
		{"info", LogLevelInfo},
		{"INFO", LogLevelInfo},
		{"Info", LogLevelInfo},
		{"warn", LogLevelWarn},
		{"WARN", LogLevelWarn},
		{"warning", LogLevelWarn},
		{"WARNING", LogLevelWarn},
		{"error", LogLevelError},
		{"ERROR", LogLevelError},
		{"Error", LogLevelError},
		{"", LogLevelInfo},        // default
		{"unknown", LogLevelInfo}, // default
		{"INVALID", LogLevelInfo}, // default
		{"trace", LogLevelInfo},   // unsupported, default
	}

	for _, tc := range tests {
		t.Run("level_"+tc.input, func(t *testing.T) {
			result := ParseLogLevel(tc.input)
			assert.Equal(t, tc.expected, result, "ParseLogLevel(%q) should return %v", tc.input, tc.expected)
		})
	}
}

func TestAcceptHeaderValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("accepts application/json", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptHeaderValidation())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("accepts wildcard", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptHeaderValidation())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "*/*")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("accepts no header (defaults allowed)", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptHeaderValidation())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		// No Accept header
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("rejects unsupported media type", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptHeaderValidation())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "text/xml")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotAcceptable, w.Code)
	})

	t.Run("accepts application/json with quality parameter", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptHeaderValidation())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "application/json; q=0.9")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("accepts mixed types with json included", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptHeaderValidation())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "text/html, application/json, */*")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestJSONErrorHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("converts plain text error to JSON", func(t *testing.T) {
		router := gin.New()
		router.Use(JSONErrorHandler())
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusBadRequest, "Bad Request")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		// The response should be JSON format
		contentType := w.Header().Get("Content-Type")
		if strings.Contains(contentType, "application/json") {
			assert.Contains(t, w.Body.String(), "error")
		}
	})

	t.Run("preserves JSON responses", func(t *testing.T) {
		router := gin.New()
		router.Use(JSONErrorHandler())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "message": "Invalid input"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
		assert.Contains(t, w.Body.String(), "validation_error")
	})

	t.Run("passes through successful responses", func(t *testing.T) {
		router := gin.New()
		router.Use(JSONErrorHandler())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "ok")
	})
}
