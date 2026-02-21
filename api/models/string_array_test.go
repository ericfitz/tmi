package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringArray_Value_Empty(t *testing.T) {
	var arr StringArray

	val, err := arr.Value()
	require.NoError(t, err)
	assert.Equal(t, "[]", val)
}

func TestStringArray_Value_Single(t *testing.T) {
	arr := StringArray{"hello"}

	val, err := arr.Value()
	require.NoError(t, err)
	assert.Equal(t, `["hello"]`, val)
}

func TestStringArray_Value_Multiple(t *testing.T) {
	arr := StringArray{"one", "two", "three"}

	val, err := arr.Value()
	require.NoError(t, err)
	assert.Equal(t, `["one","two","three"]`, val)
}

func TestStringArray_Value_SpecialChars(t *testing.T) {
	tests := []struct {
		name     string
		input    StringArray
		expected string
	}{
		{
			name:     "element with comma",
			input:    StringArray{"hello,world"},
			expected: `["hello,world"]`,
		},
		{
			name:     "element with quotes",
			input:    StringArray{`say "hello"`},
			expected: `["say \"hello\""]`,
		},
		{
			name:     "element with braces",
			input:    StringArray{"{value}"},
			expected: `["{value}"]`,
		},
		{
			name:     "element with space",
			input:    StringArray{"hello world"},
			expected: `["hello world"]`,
		},
		{
			name:     "element with backslash",
			input:    StringArray{`path\to\file`},
			expected: `["path\\to\\file"]`,
		},
		{
			name:     "mixed elements",
			input:    StringArray{"normal", "with,comma", "with space"},
			expected: `["normal","with,comma","with space"]`,
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

func TestStringArray_Scan_Nil(t *testing.T) {
	arr := StringArray{"existing"} // Set existing value

	err := arr.Scan(nil)
	require.NoError(t, err)
	assert.Empty(t, arr)
}

func TestStringArray_Scan_EmptyString(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"empty string", ""},
		{"empty braces", "{}"},
		{"empty JSON array", "[]"},
		{"empty bytes", []byte{}},
		{"empty braces bytes", []byte("{}")},
		{"empty JSON array bytes", []byte("[]")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr StringArray
			err := arr.Scan(tt.input)
			require.NoError(t, err)
			assert.Empty(t, arr)
		})
	}
}

func TestStringArray_Scan_PostgresFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected StringArray
	}{
		{
			name:     "single element",
			input:    "{hello}",
			expected: StringArray{"hello"},
		},
		{
			name:     "multiple elements",
			input:    "{one,two,three}",
			expected: StringArray{"one", "two", "three"},
		},
		{
			name:     "empty braces",
			input:    "{}",
			expected: StringArray{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr StringArray
			err := arr.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, arr)
		})
	}
}

func TestStringArray_Scan_PostgresQuoted(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected StringArray
	}{
		{
			name:     "quoted element with comma",
			input:    `{"hello,world"}`,
			expected: StringArray{"hello,world"},
		},
		{
			name:     "mixed quoted and unquoted",
			input:    `{normal,"with,comma",another}`,
			expected: StringArray{"normal", "with,comma", "another"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr StringArray
			err := arr.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, arr)
		})
	}
}

func TestStringArray_Scan_JSONFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected StringArray
	}{
		{
			name:     "JSON array single",
			input:    `["hello"]`,
			expected: StringArray{"hello"},
		},
		{
			name:     "JSON array multiple",
			input:    `["one","two","three"]`,
			expected: StringArray{"one", "two", "three"},
		},
		{
			name:     "JSON array empty",
			input:    `[]`,
			expected: StringArray{},
		},
		{
			name:     "JSON array with special chars",
			input:    `["hello,world","say \"hi\""]`,
			expected: StringArray{"hello,world", `say "hi"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr StringArray
			err := arr.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, arr)
		})
	}
}

func TestStringArray_Scan_ByteSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected StringArray
	}{
		{
			name:     "bytes postgres format",
			input:    []byte("{one,two}"),
			expected: StringArray{"one", "two"},
		},
		{
			name:     "bytes JSON format",
			input:    []byte(`["one","two"]`),
			expected: StringArray{"one", "two"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr StringArray
			err := arr.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, arr)
		})
	}
}

func TestStringArray_Scan_UnsupportedType(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"integer", 42},
		{"float", 3.14},
		{"slice of int", []int{1, 2, 3}},
		{"map", map[string]string{"a": "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr StringArray
			err := arr.Scan(tt.input)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "cannot scan type")
		})
	}
}

func TestStringArray_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input StringArray
	}{
		{"empty array", StringArray{}},
		{"single element", StringArray{"hello"}},
		{"multiple elements", StringArray{"one", "two", "three"}},
		{"with special chars", StringArray{"normal", "with,comma", "with space"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get the driver value (JSON format)
			val, err := tt.input.Value()
			require.NoError(t, err)

			// Scan it back
			var result StringArray
			err = result.Scan(val)
			require.NoError(t, err)

			// Should match original
			assert.Equal(t, tt.input, result)
		})
	}
}
