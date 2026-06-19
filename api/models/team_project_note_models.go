package models

import (
	"time"

	"github.com/ericfitz/tmi/api/validation"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TeamNoteRecord represents a note attached to a team
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a note attached to a team, with cascade delete on team removal
type TeamNoteRecord struct {
	ID           DBVarchar      `gorm:"primaryKey;not null;size:36"`
	TeamID       DBVarchar      `gorm:"size:36;not null;index:idx_tnote_team;index:idx_tnote_team_name,priority:1"`
	Name         DBVarchar      `gorm:"size:256;not null;index:idx_tnote_name;index:idx_tnote_team_name,priority:2"`
	Content      DBText         `gorm:"not null"`
	Description  NullableDBText `gorm:""`
	TimmyEnabled DBBool         `gorm:"default:1"`
	Sharable     DBBool         `gorm:"not null"`
	CreatedAt    time.Time      `gorm:"not null;autoCreateTime;index:idx_tnote_created"`
	ModifiedAt   time.Time      `gorm:"not null;autoUpdateTime"`

	// Relationships
	Team TeamRecord `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for TeamNoteRecord
// SEM@14963ec2acf3a735a933d7f1e724e4c7d224cbe6: return the database table name for TeamNoteRecord (pure)
func (TeamNoteRecord) TableName() string {
	return tableName("team_notes")
}

// BeforeCreate generates a UUID if not set and validates required fields.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: assign a UUID and validate required fields before inserting a team note (pure)
func (n *TeamNoteRecord) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = DBVarchar(uuid.New().String())
	}
	if err := validation.ValidateNonEmpty("name", string(n.Name)); err != nil {
		return err
	}
	if err := validation.ValidateNonEmpty("content", string(n.Content)); err != nil {
		return err
	}
	return nil
}

// ProjectNoteRecord represents a note attached to a project
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a note attached to a project, with cascade delete on project removal
type ProjectNoteRecord struct {
	ID           DBVarchar      `gorm:"primaryKey;not null;size:36"`
	ProjectID    DBVarchar      `gorm:"size:36;not null;index:idx_pnote_project;index:idx_pnote_project_name,priority:1"`
	Name         DBVarchar      `gorm:"size:256;not null;index:idx_pnote_name;index:idx_pnote_project_name,priority:2"`
	Content      DBText         `gorm:"not null"`
	Description  NullableDBText `gorm:""`
	TimmyEnabled DBBool         `gorm:"default:1"`
	Sharable     DBBool         `gorm:"not null"`
	CreatedAt    time.Time      `gorm:"not null;autoCreateTime;index:idx_pnote_created"`
	ModifiedAt   time.Time      `gorm:"not null;autoUpdateTime"`

	// Relationships
	Project ProjectRecord `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for ProjectNoteRecord
// SEM@14963ec2acf3a735a933d7f1e724e4c7d224cbe6: return the database table name for ProjectNoteRecord (pure)
func (ProjectNoteRecord) TableName() string {
	return tableName("project_notes")
}

// BeforeCreate generates a UUID if not set and validates required fields.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: assign a UUID and validate required fields before inserting a project note (pure)
func (n *ProjectNoteRecord) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = DBVarchar(uuid.New().String())
	}
	if err := validation.ValidateNonEmpty("name", string(n.Name)); err != nil {
		return err
	}
	if err := validation.ValidateNonEmpty("content", string(n.Content)); err != nil {
		return err
	}
	return nil
}
