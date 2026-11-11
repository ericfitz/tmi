// Package models defines GORM models for the TMI database schema.
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LinkedIdentity records an external identity provider credential linked to a
// TMI user account. Each (provider, provider_user_id) pair is globally unique —
// a provider sub cannot be linked to more than one TMI user.
//
// Referential integrity to the users table is enforced by application code; no
// DB-level FK is declared so that users can be deleted without a cascade
// constraint ordering concern.
type LinkedIdentity struct {
	ID               DBVarchar `gorm:"primaryKey;not null;size:36"`
	UserInternalUUID DBVarchar `gorm:"size:36;not null;index:idx_linked_user"`
	Provider         DBVarchar `gorm:"size:100;not null;uniqueIndex:uniq_linked_provider_sub,priority:1"`
	ProviderUserID   DBVarchar `gorm:"size:500;not null;uniqueIndex:uniq_linked_provider_sub,priority:2"`
	Email            DBVarchar `gorm:"size:320"`
	Name             DBVarchar `gorm:"size:256"`
	LinkedAt         time.Time `gorm:"not null;autoCreateTime"`
	LastUsedAt       *time.Time
}

// TableName returns the dialect-aware table name.
func (LinkedIdentity) TableName() string {
	return tableName("linked_identities")
}

// BeforeCreate generates a UUID if ID is empty.
func (l *LinkedIdentity) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = DBVarchar(uuid.New().String())
	}
	return nil
}
