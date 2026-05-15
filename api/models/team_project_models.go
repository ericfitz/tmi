// Package models defines GORM models for the TMI database schema.
// This file contains models for the Teams and Projects feature.
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TeamRecord represents a team in the system
type TeamRecord struct {
	ID                     DBVarchar         `gorm:"primaryKey;size:36"`
	Name                   DBVarchar         `gorm:"size:256;not null;index:idx_team_name"`
	Description            *string           `gorm:"type:varchar(2048)"`
	URI                    *string           `gorm:"type:varchar(1000)"`
	EmailAddress           NullableDBVarchar `gorm:"size:320"`
	Status                 NullableDBVarchar `gorm:"size:128;index:idx_team_status"`
	CreatedByInternalUUID  DBVarchar         `gorm:"size:36;not null"`
	ModifiedByInternalUUID NullableDBVarchar `gorm:"size:36"`
	ReviewedByInternalUUID NullableDBVarchar `gorm:"size:36"`
	ReviewedAt             *time.Time        `gorm:"index:idx_team_reviewed_at"`
	CreatedAt              time.Time         `gorm:"not null;autoCreateTime;index:idx_team_created_at"`
	ModifiedAt             time.Time         `gorm:"not null;autoUpdateTime"`
	// Version is incremented on every successful update (T14 / #385).
	Version int `gorm:"not null;default:1"`

	// Relationships
	CreatedBy  User  `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID;constraint:-"`
	ModifiedBy *User `gorm:"foreignKey:ModifiedByInternalUUID;references:InternalUUID;constraint:-"`
	ReviewedBy *User `gorm:"foreignKey:ReviewedByInternalUUID;references:InternalUUID;constraint:-"`
}

// TableName specifies the table name for TeamRecord
func (TeamRecord) TableName() string {
	return tableName("teams")
}

// BeforeCreate generates a UUID if not set
func (t *TeamRecord) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TeamMemberRecord represents a user's membership in a team
type TeamMemberRecord struct {
	ID               DBVarchar         `gorm:"primaryKey;size:36"`
	TeamID           DBVarchar         `gorm:"size:36;not null;index:idx_tmem_team;uniqueIndex:idx_tmem_team_user,priority:1"`
	UserInternalUUID DBVarchar         `gorm:"size:36;not null;index:idx_tmem_user;uniqueIndex:idx_tmem_team_user,priority:2"`
	Role             DBVarchar         `gorm:"size:64;not null;default:engineer"`
	CustomRole       NullableDBVarchar `gorm:"size:128"`
	CreatedAt        time.Time         `gorm:"not null;autoCreateTime"`

	// Relationships
	Team TeamRecord `gorm:"foreignKey:TeamID"`
	User User       `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for TeamMemberRecord
func (TeamMemberRecord) TableName() string {
	return tableName("team_members")
}

// BeforeCreate generates a UUID if not set
func (t *TeamMemberRecord) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TeamResponsiblePartyRecord represents a responsible party for a team
type TeamResponsiblePartyRecord struct {
	ID               DBVarchar         `gorm:"primaryKey;size:36"`
	TeamID           DBVarchar         `gorm:"size:36;not null;index:idx_trp_team;uniqueIndex:idx_trp_team_user,priority:1"`
	UserInternalUUID DBVarchar         `gorm:"size:36;not null;index:idx_trp_user;uniqueIndex:idx_trp_team_user,priority:2"`
	Role             DBVarchar         `gorm:"size:64;not null"`
	CustomRole       NullableDBVarchar `gorm:"size:128"`
	CreatedAt        time.Time         `gorm:"not null;autoCreateTime"`

	// Relationships
	Team TeamRecord `gorm:"foreignKey:TeamID"`
	User User       `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for TeamResponsiblePartyRecord
func (TeamResponsiblePartyRecord) TableName() string {
	return tableName("team_responsible_parties")
}

// BeforeCreate generates a UUID if not set
func (t *TeamResponsiblePartyRecord) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TeamRelationshipRecord represents a relationship between two teams
type TeamRelationshipRecord struct {
	ID                 DBVarchar         `gorm:"primaryKey;size:36"`
	TeamID             DBVarchar         `gorm:"size:36;not null;index:idx_trel_team;uniqueIndex:idx_trel_team_related,priority:1"`
	RelatedTeamID      DBVarchar         `gorm:"size:36;not null;index:idx_trel_related;uniqueIndex:idx_trel_team_related,priority:2"`
	Relationship       DBVarchar         `gorm:"size:64;not null"`
	CustomRelationship NullableDBVarchar `gorm:"size:128"`
	CreatedAt          time.Time         `gorm:"not null;autoCreateTime"`

	// Relationships
	Team        TeamRecord `gorm:"foreignKey:TeamID"`
	RelatedTeam TeamRecord `gorm:"foreignKey:RelatedTeamID"`
}

// TableName specifies the table name for TeamRelationshipRecord
func (TeamRelationshipRecord) TableName() string {
	return tableName("team_relationships")
}

// BeforeCreate generates a UUID if not set
func (t *TeamRelationshipRecord) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ProjectRecord represents a project in the system
type ProjectRecord struct {
	ID                     DBVarchar         `gorm:"primaryKey;size:36"`
	Name                   DBVarchar         `gorm:"size:256;not null;index:idx_proj_name"`
	Description            *string           `gorm:"type:varchar(2048)"`
	TeamID                 DBVarchar         `gorm:"size:36;not null;index:idx_proj_team"`
	URI                    *string           `gorm:"type:varchar(1000)"`
	Status                 NullableDBVarchar `gorm:"size:128;index:idx_proj_status"`
	CreatedByInternalUUID  DBVarchar         `gorm:"size:36;not null"`
	ModifiedByInternalUUID NullableDBVarchar `gorm:"size:36"`
	ReviewedByInternalUUID NullableDBVarchar `gorm:"size:36"`
	ReviewedAt             *time.Time        `gorm:"index:idx_proj_reviewed_at"`
	CreatedAt              time.Time         `gorm:"not null;autoCreateTime;index:idx_proj_created_at"`
	ModifiedAt             time.Time         `gorm:"not null;autoUpdateTime"`
	// Version is incremented on every successful update (T14 / #385).
	Version int `gorm:"not null;default:1"`

	// Relationships
	Team       TeamRecord `gorm:"foreignKey:TeamID"`
	CreatedBy  User       `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID;constraint:-"`
	ModifiedBy *User      `gorm:"foreignKey:ModifiedByInternalUUID;references:InternalUUID;constraint:-"`
	ReviewedBy *User      `gorm:"foreignKey:ReviewedByInternalUUID;references:InternalUUID;constraint:-"`
}

// TableName specifies the table name for ProjectRecord
func (ProjectRecord) TableName() string {
	return tableName("projects")
}

// BeforeCreate generates a UUID if not set
func (p *ProjectRecord) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ProjectResponsiblePartyRecord represents a responsible party for a project
type ProjectResponsiblePartyRecord struct {
	ID               DBVarchar         `gorm:"primaryKey;size:36"`
	ProjectID        DBVarchar         `gorm:"size:36;not null;index:idx_prp_project;uniqueIndex:idx_prp_project_user,priority:1"`
	UserInternalUUID DBVarchar         `gorm:"size:36;not null;index:idx_prp_user;uniqueIndex:idx_prp_project_user,priority:2"`
	Role             DBVarchar         `gorm:"size:64;not null"`
	CustomRole       NullableDBVarchar `gorm:"size:128"`
	CreatedAt        time.Time         `gorm:"not null;autoCreateTime"`

	// Relationships
	Project ProjectRecord `gorm:"foreignKey:ProjectID"`
	User    User          `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for ProjectResponsiblePartyRecord
func (ProjectResponsiblePartyRecord) TableName() string {
	return tableName("project_responsible_parties")
}

// BeforeCreate generates a UUID if not set
func (p *ProjectResponsiblePartyRecord) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ProjectRelationshipRecord represents a relationship between two projects
type ProjectRelationshipRecord struct {
	ID                 DBVarchar         `gorm:"primaryKey;size:36"`
	ProjectID          DBVarchar         `gorm:"size:36;not null;index:idx_prel_project;uniqueIndex:idx_prel_project_related,priority:1"`
	RelatedProjectID   DBVarchar         `gorm:"size:36;not null;index:idx_prel_related;uniqueIndex:idx_prel_project_related,priority:2"`
	Relationship       DBVarchar         `gorm:"size:64;not null"`
	CustomRelationship NullableDBVarchar `gorm:"size:128"`
	CreatedAt          time.Time         `gorm:"not null;autoCreateTime"`

	// Relationships
	Project        ProjectRecord `gorm:"foreignKey:ProjectID"`
	RelatedProject ProjectRecord `gorm:"foreignKey:RelatedProjectID"`
}

// TableName specifies the table name for ProjectRelationshipRecord
func (ProjectRelationshipRecord) TableName() string {
	return tableName("project_relationships")
}

// BeforeCreate generates a UUID if not set
func (p *ProjectRelationshipRecord) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = DBVarchar(uuid.New().String())
	}
	return nil
}
