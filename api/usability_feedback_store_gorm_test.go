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

const aliceTestProviderID = "alice-test"

// setupUsabilityFeedbackTestDB creates an in-memory SQLite DB with User and UsabilityFeedback tables.
func setupUsabilityFeedbackTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.UsabilityFeedback{}))
	return db
}

func TestGormUsabilityFeedbackRepository_CreateAndGet(t *testing.T) {
	db := setupUsabilityFeedbackTestDB(t)
	repo := NewGormUsabilityFeedbackRepository(db)

	// Create the prerequisite user row.
	aliceProviderIDVal := aliceTestProviderID
	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: &aliceProviderIDVal,
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	fb := &models.UsabilityFeedback{
		Sentiment:     "up",
		Surface:       "tm_list",
		ClientID:      "tmi-ux",
		CreatedByUUID: user.InternalUUID,
	}
	require.NoError(t, repo.Create(context.Background(), fb))
	require.NotEmpty(t, fb.ID)

	got, err := repo.Get(context.Background(), fb.ID)
	require.NoError(t, err)
	assert.Equal(t, "up", got.Sentiment)
	assert.Equal(t, "tm_list", got.Surface)
	assert.Equal(t, "tmi-ux", got.ClientID)
	assert.Equal(t, user.InternalUUID, got.CreatedByUUID)
	assert.False(t, got.CreatedAt.IsZero())
}

func TestGormUsabilityFeedbackRepository_ListWithFilters(t *testing.T) {
	db := setupUsabilityFeedbackTestDB(t)
	repo := NewGormUsabilityFeedbackRepository(db)
	ctx := context.Background()

	aliceProviderIDVal2 := aliceTestProviderID
	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: &aliceProviderIDVal2,
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	cases := []struct {
		sentiment string
		surface   string
		clientID  string
	}{
		{"up", "tm_list", "tmi-ux"},
		{"up", "dfd_editor.toolbar", "tmi-ux"},
		{"down", "tm_list", "tmi-ux"},
		{"down", "tm_list", "tmi-cli"},
	}
	for _, tc := range cases {
		fb := &models.UsabilityFeedback{
			Sentiment:     tc.sentiment,
			Surface:       tc.surface,
			ClientID:      tc.clientID,
			CreatedByUUID: user.InternalUUID,
		}
		require.NoError(t, repo.Create(ctx, fb))
	}

	// No filter — all 4 rows.
	rows, err := repo.List(ctx, UsabilityFeedbackListFilter{}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 4)

	// Filter by sentiment=up — 2 rows.
	rows, err = repo.List(ctx, UsabilityFeedbackListFilter{Sentiment: "up"}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 2)

	// Filter by surface=tm_list — 3 rows.
	rows, err = repo.List(ctx, UsabilityFeedbackListFilter{Surface: "tm_list"}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 3)

	// Filter by client_id=tmi-cli — 1 row.
	rows, err = repo.List(ctx, UsabilityFeedbackListFilter{ClientID: "tmi-cli"}, 0, 100)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, "down", rows[0].Sentiment)

	// Pagination: limit=2, offset=0 then limit=2, offset=2.
	page1, err := repo.List(ctx, UsabilityFeedbackListFilter{}, 0, 2)
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	page2, err := repo.List(ctx, UsabilityFeedbackListFilter{}, 2, 2)
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.NotEqual(t, page1[0].ID, page2[0].ID)

	// Count.
	total, err := repo.Count(ctx, UsabilityFeedbackListFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
}

func TestGormUsabilityFeedbackRepository_GetNotFound(t *testing.T) {
	db := setupUsabilityFeedbackTestDB(t)
	repo := NewGormUsabilityFeedbackRepository(db)
	_, err := repo.Get(context.Background(), uuid.New().String())
	assert.ErrorIs(t, err, dberrors.ErrNotFound)
}
