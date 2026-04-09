package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitCommaValues(t *testing.T) {
	t.Run("single value", func(t *testing.T) {
		result := splitCommaValues("submitted")
		assert.Equal(t, []string{"submitted"}, result)
	})

	t.Run("multiple values", func(t *testing.T) {
		result := splitCommaValues("submitted,ready_for_review")
		assert.Equal(t, []string{"submitted", "ready_for_review"}, result)
	})

	t.Run("multiple values with spaces", func(t *testing.T) {
		result := splitCommaValues("submitted, ready_for_review, draft")
		assert.Equal(t, []string{"submitted", "ready_for_review", "draft"}, result)
	})

	t.Run("empty string", func(t *testing.T) {
		result := splitCommaValues("")
		assert.Empty(t, result)
	})

	t.Run("trailing comma", func(t *testing.T) {
		result := splitCommaValues("submitted,")
		assert.Equal(t, []string{"submitted"}, result)
	})

	t.Run("leading comma", func(t *testing.T) {
		result := splitCommaValues(",submitted")
		assert.Equal(t, []string{"submitted"}, result)
	})

	t.Run("only commas", func(t *testing.T) {
		result := splitCommaValues(",,,")
		assert.Empty(t, result)
	})
}
