package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFixOwnerField tests the owner field fix that converts string owner values
// to User objects after JSON patching.
func TestFixOwnerField(t *testing.T) {
	t.Run("string_owner_with_at_sign_uses_as_email", func(t *testing.T) {
		modified := []byte(`{"owner": "alice@example.com", "name": "test"}`)
		original := []byte(`{"owner": {"provider_id": "old"}, "name": "test"}`)

		result := fixOwnerField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))

		owner := data["owner"].(map[string]interface{})
		assert.Equal(t, "alice@example.com", owner["provider_id"])
		assert.Equal(t, "alice@example.com", owner["email"])
		assert.Equal(t, "user", owner["principal_type"])
	})

	t.Run("string_owner_without_at_sign_appends_example_domain", func(t *testing.T) {
		modified := []byte(`{"owner": "alice-provider-id", "name": "test"}`)
		original := []byte(`{"owner": {"provider_id": "old"}, "name": "test"}`)

		result := fixOwnerField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))

		owner := data["owner"].(map[string]interface{})
		assert.Equal(t, "alice-provider-id", owner["provider_id"])
		assert.Equal(t, "alice-provider-id@example.com", owner["email"],
			"Email should have @example.com appended when owner string has no @")
	})

	t.Run("object_owner_not_modified", func(t *testing.T) {
		modified := []byte(`{"owner": {"principal_type": "user", "provider_id": "alice"}, "name": "test"}`)
		original := []byte(`{"owner": {"provider_id": "old"}, "name": "test"}`)

		result := fixOwnerField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))

		owner := data["owner"].(map[string]interface{})
		// Object owner should pass through unchanged
		assert.Equal(t, "alice", owner["provider_id"])
		assert.Equal(t, "user", owner["principal_type"])
	})

	t.Run("no_owner_field", func(t *testing.T) {
		modified := []byte(`{"name": "test"}`)
		original := []byte(`{"name": "old"}`)

		result := fixOwnerField(modified, original)
		assert.JSONEq(t, `{"name": "test"}`, string(result))
	})

	t.Run("null_owner", func(t *testing.T) {
		modified := []byte(`{"owner": null, "name": "test"}`)
		original := []byte(`{"owner": {"provider_id": "old"}, "name": "test"}`)

		result := fixOwnerField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))
		assert.Nil(t, data["owner"], "Null owner should remain null")
	})

	t.Run("invalid_json_returns_original", func(t *testing.T) {
		modified := []byte(`not json`)
		original := []byte(`{}`)

		result := fixOwnerField(modified, original)
		assert.Equal(t, modified, result)
	})
}

// TestFixMetadataField tests metadata field preservation during patching.
func TestFixMetadataField(t *testing.T) {
	t.Run("preserves_empty_array_when_patch_nullifies", func(t *testing.T) {
		// When original has empty array and patch produces null, restore empty array
		original := []byte(`{"metadata": [], "name": "test"}`)
		modified := []byte(`{"metadata": null, "name": "patched"}`)

		result := fixMetadataField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))
		assert.NotNil(t, data["metadata"], "Empty array metadata should not become null")
		// Should be an empty array, not null
		arr, ok := data["metadata"].([]interface{})
		assert.True(t, ok, "Metadata should remain an array")
		assert.Empty(t, arr)
	})

	t.Run("non_empty_array_not_modified", func(t *testing.T) {
		original := []byte(`{"metadata": [{"key": "k", "value": "v"}], "name": "test"}`)
		modified := []byte(`{"metadata": [{"key": "k", "value": "v"}], "name": "patched"}`)

		result := fixMetadataField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))
		arr := data["metadata"].([]interface{})
		assert.Len(t, arr, 1)
	})

	t.Run("no_metadata_field_unchanged", func(t *testing.T) {
		original := []byte(`{"name": "test"}`)
		modified := []byte(`{"name": "patched"}`)

		result := fixMetadataField(modified, original)
		assert.JSONEq(t, `{"name": "patched"}`, string(result))
	})

	t.Run("invalid_json_returns_modified", func(t *testing.T) {
		modified := []byte(`not json`)
		original := []byte(`{}`)

		result := fixMetadataField(modified, original)
		assert.Equal(t, modified, result)
	})
}

// TestFixImageField tests image field preservation during patching.
func TestFixImageField(t *testing.T) {
	t.Run("restores_image_map_when_patch_nullifies", func(t *testing.T) {
		original := []byte(`{"image": {"svg": "<svg/>", "width": 100}, "name": "test"}`)
		modified := []byte(`{"image": null, "name": "patched"}`)

		result := fixImageField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))
		assert.NotNil(t, data["image"], "Non-null image should be restored when patch nullifies")
		img := data["image"].(map[string]interface{})
		assert.Equal(t, "<svg/>", img["svg"])
	})

	t.Run("restores_image_when_field_removed", func(t *testing.T) {
		original := []byte(`{"image": {"svg": "<svg/>"}, "name": "test"}`)
		modified := []byte(`{"name": "patched"}`)

		result := fixImageField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))
		assert.NotNil(t, data["image"], "Image should be restored when field is missing from modified")
	})

	t.Run("null_original_image_stays_null", func(t *testing.T) {
		original := []byte(`{"image": null, "name": "test"}`)
		modified := []byte(`{"image": null, "name": "patched"}`)

		result := fixImageField(modified, original)

		var data map[string]interface{}
		require.NoError(t, json.Unmarshal(result, &data))
		assert.Nil(t, data["image"])
	})

	t.Run("invalid_json_returns_modified", func(t *testing.T) {
		modified := []byte(`not json`)
		original := []byte(`{}`)

		result := fixImageField(modified, original)
		assert.Equal(t, modified, result)
	})
}

// TestPreprocessPatchOperations_SVG tests base64 SVG decoding in patch preprocessing.
func TestPreprocessPatchOperations_SVG(t *testing.T) {
	t.Run("decodes_base64_svg", func(t *testing.T) {
		svgContent := "<svg><circle r='10'/></svg>"
		encoded := base64.StdEncoding.EncodeToString([]byte(svgContent))

		ops := []PatchOperation{
			{Op: "replace", Path: "/image/svg", Value: encoded},
		}

		result, err := preprocessPatchOperations(ops)
		require.NoError(t, err)
		assert.Equal(t, []byte(svgContent), result[0].Value)
	})

	t.Run("invalid_base64_returns_error", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: "replace", Path: "/image/svg", Value: "not-valid-base64!!!"},
		}

		_, err := preprocessPatchOperations(ops)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode base64")
	})

	t.Run("non_svg_path_untouched", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "test"},
		}

		result, err := preprocessPatchOperations(ops)
		require.NoError(t, err)
		assert.Equal(t, "test", result[0].Value)
	})

	t.Run("svg_with_non_string_value_untouched", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: "replace", Path: "/image/svg", Value: 42},
		}

		result, err := preprocessPatchOperations(ops)
		require.NoError(t, err)
		assert.Equal(t, 42, result[0].Value, "Non-string SVG value should pass through")
	})

	t.Run("svg_remove_operation_untouched", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: "remove", Path: "/image/svg"},
		}

		result, err := preprocessPatchOperations(ops)
		require.NoError(t, err)
		assert.Equal(t, "remove", result[0].Op)
	})

	t.Run("invalid_utf8_svg_returns_error", func(t *testing.T) {
		// Create base64 of invalid UTF-8 bytes
		invalidUTF8 := []byte{0xff, 0xfe, 0x80, 0x81}
		encoded := base64.StdEncoding.EncodeToString(invalidUTF8)

		ops := []PatchOperation{
			{Op: "replace", Path: "/image/svg", Value: encoded},
		}

		_, err := preprocessPatchOperations(ops)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid UTF-8")
	})

	t.Run("empty_operations_list", func(t *testing.T) {
		result, err := preprocessPatchOperations([]PatchOperation{})
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

// TestCheckOwnershipChanges_NearMissPaths verifies that paths similar to /owner
// and /authorization don't accidentally trigger ownership change detection.
func TestCheckOwnershipChanges_NearMissPaths(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		expectedOwner bool
		expectedAuth  bool
	}{
		{"/owner_id is not /owner", "/owner_id", false, false},
		{"/owners is not /owner", "/owners", false, false},
		{"/authorization_note is not /authorization", "/authorization_note", false, false},
		{"/auth is not /authorization", "/auth", false, false},
		{"/authorized is not /authorization", "/authorized", false, false},
		{"/owner/name is not tracked", "/owner/name", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := []PatchOperation{{Op: "replace", Path: tt.path, Value: "test"}}
			owner, auth := CheckOwnershipChanges(ops)
			assert.Equal(t, tt.expectedOwner, owner, "owner change for path %s", tt.path)
			assert.Equal(t, tt.expectedAuth, auth, "auth change for path %s", tt.path)
		})
	}
}

// TestApplyPatchOperations_EdgeCases tests edge cases in the generic patch pipeline.
func TestApplyPatchOperations_EdgeCases(t *testing.T) {
	type SimpleEntity struct {
		Name string `json:"name"`
	}

	t.Run("empty_operations_returns_original", func(t *testing.T) {
		original := SimpleEntity{Name: "original"}
		result, err := ApplyPatchOperations(original, []PatchOperation{})
		require.NoError(t, err)
		assert.Equal(t, "original", result.Name)
	})

	t.Run("test_operation_does_not_modify", func(t *testing.T) {
		original := SimpleEntity{Name: "original"}
		ops := []PatchOperation{
			{Op: "test", Path: "/name", Value: "original"},
		}
		result, err := ApplyPatchOperations(original, ops)
		require.NoError(t, err)
		assert.Equal(t, "original", result.Name,
			"Test operation should verify but not modify")
	})

	t.Run("test_operation_fails_on_mismatch", func(t *testing.T) {
		original := SimpleEntity{Name: "original"}
		ops := []PatchOperation{
			{Op: "test", Path: "/name", Value: "wrong"},
		}
		_, err := ApplyPatchOperations(original, ops)
		require.Error(t, err, "Test operation with wrong value should fail")
		var reqErr *RequestError
		require.True(t, errors.As(err, &reqErr))
		assert.Equal(t, "patch_failed", reqErr.Code)
	})

	t.Run("replace_nonexistent_path_returns_error", func(t *testing.T) {
		// RFC 6902 Section 4.3: replace on a nonexistent path MUST fail.
		// Pre-validation catches this before the library silently adds the field.
		original := SimpleEntity{Name: "original"}
		ops := []PatchOperation{
			{Op: "replace", Path: "/nonexistent", Value: "value"},
		}
		_, err := ApplyPatchOperations(original, ops)
		require.Error(t, err, "Replace on nonexistent path should fail per RFC 6902")
		var reqErr *RequestError
		require.True(t, errors.As(err, &reqErr))
		assert.Equal(t, "patch_failed", reqErr.Code)
		assert.Contains(t, reqErr.Message, "/nonexistent")
	})

	t.Run("add_to_nonexistent_nested_path_fails", func(t *testing.T) {
		original := SimpleEntity{Name: "original"}
		ops := []PatchOperation{
			{Op: "add", Path: "/nested/deep/path", Value: "value"},
		}
		_, err := ApplyPatchOperations(original, ops)
		require.Error(t, err, "Adding to a deeply nested nonexistent path should fail")
	})
}

// TestConvertJSONPatchToCellOperations documents the placeholder behavior.
func TestConvertJSONPatchToCellOperations(t *testing.T) {
	t.Run("returns_empty_cell_operations", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: "replace", Path: "/cells/0", Value: map[string]interface{}{"id": "cell-1"}},
		}

		result, err := ConvertJSONPatchToCellOperations(ops)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "patch", result.Type)
		assert.Empty(t, result.Cells,
			"Placeholder implementation returns empty cells regardless of input")
	})

	t.Run("nil_operations", func(t *testing.T) {
		result, err := ConvertJSONPatchToCellOperations(nil)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result.Cells)
	})
}
