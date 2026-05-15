package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TimmySession represents a chat session between a user and Timmy for a threat model
type TimmySession struct {
	ID               DBVarchar         `gorm:"primaryKey;not null;size:36"`
	ThreatModelID    DBVarchar         `gorm:"size:36;not null;index:idx_timmy_sessions_tm;index:idx_timmy_sessions_tm_user,priority:1"`
	UserID           DBVarchar         `gorm:"size:36;not null;index:idx_timmy_sessions_user;index:idx_timmy_sessions_tm_user,priority:2"`
	Title            DBVarchar         `gorm:"size:256"`
	SourceSnapshot   JSONRaw           `gorm:""`
	SystemPromptHash NullableDBVarchar `gorm:"size:64"`
	Status           DBVarchar         `gorm:"size:20;not null;default:active;index:idx_timmy_sessions_status"`
	CreatedAt        time.Time         `gorm:"not null;autoCreateTime"`
	ModifiedAt       time.Time         `gorm:"not null;autoUpdateTime"`
	DeletedAt        *time.Time        `gorm:"index:idx_timmy_sessions_deleted_at"`

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
		s.ID = DBVarchar(uuid.New().String())
	}
	if s.Status == "" {
		s.Status = "active"
	}
	return nil
}

// TimmyMessage represents a single message in a Timmy chat session
type TimmyMessage struct {
	ID         DBVarchar `gorm:"primaryKey;not null;size:36"`
	SessionID  DBVarchar `gorm:"size:36;not null;index:idx_timmy_messages_session;uniqueIndex:idx_timmy_messages_session_seq,priority:1"`
	Role       DBVarchar `gorm:"size:20;not null"`
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
		m.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TimmyEmbedding represents a vector embedding for a chunk of threat model content
type TimmyEmbedding struct {
	ID             DBVarchar `gorm:"primaryKey;not null;size:36"`
	ThreatModelID  DBVarchar `gorm:"size:36;not null;index:idx_timmy_embeddings_tm;index:idx_timmy_embeddings_entity,priority:1"`
	EntityType     DBVarchar `gorm:"size:30;not null;index:idx_timmy_embeddings_entity,priority:2"`
	EntityID       DBVarchar `gorm:"size:36;not null;index:idx_timmy_embeddings_entity,priority:3"`
	ChunkIndex     int       `gorm:"not null;index:idx_timmy_embeddings_entity,priority:4"`
	IndexType      DBVarchar `gorm:"size:10;not null;default:text;index:idx_timmy_embeddings_entity,priority:5"`
	ContentHash    DBVarchar `gorm:"size:64;not null"`
	EmbeddingModel DBVarchar `gorm:"size:100;not null"`
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
		e.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TimmyUsage tracks LLM token usage for billing and monitoring
type TimmyUsage struct {
	ID               DBVarchar `gorm:"primaryKey;not null;size:36"`
	UserID           DBVarchar `gorm:"size:36;not null;index:idx_timmy_usage_user"`
	SessionID        DBVarchar `gorm:"size:36;not null;index:idx_timmy_usage_session"`
	ThreatModelID    DBVarchar `gorm:"size:36;not null;index:idx_timmy_usage_tm"`
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
		u.ID = DBVarchar(uuid.New().String())
	}
	return nil
}
