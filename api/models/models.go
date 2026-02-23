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
func tableName(name string) string {
	if UseUppercaseTableNames {
		return strings.ToUpper(name)
	}
	return name
}

// User represents an authenticated user in the system
// Note: Column names are intentionally not specified to allow GORM's NamingStrategy
// to handle database-specific casing (lowercase for PostgreSQL, UPPERCASE for Oracle)
type User struct {
	InternalUUID   string         `gorm:"primaryKey;type:varchar(36)"`
	Provider       string         `gorm:"type:varchar(100);not null;index:idx_users_provider;index:idx_users_provider_lookup,priority:1"`
	ProviderUserID *string        `gorm:"type:varchar(500);index:idx_users_provider_lookup,priority:2"`
	Email          string         `gorm:"type:varchar(320);not null;index:idx_users_email"`
	Name           string         `gorm:"type:varchar(256);not null"`
	EmailVerified  DBBool         `gorm:"default:0"`
	AccessToken    NullableDBText `gorm:""`
	RefreshToken   NullableDBText `gorm:""`
	TokenExpiry    *time.Time
	CreatedAt      time.Time  `gorm:"not null;autoCreateTime"`
	ModifiedAt     time.Time  `gorm:"not null;autoUpdateTime"`
	LastLogin      *time.Time `gorm:"index:idx_users_last_login"`
}

// TableName specifies the table name for User
func (User) TableName() string {
	return tableName("users")
}

// BeforeCreate generates a UUID if not set
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.InternalUUID == "" {
		u.InternalUUID = uuid.New().String()
	}
	return nil
}

// RefreshTokenRecord represents a refresh token for a user
// Note: Explicit column tags removed for Oracle compatibility
type RefreshTokenRecord struct {
	ID               string    `gorm:"primaryKey;type:varchar(36)"`
	UserInternalUUID string    `gorm:"type:varchar(36);not null;index"`
	Token            string    `gorm:"type:varchar(4000);not null;uniqueIndex"` // varchar(4000) for Oracle compatibility (CLOB cannot have unique index)
	ExpiresAt        time.Time `gorm:"not null"`
	CreatedAt        time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	User User `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for RefreshTokenRecord
func (RefreshTokenRecord) TableName() string {
	return tableName("refresh_tokens")
}

// BeforeCreate generates a UUID if not set
func (r *RefreshTokenRecord) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

// ClientCredential represents OAuth 2.0 client credentials for machine-to-machine auth
// Note: Explicit column tags removed for Oracle compatibility
type ClientCredential struct {
	ID               string  `gorm:"primaryKey;type:varchar(36)"`
	OwnerUUID        string  `gorm:"type:varchar(36);not null;index"`
	ClientID         string  `gorm:"type:varchar(1000);not null;uniqueIndex"`
	ClientSecretHash DBText  `gorm:"not null"`
	Name             string  `gorm:"type:varchar(256);not null"`
	Description      *string `gorm:"type:varchar(1024)"`
	IsActive         DBBool  `gorm:"default:1"`
	LastUsedAt       *time.Time
	CreatedAt        time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt       time.Time `gorm:"not null;autoUpdateTime"`
	ExpiresAt        *time.Time

	// Relationships
	Owner User `gorm:"foreignKey:OwnerUUID;references:InternalUUID"`
}

// TableName specifies the table name for ClientCredential
func (ClientCredential) TableName() string {
	return tableName("client_credentials")
}

// BeforeCreate generates a UUID if not set
func (c *ClientCredential) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// ThreatModel represents a threat model in the system
// Note: Explicit column tags removed for Oracle compatibility (Oracle stores column names as UPPERCASE,
// and the Oracle GORM driver doesn't handle case-insensitive matching with explicit column tags)
type ThreatModel struct {
	ID                           string      `gorm:"primaryKey;type:varchar(36)"`
	OwnerInternalUUID            string      `gorm:"type:varchar(36);not null;index:idx_tm_owner;index:idx_tm_owner_created,priority:1"`
	Name                         string      `gorm:"type:varchar(256);not null"`
	Description                  *string     `gorm:"type:varchar(1024)"`
	CreatedByInternalUUID        string      `gorm:"type:varchar(36);not null;index:idx_tm_created_by"`
	ThreatModelFramework         string      `gorm:"type:varchar(30);default:STRIDE;index:idx_tm_framework"`
	IssueURI                     *string     `gorm:"type:varchar(1000)"`
	Status                       *string     `gorm:"type:varchar(128);index:idx_tm_status"`
	StatusUpdated                *time.Time  `gorm:"index:idx_tm_status_updated"`
	Alias                        StringArray `gorm:"column:alias"` // Alternative names/identifiers
	IsConfidential               DBBool      `gorm:"default:0"`    // Immutable after creation
	SecurityReviewerInternalUUID *string     `gorm:"type:varchar(36);index:idx_tm_security_reviewer"`
	ProjectID                    *string     `gorm:"type:varchar(36);index:idx_tm_project"`
	CreatedAt                    time.Time   `gorm:"not null;autoCreateTime;index:idx_tm_owner_created,priority:2"`
	ModifiedAt                   time.Time   `gorm:"not null;autoUpdateTime"`

	// Relationships
	Project          *ProjectRecord `gorm:"foreignKey:ProjectID"`
	Owner            User           `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
	CreatedBy        User           `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID"`
	SecurityReviewer *User          `gorm:"foreignKey:SecurityReviewerInternalUUID;references:InternalUUID"`
	Diagrams         []Diagram      `gorm:"foreignKey:ThreatModelID"`
	Threats          []Threat       `gorm:"foreignKey:ThreatModelID"`
	Assets           []Asset        `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for ThreatModel
func (ThreatModel) TableName() string {
	return tableName("threat_models")
}

// BeforeCreate generates a UUID if not set
func (t *ThreatModel) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

// Diagram represents a diagram within a threat model
// Note: Explicit column tags removed for Oracle compatibility
type Diagram struct {
	ID                string         `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID     string         `gorm:"type:varchar(36);not null;index:idx_diagrams_tm;index:idx_diagrams_tm_type,priority:1"`
	Name              string         `gorm:"type:varchar(256);not null"`
	Description       *string        `gorm:"type:varchar(1024)"`
	Type              *string        `gorm:"type:varchar(64);index:idx_diagrams_type;index:idx_diagrams_tm_type,priority:2"`
	Content           NullableDBText `gorm:""`
	Cells             JSONRaw        `gorm:""`
	SVGImage          NullableDBText `gorm:""`
	ImageUpdateVector *int64
	UpdateVector      int64     `gorm:"default:0"`
	IncludeInReport   DBBool    `gorm:"default:1"`
	CreatedAt         time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt        time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Diagram
func (Diagram) TableName() string {
	return tableName("diagrams")
}

// BeforeCreate generates a UUID if not set
func (d *Diagram) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	return nil
}

// Asset represents an asset within a threat model
// Note: Explicit column tags removed for Oracle compatibility
type Asset struct {
	ID              string      `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID   string      `gorm:"type:varchar(36);not null;index:idx_assets_tm;index:idx_assets_tm_created,priority:1;index:idx_assets_tm_modified,priority:1"`
	Name            string      `gorm:"type:varchar(256);not null;index:idx_assets_name"`
	Description     *string     `gorm:"type:varchar(1024)"`
	Type            string      `gorm:"type:varchar(64);not null;index:idx_assets_type"`
	Criticality     *string     `gorm:"type:varchar(128)"`
	Classification  StringArray `gorm:""`
	Sensitivity     *string     `gorm:"type:varchar(128)"`
	IncludeInReport DBBool      `gorm:"default:1"`
	CreatedAt       time.Time   `gorm:"not null;autoCreateTime;index:idx_assets_created;index:idx_assets_tm_created,priority:2"`
	ModifiedAt      time.Time   `gorm:"not null;autoUpdateTime;index:idx_assets_modified;index:idx_assets_tm_modified,priority:2"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Asset
func (Asset) TableName() string {
	return tableName("assets")
}

// BeforeCreate generates a UUID if not set
func (a *Asset) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// Threat represents a threat within a threat model
// Note: Explicit column tags removed for Oracle compatibility
type Threat struct {
	ID              string      `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID   string      `gorm:"type:varchar(36);not null;index:idx_threats_tm;index:idx_threats_tm_created,priority:1;index:idx_threats_tm_modified,priority:1"`
	DiagramID       *string     `gorm:"type:varchar(36);index:idx_threats_diagram"`
	CellID          *string     `gorm:"type:varchar(36);index:idx_threats_cell"`
	AssetID         *string     `gorm:"type:varchar(36);index:idx_threats_asset"`
	Name            string      `gorm:"type:varchar(256);not null;index:idx_threats_name"`
	Description     *string     `gorm:"type:varchar(1024)"`
	Severity        *string     `gorm:"type:varchar(50);index:idx_threats_severity"`
	Likelihood      *string     `gorm:"type:varchar(50)"`
	RiskLevel       *string     `gorm:"type:varchar(50);index:idx_threats_risk_level"`
	Score           *float64    `gorm:"type:decimal(3,1);index:idx_threats_score"`
	Priority        *string     `gorm:"type:varchar(256);index:idx_threats_priority"`
	Mitigated       DBBool      `gorm:"index:idx_threats_mitigated"`
	IncludeInReport DBBool      `gorm:"default:1"`
	Status          *string     `gorm:"type:varchar(128);index:idx_threats_status"`
	ThreatType      StringArray `gorm:"not null"`
	CweID           StringArray `gorm:"column:cwe_id"` // CWE identifiers (e.g., CWE-89)
	Cvss            CVSSArray   `gorm:"column:cvss"`   // CVSS vector and score pairs
	Mitigation      *string     `gorm:"type:varchar(1024)"`
	IssueURI        *string     `gorm:"type:varchar(1000)"`
	// Note: autoCreateTime/autoUpdateTime tags removed for Oracle compatibility.
	// Timestamps are set explicitly in the store layer (toGormModelForCreate).
	CreatedAt  time.Time `gorm:"not null;index:idx_threats_tm_created,priority:2"`
	ModifiedAt time.Time `gorm:"not null;index:idx_threats_modified;index:idx_threats_tm_modified,priority:2"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	Diagram     *Diagram    `gorm:"foreignKey:DiagramID"`
	Asset       *Asset      `gorm:"foreignKey:AssetID"`
}

// TableName specifies the table name for Threat
func (Threat) TableName() string {
	return tableName("threats")
}

// BeforeCreate ensures the ID is set before insert
// This is required for Oracle compatibility where the driver may not
// properly handle IDs set after struct initialization
func (t *Threat) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

// Group represents an identity provider group
// Note: Explicit column tags removed for Oracle compatibility
type Group struct {
	InternalUUID string    `gorm:"primaryKey;type:varchar(36)"`
	Provider     string    `gorm:"type:varchar(100);not null;index:idx_groups_provider"`
	GroupName    string    `gorm:"type:varchar(500);not null;index:idx_groups_group_name"`
	Name         *string   `gorm:"type:varchar(256)"`
	Description  *string   `gorm:"type:varchar(1024)"`
	FirstUsed    time.Time `gorm:"not null;autoCreateTime"`
	LastUsed     time.Time `gorm:"not null;autoUpdateTime;index:idx_groups_last_used"`
	UsageCount   int       `gorm:"default:1"`
}

// TableName specifies the table name for Group
func (Group) TableName() string {
	return tableName("groups")
}

// BeforeCreate generates a UUID if not set
func (g *Group) BeforeCreate(tx *gorm.DB) error {
	if g.InternalUUID == "" {
		g.InternalUUID = uuid.New().String()
	}
	return nil
}

// ThreatModelAccess represents access control for threat models
// Note: Explicit column tags removed for Oracle compatibility (Oracle stores column names as UPPERCASE,
// and the Oracle GORM driver doesn't handle case-insensitive matching with explicit column tags)
type ThreatModelAccess struct {
	ID                    string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID         string    `gorm:"type:varchar(36);not null;index:idx_tma_tm;index:idx_tma_perf,priority:1"`
	UserInternalUUID      *string   `gorm:"type:varchar(36);index:idx_tma_user;index:idx_tma_perf,priority:3"`
	GroupInternalUUID     *string   `gorm:"type:varchar(36);index:idx_tma_group;index:idx_tma_perf,priority:4"`
	SubjectType           string    `gorm:"type:varchar(10);not null;index:idx_tma_subject_type;index:idx_tma_perf,priority:2"`
	Role                  string    `gorm:"type:varchar(6);not null;index:idx_tma_role"`
	GrantedByInternalUUID *string   `gorm:"type:varchar(36)"`
	CreatedAt             time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt            time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	User        *User       `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
	Group       *Group      `gorm:"foreignKey:GroupInternalUUID;references:InternalUUID"`
	GrantedBy   *User       `gorm:"foreignKey:GrantedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for ThreatModelAccess
func (ThreatModelAccess) TableName() string {
	return tableName("threat_model_access")
}

// BeforeCreate generates a UUID if not set
func (t *ThreatModelAccess) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

// Document represents a document attached to a threat model
// Note: Explicit column tags removed for Oracle compatibility
type Document struct {
	ID              string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID   string    `gorm:"type:varchar(36);not null;index:idx_docs_tm;index:idx_docs_tm_created,priority:1;index:idx_docs_tm_modified,priority:1"`
	Name            string    `gorm:"type:varchar(256);not null;index:idx_docs_name"`
	URI             string    `gorm:"type:varchar(1000);not null"`
	Description     *string   `gorm:"type:varchar(1024)"`
	IncludeInReport DBBool    `gorm:"default:1"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime;index:idx_docs_created;index:idx_docs_tm_created,priority:2"`
	ModifiedAt      time.Time `gorm:"not null;autoUpdateTime;index:idx_docs_modified;index:idx_docs_tm_modified,priority:2"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Document
func (Document) TableName() string {
	return tableName("documents")
}

// BeforeCreate generates a UUID if not set
func (d *Document) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	return nil
}

// Note represents a note attached to a threat model
// Note: Explicit column tags removed for Oracle compatibility
type Note struct {
	ID              string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID   string    `gorm:"type:varchar(36);not null;index:idx_notes_tm;index:idx_notes_tm_created,priority:1;index:idx_notes_tm_modified,priority:1"`
	Name            string    `gorm:"type:varchar(256);not null;index:idx_notes_name"`
	Content         DBText    `gorm:"not null"`
	Description     *string   `gorm:"type:varchar(1024)"`
	IncludeInReport DBBool    `gorm:"default:1"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime;index:idx_notes_created;index:idx_notes_tm_created,priority:2"`
	ModifiedAt      time.Time `gorm:"not null;autoUpdateTime;index:idx_notes_modified;index:idx_notes_tm_modified,priority:2"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Note
func (Note) TableName() string {
	return tableName("notes")
}

// BeforeCreate generates a UUID if not set and validates required fields.
// Note: Required field validation is intentionally in BeforeCreate (not BeforeSave)
// because the Update path uses map-based GORM Updates() on an empty model struct.
// BeforeSave would validate the empty struct's zero-value fields, causing false
// "cannot be empty" errors. Update-time validation is handled by the API layer.
func (n *Note) BeforeCreate(tx *gorm.DB) error {
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

// Repository represents a repository attached to a threat model
// Note: Explicit column tags removed for Oracle compatibility
type Repository struct {
	ID              string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID   string    `gorm:"type:varchar(36);not null;index:idx_repos_tm;index:idx_repos_tm_created,priority:1;index:idx_repos_tm_modified,priority:1"`
	Name            *string   `gorm:"type:varchar(256);index:idx_repos_name"`
	URI             string    `gorm:"type:varchar(1000);not null"`
	Description     *string   `gorm:"type:varchar(1024)"`
	Type            *string   `gorm:"type:varchar(64);index:idx_repos_type"`
	Parameters      JSONMap   `gorm:""`
	IncludeInReport DBBool    `gorm:"default:1"`
	CreatedAt       time.Time `gorm:"not null;autoCreateTime;index:idx_repos_created;index:idx_repos_tm_created,priority:2"`
	ModifiedAt      time.Time `gorm:"not null;autoUpdateTime;index:idx_repos_modified;index:idx_repos_tm_modified,priority:2"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Repository
func (Repository) TableName() string {
	return tableName("repositories")
}

// BeforeCreate generates a UUID if not set
func (r *Repository) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

// Metadata represents key-value metadata for entities
// Note: Explicit column tags removed for Oracle compatibility
type Metadata struct {
	ID         string    `gorm:"primaryKey;type:varchar(36)"`
	EntityType string    `gorm:"type:varchar(50);not null;index:idx_metadata_entity_type_id,priority:1;index:idx_metadata_unique,priority:1,unique;index:idx_metadata_entity_created,priority:1;index:idx_metadata_entity_modified,priority:1"`
	EntityID   string    `gorm:"type:varchar(36);not null;index:idx_metadata_entity_id;index:idx_metadata_entity_type_id,priority:2;index:idx_metadata_unique,priority:2;index:idx_metadata_key_value,priority:1"`
	Key        string    `gorm:"type:varchar(256);not null;index:idx_metadata_key;index:idx_metadata_unique,priority:3;index:idx_metadata_key_value,priority:2"`
	Value      string    `gorm:"type:varchar(1024);not null;index:idx_metadata_key_value,priority:3"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime;index:idx_metadata_created;index:idx_metadata_entity_created,priority:2"`
	ModifiedAt time.Time `gorm:"not null;autoUpdateTime;index:idx_metadata_modified;index:idx_metadata_entity_modified,priority:2"`
}

// TableName specifies the table name for Metadata
func (Metadata) TableName() string {
	return tableName("metadata")
}

// BeforeCreate generates a UUID if not set
func (m *Metadata) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// CollaborationSession represents a real-time collaboration session
// Note: Explicit column tags removed for Oracle compatibility
type CollaborationSession struct {
	ID            string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID string    `gorm:"type:varchar(36);not null;index"`
	DiagramID     string    `gorm:"type:varchar(36);not null;index"`
	WebsocketURL  string    `gorm:"type:varchar(1024);not null"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime"`
	ExpiresAt     *time.Time

	// Relationships
	ThreatModel  ThreatModel          `gorm:"foreignKey:ThreatModelID"`
	Diagram      Diagram              `gorm:"foreignKey:DiagramID"`
	Participants []SessionParticipant `gorm:"foreignKey:SessionID"`
}

// TableName specifies the table name for CollaborationSession
func (CollaborationSession) TableName() string {
	return tableName("collaboration_sessions")
}

// BeforeCreate generates a UUID if not set
func (c *CollaborationSession) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// SessionParticipant represents a participant in a collaboration session
// Note: Explicit column tags removed for Oracle compatibility
type SessionParticipant struct {
	ID               string    `gorm:"primaryKey;type:varchar(36)"`
	SessionID        string    `gorm:"type:varchar(36);not null;index"`
	UserInternalUUID string    `gorm:"type:varchar(36);not null;index"`
	JoinedAt         time.Time `gorm:"not null;autoCreateTime"`
	LeftAt           *time.Time

	// Relationships
	Session CollaborationSession `gorm:"foreignKey:SessionID"`
	User    User                 `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for SessionParticipant
func (SessionParticipant) TableName() string {
	return tableName("session_participants")
}

// BeforeCreate generates a UUID if not set
func (s *SessionParticipant) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// WebhookSubscription represents a webhook subscription
// Note: Explicit column tags removed for Oracle compatibility
type WebhookSubscription struct {
	ID                  string      `gorm:"primaryKey;type:varchar(36)"`
	OwnerInternalUUID   string      `gorm:"type:varchar(36);not null;index"`
	ThreatModelID       *string     `gorm:"type:varchar(36);index"`
	Name                string      `gorm:"type:varchar(256);not null"`
	URL                 string      `gorm:"type:varchar(1024);not null"`
	Events              StringArray `gorm:"not null"`
	Secret              *string     `gorm:"type:varchar(128)"` //nolint:gosec // G117 - webhook HMAC signing secret
	Status              string      `gorm:"type:varchar(128);default:pending_verification"`
	Challenge           *string     `gorm:"type:varchar(1000)"`
	ChallengesSent      int         `gorm:"default:0"`
	TimeoutCount        int         `gorm:"default:0"`
	CreatedAt           time.Time   `gorm:"not null;autoCreateTime"`
	ModifiedAt          time.Time   `gorm:"not null;autoUpdateTime"`
	LastSuccessfulUse   *time.Time
	PublicationFailures int `gorm:"default:0"`

	// Relationships
	Owner       User         `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
	ThreatModel *ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for WebhookSubscription
func (WebhookSubscription) TableName() string {
	return tableName("webhook_subscriptions")
}

// BeforeCreate generates a UUID if not set
func (w *WebhookSubscription) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

// WebhookDelivery represents a webhook delivery attempt
// Note: Explicit column tags removed for Oracle compatibility
type WebhookDelivery struct {
	ID             string  `gorm:"primaryKey;type:varchar(36)"`
	SubscriptionID string  `gorm:"type:varchar(36);not null;index"`
	EventType      string  `gorm:"type:varchar(1000);not null"`
	Payload        JSONRaw `gorm:"not null"`
	Status         string  `gorm:"type:varchar(128);default:pending"`
	Attempts       int     `gorm:"default:0"`
	NextRetryAt    *time.Time
	LastError      *string   `gorm:"type:varchar(1000)"`
	CreatedAt      time.Time `gorm:"not null;autoCreateTime"`
	DeliveredAt    *time.Time

	// Relationships
	Subscription WebhookSubscription `gorm:"foreignKey:SubscriptionID"`
}

// TableName specifies the table name for WebhookDelivery
func (WebhookDelivery) TableName() string {
	return tableName("webhook_deliveries")
}

// BeforeCreate generates a UUID if not set
func (w *WebhookDelivery) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

// WebhookQuota represents per-user webhook quotas
// Note: Explicit column tags removed for Oracle compatibility
type WebhookQuota struct {
	OwnerID                          string    `gorm:"primaryKey;type:varchar(36)"`
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
func (WebhookQuota) TableName() string {
	return tableName("webhook_quotas")
}

// WebhookURLDenyList represents URL patterns blocked for webhooks
// Note: Explicit column tags removed for Oracle compatibility
type WebhookURLDenyList struct {
	ID          string    `gorm:"primaryKey;type:varchar(36)"`
	Pattern     string    `gorm:"type:varchar(256);not null"`
	PatternType string    `gorm:"type:varchar(64);not null"`
	Description *string   `gorm:"type:varchar(1024)"`
	CreatedAt   time.Time `gorm:"not null;autoCreateTime"`
}

// TableName specifies the table name for WebhookURLDenyList
func (WebhookURLDenyList) TableName() string {
	return tableName("webhook_url_deny_list")
}

// BeforeCreate generates a UUID if not set
func (w *WebhookURLDenyList) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

// Addon represents an addon configuration
// Note: Explicit column tags removed for Oracle compatibility
type Addon struct {
	ID            string      `gorm:"primaryKey;type:varchar(36)"`
	CreatedAt     time.Time   `gorm:"not null;autoCreateTime"`
	Name          string      `gorm:"type:varchar(256);not null"`
	WebhookID     string      `gorm:"type:varchar(36);not null;index"`
	Description   *string     `gorm:"type:varchar(1024)"`
	Icon          *string     `gorm:"type:varchar(60)"`
	Objects       StringArray `gorm:""`
	ThreatModelID *string     `gorm:"type:varchar(36);index"`

	// Relationships
	Webhook     WebhookSubscription `gorm:"foreignKey:WebhookID"`
	ThreatModel *ThreatModel        `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Addon
func (Addon) TableName() string {
	return tableName("addons")
}

// BeforeCreate generates a UUID if not set
func (a *Addon) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// AddonInvocationQuota represents per-user addon invocation quotas
// Note: Explicit column tags removed for Oracle compatibility
type AddonInvocationQuota struct {
	OwnerInternalUUID     string    `gorm:"primaryKey;type:varchar(36)"`
	MaxActiveInvocations  int       `gorm:"default:1"`
	MaxInvocationsPerHour int       `gorm:"default:10"`
	CreatedAt             time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt            time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	Owner User `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for AddonInvocationQuota
func (AddonInvocationQuota) TableName() string {
	return tableName("addon_invocation_quotas")
}

// UserAPIQuota represents per-user API rate limits
// Note: Explicit column tags removed for Oracle compatibility
type UserAPIQuota struct {
	UserInternalUUID     string `gorm:"primaryKey;type:varchar(36)"`
	MaxRequestsPerMinute int    `gorm:"default:100"`
	MaxRequestsPerHour   *int
	CreatedAt            time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt           time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	User User `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for UserAPIQuota
func (UserAPIQuota) TableName() string {
	return tableName("user_api_quotas")
}

// GroupMember represents a user's or group's membership in a group.
// Supports one level of group-in-group nesting: an external IdP group can be
// a member of a built-in group (e.g., Administrators), enabling all members of
// the external group to inherit the built-in group's privileges.
// Note: Explicit column tags removed for Oracle compatibility
type GroupMember struct {
	ID                      string    `gorm:"primaryKey;type:varchar(36)"`
	GroupInternalUUID       string    `gorm:"type:varchar(36);not null;index;uniqueIndex:idx_gm_group_user_type,priority:1"`
	UserInternalUUID        *string   `gorm:"type:varchar(36);index;uniqueIndex:idx_gm_group_user_type,priority:2"`
	MemberGroupInternalUUID *string   `gorm:"type:varchar(36);index"`
	SubjectType             string    `gorm:"type:varchar(10);not null;default:user;uniqueIndex:idx_gm_group_user_type,priority:3"`
	AddedByInternalUUID     *string   `gorm:"type:varchar(36)"`
	AddedAt                 time.Time `gorm:"not null;autoCreateTime"`
	Notes                   *string   `gorm:"type:varchar(1000)"`

	// Relationships
	Group       Group  `gorm:"foreignKey:GroupInternalUUID;references:InternalUUID"`
	User        *User  `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
	MemberGroup *Group `gorm:"foreignKey:MemberGroupInternalUUID;references:InternalUUID"`
	AddedBy     *User  `gorm:"foreignKey:AddedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for GroupMember
func (GroupMember) TableName() string {
	return tableName("group_members")
}

// BeforeCreate generates a UUID if not set
func (g *GroupMember) BeforeCreate(tx *gorm.DB) error {
	if g.ID == "" {
		g.ID = uuid.New().String()
	}
	return nil
}

// UserPreference stores user preferences as JSON
// Preferences are keyed by client application identifier (e.g., "tmi-ux", "tmi-cli")
// Maximum total size: 1KB, maximum 20 client entries
type UserPreference struct {
	ID               string    `gorm:"primaryKey;type:varchar(36)"`
	UserInternalUUID string    `gorm:"type:varchar(36);not null;uniqueIndex"`
	Preferences      JSONRaw   `gorm:"not null"`
	CreatedAt        time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt       time.Time `gorm:"not null;autoUpdateTime"`

	// Relationships
	User User `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for UserPreference
func (UserPreference) TableName() string {
	return tableName("user_preferences")
}

// BeforeCreate generates a UUID if not set
func (u *UserPreference) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}

// AllModels returns all GORM models for migration
func AllModels() []any {
	return []any{
		// Base entities (no FK dependencies)
		&User{},
		&RefreshTokenRecord{},
		&ClientCredential{},
		&Group{},
		// Teams and projects (before ThreatModel which has FK to ProjectRecord)
		&TeamRecord{},
		&TeamMemberRecord{},
		&TeamResponsiblePartyRecord{},
		&TeamRelationshipRecord{},
		&ProjectRecord{},
		&ProjectResponsiblePartyRecord{},
		&ProjectRelationshipRecord{},
		// Threat models and related entities
		&ThreatModel{},
		&Diagram{},
		&Asset{},
		&Threat{},
		&ThreatModelAccess{},
		&Document{},
		&Note{},
		&Repository{},
		&Metadata{},
		&CollaborationSession{},
		&SessionParticipant{},
		&WebhookSubscription{},
		&WebhookDelivery{},
		&WebhookQuota{},
		&WebhookURLDenyList{},
		&Addon{},
		&AddonInvocationQuota{},
		&UserAPIQuota{},
		&GroupMember{},
		&UserPreference{},
		&SystemSetting{},
		&SurveyTemplate{},
		&SurveyTemplateVersion{},
		&SurveyResponse{},
		&SurveyResponseAccess{},
		&TriageNote{},
	}
}
