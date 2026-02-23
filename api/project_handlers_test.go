package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Project Handler Tests
// =============================================================================

func TestListProjects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success with defaults", func(t *testing.T) {
		store := newMockProjectStore()
		store.listItems = []ProjectListItem{
			{Name: "Project A"},
			{Name: "Project B"},
		}
		store.listTotal = 2
		saveTeamProjectStores(t, nil, store)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/projects")
		TestUsers.Owner.SetContext(c)

		server.ListProjects(c, ListProjectsParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListProjectsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 2, resp.Total)
		assert.Equal(t, 20, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Len(t, resp.Projects, 2)
	})

	t.Run("pagination parameters", func(t *testing.T) {
		store := newMockProjectStore()
		store.listItems = []ProjectListItem{}
		store.listTotal = 50
		saveTeamProjectStores(t, nil, store)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/projects?limit=10&offset=20")
		TestUsers.Owner.SetContext(c)
		limit := 10
		offset := 20
		server.ListProjects(c, ListProjectsParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListProjectsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 10, resp.Limit)
		assert.Equal(t, 20, resp.Offset)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockProjectStore()
		store.listErr = errors.New("database connection lost")
		saveTeamProjectStores(t, nil, store)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/projects")
		TestUsers.Owner.SetContext(c)

		server.ListProjects(c, ListProjectsParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockProjectStore()
		saveTeamProjectStores(t, nil, store)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/projects")

		server.ListProjects(c, ListProjectsParams{})

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestCreateProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		teamStore := newMockTeamStore()
		teamStore.isMember = true
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, teamStore, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		body := ProjectInput{
			Name:   "New Project",
			TeamId: teamUUID,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/projects", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProject(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		var created Project
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		assert.Equal(t, "New Project", created.Name)
		assert.NotNil(t, created.Id)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)
		setupTestTeamAuthDB(t)

		teamUUID, _ := uuid.Parse(testTeamID)
		body := ProjectInput{
			Name:   "Test",
			TeamId: teamUUID,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/projects", "application/json", bodyBytes)

		server.CreateProject(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid body", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContextWithBody("POST", "/projects", "application/json", []byte(`{invalid json`))
		TestUsers.Owner.SetContext(c)

		server.CreateProject(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("forbidden - not team member", func(t *testing.T) {
		teamStore := newMockTeamStore()
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, teamStore, projectStore)

		db := setupTestTeamAuthDB(t)
		// Create team but don't add test user as member
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user",
		}).Error
		require.NoError(t, err)

		teamUUID, _ := uuid.Parse(testTeamID)
		body := ProjectInput{
			Name:   "Project",
			TeamId: teamUUID,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/projects", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProject(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		teamStore := newMockTeamStore()
		teamStore.isMember = true
		projectStore := newMockProjectStore()
		projectStore.createErr = errors.New("db error")
		saveTeamProjectStores(t, teamStore, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		body := ProjectInput{
			Name:   "Project",
			TeamId: teamUUID,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/projects", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProject(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestGetProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		projectStore := newMockProjectStore()
		seedProjectInStore(projectStore, testProjectID, "Test Project", testTeamID)
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID)
		TestUsers.Owner.SetContext(c)

		server.GetProject(c, projectUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var project Project
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &project))
		assert.Equal(t, "Test Project", project.Name)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		projectStore := newMockProjectStore()
		seedProjectInStore(projectStore, testProjectID, "Test Project", testTeamID)
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		// Create team without test user as member
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID)
		TestUsers.Owner.SetContext(c)

		server.GetProject(c, projectUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)
		setupTestTeamAuthDB(t)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID)

		server.GetProject(c, projectUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		projectStore := newMockProjectStore()
		projectStore.getErr = errors.New("db error")
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID)
		TestUsers.Owner.SetContext(c)

		server.GetProject(c, projectUUID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestUpdateProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		projectStore := newMockProjectStore()
		seedProjectInStore(projectStore, testProjectID, "Old Name", testTeamID)
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		teamUUID, _ := uuid.Parse(testTeamID)
		body := ProjectInput{
			Name:   "Updated Name",
			TeamId: teamUUID,
		}
		bodyBytes, _ := json.Marshal(body)
		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateProject(c, projectUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var updated Project
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
		assert.Equal(t, "Updated Name", updated.Name)
	})

	t.Run("invalid body", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID, "application/json", []byte(`{bad`))
		TestUsers.Owner.SetContext(c)

		server.UpdateProject(c, projectUUID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("forbidden", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		teamUUID, _ := uuid.Parse(testTeamID)
		body := ProjectInput{
			Name:   "Updated",
			TeamId: teamUUID,
		}
		bodyBytes, _ := json.Marshal(body)
		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateProject(c, projectUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)
		setupTestTeamAuthDB(t)

		teamUUID, _ := uuid.Parse(testTeamID)
		body := ProjectInput{
			Name:   "Updated",
			TeamId: teamUUID,
		}
		bodyBytes, _ := json.Marshal(body)
		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID, "application/json", bodyBytes)

		server.UpdateProject(c, projectUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestPatchProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		projectStore := newMockProjectStore()
		seedProjectInStore(projectStore, testProjectID, "Original Project", testTeamID)
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		patch := []PatchOperation{
			{Op: "replace", Path: "/description", Value: "Patched description"},
		}
		patchBytes, _ := json.Marshal(patch)
		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID, "application/json-patch+json", patchBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchProject(c, projectUUID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("prohibited field rejected", func(t *testing.T) {
		projectStore := newMockProjectStore()
		seedProjectInStore(projectStore, testProjectID, "Project", testTeamID)
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		patch := []PatchOperation{
			{Op: "replace", Path: "/id", Value: "new-id"},
		}
		patchBytes, _ := json.Marshal(patch)
		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID, "application/json-patch+json", patchBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchProject(c, projectUUID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "id")
	})

	t.Run("forbidden", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		patch := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "new"},
		}
		patchBytes, _ := json.Marshal(patch)
		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID, "application/json-patch+json", patchBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchProject(c, projectUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)
		setupTestTeamAuthDB(t)

		patch := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "new"},
		}
		patchBytes, _ := json.Marshal(patch)
		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID, "application/json-patch+json", patchBytes)

		server.PatchProject(c, projectUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDeleteProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		projectStore := newMockProjectStore()
		projectStore.teamID = testTeamID
		seedProjectInStore(projectStore, testProjectID, "Project To Delete", testTeamID)
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, _ := CreateTestGinContext("DELETE", "/projects/"+testProjectID)
		TestUsers.Owner.SetContext(c)

		server.DeleteProject(c, projectUUID)

		// c.Status() doesn't flush to httptest.ResponseRecorder; check gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	})

	t.Run("forbidden - non-owner", func(t *testing.T) {
		projectStore := newMockProjectStore()
		projectStore.teamID = testTeamID
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		// Team created by someone else
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other's Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("DELETE", "/projects/"+testProjectID)
		TestUsers.Owner.SetContext(c)

		server.DeleteProject(c, projectUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		projectStore := newMockProjectStore()
		projectStore.teamID = testTeamID
		projectStore.deleteErr = errors.New("db error")
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("DELETE", "/projects/"+testProjectID)
		TestUsers.Owner.SetContext(c)

		server.DeleteProject(c, projectUUID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, nil, projectStore)
		setupTestTeamAuthDB(t)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("DELETE", "/projects/"+testProjectID)

		server.DeleteProject(c, projectUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
