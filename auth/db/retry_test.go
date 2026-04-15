package db

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPermissionError(t *testing.T) {
	t.Run("nil error returns false", func(t *testing.T) {
		assert.False(t, IsPermissionError(nil))
	})

	t.Run("permission denied", func(t *testing.T) {
		assert.True(t, IsPermissionError(errors.New("ERROR: permission denied for table client_credentials")))
	})

	t.Run("insufficient privilege", func(t *testing.T) {
		assert.True(t, IsPermissionError(errors.New("ERROR: insufficient privilege")))
	})

	t.Run("unrelated error returns false", func(t *testing.T) {
		assert.False(t, IsPermissionError(errors.New("connection refused")))
	})

	t.Run("case insensitive", func(t *testing.T) {
		assert.True(t, IsPermissionError(errors.New("PERMISSION DENIED for relation users")))
	})
}
