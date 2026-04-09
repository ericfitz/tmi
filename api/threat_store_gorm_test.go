package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestGormThreatStore(t *testing.T) *GormThreatStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return &GormThreatStore{db: db}
}

func TestBuildOrderBy(t *testing.T) {
	store := newTestGormThreatStore(t)

	t.Run("default fallback for invalid format", func(t *testing.T) {
		assert.Equal(t, DefaultSortOrderCreatedAtDesc, store.buildOrderBy("invalid"))
	})

	t.Run("default fallback for unknown column", func(t *testing.T) {
		assert.Equal(t, DefaultSortOrderCreatedAtDesc, store.buildOrderBy("nonexistent:asc"))
	})

	t.Run("default fallback for invalid direction", func(t *testing.T) {
		result := store.buildOrderBy("name:sideways")
		assert.Contains(t, result, "DESC")
	})

	t.Run("plain column sorts unchanged", func(t *testing.T) {
		assert.Equal(t, "name ASC", store.buildOrderBy("name:asc"))
		assert.Equal(t, "created_at DESC", store.buildOrderBy("created_at:desc"))
		assert.Equal(t, "score ASC", store.buildOrderBy("score:asc"))
	})

	t.Run("severity uses CASE expression", func(t *testing.T) {
		result := store.buildOrderBy("severity:asc")
		assert.Contains(t, result, "CASE")
		assert.Contains(t, result, "critical")
		assert.Contains(t, result, "ASC")
		assert.NotEqual(t, "severity ASC", result)
	})

	t.Run("priority uses CASE expression", func(t *testing.T) {
		result := store.buildOrderBy("priority:desc")
		assert.Contains(t, result, "CASE")
		assert.Contains(t, result, "immediate")
		assert.Contains(t, result, "DESC")
	})

	t.Run("status uses CASE expression", func(t *testing.T) {
		result := store.buildOrderBy("status:asc")
		assert.Contains(t, result, "CASE")
		assert.Contains(t, result, "identified")
		assert.Contains(t, result, "ASC")
	})
}

func TestBuildSemanticOrderExpr(t *testing.T) {
	t.Run("severity ordering ranks", func(t *testing.T) {
		expr := buildSemanticOrderExpr("severity", severityOrder, "sqlite")
		// All severity values should appear in the expression
		for _, val := range []string{"unknown", "informational", "low", "medium", "high", "critical"} {
			assert.Contains(t, expr, "'"+val+"'", "should contain severity value: %s", val)
		}
		assert.Contains(t, expr, "ELSE -1", "unknown values should sort to -1")
	})

	t.Run("priority ordering ranks", func(t *testing.T) {
		expr := buildSemanticOrderExpr("priority", priorityOrder, "sqlite")
		for _, val := range []string{"deferred", "low", "medium", "high", "immediate"} {
			assert.Contains(t, expr, "'"+val+"'", "should contain priority value: %s", val)
		}
	})

	t.Run("uses LOWER for case-insensitive matching", func(t *testing.T) {
		expr := buildSemanticOrderExpr("severity", severityOrder, "sqlite")
		assert.Contains(t, expr, "LOWER(severity)")
	})

	t.Run("oracle uses uppercase column names", func(t *testing.T) {
		expr := buildSemanticOrderExpr("severity", severityOrder, "oracle")
		assert.Contains(t, expr, "LOWER(SEVERITY)")
	})
}

func TestSemanticOrderMaps(t *testing.T) {
	t.Run("severity order is correct", func(t *testing.T) {
		expected := []string{"unknown", "informational", "low", "medium", "high", "critical"}
		for i, val := range expected {
			assert.Equal(t, i, severityOrder[val], "severity %q should have rank %d", val, i)
		}
	})

	t.Run("priority order is correct", func(t *testing.T) {
		expected := []string{"deferred", "low", "medium", "high", "immediate"}
		for i, val := range expected {
			assert.Equal(t, i, priorityOrder[val], "priority %q should have rank %d", val, i)
		}
	})

	t.Run("status order is correct", func(t *testing.T) {
		expected := []string{"identified", "investigating", "in_progress", "mitigated", "resolved", "accepted", "false_positive"}
		for i, val := range expected {
			assert.Equal(t, i, statusOrder[val], "status %q should have rank %d", val, i)
		}
	})
}

func TestSemanticSortOrderIntegration(t *testing.T) {
	// Verify that semantic sort produces the correct relative ordering
	// by checking the CASE WHEN values assigned to each enum value
	t.Run("severity ascending: unknown < informational < low < medium < high < critical", func(t *testing.T) {
		ordered := []string{"unknown", "informational", "low", "medium", "high", "critical"}
		for i := 0; i < len(ordered)-1; i++ {
			assert.Less(t, severityOrder[ordered[i]], severityOrder[ordered[i+1]],
				"%s should sort before %s", ordered[i], ordered[i+1])
		}
	})

	t.Run("priority ascending: deferred < low < medium < high < immediate", func(t *testing.T) {
		ordered := []string{"deferred", "low", "medium", "high", "immediate"}
		for i := 0; i < len(ordered)-1; i++ {
			assert.Less(t, priorityOrder[ordered[i]], priorityOrder[ordered[i+1]],
				"%s should sort before %s", ordered[i], ordered[i+1])
		}
	})

	t.Run("unknown values sort before all known values", func(t *testing.T) {
		expr := buildSemanticOrderExpr("severity", severityOrder, "sqlite")
		// The ELSE -1 means unknown values get rank -1, which is less than 0 (unknown severity)
		assert.True(t, strings.Contains(expr, "ELSE -1"))
	})
}
