package api

import (
	"context"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GormTimmyUsageStore implements TimmyUsageStore using GORM
type GormTimmyUsageStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormTimmyUsageStore creates a new GORM-backed usage store
func NewGormTimmyUsageStore(db *gorm.DB) *GormTimmyUsageStore {
	return &GormTimmyUsageStore{db: db}
}

// Record persists a new usage record
func (s *GormTimmyUsageStore) Record(ctx context.Context, usage *models.TimmyUsage) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Recording Timmy usage for user %s, session %s", usage.UserID, usage.SessionID)

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(usage).Error; err != nil {
			logger.Error("Failed to record Timmy usage: %v", err)
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Debug("Recorded Timmy usage %s", usage.ID)
	return nil
}

// GetByUser returns all usage records for a user within the given time range
func (s *GormTimmyUsageStore) GetByUser(ctx context.Context, userID string, start, end time.Time) ([]models.TimmyUsage, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting Timmy usage for user %s (%s to %s)", userID, start, end)

	var records []models.TimmyUsage
	err := s.db.WithContext(ctx).
		Where(map[string]any{"user_id": userID}).
		Where("period_start >= ? AND period_end <= ?", start, end).
		Order("period_start ASC").
		Find(&records).Error
	if err != nil {
		logger.Error("Failed to get Timmy usage by user: %v", err)
		return nil, dberrors.Classify(err)
	}

	logger.Debug("Found %d usage records for user %s", len(records), userID)
	return records, nil
}

// GetByThreatModel returns all usage records for a threat model within the given time range
func (s *GormTimmyUsageStore) GetByThreatModel(ctx context.Context, threatModelID string, start, end time.Time) ([]models.TimmyUsage, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting Timmy usage for threat model %s (%s to %s)", threatModelID, start, end)

	var records []models.TimmyUsage
	err := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID}).
		Where("period_start >= ? AND period_end <= ?", start, end).
		Order("period_start ASC").
		Find(&records).Error
	if err != nil {
		logger.Error("Failed to get Timmy usage by threat model: %v", err)
		return nil, dberrors.Classify(err)
	}

	logger.Debug("Found %d usage records for threat model %s", len(records), threatModelID)
	return records, nil
}

// GetAggregated returns summed usage metrics with optional user and threat model filters
func (s *GormTimmyUsageStore) GetAggregated(ctx context.Context, userID, threatModelID string, start, end time.Time) (*UsageAggregation, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting aggregated Timmy usage (user=%s, tm=%s)", userID, threatModelID)

	type aggregateResult struct {
		TotalMessages         int
		TotalPromptTokens     int
		TotalCompletionTokens int
		TotalEmbeddingTokens  int
		SessionCount          int
	}

	query := s.db.WithContext(ctx).
		Model(&models.TimmyUsage{}).
		Select(`
			COALESCE(SUM(message_count), 0) AS total_messages,
			COALESCE(SUM(prompt_tokens), 0) AS total_prompt_tokens,
			COALESCE(SUM(completion_tokens), 0) AS total_completion_tokens,
			COALESCE(SUM(embedding_tokens), 0) AS total_embedding_tokens,
			COUNT(DISTINCT session_id) AS session_count
		`).
		Where("period_start >= ? AND period_end <= ?", start, end)

	if userID != "" {
		query = query.Where(map[string]any{"user_id": userID})
	}
	if threatModelID != "" {
		query = query.Where(map[string]any{"threat_model_id": threatModelID})
	}

	var result aggregateResult
	if err := query.Scan(&result).Error; err != nil {
		logger.Error("Failed to get aggregated Timmy usage: %v", err)
		return nil, dberrors.Classify(err)
	}

	return &UsageAggregation{
		TotalMessages:         result.TotalMessages,
		TotalPromptTokens:     result.TotalPromptTokens,
		TotalCompletionTokens: result.TotalCompletionTokens,
		TotalEmbeddingTokens:  result.TotalEmbeddingTokens,
		SessionCount:          result.SessionCount,
	}, nil
}
