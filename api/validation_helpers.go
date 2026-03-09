package api

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/ericfitz/tmi/internal/unicodecheck"
	"github.com/google/uuid"
)

// SafeParseInt safely parses an integer string with a fallback value
// Does not return errors - uses fallback for any parsing failure
func SafeParseInt(s string, fallback int) int {
	if s == "" {
		return fallback
	}

	// Prevent overflow - max length for safe int parsing
	if len(s) > 10 { // Safe for int32 range
		return fallback
	}

	value, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}

	// Ensure non-negative
	if value < 0 {
		return fallback
	}

	return value
}

// ValidateUUID validates that a string is a valid UUID format
func ValidateUUID(s string, fieldName string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s is required", fieldName),
		}
	}

	parsedUUID, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s must be a valid UUID format: %v", fieldName, err),
		}
	}

	return parsedUUID, nil
}

// ValidateNumericRange validates that a numeric value is within the specified range
// Handles int, int32, int64, float32, float64
func ValidateNumericRange(value any, minVal, maxVal int64, fieldName string) error {
	var numValue int64

	switch v := value.(type) {
	case int:
		numValue = int64(v)
	case int32:
		numValue = int64(v)
	case int64:
		numValue = v
	case float32:
		// Check for infinity and NaN
		if math.IsInf(float64(v), 0) || math.IsNaN(float64(v)) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("%s contains invalid numeric value (infinity or NaN)", fieldName),
			}
		}
		// Check if it's within int64 range before converting
		if v > float32(math.MaxInt64) || v < float32(math.MinInt64) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("%s exceeds numeric range", fieldName),
			}
		}
		numValue = int64(v)
	case float64:
		// Check for infinity and NaN
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("%s contains invalid numeric value (infinity or NaN)", fieldName),
			}
		}
		// Check if it's within int64 range before converting
		if v > float64(math.MaxInt64) || v < float64(math.MinInt64) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("%s exceeds numeric range", fieldName),
			}
		}
		numValue = int64(v)
	case *int:
		if v == nil {
			return nil // Nil pointer is allowed (optional field)
		}
		numValue = int64(*v)
	case *int32:
		if v == nil {
			return nil // Nil pointer is allowed (optional field)
		}
		numValue = int64(*v)
	case *int64:
		if v == nil {
			return nil // Nil pointer is allowed (optional field)
		}
		numValue = *v
	default:
		return &RequestError{
			Status:  500,
			Code:    "server_error",
			Message: fmt.Sprintf("unsupported numeric type for %s", fieldName),
		}
	}

	if numValue < minVal {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s is below minimum value of %d (got %d)", fieldName, minVal, numValue),
		}
	}

	if numValue > maxVal {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s exceeds maximum value of %d (got %d)", fieldName, maxVal, numValue),
		}
	}

	return nil
}

// validateTextField validates a text field with a standard pipeline:
// empty check, length check, Unicode validation, and HTML injection check.
// If required is false, empty values are allowed without error.
func validateTextField(value, fieldName string, maxLength int, required bool) error {
	if value == "" {
		if required {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("%s is required", fieldName),
			}
		}
		return nil
	}

	if len(value) > maxLength {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s exceeds maximum length of %d characters (got %d)", fieldName, maxLength, len(value)),
		}
	}

	// Check for control characters
	if unicodecheck.ContainsControlChars(value) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains control characters", fieldName),
		}
	}

	// Check for problematic Unicode categories (private use, surrogates, non-chars, Hangul fillers)
	if unicodecheck.ContainsProblematicCategories(value) {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("Field '%s' contains problematic Unicode characters", fieldName),
		}
	}

	// Run full Unicode content validation (zero-width, bidi, Hangul fillers,
	// combining marks, NFC normalization)
	if err := ValidateUnicodeContent(value, fieldName); err != nil {
		return err
	}

	// Check for HTML injection
	return CheckHTMLInjection(value, fieldName)
}

// colorHexPattern matches exactly 3 or 6 hex digits after a '#'
var colorHexPattern = regexp.MustCompile(`^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)

// expandShorthandHex expands a 3-digit hex color to 6 digits (e.g., "#f0c" -> "#ff00cc")
func expandShorthandHex(color string) string {
	if len(color) == 4 {
		return "#" + string(color[1]) + string(color[1]) +
			string(color[2]) + string(color[2]) +
			string(color[3]) + string(color[3])
	}
	return color
}

// NormalizeColorPalette validates and normalizes a color palette array.
// - Validates each color matches #RGB or #RRGGBB pattern
// - Expands 3-digit hex to 6-digit
// - Lowercases all color values
// - Rejects duplicate positions
// - Rejects more than 8 entries
// - Sorts entries by position
// - Returns nil for empty input (never returns an empty slice)
func NormalizeColorPalette(palette *[]ColorPaletteEntry) (*[]ColorPaletteEntry, error) {
	if palette == nil || len(*palette) == 0 {
		return nil, nil
	}

	entries := *palette

	if len(entries) > 8 {
		return nil, &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: "color_palette must contain at most 8 entries",
		}
	}

	seenPositions := make(map[int]bool)
	normalized := make([]ColorPaletteEntry, 0, len(entries))

	for _, entry := range entries {
		if entry.Position < 1 || entry.Position > 8 {
			return nil, &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("color_palette position must be between 1 and 8 (got %d)", entry.Position),
			}
		}

		if seenPositions[entry.Position] {
			return nil, &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("duplicate color_palette position: %d", entry.Position),
			}
		}
		seenPositions[entry.Position] = true

		if !colorHexPattern.MatchString(entry.Color) {
			return nil, &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("invalid color format %q: must be #RGB or #RRGGBB hex", entry.Color),
			}
		}

		color := strings.ToLower(expandShorthandHex(entry.Color))
		normalized = append(normalized, ColorPaletteEntry{
			Position: entry.Position,
			Color:    color,
		})
	}

	// Sort by position (insertion sort is fine for max 8 elements)
	for i := 1; i < len(normalized); i++ {
		for j := i; j > 0 && normalized[j].Position < normalized[j-1].Position; j-- {
			normalized[j], normalized[j-1] = normalized[j-1], normalized[j]
		}
	}

	return &normalized, nil
}
