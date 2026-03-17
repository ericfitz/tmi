package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/unicodecheck"
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

		// Normalize Unicode to NFC form first, so decomposed legitimate text
		// (e.g., e + combining acute) is composed before validation.
		// NFC does not strip zero-width chars, BiDi overrides, or control chars.
		bodyStr := string(bodyBytes)
		normalizedStr := norm.NFC.String(bodyStr)

		// Check for problematic Unicode characters on the normalized form
		if hasProblematicUnicode(normalizedStr) {
			logger.Warn("Request contains problematic Unicode characters")
			c.JSON(http.StatusBadRequest, Error{
				Error:            "invalid_request",
				ErrorDescription: "Request contains unsupported Unicode characters (zero-width, bidirectional overrides, excessive combining marks, or control characters)",
			})
			c.Abort()
			return
		}

		// Reset the request body with normalized content
		c.Request.Body = io.NopCloser(bytes.NewBufferString(normalizedStr))
		c.Request.ContentLength = int64(len(normalizedStr))

		c.Next()
	}
}

// hasProblematicUnicode checks for zero-width characters, bidirectional overrides, and other problematic Unicode.
// Delegates to the consolidated unicodecheck package.
// Uses context-aware zero-width checking to allow ZWNJ in Indic scripts and ZWJ in emoji sequences.
func hasProblematicUnicode(s string) bool {
	return unicodecheck.ContainsDangerousZeroWidthChars(s) ||
		unicodecheck.ContainsBidiOverrides(s) ||
		unicodecheck.ContainsHangulFillers(s) ||
		unicodecheck.ContainsFullwidthStructuralChars(s) ||
		unicodecheck.ContainsControlChars(s) ||
		unicodecheck.HasExcessiveCombiningMarks(s, 3)
}

// ContentTypeValidationMiddleware validates Content-Type header and rejects unsupported types
func ContentTypeValidationMiddleware() gin.HandlerFunc {
	// Global fallback for endpoints not found in the OpenAPI spec.
	globalSupportedContentTypes := map[string]bool{
		"application/json":            true,
		"application/json-patch+json": true,
	}

	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Only check POST, PUT, PATCH, DELETE requests with a body
		if c.Request.Method != http.MethodPost &&
			c.Request.Method != http.MethodPut &&
			c.Request.Method != http.MethodPatch &&
			c.Request.Method != http.MethodDelete {
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

		// Extract base content type (without parameters like charset, boundary)
		baseContentType := strings.Split(contentType, ";")[0]
		baseContentType = strings.TrimSpace(baseContentType)

		// Use per-endpoint accepted content types from the OpenAPI spec when available.
		// This ensures endpoints that only accept application/json reject
		// application/x-www-form-urlencoded and multipart/form-data.
		accepted := getAcceptedContentTypes(c.Request.URL.Path, c.Request.Method)
		if accepted == nil {
			// Path/method not in spec — fall back to global whitelist
			accepted = globalSupportedContentTypes
		}

		if !accepted[baseContentType] {
			logger.Warn("Unsupported Content-Type: %s for %s %s", contentType, c.Request.Method, c.Request.URL.Path)
			c.Header("Accept", "application/json")
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
		var data map[string]any
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
	return slices.Contains(requiredFieldNames, fieldName)
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
		var temp any
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
		return nil //nolint:nilerr // let json.Unmarshal handle syntax errors
	}

	switch t {
	case json.Delim('{'):
		// Object - check for duplicate keys
		keys := make(map[string]bool)
		for dec.More() {
			// Read key
			keyToken, err := dec.Token()
			if err != nil {
				return nil //nolint:nilerr // let json.Unmarshal handle syntax errors
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
