package models

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStringer implements fmt.Stringer to simulate Oracle's godror.Number type
type mockStringer struct {
	value string
}

func (m mockStringer) String() string {
	return m.value
}

func TestOracleBool_Scan_Bool(t *testing.T) {
	tests := []struct {
		name     string
		input    bool
		expected bool
	}{
		{"true value", true, true},
		{"false value", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b OracleBool
			err := b.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, b.Bool())
		})
	}
}

func TestOracleBool_Scan_Int64(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected bool
	}{
		{"zero is false", 0, false},
		{"one is true", 1, true},
		{"positive is true", 42, true},
		{"negative is true", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b OracleBool
			err := b.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, b.Bool())
		})
	}
}

func TestOracleBool_Scan_Int(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected bool
	}{
		{"zero is false", 0, false},
		{"one is true", 1, true},
		{"positive is true", 100, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b OracleBool
			err := b.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, b.Bool())
		})
	}
}

func TestOracleBool_Scan_Int32(t *testing.T) {
	tests := []struct {
		name     string
		input    int32
		expected bool
	}{
		{"zero is false", 0, false},
		{"one is true", 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b OracleBool
			err := b.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, b.Bool())
		})
	}
}

func TestOracleBool_Scan_Float64(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected bool
	}{
		{"zero is false", 0.0, false},
		{"one is true", 1.0, true},
		{"fractional is true", 0.5, true},
		{"negative is true", -1.5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b OracleBool
			err := b.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, b.Bool())
		})
	}
}

func TestOracleBool_Scan_Stringer(t *testing.T) {
	tests := []struct {
		name     string
		input    fmt.Stringer
		expected bool
	}{
		{"string zero is false", mockStringer{"0"}, false},
		{"string one is true", mockStringer{"1"}, true},
		{"empty string is false", mockStringer{""}, false},
		{"non-zero string is true", mockStringer{"42"}, true},
		{"negative string is true", mockStringer{"-1"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b OracleBool
			err := b.Scan(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, b.Bool())
		})
	}
}

func TestOracleBool_Scan_Nil(t *testing.T) {
	b := OracleBool(true) // Set to true first to verify it changes

	err := b.Scan(nil)
	require.NoError(t, err)
	assert.False(t, b.Bool(), "nil should scan as false")
}

func TestOracleBool_Scan_UnsupportedType(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"string (non-Stringer)", "not a stringer"},
		{"slice", []int{1, 2, 3}},
		{"map", map[string]int{"a": 1}},
		{"struct without String method", struct{ value int }{value: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b OracleBool
			err := b.Scan(tt.input)
			// Note: string implements fmt.Stringer, so it won't error
			// We test other types that don't implement Stringer
			if _, ok := tt.input.(fmt.Stringer); !ok {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "cannot scan type")
			}
		})
	}
}

func TestOracleBool_Value(t *testing.T) {
	tests := []struct {
		name     string
		input    OracleBool
		expected bool
	}{
		{"true returns true", OracleBool(true), true},
		{"false returns false", OracleBool(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, val)
		})
	}
}

func TestOracleBool_Bool(t *testing.T) {
	tests := []struct {
		name     string
		input    OracleBool
		expected bool
	}{
		{"true OracleBool", OracleBool(true), true},
		{"false OracleBool", OracleBool(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.input.Bool())
		})
	}
}

func TestOracleBool_RoundTrip(t *testing.T) {
	// Test that Value -> Scan produces consistent results
	tests := []struct {
		name  string
		input OracleBool
	}{
		{"round trip true", OracleBool(true)},
		{"round trip false", OracleBool(false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get the driver value
			val, err := tt.input.Value()
			require.NoError(t, err)

			// Scan it back
			var result OracleBool
			err = result.Scan(val)
			require.NoError(t, err)

			// Should match original
			assert.Equal(t, tt.input.Bool(), result.Bool())
		})
	}
}
