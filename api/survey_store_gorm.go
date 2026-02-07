package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SurveyStore defines the interface for survey operations
type SurveyStore interface {
	// CRUD operations
	Create(ctx context.Context, survey *Survey, userInternalUUID string) error
	Get(ctx context.Context, id uuid.UUID) (*Survey, error)
	Update(ctx context.Context, survey *Survey) error
	Delete(ctx context.Context, id uuid.UUID) error

	// List operations with pagination and filtering
	List(ctx context.Context, limit, offset int, status *SurveyStatus) ([]SurveyListItem, int, error)

	// List active surveys only (for intake endpoints)
	ListActive(ctx context.Context, limit, offset int) ([]SurveyListItem, int, error)

	// Check if survey has responses (for delete validation)
	HasResponses(ctx context.Context, id uuid.UUID) (bool, error)
}

// GormSurveyStore implements SurveyStore using GORM
type GormSurveyStore struct {
	db *gorm.DB
}

// NewGormSurveyStore creates a new GORM-backed survey store
func NewGormSurveyStore(db *gorm.DB) *GormSurveyStore {
	return &GormSurveyStore{db: db}
}

// Create creates a new survey
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
	model.CreatedByInternalUUID = userInternalUUID

	result := s.db.WithContext(ctx).Create(&model)
	if result.Error != nil {
		logger.Error("Failed to create survey: name=%s, version=%s, error=%v",
			survey.Name, survey.Version, result.Error)
		return fmt.Errorf("failed to create survey: %w", result.Error)
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
func (s *GormSurveyStore) Get(ctx context.Context, id uuid.UUID) (*Survey, error) {
	logger := slogging.Get()

	var model models.SurveyTemplate
	result := s.db.WithContext(ctx).
		Preload("CreatedBy").
		First(&model, "id = ?", id.String())

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			logger.Debug("Survey not found: id=%s", id)
			return nil, nil
		}
		logger.Error("Failed to get survey: id=%s, error=%v", id, result.Error)
		return nil, fmt.Errorf("failed to get survey: %w", result.Error)
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
func (s *GormSurveyStore) Update(ctx context.Context, survey *Survey) error {
	logger := slogging.Get()

	if survey.Id == nil {
		return fmt.Errorf("survey ID is required for update")
	}

	// Fetch existing
	var existing models.SurveyTemplate
	if err := s.db.WithContext(ctx).First(&existing, "id = ?", survey.Id.String()).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("survey not found: %s", survey.Id)
		}
		return fmt.Errorf("failed to get existing survey: %w", err)
	}

	model, err := s.apiToModel(survey)
	if err != nil {
		logger.Error("Failed to convert survey to model: id=%s, error=%v", survey.Id, err)
		return fmt.Errorf("failed to convert survey: %w", err)
	}

	result := s.db.WithContext(ctx).
		Model(&models.SurveyTemplate{}).
		Where("id = ?", survey.Id.String()).
		Updates(map[string]interface{}{
			"name":        model.Name,
			"description": model.Description,
			"version":     model.Version,
			"status":      model.Status,
			"survey_json": model.SurveyJSON,
			"settings":    model.Settings,
			"modified_at": time.Now().UTC(),
		})

	if result.Error != nil {
		logger.Error("Failed to update survey: id=%s, error=%v", survey.Id, result.Error)
		return fmt.Errorf("failed to update survey: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		logger.Debug("Survey not found for update: id=%s", survey.Id)
		return fmt.Errorf("survey not found: %s", survey.Id)
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
func (s *GormSurveyStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	result := s.db.WithContext(ctx).Delete(&models.SurveyTemplate{}, "id = ?", id.String())

	if result.Error != nil {
		logger.Error("Failed to delete survey: id=%s, error=%v", id, result.Error)
		return fmt.Errorf("failed to delete survey: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		logger.Debug("Survey not found for deletion: id=%s", id)
		return fmt.Errorf("survey not found: %s", id)
	}

	logger.Info("Survey deleted: id=%s", id)

	return nil
}

// List retrieves surveys with pagination and optional status filter
func (s *GormSurveyStore) List(ctx context.Context, limit, offset int, status *SurveyStatus) ([]SurveyListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.SurveyTemplate{})

	if status != nil {
		query = query.Where("status = ?", string(*status))
	}

	// Get total count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count surveys: error=%v", err)
		return nil, 0, fmt.Errorf("failed to count surveys: %w", err)
	}

	// Get surveys with pagination
	var modelList []models.SurveyTemplate
	result := query.
		Preload("CreatedBy").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list surveys: error=%v", result.Error)
		return nil, 0, fmt.Errorf("failed to list surveys: %w", result.Error)
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
func (s *GormSurveyStore) ListActive(ctx context.Context, limit, offset int) ([]SurveyListItem, int, error) {
	status := SurveyStatusActive
	return s.List(ctx, limit, offset, &status)
}

// HasResponses checks if a survey has any associated responses
func (s *GormSurveyStore) HasResponses(ctx context.Context, id uuid.UUID) (bool, error) {
	logger := slogging.Get()

	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("template_id = ?", id.String()).
		Count(&count)

	if result.Error != nil {
		logger.Error("Failed to count responses for survey: id=%s, error=%v", id, result.Error)
		return false, fmt.Errorf("failed to count responses: %w", result.Error)
	}

	return count > 0, nil
}

// apiToModel converts an API Survey to a database model
func (s *GormSurveyStore) apiToModel(survey *Survey) (*models.SurveyTemplate, error) {
	model := &models.SurveyTemplate{
		Name:    survey.Name,
		Version: survey.Version,
	}

	if survey.Id != nil {
		model.ID = survey.Id.String()
	}

	if survey.Description != nil {
		model.Description = survey.Description
	}

	if survey.Status != nil {
		model.Status = string(*survey.Status)
	} else {
		model.Status = string(SurveyStatusInactive)
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
func (s *GormSurveyStore) modelToAPI(model *models.SurveyTemplate) (*Survey, error) {
	id, err := uuid.Parse(model.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid survey ID: %w", err)
	}

	survey := &Survey{
		Id:          &id,
		Name:        model.Name,
		Description: model.Description,
		Version:     model.Version,
		CreatedAt:   &model.CreatedAt,
		ModifiedAt:  &model.ModifiedAt,
	}

	// Convert status
	status := SurveyStatus(model.Status)
	survey.Status = &status

	// Convert survey_json from JSON
	if len(model.SurveyJSON) > 0 {
		var surveyJSON map[string]interface{}
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

	// Convert created_by user
	if model.CreatedBy.InternalUUID != "" {
		survey.CreatedBy = s.userModelToAPI(&model.CreatedBy)
	}

	return survey, nil
}

// modelToListItem converts a database model to an API SurveyListItem
func (s *GormSurveyStore) modelToListItem(model *models.SurveyTemplate) SurveyListItem {
	id, _ := uuid.Parse(model.ID)

	item := SurveyListItem{
		Id:          id,
		Name:        model.Name,
		Description: model.Description,
		Version:     model.Version,
		CreatedAt:   model.CreatedAt,
		ModifiedAt:  &model.ModifiedAt,
	}

	status := SurveyStatus(model.Status)
	item.Status = status

	// Convert created_by user
	if model.CreatedBy.InternalUUID != "" {
		item.CreatedBy = s.userModelToAPI(&model.CreatedBy)
	}

	return item
}

// userModelToAPI converts a database User model to an API User
func (s *GormSurveyStore) userModelToAPI(model *models.User) *User {
	email := types.Email(model.Email)
	return &User{
		PrincipalType: UserPrincipalType(AuthorizationPrincipalTypeUser),
		Provider:      model.Provider,
		ProviderId:    model.Email,
		DisplayName:   model.Name,
		Email:         email,
	}
}

// loadMetadata loads metadata for a survey
func (s *GormSurveyStore) loadMetadata(ctx context.Context, surveyID string) ([]Metadata, error) {
	var metadataEntries []models.Metadata
	result := s.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", "survey", surveyID).
		Order("key ASC").
		Find(&metadataEntries)

	if result.Error != nil {
		return nil, result.Error
	}

	metadata := make([]Metadata, 0, len(metadataEntries))
	for _, entry := range metadataEntries {
		metadata = append(metadata, Metadata{
			Key:   entry.Key,
			Value: entry.Value,
		})
	}

	return metadata, nil
}

// saveMetadata saves metadata for a survey
func (s *GormSurveyStore) saveMetadata(ctx context.Context, surveyID string, metadata []Metadata) error {
	if len(metadata) == 0 {
		return nil
	}

	for _, meta := range metadata {
		entry := models.Metadata{
			ID:         uuid.New().String(),
			EntityType: "survey",
			EntityID:   surveyID,
			Key:        meta.Key,
			Value:      meta.Value,
		}

		result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "entity_type"}, {Name: "entity_id"}, {Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "modified_at"}),
		}).Create(&entry)

		if result.Error != nil {
			return result.Error
		}
	}

	return nil
}
