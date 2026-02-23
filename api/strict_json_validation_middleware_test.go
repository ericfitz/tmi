package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// testFormBody is a sample form-encoded body used to test non-JSON content type bypass
const testFormBody = "key=value"

func TestStrictJSONValidationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("valid JSON passes", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test", "value": 123}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("GET requests bypass validation", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("DELETE requests bypass validation", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.DELETE("/test", func(c *gin.Context) {
			c.JSON(http.StatusNoContent, nil)
		})

		req := httptest.NewRequest("DELETE", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("non-JSON content-type bypasses validation", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := testFormBody
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("empty body allowed", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(""))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("trailing garbage rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test"}garbage`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_input")
		assert.Contains(t, w.Body.String(), "unexpected content")
	})

	t.Run("trailing whitespace allowed", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test"}   ` + "\n\t"
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("multiple JSON objects rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test"}{"another": "object"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("duplicate keys rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "first", "name": "second"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "duplicate key")
	})

	t.Run("duplicate keys in nested object rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"outer": {"inner": "first", "inner": "second"}}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "duplicate key")
	})

	t.Run("invalid JSON syntax rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test",}` // Trailing comma
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON syntax")
	})

	t.Run("unclosed object rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test"`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("deeply nested objects handled", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"a": {"b": {"c": {"d": {"e": "value"}}}}}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("arrays within objects handled", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"items": [{"name": "one"}, {"name": "two"}]}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("duplicate keys in array elements rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"items": [{"name": "test", "name": "duplicate"}]}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "duplicate key")
	})

	t.Run("valid JSON array passes", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `[{"op": "replace", "path": "/name", "value": "new"}]`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("primitive JSON values pass", func(t *testing.T) {
		router := gin.New()
		router.Use(StrictJSONValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		testCases := []string{
			`"string value"`,
			`123`,
			`true`,
			`false`,
			`null`,
		}

		for _, body := range testCases {
			req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code, "Body: %s", body)
		}
	})
}

func TestValidateNoDuplicateKeys(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		expectErr bool
	}{
		{
			name:      "simple object no duplicates",
			json:      `{"a": 1, "b": 2, "c": 3}`,
			expectErr: false,
		},
		{
			name:      "simple object with duplicate",
			json:      `{"a": 1, "a": 2}`,
			expectErr: true,
		},
		{
			name:      "nested object no duplicates",
			json:      `{"outer": {"inner": 1}}`,
			expectErr: false,
		},
		{
			name:      "nested object with duplicate",
			json:      `{"outer": {"inner": 1, "inner": 2}}`,
			expectErr: true,
		},
		{
			name:      "array of objects no duplicates",
			json:      `[{"a": 1}, {"b": 2}]`,
			expectErr: false,
		},
		{
			name:      "array of objects with duplicate in element",
			json:      `[{"a": 1, "a": 2}]`,
			expectErr: true,
		},
		{
			name:      "deeply nested no duplicates",
			json:      `{"a": {"b": {"c": {"d": 1}}}}`,
			expectErr: false,
		},
		{
			name:      "deeply nested with duplicate",
			json:      `{"a": {"b": {"c": {"d": 1, "d": 2}}}}`,
			expectErr: true,
		},
		{
			name:      "empty object",
			json:      `{}`,
			expectErr: false,
		},
		{
			name:      "empty array",
			json:      `[]`,
			expectErr: false,
		},
		{
			name:      "primitive string",
			json:      `"string"`,
			expectErr: false,
		},
		{
			name:      "primitive number",
			json:      `123`,
			expectErr: false,
		},
		{
			name:      "null value",
			json:      `null`,
			expectErr: false,
		},
		{
			name:      "same key at different levels",
			json:      `{"name": "outer", "child": {"name": "inner"}}`,
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNoDuplicateKeys([]byte(tc.json))
			if tc.expectErr {
				assert.Error(t, err, "Expected error for: %s", tc.json)
				if err != nil {
					assert.Contains(t, err.Error(), "duplicate key")
				}
			} else {
				assert.NoError(t, err, "Did not expect error for: %s", tc.json)
			}
		})
	}
}

func TestBoundaryValueValidationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("valid JSON passes", func(t *testing.T) {
		router := gin.New()
		router.Use(BoundaryValueValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test", "value": 123}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("GET requests bypass validation", func(t *testing.T) {
		router := gin.New()
		router.Use(BoundaryValueValidationMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("non-JSON content bypasses validation", func(t *testing.T) {
		router := gin.New()
		router.Use(BoundaryValueValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := testFormBody
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("null values pass to handler", func(t *testing.T) {
		router := gin.New()
		router.Use(BoundaryValueValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": null}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("empty strings pass to handler", func(t *testing.T) {
		router := gin.New()
		router.Use(BoundaryValueValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": ""}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid JSON passes to handler", func(t *testing.T) {
		// BoundaryValueValidationMiddleware doesn't reject invalid JSON
		// It lets OpenAPI validation handle it
		router := gin.New()
		router.Use(BoundaryValueValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `not json`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Middleware passes through, handler still runs
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
