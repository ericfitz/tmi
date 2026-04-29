package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TimmySession represents a chat session between a user and Timmy for a threat model
type TimmySession struct {
	ID               string     `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID    string     `gorm:"type:varchar(36);not null;index:idx_timmy_sessions_tm"`
	UserID           string     `gorm:"type:varchar(36);not null;index:idx_timmy_sessions_user;index:idx_timmy_sessions_tm_user,priority:2"`
	Title            string     `gorm:"type:varchar(256)"`
	SourceSnapshot   JSONRaw    `gorm:""`
	SystemPromptHash string     `gorm:"type:varchar(64)"`
	Status           string     `gorm:"type:varchar(20);not null;default:active;index:idx_timmy_sessions_status"`
	CreatedAt        time.Time  `gorm:"not null;autoCreateTime"`
	ModifiedAt       time.Time  `gorm:"not null;autoUpdateTime"`
	DeletedAt        *time.Time `gorm:"index:idx_timmy_sessions_deleted_at"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	User        User        `gorm:"foreignKey:UserID;references:InternalUUID"`
}

// TableName specifies the table name for TimmySession
func (TimmySession) TableName() string {
	return tableName("timmy_sessions")
}

// BeforeCreate generates a UUID and sets default status if not set
func (s *TimmySession) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	if s.Status == "" {
		s.Status = "active"
	}
	return nil
}

// TimmyMessage represents a single message in a Timmy chat session
type TimmyMessage struct {
	ID         string    `gorm:"primaryKey;type:varchar(36)"`
	SessionID  string    `gorm:"type:varchar(36);not null;index:idx_timmy_messages_session;uniqueIndex:idx_timmy_messages_session_seq,priority:1"`
	Role       string    `gorm:"type:varchar(20);not null"`
	Content    DBText    `gorm:"not null"`
	TokenCount int       `gorm:"default:0"`
	Sequence   int       `gorm:"not null;uniqueIndex:idx_timmy_messages_session_seq,priority:2"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	Session TimmySession `gorm:"foreignKey:SessionID"`
}

// TableName specifies the table name for TimmyMessage
func (TimmyMessage) TableName() string {
	return tableName("timmy_messages")
}

// BeforeCreate generates a UUID if not set
func (m *TimmyMessage) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// TimmyEmbedding represents a vector embedding for a chunk of threat model content
type TimmyEmbedding struct {
	ID             string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID  string    `gorm:"type:varchar(36);not null;index:idx_timmy_embeddings_tm;index:idx_timmy_embeddings_entity,priority:1"`
	EntityType     string    `gorm:"type:varchar(30);not null;index:idx_timmy_embeddings_entity,priority:2"`
	EntityID       string    `gorm:"type:varchar(36);not null;index:idx_timmy_embeddings_entity,priority:3"`
	ChunkIndex     int       `gorm:"not null;index:idx_timmy_embeddings_entity,priority:4"`
	IndexType      string    `gorm:"type:varchar(10);not null;default:text;index:idx_timmy_embeddings_entity,priority:5"`
	ContentHash    string    `gorm:"type:varchar(64);not null"`
	EmbeddingModel string    `gorm:"type:varchar(100);not null"`
	EmbeddingDim   int       `gorm:"not null"`
	VectorData     DBBytes   `gorm:""`
	ChunkText      DBText    `gorm:"not null"`
	CreatedAt      time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

// TableName specifies the table name for TimmyEmbedding
func (TimmyEmbedding) TableName() string {
	return tableName("timmy_embeddings")
}

// BeforeCreate generates a UUID if not set
func (e *TimmyEmbedding) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}

// TimmyUsage tracks LLM token usage for billing and monitoring
type TimmyUsage struct {
	ID               string    `gorm:"primaryKey;type:varchar(36)"`
	UserID           string    `gorm:"type:varchar(36);not null;index:idx_timmy_usage_user"`
	SessionID        string    `gorm:"type:varchar(36);not null;index:idx_timmy_usage_session"`
	ThreatModelID    string    `gorm:"type:varchar(36);not null;index:idx_timmy_usage_tm"`
	MessageCount     int       `gorm:"default:0"`
	PromptTokens     int       `gorm:"default:0"`
	CompletionTokens int       `gorm:"default:0"`
	EmbeddingTokens  int       `gorm:"default:0"`
	PeriodStart      time.Time `gorm:"not null;index:idx_timmy_usage_period"`
	PeriodEnd        time.Time `gorm:"not null"`

	// Relationships
	ThreatModel ThreatModel  `gorm:"foreignKey:ThreatModelID"`
	User        User         `gorm:"foreignKey:UserID;references:InternalUUID"`
	Session     TimmySession `gorm:"foreignKey:SessionID"`
}

// TableName specifies the table name for TimmyUsage
func (TimmyUsage) TableName() string {
	return tableName("timmy_usage")
}

// BeforeCreate generates a UUID if not set
func (u *TimmyUsage) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}
