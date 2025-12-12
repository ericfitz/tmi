package api

import (
	"fmt"
	"math"
	"strconv"

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
func ValidateNumericRange(value interface{}, min, max int64, fieldName string) error {
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

	if numValue < min {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s is below minimum value of %d (got %d)", fieldName, min, numValue),
		}
	}

	if numValue > max {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s exceeds maximum value of %d (got %d)", fieldName, max, numValue),
		}
	}

	return nil
}
