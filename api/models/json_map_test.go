package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONMap_Value_Nil(t *testing.T) {
	var m JSONMap

	val, err := m.Value()
	require.NoError(t, err)
	assert.Equal(t, "{}", val)
}

func TestJSONMap_Value_Empty(t *testing.T) {
	m := make(JSONMap)

	val, err := m.Value()
	require.NoError(t, err)
	// Value() returns string (not []byte) for Oracle CLOB compatibility
	assert.Equal(t, "{}", val)
}

func TestJSONMap_Value_Simple(t *testing.T) {
	m := JSONMap{
		"name":  "test",
		"count": float64(42), // JSON numbers are float64
	}

	val, err := m.Value()
	require.NoError(t, err)

	// Parse the result back to verify it's valid JSON
	var result JSONMap
	err = result.Scan(val)
	require.NoError(t, err)
	assert.Equal(t, "test", result["name"])
	assert.Equal(t, float64(42), result["count"])
}

func TestJSONMap_Value_Nested(t *testing.T) {
	m := JSONMap{
		"outer": map[string]any{
			"inner": "value",
		},
		"array": []any{"a", "b", "c"},
	}

	val, err := m.Value()
	require.NoError(t, err)

	// Parse the result back to verify structure
	var result JSONMap
	err = result.Scan(val)
	require.NoError(t, err)

	outer, ok := result["outer"].(map[string]any)
	require.True(t, ok, "outer should be a map")
	assert.Equal(t, "value", outer["inner"])

	arr, ok := result["array"].([]any)
	require.True(t, ok, "array should be a slice")
	assert.Len(t, arr, 3)
}

func TestJSONMap_Scan_Nil(t *testing.T) {
	m := JSONMap{"existing": "value"}

	err := m.Scan(nil)
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestJSONMap_Scan_EmptyBytes(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"empty byte slice", []byte{}},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m JSONMap
			err := m.Scan(tt.input)
			require.NoError(t, err)
			assert.Empty(t, m)
		})
	}
}

func TestJSONMap_Scan_Valid(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected JSONMap
	}{
		{
			name:  "simple object from bytes",
			input: []byte(`{"name":"test","count":42}`),
			expected: JSONMap{
				"name":  "test",
				"count": float64(42),
			},
		},
		{
			name:  "simple object from string",
			input: `{"name":"test","count":42}`,
			expected: JSONMap{
				"name":  "test",
				"count": float64(42),
			},
		},
		{
			name:     "empty object",
			input:    []byte(`{}`),
			expected: JSONMap{},
		},
		{
			name:  "object with boolean",
			input: []byte(`{"active":true,"deleted":false}`),
			expected: JSONMap{
				"active":  true,
				"deleted": false,
			},
		},
		{
			name:  "object with null",
			input: []byte(`{"value":null}`),
			expected: JSONMap{
				"value": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m JSONMap
			err := m.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, m)
		})
	}
}

func TestJSONMap_Scan_Nested(t *testing.T) {
	input := []byte(`{"outer":{"inner":"value"},"array":[1,2,3]}`)

	var m JSONMap
	err := m.Scan(input)
	require.NoError(t, err)

	outer, ok := m["outer"].(map[string]any)
	require.True(t, ok, "outer should be a map")
	assert.Equal(t, "value", outer["inner"])

	arr, ok := m["array"].([]any)
	require.True(t, ok, "array should be a slice")
	assert.Len(t, arr, 3)
}

func TestJSONMap_Scan_InvalidJSON(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"malformed JSON", []byte(`{invalid}`)},
		{"unclosed brace", []byte(`{"key": "value"`)},
		{"array instead of object", []byte(`["not","an","object"]`)},
		{"plain string", []byte(`"just a string"`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m JSONMap
			err := m.Scan(tt.input)
			assert.Error(t, err)
		})
	}
}

func TestJSONMap_Scan_UnsupportedType(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"integer", 42},
		{"float", 3.14},
		{"slice", []string{"a", "b"}},
		{"map", map[string]int{"a": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m JSONMap
			err := m.Scan(tt.input)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "cannot scan type")
		})
	}
}

func TestJSONMap_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input JSONMap
	}{
		{"empty map", JSONMap{}},
		{"simple map", JSONMap{"key": "value"}},
		{"numeric values", JSONMap{"int": float64(42), "float": 3.14}},
		{"nested map", JSONMap{
			"outer": map[string]any{
				"inner": "value",
			},
		}},
		{"with array", JSONMap{
			"items": []any{"a", "b", "c"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get the driver value
			val, err := tt.input.Value()
			require.NoError(t, err)

			// Scan it back
			var result JSONMap
			err = result.Scan(val)
			require.NoError(t, err)

			// Compare - need to handle the nil vs empty map case
			if len(tt.input) == 0 {
				assert.Empty(t, result)
			} else {
				assert.Equal(t, tt.input, result)
			}
		})
	}
}
