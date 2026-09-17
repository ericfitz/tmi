package dbschema

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAuditFloorConfig_Floors(t *testing.T) {
	tests := []struct {
		name                 string
		cfg                  AuditFloorConfig
		wantAuditFloor       int
		wantSnapshotFloor    int
		wantSystemAuditFloor int
	}{
		{
			name:                 "defaults",
			cfg:                  AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 30, SystemAuditRetentionDays: 365},
			wantAuditFloor:       364,
			wantSnapshotFloor:    29,
			wantSystemAuditFloor: 364,
		},
		{
			name:                 "audit retention below hard minimum clamps to 30",
			cfg:                  AuditFloorConfig{AuditRetentionDays: 10, VersionRetentionDays: 90, TombstoneRetentionDays: 30, SystemAuditRetentionDays: 365},
			wantAuditFloor:       30,
			wantSnapshotFloor:    29,
			wantSystemAuditFloor: 364,
		},
		{
			name:                 "snapshot floor uses the smaller of version and tombstone retention",
			cfg:                  AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 20, TombstoneRetentionDays: 30, SystemAuditRetentionDays: 365},
			wantAuditFloor:       364,
			wantSnapshotFloor:    19,
			wantSystemAuditFloor: 364,
		},
		{
			name:                 "snapshot floor clamps to hard minimum 7",
			cfg:                  AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 3, SystemAuditRetentionDays: 365},
			wantAuditFloor:       364,
			wantSnapshotFloor:    7,
			wantSystemAuditFloor: 364,
		},
		{
			name:                 "system audit default",
			cfg:                  AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 30, SystemAuditRetentionDays: 365},
			wantAuditFloor:       364,
			wantSnapshotFloor:    29,
			wantSystemAuditFloor: 364,
		},
		{
			name:                 "system audit clamps to 90-day hard minimum",
			cfg:                  AuditFloorConfig{AuditRetentionDays: 365, VersionRetentionDays: 90, TombstoneRetentionDays: 30, SystemAuditRetentionDays: 30},
			wantAuditFloor:       364,
			wantSnapshotFloor:    29,
			wantSystemAuditFloor: 90,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantAuditFloor, tt.cfg.auditEntriesFloorDays())
			assert.Equal(t, tt.wantSnapshotFloor, tt.cfg.versionSnapshotsFloorDays())
			assert.Equal(t, tt.wantSystemAuditFloor, tt.cfg.systemAuditEntriesFloorDays())
		})
	}
}

// auditEntryRow mirrors api/models.AuditEntry's column shape closely enough
// to test the trigger in isolation. We don't import the real model to avoid
// a dependency cycle (api → dbschema, not the other way around).
type auditEntryRow struct {
	ID            string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID string    `gorm:"type:varchar(36);not null"`
	ObjectType    string    `gorm:"type:varchar(50);not null"`
	ObjectID      string    `gorm:"type:varchar(36);not null"`
	ChangeType    string    `gorm:"type:varchar(20);not null"`
	ActorEmail    string    `gorm:"type:varchar(320);not null"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime"`
}

func (auditEntryRow) TableName() string { return "audit_entries" }

// TestInstallAuditAppendOnlyTriggers_SqliteSkipsCleanly verifies the
// SQLite branch is a no-op (no trigger installed) so existing in-memory
// SQLite test suites that rely on writing to a stand-in audit_entries
// table are not broken by this change.
//
// PG and Oracle branches are exercised by the integration test suite
// against real database containers (see test/integration/...).
func TestInstallAuditAppendOnlyTriggers_SqliteSkipsCleanly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(&auditEntryRow{}))
	require.NoError(t, InstallAuditAppendOnlyTriggers(context.Background(), db, AuditFloorConfig{
		AuditRetentionDays:     365,
		VersionRetentionDays:   90,
		TombstoneRetentionDays: 30,
	}))

	row := auditEntryRow{
		ID:            uuid.New().String(),
		ThreatModelID: uuid.New().String(),
		ObjectType:    "threat_model",
		ObjectID:      uuid.New().String(),
		ChangeType:    "created",
		ActorEmail:    "test@example.com",
	}
	require.NoError(t, db.Create(&row).Error)

	// On SQLite the install was skipped, so UPDATE / DELETE go through.
	require.NoError(t, db.Model(&row).Update("change_type", "updated").Error,
		"SQLite install must be a no-op (no trigger blocking writes)")
	require.NoError(t, db.Delete(&row).Error,
		"SQLite install must be a no-op (no trigger blocking writes)")
}

// TestOracleTriggerMarker covers #893: the marker embedded in each Oracle
// trigger body identifies the exact DDL (so a changed floor or body text is
// reinstalled) and lands as the first line after BEGIN, where ALL_SOURCE
// preserves it for the currency probe.
func TestOracleTriggerMarker(t *testing.T) {
	ddl := "CREATE OR REPLACE TRIGGER t\n BEFORE UPDATE ON x\n FOR EACH ROW\n BEGIN\n   NULL;\n END;"
	other := "CREATE OR REPLACE TRIGGER t\n BEFORE UPDATE ON x\n FOR EACH ROW\n BEGIN\n   RAISE_APPLICATION_ERROR(-20001, 'no');\n END;"

	assert.NotEqual(t, oracleTriggerMarker(ddl), oracleTriggerMarker(other), "different bodies must carry different markers")
	assert.Equal(t, oracleTriggerMarker(ddl), oracleTriggerMarker(ddl), "marker must be deterministic")

	marked := oracleTriggerDDLWithMarker(ddl)
	assert.Contains(t, marked, "BEGIN\n\t\t   "+oracleTriggerMarker(ddl)+"\n   NULL;", "marker must be the first body line after BEGIN")
	assert.Equal(t, 1, strings.Count(marked, oracleTriggerMarkerPrefix), "exactly one marker per body")

	// The production DDL strings, not just a synthetic one: the insertion
	// keys on the literal "BEGIN\n", and a reformat that broke it would
	// silently reinstall every boot (oracle-db-admin review, #893).
	for _, trg := range oracleAppendOnlyTriggers(364, 29, 364) {
		m := oracleTriggerDDLWithMarker(trg.ddl)
		assert.Contains(t, m, "BEGIN\n\t\t   "+oracleTriggerMarker(trg.ddl)+"\n", "%s: marker must follow BEGIN", trg.name)
		assert.Contains(t, m, "TRIGGER "+trg.name+"\n", "%s: name must match the DDL", trg.name)
	}
	assert.NotEqual(t, oracleTriggerMarker(oracleAppendOnlyTriggers(364, 29, 364)[0].ddl),
		oracleTriggerMarker(oracleAppendOnlyTriggers(399, 29, 364)[0].ddl), "a changed floor must change the marker")
}
