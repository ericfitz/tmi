package api

import (
	"encoding/json"
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
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
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
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
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
		userName     interface{}
		userRole     interface{}
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
				c.Set("userID", tt.userName.(string)+"-provider-id")  // Provider ID for testing
			}
			if tt.userRole != nil {
				c.Set("userRole", tt.userRole)
			}

			// Test the function
			userName, _, userRole, err := ValidateAuthenticatedUser(c)

			if tt.expectError {
				require.Error(t, err)
				reqErr, ok := err.(*RequestError)
				require.True(t, ok, "Expected RequestError")
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
