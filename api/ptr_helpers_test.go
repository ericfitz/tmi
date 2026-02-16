package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStrPtr(t *testing.T) {
	t.Run("non-empty string returns pointer", func(t *testing.T) {
		result := strPtr("hello")
		assert.NotNil(t, result)
		assert.Equal(t, "hello", *result)
	})

	t.Run("empty string returns nil", func(t *testing.T) {
		result := strPtr("")
		assert.Nil(t, result)
	})
}

func TestStrPtrOrEmpty(t *testing.T) {
	t.Run("non-empty string returns pointer", func(t *testing.T) {
		result := strPtrOrEmpty("hello")
		assert.NotNil(t, result)
		assert.Equal(t, "hello", *result)
	})

	t.Run("empty string returns pointer to empty", func(t *testing.T) {
		result := strPtrOrEmpty("")
		assert.NotNil(t, result)
		assert.Equal(t, "", *result)
	})
}

func TestStrFromPtr(t *testing.T) {
	t.Run("non-nil pointer returns value", func(t *testing.T) {
		s := "hello"
		result := strFromPtr(&s)
		assert.Equal(t, "hello", result)
	})

	t.Run("nil pointer returns empty string", func(t *testing.T) {
		result := strFromPtr(nil)
		assert.Equal(t, "", result)
	})

	t.Run("pointer to empty string returns empty", func(t *testing.T) {
		s := ""
		result := strFromPtr(&s)
		assert.Equal(t, "", result)
	})
}

func TestTimePtr(t *testing.T) {
	t.Run("non-nil returns same pointer", func(t *testing.T) {
		now := time.Now()
		result := timePtr(&now)
		assert.Same(t, &now, result)
	})

	t.Run("nil returns nil", func(t *testing.T) {
		result := timePtr(nil)
		assert.Nil(t, result)
	})
}

func TestTimeFromPtr(t *testing.T) {
	t.Run("non-nil returns same pointer", func(t *testing.T) {
		now := time.Now()
		result := timeFromPtr(&now)
		assert.Same(t, &now, result)
	})

	t.Run("nil returns nil", func(t *testing.T) {
		result := timeFromPtr(nil)
		assert.Nil(t, result)
	})
}
