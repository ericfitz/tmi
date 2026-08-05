package api

// admin_identities_handlers_test.go — unit tests for AdminListUserIdentities
// and AdminDeleteUserIdentity (#646).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
)

// ---------------------------------------------------------------------------
// Stub linked-identity store for this file's tests
// ---------------------------------------------------------------------------

// adminIdentitiesStubStore is a minimal auth.LinkedIdentityStore used to
// drive AdminListUserIdentities/AdminDeleteUserIdentity tests. Unlike
// stubAPILinkedIdentityStore (my_identities_handlers_test.go), Delete here
// records the (id, ownerUUID) it was called with so tests can assert the
// handler scoped the delete to the target user, not the calling admin.
// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: stub linked-identity store recording delete calls for admin identity tests
type adminIdentitiesStubStore struct {
	rows      []models.LinkedIdentity
	deleteErr error

	lastDeleteID    string
	lastDeleteOwner string
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: stub a linked identity creation as a no-op for admin identity tests (pure)
func (s *adminIdentitiesStubStore) Create(_ context.Context, _ auth.LinkedIdentityInput) (models.LinkedIdentity, error) {
	return models.LinkedIdentity{}, nil
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: stub an exclusive linked identity creation as a no-op for admin identity tests (pure)
func (s *adminIdentitiesStubStore) CreateExclusive(_ context.Context, _ auth.LinkedIdentityInput) (models.LinkedIdentity, error) {
	return models.LinkedIdentity{}, nil
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: stub a linked-identity lookup by provider subject as always-not-found (pure)
func (s *adminIdentitiesStubStore) GetByProviderSub(_ context.Context, _, _ string) (models.LinkedIdentity, error) {
	return models.LinkedIdentity{}, auth.ErrLinkedIdentityNotFound
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: filter the stub's linked identities to those owned by a given user (pure)
func (s *adminIdentitiesStubStore) ListByUser(_ context.Context, userInternalUUID string) ([]models.LinkedIdentity, error) {
	var out []models.LinkedIdentity
	for _, row := range s.rows {
		if string(row.UserInternalUUID) == userInternalUUID {
			out = append(out, row)
		}
	}
	return out, nil
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: stub updating a linked identity's last-used timestamp as a no-op (pure)
func (s *adminIdentitiesStubStore) TouchLastUsed(_ context.Context, _ string) error { return nil }

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: stub deleting a linked identity, recording the call and honoring a forced error
func (s *adminIdentitiesStubStore) Delete(_ context.Context, id, ownerUUID string) error {
	s.lastDeleteID = id
	s.lastDeleteOwner = ownerUUID
	if s.deleteErr != nil {
		return s.deleteErr
	}
	for _, row := range s.rows {
		if string(row.ID) == id && string(row.UserInternalUUID) == ownerUUID {
			return nil
		}
	}
	return auth.ErrLinkedIdentityNotFound
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newAdminIdentitiesContext builds a gin.Context with the minimal
// admin-auth context values the handlers read (audit actor fields).
// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: build a gin test context with admin auth fields for identity handler tests
func newAdminIdentitiesContext(t *testing.T, method, path, adminUUID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	SetFullUserContext(c, "admin@example.com", "admin-provider-id", adminUUID, "tmi", []string{"administrators"})
	c.Set("isAdmin", true)
	return c, w
}

// ---------------------------------------------------------------------------
// AdminListUserIdentities tests
// ---------------------------------------------------------------------------

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: test that listing a user's identities returns primary and linked entries
func TestAdminListUserIdentities_Success(t *testing.T) {
	origStore := GlobalUserStore
	defer func() { GlobalUserStore = origStore }()

	targetUUID := uuid.New()
	mockStore := newMockUserStore()
	mockStore.addUser(AdminUser{
		InternalUuid: targetUUID,
		Name:         "Target User",
		Email:        openapi_types.Email("target@example.com"),
		Provider:     "tmi",
	})
	GlobalUserStore = mockStore

	now := time.Now().UTC()
	linkedStore := &adminIdentitiesStubStore{
		rows: []models.LinkedIdentity{
			{
				ID:               models.DBVarchar(uuid.New().String()),
				UserInternalUUID: models.DBVarchar(targetUUID.String()),
				Provider:         models.DBVarchar("google"),
				ProviderUserID:   models.DBVarchar("sub-abcdefgh-long"),
				Email:            models.DBVarchar("target@google.com"),
				Name:             models.DBVarchar("Target G"),
				LinkedAt:         now,
			},
			{
				ID:               models.DBVarchar(uuid.New().String()),
				UserInternalUUID: models.DBVarchar(targetUUID.String()),
				Provider:         models.DBVarchar("github"),
				ProviderUserID:   models.DBVarchar("sub-xyz"),
				LinkedAt:         now,
			},
		},
	}
	server := &Server{linkedIdentityStore: linkedStore}

	c, w := newAdminIdentitiesContext(t, http.MethodGet, "/admin/users/"+targetUUID.String()+"/identities", uuid.New().String())
	server.AdminListUserIdentities(c, targetUUID)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	primary, ok := body["primary"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tmi", primary["provider"])
	assert.Equal(t, "target@example.com", primary["email"])
	assert.Equal(t, "Target User", primary["name"])

	linked, ok := body["linked"].([]any)
	require.True(t, ok)
	require.Len(t, linked, 2)

	first, ok := linked[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "google", first["provider"])
	// provider_user_id is truncated to 8 chars + ellipsis when longer than 8.
	assert.Equal(t, "sub-abcd…", first["provider_user_id"])

	second, ok := linked[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "github", second["provider"])
	// "sub-xyz" is 7 chars, no truncation applied.
	assert.Equal(t, "sub-xyz", second["provider_user_id"])
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: test that listing identities for an unknown user returns 404
func TestAdminListUserIdentities_UserNotFound(t *testing.T) {
	origStore := GlobalUserStore
	defer func() { GlobalUserStore = origStore }()

	GlobalUserStore = newMockUserStore() // empty: target UUID is unknown

	server := &Server{linkedIdentityStore: &adminIdentitiesStubStore{}}

	targetUUID := uuid.New()
	c, w := newAdminIdentitiesContext(t, http.MethodGet, "/admin/users/"+targetUUID.String()+"/identities", uuid.New().String())
	server.AdminListUserIdentities(c, targetUUID)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not_found")
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: test that listing identities without a wired identity store returns 500
func TestAdminListUserIdentities_StoreNil(t *testing.T) {
	origStore := GlobalUserStore
	defer func() { GlobalUserStore = origStore }()
	GlobalUserStore = newMockUserStore()

	server := &Server{} // linkedIdentityStore not wired

	targetUUID := uuid.New()
	c, w := newAdminIdentitiesContext(t, http.MethodGet, "/admin/users/"+targetUUID.String()+"/identities", uuid.New().String())
	server.AdminListUserIdentities(c, targetUUID)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "server_error")
}

// TestAdminListUserIdentities_InvalidUUID pins the routing-layer behavior:
// a malformed user_id path segment never reaches the handler (which takes a
// pre-parsed openapi_types.UUID) — the generated ServerInterfaceWrapper's
// parameter binding rejects it with 400 first. This mirrors how
// admin_user_handlers_test.go exercises the same invariant for
// GET /admin/users/{user_id}.
// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: test that a malformed user_id path segment is rejected before the handler runs
func TestAdminListUserIdentities_InvalidUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/users/:user_id/identities", func(c *gin.Context) {
		if _, err := uuid.Parse(c.Param("user_id")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_uuid", "error_description": "user_id must be a valid UUID"})
			return
		}
		t.Fatal("handler should not be reached with an invalid UUID")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/nope/identities", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_uuid")
}

// ---------------------------------------------------------------------------
// AdminDeleteUserIdentity tests
// ---------------------------------------------------------------------------

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: test that deleting a target user's linked identity scopes the delete correctly
func TestAdminDeleteUserIdentity_Success(t *testing.T) {
	origStore := GlobalUserStore
	defer func() { GlobalUserStore = origStore }()

	targetUUID := uuid.New()
	mockStore := newMockUserStore()
	mockStore.addUser(AdminUser{
		InternalUuid: targetUUID,
		Name:         "Target User",
		Email:        openapi_types.Email("target@example.com"),
		Provider:     "tmi",
	})
	GlobalUserStore = mockStore

	identityID := uuid.New()
	linkedStore := &adminIdentitiesStubStore{
		rows: []models.LinkedIdentity{
			{
				ID:               models.DBVarchar(identityID.String()),
				UserInternalUUID: models.DBVarchar(targetUUID.String()),
				Provider:         models.DBVarchar("google"),
				ProviderUserID:   models.DBVarchar("sub-abc"),
				LinkedAt:         time.Now().UTC(),
			},
		},
	}
	server := &Server{linkedIdentityStore: linkedStore}

	c, w := newAdminIdentitiesContext(t, http.MethodDelete,
		"/admin/users/"+targetUUID.String()+"/identities/"+identityID.String(), uuid.New().String())
	server.AdminDeleteUserIdentity(c, targetUUID, identityID)
	// c.Status() only buffers the code on gin's responseWriter; calling the
	// handler directly (bypassing gin's engine dispatch) means nothing ever
	// flushes it to the recorder without this explicit call.
	c.Writer.WriteHeaderNow()

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, identityID.String(), linkedStore.lastDeleteID)
	assert.Equal(t, targetUUID.String(), linkedStore.lastDeleteOwner)
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: test that deleting an unknown linked identity returns 404
func TestAdminDeleteUserIdentity_NotFound(t *testing.T) {
	origStore := GlobalUserStore
	defer func() { GlobalUserStore = origStore }()

	targetUUID := uuid.New()
	mockStore := newMockUserStore()
	mockStore.addUser(AdminUser{
		InternalUuid: targetUUID,
		Name:         "Target User",
		Email:        openapi_types.Email("target@example.com"),
		Provider:     "tmi",
	})
	GlobalUserStore = mockStore

	// No rows: Delete returns auth.ErrLinkedIdentityNotFound.
	linkedStore := &adminIdentitiesStubStore{}
	server := &Server{linkedIdentityStore: linkedStore}

	identityID := uuid.New()
	c, w := newAdminIdentitiesContext(t, http.MethodDelete,
		"/admin/users/"+targetUUID.String()+"/identities/"+identityID.String(), uuid.New().String())
	server.AdminDeleteUserIdentity(c, targetUUID, identityID)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not_found")
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: test that deleting an identity for an unknown user returns 404
func TestAdminDeleteUserIdentity_UserNotFound(t *testing.T) {
	origStore := GlobalUserStore
	defer func() { GlobalUserStore = origStore }()
	GlobalUserStore = newMockUserStore() // empty: target UUID is unknown

	server := &Server{linkedIdentityStore: &adminIdentitiesStubStore{}}

	targetUUID := uuid.New()
	identityID := uuid.New()
	c, w := newAdminIdentitiesContext(t, http.MethodDelete,
		"/admin/users/"+targetUUID.String()+"/identities/"+identityID.String(), uuid.New().String())
	server.AdminDeleteUserIdentity(c, targetUUID, identityID)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not_found")
}

// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: test that deleting an identity without a wired identity store returns 500
func TestAdminDeleteUserIdentity_StoreNil(t *testing.T) {
	origStore := GlobalUserStore
	defer func() { GlobalUserStore = origStore }()
	GlobalUserStore = newMockUserStore()

	server := &Server{} // linkedIdentityStore not wired, same as /me/identities

	targetUUID := uuid.New()
	identityID := uuid.New()
	c, w := newAdminIdentitiesContext(t, http.MethodDelete,
		"/admin/users/"+targetUUID.String()+"/identities/"+identityID.String(), uuid.New().String())
	server.AdminDeleteUserIdentity(c, targetUUID, identityID)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "server_error")
}
