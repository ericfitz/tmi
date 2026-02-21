package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONRaw_Value_Nil(t *testing.T) {
	var j JSONRaw

	val, err := j.Value()
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestJSONRaw_Value_Valid(t *testing.T) {
	// Value() returns string (not []byte) for Oracle CLOB compatibility
	tests := []struct {
		name     string
		input    JSONRaw
		expected string
	}{
		{
			name:     "simple object",
			input:    JSONRaw(`{"key":"value"}`),
			expected: `{"key":"value"}`,
		},
		{
			name:     "array",
			input:    JSONRaw(`[1,2,3]`),
			expected: `[1,2,3]`,
		},
		{
			name:     "string",
			input:    JSONRaw(`"hello"`),
			expected: `"hello"`,
		},
		{
			name:     "number",
			input:    JSONRaw(`42`),
			expected: `42`,
		},
		{
			name:     "boolean",
			input:    JSONRaw(`true`),
			expected: `true`,
		},
		{
			name:     "null",
			input:    JSONRaw(`null`),
			expected: `null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, val)
		})
	}
}

func TestJSONRaw_Scan_Nil(t *testing.T) {
	j := JSONRaw(`{"existing":"data"}`)

	err := j.Scan(nil)
	require.NoError(t, err)
	assert.Nil(t, j)
}

func TestJSONRaw_Scan_Bytes(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected JSONRaw
	}{
		{
			name:     "object",
			input:    []byte(`{"key":"value"}`),
			expected: JSONRaw(`{"key":"value"}`),
		},
		{
			name:     "array",
			input:    []byte(`[1,2,3]`),
			expected: JSONRaw(`[1,2,3]`),
		},
		{
			name:     "nested",
			input:    []byte(`{"outer":{"inner":"value"}}`),
			expected: JSONRaw(`{"outer":{"inner":"value"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSONRaw
			err := j.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, j)
		})
	}
}

func TestJSONRaw_Scan_String(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected JSONRaw
	}{
		{
			name:     "object",
			input:    `{"key":"value"}`,
			expected: JSONRaw(`{"key":"value"}`),
		},
		{
			name:     "array",
			input:    `["a","b","c"]`,
			expected: JSONRaw(`["a","b","c"]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSONRaw
			err := j.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, j)
		})
	}
}

func TestJSONRaw_Scan_UnsupportedType(t *testing.T) {
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
			var j JSONRaw
			err := j.Scan(tt.input)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "cannot scan type")
		})
	}
}

func TestJSONRaw_MarshalJSON_Nil(t *testing.T) {
	var j JSONRaw

	data, err := json.Marshal(j)
	require.NoError(t, err)
	assert.Equal(t, []byte("null"), data)
}

func TestJSONRaw_MarshalJSON_Valid(t *testing.T) {
	tests := []struct {
		name     string
		input    JSONRaw
		expected []byte
	}{
		{
			name:     "object",
			input:    JSONRaw(`{"key":"value"}`),
			expected: []byte(`{"key":"value"}`),
		},
		{
			name:     "array",
			input:    JSONRaw(`[1,2,3]`),
			expected: []byte(`[1,2,3]`),
		},
		{
			name:     "string",
			input:    JSONRaw(`"hello"`),
			expected: []byte(`"hello"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, data)
		})
	}
}

func TestJSONRaw_UnmarshalJSON_Valid(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected JSONRaw
	}{
		{
			name:     "object",
			input:    []byte(`{"key":"value"}`),
			expected: JSONRaw(`{"key":"value"}`),
		},
		{
			name:     "array",
			input:    []byte(`[1,2,3]`),
			expected: JSONRaw(`[1,2,3]`),
		},
		{
			name:     "null",
			input:    []byte(`null`),
			expected: JSONRaw(`null`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JSONRaw
			err := json.Unmarshal(tt.input, &j)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, j)
		})
	}
}

func TestJSONRaw_UnmarshalJSON_NilPointer(t *testing.T) {
	var j *JSONRaw

	err := j.UnmarshalJSON([]byte(`{"key":"value"}`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil pointer")
}

func TestJSONRaw_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input JSONRaw
	}{
		{"object", JSONRaw(`{"key":"value"}`)},
		{"array", JSONRaw(`[1,2,3]`)},
		{"nested", JSONRaw(`{"outer":{"inner":"value"}}`)},
		{"complex", JSONRaw(`{"users":[{"name":"alice"},{"name":"bob"}]}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get the driver value
			val, err := tt.input.Value()
			require.NoError(t, err)

			// Scan it back
			var result JSONRaw
			err = result.Scan(val)
			require.NoError(t, err)

			// Should match original
			assert.Equal(t, tt.input, result)
		})
	}
}

func TestJSONRaw_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input JSONRaw
	}{
		{"object", JSONRaw(`{"key":"value"}`)},
		{"array", JSONRaw(`[1,2,3]`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.input)
			require.NoError(t, err)

			// Unmarshal
			var result JSONRaw
			err = json.Unmarshal(data, &result)
			require.NoError(t, err)

			// Should match original
			assert.Equal(t, tt.input, result)
		})
	}
}
