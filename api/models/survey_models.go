// Package models defines GORM models for the TMI database schema.
// This file contains models for the Survey API feature.
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SurveyTemplate represents a survey template for security review intake
type SurveyTemplate struct {
	ID                    string    `gorm:"primaryKey;type:varchar(36)"`
	Name                  string    `gorm:"type:varchar(256);not null;index:idx_st_name"`
	Description           *string   `gorm:"type:varchar(2048)"`
	Version               string    `gorm:"type:varchar(64);not null;index:idx_st_version;uniqueIndex:idx_st_name_version,priority:2"`
	Status                string    `gorm:"type:varchar(20);not null;default:inactive;index:idx_st_status"`
	SurveyJSON            JSONRaw   `gorm:"column:survey_json"` // Complete SurveyJS JSON definition (opaque blob)
	Settings              JSONRaw   `gorm:""`                   // Template settings (allow_threat_model_linking, etc.)
	CreatedByInternalUUID string    `gorm:"type:varchar(36);not null;index:idx_st_created_by"`
	CreatedAt             time.Time `gorm:"not null;autoCreateTime;index:idx_st_created_at"`
	ModifiedAt            time.Time `gorm:"not null;autoUpdateTime"`

	// Unique constraint on (name, version)
	_ struct{} `gorm:"uniqueIndex:idx_st_name_version,priority:1"`

	// Relationships
	CreatedBy User `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for SurveyTemplate
func (SurveyTemplate) TableName() string {
	return tableName("survey_templates")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyTemplate) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// SurveyTemplateVersion represents a versioned snapshot of a survey template definition
type SurveyTemplateVersion struct {
	ID                    string    `gorm:"primaryKey;type:varchar(36)"`
	TemplateID            string    `gorm:"type:varchar(36);not null;index:idx_stv_template;uniqueIndex:idx_stv_template_version,priority:1"`
	Version               string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_stv_template_version,priority:2"`
	SurveyJSON            JSONRaw   `gorm:"column:survey_json"`
	CreatedByInternalUUID string    `gorm:"type:varchar(36);not null"`
	CreatedAt             time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	Template  SurveyTemplate `gorm:"foreignKey:TemplateID"`
	CreatedBy User           `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for SurveyTemplateVersion
func (SurveyTemplateVersion) TableName() string {
	return tableName("survey_template_versions")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyTemplateVersion) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// SurveyResponse represents a user's response to a survey template
type SurveyResponse struct {
	ID                     string     `gorm:"primaryKey;type:varchar(36)"`
	TemplateID             string     `gorm:"type:varchar(36);not null;index:idx_sr_template;index:idx_sr_template_status,priority:1"`
	TemplateVersion        string     `gorm:"type:varchar(64);not null"` // Captured at creation, immutable
	Status                 string     `gorm:"type:varchar(30);not null;default:draft;index:idx_sr_status;index:idx_sr_template_status,priority:2"`
	IsConfidential         DBBool     `gorm:"default:0"`          // If true, Security Reviewers group not auto-added
	Answers                JSONRaw    `gorm:""`                   // Question answers keyed by question name
	UIState                JSONRaw    `gorm:"column:ui_state"`    // Client-managed UI state for draft resumption
	SurveyJSON             JSONRaw    `gorm:"column:survey_json"` // Snapshot of template survey_json at creation
	LinkedThreatModelID    *string    `gorm:"type:varchar(36);index:idx_sr_linked_tm"`
	CreatedThreatModelID   *string    `gorm:"type:varchar(36);index:idx_sr_created_tm"`
	RevisionNotes          *string    `gorm:"type:varchar(4096)"` // Notes from reviewer when returning for revision
	OwnerInternalUUID      string     `gorm:"type:varchar(36);not null;index:idx_sr_owner"`
	CreatedAt              time.Time  `gorm:"not null;autoCreateTime;index:idx_sr_created_at"`
	ModifiedAt             time.Time  `gorm:"not null;autoUpdateTime"`
	SubmittedAt            *time.Time `gorm:"index:idx_sr_submitted_at"`
	ReviewedAt             *time.Time
	ReviewedByInternalUUID *string `gorm:"type:varchar(36)"`

	// Relationships
	Template           SurveyTemplate `gorm:"foreignKey:TemplateID"`
	Owner              User           `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
	ReviewedBy         *User          `gorm:"foreignKey:ReviewedByInternalUUID;references:InternalUUID"`
	LinkedThreatModel  *ThreatModel   `gorm:"foreignKey:LinkedThreatModelID"`
	CreatedThreatModel *ThreatModel   `gorm:"foreignKey:CreatedThreatModelID"`
}

// TableName specifies the table name for SurveyResponse
func (SurveyResponse) TableName() string {
	return tableName("survey_responses")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyResponse) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// TriageNote represents a triage note attached to a survey response
// Uses a composite primary key (SurveyResponseID, ID) where ID is a
// per-response monotonically increasing integer.
type TriageNote struct {
	SurveyResponseID       string    `gorm:"primaryKey;type:varchar(36);index:idx_tn_sr"`
	ID                     int       `gorm:"primaryKey;autoIncrement:false"`
	Name                   string    `gorm:"type:varchar(256);not null"`
	Content                DBText    `gorm:"not null"`
	CreatedByInternalUUID  string    `gorm:"type:varchar(36);not null"`
	ModifiedByInternalUUID string    `gorm:"type:varchar(36);not null"`
	CreatedAt              time.Time `gorm:"not null;autoCreateTime;index:idx_tn_created"`
	ModifiedAt             time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	SurveyResponse SurveyResponse `gorm:"foreignKey:SurveyResponseID"`
	CreatedBy      User           `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID"`
	ModifiedBy     User           `gorm:"foreignKey:ModifiedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for TriageNote
func (TriageNote) TableName() string {
	return tableName("triage_notes")
}

// BeforeCreate assigns the next sequential ID for the survey response
func (t *TriageNote) BeforeCreate(tx *gorm.DB) error {
	if t.ID == 0 {
		var maxID *int
		tx.Model(&TriageNote{}).
			Where("survey_response_id = ?", t.SurveyResponseID).
			Select("MAX(id)").
			Scan(&maxID)
		if maxID != nil {
			t.ID = *maxID + 1
		} else {
			t.ID = 1
		}
	}
	return nil
}

// SurveyResponseAccess represents access control for a survey response
// Mirrors the ThreatModelAccess pattern for consistency
type SurveyResponseAccess struct {
	ID                    string    `gorm:"primaryKey;type:varchar(36)"`
	SurveyResponseID      string    `gorm:"type:varchar(36);not null;index:idx_sra_sr;index:idx_sra_perf,priority:1"`
	UserInternalUUID      *string   `gorm:"type:varchar(36);index:idx_sra_user;index:idx_sra_perf,priority:3"`
	GroupInternalUUID     *string   `gorm:"type:varchar(36);index:idx_sra_group;index:idx_sra_perf,priority:4"`
	SubjectType           string    `gorm:"type:varchar(10);not null;index:idx_sra_subject_type;index:idx_sra_perf,priority:2"`
	Role                  string    `gorm:"type:varchar(6);not null;index:idx_sra_role"`
	GrantedByInternalUUID *string   `gorm:"type:varchar(36)"`
	CreatedAt             time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt            time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	SurveyResponse SurveyResponse `gorm:"foreignKey:SurveyResponseID"`
	User           *User          `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
	Group          *Group         `gorm:"foreignKey:GroupInternalUUID;references:InternalUUID"`
	GrantedBy      *User          `gorm:"foreignKey:GrantedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for SurveyResponseAccess
func (SurveyResponseAccess) TableName() string {
	return tableName("survey_response_access")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyResponseAccess) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
