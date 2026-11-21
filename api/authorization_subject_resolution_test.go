package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupAuthSubjectTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Group{}, &models.ThreatModelAccess{}))
	return db
}

// TestSaveAuthorizationTx_RejectsUnresolvedUserSubject guards the CATS run
// 20261121T201842Z 500: an unknown provider_id was written verbatim into
// user_internal_uuid (varchar(36)). Every unresolved user subject, short or
// long, must be rejected before any insert.
func TestSaveAuthorizationTx_RejectsUnresolvedUserSubject(t *testing.T) {
	for name, providerID := range map[string]string{
		"long":  "ABCDEFGHIJABCDEFGHIJABCDEFGHIJABCDEFGHIJ",
		"short": "nobody",
	} {
		t.Run(name, func(t *testing.T) {
			db := setupAuthSubjectTestDB(t)
			store := NewGormThreatModelStore(db)
			err := store.saveAuthorizationTx(db, uuid.New().String(), []Authorization{{
				Provider:      "tmi",
				ProviderId:    providerID,
				PrincipalType: AuthorizationPrincipalTypeUser,
				Role:          AuthorizationRoleReader,
			}})
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidAuthorizationSubject), "got %v", err)

			var count int64
			require.NoError(t, db.Model(&models.ThreatModelAccess{}).Count(&count).Error)
			assert.Zero(t, count, "no access row may be inserted for an unresolved subject")
		})
	}
}

func TestSaveAuthorizationTx_ResolvesKnownUserAndAutoCreatesGroup(t *testing.T) {
	db := setupAuthSubjectTestDB(t)
	store := NewGormThreatModelStore(db)
	pid := "alice-sub"
	user := models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "tmi",
		ProviderUserID: models.NewNullableDBVarchar(&pid),
		Email:          "alice@tmi.local",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(&user).Error)

	err := store.saveAuthorizationTx(db, uuid.New().String(), []Authorization{
		{Provider: "tmi", ProviderId: pid, PrincipalType: AuthorizationPrincipalTypeUser, Role: AuthorizationRoleWriter},
		{Provider: "tmi", ProviderId: "security-reviewers", PrincipalType: AuthorizationPrincipalTypeGroup, Role: AuthorizationRoleReader},
	})
	require.NoError(t, err)

	var rows []models.ThreatModelAccess
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 2)
}

func TestInvalidAuthorizationSubjectMapsTo400(t *testing.T) {
	err := fmt.Errorf("authorization subject 0: %w", ErrInvalidAuthorizationSubject)
	assert.Equal(t, http.StatusBadRequest, StoreErrorToRequestError(err, "nf", "fail").Status)
	assert.Equal(t, http.StatusBadRequest, WriteErrorToRequestError(err, "fail").Status)
}
