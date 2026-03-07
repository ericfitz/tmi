package api

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// enumQueryParams maps query parameter names that carry enum values.
// Values are normalized to lowercase/snake_case before OpenAPI validation.
var enumQueryParams = map[string]bool{
	"severity":    true,
	"sort_by":     true,
	"sort_order":  true,
	"format":      true,
	"change_type": true,
	"object_type": true,
}

// enumBodyFields maps JSON body field names that carry enum values.
var enumBodyFields = map[string]bool{
	"change_type":        true,
	"default_theme":      true,
	"event_type":         true,
	"grant_type":         true,
	"object_type":        true,
	"op":                 true,
	"permissions":        true,
	"position":           true,
	"principal_type":     true,
	"refType":            true,
	"role":               true,
	"shape":              true,
	"status":             true,
	"subject_type":       true,
	"token_type":         true,
	"token_type_hint":    true,
	"severity":           true,
	"jump":               true,
	"textAnchor":         true,
	"textVerticalAnchor": true,
}

// camelToSnake converts a camelCase or PascalCase string to snake_case.
// Consecutive uppercase letters are treated as an acronym and kept together.
// e.g., "oneSide" -> "one_side", "Bearer" -> "bearer", "OK" -> "ok",
// "DEGRADED" -> "degraded", "Critical" -> "critical"
func camelToSnake(s string) string {
	runes := []rune(s)
	var result strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			// Insert underscore before an uppercase letter when:
			// - It's not the first character, AND
			// - Either the previous char is lowercase, OR
			//   the next char is lowercase (end of an acronym like "HTTPServer" -> "http_server")
			if i > 0 {
				prevLower := unicode.IsLower(runes[i-1])
				nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if prevLower || nextLower {
					result.WriteRune('_')
				}
			}
			result.WriteRune(unicode.ToLower(r))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// NormalizeEnumValue converts any case variant to canonical snake_case.
// e.g., "Critical" -> "critical", "OneSide" -> "one_side", "OK" -> "ok"
func NormalizeEnumValue(value string) string {
	return camelToSnake(strings.TrimSpace(value))
}

// EnumNormalizerMiddleware normalizes enum values in query parameters and JSON
// request bodies to their canonical snake_case form before OpenAPI validation.
// This makes enum matching case-insensitive for API consumers.
func EnumNormalizerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.GetContextLogger(c)

		// Normalize enum query parameters
		normalizeQueryParams(c, logger)

		// Normalize enum values in JSON request bodies
		if c.Request.Body != nil && c.Request.ContentLength != 0 &&
			strings.Contains(c.GetHeader("Content-Type"), "application/json") {
			normalizeJSONBody(c, logger)
		}

		c.Next()
	}
}

// normalizeQueryParams normalizes enum values in query parameters.
func normalizeQueryParams(c *gin.Context, logger slogging.SimpleLogger) {
	query := c.Request.URL.Query()
	modified := false

	for param := range enumQueryParams {
		if values, ok := query[param]; ok {
			for i, v := range values {
				normalized := NormalizeEnumValue(v)
				if normalized != v {
					logger.Debug("ENUM_NORMALIZE query param %s: %q -> %q", param, v, normalized)
					values[i] = normalized
					modified = true
				}
			}
		}
	}

	if modified {
		c.Request.URL.RawQuery = query.Encode()
	}
}

// normalizeJSONBody normalizes enum field values in a JSON request body.
func normalizeJSONBody(c *gin.Context, logger slogging.SimpleLogger) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return
	}
	_ = c.Request.Body.Close()

	if len(bodyBytes) == 0 {
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return
	}

	var body any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		// Not valid JSON — restore body and let OpenAPI validator handle the error
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return
	}

	modified := normalizeEnumFields(body)
	if !modified {
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return
	}

	newBytes, err := json.Marshal(body)
	if err != nil {
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return
	}

	logger.Debug("ENUM_NORMALIZE JSON body enum values normalized")
	c.Request.Body = io.NopCloser(bytes.NewBuffer(newBytes))
	c.Request.ContentLength = int64(len(newBytes))
}

// normalizeEnumFields recursively walks a parsed JSON value and normalizes
// string values for known enum field names. Returns true if any value was modified.
func normalizeEnumFields(v any) bool {
	modified := false
	switch val := v.(type) {
	case map[string]any:
		for key, fieldVal := range val {
			if enumBodyFields[key] {
				switch fv := fieldVal.(type) {
				case string:
					normalized := NormalizeEnumValue(fv)
					if normalized != fv {
						val[key] = normalized
						modified = true
					}
				case []any:
					// Handle array enum fields (e.g., "objects", "directions")
					for i, item := range fv {
						if s, ok := item.(string); ok {
							normalized := NormalizeEnumValue(s)
							if normalized != s {
								fv[i] = normalized
								modified = true
							}
						}
					}
				}
			}
			// Recurse into nested objects and arrays
			if normalizeEnumFields(fieldVal) {
				modified = true
			}
		}
	case []any:
		for _, item := range val {
			if normalizeEnumFields(item) {
				modified = true
			}
		}
	}
	return modified
}
