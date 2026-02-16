package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTextField(t *testing.T) {
	t.Run("required field empty returns error", func(t *testing.T) {
		err := validateTextField("", "name", 255, true)
		require.Error(t, err)
		reqErr, ok := err.(*RequestError)
		require.True(t, ok)
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
