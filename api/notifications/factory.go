package notifications

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// DatabaseType represents the type of database
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: enumerate supported database backends for notification routing (pure)
type DatabaseType string

const (
	// DatabaseTypePostgres represents PostgreSQL database
	DatabaseTypePostgres DatabaseType = "postgres"
	// DatabaseTypeOracle represents Oracle database
	DatabaseTypeOracle DatabaseType = "oracle"
)

// NewNotificationService creates a notification service appropriate for the database type
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: build a notification service appropriate for the given database type
func NewNotificationService(dbType DatabaseType, postgresConnStr string, db *sql.DB, gormDB *gorm.DB) (NotificationService, error) {
	logger := slogging.Get()
	logger.Info("Creating notification service for database type: %s", dbType)

	switch dbType {
	case DatabaseTypePostgres:
		if db == nil && postgresConnStr == "" {
			return nil, fmt.Errorf("postgres connection required for PostgreSQL notifications")
		}
		return NewPostgresNotifier(postgresConnStr, db)

	case DatabaseTypeOracle:
		if gormDB == nil {
			return nil, fmt.Errorf("GORM database required for Oracle notifications")
		}
		// Use 1-second polling interval for Oracle
		return NewPollingNotifier(gormDB, time.Second)

	default:
		// Default to polling for unknown database types
		logger.Warn("Unknown database type %s, falling back to polling notifications", dbType)
		if gormDB == nil {
			return nil, fmt.Errorf("GORM database required for polling notifications")
		}
		return NewPollingNotifier(gormDB, time.Second)
	}
}

// NewNotificationServiceFromConfig creates a notification service from configuration
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: build a notification service from a config struct
func NewNotificationServiceFromConfig(cfg Config, db *sql.DB, gormDB *gorm.DB) (NotificationService, error) {
	return NewNotificationService(
		DatabaseType(cfg.DatabaseType),
		cfg.PostgresConnectionString,
		db,
		gormDB,
	)
}
