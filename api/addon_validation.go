package api

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
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
	if name == "" {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: "Add-on name is required",
		}
	}

	if len(name) > 255 {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Add-on name exceeds maximum length of 255 characters (got %d)", len(name)),
		}
	}

	// Check for problematic Unicode characters
	if err := ValidateUnicodeContent(name, "name"); err != nil {
		return err
	}

	// Check for HTML injection patterns
	if err := checkHTMLInjection(name, "name"); err != nil {
		return err
	}

	return nil
}

// ValidateAddonDescription validates the add-on description for XSS
func ValidateAddonDescription(description string) error {
	if description == "" {
		// Empty description is allowed
		return nil
	}

	// Check for problematic Unicode characters
	if err := ValidateUnicodeContent(description, "description"); err != nil {
		return err
	}

	// Check for HTML injection patterns
	if err := checkHTMLInjection(description, "description"); err != nil {
		return err
	}

	return nil
}

// checkHTMLInjection checks for common XSS patterns
func checkHTMLInjection(value, fieldName string) error {
	// Convert to lowercase for case-insensitive matching
	lowerValue := strings.ToLower(value)

	// Blocked patterns
	blockedPatterns := []string{
		"<script",
		"</script>",
		"<iframe",
		"</iframe>",
		"javascript:",
		"onload=",
		"onerror=",
		"onclick=",
		"onmouseover=",
		"onfocus=",
		"onblur=",
		"<object",
		"<embed",
		"<applet",
	}

	for _, pattern := range blockedPatterns {
		if strings.Contains(lowerValue, pattern) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Field '%s' contains potentially unsafe content (%s)", fieldName, pattern),
			}
		}
	}

	return nil
}

// ValidateUnicodeContent checks for problematic Unicode that might slip through middleware
func ValidateUnicodeContent(value, fieldName string) error {
	if value == "" {
		return nil
	}

	// Explicit check for characters that middleware should catch
	// Check these BEFORE NFC normalization to get specific error messages
	for _, r := range value {
		// Zero-width characters
		if r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\uFEFF' {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Field '%s' contains zero-width characters", fieldName),
			}
		}

		// Bidirectional overrides
		if (r >= '\u202A' && r <= '\u202E') || (r >= '\u2066' && r <= '\u2069') {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Field '%s' contains bidirectional text control characters", fieldName),
			}
		}

		// Hangul filler
		if r == '\u3164' {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Field '%s' contains Hangul filler characters", fieldName),
			}
		}

		// Combining marks (Zalgo)
		if r >= '\u0300' && r <= '\u036F' {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Field '%s' contains excessive combining diacritical marks", fieldName),
			}
		}
	}

	// Check if NFC normalization changes the string (indicates decomposed characters)
	// This check comes AFTER specific character checks to provide better error messages
	normalized := norm.NFC.String(value)
	if normalized != value {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains non-normalized Unicode characters", fieldName),
		}
	}

	return nil
}
