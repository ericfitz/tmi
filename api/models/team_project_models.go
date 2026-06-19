// Package models defines GORM models for the TMI database schema.
// This file contains models for the Teams and Projects feature.
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TeamRecord represents a team in the system
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a team with membership, audit timestamps, and versioning (reads DB)
type TeamRecord struct {
	ID                     DBVarchar         `gorm:"primaryKey;not null;size:36"`
	Name                   DBVarchar         `gorm:"size:256;not null;index:idx_team_name"`
	Description            NullableDBText    `gorm:""`
	URI                    NullableDBText    `gorm:""`
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
// SEM@8c7929da791c778ff88713684c47aa2a10911bba: return the database table name for TeamRecord (pure)
func (TeamRecord) TableName() string {
	return tableName("teams")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate a UUID for a TeamRecord if none is set before insert (mutates shared state)
func (t *TeamRecord) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TeamMemberRecord represents a user's membership in a team
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model representing a user's role membership in a team (reads DB)
type TeamMemberRecord struct {
	ID               DBVarchar         `gorm:"primaryKey;not null;size:36"`
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
// SEM@8c7929da791c778ff88713684c47aa2a10911bba: return the DB table name for the team member entity (pure)
func (TeamMemberRecord) TableName() string {
	return tableName("team_members")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate and assign a UUID to the team member record if absent (mutates shared state)
func (t *TeamMemberRecord) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TeamResponsiblePartyRecord represents a responsible party for a team
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model mapping a responsible party user to a team with role
type TeamResponsiblePartyRecord struct {
	ID               DBVarchar         `gorm:"primaryKey;not null;size:36"`
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
// SEM@8c7929da791c778ff88713684c47aa2a10911bba: return the DB table name for the team responsible party entity (pure)
func (TeamResponsiblePartyRecord) TableName() string {
	return tableName("team_responsible_parties")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate and assign a UUID to the team responsible party record if absent (mutates shared state)
func (t *TeamResponsiblePartyRecord) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TeamRelationshipRecord represents a relationship between two teams
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model representing a typed directional relationship between two teams
type TeamRelationshipRecord struct {
	ID                 DBVarchar         `gorm:"primaryKey;not null;size:36"`
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
// SEM@8c7929da791c778ff88713684c47aa2a10911bba: return the DB table name for the team relationship entity (pure)
func (TeamRelationshipRecord) TableName() string {
	return tableName("team_relationships")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate and assign a UUID to the team relationship record if absent (mutates shared state)
func (t *TeamRelationshipRecord) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ProjectRecord represents a project in the system
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a project with ownership, status, review, and versioning metadata
type ProjectRecord struct {
	ID                     DBVarchar         `gorm:"primaryKey;not null;size:36"`
	Name                   DBVarchar         `gorm:"size:256;not null;index:idx_proj_name"`
	Description            NullableDBText    `gorm:""`
	TeamID                 DBVarchar         `gorm:"size:36;not null;index:idx_proj_team"`
	URI                    NullableDBText    `gorm:""`
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
// SEM@8c7929da791c778ff88713684c47aa2a10911bba: return the DB table name for the project entity (pure)
func (ProjectRecord) TableName() string {
	return tableName("projects")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate and assign a UUID to the project record if absent (mutates shared state)
func (p *ProjectRecord) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ProjectResponsiblePartyRecord represents a responsible party for a project
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model mapping a responsible party user to a project with role
type ProjectResponsiblePartyRecord struct {
	ID               DBVarchar         `gorm:"primaryKey;not null;size:36"`
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
// SEM@8c7929da791c778ff88713684c47aa2a10911bba: return the DB table name for the project responsible party entity (pure)
func (ProjectResponsiblePartyRecord) TableName() string {
	return tableName("project_responsible_parties")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate and assign a UUID to the project responsible party record if absent (mutates shared state)
func (p *ProjectResponsiblePartyRecord) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ProjectRelationshipRecord represents a relationship between two projects
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model representing a typed directional relationship between two projects
type ProjectRelationshipRecord struct {
	ID                 DBVarchar         `gorm:"primaryKey;not null;size:36"`
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
// SEM@8c7929da791c778ff88713684c47aa2a10911bba: return the DB table name for the project relationship entity (pure)
func (ProjectRelationshipRecord) TableName() string {
	return tableName("project_relationships")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate and assign a UUID to the project relationship record if absent (mutates shared state)
func (p *ProjectRelationshipRecord) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = DBVarchar(uuid.New().String())
	}
	return nil
}
