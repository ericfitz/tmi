package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// #1153: survey and survey-response creates must return the timestamps the
// database holds, at the microsecond resolution it stores.

// setupSurveyTimestampDB builds an in-memory DB whose NowFunc carries
// sub-microsecond digits, so a create that echoes GORM-generated timestamps
// fails on every OS (macOS clocks stop at microseconds; Linux's do not).
func setupSurveyTimestampDB(t *testing.T) (*gorm.DB, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
		NowFunc:                                  func() time.Time { return time.Now().UTC().Add(761 * time.Nanosecond) },
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.Group{},
		&models.Metadata{},
		&models.SurveyTemplate{},
		&models.SurveyResponse{},
		&models.SurveyResponseAccess{},
	))

	user := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(strPtr("survey-ts-test-user")),
		Email:          models.DBVarchar("alice@example.com"),
		Name:           models.DBVarchar("Alice"),
	}
	require.NoError(t, db.Create(user).Error)
	return db, user
}

func TestGormSurveyStore_CreateReturnsStoredTimestamps(t *testing.T) {
	ctx := context.Background()
	db, user := setupSurveyTimestampDB(t)
	store := NewGormSurveyStore(db)

	survey := &Survey{
		Name:       "Timestamp Survey",
		Version:    "1.0.0",
		SurveyJson: map[string]any{"pages": []any{}},
	}
	require.NoError(t, store.Create(ctx, survey, string(user.InternalUUID)))
	assertStoredTimes(t, db, &models.SurveyTemplate{}, survey.Id.String(), survey.CreatedAt, survey.ModifiedAt)
}

func TestGormSurveyResponseStore_CreateReturnsStoredTimestamps(t *testing.T) {
	ctx := context.Background()
	db, user := setupSurveyTimestampDB(t)

	tmpl := &models.SurveyTemplate{
		ID:                    models.DBVarchar(uuid.New().String()),
		Name:                  models.DBVarchar("Timestamp Survey"),
		Version:               models.DBVarchar("1.0.0"),
		CreatedByInternalUUID: user.InternalUUID,
	}
	require.NoError(t, db.Create(tmpl).Error)

	for _, confidential := range []bool{false, true} {
		response := &SurveyResponse{
			SurveyId:       uuid.MustParse(string(tmpl.ID)),
			IsConfidential: &confidential,
		}
		store := NewGormSurveyResponseStore(db)
		require.NoError(t, store.Create(ctx, response, string(user.InternalUUID)))
		assertStoredTimes(t, db, &models.SurveyResponse{}, response.Id.String(), response.CreatedAt, response.ModifiedAt)
	}
}
