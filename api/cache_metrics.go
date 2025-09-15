package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
)

// CacheMetrics tracks cache performance and usage statistics
type CacheMetrics struct {
	redis     *db.RedisDB
	mutex     sync.RWMutex
	counters  map[string]int64
	timings   map[string][]time.Duration
	startTime time.Time
	enabled   bool
}

// MetricType defines different types of cache metrics
type MetricType string

const (
	// Cache hit/miss metrics
	MetricCacheHit          MetricType = "cache_hit"
	MetricCacheMiss         MetricType = "cache_miss"
	MetricCacheWrite        MetricType = "cache_write"
	MetricCacheDelete       MetricType = "cache_delete"
	MetricCacheInvalidation MetricType = "cache_invalidation"

	// Entity-specific metrics
	MetricThreatCacheHit    MetricType = "threat_cache_hit"
	MetricThreatCacheMiss   MetricType = "threat_cache_miss"
	MetricDocumentCacheHit  MetricType = "document_cache_hit"
	MetricDocumentCacheMiss MetricType = "document_cache_miss"
	MetricSourceCacheHit    MetricType = "source_cache_hit"
	MetricSourceCacheMiss   MetricType = "source_cache_miss"
	MetricAuthCacheHit      MetricType = "auth_cache_hit"
	MetricAuthCacheMiss     MetricType = "auth_cache_miss"
	MetricMetadataCacheHit  MetricType = "metadata_cache_hit"
	MetricMetadataCacheMiss MetricType = "metadata_cache_miss"

	// Performance metrics
	MetricCacheLatency     MetricType = "cache_latency"
	MetricWarmingDuration  MetricType = "warming_duration"
	MetricInvalidationTime MetricType = "invalidation_time"

	// Error metrics
	MetricCacheError      MetricType = "cache_error"
	MetricConnectionError MetricType = "connection_error"
	MetricTimeoutError    MetricType = "timeout_error"
)

// CacheStats represents current cache statistics
type CacheStats struct {
	// Hit/Miss ratios
	TotalHits   int64   `json:"total_hits"`
	TotalMisses int64   `json:"total_misses"`
	HitRatio    float64 `json:"hit_ratio"`

	// Entity-specific stats
	ThreatStats   EntityStats `json:"threat_stats"`
	DocumentStats EntityStats `json:"document_stats"`
	SourceStats   EntityStats `json:"source_stats"`
	AuthStats     EntityStats `json:"auth_stats"`
	MetadataStats EntityStats `json:"metadata_stats"`

	// Performance stats
	AverageLatency time.Duration `json:"average_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	MinLatency     time.Duration `json:"min_latency"`

	// System stats
	TotalKeys         int64         `json:"total_keys"`
	MemoryUsage       int64         `json:"memory_usage_bytes"`
	ConnectionsActive int           `json:"connections_active"`
	Uptime            time.Duration `json:"uptime"`
	LastResetTime     time.Time     `json:"last_reset_time"`

	// Error stats
	TotalErrors      int64 `json:"total_errors"`
	ConnectionErrors int64 `json:"connection_errors"`
	TimeoutErrors    int64 `json:"timeout_errors"`
}

// EntityStats represents statistics for a specific entity type
type EntityStats struct {
	Hits     int64   `json:"hits"`
	Misses   int64   `json:"misses"`
	HitRatio float64 `json:"hit_ratio"`
	Writes   int64   `json:"writes"`
	Deletes  int64   `json:"deletes"`
}

// NewCacheMetrics creates a new cache metrics tracker
func NewCacheMetrics(redis *db.RedisDB) *CacheMetrics {
	return &CacheMetrics{
		redis:     redis,
		counters:  make(map[string]int64),
		timings:   make(map[string][]time.Duration),
		startTime: time.Now(),
		enabled:   true,
	}
}

// EnableMetrics enables metric collection
func (cm *CacheMetrics) EnableMetrics() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.enabled = true
}

// DisableMetrics disables metric collection
func (cm *CacheMetrics) DisableMetrics() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.enabled = false
}

// IsEnabled returns whether metrics collection is enabled
func (cm *CacheMetrics) IsEnabled() bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.enabled
}

// RecordCacheHit records a cache hit for the specified entity type
func (cm *CacheMetrics) RecordCacheHit(entityType string) {
	if !cm.IsEnabled() {
		return
	}

	cm.incrementCounter(string(MetricCacheHit))

	switch entityType {
	case "threat":
		cm.incrementCounter(string(MetricThreatCacheHit))
	case "document":
		cm.incrementCounter(string(MetricDocumentCacheHit))
	case "source":
		cm.incrementCounter(string(MetricSourceCacheHit))
	case "auth":
		cm.incrementCounter(string(MetricAuthCacheHit))
	case "metadata":
		cm.incrementCounter(string(MetricMetadataCacheHit))
	}
}

// RecordCacheMiss records a cache miss for the specified entity type
func (cm *CacheMetrics) RecordCacheMiss(entityType string) {
	if !cm.IsEnabled() {
		return
	}

	cm.incrementCounter(string(MetricCacheMiss))

	switch entityType {
	case "threat":
		cm.incrementCounter(string(MetricThreatCacheMiss))
	case "document":
		cm.incrementCounter(string(MetricDocumentCacheMiss))
	case "source":
		cm.incrementCounter(string(MetricSourceCacheMiss))
	case "auth":
		cm.incrementCounter(string(MetricAuthCacheMiss))
	case "metadata":
		cm.incrementCounter(string(MetricMetadataCacheMiss))
	}
}

// RecordCacheWrite records a cache write operation
func (cm *CacheMetrics) RecordCacheWrite(entityType string) {
	if !cm.IsEnabled() {
		return
	}

	cm.incrementCounter(string(MetricCacheWrite))
}

// RecordCacheDelete records a cache delete operation
func (cm *CacheMetrics) RecordCacheDelete(entityType string) {
	if !cm.IsEnabled() {
		return
	}

	cm.incrementCounter(string(MetricCacheDelete))
}

// RecordCacheInvalidation records a cache invalidation operation
func (cm *CacheMetrics) RecordCacheInvalidation(entityType string, duration time.Duration) {
	if !cm.IsEnabled() {
		return
	}

	cm.incrementCounter(string(MetricCacheInvalidation))
	cm.recordTiming(string(MetricInvalidationTime), duration)
}

// RecordCacheLatency records cache operation latency
func (cm *CacheMetrics) RecordCacheLatency(operation string, duration time.Duration) {
	if !cm.IsEnabled() {
		return
	}

	cm.recordTiming(string(MetricCacheLatency), duration)
}

// RecordWarmingDuration records cache warming duration
func (cm *CacheMetrics) RecordWarmingDuration(duration time.Duration) {
	if !cm.IsEnabled() {
		return
	}

	cm.recordTiming(string(MetricWarmingDuration), duration)
}

// RecordCacheError records a cache error
func (cm *CacheMetrics) RecordCacheError(errorType string) {
	if !cm.IsEnabled() {
		return
	}

	cm.incrementCounter(string(MetricCacheError))

	switch errorType {
	case "connection":
		cm.incrementCounter(string(MetricConnectionError))
	case "timeout":
		cm.incrementCounter(string(MetricTimeoutError))
	}
}

// incrementCounter safely increments a counter
func (cm *CacheMetrics) incrementCounter(key string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.counters[key]++
}

// recordTiming safely records a timing measurement
func (cm *CacheMetrics) recordTiming(key string, duration time.Duration) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if cm.timings[key] == nil {
		cm.timings[key] = make([]time.Duration, 0)
	}

	// Keep only the last 1000 measurements to prevent memory growth
	if len(cm.timings[key]) >= 1000 {
		cm.timings[key] = cm.timings[key][1:]
	}

	cm.timings[key] = append(cm.timings[key], duration)
}

// GetCacheStats returns current cache statistics
func (cm *CacheMetrics) GetCacheStats(ctx context.Context) (*CacheStats, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if !cm.enabled {
		return nil, fmt.Errorf("metrics collection is disabled")
	}

	stats := &CacheStats{
		TotalHits:        cm.counters[string(MetricCacheHit)],
		TotalMisses:      cm.counters[string(MetricCacheMiss)],
		TotalErrors:      cm.counters[string(MetricCacheError)],
		ConnectionErrors: cm.counters[string(MetricConnectionError)],
		TimeoutErrors:    cm.counters[string(MetricTimeoutError)],
		Uptime:           time.Since(cm.startTime),
		LastResetTime:    cm.startTime,
	}

	// Calculate hit ratio
	totalRequests := stats.TotalHits + stats.TotalMisses
	if totalRequests > 0 {
		stats.HitRatio = float64(stats.TotalHits) / float64(totalRequests)
	}

	// Calculate entity-specific stats
	stats.ThreatStats = cm.calculateEntityStats("threat")
	stats.DocumentStats = cm.calculateEntityStats("document")
	stats.SourceStats = cm.calculateEntityStats("source")
	stats.AuthStats = cm.calculateEntityStats("auth")
	stats.MetadataStats = cm.calculateEntityStats("metadata")

	// Calculate latency stats
	if latencies := cm.timings[string(MetricCacheLatency)]; len(latencies) > 0 {
		stats.AverageLatency = cm.calculateAverageLatency(latencies)
		stats.MaxLatency = cm.findMaxLatency(latencies)
		stats.MinLatency = cm.findMinLatency(latencies)
	}

	// Get system stats from Redis
	if err := cm.populateSystemStats(ctx, stats); err != nil {
		slogging.Get().Error("Failed to get Redis system stats: %v", err)
	}

	return stats, nil
}

// calculateEntityStats calculates statistics for a specific entity type
func (cm *CacheMetrics) calculateEntityStats(entityType string) EntityStats {
	var hitMetric, missMetric MetricType

	switch entityType {
	case "threat":
		hitMetric = MetricThreatCacheHit
		missMetric = MetricThreatCacheMiss
	case "document":
		hitMetric = MetricDocumentCacheHit
		missMetric = MetricDocumentCacheMiss
	case "source":
		hitMetric = MetricSourceCacheHit
		missMetric = MetricSourceCacheMiss
	case "auth":
		hitMetric = MetricAuthCacheHit
		missMetric = MetricAuthCacheMiss
	case "metadata":
		hitMetric = MetricMetadataCacheHit
		missMetric = MetricMetadataCacheMiss
	default:
		return EntityStats{}
	}

	hits := cm.counters[string(hitMetric)]
	misses := cm.counters[string(missMetric)]
	total := hits + misses

	stats := EntityStats{
		Hits:    hits,
		Misses:  misses,
		Writes:  cm.counters[string(MetricCacheWrite)],  // Approximation
		Deletes: cm.counters[string(MetricCacheDelete)], // Approximation
	}

	if total > 0 {
		stats.HitRatio = float64(hits) / float64(total)
	}

	return stats
}

// calculateAverageLatency calculates the average latency from a slice of durations
func (cm *CacheMetrics) calculateAverageLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	var total time.Duration
	for _, latency := range latencies {
		total += latency
	}

	return total / time.Duration(len(latencies))
}

// findMaxLatency finds the maximum latency from a slice of durations
func (cm *CacheMetrics) findMaxLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	max := latencies[0]
	for _, latency := range latencies[1:] {
		if latency > max {
			max = latency
		}
	}

	return max
}

// findMinLatency finds the minimum latency from a slice of durations
func (cm *CacheMetrics) findMinLatency(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	min := latencies[0]
	for _, latency := range latencies[1:] {
		if latency < min {
			min = latency
		}
	}

	return min
}

// populateSystemStats populates system-level statistics from Redis
func (cm *CacheMetrics) populateSystemStats(ctx context.Context, stats *CacheStats) error {
	// In a real implementation, this would query Redis INFO command
	// For now, we'll set some placeholder values
	stats.TotalKeys = 0
	stats.MemoryUsage = 0
	stats.ConnectionsActive = 1

	return nil
}

// ResetMetrics resets all metrics counters and timings
func (cm *CacheMetrics) ResetMetrics() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.counters = make(map[string]int64)
	cm.timings = make(map[string][]time.Duration)
	cm.startTime = time.Now()

	slogging.Get().Info("Cache metrics reset")
}

// ExportMetrics exports metrics in JSON format
func (cm *CacheMetrics) ExportMetrics(ctx context.Context) ([]byte, error) {
	stats, err := cm.GetCacheStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache stats: %w", err)
	}

	return json.MarshalIndent(stats, "", "  ")
}

// LogMetricsSummary logs a summary of current metrics
func (cm *CacheMetrics) LogMetricsSummary(ctx context.Context) {
	if !cm.IsEnabled() {
		return
	}

	stats, err := cm.GetCacheStats(ctx)
	if err != nil {
		slogging.Get().Error("Failed to get cache stats for logging: %v", err)
		return
	}

	logger := slogging.Get()
	logger.Info("Cache Metrics Summary:")
	logger.Info("  Hit Ratio: %.2f%% (%d hits, %d misses)",
		stats.HitRatio*100, stats.TotalHits, stats.TotalMisses)
	logger.Info("  Average Latency: %v", stats.AverageLatency)
	logger.Info("  Total Errors: %d", stats.TotalErrors)
	logger.Info("  Uptime: %v", stats.Uptime)

	// Log entity-specific stats
	logger.Info("  Entity Stats:")
	logger.Info("    Threats: %.2f%% hit ratio (%d hits, %d misses)",
		stats.ThreatStats.HitRatio*100, stats.ThreatStats.Hits, stats.ThreatStats.Misses)
	logger.Info("    Documents: %.2f%% hit ratio (%d hits, %d misses)",
		stats.DocumentStats.HitRatio*100, stats.DocumentStats.Hits, stats.DocumentStats.Misses)
	logger.Info("    Sources: %.2f%% hit ratio (%d hits, %d misses)",
		stats.SourceStats.HitRatio*100, stats.SourceStats.Hits, stats.SourceStats.Misses)
	logger.Info("    Auth: %.2f%% hit ratio (%d hits, %d misses)",
		stats.AuthStats.HitRatio*100, stats.AuthStats.Hits, stats.AuthStats.Misses)
	logger.Info("    Metadata: %.2f%% hit ratio (%d hits, %d misses)",
		stats.MetadataStats.HitRatio*100, stats.MetadataStats.Hits, stats.MetadataStats.Misses)
}

// StartMetricsReporting starts periodic metrics reporting
func (cm *CacheMetrics) StartMetricsReporting(ctx context.Context, interval time.Duration) {
	if !cm.IsEnabled() {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cm.LogMetricsSummary(ctx)
			}
		}
	}()
}

// GetHealthCheck returns cache health information
func (cm *CacheMetrics) GetHealthCheck(ctx context.Context) map[string]interface{} {
	health := make(map[string]interface{})

	stats, err := cm.GetCacheStats(ctx)
	if err != nil {
		health["status"] = "unhealthy"
		health["error"] = err.Error()
		return health
	}

	health["status"] = "healthy"
	health["hit_ratio"] = stats.HitRatio
	health["total_requests"] = stats.TotalHits + stats.TotalMisses
	health["error_rate"] = float64(stats.TotalErrors) / float64(stats.TotalHits+stats.TotalMisses+stats.TotalErrors)
	health["uptime"] = stats.Uptime.String()

	// Health thresholds
	if stats.HitRatio < 0.7 {
		health["status"] = "degraded"
		health["warning"] = "Hit ratio below 70%"
	}

	if stats.TotalErrors > 100 {
		health["status"] = "degraded"
		health["warning"] = "High error count"
	}

	return health
}
