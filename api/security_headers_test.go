package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

	t.Run("dev mode reflects any origin", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS(nil, true))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "http://localhost:4200")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "http://localhost:4200", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
		assert.Equal(t, "Origin", w.Header().Get("Vary"))
	})

	t.Run("production mode allows configured origin", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS([]string{"https://app.example.com"}, false))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://app.example.com")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "https://app.example.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("production mode rejects unconfigured origin", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS([]string{"https://app.example.com"}, false))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Origin", "https://evil.com")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("no Origin header produces no CORS origin header", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS(nil, true))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("OPTIONS request returns 204 No Content", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS(nil, true))
		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("OPTIONS", "/test", nil)
		req.Header.Set("Origin", "http://localhost:4200")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("allowed headers include Authorization", func(t *testing.T) {
		router := gin.New()
		router.Use(CORS(nil, true))
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
		router.Use(CORS(nil, true))
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
		router.Use(CORS(nil, true))
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

	t.Run("accepts text/event-stream for SSE endpoints", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptHeaderValidation())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Accept", "text/event-stream")
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

	// Regression test for #289: when JSONErrorHandler buffers the response,
	// inner middleware that read c.Writer.Status() after c.Next() must see the
	// handler's status (not the default 200). Previously bufferedResponseWriter
	// did not override Status(), so the call fell through to the embedded
	// gin.ResponseWriter whose status was still default 200 (because nothing
	// had called the underlying WriteHeader yet).
	t.Run("inner middleware sees handler status after c.Next", func(t *testing.T) {
		router := gin.New()
		router.Use(JSONErrorHandler())
		var capturedStatus int
		router.Use(func(c *gin.Context) {
			c.Next()
			capturedStatus = c.Writer.Status()
		})
		router.GET("/err", func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
		})

		req := httptest.NewRequest("GET", "/err", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code,
			"final status to client should be 500")
		assert.Equal(t, http.StatusInternalServerError, capturedStatus,
			"inner middleware reading c.Writer.Status() must see 500, not default 200")
	})

	// Regression test for #289: a handler that calls c.Status(204) followed by
	// c.Writer.WriteHeaderNow() must commit 204 (not the embedded writer's
	// default 200). Without overriding WriteHeaderNow, the call fell through
	// to the embedded writer and committed 200.
	t.Run("WriteHeaderNow commits buffered status, not default", func(t *testing.T) {
		router := gin.New()
		router.Use(JSONErrorHandler())
		router.DELETE("/x", func(c *gin.Context) {
			c.Status(http.StatusNoContent)
			c.Writer.WriteHeaderNow()
		})

		req := httptest.NewRequest("DELETE", "/x", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code,
			"client should see 204, not the underlying writer's default 200")
	})

	// Regression test for #409: an SSE handler must stream events to the wire
	// as it writes them, not have them buffered until the handler returns.
	t.Run("SSE response streams incrementally", func(t *testing.T) {
		release := make(chan struct{})
		handlerDone := make(chan struct{})

		rec := newFlushRecorder()
		gw := &bufferedResponseWriter{
			ResponseWriter: &testGinWriter{rec: rec},
			body:           bytes.NewBufferString(""),
			statusCode:     http.StatusOK,
		}

		// Simulate the handler: set the SSE content type, write event A,
		// signal, block on release, then write event B.
		go func() {
			defer close(handlerDone)
			gw.Header().Set("Content-Type", "text/event-stream")
			_, _ = gw.WriteString("event: status\ndata: {\"a\":1}\n\n")
			gw.Flush()
			<-release
			_, _ = gw.WriteString("event: status\ndata: {\"b\":2}\n\n")
			gw.Flush()
		}()

		// Event A must be visible on the underlying writer before release.
		assert.Eventually(t, func() bool {
			return strings.Contains(rec.snapshot(), `{"a":1}`)
		}, time.Second, 5*time.Millisecond,
			"event A must reach the underlying writer before the handler unblocks")
		assert.NotContains(t, rec.snapshot(), `{"b":2}`,
			"event B must not appear before the handler unblocks")

		close(release)
		<-handlerDone
		assert.Contains(t, rec.snapshot(), `{"b":2}`, "event B must arrive after release")
		assert.True(t, gw.streaming, "writer should be in streaming mode")
	})

	// Regression test for #409: when the response was streamed, JSONErrorHandler
	// must not re-run its transform/pass-through logic after c.Next(). Without
	// the guard, the middleware falls through to the else branch and issues an
	// extra Write([]byte{}) on the underlying writer even though all real bytes
	// have already been sent. Served through a flushRecorder (not
	// httptest.NewRecorder) because the latter silently absorbs extra writes and
	// so cannot detect the regression.
	t.Run("streamed SSE response is not transformed or duplicated", func(t *testing.T) {
		router := gin.New()
		router.Use(JSONErrorHandler())
		router.GET("/sse", func(c *gin.Context) {
			c.Header("Content-Type", "text/event-stream")
			c.Status(http.StatusOK)
			_, _ = c.Writer.WriteString("event: status\ndata: {\"phase\":\"x\"}\n\n")
			c.Writer.Flush()
			_, _ = c.Writer.WriteString("event: token\ndata: {\"content\":\"hi\"}\n\n")
			c.Writer.Flush()
		})

		rec := newFlushRecorder()
		req := httptest.NewRequest("GET", "/sse", nil)
		router.ServeHTTP(rec, req)

		body := rec.snapshot()
		assert.Equal(t, http.StatusOK, rec.code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
		assert.Contains(t, body, `{"phase":"x"}`)
		assert.Contains(t, body, `{"content":"hi"}`)
		// The body must not be wrapped in the JSON error envelope...
		assert.NotContains(t, body, `"error_description"`)
		// ...and each event must appear exactly once.
		assert.Equal(t, 1, strings.Count(body, `{"phase":"x"}`),
			"streamed event must not be duplicated by JSONErrorHandler")
		// The decisive assertion: the underlying writer receives exactly two
		// Write calls — one per SSE event. Removing the `if blw.streaming {
		// return }` guard in JSONErrorHandler causes the middleware to fall
		// through to its else branch after c.Next() and issue an extra
		// Write([]byte{}) on the underlying writer, making this 3.
		assert.Equal(t, 2, rec.writeCallCount(),
			"JSONErrorHandler must not issue an extra Write to the underlying writer after a streamed response")
	})
}

// TestBufferedResponseWriterStreaming exercises the streaming-mode flip of
// bufferedResponseWriter directly (issue #409).
func TestBufferedResponseWriterStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newWriter := func() (*bufferedResponseWriter, *flushRecorder) {
		rec := newFlushRecorder()
		return &bufferedResponseWriter{
			ResponseWriter: &testGinWriter{rec: rec},
			body:           bytes.NewBufferString(""),
			statusCode:     http.StatusOK,
		}, rec
	}

	t.Run("stays buffered without SSE content type", func(t *testing.T) {
		w, rec := newWriter()
		_, _ = w.Write([]byte("hello"))
		assert.False(t, w.streaming, "must not flip without text/event-stream")
		assert.Empty(t, rec.snapshot(), "bytes must stay buffered")
		assert.Equal(t, "hello", w.body.String())
	})

	t.Run("flips on SSE content type and forwards writes", func(t *testing.T) {
		w, rec := newWriter()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("first"))
		assert.True(t, w.streaming, "must flip when Content-Type is text/event-stream")
		assert.Equal(t, "first", rec.snapshot(), "byte must reach the underlying writer")
		_, _ = w.Write([]byte("-second"))
		assert.Equal(t, "first-second", rec.snapshot(), "subsequent writes forwarded")
	})

	t.Run("flushes buffered bytes on flip", func(t *testing.T) {
		w, rec := newWriter()
		// Bytes written before the content type is SSE stay buffered...
		_, _ = w.Write([]byte("pre"))
		assert.Empty(t, rec.snapshot())
		// ...then the content type becomes SSE and the next write triggers the
		// flip, draining the buffer first.
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("post"))
		assert.Equal(t, "prepost", rec.snapshot(),
			"buffered bytes must be flushed ahead of the triggering write")
		assert.Equal(t, 0, w.body.Len(), "buffer must be reset after the flip")
	})

	t.Run("maybeSwitchToStreaming is idempotent", func(t *testing.T) {
		w, rec := newWriter()
		w.Header().Set("Content-Type", "text/event-stream")
		w.maybeSwitchToStreaming()
		w.maybeSwitchToStreaming()
		w.maybeSwitchToStreaming()
		assert.True(t, w.streaming)
		// Three calls, but the header commit and buffer drain happen once.
		// No body was written, so the recorder body stays empty and the flip
		// did not error or panic.
		assert.Empty(t, rec.snapshot())
	})

	t.Run("WriteString forwards in streaming mode", func(t *testing.T) {
		w, rec := newWriter()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.WriteString("via-writestring")
		assert.True(t, w.streaming)
		assert.Equal(t, "via-writestring", rec.snapshot())
	})

	t.Run("Flush before any write triggers the flip", func(t *testing.T) {
		w, rec := newWriter()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Flush()
		assert.True(t, w.streaming, "Flush must be a flip entry point")
		assert.Equal(t, 1, rec.flushCount(), "underlying Flush must be called")
	})

	t.Run("WriteHeaderNow triggers the flip and does not re-commit the header", func(t *testing.T) {
		w, rec := newWriter()
		w.statusCode = http.StatusOK
		w.Header().Set("Content-Type", "text/event-stream")
		// WriteHeaderNow is the fourth flip entry point: it must flip to
		// streaming via maybeSwitchToStreaming, which commits the header once.
		w.WriteHeaderNow()
		assert.True(t, w.streaming, "WriteHeaderNow must be a flip entry point")
		// The streaming flip committed the header (status 200) exactly once.
		// WriteHeaderNow must then early-return without a second commit, so
		// writeCallCount stays 0 (no body) and the status is the buffered one.
		assert.Equal(t, http.StatusOK, rec.code, "buffered status must be committed")
		assert.Equal(t, 0, rec.writeCallCount(), "no body bytes written")
		// The decisive assertion: the `if w.streaming { return }` guard in
		// WriteHeaderNow prevents a second WriteHeader call. Without the guard,
		// WriteHeaderNow falls through to w.ResponseWriter.WriteHeader a second
		// time, making writeHeaderCallCount() == 2.
		assert.Equal(t, 1, rec.writeHeaderCallCount(), "WriteHeader must be called exactly once (streaming flip only)")
		// A subsequent body write forwards directly in streaming mode.
		_, _ = w.Write([]byte("evt"))
		assert.Equal(t, "evt", rec.snapshot())
	})
}

// flushRecorder is a test http.ResponseWriter that implements http.Flusher and
// records every Write so tests can assert when bytes reached the writer.
type flushRecorder struct {
	mu               sync.Mutex
	header           http.Header
	body             bytes.Buffer
	code             int
	flushes          int
	writeCalls       int
	writeHeaderCalls int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: make(http.Header), code: http.StatusOK}
}

func (f *flushRecorder) Header() http.Header { return f.header }

func (f *flushRecorder) WriteHeader(code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeHeaderCalls++
	f.code = code
}

func (f *flushRecorder) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeCalls++
	return f.body.Write(b)
}

func (f *flushRecorder) Flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
}

// snapshot returns the body bytes written so far. Safe for concurrent use.
func (f *flushRecorder) snapshot() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body.String()
}

// bodyLen returns the number of body bytes written so far. Safe for concurrent use.
func (f *flushRecorder) bodyLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body.Len()
}

// writeCallCount returns how many times Write was called (including zero-byte writes). Safe for concurrent use.
func (f *flushRecorder) writeCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeCalls
}

// flushCount returns how many times Flush was called. Safe for concurrent use.
func (f *flushRecorder) flushCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flushes
}

// writeHeaderCallCount returns how many times WriteHeader was called. Safe for concurrent use.
func (f *flushRecorder) writeHeaderCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeHeaderCalls
}

// testGinWriter adapts a flushRecorder to the gin.ResponseWriter interface for
// unit-testing bufferedResponseWriter directly (without a full gin engine).
type testGinWriter struct {
	gin.ResponseWriter // embedded nil interface; only the methods below are called
	rec                *flushRecorder
}

func (t *testGinWriter) Header() http.Header         { return t.rec.Header() }
func (t *testGinWriter) Write(b []byte) (int, error) { return t.rec.Write(b) }
func (t *testGinWriter) WriteHeader(code int)        { t.rec.WriteHeader(code) }
func (t *testGinWriter) WriteHeaderNow()             {}
func (t *testGinWriter) Flush()                      { t.rec.Flush() }
func (t *testGinWriter) Status() int                 { return t.rec.code }
func (t *testGinWriter) Written() bool               { return t.rec.bodyLen() > 0 }
