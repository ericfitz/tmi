package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGinServerErrorHandler tests that the GinServerErrorHandler converts errors to JSON
// This is the core fix for CATS fuzzer findings - ensuring parameter binding errors return JSON
func TestGinServerErrorHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		err         error
		statusCode  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "Enum validation error",
			err:         fmt.Errorf("invalid enum value"),
			statusCode:  http.StatusBadRequest,
			wantCode:    "invalid_input",
			wantMessage: "Invalid parameter value",
		},
		{
			name:        "Required parameter error",
			err:         fmt.Errorf("required field missing"),
			statusCode:  http.StatusBadRequest,
			wantCode:    "invalid_input",
			wantMessage: "Missing required parameter",
		},
		{
			name:        "Format error",
			err:         fmt.Errorf("format validation failed"),
			statusCode:  http.StatusBadRequest,
			wantCode:    "invalid_id",
			wantMessage: "format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test context with a proper request
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/test", nil)
			c.Set("requestID", "test-request-id")

			// Call the error handler
			GinServerErrorHandler(c, tt.err, tt.statusCode)

			// Assert status code
			assert.Equal(t, tt.statusCode, w.Code)

			// CRITICAL: Assert content type is JSON
			contentType := w.Header().Get("Content-Type")
			assert.Contains(t, contentType, "application/json",
				"Response must be JSON, got: %s", contentType)

			// Parse response
			var errorResponse map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
			require.NoError(t, err, "Response must be valid JSON: %s", w.Body.String())

			// Verify TMI error structure
			assert.Contains(t, errorResponse, "error")
			assert.Contains(t, errorResponse, "error_description")

			// Verify error code
			errorCode, ok := errorResponse["error"].(string)
			assert.True(t, ok)
			assert.Equal(t, tt.wantCode, errorCode)

			// Verify error description contains expected message
			errorDesc, ok := errorResponse["error_description"].(string)
			assert.True(t, ok)
			assert.Contains(t, errorDesc, tt.wantMessage)

			// Must NOT be plain text
			assert.NotEqual(t, "400 Bad Request", w.Body.String())
		})
	}
}

// TestOpenAPIErrorHandler tests that OpenAPIErrorHandler also returns JSON
// This tests the middleware-level error handler
func TestOpenAPIErrorHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		message    string
		statusCode int
		wantCode   string
	}{
		{
			name:       "No matching operation",
			message:    "no matching operation was found",
			statusCode: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:       "Required field error",
			message:    "required field is missing",
			statusCode: http.StatusBadRequest,
			wantCode:   "invalid_input",
		},
		{
			name:       "Format error",
			message:    "format validation failed",
			statusCode: http.StatusBadRequest,
			wantCode:   "invalid_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set("requestID", "test-request-id")
			c.Request = httptest.NewRequest("GET", "/test", nil)

			// Call OpenAPI error handler
			OpenAPIErrorHandler(c, tt.message, tt.statusCode)

			// Verify JSON response
			contentType := w.Header().Get("Content-Type")
			assert.Contains(t, contentType, "application/json")

			var errorResponse map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
			require.NoError(t, err)

			assert.Contains(t, errorResponse, "error")
			assert.Contains(t, errorResponse, "error_description")

			errorCode, _ := errorResponse["error"].(string)
			assert.Equal(t, tt.wantCode, errorCode)
		})
	}
}
