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
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_request",
				ErrorDescription: "Request contains unsupported Unicode characters (zero-width, bidirectional overrides, excessive combining marks, or control characters)",
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

// maxConsecutiveCombiningMarks is the threshold for detecting "Zalgo text" abuse.
// Normal text may use 1-2 combining marks per base character; 3+ consecutive
// combining marks is considered excessive.
const maxConsecutiveCombiningMarks = 3

// isCombiningMark returns true if the rune is a Unicode combining character.
// Covers Combining Diacritical Marks (U+0300–U+036F) and common extended ranges.
func isCombiningMark(r rune) bool {
	return (r >= '\u0300' && r <= '\u036F') || // Combining Diacritical Marks
		(r >= '\u0483' && r <= '\u0489') || // Combining Cyrillic
		(r >= '\u0591' && r <= '\u05BD') || // Hebrew combining marks
		(r >= '\u0610' && r <= '\u061A') || // Arabic combining marks
		(r >= '\u064B' && r <= '\u065F') || // Arabic combining marks (cont.)
		(r >= '\u0E31' && r <= '\u0E3A') || // Thai combining marks
		(r >= '\u0E47' && r <= '\u0E4E') // Thai combining marks (cont.)
}

// hasProblematicUnicode checks for zero-width characters, bidirectional overrides, and other problematic Unicode
func hasProblematicUnicode(s string) bool {
	consecutiveCombining := 0

	for _, r := range s {
		// Track consecutive combining marks to detect Zalgo text abuse
		if isCombiningMark(r) {
			consecutiveCombining++
			if consecutiveCombining >= maxConsecutiveCombiningMarks {
				return true
			}
			continue
		}
		consecutiveCombining = 0

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

		// Fullwidth characters that can be used for visual spoofing
		// Allow fullwidth forms in general (used in CJK text) but reject in JSON structure
		if r >= '\uFF00' && r <= '\uFFEF' {
			// Check if it's within JSON structural characters (brackets, quotes, etc.)
			// This is a heuristic - we allow fullwidth in string values but not structure
			if strings.ContainsAny(string(r), "[]{}\":,") {
				return true
			}
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
		"application/json-patch+json":       true,
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
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_request",
				ErrorDescription: "Content-Type header is required for requests with a body",
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
			// Note: Using gin.H for this error because the Error struct's Details field
			// doesn't support arbitrary context. The Error schema allows additionalProperties
			// in details.context, but the generated Go struct doesn't. This response will
			// include content_type and supported fields that CATS may flag for schema mismatch.
			c.JSON(http.StatusUnsupportedMediaType, Error{
				Error:            "unsupported_media_type",
				ErrorDescription: "The Content-Type header specifies an unsupported media type",
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
				c.JSON(http.StatusBadRequest, Error{
					Error:            "duplicate_header",
					ErrorDescription: fmt.Sprintf("Multiple %s headers not allowed", header),
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

// StrictJSONValidationMiddleware validates JSON syntax strictly, rejecting:
// - Trailing garbage after valid JSON (e.g., `{"name":"test"}bla`)
// - Duplicate keys in objects (RFC 8259 recommends unique keys)
// This ensures all handlers receive well-formed JSON regardless of which
// binding method they use (ShouldBindJSON vs ParseRequestBody).
func StrictJSONValidationMiddleware() gin.HandlerFunc {
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

		// Skip validation for empty bodies (will be handled by handlers)
		if len(bodyBytes) == 0 {
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			c.Next()
			return
		}

		// Use json.Decoder with DisallowUnknownFields equivalent for strict parsing
		// and check for trailing garbage by ensuring we're at EOF after decoding
		decoder := json.NewDecoder(bytes.NewReader(bodyBytes))

		// Decode to validate the JSON is well-formed
		var temp interface{}
		if err := decoder.Decode(&temp); err != nil {
			logger.Warn("Invalid JSON syntax: %v", err)
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: "Request body contains invalid JSON syntax",
			})
			c.Abort()
			return
		}

		// Check for trailing garbage after the JSON value
		// If we can read another token, there's extra content
		if decoder.More() {
			logger.Warn("JSON contains trailing garbage after valid value")
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: "Request body contains invalid JSON: unexpected content after JSON value",
			})
			c.Abort()
			return
		}

		// Also check if there's any non-whitespace content remaining
		remaining, _ := io.ReadAll(decoder.Buffered())
		if len(bytes.TrimSpace(remaining)) > 0 {
			logger.Warn("JSON contains trailing content: %q", remaining)
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: "Request body contains invalid JSON: unexpected content after JSON value",
			})
			c.Abort()
			return
		}

		// Check for duplicate keys in the JSON object
		if err := validateNoDuplicateKeys(bodyBytes); err != nil {
			logger.Warn("JSON contains duplicate keys: %v", err)
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_input",
				ErrorDescription: err.Error(),
			})
			c.Abort()
			return
		}

		// Reset the request body for handlers
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		c.Next()
	}
}

// validateNoDuplicateKeys checks for duplicate keys in a JSON object
// RFC 8259 recommends unique keys, and duplicate keys can cause unexpected behavior
func validateNoDuplicateKeys(jsonBytes []byte) error {
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	return checkDuplicateKeysRecursive(dec, "")
}

// checkDuplicateKeysRecursive recursively checks for duplicate keys in JSON
func checkDuplicateKeysRecursive(dec *json.Decoder, path string) error {
	// Read opening token
	t, err := dec.Token()
	if err != nil {
		return nil // Let json.Unmarshal handle syntax errors
	}

	switch t {
	case json.Delim('{'):
		// Object - check for duplicate keys
		keys := make(map[string]bool)
		for dec.More() {
			// Read key
			keyToken, err := dec.Token()
			if err != nil {
				return nil // Let json.Unmarshal handle syntax errors
			}

			key, ok := keyToken.(string)
			if !ok {
				continue
			}

			if keys[key] {
				return fmt.Errorf("duplicate key '%s' in JSON object", key)
			}
			keys[key] = true

			// Recursively check the value
			keyPath := key
			if path != "" {
				keyPath = path + "." + key
			}
			if err := checkDuplicateKeysRecursive(dec, keyPath); err != nil {
				return err
			}
		}
		// Read closing brace
		_, _ = dec.Token()

	case json.Delim('['):
		// Array - check each element
		for dec.More() {
			if err := checkDuplicateKeysRecursive(dec, path); err != nil {
				return err
			}
		}
		// Read closing bracket
		_, _ = dec.Token()

	default:
		// Primitive value - nothing to check
	}

	return nil
}
