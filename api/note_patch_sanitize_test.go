package api

import (
	"bytes"
	"encoding/json"
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

// assertPatchResultSafe checks one handler outcome: a 400, or a 200 whose
// operations, applied to the existing note, leave no script markup in any
// field and no unsafe link in content.
func assertPatchResultSafe(t *testing.T, code int, existing any, ops []PatchOperation) {
	t.Helper()
	if code == http.StatusBadRequest {
		return
	}
	assert.Equal(t, http.StatusOK, code)
	raw, _ := json.Marshal(existing)
	var before map[string]any
	assert.NoError(t, json.Unmarshal(raw, &before))
	after, err := ApplyPatchOperations(before, ops)
	assert.NoError(t, err)
	for _, field := range []string{"content", "name", "description"} {
		v, _ := after[field].(string)
		assert.NotContains(t, v, "<script", "patched %s must be sanitized", field)
	}
	content, _ := after["content"].(string)
	assert.False(t, markdownHasUnsafeLink(content), "patched content keeps an unsafe link: %q", content)
}

func TestPatchTeamNote_SanitizesPatchedResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	for name, attack := range patchAttacks() {
		t.Run(name, func(t *testing.T) {
			store := newMockTeamNoteStore()
			seedTeamNoteInStore(store, testTeamNoteID, true)
			saveTeamNoteStore(t, store)
			db := setupTestTeamAuthDB(t)
			seedTeamAuthData(t, db, testTeamID, testUserUUID)

			body, _ := json.Marshal(attack)
			teamUUID, _ := uuid.Parse(testTeamID)
			noteUUID, _ := uuid.Parse(testTeamNoteID)
			c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", body)
			TestUsers.Owner.SetContext(c)
			existing := *store.notes[testTeamNoteID]
			server.PatchTeamNote(c, teamUUID, noteUUID)

			assertPatchResultSafe(t, w.Code, existing, store.patchedOps)
		})
	}
}

func TestPatchProjectNote_SanitizesPatchedResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	for name, attack := range patchAttacks() {
		t.Run(name, func(t *testing.T) {
			store := newMockProjectNoteStore()
			seedProjectNoteInStore(store, testProjectNoteID, true)
			saveProjectNoteStore(t, store)
			db := setupTestTeamAuthDB(t)
			seedTeamAuthData(t, db, testTeamID, testUserUUID)
			seedProjectAuthData(t, db, testProjectID, testTeamID)

			body, _ := json.Marshal(attack)
			projectUUID, _ := uuid.Parse(testProjectID)
			noteUUID, _ := uuid.Parse(testProjectNoteID)
			c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", body)
			TestUsers.Owner.SetContext(c)
			existing := *store.notes[testProjectNoteID]
			server.PatchProjectNote(c, projectUUID, noteUUID)

			assertPatchResultSafe(t, w.Code, existing, store.patchedOps)
		})
	}
}

func TestPatchNote_SanitizesPatchedResult(t *testing.T) {
	for name, attack := range patchAttacks() {
		t.Run(name, func(t *testing.T) {
			r, mockStore := setupNoteSubResourceHandler()
			noteID := testUUID2
			noteUUID, _ := uuid.Parse(noteID)
			existing := &Note{Id: &noteUUID, Name: "n", Content: "ok"}
			var captured []PatchOperation

			mockStore.On("Get", mock.Anything, noteID).Return(existing, nil)
			mockStore.On("Patch", mock.Anything, noteID, mock.AnythingOfType("[]api.PatchOperation")).
				Run(func(args mock.Arguments) { captured = args.Get(2).([]PatchOperation) }).
				Return(existing, nil)

			body, _ := json.Marshal(attack)
			req := httptest.NewRequest("PATCH", "/threat_models/"+testUUID1+"/notes/"+noteID, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json-patch+json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// The TM note path sanitizes content only (as its create/update do).
			if w.Code == http.StatusOK {
				before, _ := json.Marshal(existing)
				var m map[string]any
				assert.NoError(t, json.Unmarshal(before, &m))
				after, err := ApplyPatchOperations(m, captured)
				assert.NoError(t, err)
				content, _ := after["content"].(string)
				assert.NotContains(t, content, "<script")
				assert.False(t, markdownHasUnsafeLink(content), "patched content keeps an unsafe link: %q", content)
			} else {
				assert.Equal(t, http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestSanitizePatchedNote(t *testing.T) {
	existing := &Note{Name: "n", Content: "ok"}

	// Unchanged content is not re-checked.
	ops, err := sanitizePatchedNote(existing, []PatchOperation{{Op: "replace", Path: "/name", Value: "new"}}, false)
	assert.Nil(t, err)
	assert.Len(t, ops, 1)

	// Sanitized content is appended as a trailing replace.
	ops, err = sanitizePatchedNote(existing, []PatchOperation{{Op: "replace", Path: "/content", Value: "a <b onclick=x>b</b> " + patchUnsafeLink}}, false)
	assert.Nil(t, err)
	assert.Len(t, ops, 2)
	assert.Equal(t, "/content", ops[1].Path)

	// Content that sanitizes to nothing is a 400.
	_, err = sanitizePatchedNote(existing, []PatchOperation{{Op: "replace", Path: "/content", Value: "<script>x</script>"}}, false)
	if assert.NotNil(t, err) {
		assert.Equal(t, http.StatusBadRequest, err.Status)
	}

	// Inapplicable patches are left for the store to reject.
	ops, err = sanitizePatchedNote(existing, []PatchOperation{{Op: "remove", Path: "/nope"}}, false)
	assert.Nil(t, err)
	assert.Len(t, ops, 1)
}
