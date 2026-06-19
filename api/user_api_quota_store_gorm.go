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
// SEM@b7b932142ab960e30c578c15382ac17d2ac13d79: GORM-backed persistent store for per-user API rate quotas
type GormUserAPIQuotaStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormUserAPIQuotaStore creates a new GORM-backed user API quota store
// SEM@b7b932142ab960e30c578c15382ac17d2ac13d79: build a GormUserAPIQuotaStore backed by the given database connection (pure)
func NewGormUserAPIQuotaStore(db *gorm.DB) *GormUserAPIQuotaStore {
	return &GormUserAPIQuotaStore{db: db}
}

// Get retrieves a user API quota by user ID
// SEM@f02caa14cf5cd68c437a2bddba77d5f8f0d17f8c: fetch the API quota record for a user by internal UUID (reads DB)
func (s *GormUserAPIQuotaStore) Get(ctx context.Context, userID string) (UserAPIQuota, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()

	var model models.UserAPIQuota
	result := s.db.WithContext(ctx).Where("user_internal_uuid = ?", userID).First(&model)

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
// SEM@f02caa14cf5cd68c437a2bddba77d5f8f0d17f8c: fetch the user's API quota or return platform defaults when none is stored (reads DB)
func (s *GormUserAPIQuotaStore) GetOrDefault(ctx context.Context, userID string) UserAPIQuota {
	quota, err := s.Get(ctx, userID)
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
// SEM@f02caa14cf5cd68c437a2bddba77d5f8f0d17f8c: list all user API quota records with pagination (reads DB)
func (s *GormUserAPIQuotaStore) List(ctx context.Context, offset, limit int) ([]UserAPIQuota, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()

	var modelList []models.UserAPIQuota
	result := s.db.WithContext(ctx).Order("created_at DESC").
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
// SEM@f02caa14cf5cd68c437a2bddba77d5f8f0d17f8c: return the total number of stored user API quota records (reads DB)
func (s *GormUserAPIQuotaStore) Count(ctx context.Context) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.UserAPIQuota{}).Count(&count).Error; err != nil {
		return 0, dberrors.Classify(err)
	}
	return int(count), nil
}

// Create creates a new user API quota
// SEM@f02caa14cf5cd68c437a2bddba77d5f8f0d17f8c: store a new user API quota record in the database (mutates shared state)
func (s *GormUserAPIQuotaStore) Create(ctx context.Context, item UserAPIQuota) (UserAPIQuota, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	// Update timestamps
	updatedItem := UpdateTimestamps(&item, true)
	item = *updatedItem

	model := s.apiToModel(item)

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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
// SEM@f02caa14cf5cd68c437a2bddba77d5f8f0d17f8c: update rate limit fields of an existing user API quota (mutates shared state)
func (s *GormUserAPIQuotaStore) Update(ctx context.Context, userID string, item UserAPIQuota) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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
// SEM@f02caa14cf5cd68c437a2bddba77d5f8f0d17f8c: delete the API quota record for a user by internal UUID (mutates shared state)
func (s *GormUserAPIQuotaStore) Delete(ctx context.Context, userID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
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
// SEM@aa6d284f5df5c13ccb0001366a1f228490aba957: create or update a user API quota using a cross-DB conflict clause (mutates shared state)
func (s *GormUserAPIQuotaStore) Upsert(ctx context.Context, item UserAPIQuota) (UserAPIQuota, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	now := time.Now().UTC()
	item.ModifiedAt = now
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}

	model := s.apiToModel(item)

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Use Col()/ColumnName() so the Oracle GORM driver receives uppercase
		// column identifiers when emitting MERGE INTO.
		dialect := tx.Name()
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{Col(dialect, "user_internal_uuid")},
			DoUpdates: clause.AssignmentColumns([]string{
				ColumnName(dialect, "max_requests_per_minute"),
				ColumnName(dialect, "max_requests_per_hour"),
				ColumnName(dialect, "modified_at"),
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
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: convert a GORM UserAPIQuota model to its API representation (pure)
func (s *GormUserAPIQuotaStore) modelToAPI(model models.UserAPIQuota) UserAPIQuota {
	userUUID, _ := uuid.Parse(string(model.UserInternalUUID))
	return UserAPIQuota{
		UserId:               userUUID,
		MaxRequestsPerMinute: model.MaxRequestsPerMinute,
		MaxRequestsPerHour:   model.MaxRequestsPerHour,
		CreatedAt:            model.CreatedAt,
		ModifiedAt:           model.ModifiedAt,
	}
}

// apiToModel converts an API type to a GORM model
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: convert an API UserAPIQuota to its GORM model representation (pure)
func (s *GormUserAPIQuotaStore) apiToModel(api UserAPIQuota) models.UserAPIQuota {
	return models.UserAPIQuota{
		UserInternalUUID:     models.DBVarchar(api.UserId.String()),
		MaxRequestsPerMinute: api.MaxRequestsPerMinute,
		MaxRequestsPerHour:   api.MaxRequestsPerHour,
		CreatedAt:            api.CreatedAt,
		ModifiedAt:           api.ModifiedAt,
	}
}
