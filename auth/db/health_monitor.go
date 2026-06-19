package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// HealthMonitor provides background connection pool health monitoring
// SEM@b6e227d0511f3e7d38d29e5059408fe7b2cd56eb: background monitor that periodically pings the DB connection pool and refreshes stale connections (mutates shared state)
type HealthMonitor struct {
	db       *sql.DB
	interval time.Duration
	cancel   context.CancelFunc
}

// NewHealthMonitor creates a new health monitor for the given database connection pool
// SEM@b6e227d0511f3e7d38d29e5059408fe7b2cd56eb: build a health monitor for a database connection pool at a given interval (pure)
func NewHealthMonitor(db *sql.DB, interval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		db:       db,
		interval: interval,
	}
}

// Start begins the background health monitoring goroutine.
// It periodically pings the database to keep connections warm and detect issues early.
// If a ping fails, it refreshes the connection pool to clear stale connections.
// SEM@b6e227d0511f3e7d38d29e5059408fe7b2cd56eb: start the background health-check goroutine for the connection pool (mutates shared state)
func (h *HealthMonitor) Start(ctx context.Context) {
	logger := slogging.Get()

	// Create a cancellable context for the monitor
	monitorCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel

	ticker := time.NewTicker(h.interval)

	go func() {
		defer ticker.Stop()
		logger.Info("Connection health monitor started with interval %v", h.interval)

		for {
			select {
			case <-monitorCtx.Done():
				logger.Debug("Connection health monitor stopping")
				return
			case <-ticker.C:
				h.performHealthCheck(monitorCtx)
			}
		}
	}()
}

// Stop stops the health monitor
// SEM@b6e227d0511f3e7d38d29e5059408fe7b2cd56eb: stop the background health-check goroutine (mutates shared state)
func (h *HealthMonitor) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
}

// performHealthCheck runs a single health check cycle
// SEM@b6e227d0511f3e7d38d29e5059408fe7b2cd56eb: ping the DB and refresh the connection pool if the ping fails (mutates shared state)
func (h *HealthMonitor) performHealthCheck(ctx context.Context) {
	logger := slogging.Get()

	// Perform a lightweight health check with timeout
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := h.db.PingContext(pingCtx); err != nil {
		logger.Warn("Connection health check failed: %v", err)

		// Refresh the pool to clear stale connections
		if refreshErr := RefreshConnectionPool(h.db); refreshErr != nil {
			logger.Error("Failed to refresh connection pool after health check failure: %v", refreshErr)
		} else {
			logger.Info("Connection pool refreshed after health check failure")
		}
	} else {
		logger.Debug("Connection health check passed")
	}

	// Log pool stats periodically for observability
	stats := h.db.Stats()
	logger.Debug("Connection pool stats: open=%d, inUse=%d, idle=%d, waitCount=%d, waitDuration=%s",
		stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount, stats.WaitDuration)
}

// StartHealthMonitor is a convenience function that starts a health monitor
// with the given interval and returns the monitor for later stopping.
// Recommended interval for Heroku: 25 seconds (under Heroku's ~30s idle timeout)
// SEM@b6e227d0511f3e7d38d29e5059408fe7b2cd56eb: build and start a DB connection pool health monitor, returning it for later stopping
func StartHealthMonitor(ctx context.Context, db *sql.DB, interval time.Duration) *HealthMonitor {
	monitor := NewHealthMonitor(db, interval)
	monitor.Start(ctx)
	return monitor
}
