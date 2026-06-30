package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColumnMap(t *testing.T) {
	t.Run("oracle uppercases all keys", func(t *testing.T) {
		out := ColumnMap(DialectOracle, map[string]any{"team_id": "t1", "user_internal_uuid": "u1"})
		assert.Equal(t, "t1", out["TEAM_ID"])
		assert.Equal(t, "u1", out["USER_INTERNAL_UUID"])
		_, hasLower := out["team_id"]
		assert.False(t, hasLower, "lowercase key must not survive on Oracle")
	})

	t.Run("postgres returns predicate unchanged", func(t *testing.T) {
		in := map[string]any{"team_id": "t1"}
		out := ColumnMap(DialectPostgres, in)
		assert.Equal(t, "t1", out["team_id"])
		_, hasUpper := out["TEAM_ID"]
		assert.False(t, hasUpper)
	})

	t.Run("sqlite returns predicate unchanged", func(t *testing.T) {
		out := ColumnMap(DialectSQLite, map[string]any{"id": "x"})
		assert.Equal(t, "x", out["id"])
	})

	t.Run("empty map is safe", func(t *testing.T) {
		assert.Empty(t, ColumnMap(DialectOracle, map[string]any{}))
	})

	t.Run("preserves non-string values", func(t *testing.T) {
		out := ColumnMap(DialectOracle, map[string]any{"sharable": true, "count": 3})
		assert.Equal(t, true, out["SHARABLE"])
		assert.Equal(t, 3, out["COUNT"])
	})
}
