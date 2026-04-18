package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserContentToken_AutoMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(&UserContentToken{})
	require.NoError(t, err)
	assert.True(t, db.Migrator().HasTable(&UserContentToken{}))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "user_id"))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "provider_id"))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "access_token"))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "refresh_token"))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "status"))
}

func TestUserContentToken_TableName(t *testing.T) {
	assert.Equal(t, tableName("user_content_tokens"), UserContentToken{}.TableName())
}

func TestUserContentToken_BeforeCreate_GeneratesUUID(t *testing.T) {
	tok := &UserContentToken{}
	err := tok.BeforeCreate(nil)
	require.NoError(t, err)
	_, err = uuid.Parse(tok.ID)
	assert.NoError(t, err)
}

func TestUserContentToken_BeforeCreate_PreservesExistingID(t *testing.T) {
	id := uuid.New().String()
	tok := &UserContentToken{ID: id, CreatedAt: time.Now()}
	err := tok.BeforeCreate(nil)
	require.NoError(t, err)
	assert.Equal(t, id, tok.ID)
}
