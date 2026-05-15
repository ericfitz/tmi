package api

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupContentFeedbackTestDB creates an in-memory SQLite DB with User, ThreatModel,
// and ContentFeedback tables. FK constraints are disabled for SQLite compatibility.
func setupContentFeedbackTestDB(t *testing.T) (*gorm.DB, *models.User, *models.ThreatModel) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.ThreatModel{}, &models.Threat{}, &models.Diagram{}, &models.Note{}, &models.ContentFeedback{}))

	user := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: strPtr("alice-test"),
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(uuid.New().String()),
		OwnerInternalUUID:     user.InternalUUID,
		CreatedByInternalUUID: user.InternalUUID,
		Name:                  "Test TM",
	}
	require.NoError(t, db.Create(tm).Error)

	return db, user, tm
}

func TestGormContentFeedbackRepository_CreateAndGet(t *testing.T) {
	db, user, tm := setupContentFeedbackTestDB(t)
	repo := NewGormContentFeedbackRepository(db)

	targetID := uuid.New().String()
	fb := &models.ContentFeedback{
		ThreatModelID: tm.ID,
		TargetType:    models.DBVarchar("note"),
		TargetID:      models.DBVarchar(targetID),
		Sentiment:     models.DBVarchar("down"),
		ClientID:      models.DBVarchar("tmi-ux"),
		CreatedByUUID: user.InternalUUID,
	}
	require.NoError(t, repo.Create(context.Background(), fb))
	require.NotEmpty(t, fb.ID)

	got, err := repo.Get(context.Background(), string(fb.ID))
	require.NoError(t, err)
	assert.Equal(t, tm.ID, got.ThreatModelID)
	assert.Equal(t, "note", string(got.TargetType))
	assert.Equal(t, targetID, string(got.TargetID))
	assert.Equal(t, "down", string(got.Sentiment))
}

func TestGormContentFeedbackRepository_ListFilters(t *testing.T) {
	db, user, tm := setupContentFeedbackTestDB(t)
	repo := NewGormContentFeedbackRepository(db)
	ctx := context.Background()

	cases := []struct {
		ttype     string
		sentiment string
		fpr       *string
	}{
		{"note", "up", nil},
		{"diagram", "up", nil},
		{"threat", "down", strPtr("detection_misfired")},
		{"threat", "down", strPtr("duplicate")},
	}
	for _, tc := range cases {
		fb := &models.ContentFeedback{
			ThreatModelID:       tm.ID,
			TargetType:          models.DBVarchar(tc.ttype),
			TargetID:            models.DBVarchar(uuid.New().String()),
			Sentiment:           models.DBVarchar(tc.sentiment),
			FalsePositiveReason: models.NewNullableDBVarchar(tc.fpr),
			ClientID:            "tmi-ux",
			CreatedByUUID:       user.InternalUUID,
		}
		require.NoError(t, repo.Create(ctx, fb))
	}

	rows, err := repo.List(ctx, string(tm.ID), ContentFeedbackListFilter{}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 4)

	rows, err = repo.List(ctx, string(tm.ID), ContentFeedbackListFilter{TargetType: "threat"}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 2)

	rows, err = repo.List(ctx, string(tm.ID), ContentFeedbackListFilter{FalsePositiveReason: "duplicate"}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	total, err := repo.Count(ctx, string(tm.ID), ContentFeedbackListFilter{Sentiment: "up"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
}

func TestGormContentFeedbackRepository_GetNotFound(t *testing.T) {
	db, _, _ := setupContentFeedbackTestDB(t)
	repo := NewGormContentFeedbackRepository(db)
	_, err := repo.Get(context.Background(), uuid.New().String())
	assert.ErrorIs(t, err, dberrors.ErrNotFound)
}

// TestContentFeedback_CreatedAt_NoAutoCreateTimeTag pins the GORM tag for
// the CreatedAt column: it must NOT use autoCreateTime, mirroring the Threat
// model pattern (api/models/models.go ~L284) for Oracle high-volume insert
// compatibility.
func TestContentFeedback_CreatedAt_NoAutoCreateTimeTag(t *testing.T) {
	field, ok := reflect.TypeOf(models.ContentFeedback{}).FieldByName("CreatedAt")
	require.True(t, ok, "CreatedAt field must exist")
	tag := field.Tag.Get("gorm")
	assert.False(t, strings.Contains(tag, "autoCreateTime"),
		"ContentFeedback.CreatedAt must not use autoCreateTime (Oracle compat); got gorm tag %q", tag)
}

// TestGormContentFeedbackRepository_Create_SetsCreatedAtExplicitly verifies the
// Create path populates CreatedAt itself (no autoCreateTime).
func TestGormContentFeedbackRepository_Create_SetsCreatedAtExplicitly(t *testing.T) {
	db, user, tm := setupContentFeedbackTestDB(t)
	repo := NewGormContentFeedbackRepository(db)

	fb := &models.ContentFeedback{
		ThreatModelID: tm.ID,
		TargetType:    "note",
		TargetID:      models.DBVarchar(uuid.New().String()),
		Sentiment:     "down",
		ClientID:      "tmi-ux",
		CreatedByUUID: user.InternalUUID,
	}
	require.True(t, fb.CreatedAt.IsZero(), "precondition: CreatedAt zero before Create")

	before := time.Now().UTC().Add(-time.Second)
	require.NoError(t, repo.Create(context.Background(), fb))
	after := time.Now().UTC().Add(time.Second)

	assert.False(t, fb.CreatedAt.IsZero(), "CreatedAt must be populated by repo")
	assert.True(t, !fb.CreatedAt.Before(before) && !fb.CreatedAt.After(after),
		"CreatedAt %v must be within [%v, %v]", fb.CreatedAt, before, after)
	assert.Equal(t, time.UTC, fb.CreatedAt.Location(), "CreatedAt must be UTC")
}

// TestGormContentFeedbackRepository_CreateWithTargetCheck_SetsCreatedAtExplicitly
// verifies the transactional create path also populates CreatedAt itself.
func TestGormContentFeedbackRepository_CreateWithTargetCheck_SetsCreatedAtExplicitly(t *testing.T) {
	db, user, tm := setupContentFeedbackTestDB(t)
	repo := NewGormContentFeedbackRepository(db)

	// Create a note row that the feedback targets.
	note := &models.Note{
		ID:            models.DBVarchar(uuid.New().String()),
		ThreatModelID: tm.ID,
		Name:          "Test note",
		Content:       models.DBText("body"),
	}
	require.NoError(t, db.Create(note).Error)

	fb := &models.ContentFeedback{
		ThreatModelID: tm.ID,
		TargetType:    "note",
		TargetID:      note.ID,
		Sentiment:     "up",
		ClientID:      "tmi-ux",
		CreatedByUUID: user.InternalUUID,
	}
	require.True(t, fb.CreatedAt.IsZero(), "precondition: CreatedAt zero before Create")

	before := time.Now().UTC().Add(-time.Second)
	err := repo.CreateWithTargetCheck(context.Background(), fb, ContentFeedbackTargetRef{
		ThreatModelID: string(tm.ID),
		TargetID:      string(note.ID),
		Table:         models.Note{}.TableName(),
	})
	after := time.Now().UTC().Add(time.Second)
	require.NoError(t, err)

	assert.False(t, fb.CreatedAt.IsZero(), "CreatedAt must be populated by repo")
	assert.True(t, !fb.CreatedAt.Before(before) && !fb.CreatedAt.After(after),
		"CreatedAt %v must be within [%v, %v]", fb.CreatedAt, before, after)
}
