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
	ID                    DBVarchar `gorm:"primaryKey;size:36"`
	Name                  string    `gorm:"type:varchar(256);not null;index:idx_st_name;uniqueIndex:idx_st_name_version,priority:1"`
	Description           *string   `gorm:"type:varchar(2048)"`
	Version               DBVarchar `gorm:"size:64;not null;index:idx_st_version;uniqueIndex:idx_st_name_version,priority:2"`
	Status                DBVarchar `gorm:"size:20;not null;default:inactive;index:idx_st_status"`
	SurveyJSON            JSONRaw   `gorm:"column:survey_json"` // Complete SurveyJS JSON definition (opaque blob)
	Settings              JSONRaw   `gorm:""`                   // Template settings (allow_threat_model_linking, etc.)
	CreatedByInternalUUID DBVarchar `gorm:"size:36;not null;index:idx_st_created_by"`
	CreatedAt             time.Time `gorm:"not null;autoCreateTime;index:idx_st_created_at"`
	ModifiedAt            time.Time `gorm:"not null;autoUpdateTime"`
}

// TableName specifies the table name for SurveyTemplate
func (SurveyTemplate) TableName() string {
	return tableName("survey_templates")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyTemplate) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// SurveyTemplateVersion represents a versioned snapshot of a survey template definition
type SurveyTemplateVersion struct {
	ID                    DBVarchar `gorm:"primaryKey;size:36"`
	TemplateID            DBVarchar `gorm:"size:36;not null;index:idx_stv_template;uniqueIndex:idx_stv_template_version,priority:1"`
	Version               DBVarchar `gorm:"size:64;not null;uniqueIndex:idx_stv_template_version,priority:2"`
	SurveyJSON            JSONRaw   `gorm:"column:survey_json"`
	CreatedByInternalUUID DBVarchar `gorm:"size:36;not null"`
	CreatedAt             time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	Template SurveyTemplate `gorm:"foreignKey:TemplateID"`
}

// TableName specifies the table name for SurveyTemplateVersion
func (SurveyTemplateVersion) TableName() string {
	return tableName("survey_template_versions")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyTemplateVersion) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// SurveyResponse represents a user's response to a survey template
type SurveyResponse struct {
	ID                     DBVarchar         `gorm:"primaryKey;size:36"`
	TemplateID             DBVarchar         `gorm:"size:36;not null;index:idx_sr_template;index:idx_sr_template_status,priority:1"`
	TemplateVersion        DBVarchar         `gorm:"size:64;not null"` // Captured at creation, immutable
	Status                 DBVarchar         `gorm:"size:30;not null;default:draft;index:idx_sr_status;index:idx_sr_template_status,priority:2"`
	IsConfidential         DBBool            `gorm:"default:0"`          // If true, Security Reviewers group not auto-added
	Answers                JSONRaw           `gorm:""`                   // Question answers keyed by question name
	UIState                JSONRaw           `gorm:"column:ui_state"`    // Client-managed UI state for draft resumption
	SurveyJSON             JSONRaw           `gorm:"column:survey_json"` // Snapshot of template survey_json at creation
	LinkedThreatModelID    NullableDBVarchar `gorm:"size:36;index:idx_sr_linked_tm"`
	CreatedThreatModelID   NullableDBVarchar `gorm:"size:36;index:idx_sr_created_tm"`
	RevisionNotes          *string           `gorm:"type:varchar(4000)"` // Notes from reviewer when returning for revision (varchar(4000) for Oracle ADB-STANDARD compatibility)
	OwnerInternalUUID      NullableDBVarchar `gorm:"size:36;index:idx_sr_owner"`
	CreatedAt              time.Time         `gorm:"not null;autoCreateTime;index:idx_sr_created_at"`
	ModifiedAt             time.Time         `gorm:"not null;autoUpdateTime"`
	SubmittedAt            *time.Time        `gorm:"index:idx_sr_submitted_at"`
	ReviewedAt             *time.Time
	ReviewedByInternalUUID NullableDBVarchar `gorm:"size:36"`
	ProjectID              NullableDBVarchar `gorm:"size:36;index:idx_sr_project"`
	// Version is incremented on every successful update (T14 / #385).
	Version int `gorm:"not null;default:1"`

	// Relationships
	Template           SurveyTemplate `gorm:"foreignKey:TemplateID"`
	Owner              *User          `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
	ReviewedBy         *User          `gorm:"foreignKey:ReviewedByInternalUUID;references:InternalUUID;constraint:-"`
	LinkedThreatModel  *ThreatModel   `gorm:"foreignKey:LinkedThreatModelID"`
	CreatedThreatModel *ThreatModel   `gorm:"foreignKey:CreatedThreatModelID"`
	Project            *ProjectRecord `gorm:"foreignKey:ProjectID"`
}

// TableName specifies the table name for SurveyResponse
func (SurveyResponse) TableName() string {
	return tableName("survey_responses")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyResponse) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TriageNote represents a triage note attached to a survey response
// Uses a composite primary key (SurveyResponseID, ID) where ID is a
// per-response monotonically increasing integer.
type TriageNote struct {
	SurveyResponseID       DBVarchar         `gorm:"primaryKey;size:36;index:idx_tn_sr"`
	ID                     int               `gorm:"primaryKey;autoIncrement:false"`
	Name                   string            `gorm:"type:varchar(256);not null"`
	Content                DBText            `gorm:"not null"`
	CreatedByInternalUUID  NullableDBVarchar `gorm:"size:36"`
	ModifiedByInternalUUID NullableDBVarchar `gorm:"size:36"`
	CreatedAt              time.Time         `gorm:"not null;autoCreateTime;index:idx_tn_created"`
	ModifiedAt             time.Time         `gorm:"not null;autoUpdateTime"`

	// Relationships
	SurveyResponse SurveyResponse `gorm:"foreignKey:SurveyResponseID"`
	CreatedBy      *User          `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID;constraint:-"`
	ModifiedBy     *User          `gorm:"foreignKey:ModifiedByInternalUUID;references:InternalUUID;constraint:-"`
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
	ID                    DBVarchar         `gorm:"primaryKey;size:36"`
	SurveyResponseID      DBVarchar         `gorm:"size:36;not null;index:idx_sra_sr;index:idx_sra_perf,priority:1"`
	UserInternalUUID      NullableDBVarchar `gorm:"size:36;index:idx_sra_user;index:idx_sra_perf,priority:3"`
	GroupInternalUUID     NullableDBVarchar `gorm:"size:36;index:idx_sra_group;index:idx_sra_perf,priority:4"`
	SubjectType           DBVarchar         `gorm:"size:10;not null;index:idx_sra_subject_type;index:idx_sra_perf,priority:2"`
	Role                  DBVarchar         `gorm:"size:6;not null;index:idx_sra_role"`
	GrantedByInternalUUID NullableDBVarchar `gorm:"size:36"`
	CreatedAt             time.Time         `gorm:"not null;autoCreateTime"`
	ModifiedAt            time.Time         `gorm:"not null;autoUpdateTime"`

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
		s.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// SurveyAnswer represents an extracted answer from a survey response.
// Rows are fully replaced on every response save for consistency.
type SurveyAnswer struct {
	ID             DBVarchar         `gorm:"primaryKey;size:36"`
	ResponseID     DBVarchar         `gorm:"size:36;not null;index:idx_sa_response_id;index:idx_sa_response_mapping"`
	QuestionName   DBVarchar         `gorm:"size:256;not null"`
	QuestionType   DBVarchar         `gorm:"size:64;not null"`
	QuestionTitle  *string           `gorm:"type:varchar(1024)"`
	MapsToTmField  NullableDBVarchar `gorm:"size:128;index:idx_sa_response_mapping"`
	AnswerValue    JSONRaw           `gorm:""`
	ResponseStatus DBVarchar         `gorm:"size:30;not null"`
	CreatedAt      time.Time         `gorm:"not null;autoCreateTime"`

	// Relationships
	SurveyResponse SurveyResponse `gorm:"foreignKey:ResponseID"`
}

// TableName specifies the table name for SurveyAnswer
func (SurveyAnswer) TableName() string {
	return tableName("survey_answers")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyAnswer) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = DBVarchar(uuid.New().String())
	}
	return nil
}
