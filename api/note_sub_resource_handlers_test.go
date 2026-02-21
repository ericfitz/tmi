package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockNoteStore is a mock implementation of NoteStore for testing
type MockNoteStore struct {
	mock.Mock
}

func (m *MockNoteStore) Create(ctx context.Context, note *Note, threatModelID string) error {
	args := m.Called(ctx, note, threatModelID)
	return args.Error(0)
}

func (m *MockNoteStore) Get(ctx context.Context, id string) (*Note, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Note), args.Error(1)
}

func (m *MockNoteStore) Update(ctx context.Context, note *Note, threatModelID string) error {
	args := m.Called(ctx, note, threatModelID)
	return args.Error(0)
}

func (m *MockNoteStore) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockNoteStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Note, error) {
	args := m.Called(ctx, threatModelID, offset, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]Note), args.Error(1)
}

func (m *MockNoteStore) Count(ctx context.Context, threatModelID string) (int, error) {
	args := m.Called(ctx, threatModelID)
	return args.Int(0), args.Error(1)
}

func (m *MockNoteStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Note, error) {
	args := m.Called(ctx, id, operations)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Note), args.Error(1)
}

func (m *MockNoteStore) InvalidateCache(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockNoteStore) WarmCache(ctx context.Context, threatModelID string) error {
	args := m.Called(ctx, threatModelID)
	return args.Error(0)
}

// setupNoteSubResourceHandler creates a test router with note sub-resource handlers
func setupNoteSubResourceHandler() (*gin.Engine, *MockNoteStore) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	mockNoteStore := &MockNoteStore{}
	handler := NewNoteSubResourceHandler(mockNoteStore, nil, nil, nil)

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})

	// Register note sub-resource routes
	r.GET("/threat_models/:threat_model_id/notes", handler.GetNotes)
	r.POST("/threat_models/:threat_model_id/notes", handler.CreateNote)
	r.GET("/threat_models/:threat_model_id/notes/:note_id", handler.GetNote)
	r.PUT("/threat_models/:threat_model_id/notes/:note_id", handler.UpdateNote)
	r.PATCH("/threat_models/:threat_model_id/notes/:note_id", handler.PatchNote)
	r.DELETE("/threat_models/:threat_model_id/notes/:note_id", handler.DeleteNote)

	return r, mockNoteStore
}

// TestGetNotes tests retrieving notes for a threat model
func TestGetNotes(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupNoteSubResourceHandler()

		threatModelID := testUUID1
		notes := []Note{
			{Name: "Security Review", Content: "Security review findings", Description: new("Review note")},
			{Name: "Architecture Notes", Content: "Architecture design notes", Description: new("Design note")},
		}

		uuid1, _ := uuid.Parse(testUUID1)
		uuid2, _ := uuid.Parse(testUUID2)
		notes[0].Id = &uuid1
		notes[1].Id = &uuid2

		mockStore.On("List", mock.Anything, threatModelID, 0, 20).Return(notes, nil)
		mockStore.On("Count", mock.Anything, threatModelID).Return(2, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.Notes, 2)
		assert.Equal(t, "Security Review", response.Notes[0].Name)
		assert.Equal(t, "Architecture Notes", response.Notes[1].Name)
		assert.Equal(t, 2, response.Total)
		assert.Equal(t, 20, response.Limit)
		assert.Equal(t, 0, response.Offset)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		req := httptest.NewRequest("GET", "/threat_models/invalid-uuid/notes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("WithPagination", func(t *testing.T) {
		r, mockStore := setupNoteSubResourceHandler()

		threatModelID := testUUID1
		notes := []Note{
			{Name: "Security Review", Content: "Security Review Note"},
		}

		uuid1, _ := uuid.Parse(testUUID1)
		notes[0].Id = &uuid1

		mockStore.On("List", mock.Anything, threatModelID, 10, 5).Return(notes, nil)
		mockStore.On("Count", mock.Anything, threatModelID).Return(100, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes?limit=5&offset=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListNotesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Len(t, response.Notes, 1)
		assert.Equal(t, 100, response.Total)
		assert.Equal(t, 5, response.Limit)
		assert.Equal(t, 10, response.Offset)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidLimit", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes?limit=150", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidOffset", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes?offset=-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestGetNote tests retrieving a specific note
func TestGetNote(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupNoteSubResourceHandler()

		threatModelID := testUUID1
		noteID := testUUID2

		note := &Note{Name: "Findings Note", Content: "Important findings", Description: new("Security review note")}
		uuid1, _ := uuid.Parse(noteID)
		note.Id = &uuid1

		mockStore.On("Get", mock.Anything, noteID).Return(note, nil)

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes/"+noteID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Important findings", response["content"])

		mockStore.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		r, mockStore := setupNoteSubResourceHandler()

		threatModelID := testUUID1
		noteID := testUUID2

		mockStore.On("Get", mock.Anything, noteID).Return(nil, NotFoundError("Note not found"))

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes/"+noteID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidNoteID", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("GET", "/threat_models/"+threatModelID+"/notes/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestCreateNote tests creating a new note
func TestCreateNote(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupNoteSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]any{
			"name":        "Security Review Note",
			"content":     "Important security findings",
			"description": "New Security Note",
		}

		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Note"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/notes", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Important security findings", response["content"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidRequestBody", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		threatModelID := testUUID1

		// Missing required content field
		requestBody := map[string]any{
			"description": "A note without content",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/notes", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		requestBody := map[string]any{
			"content": "Test Note Content",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("POST", "/threat_models/invalid-uuid/notes", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestUpdateNote tests updating an existing note
func TestUpdateNote(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupNoteSubResourceHandler()

		threatModelID := testUUID1
		noteID := testUUID2

		requestBody := map[string]any{
			"name":        "Updated Note Name",
			"content":     "Updated content",
			"description": "Updated Note",
		}

		mockStore.On("Update", mock.Anything, mock.AnythingOfType("*api.Note"), threatModelID).Return(nil)

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/notes/"+noteID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidNoteID", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		threatModelID := testUUID1

		requestBody := map[string]any{
			"content": "Test Note Content",
		}

		body, _ := json.Marshal(requestBody)
		req := httptest.NewRequest("PUT", "/threat_models/"+threatModelID+"/notes/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestPatchNote tests applying JSON patch operations to a note
func TestPatchNote(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupNoteSubResourceHandler()

		threatModelID := testUUID1
		noteID := testUUID2

		patchOps := []map[string]any{
			{"op": "replace", "path": "/content", "value": "Patched Content"},
		}

		updatedNote := &Note{Name: "Patched Note", Content: "Patched Content"}
		uuid1, _ := uuid.Parse(noteID)
		updatedNote.Id = &uuid1

		mockStore.On("Patch", mock.Anything, noteID, mock.AnythingOfType("[]api.PatchOperation")).Return(updatedNote, nil)

		body, _ := json.Marshal(patchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/notes/"+noteID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json-patch+json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "Patched Content", response["content"])

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidNoteID", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		threatModelID := testUUID1

		patchOps := []map[string]any{
			{"op": "replace", "path": "/title", "value": "Patched Title"},
		}

		body, _ := json.Marshal(patchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/notes/invalid-uuid", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json-patch+json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EmptyPatchOperations", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		threatModelID := testUUID1
		noteID := testUUID2

		patchOps := []map[string]any{}

		body, _ := json.Marshal(patchOps)
		req := httptest.NewRequest("PATCH", "/threat_models/"+threatModelID+"/notes/"+noteID, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json-patch+json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestDeleteNote tests deleting a note
func TestDeleteNote(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r, mockStore := setupNoteSubResourceHandler()

		threatModelID := testUUID1
		noteID := testUUID2

		mockStore.On("Delete", mock.Anything, noteID).Return(nil)

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/notes/"+noteID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		mockStore.AssertExpectations(t)
	})

	t.Run("InvalidNoteID", func(t *testing.T) {
		r, _ := setupNoteSubResourceHandler()

		threatModelID := testUUID1

		req := httptest.NewRequest("DELETE", "/threat_models/"+threatModelID+"/notes/invalid-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
