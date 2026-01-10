package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// HealthMonitor provides background connection pool health monitoring
type HealthMonitor struct {
	db       *sql.DB
	interval time.Duration
	cancel   context.CancelFunc
}

// NewHealthMonitor creates a new health monitor for the given database connection pool
func NewHealthMonitor(db *sql.DB, interval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		db:       db,
		interval: interval,
	}
}

// Start begins the background health monitoring goroutine.
// It periodically pings the database to keep connections warm and detect issues early.
// If a ping fails, it refreshes the connection pool to clear stale connections.
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
func (h *HealthMonitor) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
}

// performHealthCheck runs a single health check cycle
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
func StartHealthMonitor(ctx context.Context, db *sql.DB, interval time.Duration) *HealthMonitor {
	monitor := NewHealthMonitor(db, interval)
	monitor.Start(ctx)
	return monitor
}
