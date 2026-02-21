package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for claim_extractor.go functions

func TestExtractValue(t *testing.T) {
	t.Run("extracts simple field from map", func(t *testing.T) {
		data := map[string]any{
			"email": "user@example.com",
			"name":  "Test User",
		}

		value, err := extractValue(data, "email")
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", value)
	})

	t.Run("extracts nested field from map", func(t *testing.T) {
		data := map[string]any{
			"user": map[string]any{
				"profile": map[string]any{
					"email": "nested@example.com",
				},
			},
		}

		value, err := extractValue(data, "user.profile.email")
		require.NoError(t, err)
		assert.Equal(t, "nested@example.com", value)
	})

	t.Run("extracts value from array by index", func(t *testing.T) {
		data := map[string]any{
			"items": []any{
				map[string]any{"name": "first"},
				map[string]any{"name": "second"},
			},
		}

		value, err := extractValue(data, "items.[0].name")
		require.NoError(t, err)
		assert.Equal(t, "first", value)

		value, err = extractValue(data, "items.[1].name")
		require.NoError(t, err)
		assert.Equal(t, "second", value)
	})

	t.Run("extracts all values from array with wildcard", func(t *testing.T) {
		data := map[string]any{
			"groups": []any{
				map[string]any{"name": "admins"},
				map[string]any{"name": "users"},
				map[string]any{"name": "developers"},
			},
		}

		value, err := extractValue(data, "groups.[*].name")
		require.NoError(t, err)

		arr, ok := value.([]any)
		require.True(t, ok)
		assert.Len(t, arr, 3)
		assert.Equal(t, "admins", arr[0])
		assert.Equal(t, "users", arr[1])
		assert.Equal(t, "developers", arr[2])
	})

	t.Run("returns literal true value", func(t *testing.T) {
		data := map[string]any{}

		value, err := extractValue(data, "true")
		require.NoError(t, err)
		assert.Equal(t, true, value)
	})

	t.Run("returns literal false value", func(t *testing.T) {
		data := map[string]any{}

		value, err := extractValue(data, "false")
		require.NoError(t, err)
		assert.Equal(t, false, value)
	})

	t.Run("returns literal number value", func(t *testing.T) {
		data := map[string]any{}

		value, err := extractValue(data, "42")
		require.NoError(t, err)
		assert.Equal(t, float64(42), value)
	})

	t.Run("returns error for missing field", func(t *testing.T) {
		data := map[string]any{
			"email": "user@example.com",
		}

		_, err := extractValue(data, "missing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "field not found")
	})

	t.Run("returns error for array index out of bounds", func(t *testing.T) {
		data := map[string]any{
			"items": []any{"first", "second"},
		}

		_, err := extractValue(data, "items.[5]")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "array index out of bounds")
	})

	t.Run("returns error when expecting array but getting object", func(t *testing.T) {
		data := map[string]any{
			"items": map[string]any{"key": "value"},
		}

		_, err := extractValue(data, "items.[0]")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected array")
	})

	t.Run("returns error when expecting object but getting array", func(t *testing.T) {
		data := map[string]any{
			"items": []any{"first", "second"},
		}

		_, err := extractValue(data, "items.key")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected object")
	})

	t.Run("handles empty path segments", func(t *testing.T) {
		data := map[string]any{
			"email": "user@example.com",
		}

		// Path with leading/trailing dots
		value, err := extractValue(data, ".email")
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", value)
	})

	t.Run("extracts whole array with [*] at end", func(t *testing.T) {
		data := map[string]any{
			"roles": []any{"admin", "user", "moderator"},
		}

		value, err := extractValue(data, "roles.[*]")
		require.NoError(t, err)

		arr, ok := value.([]any)
		require.True(t, ok)
		assert.Equal(t, []any{"admin", "user", "moderator"}, arr)
	})

	t.Run("handles negative array index error", func(t *testing.T) {
		data := map[string]any{
			"items": []any{"first", "second"},
		}

		_, err := extractValue(data, "items.[-1]")
		assert.Error(t, err)
	})
}

func TestToString(t *testing.T) {
	t.Run("converts string to string", func(t *testing.T) {
		result := toString("hello")
		assert.Equal(t, "hello", result)
	})

	t.Run("converts float64 to string", func(t *testing.T) {
		result := toString(float64(42))
		assert.Equal(t, "42", result)
	})

	t.Run("converts float64 with decimals to string", func(t *testing.T) {
		result := toString(float64(3.14))
		assert.Equal(t, "3", result) // Uses %.0f format
	})

	t.Run("converts bool true to string", func(t *testing.T) {
		result := toString(true)
		assert.Equal(t, "true", result)
	})

	t.Run("converts bool false to string", func(t *testing.T) {
		result := toString(false)
		assert.Equal(t, "false", result)
	})

	t.Run("converts nil to empty string", func(t *testing.T) {
		result := toString(nil)
		assert.Equal(t, "", result)
	})

	t.Run("converts slice to JSON string", func(t *testing.T) {
		result := toString([]string{"a", "b", "c"})
		assert.Equal(t, `["a","b","c"]`, result)
	})

	t.Run("converts map to JSON string", func(t *testing.T) {
		result := toString(map[string]string{"key": "value"})
		assert.Equal(t, `{"key":"value"}`, result)
	})
}

func TestToBool(t *testing.T) {
	t.Run("returns true for bool true", func(t *testing.T) {
		result := toBool(true)
		assert.True(t, result)
	})

	t.Run("returns false for bool false", func(t *testing.T) {
		result := toBool(false)
		assert.False(t, result)
	})

	t.Run("returns true for string 'true'", func(t *testing.T) {
		result := toBool("true")
		assert.True(t, result)
	})

	t.Run("returns true for string '1'", func(t *testing.T) {
		result := toBool("1")
		assert.True(t, result)
	})

	t.Run("returns true for string 'yes'", func(t *testing.T) {
		result := toBool("yes")
		assert.True(t, result)
	})

	t.Run("returns false for string 'false'", func(t *testing.T) {
		result := toBool("false")
		assert.False(t, result)
	})

	t.Run("returns false for empty string", func(t *testing.T) {
		result := toBool("")
		assert.False(t, result)
	})

	t.Run("returns true for non-zero float64", func(t *testing.T) {
		result := toBool(float64(1))
		assert.True(t, result)
	})

	t.Run("returns false for zero float64", func(t *testing.T) {
		result := toBool(float64(0))
		assert.False(t, result)
	})

	t.Run("returns false for unsupported type", func(t *testing.T) {
		result := toBool([]string{"a", "b"})
		assert.False(t, result)
	})

	t.Run("returns false for nil", func(t *testing.T) {
		result := toBool(nil)
		assert.False(t, result)
	})
}

func TestProcessGroupsClaim(t *testing.T) {
	t.Run("processes array of groups", func(t *testing.T) {
		input := []any{"admins", "users", "developers"}
		result := processGroupsClaim(input)
		assert.Equal(t, []string{"admins", "users", "developers"}, result)
	})

	t.Run("processes single string group", func(t *testing.T) {
		result := processGroupsClaim("admins")
		assert.Equal(t, []string{"admins"}, result)
	})

	t.Run("processes comma-separated string groups", func(t *testing.T) {
		result := processGroupsClaim("admins, users, developers")
		assert.Equal(t, []string{"admins", "users", "developers"}, result)
	})

	t.Run("returns nil for empty string", func(t *testing.T) {
		result := processGroupsClaim("")
		assert.Nil(t, result)
	})

	t.Run("handles mixed types in array", func(t *testing.T) {
		input := []any{"group1", float64(2), true}
		result := processGroupsClaim(input)
		assert.Contains(t, result, "group1")
		assert.Contains(t, result, "2")
		assert.Contains(t, result, "true")
	})

	t.Run("filters empty strings from array", func(t *testing.T) {
		input := []any{"group1", "", "group2"}
		result := processGroupsClaim(input)
		assert.Equal(t, []string{"group1", "group2"}, result)
	})

	t.Run("handles fallback for unsupported type", func(t *testing.T) {
		result := processGroupsClaim(float64(42))
		assert.Equal(t, []string{"42"}, result)
	})
}

func TestProcessGroupsArray(t *testing.T) {
	t.Run("converts interface array to string array", func(t *testing.T) {
		input := []any{"a", "b", "c"}
		result := processGroupsArray(input)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("skips empty strings", func(t *testing.T) {
		input := []any{"a", "", "b", "", "c"}
		result := processGroupsArray(input)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("returns empty slice for empty input", func(t *testing.T) {
		input := []any{}
		result := processGroupsArray(input)
		assert.Empty(t, result)
	})
}

func TestProcessGroupsString(t *testing.T) {
	t.Run("returns nil for empty string", func(t *testing.T) {
		result := processGroupsString("")
		assert.Nil(t, result)
	})

	t.Run("returns single-element slice for single group", func(t *testing.T) {
		result := processGroupsString("admins")
		assert.Equal(t, []string{"admins"}, result)
	})

	t.Run("splits comma-separated groups", func(t *testing.T) {
		result := processGroupsString("admins,users,developers")
		assert.Equal(t, []string{"admins", "users", "developers"}, result)
	})

	t.Run("trims whitespace from groups", func(t *testing.T) {
		result := processGroupsString("admins , users , developers")
		assert.Equal(t, []string{"admins", "users", "developers"}, result)
	})
}

func TestProcessGroupsFallback(t *testing.T) {
	t.Run("converts number to single group", func(t *testing.T) {
		result := processGroupsFallback(float64(42))
		assert.Equal(t, []string{"42"}, result)
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		result := processGroupsFallback(nil)
		assert.Nil(t, result)
	})

	t.Run("converts struct to JSON string", func(t *testing.T) {
		result := processGroupsFallback(map[string]string{"name": "group"})
		assert.Len(t, result, 1)
		assert.Contains(t, result[0], "group")
	})
}

func TestProcessSingleClaim(t *testing.T) {
	t.Run("processes subject claim", func(t *testing.T) {
		userInfo := &UserInfo{}
		processSingleClaim("subject_claim", "user-123", userInfo)
		assert.Equal(t, "user-123", userInfo.ID)
	})

	t.Run("processes email claim", func(t *testing.T) {
		userInfo := &UserInfo{}
		processSingleClaim("email_claim", "user@example.com", userInfo)
		assert.Equal(t, "user@example.com", userInfo.Email)
	})

	t.Run("processes name claim", func(t *testing.T) {
		userInfo := &UserInfo{}
		processSingleClaim("name_claim", "John Doe", userInfo)
		assert.Equal(t, "John Doe", userInfo.Name)
	})

	t.Run("processes given_name claim", func(t *testing.T) {
		userInfo := &UserInfo{}
		processSingleClaim("given_name_claim", "John", userInfo)
		assert.Equal(t, "John", userInfo.GivenName)
	})

	t.Run("processes family_name claim", func(t *testing.T) {
		userInfo := &UserInfo{}
		processSingleClaim("family_name_claim", "Doe", userInfo)
		assert.Equal(t, "Doe", userInfo.FamilyName)
	})

	t.Run("processes picture claim", func(t *testing.T) {
		userInfo := &UserInfo{}
		processSingleClaim("picture_claim", "https://example.com/photo.jpg", userInfo)
		assert.Equal(t, "https://example.com/photo.jpg", userInfo.Picture)
	})

	t.Run("processes email_verified claim", func(t *testing.T) {
		userInfo := &UserInfo{}
		processSingleClaim("email_verified_claim", true, userInfo)
		assert.True(t, userInfo.EmailVerified)
	})

	t.Run("processes groups claim with array", func(t *testing.T) {
		userInfo := &UserInfo{}
		processSingleClaim("groups_claim", []any{"admins", "users"}, userInfo)
		assert.Equal(t, []string{"admins", "users"}, userInfo.Groups)
	})

	t.Run("does not overwrite existing values", func(t *testing.T) {
		userInfo := &UserInfo{Email: "existing@example.com"}
		processSingleClaim("email_claim", "new@example.com", userInfo)
		assert.Equal(t, "existing@example.com", userInfo.Email)
	})
}

func TestExtractClaims(t *testing.T) {
	t.Run("extracts all claims with standard mappings", func(t *testing.T) {
		jsonData := map[string]any{
			"sub":            "user-123",
			"email":          "user@example.com",
			"name":           "Test User",
			"given_name":     "Test",
			"family_name":    "User",
			"picture":        "https://example.com/photo.jpg",
			"email_verified": true,
			"groups":         []any{"admins", "developers"},
		}

		mappings := map[string]string{
			"subject_claim":        "sub",
			"email_claim":          "email",
			"name_claim":           "name",
			"given_name_claim":     "given_name",
			"family_name_claim":    "family_name",
			"picture_claim":        "picture",
			"email_verified_claim": "email_verified",
			"groups_claim":         "groups",
		}

		userInfo := &UserInfo{}
		err := extractClaims(jsonData, mappings, userInfo)
		require.NoError(t, err)

		assert.Equal(t, "user-123", userInfo.ID)
		assert.Equal(t, "user@example.com", userInfo.Email)
		assert.Equal(t, "Test User", userInfo.Name)
		assert.Equal(t, "Test", userInfo.GivenName)
		assert.Equal(t, "User", userInfo.FamilyName)
		assert.Equal(t, "https://example.com/photo.jpg", userInfo.Picture)
		assert.True(t, userInfo.EmailVerified)
		assert.Equal(t, []string{"admins", "developers"}, userInfo.Groups)
	})

	t.Run("skips missing claims without error", func(t *testing.T) {
		jsonData := map[string]any{
			"email": "user@example.com",
		}

		mappings := map[string]string{
			"email_claim": "email",
			"name_claim":  "name", // Not present in data
		}

		userInfo := &UserInfo{}
		err := extractClaims(jsonData, mappings, userInfo)
		require.NoError(t, err)

		assert.Equal(t, "user@example.com", userInfo.Email)
		assert.Equal(t, "", userInfo.Name)
	})

	t.Run("handles nested claim mappings", func(t *testing.T) {
		jsonData := map[string]any{
			"profile": map[string]any{
				"contact": map[string]any{
					"email": "nested@example.com",
				},
			},
		}

		mappings := map[string]string{
			"email_claim": "profile.contact.email",
		}

		userInfo := &UserInfo{}
		err := extractClaims(jsonData, mappings, userInfo)
		require.NoError(t, err)

		assert.Equal(t, "nested@example.com", userInfo.Email)
	})
}

func TestApplyDefaultMappings(t *testing.T) {
	t.Run("applies defaults for missing mappings", func(t *testing.T) {
		jsonData := map[string]any{
			"sub":   "user-123",
			"email": "user@example.com",
		}

		mappings := map[string]string{}
		applyDefaultMappings(mappings, jsonData)

		assert.Equal(t, "sub", mappings["subject_claim"])
		assert.Equal(t, "email", mappings["email_claim"])
	})

	t.Run("does not override existing mappings", func(t *testing.T) {
		jsonData := map[string]any{
			"sub":       "user-123",
			"custom_id": "custom-456",
		}

		mappings := map[string]string{
			"subject_claim": "custom_id",
		}

		applyDefaultMappings(mappings, jsonData)

		assert.Equal(t, "custom_id", mappings["subject_claim"])
	})

	t.Run("only applies defaults for fields that exist in data", func(t *testing.T) {
		jsonData := map[string]any{
			"email": "user@example.com",
			// No "sub" field
		}

		mappings := map[string]string{}
		applyDefaultMappings(mappings, jsonData)

		assert.Equal(t, "email", mappings["email_claim"])
		_, exists := mappings["subject_claim"]
		assert.False(t, exists, "should not add mapping for missing field")
	})

	t.Run("applies all available defaults", func(t *testing.T) {
		jsonData := map[string]any{
			"sub":            "user-123",
			"email":          "user@example.com",
			"name":           "Test User",
			"given_name":     "Test",
			"family_name":    "User",
			"picture":        "https://example.com/photo.jpg",
			"email_verified": true,
			"groups":         []any{"admins"},
		}

		mappings := map[string]string{}
		applyDefaultMappings(mappings, jsonData)

		assert.Len(t, mappings, 8, "should apply all 8 default mappings")
		assert.Equal(t, "sub", mappings["subject_claim"])
		assert.Equal(t, "email", mappings["email_claim"])
		assert.Equal(t, "name", mappings["name_claim"])
		assert.Equal(t, "given_name", mappings["given_name_claim"])
		assert.Equal(t, "family_name", mappings["family_name_claim"])
		assert.Equal(t, "picture", mappings["picture_claim"])
		assert.Equal(t, "email_verified", mappings["email_verified_claim"])
		assert.Equal(t, "groups", mappings["groups_claim"])
	})
}

func TestGetObjectKeys(t *testing.T) {
	t.Run("returns keys from map", func(t *testing.T) {
		obj := map[string]any{
			"email": "user@example.com",
			"name":  "Test User",
			"id":    "123",
		}

		keys := getObjectKeys(obj)
		assert.Len(t, keys, 3)
		assert.Contains(t, keys, "email")
		assert.Contains(t, keys, "name")
		assert.Contains(t, keys, "id")
	})

	t.Run("returns empty slice for empty map", func(t *testing.T) {
		obj := map[string]any{}
		keys := getObjectKeys(obj)
		assert.Empty(t, keys)
	})
}

func TestProcessStringClaim(t *testing.T) {
	t.Run("sets value when current is empty", func(t *testing.T) {
		result := processStringClaim("new-value", "", "email")
		assert.Equal(t, "new-value", result)
	})

	t.Run("preserves existing value", func(t *testing.T) {
		result := processStringClaim("new-value", "existing-value", "email")
		assert.Equal(t, "existing-value", result)
	})

	t.Run("converts non-string to string", func(t *testing.T) {
		result := processStringClaim(float64(42), "", "id")
		assert.Equal(t, "42", result)
	})
}

func TestDefaultClaimMappings(t *testing.T) {
	t.Run("contains standard OIDC claims", func(t *testing.T) {
		assert.Equal(t, "sub", DefaultClaimMappings["subject_claim"])
		assert.Equal(t, "email", DefaultClaimMappings["email_claim"])
		assert.Equal(t, "name", DefaultClaimMappings["name_claim"])
		assert.Equal(t, "given_name", DefaultClaimMappings["given_name_claim"])
		assert.Equal(t, "family_name", DefaultClaimMappings["family_name_claim"])
		assert.Equal(t, "picture", DefaultClaimMappings["picture_claim"])
		assert.Equal(t, "email_verified", DefaultClaimMappings["email_verified_claim"])
		assert.Equal(t, "groups", DefaultClaimMappings["groups_claim"])
	})
}
