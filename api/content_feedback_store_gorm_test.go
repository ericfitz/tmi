package api

import (
	"context"
	"testing"

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
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.ThreatModel{}, &models.ContentFeedback{}))

	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: strPtr("alice-test"),
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	tm := &models.ThreatModel{
		ID:                    uuid.New().String(),
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
		TargetType:    "note",
		TargetID:      targetID,
		Sentiment:     "down",
		ClientID:      "tmi-ux",
		CreatedByUUID: user.InternalUUID,
	}
	require.NoError(t, repo.Create(context.Background(), fb))
	require.NotEmpty(t, fb.ID)

	got, err := repo.Get(context.Background(), fb.ID)
	require.NoError(t, err)
	assert.Equal(t, tm.ID, got.ThreatModelID)
	assert.Equal(t, "note", got.TargetType)
	assert.Equal(t, targetID, got.TargetID)
	assert.Equal(t, "down", got.Sentiment)
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
			TargetType:          tc.ttype,
			TargetID:            uuid.New().String(),
			Sentiment:           tc.sentiment,
			FalsePositiveReason: tc.fpr,
			ClientID:            "tmi-ux",
			CreatedByUUID:       user.InternalUUID,
		}
		require.NoError(t, repo.Create(ctx, fb))
	}

	rows, err := repo.List(ctx, tm.ID, ContentFeedbackListFilter{}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 4)

	rows, err = repo.List(ctx, tm.ID, ContentFeedbackListFilter{TargetType: "threat"}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 2)

	rows, err = repo.List(ctx, tm.ID, ContentFeedbackListFilter{FalsePositiveReason: "duplicate"}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	total, err := repo.Count(ctx, tm.ID, ContentFeedbackListFilter{Sentiment: "up"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
}

func TestGormContentFeedbackRepository_GetNotFound(t *testing.T) {
	db, _, _ := setupContentFeedbackTestDB(t)
	repo := NewGormContentFeedbackRepository(db)
	_, err := repo.Get(context.Background(), uuid.New().String())
	assert.ErrorIs(t, err, dberrors.ErrNotFound)
}
