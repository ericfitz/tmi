//go:build oracle

package dbschema_test

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// oracleAppendOnlyTriggerNames are the three T19 triggers as Oracle holds them
// (unquoted identifiers fold to upper case under SkipQuoteIdentifiers).
var oracleAppendOnlyTriggerNames = []string{
	"TMI_AUDIT_ENTRIES_NO_MUTATE",
	"TMI_VERSION_SNAPSHOTS_NO_MUTATE",
	"TMI_SYSTEM_AUDIT_ENTRIES_NO_MUTATE",
}

// readOracleTriggerDDLTimes returns LAST_DDL_TIME per trigger from ALL_OBJECTS,
// scoped to CURRENT_SCHEMA like the production probes (#736). Any missing
// trigger fails the test.
func readOracleTriggerDDLTimes(t *testing.T, db *gorm.DB) map[string]time.Time {
	t.Helper()
	out := map[string]time.Time{}
	for _, name := range oracleAppendOnlyTriggerNames {
		var ts []time.Time
		require.NoError(t, db.Raw(
			"SELECT LAST_DDL_TIME FROM ALL_OBJECTS WHERE OBJECT_TYPE = 'TRIGGER' AND OBJECT_NAME = ? "+
				"AND OWNER = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')", name,
		).Scan(&ts).Error)
		require.Len(t, ts, 1, "trigger %s must exist exactly once", name)
		out[name] = ts[0]
	}
	return out
}

// TestAuditAppendOnlyTriggersSteadyStateNoDDLOracleIntegration covers #893: a
// second install with the same floors must issue no DDL (LAST_DDL_TIME is
// untouched on all three triggers), while a changed floor must reinstall.
func TestAuditAppendOnlyTriggersSteadyStateNoDDLOracleIntegration(t *testing.T) {
	db := openOracleDB(t)
	ctx := context.Background()
	base := dbschema.AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 30, SystemAuditRetentionDays: 365}
	changed := base
	changed.AuditRetentionDays = 400

	// Whatever happens below, leave the production floors installed.
	t.Cleanup(func() {
		if err := dbschema.InstallAuditAppendOnlyTriggers(ctx, db, base); err != nil {
			t.Errorf("restoring append-only triggers during cleanup: %v", err)
		}
	})

	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, base))
	first := readOracleTriggerDDLTimes(t, db)

	// LAST_DDL_TIME has one-second resolution; make a reinstall observable.
	time.Sleep(1500 * time.Millisecond)
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, base))
	second := readOracleTriggerDDLTimes(t, db)
	assert.Equal(t, first, second, "a steady-state install must not issue trigger DDL")

	time.Sleep(1500 * time.Millisecond)
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, changed))
	third := readOracleTriggerDDLTimes(t, db)
	assert.True(t, third["TMI_AUDIT_ENTRIES_NO_MUTATE"].After(second["TMI_AUDIT_ENTRIES_NO_MUTATE"]),
		"a changed audit floor must reinstall tmi_audit_entries_no_mutate")
	assert.Equal(t, second["TMI_VERSION_SNAPSHOTS_NO_MUTATE"], third["TMI_VERSION_SNAPSHOTS_NO_MUTATE"],
		"an unchanged trigger must be left alone even when a sibling is reinstalled")
	assert.Equal(t, second["TMI_SYSTEM_AUDIT_ENTRIES_NO_MUTATE"], third["TMI_SYSTEM_AUDIT_ENTRIES_NO_MUTATE"])
}
