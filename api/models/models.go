// Package models defines GORM models for the TMI database schema.
// These models support both PostgreSQL and Oracle databases through GORM's
// dialect abstraction.
package models

import (
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OracleBool is a custom boolean type that handles Oracle's SMALLINT representation
// of booleans. Oracle doesn't have a native boolean type, so GORM uses SMALLINT (0/1).
// The godror driver returns these as godror.Number, which doesn't automatically
// convert to Go bool. This type implements sql.Scanner and driver.Valuer to handle
// the conversion for both PostgreSQL (native bool) and Oracle (SMALLINT as Number).
type OracleBool bool

// Scan implements the sql.Scanner interface for OracleBool.
// It handles:
// - bool (PostgreSQL native boolean)
// - int64/int/int32 (numeric representation)
// - godror.Number (Oracle's numeric type, implements fmt.Stringer)
// - nil (NULL values)
func (b *OracleBool) Scan(value interface{}) error {
	if value == nil {
		*b = false
		return nil
	}

	switch v := value.(type) {
	case bool:
		*b = OracleBool(v)
	case int64:
		*b = v != 0
	case int:
		*b = v != 0
	case int32:
		*b = v != 0
	case float64:
		*b = v != 0
	default:
		// Handle godror.Number which implements fmt.Stringer
		if stringer, ok := value.(fmt.Stringer); ok {
			str := stringer.String()
			*b = str != "0" && str != ""
		} else {
			return fmt.Errorf("cannot scan type %T into OracleBool", value)
		}
	}
	return nil
}

// Value implements the driver.Valuer interface for OracleBool.
// It returns the boolean as a native Go bool for cross-database compatibility.
// PostgreSQL expects bool for boolean columns, and Oracle's godror driver
// can handle Go bool and convert it to NUMBER(1) appropriately.
func (b OracleBool) Value() (driver.Value, error) {
	return bool(b), nil
}

// Bool returns the underlying bool value.
func (b OracleBool) Bool() bool {
	return bool(b)
}

// User represents an authenticated user in the system
// Note: Column names are intentionally not specified to allow GORM's NamingStrategy
// to handle database-specific casing (lowercase for PostgreSQL, UPPERCASE for Oracle)
type User struct {
	InternalUUID   string     `gorm:"primaryKey;type:varchar(36)"`
	Provider       string     `gorm:"type:varchar(255);not null"`
	ProviderUserID *string    `gorm:"type:varchar(255)"`
	Email          string     `gorm:"type:varchar(255);not null"`
	Name           string     `gorm:"type:varchar(255);not null"`
	EmailVerified  OracleBool `gorm:"default:0"`
	AccessToken    *string    `gorm:"type:clob"`
	RefreshToken   *string    `gorm:"type:clob"`
	TokenExpiry    *time.Time
	CreatedAt      time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt     time.Time `gorm:"not null;autoUpdateTime"`
	LastLogin      *time.Time
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
// Note: Explicit column tags removed for Oracle compatibility
type ClientCredential struct {
	ID               string     `gorm:"primaryKey;type:varchar(36)"`
	OwnerUUID        string     `gorm:"type:varchar(36);not null;index"`
	ClientID         string     `gorm:"type:varchar(255);not null;uniqueIndex"`
	ClientSecretHash string     `gorm:"type:clob;not null"`
	Name             string     `gorm:"type:varchar(255);not null"`
	Description      *string    `gorm:"type:clob"`
	IsActive         OracleBool `gorm:"default:1"`
	LastUsedAt       *time.Time
	CreatedAt        time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt       time.Time `gorm:"not null;autoUpdateTime"`
	ExpiresAt        *time.Time

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
// Note: Explicit column tags removed for Oracle compatibility (Oracle stores column names as UPPERCASE,
// and the Oracle GORM driver doesn't handle case-insensitive matching with explicit column tags)
type ThreatModel struct {
	ID                    string  `gorm:"primaryKey;type:varchar(36)"`
	OwnerInternalUUID     string  `gorm:"type:varchar(36);not null;index"`
	Name                  string  `gorm:"type:varchar(255);not null"`
	Description           *string `gorm:"type:clob"`
	CreatedByInternalUUID string  `gorm:"type:varchar(36);not null"`
	ThreatModelFramework  string  `gorm:"type:varchar(50);default:STRIDE"`
	IssueURI              *string `gorm:"type:varchar(2048)"`
	Status                *string `gorm:"type:varchar(128)"`
	StatusUpdated         *time.Time
	CreatedAt             time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt            time.Time `gorm:"not null;autoUpdateTime"`

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
// Note: Explicit column tags removed for Oracle compatibility
type Diagram struct {
	ID                string  `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID     string  `gorm:"type:varchar(36);not null;index"`
	Name              string  `gorm:"type:varchar(255);not null"`
	Description       *string `gorm:"type:clob"`
	Type              *string `gorm:"type:varchar(50)"`
	Content           *string `gorm:"type:clob"`
	Cells             JSONRaw `gorm:"type:json"`
	SVGImage          *string `gorm:"type:clob"`
	ImageUpdateVector *int64
	UpdateVector      int64     `gorm:"default:0"`
	CreatedAt         time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt        time.Time `gorm:"not null;autoUpdateTime"`

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
// Note: Explicit column tags removed for Oracle compatibility
type Asset struct {
	ID             string      `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID  string      `gorm:"type:varchar(36);not null;index"`
	Name           string      `gorm:"type:varchar(255);not null"`
	Description    *string     `gorm:"type:clob"`
	Type           string      `gorm:"type:varchar(50);not null"`
	Criticality    *string     `gorm:"type:varchar(50)"`
	Classification StringArray `gorm:"type:json"`
	Sensitivity    *string     `gorm:"type:varchar(50)"`
	CreatedAt      time.Time   `gorm:"not null;autoCreateTime"`
	ModifiedAt     time.Time   `gorm:"not null;autoUpdateTime"`

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
// Note: Explicit column tags removed for Oracle compatibility
type Threat struct {
	ID            string   `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID string   `gorm:"type:varchar(36);not null;index"`
	DiagramID     *string  `gorm:"type:varchar(36);index"`
	CellID        *string  `gorm:"type:varchar(36)"`
	AssetID       *string  `gorm:"type:varchar(36);index"`
	Name          string   `gorm:"type:varchar(255);not null"`
	Description   *string  `gorm:"type:clob"`
	Severity      *string  `gorm:"type:varchar(50)"`
	Likelihood    *string  `gorm:"type:varchar(50)"`
	RiskLevel     *string  `gorm:"type:varchar(50)"`
	Score         *float64 `gorm:"type:decimal(3,1)"`
	Priority      *string  `gorm:"type:varchar(50)"`
	Mitigated     OracleBool
	Status        *string     `gorm:"type:varchar(50)"`
	ThreatType    StringArray `gorm:"type:clob;serializer:json;not null"`
	Mitigation    *string     `gorm:"type:clob"`
	IssueURI      *string     `gorm:"type:varchar(2048)"`
	// Note: autoCreateTime/autoUpdateTime tags removed for Oracle compatibility.
	// Timestamps are set explicitly in the store layer (toGormModelForCreate).
	CreatedAt  time.Time `gorm:"not null"`
	ModifiedAt time.Time `gorm:"not null"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	Diagram     *Diagram    `gorm:"foreignKey:DiagramID"`
	Asset       *Asset      `gorm:"foreignKey:AssetID"`
}

// TableName specifies the table name for Threat
func (Threat) TableName() string {
	return "threats"
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
	Provider     string    `gorm:"type:varchar(255);not null"`
	GroupName    string    `gorm:"type:varchar(255);not null"`
	Name         *string   `gorm:"type:varchar(255)"`
	Description  *string   `gorm:"type:clob"`
	FirstUsed    time.Time `gorm:"not null;autoCreateTime"`
	LastUsed     time.Time `gorm:"not null;autoUpdateTime"`
	UsageCount   int       `gorm:"default:1"`
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
// Note: Explicit column tags removed for Oracle compatibility (Oracle stores column names as UPPERCASE,
// and the Oracle GORM driver doesn't handle case-insensitive matching with explicit column tags)
type ThreatModelAccess struct {
	ID                    string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID         string    `gorm:"type:varchar(36);not null;index"`
	UserInternalUUID      *string   `gorm:"type:varchar(36);index"`
	GroupInternalUUID     *string   `gorm:"type:varchar(36);index"`
	SubjectType           string    `gorm:"type:varchar(10);not null"`
	Role                  string    `gorm:"type:varchar(20);not null"`
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
// Note: Explicit column tags removed for Oracle compatibility
type Document struct {
	ID            string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID string    `gorm:"type:varchar(36);not null;index"`
	Name          string    `gorm:"type:varchar(255);not null"`
	URI           string    `gorm:"type:varchar(2048);not null"`
	Description   *string   `gorm:"type:clob"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt    time.Time `gorm:"not null;autoUpdateTime"`

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
// Note: Explicit column tags removed for Oracle compatibility
type Note struct {
	ID            string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID string    `gorm:"type:varchar(36);not null;index"`
	Name          string    `gorm:"type:varchar(255);not null"`
	Content       string    `gorm:"type:clob;not null"`
	Description   *string   `gorm:"type:clob"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt    time.Time `gorm:"not null;autoUpdateTime"`

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
// Note: Explicit column tags removed for Oracle compatibility
type Repository struct {
	ID            string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID string    `gorm:"type:varchar(36);not null;index"`
	Name          *string   `gorm:"type:varchar(255)"`
	URI           string    `gorm:"type:varchar(2048);not null"`
	Description   *string   `gorm:"type:clob"`
	Type          *string   `gorm:"type:varchar(50)"`
	Parameters    JSONMap   `gorm:"type:json"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt    time.Time `gorm:"not null;autoUpdateTime"`

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
// Note: Explicit column tags removed for Oracle compatibility
type Metadata struct {
	ID         string    `gorm:"primaryKey;type:varchar(36)"`
	EntityType string    `gorm:"type:varchar(50);not null"`
	EntityID   string    `gorm:"type:varchar(36);not null"`
	Key        string    `gorm:"type:varchar(128);not null"`
	Value      string    `gorm:"type:clob;not null"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime"`
	ModifiedAt time.Time `gorm:"not null;autoUpdateTime"`
}

// TableName specifies the table name for Metadata
func (Metadata) TableName() string {
	return "metadata"
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
	WebsocketURL  string    `gorm:"type:varchar(2048);not null"`
	CreatedAt     time.Time `gorm:"not null;autoCreateTime"`
	ExpiresAt     *time.Time

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
// Note: Explicit column tags removed for Oracle compatibility
type WebhookSubscription struct {
	ID                  string      `gorm:"primaryKey;type:varchar(36)"`
	OwnerInternalUUID   string      `gorm:"type:varchar(36);not null;index"`
	ThreatModelID       *string     `gorm:"type:varchar(36);index"`
	Name                string      `gorm:"type:varchar(255);not null"`
	URL                 string      `gorm:"type:varchar(2048);not null"`
	Events              StringArray `gorm:"type:json;not null"`
	Secret              *string     `gorm:"type:clob"`
	Status              string      `gorm:"type:varchar(50);default:pending_verification"`
	Challenge           *string     `gorm:"type:clob"`
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
// Note: Explicit column tags removed for Oracle compatibility
type WebhookDelivery struct {
	ID             string  `gorm:"primaryKey;type:varchar(36)"`
	SubscriptionID string  `gorm:"type:varchar(36);not null;index"`
	EventType      string  `gorm:"type:varchar(100);not null"`
	Payload        JSONRaw `gorm:"type:json;not null"`
	Status         string  `gorm:"type:varchar(20);default:pending"`
	Attempts       int     `gorm:"default:0"`
	NextRetryAt    *time.Time
	LastError      *string   `gorm:"type:clob"`
	CreatedAt      time.Time `gorm:"not null;autoCreateTime"`
	DeliveredAt    *time.Time

	// Relationships
	Subscription WebhookSubscription `gorm:"foreignKey:SubscriptionID"`
}

// TableName specifies the table name for WebhookDelivery
func (WebhookDelivery) TableName() string {
	return "webhook_deliveries"
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
	return "webhook_quotas"
}

// WebhookURLDenyList represents URL patterns blocked for webhooks
// Note: Explicit column tags removed for Oracle compatibility
type WebhookURLDenyList struct {
	ID          string    `gorm:"primaryKey;type:varchar(36)"`
	Pattern     string    `gorm:"type:varchar(255);not null"`
	PatternType string    `gorm:"type:varchar(20);not null"`
	Description *string   `gorm:"type:clob"`
	CreatedAt   time.Time `gorm:"not null;autoCreateTime"`
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
// Note: Explicit column tags removed for Oracle compatibility
type Administrator struct {
	ID                    string    `gorm:"primaryKey;type:varchar(36)"`
	UserInternalUUID      *string   `gorm:"type:varchar(36);index"`
	GroupInternalUUID     *string   `gorm:"type:varchar(36);index"`
	SubjectType           string    `gorm:"type:varchar(10);not null"`
	Provider              string    `gorm:"type:varchar(255);not null"`
	GrantedAt             time.Time `gorm:"not null;autoCreateTime"`
	GrantedByInternalUUID *string   `gorm:"type:varchar(36)"`
	Notes                 *string   `gorm:"type:clob"`

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
// Note: Explicit column tags removed for Oracle compatibility
type Addon struct {
	ID            string      `gorm:"primaryKey;type:varchar(36)"`
	CreatedAt     time.Time   `gorm:"not null;autoCreateTime"`
	Name          string      `gorm:"type:varchar(255);not null"`
	WebhookID     string      `gorm:"type:varchar(36);not null;index"`
	Description   *string     `gorm:"type:clob"`
	Icon          *string     `gorm:"type:clob"`
	Objects       StringArray `gorm:"type:json"`
	ThreatModelID *string     `gorm:"type:varchar(36);index"`

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
	return "addon_invocation_quotas"
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
	return "user_api_quotas"
}

// GroupMember represents a user's membership in a group
// Note: Explicit column tags removed for Oracle compatibility
type GroupMember struct {
	ID                  string    `gorm:"primaryKey;type:varchar(36)"`
	GroupInternalUUID   string    `gorm:"type:varchar(36);not null;index"`
	UserInternalUUID    string    `gorm:"type:varchar(36);not null;index"`
	AddedByInternalUUID *string   `gorm:"type:varchar(36)"`
	AddedAt             time.Time `gorm:"not null;autoCreateTime"`
	Notes               *string   `gorm:"type:clob"`

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
