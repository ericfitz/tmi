package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
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

	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		logger.Error("Failed to create Timmy session: %v", err)
		return fmt.Errorf("failed to create session: %w", err)
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
			return nil, fmt.Errorf("session not found: %w", err)
		}
		logger.Error("Failed to get Timmy session %s: %v", id, err)
		return nil, fmt.Errorf("failed to get session: %w", err)
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
		return nil, 0, fmt.Errorf("failed to count sessions: %w", err)
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
		return nil, 0, fmt.Errorf("failed to list sessions: %w", err)
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
	result := s.db.WithContext(ctx).
		Model(&models.TimmySession{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", now)
	if result.Error != nil {
		logger.Error("Failed to soft delete Timmy session %s: %v", id, result.Error)
		return fmt.Errorf("failed to delete session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	logger.Debug("Soft deleted Timmy session %s", id)
	return nil
}

// UpdateSnapshot updates the source_snapshot JSON column for a session.
func (s *GormTimmySessionStore) UpdateSnapshot(ctx context.Context, id string, snapshot models.JSONRaw) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Updating snapshot for Timmy session %s", id)

	result := s.db.WithContext(ctx).
		Model(&models.TimmySession{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("source_snapshot", snapshot)
	if result.Error != nil {
		logger.Error("Failed to update snapshot for Timmy session %s: %v", id, result.Error)
		return fmt.Errorf("failed to update session snapshot: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	logger.Debug("Updated snapshot for Timmy session %s", id)
	return nil
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
		return 0, fmt.Errorf("failed to count active sessions: %w", err)
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

	if err := s.db.WithContext(ctx).Create(message).Error; err != nil {
		logger.Error("Failed to create Timmy message: %v", err)
		return fmt.Errorf("failed to create message: %w", err)
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
		return nil, 0, fmt.Errorf("failed to count messages: %w", err)
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
		return nil, 0, fmt.Errorf("failed to list messages: %w", err)
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
		return 0, fmt.Errorf("failed to get next sequence: %w", err)
	}

	if maxSeq == nil {
		return 1, nil
	}
	return *maxSeq + 1, nil
}
