// Package notifications provides database notification services that work across
// different database backends (PostgreSQL and Oracle).
package notifications

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Notification represents a database notification event
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: database notification event carrying a channel name, payload, and timestamp
type Notification struct {
	// Channel is the notification channel name
	Channel string

	// Payload contains the notification data (typically JSON)
	Payload string

	// Timestamp when the notification was received or created
	Timestamp time.Time
}

// NotificationService defines the interface for database notifications
// This abstraction allows the application to work with both PostgreSQL's
// LISTEN/NOTIFY mechanism and Oracle's polling-based approach.
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: interface for subscribing to and sending database notifications across DB backends
type NotificationService interface {
	// Subscribe starts listening for notifications on the specified channel.
	// It returns a channel that will receive notifications.
	// The caller should handle errors returned in the notifications channel's close.
	Subscribe(ctx context.Context, channel string) (<-chan Notification, error)

	// Notify sends a notification on the specified channel.
	// For PostgreSQL, this uses pg_notify.
	// For Oracle, this inserts into a polling table.
	Notify(ctx context.Context, channel string, payload string) error

	// Close cleans up resources used by the notification service
	Close() error
}

// dispatchToSubscribers performs a non-blocking fan-out of a notification to
// every subscriber channel. A subscriber whose buffer is full has the
// notification dropped (with a warning) rather than blocking the dispatcher.
// The channel name is used only for log context. Each notifier builds the
// Notification from its own input type and then calls this shared helper.
// SEM@23998f331524274d028e5ec84e6d6b7d29d4e332: non-blocking fan-out of a notification to all subscriber channels (mutates shared state)
func dispatchToSubscribers(subscribers []chan Notification, notification Notification, channel string, logger *slogging.Logger) {
	for _, ch := range subscribers {
		select {
		case ch <- notification:
			logger.Debug("Sent notification to subscriber on channel %s", channel)
		default:
			logger.Warn("Subscriber channel full, dropping notification on %s", channel)
		}
	}
}

// Config holds configuration for the notification service
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: configuration for the notification service including DB type and polling settings
type Config struct {
	// DatabaseType is "postgres" or "oracle"
	DatabaseType string

	// PostgresConnectionString for PostgreSQL LISTEN/NOTIFY
	PostgresConnectionString string

	// For Oracle polling approach
	PollingInterval time.Duration

	// Table name for Oracle polling notifications
	PollingTableName string
}

// DefaultConfig returns a default configuration
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: build a default notification service config for the given database type (pure)
func DefaultConfig(databaseType string) Config {
	return Config{
		DatabaseType:     databaseType,
		PollingInterval:  time.Second,
		PollingTableName: "notification_queue",
	}
}
