// Package models defines GORM models for the TMI database schema.
// These models support both PostgreSQL and Oracle databases through GORM's
// dialect abstraction.
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User represents an authenticated user in the system
type User struct {
	InternalUUID   string     `gorm:"column:internal_uuid;primaryKey;type:varchar(36)"`
	Provider       string     `gorm:"column:provider;type:varchar(255);not null"`
	ProviderUserID *string    `gorm:"column:provider_user_id;type:varchar(255)"`
	Email          string     `gorm:"column:email;type:varchar(255);not null"`
	Name           string     `gorm:"column:name;type:varchar(255);not null"`
	EmailVerified  bool       `gorm:"column:email_verified;default:false"`
	AccessToken    *string    `gorm:"column:access_token;type:text"`
	RefreshToken   *string    `gorm:"column:refresh_token;type:text"`
	TokenExpiry    *time.Time `gorm:"column:token_expiry"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt     time.Time  `gorm:"column:modified_at;not null;autoUpdateTime"`
	LastLogin      *time.Time `gorm:"column:last_login"`
}

// TableName specifies the table name for User
func (User) TableName() string {
	return "users"
}

// BeforeCreate generates a UUID if not set
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.InternalUUID == "" {
		u.InternalUUID = uuid.New().String()
	}
	return nil
}

// RefreshTokenRecord represents a refresh token for a user
type RefreshTokenRecord struct {
	ID               string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	UserInternalUUID string    `gorm:"column:user_internal_uuid;type:varchar(36);not null;index"`
	Token            string    `gorm:"column:token;type:text;not null;uniqueIndex"`
	ExpiresAt        time.Time `gorm:"column:expires_at;not null"`
	CreatedAt        time.Time `gorm:"column:created_at;not null;autoCreateTime"`

	// Relationships
	User User `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for RefreshTokenRecord
func (RefreshTokenRecord) TableName() string {
	return "refresh_tokens"
}

// BeforeCreate generates a UUID if not set
func (r *RefreshTokenRecord) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

// ClientCredential represents OAuth 2.0 client credentials for machine-to-machine auth
type ClientCredential struct {
	ID               string     `gorm:"column:id;primaryKey;type:varchar(36)"`
	OwnerUUID        string     `gorm:"column:owner_uuid;type:varchar(36);not null;index"`
	ClientID         string     `gorm:"column:client_id;type:varchar(255);not null;uniqueIndex"`
	ClientSecretHash string     `gorm:"column:client_secret_hash;type:text;not null"`
	Name             string     `gorm:"column:name;type:varchar(255);not null"`
	Description      *string    `gorm:"column:description;type:text"`
	IsActive         bool       `gorm:"column:is_active;not null;default:true"`
	LastUsedAt       *time.Time `gorm:"column:last_used_at"`
	CreatedAt        time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt       time.Time  `gorm:"column:modified_at;not null;autoUpdateTime"`
	ExpiresAt        *time.Time `gorm:"column:expires_at"`

	// Relationships
	Owner User `gorm:"foreignKey:OwnerUUID;references:InternalUUID"`
}

// TableName specifies the table name for ClientCredential
func (ClientCredential) TableName() string {
	return "client_credentials"
}

// BeforeCreate generates a UUID if not set
func (c *ClientCredential) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// ThreatModel represents a threat model in the system
type ThreatModel struct {
	ID                    string     `gorm:"column:id;primaryKey;type:varchar(36)"`
	OwnerInternalUUID     string     `gorm:"column:owner_internal_uuid;type:varchar(36);not null;index"`
	Name                  string     `gorm:"column:name;type:varchar(255);not null"`
	Description           *string    `gorm:"column:description;type:text"`
	CreatedByInternalUUID string     `gorm:"column:created_by_internal_uuid;type:varchar(36);not null"`
	ThreatModelFramework  string     `gorm:"column:threat_model_framework;type:varchar(50);not null;default:STRIDE"`
	IssueURI              *string    `gorm:"column:issue_uri;type:varchar(2048)"`
	Status                *string    `gorm:"column:status;type:varchar(128)"`
	StatusUpdated         *time.Time `gorm:"column:status_updated"`
	CreatedAt             time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt            time.Time  `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	Owner     User      `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
	CreatedBy User      `gorm:"foreignKey:CreatedByInternalUUID;references:InternalUUID"`
	Diagrams  []Diagram `gorm:"foreignKey:ThreatModelID"`
	Threats   []Threat  `gorm:"foreignKey:ThreatModelID"`
	Assets    []Asset   `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for ThreatModel
func (ThreatModel) TableName() string {
	return "threat_models"
}

// BeforeCreate generates a UUID if not set
func (t *ThreatModel) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

// Diagram represents a diagram within a threat model
type Diagram struct {
	ID                string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	ThreatModelID     string    `gorm:"column:threat_model_id;type:varchar(36);not null;index"`
	Name              string    `gorm:"column:name;type:varchar(255);not null"`
	Description       *string   `gorm:"column:description;type:text"`
	Type              *string   `gorm:"column:type;type:varchar(50)"`
	Content           *string   `gorm:"column:content;type:text"`
	Cells             JSONRaw   `gorm:"column:cells;type:json"`
	SVGImage          *string   `gorm:"column:svg_image;type:text"`
	ImageUpdateVector *int64    `gorm:"column:image_update_vector"`
	UpdateVector      int64     `gorm:"column:update_vector;not null;default:0"`
	CreatedAt         time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt        time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Diagram
func (Diagram) TableName() string {
	return "diagrams"
}

// BeforeCreate generates a UUID if not set
func (d *Diagram) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	return nil
}

// Asset represents an asset within a threat model
type Asset struct {
	ID             string      `gorm:"column:id;primaryKey;type:varchar(36)"`
	ThreatModelID  string      `gorm:"column:threat_model_id;type:varchar(36);not null;index"`
	Name           string      `gorm:"column:name;type:varchar(255);not null"`
	Description    *string     `gorm:"column:description;type:text"`
	Type           string      `gorm:"column:type;type:varchar(50);not null"`
	Criticality    *string     `gorm:"column:criticality;type:varchar(50)"`
	Classification StringArray `gorm:"column:classification;type:json"`
	Sensitivity    *string     `gorm:"column:sensitivity;type:varchar(50)"`
	CreatedAt      time.Time   `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt     time.Time   `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Asset
func (Asset) TableName() string {
	return "assets"
}

// BeforeCreate generates a UUID if not set
func (a *Asset) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// Threat represents a threat within a threat model
type Threat struct {
	ID            string      `gorm:"column:id;primaryKey;type:varchar(36)"`
	ThreatModelID string      `gorm:"column:threat_model_id;type:varchar(36);not null;index"`
	DiagramID     *string     `gorm:"column:diagram_id;type:varchar(36);index"`
	CellID        *string     `gorm:"column:cell_id;type:varchar(36)"`
	AssetID       *string     `gorm:"column:asset_id;type:varchar(36);index"`
	Name          string      `gorm:"column:name;type:varchar(255);not null"`
	Description   *string     `gorm:"column:description;type:text"`
	Severity      *string     `gorm:"column:severity;type:varchar(50)"`
	Likelihood    *string     `gorm:"column:likelihood;type:varchar(50)"`
	RiskLevel     *string     `gorm:"column:risk_level;type:varchar(50)"`
	Score         *float64    `gorm:"column:score;type:decimal(3,1)"`
	Priority      *string     `gorm:"column:priority;type:varchar(50);default:Medium"`
	Mitigated     bool        `gorm:"column:mitigated;default:false"`
	Status        *string     `gorm:"column:status;type:varchar(50);default:Active"`
	ThreatType    StringArray `gorm:"column:threat_type;type:json;not null"`
	Mitigation    *string     `gorm:"column:mitigation;type:text"`
	IssueURI      *string     `gorm:"column:issue_uri;type:varchar(2048)"`
	CreatedAt     time.Time   `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt    time.Time   `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	Diagram     *Diagram    `gorm:"foreignKey:DiagramID"`
	Asset       *Asset      `gorm:"foreignKey:AssetID"`
}

// TableName specifies the table name for Threat
func (Threat) TableName() string {
	return "threats"
}

// Group represents an identity provider group
type Group struct {
	InternalUUID string    `gorm:"column:internal_uuid;primaryKey;type:varchar(36)"`
	Provider     string    `gorm:"column:provider;type:varchar(255);not null"`
	GroupName    string    `gorm:"column:group_name;type:varchar(255);not null"`
	Name         *string   `gorm:"column:name;type:varchar(255)"`
	Description  *string   `gorm:"column:description;type:text"`
	FirstUsed    time.Time `gorm:"column:first_used;not null;autoCreateTime"`
	LastUsed     time.Time `gorm:"column:last_used;not null;autoUpdateTime"`
	UsageCount   int       `gorm:"column:usage_count;default:1"`
}

// TableName specifies the table name for Group
func (Group) TableName() string {
	return "groups"
}

// BeforeCreate generates a UUID if not set
func (g *Group) BeforeCreate(tx *gorm.DB) error {
	if g.InternalUUID == "" {
		g.InternalUUID = uuid.New().String()
	}
	return nil
}

// ThreatModelAccess represents access control for threat models
type ThreatModelAccess struct {
	ID                    string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	ThreatModelID         string    `gorm:"column:threat_model_id;type:varchar(36);not null;index"`
	UserInternalUUID      *string   `gorm:"column:user_internal_uuid;type:varchar(36);index"`
	GroupInternalUUID     *string   `gorm:"column:group_internal_uuid;type:varchar(36);index"`
	SubjectType           string    `gorm:"column:subject_type;type:varchar(10);not null"`
	Role                  string    `gorm:"column:role;type:varchar(20);not null"`
	GrantedByInternalUUID *string   `gorm:"column:granted_by_internal_uuid;type:varchar(36)"`
	CreatedAt             time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt            time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	User        *User       `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
	Group       *Group      `gorm:"foreignKey:GroupInternalUUID;references:InternalUUID"`
	GrantedBy   *User       `gorm:"foreignKey:GrantedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for ThreatModelAccess
func (ThreatModelAccess) TableName() string {
	return "threat_model_access"
}

// BeforeCreate generates a UUID if not set
func (t *ThreatModelAccess) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return nil
}

// Document represents a document attached to a threat model
type Document struct {
	ID            string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	ThreatModelID string    `gorm:"column:threat_model_id;type:varchar(36);not null;index"`
	Name          string    `gorm:"column:name;type:varchar(255);not null"`
	URI           string    `gorm:"column:uri;type:varchar(2048);not null"`
	Description   *string   `gorm:"column:description;type:text"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt    time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Document
func (Document) TableName() string {
	return "documents"
}

// BeforeCreate generates a UUID if not set
func (d *Document) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	return nil
}

// Note represents a note attached to a threat model
type Note struct {
	ID            string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	ThreatModelID string    `gorm:"column:threat_model_id;type:varchar(36);not null;index"`
	Name          string    `gorm:"column:name;type:varchar(255);not null"`
	Content       string    `gorm:"column:content;type:text;not null"`
	Description   *string   `gorm:"column:description;type:text"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt    time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Note
func (Note) TableName() string {
	return "notes"
}

// BeforeCreate generates a UUID if not set
func (n *Note) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	return nil
}

// Repository represents a repository attached to a threat model
type Repository struct {
	ID            string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	ThreatModelID string    `gorm:"column:threat_model_id;type:varchar(36);not null;index"`
	Name          *string   `gorm:"column:name;type:varchar(255)"`
	URI           string    `gorm:"column:uri;type:varchar(2048);not null"`
	Description   *string   `gorm:"column:description;type:text"`
	Type          *string   `gorm:"column:type;type:varchar(50)"`
	Parameters    JSONMap   `gorm:"column:parameters;type:json"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt    time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Repository
func (Repository) TableName() string {
	return "repositories"
}

// BeforeCreate generates a UUID if not set
func (r *Repository) BeforeCreate(tx *gorm.DB) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return nil
}

// Metadata represents key-value metadata for entities
type Metadata struct {
	ID         string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	EntityType string    `gorm:"column:entity_type;type:varchar(50);not null"`
	EntityID   string    `gorm:"column:entity_id;type:varchar(36);not null"`
	Key        string    `gorm:"column:key;type:varchar(128);not null"`
	Value      string    `gorm:"column:value;type:text;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`
}

// TableName specifies the table name for Metadata
func (Metadata) TableName() string {
	return "metadata"
}

// CollaborationSession represents a real-time collaboration session
type CollaborationSession struct {
	ID            string     `gorm:"column:id;primaryKey;type:varchar(36)"`
	ThreatModelID string     `gorm:"column:threat_model_id;type:varchar(36);not null;index"`
	DiagramID     string     `gorm:"column:diagram_id;type:varchar(36);not null;index"`
	WebsocketURL  string     `gorm:"column:websocket_url;type:varchar(2048);not null"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	ExpiresAt     *time.Time `gorm:"column:expires_at"`

	// Relationships
	ThreatModel  ThreatModel          `gorm:"foreignKey:ThreatModelID"`
	Diagram      Diagram              `gorm:"foreignKey:DiagramID"`
	Participants []SessionParticipant `gorm:"foreignKey:SessionID"`
}

// TableName specifies the table name for CollaborationSession
func (CollaborationSession) TableName() string {
	return "collaboration_sessions"
}

// BeforeCreate generates a UUID if not set
func (c *CollaborationSession) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return nil
}

// SessionParticipant represents a participant in a collaboration session
type SessionParticipant struct {
	ID               string     `gorm:"column:id;primaryKey;type:varchar(36)"`
	SessionID        string     `gorm:"column:session_id;type:varchar(36);not null;index"`
	UserInternalUUID string     `gorm:"column:user_internal_uuid;type:varchar(36);not null;index"`
	JoinedAt         time.Time  `gorm:"column:joined_at;not null;autoCreateTime"`
	LeftAt           *time.Time `gorm:"column:left_at"`

	// Relationships
	Session CollaborationSession `gorm:"foreignKey:SessionID"`
	User    User                 `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for SessionParticipant
func (SessionParticipant) TableName() string {
	return "session_participants"
}

// BeforeCreate generates a UUID if not set
func (s *SessionParticipant) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// WebhookSubscription represents a webhook subscription
type WebhookSubscription struct {
	ID                  string      `gorm:"column:id;primaryKey;type:varchar(36)"`
	OwnerInternalUUID   string      `gorm:"column:owner_internal_uuid;type:varchar(36);not null;index"`
	ThreatModelID       *string     `gorm:"column:threat_model_id;type:varchar(36);index"`
	Name                string      `gorm:"column:name;type:varchar(255);not null"`
	URL                 string      `gorm:"column:url;type:varchar(2048);not null"`
	Events              StringArray `gorm:"column:events;type:json;not null"`
	Secret              *string     `gorm:"column:secret;type:text"`
	Status              string      `gorm:"column:status;type:varchar(50);not null;default:pending_verification"`
	Challenge           *string     `gorm:"column:challenge;type:text"`
	ChallengesSent      int         `gorm:"column:challenges_sent;not null;default:0"`
	TimeoutCount        int         `gorm:"column:timeout_count;not null;default:0"`
	CreatedAt           time.Time   `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt          time.Time   `gorm:"column:modified_at;not null;autoUpdateTime"`
	LastSuccessfulUse   *time.Time  `gorm:"column:last_successful_use"`
	PublicationFailures int         `gorm:"column:publication_failures;not null;default:0"`

	// Relationships
	Owner       User         `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
	ThreatModel *ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for WebhookSubscription
func (WebhookSubscription) TableName() string {
	return "webhook_subscriptions"
}

// BeforeCreate generates a UUID if not set
func (w *WebhookSubscription) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

// WebhookDelivery represents a webhook delivery attempt
type WebhookDelivery struct {
	ID             string     `gorm:"column:id;primaryKey;type:varchar(36)"`
	SubscriptionID string     `gorm:"column:subscription_id;type:varchar(36);not null;index"`
	EventType      string     `gorm:"column:event_type;type:varchar(100);not null"`
	Payload        JSONRaw    `gorm:"column:payload;type:json;not null"`
	Status         string     `gorm:"column:status;type:varchar(20);not null;default:pending"`
	Attempts       int        `gorm:"column:attempts;not null;default:0"`
	NextRetryAt    *time.Time `gorm:"column:next_retry_at"`
	LastError      *string    `gorm:"column:last_error;type:text"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	DeliveredAt    *time.Time `gorm:"column:delivered_at"`

	// Relationships
	Subscription WebhookSubscription `gorm:"foreignKey:SubscriptionID"`
}

// TableName specifies the table name for WebhookDelivery
func (WebhookDelivery) TableName() string {
	return "webhook_deliveries"
}

// WebhookQuota represents per-user webhook quotas
type WebhookQuota struct {
	OwnerID                          string    `gorm:"column:owner_id;primaryKey;type:varchar(36)"`
	MaxSubscriptions                 int       `gorm:"column:max_subscriptions;not null;default:10"`
	MaxEventsPerMinute               int       `gorm:"column:max_events_per_minute;not null;default:12"`
	MaxSubscriptionRequestsPerMinute int       `gorm:"column:max_subscription_requests_per_minute;not null;default:10"`
	MaxSubscriptionRequestsPerDay    int       `gorm:"column:max_subscription_requests_per_day;not null;default:20"`
	CreatedAt                        time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt                       time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	Owner User `gorm:"foreignKey:OwnerID;references:InternalUUID"`
}

// TableName specifies the table name for WebhookQuota
func (WebhookQuota) TableName() string {
	return "webhook_quotas"
}

// WebhookURLDenyList represents URL patterns blocked for webhooks
type WebhookURLDenyList struct {
	ID          string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	Pattern     string    `gorm:"column:pattern;type:varchar(255);not null"`
	PatternType string    `gorm:"column:pattern_type;type:varchar(20);not null"`
	Description *string   `gorm:"column:description;type:text"`
	CreatedAt   time.Time `gorm:"column:created_at;not null;autoCreateTime"`
}

// TableName specifies the table name for WebhookURLDenyList
func (WebhookURLDenyList) TableName() string {
	return "webhook_url_deny_list"
}

// BeforeCreate generates a UUID if not set
func (w *WebhookURLDenyList) BeforeCreate(tx *gorm.DB) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	return nil
}

// Administrator represents an administrator (user or group)
type Administrator struct {
	ID                    string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	UserInternalUUID      *string   `gorm:"column:user_internal_uuid;type:varchar(36);index"`
	GroupInternalUUID     *string   `gorm:"column:group_internal_uuid;type:varchar(36);index"`
	SubjectType           string    `gorm:"column:subject_type;type:varchar(10);not null"`
	Provider              string    `gorm:"column:provider;type:varchar(255);not null"`
	GrantedAt             time.Time `gorm:"column:granted_at;not null;autoCreateTime"`
	GrantedByInternalUUID *string   `gorm:"column:granted_by_internal_uuid;type:varchar(36)"`
	Notes                 *string   `gorm:"column:notes;type:text"`

	// Relationships
	User      *User  `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
	Group     *Group `gorm:"foreignKey:GroupInternalUUID;references:InternalUUID"`
	GrantedBy *User  `gorm:"foreignKey:GrantedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for Administrator
func (Administrator) TableName() string {
	return "administrators"
}

// BeforeCreate generates a UUID if not set
func (a *Administrator) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// Addon represents an addon configuration
type Addon struct {
	ID            string      `gorm:"column:id;primaryKey;type:varchar(36)"`
	CreatedAt     time.Time   `gorm:"column:created_at;not null;autoCreateTime"`
	Name          string      `gorm:"column:name;type:varchar(255);not null"`
	WebhookID     string      `gorm:"column:webhook_id;type:varchar(36);not null;index"`
	Description   *string     `gorm:"column:description;type:text"`
	Icon          *string     `gorm:"column:icon;type:text"`
	Objects       StringArray `gorm:"column:objects;type:json"`
	ThreatModelID *string     `gorm:"column:threat_model_id;type:varchar(36);index"`

	// Relationships
	Webhook     WebhookSubscription `gorm:"foreignKey:WebhookID"`
	ThreatModel *ThreatModel        `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for Addon
func (Addon) TableName() string {
	return "addons"
}

// BeforeCreate generates a UUID if not set
func (a *Addon) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

// AddonInvocationQuota represents per-user addon invocation quotas
type AddonInvocationQuota struct {
	OwnerInternalUUID     string    `gorm:"column:owner_internal_uuid;primaryKey;type:varchar(36)"`
	MaxActiveInvocations  int       `gorm:"column:max_active_invocations;not null;default:1"`
	MaxInvocationsPerHour int       `gorm:"column:max_invocations_per_hour;not null;default:10"`
	CreatedAt             time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt            time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	Owner User `gorm:"foreignKey:OwnerInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for AddonInvocationQuota
func (AddonInvocationQuota) TableName() string {
	return "addon_invocation_quotas"
}

// UserAPIQuota represents per-user API rate limits
type UserAPIQuota struct {
	UserInternalUUID     string    `gorm:"column:user_internal_uuid;primaryKey;type:varchar(36)"`
	MaxRequestsPerMinute int       `gorm:"column:max_requests_per_minute;not null;default:100"`
	MaxRequestsPerHour   *int      `gorm:"column:max_requests_per_hour"`
	CreatedAt            time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	ModifiedAt           time.Time `gorm:"column:modified_at;not null;autoUpdateTime"`

	// Relationships
	User User `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for UserAPIQuota
func (UserAPIQuota) TableName() string {
	return "user_api_quotas"
}

// GroupMember represents a user's membership in a group
type GroupMember struct {
	ID                  string    `gorm:"column:id;primaryKey;type:varchar(36)"`
	GroupInternalUUID   string    `gorm:"column:group_internal_uuid;type:varchar(36);not null;index"`
	UserInternalUUID    string    `gorm:"column:user_internal_uuid;type:varchar(36);not null;index"`
	AddedByInternalUUID *string   `gorm:"column:added_by_internal_uuid;type:varchar(36)"`
	AddedAt             time.Time `gorm:"column:added_at;not null;autoCreateTime"`
	Notes               *string   `gorm:"column:notes;type:text"`

	// Relationships
	Group   Group `gorm:"foreignKey:GroupInternalUUID;references:InternalUUID"`
	User    User  `gorm:"foreignKey:UserInternalUUID;references:InternalUUID"`
	AddedBy *User `gorm:"foreignKey:AddedByInternalUUID;references:InternalUUID"`
}

// TableName specifies the table name for GroupMember
func (GroupMember) TableName() string {
	return "group_members"
}

// BeforeCreate generates a UUID if not set
func (g *GroupMember) BeforeCreate(tx *gorm.DB) error {
	if g.ID == "" {
		g.ID = uuid.New().String()
	}
	return nil
}

// AllModels returns all GORM models for migration
func AllModels() []interface{} {
	return []interface{}{
		&User{},
		&RefreshTokenRecord{},
		&ClientCredential{},
		&Group{},
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
		&Administrator{},
		&Addon{},
		&AddonInvocationQuota{},
		&UserAPIQuota{},
		&GroupMember{},
	}
}
