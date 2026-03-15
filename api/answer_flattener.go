package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// flattenAnswerValue converts a JSON answer value to a plain string.
// Arrays of strings become comma-separated. Booleans and numbers become
// their string representations. Objects and mixed arrays become JSON strings.
// Null and nil become empty string.
func flattenAnswerValue(value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}

	// Try null
	if string(value) == "null" {
		return ""
	}

	// Try string
	var s string
	if err := json.Unmarshal(value, &s); err == nil {
		return s
	}

	// Try number (json.Number or float64)
	var f float64
	if err := json.Unmarshal(value, &f); err == nil {
		return fmt.Sprintf("%g", f)
	}

	// Try boolean
	var b bool
	if err := json.Unmarshal(value, &b); err == nil {
		return fmt.Sprintf("%t", b)
	}

	// Try array
	var arr []json.RawMessage
	if err := json.Unmarshal(value, &arr); err == nil {
		if len(arr) == 0 {
			return ""
		}
		strs := make([]string, 0, len(arr))
		allStrings := true
		for _, elem := range arr {
			var s string
			if err := json.Unmarshal(elem, &s); err != nil {
				allStrings = false
				break
			}
			strs = append(strs, s)
		}
		if allStrings {
			return strings.Join(strs, ", ")
		}
		return string(value)
	}

	return string(value)
}

// flattenAndSanitize flattens a JSON answer value to a string and sanitizes
// it via bluemonday to prevent injection attacks.
func flattenAndSanitize(value json.RawMessage) string {
	flat := flattenAnswerValue(value)
	return SanitizePlainText(flat)
}
