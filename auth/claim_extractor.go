package auth

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
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
	logger := slogging.Get()
	logger.Debug("Extracting value from JSON data path=%v", path)

	// Check if it's a literal value
	if path == "true" {
		logger.Debug("Extracting literal boolean value path=%v value=%v", path, true)
		return true, nil
	}
	if path == "false" {
		logger.Debug("Extracting literal boolean value path=%v value=%v", path, false)
		return false, nil
	}
	// Try to parse as number
	if num, err := strconv.ParseFloat(path, 64); err == nil {
		logger.Debug("Extracting literal number value path=%v value=%v", path, num)
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
				logger.Error("Invalid array index in path path=%v part=%v index_str=%v error=%v", path, part, indexStr, err)
				return nil, fmt.Errorf("invalid array index: %s", indexStr)
			}

			arr, ok := current.([]interface{})
			if !ok {
				logger.Error("Expected array but found different type path=%v part=%v actual_type=%v", path, part, fmt.Sprintf("%T", current))
				return nil, fmt.Errorf("expected array at path segment: %s", part)
			}

			if index < 0 || index >= len(arr) {
				logger.Error("Array index out of bounds path=%v part=%v index=%v array_length=%v", path, part, index, len(arr))
				return nil, fmt.Errorf("array index out of bounds: %d", index)
			}

			current = arr[index]
		} else {
			// Object field access
			obj, ok := current.(map[string]interface{})
			if !ok {
				logger.Error("Expected object but found different type path=%v part=%v actual_type=%v", path, part, fmt.Sprintf("%T", current))
				return nil, fmt.Errorf("expected object at path segment: %s", part)
			}

			val, exists := obj[part]
			if !exists {
				logger.Debug("Field not found in object path=%v part=%v available_fields=%v", path, part, getObjectKeys(obj))
				return nil, fmt.Errorf("field not found: %s", part)
			}

			current = val
		}
	}

	logger.Debug("Value extracted successfully path=%v value_type=%v", path, fmt.Sprintf("%T", current))
	return current, nil
}

// getObjectKeys returns the keys of a map for debugging purposes
func getObjectKeys(obj map[string]interface{}) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	return keys
}

// extractClaims extracts claims from JSON data using the provided mappings
func extractClaims(jsonData map[string]interface{}, mappings map[string]string, userInfo *UserInfo) error {
	logger := slogging.Get()
	logger.Debug("Extracting claims from JSON data mappings_count=%v data_keys=%v", len(mappings), getObjectKeys(jsonData))

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
		logger.Debug("Processing claim mapping claim_type=%v path=%v", claimType, path)
		value, err := extractValue(jsonData, path)
		if err != nil {
			// Skip if field not found - it might be in another endpoint
			logger.Debug("Claim extraction failed, skipping claim_type=%v path=%v error=%v", claimType, path, err)
			continue
		}

		switch claimType {
		case "subject_claim":
			if userInfo.ID == "" {
				userInfo.ID = toString(value)
				logger.Debug("Set subject claim value=%v", userInfo.ID)
			}
		case "email_claim":
			if userInfo.Email == "" {
				userInfo.Email = toString(value)
				logger.Debug("Set email claim value=%v", userInfo.Email)
			}
		case "name_claim":
			if userInfo.Name == "" {
				userInfo.Name = toString(value)
				logger.Debug("Set name claim value=%v", userInfo.Name)
			}
		case "given_name_claim":
			if userInfo.GivenName == "" {
				userInfo.GivenName = toString(value)
				logger.Debug("Set given name claim value=%v", userInfo.GivenName)
			}
		case "family_name_claim":
			if userInfo.FamilyName == "" {
				userInfo.FamilyName = toString(value)
				logger.Debug("Set family name claim value=%v", userInfo.FamilyName)
			}
		case "picture_claim":
			if userInfo.Picture == "" {
				userInfo.Picture = toString(value)
				logger.Debug("Set picture claim value=%v", userInfo.Picture)
			}
		case "email_verified_claim":
			userInfo.EmailVerified = toBool(value)
			logger.Debug("Set email verified claim value=%v", userInfo.EmailVerified)
		}
	}

	logger.Info("Claims extraction completed user_id=%v user_email=%v user_name=%v email_verified=%v", userInfo.ID, userInfo.Email, userInfo.Name, userInfo.EmailVerified)
	return nil
}

// applyDefaultMappings applies default claim mappings for unmapped essential claims
func applyDefaultMappings(mappings map[string]string, jsonData map[string]interface{}) {
	logger := slogging.Get()
	logger.Debug("Applying default claim mappings existing_mappings_count=%v data_keys=%v", len(mappings), getObjectKeys(jsonData))

	appliedCount := 0
	// Check each default claim
	for claimType, defaultField := range DefaultClaimMappings {
		// Skip if already mapped
		if _, exists := mappings[claimType]; exists {
			logger.Debug("Claim already mapped, skipping default claim_type=%v existing_mapping=%v", claimType, mappings[claimType])
			continue
		}

		// Check if the default field exists in the JSON data
		if _, err := extractValue(jsonData, defaultField); err == nil {
			// Add the default mapping
			mappings[claimType] = defaultField
			appliedCount++
			logger.Debug("Applied default claim mapping claim_type=%v default_field=%v", claimType, defaultField)
		} else {
			logger.Debug("Default field not found in data claim_type=%v default_field=%v error=%v", claimType, defaultField, err)
		}
	}
	logger.Info("Default claim mappings applied applied_count=%v total_mappings=%v", appliedCount, len(mappings))
}
