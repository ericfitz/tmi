package api

import (
	"fmt"
	"math"
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
	registry.Register("note_markdown", ValidateNoteMarkdown)
	registry.Register("triage_note_markdown", ValidateTriageNoteMarkdown)
	registry.Register("string_length", ValidateStringLengths)
	registry.Register("no_duplicates", ValidateNoDuplicateEntries)
	registry.Register("score_precision", ValidateScorePrecision)

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
func ValidateEmailFields(data any) error {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
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
func ValidateURLFields(data any) error {
	urlRegex := regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(\.[a-zA-Z]{2,})?(:\d+)?(/.*)?$`)

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	return validateFieldsByPattern(v, "url", func(fieldValue string) error {
		if fieldValue != "" && !urlRegex.MatchString(fieldValue) {
			return InvalidInputError(fmt.Sprintf("Invalid URL format: '%s'. Must be a valid HTTP or HTTPS URL.", fieldValue))
		}
		return nil
	})
}

// ValidateThreatSeverity is a no-op validator that accepts any severity value
// Severity is now a free-form string field per the OpenAPI schema
func ValidateThreatSeverity(data any) error {
	// No validation needed - severity is a free-form string field
	return nil
}

// ValidateRoleFields validates role format in struct fields
func ValidateRoleFields(data any) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
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
func ValidateMetadataKey(data any) error {
	keyRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	return validateFieldsByPattern(v, "key", func(fieldValue string) error {
		if fieldValue != "" && !keyRegex.MatchString(fieldValue) {
			return InvalidInputError(fmt.Sprintf("Invalid metadata key '%s'. Must contain only letters, numbers, underscores, and hyphens.", fieldValue))
		}
		return nil
	})
}

// ValidateNoHTMLInjection prevents HTML/script injection in text fields.
// Uses the unified CheckHTMLInjection checker for consistent pattern coverage.
func ValidateNoHTMLInjection(data any) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check string fields and pointer to string fields
		var fieldValue string
		switch {
		case value.Kind() == reflect.String:
			fieldValue = value.String()
		case value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.String:
			fieldValue = value.Elem().String()
		default:
			continue
		}

		fieldName := getJSONFieldName(field)
		if err := CheckHTMLInjection(fieldValue, fieldName); err != nil {
			// Preserve the original error message format for backward compatibility
			return InvalidInputError(fmt.Sprintf("Field '%s' contains potentially dangerous content", fieldName))
		}
	}

	return nil
}

// ValidateMarkdownContent validates a markdown content string for dangerous HTML.
// It strips Markdown code blocks first, then checks remaining content for HTML tags.
// This prevents false positives from code examples while still blocking actual HTML.
// This is the shared validation core used by both Note and TriageNote validators.
func ValidateMarkdownContent(content string) error {
	// Remove code blocks (both ``` and indented) to avoid false positives
	// This regex removes fenced code blocks (```...```)
	codeBlockRegex := regexp.MustCompile("(?s)```[^`]*```")
	contentWithoutCodeBlocks := codeBlockRegex.ReplaceAllString(content, "")

	// Also remove inline code (`...`)
	inlineCodeRegex := regexp.MustCompile("`[^`]+`")
	contentWithoutCode := inlineCodeRegex.ReplaceAllString(contentWithoutCodeBlocks, "")

	// Now check for HTML tags in the remaining content
	// Match HTML tags: < followed by letter/slash, then tag content, then >
	// This avoids false positives from math expressions like "x < y > z"
	htmlTagRegex := regexp.MustCompile("<[a-zA-Z/][^>]*>")
	if htmlTagRegex.MatchString(contentWithoutCode) {
		return InvalidInputError("Field 'content' contains HTML tags. Only Markdown formatting is allowed")
	}

	return nil
}

// ValidateNoteMarkdown validates Note.Content field for dangerous HTML.
// This validator is specifically designed for Note objects that contain Markdown content.
func ValidateNoteMarkdown(data any) error {
	note, ok := data.(*Note)
	if !ok {
		return nil // Skip validation for non-Note types
	}
	return ValidateMarkdownContent(note.Content)
}

// ValidateTriageNoteMarkdown validates TriageNote.Content field for dangerous HTML.
// This validator uses the same shared markdown validation as Note objects.
func ValidateTriageNoteMarkdown(data any) error {
	triageNote, ok := data.(*TriageNote)
	if !ok {
		return nil // Skip validation for non-TriageNote types
	}
	return ValidateMarkdownContent(triageNote.Content)
}

// ValidateStringLengths validates string field lengths based on struct tags
func ValidateStringLengths(data any) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
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
			switch {
			case value.Kind() == reflect.String:
				fieldValue = value.String()
			case value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.String:
				fieldValue = value.Elem().String()
			default:
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
func ValidateNoDuplicateEntries(data any) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
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

// ValidateScorePrecision validates that score fields have at most 1 decimal place
// This matches the OpenAPI spec constraint: multipleOf: 0.1
func ValidateScorePrecision(data any) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check for "score" field by name (case insensitive)
		fieldName := strings.ToLower(field.Name)
		jsonName := strings.ToLower(getJSONFieldName(field))

		if fieldName == "score" || jsonName == "score" {
			// Handle *float32 (the type used in generated API code)
			if value.Kind() == reflect.Pointer && !value.IsNil() {
				elemKind := value.Elem().Kind()
				var scoreValue float64

				switch elemKind {
				case reflect.Float32:
					scoreValue = float64(value.Elem().Float())
				case reflect.Float64:
					scoreValue = value.Elem().Float()
				default:
					continue
				}

				// Check if the value has more than 1 decimal place
				// Multiply by 10 and check if there's a fractional part
				scaled := scoreValue * 10
				if math.Abs(scaled-math.Round(scaled)) > 0.0001 {
					return InvalidInputError(fmt.Sprintf("Field 'score' must have at most 1 decimal place (got %.6f)", scoreValue))
				}
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
			switch {
			case value.Kind() == reflect.String:
				fieldValue = value.String()
			case value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.String:
				fieldValue = value.Elem().String()
			default:
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

	seen := make(map[any]bool)
	for i := 0; i < sliceValue.Len(); i++ {
		item := sliceValue.Index(i).Interface()

		// For structs, use a string representation
		var key any
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
func ValidateUUIDFieldsFromStruct(data any) error {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)

		// Check UUID fields by name or tag
		fieldName := strings.ToLower(field.Name)
		if strings.Contains(fieldName, "id") || field.Tag.Get("format") == "uuid" {
			if value.Kind() == reflect.Pointer && !value.IsNil() {
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
