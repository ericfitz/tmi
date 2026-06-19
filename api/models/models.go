// Package models defines GORM models for the TMI database schema.
// These models support both PostgreSQL and Oracle databases through GORM's
// dialect abstraction.
package models

import (
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/validation"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UseUppercaseTableNames controls whether table names should be uppercase.
// Set to true for Oracle databases where unquoted identifiers are folded to uppercase.
// This must be set before any GORM operations occur.
var UseUppercaseTableNames = false

// tableName returns the table name, converting to uppercase if UseUppercaseTableNames is true.
// SEM@0953d9ec7f7a4717796566e1b4379a976404b07e: return the dialect-appropriate table name, uppercased for Oracle (pure)
func tableName(name string) string {
	if UseUppercaseTableNames {
		return strings.ToUpper(name)
	}
	return name
}

// User represents an authenticated user in the system
// Note: Column names are intentionally not specified to allow GORM's NamingStrategy
// to handle database-specific casing (lowercase for PostgreSQL, UPPERCASE for Oracle)
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for an authenticated user with OAuth identity and token fields
type User struct {
	InternalUUID   DBVarchar         `gorm:"primaryKey;not null;size:36"`
	Provider       DBVarchar         `gorm:"size:100;not null;index:idx_users_provider;index:idx_users_provider_lookup,priority:1"`
	ProviderUserID NullableDBVarchar `gorm:"size:500;index:idx_users_provider_lookup,priority:2"`
	Email          DBVarchar         `gorm:"size:320;not null;index:idx_users_email"`
	Name           DBVarchar         `gorm:"size:256;not null"`
	EmailVerified  DBBool            `gorm:"default:0"`
	AccessToken    NullableDBText    `gorm:""`
	RefreshToken   NullableDBText    `gorm:""`
	TokenExpiry    *time.Time
	CreatedAt      time.Time  `gorm:"not null;autoCreateTime"`
	ModifiedAt     time.Time  `gorm:"not null;autoUpdateTime"`
	LastLogin      *time.Time `gorm:"index:idx_users_last_login"`
	Automation     *bool      `gorm:"default:null"`
	// ExtractionConcurrencyOverride lets a trusted machine account run more
	// concurrent OOXML extractions than the operator default. NULL = use
	// default. Hard-capped at maxPerUserConcurrency (16) regardless of value.
	ExtractionConcurrencyOverride *int `gorm:""`
}

// TableName specifies the table name for User
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware table name for User (pure)
func (User) TableName() string {
	return tableName("users")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate a UUID primary key for User before insert if not already set
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.InternalUUID == "" {
		u.InternalUUID = DBVarchar(uuid.New().String())
	}
	return nil
}

// RefreshTokenRecord represents a refresh token for a user
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a persisted OAuth refresh token linked to a user
type RefreshTokenRecord struct {
	ID               DBVarchar `gorm:"primaryKey;not null;size:36"`
	UserInternalUUID DBVarchar `gorm:"size:36;not null;index"`
	Token            DBVarchar `gorm:"size:4000;not null;uniqueIndex"` // DBVarchar size:4000 for Oracle compatibility (CLOB cannot have unique index)
	ExpiresAt        time.Time `gorm:"not null"`
	CreatedAt        time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	User User `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for RefreshTokenRecord
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware table name for RefreshTokenRecord (pure)
func (RefreshTokenRecord) TableName() string {
	return tableName("refresh_tokens")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate a UUID primary key for RefreshTokenRecord before insert if not already set
func (r *RefreshTokenRecord) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ClientCredential represents OAuth 2.0 client credentials for machine-to-machine auth
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for an OAuth 2.0 client credential used in machine-to-machine auth
type ClientCredential struct {
	ID               DBVarchar      `gorm:"primaryKey;not null;size:36"`
	OwnerUUID        DBVarchar      `gorm:"size:36;not null;index"`
	ClientID         DBVarchar      `gorm:"size:1000;not null;uniqueIndex"`
	ClientSecretHash DBText         `gorm:"not null"`
	Name             DBVarchar      `gorm:"size:256;not null"`
	Description      NullableDBText `gorm:""`
	IsActive         DBBool         `gorm:"default:1"`
	LastUsedAt       *time.Time
	CreatedAt        time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt       time.Time `gorm:"not null;autoUpdateTime"`
	ExpiresAt        *time.Time

	// Relationships
	Owner User `gorm:"foreignKey:OwnerUUID;references:InternalUUID"`
}

// TableName specifies the table name for ClientCredential
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware table name for ClientCredential (pure)
func (ClientCredential) TableName() string {
	return tableName("client_credentials")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate a UUID primary key for ClientCredential before insert if not already set
func (c *ClientCredential) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ThreatModel represents a threat model in the system
// Note: Explicit column tags removed for Oracle compatibility (Oracle stores column names as UPPERCASE,
// and the Oracle GORM driver doesn't handle case-insensitive matching with explicit column tags)
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a threat model with ownership, status, versioning, and sub-resource relationships
type ThreatModel struct {
	ID                           DBVarchar         `gorm:"primaryKey;not null;size:36"`
	OwnerInternalUUID            DBVarchar         `gorm:"size:36;not null;index:idx_tm_owner;index:idx_tm_owner_created,priority:1"`
	Name                         DBVarchar         `gorm:"size:256;not null"`
	Description                  NullableDBText    `gorm:""`
	CreatedByInternalUUID        DBVarchar         `gorm:"size:36;not null;index:idx_tm_created_by"`
	ThreatModelFramework         DBVarchar         `gorm:"size:30;default:STRIDE;index:idx_tm_framework"`
	IssueURI                     NullableDBText    `gorm:""`
	Status                       DBVarchar         `gorm:"size:128;not null;default:'not_started';index:idx_tm_status"`
	StatusUpdated                time.Time         `gorm:"not null;default:CURRENT_TIMESTAMP;index:idx_tm_status_updated"`
	Alias                        int32             `gorm:"column:alias;not null;default:0;<-:create;uniqueIndex:uniq_threat_models_alias"` // Server-assigned globally-unique integer alias
	IsConfidential               DBBool            `gorm:"default:0"`                                                                      // Immutable after creation
	SecurityReviewerInternalUUID NullableDBVarchar `gorm:"size:36;index:idx_tm_security_reviewer"`
	ProjectID                    NullableDBVarchar `gorm:"size:36;index:idx_tm_project"`
	// Timestamp columns map to precision-6 TIMESTAMP WITH TIME ZONE on both
	// PostgreSQL and Oracle (the gorm-oracle dialector emits bare TIMESTAMP WITH
	// TIME ZONE = microsecond). The application truncates created_at/modified_at
	// to microseconds at generation (see api/store.go UpdateTimestamps and the
	// threat-model handlers) so in-memory values match what the DB persists and
	// conform to the OpenAPI timestamp schema (max 6 fractional digits). If these
	// columns are ever pinned to a different precision, revisit that truncation.
	CreatedAt      time.Time  `gorm:"not null;autoCreateTime;index:idx_tm_owner_created,priority:2"`
	ModifiedAt     time.Time  `gorm:"not null;autoUpdateTime"`
	DeletedAt      *time.Time `gorm:"index:idx_tm_deleted_at"`
	LastAccessedAt *time.Time `gorm:"index:idx_tm_last_accessed_at"`
	// Version is incremented on every successful update (T14 / #385).
	// Clients pass the expected value via If-Match (or version body field) on
	// PUT/PATCH; mismatches return 409 Conflict.
	Version int `gorm:"not null;default:1"`

	// Relationships
	Project          *ProjectRecord `gorm:"foreignKey:ProjectID"`
	Owner            User           `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
	CreatedBy        User           `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID;constraint:-"`
	SecurityReviewer *User          `gorm:"foreignKey:SecurityReviewerInternalUUID;references:InternalUUID"`
	Diagrams         []Diagram      `gorm:"foreignKey:ThreatModelID"`
	Threats          []Threat       `gorm:"foreignKey:ThreatModelID"`
	Assets           []Asset        `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for ThreatModel
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware table name for ThreatModel (pure)
func (ThreatModel) TableName() string {
	return tableName("threat_models")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate a UUID primary key for ThreatModel before insert if not already set
func (t *ThreatModel) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// Diagram represents a diagram within a threat model
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a diagram within a threat model, storing cells, SVG, and version state
type Diagram struct {
	ID                DBVarchar         `gorm:"primaryKey;not null;size:36"`
	ThreatModelID     DBVarchar         `gorm:"size:36;not null;index:idx_diagrams_tm;index:idx_diagrams_tm_type,priority:1;uniqueIndex:uniq_diagrams_tm_alias,priority:1"`
	Name              DBVarchar         `gorm:"size:256;not null"`
	Description       NullableDBText    `gorm:""`
	Type              NullableDBVarchar `gorm:"size:64;index:idx_diagrams_type;index:idx_diagrams_tm_type,priority:2"`
	Content           NullableDBText    `gorm:""`
	Cells             JSONRaw           `gorm:""`
	ColorPalette      JSONRaw           `gorm:""`
	SVGImage          NullableDBText    `gorm:""`
	ImageUpdateVector *int64
	UpdateVector      int64      `gorm:"default:0"`
	IncludeInReport   DBBool     `gorm:"default:1"`
	TimmyEnabled      DBBool     `gorm:"default:1"`
	AutoGenerated     DBBool     `gorm:"default:0;<-:create" json:"auto_generated"`
	Alias             int32      `gorm:"column:alias;not null;default:0;<-:create;uniqueIndex:uniq_diagrams_tm_alias,priority:2"` // Server-assigned per-(threat_model_id, type) alias
	CreatedAt         time.Time  `gorm:"not null;autoCreateTime"`
	ModifiedAt        time.Time  `gorm:"not null;autoUpdateTime"`
	DeletedAt         *time.Time `gorm:"index:idx_diagrams_deleted_at"`
	// Version is incremented on every successful update (T14 / #385).
	Version int `gorm:"not null;default:1"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Diagram
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware table name for Diagram (pure)
func (Diagram) TableName() string {
	return tableName("diagrams")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate a UUID primary key for Diagram before insert if not already set
func (d *Diagram) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// Asset represents an asset within a threat model
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for an asset within a threat model with type, criticality, and classification
type Asset struct {
	ID              DBVarchar         `gorm:"primaryKey;not null;size:36"`
	ThreatModelID   DBVarchar         `gorm:"size:36;not null;index:idx_assets_tm;index:idx_assets_tm_created,priority:1;index:idx_assets_tm_modified,priority:1;uniqueIndex:uniq_assets_tm_alias,priority:1"`
	Name            DBVarchar         `gorm:"size:256;not null;index:idx_assets_name"`
	Description     NullableDBText    `gorm:""`
	Type            DBVarchar         `gorm:"size:64;not null;index:idx_assets_type"`
	Criticality     NullableDBVarchar `gorm:"size:128"`
	Classification  StringArray       `gorm:""`
	Sensitivity     NullableDBVarchar `gorm:"size:128"`
	IncludeInReport DBBool            `gorm:"default:1"`
	TimmyEnabled    DBBool            `gorm:"default:1"`
	Alias           int32             `gorm:"column:alias;not null;default:0;<-:create;uniqueIndex:uniq_assets_tm_alias,priority:2"` // Server-assigned per-(threat_model_id, type) alias
	CreatedAt       time.Time         `gorm:"not null;autoCreateTime;index:idx_assets_created;index:idx_assets_tm_created,priority:2"`
	ModifiedAt      time.Time         `gorm:"not null;autoUpdateTime;index:idx_assets_modified;index:idx_assets_tm_modified,priority:2"`
	DeletedAt       *time.Time        `gorm:"index:idx_assets_deleted_at"`
	// Version is incremented on every successful update (T14 / #385).
	Version int `gorm:"not null;default:1"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Asset
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware table name for Asset (pure)
func (Asset) TableName() string {
	return tableName("assets")
}

// BeforeCreate generates a UUID if not set and validates required fields.
// Validation is in BeforeCreate (not BeforeSave) because GORM map-based updates
// trigger BeforeSave on the empty model struct, causing false validation errors.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate a UUID, validate name and type for Asset before insert
func (a *Asset) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = DBVarchar(uuid.New().String())
	}
	if err := validation.ValidateNonEmpty("name", string(a.Name)); err != nil {
		return err
	}
	if err := validation.ValidateAssetType(string(a.Type)); err != nil {
		return err
	}
	return nil
}

// Threat represents a threat within a threat model
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a threat entry with severity, CVSS, SSVC, mitigation, and status fields
type Threat struct {
	ID              DBVarchar         `gorm:"primaryKey;not null;size:36"`
	ThreatModelID   DBVarchar         `gorm:"size:36;not null;index:idx_threats_tm;index:idx_threats_tm_created,priority:1;index:idx_threats_tm_modified,priority:1;uniqueIndex:uniq_threats_tm_alias,priority:1"`
	DiagramID       NullableDBVarchar `gorm:"size:36;index:idx_threats_diagram"`
	CellID          NullableDBVarchar `gorm:"size:36;index:idx_threats_cell"`
	AssetID         NullableDBVarchar `gorm:"size:36;index:idx_threats_asset"`
	Name            DBVarchar         `gorm:"size:256;not null;index:idx_threats_name"`
	Description     NullableDBText    `gorm:""`
	Severity        NullableDBVarchar `gorm:"size:50;index:idx_threats_severity"`
	Likelihood      NullableDBVarchar `gorm:"size:50"`
	RiskLevel       NullableDBVarchar `gorm:"size:50;index:idx_threats_risk_level"`
	Score           *float64          `gorm:"type:decimal(3,1);index:idx_threats_score"`
	Priority        NullableDBVarchar `gorm:"size:256;index:idx_threats_priority"`
	Mitigated       DBBool            `gorm:"index:idx_threats_mitigated"`
	IncludeInReport DBBool            `gorm:"default:1"`
	TimmyEnabled    DBBool            `gorm:"default:1"`
	AutoGenerated   DBBool            `gorm:"default:0;<-:create" json:"auto_generated"`
	Status          NullableDBVarchar `gorm:"size:128;index:idx_threats_status"`
	ThreatType      StringArray       `gorm:"not null"`
	CweID           StringArray       `gorm:"column:cwe_id"` // CWE identifiers (e.g., CWE-89)
	Cvss            CVSSArray         `gorm:"column:cvss"`   // CVSS vector and score pairs
	Ssvc            NullableSSVC      `gorm:"column:ssvc"`   // SSVC assessment result
	Mitigation      NullableDBText    `gorm:""`
	IssueURI        NullableDBText    `gorm:""`
	Alias           int32             `gorm:"column:alias;not null;default:0;<-:create;uniqueIndex:uniq_threats_tm_alias,priority:2"` // Server-assigned per-(threat_model_id, type) alias
	// Note: autoCreateTime/autoUpdateTime tags removed for Oracle compatibility.
	// Timestamps are set explicitly in the store layer (toGormModelForCreate).
	CreatedAt  time.Time  `gorm:"not null;index:idx_threats_tm_created,priority:2"`
	ModifiedAt time.Time  `gorm:"not null;index:idx_threats_modified;index:idx_threats_tm_modified,priority:2"`
	DeletedAt  *time.Time `gorm:"index:idx_threats_deleted_at"`
	// Version is incremented on every successful update (T14 / #385).
	Version int `gorm:"not null;default:1"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	Diagram     *Diagram    `gorm:"foreignKey:DiagramID"`
	Asset       *Asset      `gorm:"foreignKey:AssetID"`
}

// TableName specifies the table name for Threat
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for Threat (pure)
func (Threat) TableName() string {
	return tableName("threats")
}

// BeforeCreate ensures the ID is set before insert
// This is required for Oracle compatibility where the driver may not
// properly handle IDs set after struct initialization
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to Threat if unset before DB insert (mutates shared state)
func (t *Threat) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// Group represents an identity provider group
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for an identity-provider group with usage tracking
type Group struct {
	InternalUUID DBVarchar         `gorm:"primaryKey;not null;size:36"`
	Provider     DBVarchar         `gorm:"size:100;not null;index:idx_groups_provider"`
	GroupName    DBVarchar         `gorm:"size:500;not null;index:idx_groups_group_name"`
	Name         NullableDBVarchar `gorm:"size:256"`
	Description  NullableDBText    `gorm:""`
	FirstUsed    time.Time         `gorm:"not null;autoCreateTime"`
	LastUsed     time.Time         `gorm:"not null;autoUpdateTime;index:idx_groups_last_used"`
	UsageCount   int               `gorm:"default:1"`
}

// TableName specifies the table name for Group
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for Group (pure)
func (Group) TableName() string {
	return tableName("groups")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to Group if unset before DB insert (mutates shared state)
func (g *Group) BeforeCreate(tx *gorm.DB) error {
	if g.InternalUUID == "" {
		g.InternalUUID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ThreatModelAccess represents access control for threat models
// Note: Explicit column tags removed for Oracle compatibility (Oracle stores column names as UPPERCASE,
// and the Oracle GORM driver doesn't handle case-insensitive matching with explicit column tags)
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for a role-based access-control entry on a threat model
type ThreatModelAccess struct {
	ID                    DBVarchar         `gorm:"primaryKey;not null;size:36"`
	ThreatModelID         DBVarchar         `gorm:"size:36;not null;index:idx_tma_tm;index:idx_tma_perf,priority:1"`
	UserInternalUUID      NullableDBVarchar `gorm:"size:36;index:idx_tma_user;index:idx_tma_perf,priority:3"`
	GroupInternalUUID     NullableDBVarchar `gorm:"size:36;index:idx_tma_group;index:idx_tma_perf,priority:4"`
	SubjectType           DBVarchar         `gorm:"size:10;not null;index:idx_tma_subject_type;index:idx_tma_perf,priority:2"`
	Role                  DBVarchar         `gorm:"size:6;not null;index:idx_tma_role"`
	GrantedByInternalUUID NullableDBVarchar `gorm:"size:36"`
	CreatedAt             time.Time         `gorm:"not null;autoCreateTime"`
	ModifiedAt            time.Time         `gorm:"not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	User        *User       `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
	Group       *Group      `gorm:"foreignKey:GroupInternalUUID;references:InternalUUID"`
	GrantedBy   *User       `gorm:"foreignKey:GrantedByInternalUUID;references:InternalUUID;constraint:-"`
}

// TableName specifies the table name for ThreatModelAccess
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for ThreatModelAccess (pure)
func (ThreatModelAccess) TableName() string {
	return tableName("threat_model_access")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to ThreatModelAccess if unset before DB insert (mutates shared state)
func (t *ThreatModelAccess) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// Document represents a document attached to a threat model
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for a document attached to a threat model, with picker and access diagnostics
type Document struct {
	ID              DBVarchar         `gorm:"primaryKey;not null;size:36"`
	ThreatModelID   DBVarchar         `gorm:"size:36;not null;index:idx_docs_tm;index:idx_docs_tm_created,priority:1;index:idx_docs_tm_modified,priority:1;uniqueIndex:uniq_documents_tm_alias,priority:1"`
	Name            DBVarchar         `gorm:"size:256;not null;index:idx_docs_name"`
	URI             DBText            `gorm:"not null"`
	Description     NullableDBText    `gorm:""`
	IncludeInReport DBBool            `gorm:"default:1"`
	TimmyEnabled    DBBool            `gorm:"default:1"`
	AccessStatus    NullableDBVarchar `gorm:"size:32;default:unknown"`
	ContentSource   NullableDBVarchar `gorm:"size:64"`

	// Picker registration (all three set together or all null — enforced by application code).
	PickerProviderID NullableDBVarchar `gorm:"size:64;index:idx_docs_picker,priority:1"`
	PickerFileID     NullableDBVarchar `gorm:"size:255;index:idx_docs_picker,priority:2"`
	PickerMimeType   NullableDBVarchar `gorm:"size:128"`

	// Access diagnostics (populated when access_status != accessible/unknown).
	AccessReasonCode      NullableDBVarchar `gorm:"size:64"`
	AccessReasonDetail    NullableDBText
	AccessStatusUpdatedAt *time.Time

	Alias      int32      `gorm:"column:alias;not null;default:0;<-:create;uniqueIndex:uniq_documents_tm_alias,priority:2"` // Server-assigned per-(threat_model_id, type) alias
	CreatedAt  time.Time  `gorm:"not null;autoCreateTime;index:idx_docs_created;index:idx_docs_tm_created,priority:2"`
	ModifiedAt time.Time  `gorm:"not null;autoUpdateTime;index:idx_docs_modified;index:idx_docs_tm_modified,priority:2"`
	DeletedAt  *time.Time `gorm:"index:idx_docs_deleted_at"`
	// Version is incremented on every successful update (T14 / #385).
	Version int `gorm:"not null;default:1"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Document
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for Document (pure)
func (Document) TableName() string {
	return tableName("documents")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to Document if unset before DB insert (mutates shared state)
func (d *Document) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// Note represents a note attached to a threat model
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for a text note attached to a threat model
type Note struct {
	ID              DBVarchar      `gorm:"primaryKey;not null;size:36"`
	ThreatModelID   DBVarchar      `gorm:"size:36;not null;index:idx_notes_tm;index:idx_notes_tm_created,priority:1;index:idx_notes_tm_modified,priority:1;uniqueIndex:uniq_notes_tm_alias,priority:1"`
	Name            DBVarchar      `gorm:"size:256;not null;index:idx_notes_name"`
	Content         DBText         `gorm:"not null"`
	Description     NullableDBText `gorm:""`
	IncludeInReport DBBool         `gorm:"default:1"`
	TimmyEnabled    DBBool         `gorm:"default:1"`
	AutoGenerated   DBBool         `gorm:"default:0;<-:create" json:"auto_generated"`
	Alias           int32          `gorm:"column:alias;not null;default:0;<-:create;uniqueIndex:uniq_notes_tm_alias,priority:2"` // Server-assigned per-(threat_model_id, type) alias
	CreatedAt       time.Time      `gorm:"not null;autoCreateTime;index:idx_notes_created;index:idx_notes_tm_created,priority:2"`
	ModifiedAt      time.Time      `gorm:"not null;autoUpdateTime;index:idx_notes_modified;index:idx_notes_tm_modified,priority:2"`
	DeletedAt       *time.Time     `gorm:"index:idx_notes_deleted_at"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Note
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for Note (pure)
func (Note) TableName() string {
	return tableName("notes")
}

// BeforeCreate generates a UUID if not set and validates required fields.
// Note: Required field validation is intentionally in BeforeCreate (not BeforeSave)
// because the Update path uses map-based GORM Updates() on an empty model struct.
// BeforeSave would validate the empty struct's zero-value fields, causing false
// "cannot be empty" errors. Update-time validation is handled by the API layer.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to Note and validate required fields before DB insert (mutates shared state)
func (n *Note) BeforeCreate(tx *gorm.DB) error {
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

// Repository represents a repository attached to a threat model
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for a source-code repository attached to a threat model
type Repository struct {
	ID              DBVarchar         `gorm:"primaryKey;not null;size:36"`
	ThreatModelID   DBVarchar         `gorm:"size:36;not null;index:idx_repos_tm;index:idx_repos_tm_created,priority:1;index:idx_repos_tm_modified,priority:1;uniqueIndex:uniq_repositories_tm_alias,priority:1"`
	Name            NullableDBVarchar `gorm:"size:256;index:idx_repos_name"`
	URI             DBText            `gorm:"not null"`
	Description     NullableDBText    `gorm:""`
	Type            NullableDBVarchar `gorm:"size:64;index:idx_repos_type"`
	Parameters      JSONMap           `gorm:""`
	IncludeInReport DBBool            `gorm:"default:1"`
	TimmyEnabled    DBBool            `gorm:"default:1"`
	Alias           int32             `gorm:"column:alias;not null;default:0;<-:create;uniqueIndex:uniq_repositories_tm_alias,priority:2"` // Server-assigned per-(threat_model_id, type) alias
	CreatedAt       time.Time         `gorm:"not null;autoCreateTime;index:idx_repos_created;index:idx_repos_tm_created,priority:2"`
	ModifiedAt      time.Time         `gorm:"not null;autoUpdateTime;index:idx_repos_modified;index:idx_repos_tm_modified,priority:2"`
	DeletedAt       *time.Time        `gorm:"index:idx_repos_deleted_at"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Repository
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for Repository (pure)
func (Repository) TableName() string {
	return tableName("repositories")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to Repository if unset before DB insert (mutates shared state)
func (r *Repository) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// Metadata represents key-value metadata for entities
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for arbitrary key-value metadata on any entity
type Metadata struct {
	ID         DBVarchar `gorm:"primaryKey;not null;size:36"`
	EntityType DBVarchar `gorm:"size:50;not null;index:idx_metadata_entity_type_id,priority:1;index:idx_metadata_unique,priority:1,unique;index:idx_metadata_entity_created,priority:1;index:idx_metadata_entity_modified,priority:1"`
	EntityID   DBVarchar `gorm:"size:36;not null;index:idx_metadata_entity_id;index:idx_metadata_entity_type_id,priority:2;index:idx_metadata_unique,priority:2;index:idx_metadata_key_value,priority:1"`
	Key        DBVarchar `gorm:"size:256;not null;index:idx_metadata_key;index:idx_metadata_unique,priority:3;index:idx_metadata_key_value,priority:2"`
	Value      DBVarchar `gorm:"size:1024;not null;index:idx_metadata_key_value,priority:3"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime;index:idx_metadata_created;index:idx_metadata_entity_created,priority:2"`
	ModifiedAt time.Time `gorm:"not null;autoUpdateTime;index:idx_metadata_modified;index:idx_metadata_entity_modified,priority:2"`
}

// TableName specifies the table name for Metadata
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for Metadata (pure)
func (Metadata) TableName() string {
	return tableName("metadata")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to Metadata if unset before DB insert (mutates shared state)
func (m *Metadata) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// CollaborationSession represents a real-time collaboration session
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for a real-time WebSocket collaboration session on a diagram
type CollaborationSession struct {
	ID            DBVarchar `gorm:"primaryKey;not null;size:36"`
	ThreatModelID DBVarchar `gorm:"size:36;not null;index"`
	DiagramID     DBVarchar `gorm:"size:36;not null;index"`
	WebsocketURL  DBText    `gorm:"not null"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime"`
	ExpiresAt     *time.Time

	// Relationships
	ThreatModel  ThreatModel          `gorm:"foreignKey:ThreatModelID"`
	Diagram      Diagram              `gorm:"foreignKey:DiagramID"`
	Participants []SessionParticipant `gorm:"foreignKey:SessionID"`
}

// TableName specifies the table name for CollaborationSession
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for CollaborationSession (pure)
func (CollaborationSession) TableName() string {
	return tableName("collaboration_sessions")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to CollaborationSession if unset before DB insert (mutates shared state)
func (c *CollaborationSession) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// SessionParticipant represents a participant in a collaboration session
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for a user participating in a collaboration session
type SessionParticipant struct {
	ID               DBVarchar `gorm:"primaryKey;not null;size:36"`
	SessionID        DBVarchar `gorm:"size:36;not null;index"`
	UserInternalUUID DBVarchar `gorm:"size:36;not null;index"`
	JoinedAt         time.Time `gorm:"not null;autoCreateTime"`
	LeftAt           *time.Time

	// Relationships
	Session CollaborationSession `gorm:"foreignKey:SessionID"`
	User    User                 `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for SessionParticipant
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for SessionParticipant (pure)
func (SessionParticipant) TableName() string {
	return tableName("session_participants")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to SessionParticipant if unset before DB insert (mutates shared state)
func (s *SessionParticipant) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// WebhookSubscription represents a webhook subscription
// Note: Explicit column tags removed for Oracle compatibility
// SEM@c0d6404284f25e45cfa9076be2c6375c2f93913e: DB model for a webhook subscription with delivery tracking and operator-pin support
type WebhookSubscription struct {
	ID                  DBVarchar         `gorm:"primaryKey;not null;size:36"`
	OwnerInternalUUID   DBVarchar         `gorm:"size:36;not null;index"`
	ThreatModelID       NullableDBVarchar `gorm:"size:36;index"`
	Name                DBVarchar         `gorm:"size:256;not null"`
	URL                 DBText            `gorm:"not null"`
	Events              StringArray       `gorm:"not null"`
	Secret              NullableDBVarchar `gorm:"size:128"`
	Status              DBVarchar         `gorm:"size:128;default:pending_verification"`
	Challenge           NullableDBVarchar `gorm:"size:1000"`
	ChallengesSent      int               `gorm:"default:0"`
	TimeoutCount        int               `gorm:"default:0"`
	CreatedAt           time.Time         `gorm:"not null;autoCreateTime"`
	ModifiedAt          time.Time         `gorm:"not null;autoUpdateTime"`
	LastSuccessfulUse   *time.Time
	PublicationFailures int `gorm:"default:0"`

	// OperatorPinned marks the subscription as materialized from operator
	// config (alerting block, #395). Pinned rows cannot be modified or
	// deleted through /admin/webhooks and their URL is redacted in reads.
	// DBBool is required for Oracle compatibility (NUMBER(1) column).
	OperatorPinned DBBool `gorm:"not null;default:false" json:"operator_pinned"`

	// Relationships
	Owner       User         `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
	ThreatModel *ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for WebhookSubscription
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for WebhookSubscription (pure)
func (WebhookSubscription) TableName() string {
	return tableName("webhook_subscriptions")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to WebhookSubscription if unset before DB insert (mutates shared state)
func (w *WebhookSubscription) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// WebhookQuota represents per-user webhook quotas
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for per-user webhook subscription and delivery rate limits
type WebhookQuota struct {
	OwnerID                          DBVarchar `gorm:"primaryKey;not null;size:36"`
	MaxSubscriptions                 int       `gorm:"default:10"`
	MaxEventsPerMinute               int       `gorm:"default:12"`
	MaxSubscriptionRequestsPerMinute int       `gorm:"default:10"`
	MaxSubscriptionRequestsPerDay    int       `gorm:"default:20"`
	CreatedAt                        time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt                       time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	Owner User `gorm:"foreignKey:OwnerID;references:InternalUUID"`
}

// TableName specifies the table name for WebhookQuota
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for WebhookQuota (pure)
func (WebhookQuota) TableName() string {
	return tableName("webhook_quotas")
}

// WebhookURLDenyList represents URL patterns blocked for webhooks
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for URL patterns that are blocked from webhook delivery targets
type WebhookURLDenyList struct {
	ID          DBVarchar      `gorm:"primaryKey;not null;size:36"`
	Pattern     DBVarchar      `gorm:"size:256;not null;uniqueIndex:idx_webhook_deny_pattern"`
	PatternType DBVarchar      `gorm:"size:64;not null"`
	Description NullableDBText `gorm:""`
	CreatedAt   time.Time      `gorm:"not null;autoCreateTime"`
}

// TableName specifies the table name for WebhookURLDenyList
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for WebhookURLDenyList (pure)
func (WebhookURLDenyList) TableName() string {
	return tableName("webhook_url_deny_list")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to WebhookURLDenyList entry if unset before DB insert (mutates shared state)
func (w *WebhookURLDenyList) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// Addon represents an addon configuration
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for an addon configuration bound to a webhook and optional threat model
type Addon struct {
	ID            DBVarchar         `gorm:"primaryKey;not null;size:36"`
	CreatedAt     time.Time         `gorm:"not null;autoCreateTime"`
	Name          DBVarchar         `gorm:"size:256;not null"`
	WebhookID     DBVarchar         `gorm:"size:36;not null;index"`
	Description   NullableDBText    `gorm:""`
	Icon          NullableDBVarchar `gorm:"size:60"`
	Objects       StringArray       `gorm:""`
	ThreatModelID NullableDBVarchar `gorm:"size:36;index"`
	Parameters    JSONRaw           `gorm:""`

	// Relationships
	Webhook     WebhookSubscription `gorm:"foreignKey:WebhookID"`
	ThreatModel *ThreatModel        `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Addon
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for Addon (pure)
func (Addon) TableName() string {
	return tableName("addons")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to Addon if unset before DB insert (mutates shared state)
func (a *Addon) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// AddonInvocationQuota represents per-user addon invocation quotas
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for per-user addon concurrency and hourly invocation limits
type AddonInvocationQuota struct {
	OwnerInternalUUID     DBVarchar `gorm:"primaryKey;not null;size:36"`
	MaxActiveInvocations  int       `gorm:"default:1"`
	MaxInvocationsPerHour int       `gorm:"default:10"`
	CreatedAt             time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt            time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	Owner User `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for AddonInvocationQuota
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for AddonInvocationQuota (pure)
func (AddonInvocationQuota) TableName() string {
	return tableName("addon_invocation_quotas")
}

// UserAPIQuota represents per-user API rate limits
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for per-user API request rate limits
type UserAPIQuota struct {
	UserInternalUUID     DBVarchar `gorm:"primaryKey;not null;size:36"`
	MaxRequestsPerMinute int       `gorm:"default:100"`
	MaxRequestsPerHour   *int
	CreatedAt            time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt           time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	User User `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for UserAPIQuota
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for UserAPIQuota (pure)
func (UserAPIQuota) TableName() string {
	return tableName("user_api_quotas")
}

// GroupMember represents a user's or group's membership in a group.
// Supports one level of group-in-group nesting: an external IdP group can be
// a member of a built-in group (e.g., Administrators), enabling all members of
// the external group to inherit the built-in group's privileges.
// Note: Explicit column tags removed for Oracle compatibility
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for a user or nested group membership within a group
type GroupMember struct {
	ID                      DBVarchar         `gorm:"primaryKey;not null;size:36"`
	GroupInternalUUID       DBVarchar         `gorm:"size:36;not null;index;uniqueIndex:idx_gm_group_user_type,priority:1"`
	UserInternalUUID        NullableDBVarchar `gorm:"size:36;index;uniqueIndex:idx_gm_group_user_type,priority:2"`
	MemberGroupInternalUUID NullableDBVarchar `gorm:"size:36;index"`
	SubjectType             DBVarchar         `gorm:"size:10;not null;default:user;uniqueIndex:idx_gm_group_user_type,priority:3"`
	AddedByInternalUUID     NullableDBVarchar `gorm:"size:36"`
	AddedAt                 time.Time         `gorm:"not null;autoCreateTime"`
	Notes                   NullableDBText    `gorm:""`

	// Relationships
	Group       Group  `gorm:"foreignKey:GroupInternalUUID;references:InternalUUID"`
	User        *User  `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
	MemberGroup *Group `gorm:"foreignKey:MemberGroupInternalUUID;references:InternalUUID"`
	AddedBy     *User  `gorm:"foreignKey:AddedByInternalUUID;references:InternalUUID;constraint:-"`
}

// TableName specifies the table name for GroupMember
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for GroupMember (pure)
func (GroupMember) TableName() string {
	return tableName("group_members")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to GroupMember if unset before DB insert (mutates shared state)
func (g *GroupMember) BeforeCreate(tx *gorm.DB) error {
	if g.ID == "" {
		g.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// UserPreference stores user preferences as JSON
// Preferences are keyed by client application identifier (e.g., "tmi-ux", "tmi-cli")
// Maximum total size: 1KB, maximum 20 client entries
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for per-user, per-client JSON preferences
type UserPreference struct {
	ID               DBVarchar `gorm:"primaryKey;not null;size:36"`
	UserInternalUUID DBVarchar `gorm:"size:36;not null;uniqueIndex"`
	Preferences      JSONRaw   `gorm:"not null"`
	CreatedAt        time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt       time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	User User `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for UserPreference
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for UserPreference (pure)
func (UserPreference) TableName() string {
	return tableName("user_preferences")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to UserPreference if unset before DB insert (mutates shared state)
func (u *UserPreference) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// UsabilityFeedback represents user feedback about UI usability.
// Issued via POST /usability_feedback by any authenticated user.
// Listed via GET /usability_feedback (admin only).
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for user-submitted UI usability feedback with client context
type UsabilityFeedback struct {
	ID            DBVarchar         `gorm:"primaryKey;not null;size:36"`
	Sentiment     DBVarchar         `gorm:"size:8;not null;index:idx_usability_feedback_sentiment"`
	Verbatim      NullableDBText    `gorm:""`
	Surface       DBVarchar         `gorm:"size:32;not null;index:idx_usability_feedback_surface"`
	ClientID      DBVarchar         `gorm:"column:client_id;size:32;not null"`
	ClientVersion NullableDBVarchar `gorm:"column:client_version;size:32"`
	ClientBuild   NullableDBVarchar `gorm:"column:client_build;size:12"`
	UserAgent     NullableDBVarchar `gorm:"column:user_agent;size:512"`
	UserAgentData JSONRaw           `gorm:"column:user_agent_data"`
	Viewport      NullableDBVarchar `gorm:"size:11"`
	Screenshot    NullableDBText    `gorm:"column:screenshot"`
	CreatedByUUID DBVarchar         `gorm:"column:created_by;size:36;not null;index:idx_usability_feedback_created_by"`
	// Note: autoCreateTime tag removed for Oracle compatibility (#380). The
	// repository sets CreatedAt explicitly in Create before INSERT, matching
	// the Threat model pattern (see api/models/models.go Threat.CreatedAt).
	CreatedAt time.Time `gorm:"not null;index:idx_usability_feedback_created_at"`

	// Relationships. constraint:- suppresses the DB-level FK so a user with
	// outstanding feedback rows can still be deleted without an integrity-
	// constraint error. Application-layer integrity (CreatedByUUID is always
	// set from the authenticated user) is sufficient.
	CreatedBy User `gorm:"foreignKey:CreatedByUUID;references:InternalUUID;constraint:-"`
}

// TableName returns the dialect-aware table name.
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for UsabilityFeedback (pure)
func (UsabilityFeedback) TableName() string {
	return tableName("usability_feedback")
}

// BeforeCreate generates a UUID if not set.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to UsabilityFeedback if unset before DB insert (mutates shared state)
func (u *UsabilityFeedback) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// ContentFeedback represents user feedback on AI/automation-generated artifacts
// (notes, diagrams, threats, threat-classification fields) within a threat model.
// Issued via POST /threat_models/{id}/feedback by reader+ on the parent TM.
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for user feedback on AI-generated threat-model content artifacts
type ContentFeedback struct {
	ID                     DBVarchar         `gorm:"primaryKey;not null;size:36"`
	ThreatModelID          DBVarchar         `gorm:"size:36;not null;index:idx_content_feedback_target,priority:1"`
	TargetType             DBVarchar         `gorm:"size:24;not null;index:idx_content_feedback_target,priority:2"`
	TargetID               DBVarchar         `gorm:"size:36;not null;index:idx_content_feedback_target,priority:3"`
	TargetField            NullableDBVarchar `gorm:"size:64"`
	Sentiment              DBVarchar         `gorm:"size:8;not null;index:idx_content_feedback_sentiment"`
	Verbatim               NullableDBText    `gorm:""`
	FalsePositiveReason    NullableDBVarchar `gorm:"column:false_positive_reason;size:32;index:idx_content_feedback_fp_reason"`
	FalsePositiveSubreason NullableDBVarchar `gorm:"column:false_positive_subreason;size:40"`
	ClientID               DBVarchar         `gorm:"column:client_id;size:32;not null"`
	ClientVersion          NullableDBVarchar `gorm:"column:client_version;size:32"`
	Screenshot             NullableDBText    `gorm:"column:screenshot"`
	CreatedByUUID          DBVarchar         `gorm:"column:created_by;size:36;not null"`
	// Note: autoCreateTime tag removed for Oracle compatibility (#380). The
	// repository sets CreatedAt explicitly in Create / CreateWithTargetCheck
	// before INSERT, matching the Threat model pattern.
	CreatedAt time.Time `gorm:"not null;index:idx_content_feedback_created_at"`

	// Relationships. ContentFeedback rows are cleaned up explicitly by
	// deleteThreatModelChildren (issue #378), matching every other TM child.
	// The CreatedBy FK is suppressed (constraint:-) so a user with outstanding
	// feedback can still be deleted; application-layer integrity is sufficient.
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	CreatedBy   User        `gorm:"foreignKey:CreatedByUUID;references:InternalUUID;constraint:-"`
}

// TableName returns the dialect-aware table name.
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for ContentFeedback (pure)
func (ContentFeedback) TableName() string {
	return tableName("content_feedback")
}

// BeforeCreate generates a UUID if not set.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to ContentFeedback if unset before DB insert (mutates shared state)
func (c *ContentFeedback) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// AliasCounter holds the next-alias value for a given (parent_id, object_type)
// scope. ThreatModel global counter uses parent_id="__global__"; sub-object
// counters use the parent threat-model UUID. Allocation is done via
// SELECT ... FOR UPDATE inside the calling repository's transaction.
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for the next sequential alias value scoped to a parent and object type
type AliasCounter struct {
	ParentID   DBVarchar `gorm:"primaryKey;not null;size:36;column:parent_id"`
	ObjectType DBVarchar `gorm:"primaryKey;not null;size:16;column:object_type"`
	NextAlias  int32     `gorm:"not null;default:1;column:next_alias"`
}

// TableName returns the dialect-aware table name.
// SEM@7d6e24510cb080b00d72322263caafc04d1d57dc: return the dialect-aware DB table name for AliasCounter (pure)
func (AliasCounter) TableName() string {
	return tableName("alias_counters")
}

// AllModels returns all GORM models for migration
// SEM@211793c39ea528b3d2da244f3504963c40584df7: list all GORM models in dependency order for schema migration (pure)
func AllModels() []any {
	return []any{
		// Base entities (no FK dependencies)
		&User{},
		&RefreshTokenRecord{},
		&ClientCredential{},
		&UserContentToken{},
		&Group{},
		// Teams and projects (before ThreatModel which has FK to ProjectRecord)
		&TeamRecord{},
		&TeamMemberRecord{},
		&TeamResponsiblePartyRecord{},
		&TeamRelationshipRecord{},
		&ProjectRecord{},
		&ProjectResponsiblePartyRecord{},
		&ProjectRelationshipRecord{},
		// Team and project notes (after teams/projects, before threat models)
		&TeamNoteRecord{},
		&ProjectNoteRecord{},
		// Threat models and related entities
		&ThreatModel{},
		&Diagram{},
		&Asset{},
		&Threat{},
		&ThreatModelAccess{},
		&Document{},
		&ExtractionJob{},
		&Note{},
		&Repository{},
		&Metadata{},
		// Alias counter (referenced by every entity that has an alias column)
		&AliasCounter{},
		// Feedback (top-level usability + TM-scoped content)
		&UsabilityFeedback{},
		&ContentFeedback{},
		&CollaborationSession{},
		&SessionParticipant{},
		&WebhookSubscription{},
		&WebhookQuota{},
		&WebhookURLDenyList{},
		&Addon{},
		&AddonInvocationQuota{},
		&UserAPIQuota{},
		&GroupMember{},
		&UserPreference{},
		&LinkedIdentity{},
		&SystemSetting{},
		&SurveyTemplate{},
		&SurveyTemplateVersion{},
		&SurveyResponse{},
		&SurveyResponseAccess{},
		&TriageNote{},
		&SurveyAnswer{},
		// Audit trail and versioning
		&AuditEntry{},
		&SystemAuditEntry{},
		&VersionSnapshot{},
		// Timmy AI assistant stores
		&TimmySession{},
		&TimmyMessage{},
		&TimmyEmbedding{},
		&TimmyUsage{},
	}
}
