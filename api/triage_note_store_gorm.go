package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// GormTriageNoteStore implements TriageNoteStore using GORM
type GormTriageNoteStore struct {
	db *gorm.DB
}

// NewGormTriageNoteStore creates a new GORM-backed triage note store
func NewGormTriageNoteStore(db *gorm.DB) *GormTriageNoteStore {
	return &GormTriageNoteStore{db: db}
}

// Create creates a new triage note with an auto-assigned sequential ID
func (s *GormTriageNoteStore) Create(ctx context.Context, note *TriageNote, surveyResponseID string, creatorInternalUUID string) error {
	logger := slogging.Get()
	logger.Debug("Creating triage note in survey response: %s", surveyResponseID)

	now := time.Now().UTC()

	model := models.TriageNote{
		SurveyResponseID:       surveyResponseID,
		Name:                   note.Name,
		Content:                models.DBText(note.Content),
		CreatedByInternalUUID:  &creatorInternalUUID,
		ModifiedByInternalUUID: &creatorInternalUUID,
		CreatedAt:              now,
		ModifiedAt:             now,
	}

	// BeforeCreate hook on the model handles sequential ID assignment
	if err := s.db.WithContext(ctx).Create(&model).Error; err != nil {
		logger.Error("Failed to create triage note: %v", err)
		return fmt.Errorf("failed to create triage note: %w", err)
	}

	// Populate the API response fields
	note.Id = &model.ID
	note.CreatedAt = &now
	note.ModifiedAt = &now

	// Load the creator user for the response
	var creator models.User
	if err := s.db.WithContext(ctx).Where("internal_uuid = ?", creatorInternalUUID).First(&creator).Error; err == nil {
		user := userModelToAPIUser(&creator)
		note.CreatedBy = user
		note.ModifiedBy = user
	}

	logger.Debug("Successfully created triage note %d in survey response %s", model.ID, surveyResponseID)
	return nil
}

// Get retrieves a specific triage note by survey response ID and note ID
func (s *GormTriageNoteStore) Get(ctx context.Context, surveyResponseID string, noteID int) (*TriageNote, error) {
	logger := slogging.Get()
	logger.Debug("Getting triage note %d for survey response %s", noteID, surveyResponseID)

	var model models.TriageNote
	err := s.db.WithContext(ctx).
		Preload("CreatedBy").
		Preload("ModifiedBy").
		Where("survey_response_id = ? AND id = ?", surveyResponseID, noteID).
		First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("triage note not found: %w", err)
	}

	return s.modelToAPI(&model), nil
}

// List returns triage notes for a survey response with pagination, ordered by ID ascending
func (s *GormTriageNoteStore) List(ctx context.Context, surveyResponseID string, offset, limit int) ([]TriageNote, error) {
	logger := slogging.Get()
	logger.Debug("Listing triage notes for survey response %s (offset: %d, limit: %d)", surveyResponseID, offset, limit)

	var noteModels []models.TriageNote
	err := s.db.WithContext(ctx).
		Preload("CreatedBy").
		Where("survey_response_id = ?", surveyResponseID).
		Order("id ASC").
		Offset(offset).
		Limit(limit).
		Find(&noteModels).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list triage notes: %w", err)
	}

	notes := make([]TriageNote, 0, len(noteModels))
	for i := range noteModels {
		notes = append(notes, *s.modelToAPI(&noteModels[i]))
	}

	return notes, nil
}

// Count returns the total number of triage notes for a survey response
func (s *GormTriageNoteStore) Count(ctx context.Context, surveyResponseID string) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.TriageNote{}).
		Where("survey_response_id = ?", surveyResponseID).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count triage notes: %w", err)
	}
	return int(count), nil
}

// modelToAPI converts a database TriageNote model to an API TriageNote
func (s *GormTriageNoteStore) modelToAPI(model *models.TriageNote) *TriageNote {
	note := &TriageNote{
		Id:         &model.ID,
		Name:       model.Name,
		Content:    string(model.Content),
		CreatedAt:  &model.CreatedAt,
		ModifiedAt: &model.ModifiedAt,
	}

	if model.CreatedBy != nil && model.CreatedBy.InternalUUID != "" {
		note.CreatedBy = userModelToAPIUser(model.CreatedBy)
	}
	if model.ModifiedBy != nil && model.ModifiedBy.InternalUUID != "" {
		note.ModifiedBy = userModelToAPIUser(model.ModifiedBy)
	}

	return note
}

// userModelToAPIUser converts a database User model to an API User
func userModelToAPIUser(model *models.User) *User {
	email := types.Email(model.Email)
	return &User{
		PrincipalType: UserPrincipalType(AuthorizationPrincipalTypeUser),
		Provider:      model.Provider,
		ProviderId:    model.Email,
		DisplayName:   model.Name,
		Email:         email,
	}
}
