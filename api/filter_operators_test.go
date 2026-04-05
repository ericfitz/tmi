package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFilterValue(t *testing.T) {
	t.Run("plain value returns FilterOpNone", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "alice@example.com")
		require.NoError(t, err)
		assert.Equal(t, FilterOpNone, result.Operator)
		assert.Equal(t, "alice@example.com", result.Value)
	})

	t.Run("empty value returns FilterOpNone with empty value", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "")
		require.NoError(t, err)
		assert.Equal(t, FilterOpNone, result.Operator)
		assert.Equal(t, "", result.Value)
	})

	t.Run("is:null returns FilterOpIsNull", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "is:null")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNull, result.Operator)
		assert.Equal(t, "", result.Value)
	})

	t.Run("is:notnull returns FilterOpIsNotNull", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "is:notnull")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNotNull, result.Operator)
		assert.Equal(t, "", result.Value)
	})

	t.Run("is:NULL is case insensitive", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "is:NULL")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNull, result.Operator)
	})

	t.Run("IS:NOTNULL is case insensitive", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "IS:NOTNULL")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNotNull, result.Operator)
	})

	t.Run("Is:Null mixed case is case insensitive", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "Is:Null")
		require.NoError(t, err)
		assert.Equal(t, FilterOpIsNull, result.Operator)
	})

	t.Run("unsupported is: operand returns error", func(t *testing.T) {
		_, err := ParseFilterValue("security_reviewer", "is:banana")
		require.Error(t, err)
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr)
		assert.Equal(t, 400, reqErr.Status)
		assert.Contains(t, reqErr.Message, "is:banana")
		assert.Contains(t, reqErr.Message, "security_reviewer")
	})

	t.Run("unsupported operator prefix returns error", func(t *testing.T) {
		_, err := ParseFilterValue("status", "gt:5")
		require.Error(t, err)
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr)
		assert.Equal(t, 400, reqErr.Status)
		assert.Contains(t, reqErr.Message, "gt")
	})

	t.Run("incomplete operator is: returns error", func(t *testing.T) {
		_, err := ParseFilterValue("security_reviewer", "is:")
		require.Error(t, err)
		var reqErr *RequestError
		require.ErrorAs(t, err, &reqErr)
		assert.Equal(t, 400, reqErr.Status)
	})

	t.Run("value containing colon but not operator prefix is plain value", func(t *testing.T) {
		result, err := ParseFilterValue("security_reviewer", "user:name@example.com")
		require.NoError(t, err)
		assert.Equal(t, FilterOpNone, result.Operator)
		assert.Equal(t, "user:name@example.com", result.Value)
	})
}
