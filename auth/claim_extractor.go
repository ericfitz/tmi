package auth

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// literalTrue is the string literal "true" used in claim path extraction and boolean parsing
const literalTrue = "true"

// DefaultClaimMappings provides standard claim names for common OAuth providers
var DefaultClaimMappings = map[string]string{
	"subject_claim":        "sub",
	"email_claim":          "email",
	"name_claim":           "name",
	"given_name_claim":     "given_name",
	"family_name_claim":    "family_name",
	"picture_claim":        "picture",
	"email_verified_claim": "email_verified",
	"groups_claim":         "groups", // RFC 9068 standard: "groups" claim
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
	if path == literalTrue {
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

			// Handle wildcard [*] to extract all items
			if indexStr == "*" {
				arr, ok := current.([]interface{})
				if !ok {
					logger.Error("Expected array but found different type path=%v part=%v actual_type=%v", path, part, fmt.Sprintf("%T", current))
					return nil, fmt.Errorf("expected array at path segment: %s", part)
				}

				// If this is the last part, return the whole array
				partIndex := 0
				for i, p := range parts {
					if p == part {
						partIndex = i
						break
					}
				}

				// If there are more parts after [*], we need to extract the field from each item
				if partIndex < len(parts)-1 {
					remainingPath := strings.Join(parts[partIndex+1:], ".")
					results := make([]interface{}, 0, len(arr))
					for _, item := range arr {
						if val, err := extractValue(item, remainingPath); err == nil {
							results = append(results, val)
						}
					}
					return results, nil
				}

				// Return the array as-is if [*] is the last part
				return arr, nil
			}

			// Handle numeric index
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

// toString converts interface{} to string
func toString(v interface{}) string {
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

// toBool converts interface{} to bool
func toBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == literalTrue || val == "1" || val == "yes"
	case float64:
		return val != 0
	default:
		return false
	}
}

// processStringClaim processes a string-based claim value
func processStringClaim(value interface{}, currentValue string, claimName string) string {
	logger := slogging.Get()
	if currentValue == "" {
		result := toString(value)
		logger.Debug("Set %s claim value=%v", claimName, result)
		return result
	}
	return currentValue
}

// processGroupsClaim processes the groups claim which can be an array or string
func processGroupsClaim(value interface{}) []string {
	switch v := value.(type) {
	case []interface{}:
		return processGroupsArray(v)
	case string:
		return processGroupsString(v)
	default:
		return processGroupsFallback(value)
	}
}

// processGroupsArray converts an array of interfaces to a string array
func processGroupsArray(v []interface{}) []string {
	logger := slogging.Get()
	groups := make([]string, 0, len(v))
	for _, g := range v {
		if groupStr := toString(g); groupStr != "" {
			groups = append(groups, groupStr)
		}
	}
	logger.Debug("Set groups claim from array value=%v count=%d", groups, len(groups))
	return groups
}

// processGroupsString handles string-based groups (single or comma-separated)
func processGroupsString(v string) []string {
	logger := slogging.Get()
	if v == "" {
		return nil
	}

	if strings.Contains(v, ",") {
		groups := strings.Split(v, ",")
		for i := range groups {
			groups[i] = strings.TrimSpace(groups[i])
		}
		logger.Debug("Set groups claim from comma-separated string value=%v count=%d", groups, len(groups))
		return groups
	}

	logger.Debug("Set groups claim from single string value=%v", v)
	return []string{v}
}

// processGroupsFallback handles unexpected group value types
func processGroupsFallback(value interface{}) []string {
	logger := slogging.Get()
	if str := toString(value); str != "" {
		logger.Debug("Set groups claim from converted value type=%T value=%v", value, str)
		return []string{str}
	}
	logger.Debug("Unsupported groups claim type: %T", value)
	return nil
}

// processSingleClaim processes a single claim based on its type
func processSingleClaim(claimType string, value interface{}, userInfo *UserInfo) {
	switch claimType {
	case "subject_claim":
		userInfo.ID = processStringClaim(value, userInfo.ID, "subject")
	case "email_claim":
		userInfo.Email = processStringClaim(value, userInfo.Email, "email")
	case "name_claim":
		userInfo.Name = processStringClaim(value, userInfo.Name, "name")
	case "given_name_claim":
		userInfo.GivenName = processStringClaim(value, userInfo.GivenName, "given name")
	case "family_name_claim":
		userInfo.FamilyName = processStringClaim(value, userInfo.FamilyName, "family name")
	case "picture_claim":
		userInfo.Picture = processStringClaim(value, userInfo.Picture, "picture")
	case "email_verified_claim":
		userInfo.EmailVerified = toBool(value)
		slogging.Get().Debug("Set email verified claim value=%v", userInfo.EmailVerified)
	case "groups_claim":
		userInfo.Groups = processGroupsClaim(value)
	}
}

// extractClaims extracts claims from JSON data using the provided mappings
func extractClaims(jsonData map[string]interface{}, mappings map[string]string, userInfo *UserInfo) error {
	logger := slogging.Get()
	logger.Debug("Extracting claims from JSON data mappings_count=%v data_keys=%v", len(mappings), getObjectKeys(jsonData))

	// Process each mapping
	for claimType, path := range mappings {
		logger.Debug("Processing claim mapping claim_type=%v path=%v", claimType, path)
		value, err := extractValue(jsonData, path)
		if err != nil {
			// Skip if field not found - it might be in another endpoint
			logger.Debug("Claim extraction failed, skipping claim_type=%v path=%v error=%v", claimType, path, err)
			continue
		}

		processSingleClaim(claimType, value, userInfo)
	}

	logger.Info("Claims extraction completed user_id=%v user_email=%v user_name=%v email_verified=%v groups_count=%v", userInfo.ID, userInfo.Email, userInfo.Name, userInfo.EmailVerified, len(userInfo.Groups))
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
