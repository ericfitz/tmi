package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupNoteTestDB creates an in-memory SQLite DB with all tables needed for NoteRepository tests.
func setupNoteTestDB(t *testing.T) (*gorm.DB, *models.User, *models.ThreatModel) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.ThreatModel{},
		&models.Note{},
		&models.Metadata{},
		&models.AliasCounter{},
	))

	user := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(strPtr("note-test-user")),
		Email:          models.DBVarchar("alice@example.com"),
		Name:           models.DBVarchar("Alice"),
	}
	require.NoError(t, db.Create(user).Error)

	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(uuid.New().String()),
		OwnerInternalUUID:     user.InternalUUID,
		CreatedByInternalUUID: user.InternalUUID,
		Name:                  models.DBVarchar("Test TM for Notes"),
	}
	require.NoError(t, db.Create(tm).Error)

	return db, user, tm
}

func TestGormNoteRepository_CreateAssignsAlias(t *testing.T) {
	db, _, tm := setupNoteTestDB(t)
	repo := NewGormNoteRepository(db, nil, nil)
	ctx := context.Background()

	noteID := uuid.New()
	note := &Note{
		Id:      &noteID,
		Name:    "Test Note",
		Content: "Some note content",
	}

	require.NoError(t, repo.Create(ctx, note, string(tm.ID)))

	stored, err := repo.Get(ctx, noteID.String())
	require.NoError(t, err)
	require.NotNil(t, stored.Alias)
	assert.GreaterOrEqual(t, *stored.Alias, int32(1))
}

// backdateNote forces a note's stored timestamps into the past so a later
// update must visibly move modified_at.
func backdateNote(t *testing.T, db *gorm.DB, noteID string, at time.Time) {
	t.Helper()
	require.NoError(t, db.Model(&models.Note{}).Where("id = ?", noteID).
		UpdateColumns(map[string]any{"created_at": at, "modified_at": at}).Error)
}

// storedNoteTimes reads the timestamps the database holds for a note.
func storedNoteTimes(t *testing.T, db *gorm.DB, noteID string) (time.Time, time.Time) {
	t.Helper()
	var row models.Note
	require.NoError(t, db.Select("created_at", "modified_at").First(&row, "id = ?", noteID).Error)
	return row.CreatedAt, row.ModifiedAt
}

// #1149: a PUT-style update (timestamps absent from the body) must return the
// stored created_at and the new modified_at.
func TestGormNoteRepository_UpdateReturnsStoredTimestamps(t *testing.T) {
	db, _, tm := setupNoteTestDB(t)
	repo := NewGormNoteRepository(db, nil, nil)
	ctx := context.Background()

	noteID := uuid.New()
	require.NoError(t, repo.Create(ctx, &Note{Id: &noteID, Name: "n", Content: "c"}, string(tm.ID)))
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	backdateNote(t, db, noteID.String(), old)

	update := &Note{Id: &noteID, Name: "n2", Content: "c2"}
	require.NoError(t, repo.Update(ctx, update, string(tm.ID)))

	created, modified := storedNoteTimes(t, db, noteID.String())
	require.True(t, modified.After(old), "update must move modified_at in the database")
	require.NotNil(t, update.CreatedAt)
	require.NotNil(t, update.ModifiedAt)
	assert.True(t, created.Equal(*update.CreatedAt), "created_at: got %v, stored %v", *update.CreatedAt, created)
	assert.True(t, modified.Equal(*update.ModifiedAt), "modified_at: got %v, stored %v", *update.ModifiedAt, modified)
}

// #1149: a PATCH returns the note loaded before the write; its modified_at
// must be the new stored value, not the pre-update one.
func TestGormNoteRepository_PatchReturnsNewModifiedAt(t *testing.T) {
	db, _, tm := setupNoteTestDB(t)
	repo := NewGormNoteRepository(db, nil, nil)
	ctx := context.Background()

	noteID := uuid.New()
	require.NoError(t, repo.Create(ctx, &Note{Id: &noteID, Name: "n", Content: "c"}, string(tm.ID)))
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	backdateNote(t, db, noteID.String(), old)

	patched, err := repo.Patch(ctx, noteID.String(),
		[]PatchOperation{{Op: "replace", Path: "/content", Value: "patched"}}, nil)
	require.NoError(t, err)

	_, modified := storedNoteTimes(t, db, noteID.String())
	require.True(t, modified.After(old), "patch must move modified_at in the database")
	require.NotNil(t, patched.ModifiedAt)
	assert.True(t, modified.Equal(*patched.ModifiedAt), "modified_at: got %v, stored %v", *patched.ModifiedAt, modified)
}
