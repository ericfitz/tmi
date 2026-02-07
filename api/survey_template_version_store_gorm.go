package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// SurveyTemplateVersionStore defines the interface for survey template version operations
type SurveyTemplateVersionStore interface {
	Create(ctx context.Context, version *models.SurveyTemplateVersion) error
	List(ctx context.Context, templateID uuid.UUID, limit, offset int) ([]SurveyTemplateVersion, int, error)
	Get(ctx context.Context, templateID uuid.UUID, version string) (*SurveyTemplateVersion, error)
}

// GormSurveyTemplateVersionStore implements SurveyTemplateVersionStore using GORM
type GormSurveyTemplateVersionStore struct {
	db *gorm.DB
}

// NewGormSurveyTemplateVersionStore creates a new GORM-backed survey template version store
func NewGormSurveyTemplateVersionStore(db *gorm.DB) *GormSurveyTemplateVersionStore {
	return &GormSurveyTemplateVersionStore{db: db}
}

// Create creates a new survey template version record
func (s *GormSurveyTemplateVersionStore) Create(ctx context.Context, version *models.SurveyTemplateVersion) error {
	logger := slogging.Get()

	result := s.db.WithContext(ctx).Create(version)
	if result.Error != nil {
		logger.Error("Failed to create survey template version: template_id=%s, version=%s, error=%v",
			version.TemplateID, version.Version, result.Error)
		return fmt.Errorf("failed to create survey template version: %w", result.Error)
	}

	logger.Info("Survey template version created: template_id=%s, version=%s", version.TemplateID, version.Version)
	return nil
}

// List retrieves all versions for a template with pagination
func (s *GormSurveyTemplateVersionStore) List(ctx context.Context, templateID uuid.UUID, limit, offset int) ([]SurveyTemplateVersion, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.SurveyTemplateVersion{}).
		Where("template_id = ?", templateID.String())

	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count survey template versions: template_id=%s, error=%v", templateID, err)
		return nil, 0, fmt.Errorf("failed to count versions: %w", err)
	}

	var modelList []models.SurveyTemplateVersion
	result := query.
		Preload("CreatedBy").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list survey template versions: template_id=%s, error=%v", templateID, result.Error)
		return nil, 0, fmt.Errorf("failed to list versions: %w", result.Error)
	}

	items := make([]SurveyTemplateVersion, len(modelList))
	for i, model := range modelList {
		item, err := s.modelToAPI(&model)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to convert version: %w", err)
		}
		items[i] = *item
	}

	logger.Debug("Listed %d survey template versions for template %s (total: %d)", len(items), templateID, total)
	return items, int(total), nil
}

// Get retrieves a specific version of a template by version string
func (s *GormSurveyTemplateVersionStore) Get(ctx context.Context, templateID uuid.UUID, version string) (*SurveyTemplateVersion, error) {
	logger := slogging.Get()

	var model models.SurveyTemplateVersion
	result := s.db.WithContext(ctx).
		Preload("CreatedBy").
		Where("template_id = ? AND version = ?", templateID.String(), version).
		First(&model)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			logger.Debug("Survey template version not found: template_id=%s, version=%s", templateID, version)
			return nil, nil
		}
		logger.Error("Failed to get survey template version: template_id=%s, version=%s, error=%v", templateID, version, result.Error)
		return nil, fmt.Errorf("failed to get version: %w", result.Error)
	}

	return s.modelToAPI(&model)
}

// modelToAPI converts a database model to an API SurveyTemplateVersion
func (s *GormSurveyTemplateVersionStore) modelToAPI(model *models.SurveyTemplateVersion) (*SurveyTemplateVersion, error) {
	id, err := uuid.Parse(model.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid version ID: %w", err)
	}

	templateID, err := uuid.Parse(model.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("invalid template ID: %w", err)
	}

	version := &SurveyTemplateVersion{
		Id:         &id,
		TemplateId: templateID,
		Version:    model.Version,
		CreatedAt:  &model.CreatedAt,
	}

	// Convert survey_json from JSON
	if len(model.SurveyJSON) > 0 {
		var surveyJSON map[string]interface{}
		if err := json.Unmarshal(model.SurveyJSON, &surveyJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal survey_json: %w", err)
		}
		version.SurveyJson = surveyJSON
	}

	// Convert created_by user
	if model.CreatedBy.InternalUUID != "" {
		email := types.Email(model.CreatedBy.Email)
		version.CreatedBy = &User{
			PrincipalType: UserPrincipalType(AuthorizationPrincipalTypeUser),
			Provider:      model.CreatedBy.Provider,
			ProviderId:    model.CreatedBy.Email,
			DisplayName:   model.CreatedBy.Name,
			Email:         email,
		}
	}

	return version, nil
}
