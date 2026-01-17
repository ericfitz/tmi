package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
)

// HealthChecker performs health checks on system components
type HealthChecker struct {
	timeout time.Duration
}

// NewHealthChecker creates a new health checker with the specified timeout
func NewHealthChecker(timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		timeout: timeout,
	}
}

// ComponentHealthResult holds health check results for a single component
type ComponentHealthResult struct {
	Status    ComponentHealthStatus
	LatencyMs int64
	Message   string
}

// SystemHealthResult holds health check results for all components
type SystemHealthResult struct {
	Database ComponentHealthResult
	Redis    ComponentHealthResult
	Overall  ApiInfoStatusCode
}

// CheckHealth performs health checks on all system components
func (h *HealthChecker) CheckHealth(ctx context.Context) SystemHealthResult {
	logger := slogging.Get()
	result := SystemHealthResult{
		Database: ComponentHealthResult{Status: ComponentHealthStatusUnknown, Message: "Not checked"},
		Redis:    ComponentHealthResult{Status: ComponentHealthStatusUnknown, Message: "Not checked"},
		Overall:  OK,
	}

	// Get global database manager
	manager := db.GetGlobalManager()
	if manager == nil {
		logger.Warn("Database manager not available for health check")
		result.Database = ComponentHealthResult{
			Status:  ComponentHealthStatusUnknown,
			Message: "Database manager not initialized",
		}
		result.Redis = ComponentHealthResult{
			Status:  ComponentHealthStatusUnknown,
			Message: "Database manager not initialized",
		}
		result.Overall = DEGRADED
		return result
	}

	// Create timeout context for health checks
	checkCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	// Check database health
	result.Database = h.checkDatabase(checkCtx, manager)

	// Check Redis health
	result.Redis = h.checkRedis(checkCtx, manager)

	// Determine overall status
	if result.Database.Status != ComponentHealthStatusHealthy || result.Redis.Status != ComponentHealthStatusHealthy {
		result.Overall = DEGRADED
	}

	return result
}

// checkDatabase performs a health check on the database
func (h *HealthChecker) checkDatabase(ctx context.Context, manager *db.Manager) ComponentHealthResult {
	logger := slogging.Get()

	gormDB := manager.Gorm()
	if gormDB == nil {
		return ComponentHealthResult{
			Status:  ComponentHealthStatusUnknown,
			Message: "Database connection not configured",
		}
	}

	start := time.Now()
	err := gormDB.Ping(ctx)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		logger.Warn("Database health check failed: %v", err)
		return ComponentHealthResult{
			Status:    ComponentHealthStatusUnhealthy,
			LatencyMs: latency,
			Message:   "Database ping failed",
		}
	}

	logger.Debug("Database health check passed (latency: %dms)", latency)
	return ComponentHealthResult{
		Status:    ComponentHealthStatusHealthy,
		LatencyMs: latency,
		Message:   "Database is responsive",
	}
}

// checkRedis performs a health check on Redis
func (h *HealthChecker) checkRedis(ctx context.Context, manager *db.Manager) ComponentHealthResult {
	logger := slogging.Get()

	redisDB := manager.Redis()
	if redisDB == nil {
		return ComponentHealthResult{
			Status:  ComponentHealthStatusUnknown,
			Message: "Redis connection not configured",
		}
	}

	start := time.Now()
	err := redisDB.Ping(ctx)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		logger.Warn("Redis health check failed: %v", err)
		return ComponentHealthResult{
			Status:    ComponentHealthStatusUnhealthy,
			LatencyMs: latency,
			Message:   "Redis ping failed",
		}
	}

	logger.Debug("Redis health check passed (latency: %dms)", latency)
	return ComponentHealthResult{
		Status:    ComponentHealthStatusHealthy,
		LatencyMs: latency,
		Message:   "Redis is responsive",
	}
}

// ToAPIComponentHealth converts a ComponentHealthResult to the API ComponentHealth type
func (r ComponentHealthResult) ToAPIComponentHealth() *ComponentHealth {
	return &ComponentHealth{
		Status:    r.Status,
		LatencyMs: &r.LatencyMs,
		Message:   &r.Message,
	}
}
