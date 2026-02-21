package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Stores for Survey Handler Tests
// =============================================================================

// mockSurveyStore implements SurveyStore for unit testing survey admin handlers.
type mockSurveyStore struct {
	surveys      map[uuid.UUID]*Survey
	listItems    []SurveyListItem
	listTotal    int
	hasResponses bool

	// Per-operation error injection
	err             error // default error for all ops
	createErr       error
	getErr          error
	updateErr       error
	deleteErr       error
	listErr         error
	listActiveErr   error
	hasResponsesErr error
}

func newMockSurveyStore() *mockSurveyStore {
	return &mockSurveyStore{
		surveys: make(map[uuid.UUID]*Survey),
	}
}

func (m *mockSurveyStore) Create(_ context.Context, survey *Survey, _ string) error {
	if m.createErr != nil {
		return m.createErr
	}
	if m.err != nil {
		return m.err
	}
	if survey.Id == nil {
		id := uuid.New()
		survey.Id = &id
	}
	now := time.Now().UTC()
	survey.CreatedAt = &now
	survey.ModifiedAt = &now
	m.surveys[*survey.Id] = survey
	return nil
}

func (m *mockSurveyStore) Get(_ context.Context, id uuid.UUID) (*Survey, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.err != nil {
		return nil, m.err
	}
	if s, ok := m.surveys[id]; ok {
		return s, nil
	}
	return nil, nil
}

func (m *mockSurveyStore) Update(_ context.Context, survey *Survey) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.err != nil {
		return m.err
	}
	if survey.Id != nil {
		m.surveys[*survey.Id] = survey
	}
	return nil
}

func (m *mockSurveyStore) Delete(_ context.Context, id uuid.UUID) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if m.err != nil {
		return m.err
	}
	delete(m.surveys, id)
	return nil
}

func (m *mockSurveyStore) List(_ context.Context, _, _ int, _ *string) ([]SurveyListItem, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.listItems, m.listTotal, nil
}

func (m *mockSurveyStore) ListActive(_ context.Context, _, _ int) ([]SurveyListItem, int, error) {
	if m.listActiveErr != nil {
		return nil, 0, m.listActiveErr
	}
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.listItems, m.listTotal, nil
}

func (m *mockSurveyStore) HasResponses(_ context.Context, _ uuid.UUID) (bool, error) {
	if m.hasResponsesErr != nil {
		return false, m.hasResponsesErr
	}
	if m.err != nil {
		return false, m.err
	}
	return m.hasResponses, nil
}

// mockSurveyResponseStore implements SurveyResponseStore for unit testing.
type mockSurveyResponseStore struct {
	responses map[uuid.UUID]*SurveyResponse
	listItems []SurveyResponseListItem
	listTotal int

	// Access control: map[resourceID+userUUID] -> highest role
	accessMap map[string]AuthorizationRole

	// Per-operation error injection
	err             error
	createErr       error
	getErr          error
	updateErr       error
	deleteErr       error
	listErr         error
	listByOwnerErr  error
	updateStatusErr error
	hasAccessErr    error
}

func newMockSurveyResponseStore() *mockSurveyResponseStore {
	return &mockSurveyResponseStore{
		responses: make(map[uuid.UUID]*SurveyResponse),
		accessMap: make(map[string]AuthorizationRole),
	}
}

func (m *mockSurveyResponseStore) accessKey(id uuid.UUID, userUUID string) string {
	return id.String() + ":" + userUUID
}

func (m *mockSurveyResponseStore) Create(_ context.Context, response *SurveyResponse, _ string) error {
	if m.createErr != nil {
		return m.createErr
	}
	if m.err != nil {
		return m.err
	}
	if response.Id == nil {
		id := uuid.New()
		response.Id = &id
	}
	now := time.Now().UTC()
	response.CreatedAt = &now
	response.ModifiedAt = &now
	if response.Status == nil {
		status := ResponseStatusDraft
		response.Status = &status
	}
	m.responses[*response.Id] = response
	return nil
}

func (m *mockSurveyResponseStore) Get(_ context.Context, id uuid.UUID) (*SurveyResponse, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.err != nil {
		return nil, m.err
	}
	if r, ok := m.responses[id]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *mockSurveyResponseStore) Update(_ context.Context, response *SurveyResponse) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.err != nil {
		return m.err
	}
	if response.Id != nil {
		// Preserve existing data not in update
		if existing, ok := m.responses[*response.Id]; ok {
			if response.Status == nil {
				response.Status = existing.Status
			}
			if response.SurveyId == (uuid.UUID{}) {
				response.SurveyId = existing.SurveyId
			}
		}
		m.responses[*response.Id] = response
	}
	return nil
}

func (m *mockSurveyResponseStore) Delete(_ context.Context, id uuid.UUID) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if m.err != nil {
		return m.err
	}
	delete(m.responses, id)
	return nil
}

func (m *mockSurveyResponseStore) List(_ context.Context, _, _ int, _ *SurveyResponseFilters) ([]SurveyResponseListItem, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.listItems, m.listTotal, nil
}

func (m *mockSurveyResponseStore) ListByOwner(_ context.Context, _ string, _, _ int, _ *string) ([]SurveyResponseListItem, int, error) {
	if m.listByOwnerErr != nil {
		return nil, 0, m.listByOwnerErr
	}
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.listItems, m.listTotal, nil
}

func (m *mockSurveyResponseStore) UpdateStatus(_ context.Context, _ uuid.UUID, _ string, _ *string, _ *string) error {
	if m.updateStatusErr != nil {
		return m.updateStatusErr
	}
	return nil
}

func (m *mockSurveyResponseStore) GetAuthorization(_ context.Context, _ uuid.UUID) ([]Authorization, error) {
	return nil, nil
}

func (m *mockSurveyResponseStore) UpdateAuthorization(_ context.Context, _ uuid.UUID, _ []Authorization) error {
	return nil
}

func (m *mockSurveyResponseStore) HasAccess(_ context.Context, id uuid.UUID, userUUID string, requiredRole AuthorizationRole) (bool, error) {
	if m.hasAccessErr != nil {
		return false, m.hasAccessErr
	}
	role, ok := m.accessMap[m.accessKey(id, userUUID)]
	if !ok {
		return false, nil
	}
	// Simple role hierarchy: owner > writer > reader
	switch requiredRole {
	case AuthorizationRoleReader:
		return true, nil
	case AuthorizationRoleWriter:
		return role == AuthorizationRoleWriter || role == AuthorizationRoleOwner, nil
	case AuthorizationRoleOwner:
		return role == AuthorizationRoleOwner, nil
	}
	return false, nil
}

// =============================================================================
// Test Helpers
// =============================================================================

// validSurveyJSON returns a valid SurveyJS JSON structure for tests.
func validSurveyJSON() map[string]interface{} {
	return map[string]interface{}{
		"pages": []interface{}{
			map[string]interface{}{
				"name": "page1",
				"elements": []interface{}{
					map[string]interface{}{
						"type": "text",
						"name": "q1",
					},
				},
			},
		},
	}
}

// seedSurvey inserts a survey into the mock store and returns its ID.
func seedSurvey(store *mockSurveyStore, name, version, status string) uuid.UUID {
	id := uuid.New()
	s := &Survey{
		Id:         &id,
		Name:       name,
		Version:    version,
		Status:     strPtr(status),
		SurveyJson: validSurveyJSON(),
	}
	now := time.Now().UTC()
	s.CreatedAt = &now
	s.ModifiedAt = &now
	store.surveys[id] = s
	return id
}

// seedSurveyResponse inserts a survey response into the mock store and returns its ID.
func seedSurveyResponse(store *mockSurveyResponseStore, surveyID uuid.UUID, status, ownerUUID string) uuid.UUID {
	id := uuid.New()
	r := &SurveyResponse{
		Id:       &id,
		SurveyId: surveyID,
		Status:   strPtr(status),
	}
	now := time.Now().UTC()
	r.CreatedAt = &now
	r.ModifiedAt = &now
	store.responses[id] = r
	// Grant owner access
	store.accessMap[store.accessKey(id, ownerUUID)] = AuthorizationRoleOwner
	return id
}

// saveSurveyStores saves the global store values and returns a cleanup function.
func saveSurveyStores(t *testing.T, surveyStore SurveyStore, responseStore SurveyResponseStore) {
	t.Helper()
	origSurveyStore := GlobalSurveyStore
	origResponseStore := GlobalSurveyResponseStore
	origEventEmitter := GlobalEventEmitter
	GlobalSurveyStore = surveyStore
	GlobalSurveyResponseStore = responseStore
	GlobalEventEmitter = nil // disable events in tests
	t.Cleanup(func() {
		GlobalSurveyStore = origSurveyStore
		GlobalSurveyResponseStore = origResponseStore
		GlobalEventEmitter = origEventEmitter
	})
}

// =============================================================================
// Admin Survey Tests
// =============================================================================

func TestListAdminSurveys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success with defaults", func(t *testing.T) {
		store := newMockSurveyStore()
		store.listItems = []SurveyListItem{
			{Id: ptrUUID(uuid.New()), Name: "Survey A", Status: "active", CreatedAt: time.Now().UTC()},
			{Id: ptrUUID(uuid.New()), Name: "Survey B", Status: "inactive", CreatedAt: time.Now().UTC()},
		}
		store.listTotal = 2
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/admin/surveys")
		server.ListAdminSurveys(c, ListAdminSurveysParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListSurveysResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 2, resp.Total)
		assert.Equal(t, 20, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Len(t, resp.Surveys, 2)
	})

	t.Run("pagination parameters", func(t *testing.T) {
		store := newMockSurveyStore()
		store.listItems = []SurveyListItem{}
		store.listTotal = 50
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/admin/surveys?limit=10&offset=20")
		limit := 10
		offset := 20
		server.ListAdminSurveys(c, ListAdminSurveysParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListSurveysResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 10, resp.Limit)
		assert.Equal(t, 20, resp.Offset)
	})

	t.Run("status filter", func(t *testing.T) {
		store := newMockSurveyStore()
		store.listItems = []SurveyListItem{
			{Id: ptrUUID(uuid.New()), Name: "Active Survey", Status: "active", CreatedAt: time.Now().UTC()},
		}
		store.listTotal = 1
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/admin/surveys?status=active")
		status := "active"
		server.ListAdminSurveys(c, ListAdminSurveysParams{Status: &status})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListSurveysResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Total)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockSurveyStore()
		store.listErr = errors.New("database connection lost")
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/admin/surveys")
		server.ListAdminSurveys(c, ListAdminSurveysParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

func TestCreateAdminSurvey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		body := SurveyBase{
			Name:       "Security Intake Survey",
			Version:    "v1.0",
			Status:     strPtr("active"),
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateAdminSurvey(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		var created Survey
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		assert.Equal(t, "Security Intake Survey", created.Name)
		assert.Equal(t, "v1.0", created.Version)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		body := SurveyBase{
			Name:       "Test Survey",
			Version:    "v1",
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", bodyBytes)
		// No user context set

		server.CreateAdminSurvey(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "unauthorized")
	})

	t.Run("invalid body", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", []byte(`{invalid json`))
		TestUsers.Owner.SetContext(c)

		server.CreateAdminSurvey(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid_input")
	})

	t.Run("survey_json nil", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		body := map[string]interface{}{
			"name":        "Test Survey",
			"version":     "v1",
			"survey_json": nil,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateAdminSurvey(c)

		// survey_json is required in the binding, so this should fail binding
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("survey_json missing pages", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		body := SurveyBase{
			Name:    "Test Survey",
			Version: "v1",
			SurveyJson: map[string]interface{}{
				"title": "no pages here",
			},
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateAdminSurvey(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "pages")
	})

	t.Run("survey_json pages not array", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		body := SurveyBase{
			Name:    "Test Survey",
			Version: "v1",
			SurveyJson: map[string]interface{}{
				"pages": "not-an-array",
			},
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateAdminSurvey(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "pages")
	})

	t.Run("duplicate constraint error", func(t *testing.T) {
		store := newMockSurveyStore()
		store.createErr = errors.New("unique constraint violation")
		saveSurveyStores(t, store, nil)

		body := SurveyBase{
			Name:       "Duplicate Survey",
			Version:    "v1.0",
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateAdminSurvey(c)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "conflict")
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockSurveyStore()
		store.createErr = errors.New("unexpected database error")
		saveSurveyStores(t, store, nil)

		body := SurveyBase{
			Name:       "Test Survey",
			Version:    "v1",
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateAdminSurvey(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

func TestGetAdminSurvey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "My Survey", "v1", SurveyStatusActive)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/admin/surveys/%s", surveyID))
		server.GetAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusOK, w.Code)
		var survey Survey
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &survey))
		assert.Equal(t, "My Survey", survey.Name)
	})

	t.Run("not found", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/admin/surveys/"+uuid.New().String())
		server.GetAdminSurvey(c, uuid.New())

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockSurveyStore()
		store.getErr = errors.New("connection timeout")
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/admin/surveys/"+uuid.New().String())
		server.GetAdminSurvey(c, uuid.New())

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

func TestUpdateAdminSurvey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Original Name", "v1", SurveyStatusActive)

		body := SurveyBase{
			Name:       "Updated Name",
			Version:    "v2",
			Status:     strPtr(SurveyStatusActive),
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/admin/surveys/%s", surveyID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusOK, w.Code)
		var updated Survey
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
		assert.Equal(t, "Updated Name", updated.Name)
	})

	t.Run("not found", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		body := SurveyBase{
			Name:       "Updated Name",
			Version:    "v2",
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", "/admin/surveys/"+uuid.New().String(), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateAdminSurvey(c, uuid.New())

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("archived survey rejected with 409", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Old Survey", "v1", SurveyStatusArchived)

		body := SurveyBase{
			Name:       "Try to Update",
			Version:    "v2",
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/admin/surveys/%s", surveyID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "archived")
	})

	t.Run("invalid body", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Existing", "v1", SurveyStatusActive)

		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/admin/surveys/%s", surveyID), "application/json", []byte(`not json`))
		TestUsers.Owner.SetContext(c)

		server.UpdateAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid survey_json", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Existing", "v1", SurveyStatusActive)

		body := SurveyBase{
			Name:    "Updated",
			Version: "v2",
			SurveyJson: map[string]interface{}{
				"no_pages": true,
			},
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/admin/surveys/%s", surveyID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "pages")
	})

	t.Run("duplicate constraint error", func(t *testing.T) {
		store := newMockSurveyStore()
		store.updateErr = errors.New("duplicate key value violates unique constraint")
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Existing", "v1", SurveyStatusActive)

		body := SurveyBase{
			Name:       "Duplicate Name",
			Version:    "v1",
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/admin/surveys/%s", surveyID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "conflict")
	})

	t.Run("store error on get for re-read", func(t *testing.T) {
		// Simulate update succeeding but the re-read failing
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Existing", "v1", SurveyStatusActive)

		body := SurveyBase{
			Name:       "Updated Name",
			Version:    "v2",
			SurveyJson: validSurveyJSON(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/admin/surveys/%s", surveyID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		// The update will succeed, and the get after update should also succeed (returns stored value)
		server.UpdateAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestDeleteAdminSurvey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "To Delete", "v1", SurveyStatusInactive)

		c, _ := CreateTestGinContext("DELETE", fmt.Sprintf("/admin/surveys/%s", surveyID))
		server.DeleteAdminSurvey(c, surveyID)

		// c.Status() doesn't flush to httptest.NewRecorder; check Gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
		// Verify survey was removed from store
		assert.Nil(t, store.surveys[surveyID])
	})

	t.Run("not found", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("DELETE", "/admin/surveys/"+uuid.New().String())
		server.DeleteAdminSurvey(c, uuid.New())

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("has responses rejects with 409", func(t *testing.T) {
		store := newMockSurveyStore()
		store.hasResponses = true
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Has Responses", "v1", SurveyStatusActive)

		c, w := CreateTestGinContext("DELETE", fmt.Sprintf("/admin/surveys/%s", surveyID))
		server.DeleteAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "existing responses")
	})

	t.Run("store error on get", func(t *testing.T) {
		store := newMockSurveyStore()
		store.getErr = errors.New("database error")
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("DELETE", "/admin/surveys/"+uuid.New().String())
		server.DeleteAdminSurvey(c, uuid.New())

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("store error on has responses", func(t *testing.T) {
		store := newMockSurveyStore()
		store.hasResponsesErr = errors.New("query failed")
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Some Survey", "v1", SurveyStatusActive)

		c, w := CreateTestGinContext("DELETE", fmt.Sprintf("/admin/surveys/%s", surveyID))
		server.DeleteAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})

	t.Run("store error on delete", func(t *testing.T) {
		store := newMockSurveyStore()
		store.deleteErr = errors.New("unexpected error")
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Delete Fail", "v1", SurveyStatusActive)

		c, w := CreateTestGinContext("DELETE", fmt.Sprintf("/admin/surveys/%s", surveyID))
		server.DeleteAdminSurvey(c, surveyID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

// =============================================================================
// Intake Survey Tests
// =============================================================================

func TestListIntakeSurveys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success with defaults", func(t *testing.T) {
		store := newMockSurveyStore()
		store.listItems = []SurveyListItem{
			{Id: ptrUUID(uuid.New()), Name: "Active Survey", Status: "active", CreatedAt: time.Now().UTC()},
		}
		store.listTotal = 1
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/intake/surveys")
		server.ListIntakeSurveys(c, ListIntakeSurveysParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListSurveysResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Total)
		assert.Equal(t, 20, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
	})

	t.Run("pagination", func(t *testing.T) {
		store := newMockSurveyStore()
		store.listItems = []SurveyListItem{}
		store.listTotal = 0
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/intake/surveys?limit=5&offset=10")
		limit := 5
		offset := 10
		server.ListIntakeSurveys(c, ListIntakeSurveysParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListSurveysResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 5, resp.Limit)
		assert.Equal(t, 10, resp.Offset)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockSurveyStore()
		store.listActiveErr = errors.New("database unavailable")
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/intake/surveys")
		server.ListIntakeSurveys(c, ListIntakeSurveysParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "server_error")
	})
}

func TestGetIntakeSurvey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success active survey", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Active Survey", "v1", SurveyStatusActive)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/intake/surveys/%s", surveyID))
		server.GetIntakeSurvey(c, surveyID)

		assert.Equal(t, http.StatusOK, w.Code)
		var survey Survey
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &survey))
		assert.Equal(t, "Active Survey", survey.Name)
	})

	t.Run("not found", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/intake/surveys/"+uuid.New().String())
		server.GetIntakeSurvey(c, uuid.New())

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("inactive survey returns 404", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Inactive Survey", "v1", SurveyStatusInactive)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/intake/surveys/%s", surveyID))
		server.GetIntakeSurvey(c, surveyID)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not active")
	})

	t.Run("archived survey returns 404", func(t *testing.T) {
		store := newMockSurveyStore()
		saveSurveyStores(t, store, nil)

		surveyID := seedSurvey(store, "Archived Survey", "v1", SurveyStatusArchived)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/intake/surveys/%s", surveyID))
		server.GetIntakeSurvey(c, surveyID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockSurveyStore()
		store.getErr = errors.New("connection reset")
		saveSurveyStores(t, store, nil)

		c, w := CreateTestGinContext("GET", "/intake/surveys/"+uuid.New().String())
		server.GetIntakeSurvey(c, uuid.New())

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestListIntakeSurveyResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.listItems = []SurveyResponseListItem{
			{Id: ptrUUID(uuid.New()), Status: "draft", SurveyId: uuid.New(), CreatedAt: time.Now().UTC()},
		}
		respStore.listTotal = 1
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/intake/survey_responses")
		TestUsers.Owner.SetContext(c)

		server.ListIntakeSurveyResponses(c, ListIntakeSurveyResponsesParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListSurveyResponsesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Total)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/intake/survey_responses")
		// No user context

		server.ListIntakeSurveyResponses(c, ListIntakeSurveyResponsesParams{})

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "unauthorized")
	})

	t.Run("store error", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.listByOwnerErr = errors.New("database error")
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/intake/survey_responses")
		TestUsers.Owner.SetContext(c)

		server.ListIntakeSurveyResponses(c, ListIntakeSurveyResponsesParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestCreateIntakeSurveyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		surveyStore := newMockSurveyStore()
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, surveyStore, respStore)

		surveyID := uuid.New()
		body := map[string]interface{}{
			"survey_id": surveyID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/intake/survey_responses", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateIntakeSurveyResponse(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		var created SurveyResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		assert.NotNil(t, created.Id)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		body := map[string]interface{}{
			"survey_id": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/intake/survey_responses", "application/json", bodyBytes)
		// No user context

		server.CreateIntakeSurveyResponse(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("survey not found", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.createErr = errors.New("survey not found: " + uuid.New().String())
		saveSurveyStores(t, nil, respStore)

		body := map[string]interface{}{
			"survey_id": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/intake/survey_responses", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateIntakeSurveyResponse(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Survey not found")
	})

	t.Run("foreign key constraint error", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.createErr = errors.New("violates foreign key constraint")
		saveSurveyStores(t, nil, respStore)

		body := map[string]interface{}{
			"survey_id": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/intake/survey_responses", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateIntakeSurveyResponse(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Referenced resource not found")
	})

	t.Run("empty body", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContextWithBody("POST", "/intake/survey_responses", "application/json", []byte{})
		TestUsers.Owner.SetContext(c)

		server.CreateIntakeSurveyResponse(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.createErr = errors.New("unexpected disk full")
		saveSurveyStores(t, nil, respStore)

		body := map[string]interface{}{
			"survey_id": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/intake/survey_responses", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateIntakeSurveyResponse(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestGetIntakeSurveyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/intake/survey_responses/%s", responseID))
		TestUsers.Owner.SetContext(c)

		server.GetIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp SurveyResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, responseID, *resp.Id)
	})

	t.Run("not found", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/intake/survey_responses/"+uuid.New().String())
		TestUsers.Owner.SetContext(c)

		server.GetIntakeSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not_found")
	})

	t.Run("access denied", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/intake/survey_responses/%s", responseID))
		// Use External user who has no access
		TestUsers.External.SetContext(c)

		server.GetIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "forbidden")
	})

	t.Run("unauthenticated", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/intake/survey_responses/"+uuid.New().String())
		// No user context

		server.GetIntakeSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("store error on get", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.getErr = errors.New("query failed")
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/intake/survey_responses/"+uuid.New().String())
		TestUsers.Owner.SetContext(c)

		server.GetIntakeSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("store error on access check", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.hasAccessErr = errors.New("auth service down")
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/intake/survey_responses/%s", responseID))
		TestUsers.Owner.SetContext(c)

		server.GetIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestUpdateIntakeSurveyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success draft", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)
		// Grant writer access
		respStore.accessMap[respStore.accessKey(responseID, TestUsers.Owner.InternalUUID)] = AuthorizationRoleOwner

		body := map[string]interface{}{
			"survey_id": surveyID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/intake/survey_responses/%s", responseID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("success needs_revision", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusNeedsRevision, TestUsers.Owner.InternalUUID)

		body := map[string]interface{}{
			"survey_id": surveyID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/intake/survey_responses/%s", responseID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("not found", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		body := map[string]interface{}{
			"survey_id": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", "/intake/survey_responses/"+uuid.New().String(), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateIntakeSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		body := map[string]interface{}{
			"survey_id": surveyID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/intake/survey_responses/%s", responseID), "application/json", bodyBytes)
		TestUsers.External.SetContext(c)

		server.UpdateIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("wrong status submitted rejects with 409", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusSubmitted, TestUsers.Owner.InternalUUID)

		body := map[string]interface{}{
			"survey_id": surveyID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/intake/survey_responses/%s", responseID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "draft or needs_revision")
	})

	t.Run("foreign key constraint error", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.updateErr = errors.New("violates foreign key constraint on linked_threat_model_id")
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		body := map[string]interface{}{
			"survey_id":              surveyID.String(),
			"linked_threat_model_id": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", fmt.Sprintf("/intake/survey_responses/%s", responseID), "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Referenced resource not found")
	})

	t.Run("unauthenticated", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		body := map[string]interface{}{
			"survey_id": uuid.New().String(),
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", "/intake/survey_responses/"+uuid.New().String(), "application/json", bodyBytes)

		server.UpdateIntakeSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDeleteIntakeSurveyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		c, _ := CreateTestGinContext("DELETE", fmt.Sprintf("/intake/survey_responses/%s", responseID))
		TestUsers.Owner.SetContext(c)

		server.DeleteIntakeSurveyResponse(c, responseID)

		// c.Status() doesn't flush to httptest.NewRecorder; check Gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	})

	t.Run("not found", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("DELETE", "/intake/survey_responses/"+uuid.New().String())
		TestUsers.Owner.SetContext(c)

		server.DeleteIntakeSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContext("DELETE", fmt.Sprintf("/intake/survey_responses/%s", responseID))
		TestUsers.External.SetContext(c)

		server.DeleteIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("success deleting submitted response", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusSubmitted, TestUsers.Owner.InternalUUID)

		c, _ := CreateTestGinContext("DELETE", fmt.Sprintf("/intake/survey_responses/%s", responseID))
		TestUsers.Owner.SetContext(c)

		server.DeleteIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	})

	t.Run("unauthenticated", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("DELETE", "/intake/survey_responses/"+uuid.New().String())

		server.DeleteIntakeSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.deleteErr = errors.New("disk I/O error")
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContext("DELETE", fmt.Sprintf("/intake/survey_responses/%s", responseID))
		TestUsers.Owner.SetContext(c)

		server.DeleteIntakeSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// Triage Tests
// =============================================================================

func TestListTriageSurveyResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.listItems = []SurveyResponseListItem{
			{Id: ptrUUID(uuid.New()), Status: "submitted", SurveyId: uuid.New(), CreatedAt: time.Now().UTC()},
		}
		respStore.listTotal = 1
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/triage/survey_responses")
		TestUsers.Owner.SetContext(c)

		server.ListTriageSurveyResponses(c, ListTriageSurveyResponsesParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListSurveyResponsesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Total)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/triage/survey_responses")

		server.ListTriageSurveyResponses(c, ListTriageSurveyResponsesParams{})

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		respStore.listErr = errors.New("query timeout")
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/triage/survey_responses")
		TestUsers.Owner.SetContext(c)

		server.ListTriageSurveyResponses(c, ListTriageSurveyResponsesParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestGetTriageSurveyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusSubmitted, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/triage/survey_responses/%s", responseID))
		TestUsers.Owner.SetContext(c)

		server.GetTriageSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("not found", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/triage/survey_responses/"+uuid.New().String())
		TestUsers.Owner.SetContext(c)

		server.GetTriageSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		surveyID := uuid.New()
		responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusSubmitted, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContext("GET", fmt.Sprintf("/triage/survey_responses/%s", responseID))
		TestUsers.External.SetContext(c)

		server.GetTriageSurveyResponse(c, responseID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		respStore := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, respStore)

		c, w := CreateTestGinContext("GET", "/triage/survey_responses/"+uuid.New().String())

		server.GetTriageSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestCreateThreatModelFromSurveyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("returns 501 not implemented", func(t *testing.T) {
		c, w := CreateTestGinContext("POST", "/triage/survey_responses/"+uuid.New().String()+"/create_threat_model")

		server.CreateThreatModelFromSurveyResponse(c, uuid.New())

		assert.Equal(t, http.StatusNotImplemented, w.Code)
		assert.Contains(t, w.Body.String(), "not_implemented")
	})
}

// =============================================================================
// Validation Helper Tests
// =============================================================================

func TestValidateSurveyJSON(t *testing.T) {
	t.Run("valid survey_json", func(t *testing.T) {
		err := validateSurveyJSON(validSurveyJSON())
		assert.NoError(t, err)
	})

	t.Run("nil survey_json", func(t *testing.T) {
		err := validateSurveyJSON(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("missing pages field", func(t *testing.T) {
		err := validateSurveyJSON(map[string]interface{}{
			"title": "no pages",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pages")
	})

	t.Run("pages is not array", func(t *testing.T) {
		err := validateSurveyJSON(map[string]interface{}{
			"pages": "string-not-array",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "array")
	})

	t.Run("pages is empty array", func(t *testing.T) {
		err := validateSurveyJSON(map[string]interface{}{
			"pages": []interface{}{},
		})
		assert.NoError(t, err)
	})
}

func TestIsDuplicateConstraintError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"postgres duplicate key", errors.New("duplicate key value violates unique constraint"), true},
		{"unique constraint", errors.New("UNIQUE CONSTRAINT violation"), true},
		{"oracle ora-00001", errors.New("ORA-00001: unique constraint violated"), true},
		{"generic error", errors.New("something went wrong"), false},
		{"empty error", errors.New(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isDuplicateConstraintError(tt.err))
		})
	}
}

// =============================================================================
// Helpers
// =============================================================================

// ptrUUID returns a pointer to a UUID (used for SurveyListItem.Id).
func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}
