package api

import (
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func newTestGormThreatStore(t *testing.T) *GormThreatRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return &GormThreatRepository{db: db}
}

// fakeOracleDialector is a minimal gorm.Dialector stub that reports itself as
// "oracle" without opening any real database connection, so buildOrderBy's
// dialect-aware column casing (ColumnName uppercases columns on Oracle) can
// be exercised from a unit test. buildOrderBy/defaultOrder only ever read
// s.db.Name() -- they never execute a query -- so every method below besides
// Name and Initialize is unreachable and panics if that assumption breaks.
type fakeOracleDialector struct{}

func (fakeOracleDialector) Name() string              { return "oracle" }
func (fakeOracleDialector) Initialize(*gorm.DB) error { return nil }
func (fakeOracleDialector) Migrator(*gorm.DB) gorm.Migrator {
	panic("fakeOracleDialector: Migrator unexpectedly called")
}
func (fakeOracleDialector) DataTypeOf(*schema.Field) string {
	panic("fakeOracleDialector: DataTypeOf unexpectedly called")
}
func (fakeOracleDialector) DefaultValueOf(*schema.Field) clause.Expression {
	panic("fakeOracleDialector: DefaultValueOf unexpectedly called")
}
func (fakeOracleDialector) BindVarTo(clause.Writer, *gorm.Statement, interface{}) {
	panic("fakeOracleDialector: BindVarTo unexpectedly called")
}
func (fakeOracleDialector) QuoteTo(clause.Writer, string) {
	panic("fakeOracleDialector: QuoteTo unexpectedly called")
}
func (fakeOracleDialector) Explain(sql string, vars ...interface{}) string {
	panic("fakeOracleDialector: Explain unexpectedly called")
}

// newTestGormThreatStoreOracle builds a store whose dialect name is "oracle"
// without any real Oracle connection, to unit-test Oracle-specific casing.
func newTestGormThreatStoreOracle(t *testing.T) *GormThreatRepository {
	t.Helper()
	db, err := gorm.Open(fakeOracleDialector{}, &gorm.Config{})
	require.NoError(t, err)
	return &GormThreatRepository{db: db}
}

func TestBuildOrderBy(t *testing.T) {
	store := newTestGormThreatStore(t)

	t.Run("default fallback for invalid format", func(t *testing.T) {
		assert.Equal(t, "created_at DESC, id ASC", store.buildOrderBy("invalid"))
	})

	t.Run("default fallback for unknown column", func(t *testing.T) {
		assert.Equal(t, "created_at DESC, id ASC", store.buildOrderBy("nonexistent:asc"))
	})

	t.Run("default fallback for invalid direction", func(t *testing.T) {
		result := store.buildOrderBy("name:sideways")
		assert.Contains(t, result, "DESC")
	})

	t.Run("plain column sorts carry tiebreaker", func(t *testing.T) {
		assert.Equal(t, "name ASC, id ASC", store.buildOrderBy("name:asc"))
		assert.Equal(t, "created_at DESC, id ASC", store.buildOrderBy("created_at:desc"))
		assert.Equal(t, "score ASC, id ASC", store.buildOrderBy("score:asc"))
	})

	t.Run("malformed sort falls back to default with tiebreaker", func(t *testing.T) {
		assert.Equal(t, "created_at DESC, id ASC", store.buildOrderBy("bogus"))
	})

	t.Run("unknown column falls back to default with tiebreaker", func(t *testing.T) {
		assert.Equal(t, "created_at DESC, id ASC", store.buildOrderBy("nope:asc"))
	})

	t.Run("severity uses CASE expression with tiebreaker", func(t *testing.T) {
		result := store.buildOrderBy("severity:asc")
		assert.Contains(t, result, "CASE")
		assert.Contains(t, result, "critical")
		assert.Contains(t, result, "ASC")
		assert.NotEqual(t, "severity ASC", result)
		assert.True(t, strings.HasSuffix(result, " ASC, id ASC"))
	})

	t.Run("priority uses CASE expression with tiebreaker", func(t *testing.T) {
		result := store.buildOrderBy("priority:desc")
		assert.Contains(t, result, "CASE")
		assert.Contains(t, result, "immediate")
		assert.Contains(t, result, "DESC")
		assert.True(t, strings.HasSuffix(result, " DESC, id ASC"))
	})

	t.Run("status uses CASE expression with tiebreaker", func(t *testing.T) {
		result := store.buildOrderBy("status:asc")
		assert.Contains(t, result, "CASE")
		assert.Contains(t, result, "open")
		assert.Contains(t, result, "ASC")
		assert.True(t, strings.HasSuffix(result, " ASC, id ASC"))
	})

	t.Run("threat_type is not sortable (StringArray maps to CLOB on Oracle)", func(t *testing.T) {
		// threat_type must fall back to defaultOrder(), the same as any other
		// unrecognized column -- it must never reach ColumnName/ORDER BY,
		// since Oracle rejects ORDER BY on a LOB column (ORA-00932).
		assert.Equal(t, "created_at DESC, id ASC", store.buildOrderBy("threat_type:asc"))
	})

	t.Run("oracle dialect renders uppercase columns in default order", func(t *testing.T) {
		oracleStore := newTestGormThreatStoreOracle(t)
		assert.Equal(t, "CREATED_AT DESC, ID ASC", oracleStore.defaultOrder())
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
		expected := map[string]int{
			"open":                   0,
			"confirmed":              1,
			"deferred":               2,
			"mitigation_planned":     3,
			"mitigation_in_progress": 4,
			"verification_pending":   5,
			"mitigated":              6,
			"resolved":               7,
			"accepted":               8,
			"closed":                 9,
			"false_positive":         10,
		}
		for val, rank := range expected {
			assert.Equal(t, rank, statusOrder[val], "status %q should have rank %d", val, rank)
		}
	})

	t.Run("legacy status values rank alongside their current equivalents", func(t *testing.T) {
		assert.Equal(t, statusOrder["open"], statusOrder["identified"])
		assert.Equal(t, statusOrder["confirmed"], statusOrder["investigating"])
		assert.Equal(t, statusOrder["mitigation_in_progress"], statusOrder["in_progress"])
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

func TestSSVCConversion(t *testing.T) {
	store := newTestGormThreatStore(t)

	t.Run("toGormModelForCreate with SSVC", func(t *testing.T) {
		decision := SSVCScoreDecision("Immediate")
		tmID := uuid.New()
		threat := &Threat{
			Name:          "Test Threat",
			ThreatType:    []string{"spoofing"},
			ThreatModelId: &tmID,
			Ssvc: &SSVCScore{
				Vector:      "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/",
				Decision:    decision,
				Methodology: "Supplier",
			},
		}

		gm := store.toGormModelForCreate(threat)
		assert.True(t, gm.Ssvc.Valid)
		assert.Equal(t, "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/", gm.Ssvc.Vector)
		assert.Equal(t, "Immediate", gm.Ssvc.Decision)
		assert.Equal(t, "Supplier", gm.Ssvc.Methodology)
	})

	t.Run("toGormModelForCreate without SSVC", func(t *testing.T) {
		tmID := uuid.New()
		threat := &Threat{
			Name:          "Test Threat",
			ThreatType:    []string{"spoofing"},
			ThreatModelId: &tmID,
		}

		gm := store.toGormModelForCreate(threat)
		assert.False(t, gm.Ssvc.Valid)
	})

	t.Run("toAPIModel with SSVC", func(t *testing.T) {
		gm := &models.Threat{
			ID:            models.DBVarchar(uuid.New().String()),
			ThreatModelID: models.DBVarchar(uuid.New().String()),
			Name:          "Test Threat",
			ThreatType:    models.StringArray{"spoofing"},
			Ssvc: models.NullableSSVC{
				SSVCScore: models.SSVCScore{
					Vector:      "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/",
					Decision:    "Immediate",
					Methodology: "Supplier",
				},
				Valid: true,
			},
		}

		threat := store.toAPIModel(gm)
		require.NotNil(t, threat.Ssvc)
		assert.Equal(t, "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/", threat.Ssvc.Vector)
		assert.Equal(t, SSVCScoreDecision("Immediate"), threat.Ssvc.Decision)
		assert.Equal(t, "Supplier", threat.Ssvc.Methodology)
	})

	t.Run("toAPIModel without SSVC", func(t *testing.T) {
		gm := &models.Threat{
			ID:            models.DBVarchar(uuid.New().String()),
			ThreatModelID: models.DBVarchar(uuid.New().String()),
			Name:          "Test Threat",
			ThreatType:    models.StringArray{"spoofing"},
			Ssvc:          models.NullableSSVC{Valid: false},
		}

		threat := store.toAPIModel(gm)
		assert.Nil(t, threat.Ssvc)
	})

	t.Run("buildThreatUpdateMap with SSVC", func(t *testing.T) {
		decision := SSVCScoreDecision("Scheduled")
		tmID := uuid.New()
		threat := &Threat{
			Name:          "Test Threat",
			ThreatType:    []string{"spoofing"},
			ThreatModelId: &tmID,
			Ssvc: &SSVCScore{
				Vector:      "SSVCv2/E:A/U:S/T:T/P:S/2026-04-08/",
				Decision:    decision,
				Methodology: "Supplier",
			},
		}

		updateMap := store.buildThreatUpdateMap(threat, time.Now())
		ssvcVal, ok := updateMap["ssvc"]
		assert.True(t, ok)
		assert.NotNil(t, ssvcVal)
		ssvcStr, ok := ssvcVal.(string)
		assert.True(t, ok)
		assert.Contains(t, ssvcStr, "Scheduled")
	})

	t.Run("buildThreatUpdateMap without SSVC writes nil", func(t *testing.T) {
		tmID := uuid.New()
		threat := &Threat{
			Name:          "Test Threat",
			ThreatType:    []string{"spoofing"},
			ThreatModelId: &tmID,
		}

		updateMap := store.buildThreatUpdateMap(threat, time.Now())
		ssvcVal, ok := updateMap["ssvc"]
		assert.True(t, ok)
		assert.Nil(t, ssvcVal)
	})
}
