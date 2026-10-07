package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
			var errorResponse map[string]any
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
			name:       "Malformed path parameter format",
			message:    "error in openapi3filter.RequestError: parameter \"threat_model_id\" in path has an error: string doesn't match the format \"uuid\"",
			statusCode: http.StatusBadRequest,
			wantCode:   "invalid_id",
		},
		{
			name:       "Malformed query parameter format",
			message:    "error in openapi3filter.RequestError: parameter \"created_after\" in query has an error: string doesn't match the format \"date-time\"",
			statusCode: http.StatusBadRequest,
			wantCode:   "invalid_id",
		},

		{
			// kin-openapi embeds the failing schema (with its "pattern" and
			// "format" keywords) in the message; that must not make a body
			// validation failure look like a malformed identifier.
			name: "Request body constraint violation with schema dump",
			message: "multiple errors encountered: Error at \"/severity\": maximum string length is 50 " +
				"Schema:   {     \"maxLength\": 50,     \"pattern\": \"^[^\\\\x00-\\\\x1F]*$\",     \"type\": \"string\"   }",
			statusCode: http.StatusBadRequest,
			wantCode:   "invalid_input",
		},
		{
			name:       "Request body format violation",
			message:    "error in openapi3filter.RequestError: request body has an error: doc validation failed: Error at \"/created_at\": string doesn't match the format \"date-time\"",
			statusCode: http.StatusBadRequest,
			wantCode:   "invalid_input",
		},
		{
			name:       "Unclassified validation failure",
			message:    "format validation failed",
			statusCode: http.StatusBadRequest,
			wantCode:   "invalid_input",
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

			var errorResponse map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
			require.NoError(t, err)

			assert.Contains(t, errorResponse, "error")
			assert.Contains(t, errorResponse, "error_description")

			errorCode, _ := errorResponse["error"].(string)
			assert.Equal(t, tt.wantCode, errorCode)
		})
	}
}

// TestOpenAPIValidation_BodyViolationsAreInvalidInput drives the real OpenAPI
// request validator with request bodies that break the spec's constraints and
// asserts the documented 400 error code, invalid_input, rather than invalid_id
// (which is reserved for malformed path/query identifiers).
func TestOpenAPIValidation_BodyViolationsAreInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validator, err := SetupOpenAPIValidation()
	require.NoError(t, err)

	r := gin.New()
	r.Use(validator)
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	r.POST("/threat_models/:threat_model_id/threats", ok)
	r.POST("/threat_models/:threat_model_id/threats/bulk", ok)

	tmPath := "/threat_models/" + testUUID1 + "/threats"
	tests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "single create with constraint violations",
			path: tmPath,
			body: `{"name":"T","threat_type":["Spoofing","Spoofing"],"severity":"` + strings.Repeat("x", 51) + `","mitigated":false}`,
		},
		{
			name: "bulk create with wrong data types",
			path: tmPath + "/bulk",
			body: `[{"name":"Invalid Threat","description":"x"},{"name":123,"threat_type":true,"severity":"InvalidSeverity"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var resp map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, "invalid_input", resp["error"], w.Body.String())
		})
	}
}

// TestOpenAPIValidation_ParameterViolationsAreInvalidID drives the real OpenAPI
// request validator with malformed path/query parameters and asserts the
// documented identifier error, invalid_id. It guards the classification in
// OpenAPIErrorHandler against a change in kin-openapi's error wording.
func TestOpenAPIValidation_ParameterViolationsAreInvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validator, err := SetupOpenAPIValidation()
	require.NoError(t, err)

	r := gin.New()
	r.Use(validator)
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	// Note: format "uuid" is not enforced by the validator (UUIDValidationMiddleware
	// handles path UUIDs), so use parameters whose format/pattern it does enforce.
	r.GET("/usability_feedback", ok)

	tests := []struct {
		name string
		path string
	}{
		{name: "query parameter with date-time format", path: "/usability_feedback?created_after=not-a-date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var resp map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, "invalid_id", resp["error"], w.Body.String())
		})
	}
}
