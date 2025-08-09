package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test request structs
type TestCreateRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description,omitempty"`
	Email       string  `json:"email" binding:"required"`
}

type TestUpdateRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description,omitempty"`
	Owner       string  `json:"owner,omitempty"`
}

type TestAuthRequest struct {
	Name          string          `json:"name" binding:"required"`
	Authorization []Authorization `json:"authorization,omitempty"`
}

func TestValidateAndParseRequest(t *testing.T) {
	// Setup test config
	testConfig := ValidationConfig{
		ProhibitedFields: []string{"id", "created_at", "owner"},
		CustomValidators: []ValidatorFunc{},
		Operation:        "POST",
	}

	testConfigWithOwner := ValidationConfig{
		ProhibitedFields: []string{"id", "created_at"},
		AllowOwnerField:  true,
		Operation:        "PUT",
	}

	t.Run("Valid Request", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name":        "Test Name",
			"description": "Test Description",
			"email":       "test@example.com",
		}

		c, _ := createTestContext(requestBody)
		result, err := ValidateAndParseRequest[TestCreateRequest](c, testConfig)

		require.NoError(t, err)
		assert.Equal(t, "Test Name", result.Name)
		assert.Equal(t, "test@example.com", result.Email)
		assert.NotNil(t, result.Description)
		assert.Equal(t, "Test Description", *result.Description)
	})

	t.Run("Missing Required Field", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"description": "Test Description",
			// Missing "name" and "email"
		}

		c, _ := createTestContext(requestBody)
		result, err := ValidateAndParseRequest[TestCreateRequest](c, testConfig)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Field 'name' is required")
	})

	t.Run("Prohibited Field in POST", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name":  "Test Name",
			"email": "test@example.com",
			"owner": "prohibited@example.com",
		}

		c, _ := createTestContext(requestBody)
		result, err := ValidateAndParseRequest[TestCreateRequest](c, testConfig)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Field 'owner' is not allowed in POST requests")
		assert.Contains(t, err.Error(), "set automatically to the authenticated user")
	})

	t.Run("Prohibited Timestamp Field", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name":       "Test Name",
			"email":      "test@example.com",
			"created_at": "2023-01-01T00:00:00Z",
		}

		c, _ := createTestContext(requestBody)
		result, err := ValidateAndParseRequest[TestCreateRequest](c, testConfig)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Field 'created_at' is not allowed in POST requests")
		assert.Contains(t, err.Error(), "set automatically by the server")
	})

	t.Run("Owner Allowed in PUT", func(t *testing.T) {
		requestBody := map[string]interface{}{
			"name":  "Updated Name",
			"owner": "newowner@example.com",
		}

		c, _ := createTestContext(requestBody)
		result, err := ValidateAndParseRequest[TestUpdateRequest](c, testConfigWithOwner)

		require.NoError(t, err)
		assert.Equal(t, "Updated Name", result.Name)
		assert.Equal(t, "newowner@example.com", result.Owner)
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString("{invalid json"))
		c.Request.Header.Set("Content-Type", "application/json")

		result, err := ValidateAndParseRequest[TestCreateRequest](c, testConfig)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Invalid JSON format")
	})
}

func TestFieldErrorMessages(t *testing.T) {
	tests := []struct {
		field     string
		operation string
		expected  string
	}{
		{
			field:     "owner",
			operation: "post",
			expected:  "The owner field is set automatically to the authenticated user during creation.",
		},
		{
			field:     "owner",
			operation: "put",
			expected:  "Owner can only be changed by the current owner.",
		},
		{
			field:     "id",
			operation: "post",
			expected:  "The ID is read-only and set by the server.",
		},
		{
			field:     "unknown_field",
			operation: "post",
			expected:  "This field cannot be set directly.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.field+"_"+tt.operation, func(t *testing.T) {
			message := GetFieldErrorMessage(tt.field, tt.operation)
			assert.Equal(t, tt.expected, message)
		})
	}
}

func TestValidationConfigs(t *testing.T) {
	t.Run("Get Existing Config", func(t *testing.T) {
		config, exists := GetValidationConfig("threat_model_create")
		assert.True(t, exists)
		assert.Contains(t, config.ProhibitedFields, "owner")
		assert.Contains(t, config.ProhibitedFields, "id")
		assert.Equal(t, "POST", config.Operation)
		assert.False(t, config.AllowOwnerField)
	})

	t.Run("Get Non-Existing Config", func(t *testing.T) {
		config, exists := GetValidationConfig("non_existing_endpoint")
		assert.False(t, exists)
		assert.Empty(t, config.ProhibitedFields)
	})

	t.Run("Threat Model Update Allows Owner", func(t *testing.T) {
		config, exists := GetValidationConfig("threat_model_update")
		assert.True(t, exists)
		assert.True(t, config.AllowOwnerField)
		assert.Equal(t, "PUT", config.Operation)
	})
}

func TestValidateRequiredFields(t *testing.T) {
	t.Run("All Required Fields Present", func(t *testing.T) {
		testStruct := TestCreateRequest{
			Name:  "Test Name",
			Email: "test@example.com",
		}

		err := validateRequiredFields(&testStruct)
		assert.NoError(t, err)
	})

	t.Run("Missing Required Field", func(t *testing.T) {
		testStruct := TestCreateRequest{
			Name: "Test Name",
			// Email is missing
		}

		err := validateRequiredFields(&testStruct)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Field 'email' is required")
	})

	t.Run("Empty String Required Field", func(t *testing.T) {
		testStruct := TestCreateRequest{
			Name:  "", // Empty string
			Email: "test@example.com",
		}

		err := validateRequiredFields(&testStruct)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Field 'name' is required")
	})
}

func TestValidateAuthorizationEntriesFromStruct(t *testing.T) {
	t.Run("Valid Authorization Entries", func(t *testing.T) {
		testStruct := TestAuthRequest{
			Name: "Test",
			Authorization: []Authorization{
				{Subject: "user1@example.com", Role: RoleReader},
				{Subject: "user2@example.com", Role: RoleWriter},
			},
		}

		err := ValidateAuthorizationEntriesFromStruct(&testStruct)
		assert.NoError(t, err)
	})

	t.Run("Invalid Authorization Role", func(t *testing.T) {
		testStruct := TestAuthRequest{
			Name: "Test",
			Authorization: []Authorization{
				{Subject: "user1@example.com", Role: "invalid_role"},
			},
		}

		err := ValidateAuthorizationEntriesFromStruct(&testStruct)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Invalid role")
	})

	t.Run("Empty Subject", func(t *testing.T) {
		testStruct := TestAuthRequest{
			Name: "Test",
			Authorization: []Authorization{
				{Subject: "", Role: RoleReader},
			},
		}

		err := ValidateAuthorizationEntriesFromStruct(&testStruct)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "subject")
	})

	t.Run("No Authorization Field", func(t *testing.T) {
		testStruct := TestCreateRequest{
			Name:  "Test",
			Email: "test@example.com",
		}

		err := ValidateAuthorizationEntriesFromStruct(&testStruct)
		assert.NoError(t, err) // Should not error if no Authorization field
	})
}

func TestValidateStruct(t *testing.T) {
	config := ValidationConfig{
		CustomValidators: []ValidatorFunc{
			func(data interface{}) error {
				// Custom validator that fails if name contains "invalid"
				if req, ok := data.(*TestCreateRequest); ok {
					if req.Name == "invalid" {
						return InvalidInputError("Name cannot be 'invalid'")
					}
				}
				return nil
			},
		},
	}

	t.Run("Valid Struct", func(t *testing.T) {
		testStruct := TestCreateRequest{
			Name:  "Valid Name",
			Email: "test@example.com",
		}

		result := ValidateStruct(&testStruct, config)
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
	})

	t.Run("Struct Fails Custom Validation", func(t *testing.T) {
		testStruct := TestCreateRequest{
			Name:  "invalid",
			Email: "test@example.com",
		}

		result := ValidateStruct(&testStruct, config)
		assert.False(t, result.Valid)
		assert.Len(t, result.Errors, 1)
		assert.Contains(t, result.Errors[0], "Name cannot be 'invalid'")
	})

	t.Run("Struct Fails Required Field Validation", func(t *testing.T) {
		testStruct := TestCreateRequest{
			Name: "Valid Name",
			// Email missing
		}

		result := ValidateStruct(&testStruct, config)
		assert.False(t, result.Valid)
		assert.Len(t, result.Errors, 1)
		assert.Contains(t, result.Errors[0], "Field 'email' is required")
	})
}

func TestIsEmptyValue(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected bool
	}{
		{"Empty String", "", true},
		{"Non-Empty String", "test", false},
		{"Zero Int", 0, true},
		{"Non-Zero Int", 42, false},
		{"False Bool", false, true},
		{"True Bool", true, false},
		{"Nil Pointer", (*string)(nil), true},
		{"Non-Nil Pointer", validationStringPtr("test"), false},
		{"Empty Slice", []string{}, true},
		{"Non-Empty Slice", []string{"test"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := reflect.ValueOf(tt.value)
			result := isEmptyValue(v)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetJSONFieldName(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		field    string
		expected string
	}{
		{"Simple JSON tag", `json:"test_field"`, "TestField", "test_field"},
		{"JSON tag with omitempty", `json:"test_field,omitempty"`, "TestField", "test_field"},
		{"Empty JSON tag", `json:""`, "TestField", "testfield"},
		{"No JSON tag", "", "TestField", "testfield"},
		{"JSON tag with dash", `json:"-"`, "TestField", "testfield"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := reflect.StructField{
				Name: tt.field,
				Tag:  reflect.StructTag(tt.tag),
			}
			result := getJSONFieldName(field)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper functions

func createTestContext(body map[string]interface{}) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	jsonBody, _ := json.Marshal(body)
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	return c, w
}

func validationStringPtr(s string) *string {
	return &s
}
