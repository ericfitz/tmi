package api

// my_identities_handlers_test.go — unit tests for ListMyIdentities and
// DeleteMyIdentity error-classification paths (#383, T25).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/dberrors"
)

// ---------------------------------------------------------------------------
// Stub linked-identity store for api-package tests
// ---------------------------------------------------------------------------

// stubAPILinkedIdentityStore is a minimal LinkedIdentityStore used to drive
// error-classification tests in this package.
type stubAPILinkedIdentityStore struct {
	listErr   error
	deleteErr error
	rows      []models.LinkedIdentity
}

func (s *stubAPILinkedIdentityStore) Create(_ context.Context, _ auth.LinkedIdentityInput) (models.LinkedIdentity, error) {
	return models.LinkedIdentity{}, nil
}

func (s *stubAPILinkedIdentityStore) CreateExclusive(_ context.Context, _ auth.LinkedIdentityInput) (models.LinkedIdentity, error) {
	return models.LinkedIdentity{}, nil
}

func (s *stubAPILinkedIdentityStore) GetByProviderSub(_ context.Context, _, _ string) (models.LinkedIdentity, error) {
	return models.LinkedIdentity{}, auth.ErrLinkedIdentityNotFound
}

func (s *stubAPILinkedIdentityStore) ListByUser(_ context.Context, _ string) ([]models.LinkedIdentity, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.rows, nil
}

func (s *stubAPILinkedIdentityStore) TouchLastUsed(_ context.Context, _ string) error { return nil }

func (s *stubAPILinkedIdentityStore) Delete(_ context.Context, id, _ string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	for _, row := range s.rows {
		if string(row.ID) == id {
			return nil
		}
	}
	return auth.ErrLinkedIdentityNotFound
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newMyIdentitiesContext builds a gin.Context with the minimal JWT-middleware
// context values that ListMyIdentities and DeleteMyIdentity require.
func newMyIdentitiesContext(t *testing.T, userUUID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/me/identities", nil)
	c.Set("userInternalUUID", userUUID)
	c.Set("userIdP", "tmi")
	c.Set("userEmail", "alice@tmi.local")
	c.Set("userDisplayName", "Alice")
	c.Set("userID", "alice")
	// Not a service account
	return c, w
}

// ---------------------------------------------------------------------------
// ListMyIdentities tests
// ---------------------------------------------------------------------------

func TestListMyIdentities_StoreConstraintError_Returns500(t *testing.T) {
	// A transient store error (ErrTransient) should surface as 500 via
	// StoreErrorToRequestError.
	store := &stubAPILinkedIdentityStore{
		listErr: dberrors.Wrap(errors.New("connection reset"), dberrors.ErrTransient),
	}
	server := &Server{linkedIdentityStore: store}

	userUUID := uuid.New().String()
	c, w := newMyIdentitiesContext(t, userUUID)
	server.ListMyIdentities(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEmpty(t, body["error"])
}

func TestListMyIdentities_StoreConstraintError_NotFoundReturns404(t *testing.T) {
	// ErrNotFound should be surfaced as 404, not 500, via StoreErrorToRequestError.
	store := &stubAPILinkedIdentityStore{
		listErr: dberrors.Wrap(errors.New("no rows"), dberrors.ErrNotFound),
	}
	server := &Server{linkedIdentityStore: store}

	userUUID := uuid.New().String()
	c, w := newMyIdentitiesContext(t, userUUID)
	server.ListMyIdentities(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListMyIdentities_Success_ReturnsLinkedRows(t *testing.T) {
	userUUID := uuid.New().String()
	now := time.Now().UTC()
	store := &stubAPILinkedIdentityStore{
		rows: []models.LinkedIdentity{
			{
				ID:               models.DBVarchar(uuid.New().String()),
				UserInternalUUID: models.DBVarchar(userUUID),
				Provider:         models.DBVarchar("google"),
				ProviderUserID:   models.DBVarchar("sub-abc"),
				Email:            models.DBVarchar("alice@google.com"),
				Name:             models.DBVarchar("Alice G"),
				LinkedAt:         now,
			},
		},
	}
	server := &Server{linkedIdentityStore: store}

	c, w := newMyIdentitiesContext(t, userUUID)
	server.ListMyIdentities(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	linked, ok := body["linked"].([]any)
	require.True(t, ok)
	assert.Len(t, linked, 1)
}

// ---------------------------------------------------------------------------
// DeleteMyIdentity tests
// ---------------------------------------------------------------------------

func newDeleteIdentityContext(t *testing.T, userUUID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/me/identities/some-id", nil)
	c.Set("userInternalUUID", userUUID)
	c.Set("userIdP", "tmi")
	c.Set("userEmail", "alice@tmi.local")
	c.Set("userDisplayName", "Alice")
	c.Set("userID", "alice")
	return c, w
}

func TestDeleteMyIdentity_TransientError_Returns500(t *testing.T) {
	store := &stubAPILinkedIdentityStore{
		deleteErr: dberrors.Wrap(errors.New("db unavailable"), dberrors.ErrTransient),
	}
	server := &Server{linkedIdentityStore: store}

	userUUID := uuid.New().String()
	linkID := uuid.New()

	c, w := newDeleteIdentityContext(t, userUUID)
	server.DeleteMyIdentity(c, linkID)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeleteMyIdentity_ConstraintError_Returns400(t *testing.T) {
	// ErrConstraint from the store should be surfaced as 400 via
	// StoreErrorToRequestError (not a 500).
	store := &stubAPILinkedIdentityStore{
		deleteErr: dberrors.Wrap(errors.New("check constraint"), dberrors.ErrConstraint),
	}
	server := &Server{linkedIdentityStore: store}

	userUUID := uuid.New().String()
	linkID := uuid.New()

	c, w := newDeleteIdentityContext(t, userUUID)
	server.DeleteMyIdentity(c, linkID)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteMyIdentity_NotFound_Returns404(t *testing.T) {
	store := &stubAPILinkedIdentityStore{
		// deleteErr nil, rows empty → Delete returns ErrLinkedIdentityNotFound
	}
	server := &Server{linkedIdentityStore: store}

	userUUID := uuid.New().String()
	linkID := uuid.New()

	c, w := newDeleteIdentityContext(t, userUUID)
	server.DeleteMyIdentity(c, linkID)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
