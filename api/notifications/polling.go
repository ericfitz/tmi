package notifications

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NotificationQueueEntry represents an entry in the notification polling table
// SEM@db6c3b75a42a48dd122e5984e9efdf0e6e15ca9d: DB row representing a pending or processed notification in the polling queue
type NotificationQueueEntry struct {
	ID        models.DBVarchar `gorm:"column:id;primaryKey;not null;size:36"`
	Channel   models.DBVarchar `gorm:"column:channel;size:255;not null;index"`
	Payload   models.DBText    `gorm:"column:payload"`
	CreatedAt time.Time        `gorm:"column:created_at;not null;autoCreateTime"`
	Processed bool             `gorm:"column:processed;default:false;not null;index"`
}

// TableName specifies the table name for NotificationQueueEntry
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: return the table name for the notification queue (pure)
func (NotificationQueueEntry) TableName() string {
	return "notification_queue"
}

// PollingNotifier implements NotificationService using database polling
// This is used for Oracle ADB and other databases that don't support LISTEN/NOTIFY
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: DB-polling notification service for databases lacking LISTEN/NOTIFY support (mutates shared state)
type PollingNotifier struct {
	db            *gorm.DB
	pollInterval  time.Duration
	tableName     string
	mu            sync.RWMutex
	channels      map[string][]chan Notification
	stopChan      chan struct{}
	running       bool
	logger        *slogging.Logger
	lastProcessed time.Time
}

// NewPollingNotifier creates a new polling-based notification service
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: build and start a polling notification service against the given DB with the given interval (reads DB)
func NewPollingNotifier(db *gorm.DB, pollInterval time.Duration) (*PollingNotifier, error) {
	logger := slogging.Get()
	logger.Debug("Initializing polling notification service (interval: %v)", pollInterval)

	notifier := &PollingNotifier{
		db:            db,
		pollInterval:  pollInterval,
		tableName:     "notification_queue",
		channels:      make(map[string][]chan Notification),
		stopChan:      make(chan struct{}),
		logger:        logger,
		lastProcessed: time.Now().UTC(),
	}

	// Ensure the notification queue table exists
	if err := notifier.ensureTable(); err != nil {
		return nil, fmt.Errorf("failed to ensure notification table: %w", err)
	}

	// Start the polling goroutine
	go notifier.pollLoop()

	notifier.running = true
	logger.Info("Polling notification service initialized")

	return notifier, nil
}

// ensureTable creates the notification queue table if it doesn't exist
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: auto-migrate the notification queue table into the DB if absent (reads DB)
func (p *PollingNotifier) ensureTable() error {
	return p.db.AutoMigrate(&NotificationQueueEntry{})
}

// pollLoop continuously polls for new notifications
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: background loop that periodically dispatches new-notification processing until stopped (mutates shared state)
func (p *PollingNotifier) pollLoop() {
	p.logger.Debug("Starting notification polling loop")
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			p.logger.Info("Notification polling loop stopped")
			return
		case <-ticker.C:
			p.processNewNotifications()
		}
	}
}

// processNewNotifications fetches and processes unprocessed notifications
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: fetch unprocessed notifications for subscribed channels and dispatch them to subscribers (reads DB)
func (p *PollingNotifier) processNewNotifications() {
	p.mu.RLock()
	subscribedChannels := make([]string, 0, len(p.channels))
	for channel := range p.channels {
		subscribedChannels = append(subscribedChannels, channel)
	}
	p.mu.RUnlock()

	if len(subscribedChannels) == 0 {
		return
	}

	// Query for unprocessed notifications in subscribed channels
	// Use clause.OrderByColumn for cross-database compatibility (Oracle requires uppercase column names)
	var entries []NotificationQueueEntry
	result := p.db.Where("channel IN ? AND processed = ? AND created_at > ?",
		subscribedChannels, false, p.lastProcessed).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{api.OrderByCol(p.db.Name(), "created_at", false)}}).
		Limit(100).
		Find(&entries)

	if result.Error != nil {
		p.logger.Error("Failed to query notifications: %v", result.Error)
		return
	}

	if len(entries) == 0 {
		return
	}

	p.logger.Debug("Processing %d new notifications", len(entries))

	// Process each notification
	var processedIDs []string
	for _, entry := range entries {
		p.handleNotification(entry)
		processedIDs = append(processedIDs, string(entry.ID))
		p.lastProcessed = entry.CreatedAt
	}

	// Mark notifications as processed
	if len(processedIDs) > 0 {
		if err := p.db.Model(&NotificationQueueEntry{}).
			Where("id IN ?", processedIDs).
			Update("processed", true).Error; err != nil {
			p.logger.Error("Failed to mark notifications as processed: %v", err)
		}
	}

	// Clean up old processed notifications (older than 1 hour)
	go p.cleanupOldNotifications()
}

// handleNotification distributes a notification to subscribers
// SEM@23998f331524274d028e5ec84e6d6b7d29d4e332: dispatch a single notification entry to all registered channel subscribers (mutates shared state)
func (p *PollingNotifier) handleNotification(entry NotificationQueueEntry) {
	p.mu.RLock()
	subscribers, exists := p.channels[string(entry.Channel)]
	p.mu.RUnlock()

	if !exists || len(subscribers) == 0 {
		p.logger.Debug("No subscribers for channel %s", string(entry.Channel))
		return
	}

	notification := Notification{
		Channel:   string(entry.Channel),
		Payload:   entry.Payload.String(),
		Timestamp: entry.CreatedAt,
	}

	dispatchToSubscribers(subscribers, notification, string(entry.Channel), p.logger)
}

// cleanupOldNotifications removes processed notifications older than 1 hour
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: delete processed notification rows older than one hour from the queue (reads DB)
func (p *PollingNotifier) cleanupOldNotifications() {
	cutoff := time.Now().UTC().Add(-1 * time.Hour)
	result := p.db.Where("processed = ? AND created_at < ?", true, cutoff).
		Delete(&NotificationQueueEntry{})
	if result.Error != nil {
		p.logger.Error("Failed to cleanup old notifications: %v", result.Error)
	} else if result.RowsAffected > 0 {
		p.logger.Debug("Cleaned up %d old notifications", result.RowsAffected)
	}
}

// Subscribe implements NotificationService.Subscribe
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: register a channel subscriber and return a notification channel; unsubscribe on context cancellation (mutates shared state)
func (p *PollingNotifier) Subscribe(ctx context.Context, channel string) (<-chan Notification, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create a new notification channel for this subscriber
	notifyChan := make(chan Notification, 100)

	// Add subscriber
	p.channels[channel] = append(p.channels[channel], notifyChan)
	p.logger.Info("Subscribed to polling notification channel: %s", channel)

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		p.unsubscribe(channel, notifyChan)
	}()

	return notifyChan, nil
}

// unsubscribe removes a subscriber from a channel
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: remove a subscriber from a notification channel and close its channel (mutates shared state)
func (p *PollingNotifier) unsubscribe(channel string, notifyChan chan Notification) {
	p.mu.Lock()
	defer p.mu.Unlock()

	subscribers := p.channels[channel]
	for i, ch := range subscribers {
		if ch == notifyChan {
			// Remove this subscriber
			p.channels[channel] = append(subscribers[:i], subscribers[i+1:]...)
			close(notifyChan)
			break
		}
	}

	// Remove channel entry if no more subscribers
	if len(p.channels[channel]) == 0 {
		delete(p.channels, channel)
		p.logger.Info("Unsubscribed from polling notification channel: %s", channel)
	}
}

// Notify implements NotificationService.Notify
// SEM@23998f331524274d028e5ec84e6d6b7d29d4e332: store a notification payload into the DB queue for polling delivery (reads DB)
func (p *PollingNotifier) Notify(ctx context.Context, channel string, payload string) error {
	entry := NotificationQueueEntry{
		ID:        models.DBVarchar(generateUUID()),
		Channel:   models.DBVarchar(channel),
		Payload:   models.DBText(payload),
		Processed: false,
	}

	if err := p.db.WithContext(ctx).Create(&entry).Error; err != nil {
		p.logger.Error("Failed to insert notification into queue: %v", err)
		return fmt.Errorf("failed to send notification: %w", err)
	}

	p.logger.Debug("Inserted notification into queue on channel %s", channel)
	return nil
}

// Close implements NotificationService.Close
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: stop the polling loop and close all subscriber channels (mutates shared state)
func (p *PollingNotifier) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	p.running = false
	close(p.stopChan)

	// Close all subscriber channels
	for channel, subscribers := range p.channels {
		for _, ch := range subscribers {
			close(ch)
		}
		delete(p.channels, channel)
	}

	p.logger.Info("Polling notification service closed")
	return nil
}

// generateUUID generates a simple UUID for notification entries
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: generate a unique identifier for a notification queue entry (pure)
func generateUUID() string {
	// Use a simple implementation - in production, use a proper UUID library
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
