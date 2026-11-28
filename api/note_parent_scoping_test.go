package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A note addressed under a parent it does not belong to must behave exactly
// like a nonexistent note, even when the caller is authorized on the parent in
// the path. These tests use the real GORM stores so the scoping is verified in
// the database statements, not in a mock.

const (
	otherTeamID    = "11111111-aaaa-aaaa-aaaa-111111111111"
	otherProjectID = "33333333-aaaa-aaaa-aaaa-333333333333"
)

// setupNoteScopingDB returns a DB with the caller authorized on testTeamID and
// testProjectID (the "A" parents). A second team and project (the "B" parents,
// otherTeamID/otherProjectID) exist but the caller is not a member of B.
func setupNoteScopingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestTeamAuthDB(t)
	require.NoError(t, db.AutoMigrate(&models.TeamNoteRecord{}, &models.ProjectNoteRecord{}))
	seedTeamAuthData(t, db, testTeamID, testUserUUID)
	seedProjectAuthData(t, db, testProjectID, testTeamID)
	require.NoError(t, db.Create(&models.TeamRecord{
		ID: models.DBVarchar(otherTeamID), Name: "Other Team", CreatedByInternalUUID: models.DBVarchar("someone-else"),
	}).Error)
	seedProjectAuthData(t, db, otherProjectID, otherTeamID)

	origP, origT := GlobalProjectNoteStore, GlobalTeamNoteStore
	GlobalProjectNoteStore = NewGormProjectNoteStore(db)
	GlobalTeamNoteStore = NewGormTeamNoteStore(db)
	t.Cleanup(func() { GlobalProjectNoteStore, GlobalTeamNoteStore = origP, origT })
	return db
}

func noteRowCount(t *testing.T, db *gorm.DB, model any, id string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(model).Where("id = ?", id).Count(&n).Error)
	return n
}

func TestNoteParentScoping_ProjectNotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	ctx := context.Background()

	setup := func(t *testing.T) (*gorm.DB, uuid.UUID, string) {
		db := setupNoteScopingDB(t)
		foreign, err := GlobalProjectNoteStore.Create(ctx, &ProjectNote{Name: "B note", Content: "secret"}, otherProjectID)
		require.NoError(t, err)
		return db, uuid.MustParse(testProjectID), foreign.Id.String()
	}
	assertNotFound := func(t *testing.T, code int, body []byte) {
		assert.Equal(t, http.StatusNotFound, code)
		assert.Contains(t, string(body), "Project note not found")
		assert.NotContains(t, string(body), "secret")
	}

	t.Run("get under foreign parent is 404", func(t *testing.T) {
		_, pid, noteID := setup(t)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes/"+noteID)
		TestUsers.Owner.SetContext(c)
		server.GetProjectNote(c, pid, uuid.MustParse(noteID))
		assertNotFound(t, w.Code, w.Body.Bytes())
	})

	t.Run("update under foreign parent is 404 and row unchanged", func(t *testing.T) {
		db, pid, noteID := setup(t)
		body, _ := json.Marshal(ProjectNoteInput{Name: "hijacked", Content: "hijacked"})
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID+"/notes/"+noteID, "application/json", body)
		TestUsers.Owner.SetContext(c)
		server.UpdateProjectNote(c, pid, uuid.MustParse(noteID))
		assertNotFound(t, w.Code, w.Body.Bytes())
		var rec models.ProjectNoteRecord
		require.NoError(t, db.Where("id = ?", noteID).First(&rec).Error)
		assert.Equal(t, "B note", string(rec.Name))
	})

	t.Run("patch under foreign parent is 404 and row unchanged", func(t *testing.T) {
		db, pid, noteID := setup(t)
		body, _ := json.Marshal([]PatchOperation{{Op: "replace", Path: "/name", Value: "hijacked"}})
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID+"/notes/"+noteID, "application/json", body)
		TestUsers.Owner.SetContext(c)
		server.PatchProjectNote(c, pid, uuid.MustParse(noteID))
		assertNotFound(t, w.Code, w.Body.Bytes())
		var rec models.ProjectNoteRecord
		require.NoError(t, db.Where("id = ?", noteID).First(&rec).Error)
		assert.Equal(t, "B note", string(rec.Name))
		assert.Equal(t, otherProjectID, string(rec.ProjectID))
	})

	t.Run("delete under foreign parent is 404 and row survives", func(t *testing.T) {
		db, pid, noteID := setup(t)
		c, w := CreateTestGinContext("DELETE", "/projects/"+testProjectID+"/notes/"+noteID)
		TestUsers.Owner.SetContext(c)
		server.DeleteProjectNote(c, pid, uuid.MustParse(noteID))
		assertNotFound(t, w.Code, w.Body.Bytes())
		assert.Equal(t, int64(1), noteRowCount(t, db, &models.ProjectNoteRecord{}, noteID))
	})

	t.Run("own parent still works", func(t *testing.T) {
		setup(t)
		own, err := GlobalProjectNoteStore.Create(ctx, &ProjectNote{Name: "A note", Content: "c"}, testProjectID)
		require.NoError(t, err)
		c, w := CreateTestGinContext("GET", "/projects/"+testProjectID+"/notes/"+own.Id.String())
		TestUsers.Owner.SetContext(c)
		server.GetProjectNote(c, uuid.MustParse(testProjectID), *own.Id)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestNoteParentScoping_TeamNotes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	ctx := context.Background()

	setup := func(t *testing.T) (*gorm.DB, uuid.UUID, string) {
		db := setupNoteScopingDB(t)
		foreign, err := GlobalTeamNoteStore.Create(ctx, &TeamNote{Name: "B note", Content: "secret"}, otherTeamID)
		require.NoError(t, err)
		return db, uuid.MustParse(testTeamID), foreign.Id.String()
	}
	assertNotFound := func(t *testing.T, code int, body []byte) {
		assert.Equal(t, http.StatusNotFound, code)
		assert.Contains(t, string(body), "Team note not found")
		assert.NotContains(t, string(body), "secret")
	}

	t.Run("get under foreign parent is 404", func(t *testing.T) {
		_, tid, noteID := setup(t)
		c, w := CreateTestGinContext("GET", "/teams/"+testTeamID+"/notes/"+noteID)
		TestUsers.Owner.SetContext(c)
		server.GetTeamNote(c, tid, uuid.MustParse(noteID))
		assertNotFound(t, w.Code, w.Body.Bytes())
	})

	t.Run("update under foreign parent is 404 and row unchanged", func(t *testing.T) {
		db, tid, noteID := setup(t)
		body, _ := json.Marshal(TeamNoteInput{Name: "hijacked", Content: "hijacked"})
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID+"/notes/"+noteID, "application/json", body)
		TestUsers.Owner.SetContext(c)
		server.UpdateTeamNote(c, tid, uuid.MustParse(noteID))
		assertNotFound(t, w.Code, w.Body.Bytes())
		var rec models.TeamNoteRecord
		require.NoError(t, db.Where("id = ?", noteID).First(&rec).Error)
		assert.Equal(t, "B note", string(rec.Name))
	})

	t.Run("patch under foreign parent is 404 and row unchanged", func(t *testing.T) {
		db, tid, noteID := setup(t)
		body, _ := json.Marshal([]PatchOperation{{Op: "replace", Path: "/name", Value: "hijacked"}})
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID+"/notes/"+noteID, "application/json", body)
		TestUsers.Owner.SetContext(c)
		server.PatchTeamNote(c, tid, uuid.MustParse(noteID))
		assertNotFound(t, w.Code, w.Body.Bytes())
		var rec models.TeamNoteRecord
		require.NoError(t, db.Where("id = ?", noteID).First(&rec).Error)
		assert.Equal(t, "B note", string(rec.Name))
		assert.Equal(t, otherTeamID, string(rec.TeamID))
	})

	t.Run("delete under foreign parent is 404 and row survives", func(t *testing.T) {
		db, tid, noteID := setup(t)
		c, w := CreateTestGinContext("DELETE", "/teams/"+testTeamID+"/notes/"+noteID)
		TestUsers.Owner.SetContext(c)
		server.DeleteTeamNote(c, tid, uuid.MustParse(noteID))
		assertNotFound(t, w.Code, w.Body.Bytes())
		assert.Equal(t, int64(1), noteRowCount(t, db, &models.TeamNoteRecord{}, noteID))
	})
}

// Store-level: the parent id is part of the statement, so a wrong parent is
// indistinguishable from a missing row, and nothing is modified.
func TestNoteStores_WrongParentIsNotFound(t *testing.T) {
	ctx := context.Background()
	db := setupNoteScopingDB(t)
	ops := []PatchOperation{{Op: "replace", Path: "/name", Value: "hijacked"}}

	t.Run("project notes", func(t *testing.T) {
		store := NewGormProjectNoteStore(db)
		n, err := store.Create(ctx, &ProjectNote{Name: "B note", Content: "c"}, otherProjectID)
		require.NoError(t, err)
		id := n.Id.String()

		_, err = store.Get(ctx, id, testProjectID)
		assert.ErrorIs(t, err, ErrProjectNoteNotFound)
		_, err = store.Patch(ctx, id, testProjectID, ops, nil)
		assert.ErrorIs(t, err, ErrProjectNoteNotFound)
		assert.ErrorIs(t, store.Delete(ctx, id, testProjectID), ErrProjectNoteNotFound)
		_, err = store.Update(ctx, id, &ProjectNote{Name: "hijacked", Content: "c"}, testProjectID)
		assert.ErrorIs(t, err, ErrProjectNoteNotFound)

		got, err := store.Get(ctx, id, otherProjectID)
		require.NoError(t, err)
		assert.Equal(t, "B note", got.Name)
		_, err = store.Patch(ctx, id, otherProjectID, ops, nil)
		require.NoError(t, err)
		require.NoError(t, store.Delete(ctx, id, otherProjectID))
		assert.Equal(t, int64(0), noteRowCount(t, db, &models.ProjectNoteRecord{}, id))
	})

	t.Run("team notes", func(t *testing.T) {
		store := NewGormTeamNoteStore(db)
		n, err := store.Create(ctx, &TeamNote{Name: "B note", Content: "c"}, otherTeamID)
		require.NoError(t, err)
		id := n.Id.String()

		_, err = store.Get(ctx, id, testTeamID)
		assert.ErrorIs(t, err, ErrTeamNoteNotFound)
		_, err = store.Patch(ctx, id, testTeamID, ops, nil)
		assert.ErrorIs(t, err, ErrTeamNoteNotFound)
		assert.ErrorIs(t, store.Delete(ctx, id, testTeamID), ErrTeamNoteNotFound)
		_, err = store.Update(ctx, id, &TeamNote{Name: "hijacked", Content: "c"}, testTeamID)
		assert.ErrorIs(t, err, ErrTeamNoteNotFound)

		got, err := store.Get(ctx, id, otherTeamID)
		require.NoError(t, err)
		assert.Equal(t, "B note", got.Name)
		_, err = store.Patch(ctx, id, otherTeamID, ops, nil)
		require.NoError(t, err)
		require.NoError(t, store.Delete(ctx, id, otherTeamID))
		assert.Equal(t, int64(0), noteRowCount(t, db, &models.TeamNoteRecord{}, id))
	})
}
