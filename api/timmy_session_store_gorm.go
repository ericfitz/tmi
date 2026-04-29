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
	"gorm.io/gorm"
)

// GormTimmySessionStore implements TimmySessionStore using GORM
type GormTimmySessionStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormTimmySessionStore creates a new GORM-backed session store
func NewGormTimmySessionStore(db *gorm.DB) *GormTimmySessionStore {
	return &GormTimmySessionStore{db: db}
}

// Create persists a new session
func (s *GormTimmySessionStore) Create(ctx context.Context, session *models.TimmySession) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Creating Timmy session for user %s, threat model %s", session.UserID, session.ThreatModelID)

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(session).Error; err != nil {
			logger.Error("Failed to create Timmy session: %v", err)
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Debug("Created Timmy session %s", session.ID)
	return nil
}

// Get retrieves a session by ID, excluding soft-deleted sessions
func (s *GormTimmySessionStore) Get(ctx context.Context, id string) (*models.TimmySession, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting Timmy session %s", id)

	var session models.TimmySession
	err := s.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&session).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTimmySessionNotFound
		}
		logger.Error("Failed to get Timmy session %s: %v", id, err)
		return nil, dberrors.Classify(err)
	}

	return &session, nil
}

// ListByUserAndThreatModel returns paginated sessions for a user and threat model
// Returns the sessions, the total count, and any error
func (s *GormTimmySessionStore) ListByUserAndThreatModel(ctx context.Context, userID, threatModelID string, offset, limit int) ([]models.TimmySession, int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing Timmy sessions for user %s, threat model %s (offset=%d, limit=%d)", userID, threatModelID, offset, limit)

	conditions := map[string]any{
		"user_id":         userID,
		"threat_model_id": threatModelID,
	}

	var total int64
	err := s.db.WithContext(ctx).
		Model(&models.TimmySession{}).
		Where(conditions).
		Where("deleted_at IS NULL").
		Count(&total).Error
	if err != nil {
		logger.Error("Failed to count Timmy sessions: %v", err)
		return nil, 0, dberrors.Classify(err)
	}

	var sessions []models.TimmySession
	err = s.db.WithContext(ctx).
		Where(conditions).
		Where("deleted_at IS NULL").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&sessions).Error
	if err != nil {
		logger.Error("Failed to list Timmy sessions: %v", err)
		return nil, 0, dberrors.Classify(err)
	}

	logger.Debug("Found %d Timmy sessions (total=%d)", len(sessions), total)
	return sessions, int(total), nil
}

// SoftDelete marks a session as deleted by setting its deleted_at timestamp
func (s *GormTimmySessionStore) SoftDelete(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Soft deleting Timmy session %s", id)

	now := time.Now().UTC()
	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Model(&models.TimmySession{}).
			Where("id = ? AND deleted_at IS NULL", id).
			Update("deleted_at", now)
		if result.Error != nil {
			logger.Error("Failed to soft delete Timmy session %s: %v", id, result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrTimmySessionNotFound
		}
		logger.Debug("Soft deleted Timmy session %s", id)
		return nil
	})
}

// UpdateSnapshot updates the source_snapshot JSON column for a session.
func (s *GormTimmySessionStore) UpdateSnapshot(ctx context.Context, id string, snapshot models.JSONRaw) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Updating snapshot for Timmy session %s", id)

	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Model(&models.TimmySession{}).
			Where("id = ? AND deleted_at IS NULL", id).
			Update("source_snapshot", snapshot)
		if result.Error != nil {
			logger.Error("Failed to update snapshot for Timmy session %s: %v", id, result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrTimmySessionNotFound
		}
		logger.Debug("Updated snapshot for Timmy session %s", id)
		return nil
	})
}

// CountActiveByThreatModel returns the number of active sessions for a threat model
func (s *GormTimmySessionStore) CountActiveByThreatModel(ctx context.Context, threatModelID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Counting active Timmy sessions for threat model %s", threatModelID)

	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.TimmySession{}).
		Where(map[string]any{
			"threat_model_id": threatModelID,
			"status":          "active",
		}).
		Where("deleted_at IS NULL").
		Count(&count).Error
	if err != nil {
		logger.Error("Failed to count active Timmy sessions: %v", err)
		return 0, dberrors.Classify(err)
	}

	return int(count), nil
}

// GormTimmyMessageStore implements TimmyMessageStore using GORM
type GormTimmyMessageStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormTimmyMessageStore creates a new GORM-backed message store
func NewGormTimmyMessageStore(db *gorm.DB) *GormTimmyMessageStore {
	return &GormTimmyMessageStore{db: db}
}

// Create persists a new message
func (s *GormTimmyMessageStore) Create(ctx context.Context, message *models.TimmyMessage) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Creating Timmy message for session %s (sequence=%d)", message.SessionID, message.Sequence)

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			logger.Error("Failed to create Timmy message: %v", err)
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Debug("Created Timmy message %s", message.ID)
	return nil
}

// ListBySession returns paginated messages for a session ordered by sequence ascending
// Returns the messages, the total count, and any error
func (s *GormTimmyMessageStore) ListBySession(ctx context.Context, sessionID string, offset, limit int) ([]models.TimmyMessage, int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing Timmy messages for session %s (offset=%d, limit=%d)", sessionID, offset, limit)

	var total int64
	err := s.db.WithContext(ctx).
		Model(&models.TimmyMessage{}).
		Where(map[string]any{"session_id": sessionID}).
		Count(&total).Error
	if err != nil {
		logger.Error("Failed to count Timmy messages: %v", err)
		return nil, 0, dberrors.Classify(err)
	}

	var messages []models.TimmyMessage
	err = s.db.WithContext(ctx).
		Where(map[string]any{"session_id": sessionID}).
		Order("sequence ASC").
		Offset(offset).
		Limit(limit).
		Find(&messages).Error
	if err != nil {
		logger.Error("Failed to list Timmy messages: %v", err)
		return nil, 0, dberrors.Classify(err)
	}

	logger.Debug("Found %d Timmy messages (total=%d)", len(messages), total)
	return messages, int(total), nil
}

// GetNextSequence returns the next sequence number for a session (MAX(sequence) + 1, starting at 1)
func (s *GormTimmyMessageStore) GetNextSequence(ctx context.Context, sessionID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting next sequence for Timmy session %s", sessionID)

	var maxSeq *int
	err := s.db.WithContext(ctx).
		Model(&models.TimmyMessage{}).
		Where(map[string]any{"session_id": sessionID}).
		Select("MAX(sequence)").
		Scan(&maxSeq).Error
	if err != nil {
		logger.Error("Failed to get max sequence for session %s: %v", sessionID, err)
		return 0, dberrors.Classify(err)
	}

	if maxSeq == nil {
		return 1, nil
	}
	return *maxSeq + 1, nil
}
