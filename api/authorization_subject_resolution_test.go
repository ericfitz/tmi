package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// unresolvedUserThreatModelStore forces Create to fail the way the Gorm store
// does for an unknown owner, security_reviewer, or authorization user.
type unresolvedUserThreatModelStore struct {
	*MockThreatModelStore
	err error
}

func (m *unresolvedUserThreatModelStore) Create(_ context.Context, _ ThreatModel, _ func(ThreatModel, string) ThreatModel) (ThreatModel, error) {
	return ThreatModel{}, m.err
}

func TestCreateThreatModel_UnresolvedUserReference_Returns400(t *testing.T) {
	cases := map[string]error{
		"authorization_subject": fmt.Errorf("authorization subject 0: %w", ErrInvalidAuthorizationSubject),
		"security_reviewer":     wrapUserReferenceError("security_reviewer", ErrUserNotFound),
		"owner":                 wrapUserReferenceError("owner", ErrUserNotFound),
	}
	for name, storeErr := range cases {
		t.Run(name, func(t *testing.T) {
			r := setupThreatModelRouter()
			orig := ThreatModelStore
			defer func() { ThreatModelStore = orig }()
			ThreatModelStore = &unresolvedUserThreatModelStore{MockThreatModelStore: &MockThreatModelStore{data: make(map[string]ThreatModel)}, err: storeErr}

			body, _ := json.Marshal(map[string]any{
				"name": "TM",
				"authorization": []map[string]any{
					{"principal_type": "user", "provider": "tmi", "provider_id": "ABCDEFGHIJABCDEFGHIJABCDEFGHIJABCDEFGHIJ", "role": "reader"},
				},
			})
			req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
			assert.Contains(t, w.Body.String(), "do not exist")
			assert.NotContains(t, w.Body.String(), "ABCDEFGHIJ")
		})
	}
}

func TestWrapUserReferenceError_PassesThroughOtherErrors(t *testing.T) {
	other := errors.New("boom")
	got := wrapUserReferenceError("owner", other)
	assert.ErrorIs(t, got, other)
	assert.NotErrorIs(t, got, ErrInvalidAuthorizationSubject)
	assert.ErrorIs(t, wrapUserReferenceError("owner", ErrUserNotFound), ErrInvalidAuthorizationSubject)
}

// A DB failure during the user lookup is not "user not found": it must be
// classified (retry/503/500), never reported as a false 400.
func TestResolveUserIdentifierToUUID_DBErrorIsNotUserNotFound(t *testing.T) {
	db := setupAuthSubjectTestDB(t)
	store := NewGormThreatModelStore(db)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail_user_query", func(tx *gorm.DB) {
		tx.Error = errors.New("connection reset by peer")
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:fail_user_query") })

	_, err := store.resolveUserIdentifierToUUID(db, "someone")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrUserNotFound)

	err = store.saveAuthorizationTx(db, uuid.New().String(), []Authorization{{
		Provider: "tmi", ProviderId: "someone", PrincipalType: AuthorizationPrincipalTypeUser, Role: AuthorizationRoleReader,
	}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidAuthorizationSubject)
}

func TestSaveAuthorizationTx_RejectsUnsupportedPrincipalType(t *testing.T) {
	db := setupAuthSubjectTestDB(t)
	store := NewGormThreatModelStore(db)
	err := store.saveAuthorizationTx(db, uuid.New().String(), []Authorization{{
		Provider: "tmi", ProviderId: "svc", PrincipalType: AuthorizationPrincipalType("service"), Role: AuthorizationRoleReader,
	}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAuthorizationSubject)
	var count int64
	require.NoError(t, db.Model(&models.ThreatModelAccess{}).Count(&count).Error)
	assert.Zero(t, count)
}

// A valid entry followed by an invalid one must leave no rows once the
// surrounding transaction rolls back.
func TestSaveAuthorizationTx_PartialListRollsBack(t *testing.T) {
	db := setupAuthSubjectTestDB(t)
	store := NewGormThreatModelStore(db)
	pid := "bob-sub"
	require.NoError(t, db.Create(&models.User{
		InternalUUID: models.DBVarchar(uuid.New().String()), Provider: "tmi",
		ProviderUserID: models.NewNullableDBVarchar(&pid), Email: "bob@tmi.local", Name: "Bob",
	}).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return store.saveAuthorizationTx(tx, uuid.New().String(), []Authorization{
			{Provider: "tmi", ProviderId: pid, PrincipalType: AuthorizationPrincipalTypeUser, Role: AuthorizationRoleReader},
			{Provider: "tmi", ProviderId: "ghost", PrincipalType: AuthorizationPrincipalTypeUser, Role: AuthorizationRoleReader},
		})
	})
	require.ErrorIs(t, err, ErrInvalidAuthorizationSubject)
	var count int64
	require.NoError(t, db.Model(&models.ThreatModelAccess{}).Count(&count).Error)
	assert.Zero(t, count, "valid first entry must be rolled back")
}
