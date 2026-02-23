package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// DefaultMaxIdleConns is the default number of idle connections to maintain in the pool
const DefaultMaxIdleConns = 2

// RefreshConnectionPool closes idle connections and validates the pool with fresh connections.
// This should be called before long-running batch operations to ensure all connections are healthy.
// It works by temporarily setting MaxIdleConns to 0 (forcing closure of idle connections),
// then restoring the original value and warming the pool with multiple pings.
func RefreshConnectionPool(db *sql.DB) error {
	logger := slogging.Get()
	logger.Debug("Refreshing connection pool - closing idle connections")

	// Get current stats for logging
	statsBefore := db.Stats()
	logger.Debug("Pool stats before refresh: open=%d, inUse=%d, idle=%d, maxIdleClosed=%d, maxLifetimeClosed=%d",
		statsBefore.OpenConnections, statsBefore.InUse, statsBefore.Idle,
		statsBefore.MaxIdleClosed, statsBefore.MaxLifetimeClosed)

	// SetMaxIdleConns(0) forces immediate closure of all idle connections
	// Then restore the original value to allow new idle connections
	db.SetMaxIdleConns(0)
	db.SetMaxIdleConns(DefaultMaxIdleConns)

	// Validate with multiple pings to warm fresh connections
	// This ensures we have healthy connections ready for use
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := range 3 {
		if err := db.PingContext(ctx); err != nil {
			logger.Error("Pool refresh ping %d/3 failed: %v", i+1, err)
			return err
		}
	}

	statsAfter := db.Stats()
	logger.Debug("Pool stats after refresh: open=%d, inUse=%d, idle=%d, maxIdleClosed=%d, maxLifetimeClosed=%d",
		statsAfter.OpenConnections, statsAfter.InUse, statsAfter.Idle,
		statsAfter.MaxIdleClosed, statsAfter.MaxLifetimeClosed)

	logger.Debug("Connection pool refreshed successfully")
	return nil
}

// EnsureHealthyConnection gets a dedicated connection from the pool and validates it.
// The caller is responsible for closing the returned connection when done.
// This is useful when you need guaranteed connection health for a specific operation.
func EnsureHealthyConnection(ctx context.Context, db *sql.DB) (*sql.Conn, error) {
	logger := slogging.Get()

	// Get a dedicated connection from the pool
	conn, err := db.Conn(ctx)
	if err != nil {
		logger.Error("Failed to get dedicated connection: %v", err)
		return nil, err
	}

	// Validate this specific connection with a ping
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := conn.PingContext(pingCtx); err != nil {
		logger.Error("Dedicated connection ping failed: %v", err)
		if closeErr := conn.Close(); closeErr != nil {
			logger.Error("Failed to close connection after ping failure: %v", closeErr)
		}
		return nil, err
	}

	logger.Debug("Obtained healthy dedicated connection")
	return conn, nil
}
