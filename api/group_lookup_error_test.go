package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A genuine DB failure while resolving a group subject must be returned, not
// treated as not-found and followed by group creation.
func TestSaveAuthorizationTx_GroupLookupDBErrorIsReturned(t *testing.T) {
	db := setupAuthSubjectTestDB(t)
	store := NewGormThreatModelStore(db)

	failed := false
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-first-group-query", func(tx *gorm.DB) {
		if !failed && tx.Statement.Table == "groups" {
			failed = true
			_ = tx.AddError(errors.New("injected lookup failure"))
		}
	}))

	err := store.saveAuthorizationTx(db, uuid.New().String(), []Authorization{{
		Provider: "tmi", ProviderId: "reviewers", PrincipalType: AuthorizationPrincipalTypeGroup, Role: AuthorizationRoleReader,
	}})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "injected lookup failure"), "got %v", err)
	assert.False(t, errors.Is(err, ErrInvalidAuthorizationSubject))

	var count int64
	require.NoError(t, db.Model(&models.Group{}).Where("group_name = ?", "reviewers").Count(&count).Error)
	assert.Zero(t, count, "a lookup failure must not create the group")
}
