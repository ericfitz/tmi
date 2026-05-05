package dbschema

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
	require.NoError(t, InstallAuditAppendOnlyTriggers(context.Background(), db))

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
