package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SystemAuditEntry is a system-level audit row recording a single /admin/*
// write. Distinct from AuditEntry (which is threat-model-scoped). Append-only;
// writes only happen on 2xx responses; redaction policy is applied at write
// time per the deny-list in api/admin_audit_redaction.go (see #355).
//
// Actor fields are denormalized (matches AuditEntry pattern) so audit rows
// persist after user deletion. No FKs by design — investigators rely on the
// row content, not on join integrity.
type SystemAuditEntry struct {
	ID DBVarchar `gorm:"primaryKey;not null;size:36;index:idx_sysaudit_created_id,priority:2"`

	// Actor identity (denormalized)
	ActorEmail       DBVarchar `gorm:"size:320;not null;index:idx_sysaudit_actor,priority:1"`
	ActorProvider    DBVarchar `gorm:"size:100;not null"`
	ActorProviderID  DBVarchar `gorm:"size:500;not null"`
	ActorDisplayName DBVarchar `gorm:"size:256;not null"`

	// Request shape
	HTTPMethod DBVarchar `gorm:"size:10;not null"`
	HTTPPath   DBText    `gorm:"not null"`

	// Change description
	FieldPath        DBVarchar      `gorm:"size:1024;not null;index:idx_sysaudit_field"`
	OldValueRedacted NullableDBText `gorm:""`
	NewValueRedacted NullableDBText `gorm:""`
	ChangeSummary    NullableDBText `gorm:""`

	// Composite (created_at, id) index serves the bidirectional keyset scans and
	// the unfiltered full-table export in both ASC and DESC directions (#473). It
	// also covers single-column created_at lookups by prefix, so the former
	// single-column idx_sysaudit_created has been dropped from fresh schemas.
	CreatedAt time.Time `gorm:"not null;autoCreateTime;index:idx_sysaudit_actor,priority:2;index:idx_sysaudit_created_id,priority:1"`
}

// TableName returns the table name, casing-aware for Oracle compatibility.
func (SystemAuditEntry) TableName() string {
	return tableName("system_audit_entries")
}

// BeforeCreate generates a UUID if not set.
func (s *SystemAuditEntry) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = DBVarchar(uuid.New().String())
	}
	return nil
}
