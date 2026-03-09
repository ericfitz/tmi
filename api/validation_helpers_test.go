package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeColorPalette(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result, err := NormalizeColorPalette(nil)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("empty slice returns nil", func(t *testing.T) {
		empty := []ColorPaletteEntry{}
		result, err := NormalizeColorPalette(&empty)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("valid 6-digit hex colors", func(t *testing.T) {
		entries := []ColorPaletteEntry{
			{Position: 1, Color: "#D32F2F"},
			{Position: 2, Color: "#1565C0"},
		}
		result, err := NormalizeColorPalette(&entries)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Len(t, *result, 2)
		assert.Equal(t, "#d32f2f", (*result)[0].Color)
		assert.Equal(t, "#1565c0", (*result)[1].Color)
	})

	t.Run("3-digit hex expanded to 6-digit", func(t *testing.T) {
		entries := []ColorPaletteEntry{
			{Position: 1, Color: "#FFF"},
			{Position: 2, Color: "#f0c"},
		}
		result, err := NormalizeColorPalette(&entries)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "#ffffff", (*result)[0].Color)
		assert.Equal(t, "#ff00cc", (*result)[1].Color)
	})

	t.Run("sorted by position", func(t *testing.T) {
		entries := []ColorPaletteEntry{
			{Position: 3, Color: "#333333"},
			{Position: 1, Color: "#111111"},
			{Position: 2, Color: "#222222"},
		}
		result, err := NormalizeColorPalette(&entries)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, 1, (*result)[0].Position)
		assert.Equal(t, 2, (*result)[1].Position)
		assert.Equal(t, 3, (*result)[2].Position)
	})

	t.Run("rejects more than 8 entries", func(t *testing.T) {
		entries := make([]ColorPaletteEntry, 9)
		for i := range entries {
			entries[i] = ColorPaletteEntry{Position: i + 1, Color: "#aabbcc"}
		}
		// Position 9 is out of range, but we hit the >8 check first
		_, err := NormalizeColorPalette(&entries)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 8")
	})

	t.Run("rejects duplicate positions", func(t *testing.T) {
		entries := []ColorPaletteEntry{
			{Position: 1, Color: "#aaaaaa"},
			{Position: 1, Color: "#bbbbbb"},
		}
		_, err := NormalizeColorPalette(&entries)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate")
	})

	t.Run("rejects position out of range", func(t *testing.T) {
		entries := []ColorPaletteEntry{
			{Position: 0, Color: "#aaaaaa"},
		}
		_, err := NormalizeColorPalette(&entries)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "between 1 and 8")

		entries = []ColorPaletteEntry{
			{Position: 9, Color: "#aaaaaa"},
		}
		_, err = NormalizeColorPalette(&entries)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "between 1 and 8")
	})

	t.Run("rejects invalid color format", func(t *testing.T) {
		cases := []struct {
			color string
			desc  string
		}{
			{"red", "named color"},
			{"#GGHHII", "invalid hex chars"},
			{"#12345", "5 hex digits"},
			{"#1234", "4 hex digits"},
			{"123456", "missing hash"},
			{"#12345678", "too long"},
		}
		for _, tc := range cases {
			t.Run(tc.desc, func(t *testing.T) {
				entries := []ColorPaletteEntry{
					{Position: 1, Color: tc.color},
				}
				_, err := NormalizeColorPalette(&entries)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid color format")
			})
		}
	})

	t.Run("accepts exactly 8 entries", func(t *testing.T) {
		entries := make([]ColorPaletteEntry, 8)
		for i := range entries {
			entries[i] = ColorPaletteEntry{Position: i + 1, Color: "#aabbcc"}
		}
		result, err := NormalizeColorPalette(&entries)
		require.NoError(t, err)
		assert.Len(t, *result, 8)
	})

	t.Run("single entry works", func(t *testing.T) {
		entries := []ColorPaletteEntry{
			{Position: 5, Color: "#ABC"},
		}
		result, err := NormalizeColorPalette(&entries)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Len(t, *result, 1)
		assert.Equal(t, 5, (*result)[0].Position)
		assert.Equal(t, "#aabbcc", (*result)[0].Color)
	})
}

func TestExpandShorthandHex(t *testing.T) {
	assert.Equal(t, "#ffffff", expandShorthandHex("#fff"))
	assert.Equal(t, "#ff00cc", expandShorthandHex("#f0c"))
	assert.Equal(t, "#aabbcc", expandShorthandHex("#aabbcc"))
}

func TestValidateTextField(t *testing.T) {
	t.Run("required field empty returns error", func(t *testing.T) {
		err := validateTextField("", "name", 255, true)
		require.Error(t, err)
		var reqErr *RequestError
		require.True(t, errors.As(err, &reqErr))
		assert.Equal(t, 400, reqErr.Status)
		assert.Contains(t, reqErr.Message, "required")
	})

	t.Run("optional field empty returns nil", func(t *testing.T) {
		err := validateTextField("", "description", 500, false)
		assert.NoError(t, err)
	})

	t.Run("valid input passes", func(t *testing.T) {
		err := validateTextField("hello world", "name", 255, true)
		assert.NoError(t, err)
	})

	t.Run("exceeds max length", func(t *testing.T) {
		long := strings.Repeat("a", 256)
		err := validateTextField(long, "name", 255, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum length")
	})

	t.Run("at max length passes", func(t *testing.T) {
		exact := strings.Repeat("a", 255)
		err := validateTextField(exact, "name", 255, true)
		assert.NoError(t, err)
	})

	t.Run("control characters rejected", func(t *testing.T) {
		err := validateTextField("hello\x00world", "name", 255, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "control characters")
	})

	t.Run("zero-width characters rejected", func(t *testing.T) {
		err := validateTextField("hello\u200Bworld", "name", 255, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "zero-width")
	})

	t.Run("problematic Unicode rejected", func(t *testing.T) {
		err := validateTextField("hello\uE000world", "name", 255, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "problematic Unicode")
	})

	t.Run("HTML injection rejected", func(t *testing.T) {
		err := validateTextField("<script>alert(1)</script>", "name", 255, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsafe content")
	})

	t.Run("normal Unicode allowed", func(t *testing.T) {
		err := validateTextField("café résumé", "name", 255, true)
		assert.NoError(t, err)
	})
}
