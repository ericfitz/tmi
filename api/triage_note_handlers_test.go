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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Stores for Triage Note Handler Tests
// =============================================================================

// mockTriageNoteStore implements TriageNoteStore for testing using in-memory maps.
// It supports independent error simulation for each operation via named error fields.
type mockTriageNoteStore struct {
	// notes stores triage notes keyed by "surveyResponseID:noteID"
	notes map[string]TriageNote
	// nextID tracks the next auto-increment ID per survey response
	nextID map[string]int
	// err simulates store-level errors for Create, Get, and List when set
	err error
	// countErr simulates errors only for Count (allows testing Count fallback independently)
	countErr error
}

func newMockTriageNoteStore() *mockTriageNoteStore {
	return &mockTriageNoteStore{
		notes:  make(map[string]TriageNote),
		nextID: make(map[string]int),
	}
}

func (m *mockTriageNoteStore) noteKey(surveyResponseID string, noteID int) string {
	return fmt.Sprintf("%s:%d", surveyResponseID, noteID)
}

func (m *mockTriageNoteStore) Create(_ context.Context, note *TriageNote, surveyResponseID string, _ string) error {
	if m.err != nil {
		return m.err
	}
	if _, ok := m.nextID[surveyResponseID]; !ok {
		m.nextID[surveyResponseID] = 1
	}
	id := m.nextID[surveyResponseID]
	m.nextID[surveyResponseID] = id + 1
	note.Id = &id
	now := time.Now().UTC()
	note.CreatedAt = &now
	note.ModifiedAt = &now
	m.notes[m.noteKey(surveyResponseID, id)] = *note
	return nil
}

func (m *mockTriageNoteStore) Get(_ context.Context, surveyResponseID string, noteID int) (*TriageNote, error) {
	if m.err != nil {
		return nil, m.err
	}
	if note, ok := m.notes[m.noteKey(surveyResponseID, noteID)]; ok {
		return &note, nil
	}
	return nil, errors.New("not found")
}

func (m *mockTriageNoteStore) List(_ context.Context, surveyResponseID string, offset, limit int) ([]TriageNote, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []TriageNote
	for key, note := range m.notes {
		// Match notes belonging to this survey response
		if len(key) > len(surveyResponseID) && key[:len(surveyResponseID)] == surveyResponseID {
			result = append(result, note)
		}
	}
	if offset > len(result) {
		return []TriageNote{}, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

func (m *mockTriageNoteStore) Count(_ context.Context, surveyResponseID string) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	if m.err != nil {
		return 0, m.err
	}
	count := 0
	for key := range m.notes {
		if len(key) > len(surveyResponseID) && key[:len(surveyResponseID)] == surveyResponseID {
			count++
		}
	}
	return count, nil
}

// mockSurveyResponseStoreForTriageNotes is a minimal mock of SurveyResponseStore
// used only to satisfy the verifySurveyResponseExists check in triage note handlers.
type mockSurveyResponseStoreForTriageNotes struct {
	responses map[uuid.UUID]*SurveyResponse
	err       error
}

func newMockSurveyResponseStoreForTriageNotes() *mockSurveyResponseStoreForTriageNotes {
	return &mockSurveyResponseStoreForTriageNotes{
		responses: make(map[uuid.UUID]*SurveyResponse),
	}
}

func (m *mockSurveyResponseStoreForTriageNotes) Create(_ context.Context, _ *SurveyResponse, _ string) error {
	return nil
}

func (m *mockSurveyResponseStoreForTriageNotes) Get(_ context.Context, id uuid.UUID) (*SurveyResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if resp, ok := m.responses[id]; ok {
		return resp, nil
	}
	return nil, nil
}

func (m *mockSurveyResponseStoreForTriageNotes) Update(_ context.Context, _ *SurveyResponse) error {
	return nil
}

func (m *mockSurveyResponseStoreForTriageNotes) Delete(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockSurveyResponseStoreForTriageNotes) List(_ context.Context, _, _ int, _ *SurveyResponseFilters) ([]SurveyResponseListItem, int, error) {
	return nil, 0, nil
}

func (m *mockSurveyResponseStoreForTriageNotes) ListByOwner(_ context.Context, _ string, _, _ int, _ *string) ([]SurveyResponseListItem, int, error) {
	return nil, 0, nil
}

func (m *mockSurveyResponseStoreForTriageNotes) UpdateStatus(_ context.Context, _ uuid.UUID, _ string, _ *string, _ *string) error {
	return nil
}

func (m *mockSurveyResponseStoreForTriageNotes) GetAuthorization(_ context.Context, _ uuid.UUID) ([]Authorization, error) {
	return nil, nil
}

func (m *mockSurveyResponseStoreForTriageNotes) UpdateAuthorization(_ context.Context, _ uuid.UUID, _ []Authorization) error {
	return nil
}

func (m *mockSurveyResponseStoreForTriageNotes) HasAccess(_ context.Context, _ uuid.UUID, _ string, _ AuthorizationRole) (bool, error) {
	return true, nil
}

// =============================================================================
// Test Setup Helpers
// =============================================================================

// setupTriageNoteTestRouter creates a gin router with triage note handlers and mock stores.
// It saves and restores GlobalSurveyResponseStore via t.Cleanup.
func setupTriageNoteTestRouter(t *testing.T) (*gin.Engine, *mockTriageNoteStore, *mockSurveyResponseStoreForTriageNotes) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mockTNStore := newMockTriageNoteStore()
	mockSRStore := newMockSurveyResponseStoreForTriageNotes()

	// Save original global store and swap in mock
	origSurveyResponseStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockSRStore
	t.Cleanup(func() {
		GlobalSurveyResponseStore = origSurveyResponseStore
	})

	handler := NewTriageNoteSubResourceHandler(mockTNStore)

	r := gin.New()

	// Add fake auth middleware that sets full user context
	r.Use(func(c *gin.Context) {
		TestUsers.Owner.SetContext(c)
		c.Next()
	})

	// Register triage note routes matching the real path pattern
	r.GET("/survey_responses/:survey_response_id/triage_notes", handler.ListTriageNotes)
	r.POST("/survey_responses/:survey_response_id/triage_notes", handler.CreateTriageNote)
	r.GET("/survey_responses/:survey_response_id/triage_notes/:triage_note_id", handler.GetTriageNote)

	return r, mockTNStore, mockSRStore
}

// setupTriageNoteUnauthenticatedRouter creates a gin router without auth middleware
// for testing that handlers reject unauthenticated requests.
func setupTriageNoteUnauthenticatedRouter(t *testing.T) (*gin.Engine, *mockTriageNoteStore, *mockSurveyResponseStoreForTriageNotes) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mockTNStore := newMockTriageNoteStore()
	mockSRStore := newMockSurveyResponseStoreForTriageNotes()

	origSRStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockSRStore
	t.Cleanup(func() {
		GlobalSurveyResponseStore = origSRStore
	})

	handler := NewTriageNoteSubResourceHandler(mockTNStore)

	r := gin.New()
	// No auth middleware - simulates unauthenticated request
	r.GET("/survey_responses/:survey_response_id/triage_notes", handler.ListTriageNotes)
	r.POST("/survey_responses/:survey_response_id/triage_notes", handler.CreateTriageNote)
	r.GET("/survey_responses/:survey_response_id/triage_notes/:triage_note_id", handler.GetTriageNote)

	return r, mockTNStore, mockSRStore
}

// setupTriageNotePartialAuthRouter creates a gin router with partial auth context
// (email and userID set but no internalUUID) for testing the missing internalUUID path.
func setupTriageNotePartialAuthRouter(t *testing.T) (*gin.Engine, *mockTriageNoteStore, *mockSurveyResponseStoreForTriageNotes) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mockTNStore := newMockTriageNoteStore()
	mockSRStore := newMockSurveyResponseStoreForTriageNotes()

	origSRStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockSRStore
	t.Cleanup(func() {
		GlobalSurveyResponseStore = origSRStore
	})

	handler := NewTriageNoteSubResourceHandler(mockTNStore)

	r := gin.New()
	// Auth middleware that sets email and userID but omits internalUUID
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", TestUsers.Owner.Email)
		c.Set("userID", TestUsers.Owner.ProviderID)
		// Intentionally do NOT set userInternalUUID
		c.Next()
	})

	r.GET("/survey_responses/:survey_response_id/triage_notes", handler.ListTriageNotes)
	r.POST("/survey_responses/:survey_response_id/triage_notes", handler.CreateTriageNote)
	r.GET("/survey_responses/:survey_response_id/triage_notes/:triage_note_id", handler.GetTriageNote)

	return r, mockTNStore, mockSRStore
}

// addTestSurveyResponse adds a survey response to the mock store so parent
// verification passes in handlers.
func addTestSurveyResponse(mockSRStore *mockSurveyResponseStoreForTriageNotes, surveyResponseID string) {
	id, _ := uuid.Parse(surveyResponseID)
	typedID := id
	mockSRStore.responses[id] = &SurveyResponse{
		Id: &typedID,
	}
}

// seedTriageNote creates a triage note directly in the mock store for test setup.
func seedTriageNote(mockTNStore *mockTriageNoteStore, surveyResponseID string, name, content string) TriageNote {
	id := 1
	if nextID, ok := mockTNStore.nextID[surveyResponseID]; ok {
		id = nextID
	}
	mockTNStore.nextID[surveyResponseID] = id + 1
	now := time.Now().UTC()
	note := TriageNote{
		Id:        &id,
		Name:      name,
		Content:   content,
		CreatedAt: &now,
	}
	mockTNStore.notes[mockTNStore.noteKey(surveyResponseID, id)] = note
	return note
}

// =============================================================================
// Constructor Tests
// =============================================================================

// TestNewTriageNoteSubResourceHandler verifies the constructor wires the store correctly.
func TestNewTriageNoteSubResourceHandler(t *testing.T) {
	store := newMockTriageNoteStore()
	handler := NewTriageNoteSubResourceHandler(store)

	require.NotNil(t, handler, "handler should not be nil")
	assert.Equal(t, store, handler.triageNoteStore, "handler should reference the provided store")
}

// =============================================================================
// CreateTriageNote Tests
// =============================================================================

func TestCreateTriageNote(t *testing.T) {
	surveyResponseID := "00000000-0000-0000-0000-000000000001"

	t.Run("ValidInput", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":    "Initial Triage",
			"content": "This is the initial triage assessment.",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var created TriageNote
		err := json.Unmarshal(w.Body.Bytes(), &created)
		require.NoError(t, err)

		assert.Equal(t, "Initial Triage", created.Name)
		assert.Equal(t, "This is the initial triage assessment.", created.Content)
		assert.NotNil(t, created.Id)
		assert.Equal(t, 1, *created.Id)
	})

	t.Run("MissingName", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"content": "Content without a name.",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Expect 400 because name is required (either via binding or custom validator)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingContent", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name": "Note without content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Expect 400 because content is required (either via binding or custom validator)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EmptyBody", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes",
			bytes.NewReader([]byte("{invalid json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidSurveyResponseID", func(t *testing.T) {
		r, _, _ := setupTriageNoteTestRouter(t)

		body := map[string]interface{}{
			"name":    "Test",
			"content": "Test content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/not-a-uuid/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid survey response ID")
	})

	t.Run("SurveyResponseNotFound", func(t *testing.T) {
		r, _, _ := setupTriageNoteTestRouter(t)
		// Do NOT add survey response to mock store

		body := map[string]interface{}{
			"name":    "Test",
			"content": "Test content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("SurveyResponseStoreError", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		// Configure the survey response store to return an error
		mockSRStore.err = errors.New("database connection refused")

		body := map[string]interface{}{
			"name":    "Test",
			"content": "Test content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to verify survey response")
	})

	t.Run("ProhibitedFieldId", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":    "Test",
			"content": "Test content",
			"id":      42,
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ProhibitedFieldCreatedAt", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":       "Test",
			"content":    "Test content",
			"created_at": "2025-01-01T00:00:00Z",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ProhibitedFieldModifiedAt", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":        "Test",
			"content":     "Test content",
			"modified_at": "2025-01-01T00:00:00Z",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ProhibitedFieldCreatedBy", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":       "Test",
			"content":    "Test content",
			"created_by": map[string]interface{}{"email": "attacker@evil.com"},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ProhibitedFieldModifiedBy", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":        "Test",
			"content":     "Test content",
			"modified_by": map[string]interface{}{"email": "attacker@evil.com"},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingUserInternalUUID", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNotePartialAuthRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":    "Test",
			"content": "Test content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "not authenticated")
	})

	t.Run("StoreError", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)
		mockTNStore.err = errors.New("database connection lost")

		body := map[string]interface{}{
			"name":    "Test",
			"content": "Test content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("MultipleCreationsIncrementID", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		for i := 1; i <= 3; i++ {
			body := map[string]interface{}{
				"name":    fmt.Sprintf("Note %d", i),
				"content": fmt.Sprintf("Content for note %d", i),
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code, "creation %d should succeed", i)

			var created TriageNote
			err := json.Unmarshal(w.Body.Bytes(), &created)
			require.NoError(t, err)
			assert.Equal(t, i, *created.Id, "note %d should have ID %d", i, i)
		}
	})
}

// =============================================================================
// ListTriageNotes Tests
// =============================================================================

func TestListTriageNotes(t *testing.T) {
	surveyResponseID := "00000000-0000-0000-0000-000000000001"

	t.Run("Success", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		seedTriageNote(mockTNStore, surveyResponseID, "Note 1", "First note content")
		seedTriageNote(mockTNStore, surveyResponseID, "Note 2", "Second note content")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.TriageNotes, 2)
		assert.Equal(t, 2, response.Total)
		assert.Equal(t, 20, response.Limit)
		assert.Equal(t, 0, response.Offset)
	})

	t.Run("EmptyList", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Empty(t, response.TriageNotes)
		assert.Equal(t, 0, response.Total)
	})

	t.Run("DefaultPagination", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		seedTriageNote(mockTNStore, surveyResponseID, "Note", "Content")

		// No limit or offset query params - should use defaults
		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, 20, response.Limit, "default limit should be 20")
		assert.Equal(t, 0, response.Offset, "default offset should be 0")
	})

	t.Run("PaginationWithLimit", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		// Seed 3 notes
		seedTriageNote(mockTNStore, surveyResponseID, "Note 1", "Content 1")
		seedTriageNote(mockTNStore, surveyResponseID, "Note 2", "Content 2")
		seedTriageNote(mockTNStore, surveyResponseID, "Note 3", "Content 3")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?limit=2&offset=0", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.TriageNotes, 2)
		assert.Equal(t, 3, response.Total)
		assert.Equal(t, 2, response.Limit)
		assert.Equal(t, 0, response.Offset)
	})

	t.Run("PaginationWithOffset", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		seedTriageNote(mockTNStore, surveyResponseID, "Note 1", "Content 1")
		seedTriageNote(mockTNStore, surveyResponseID, "Note 2", "Content 2")
		seedTriageNote(mockTNStore, surveyResponseID, "Note 3", "Content 3")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?limit=10&offset=2", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.TriageNotes, 1)
		assert.Equal(t, 3, response.Total)
		assert.Equal(t, 10, response.Limit)
		assert.Equal(t, 2, response.Offset)
	})

	t.Run("PaginationMaxLimit", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?limit=100", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, 100, response.Limit, "limit=100 should be accepted as the maximum")
	})

	t.Run("PaginationMinLimit", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?limit=1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, 1, response.Limit, "limit=1 should be accepted as the minimum")
	})

	t.Run("InvalidLimitTooHigh", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?limit=200", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Limit must be between 1 and 100")
	})

	t.Run("InvalidLimitZero", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?limit=0", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Limit must be between 1 and 100")
	})

	t.Run("InvalidLimitNegative", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?limit=-5", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Limit must be between 1 and 100")
	})

	t.Run("InvalidOffsetNegative", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?offset=-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Offset must be non-negative")
	})

	t.Run("NonNumericLimitUsesDefault", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		// Non-numeric limit should fall back to default (20) via parseIntParam
		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?limit=abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, 20, response.Limit, "non-numeric limit should fall back to default 20")
	})

	t.Run("NonNumericOffsetUsesDefault", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		// Non-numeric offset should fall back to default (0) via parseIntParam
		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes?offset=xyz", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, 0, response.Offset, "non-numeric offset should fall back to default 0")
	})

	t.Run("InvalidSurveyResponseID", func(t *testing.T) {
		r, _, _ := setupTriageNoteTestRouter(t)

		req := httptest.NewRequest("GET", "/survey_responses/not-a-uuid/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid survey response ID")
	})

	t.Run("SurveyResponseNotFound", func(t *testing.T) {
		r, _, _ := setupTriageNoteTestRouter(t)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("SurveyResponseStoreError", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		// Set error on the survey response store, not the triage note store
		mockSRStore.err = errors.New("database unreachable")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to verify survey response")
	})

	t.Run("StoreListError", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)
		mockTNStore.err = errors.New("database timeout")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("CountErrorGracefulFallback", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		seedTriageNote(mockTNStore, surveyResponseID, "Note 1", "Content 1")
		seedTriageNote(mockTNStore, surveyResponseID, "Note 2", "Content 2")

		// Set count-only error so List succeeds but Count fails
		mockTNStore.countErr = errors.New("count query timeout")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "should still return 200 when count fails")

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// When Count fails, the handler falls back to len(notes) as the total
		assert.Equal(t, len(response.TriageNotes), response.Total,
			"total should fall back to page size when count fails")
	})

	t.Run("ListItemsContainExpectedFields", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		seedTriageNote(mockTNStore, surveyResponseID, "Detailed Note", "Detailed content here")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		require.Len(t, response.TriageNotes, 1)
		item := response.TriageNotes[0]
		assert.Equal(t, "Detailed Note", item.Name)
		assert.NotNil(t, item.Id)
		assert.NotNil(t, item.CreatedAt)
	})

	t.Run("ListItemsOmitContent", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		seedTriageNote(mockTNStore, surveyResponseID, "Note", "Content should not appear in list")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify the raw JSON does not include a "content" field in list items
		var rawResp map[string]json.RawMessage
		err := json.Unmarshal(w.Body.Bytes(), &rawResp)
		require.NoError(t, err)

		var items []map[string]json.RawMessage
		err = json.Unmarshal(rawResp["triage_notes"], &items)
		require.NoError(t, err)
		require.Len(t, items, 1)

		_, hasContent := items[0]["content"]
		assert.False(t, hasContent, "list items should not include content field")
	})

	t.Run("DifferentSurveyResponsesIsolated", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)

		srID1 := "00000000-0000-0000-0000-000000000001"
		srID2 := "00000000-0000-0000-0000-000000000002"
		addTestSurveyResponse(mockSRStore, srID1)
		addTestSurveyResponse(mockSRStore, srID2)

		seedTriageNote(mockTNStore, srID1, "SR1 Note 1", "Content for SR1")
		seedTriageNote(mockTNStore, srID1, "SR1 Note 2", "Content for SR1")
		seedTriageNote(mockTNStore, srID2, "SR2 Note 1", "Content for SR2")

		// List notes for srID1
		req := httptest.NewRequest("GET", "/survey_responses/"+srID1+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListTriageNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.TriageNotes, 2, "should only return notes for the requested survey response")
		assert.Equal(t, 2, response.Total)
	})
}

// =============================================================================
// GetTriageNote Tests
// =============================================================================

func TestGetTriageNote(t *testing.T) {
	surveyResponseID := "00000000-0000-0000-0000-000000000001"

	t.Run("Found", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		seeded := seedTriageNote(mockTNStore, surveyResponseID, "Test Note", "Note content in markdown")

		noteID := fmt.Sprintf("%d", *seeded.Id)
		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/"+noteID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var note TriageNote
		err := json.Unmarshal(w.Body.Bytes(), &note)
		require.NoError(t, err)

		assert.Equal(t, "Test Note", note.Name)
		assert.Equal(t, "Note content in markdown", note.Content)
		assert.NotNil(t, note.Id)
		assert.Equal(t, *seeded.Id, *note.Id)
	})

	t.Run("FoundIncludesContent", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		seeded := seedTriageNote(mockTNStore, surveyResponseID, "Full Note", "# Markdown\n\nDetailed content with **bold** text.")

		noteID := fmt.Sprintf("%d", *seeded.Id)
		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/"+noteID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var note TriageNote
		err := json.Unmarshal(w.Body.Bytes(), &note)
		require.NoError(t, err)

		assert.Contains(t, note.Content, "# Markdown")
		assert.Contains(t, note.Content, "**bold**")
	})

	t.Run("NotFound", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/999", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not found")
	})

	t.Run("InvalidNoteIDNotInteger", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid triage note ID")
	})

	t.Run("InvalidNoteIDZero", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/0", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid triage note ID")
	})

	t.Run("InvalidNoteIDNegative", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid triage note ID")
	})

	t.Run("InvalidNoteIDFloat", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/1.5", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid triage note ID")
	})

	t.Run("LargeNoteID", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		// A large but valid integer - should pass validation but return not found
		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/999999", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("InvalidSurveyResponseID", func(t *testing.T) {
		r, _, _ := setupTriageNoteTestRouter(t)

		req := httptest.NewRequest("GET", "/survey_responses/bad-uuid/triage_notes/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid survey response ID")
	})

	t.Run("SurveyResponseNotFound", func(t *testing.T) {
		r, _, _ := setupTriageNoteTestRouter(t)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("SurveyResponseStoreError", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		mockSRStore.err = errors.New("database connection dropped")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to verify survey response")
	})

	t.Run("StoreError", func(t *testing.T) {
		r, mockTNStore, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)
		mockTNStore.err = errors.New("database error")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// Append-Only Behavior Tests
// =============================================================================

// TestTriageNotesAppendOnly verifies that triage notes are truly append-only:
// there are no Update, Delete, or Patch handlers, matching the TriageNoteStore
// interface which only provides Create, Get, List, and Count.
func TestTriageNotesAppendOnly(t *testing.T) {
	surveyResponseID := "00000000-0000-0000-0000-000000000001"

	t.Run("PUTReturns404", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":    "Updated",
			"content": "Updated content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("PUT", "/survey_responses/"+surveyResponseID+"/triage_notes/1", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// PUT is not registered, so gin returns 404
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("PATCHReturns404", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := []map[string]interface{}{
			{"op": "replace", "path": "/name", "value": "Patched"},
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("PATCH", "/survey_responses/"+surveyResponseID+"/triage_notes/1", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json-patch+json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// PATCH is not registered, so gin returns 404
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("DELETEReturns404", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("DELETE", "/survey_responses/"+surveyResponseID+"/triage_notes/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// DELETE is not registered, so gin returns 404
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// Unauthenticated User Tests
// =============================================================================

func TestTriageNoteUnauthenticatedUser(t *testing.T) {
	surveyResponseID := "00000000-0000-0000-0000-000000000001"

	t.Run("ListRequiresAuth", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteUnauthenticatedRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("CreateRequiresAuth", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteUnauthenticatedRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		body := map[string]interface{}{
			"name":    "Test",
			"content": "Content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("GetRequiresAuth", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteUnauthenticatedRouter(t)
		addTestSurveyResponse(mockSRStore, surveyResponseID)

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// =============================================================================
// verifySurveyResponseExists Tests
// =============================================================================

// TestTriageNoteVerifySurveyResponseExists tests the verifySurveyResponseExists
// helper indirectly through the handlers, covering the store error path that
// returns a 500 response.
func TestTriageNoteVerifySurveyResponseExists(t *testing.T) {
	surveyResponseID := "00000000-0000-0000-0000-000000000001"

	t.Run("StoreErrorReturns500ViaList", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		mockSRStore.err = errors.New("pg connection pool exhausted")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to verify survey response")
	})

	t.Run("StoreErrorReturns500ViaGet", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		mockSRStore.err = errors.New("pg connection pool exhausted")

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to verify survey response")
	})

	t.Run("StoreErrorReturns500ViaCreate", func(t *testing.T) {
		r, _, mockSRStore := setupTriageNoteTestRouter(t)
		mockSRStore.err = errors.New("pg connection pool exhausted")

		body := map[string]interface{}{
			"name":    "Test",
			"content": "Content",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/survey_responses/"+surveyResponseID+"/triage_notes", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to verify survey response")
	})

	t.Run("NilResponseReturns404", func(t *testing.T) {
		r, _, _ := setupTriageNoteTestRouter(t)
		// Do not add any survey response - Get returns nil, nil

		req := httptest.NewRequest("GET", "/survey_responses/"+surveyResponseID+"/triage_notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "not found")
	})
}
