package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormAddonInvocationQuotaStore implements AddonInvocationQuotaStore using GORM
type GormAddonInvocationQuotaStore struct {
	db *gorm.DB
}

// NewGormAddonInvocationQuotaStore creates a new GORM-backed quota store
func NewGormAddonInvocationQuotaStore(db *gorm.DB) *GormAddonInvocationQuotaStore {
	return &GormAddonInvocationQuotaStore{db: db}
}

// Get retrieves quota for a user, returns error if not found
func (s *GormAddonInvocationQuotaStore) Get(ctx context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error) {
	logger := slogging.Get()

	var model models.AddonInvocationQuota
	result := s.db.WithContext(ctx).First(&model, "owner_internal_uuid = ?", ownerID.String())

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			logger.Debug("No quota found for owner_id=%s", ownerID)
			return nil, fmt.Errorf("quota not found for owner_id=%s", ownerID)
		}
		logger.Error("Failed to get quota for owner_id=%s: %v", ownerID, result.Error)
		return nil, fmt.Errorf("failed to get quota: %w", result.Error)
	}

	quota := s.modelToAPI(model)
	logger.Debug("Retrieved quota for owner_id=%s: active=%d, hourly=%d",
		ownerID, quota.MaxActiveInvocations, quota.MaxInvocationsPerHour)

	return &quota, nil
}

// GetOrDefault retrieves quota for a user, or returns defaults if not set
func (s *GormAddonInvocationQuotaStore) GetOrDefault(ctx context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error) {
	logger := slogging.Get()

	var model models.AddonInvocationQuota
	result := s.db.WithContext(ctx).First(&model, "owner_internal_uuid = ?", ownerID.String())

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			// Return defaults
			logger.Debug("No quota found for owner_id=%s, using defaults", ownerID)
			return &AddonInvocationQuota{
				OwnerId:               ownerID,
				MaxActiveInvocations:  DefaultMaxActiveInvocations,
				MaxInvocationsPerHour: DefaultMaxInvocationsPerHour,
				CreatedAt:             time.Now(),
				ModifiedAt:            time.Now(),
			}, nil
		}
		logger.Error("Failed to get quota for owner_id=%s: %v", ownerID, result.Error)
		return nil, fmt.Errorf("failed to get quota: %w", result.Error)
	}

	quota := s.modelToAPI(model)
	logger.Debug("Retrieved quota for owner_id=%s: active=%d, hourly=%d",
		ownerID, quota.MaxActiveInvocations, quota.MaxInvocationsPerHour)

	return &quota, nil
}

// List retrieves all addon invocation quotas with pagination
func (s *GormAddonInvocationQuotaStore) List(ctx context.Context, offset, limit int) ([]*AddonInvocationQuota, error) {
	logger := slogging.Get()

	var modelList []models.AddonInvocationQuota
	result := s.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list quotas: %v", result.Error)
		return nil, fmt.Errorf("failed to list quotas: %w", result.Error)
	}

	quotas := make([]*AddonInvocationQuota, len(modelList))
	for i, model := range modelList {
		quota := s.modelToAPI(model)
		quotas[i] = &quota
	}

	logger.Debug("Listed %d addon invocation quotas (offset=%d, limit=%d)", len(quotas), offset, limit)

	return quotas, nil
}

// Set creates or updates quota for a user using GORM's OnConflict clause
func (s *GormAddonInvocationQuotaStore) Set(ctx context.Context, quota *AddonInvocationQuota) error {
	logger := slogging.Get()

	now := time.Now().UTC()
	if quota.CreatedAt.IsZero() {
		quota.CreatedAt = now
	}
	quota.ModifiedAt = now

	model := s.apiToModel(*quota)

	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "owner_internal_uuid"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"max_active_invocations",
			"max_invocations_per_hour",
			"modified_at",
		}),
	}).Create(&model)

	if result.Error != nil {
		logger.Error("Failed to set quota for owner_id=%s: %v", quota.OwnerId, result.Error)
		return fmt.Errorf("failed to set quota: %w", result.Error)
	}

	// Update timestamps from database
	quota.CreatedAt = model.CreatedAt
	quota.ModifiedAt = model.ModifiedAt

	logger.Info("Quota set for owner_id=%s: active=%d, hourly=%d",
		quota.OwnerId, quota.MaxActiveInvocations, quota.MaxInvocationsPerHour)

	return nil
}

// Delete removes quota for a user (reverts to defaults)
func (s *GormAddonInvocationQuotaStore) Delete(ctx context.Context, ownerID uuid.UUID) error {
	logger := slogging.Get()

	result := s.db.WithContext(ctx).Delete(&models.AddonInvocationQuota{}, "owner_internal_uuid = ?", ownerID.String())

	if result.Error != nil {
		logger.Error("Failed to delete quota for owner_id=%s: %v", ownerID, result.Error)
		return fmt.Errorf("failed to delete quota: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		logger.Debug("No quota to delete for owner_id=%s", ownerID)
		return fmt.Errorf("quota not found for owner_id=%s", ownerID)
	}

	logger.Info("Quota deleted for owner_id=%s (reverted to defaults)", ownerID)

	return nil
}

// modelToAPI converts a GORM model to the API type
func (s *GormAddonInvocationQuotaStore) modelToAPI(model models.AddonInvocationQuota) AddonInvocationQuota {
	ownerUUID, _ := uuid.Parse(model.OwnerInternalUUID)
	return AddonInvocationQuota{
		OwnerId:               ownerUUID,
		MaxActiveInvocations:  model.MaxActiveInvocations,
		MaxInvocationsPerHour: model.MaxInvocationsPerHour,
		CreatedAt:             model.CreatedAt,
		ModifiedAt:            model.ModifiedAt,
	}
}

// apiToModel converts an API type to a GORM model
func (s *GormAddonInvocationQuotaStore) apiToModel(quota AddonInvocationQuota) models.AddonInvocationQuota {
	return models.AddonInvocationQuota{
		OwnerInternalUUID:     quota.OwnerId.String(),
		MaxActiveInvocations:  quota.MaxActiveInvocations,
		MaxInvocationsPerHour: quota.MaxInvocationsPerHour,
		CreatedAt:             quota.CreatedAt,
		ModifiedAt:            quota.ModifiedAt,
	}
}
