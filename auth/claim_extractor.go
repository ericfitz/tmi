package auth

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// DefaultClaimMappings provides standard claim names for common OAuth providers
var DefaultClaimMappings = map[string]string{
	"subject_claim":        "sub",
	"email_claim":          "email",
	"name_claim":           "name",
	"given_name_claim":     "given_name",
	"family_name_claim":    "family_name",
	"picture_claim":        "picture",
	"email_verified_claim": "email_verified",
}

// extractValue extracts a value from JSON data using a path expression
// Supports:
// - Simple field access: "email"
// - Nested field access: "user.email"
// - Array index access: "[0].email"
// - Literal values: "true", "false", numbers
func extractValue(data interface{}, path string) (interface{}, error) {
	// Check if it's a literal value
	if path == "true" {
		return true, nil
	}
	if path == "false" {
		return false, nil
	}
	// Try to parse as number
	if num, err := strconv.ParseFloat(path, 64); err == nil {
		return num, nil
	}

	// Parse the path
	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		if part == "" {
			continue
		}

		// Check for array access
		if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			indexStr := strings.TrimSuffix(strings.TrimPrefix(part, "["), "]")
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return nil, fmt.Errorf("invalid array index: %s", indexStr)
			}

			arr, ok := current.([]interface{})
			if !ok {
				return nil, fmt.Errorf("expected array at path segment: %s", part)
			}

			if index < 0 || index >= len(arr) {
				return nil, fmt.Errorf("array index out of bounds: %d", index)
			}

			current = arr[index]
		} else {
			// Object field access
			obj, ok := current.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("expected object at path segment: %s", part)
			}

			val, exists := obj[part]
			if !exists {
				return nil, fmt.Errorf("field not found: %s", part)
			}

			current = val
		}
	}

	return current, nil
}

// extractClaims extracts claims from JSON data using the provided mappings
func extractClaims(jsonData map[string]interface{}, mappings map[string]string, userInfo *UserInfo) error {
	// Helper to convert interface{} to string
	toString := func(v interface{}) string {
		switch val := v.(type) {
		case string:
			return val
		case float64:
			return fmt.Sprintf("%.0f", val)
		case bool:
			return strconv.FormatBool(val)
		default:
			if val == nil {
				return ""
			}
			// Try JSON encoding as last resort
			if b, err := json.Marshal(val); err == nil {
				return string(b)
			}
			return fmt.Sprintf("%v", val)
		}
	}

	// Helper to convert interface{} to bool
	toBool := func(v interface{}) bool {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return val == "true" || val == "1" || val == "yes"
		case float64:
			return val != 0
		default:
			return false
		}
	}

	// Process each mapping
	for claimType, path := range mappings {
		value, err := extractValue(jsonData, path)
		if err != nil {
			// Skip if field not found - it might be in another endpoint
			continue
		}

		switch claimType {
		case "subject_claim":
			if userInfo.ID == "" {
				userInfo.ID = toString(value)
			}
		case "email_claim":
			if userInfo.Email == "" {
				userInfo.Email = toString(value)
			}
		case "name_claim":
			if userInfo.Name == "" {
				userInfo.Name = toString(value)
			}
		case "given_name_claim":
			if userInfo.GivenName == "" {
				userInfo.GivenName = toString(value)
			}
		case "family_name_claim":
			if userInfo.FamilyName == "" {
				userInfo.FamilyName = toString(value)
			}
		case "picture_claim":
			if userInfo.Picture == "" {
				userInfo.Picture = toString(value)
			}
		case "email_verified_claim":
			userInfo.EmailVerified = toBool(value)
		}
	}

	return nil
}

// applyDefaultMappings applies default claim mappings for unmapped essential claims
func applyDefaultMappings(mappings map[string]string, jsonData map[string]interface{}) {
	// Check each default claim
	for claimType, defaultField := range DefaultClaimMappings {
		// Skip if already mapped
		if _, exists := mappings[claimType]; exists {
			continue
		}

		// Check if the default field exists in the JSON data
		if _, err := extractValue(jsonData, defaultField); err == nil {
			// Add the default mapping
			mappings[claimType] = defaultField
		}
	}
}
