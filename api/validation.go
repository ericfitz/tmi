package api

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
)

// ValidationConfig defines validation rules for an endpoint
// SEM@63189587a90229342bc1d25023ec7a515412fb4f: configuration of prohibited fields, custom validators, and operation context for request validation
type ValidationConfig struct {
	// ProhibitedFields lists fields that cannot be set for this operation
	ProhibitedFields []string
	// CustomValidators are additional validation functions to run
	CustomValidators []ValidatorFunc
	// AllowOwnerField permits the owner field (for PUT operations)
	AllowOwnerField bool
	// Operation type for context-specific error messages
	Operation string
}

// ValidatorFunc is a function that validates a parsed request
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: function type for a custom per-struct validation rule
type ValidatorFunc func(any) error

// ValidateAndParseRequest provides unified request validation and parsing
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: parse and validate a JSON request body against prohibited fields, required fields, and custom validators (pure)
func ValidateAndParseRequest[T any](c *gin.Context, config ValidationConfig) (*T, error) {
	// Phase 1: Parse raw JSON for prohibited field checking
	var rawRequest map[string]any
	if err := c.ShouldBindJSON(&rawRequest); err != nil {
		return nil, InvalidInputError("Invalid JSON format: " + err.Error())
	}

	// Phase 2: Check prohibited fields with contextual messages
	if err := validateProhibitedFields(rawRequest, config); err != nil {
		return nil, err
	}

	// Phase 3: Parse into typed struct and validate required fields
	var result T
	if err := parseAndValidateStruct(rawRequest, &result); err != nil {
		return nil, err
	}

	// Phase 4: Run custom validators
	for _, validator := range config.CustomValidators {
		if err := validator(&result); err != nil {
			return nil, err
		}
	}

	return &result, nil
}

// validateProhibitedFields checks for fields that shouldn't be set
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: reject a raw request map that contains any prohibited field names (pure)
func validateProhibitedFields(rawRequest map[string]any, config ValidationConfig) error {
	for _, field := range config.ProhibitedFields {
		// Skip owner field check if explicitly allowed
		if field == "owner" && config.AllowOwnerField {
			continue
		}

		if _, exists := rawRequest[field]; exists {
			message := GetFieldErrorMessage(field, config.Operation)
			return InvalidInputError(fmt.Sprintf(
				"Field '%s' is not allowed in %s requests. %s",
				field, strings.ToUpper(config.Operation), message,
			))
		}
	}
	return nil
}

// parseAndValidateStruct converts raw data to typed struct and validates required fields
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: unmarshal raw JSON data into a typed struct and validate required fields via binding tags (pure)
func parseAndValidateStruct[T any](rawData map[string]any, result *T) error {
	// Marshal back to JSON to parse into struct
	jsonData, err := json.Marshal(rawData)
	if err != nil {
		return InvalidInputError("Failed to process request data")
	}

	// Parse into struct
	if err := json.Unmarshal(jsonData, result); err != nil {
		// Check for UUID parsing errors and provide helpful context
		errMsg := err.Error()
		if strings.Contains(errMsg, "invalid UUID") {
			// Extract field name if possible from error context
			return InvalidInputError(fmt.Sprintf(
				"Invalid UUID format in request. UUIDs must be in standard format (e.g., '550e8400-e29b-41d4-a716-446655440000'). "+
					"If a UUID field is optional, omit it or send null instead of invalid values. Error: %s",
				errMsg,
			))
		}
		return InvalidInputError("Invalid request format: " + err.Error())
	}

	// Validate required fields using reflection and binding tags
	return validateRequiredFields(result)
}

// validateRequiredFields uses reflection to check required field binding tags
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: check all struct fields tagged binding:required are non-empty and return a descriptive error if any are missing (pure)
func validateRequiredFields(s any) error {
	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	t := v.Type()

	var missingFields []string

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check binding tag for required
		if binding := field.Tag.Get("binding"); strings.Contains(binding, "required") {
			if isEmptyValue(value) {
				jsonName := getJSONFieldName(field)
				missingFields = append(missingFields, jsonName)
			}
		}
	}

	// Return enhanced error message for missing fields
	if len(missingFields) > 0 {
		return createRequiredFieldError(missingFields)
	}

	return nil
}

// createRequiredFieldError creates contextual error messages for missing required fields
// SEM@4fa1d37f07fd36467dea01526b97410523474271: build a human-readable error message listing one or more missing required field names (pure)
func createRequiredFieldError(missingFields []string) error {
	if len(missingFields) == 1 {
		fieldName := missingFields[0]
		contextualMessage := getRequiredFieldContext(fieldName)
		if contextualMessage != "" {
			return InvalidInputError(fmt.Sprintf("Field '%s' is required. %s", fieldName, contextualMessage))
		}
		return InvalidInputError(fmt.Sprintf("Field '%s' is required", fieldName))
	}

	// Multiple missing fields
	if len(missingFields) == 2 {
		return InvalidInputError(fmt.Sprintf("Fields '%s' and '%s' are required", missingFields[0], missingFields[1]))
	}

	// More than 2 missing fields
	lastField := missingFields[len(missingFields)-1]
	otherFields := strings.Join(missingFields[:len(missingFields)-1], "', '")
	return InvalidInputError(fmt.Sprintf("Fields '%s' and '%s' are required", otherFields, lastField))
}

// getRequiredFieldContext provides contextual help for specific required fields
// SEM@4fa1d37f07fd36467dea01526b97410523474271: return a contextual hint string for a known required field name (pure)
func getRequiredFieldContext(fieldName string) string {
	contextMap := map[string]string{
		"name":        "This field identifies the resource and must be provided.",
		"email":       "A valid email address is required for user identification.",
		"type":        "The type field specifies the format and must be provided.",
		"key":         "The key identifies this metadata entry and cannot be empty.",
		"value":       "The value contains the metadata content and cannot be empty.",
		"subject":     "The subject identifies the user or service being authorized.",
		"role":        "The role defines the level of access (reader, writer, or owner).",
		"url":         "A valid URL is required to reference the external resource.",
		"severity":    "The severity level helps prioritize threat response.",
		"description": "A description helps others understand the purpose and context.",
	}

	return contextMap[fieldName]
}

// isEmptyValue checks if a reflect.Value is empty
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: report whether a reflected value is the zero or nil value for its kind (pure)
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	case reflect.Array:
		return v.Len() == 0
	case reflect.Slice, reflect.Map:
		return v.Len() == 0 || v.IsNil()
	}
	return false
}

// getJSONFieldName extracts the JSON field name from struct field
// SEM@44e3609f57929c6c53fe68bbc7343fcc11348adb: extract the JSON property name from a struct field's json tag (pure)
func getJSONFieldName(field reflect.StructField) string {
	jsonTag := field.Tag.Get("json")
	if jsonTag == "" {
		return strings.ToLower(field.Name)
	}

	// Handle json:",omitempty" and json:"name,omitempty" cases
	parts := strings.Split(jsonTag, ",")
	if len(parts) > 0 && parts[0] != "" && parts[0] != "-" {
		return parts[0]
	}

	return strings.ToLower(field.Name)
}

// Common validator functions

// validateAuthorizationEntries validates authorization array (internal function)
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: validate the Authorization slice on a struct via reflection, checking entry format (pure)
func validateAuthorizationEntries(data any) error {
	// Use reflection to find Authorization field
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	// Look for Authorization field
	authField := v.FieldByName("Authorization")
	if !authField.IsValid() {
		return nil // No authorization field to validate
	}

	// Convert to []Authorization
	if authField.Kind() == reflect.Slice {
		authSlice := authField.Interface()
		if authList, ok := authSlice.([]Authorization); ok {
			return ValidateAuthorizationEntriesWithFormat(authList)
		}
	}

	return nil
}

// ValidateAuthorizationEntriesFromStruct is the public wrapper for the validator
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: public wrapper that validates authorization entries on an arbitrary struct (pure)
func ValidateAuthorizationEntriesFromStruct(data any) error {
	return validateAuthorizationEntries(data)
}

// ValidateDiagramType validates diagram type field
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: validate that a struct's Type field equals the only supported diagram type DFD-1.0.0 (pure)
func ValidateDiagramType(data any) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	typeField := v.FieldByName("Type")
	if typeField.IsValid() && typeField.Kind() == reflect.String {
		diagType := typeField.String()
		if diagType != "" && diagType != string(DfdDiagramTypeDFD100) {
			return InvalidInputError(fmt.Sprintf("Invalid diagram type '%s'. Must be 'DFD-1.0.0'", diagType))
		}
	}

	return nil
}

// ValidationResult provides validation outcome details
// SEM@63189587a90229342bc1d25023ec7a515412fb4f: outcome of a struct validation run with a validity flag and list of error messages
type ValidationResult struct {
	Valid  bool
	Errors []string
}

// ValidateStruct performs validation on any struct and returns detailed results
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: run custom validators and required-field checks on a struct and return aggregated validation results (pure)
func ValidateStruct(s any, config ValidationConfig) ValidationResult {
	result := ValidationResult{Valid: true, Errors: []string{}}

	// Run custom validators
	for _, validator := range config.CustomValidators {
		if err := validator(s); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}

	// Validate required fields
	if err := validateRequiredFields(s); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	}

	return result
}
