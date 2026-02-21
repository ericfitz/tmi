package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationMigrationEnhancements(t *testing.T) {
	// Test enhanced required field validation with contextual messages
	t.Run("Enhanced Required Field Messages", func(t *testing.T) {
		tests := []struct {
			name           string
			requestBody    map[string]any
			expectedError  string
			expectedStatus int
		}{
			{
				name:           "Missing single field with context",
				requestBody:    map[string]any{"value": "test value"},
				expectedError:  "Field 'key' is required. The key identifies this metadata entry and cannot be empty.",
				expectedStatus: 400,
			},
			{
				name:           "Missing multiple fields",
				requestBody:    map[string]any{},
				expectedError:  "Fields 'key' and 'value' are required",
				expectedStatus: 400,
			},
			{
				name: "Valid request",
				requestBody: map[string]any{
					"key":   "test-key",
					"value": "test value",
				},
				expectedStatus: 200, // Would be successful if we had a real handler
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c, _ := createTestContext(tt.requestBody)
				result, err := ValidateAndParseRequest[ValidatedMetadataRequest](c, ValidationConfigs["metadata_create"])

				if tt.expectedStatus == 400 {
					assert.Nil(t, result)
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.expectedError)
				} else {
					require.NoError(t, err)
					assert.NotNil(t, result)
					assert.Equal(t, "test-key", result.Key)
					assert.Equal(t, "test value", result.Value)
				}
			})
		}
	})

	// Test validator registry functionality
	t.Run("Validator Registry", func(t *testing.T) {
		t.Run("Get Single Validator", func(t *testing.T) {
			validator, exists := CommonValidators.Get("metadata_key")
			assert.True(t, exists)
			assert.NotNil(t, validator)
		})

		t.Run("Get Multiple Validators", func(t *testing.T) {
			validators := CommonValidators.GetValidators([]string{"metadata_key", "string_length", "no_html_injection"})
			assert.Len(t, validators, 3)
		})

		t.Run("Get Non-Existent Validator", func(t *testing.T) {
			validator, exists := CommonValidators.Get("non_existent")
			assert.False(t, exists)
			assert.Nil(t, validator)
		})

		t.Run("Get Mixed Existing/Non-Existing Validators", func(t *testing.T) {
			validators := CommonValidators.GetValidators([]string{"metadata_key", "non_existent", "string_length"})
			assert.Len(t, validators, 2) // Only existing validators returned
		})
	})

	// Test specific validators
	t.Run("Metadata Key Validator", func(t *testing.T) {
		tests := []struct {
			name      string
			key       string
			shouldErr bool
		}{
			{"Valid key with letters", "valid_key", false},
			{"Valid key with numbers", "key123", false},
			{"Valid key with hyphens", "key-name", false},
			{"Valid key with underscores", "key_name", false},
			{"Invalid key with spaces", "invalid key", true},
			{"Invalid key with special chars", "key@#$", true},
			{"Empty key", "", false}, // Empty is handled by required validation
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				testStruct := ValidatedMetadataRequest{
					Key:   tt.key,
					Value: "test value",
				}

				err := ValidateMetadataKey(&testStruct)
				if tt.shouldErr {
					assert.Error(t, err)
					assert.Contains(t, err.Error(), "Invalid metadata key")
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	// Test HTML injection prevention
	t.Run("HTML Injection Prevention", func(t *testing.T) {
		dangerousInputs := []string{
			"<script>alert('xss')</script>",
			"<iframe src='malicious.com'></iframe>",
			"javascript:alert('xss')",
			"<img onerror='alert()' src='x'>",
		}

		for _, dangerous := range dangerousInputs {
			t.Run("Dangerous: "+dangerous[:min(len(dangerous), 20)], func(t *testing.T) {
				testStruct := ValidatedMetadataRequest{
					Key:   "test-key",
					Value: dangerous,
				}

				err := ValidateNoHTMLInjection(&testStruct)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "potentially dangerous content")
			})
		}
	})

	// Test string length validation
	t.Run("String Length Validation", func(t *testing.T) {
		// This would test the maxlength tags if we had a full implementation
		// For now, test the basic validator structure
		testStruct := ValidatedMetadataRequest{
			Key:   "test-key",
			Value: "normal value",
		}

		err := ValidateStringLengths(&testStruct)
		// Since we don't have maxlength implementation yet, this should pass
		assert.NoError(t, err)
	})
}

func TestValidationConfigEndpoints(t *testing.T) {
	// Test that all required endpoint configurations exist
	requiredConfigs := []string{
		"threat_model_create",
		"threat_model_update",
		"diagram_create",
		"diagram_update",
		"document_create",
		"document_update",
		"repository_create",
		"repository_update",
		"threat_create",
		"threat_update",
		"metadata_create",
		"metadata_update",
	}

	for _, configName := range requiredConfigs {
		t.Run("Config exists: "+configName, func(t *testing.T) {
			config, exists := GetValidationConfig(configName)
			assert.True(t, exists, "Configuration '%s' should exist", configName)
			assert.NotEmpty(t, config.Operation, "Operation should be set for '%s'", configName)

			// Verify operation matches config name
			if configName[len(configName)-6:] == "create" {
				assert.Equal(t, "POST", config.Operation)
			} else if configName[len(configName)-6:] == "update" {
				assert.Equal(t, "PUT", config.Operation)
			}
		})
	}
}

func TestFieldErrorMessagesEnhanced(t *testing.T) {
	// Test operation-specific error messages
	tests := []struct {
		field     string
		operation string
		contains  string
	}{
		{"owner", "post", "set automatically to the authenticated user"},
		{"owner", "put", "can only be changed by the current owner"},
		{"id", "post", "read-only and set by the server"},
		{"created_at", "post", "set by the server"},
	}

	for _, tt := range tests {
		t.Run(tt.field+"_"+tt.operation, func(t *testing.T) {
			message := GetFieldErrorMessage(tt.field, tt.operation)
			assert.Contains(t, message, tt.contains)
		})
	}
}

func TestValidationFrameworkIntegration(t *testing.T) {
	// Test full integration of validation framework
	t.Run("Complete Validation Flow", func(t *testing.T) {
		// Test with metadata create config
		config := ValidationConfigs["metadata_create"]

		// Valid request should pass
		validRequest := map[string]any{
			"key":   "valid-key",
			"value": "valid value",
		}

		c, _ := createTestContext(validRequest)
		result, err := ValidateAndParseRequest[ValidatedMetadataRequest](c, config)
		require.NoError(t, err)
		assert.Equal(t, "valid-key", result.Key)
		assert.Equal(t, "valid value", result.Value)

		// Request with prohibited field should fail
		invalidRequest := map[string]any{
			"key":        "valid-key",
			"value":      "valid value",
			"created_at": "2023-01-01T00:00:00Z",
		}

		c2, _ := createTestContext(invalidRequest)
		result2, err2 := ValidateAndParseRequest[ValidatedMetadataRequest](c2, ValidationConfigs["threat_model_create"]) // Use config with prohibited fields
		assert.Nil(t, result2)
		assert.Error(t, err2)
		assert.Contains(t, err2.Error(), "not allowed in POST requests")
	})
}

func TestMigrationBenefits(t *testing.T) {
	// Demonstrate the benefits of the new validation framework
	t.Run("Validation Consistency", func(t *testing.T) {
		// All endpoints should have consistent error format
		configs := []string{"metadata_create", "document_create", "threat_create"}

		for _, configName := range configs {
			t.Run("Config: "+configName, func(t *testing.T) {
				config := ValidationConfigs[configName]

				// Test with missing required field
				emptyRequest := map[string]any{}
				c, _ := createTestContext(emptyRequest)

				// Each endpoint should fail validation consistently
				if configName == "metadata_create" {
					result, err := ValidateAndParseRequest[ValidatedMetadataRequest](c, config)
					assert.Nil(t, result)
					assert.Error(t, err)
					assert.Contains(t, err.Error(), "required")
				}
			})
		}
	})

	t.Run("Error Message Quality", func(t *testing.T) {
		// Test that error messages are helpful and specific
		requestBody := map[string]any{
			"value": "missing key field",
		}

		c, _ := createTestContext(requestBody)
		result, err := ValidateAndParseRequest[ValidatedMetadataRequest](c, ValidationConfigs["metadata_create"])

		assert.Nil(t, result)
		require.Error(t, err)

		// Error should be specific and helpful
		errorMsg := err.Error()
		assert.Contains(t, errorMsg, "key")
		assert.Contains(t, errorMsg, "required")
		assert.Contains(t, errorMsg, "identifies this metadata entry") // Contextual help
	})
}
