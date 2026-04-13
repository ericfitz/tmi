package api

import (
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GlobalAccessTracker is initialized at server startup and used by ThreatModelMiddleware.
var GlobalAccessTracker *AccessTracker

const defaultDebounceDuration = 1 * time.Minute

// AccessTracker records threat model access times with in-memory debouncing.
// It uses a sync.Map to avoid writing to the database more than once per
// debounce window per threat model.
type AccessTracker struct {
	db               *gorm.DB
	debounceDuration time.Duration
	lastWriteTimes   sync.Map // map[string]time.Time
}

// NewAccessTracker creates an AccessTracker with the default 1-minute debounce.
func NewAccessTracker(db *gorm.DB) *AccessTracker {
	return &AccessTracker{
		db:               db,
		debounceDuration: defaultDebounceDuration,
	}
}

// NewAccessTrackerWithDebounce creates an AccessTracker with a custom debounce duration (for testing).
func NewAccessTrackerWithDebounce(db *gorm.DB, debounce time.Duration) *AccessTracker {
	return &AccessTracker{
		db:               db,
		debounceDuration: debounce,
	}
}

// RecordAccess updates last_accessed_at for a threat model, debouncing writes.
// The DB update runs in a fire-and-forget goroutine to avoid adding latency.
func (at *AccessTracker) RecordAccess(threatModelID string) {
	now := time.Now()

	// Check debounce: skip if we wrote recently
	if lastWrite, ok := at.lastWriteTimes.Load(threatModelID); ok {
		if t, ok := lastWrite.(time.Time); ok && now.Sub(t) < at.debounceDuration {
			return
		}
	}

	// Update debounce map immediately (before goroutine) to prevent duplicates
	at.lastWriteTimes.Store(threatModelID, now)

	go func() {
		logger := slogging.Get()
		result := at.db.Table("threat_models").
			Where("id = ?", threatModelID).
			Update("last_accessed_at", now)
		if result.Error != nil {
			logger.Error("AccessTracker: failed to update last_accessed_at for %s: %v", threatModelID, result.Error)
		}
	}()
}

// Reset clears the debounce map. Used in tests.
func (at *AccessTracker) Reset() {
	at.lastWriteTimes = sync.Map{}
}
