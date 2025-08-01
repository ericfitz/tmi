package telemetry

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// LogSampler provides intelligent log sampling to reduce overhead and storage costs
type LogSampler struct {
	config             *LogSamplingConfig
	rateLimiters       map[LogLevel]*RateLimiter
	adaptiveSampler    *AdaptiveSampler
	contextSampler     *ContextSampler
	performanceTracker *PerformanceTracker
	mu                 sync.RWMutex
}

// LogSamplingConfig defines sampling behavior for different log levels and scenarios
type LogSamplingConfig struct {
	// Base sampling rates by log level
	DebugSampleRate float64
	InfoSampleRate  float64
	WarnSampleRate  float64
	ErrorSampleRate float64
	FatalSampleRate float64

	// Rate limiting (logs per second)
	DebugRateLimit int
	InfoRateLimit  int
	WarnRateLimit  int
	ErrorRateLimit int

	// Adaptive sampling
	EnableAdaptive     bool
	AdaptiveWindow     time.Duration
	AdaptiveTargetRate float64

	// Context-aware sampling
	EnableContextual  bool
	HighValueContexts []string
	LowValueContexts  []string

	// Performance-based sampling
	EnablePerformanceBased bool
	MaxOverheadPercent     float64

	// Burst handling
	EnableBurstProtection bool
	BurstThreshold        int
	BurstSampleRate       float64
	BurstWindow           time.Duration
}

// RateLimiter implements a token bucket rate limiter for log sampling
type RateLimiter struct {
	rate     int
	tokens   int64
	lastTime int64
	mu       sync.Mutex
}

// AdaptiveSampler adjusts sampling rates based on log volume and system performance
type AdaptiveSampler struct {
	targetRate   float64
	window       time.Duration
	currentRate  float64
	logCount     int64
	sampledCount int64
	windowStart  time.Time
	mu           sync.RWMutex
}

// ContextSampler provides context-aware sampling decisions
type ContextSampler struct {
	highValueContexts map[string]bool
	lowValueContexts  map[string]bool
	contextRates      map[string]float64
	mu                sync.RWMutex
}

// PerformanceTracker monitors logging performance impact
type PerformanceTracker struct {
	maxOverheadPercent float64
	totalDuration      int64
	logCount           int64
	startTime          time.Time
	enabledMetrics     bool

	// Metrics
	meter               metric.Meter
	samplingDecisions   metric.Int64Counter
	performanceOverhead metric.Float64Histogram
	adaptiveAdjustments metric.Int64Counter
}

// NewLogSampler creates a new log sampler with the given configuration
func NewLogSampler(sampleRate float64) *LogSampler {
	config := &LogSamplingConfig{
		DebugSampleRate:        sampleRate,
		InfoSampleRate:         1.0,
		WarnSampleRate:         1.0,
		ErrorSampleRate:        1.0,
		FatalSampleRate:        1.0,
		DebugRateLimit:         100,
		InfoRateLimit:          1000,
		WarnRateLimit:          -1, // No limit
		ErrorRateLimit:         -1, // No limit
		EnableAdaptive:         true,
		AdaptiveWindow:         time.Minute,
		AdaptiveTargetRate:     0.8,
		EnableContextual:       true,
		EnablePerformanceBased: true,
		MaxOverheadPercent:     2.0,
		EnableBurstProtection:  true,
		BurstThreshold:         1000,
		BurstSampleRate:        0.1,
		BurstWindow:            time.Second * 10,
	}

	return NewLogSamplerWithConfig(config)
}

// NewLogSamplerWithConfig creates a new log sampler with detailed configuration
func NewLogSamplerWithConfig(config *LogSamplingConfig) *LogSampler {
	sampler := &LogSampler{
		config:       config,
		rateLimiters: make(map[LogLevel]*RateLimiter),
	}

	// Initialize rate limiters
	sampler.rateLimiters[LevelDebug] = NewRateLimiter(config.DebugRateLimit)
	sampler.rateLimiters[LevelInfo] = NewRateLimiter(config.InfoRateLimit)
	sampler.rateLimiters[LevelWarn] = NewRateLimiter(config.WarnRateLimit)
	sampler.rateLimiters[LevelError] = NewRateLimiter(config.ErrorRateLimit)

	// Initialize adaptive sampler
	if config.EnableAdaptive {
		sampler.adaptiveSampler = NewAdaptiveSampler(config.AdaptiveTargetRate, config.AdaptiveWindow)
	}

	// Initialize context sampler
	if config.EnableContextual {
		sampler.contextSampler = NewContextSampler(config.HighValueContexts, config.LowValueContexts)
	}

	// Initialize performance tracker
	if config.EnablePerformanceBased {
		sampler.performanceTracker = NewPerformanceTracker(config.MaxOverheadPercent)
	}

	return sampler
}

// ShouldLog determines whether a log entry should be recorded based on sampling rules
func (ls *LogSampler) ShouldLog(level LogLevel) bool {
	return ls.ShouldLogWithContext(context.Background(), level, "", nil)
}

// ShouldLogWithContext determines whether a log entry should be recorded with context awareness
func (ls *LogSampler) ShouldLogWithContext(ctx context.Context, level LogLevel, message string, attrs []attribute.KeyValue) bool {
	// Always log errors and fatal messages
	if level >= LevelError {
		return true
	}

	// Check performance-based sampling
	if ls.performanceTracker != nil && !ls.performanceTracker.ShouldLog() {
		ls.recordSamplingDecision("performance_limited", false)
		return false
	}

	// Check rate limiting
	if rateLimiter, exists := ls.rateLimiters[level]; exists && !rateLimiter.Allow() {
		ls.recordSamplingDecision("rate_limited", false)
		return false
	}

	// Get base sampling rate
	baseRate := ls.getBaseSamplingRate(level)

	// Apply adaptive sampling
	if ls.adaptiveSampler != nil {
		baseRate = ls.adaptiveSampler.AdjustRate(baseRate)
	}

	// Apply context-aware sampling
	if ls.contextSampler != nil {
		contextRate := ls.contextSampler.GetContextRate(ctx, attrs)
		baseRate = baseRate * contextRate
	}

	// Apply burst protection
	if ls.config.EnableBurstProtection && ls.detectBurst(level) {
		baseRate = ls.config.BurstSampleRate
		ls.recordSamplingDecision("burst_protection", baseRate > rand.Float64()) // #nosec G404 - math/rand is acceptable for log sampling
	}

	// Make sampling decision
	shouldSample := baseRate >= 1.0 || rand.Float64() < baseRate // #nosec G404 - math/rand is acceptable for log sampling
	ls.recordSamplingDecision("normal", shouldSample)

	return shouldSample
}

// RecordLogPerformance records the performance impact of a log operation
func (ls *LogSampler) RecordLogPerformance(ctx context.Context, duration time.Duration, level LogLevel) {
	if ls.performanceTracker != nil {
		ls.performanceTracker.RecordLogDuration(duration)
	}

	if ls.adaptiveSampler != nil {
		ls.adaptiveSampler.RecordLog(true) // Assuming it was sampled since we're recording
	}
}

// UpdateSamplingRates dynamically updates sampling rates based on system conditions
func (ls *LogSampler) UpdateSamplingRates(debugRate, infoRate float64) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	ls.config.DebugSampleRate = debugRate
	ls.config.InfoSampleRate = infoRate

	if ls.performanceTracker != nil && ls.performanceTracker.enabledMetrics {
		ls.performanceTracker.adaptiveAdjustments.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("adjustment_type", "manual_update"),
			))
	}
}

// GetSamplingStats returns current sampling statistics
func (ls *LogSampler) GetSamplingStats() map[string]interface{} {
	stats := make(map[string]interface{})

	ls.mu.RLock()
	defer ls.mu.RUnlock()

	// Base rates
	stats["debug_sample_rate"] = ls.config.DebugSampleRate
	stats["info_sample_rate"] = ls.config.InfoSampleRate
	stats["warn_sample_rate"] = ls.config.WarnSampleRate
	stats["error_sample_rate"] = ls.config.ErrorSampleRate

	// Adaptive sampler stats
	if ls.adaptiveSampler != nil {
		adaptiveStats := ls.adaptiveSampler.GetStats()
		for k, v := range adaptiveStats {
			stats["adaptive_"+k] = v
		}
	}

	// Performance tracker stats
	if ls.performanceTracker != nil {
		perfStats := ls.performanceTracker.GetStats()
		for k, v := range perfStats {
			stats["performance_"+k] = v
		}
	}

	return stats
}

// Helper methods

func (ls *LogSampler) getBaseSamplingRate(level LogLevel) float64 {
	switch level {
	case LevelDebug:
		return ls.config.DebugSampleRate
	case LevelInfo:
		return ls.config.InfoSampleRate
	case LevelWarn:
		return ls.config.WarnSampleRate
	case LevelError:
		return ls.config.ErrorSampleRate
	case LevelFatal:
		return ls.config.FatalSampleRate
	default:
		return 1.0
	}
}

func (ls *LogSampler) detectBurst(level LogLevel) bool {
	// Simple burst detection based on recent log frequency
	// In a real implementation, this would be more sophisticated
	return false
}

func (ls *LogSampler) recordSamplingDecision(reason string, sampled bool) {
	if ls.performanceTracker != nil && ls.performanceTracker.enabledMetrics {
		result := "dropped"
		if sampled {
			result = "sampled"
		}

		ls.performanceTracker.samplingDecisions.Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("reason", reason),
				attribute.String("result", result),
			))
	}
}

// RateLimiter implementation

// NewRateLimiter creates a new token bucket rate limiter
func NewRateLimiter(rate int) *RateLimiter {
	if rate <= 0 {
		return &RateLimiter{rate: -1} // No limit
	}

	return &RateLimiter{
		rate:     rate,
		tokens:   int64(rate),
		lastTime: time.Now().UnixNano(),
	}
}

// Allow checks if an operation is allowed under the rate limit
func (rl *RateLimiter) Allow() bool {
	if rl.rate <= 0 {
		return true // No limit
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now().UnixNano()
	elapsed := now - rl.lastTime

	// Add tokens based on elapsed time
	tokensToAdd := int64(float64(elapsed) * float64(rl.rate) / float64(time.Second))
	rl.tokens += tokensToAdd

	// Cap at rate limit
	if rl.tokens > int64(rl.rate) {
		rl.tokens = int64(rl.rate)
	}

	rl.lastTime = now

	// Check if we have tokens available
	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// AdaptiveSampler implementation

// NewAdaptiveSampler creates a new adaptive sampler
func NewAdaptiveSampler(targetRate float64, window time.Duration) *AdaptiveSampler {
	return &AdaptiveSampler{
		targetRate:  targetRate,
		window:      window,
		currentRate: targetRate,
		windowStart: time.Now(),
	}
}

// AdjustRate adjusts the sampling rate based on current conditions
func (as *AdaptiveSampler) AdjustRate(baseRate float64) float64 {
	as.mu.Lock()
	defer as.mu.Unlock()

	now := time.Now()
	if now.Sub(as.windowStart) >= as.window {
		// Calculate actual rate for the window
		actualRate := float64(as.sampledCount) / float64(as.logCount)

		// Adjust current rate based on target
		if actualRate > as.targetRate {
			as.currentRate *= 0.9 // Reduce sampling
		} else if actualRate < as.targetRate*0.8 {
			as.currentRate *= 1.1 // Increase sampling
		}

		// Clamp between 0 and 1
		if as.currentRate > 1.0 {
			as.currentRate = 1.0
		} else if as.currentRate < 0.01 {
			as.currentRate = 0.01
		}

		// Reset counters
		as.logCount = 0
		as.sampledCount = 0
		as.windowStart = now
	}

	return baseRate * as.currentRate
}

// RecordLog records a log event for adaptive sampling
func (as *AdaptiveSampler) RecordLog(sampled bool) {
	as.mu.Lock()
	defer as.mu.Unlock()

	atomic.AddInt64(&as.logCount, 1)
	if sampled {
		atomic.AddInt64(&as.sampledCount, 1)
	}
}

// GetStats returns adaptive sampler statistics
func (as *AdaptiveSampler) GetStats() map[string]interface{} {
	as.mu.RLock()
	defer as.mu.RUnlock()

	return map[string]interface{}{
		"current_rate":  as.currentRate,
		"target_rate":   as.targetRate,
		"log_count":     as.logCount,
		"sampled_count": as.sampledCount,
	}
}

// ContextSampler implementation

// NewContextSampler creates a new context-aware sampler
func NewContextSampler(highValueContexts, lowValueContexts []string) *ContextSampler {
	cs := &ContextSampler{
		highValueContexts: make(map[string]bool),
		lowValueContexts:  make(map[string]bool),
		contextRates:      make(map[string]float64),
	}

	for _, context := range highValueContexts {
		cs.highValueContexts[context] = true
		cs.contextRates[context] = 2.0 // Higher sampling
	}

	for _, context := range lowValueContexts {
		cs.lowValueContexts[context] = true
		cs.contextRates[context] = 0.5 // Lower sampling
	}

	return cs
}

// GetContextRate returns a multiplier for the sampling rate based on context
func (cs *ContextSampler) GetContextRate(ctx context.Context, attrs []attribute.KeyValue) float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// Check context values
	if component := ctx.Value("component"); component != nil {
		if rate, exists := cs.contextRates[component.(string)]; exists {
			return rate
		}
	}

	// Check attributes
	for _, attr := range attrs {
		key := string(attr.Key)
		value := attr.Value.AsString()

		contextKey := key + ":" + value
		if rate, exists := cs.contextRates[contextKey]; exists {
			return rate
		}
	}

	return 1.0 // Default rate
}

// PerformanceTracker implementation

// NewPerformanceTracker creates a new performance tracker
func NewPerformanceTracker(maxOverheadPercent float64) *PerformanceTracker {
	service := GetService()
	var meter metric.Meter
	enabledMetrics := false

	if service != nil {
		meter = service.GetMeter()
		enabledMetrics = true
	}

	pt := &PerformanceTracker{
		maxOverheadPercent: maxOverheadPercent,
		startTime:          time.Now(),
		enabledMetrics:     enabledMetrics,
		meter:              meter,
	}

	if enabledMetrics {
		pt.initializeMetrics()
	}

	return pt
}

func (pt *PerformanceTracker) initializeMetrics() {
	var err error

	pt.samplingDecisions, err = pt.meter.Int64Counter(
		"log_sampling_decisions_total",
		metric.WithDescription("Total log sampling decisions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		pt.enabledMetrics = false
		return
	}

	pt.performanceOverhead, err = pt.meter.Float64Histogram(
		"log_performance_overhead_ratio",
		metric.WithDescription("Logging performance overhead ratio"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2),
	)
	if err != nil {
		pt.enabledMetrics = false
		return
	}

	pt.adaptiveAdjustments, err = pt.meter.Int64Counter(
		"log_adaptive_adjustments_total",
		metric.WithDescription("Total adaptive sampling adjustments"),
		metric.WithUnit("1"),
	)
	if err != nil {
		pt.enabledMetrics = false
	}
}

// ShouldLog determines if logging should proceed based on performance impact
func (pt *PerformanceTracker) ShouldLog() bool {
	overheadRatio := pt.GetOverheadRatio()

	if pt.enabledMetrics {
		pt.performanceOverhead.Record(context.Background(), overheadRatio)
	}

	return overheadRatio < pt.maxOverheadPercent/100.0
}

// RecordLogDuration records the duration of a log operation
func (pt *PerformanceTracker) RecordLogDuration(duration time.Duration) {
	atomic.AddInt64(&pt.totalDuration, duration.Nanoseconds())
	atomic.AddInt64(&pt.logCount, 1)
}

// GetOverheadRatio returns the current logging overhead ratio
func (pt *PerformanceTracker) GetOverheadRatio() float64 {
	totalTime := time.Since(pt.startTime).Nanoseconds()
	logTime := atomic.LoadInt64(&pt.totalDuration)

	if totalTime == 0 {
		return 0.0
	}

	return float64(logTime) / float64(totalTime)
}

// GetStats returns performance tracker statistics
func (pt *PerformanceTracker) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"overhead_ratio":     pt.GetOverheadRatio(),
		"max_overhead":       pt.maxOverheadPercent,
		"total_log_count":    atomic.LoadInt64(&pt.logCount),
		"total_log_duration": time.Duration(atomic.LoadInt64(&pt.totalDuration)),
	}
}

// GetDefaultLogSamplingConfig returns a default sampling configuration
func GetDefaultLogSamplingConfig() *LogSamplingConfig {
	return &LogSamplingConfig{
		DebugSampleRate:        0.1,
		InfoSampleRate:         1.0,
		WarnSampleRate:         1.0,
		ErrorSampleRate:        1.0,
		FatalSampleRate:        1.0,
		DebugRateLimit:         100,
		InfoRateLimit:          1000,
		WarnRateLimit:          -1,
		ErrorRateLimit:         -1,
		EnableAdaptive:         true,
		AdaptiveWindow:         time.Minute,
		AdaptiveTargetRate:     0.8,
		EnableContextual:       true,
		EnablePerformanceBased: true,
		MaxOverheadPercent:     2.0,
		EnableBurstProtection:  true,
		BurstThreshold:         1000,
		BurstSampleRate:        0.1,
		BurstWindow:            time.Second * 10,
	}
}
