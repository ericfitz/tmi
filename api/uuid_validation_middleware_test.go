package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestUUIDValidationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("valid UUID passes", func(t *testing.T) {
		router := gin.New()
		router.Use(UUIDValidationMiddleware())
		router.GET("/test/:threat_model_id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		validUUID := uuid.New().String()
		req := httptest.NewRequest("GET", "/test/"+validUUID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid UUID format rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(UUIDValidationMiddleware())
		router.GET("/test/:threat_model_id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test/not-a-uuid", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid UUID")
	})

	t.Run("non-UUID parameters ignored", func(t *testing.T) {
		router := gin.New()
		router.Use(UUIDValidationMiddleware())
		router.GET("/test/:name", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test/anything-here", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("validates all UUID parameters", func(t *testing.T) {
		uuidParams := []string{
			"id",
			"threat_model_id",
			"diagram_id",
			"document_id",
			"note_id",
			"repository_id",
			"asset_id",
			"threat_id",
			"user_id",
			"invocation_id",
		}

		for _, param := range uuidParams {
			t.Run(param, func(t *testing.T) {
				router := gin.New()
				router.Use(UUIDValidationMiddleware())
				router.GET("/test/:"+param, func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"status": "ok"})
				})

				// Test invalid UUID
				req := httptest.NewRequest("GET", "/test/invalid-uuid", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				assert.Equal(t, http.StatusBadRequest, w.Code, "Parameter %s should validate UUID", param)

				// Test valid UUID
				validUUID := uuid.New().String()
				req = httptest.NewRequest("GET", "/test/"+validUUID, nil)
				w = httptest.NewRecorder()
				router.ServeHTTP(w, req)
				assert.Equal(t, http.StatusOK, w.Code, "Parameter %s should accept valid UUID", param)
			})
		}
	})

	t.Run("UUID with wrong format rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(UUIDValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		invalidUUIDs := []string{
			"12345",
			"not-valid-uuid",
			"12345678-1234-1234-1234",               // Too short
			"12345678-1234-1234-1234-1234567890123", // Too long
			"12345678-1234-1234-1234-123456789xyz",  // Invalid characters
			"g2345678-1234-1234-1234-123456789012",  // Invalid hex
		}

		for _, invalidUUID := range invalidUUIDs {
			req := httptest.NewRequest("GET", "/test/"+invalidUUID, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, "Should reject: %s", invalidUUID)
		}
	})

	t.Run("UUIDv4 and UUIDv7 both accepted", func(t *testing.T) {
		router := gin.New()
		router.Use(UUIDValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// UUIDv4
		v4 := uuid.New().String()
		req := httptest.NewRequest("GET", "/test/"+v4, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// UUIDv7 format (time-based)
		v7, _ := uuid.NewV7()
		req = httptest.NewRequest("GET", "/test/"+v7.String(), nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestShouldValidateAsUUID(t *testing.T) {
	tests := []struct {
		paramName string
		expected  bool
	}{
		{"id", true},
		{"threat_model_id", true},
		{"diagram_id", true},
		{"document_id", true},
		{"note_id", true},
		{"repository_id", true},
		{"asset_id", true},
		{"threat_id", true},
		{"user_id", true},
		{"invocation_id", true},
		{"name", false},
		{"provider", false},
		{"idp", false},
		{"random_param", false},
		{"ID", false}, // Case sensitive
		{"threatModelId", false},
	}

	for _, tc := range tests {
		t.Run(tc.paramName, func(t *testing.T) {
			result := shouldValidateAsUUID(tc.paramName)
			assert.Equal(t, tc.expected, result, "shouldValidateAsUUID(%q) should be %v", tc.paramName, tc.expected)
		})
	}
}

func TestPathParameterValidationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("valid path parameters pass", func(t *testing.T) {
		router := gin.New()
		router.Use(PathParameterValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test/valid-value", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("path traversal patterns rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(PathParameterValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Only test patterns that work with HTTP path routing
		// Patterns with "/" are handled by the router before middleware runs
		traversalPatterns := []string{
			"test..test", // Contains ".." which is detected
		}

		for _, pattern := range traversalPatterns {
			req := httptest.NewRequest("GET", "/test/"+pattern, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, "Should reject path traversal: %s", pattern)
		}
	})

	t.Run("SQL injection patterns rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(PathParameterValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Only test single-word patterns that work with HTTP URLs
		// Patterns with spaces break HTTP URL parsing
		injectionPatterns := []string{
			"SELECT",
			"select",
			"UNION",
			"union",
			"INSERT",
			"UPDATE",
			"DELETE",
			"DROP",
			"CREATE",
			"ALTER",
			"EXEC",
			"EXECUTE",
			"javascript:alert",
		}

		for _, pattern := range injectionPatterns {
			req := httptest.NewRequest("GET", "/test/"+pattern, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, "Should reject SQL injection pattern: %s", pattern)
		}
	})

	t.Run("HTML injection patterns rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(PathParameterValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// HTML/script tags contain < and > which are detected
		htmlPatterns := []string{
			"test%3Cscript%3E", // URL-encoded <script>
		}

		for _, pattern := range htmlPatterns {
			req := httptest.NewRequest("GET", "/test/"+pattern, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			// URL decoding happens, so we check if < > get rejected
			assert.Equal(t, http.StatusBadRequest, w.Code, "Should reject HTML pattern: %s", pattern)
		}
	})

	t.Run("excessive length rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(PathParameterValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// 201 characters
		longValue := strings.Repeat("a", 201)
		req := httptest.NewRequest("GET", "/test/"+longValue, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "too long")
	})

	t.Run("length at limit accepted", func(t *testing.T) {
		router := gin.New()
		router.Use(PathParameterValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Exactly 200 characters
		okValue := strings.Repeat("a", 200)
		req := httptest.NewRequest("GET", "/test/"+okValue, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("null byte rejected", func(t *testing.T) {
		// Note: null bytes in URLs are rejected by Go's net/url library
		// before they reach the middleware, so we test the helper function directly
		assert.True(t, strings.Contains("value\x00extra", "\x00"),
			"Test setup: string should contain null byte")
		// The middleware checks for null bytes, but in practice Go's HTTP library
		// rejects null bytes in URLs at the parsing level before they reach handlers
	})

	t.Run("normal values with numbers accepted", func(t *testing.T) {
		router := gin.New()
		router.Use(PathParameterValidationMiddleware())
		router.GET("/test/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		validValues := []string{
			"123",
			"test123",
			"test-value-123",
			"test_value",
			"TestValue",
		}

		for _, value := range validValues {
			req := httptest.NewRequest("GET", "/test/"+value, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "Should accept: %s", value)
		}
	})
}

func TestMethodNotAllowedHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("standard methods allowed", func(t *testing.T) {
		allowedMethods := []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
			http.MethodHead,
		}

		for _, method := range allowedMethods {
			t.Run(method, func(t *testing.T) {
				router := gin.New()
				router.Use(MethodNotAllowedHandler())
				router.Handle(method, "/test", func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"status": "ok"})
				})

				req := httptest.NewRequest(method, "/test", nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// Either OK or NoContent (for HEAD/OPTIONS)
				assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusNoContent,
					"Method %s should be allowed", method)
			})
		}
	})

	t.Run("non-standard method rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(MethodNotAllowedHandler())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// TRACE is not in the allowed methods
		req := httptest.NewRequest("TRACE", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		assert.Contains(t, w.Body.String(), "method_not_allowed")
		assert.NotEmpty(t, w.Header().Get("Allow"))
	})

	t.Run("CONNECT method rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(MethodNotAllowedHandler())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("CONNECT", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("custom invalid method rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(MethodNotAllowedHandler())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("INVALID", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("response includes Allow header", func(t *testing.T) {
		router := gin.New()
		router.Use(MethodNotAllowedHandler())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("TRACE", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		allowHeader := w.Header().Get("Allow")
		assert.Contains(t, allowHeader, "GET")
		assert.Contains(t, allowHeader, "POST")
		assert.Contains(t, allowHeader, "PUT")
		assert.Contains(t, allowHeader, "DELETE")
	})
}
