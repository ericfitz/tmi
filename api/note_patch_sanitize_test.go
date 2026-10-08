package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const patchAttackContent = `<script>x</script>[x](javascript:alert(1))`

const patchUnsafeLink = `[x](javascript:alert(1))`

// patchAttacks are JSON Patch bodies that try to store unsafe content: direct
// writes, and copy/move smuggling from a field that is only plain-text sanitized.
func patchAttacks() map[string][]PatchOperation {
	return map[string][]PatchOperation{
		"replace content":     {{Op: "replace", Path: "/content", Value: patchAttackContent}},
		"replace name":        {{Op: "replace", Path: "/name", Value: patchAttackContent}},
		"replace description": {{Op: "replace", Path: "/description", Value: patchAttackContent}},
		"copy name to content": {
			{Op: "replace", Path: "/name", Value: patchUnsafeLink},
			{Op: "copy", From: "/name", Path: "/content"},
		},
		"move name to content": {
			{Op: "replace", Path: "/name", Value: patchUnsafeLink},
			{Op: "move", From: "/name", Path: "/content"},
		},
		"copy description to content": {
			{Op: "replace", Path: "/description", Value: patchUnsafeLink},
			{Op: "copy", From: "/description", Path: "/content"},
		},
	}
}

// assertPersistedNoteSafe checks one handler outcome: a 400, or a 200 where the
// note the store persisted has no script markup in any checked field and no
// unsafe link in content.
func assertPersistedNoteSafe(t *testing.T, code int, persisted any, fields ...string) {
	t.Helper()
	if code == http.StatusBadRequest {
		return
	}
	if !assert.Equal(t, http.StatusOK, code) {
		return
	}
	raw, err := json.Marshal(persisted)
	assert.NoError(t, err)
	var after map[string]any
	assert.NoError(t, json.Unmarshal(raw, &after))
	for _, field := range fields {
		v, _ := after[field].(string)
		assert.NotContains(t, v, "<script", "persisted %s must be sanitized", field)
	}
	content, _ := after["content"].(string)
	assert.NotEmpty(t, content)
	assert.False(t, markdownHasUnsafeLink(content), "persisted content keeps an unsafe link: %q", content)
}

// The copy race (#1013): the handler reads a safe note, a concurrent request
// then puts an unsafe link in /name, and the store applies "copy /name ->
// /content" to that newer row. Only a check on the entity the store persists
// catches it.
var raceCopyNameToContent = []PatchOperation{{Op: "copy", From: "/name", Path: "/content"}}

func patchTeamNoteRequest(t *testing.T, store *mockTeamNoteStore, ops []PatchOperation) int {
	t.Helper()
	saveTeamNoteStore(t, store)
	db := setupTestTeamAuthDB(t)
	seedTeamAuthData(t, db, testTeamID, testUserUUID)
	body, _ := json.Marshal(ops)
	teamUUID, _ := uuid.Parse(testTeamID)
	noteUUID, _ := uuid.Parse(testTeamNoteID)
	c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", body)
	TestUsers.Owner.SetContext(c)
	(&Server{}).PatchTeamNote(c, teamUUID, noteUUID)
	return w.Code
}

func patchProjectNoteRequest(t *testing.T, store *mockProjectNoteStore, ops []PatchOperation) int {
	t.Helper()
	saveProjectNoteStore(t, store)
	db := setupTestTeamAuthDB(t)
	seedTeamAuthData(t, db, testTeamID, testUserUUID)
	seedProjectAuthData(t, db, testProjectID, testTeamID)
	body, _ := json.Marshal(ops)
	projectUUID, _ := uuid.Parse(testProjectID)
	noteUUID, _ := uuid.Parse(testProjectNoteID)
	c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", body)
	TestUsers.Owner.SetContext(c)
	(&Server{}).PatchProjectNote(c, projectUUID, noteUUID)
	return w.Code
}

func TestPatchTeamNote_SanitizesPatchedResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, attack := range patchAttacks() {
		t.Run(name, func(t *testing.T) {
			store := newMockTeamNoteStore()
			seedTeamNoteInStore(store, testTeamNoteID, true)
			code := patchTeamNoteRequest(t, store, attack)
			assertPersistedNoteSafe(t, code, store.notes[testTeamNoteID], "content", "name", "description")
		})
	}
}

func TestPatchTeamNote_SanitizesRowTheStorePatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockTeamNoteStore()
	seedTeamNoteInStore(store, testTeamNoteID, true)
	raced := *store.notes[testTeamNoteID]
	raced.Name = patchUnsafeLink
	store.current = map[string]*TeamNote{testTeamNoteID: &raced}

	code := patchTeamNoteRequest(t, store, raceCopyNameToContent)
	assertPersistedNoteSafe(t, code, store.notes[testTeamNoteID], "content")
}

func TestPatchProjectNote_SanitizesPatchedResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, attack := range patchAttacks() {
		t.Run(name, func(t *testing.T) {
			store := newMockProjectNoteStore()
			seedProjectNoteInStore(store, testProjectNoteID, true)
			code := patchProjectNoteRequest(t, store, attack)
			assertPersistedNoteSafe(t, code, store.notes[testProjectNoteID], "content", "name", "description")
		})
	}
}

func TestPatchProjectNote_SanitizesRowTheStorePatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockProjectNoteStore()
	seedProjectNoteInStore(store, testProjectNoteID, true)
	raced := *store.notes[testProjectNoteID]
	raced.Name = patchUnsafeLink
	store.current = map[string]*ProjectNote{testProjectNoteID: &raced}

	code := patchProjectNoteRequest(t, store, raceCopyNameToContent)
	assertPersistedNoteSafe(t, code, store.notes[testProjectNoteID], "content")
}

// patchNoteLikeStore makes the mock's Patch behave like a store: apply the
// operations to row (the store's own read, which may differ from what the
// handler's Get returned), run the handler's check, and persist the result
// into the returned note. A failed check returns its error and persists
// nothing.
func patchNoteLikeStore(mockStore *MockNoteStore, noteID string, row *Note) *Note {
	persisted := &Note{}
	mockStore.On("Patch", mock.Anything, noteID, mock.AnythingOfType("[]api.PatchOperation"), mock.Anything).
		Return(func(_ context.Context, _ string, ops []PatchOperation, check func(before, after *Note) error) (*Note, error) {
			patched, err := ApplyPatchOperations(*row, ops)
			if err != nil {
				return nil, err
			}
			if err := check(row, &patched); err != nil {
				return nil, err
			}
			*persisted = patched
			return persisted, nil
		}, nil)
	return persisted
}

func patchNoteRequest(r http.Handler, noteID string, ops []PatchOperation) int {
	body, _ := json.Marshal(ops)
	req := httptest.NewRequest("PATCH", "/threat_models/"+testUUID1+"/notes/"+noteID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestPatchNote_SanitizesPatchedResult(t *testing.T) {
	for name, attack := range patchAttacks() {
		t.Run(name, func(t *testing.T) {
			r, mockStore := setupNoteSubResourceHandler()
			noteID := testUUID2
			noteUUID, _ := uuid.Parse(noteID)
			existing := &Note{Id: &noteUUID, Name: "n", Content: "ok"}
			mockStore.On("Get", mock.Anything, noteID).Return(existing, nil)
			persisted := patchNoteLikeStore(mockStore, noteID, existing)

			code := patchNoteRequest(r, noteID, attack)
			// The TM note path sanitizes content only (as its create/update do).
			assertPersistedNoteSafe(t, code, persisted, "content")
		})
	}
}

func TestPatchNote_SanitizesRowTheStorePatches(t *testing.T) {
	r, mockStore := setupNoteSubResourceHandler()
	noteID := testUUID2
	noteUUID, _ := uuid.Parse(noteID)
	mockStore.On("Get", mock.Anything, noteID).Return(&Note{Id: &noteUUID, Name: "safe", Content: "ok"}, nil)
	persisted := patchNoteLikeStore(mockStore, noteID, &Note{Id: &noteUUID, Name: patchUnsafeLink, Content: "ok"})

	code := patchNoteRequest(r, noteID, raceCopyNameToContent)
	assertPersistedNoteSafe(t, code, persisted, "content")
}

// When the handler's own read fails, sanitization must still run against the
// note the store patches, not an empty stand-in on which a "test" operation
// fails and the check is skipped.
func TestPatchNote_SanitizesWhenHandlerReadFails(t *testing.T) {
	r, mockStore := setupNoteSubResourceHandler()
	noteID := testUUID2
	noteUUID, _ := uuid.Parse(noteID)
	mockStore.On("Get", mock.Anything, noteID).Return(nil, errors.New("transient read failure"))
	persisted := patchNoteLikeStore(mockStore, noteID, &Note{Id: &noteUUID, Name: "n", Content: "ok"})

	code := patchNoteRequest(r, noteID, []PatchOperation{
		{Op: "test", Path: "/name", Value: "n"},
		{Op: "replace", Path: "/content", Value: patchAttackContent},
	})
	assertPersistedNoteSafe(t, code, persisted, "content")
}

func TestSanitizePatchedNoteText(t *testing.T) {
	legacy := "legacy " + patchUnsafeLink

	// Unchanged content is not re-checked, so a legacy value cannot block an
	// unrelated edit.
	before := &TeamNote{Name: "n", Content: legacy}
	after := &TeamNote{Name: "new <b>name</b>", Content: legacy}
	assert.NoError(t, checkPatchedTeamNote(before, after))
	assert.Equal(t, legacy, after.Content)
	assert.Equal(t, "new name", after.Name)

	// Changed content is sanitized in place.
	before = &TeamNote{Name: "n", Content: "ok"}
	after = &TeamNote{Name: "n", Content: "a <b onclick=x>b</b> " + patchUnsafeLink}
	assert.NoError(t, checkPatchedTeamNote(before, after))
	assert.NotContains(t, after.Content, "onclick")
	assert.NotContains(t, after.Content, "javascript:")

	// Content that sanitizes to nothing is a 400 RequestError.
	err := checkPatchedProjectNote(&ProjectNote{Content: "ok"}, &ProjectNote{Content: "<script>x</script>"})
	var reqErr *RequestError
	if assert.ErrorAs(t, err, &reqErr) {
		assert.Equal(t, http.StatusBadRequest, reqErr.Status)
	}

	// A new or changed description is plain-text sanitized; nil stays nil.
	desc := "<script>x</script>d"
	pAfter := &ProjectNote{Content: "ok", Description: &desc}
	assert.NoError(t, checkPatchedProjectNote(&ProjectNote{Content: "ok"}, pAfter))
	assert.Equal(t, "d", *pAfter.Description)
	pAfter = &ProjectNote{Content: "ok"}
	assert.NoError(t, checkPatchedProjectNote(&ProjectNote{Content: "ok", Description: &desc}, pAfter))
	assert.Nil(t, pAfter.Description)

	// A name that sanitizes to nothing is a 400 (Oracle would store NULL in a
	// NOT NULL column); a description that does becomes nil.
	err = checkPatchedTeamNote(&TeamNote{Name: "n", Content: "ok"}, &TeamNote{Name: "<b></b>", Content: "ok"})
	if assert.ErrorAs(t, err, &reqErr) {
		assert.Equal(t, http.StatusBadRequest, reqErr.Status)
		assert.Contains(t, reqErr.Message, "name is empty")
	}
	emptied := "<b></b>"
	pAfter = &ProjectNote{Content: "ok", Description: &emptied}
	assert.NoError(t, checkPatchedProjectNote(&ProjectNote{Content: "ok"}, pAfter))
	assert.Nil(t, pAfter.Description)

	// Threat-model notes sanitize content only, as their create/update do.
	tmAfter := &Note{Name: "<b>x</b>", Content: "ok"}
	assert.NoError(t, checkPatchedNote(&Note{Name: "n", Content: "ok"}, tmAfter))
	assert.Equal(t, "<b>x</b>", tmAfter.Name)

	// A passing check returns a nil error interface, not a typed nil.
	assert.Nil(t, checkPatchedNote(&Note{Content: "ok"}, &Note{Content: "ok"}))
}
