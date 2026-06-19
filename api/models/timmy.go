package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TimmySession represents a chat session between a user and Timmy for a threat model
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a Timmy AI chat session scoped to a threat model and user (reads DB)
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
// SEM@38c9cd78ea6f81a7cfa5891e34a980915566378b: return the DB table name for TimmySession (pure)
func (TimmySession) TableName() string {
	return tableName("timmy_sessions")
}

// BeforeCreate generates a UUID and sets default status if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID and default status to a TimmySession before insert (pure)
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
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a single ordered message in a Timmy chat session (reads DB)
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
// SEM@38c9cd78ea6f81a7cfa5891e34a980915566378b: return the DB table name for TimmyMessage (pure)
func (TimmyMessage) TableName() string {
	return tableName("timmy_messages")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a UUID to a TimmyMessage before insert (pure)
func (m *TimmyMessage) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TimmyEmbedding represents a vector embedding for a chunk of threat model content
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: GORM model for a vector embedding chunk derived from threat model content (reads DB)
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
// SEM@38c9cd78ea6f81a7cfa5891e34a980915566378b: return the database table name for TimmyEmbedding records (pure)
func (TimmyEmbedding) TableName() string {
	return tableName("timmy_embeddings")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a new UUID to TimmyEmbedding if none is set before insert (mutates shared state)
func (e *TimmyEmbedding) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = DBVarchar(uuid.New().String())
	}
	return nil
}

// TimmyUsage tracks LLM token usage for billing and monitoring
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: store LLM token usage metrics per user, session, and threat model for billing (reads DB)
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
// SEM@38c9cd78ea6f81a7cfa5891e34a980915566378b: return the database table name for TimmyUsage records (pure)
func (TimmyUsage) TableName() string {
	return tableName("timmy_usage")
}

// BeforeCreate generates a UUID if not set
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: assign a new UUID to TimmyUsage if none is set before insert (mutates shared state)
func (u *TimmyUsage) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = DBVarchar(uuid.New().String())
	}
	return nil
}
