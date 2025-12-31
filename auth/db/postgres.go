package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	_ "github.com/jackc/pgx/v4/stdlib" // pgx driver for database/sql
)

// PostgresConfig holds the configuration for PostgreSQL connection
type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	SSLMode  string
}

// PostgresDB represents a PostgreSQL database connection
type PostgresDB struct {
	db  *sql.DB
	cfg PostgresConfig
}

// NewPostgresDB creates a new PostgreSQL database connection
func NewPostgresDB(cfg PostgresConfig) (*PostgresDB, error) {
	logger := slogging.Get()
	logger.Debug("Initializing PostgreSQL connection to %s:%s/%s", cfg.Host, cfg.Port, cfg.Database)

	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.SSLMode,
	)

	// Open database connection
	logger.Debug("Opening PostgreSQL connection")
	db, err := sql.Open("pgx", connString)
	if err != nil {
		logger.Error("Failed to open PostgreSQL connection: %v", err)
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Set connection pool parameters
	// Use shorter max lifetime (5 min) to proactively recycle connections before they go stale
	// This helps prevent "driver: bad connection" errors in cloud environments where
	// connections may be terminated by load balancers, firewalls, or database restarts
	logger.Debug("Setting PostgreSQL connection pool parameters: maxOpen=10, maxIdle=2, maxLifetime=5m, maxIdleTime=2m")
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Debug("Testing PostgreSQL connection with ping")
	if err := db.PingContext(ctx); err != nil {
		logger.Error("Failed to ping PostgreSQL: %v", err)
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}
	logger.Debug("PostgreSQL connection established successfully")

	return &PostgresDB{
		db:  db,
		cfg: cfg,
	}, nil
}

// Close closes the database connection
func (db *PostgresDB) Close() error {
	logger := slogging.Get()
	logger.Debug("Closing PostgreSQL connection to %s:%s/%s", db.cfg.Host, db.cfg.Port, db.cfg.Database)

	if db.db != nil {
		if err := db.db.Close(); err != nil {
			logger.Error("Error closing PostgreSQL connection: %v", err)
			return fmt.Errorf("error closing database connection: %w", err)
		}
		logger.Debug("PostgreSQL connection closed successfully")
	}
	return nil
}

// GetDB returns the database connection
func (db *PostgresDB) GetDB() *sql.DB {
	return db.db
}

// Ping checks if the database connection is alive
func (db *PostgresDB) Ping(ctx context.Context) error {
	logger := slogging.Get()
	logger.Debug("Pinging PostgreSQL connection to %s:%s/%s", db.cfg.Host, db.cfg.Port, db.cfg.Database)

	err := db.db.PingContext(ctx)
	if err != nil {
		logger.Error("PostgreSQL ping failed: %v", err)
	} else {
		logger.Debug("PostgreSQL ping successful")
	}
	return err
}

// LogStats logs statistics about the database connection pool
func (db *PostgresDB) LogStats() {
	logger := slogging.Get()
	stats := db.db.Stats()
	logger.Debug("PostgreSQL connection pool stats: open=%d, inUse=%d, idle=%d, waitCount=%d, waitDuration=%s, maxIdleClosed=%d, maxLifetimeClosed=%d",
		stats.OpenConnections,
		stats.InUse,
		stats.Idle,
		stats.WaitCount,
		stats.WaitDuration,
		stats.MaxIdleClosed,
		stats.MaxLifetimeClosed,
	)
}
