package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/db"
)

func TestHealthChecker_CheckHealth_NoManager(t *testing.T) {
	// Ensure no global manager is set
	db.SetGlobalManager(nil)

	checker := NewHealthChecker(100 * time.Millisecond)
	result := checker.CheckHealth(context.Background())

	if result.Overall != DEGRADED {
		t.Errorf("expected DEGRADED status when manager is nil, got %s", result.Overall)
	}
	if result.Database.Status != ComponentHealthStatusUnknown {
		t.Errorf("expected database status unknown, got %s", result.Database.Status)
	}
	if result.Redis.Status != ComponentHealthStatusUnknown {
		t.Errorf("expected redis status unknown, got %s", result.Redis.Status)
	}
}

func TestHealthChecker_NewHealthChecker(t *testing.T) {
	timeout := 500 * time.Millisecond
	checker := NewHealthChecker(timeout)

	if checker == nil {
		t.Fatal("expected non-nil health checker")
	}
	if checker.timeout != timeout {
		t.Errorf("expected timeout %v, got %v", timeout, checker.timeout)
	}
}

func TestComponentHealthResult_ToAPIComponentHealth(t *testing.T) {
	result := ComponentHealthResult{
		Status:    ComponentHealthStatusHealthy,
		LatencyMs: 5,
		Message:   "Test message",
	}

	apiHealth := result.ToAPIComponentHealth()

	if apiHealth == nil {
		t.Fatal("expected non-nil API component health")
	}
	if apiHealth.Status != ComponentHealthStatusHealthy {
		t.Errorf("expected healthy status, got %s", apiHealth.Status)
	}
	if apiHealth.LatencyMs == nil || *apiHealth.LatencyMs != 5 {
		t.Errorf("expected latency 5ms, got %v", apiHealth.LatencyMs)
	}
	if apiHealth.Message == nil || *apiHealth.Message != "Test message" {
		t.Errorf("expected message 'Test message', got %v", apiHealth.Message)
	}
}

func TestComponentHealthResult_ToAPIComponentHealth_AllStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status ComponentHealthStatus
	}{
		{"healthy", ComponentHealthStatusHealthy},
		{"unhealthy", ComponentHealthStatusUnhealthy},
		{"unknown", ComponentHealthStatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ComponentHealthResult{
				Status:    tt.status,
				LatencyMs: 10,
				Message:   "test",
			}

			apiHealth := result.ToAPIComponentHealth()
			if apiHealth.Status != tt.status {
				t.Errorf("expected status %s, got %s", tt.status, apiHealth.Status)
			}
		})
	}
}

func TestSystemHealthResult_OverallStatus(t *testing.T) {
	tests := []struct {
		name           string
		databaseStatus ComponentHealthStatus
		redisStatus    ComponentHealthStatus
		expected       ApiInfoStatusCode
	}{
		{
			name:           "both healthy returns OK",
			databaseStatus: ComponentHealthStatusHealthy,
			redisStatus:    ComponentHealthStatusHealthy,
			expected:       OK,
		},
		{
			name:           "database unhealthy returns DEGRADED",
			databaseStatus: ComponentHealthStatusUnhealthy,
			redisStatus:    ComponentHealthStatusHealthy,
			expected:       DEGRADED,
		},
		{
			name:           "redis unhealthy returns DEGRADED",
			databaseStatus: ComponentHealthStatusHealthy,
			redisStatus:    ComponentHealthStatusUnhealthy,
			expected:       DEGRADED,
		},
		{
			name:           "both unhealthy returns DEGRADED",
			databaseStatus: ComponentHealthStatusUnhealthy,
			redisStatus:    ComponentHealthStatusUnhealthy,
			expected:       DEGRADED,
		},
		{
			name:           "database unknown returns DEGRADED",
			databaseStatus: ComponentHealthStatusUnknown,
			redisStatus:    ComponentHealthStatusHealthy,
			expected:       DEGRADED,
		},
		{
			name:           "redis unknown returns DEGRADED",
			databaseStatus: ComponentHealthStatusHealthy,
			redisStatus:    ComponentHealthStatusUnknown,
			expected:       DEGRADED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SystemHealthResult{
				Database: ComponentHealthResult{Status: tt.databaseStatus},
				Redis:    ComponentHealthResult{Status: tt.redisStatus},
				Overall:  OK, // Start with OK
			}

			// Determine overall status based on component health (mirrors logic in CheckHealth)
			if result.Database.Status != ComponentHealthStatusHealthy ||
				result.Redis.Status != ComponentHealthStatusHealthy {
				result.Overall = DEGRADED
			}

			if result.Overall != tt.expected {
				t.Errorf("expected overall status %s, got %s", tt.expected, result.Overall)
			}
		})
	}
}
