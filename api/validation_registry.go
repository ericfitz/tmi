package api

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// CommonValidatorRegistry provides a centralized registry of reusable validators
type CommonValidatorRegistry struct {
	validators map[string]ValidatorFunc
}

// NewValidatorRegistry creates a new validator registry with common validators
func NewValidatorRegistry() *CommonValidatorRegistry {
	registry := &CommonValidatorRegistry{
		validators: make(map[string]ValidatorFunc),
	}

	// Register common validators
	registry.Register("authorization", ValidateAuthorizationEntriesFromStruct)
	registry.Register("uuid_fields", ValidateUUIDFieldsFromStruct)
	registry.Register("diagram_type", ValidateDiagramType)
	registry.Register("email_format", ValidateEmailFields)
	registry.Register("url_format", ValidateURLFields)
	registry.Register("threat_severity", ValidateThreatSeverity)
	registry.Register("role_format", ValidateRoleFields)
	registry.Register("metadata_key", ValidateMetadataKey)
	registry.Register("no_html_injection", ValidateNoHTMLInjection)
	registry.Register("string_length", ValidateStringLengths)
	registry.Register("no_duplicates", ValidateNoDuplicateEntries)

	return registry
}

// Register adds a validator to the registry
func (r *CommonValidatorRegistry) Register(name string, validator ValidatorFunc) {
	r.validators[name] = validator
}

// Get retrieves a validator by name
func (r *CommonValidatorRegistry) Get(name string) (ValidatorFunc, bool) {
	validator, exists := r.validators[name]
	return validator, exists
}

// GetValidators returns multiple validators by names
func (r *CommonValidatorRegistry) GetValidators(names []string) []ValidatorFunc {
	var validators []ValidatorFunc
	for _, name := range names {
		if validator, exists := r.validators[name]; exists {
			validators = append(validators, validator)
		}
	}
	return validators
}

// Global validator registry instance
var CommonValidators = NewValidatorRegistry()

// Common Validator Implementations

// ValidateEmailFields validates email format in struct fields
func ValidateEmailFields(data interface{}) error {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	return validateFieldsByPattern(v, "email", func(fieldValue string) error {
		if fieldValue != "" && !emailRegex.MatchString(fieldValue) {
			return InvalidInputError(fmt.Sprintf("Invalid email format: '%s'", fieldValue))
		}
		return nil
	})
}

// ValidateURLFields validates URL format in struct fields
func ValidateURLFields(data interface{}) error {
	urlRegex := regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(\.[a-zA-Z]{2,})?(:\d+)?(/.*)?$`)
	
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	return validateFieldsByPattern(v, "url", func(fieldValue string) error {
		if fieldValue != "" && !urlRegex.MatchString(fieldValue) {
			return InvalidInputError(fmt.Sprintf("Invalid URL format: '%s'. Must be a valid HTTP or HTTPS URL.", fieldValue))
		}
		return nil
	})
}

// ValidateThreatSeverity validates threat severity values
func ValidateThreatSeverity(data interface{}) error {
	validSeverities := map[string]bool{
		"low":      true,
		"medium":   true,
		"high":     true,
		"critical": true,
		"":         true, // Allow empty for optional fields
	}

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	return validateFieldsByPattern(v, "severity", func(fieldValue string) error {
		if !validSeverities[strings.ToLower(fieldValue)] {
			return InvalidInputError(fmt.Sprintf("Invalid severity '%s'. Must be one of: low, medium, high, critical", fieldValue))
		}
		return nil
	})
}

// ValidateRoleFields validates role format in struct fields
func ValidateRoleFields(data interface{}) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	return validateFieldsByPattern(v, "role", func(fieldValue string) error {
		if fieldValue != "" && fieldValue != string(RoleReader) && 
		   fieldValue != string(RoleWriter) && fieldValue != string(RoleOwner) {
			return InvalidInputError(fmt.Sprintf("Invalid role '%s'. Must be one of: reader, writer, owner", fieldValue))
		}
		return nil
	})
}

// ValidateMetadataKey validates metadata key format (no spaces, special chars)
func ValidateMetadataKey(data interface{}) error {
	keyRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	return validateFieldsByPattern(v, "key", func(fieldValue string) error {
		if fieldValue != "" && !keyRegex.MatchString(fieldValue) {
			return InvalidInputError(fmt.Sprintf("Invalid metadata key '%s'. Must contain only letters, numbers, underscores, and hyphens.", fieldValue))
		}
		return nil
	})
}

// ValidateNoHTMLInjection prevents HTML/script injection in text fields
func ValidateNoHTMLInjection(data interface{}) error {
	dangerousPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`),
		regexp.MustCompile(`(?i)<iframe[^>]*>.*?</iframe>`),
		regexp.MustCompile(`(?i)<object[^>]*>.*?</object>`),
		regexp.MustCompile(`(?i)<embed[^>]*>`),
		regexp.MustCompile(`(?i)javascript:`),
		regexp.MustCompile(`(?i)on\w+\s*=`),
	}

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check string fields and pointer to string fields
		var fieldValue string
		if value.Kind() == reflect.String {
			fieldValue = value.String()
		} else if value.Kind() == reflect.Ptr && !value.IsNil() && value.Elem().Kind() == reflect.String {
			fieldValue = value.Elem().String()
		} else {
			continue
		}

		// Check for dangerous patterns
		for _, pattern := range dangerousPatterns {
			if pattern.MatchString(fieldValue) {
				fieldName := getJSONFieldName(field)
				return InvalidInputError(fmt.Sprintf("Field '%s' contains potentially dangerous content", fieldName))
			}
		}
	}

	return nil
}

// ValidateStringLengths validates string field lengths based on struct tags
func ValidateStringLengths(data interface{}) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check for maxlength tag
		if maxLenStr := field.Tag.Get("maxlength"); maxLenStr != "" {
			var maxLen int
			if _, err := fmt.Sscanf(maxLenStr, "%d", &maxLen); err != nil {
				continue
			}

			// Get string value
			var fieldValue string
			if value.Kind() == reflect.String {
				fieldValue = value.String()
			} else if value.Kind() == reflect.Ptr && !value.IsNil() && value.Elem().Kind() == reflect.String {
				fieldValue = value.Elem().String()
			} else {
				continue
			}

			if len(fieldValue) > maxLen {
				fieldName := getJSONFieldName(field)
				return InvalidInputError(fmt.Sprintf("Field '%s' exceeds maximum length of %d characters (current: %d)", fieldName, maxLen, len(fieldValue)))
			}
		}
	}

	return nil
}

// ValidateNoDuplicateEntries validates that slice fields don't contain duplicates
func ValidateNoDuplicateEntries(data interface{}) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check slice fields with "unique" tag
		if field.Tag.Get("unique") == "true" && value.Kind() == reflect.Slice {
			if err := validateUniqueSlice(value, field); err != nil {
				return err
			}
		}
	}

	return nil
}

// Helper Functions

// validateFieldsByPattern applies a validation function to fields matching a pattern
func validateFieldsByPattern(v reflect.Value, fieldPattern string, validationFunc func(string) error) error {
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check if field name contains the pattern (case insensitive)
		fieldName := strings.ToLower(field.Name)
		jsonName := strings.ToLower(getJSONFieldName(field))
		
		if strings.Contains(fieldName, fieldPattern) || strings.Contains(jsonName, fieldPattern) {
			// Get string value
			var fieldValue string
			if value.Kind() == reflect.String {
				fieldValue = value.String()
			} else if value.Kind() == reflect.Ptr && !value.IsNil() && value.Elem().Kind() == reflect.String {
				fieldValue = value.Elem().String()
			} else {
				continue
			}

			if err := validationFunc(fieldValue); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateUniqueSlice checks for duplicates in a slice
func validateUniqueSlice(sliceValue reflect.Value, field reflect.StructField) error {
	if sliceValue.Len() <= 1 {
		return nil
	}

	seen := make(map[interface{}]bool)
	for i := 0; i < sliceValue.Len(); i++ {
		item := sliceValue.Index(i).Interface()
		
		// For structs, use a string representation
		var key interface{}
		if reflect.ValueOf(item).Kind() == reflect.Struct {
			key = fmt.Sprintf("%+v", item)
		} else {
			key = item
		}

		if seen[key] {
			fieldName := getJSONFieldName(field)
			return InvalidInputError(fmt.Sprintf("Field '%s' contains duplicate entries", fieldName))
		}
		seen[key] = true
	}

	return nil
}

// Enhanced UUID validation with better error messages
func ValidateUUIDFieldsFromStruct(data interface{}) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check UUID fields by name or tag
		fieldName := strings.ToLower(field.Name)
		if strings.Contains(fieldName, "id") || field.Tag.Get("format") == "uuid" {
			if value.Kind() == reflect.Ptr && !value.IsNil() {
				// Assume it's a *uuid.UUID or similar
				if uuidValue := value.Interface(); uuidValue != nil {
					if uuidPtr, ok := uuidValue.(*uuid.UUID); ok && uuidPtr != nil {
						if *uuidPtr == uuid.Nil {
							jsonName := getJSONFieldName(field)
							return InvalidInputError(fmt.Sprintf("Field '%s' contains an invalid UUID", jsonName))
						}
					}
				}
			} else if value.Kind() == reflect.String {
				uuidStr := value.String()
				if uuidStr != "" {
					if _, err := uuid.Parse(uuidStr); err != nil {
						jsonName := getJSONFieldName(field)
						return InvalidInputError(fmt.Sprintf("Field '%s' must be a valid UUID format", jsonName))
					}
				}
			}
		}
	}

	return nil
}