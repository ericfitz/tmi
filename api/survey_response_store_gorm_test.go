package api

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupSurveyResponseUpdateTestDB creates an in-memory SQLite DB with the
// tables needed for GormSurveyResponseStore.Update tests, seeded with an
// owning user and a survey template.
func setupSurveyResponseUpdateTestDB(t *testing.T) (*gorm.DB, *models.User, *models.SurveyTemplate) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.SurveyTemplate{},
		&models.SurveyResponse{},
	))

	user := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(strPtr("survey-update-test-user")),
		Email:          models.DBVarchar("owner@example.com"),
		Name:           models.DBVarchar("Owner"),
	}
	require.NoError(t, db.Create(user).Error)

	tmpl := &models.SurveyTemplate{
		ID:                    models.DBVarchar(uuid.New().String()),
		Name:                  models.DBVarchar("Test Survey"),
		Version:               models.DBVarchar("1.0.0"),
		CreatedByInternalUUID: user.InternalUUID,
	}
	require.NoError(t, db.Create(tmpl).Error)

	return db, user, tmpl
}

// TestGormSurveyResponseStore_Update_AnswersUIStateJSONRaw locks in that the
// Update path routes answers/ui_state through models.JSONRaw rather than a
// bare []byte (#697): empty-but-non-nil values round-trip as "{}", and nil
// clears the stored value.
func TestGormSurveyResponseStore_Update_AnswersUIStateJSONRaw(t *testing.T) {
	db, user, tmpl := setupSurveyResponseUpdateTestDB(t)
	store := NewGormSurveyResponseStore(db)
	ctx := context.Background()

	responseID := uuid.New()
	seed := &models.SurveyResponse{
		ID:                models.DBVarchar(responseID.String()),
		TemplateID:        tmpl.ID,
		TemplateVersion:   tmpl.Version,
		Status:            models.DBVarchar(ResponseStatusDraft),
		OwnerInternalUUID: models.NewNullableDBVarchar(strPtr(string(user.InternalUUID))),
	}
	require.NoError(t, db.Create(seed).Error)

	t.Run("empty-but-non-nil answers and ui_state round-trip as {}", func(t *testing.T) {
		emptyAnswers := map[string]any{}
		emptyUIState := map[string]any{}
		update := &SurveyResponse{
			Id:      &responseID,
			Answers: &emptyAnswers,
			UiState: &emptyUIState,
		}
		require.NoError(t, store.Update(ctx, update))

		var stored models.SurveyResponse
		require.NoError(t, db.First(&stored, "id = ?", responseID.String()).Error)
		assert.JSONEq(t, "{}", string(stored.Answers))
		assert.JSONEq(t, "{}", string(stored.UIState))
	})

	t.Run("nil answers and ui_state clear the stored value", func(t *testing.T) {
		update := &SurveyResponse{
			Id:      &responseID,
			Answers: nil,
			UiState: nil,
		}
		require.NoError(t, store.Update(ctx, update))

		var stored models.SurveyResponse
		require.NoError(t, db.First(&stored, "id = ?", responseID.String()).Error)
		assert.Empty(t, []byte(stored.Answers))
		assert.Empty(t, []byte(stored.UIState))
	})
}

// TestGormSurveyResponseStore_Update_BindsAnswersAsValuer pins the actual
// defect behind #697, which the SQLite round-trip test above cannot observe:
// GORM's map-based Updates() passes map values straight through to
// database/sql as bind parameters without invoking the destination column's
// Valuer, so a bare []byte silently bypasses models.JSONRaw.Value() (the
// Oracle CLOB string-binding and empty-to-NULL normalization). This registers
// a GORM callback to capture the actual bound vars produced by
// GormSurveyResponseStore.Update itself, so a regression (reverting either
// assignment site back to a bare []byte) fails this test regardless of which
// database backend eventually executes the query.
func TestGormSurveyResponseStore_Update_BindsAnswersAsValuer(t *testing.T) {
	db, user, tmpl := setupSurveyResponseUpdateTestDB(t)
	store := NewGormSurveyResponseStore(db)
	ctx := context.Background()

	responseID := uuid.New()
	seed := &models.SurveyResponse{
		ID:                models.DBVarchar(responseID.String()),
		TemplateID:        tmpl.ID,
		TemplateVersion:   tmpl.Version,
		Status:            models.DBVarchar(ResponseStatusDraft),
		OwnerInternalUUID: models.NewNullableDBVarchar(strPtr(string(user.InternalUUID))),
	}
	require.NoError(t, db.Create(seed).Error)

	var capturedVars []any
	require.NoError(t, db.Callback().Update().After("gorm:update").
		Register("test:capture-update-vars", func(tx *gorm.DB) {
			capturedVars = append(capturedVars, tx.Statement.Vars...)
		}))
	defer func() {
		_ = db.Callback().Update().Remove("test:capture-update-vars")
	}()

	answers := map[string]any{"q1": "a1"}
	uiState := map[string]any{"step": 1}
	update := &SurveyResponse{Id: &responseID, Answers: &answers, UiState: &uiState}
	require.NoError(t, store.Update(ctx, update))

	require.NotEmpty(t, capturedVars, "expected the update callback to capture bound vars")
	foundValuer := false
	for _, v := range capturedVars {
		if b, ok := v.([]byte); ok {
			t.Fatalf("bare []byte bound as an update var: %q (bypasses JSONRaw.Value on Oracle)", b)
		}
		if _, ok := v.(driver.Valuer); ok {
			foundValuer = true
		}
	}
	assert.True(t, foundValuer, "expected answers/ui_state to bind as a driver.Valuer (models.JSONRaw), not a raw type")
}
