package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

const patchAttackContent = `<script>x</script>[x](javascript:alert(1))`

// assertPatchedValueSafe checks one patch outcome: a 400, or a stored value
// that is sanitized (no script tag, no unsafe link).
func assertPatchedValueSafe(t *testing.T, code int, ops []PatchOperation, path string) {
	t.Helper()
	if code == http.StatusBadRequest {
		return
	}
	assert.Equal(t, http.StatusOK, code)
	for _, op := range ops {
		if op.Path != path {
			continue
		}
		v, _ := op.Value.(string)
		assert.NotContains(t, v, "<script", "patched %s must be sanitized", path)
		if path != "/content" {
			continue // plain-text fields are not rendered as markdown links
		}
		assert.False(t, markdownHasUnsafeLink(v), "patched %s keeps an unsafe link: %q", path, v)
	}
}

func TestPatchTeamNote_SanitizesFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	for _, path := range []string{"/content", "/name", "/description"} {
		t.Run(path, func(t *testing.T) {
			store := newMockTeamNoteStore()
			seedTeamNoteInStore(store, testTeamNoteID, true)
			saveTeamNoteStore(t, store)
			db := setupTestTeamAuthDB(t)
			seedTeamAuthData(t, db, testTeamID, testUserUUID)

			body, _ := json.Marshal([]PatchOperation{{Op: "replace", Path: path, Value: patchAttackContent}})
			teamUUID, _ := uuid.Parse(testTeamID)
			noteUUID, _ := uuid.Parse(testTeamNoteID)
			c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+testTeamNoteID, "application/json", body)
			TestUsers.Owner.SetContext(c)
			server.PatchTeamNote(c, teamUUID, noteUUID)

			assertPatchedValueSafe(t, w.Code, store.patchedOps, path)
		})
	}
}

func TestPatchProjectNote_SanitizesFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	for _, path := range []string{"/content", "/name", "/description"} {
		t.Run(path, func(t *testing.T) {
			store := newMockProjectNoteStore()
			seedProjectNoteInStore(store, testProjectNoteID, true)
			saveProjectNoteStore(t, store)
			db := setupTestTeamAuthDB(t)
			seedTeamAuthData(t, db, testTeamID, testUserUUID)
			seedProjectAuthData(t, db, testProjectID, testTeamID)

			body, _ := json.Marshal([]PatchOperation{{Op: "replace", Path: path, Value: patchAttackContent}})
			projectUUID, _ := uuid.Parse(testProjectID)
			noteUUID, _ := uuid.Parse(testProjectNoteID)
			c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+testProjectNoteID, "application/json", body)
			TestUsers.Owner.SetContext(c)
			server.PatchProjectNote(c, projectUUID, noteUUID)

			assertPatchedValueSafe(t, w.Code, store.patchedOps, path)
		})
	}
}

func TestSanitizeNotePatchOperations(t *testing.T) {
	ops := []PatchOperation{
		{Op: "replace", Path: "/name", Value: "<b>n</b>"},
		{Op: "replace", Path: "/sharable", Value: false},
	}
	assert.Nil(t, sanitizeNotePatchOperations(ops))
	assert.Equal(t, "n", ops[0].Value)
	assert.Equal(t, false, ops[1].Value)

	bad := []PatchOperation{{Op: "add", Path: "/content", Value: "<script>x</script>"}}
	err := sanitizeNotePatchOperations(bad)
	if assert.NotNil(t, err) {
		assert.Equal(t, http.StatusBadRequest, err.Status)
	}
}
