// Package notifications provides database notification services that work across
// different database backends (PostgreSQL and Oracle).
package notifications

import (
	"context"
	"time"
)

// Notification represents a database notification event
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

// Config holds configuration for the notification service
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
func DefaultConfig(databaseType string) Config {
	return Config{
		DatabaseType:     databaseType,
		PollingInterval:  time.Second,
		PollingTableName: "notification_queue",
	}
}
