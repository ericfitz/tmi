package models

import (
	"time"

	"github.com/ericfitz/tmi/api/validation"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TeamNoteRecord represents a note attached to a team
type TeamNoteRecord struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)"`
	TeamID       string    `gorm:"type:varchar(36);not null;index:idx_tnote_team;index:idx_tnote_team_name,priority:1"`
	Name         string    `gorm:"type:varchar(256);not null;index:idx_tnote_name;index:idx_tnote_team_name,priority:2"`
	Content      DBText    `gorm:"not null"`
	Description  *string   `gorm:"type:varchar(2048)"`
	TimmyEnabled DBBool    `gorm:"default:1"`
	Sharable     DBBool    `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null;autoCreateTime;index:idx_tnote_created"`
	ModifiedAt   time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	Team TeamRecord `gorm:"foreignKey:TeamID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for TeamNoteRecord
func (TeamNoteRecord) TableName() string {
	return tableName("team_notes")
}

// BeforeCreate generates a UUID if not set and validates required fields.
func (n *TeamNoteRecord) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	if err := validation.ValidateNonEmpty("name", n.Name); err != nil {
		return err
	}
	if err := validation.ValidateNonEmpty("content", string(n.Content)); err != nil {
		return err
	}
	return nil
}

// ProjectNoteRecord represents a note attached to a project
type ProjectNoteRecord struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)"`
	ProjectID    string    `gorm:"type:varchar(36);not null;index:idx_pnote_project;index:idx_pnote_project_name,priority:1"`
	Name         string    `gorm:"type:varchar(256);not null;index:idx_pnote_name;index:idx_pnote_project_name,priority:2"`
	Content      DBText    `gorm:"not null"`
	Description  *string   `gorm:"type:varchar(2048)"`
	TimmyEnabled DBBool    `gorm:"default:1"`
	Sharable     DBBool    `gorm:"not null"`
	CreatedAt    time.Time `gorm:"not null;autoCreateTime;index:idx_pnote_created"`
	ModifiedAt   time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	Project ProjectRecord `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for ProjectNoteRecord
func (ProjectNoteRecord) TableName() string {
	return tableName("project_notes")
}

// BeforeCreate generates a UUID if not set and validates required fields.
func (n *ProjectNoteRecord) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	if err := validation.ValidateNonEmpty("name", n.Name); err != nil {
		return err
	}
	if err := validation.ValidateNonEmpty("content", string(n.Content)); err != nil {
		return err
	}
	return nil
}
