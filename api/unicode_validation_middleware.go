package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"golang.org/x/text/unicode/norm"
)

// UnicodeNormalizationMiddleware normalizes Unicode in request bodies and rejects problematic characters
func UnicodeNormalizationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Only process JSON requests with a body
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}

		contentType := c.GetHeader("Content-Type")
		if !strings.Contains(contentType, "application/json") {
			c.Next()
			return
		}

		// Read the request body
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			logger.Error("Failed to read request body: %v", err)
			c.Next()
			return
		}

		// Close the original body
		_ = c.Request.Body.Close()

		// Check for problematic Unicode characters
		bodyStr := string(bodyBytes)
		if hasProblematicUnicode(bodyStr) {
			logger.Warn("Request contains problematic Unicode characters")
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "Request contains unsupported Unicode characters (zero-width, bidirectional overrides, or control characters)",
			})
			c.Abort()
			return
		}

		// Normalize Unicode to NFC form (canonical composition)
		normalizedStr := norm.NFC.String(bodyStr)

		// Reset the request body with normalized content
		c.Request.Body = io.NopCloser(bytes.NewBufferString(normalizedStr))
		c.Request.ContentLength = int64(len(normalizedStr))

		c.Next()
	}
}

// hasProblematicUnicode checks for zero-width characters, bidirectional overrides, and other problematic Unicode
func hasProblematicUnicode(s string) bool {
	for _, r := range s {
		// Zero-width characters
		if r == '\u200B' || // Zero Width Space
			r == '\u200C' || // Zero Width Non-Joiner
			r == '\u200D' || // Zero Width Joiner
			r == '\uFEFF' { // Zero Width No-Break Space (BOM)
			return true
		}

		// Bidirectional text override characters
		if r == '\u202A' || // Left-to-Right Embedding
			r == '\u202B' || // Right-to-Left Embedding
			r == '\u202C' || // Pop Directional Formatting
			r == '\u202D' || // Left-to-Right Override
			r == '\u202E' || // Right-to-Left Override
			r == '\u2066' || // Left-to-Right Isolate
			r == '\u2067' || // Right-to-Left Isolate
			r == '\u2068' || // First Strong Isolate
			r == '\u2069' { // Pop Directional Isolate
			return true
		}

		// Hangul filler characters
		if r == '\u3164' { // Hangul Filler
			return true
		}

		// Check for control characters (except common whitespace)
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}

	return false
}

// ContentTypeValidationMiddleware validates Content-Type header and rejects unsupported types
func ContentTypeValidationMiddleware() gin.HandlerFunc {
	supportedContentTypes := map[string]bool{
		"application/json":                  true,
		"application/json; charset=utf-8":   true,
		"application/x-www-form-urlencoded": true,
		"multipart/form-data":               true,
	}

	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Only check POST, PUT, PATCH requests with a body
		if c.Request.Method != http.MethodPost &&
			c.Request.Method != http.MethodPut &&
			c.Request.Method != http.MethodPatch {
			c.Next()
			return
		}

		contentType := c.GetHeader("Content-Type")
		if contentType == "" {
			// Allow empty Content-Type for requests without a body
			if c.Request.ContentLength == 0 {
				c.Next()
				return
			}

			logger.Warn("Missing Content-Type header for request with body")
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "Content-Type header is required for requests with a body",
			})
			c.Abort()
			return
		}

		// Extract base content type (without parameters)
		baseContentType := strings.Split(contentType, ";")[0]
		baseContentType = strings.TrimSpace(baseContentType)

		// Check if content type is supported
		if !supportedContentTypes[contentType] && !supportedContentTypes[baseContentType] {
			logger.Warn("Unsupported Content-Type: %s", contentType)
			c.Header("Accept", "application/json")
			c.JSON(http.StatusUnsupportedMediaType, gin.H{
				"error":             "unsupported_media_type",
				"error_description": "The Content-Type header specifies an unsupported media type",
				"details": gin.H{
					"content_type": contentType,
					"supported":    []string{"application/json", "application/x-www-form-urlencoded", "multipart/form-data"},
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// DuplicateHeaderValidationMiddleware rejects requests with duplicate critical security headers
// Per RFC 7230 Section 3.2.2, duplicate headers are only allowed if the header is defined
// as a comma-separated list or is a known exception (like Set-Cookie).
// Duplicate security-critical headers can enable various attacks including request smuggling,
// authentication bypass, and cache poisoning.
func DuplicateHeaderValidationMiddleware() gin.HandlerFunc {
	// Headers that MUST NOT appear multiple times per RFC 7230 and security best practices
	criticalHeaders := []string{
		"Authorization",  // Multiple auth tokens could cause confusion about which identity to use
		"Host",           // Ambiguous host routing can lead to cache poisoning
		"Content-Type",   // Ambiguous content type can bypass validation
		"Content-Length", // Multiple Content-Length headers enable HTTP request smuggling (Go already rejects these)
	}

	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		for _, header := range criticalHeaders {
			values := c.Request.Header.Values(header)
			if len(values) > 1 {
				logger.Warn("Rejected request with duplicate %s header: %d instances found", header, len(values))
				c.JSON(http.StatusBadRequest, gin.H{
					"error":  "duplicate_header",
					"detail": fmt.Sprintf("Multiple %s headers not allowed", header),
				})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// AcceptLanguageMiddleware handles Accept-Language headers gracefully
func AcceptLanguageMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		acceptLanguage := c.GetHeader("Accept-Language")

		// Set default language if not specified
		if acceptLanguage == "" {
			c.Set("language", "en")
		} else {
			// Parse and use first language preference
			langs := strings.Split(acceptLanguage, ",")
			if len(langs) > 0 {
				// Extract language code (before quality value)
				lang := strings.Split(langs[0], ";")[0]
				lang = strings.TrimSpace(lang)
				c.Set("language", lang)
			} else {
				c.Set("language", "en")
			}
		}

		// Never fail requests due to language preferences
		c.Next()
	}
}

// BoundaryValueValidationMiddleware enhances validation of boundary values in JSON
func BoundaryValueValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Only process JSON requests with a body
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodDelete || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}

		contentType := c.GetHeader("Content-Type")
		if !strings.Contains(contentType, "application/json") {
			c.Next()
			return
		}

		// Read the request body
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			logger.Error("Failed to read request body: %v", err)
			c.Next()
			return
		}

		// Close the original body
		_ = c.Request.Body.Close()

		// Parse JSON to check for null values and empty strings in required fields
		var data map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &data); err != nil {
			// Not valid JSON, let OpenAPI validation handle it
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			c.Next()
			return
		}

		// Check for explicit null values in string fields
		for key, value := range data {
			if value == nil {
				// Null values will be handled by OpenAPI validation
				continue
			}

			// Check for empty strings in fields that likely shouldn't be empty
			if str, ok := value.(string); ok {
				if str == "" && isLikelyRequiredField(key) {
					logger.Warn("Empty string in likely required field: %s", key)
					// Let it pass - OpenAPI validation will handle required field validation
				}
			}
		}

		// Reset the request body
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		c.Next()
	}
}

// isLikelyRequiredField checks if a field name suggests it's required
func isLikelyRequiredField(fieldName string) bool {
	requiredFieldNames := []string{"name", "title", "id", "type", "email"}
	for _, required := range requiredFieldNames {
		if fieldName == required {
			return true
		}
	}
	return false
}
