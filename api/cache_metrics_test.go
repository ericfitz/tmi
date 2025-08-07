package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// MockRedisDB is reused from cache_service_test.go for consistency

// TestCacheMetrics wraps CacheMetrics for testing
type TestCacheMetrics struct {
	*CacheMetrics
}

// newTestCacheMetrics creates a test cache metrics instance with mock Redis
func newTestCacheMetrics() (*TestCacheMetrics, *MockRedisDB) {
	mockRedis := &MockRedisDB{}
	// Create CacheMetrics with direct field assignment to avoid casting issues
	metrics := &CacheMetrics{
		redis:     nil, // We'll use mockRedis through the wrapper
		counters:  make(map[string]int64),
		timings:   make(map[string][]time.Duration),
		startTime: time.Now(),
		enabled:   true,
	}
	return &TestCacheMetrics{CacheMetrics: metrics}, mockRedis
}

// TestNewCacheMetrics tests cache metrics creation
func TestNewCacheMetrics(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	assert.NotNil(t, metrics)
	assert.NotNil(t, metrics.counters)
	assert.NotNil(t, metrics.timings)
	assert.True(t, metrics.enabled)
	assert.NotZero(t, metrics.startTime)
}

// TestCacheMetrics_EnableDisableMetrics tests metrics enable/disable functionality
func TestCacheMetrics_EnableDisableMetrics(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	// Initially enabled
	assert.True(t, metrics.IsEnabled())

	// Disable metrics
	metrics.DisableMetrics()
	assert.False(t, metrics.IsEnabled())

	// Re-enable metrics
	metrics.EnableMetrics()
	assert.True(t, metrics.IsEnabled())
}

// TestCacheMetrics_RecordCacheHit tests cache hit recording
func TestCacheMetrics_RecordCacheHit(t *testing.T) {
	t.Run("EnabledMetrics", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()

		// Record hits for different entity types
		metrics.RecordCacheHit("threat")
		metrics.RecordCacheHit("document")
		metrics.RecordCacheHit("source")
		metrics.RecordCacheHit("auth")
		metrics.RecordCacheHit("metadata")
		metrics.RecordCacheHit("unknown") // Should still record general hit

		// Check counters
		assert.Equal(t, int64(6), metrics.counters[string(MetricCacheHit)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricThreatCacheHit)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricDocumentCacheHit)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricSourceCacheHit)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricAuthCacheHit)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricMetadataCacheHit)])
	})

	t.Run("DisabledMetrics", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		metrics.DisableMetrics()

		metrics.RecordCacheHit("threat")

		// Should not record anything when disabled
		assert.Equal(t, int64(0), metrics.counters[string(MetricCacheHit)])
	})
}

// TestCacheMetrics_RecordCacheMiss tests cache miss recording
func TestCacheMetrics_RecordCacheMiss(t *testing.T) {
	t.Run("EnabledMetrics", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()

		// Record misses for different entity types
		metrics.RecordCacheMiss("threat")
		metrics.RecordCacheMiss("document")
		metrics.RecordCacheMiss("source")
		metrics.RecordCacheMiss("auth")
		metrics.RecordCacheMiss("metadata")

		// Check counters
		assert.Equal(t, int64(5), metrics.counters[string(MetricCacheMiss)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricThreatCacheMiss)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricDocumentCacheMiss)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricSourceCacheMiss)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricAuthCacheMiss)])
		assert.Equal(t, int64(1), metrics.counters[string(MetricMetadataCacheMiss)])
	})

	t.Run("DisabledMetrics", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		metrics.DisableMetrics()

		metrics.RecordCacheMiss("threat")

		// Should not record anything when disabled
		assert.Equal(t, int64(0), metrics.counters[string(MetricCacheMiss)])
	})
}

// TestCacheMetrics_RecordCacheOperations tests cache operation recording
func TestCacheMetrics_RecordCacheOperations(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	// Test write recording
	metrics.RecordCacheWrite("threat")
	metrics.RecordCacheWrite("document")
	assert.Equal(t, int64(2), metrics.counters[string(MetricCacheWrite)])

	// Test delete recording
	metrics.RecordCacheDelete("threat")
	assert.Equal(t, int64(1), metrics.counters[string(MetricCacheDelete)])

	// Test invalidation recording
	duration := 50 * time.Millisecond
	metrics.RecordCacheInvalidation("threat", duration)
	assert.Equal(t, int64(1), metrics.counters[string(MetricCacheInvalidation)])
	assert.Len(t, metrics.timings[string(MetricInvalidationTime)], 1)
	assert.Equal(t, duration, metrics.timings[string(MetricInvalidationTime)][0])
}

// TestCacheMetrics_RecordLatency tests latency recording
func TestCacheMetrics_RecordLatency(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	// Record multiple latency measurements
	latencies := []time.Duration{
		10 * time.Millisecond,
		25 * time.Millisecond,
		15 * time.Millisecond,
	}

	for _, latency := range latencies {
		metrics.RecordCacheLatency("get", latency)
	}

	assert.Len(t, metrics.timings[string(MetricCacheLatency)], 3)
	assert.Equal(t, latencies, metrics.timings[string(MetricCacheLatency)])
}

// TestCacheMetrics_RecordWarmingDuration tests warming duration recording
func TestCacheMetrics_RecordWarmingDuration(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	duration := 2 * time.Second
	metrics.RecordWarmingDuration(duration)

	assert.Len(t, metrics.timings[string(MetricWarmingDuration)], 1)
	assert.Equal(t, duration, metrics.timings[string(MetricWarmingDuration)][0])
}

// TestCacheMetrics_RecordCacheError tests error recording
func TestCacheMetrics_RecordCacheError(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	// Record different types of errors
	metrics.RecordCacheError("connection")
	metrics.RecordCacheError("timeout")
	metrics.RecordCacheError("generic")

	assert.Equal(t, int64(3), metrics.counters[string(MetricCacheError)])
	assert.Equal(t, int64(1), metrics.counters[string(MetricConnectionError)])
	assert.Equal(t, int64(1), metrics.counters[string(MetricTimeoutError)])
}

// TestCacheMetrics_TimingMemoryManagement tests timing memory management
func TestCacheMetrics_TimingMemoryManagement(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	// Record 1001 latency measurements to test memory limit
	for i := 0; i < 1001; i++ {
		metrics.RecordCacheLatency("test", time.Duration(i)*time.Millisecond)
	}

	// Should be limited to 1000 measurements
	assert.Len(t, metrics.timings[string(MetricCacheLatency)], 1000)
	// First measurement should be removed
	assert.Equal(t, 1*time.Millisecond, metrics.timings[string(MetricCacheLatency)][0])
	// Last measurement should be preserved
	assert.Equal(t, 1000*time.Millisecond, metrics.timings[string(MetricCacheLatency)][999])
}

// TestCacheMetrics_GetCacheStats tests statistics generation
func TestCacheMetrics_GetCacheStats(t *testing.T) {
	t.Run("EnabledMetrics", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		ctx := context.Background()

		// Record some test data
		metrics.RecordCacheHit("threat")
		metrics.RecordCacheHit("threat")
		metrics.RecordCacheHit("document")
		metrics.RecordCacheMiss("threat")
		metrics.RecordCacheMiss("document")
		metrics.RecordCacheMiss("document")

		metrics.RecordCacheLatency("get", 10*time.Millisecond)
		metrics.RecordCacheLatency("get", 20*time.Millisecond)
		metrics.RecordCacheLatency("get", 30*time.Millisecond)

		metrics.RecordCacheError("connection")
		metrics.RecordCacheError("timeout")

		stats, err := metrics.GetCacheStats(ctx)

		assert.NoError(t, err)
		assert.NotNil(t, stats)

		// Check overall stats
		assert.Equal(t, int64(3), stats.TotalHits)
		assert.Equal(t, int64(3), stats.TotalMisses)
		assert.Equal(t, 0.5, stats.HitRatio) // 3 hits / (3 hits + 3 misses) = 0.5

		// Check entity-specific stats
		assert.Equal(t, int64(2), stats.ThreatStats.Hits)
		assert.Equal(t, int64(1), stats.ThreatStats.Misses)
		assert.Equal(t, float64(2)/float64(3), stats.ThreatStats.HitRatio) // 2/(2+1)

		assert.Equal(t, int64(1), stats.DocumentStats.Hits)
		assert.Equal(t, int64(2), stats.DocumentStats.Misses)
		assert.Equal(t, float64(1)/float64(3), stats.DocumentStats.HitRatio) // 1/(1+2)

		// Check latency stats
		assert.Equal(t, 20*time.Millisecond, stats.AverageLatency) // (10+20+30)/3
		assert.Equal(t, 30*time.Millisecond, stats.MaxLatency)
		assert.Equal(t, 10*time.Millisecond, stats.MinLatency)

		// Check error stats
		assert.Equal(t, int64(2), stats.TotalErrors)
		assert.Equal(t, int64(1), stats.ConnectionErrors)
		assert.Equal(t, int64(1), stats.TimeoutErrors)

		// Check system stats
		assert.NotZero(t, stats.Uptime)
	})

	t.Run("DisabledMetrics", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		metrics.DisableMetrics()
		ctx := context.Background()

		_, err := metrics.GetCacheStats(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "metrics collection is disabled")
	})

	t.Run("NoData", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		ctx := context.Background()

		stats, err := metrics.GetCacheStats(ctx)

		assert.NoError(t, err)
		assert.NotNil(t, stats)
		assert.Equal(t, int64(0), stats.TotalHits)
		assert.Equal(t, int64(0), stats.TotalMisses)
		assert.Equal(t, float64(0), stats.HitRatio)
		assert.Equal(t, time.Duration(0), stats.AverageLatency)
	})
}

// TestCacheMetrics_CalculateEntityStats tests entity-specific statistics calculation
func TestCacheMetrics_CalculateEntityStats(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	// Record specific threat metrics
	metrics.RecordCacheHit("threat")
	metrics.RecordCacheHit("threat")
	metrics.RecordCacheMiss("threat")
	metrics.RecordCacheWrite("threat")
	metrics.RecordCacheDelete("threat")

	stats := metrics.calculateEntityStats("threat")

	assert.Equal(t, int64(2), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Equal(t, float64(2)/float64(3), stats.HitRatio) // 2/(2+1)
	assert.Equal(t, int64(1), stats.Writes)
	assert.Equal(t, int64(1), stats.Deletes)
}

// TestCacheMetrics_LatencyCalculations tests latency calculation methods
func TestCacheMetrics_LatencyCalculations(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	t.Run("CalculateAverageLatency", func(t *testing.T) {
		latencies := []time.Duration{
			10 * time.Millisecond,
			20 * time.Millisecond,
			30 * time.Millisecond,
		}
		avg := metrics.calculateAverageLatency(latencies)
		assert.Equal(t, 20*time.Millisecond, avg)

		// Test empty slice
		avg = metrics.calculateAverageLatency([]time.Duration{})
		assert.Equal(t, time.Duration(0), avg)
	})

	t.Run("FindMaxLatency", func(t *testing.T) {
		latencies := []time.Duration{
			10 * time.Millisecond,
			30 * time.Millisecond,
			20 * time.Millisecond,
		}
		max := metrics.findMaxLatency(latencies)
		assert.Equal(t, 30*time.Millisecond, max)

		// Test empty slice
		max = metrics.findMaxLatency([]time.Duration{})
		assert.Equal(t, time.Duration(0), max)
	})

	t.Run("FindMinLatency", func(t *testing.T) {
		latencies := []time.Duration{
			20 * time.Millisecond,
			10 * time.Millisecond,
			30 * time.Millisecond,
		}
		min := metrics.findMinLatency(latencies)
		assert.Equal(t, 10*time.Millisecond, min)

		// Test empty slice
		min = metrics.findMinLatency([]time.Duration{})
		assert.Equal(t, time.Duration(0), min)
	})
}

// TestCacheMetrics_ResetMetrics tests metrics reset functionality
func TestCacheMetrics_ResetMetrics(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	// Record some data
	metrics.RecordCacheHit("threat")
	metrics.RecordCacheMiss("document")
	metrics.RecordCacheLatency("get", 10*time.Millisecond)

	// Verify data exists
	assert.Equal(t, int64(1), metrics.counters[string(MetricCacheHit)])
	assert.Equal(t, int64(1), metrics.counters[string(MetricCacheMiss)])
	assert.Len(t, metrics.timings[string(MetricCacheLatency)], 1)

	oldStartTime := metrics.startTime

	// Reset metrics
	time.Sleep(1 * time.Millisecond) // Ensure time difference
	metrics.ResetMetrics()

	// Verify reset
	assert.Equal(t, int64(0), metrics.counters[string(MetricCacheHit)])
	assert.Equal(t, int64(0), metrics.counters[string(MetricCacheMiss)])
	assert.Len(t, metrics.timings[string(MetricCacheLatency)], 0)
	assert.True(t, metrics.startTime.After(oldStartTime))
}

// TestCacheMetrics_ExportMetrics tests metrics export functionality
func TestCacheMetrics_ExportMetrics(t *testing.T) {
	t.Run("ValidExport", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		ctx := context.Background()

		// Record some test data
		metrics.RecordCacheHit("threat")
		metrics.RecordCacheMiss("document")

		data, err := metrics.ExportMetrics(ctx)

		assert.NoError(t, err)
		assert.NotNil(t, data)

		// Verify it's valid JSON
		var stats CacheStats
		err = json.Unmarshal(data, &stats)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), stats.TotalHits)
		assert.Equal(t, int64(1), stats.TotalMisses)
	})

	t.Run("DisabledMetrics", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		metrics.DisableMetrics()
		ctx := context.Background()

		_, err := metrics.ExportMetrics(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get cache stats")
	})
}

// TestCacheMetrics_GetHealthCheck tests health check functionality
func TestCacheMetrics_GetHealthCheck(t *testing.T) {
	t.Run("HealthyStatus", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		ctx := context.Background()

		// Record healthy metrics (high hit ratio, low errors)
		for i := 0; i < 80; i++ {
			metrics.RecordCacheHit("threat")
		}
		for i := 0; i < 20; i++ {
			metrics.RecordCacheMiss("threat")
		}
		// Hit ratio = 80/100 = 0.8 (above 70% threshold)

		health := metrics.GetHealthCheck(ctx)

		assert.Equal(t, "healthy", health["status"])
		assert.Equal(t, 0.8, health["hit_ratio"])
		assert.Equal(t, int64(100), health["total_requests"])
		assert.NotNil(t, health["uptime"])
	})

	t.Run("DegradedStatus_LowHitRatio", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		ctx := context.Background()

		// Record low hit ratio (below 70%)
		for i := 0; i < 30; i++ {
			metrics.RecordCacheHit("threat")
		}
		for i := 0; i < 70; i++ {
			metrics.RecordCacheMiss("threat")
		}
		// Hit ratio = 30/100 = 0.3 (below 70% threshold)

		health := metrics.GetHealthCheck(ctx)

		assert.Equal(t, "degraded", health["status"])
		assert.Equal(t, "Hit ratio below 70%", health["warning"])
		assert.Equal(t, 0.3, health["hit_ratio"])
	})

	t.Run("DegradedStatus_HighErrors", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		ctx := context.Background()

		// Record high error count
		for i := 0; i < 150; i++ {
			metrics.RecordCacheError("connection")
		}

		health := metrics.GetHealthCheck(ctx)

		assert.Equal(t, "degraded", health["status"])
		assert.Equal(t, "High error count", health["warning"])
	})

	t.Run("UnhealthyStatus", func(t *testing.T) {
		metrics, _ := newTestCacheMetrics()
		metrics.DisableMetrics()
		ctx := context.Background()

		health := metrics.GetHealthCheck(ctx)

		assert.Equal(t, "unhealthy", health["status"])
		assert.NotNil(t, health["error"])
	})
}

// TestMetricType tests metric type constants
func TestMetricType(t *testing.T) {
	assert.Equal(t, MetricType("cache_hit"), MetricCacheHit)
	assert.Equal(t, MetricType("cache_miss"), MetricCacheMiss)
	assert.Equal(t, MetricType("threat_cache_hit"), MetricThreatCacheHit)
	assert.Equal(t, MetricType("cache_latency"), MetricCacheLatency)
	assert.Equal(t, MetricType("cache_error"), MetricCacheError)
}

// TestEntityStats tests entity statistics structure
func TestEntityStats(t *testing.T) {
	stats := EntityStats{
		Hits:     10,
		Misses:   5,
		HitRatio: 0.67,
		Writes:   8,
		Deletes:  2,
	}

	assert.Equal(t, int64(10), stats.Hits)
	assert.Equal(t, int64(5), stats.Misses)
	assert.Equal(t, 0.67, stats.HitRatio)
	assert.Equal(t, int64(8), stats.Writes)
	assert.Equal(t, int64(2), stats.Deletes)
}

// TestCacheStats tests cache statistics structure
func TestCacheStats(t *testing.T) {
	now := time.Now()
	stats := CacheStats{
		TotalHits:   100,
		TotalMisses: 25,
		HitRatio:    0.8,
		ThreatStats: EntityStats{
			Hits:     50,
			Misses:   10,
			HitRatio: 0.83,
		},
		AverageLatency:    15 * time.Millisecond,
		MaxLatency:        50 * time.Millisecond,
		MinLatency:        5 * time.Millisecond,
		TotalKeys:         1000,
		MemoryUsage:       1024 * 1024, // 1MB
		ConnectionsActive: 5,
		Uptime:            2 * time.Hour,
		LastResetTime:     now,
		TotalErrors:       5,
		ConnectionErrors:  2,
		TimeoutErrors:     1,
	}

	assert.Equal(t, int64(100), stats.TotalHits)
	assert.Equal(t, int64(25), stats.TotalMisses)
	assert.Equal(t, 0.8, stats.HitRatio)
	assert.Equal(t, 15*time.Millisecond, stats.AverageLatency)
	assert.Equal(t, int64(1000), stats.TotalKeys)
	assert.Equal(t, 2*time.Hour, stats.Uptime)
	assert.Equal(t, int64(50), stats.ThreatStats.Hits)
}

// TestCacheMetrics_ConcurrentAccess tests concurrent access safety
func TestCacheMetrics_ConcurrentAccess(t *testing.T) {
	metrics, _ := newTestCacheMetrics()

	// Test concurrent counter increments
	done := make(chan bool)
	numGoroutines := 10
	incrementsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < incrementsPerGoroutine; j++ {
				metrics.RecordCacheHit("threat")
				metrics.RecordCacheMiss("document")
				metrics.RecordCacheLatency("get", time.Duration(j)*time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	expectedTotal := int64(numGoroutines * incrementsPerGoroutine)
	assert.Equal(t, expectedTotal, metrics.counters[string(MetricCacheHit)])
	assert.Equal(t, expectedTotal, metrics.counters[string(MetricCacheMiss)])
	assert.Equal(t, expectedTotal, metrics.counters[string(MetricThreatCacheHit)])
	assert.Equal(t, expectedTotal, metrics.counters[string(MetricDocumentCacheMiss)])

	// Check that timings were recorded (may be limited by memory management)
	latencyCount := int64(len(metrics.timings[string(MetricCacheLatency)]))
	assert.True(t, latencyCount > 0)
	assert.True(t, latencyCount <= 1000) // Should not exceed memory limit
}
