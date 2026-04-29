package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormUserAPIQuotaStore implements UserAPIQuotaStoreInterface using GORM
type GormUserAPIQuotaStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormUserAPIQuotaStore creates a new GORM-backed user API quota store
func NewGormUserAPIQuotaStore(db *gorm.DB) *GormUserAPIQuotaStore {
	return &GormUserAPIQuotaStore{db: db}
}

// Get retrieves a user API quota by user ID
func (s *GormUserAPIQuotaStore) Get(userID string) (UserAPIQuota, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()

	var model models.UserAPIQuota
	result := s.db.Where("user_internal_uuid = ?", userID).First(&model)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			logger.Debug("User API quota not found for user_id=%s", userID)
			return UserAPIQuota{}, ErrUserAPIQuotaNotFound
		}
		logger.Error("Failed to get user API quota for user_id=%s: %v", userID, result.Error)
		return UserAPIQuota{}, dberrors.Classify(result.Error)
	}

	return s.modelToAPI(model), nil
}

// GetOrDefault retrieves a quota or returns default values
func (s *GormUserAPIQuotaStore) GetOrDefault(userID string) UserAPIQuota {
	quota, err := s.Get(userID)
	if err != nil {
		// Return default quota
		userUUID, _ := uuid.Parse(userID)
		defaultHourly := DefaultMaxRequestsPerHour
		return UserAPIQuota{
			UserId:               userUUID,
			MaxRequestsPerMinute: DefaultMaxRequestsPerMinute,
			MaxRequestsPerHour:   &defaultHourly,
		}
	}
	return quota
}

// List retrieves all user API quotas with pagination
func (s *GormUserAPIQuotaStore) List(offset, limit int) ([]UserAPIQuota, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()

	var modelList []models.UserAPIQuota
	result := s.db.Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to list user API quotas: %v", result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	quotas := make([]UserAPIQuota, len(modelList))
	for i, model := range modelList {
		quotas[i] = s.modelToAPI(model)
	}

	logger.Debug("Listed %d user API quotas (offset=%d, limit=%d)", len(quotas), offset, limit)

	return quotas, nil
}

// Count returns the total number of user API quotas
func (s *GormUserAPIQuotaStore) Count() (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int64
	if err := s.db.Model(&models.UserAPIQuota{}).Count(&count).Error; err != nil {
		return 0, dberrors.Classify(err)
	}
	return int(count), nil
}

// Create creates a new user API quota
func (s *GormUserAPIQuotaStore) Create(item UserAPIQuota) (UserAPIQuota, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	// Update timestamps
	updatedItem := UpdateTimestamps(&item, true)
	item = *updatedItem

	model := s.apiToModel(item)

	err := authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			logger.Error("Failed to create user API quota for user_id=%s: %v", item.UserId, err)
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return UserAPIQuota{}, err
	}

	logger.Info("User API quota created for user_id=%s", item.UserId)

	return s.modelToAPI(model), nil
}

// Update updates an existing user API quota
func (s *GormUserAPIQuotaStore) Update(userID string, item UserAPIQuota) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag

	err := authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.UserAPIQuota{}).
			Where("user_internal_uuid = ?", userID).
			Updates(map[string]any{
				"max_requests_per_minute": item.MaxRequestsPerMinute,
				"max_requests_per_hour":   item.MaxRequestsPerHour,
			})
		if result.Error != nil {
			logger.Error("Failed to update user API quota for user_id=%s: %v", userID, result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			logger.Debug("User API quota not found for user_id=%s", userID)
			return ErrUserAPIQuotaNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("User API quota updated for user_id=%s", userID)

	return nil
}

// Delete deletes a user API quota
func (s *GormUserAPIQuotaStore) Delete(userID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	err := authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Where("user_internal_uuid = ?", userID).Delete(&models.UserAPIQuota{})
		if result.Error != nil {
			logger.Error("Failed to delete user API quota for user_id=%s: %v", userID, result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			logger.Debug("User API quota not found for user_id=%s", userID)
			return ErrUserAPIQuotaNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("User API quota deleted for user_id=%s", userID)

	return nil
}

// Upsert creates or updates a user API quota using GORM's OnConflict clause
// This is cross-database compatible via GORM's dialect abstraction
func (s *GormUserAPIQuotaStore) Upsert(item UserAPIQuota) (UserAPIQuota, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	now := time.Now().UTC()
	item.ModifiedAt = now
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}

	model := s.apiToModel(item)

	err := authdb.WithRetryableGormTransaction(context.Background(), s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_internal_uuid"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"max_requests_per_minute",
				"max_requests_per_hour",
				"modified_at",
			}),
		}).Create(&model).Error; err != nil {
			logger.Error("Failed to upsert user API quota for user_id=%s: %v", item.UserId, err)
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return UserAPIQuota{}, err
	}

	logger.Info("User API quota upserted for user_id=%s", item.UserId)

	return s.modelToAPI(model), nil
}

// modelToAPI converts a GORM model to the API type
func (s *GormUserAPIQuotaStore) modelToAPI(model models.UserAPIQuota) UserAPIQuota {
	userUUID, _ := uuid.Parse(model.UserInternalUUID)
	return UserAPIQuota{
		UserId:               userUUID,
		MaxRequestsPerMinute: model.MaxRequestsPerMinute,
		MaxRequestsPerHour:   model.MaxRequestsPerHour,
		CreatedAt:            model.CreatedAt,
		ModifiedAt:           model.ModifiedAt,
	}
}

// apiToModel converts an API type to a GORM model
func (s *GormUserAPIQuotaStore) apiToModel(api UserAPIQuota) models.UserAPIQuota {
	return models.UserAPIQuota{
		UserInternalUUID:     api.UserId.String(),
		MaxRequestsPerMinute: api.MaxRequestsPerMinute,
		MaxRequestsPerHour:   api.MaxRequestsPerHour,
		CreatedAt:            api.CreatedAt,
		ModifiedAt:           api.ModifiedAt,
	}
}
