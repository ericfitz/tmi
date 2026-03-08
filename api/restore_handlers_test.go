package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRestoreTest creates a test threat model in the mock store and returns the ID.
// If deleted is true, the threat model is soft-deleted.
func setupRestoreTest(t *testing.T, deleted bool) string {
	t.Helper()
	InitTestFixtures()

	tmID := uuid.New().String()
	tmUUID, _ := uuid.Parse(tmID)
	now := time.Now().UTC()

	tm := ThreatModel{
		Id:        &tmUUID,
		Name:      "Test Threat Model",
		CreatedAt: &now,
		Owner: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "tmi",
			ProviderId:    TestFixtures.OwnerUser,
			Email:         openapi_types.Email(TestFixtures.OwnerUser),
			DisplayName:   "Test Owner",
		},
		Authorization: []Authorization{
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "tmi",
				ProviderId:    TestFixtures.OwnerUser,
				Role:          RoleOwner,
			},
		},
	}

	if deleted {
		deletedAt := now.Add(-1 * time.Hour)
		tm.DeletedAt = &deletedAt
	}

	mockStore := ThreatModelStore.(*MockThreatModelStore)
	mockStore.data[tmID] = tm

	return tmID
}

func TestRestoreThreatModel_Success(t *testing.T) {
	tmID := setupRestoreTest(t, true)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/threat_models/"+tmID+"/restore", nil)

	HandleRestoreThreatModel(c, tmID)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify the response body contains the restored threat model
	var response map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "Test Threat Model", response["name"])

	// Verify the threat model is no longer deleted in the store
	tm, err := ThreatModelStore.Get(tmID)
	require.NoError(t, err)
	assert.Nil(t, tm.DeletedAt)
}

func TestRestoreThreatModel_NotDeleted(t *testing.T) {
	tmID := setupRestoreTest(t, false)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/threat_models/"+tmID+"/restore", nil)

	HandleRestoreThreatModel(c, tmID)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestRestoreThreatModel_NotFound(t *testing.T) {
	InitTestFixtures()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	nonExistentID := uuid.New().String()
	c.Request = httptest.NewRequest(http.MethodPost, "/threat_models/"+nonExistentID+"/restore", nil)

	HandleRestoreThreatModel(c, nonExistentID)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRestoreThreatModel_InvalidID(t *testing.T) {
	InitTestFixtures()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/threat_models/not-a-uuid/restore", nil)

	HandleRestoreThreatModel(c, "not-a-uuid")

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRestoreSubEntity_ParentDeleted(t *testing.T) {
	// When the parent threat model is also deleted, sub-entity restore should return 409
	tmID := setupRestoreTest(t, true) // Parent is deleted
	entityID := uuid.New().String()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/threat_models/"+tmID+"/diagrams/"+entityID+"/restore", nil)

	restoreSubEntity(c, tmID, entityID, "diagram",
		func() error { return nil },
		func() (any, error) { return map[string]string{"id": entityID}, nil },
	)

	assert.Equal(t, http.StatusConflict, w.Code)

	var response map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response["error_description"], "parent threat model is deleted")
}

func TestRestoreSubEntity_ParentNotDeleted(t *testing.T) {
	// When the parent is active, sub-entity restore should succeed
	tmID := setupRestoreTest(t, false) // Parent is NOT deleted
	entityID := uuid.New().String()

	restored := map[string]string{"id": entityID, "name": "Test Entity"}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/threat_models/"+tmID+"/diagrams/"+entityID+"/restore", nil)

	restoreSubEntity(c, tmID, entityID, "diagram",
		func() error { return nil },
		func() (any, error) { return restored, nil },
	)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRestoreSubEntity_ParentNotFound(t *testing.T) {
	InitTestFixtures()

	nonExistentTMID := uuid.New().String()
	entityID := uuid.New().String()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/threat_models/"+nonExistentTMID+"/diagrams/"+entityID+"/restore", nil)

	restoreSubEntity(c, nonExistentTMID, entityID, "diagram",
		func() error { return nil },
		func() (any, error) { return nil, nil },
	)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRestoreSubEntity_InvalidIDs(t *testing.T) {
	InitTestFixtures()

	t.Run("invalid threat model ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)

		restoreSubEntity(c, "bad-id", uuid.New().String(), "diagram",
			func() error { return nil },
			func() (any, error) { return nil, nil },
		)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid entity ID", func(t *testing.T) {
		tmID := setupRestoreTest(t, false)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)

		restoreSubEntity(c, tmID, "bad-id", "diagram",
			func() error { return nil },
			func() (any, error) { return nil, nil },
		)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestIsNotFoundOrNotDeleted(t *testing.T) {
	assert.True(t, isNotFoundOrNotDeleted(fmt.Errorf("entity not found")))
	assert.True(t, isNotFoundOrNotDeleted(fmt.Errorf("threat model with ID abc not found or not deleted")))
	assert.False(t, isNotFoundOrNotDeleted(fmt.Errorf("database connection failed")))
	assert.False(t, isNotFoundOrNotDeleted(nil))
}
