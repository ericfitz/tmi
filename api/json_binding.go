package api

import (
	"encoding/json"
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
func StrictJSONBind(c *gin.Context, target interface{}) string {
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
		if syntaxErr, ok := err.(*json.SyntaxError); ok {
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
func StrictFormBind(c *gin.Context, target interface{}, allowedFields map[string]bool) string {
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

// RespondWithError sends a standardized error response matching the OpenAPI Error schema
func RespondWithError(c *gin.Context, statusCode int, errorCode, errorDescription string) {
	c.JSON(statusCode, Error{
		Error:            errorCode,
		ErrorDescription: errorDescription,
	})
}

// RespondWithBadRequest sends a 400 Bad Request error response
func RespondWithBadRequest(c *gin.Context, errorDescription string) {
	RespondWithError(c, http.StatusBadRequest, "invalid_request", errorDescription)
}
