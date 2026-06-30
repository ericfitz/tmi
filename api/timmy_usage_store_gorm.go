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
// SEM@e5e141caabe74e3ce853b6d7b45827bb1864fb32: GORM-backed store for Timmy AI usage records with a reader-writer mutex
type GormTimmyUsageStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormTimmyUsageStore creates a new GORM-backed usage store
// SEM@e5e141caabe74e3ce853b6d7b45827bb1864fb32: build a GormTimmyUsageStore backed by the given GORM DB (pure)
func NewGormTimmyUsageStore(db *gorm.DB) *GormTimmyUsageStore {
	return &GormTimmyUsageStore{db: db}
}

// Record persists a new usage record
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: persist a new Timmy usage record in a retryable transaction (reads DB)
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
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: fetch all Timmy usage records for a user within a time range (reads DB)
func (s *GormTimmyUsageStore) GetByUser(ctx context.Context, userID string, start, end time.Time) ([]models.TimmyUsage, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting Timmy usage for user %s (%s to %s)", userID, start, end)

	var records []models.TimmyUsage
	err := s.db.WithContext(ctx).
		Where(ColumnMap(s.db.Name(), map[string]any{"user_id": userID})).
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
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: fetch all Timmy usage records for a threat model within a time range (reads DB)
func (s *GormTimmyUsageStore) GetByThreatModel(ctx context.Context, threatModelID string, start, end time.Time) ([]models.TimmyUsage, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting Timmy usage for threat model %s (%s to %s)", threatModelID, start, end)

	var records []models.TimmyUsage
	err := s.db.WithContext(ctx).
		Where(ColumnMap(s.db.Name(), map[string]any{"threat_model_id": threatModelID})).
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
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: aggregate Timmy token and session counts filtered by user and threat model over a time range (reads DB)
func (s *GormTimmyUsageStore) GetAggregated(ctx context.Context, userID, threatModelID string, start, end time.Time) (*UsageAggregation, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting aggregated Timmy usage (user=%s, tm=%s)", userID, threatModelID)

	// SEM@e5e141caabe74e3ce853b6d7b45827bb1864fb32: local struct holding raw DB aggregate sums before mapping to UsageAggregation (pure)
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
		query = query.Where(ColumnMap(query.Name(), map[string]any{"user_id": userID}))
	}
	if threatModelID != "" {
		query = query.Where(ColumnMap(query.Name(), map[string]any{"threat_model_id": threatModelID}))
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
