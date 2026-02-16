package api

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ericfitz/tmi/internal/unicodecheck"
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
	MaxAddonDescriptionLength = 1024
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
func ValidateUnicodeContent(value, fieldName string) error {
	if value == "" {
		return nil
	}

	// Check these BEFORE NFC normalization to get specific error messages
	if unicodecheck.ContainsZeroWidthChars(value) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains zero-width characters", fieldName),
		}
	}

	if unicodecheck.ContainsBidiOverrides(value) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains bidirectional text control characters", fieldName),
		}
	}

	if unicodecheck.ContainsHangulFillers(value) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains Hangul filler characters", fieldName),
		}
	}

	// Reject any combining marks in basic range (stricter than middleware's Zalgo detection)
	if unicodecheck.ContainsAnyCombiningMarks(value) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains excessive combining diacritical marks", fieldName),
		}
	}

	// Check if NFC normalization changes the string (indicates decomposed characters)
	// This check comes AFTER specific character checks to provide better error messages
	if !unicodecheck.IsNFCNormalized(value) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains non-normalized Unicode characters", fieldName),
		}
	}

	return nil
}
