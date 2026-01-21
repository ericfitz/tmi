package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormAddonStore implements AddonStore using GORM
type GormAddonStore struct {
	db *gorm.DB
}

// NewGormAddonStore creates a new GORM-backed add-on store
func NewGormAddonStore(db *gorm.DB) *GormAddonStore {
	return &GormAddonStore{db: db}
}

// Create creates a new add-on
func (s *GormAddonStore) Create(ctx context.Context, addon *Addon) error {
	logger := slogging.Get()

	// Generate ID if not provided
	if addon.ID == uuid.Nil {
		addon.ID = uuid.New()
	}

	// Set creation time if not set
	if addon.CreatedAt.IsZero() {
		addon.CreatedAt = time.Now().UTC()
	}

	model := s.apiToModel(*addon)

	result := s.db.WithContext(ctx).Create(&model)
	if result.Error != nil {
		logger.Error("Failed to create add-on: name=%s, webhook_id=%s, error=%v",
			addon.Name, addon.WebhookID, result.Error)
		return fmt.Errorf("failed to create add-on: %w", result.Error)
	}

	// Update addon with values from the database
	addon.CreatedAt = model.CreatedAt

	logger.Info("Add-on created: id=%s, name=%s, webhook_id=%s",
		addon.ID, addon.Name, addon.WebhookID)

	return nil
}

// Get retrieves an add-on by ID
func (s *GormAddonStore) Get(ctx context.Context, id uuid.UUID) (*Addon, error) {
	logger := slogging.Get()

	var model models.Addon
	result := s.db.WithContext(ctx).First(&model, "id = ?", id.String())

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			logger.Debug("Add-on not found: id=%s", id)
			return nil, fmt.Errorf("add-on not found: %s", id)
		}
		logger.Error("Failed to get add-on: id=%s, error=%v", id, result.Error)
		return nil, fmt.Errorf("failed to get add-on: %w", result.Error)
	}

	addon := s.modelToAPI(model)
	logger.Debug("Retrieved add-on: id=%s, name=%s", addon.ID, addon.Name)

	return &addon, nil
}

// List retrieves add-ons with pagination, optionally filtered by threat model
func (s *GormAddonStore) List(ctx context.Context, limit, offset int, threatModelID *uuid.UUID) ([]Addon, int, error) {
	logger := slogging.Get()

	// Build query with optional threat model filter
	query := s.db.WithContext(ctx).Model(&models.Addon{})

	if threatModelID != nil {
		// Include add-ons that are either for this threat model or global (no threat model)
		query = query.Where("threat_model_id = ? OR threat_model_id IS NULL", threatModelID.String())
	}

	// Get total count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count add-ons: error=%v", err)
		return nil, 0, fmt.Errorf("failed to count add-ons: %w", err)
	}

	// Get add-ons with pagination
	var modelList []models.Addon
	result := query.Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list add-ons: error=%v", result.Error)
		return nil, 0, fmt.Errorf("failed to list add-ons: %w", result.Error)
	}

	addons := make([]Addon, len(modelList))
	for i, model := range modelList {
		addons[i] = s.modelToAPI(model)
	}

	logger.Debug("Listed %d add-ons (total: %d, limit: %d, offset: %d)",
		len(addons), total, limit, offset)

	return addons, int(total), nil
}

// Delete removes an add-on by ID
func (s *GormAddonStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	result := s.db.WithContext(ctx).Delete(&models.Addon{}, "id = ?", id.String())

	if result.Error != nil {
		logger.Error("Failed to delete add-on: id=%s, error=%v", id, result.Error)
		return fmt.Errorf("failed to delete add-on: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		logger.Debug("Add-on not found for delete: id=%s", id)
		return fmt.Errorf("add-on not found: %s", id)
	}

	logger.Info("Add-on deleted: id=%s", id)

	return nil
}

// GetByWebhookID retrieves all add-ons associated with a webhook
func (s *GormAddonStore) GetByWebhookID(ctx context.Context, webhookID uuid.UUID) ([]Addon, error) {
	logger := slogging.Get()

	var modelList []models.Addon
	result := s.db.WithContext(ctx).
		Where("webhook_id = ?", webhookID.String()).
		Order("created_at DESC").
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to get add-ons by webhook_id=%s: %v", webhookID, result.Error)
		return nil, fmt.Errorf("failed to get add-ons by webhook: %w", result.Error)
	}

	addons := make([]Addon, len(modelList))
	for i, model := range modelList {
		addons[i] = s.modelToAPI(model)
	}

	logger.Debug("Found %d add-ons for webhook_id=%s", len(addons), webhookID)

	return addons, nil
}

// CountActiveInvocations counts pending/in_progress invocations for an add-on
func (s *GormAddonStore) CountActiveInvocations(ctx context.Context, addonID uuid.UUID) (int, error) {
	logger := slogging.Get()

	// Use the Redis invocation store to count active invocations
	if GlobalAddonInvocationStore == nil {
		logger.Warn("GlobalAddonInvocationStore not initialized, cannot count active invocations")
		return 0, nil // Allow deletion if store not available
	}

	count, err := GlobalAddonInvocationStore.CountActive(ctx, addonID)
	if err != nil {
		logger.Error("Failed to count active invocations for addon_id=%s: %v", addonID, err)
		return 0, fmt.Errorf("failed to count active invocations: %w", err)
	}

	logger.Debug("Counted %d active invocations for addon_id=%s", count, addonID)

	return count, nil
}

// DeleteByWebhookID deletes all add-ons associated with a webhook
func (s *GormAddonStore) DeleteByWebhookID(ctx context.Context, webhookID uuid.UUID) (int, error) {
	logger := slogging.Get()

	result := s.db.WithContext(ctx).Where("webhook_id = ?", webhookID.String()).Delete(&models.Addon{})

	if result.Error != nil {
		logger.Error("Failed to delete add-ons by webhook_id=%s: %v", webhookID, result.Error)
		return 0, fmt.Errorf("failed to delete add-ons: %w", result.Error)
	}

	count := int(result.RowsAffected)
	if count > 0 {
		logger.Info("Deleted %d add-ons for webhook_id=%s", count, webhookID)
	} else {
		logger.Debug("No add-ons found for webhook_id=%s", webhookID)
	}

	return count, nil
}

// modelToAPI converts a GORM model to the API type
func (s *GormAddonStore) modelToAPI(model models.Addon) Addon {
	addon := Addon{
		Name:      model.Name,
		CreatedAt: model.CreatedAt,
	}

	if id, err := uuid.Parse(model.ID); err == nil {
		addon.ID = id
	}

	if webhookID, err := uuid.Parse(model.WebhookID); err == nil {
		addon.WebhookID = webhookID
	}

	if model.Description != nil {
		addon.Description = *model.Description
	}

	if model.Icon != nil {
		addon.Icon = *model.Icon
	}

	if model.ThreatModelID != nil {
		if tmID, err := uuid.Parse(*model.ThreatModelID); err == nil {
			addon.ThreatModelID = &tmID
		}
	}

	// Convert StringArray to []string
	if model.Objects != nil {
		addon.Objects = []string(model.Objects)
	}

	return addon
}

// apiToModel converts an API type to a GORM model
func (s *GormAddonStore) apiToModel(addon Addon) models.Addon {
	model := models.Addon{
		ID:        addon.ID.String(),
		CreatedAt: addon.CreatedAt,
		Name:      addon.Name,
		WebhookID: addon.WebhookID.String(),
	}

	if addon.Description != "" {
		model.Description = &addon.Description
	}

	if addon.Icon != "" {
		model.Icon = &addon.Icon
	}

	if addon.ThreatModelID != nil {
		tmIDStr := addon.ThreatModelID.String()
		model.ThreatModelID = &tmIDStr
	}

	// Convert []string to StringArray
	if addon.Objects != nil {
		model.Objects = models.StringArray(addon.Objects)
	}

	return model
}
