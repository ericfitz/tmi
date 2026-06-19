package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserContentToken is a per-user OAuth token used by delegated content providers.
// access_token and refresh_token are AES-256-GCM ciphertexts (nonce prepended).
// DBBytes maps to BYTEA on PostgreSQL and BLOB on Oracle / SQLite (#404).
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB model for a per-user OAuth token used by delegated content providers, with encrypted token fields (pure)
type UserContentToken struct {
	ID                   DBVarchar `gorm:"primaryKey;not null;size:36"`
	UserID               DBVarchar `gorm:"size:36;not null;index:idx_uct_user;uniqueIndex:uq_uct_user_provider,priority:1"`
	ProviderID           DBVarchar `gorm:"size:64;not null;uniqueIndex:uq_uct_user_provider,priority:2"`
	AccessToken          DBBytes   `gorm:"not null"`
	RefreshToken         DBBytes
	Scopes               DBText
	ExpiresAt            *time.Time
	Status               NullableDBVarchar `gorm:"size:16;default:active;index:idx_uct_status_expires,priority:1"`
	LastRefreshAt        *time.Time        `gorm:"index:idx_uct_status_expires,priority:2"`
	LastError            DBText
	ProviderAccountID    NullableDBVarchar `gorm:"size:255"`
	ProviderAccountLabel NullableDBVarchar `gorm:"size:255"`
	CreatedAt            time.Time         `gorm:"not null;autoCreateTime"`
	ModifiedAt           time.Time         `gorm:"not null;autoUpdateTime"`

	// Owner is the user who owns this token; ON DELETE CASCADE removes the token when the user is deleted.
	Owner User `gorm:"foreignKey:UserID;references:InternalUUID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for UserContentToken.
// SEM@fa58c5122fea64e4d8baa4116b86a3f00053b0c3: return the DB table name for UserContentToken (pure)
func (UserContentToken) TableName() string {
	return tableName("user_content_tokens")
}

// BeforeCreate generates a UUID if not set.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: generate and assign a UUID primary key before inserting a UserContentToken (pure)
func (u *UserContentToken) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = DBVarchar(uuid.New().String())
	}
	return nil
}
