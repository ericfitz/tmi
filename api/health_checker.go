package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
)

// HealthChecker performs health checks on system components
// SEM@5775fac65ab5239fc263439d133089dda83af787: component that probes system dependencies within a configurable timeout (pure)
type HealthChecker struct {
	timeout time.Duration
}

// NewHealthChecker creates a new health checker with the specified timeout
// SEM@5775fac65ab5239fc263439d133089dda83af787: build a HealthChecker with the given probe timeout (pure)
func NewHealthChecker(timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		timeout: timeout,
	}
}

// ComponentHealthResult holds health check results for a single component
// SEM@5775fac65ab5239fc263439d133089dda83af787: health probe result for a single infrastructure component with status, latency, and message (pure)
type ComponentHealthResult struct {
	Status    ComponentHealthStatus
	LatencyMs int64
	Message   string
}

// SystemHealthResult holds health check results for all components
// SEM@5775fac65ab5239fc263439d133089dda83af787: aggregate health result for all system components with an overall status code (pure)
type SystemHealthResult struct {
	Database ComponentHealthResult
	Redis    ComponentHealthResult
	Overall  ApiInfoStatusCode
}

// CheckHealth performs health checks on all system components
// SEM@034968fa0e0ba8c15e9af9052b475f4d5dd72d50: probe database and Redis health and return an aggregated system health result
func (h *HealthChecker) CheckHealth(ctx context.Context) SystemHealthResult {
	logger := slogging.Get()
	result := SystemHealthResult{
		Database: ComponentHealthResult{Status: ComponentHealthStatusUnknown, Message: "Not checked"},
		Redis:    ComponentHealthResult{Status: ComponentHealthStatusUnknown, Message: "Not checked"},
		Overall:  ApiInfoStatusCodeOk,
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
		result.Overall = ApiInfoStatusCodeDegraded
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
		result.Overall = ApiInfoStatusCodeDegraded
	}

	return result
}

// checkDatabase performs a health check on the database
// SEM@5775fac65ab5239fc263439d133089dda83af787: ping the database and return its latency and health status
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
// SEM@5775fac65ab5239fc263439d133089dda83af787: ping Redis and return its latency and health status
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
// SEM@5775fac65ab5239fc263439d133089dda83af787: convert a ComponentHealthResult to its API DTO (pure)
func (r ComponentHealthResult) ToAPIComponentHealth() *ComponentHealth {
	return &ComponentHealth{
		Status:    r.Status,
		LatencyMs: &r.LatencyMs,
		Message:   &r.Message,
	}
}
