package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// StrictJSONBind binds JSON request body to the target struct, rejecting unknown fields.
// This prevents mass assignment vulnerabilities where attackers send undeclared fields
// that might be accidentally processed.
//
// Returns an error message suitable for the client if binding fails, or empty string on success.
func StrictJSONBind(c *gin.Context, target any) string {
	// Read body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "Failed to read request body"
	}

	// Empty body check
	if len(body) == 0 {
		return "Request body is required"
	}

	// Use strict decoder that rejects unknown fields
	decoder := json.NewDecoder(jsonBytesReader(body))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		// Check if it's an unknown field error
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return fmt.Sprintf("Invalid JSON syntax at position %d", syntaxErr.Offset)
		}
		// Unknown field errors contain "unknown field"
		return fmt.Sprintf("Invalid request: %s", err.Error())
	}

	return ""
}

// StrictFormBind binds form-urlencoded request body to the target struct.
// For form data, we validate against a whitelist of allowed fields.
//
// allowedFields is a map of field names that are permitted.
// Returns an error message if unknown fields are present, or empty string on success.
func StrictFormBind(c *gin.Context, target any, allowedFields map[string]bool) string {
	// First do the normal binding
	if err := c.ShouldBind(target); err != nil {
		return fmt.Sprintf("Invalid request: %s", err.Error())
	}

	// Check for unknown fields in form data
	if err := c.Request.ParseForm(); err != nil {
		return "Failed to parse form data"
	}

	for field := range c.Request.PostForm {
		if !allowedFields[field] {
			return fmt.Sprintf("Unknown field in request: %s", field)
		}
	}

	return ""
}

// jsonBytesReader creates an io.Reader from a byte slice
func jsonBytesReader(data []byte) io.Reader {
	return &bytesReader{data: data}
}

type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// ValidateJSONStringFields checks that the specified fields in a JSON request body are
// string types (not numbers, booleans, objects, or arrays). This prevents type coercion
// where Go's json.Unmarshal silently converts e.g. numeric 123 to string "123".
// The body bytes are read from the gin context and restored for subsequent binding.
// Returns an error message if any field has the wrong type, or empty string on success.
func ValidateJSONStringFields(c *gin.Context, fields ...string) string {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "Failed to read request body"
	}
	// Restore body for subsequent binding
	c.Request.Body = io.NopCloser(jsonBytesReader(body))

	if len(body) == 0 {
		return "" // Let the binding step handle empty body
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return "" // Let the binding step handle parse errors
	}

	for _, field := range fields {
		val, exists := raw[field]
		if !exists || string(val) == jsonNull {
			continue // Optional fields that are absent or null are fine
		}
		// A JSON string always starts with a double quote
		if len(val) == 0 || val[0] != '"' {
			return fmt.Sprintf("Field '%s' must be a string, not a %s", field, jsonTypeName(val))
		}
	}

	return ""
}

// jsonTypeName returns a human-readable name for the JSON type of a raw value.
func jsonTypeName(val json.RawMessage) string {
	if len(val) == 0 {
		return "null"
	}
	switch val[0] {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "boolean"
	default:
		return "number"
	}
}

// RespondWithError sends a standardized error response matching the OpenAPI Error schema.
// Calls c.Abort() so downstream middleware in the chain do not overwrite the status.
func RespondWithError(c *gin.Context, statusCode int, errorCode, errorDescription string) {
	c.JSON(statusCode, Error{
		Error:            errorCode,
		ErrorDescription: errorDescription,
	})
	c.Abort()
}

// RespondWithBadRequest sends a 400 Bad Request error response
func RespondWithBadRequest(c *gin.Context, errorDescription string) {
	RespondWithError(c, http.StatusBadRequest, "invalid_request", errorDescription)
}
