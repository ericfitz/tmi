package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SurveyStore defines the interface for survey operations
// SEM@0bd9c0e0e0c0649294d164b9dc945b801cfd507c: interface for CRUD and list operations on survey templates (reads/writes DB)
type SurveyStore interface {
	// CRUD operations
	Create(ctx context.Context, survey *Survey, userInternalUUID string) error
	Get(ctx context.Context, id uuid.UUID) (*Survey, error)
	Update(ctx context.Context, survey *Survey) error
	Delete(ctx context.Context, id uuid.UUID) error

	// List operations with pagination and filtering
	List(ctx context.Context, limit, offset int, status *string) ([]SurveyListItem, int, error)

	// List active surveys only (for intake endpoints)
	ListActive(ctx context.Context, limit, offset int) ([]SurveyListItem, int, error)

	// Check if survey has responses (for delete validation)
	HasResponses(ctx context.Context, id uuid.UUID) (bool, error)
}

// GormSurveyStore implements SurveyStore using GORM
// SEM@bd26290d65c881980433c4a4b599847bb68193d1: GORM-backed implementation of SurveyStore (reads/writes DB)
type GormSurveyStore struct {
	db *gorm.DB
}

// NewGormSurveyStore creates a new GORM-backed survey store
// SEM@bd26290d65c881980433c4a4b599847bb68193d1: build a GORM-backed SurveyStore from a database connection (pure)
func NewGormSurveyStore(db *gorm.DB) *GormSurveyStore {
	return &GormSurveyStore{db: db}
}

// Create creates a new survey
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: store a new survey template and return server-assigned timestamps (writes DB)
func (s *GormSurveyStore) Create(ctx context.Context, survey *Survey, userInternalUUID string) error {
	logger := slogging.Get()

	// Generate ID if not provided
	if survey.Id == nil {
		id := uuid.New()
		survey.Id = &id
	}

	// Set default status if not provided
	if survey.Status == nil {
		status := SurveyStatusInactive
		survey.Status = &status
	}

	model, err := s.apiToModel(survey)
	if err != nil {
		logger.Error("Failed to convert survey to model: name=%s, error=%v", survey.Name, err)
		return fmt.Errorf("failed to convert survey: %w", err)
	}

	// Set the creator
	model.CreatedByInternalUUID = models.DBVarchar(userInternalUUID)

	err = authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			logger.Error("Failed to create survey: name=%s, version=%s, error=%v",
				survey.Name, survey.Version, err)
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Update survey with server-generated values
	survey.CreatedAt = &model.CreatedAt
	survey.ModifiedAt = &model.ModifiedAt

	// Save metadata if provided
	if survey.Metadata != nil {
		if err := s.saveMetadata(ctx, survey.Id.String(), *survey.Metadata); err != nil {
			logger.Error("Failed to save metadata for survey: id=%s, error=%v", survey.Id, err)
		}
	}

	logger.Info("Survey created: id=%s, name=%s, version=%s",
		survey.Id, survey.Name, survey.Version)

	return nil
}

// Get retrieves a survey by ID
// SEM@b11b7d1f947994479701d4db877ed4964b3bfaa6: fetch a survey template by ID including its metadata (reads DB)
func (s *GormSurveyStore) Get(ctx context.Context, id uuid.UUID) (*Survey, error) {
	logger := slogging.Get()

	var model models.SurveyTemplate
	result := s.db.WithContext(ctx).
		First(&model, "id = ?", id.String())

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			logger.Debug("Survey not found: id=%s", id)
			return nil, nil
		}
		logger.Error("Failed to get survey: id=%s, error=%v", id, result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	survey, err := s.modelToAPI(&model)
	if err != nil {
		logger.Error("Failed to convert model to API: id=%s, error=%v", id, err)
		return nil, fmt.Errorf("failed to convert survey: %w", err)
	}

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id.String())
	if err != nil {
		logger.Error("Failed to load metadata for survey: id=%s, error=%v", id, err)
	} else if len(metadata) > 0 {
		survey.Metadata = &metadata
	}

	logger.Debug("Retrieved survey: id=%s, name=%s", survey.Id, survey.Name)

	return survey, nil
}

// Update updates an existing survey
// SEM@b11b7d1f947994479701d4db877ed4964b3bfaa6: update an existing survey template's fields and metadata (writes DB)
func (s *GormSurveyStore) Update(ctx context.Context, survey *Survey) error {
	logger := slogging.Get()

	if survey.Id == nil {
		return fmt.Errorf("survey ID is required for update")
	}

	// Fetch existing
	var existing models.SurveyTemplate
	if err := s.db.WithContext(ctx).First(&existing, "id = ?", survey.Id.String()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSurveyNotFound
		}
		return dberrors.Classify(err)
	}

	model, err := s.apiToModel(survey)
	if err != nil {
		logger.Error("Failed to convert survey to model: id=%s, error=%v", survey.Id, err)
		return fmt.Errorf("failed to convert survey: %w", err)
	}

	// Build update map with only fields that were provided
	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	updates := map[string]any{
		"name":        model.Name,
		"description": model.Description,
		"version":     model.Version,
		"status":      model.Status,
	}

	// Only include survey_json and settings if they were provided in the update,
	// to avoid overwriting existing data with empty values during PATCH operations
	if survey.SurveyJson != nil {
		updates["survey_json"] = model.SurveyJSON
	}
	if survey.Settings != nil {
		updates["settings"] = model.Settings
	}

	err = authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Model(&models.SurveyTemplate{}).
			Where("id = ?", survey.Id.String()).
			Updates(updates)
		if result.Error != nil {
			logger.Error("Failed to update survey: id=%s, error=%v", survey.Id, result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			logger.Debug("Survey not found for update: id=%s", survey.Id)
			return ErrSurveyNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Save metadata if provided
	if survey.Metadata != nil {
		if err := s.saveMetadata(ctx, survey.Id.String(), *survey.Metadata); err != nil {
			logger.Error("Failed to save metadata for survey: id=%s, error=%v", survey.Id, err)
		}
	}

	logger.Info("Survey updated: id=%s, name=%s, version=%s",
		survey.Id, survey.Name, survey.Version)

	return nil
}

// Delete removes a survey by ID
// SEM@b11b7d1f947994479701d4db877ed4964b3bfaa6: delete a survey template by ID (writes DB)
func (s *GormSurveyStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Delete(&models.SurveyTemplate{}, "id = ?", id.String())
		if result.Error != nil {
			logger.Error("Failed to delete survey: id=%s, error=%v", id, result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			logger.Debug("Survey not found for deletion: id=%s", id)
			return ErrSurveyNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("Survey deleted: id=%s", id)

	return nil
}

// List retrieves surveys with pagination and optional status filter
// SEM@b11b7d1f947994479701d4db877ed4964b3bfaa6: list survey templates with optional status filter and pagination (reads DB)
func (s *GormSurveyStore) List(ctx context.Context, limit, offset int, status *string) ([]SurveyListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.SurveyTemplate{})

	if status != nil {
		query = query.Where("status = ?", *status)
	}

	// Get total count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count surveys: error=%v", err)
		return nil, 0, dberrors.Classify(err)
	}

	// Get surveys with pagination
	var modelList []models.SurveyTemplate
	result := query.
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list surveys: error=%v", result.Error)
		return nil, 0, dberrors.Classify(result.Error)
	}

	items := make([]SurveyListItem, len(modelList))
	for i, model := range modelList {
		items[i] = s.modelToListItem(&model)
	}

	logger.Debug("Listed %d surveys (total: %d, limit: %d, offset: %d)",
		len(items), total, limit, offset)

	return items, int(total), nil
}

// ListActive retrieves only active surveys (for intake endpoints)
// SEM@bd26290d65c881980433c4a4b599847bb68193d1: list only active survey templates with pagination (reads DB)
func (s *GormSurveyStore) ListActive(ctx context.Context, limit, offset int) ([]SurveyListItem, int, error) {
	status := SurveyStatusActive
	return s.List(ctx, limit, offset, &status)
}

// HasResponses checks if a survey has any associated responses
// SEM@b11b7d1f947994479701d4db877ed4964b3bfaa6: check whether a survey template has any associated responses (reads DB)
func (s *GormSurveyStore) HasResponses(ctx context.Context, id uuid.UUID) (bool, error) {
	logger := slogging.Get()

	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("template_id = ?", id.String()).
		Count(&count)

	if result.Error != nil {
		logger.Error("Failed to count responses for survey: id=%s, error=%v", id, result.Error)
		return false, dberrors.Classify(result.Error)
	}

	return count > 0, nil
}

// apiToModel converts an API Survey to a database model
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: convert an API Survey to its database model (pure)
func (s *GormSurveyStore) apiToModel(survey *Survey) (*models.SurveyTemplate, error) {
	model := &models.SurveyTemplate{
		Name:    models.DBVarchar(survey.Name),
		Version: models.DBVarchar(survey.Version),
	}

	if survey.Id != nil {
		model.ID = models.DBVarchar(survey.Id.String())
	}

	if survey.Description != nil {
		model.Description = models.NewNullableDBText(survey.Description)
	}

	if survey.Status != nil {
		model.Status = models.DBVarchar(*survey.Status)
	} else {
		model.Status = models.DBVarchar(SurveyStatusInactive)
	}

	// Convert survey_json to JSON
	if survey.SurveyJson != nil {
		surveyJSON, err := json.Marshal(survey.SurveyJson)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal survey_json: %w", err)
		}
		model.SurveyJSON = surveyJSON
	}

	// Convert settings to JSON
	if survey.Settings != nil {
		settingsJSON, err := json.Marshal(survey.Settings)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal settings: %w", err)
		}
		model.Settings = settingsJSON
	}

	return model, nil
}

// modelToAPI converts a database model to an API Survey
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: convert a database survey template model to the API Survey type (reads DB)
func (s *GormSurveyStore) modelToAPI(model *models.SurveyTemplate) (*Survey, error) {
	id, err := uuid.Parse(string(model.ID))
	if err != nil {
		return nil, fmt.Errorf("invalid survey ID: %w", err)
	}

	survey := &Survey{
		Id:          &id,
		Name:        string(model.Name),
		Description: model.Description.Ptr(),
		Version:     string(model.Version),
		CreatedAt:   &model.CreatedAt,
		ModifiedAt:  &model.ModifiedAt,
	}

	// Convert status
	status := string(model.Status)
	survey.Status = &status

	// Convert survey_json from JSON
	if len(model.SurveyJSON) > 0 {
		var surveyJSON map[string]any
		if err := json.Unmarshal(model.SurveyJSON, &surveyJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal survey_json: %w", err)
		}
		survey.SurveyJson = surveyJSON
	}

	// Convert settings from JSON
	if len(model.Settings) > 0 {
		var settings SurveySettings
		if err := json.Unmarshal(model.Settings, &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
		}
		survey.Settings = &settings
	}

	// Look up created_by user (no FK relationship, manual lookup)
	if model.CreatedByInternalUUID != "" {
		var user models.User
		if s.db.Where("internal_uuid = ?", model.CreatedByInternalUUID).First(&user).Error == nil {
			survey.CreatedBy = userModelToAPI(&user)
		}
	}

	return survey, nil
}

// modelToListItem converts a database model to an API SurveyListItem
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: convert a database survey template model to an API SurveyListItem (reads DB)
func (s *GormSurveyStore) modelToListItem(model *models.SurveyTemplate) SurveyListItem {
	id, _ := uuid.Parse(string(model.ID))

	item := SurveyListItem{
		Id:          &id,
		Name:        string(model.Name),
		Description: model.Description.Ptr(),
		Version:     string(model.Version),
		CreatedAt:   model.CreatedAt,
		ModifiedAt:  &model.ModifiedAt,
	}

	item.Status = string(model.Status)

	// Look up created_by user (no FK relationship, manual lookup)
	if model.CreatedByInternalUUID != "" {
		var user models.User
		if s.db.Where("internal_uuid = ?", model.CreatedByInternalUUID).First(&user).Error == nil {
			item.CreatedBy = userModelToAPI(&user)
		}
	}

	return item
}

// userModelToAPI converts a database User model to an API User

// loadMetadata loads metadata for a survey
// SEM@22b222cb8680df2700e22f0e8538874669789920: fetch metadata entries for a survey from the database (reads DB)
func (s *GormSurveyStore) loadMetadata(ctx context.Context, surveyID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db.WithContext(ctx), "survey", surveyID)
}

// saveMetadata saves metadata for a survey
// SEM@22b222cb8680df2700e22f0e8538874669789920: store metadata entries for a survey to the database (writes DB)
func (s *GormSurveyStore) saveMetadata(ctx context.Context, surveyID string, metadata []Metadata) error {
	return saveEntityMetadata(s.db.WithContext(ctx), "survey", surveyID, metadata)
}
