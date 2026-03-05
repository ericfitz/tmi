package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerializeForAudit(t *testing.T) {
	t.Run("strips server-managed fields", func(t *testing.T) {
		entity := map[string]any{
			"id":            "some-id",
			"created_at":    "2024-01-01",
			"modified_at":   "2024-01-02",
			"update_vector": 5,
			"name":          "Test Entity",
			"description":   "A test",
		}

		data, err := SerializeForAudit(entity)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, unmarshalJSON(data, &result))

		assert.NotContains(t, result, "id")
		assert.NotContains(t, result, "created_at")
		assert.NotContains(t, result, "modified_at")
		assert.NotContains(t, result, "update_vector")
		assert.Equal(t, "Test Entity", result["name"])
		assert.Equal(t, "A test", result["description"])
	})

	t.Run("strips image field", func(t *testing.T) {
		entity := map[string]any{
			"name":  "Diagram",
			"image": "large-svg-data",
		}

		data, err := SerializeForAudit(entity)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, unmarshalJSON(data, &result))

		assert.NotContains(t, result, "image")
		assert.Equal(t, "Diagram", result["name"])
	})

	t.Run("returns as-is for non-objects", func(t *testing.T) {
		entity := []string{"a", "b", "c"}
		data, err := SerializeForAudit(entity)
		require.NoError(t, err)

		// Should return the array JSON as-is
		assert.Contains(t, string(data), "a")
	})
}

func TestShouldAudit(t *testing.T) {
	t.Run("returns false for server-managed-only changes", func(t *testing.T) {
		original := []byte(`{"id":"1","name":"Test","modified_at":"2024-01-01"}`)
		modified := []byte(`{"id":"1","name":"Test","modified_at":"2024-01-02"}`)

		assert.False(t, ShouldAudit(original, modified))
	})

	t.Run("returns true for user field changes", func(t *testing.T) {
		original := []byte(`{"id":"1","name":"Test","description":"old"}`)
		modified := []byte(`{"id":"1","name":"Test","description":"new"}`)

		assert.True(t, ShouldAudit(original, modified))
	})

	t.Run("returns true for added fields", func(t *testing.T) {
		original := []byte(`{"name":"Test"}`)
		modified := []byte(`{"name":"Test","description":"new"}`)

		assert.True(t, ShouldAudit(original, modified))
	})

	t.Run("returns true for removed fields", func(t *testing.T) {
		original := []byte(`{"name":"Test","description":"old"}`)
		modified := []byte(`{"name":"Test"}`)

		assert.True(t, ShouldAudit(original, modified))
	})

	t.Run("returns false for identical objects", func(t *testing.T) {
		original := []byte(`{"name":"Test","description":"same"}`)
		modified := []byte(`{"name":"Test","description":"same"}`)

		assert.False(t, ShouldAudit(original, modified))
	})

	t.Run("returns true for invalid JSON", func(t *testing.T) {
		assert.True(t, ShouldAudit([]byte("invalid"), []byte(`{"name":"Test"}`)))
		assert.True(t, ShouldAudit([]byte(`{"name":"Test"}`), []byte("invalid")))
	})
}

func TestGenerateChangeSummary(t *testing.T) {
	t.Run("shows field changes", func(t *testing.T) {
		original := []byte(`{"name":"Old Name","description":"old desc"}`)
		modified := []byte(`{"name":"New Name","description":"old desc"}`)

		summary := GenerateChangeSummary(original, modified)
		assert.Contains(t, summary, "name:")
		assert.Contains(t, summary, "Old Name")
		assert.Contains(t, summary, "New Name")
	})

	t.Run("shows added fields", func(t *testing.T) {
		original := []byte(`{"name":"Test"}`)
		modified := []byte(`{"name":"Test","description":"new"}`)

		summary := GenerateChangeSummary(original, modified)
		assert.Contains(t, summary, "description: added")
	})

	t.Run("shows removed fields", func(t *testing.T) {
		original := []byte(`{"name":"Test","description":"old"}`)
		modified := []byte(`{"name":"Test"}`)

		summary := GenerateChangeSummary(original, modified)
		assert.Contains(t, summary, "description: removed")
	})

	t.Run("returns no significant changes for identical objects", func(t *testing.T) {
		original := []byte(`{"name":"Test"}`)
		modified := []byte(`{"name":"Test"}`)

		summary := GenerateChangeSummary(original, modified)
		assert.Equal(t, "no significant changes", summary)
	})

	t.Run("excludes server-managed fields", func(t *testing.T) {
		original := []byte(`{"name":"Test","modified_at":"2024-01-01"}`)
		modified := []byte(`{"name":"Test","modified_at":"2024-01-02"}`)

		summary := GenerateChangeSummary(original, modified)
		assert.Equal(t, "no significant changes", summary)
	})
}

func TestComputeReverseDiff(t *testing.T) {
	t.Run("computes reverse diff", func(t *testing.T) {
		before := []byte(`{"name":"Before","value":1}`)
		after := []byte(`{"name":"After","value":2}`)

		diff, err := ComputeReverseDiff(before, after)
		require.NoError(t, err)

		// Applying the reverse diff to 'after' should give us 'before'
		restored, err := ApplyDiff(after, diff)
		require.NoError(t, err)

		var restoredMap, beforeMap map[string]any
		require.NoError(t, unmarshalJSON(restored, &restoredMap))
		require.NoError(t, unmarshalJSON(before, &beforeMap))

		assert.Equal(t, beforeMap, restoredMap)
	})
}

func TestApplyDiff(t *testing.T) {
	t.Run("applies merge patch", func(t *testing.T) {
		state := []byte(`{"name":"Original","value":1}`)
		diff := []byte(`{"name":"Modified"}`)

		result, err := ApplyDiff(state, diff)
		require.NoError(t, err)

		var resultMap map[string]any
		require.NoError(t, unmarshalJSON(result, &resultMap))

		assert.Equal(t, "Modified", resultMap["name"])
		assert.Equal(t, float64(1), resultMap["value"])
	})
}

func TestTruncateValue(t *testing.T) {
	t.Run("does not truncate short strings", func(t *testing.T) {
		assert.Equal(t, "short", truncateValue("short", 50))
	})

	t.Run("truncates long strings", func(t *testing.T) {
		long := "this is a very long string that should be truncated at some point"
		result := truncateValue(long, 20)
		assert.Len(t, result, 20)
		assert.True(t, len(result) <= 20)
		assert.Contains(t, result, "...")
	})
}

// unmarshalJSON is a helper for test readability
func unmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
