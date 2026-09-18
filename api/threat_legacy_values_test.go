package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SEM@d4baf9204f11e11bdb71462a0ea5af2d70f2cad5: verify legacy threat field values map to canonical and free-form values pass through (pure)
func TestCanonicalThreatValue(t *testing.T) {
	tests := []struct{ column, in, want string }{
		{"severity", "0", "critical"}, // old numeric keys run opposite to rank
		{"severity", "4", "informational"},
		{"severity", "5", "unknown"},
		{"severity", "Info", "informational"},
		{"severity", "none", "informational"},
		{"severity", "High", "high"},
		{"severity", "critical", "critical"},
		{"severity", "Sev-A (custom)", "Sev-A (custom)"}, // free-form stays untouched
		{"priority", "0", "immediate"},
		{"priority", "4", "deferred"},
		{"priority", "High (P1)", "high"},
		{"priority", "Medium", "medium"},
		{"status", "0", "open"},
		{"status", "9", "closed"},
		{"status", "Mitigation In Progress", "mitigation_in_progress"},
		{"status", "in_progress", "in_progress"}, // server-side legacy status: out of scope (#925)
		{"name", "0", "0"},                       // columns without a legacy map are untouched
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, canonicalThreatValue(tt.column, tt.in), "%s=%q", tt.column, tt.in)
	}
}

// SEM@d4baf9204f11e11bdb71462a0ea5af2d70f2cad5: verify the startup migration rewrites stored legacy threat values once and is idempotent (reads DB)
func TestMigrateLegacyThreatValues(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE threats (id TEXT PRIMARY KEY, severity TEXT, priority TEXT, status TEXT)").Error)
	rows := [][4]any{
		{"a", "0", "Immediate (P0)", "2"},
		{"b", "info", "4", "False Positive"},
		{"c", "critical", "high", "open"},          // already canonical
		{"d", "Sev-A (custom)", "P-custom", "WIP"}, // free-form
		{"e", nil, nil, nil},
		{"f", "5", "Low", "in_progress"},
	}
	for _, r := range rows {
		require.NoError(t, db.Exec("INSERT INTO threats VALUES (?, ?, ?, ?)", r[0], r[1], r[2], r[3]).Error)
	}

	n, err := MigrateLegacyThreatValues(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, int64(8), n)

	type row struct{ ID, Severity, Priority, Status *string }
	var got []row
	require.NoError(t, db.Raw("SELECT id, severity, priority, status FROM threats ORDER BY id").Scan(&got).Error)
	s := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	want := [][3]string{
		{"critical", "immediate", "mitigation_planned"},
		{"informational", "deferred", "false_positive"},
		{"critical", "high", "open"},
		{"Sev-A (custom)", "P-custom", "WIP"},
		{"<nil>", "<nil>", "<nil>"},
		{"unknown", "low", "in_progress"},
	}
	require.Len(t, got, len(want))
	for i, w := range want {
		assert.Equal(t, w, [3]string{s(got[i].Severity), s(got[i].Priority), s(got[i].Status)}, "row %s", *got[i].ID)
	}

	n, err = MigrateLegacyThreatValues(context.Background(), db)
	require.NoError(t, err)
	assert.Zero(t, n, "second run must be a no-op")
}
