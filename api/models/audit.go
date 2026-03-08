package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuditEntry represents an entry in the audit trail.
// The audit trail tracks who changed what and when for all entity mutations.
// Actor fields are denormalized (not FKs) so audit entries persist after user deletion.
// threat_model_id is not a FK so the "threat model deleted" entry persists after TM deletion.
type AuditEntry struct {
	ID               string         `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID    string         `gorm:"type:varchar(36);not null;index:idx_audit_tm;index:idx_audit_tm_created,priority:1"`
	ObjectType       string         `gorm:"type:varchar(50);not null;index:idx_audit_object,priority:1"`
	ObjectID         string         `gorm:"type:varchar(36);not null;index:idx_audit_object,priority:2"`
	Version          *int           `gorm:"index:idx_audit_object_version,priority:3"` // nullable: NULL means version snapshot has been pruned
	ChangeType       string         `gorm:"type:varchar(20);not null;index:idx_audit_change_type"`
	ActorEmail       string         `gorm:"type:varchar(320);not null"`
	ActorProvider    string         `gorm:"type:varchar(100);not null"`
	ActorProviderID  string         `gorm:"type:varchar(500);not null"`
	ActorDisplayName string         `gorm:"type:varchar(256);not null"`
	ChangeSummary    NullableDBText `gorm:""`
	CreatedAt        time.Time      `gorm:"not null;autoCreateTime;index:idx_audit_tm_created,priority:2"`
}

// TableName specifies the table name for AuditEntry
func (AuditEntry) TableName() string {
	return tableName("audit_entries")
}

// BeforeCreate generates a UUID if not set
func (a *AuditEntry) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// VersionSnapshot stores the data needed for rollback.
// Each snapshot is either a full JSON checkpoint or a reverse JSON Patch diff (RFC 6902).
// Checkpoints are stored every 10th version; all others are diffs.
// Snapshots have their own retention policy and can be pruned independently of audit entries.
type VersionSnapshot struct {
	ID           string         `gorm:"primaryKey;type:varchar(36)"`
	AuditEntryID string         `gorm:"type:varchar(36);not null;index:idx_vs_audit_entry"`
	ObjectType   string         `gorm:"type:varchar(50);not null;index:idx_vs_object,priority:1;index:idx_vs_object_snapshot,priority:1"`
	ObjectID     string         `gorm:"type:varchar(36);not null;index:idx_vs_object,priority:2;index:idx_vs_object_snapshot,priority:2"`
	Version      int            `gorm:"not null;index:idx_vs_object,priority:3"`
	SnapshotType string         `gorm:"type:varchar(20);not null;index:idx_vs_object_snapshot,priority:3"` // "checkpoint" or "diff"
	Data         NullableDBText `gorm:""`                                                                  // full JSON snapshot or reverse JSON Patch
	CreatedAt    time.Time      `gorm:"not null;autoCreateTime"`
}

// TableName specifies the table name for VersionSnapshot
func (VersionSnapshot) TableName() string {
	return tableName("version_snapshots")
}

// BeforeCreate generates a UUID if not set
func (v *VersionSnapshot) BeforeCreate(tx *gorm.DB) error {
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	return nil
}

// Audit change type constants
const (
	ChangeTypeCreated    = "created"
	ChangeTypeUpdated    = "updated"
	ChangeTypePatched    = "patched"
	ChangeTypeDeleted    = "deleted"
	ChangeTypeRestored   = "restored"
	ChangeTypeRolledBack = "rolled_back"
)

// Audit object type constants
const (
	ObjectTypeThreatModel = "threat_model"
	ObjectTypeDiagram     = "diagram"
	ObjectTypeThreat      = "threat"
	ObjectTypeAsset       = "asset"
	ObjectTypeDocument    = "document"
	ObjectTypeNote        = "note"
	ObjectTypeRepository  = "repository"
)

// Snapshot type constants
const (
	SnapshotTypeCheckpoint = "checkpoint"
	SnapshotTypeDiff       = "diff"
)

// CheckpointInterval defines how often a full checkpoint is stored (every Nth version)
const CheckpointInterval = 10
