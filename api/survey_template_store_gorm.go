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
)

// SurveyTemplateStore defines the interface for survey template operations
type SurveyTemplateStore interface {
	// CRUD operations
	Create(ctx context.Context, template *SurveyTemplate) error
	Get(ctx context.Context, id uuid.UUID) (*SurveyTemplate, error)
	Update(ctx context.Context, template *SurveyTemplate) error
	Delete(ctx context.Context, id uuid.UUID) error

	// List operations with pagination and filtering
	List(ctx context.Context, limit, offset int, status *SurveyTemplateStatus) ([]SurveyTemplateListItem, int, error)

	// List active templates only (for intake endpoints)
	ListActive(ctx context.Context, limit, offset int) ([]SurveyTemplateListItem, int, error)

	// Check if template has responses (for delete validation)
	HasResponses(ctx context.Context, id uuid.UUID) (bool, error)
}

// GormSurveyTemplateStore implements SurveyTemplateStore using GORM
type GormSurveyTemplateStore struct {
	db *gorm.DB
}

// NewGormSurveyTemplateStore creates a new GORM-backed survey template store
func NewGormSurveyTemplateStore(db *gorm.DB) *GormSurveyTemplateStore {
	return &GormSurveyTemplateStore{db: db}
}

// Create creates a new survey template
func (s *GormSurveyTemplateStore) Create(ctx context.Context, template *SurveyTemplate) error {
	logger := slogging.Get()

	// Generate ID if not provided
	if template.Id == nil {
		id := uuid.New()
		template.Id = &id
	}

	// Set default status if not provided
	if template.Status == nil {
		status := SurveyTemplateStatusInactive
		template.Status = &status
	}

	model, err := s.apiToModel(template)
	if err != nil {
		logger.Error("Failed to convert template to model: name=%s, error=%v", template.Name, err)
		return fmt.Errorf("failed to convert template: %w", err)
	}

	result := s.db.WithContext(ctx).Create(&model)
	if result.Error != nil {
		logger.Error("Failed to create survey template: name=%s, version=%s, error=%v",
			template.Name, template.Version, result.Error)
		return fmt.Errorf("failed to create survey template: %w", result.Error)
	}

	// Update template with server-generated values
	template.CreatedAt = &model.CreatedAt
	template.ModifiedAt = &model.ModifiedAt

	logger.Info("Survey template created: id=%s, name=%s, version=%s",
		template.Id, template.Name, template.Version)

	return nil
}

// Get retrieves a survey template by ID
func (s *GormSurveyTemplateStore) Get(ctx context.Context, id uuid.UUID) (*SurveyTemplate, error) {
	logger := slogging.Get()

	var model models.SurveyTemplate
	result := s.db.WithContext(ctx).
		Preload("CreatedBy").
		First(&model, "id = ?", id.String())

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			logger.Debug("Survey template not found: id=%s", id)
			return nil, nil
		}
		logger.Error("Failed to get survey template: id=%s, error=%v", id, result.Error)
		return nil, fmt.Errorf("failed to get survey template: %w", result.Error)
	}

	template, err := s.modelToAPI(&model)
	if err != nil {
		logger.Error("Failed to convert model to API: id=%s, error=%v", id, err)
		return nil, fmt.Errorf("failed to convert template: %w", err)
	}

	logger.Debug("Retrieved survey template: id=%s, name=%s", template.Id, template.Name)

	return template, nil
}

// Update updates an existing survey template
func (s *GormSurveyTemplateStore) Update(ctx context.Context, template *SurveyTemplate) error {
	logger := slogging.Get()

	if template.Id == nil {
		return fmt.Errorf("template ID is required for update")
	}

	model, err := s.apiToModel(template)
	if err != nil {
		logger.Error("Failed to convert template to model: id=%s, error=%v", template.Id, err)
		return fmt.Errorf("failed to convert template: %w", err)
	}

	result := s.db.WithContext(ctx).
		Model(&models.SurveyTemplate{}).
		Where("id = ?", template.Id.String()).
		Updates(map[string]interface{}{
			"name":        model.Name,
			"description": model.Description,
			"version":     model.Version,
			"status":      model.Status,
			"questions":   model.Questions,
			"settings":    model.Settings,
			"modified_at": time.Now().UTC(),
		})

	if result.Error != nil {
		logger.Error("Failed to update survey template: id=%s, error=%v", template.Id, result.Error)
		return fmt.Errorf("failed to update survey template: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		logger.Debug("Survey template not found for update: id=%s", template.Id)
		return fmt.Errorf("survey template not found: %s", template.Id)
	}

	logger.Info("Survey template updated: id=%s, name=%s, version=%s",
		template.Id, template.Name, template.Version)

	return nil
}

// Delete removes a survey template by ID
func (s *GormSurveyTemplateStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	result := s.db.WithContext(ctx).Delete(&models.SurveyTemplate{}, "id = ?", id.String())

	if result.Error != nil {
		logger.Error("Failed to delete survey template: id=%s, error=%v", id, result.Error)
		return fmt.Errorf("failed to delete survey template: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		logger.Debug("Survey template not found for deletion: id=%s", id)
		return fmt.Errorf("survey template not found: %s", id)
	}

	logger.Info("Survey template deleted: id=%s", id)

	return nil
}

// List retrieves survey templates with pagination and optional status filter
func (s *GormSurveyTemplateStore) List(ctx context.Context, limit, offset int, status *SurveyTemplateStatus) ([]SurveyTemplateListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.SurveyTemplate{})

	if status != nil {
		query = query.Where("status = ?", string(*status))
	}

	// Get total count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count survey templates: error=%v", err)
		return nil, 0, fmt.Errorf("failed to count survey templates: %w", err)
	}

	// Get templates with pagination
	var modelList []models.SurveyTemplate
	result := query.
		Preload("CreatedBy").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list survey templates: error=%v", result.Error)
		return nil, 0, fmt.Errorf("failed to list survey templates: %w", result.Error)
	}

	items := make([]SurveyTemplateListItem, len(modelList))
	for i, model := range modelList {
		items[i] = s.modelToListItem(&model)
	}

	logger.Debug("Listed %d survey templates (total: %d, limit: %d, offset: %d)",
		len(items), total, limit, offset)

	return items, int(total), nil
}

// ListActive retrieves only active survey templates (for intake endpoints)
func (s *GormSurveyTemplateStore) ListActive(ctx context.Context, limit, offset int) ([]SurveyTemplateListItem, int, error) {
	status := SurveyTemplateStatusActive
	return s.List(ctx, limit, offset, &status)
}

// HasResponses checks if a template has any associated responses
func (s *GormSurveyTemplateStore) HasResponses(ctx context.Context, id uuid.UUID) (bool, error) {
	logger := slogging.Get()

	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("template_id = ?", id.String()).
		Count(&count)

	if result.Error != nil {
		logger.Error("Failed to count responses for template: id=%s, error=%v", id, result.Error)
		return false, fmt.Errorf("failed to count responses: %w", result.Error)
	}

	return count > 0, nil
}

// apiToModel converts an API SurveyTemplate to a database model
func (s *GormSurveyTemplateStore) apiToModel(template *SurveyTemplate) (*models.SurveyTemplate, error) {
	model := &models.SurveyTemplate{
		Name:    template.Name,
		Version: template.Version,
	}

	if template.Id != nil {
		model.ID = template.Id.String()
	}

	if template.Description != nil {
		model.Description = template.Description
	}

	if template.Status != nil {
		model.Status = string(*template.Status)
	} else {
		model.Status = string(SurveyTemplateStatusInactive)
	}

	// Convert questions to JSON
	if template.Questions != nil {
		questionsJSON, err := json.Marshal(template.Questions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal questions: %w", err)
		}
		model.Questions = questionsJSON
	}

	// Convert settings to JSON
	if template.Settings != nil {
		settingsJSON, err := json.Marshal(template.Settings)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal settings: %w", err)
		}
		model.Settings = settingsJSON
	}

	return model, nil
}

// modelToAPI converts a database model to an API SurveyTemplate
func (s *GormSurveyTemplateStore) modelToAPI(model *models.SurveyTemplate) (*SurveyTemplate, error) {
	id, err := uuid.Parse(model.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid template ID: %w", err)
	}

	template := &SurveyTemplate{
		Id:          &id,
		Name:        model.Name,
		Description: model.Description,
		Version:     model.Version,
		CreatedAt:   &model.CreatedAt,
		ModifiedAt:  &model.ModifiedAt,
	}

	// Convert status
	status := SurveyTemplateStatus(model.Status)
	template.Status = &status

	// Convert questions from JSON
	if len(model.Questions) > 0 {
		var questions []SurveyQuestion
		if err := json.Unmarshal(model.Questions, &questions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal questions: %w", err)
		}
		template.Questions = questions
	}

	// Convert settings from JSON
	if len(model.Settings) > 0 {
		var settings SurveyTemplateSettings
		if err := json.Unmarshal(model.Settings, &settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
		}
		template.Settings = &settings
	}

	// Convert created_by user
	if model.CreatedBy.InternalUUID != "" {
		template.CreatedBy = s.userModelToAPI(&model.CreatedBy)
	}

	return template, nil
}

// modelToListItem converts a database model to an API SurveyTemplateListItem
func (s *GormSurveyTemplateStore) modelToListItem(model *models.SurveyTemplate) SurveyTemplateListItem {
	id, _ := uuid.Parse(model.ID)

	item := SurveyTemplateListItem{
		Id:          id,
		Name:        model.Name,
		Description: model.Description,
		Version:     model.Version,
		CreatedAt:   model.CreatedAt,
		ModifiedAt:  &model.ModifiedAt,
	}

	status := SurveyTemplateStatus(model.Status)
	item.Status = status

	// Count questions
	if len(model.Questions) > 0 {
		var questions []SurveyQuestion
		if err := json.Unmarshal(model.Questions, &questions); err == nil {
			count := len(questions)
			item.QuestionCount = &count
		}
	}

	// Convert created_by user
	if model.CreatedBy.InternalUUID != "" {
		item.CreatedBy = s.userModelToAPI(&model.CreatedBy)
	}

	return item
}

// userModelToAPI converts a database User model to an API User
func (s *GormSurveyTemplateStore) userModelToAPI(model *models.User) *User {
	email := types.Email(model.Email)
	return &User{
		PrincipalType: UserPrincipalType(AuthorizationPrincipalTypeUser),
		Provider:      model.Provider,
		ProviderId:    model.Email,
		DisplayName:   model.Name,
		Email:         email,
	}
}
