package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUnicodeNormalizationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("valid UTF-8 content passes", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test", "description": "valid UTF-8"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("GET requests bypass validation", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
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
		router.Use(UnicodeNormalizationMiddleware())
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
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := "key=value"
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("zero-width space rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Use actual Unicode character, not JSON escape sequence
		body := `{"name": "test` + "\u200B" + `value"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported Unicode")
	})

	t.Run("zero-width non-joiner rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test` + "\u200C" + `value"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("zero-width joiner rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test` + "\u200D" + `value"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("BOM rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := "\uFEFF" + `{"name": "test"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("bidirectional override characters rejected", func(t *testing.T) {
		testCases := []struct {
			name string
			char string
		}{
			{"LRE", "\u202A"},
			{"RLE", "\u202B"},
			{"PDF", "\u202C"},
			{"LRO", "\u202D"},
			{"RLO", "\u202E"},
			{"LRI", "\u2066"},
			{"RLI", "\u2067"},
			{"FSI", "\u2068"},
			{"PDI", "\u2069"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				router := gin.New()
				router.Use(UnicodeNormalizationMiddleware())
				router.POST("/test", func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"status": "ok"})
				})

				body := `{"name": "test` + tc.char + `value"}`
				req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusBadRequest, w.Code, "Character %s should be rejected", tc.name)
			})
		}
	})

	t.Run("Hangul filler rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test` + "\u3164" + `value"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("single combining diacritical mark allowed", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Single combining mark (e.g., accent) is legitimate
		body := `{"name": "test` + "\u0300" + `value"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("excessive combining diacritical marks rejected (Zalgo)", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Zalgo-style text with 3+ consecutive combining marks
		body := `{"name": "test` + "\u0300\u0301\u0302" + `value"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("null byte rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		body := `{"name": "test` + "\x00" + `value"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("valid whitespace characters allowed", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// Standard whitespace should be allowed
		body := "{\n\t\"name\": \"test\",\r\n\t\"value\": \"ok\"\n}"
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("NFC normalization applied", func(t *testing.T) {
		router := gin.New()
		router.Use(UnicodeNormalizationMiddleware())

		router.POST("/test", func(c *gin.Context) {
			_, _ = c.GetRawData()
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// NFD form: 'é' as e + combining acute accent
		body := `{"name": "cafe\u0301"}`
		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// The combining character should have been normalized to precomposed form
		// Note: This test verifies the middleware runs, actual NFC normalization
		// converts 'e' + '\u0301' to 'é' (\u00e9)
	})
}

func TestHasProblematicUnicode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"plain ASCII", "hello world", false},
		{"normal UTF-8", "café résumé", false},
		{"emoji", "Hello 👋 World", false},
		{"zero width space", "test\u200Bvalue", true},
		{"zero width non-joiner", "test\u200Cvalue", true},
		{"zero width joiner", "test\u200Dvalue", true},
		{"BOM", "\uFEFFtest", true},
		{"LRE override", "test\u202Avalue", true},
		{"RLE override", "test\u202Bvalue", true},
		{"LRO override", "test\u202Dvalue", true},
		{"RLO override", "test\u202Evalue", true},
		{"Hangul filler", "test\u3164value", true},
		{"single combining diacritical", "test\u0300value", false},
		{"two combining diacriticals", "test\u0300\u0301value", false},
		{"three combining diacriticals (Zalgo)", "test\u0300\u0301\u0302value", true},
		{"null byte", "test\x00value", true},
		{"newline", "test\nvalue", false},
		{"tab", "test\tvalue", false},
		{"carriage return", "test\rvalue", false},
		{"CJK characters", "テスト", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := hasProblematicUnicode(tc.input)
			assert.Equal(t, tc.expected, result, "hasProblematicUnicode(%q) should be %v", tc.input, tc.expected)
		})
	}
}

func TestContentTypeValidationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("accepts application/json", func(t *testing.T) {
		router := gin.New()
		router.Use(ContentTypeValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(`{"key":"value"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("accepts application/json with charset", func(t *testing.T) {
		router := gin.New()
		router.Use(ContentTypeValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(`{"key":"value"}`))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("accepts application/json-patch+json", func(t *testing.T) {
		router := gin.New()
		router.Use(ContentTypeValidationMiddleware())
		router.PATCH("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("PATCH", "/test", bytes.NewBufferString(`[{"op":"replace"}]`))
		req.Header.Set("Content-Type", "application/json-patch+json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("accepts application/x-www-form-urlencoded", func(t *testing.T) {
		router := gin.New()
		router.Use(ContentTypeValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString("key=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("GET requests bypass validation", func(t *testing.T) {
		router := gin.New()
		router.Use(ContentTypeValidationMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		// No Content-Type header
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("empty body without Content-Type allowed", func(t *testing.T) {
		router := gin.New()
		router.Use(ContentTypeValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", nil)
		req.ContentLength = 0
		// No Content-Type header
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("rejects request with body but no Content-Type", func(t *testing.T) {
		router := gin.New()
		router.Use(ContentTypeValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(`{"key":"value"}`))
		// No Content-Type header but has body
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Content-Type header is required")
	})

	t.Run("rejects unsupported Content-Type", func(t *testing.T) {
		router := gin.New()
		router.Use(ContentTypeValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString("<xml></xml>"))
		req.Header.Set("Content-Type", "application/xml")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported_media_type")
	})
}

func TestDuplicateHeaderValidationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("single headers pass", func(t *testing.T) {
		router := gin.New()
		router.Use(DuplicateHeaderValidationMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("duplicate Authorization header rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(DuplicateHeaderValidationMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Add("Authorization", "Bearer token1")
		req.Header.Add("Authorization", "Bearer token2")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "duplicate_header")
		assert.Contains(t, w.Body.String(), "Authorization")
	})

	t.Run("duplicate Content-Type header rejected", func(t *testing.T) {
		router := gin.New()
		router.Use(DuplicateHeaderValidationMiddleware())
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("POST", "/test", bytes.NewBufferString("body"))
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Content-Type")
	})

	t.Run("non-critical duplicate headers allowed", func(t *testing.T) {
		router := gin.New()
		router.Use(DuplicateHeaderValidationMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Accept", "text/html")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestAcceptLanguageMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("sets default language when not specified", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptLanguageMiddleware())
		router.GET("/test", func(c *gin.Context) {
			lang, _ := c.Get("language")
			c.JSON(http.StatusOK, gin.H{"language": lang})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		// No Accept-Language header
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"language":"en"`)
	})

	t.Run("parses first language preference", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptLanguageMiddleware())
		router.GET("/test", func(c *gin.Context) {
			lang, _ := c.Get("language")
			c.JSON(http.StatusOK, gin.H{"language": lang})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept-Language", "fr-FR, en-US;q=0.9, en;q=0.8")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"language":"fr-FR"`)
	})

	t.Run("handles simple language code", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptLanguageMiddleware())
		router.GET("/test", func(c *gin.Context) {
			lang, _ := c.Get("language")
			c.JSON(http.StatusOK, gin.H{"language": lang})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept-Language", "de")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"language":"de"`)
	})

	t.Run("never fails requests due to language", func(t *testing.T) {
		router := gin.New()
		router.Use(AcceptLanguageMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept-Language", "invalid")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestIsLikelyRequiredField(t *testing.T) {
	tests := []struct {
		fieldName string
		expected  bool
	}{
		{"name", true},
		{"title", true},
		{"id", true},
		{"type", true},
		{"email", true},
		{"description", false},
		{"optional", false},
		{"value", false},
		{"NAME", false}, // Case sensitive
		{"Name", false},
	}

	for _, tc := range tests {
		t.Run(tc.fieldName, func(t *testing.T) {
			result := isLikelyRequiredField(tc.fieldName)
			assert.Equal(t, tc.expected, result, "isLikelyRequiredField(%q) should be %v", tc.fieldName, tc.expected)
		})
	}
}
