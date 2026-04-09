package api

import (
	"context"

	"github.com/ericfitz/tmi/api/models"
)

// TimmySessionStore defines operations for managing Timmy chat sessions
type TimmySessionStore interface {
	Create(ctx context.Context, session *models.TimmySession) error
	Get(ctx context.Context, id string) (*models.TimmySession, error)
	ListByUserAndThreatModel(ctx context.Context, userID, threatModelID string, offset, limit int) ([]models.TimmySession, int, error)
	SoftDelete(ctx context.Context, id string) error
	CountActiveByThreatModel(ctx context.Context, threatModelID string) (int, error)
	// UpdateSnapshot updates the source_snapshot JSON column for a session.
	UpdateSnapshot(ctx context.Context, id string, snapshot models.JSONRaw) error
}

// TimmyMessageStore defines operations for managing Timmy chat messages
type TimmyMessageStore interface {
	Create(ctx context.Context, message *models.TimmyMessage) error
	ListBySession(ctx context.Context, sessionID string, offset, limit int) ([]models.TimmyMessage, int, error)
	GetNextSequence(ctx context.Context, sessionID string) (int, error)
}

// GlobalTimmySessionStore is the global session store instance
var GlobalTimmySessionStore TimmySessionStore

// GlobalTimmyMessageStore is the global message store instance
var GlobalTimmyMessageStore TimmyMessageStore
