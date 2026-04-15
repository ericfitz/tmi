package dberrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSentinelErrorHierarchy(t *testing.T) {
	t.Run("ErrDuplicate is ErrConstraint", func(t *testing.T) {
		assert.True(t, errors.Is(ErrDuplicate, ErrConstraint))
	})

	t.Run("ErrForeignKey is ErrConstraint", func(t *testing.T) {
		assert.True(t, errors.Is(ErrForeignKey, ErrConstraint))
	})

	t.Run("ErrDuplicate is not ErrForeignKey", func(t *testing.T) {
		assert.False(t, errors.Is(ErrDuplicate, ErrForeignKey))
	})

	t.Run("ErrTransient is not ErrConstraint", func(t *testing.T) {
		assert.False(t, errors.Is(ErrTransient, ErrConstraint))
	})
}

func TestWrap(t *testing.T) {
	raw := fmt.Errorf("pg: unique violation on idx_email")

	t.Run("wrapped error matches sentinel", func(t *testing.T) {
		wrapped := Wrap(raw, ErrDuplicate)
		assert.True(t, errors.Is(wrapped, ErrDuplicate))
		assert.True(t, errors.Is(wrapped, ErrConstraint))
	})

	t.Run("wrapped error matches original", func(t *testing.T) {
		wrapped := Wrap(raw, ErrDuplicate)
		assert.True(t, errors.Is(wrapped, raw))
	})

	t.Run("nil error returns nil", func(t *testing.T) {
		assert.Nil(t, Wrap(nil, ErrDuplicate))
	})
}

func TestIsRetryable(t *testing.T) {
	t.Run("transient error is retryable", func(t *testing.T) {
		err := Wrap(fmt.Errorf("connection reset"), ErrTransient)
		assert.True(t, IsRetryable(err))
	})

	t.Run("constraint error is not retryable", func(t *testing.T) {
		err := Wrap(fmt.Errorf("duplicate key"), ErrDuplicate)
		assert.False(t, IsRetryable(err))
	})

	t.Run("bare sentinel is retryable", func(t *testing.T) {
		assert.True(t, IsRetryable(ErrTransient))
	})
}

func TestIsFatal(t *testing.T) {
	t.Run("permission error is fatal", func(t *testing.T) {
		err := Wrap(fmt.Errorf("insufficient privilege"), ErrPermission)
		assert.True(t, IsFatal(err))
	})

	t.Run("transient error is not fatal", func(t *testing.T) {
		err := Wrap(fmt.Errorf("connection reset"), ErrTransient)
		assert.False(t, IsFatal(err))
	})
}
