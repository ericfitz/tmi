package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ericfitz/tmi/internal/unicodecheck"
	"golang.org/x/text/unicode/norm"
)

// Boolean string constants used in parameter validation
const (
	boolTrue  = "true"
	boolFalse = "false"
)

// TMI object types taxonomy (valid values for objects field)
var TMIObjectTypes = []string{
	"threat_model",
	"diagram",
	"asset",
	"threat",
	"document",
	"note",
	"repository",
	"metadata",
	"survey",
	"survey_response",
}

// Icon validation regex patterns
var (
	// Material Symbols: material-symbols:[a-z]([a-z0-9_]*[a-z0-9])?
	// Must start with lowercase letter, can contain lowercase letters, digits, underscores
	// No consecutive underscores, cannot end with underscore
	materialSymbolsPattern = regexp.MustCompile(`^material-symbols:[a-z]([a-z0-9_]*[a-z0-9])?$`)

	// FontAwesome: fa-[a-z]([a-z]*[a-z])?(\-[a-z]+)? fa-([a-z]+)(-[a-z]+)*
	// Two parts: style + icon key, separated by space
	// Style: fa-{style} where style is lowercase letters with optional single hyphens
	// Icon key: fa-{icon} where icon is lowercase letters with hyphens between words
	fontAwesomePattern = regexp.MustCompile(`^fa-[a-z]([a-z]*[a-z])?(\-[a-z]+)? fa-([a-z]+)(-[a-z]+)*$`)
)

const (
	// MaxIconLength is the maximum allowed length for icon strings
	MaxIconLength = 60
	// MaxAddonObjects is the maximum number of objects allowed in an add-on
	MaxAddonObjects = 100
	// MaxAddonDescriptionLength is the maximum allowed length for add-on descriptions
	// Consistent with ThreatBase.description maxLength in OpenAPI spec
	MaxAddonDescriptionLength = 2048
)

// ValidateIcon validates an icon string against Material Symbols or FontAwesome formats
func ValidateIcon(icon string) error {
	if icon == "" {
		// Empty icon is allowed (optional field)
		return nil
	}

	// Check max length
	if len(icon) > MaxIconLength {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Icon exceeds maximum length of %d characters (got %d)", MaxIconLength, len(icon)),
		}
	}

	// Check Material Symbols format
	if strings.HasPrefix(icon, "material-symbols:") {
		if materialSymbolsPattern.MatchString(icon) {
			return nil
		}
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: "Invalid Material Symbols icon format. Must be: material-symbols:{name} where name is snake_case (lowercase letters, digits, underscores; no consecutive underscores; no trailing underscore)",
		}
	}

	// Check FontAwesome format
	if strings.HasPrefix(icon, "fa-") {
		if fontAwesomePattern.MatchString(icon) {
			return nil
		}
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: "Invalid FontAwesome icon format. Must be: fa-{style} fa-{icon} where style and icon are lowercase with single hyphens (e.g., 'fa-solid fa-rocket')",
		}
	}

	// Unknown icon format
	return &RequestError{
		Status:  400,
		Code:    "invalid_input",
		Message: "Icon must be in Material Symbols format (material-symbols:name) or FontAwesome format (fa-style fa-icon)",
	}
}

// ValidateObjects validates that all object types are in the TMI taxonomy
func ValidateObjects(objects []string) error {
	if len(objects) == 0 {
		// Empty objects array is allowed
		return nil
	}

	// Check array size to prevent overflow attacks
	if len(objects) > MaxAddonObjects {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Objects array exceeds maximum size of %d (got %d)", MaxAddonObjects, len(objects)),
		}
	}

	// Create map for efficient lookup
	validObjects := make(map[string]bool)
	for _, obj := range TMIObjectTypes {
		validObjects[obj] = true
	}

	// Check each object type
	var invalidObjects []string
	for _, obj := range objects {
		if !validObjects[obj] {
			invalidObjects = append(invalidObjects, obj)
		}
	}

	if len(invalidObjects) > 0 {
		return &RequestError{
			Status: 400,
			Code:   "invalid_input",
			Message: fmt.Sprintf("Invalid object types: %s. Valid types: %s",
				strings.Join(invalidObjects, ", "),
				strings.Join(TMIObjectTypes, ", ")),
		}
	}

	return nil
}

// ValidateAddonName validates the add-on name for XSS and length
func ValidateAddonName(name string) error {
	return validateTextField(name, "Add-on name", 255, true)
}

// ValidateAddonDescription validates the add-on description for XSS and length
func ValidateAddonDescription(description string) error {
	return validateTextField(description, "description", MaxAddonDescriptionLength, false)
}

// checkHTMLInjection delegates to the unified HTML/XSS injection checker.
func checkHTMLInjection(value, fieldName string) error {
	return CheckHTMLInjection(value, fieldName)
}

// ValidateUnicodeContent checks for problematic Unicode that might slip through middleware.
// Delegates to the consolidated unicodecheck package for consistent character detection.
// Uses context-aware zero-width checking and threshold-based combining mark detection
// to support international text while blocking attacks.
func ValidateUnicodeContent(value, fieldName string) error {
	if value == "" {
		return nil
	}

	// Normalize to NFC first so decomposed legitimate text (e.g., e + combining acute)
	// won't trigger false positives. The middleware already normalizes the request body,
	// but this provides defense-in-depth for any code path that calls this directly.
	normalizedValue := norm.NFC.String(value)

	// Context-aware zero-width check: allows ZWNJ in Indic scripts, ZWJ in emoji
	if unicodecheck.ContainsDangerousZeroWidthChars(normalizedValue) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains zero-width characters", fieldName),
		}
	}

	if unicodecheck.ContainsBidiOverrides(normalizedValue) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains bidirectional text control characters", fieldName),
		}
	}

	if unicodecheck.ContainsHangulFillers(normalizedValue) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains Hangul filler characters", fieldName),
		}
	}

	// Reject excessive combining marks (Zalgo text prevention).
	// Threshold of 3 allows legitimate diacritics (1-2 marks per base character)
	// while blocking stacking attacks that use 3+ consecutive marks.
	if unicodecheck.HasExcessiveCombiningMarks(normalizedValue, 3) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains excessive combining diacritical marks", fieldName),
		}
	}

	return nil
}

// Addon parameter validation constants
const (
	// MaxAddonParameters is the maximum number of parameters allowed per add-on
	MaxAddonParameters = 20
	// MaxAddonParameterEnumValues is the maximum number of enum values per parameter
	MaxAddonParameterEnumValues = 50
	// MaxAddonParameterNameLength is the maximum length for parameter names
	MaxAddonParameterNameLength = 128
	// MaxAddonParameterDescriptionLength is the maximum length for parameter descriptions
	MaxAddonParameterDescriptionLength = 512
	// MaxAddonParameterValueLength is the maximum length for enum values and default values
	MaxAddonParameterValueLength = 256
	// MaxAddonParameterMetadataKeyLength is the maximum length for metadata key names
	MaxAddonParameterMetadataKeyLength = 256
	// MaxStringValidationRegexLength is the maximum length for string_validation_regex patterns
	MaxStringValidationRegexLength = 256
	// MaxStringMaxLength is the upper bound for string_max_length constraint
	MaxStringMaxLength = 10000
)

// addonParameterNamePattern validates parameter names: starts with letter, then alphanumeric, hyphens, underscores
var addonParameterNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// metadataKeyPattern validates metadata key format (matches Metadata.key in OpenAPI spec)
var metadataKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_./:-]+$`)

// ValidateAddonParameters validates parameter definitions at addon creation time
func ValidateAddonParameters(params []AddonParameter) error {
	if len(params) == 0 {
		return nil
	}

	if len(params) > MaxAddonParameters {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameters array exceeds maximum size of %d (got %d)", MaxAddonParameters, len(params)),
		}
	}

	// Check for duplicate names
	seen := make(map[string]bool)
	for _, p := range params {
		nameLower := strings.ToLower(p.Name)
		if seen[nameLower] {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Duplicate parameter name: %s", p.Name),
			}
		}
		seen[nameLower] = true

		if err := validateAddonParameter(p); err != nil {
			return err
		}
	}

	return nil
}

// validateAddonParameter validates a single parameter definition
func validateAddonParameter(p AddonParameter) error {
	// Validate name
	if p.Name == "" {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: "Parameter name is required",
		}
	}
	if len(p.Name) > MaxAddonParameterNameLength {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter name '%s' exceeds maximum length of %d", p.Name, MaxAddonParameterNameLength),
		}
	}
	if !addonParameterNamePattern.MatchString(p.Name) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter name '%s' must start with a letter and contain only letters, digits, hyphens, and underscores", p.Name),
		}
	}

	// Validate text fields for XSS/unicode
	if err := validateTextField(p.Name, fmt.Sprintf("parameter name '%s'", p.Name), MaxAddonParameterNameLength, true); err != nil {
		return err
	}
	if p.Description != nil {
		if err := validateTextField(*p.Description, fmt.Sprintf("parameter '%s' description", p.Name), MaxAddonParameterDescriptionLength, false); err != nil {
			return err
		}
	}

	// Type-specific validation
	switch p.Type {
	case AddonParameterTypeEnum:
		return validateEnumParameter(p)
	case AddonParameterTypeBoolean:
		return validateBooleanParameter(p)
	case AddonParameterTypeString:
		return validateStringParameter(p)
	case AddonParameterTypeNumber:
		return validateNumberParameter(p)
	case AddonParameterTypeMetadataKey:
		return validateMetadataKeyParameter(p)
	default:
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' has invalid type: %s", p.Name, p.Type),
		}
	}
}

// rejectConstraintFields rejects number_min, number_max, string_max_length, and string_validation_regex
// on parameter types where they don't apply (everything except number and string respectively)
func rejectConstraintFields(p AddonParameter, typeName string) error {
	if p.NumberMin != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type '%s' must not have number_min", p.Name, typeName),
		}
	}
	if p.NumberMax != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type '%s' must not have number_max", p.Name, typeName),
		}
	}
	if p.StringMaxLength != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type '%s' must not have string_max_length", p.Name, typeName),
		}
	}
	if p.StringValidationRegex != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type '%s' must not have string_validation_regex", p.Name, typeName),
		}
	}
	return nil
}

func validateEnumParameter(p AddonParameter) error {
	if p.EnumValues == nil || len(*p.EnumValues) == 0 {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'enum' must have enum_values", p.Name),
		}
	}
	if len(*p.EnumValues) > MaxAddonParameterEnumValues {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' enum_values exceeds maximum of %d", p.Name, MaxAddonParameterEnumValues),
		}
	}
	for i, v := range *p.EnumValues {
		if v == "" {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' enum_values[%d] must not be empty", p.Name, i),
			}
		}
		if len(v) > MaxAddonParameterValueLength {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' enum_values[%d] exceeds maximum length of %d", p.Name, i, MaxAddonParameterValueLength),
			}
		}
		if err := validateTextField(v, fmt.Sprintf("parameter '%s' enum_values[%d]", p.Name, i), MaxAddonParameterValueLength, true); err != nil {
			return err
		}
	}
	// Validate default_value is in enum_values
	if p.DefaultValue != nil && *p.DefaultValue != "" {
		found := false
		for _, v := range *p.EnumValues {
			if v == *p.DefaultValue {
				found = true
				break
			}
		}
		if !found {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' default_value '%s' is not in enum_values", p.Name, *p.DefaultValue),
			}
		}
	}
	if p.MetadataKey != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'enum' must not have metadata_key", p.Name),
		}
	}
	if err := rejectConstraintFields(p, "enum"); err != nil {
		return err
	}
	return nil
}

func validateBooleanParameter(p AddonParameter) error {
	if p.EnumValues != nil && len(*p.EnumValues) > 0 {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'boolean' must not have enum_values", p.Name),
		}
	}
	if p.DefaultValue != nil && *p.DefaultValue != "" {
		if *p.DefaultValue != boolTrue && *p.DefaultValue != boolFalse {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' of type 'boolean' default_value must be 'true' or 'false'", p.Name),
			}
		}
	}
	if p.MetadataKey != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'boolean' must not have metadata_key", p.Name),
		}
	}
	if err := rejectConstraintFields(p, "boolean"); err != nil {
		return err
	}
	return nil
}

func validateStringParameter(p AddonParameter) error {
	if p.EnumValues != nil && len(*p.EnumValues) > 0 {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'string' must not have enum_values", p.Name),
		}
	}
	if p.MetadataKey != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'string' must not have metadata_key", p.Name),
		}
	}
	// Reject number-only constraint fields
	if p.NumberMin != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'string' must not have number_min", p.Name),
		}
	}
	if p.NumberMax != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'string' must not have number_max", p.Name),
		}
	}
	// Validate string_max_length
	if p.StringMaxLength != nil {
		if *p.StringMaxLength < 1 || *p.StringMaxLength > MaxStringMaxLength {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' string_max_length must be between 1 and %d", p.Name, MaxStringMaxLength),
			}
		}
	}
	// Validate string_validation_regex
	if p.StringValidationRegex != nil {
		if len(*p.StringValidationRegex) > MaxStringValidationRegexLength {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' string_validation_regex exceeds maximum length of %d", p.Name, MaxStringValidationRegexLength),
			}
		}
		if *p.StringValidationRegex != "" {
			if _, err := regexp.Compile(*p.StringValidationRegex); err != nil {
				return &RequestError{
					Status:  400,
					Code:    "invalid_input",
					Message: fmt.Sprintf("Parameter '%s' string_validation_regex is not a valid regular expression: %s", p.Name, err.Error()),
				}
			}
		}
	}
	if p.DefaultValue != nil && *p.DefaultValue != "" {
		if err := validateTextField(*p.DefaultValue, fmt.Sprintf("parameter '%s' default_value", p.Name), MaxAddonParameterValueLength, false); err != nil {
			return err
		}
		// Validate default against string_max_length
		if p.StringMaxLength != nil && len(*p.DefaultValue) > *p.StringMaxLength {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' default_value exceeds string_max_length of %d", p.Name, *p.StringMaxLength),
			}
		}
		// Validate default against string_validation_regex
		if p.StringValidationRegex != nil && *p.StringValidationRegex != "" {
			re, err := regexp.Compile(*p.StringValidationRegex)
			if err == nil && !re.MatchString(*p.DefaultValue) {
				return &RequestError{
					Status:  400,
					Code:    "invalid_input",
					Message: fmt.Sprintf("Parameter '%s' default_value does not match string_validation_regex", p.Name),
				}
			}
		}
	}
	return nil
}

func validateNumberParameter(p AddonParameter) error {
	if p.EnumValues != nil && len(*p.EnumValues) > 0 {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'number' must not have enum_values", p.Name),
		}
	}
	if p.MetadataKey != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'number' must not have metadata_key", p.Name),
		}
	}
	// Reject string-only constraint fields
	if p.StringMaxLength != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'number' must not have string_max_length", p.Name),
		}
	}
	if p.StringValidationRegex != nil {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'number' must not have string_validation_regex", p.Name),
		}
	}
	// Validate number_min <= number_max if both set
	if p.NumberMin != nil && p.NumberMax != nil && *p.NumberMin > *p.NumberMax {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' number_min (%g) must not exceed number_max (%g)", p.Name, *p.NumberMin, *p.NumberMax),
		}
	}
	if p.DefaultValue != nil && *p.DefaultValue != "" {
		defVal, err := strconv.ParseFloat(*p.DefaultValue, 64)
		if err != nil {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' of type 'number' default_value must be a valid number", p.Name),
			}
		}
		if p.NumberMin != nil && defVal < float64(*p.NumberMin) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' default_value %s is below number_min (%g)", p.Name, *p.DefaultValue, *p.NumberMin),
			}
		}
		if p.NumberMax != nil && defVal > float64(*p.NumberMax) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' default_value %s exceeds number_max (%g)", p.Name, *p.DefaultValue, *p.NumberMax),
			}
		}
	}
	return nil
}

func validateMetadataKeyParameter(p AddonParameter) error {
	if p.MetadataKey == nil || *p.MetadataKey == "" {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'metadata_key' must have metadata_key field set", p.Name),
		}
	}
	if len(*p.MetadataKey) > MaxAddonParameterMetadataKeyLength {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' metadata_key exceeds maximum length of %d", p.Name, MaxAddonParameterMetadataKeyLength),
		}
	}
	if !metadataKeyPattern.MatchString(*p.MetadataKey) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' metadata_key must match pattern: alphanumeric, underscores, dots, slashes, colons, hyphens", p.Name),
		}
	}
	if p.EnumValues != nil && len(*p.EnumValues) > 0 {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' of type 'metadata_key' must not have enum_values", p.Name),
		}
	}
	if err := rejectConstraintFields(p, "metadata_key"); err != nil {
		return err
	}
	return nil
}

// ValidateInvocationData validates invocation data against declared addon parameters
func ValidateInvocationData(data map[string]interface{}, params []AddonParameter) error {
	if len(params) == 0 {
		return nil
	}

	// Check required parameters are present
	for _, p := range params {
		isRequired := p.Required != nil && *p.Required
		if !isRequired {
			continue
		}
		if data == nil {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Required parameter '%s' is missing", p.Name),
			}
		}
		if _, ok := data[p.Name]; !ok {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Required parameter '%s' is missing", p.Name),
			}
		}
	}

	if data == nil {
		return nil
	}

	// Build parameter lookup
	paramMap := make(map[string]AddonParameter)
	for _, p := range params {
		paramMap[p.Name] = p
	}

	// Validate provided values against declared parameters
	for key, value := range data {
		p, ok := paramMap[key]
		if !ok {
			// Extra keys allowed for backward compatibility
			continue
		}

		if err := validateInvocationValue(p, value); err != nil {
			return err
		}
	}

	return nil
}

// validateInvocationValue validates a single invocation data value against its parameter definition
func validateInvocationValue(p AddonParameter, value interface{}) error {
	switch p.Type {
	case AddonParameterTypeEnum:
		strVal, ok := value.(string)
		if !ok {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' must be a string", p.Name),
			}
		}
		if p.EnumValues != nil {
			found := false
			for _, v := range *p.EnumValues {
				if v == strVal {
					found = true
					break
				}
			}
			if !found {
				return &RequestError{
					Status:  400,
					Code:    "invalid_input",
					Message: fmt.Sprintf("Parameter '%s' value '%s' is not in allowed values", p.Name, strVal),
				}
			}
		}

	case AddonParameterTypeBoolean:
		switch v := value.(type) {
		case bool:
			// OK
		case string:
			if v != boolTrue && v != boolFalse {
				return &RequestError{
					Status:  400,
					Code:    "invalid_input",
					Message: fmt.Sprintf("Parameter '%s' must be 'true' or 'false'", p.Name),
				}
			}
		default:
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' must be a boolean or string 'true'/'false'", p.Name),
			}
		}

	case AddonParameterTypeString, AddonParameterTypeMetadataKey:
		return validateInvocationStringValue(p, value)

	case AddonParameterTypeNumber:
		return validateInvocationNumberValue(p, value)
	}

	return nil
}

// validateInvocationStringValue validates a string invocation value against constraints
func validateInvocationStringValue(p AddonParameter, value interface{}) error {
	strVal, ok := value.(string)
	if !ok {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' must be a string", p.Name),
		}
	}
	if p.StringMaxLength != nil && len(strVal) > *p.StringMaxLength {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' value exceeds maximum length of %d", p.Name, *p.StringMaxLength),
		}
	}
	if p.StringValidationRegex != nil && *p.StringValidationRegex != "" {
		re, err := regexp.Compile(*p.StringValidationRegex)
		if err == nil && !re.MatchString(strVal) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' value does not match validation pattern", p.Name),
			}
		}
	}
	return nil
}

// validateInvocationNumberValue validates a numeric invocation value against constraints
func validateInvocationNumberValue(p AddonParameter, value interface{}) error {
	var numVal float64
	switch v := value.(type) {
	case float64:
		numVal = v
	case float32:
		numVal = float64(v)
	case int:
		numVal = float64(v)
	case int64:
		numVal = float64(v)
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Parameter '%s' must be a valid number", p.Name),
			}
		}
		numVal = parsed
	default:
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' must be a number", p.Name),
		}
	}
	if p.NumberMin != nil && numVal < float64(*p.NumberMin) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' value %g is below minimum of %g", p.Name, numVal, *p.NumberMin),
		}
	}
	if p.NumberMax != nil && numVal > float64(*p.NumberMax) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Parameter '%s' value %g exceeds maximum of %g", p.Name, numVal, *p.NumberMax),
		}
	}
	return nil
}
