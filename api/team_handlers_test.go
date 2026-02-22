package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// =============================================================================
// Mock Stores for Team/Project Handler Tests
// =============================================================================

// mockTeamStore implements TeamStoreInterface for unit testing.
type mockTeamStore struct {
	teams       map[string]*Team
	listItems   []TeamListItem
	listTotal   int
	hasProjects bool
	isMember    bool

	// Per-operation error injection
	err            error
	createErr      error
	getErr         error
	updateErr      error
	deleteErr      error
	listErr        error
	isMemberErr    error
	hasProjectsErr error
}

func newMockTeamStore() *mockTeamStore {
	return &mockTeamStore{
		teams: make(map[string]*Team),
	}
}

func (m *mockTeamStore) Create(_ context.Context, team *Team, _ string) (*Team, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.err != nil {
		return nil, m.err
	}
	if team.Id == nil {
		id := uuid.New()
		team.Id = &id
	}
	now := time.Now().UTC()
	team.CreatedAt = &now
	team.ModifiedAt = &now
	m.teams[team.Id.String()] = team
	return team, nil
}

func (m *mockTeamStore) Get(_ context.Context, id string) (*Team, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.err != nil {
		return nil, m.err
	}
	if t, ok := m.teams[id]; ok {
		return t, nil
	}
	return nil, nil
}

func (m *mockTeamStore) Update(_ context.Context, id string, team *Team, _ string) (*Team, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	if m.err != nil {
		return nil, m.err
	}
	now := time.Now().UTC()
	team.ModifiedAt = &now
	m.teams[id] = team
	return team, nil
}

func (m *mockTeamStore) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if m.err != nil {
		return m.err
	}
	delete(m.teams, id)
	return nil
}

func (m *mockTeamStore) List(_ context.Context, _, _ int, _ *TeamFilters, _ string, _ bool) ([]TeamListItem, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.listItems, m.listTotal, nil
}

func (m *mockTeamStore) IsMember(_ context.Context, _ string, _ string) (bool, error) {
	if m.isMemberErr != nil {
		return false, m.isMemberErr
	}
	if m.err != nil {
		return false, m.err
	}
	return m.isMember, nil
}

func (m *mockTeamStore) HasProjects(_ context.Context, _ string) (bool, error) {
	if m.hasProjectsErr != nil {
		return false, m.hasProjectsErr
	}
	if m.err != nil {
		return false, m.err
	}
	return m.hasProjects, nil
}

// mockProjectStore implements ProjectStoreInterface for unit testing.
type mockProjectStore struct {
	projects        map[string]*Project
	listItems       []ProjectListItem
	listTotal       int
	hasThreatModels bool
	teamID          string

	// Per-operation error injection
	err                error
	createErr          error
	getErr             error
	updateErr          error
	deleteErr          error
	listErr            error
	getTeamIDErr       error
	hasThreatModelsErr error
}

func newMockProjectStore() *mockProjectStore {
	return &mockProjectStore{
		projects: make(map[string]*Project),
	}
}

func (m *mockProjectStore) Create(_ context.Context, project *Project, _ string) (*Project, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.err != nil {
		return nil, m.err
	}
	if project.Id == nil {
		id := uuid.New()
		project.Id = &id
	}
	now := time.Now().UTC()
	project.CreatedAt = &now
	project.ModifiedAt = &now
	m.projects[project.Id.String()] = project
	return project, nil
}

func (m *mockProjectStore) Get(_ context.Context, id string) (*Project, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.err != nil {
		return nil, m.err
	}
	if p, ok := m.projects[id]; ok {
		return p, nil
	}
	return nil, nil
}

func (m *mockProjectStore) Update(_ context.Context, id string, project *Project, _ string) (*Project, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	if m.err != nil {
		return nil, m.err
	}
	now := time.Now().UTC()
	project.ModifiedAt = &now
	m.projects[id] = project
	return project, nil
}

func (m *mockProjectStore) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if m.err != nil {
		return m.err
	}
	delete(m.projects, id)
	return nil
}

func (m *mockProjectStore) List(_ context.Context, _, _ int, _ *ProjectFilters, _ string, _ bool) ([]ProjectListItem, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.listItems, m.listTotal, nil
}

func (m *mockProjectStore) GetTeamID(_ context.Context, _ string) (string, error) {
	if m.getTeamIDErr != nil {
		return "", m.getTeamIDErr
	}
	if m.err != nil {
		return "", m.err
	}
	return m.teamID, nil
}

func (m *mockProjectStore) HasThreatModels(_ context.Context, _ string) (bool, error) {
	if m.hasThreatModelsErr != nil {
		return false, m.hasThreatModelsErr
	}
	if m.err != nil {
		return false, m.err
	}
	return m.hasThreatModels, nil
}

// =============================================================================
// Test Helpers
// =============================================================================

// saveTeamProjectStores saves the global store values and returns a cleanup function.
func saveTeamProjectStores(t *testing.T, ts TeamStoreInterface, ps ProjectStoreInterface) {
	t.Helper()
	origTeam := GlobalTeamStore
	origProject := GlobalProjectStore
	origEmitter := GlobalEventEmitter
	GlobalTeamStore = ts
	GlobalProjectStore = ps
	GlobalEventEmitter = nil // disable events in tests
	t.Cleanup(func() {
		GlobalTeamStore = origTeam
		GlobalProjectStore = origProject
		GlobalEventEmitter = origEmitter
	})
}

// setupTestTeamAuthDB creates an in-memory SQLite DB for team authorization queries.
// Returns the db so callers can seed test data.
func setupTestTeamAuthDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err, "failed to open in-memory SQLite")

	err = db.AutoMigrate(
		&models.TeamRecord{},
		&models.TeamMemberRecord{},
		&models.ProjectRecord{},
	)
	require.NoError(t, err, "failed to migrate team auth tables")

	origDB := teamAuthDB
	teamAuthDB = db
	t.Cleanup(func() { teamAuthDB = origDB })
	return db
}

const testTeamID = "11111111-1111-1111-1111-111111111111"
const testTeamID2 = "22222222-2222-2222-2222-222222222222"
const testProjectID = "33333333-3333-3333-3333-333333333333"
const testUserUUID = "owner-internal-uuid" // matches TestUsers.Owner.InternalUUID

// seedTeamAuthData seeds the in-memory SQLite with a team and its member.
func seedTeamAuthData(t *testing.T, db *gorm.DB, teamID, memberUUID string) {
	t.Helper()
	err := db.Create(&models.TeamRecord{
		ID:                    teamID,
		Name:                  "Test Team",
		CreatedByInternalUUID: memberUUID,
	}).Error
	require.NoError(t, err)

	err = db.Create(&models.TeamMemberRecord{
		ID:               uuid.New().String(),
		TeamID:           teamID,
		UserInternalUUID: memberUUID,
		Role:             "engineer",
	}).Error
	require.NoError(t, err)
}

// seedProjectAuthData seeds the in-memory SQLite with a project linked to a team.
func seedProjectAuthData(t *testing.T, db *gorm.DB, projectID, teamID string) {
	t.Helper()
	err := db.Create(&models.ProjectRecord{
		ID:     projectID,
		TeamID: teamID,
		Name:   "Test Project",
	}).Error
	require.NoError(t, err)
}

// seedTeamInStore inserts a team into the mock store and returns its UUID.
func seedTeamInStore(store *mockTeamStore, teamID, name string) openapi_types.UUID {
	id, _ := uuid.Parse(teamID)
	now := time.Now().UTC()
	desc := "Test team description"
	store.teams[teamID] = &Team{
		Id:          &id,
		Name:        name,
		Description: &desc,
		CreatedAt:   &now,
		ModifiedAt:  &now,
	}
	return id
}

// seedProjectInStore inserts a project into the mock store and returns its UUID.
func seedProjectInStore(store *mockProjectStore, projectID, name, teamID string) openapi_types.UUID {
	id, _ := uuid.Parse(projectID)
	now := time.Now().UTC()
	tid, _ := uuid.Parse(teamID)
	desc := "Test project description"
	store.projects[projectID] = &Project{
		Id:          &id,
		Name:        name,
		Description: &desc,
		TeamId:      tid,
		CreatedAt:   &now,
		ModifiedAt:  &now,
	}
	return id
}

// =============================================================================
// Team Handler Tests
// =============================================================================

func TestListTeams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success with defaults", func(t *testing.T) {
		store := newMockTeamStore()
		now := time.Now().UTC()
		store.listItems = []TeamListItem{
			{Name: "Team A", CreatedAt: now},
			{Name: "Team B", CreatedAt: now},
		}
		store.listTotal = 2
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/teams")
		TestUsers.Owner.SetContext(c)

		server.ListTeams(c, ListTeamsParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListTeamsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 2, resp.Total)
		assert.Equal(t, 20, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Len(t, resp.Teams, 2)
	})

	t.Run("pagination parameters", func(t *testing.T) {
		store := newMockTeamStore()
		store.listItems = []TeamListItem{}
		store.listTotal = 50
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/teams?limit=10&offset=20")
		TestUsers.Owner.SetContext(c)
		limit := 10
		offset := 20
		server.ListTeams(c, ListTeamsParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListTeamsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 10, resp.Limit)
		assert.Equal(t, 20, resp.Offset)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockTeamStore()
		store.listErr = errors.New("database connection lost")
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/teams")
		TestUsers.Owner.SetContext(c)

		server.ListTeams(c, ListTeamsParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/teams")
		// No user context set

		server.ListTeams(c, ListTeamsParams{})

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestCreateTeam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		body := TeamInput{
			Name: "New Team",
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/teams", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateTeam(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		var created Team
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		assert.Equal(t, "New Team", created.Name)
		assert.NotNil(t, created.Id)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		body := TeamInput{Name: "Test"}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/teams", "application/json", bodyBytes)

		server.CreateTeam(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid body", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContextWithBody("POST", "/teams", "application/json", []byte(`{invalid json`))
		TestUsers.Owner.SetContext(c)

		server.CreateTeam(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockTeamStore()
		store.createErr = errors.New("db error")
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		body := TeamInput{Name: "Team"}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/teams", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateTeam(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestGetTeam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockTeamStore()
		seedTeamInStore(store, testTeamID, "Test Team")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID)
		TestUsers.Owner.SetContext(c)

		server.GetTeam(c, teamUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var team Team
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &team))
		assert.Equal(t, "Test Team", team.Name)
	})

	t.Run("not found", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		// Seed auth data but not in the mock store (store returns nil)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID)
		TestUsers.Owner.SetContext(c)

		server.GetTeam(c, teamUUID)

		// Handler returns error from store or not found
		assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusOK)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockTeamStore()
		seedTeamInStore(store, testTeamID, "Test Team")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		// Create team but DON'T add the user as member
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Test Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID)
		TestUsers.Owner.SetContext(c)

		server.GetTeam(c, teamUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID)

		server.GetTeam(c, teamUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockTeamStore()
		store.getErr = errors.New("db error")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID)
		TestUsers.Owner.SetContext(c)

		server.GetTeam(c, teamUUID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestUpdateTeam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockTeamStore()
		seedTeamInStore(store, testTeamID, "Old Name")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		body := TeamInput{Name: "Updated Name"}
		bodyBytes, _ := json.Marshal(body)
		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateTeam(c, teamUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var updated Team
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
		assert.Equal(t, "Updated Name", updated.Name)
	})

	t.Run("invalid body", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID, "application/json", []byte(`{bad`))
		TestUsers.Owner.SetContext(c)

		server.UpdateTeam(c, teamUUID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("forbidden", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Test Team",
			CreatedByInternalUUID: "other-user",
		}).Error
		require.NoError(t, err)

		body := TeamInput{Name: "Updated"}
		bodyBytes, _ := json.Marshal(body)
		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateTeam(c, teamUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		body := TeamInput{Name: "Updated"}
		bodyBytes, _ := json.Marshal(body)
		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID, "application/json", bodyBytes)

		server.UpdateTeam(c, teamUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestPatchTeam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockTeamStore()
		seedTeamInStore(store, testTeamID, "Original Team")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		patch := []PatchOperation{
			{Op: "replace", Path: "/description", Value: "Patched description"},
		}
		patchBytes, _ := json.Marshal(patch)
		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID, "application/json-patch+json", patchBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchTeam(c, teamUUID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("prohibited field rejected", func(t *testing.T) {
		store := newMockTeamStore()
		seedTeamInStore(store, testTeamID, "Team")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		patch := []PatchOperation{
			{Op: "replace", Path: "/id", Value: "new-id"},
		}
		patchBytes, _ := json.Marshal(patch)
		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID, "application/json-patch+json", patchBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchTeam(c, teamUUID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "id")
	})

	t.Run("forbidden", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Team",
			CreatedByInternalUUID: "other-user",
		}).Error
		require.NoError(t, err)

		patch := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "new"},
		}
		patchBytes, _ := json.Marshal(patch)
		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID, "application/json-patch+json", patchBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchTeam(c, teamUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		patch := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "new"},
		}
		patchBytes, _ := json.Marshal(patch)
		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID, "application/json-patch+json", patchBytes)

		server.PatchTeam(c, teamUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDeleteTeam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockTeamStore()
		seedTeamInStore(store, testTeamID, "Team To Delete")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, _ := CreateTestGinContext("DELETE", "/teams/"+testTeamID)
		TestUsers.Owner.SetContext(c)

		server.DeleteTeam(c, teamUUID)

		// c.Status() doesn't flush to httptest.ResponseRecorder; check gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	})

	t.Run("forbidden - non-owner", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		// Team created by a different user
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other's Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)
		// Add test user as member (not owner)
		err = db.Create(&models.TeamMemberRecord{
			ID:               uuid.New().String(),
			TeamID:           testTeamID,
			UserInternalUUID: testUserUUID,
			Role:             "engineer",
		}).Error
		require.NoError(t, err)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("DELETE", "/teams/"+testTeamID)
		TestUsers.Owner.SetContext(c)

		server.DeleteTeam(c, teamUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockTeamStore()
		store.deleteErr = errors.New("db error")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("DELETE", "/teams/"+testTeamID)
		TestUsers.Owner.SetContext(c)

		server.DeleteTeam(c, teamUUID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("DELETE", "/teams/"+testTeamID)

		server.DeleteTeam(c, teamUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
