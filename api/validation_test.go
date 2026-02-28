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
		requestBody := map[string]any{
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
		requestBody := map[string]any{
			"description": "Test Description",
			// Missing "name" and "email"
		}

		c, _ := createTestContext(requestBody)
		result, err := ValidateAndParseRequest[TestCreateRequest](c, testConfig)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Fields 'name' and 'email' are required")
	})

	t.Run("Prohibited Field in POST", func(t *testing.T) {
		requestBody := map[string]any{
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
		requestBody := map[string]any{
			"name":       "Test Name",
			"email":      "test@example.com",
			"created_at": "2023-01-01T00:00:00Z",
		}

		c, _ := createTestContext(requestBody)
		result, err := ValidateAndParseRequest[TestCreateRequest](c, testConfig)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Field 'created_at' is not allowed in POST requests")
		assert.Contains(t, err.Error(), "set by the server")
	})

	t.Run("Owner Allowed in PUT", func(t *testing.T) {
		requestBody := map[string]any{
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
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test", ProviderId: "user1@example.com", Role: RoleReader},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test", ProviderId: "user2@example.com", Role: RoleWriter},
			},
		}

		err := ValidateAuthorizationEntriesFromStruct(&testStruct)
		assert.NoError(t, err)
	})

	t.Run("Invalid Authorization Role", func(t *testing.T) {
		testStruct := TestAuthRequest{
			Name: "Test",
			Authorization: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test", ProviderId: "user1@example.com", Role: "invalid_role"},
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
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test", ProviderId: "", Role: RoleReader},
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
			func(data any) error {
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
		value    any
		expected bool
	}{
		{"Empty String", "", true},
		{"Non-Empty String", "test", false},
		{"Zero Int", 0, true},
		{"Non-Zero Int", 42, false},
		{"False Bool", false, true},
		{"True Bool", true, false},
		{"Nil Pointer", (*string)(nil), true},
		{"Non-Nil Pointer", new("test"), false},
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

func createTestContext(body map[string]any) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	jsonBody, _ := json.Marshal(body)
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	return c, w
}

func TestValidateNoteMarkdown(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		description string
	}{
		{
			name:        "Valid Markdown with headings",
			content:     "# Heading 1\n## Heading 2\n### Heading 3",
			expectError: false,
			description: "Standard Markdown headings should be allowed",
		},
		{
			name:        "Valid Markdown with code block containing JSON",
			content:     "```json\n{\"option_key_1\": true, \"option_key_2\": \"value\"}\n```",
			expectError: false,
			description: "JSON in code blocks should not trigger false positives",
		},
		{
			name:        "Valid Markdown with inline code",
			content:     "Use the `onclick` handler in your code",
			expectError: false,
			description: "Code references to event handlers should be allowed",
		},
		{
			name: "Valid Markdown with complex example from user",
			content: `# Heading 1
## Heading 2
### Heading 3
#### Heading 4
##### Heading 5
###### Heading 6

## Text Styles
__Bold__ or **Bold**
_Italicized_ or *Italicized*
~Strikethrough~ or ~~Strikethrough~~
> Quoted text

_Note: superscripts, subscripts, underlining, and color are not supported_

## Unordered (bullet) list:
- item
- item
    - sub-item
    - sub-item
- item

## Ordered (numbered) list:

## Task list:
- [x] Completed task
- [ ] Task 2
- [ ] Task 3

## Hyperlinks in a numbered (ordered) list:
- [Local (heading 1)](#heading-1)
- [External (tmi.dev)](https://www.tmi.dev)

## Code

Inline ` + "`code`" + ` reference

Code block with syntax highlighting:
` + "```python" + `
import argparse
import json
import yaml
from collections import defaultdict
from pathlib import Path
from typing import Dict, List, Tuple, Any, Set


class LocalizationDeDuplicator:
    def __init__(self, locale_file_path: str, skip_policy: bool = False):
        self.locale_file_path = Path(locale_file_path)
        self.skip_policy = skip_policy
        self.localization_data = {}
        self.key_value_map = {}  # Full path key -> value
        self.value_to_keys = defaultdict(list)  # Value -> list of full path keys
        self.dedup_plan = []

    def load_localization_file(self):
        """Load the localization JSON file."""
        with open(self.locale_file_path, 'r', encoding='utf-8') as f:
            self.localization_data = json.load(f)
` + "```" + ``,
			expectError: false,
			description: "Complex real-world Markdown example should be allowed",
		},
		{
			name:        "HTML script tag allowed (sanitized in handler)",
			content:     "This is a note with <script>alert('xss')</script> dangerous content",
			expectError: false,
			description: "Script tags pass validation; sanitized by bluemonday in the handler layer",
		},
		{
			name:        "HTML with onclick handler allowed (sanitized in handler)",
			content:     "Click <a href='#' onclick='alert(1)'>here</a>",
			expectError: false,
			description: "HTML with event handlers passes validation; sanitized by bluemonday in the handler layer",
		},
		{
			name:        "Iframe tag allowed (sanitized in handler)",
			content:     "Embedded content: <iframe src='http://evil.com'></iframe>",
			expectError: false,
			description: "Iframe tags pass validation; sanitized by bluemonday in the handler layer",
		},
		{
			name:        "HTML img tag allowed (sanitized in handler)",
			content:     "Image: <img src='x' onerror='alert(1)'>",
			expectError: false,
			description: "HTML img tags pass validation; sanitized by bluemonday in the handler layer",
		},
		{
			name:        "Valid Markdown with link",
			content:     "Check out [this link](https://example.com)",
			expectError: false,
			description: "Markdown links should be allowed",
		},
		{
			name:        "Valid Markdown with image",
			content:     "![alt text](https://example.com/image.png)",
			expectError: false,
			description: "Markdown images should be allowed",
		},
		{
			name:        "Valid empty content",
			content:     "",
			expectError: false,
			description: "Empty content should pass validation (required validation is separate)",
		},
		{
			name:        "HTML paragraph tag allowed (sanitized in handler)",
			content:     "<p>This is HTML</p>",
			expectError: false,
			description: "HTML paragraph tags pass validation; sanitized by bluemonday in the handler layer",
		},
		{
			name:        "HTML div tag allowed (sanitized in handler)",
			content:     "<div class='container'>Content</div>",
			expectError: false,
			description: "HTML div tags pass validation; sanitized by bluemonday in the handler layer",
		},
		{
			name:        "Valid Markdown with special characters",
			content:     "Special chars: & < > \" ' are allowed in plain text",
			expectError: false,
			description: "Special characters in plain text should be allowed",
		},
		{
			name:        "Template expression rejected",
			content:     "Hello {{ user }} world",
			expectError: true,
			description: "Template expressions should be rejected",
		},
		{
			name:        "Template expression in code block allowed",
			content:     "```\n{{ user }}\n```",
			expectError: false,
			description: "Template expressions in code blocks should be allowed",
		},
		{
			name:        "Template expression in inline code allowed",
			content:     "Use `{{ template }}` syntax",
			expectError: false,
			description: "Template expressions in inline code should be allowed",
		},
		{
			name:        "JavaScript template literal rejected",
			content:     "Hello ${ name } world",
			expectError: true,
			description: "JavaScript template interpolation should be rejected",
		},
		{
			name:        "Server template tag rejected",
			content:     "Hello <% code %> world",
			expectError: true,
			description: "Server template tags should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test Note
			note := &Note{
				Content: tt.content,
				Name:    "Test Note",
			}

			// Run validation
			err := ValidateNoteMarkdown(note)

			if tt.expectError {
				assert.Error(t, err, tt.description)
				if err != nil {
					assert.Contains(t, err.Error(), "unsafe", "Error should mention unsafe content")
				}
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

func TestValidateNoteMarkdown_NonNoteType(t *testing.T) {
	// Test that non-Note types are skipped
	testStruct := struct {
		Content string
	}{
		Content: "<script>alert('xss')</script>",
	}

	err := ValidateNoteMarkdown(&testStruct)
	assert.NoError(t, err, "Non-Note types should be skipped by ValidateNoteMarkdown")
}
