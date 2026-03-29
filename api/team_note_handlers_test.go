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
// Mock Team Note Store
// =============================================================================

type mockTeamNoteStore struct {
	notes     map[string]*TeamNote
	listItems []TeamNoteListItem
	listTotal int

	err       error
	createErr error
	getErr    error
	updateErr error
	deleteErr error
	listErr   error
	patchErr  error
}

func newMockTeamNoteStore() *mockTeamNoteStore {
	return &mockTeamNoteStore{
		notes: make(map[string]*TeamNote),
	}
}

func (m *mockTeamNoteStore) Create(_ context.Context, note *TeamNote, _ string) (*TeamNote, error) {
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

func (m *mockTeamNoteStore) Get(_ context.Context, id string) (*TeamNote, error) {
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

func (m *mockTeamNoteStore) Update(_ context.Context, id string, note *TeamNote, _ string) (*TeamNote, error) {
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

func (m *mockTeamNoteStore) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if m.err != nil {
		return m.err
	}
	delete(m.notes, id)
	return nil
}

func (m *mockTeamNoteStore) Patch(_ context.Context, id string, _ []PatchOperation) (*TeamNote, error) {
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

func (m *mockTeamNoteStore) List(_ context.Context, _ string, _, _ int, _ bool) ([]TeamNoteListItem, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.listItems, m.listTotal, nil
}

func (m *mockTeamNoteStore) Count(_ context.Context, _ string, _ bool) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.listTotal, nil
}

// =============================================================================
// Test Helpers
// =============================================================================

func saveTeamNoteStore(t *testing.T, store TeamNoteStoreInterface) {
	t.Helper()
	orig := GlobalTeamNoteStore
	origEmitter := GlobalEventEmitter
	GlobalTeamNoteStore = store
	GlobalEventEmitter = nil
	t.Cleanup(func() {
		GlobalTeamNoteStore = orig
		GlobalEventEmitter = origEmitter
	})
}

// seedTeamNoteInStore inserts a team note into the mock store and returns its UUID string.
func seedTeamNoteInStore(store *mockTeamNoteStore, noteID string, sharable bool) string {
	id := uuid.MustParse(noteID)
	now := time.Now().UTC()
	desc := "Test note description"
	timmyEnabled := true
	store.notes[noteID] = &TeamNote{
		Id:           &id,
		Name:         "Test Note",
		Content:      "Test content",
		Description:  &desc,
		Sharable:     &sharable,
		TimmyEnabled: &timmyEnabled,
		CreatedAt:    &now,
		ModifiedAt:   &now,
	}
	return noteID
}

const testTeamNoteID = "22222222-2222-2222-2222-222222222222"

// =============================================================================
// Team Note Handler Tests
// =============================================================================

func TestListTeamNotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success with defaults", func(t *testing.T) {
		store := newMockTeamNoteStore()
		now := time.Now().UTC()
		noteID := uuid.New()
		sharable := true
		store.listItems = []TeamNoteListItem{
			{Id: &noteID, Name: "Note A", Sharable: &sharable, CreatedAt: &now, ModifiedAt: &now},
		}
		store.listTotal = 1
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes")
		TestUsers.Owner.SetContext(c)

		server.ListTeamNotes(c, teamUUID, ListTeamNotesParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListTeamNotesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Total)
		assert.Equal(t, 20, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Len(t, resp.Notes, 1)
	})

	t.Run("pagination parameters", func(t *testing.T) {
		store := newMockTeamNoteStore()
		store.listItems = []TeamNoteListItem{}
		store.listTotal = 50
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes?limit=10&offset=20")
		TestUsers.Owner.SetContext(c)

		limit := 10
		offset := 20
		server.ListTeamNotes(c, teamUUID, ListTeamNotesParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListTeamNotesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 10, resp.Limit)
		assert.Equal(t, 20, resp.Offset)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		// Create team without test user as member
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Test Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes")
		TestUsers.Owner.SetContext(c)

		server.ListTeamNotes(c, teamUUID, ListTeamNotesParams{})

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)
		setupTestTeamAuthDB(t)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes")

		server.ListTeamNotes(c, teamUUID, ListTeamNotesParams{})

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("store error returns 500", func(t *testing.T) {
		store := newMockTeamNoteStore()
		store.listErr = errors.New("database connection lost")
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes")
		TestUsers.Owner.SetContext(c)

		server.ListTeamNotes(c, teamUUID, ListTeamNotesParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestCreateTeamNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success with default sharable", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		body := TeamNoteInput{
			Name:    "New Note",
			Content: "Some content",
		}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("POST", "/teams/"+testTeamID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateTeamNote(c, teamUUID)

		assert.Equal(t, http.StatusCreated, w.Code)
		var created TeamNote
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		assert.Equal(t, "New Note", created.Name)
		assert.NotNil(t, created.Id)
		// Regular user default: sharable=true
		require.NotNil(t, created.Sharable)
		assert.True(t, *created.Sharable)
	})

	t.Run("regular user 403 when sharable field included as true", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		sharable := true
		body := TeamNoteInput{
			Name:     "New Note",
			Content:  "Some content",
			Sharable: &sharable,
		}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("POST", "/teams/"+testTeamID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateTeamNote(c, teamUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("regular user 403 when sharable field included as false", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		sharable := false
		body := TeamNoteInput{
			Name:     "New Note",
			Content:  "Some content",
			Sharable: &sharable,
		}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("POST", "/teams/"+testTeamID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateTeamNote(c, teamUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Test Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)

		body := TeamNoteInput{Name: "Note", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("POST", "/teams/"+testTeamID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateTeamNote(c, teamUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid body returns 400", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("POST", "/teams/"+testTeamID+"/notes", "application/json", []byte(`{invalid json`))
		TestUsers.Owner.SetContext(c)

		server.CreateTeamNote(c, teamUUID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("store error", func(t *testing.T) {
		store := newMockTeamNoteStore()
		store.createErr = errors.New("db error")
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		body := TeamNoteInput{Name: "Note", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("POST", "/teams/"+testTeamID+"/notes", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateTeamNote(c, teamUUID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)
		setupTestTeamAuthDB(t)

		body := TeamNoteInput{Name: "Note", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		c, w := CreateTestGinContextWithBody("POST", "/teams/"+testTeamID+"/notes", "application/json", bodyBytes)

		server.CreateTeamNote(c, teamUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestGetTeamNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - sharable note", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)
		TestUsers.Owner.SetContext(c)

		server.GetTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var note TeamNote
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &note))
		assert.Equal(t, "Test Note", note.Name)
	})

	t.Run("regular user gets 404 for non-sharable note", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, false)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)
		TestUsers.Owner.SetContext(c)

		server.GetTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Test Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)
		TestUsers.Owner.SetContext(c)

		server.GetTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("note not found returns 404", func(t *testing.T) {
		store := newMockTeamNoteStore()
		// Don't seed any note
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)
		TestUsers.Owner.SetContext(c)

		server.GetTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)
		setupTestTeamAuthDB(t)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)

		server.GetTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestUpdateTeamNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - update sharable note", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		body := TeamNoteInput{
			Name:    "Updated Note",
			Content: "Updated content",
		}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var updated TeamNote
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
		assert.Equal(t, "Updated Note", updated.Name)
	})

	t.Run("regular user gets 404 for non-sharable note", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, false)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		body := TeamNoteInput{Name: "Updated", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("regular user 403 when sharable field included", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		sharable := true
		body := TeamNoteInput{
			Name:     "Updated",
			Content:  "Content",
			Sharable: &sharable,
		}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Test Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)

		body := TeamNoteInput{Name: "Updated", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)
		setupTestTeamAuthDB(t)

		body := TeamNoteInput{Name: "Note", Content: "Content"}
		bodyBytes, _ := json.Marshal(body)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)

		server.UpdateTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestPatchTeamNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - patch sharable note", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "Patched Name"},
		}
		bodyBytes, _ := json.Marshal(ops)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("regular user 403 when patching sharable path", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		ops := []PatchOperation{
			{Op: "replace", Path: "/sharable", Value: false},
		}
		bodyBytes, _ := json.Marshal(ops)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("regular user gets 404 for non-sharable note", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, false)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "New Name"},
		}
		bodyBytes, _ := json.Marshal(ops)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Test Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)

		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "Patched"},
		}
		bodyBytes, _ := json.Marshal(ops)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.PatchTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)
		setupTestTeamAuthDB(t)

		ops := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "Patched"},
		}
		bodyBytes, _ := json.Marshal(ops)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", bodyBytes)

		server.PatchTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDeleteTeamNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("success - delete sharable note", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, _ := CreateTestGinContext("DELETE", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)
		TestUsers.Owner.SetContext(c)

		server.DeleteTeamNote(c, teamUUID, noteUUID)

		// c.Status() doesn't flush to httptest.NewRecorder; check Gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	})

	t.Run("regular user gets 404 for non-sharable note", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, false)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContext("DELETE", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)
		TestUsers.Owner.SetContext(c)

		server.DeleteTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("forbidden - non-member", func(t *testing.T) {
		store := newMockTeamNoteStore()
		seedTeamNoteInStore(store, testTeamNoteID, true)
		saveTeamNoteStore(t, store)

		db := setupTestTeamAuthDB(t)
		err := db.Create(&models.TeamRecord{
			ID:                    testTeamID,
			Name:                  "Test Team",
			CreatedByInternalUUID: "other-user-uuid",
		}).Error
		require.NoError(t, err)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContext("DELETE", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)
		TestUsers.Owner.SetContext(c)

		server.DeleteTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		store := newMockTeamNoteStore()
		saveTeamNoteStore(t, store)
		setupTestTeamAuthDB(t)

		teamUUID, _ := uuid.Parse(testTeamID)
		noteUUID, _ := uuid.Parse(testTeamNoteID)
		c, w := CreateTestGinContext("DELETE", "/teams/"+testTeamID+"/notes/"+testTeamNoteID)

		server.DeleteTeamNote(c, teamUUID, noteUUID)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
