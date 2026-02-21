package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePatchRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		body        string
		expected    []PatchOperation
		expectError bool
		errorCode   string
	}{
		{
			name: "valid patch operations",
			body: `[
				{"op": "replace", "path": "/name", "value": "new name"},
				{"op": "add", "path": "/description", "value": "new description"}
			]`,
			expected: []PatchOperation{
				{Op: "replace", Path: "/name", Value: "new name"},
				{Op: "add", Path: "/description", Value: "new description"},
			},
			expectError: false,
		},
		{
			name:        "empty body",
			body:        "",
			expectError: true,
			errorCode:   "invalid_input",
		},
		{
			name:        "invalid JSON",
			body:        `{"invalid": json}`,
			expectError: true,
			errorCode:   "invalid_input",
		},
		{
			name:        "not an array",
			body:        `{"op": "replace", "path": "/name", "value": "test"}`,
			expectError: true,
			errorCode:   "invalid_input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test context
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("PATCH", "/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			// Test the function
			operations, err := ParsePatchRequest(c)

			if tt.expectError {
				require.Error(t, err)
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "Expected RequestError")
				assert.Equal(t, tt.errorCode, reqErr.Code)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, operations)
			}
		})
	}
}

func TestParseRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type TestStruct struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Count       int    `json:"count"`
	}

	tests := []struct {
		name        string
		body        string
		expected    TestStruct
		expectError bool
		errorCode   string
	}{
		{
			name: "valid JSON",
			body: `{"name": "test", "description": "test desc", "count": 42}`,
			expected: TestStruct{
				Name:        "test",
				Description: "test desc",
				Count:       42,
			},
			expectError: false,
		},
		{
			name:        "empty body",
			body:        "",
			expectError: true,
			errorCode:   "invalid_input",
		},
		{
			name:        "invalid JSON",
			body:        `{"name": invalid}`,
			expectError: true,
			errorCode:   "invalid_input",
		},
		{
			name: "partial JSON",
			body: `{"name": "test"}`,
			expected: TestStruct{
				Name:        "test",
				Description: "",
				Count:       0,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test context
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			// Test the function
			result, err := ParseRequestBody[TestStruct](c)

			if tt.expectError {
				require.Error(t, err)
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "Expected RequestError")
				assert.Equal(t, tt.errorCode, reqErr.Code)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValidateAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		userName     any
		userRole     any
		expectedUser string
		expectedRole Role
		expectError  bool
		errorCode    string
	}{
		{
			name:         "valid user with role",
			userName:     "testuser",
			userRole:     RoleOwner,
			expectedUser: "testuser",
			expectedRole: RoleOwner,
			expectError:  false,
		},
		{
			name:         "valid user without role",
			userName:     "testuser",
			userRole:     nil, // Not set in context
			expectedUser: "testuser",
			expectedRole: "",
			expectError:  false,
		},
		{
			name:         "no username",
			userName:     nil,
			userRole:     RoleOwner,
			expectedUser: "",
			expectedRole: "",
			expectError:  true,
			errorCode:    "unauthorized",
		},
		{
			name:         "empty username",
			userName:     "",
			userRole:     RoleOwner,
			expectedUser: "",
			expectedRole: "",
			expectError:  true,
			errorCode:    "unauthorized",
		},
		{
			name:         "invalid username type",
			userName:     123,
			userRole:     RoleOwner,
			expectedUser: "",
			expectedRole: "",
			expectError:  true,
			errorCode:    "unauthorized",
		},
		{
			name:         "invalid role type",
			userName:     "testuser",
			userRole:     "invalid-role-type",
			expectedUser: "",
			expectedRole: "",
			expectError:  true,
			errorCode:    "server_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test context
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Set context values
			if tt.userName != nil {
				c.Set("userEmail", tt.userName)
				// Only set userID if userName is a string (for valid test cases)
				if userNameStr, ok := tt.userName.(string); ok {
					c.Set("userID", userNameStr+"-provider-id") // Provider ID for testing
				} else {
					// For invalid type tests, set an invalid userID too
					c.Set("userID", tt.userName)
				}
			}
			if tt.userRole != nil {
				c.Set("userRole", tt.userRole)
			}

			// Test the function
			userName, _, userRole, err := ValidateAuthenticatedUser(c)

			if tt.expectError {
				require.Error(t, err)
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "Expected RequestError")
				assert.Equal(t, tt.errorCode, reqErr.Code)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedUser, userName)
				assert.Equal(t, tt.expectedRole, userRole)
			}
		})
	}
}

func TestHandleRequestError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
	}{
		{
			name: "RequestError",
			err: &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: "Test error message",
			},
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "invalid_input",
		},
		{
			name:           "Generic error",
			err:            assert.AnError,
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "server_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test context
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Test the function
			HandleRequestError(c, tt.err)

			// Check response
			assert.Equal(t, tt.expectedStatus, w.Code)

			var response Error
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCode, response.Error)
		})
	}
}

func TestRequestError(t *testing.T) {
	err := &RequestError{
		Status:  http.StatusBadRequest,
		Code:    "test_code",
		Message: "test message",
	}

	assert.Equal(t, "test message", err.Error())
}

// TestParsePatchRequestBodyReuse tests that the request body can be read multiple times
func TestParsePatchRequestBodyReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `[{"op": "replace", "path": "/name", "value": "test"}]`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PATCH", "/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	// Parse patch request first
	operations, err := ParsePatchRequest(c)
	require.NoError(t, err)
	assert.Len(t, operations, 1)

	// Try to read body again using gin's ShouldBindJSON
	var operations2 []PatchOperation
	err = c.ShouldBindJSON(&operations2)
	require.NoError(t, err)
	assert.Equal(t, operations, operations2)
}

// TestParseRequestBodyReuse tests that the request body can be read multiple times
func TestParseRequestBodyReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type TestStruct struct {
		Name string `json:"name"`
	}

	body := `{"name": "test"}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	// Parse request body first
	result1, err := ParseRequestBody[TestStruct](c)
	require.NoError(t, err)
	assert.Equal(t, "test", result1.Name)

	// Try to read body again using gin's ShouldBindJSON
	var result2 TestStruct
	err = c.ShouldBindJSON(&result2)
	require.NoError(t, err)
	assert.Equal(t, result1, result2)
}

func TestErrorUtilities(t *testing.T) {
	tests := []struct {
		name           string
		errFunc        func(string) *RequestError
		message        string
		expectedCode   string
		expectedStatus int
	}{
		{
			name:           "InvalidInputError",
			errFunc:        InvalidInputError,
			message:        "Test validation error",
			expectedCode:   "invalid_input",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "InvalidIDError",
			errFunc:        InvalidIDError,
			message:        "Test ID error",
			expectedCode:   "invalid_id",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "NotFoundError",
			errFunc:        NotFoundError,
			message:        "Test not found error",
			expectedCode:   "not_found",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "ServerError",
			errFunc:        ServerError,
			message:        "Test server error",
			expectedCode:   "server_error",
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "ForbiddenError",
			errFunc:        ForbiddenError,
			message:        "Test forbidden error",
			expectedCode:   "forbidden",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errFunc(tt.message)

			assert.Equal(t, tt.expectedStatus, err.Status)
			assert.Equal(t, tt.expectedCode, err.Code)
			assert.Equal(t, tt.message, err.Message)
			assert.Equal(t, tt.message, err.Error())
		})
	}
}

// ===========================================================================
// CATS Fuzzer Test Cases - Testing fixes for security vulnerabilities
// These tests validate that malformed JSON patterns from CATS fuzzers
// return 400 Bad Request instead of causing 500 Internal Server Error panics
// ===========================================================================

// TestParseRequestBody_ZeroWidthChars tests handling of zero-width Unicode characters
// This tests the fix for CATS fuzzer: ZeroWidthCharsInNamesFields, ZeroWidthCharsInValuesFields
func TestParseRequestBody_ZeroWidthChars(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "zero-width space in field name",
			body: `{"name\u200B":"test"}`, // U+200B is zero-width space
		},
		{
			name: "zero-width non-joiner in value",
			body: `{"name":"test\u200C"}`, // U+200C is zero-width non-joiner
		},
		{
			name: "zero-width joiner in field name",
			body: `{"na\u200Dme":"test"}`, // U+200D is zero-width joiner
		},
		{
			name: "multiple zero-width characters",
			body: `{"name\u200B\u200C":"test\u200D"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			type TestStruct struct {
				Name string `json:"name"`
			}

			// Zero-width characters in field names make the JSON valid but potentially problematic
			// Our fix ensures we handle these gracefully without panicking
			_, err := ParseRequestBody[TestStruct](c)

			// These should either parse successfully (if json.Valid accepts them)
			// or return a proper 400 error (not panic with 500)
			if err != nil {
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "Error should be RequestError type")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status, "Should return 400, not 500")
			}
		})
	}
}

// TestParseRequestBody_FullwidthBrackets tests handling of fullwidth Unicode brackets
// This tests the fix for CATS fuzzer: FullwidthBracketsFields
func TestParseRequestBody_FullwidthBrackets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "fullwidth left curly bracket",
			body: `｛"name":"test"}`, // U+FF5B fullwidth left curly bracket
		},
		{
			name: "fullwidth right curly bracket",
			body: `{"name":"test"｝`, // U+FF5D fullwidth right curly bracket
		},
		// Note: Fullwidth brackets WITHIN string values are valid JSON
		// They only cause errors when used as JSON structure characters
		{
			name: "mix of normal and fullwidth brackets",
			body: `｛"name":"test"｝`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			type TestStruct struct {
				Name string `json:"name"`
			}

			_, err := ParseRequestBody[TestStruct](c)

			// Fullwidth brackets are invalid JSON - should return 400, not panic with 500
			require.Error(t, err, "Fullwidth brackets should be rejected")
			var reqErr *RequestError
			require.True(t, errors.As(err, &reqErr), "Error should be RequestError type")
			assert.Equal(t, http.StatusBadRequest, reqErr.Status, "Should return 400 Bad Request")
			assert.Equal(t, "invalid_input", reqErr.Code)
		})
	}
}

// TestParseRequestBody_MalformedJSONPatterns tests handling of various malformed JSON patterns
// This tests the fix for CATS fuzzers: RandomDummyInvalidJsonBody, NewFields
func TestParseRequestBody_MalformedJSONPatterns(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing closing brace",
			body: `{"name":"test"`,
		},
		{
			name: "missing opening brace",
			body: `"name":"test"}`,
		},
		{
			name: "trailing comma",
			body: `{"name":"test",}`,
		},
		{
			name: "unquoted field name",
			body: `{name:"test"}`,
		},
		{
			name: "single quotes instead of double",
			body: `{'name':'test'}`,
		},
		{
			name: "completely invalid",
			body: `not json at all`,
		},
		{
			name: "null bytes",
			body: "{\"name\":\"test\x00\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			type TestStruct struct {
				Name string `json:"name"`
			}

			_, err := ParseRequestBody[TestStruct](c)

			// All malformed JSON should return 400, not panic with 500
			require.Error(t, err, "Malformed JSON should be rejected")
			var reqErr *RequestError
			require.True(t, errors.As(err, &reqErr), "Error should be RequestError type")
			assert.Equal(t, http.StatusBadRequest, reqErr.Status, "Should return 400 Bad Request")
			assert.Equal(t, "invalid_input", reqErr.Code)
			assert.Contains(t, reqErr.Message, "invalid JSON", "Error message should mention invalid JSON")
		})
	}
}

// TestParseRequestBody_BidirectionalOverride tests handling of bidirectional override characters
// This tests the fix for CATS fuzzer: BidirectionalOverrideFields
func TestParseRequestBody_BidirectionalOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "left-to-right override in value",
			body: `{"name":"test\u202Evalue"}`, // U+202E right-to-left override
		},
		{
			name: "right-to-left override in field",
			body: `{"name\u202E":"test"}`,
		},
		{
			name: "left-to-right mark",
			body: `{"name":"test\u200E"}`, // U+200E left-to-right mark
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			type TestStruct struct {
				Name string `json:"name"`
			}

			// Bidirectional override characters are valid in JSON strings
			// Our fix ensures we handle them without panicking
			_, err := ParseRequestBody[TestStruct](c)

			// Should either parse successfully or return proper 400 (not panic with 500)
			if err != nil {
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "Error should be RequestError type")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status, "Should return 400, not 500")
			}
		})
	}
}

// TestParseRequestBody_ZalgoText tests handling of combining diacritical marks (Zalgo text)
// This tests the fix for CATS fuzzer: ZalgoTextInFields
func TestParseRequestBody_ZalgoText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Zalgo text with multiple combining marks
	zalgoText := "t\u0308\u0308\u0308e\u0308\u0308s\u0308\u0308t\u0308"

	tests := []struct {
		name string
		body string
	}{
		{
			name: "zalgo text in value",
			body: `{"name":"` + zalgoText + `"}`,
		},
		{
			name: "zalgo text in field name",
			body: `{"nam` + zalgoText + `":"test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			type TestStruct struct {
				Name string `json:"name"`
			}

			// Combining marks are valid Unicode in JSON strings
			// Our fix ensures we handle them without panicking
			_, err := ParseRequestBody[TestStruct](c)

			// Should either parse successfully or return proper 400 (not panic with 500)
			if err != nil {
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "Error should be RequestError type")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status, "Should return 400, not 500")
			}
		})
	}
}

// TestParseRequestBody_HangulFiller tests handling of Hangul Filler characters
// This tests the fix for CATS fuzzer: HangulFillerFields
func TestParseRequestBody_HangulFiller(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "Hangul filler in value",
			body: `{"name":"test\u3164"}`, // U+3164 Hangul filler
		},
		{
			name: "Hangul filler in field name",
			body: `{"name\u3164":"test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/test", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			type TestStruct struct {
				Name string `json:"name"`
			}

			// Hangul filler is valid Unicode in JSON strings
			// Our fix ensures we handle it without panicking
			_, err := ParseRequestBody[TestStruct](c)

			// Should either parse successfully or return proper 400 (not panic with 500)
			if err != nil {
				var reqErr *RequestError
				require.True(t, errors.As(err, &reqErr), "Error should be RequestError type")
				assert.Equal(t, http.StatusBadRequest, reqErr.Status, "Should return 400, not 500")
			}
		})
	}
}
