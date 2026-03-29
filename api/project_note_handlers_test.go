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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Project Note Store
// =============================================================================

type mockProjectNoteStore struct {
	notes     map[string]*ProjectNote
	listItems []ProjectNoteListItem
	listTotal int

	err       error
	createErr error
	getErr    error
	updateErr error
	deleteErr error
	listErr   error
	patchErr  error
}

func newMockProjectNoteStore() *mockProjectNoteStore {
	return &mockProjectNoteStore{
		notes: make(map[string]*ProjectNote),
	}
}

func (m *mockProjectNoteStore) Create(_ context.Context, note *ProjectNote, _ string) (*ProjectNote, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.err != nil {
		return nil, m.err
	}
	if note.Id == nil {
		id := uuid.New()
		note.Id = &id
	}
	now := time.Now().UTC()
	note.CreatedAt = &now
	note.ModifiedAt = &now
	m.notes[note.Id.String()] = note
	return note, nil
}

func (m *mockProjectNoteStore) Get(_ context.Context, id string) (*ProjectNote, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.err != nil {
		return nil, m.err
	}
	note, ok := m.notes[id]
	if !ok {
		return nil, &RequestError{Status: 404, Code: "not_found", Message: "not found"}
	}
	return note, nil
}

func (m *mockProjectNoteStore) Update(_ context.Context, id string, note *ProjectNote, _ string) (*ProjectNote, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	if m.err != nil {
		return nil, m.err
	}
	u := uuid.MustParse(id)
	note.Id = &u
	now := time.Now().UTC()
	note.ModifiedAt = &now
	m.notes[id] = note
	return note, nil
}

func (m *mockProjectNoteStore) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if m.err != nil {
		return m.err
	}
	delete(m.notes, id)
	return nil
}

func (m *mockProjectNoteStore) Patch(_ context.Context, id string, _ []PatchOperation) (*ProjectNote, error) {
	if m.patchErr != nil {
		return nil, m.patchErr
	}
	if m.err != nil {
		return nil, m.err
	}
	note, ok := m.notes[id]
	if !ok {
		return nil, &RequestError{Status: 404, Code: "not_found", Message: "not found"}
	}
	now := time.Now().UTC()
	note.ModifiedAt = &now
	return note, nil
}

func (m *mockProjectNoteStore) List(_ context.Context, _ string, _, _ int, _ bool) ([]ProjectNoteListItem, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.listItems, m.listTotal, nil
}

func (m *mockProjectNoteStore) Count(_ context.Context, _ string, _ bool) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.listTotal, nil
}

// =============================================================================
// Test Helpers
// =============================================================================

func saveProjectNoteStore(t *testing.T, store ProjectNoteStoreInterface) {
	t.Helper()
	orig := GlobalProjectNoteStore
	origEmitter := GlobalEventEmitter
	GlobalProjectNoteStore = store
	GlobalEventEmitter = nil
	t.Cleanup(func() {
		GlobalProjectNoteStore = orig
		GlobalEventEmitter = origEmitter
	})
}

// seedProjectNoteInStore inserts a project note into the mock store and returns its UUID string.
func seedProjectNoteInStore(store *mockProjectNoteStore, noteID string, sharable bool) string {
	id := uuid.MustParse(noteID)
	now := time.Now().UTC()
	desc := "Test project note description"
	timmyEnabled := true
	store.notes[noteID] = &ProjectNote{
		Id:           &id,
		Name:         "Test Project Note",
		Content:      "Test project note content",
		Description:  &desc,
		Sharable:     &sharable,
		TimmyEnabled: &timmyEnabled,
		CreatedAt:    &now,
		ModifiedAt:   &now,
	}
	return noteID
}

const testProjectNoteID = "44444444-4444-4444-4444-444444444444"

// =============================================================================
// Project Note Handler Tests
// =============================================================================

func TestListProjectNotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success with defaults", func(t *testing.T) {
		store := newMockProjectNoteStore()
		now := time.Now().UTC()
		noteID := uuid.New()
		sharable := true
		store.listItems = []ProjectNoteListItem{
			{Id: &noteID, Name: "Note A", Sharable: &sharable, CreatedAt: &now, ModifiedAt: &now},
		}
		store.listTotal = 1
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes")
		TestUsers.Owner.SetContext(c)

		server.ListProjectNotes(c, projectUUID, ListProjectNotesParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListProjectNotesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Total)
		assert.Equal(t, 20, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Len(t, resp.Notes, 1)
	})

	t.Run("pagination parameters", func(t *testing.T) {
		store := newMockProjectNoteStore()
		store.listItems = []ProjectNoteListItem{}
		store.listTotal = 50
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes?limit=10&offset=20")
		TestUsers.Owner.SetContext(c)

		limit := 10
		offset := 20
		server.ListProjectNotes(c, projectUUID, ListProjectNotesParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListProjectNotesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 10, resp.Limit)
		assert.Equal(t, 20, resp.Offset)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		// Create team without test user as member
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes")
		TestUsers.Owner.SetContext(c)

		server.ListProjectNotes(c, projectUUID, ListProjectNotesParams{})

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)
		setupTestTeamAuthDB(t)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes")

		server.ListProjectNotes(c, projectUUID, ListProjectNotesParams{})

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("store error returns 500", func(t *testing.T) {
		store := newMockProjectNoteStore()
		store.listErr = errors.New("database connection lost")
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes")
		TestUsers.Owner.SetContext(c)

		server.ListProjectNotes(c, projectUUID, ListProjectNotesParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestCreateProjectNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success with default sharable", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		body := ProjectNoteInput{
			Name:    "New Project Note",
			Content: "Some project content",
		}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("POST", "/projects/"+testProjectID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProjectNote(c, projectUUID)

		assert.Equal(t, http.StatusCreated, w.Code)
		var created ProjectNote
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		assert.Equal(t, "New Project Note", created.Name)
		assert.NotNil(t, created.Id)
		// Regular user default: sharable=true
		require.NotNil(t, created.Sharable)
		assert.True(t, *created.Sharable)
	})

	t.Run("regular user 403 when sharable field included as true", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		sharable := true
		body := ProjectNoteInput{
			Name:     "New Note",
			Content:  "Some content",
			Sharable: &sharable,
		}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("POST", "/projects/"+testProjectID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProjectNote(c, projectUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("regular user 403 when sharable field included as false", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		sharable := false
		body := ProjectNoteInput{
			Name:     "New Note",
			Content:  "Some content",
			Sharable: &sharable,
		}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("POST", "/projects/"+testProjectID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProjectNote(c, projectUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		body := ProjectNoteInput{Name: "Note", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("POST", "/projects/"+testProjectID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProjectNote(c, projectUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid body returns 400", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("POST", "/projects/"+testProjectID+"/notes", "application/json", []byte(`{invalid json`))
		TestUsers.Owner.SetContext(c)

		server.CreateProjectNote(c, projectUUID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockProjectNoteStore()
		store.createErr = errors.New("db error")
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		body := ProjectNoteInput{Name: "Note", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("POST", "/projects/"+testProjectID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProjectNote(c, projectUUID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)
		setupTestTeamAuthDB(t)

		body := ProjectNoteInput{Name: "Note", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		c, w := CreateTestGinContextWithBody("POST", "/projects/"+testProjectID+"/notes", "application/json", bodyBytes)

		server.CreateProjectNote(c, projectUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestGetProjectNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - sharable note", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)
		TestUsers.Owner.SetContext(c)

		server.GetProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var note ProjectNote
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &note))
		assert.Equal(t, "Test Project Note", note.Name)
	})

	t.Run("regular user gets 404 for non-sharable note", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, false)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)
		TestUsers.Owner.SetContext(c)

		server.GetProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)
		TestUsers.Owner.SetContext(c)

		server.GetProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("note not found returns 404", func(t *testing.T) {
		store := newMockProjectNoteStore()
		// Don't seed any note
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)
		TestUsers.Owner.SetContext(c)

		server.GetProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)
		setupTestTeamAuthDB(t)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)

		server.GetProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestUpdateProjectNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - update sharable note", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		body := ProjectNoteInput{
			Name:    "Updated Project Note",
			Content: "Updated project content",
		}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var updated ProjectNote
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
		assert.Equal(t, "Updated Project Note", updated.Name)
	})

	t.Run("regular user gets 404 for non-sharable note", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, false)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		body := ProjectNoteInput{Name: "Updated", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("regular user 403 when sharable field included", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		sharable := true
		body := ProjectNoteInput{
			Name:     "Updated",
			Content:  "Content",
			Sharable: &sharable,
		}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		body := ProjectNoteInput{Name: "Updated", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)
		setupTestTeamAuthDB(t)

		body := ProjectNoteInput{Name: "Note", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)

		server.UpdateProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestPatchProjectNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - patch sharable note", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "Patched Name"},
		}
		bodyBytes, _ := json.Marshal(ops)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("regular user 403 when patching sharable path", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		ops := []PatchOperation{
			{Op: "replace", Path: "/sharable", Value: false},
		}
		bodyBytes, _ := json.Marshal(ops)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("regular user gets 404 for non-sharable note", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, false)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "New Name"},
		}
		bodyBytes, _ := json.Marshal(ops)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "Patched"},
		}
		bodyBytes, _ := json.Marshal(ops)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)
		setupTestTeamAuthDB(t)

		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "Patched"},
		}
		bodyBytes, _ := json.Marshal(ops)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", bodyBytes)

		server.PatchProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDeleteProjectNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - delete sharable note", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, _ := CreateTestGinContext("DELETE", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)
		TestUsers.Owner.SetContext(c)

		server.DeleteProjectNote(c, projectUUID, noteUUID)

		// c.Status() doesn't flush to httptest.NewRecorder; check Gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	})

	t.Run("regular user gets 404 for non-sharable note", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, false)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContext("DELETE", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)
		TestUsers.Owner.SetContext(c)

		server.DeleteProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockProjectNoteStore()
		seedProjectNoteInStore(store, testProjectNoteID, true)
		saveProjectNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Other Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContext("DELETE", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)
		TestUsers.Owner.SetContext(c)

		server.DeleteProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockProjectNoteStore()
		saveProjectNoteStore(t, store)
		setupTestTeamAuthDB(t)

		projectUUID, _ := uuid.Parse(testProjectID)
		noteUUID, _ := uuid.Parse(testProjectNoteID)
		c, w := CreateTestGinContext("DELETE", "/projects/"+testProjectID+"/notes/"+testProjectNoteID)

		server.DeleteProjectNote(c, projectUUID, noteUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
