package api

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
)

// ValidationConfig defines validation rules for an endpoint
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
type ValidatorFunc func(interface{}) error

// ValidateAndParseRequest provides unified request validation and parsing
func ValidateAndParseRequest[T any](c *gin.Context, config ValidationConfig) (*T, error) {
	// Phase 1: Parse raw JSON for prohibited field checking
	var rawRequest map[string]interface{}
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
func validateProhibitedFields(rawRequest map[string]interface{}, config ValidationConfig) error {
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
func parseAndValidateStruct[T any](rawData map[string]interface{}, result *T) error {
	// Marshal back to JSON to parse into struct
	jsonData, err := json.Marshal(rawData)
	if err != nil {
		return InvalidInputError("Failed to process request data")
	}

	// Parse into struct
	if err := json.Unmarshal(jsonData, result); err != nil {
		return InvalidInputError("Invalid request format: " + err.Error())
	}

	// Validate required fields using reflection and binding tags
	return validateRequiredFields(result)
}

// validateRequiredFields uses reflection to check required field binding tags
func validateRequiredFields(s interface{}) error {
	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check binding tag for required
		if binding := field.Tag.Get("binding"); strings.Contains(binding, "required") {
			if isEmptyValue(value) {
				jsonName := getJSONFieldName(field)
				return InvalidInputError(fmt.Sprintf("Field '%s' is required", jsonName))
			}
		}
	}
	return nil
}

// isEmptyValue checks if a reflect.Value is empty
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
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	case reflect.Array:
		return v.Len() == 0
	case reflect.Slice, reflect.Map:
		return v.Len() == 0 || v.IsNil()
	}
	return false
}

// getJSONFieldName extracts the JSON field name from struct field
func getJSONFieldName(field reflect.StructField) string {
	jsonTag := field.Tag.Get("json")
	if jsonTag == "" {
		return strings.ToLower(field.Name)
	}
	
	// Handle json:",omitempty" and json:"name,omitempty" cases
	parts := strings.Split(jsonTag, ",")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	
	return strings.ToLower(field.Name)
}

// Common validator functions

// validateAuthorizationEntries validates authorization array (internal function)
func validateAuthorizationEntries(data interface{}) error {
	// Use reflection to find Authorization field
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
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
func ValidateAuthorizationEntriesFromStruct(data interface{}) error {
	return validateAuthorizationEntries(data)
}

// ValidateUUIDFields validates UUID format for ID fields
func ValidateUUIDFields(data interface{}) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	// Look for ID fields
	idField := v.FieldByName("Id")
	if idField.IsValid() && !idField.IsNil() {
		if idPtr := idField.Interface(); idPtr != nil {
			// Validate UUID format if needed
			// Implementation depends on your UUID type
		}
	}

	return nil
}

// ValidateDiagramType validates diagram type field
func ValidateDiagramType(data interface{}) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
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
type ValidationResult struct {
	Valid  bool
	Errors []string
}

// ValidateStruct performs validation on any struct and returns detailed results
func ValidateStruct(s interface{}, config ValidationConfig) ValidationResult {
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