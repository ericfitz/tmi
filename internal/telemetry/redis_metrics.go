package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// RedisMetrics provides comprehensive Redis cache performance metrics
type RedisMetrics struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Core Redis metrics
	operationDuration metric.Float64Histogram
	operationCounter  metric.Int64Counter
	connectionCounter metric.Int64UpDownCounter
	commandLatency    metric.Float64Histogram

	// Cache performance metrics
	cacheHitCounter      metric.Int64Counter
	cacheMissCounter     metric.Int64Counter
	cacheHitRatio        metric.Float64Histogram
	keyExpirationCounter metric.Int64Counter
	evictionCounter      metric.Int64Counter

	// Memory and storage metrics
	memoryUsage    metric.Int64UpDownCounter
	keyspaceSize   metric.Int64UpDownCounter
	keyspaceHits   metric.Int64Counter
	keyspaceMisses metric.Int64Counter

	// Pipeline and batch metrics
	pipelineCounter  metric.Int64Counter
	pipelineDuration metric.Float64Histogram
	batchSize        metric.Int64Histogram

	// Error and reliability metrics
	errorCounter        metric.Int64Counter
	timeoutCounter      metric.Int64Counter
	reconnectionCounter metric.Int64Counter

	// Advanced metrics
	slowCommandCounter   metric.Int64Counter
	commandByType        metric.Int64Counter
	dataTypeUsage        metric.Int64Counter
	clientConnectionTime metric.Float64Histogram
}

// NewRedisMetrics creates a new Redis metrics instance
func NewRedisMetrics(tracer trace.Tracer, meter metric.Meter) (*RedisMetrics, error) {
	r := &RedisMetrics{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Core Redis metrics
	r.operationDuration, err = meter.Float64Histogram(
		"redis_operation_duration_seconds",
		metric.WithDescription("Duration of Redis operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create operation duration histogram: %w", err)
	}

	r.operationCounter, err = meter.Int64Counter(
		"redis_operations_total",
		metric.WithDescription("Total number of Redis operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create operation counter: %w", err)
	}

	r.connectionCounter, err = meter.Int64UpDownCounter(
		"redis_connections_active",
		metric.WithDescription("Number of active Redis connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection counter: %w", err)
	}

	r.commandLatency, err = meter.Float64Histogram(
		"redis_command_latency_seconds",
		metric.WithDescription("Latency of Redis commands"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.00001, 0.00005, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create command latency histogram: %w", err)
	}

	// Cache performance metrics
	r.cacheHitCounter, err = meter.Int64Counter(
		"redis_cache_hits_total",
		metric.WithDescription("Total number of cache hits"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache hit counter: %w", err)
	}

	r.cacheMissCounter, err = meter.Int64Counter(
		"redis_cache_misses_total",
		metric.WithDescription("Total number of cache misses"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache miss counter: %w", err)
	}

	r.cacheHitRatio, err = meter.Float64Histogram(
		"redis_cache_hit_ratio",
		metric.WithDescription("Cache hit ratio over time windows"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99, 1.0),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache hit ratio histogram: %w", err)
	}

	r.keyExpirationCounter, err = meter.Int64Counter(
		"redis_key_expirations_total",
		metric.WithDescription("Total number of key expirations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create key expiration counter: %w", err)
	}

	r.evictionCounter, err = meter.Int64Counter(
		"redis_evictions_total",
		metric.WithDescription("Total number of key evictions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create eviction counter: %w", err)
	}

	// Memory and storage metrics
	r.memoryUsage, err = meter.Int64UpDownCounter(
		"redis_memory_usage_bytes",
		metric.WithDescription("Redis memory usage in bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory usage counter: %w", err)
	}

	r.keyspaceSize, err = meter.Int64UpDownCounter(
		"redis_keyspace_size",
		metric.WithDescription("Number of keys in Redis keyspace"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyspace size counter: %w", err)
	}

	r.keyspaceHits, err = meter.Int64Counter(
		"redis_keyspace_hits_total",
		metric.WithDescription("Total keyspace hits"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyspace hits counter: %w", err)
	}

	r.keyspaceMisses, err = meter.Int64Counter(
		"redis_keyspace_misses_total",
		metric.WithDescription("Total keyspace misses"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyspace misses counter: %w", err)
	}

	// Pipeline and batch metrics
	r.pipelineCounter, err = meter.Int64Counter(
		"redis_pipelines_total",
		metric.WithDescription("Total number of Redis pipelines"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline counter: %w", err)
	}

	r.pipelineDuration, err = meter.Float64Histogram(
		"redis_pipeline_duration_seconds",
		metric.WithDescription("Duration of Redis pipeline operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline duration histogram: %w", err)
	}

	r.batchSize, err = meter.Int64Histogram(
		"redis_batch_size",
		metric.WithDescription("Size of Redis batch operations"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create batch size histogram: %w", err)
	}

	// Error and reliability metrics
	r.errorCounter, err = meter.Int64Counter(
		"redis_errors_total",
		metric.WithDescription("Total number of Redis errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create error counter: %w", err)
	}

	r.timeoutCounter, err = meter.Int64Counter(
		"redis_timeouts_total",
		metric.WithDescription("Total number of Redis timeouts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create timeout counter: %w", err)
	}

	r.reconnectionCounter, err = meter.Int64Counter(
		"redis_reconnections_total",
		metric.WithDescription("Total number of Redis reconnections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create reconnection counter: %w", err)
	}

	// Advanced metrics
	r.slowCommandCounter, err = meter.Int64Counter(
		"redis_slow_commands_total",
		metric.WithDescription("Total number of slow Redis commands"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create slow command counter: %w", err)
	}

	r.commandByType, err = meter.Int64Counter(
		"redis_commands_by_type_total",
		metric.WithDescription("Total Redis commands by type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create commands by type counter: %w", err)
	}

	r.dataTypeUsage, err = meter.Int64Counter(
		"redis_data_type_usage_total",
		metric.WithDescription("Usage of different Redis data types"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create data type usage counter: %w", err)
	}

	r.clientConnectionTime, err = meter.Float64Histogram(
		"redis_client_connection_time_seconds",
		metric.WithDescription("Time to establish Redis client connections"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client connection time histogram: %w", err)
	}

	return r, nil
}

// OperationMetrics holds information about a Redis operation for metrics recording
type OperationMetrics struct {
	StartTime time.Time
	Command   string
	Key       string
	CacheType string
	IsHit     bool
	IsMiss    bool
	ValueSize int64
	IsSlow    bool
	DataType  string
	TTL       time.Duration
}

// RecordOperation records comprehensive metrics for a Redis operation
func (r *RedisMetrics) RecordOperation(ctx context.Context, opMetrics *OperationMetrics, err error) {
	duration := time.Since(opMetrics.StartTime)

	// Base attributes
	baseAttrs := []attribute.KeyValue{
		attribute.String("command", strings.ToUpper(opMetrics.Command)),
		attribute.String("cache_type", opMetrics.CacheType),
	}

	// Add status
	status := "success"
	if err != nil {
		status = getRedisErrorType(err)
		baseAttrs = append(baseAttrs, attribute.String("error_type", status))
	}
	baseAttrs = append(baseAttrs, attribute.String("status", status))

	// Record core metrics
	r.operationCounter.Add(ctx, 1, metric.WithAttributes(baseAttrs...))
	r.operationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(baseAttrs...))
	r.commandLatency.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("command", strings.ToUpper(opMetrics.Command)),
	))

	// Record cache hit/miss
	if opMetrics.IsHit {
		r.cacheHitCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("cache_type", opMetrics.CacheType),
			attribute.String("command", strings.ToUpper(opMetrics.Command)),
		))
		r.keyspaceHits.Add(ctx, 1)
	} else if opMetrics.IsMiss {
		r.cacheMissCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("cache_type", opMetrics.CacheType),
			attribute.String("command", strings.ToUpper(opMetrics.Command)),
		))
		r.keyspaceMisses.Add(ctx, 1)
	}

	// Record slow commands
	if opMetrics.IsSlow || duration > 10*time.Millisecond {
		r.slowCommandCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("command", strings.ToUpper(opMetrics.Command)),
			attribute.String("cache_type", opMetrics.CacheType),
			attribute.String("duration_bucket", getCommandDurationBucket(duration)),
		))
	}

	// Record command by type
	r.commandByType.Add(ctx, 1, metric.WithAttributes(
		attribute.String("command_type", getCommandType(opMetrics.Command)),
		attribute.String("command", strings.ToUpper(opMetrics.Command)),
		attribute.String("status", status),
	))

	// Record data type usage
	if opMetrics.DataType != "" {
		r.dataTypeUsage.Add(ctx, 1, metric.WithAttributes(
			attribute.String("data_type", opMetrics.DataType),
			attribute.String("operation", getDataTypeOperation(opMetrics.Command)),
		))
	}

	// Record errors
	if err != nil {
		errorType := getRedisErrorType(err)
		r.errorCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", errorType),
			attribute.String("command", strings.ToUpper(opMetrics.Command)),
		))

		if isTimeout(err) {
			r.timeoutCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("command", strings.ToUpper(opMetrics.Command)),
				attribute.String("cache_type", opMetrics.CacheType),
			))
		}
	}
}

// RecordPipelineOperation records metrics for Redis pipeline operations
func (r *RedisMetrics) RecordPipelineOperation(ctx context.Context, commandCount int, duration time.Duration, successCount, errorCount int) {
	attrs := []attribute.KeyValue{
		attribute.Int("command_count", commandCount),
		attribute.Int("success_count", successCount),
		attribute.Int("error_count", errorCount),
	}

	r.pipelineCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	r.pipelineDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	r.batchSize.Record(ctx, int64(commandCount), metric.WithAttributes(
		attribute.String("operation_type", "pipeline"),
	))

	if errorCount > 0 {
		r.errorCounter.Add(ctx, int64(errorCount), metric.WithAttributes(
			attribute.String("error_type", "pipeline_error"),
		))
	}
}

// RecordConnectionEvent records Redis connection events
func (r *RedisMetrics) RecordConnectionEvent(ctx context.Context, event string, connectionTime time.Duration) {
	switch event {
	case "connected":
		r.connectionCounter.Add(ctx, 1)
		if connectionTime > 0 {
			r.clientConnectionTime.Record(ctx, connectionTime.Seconds())
		}
	case "disconnected":
		r.connectionCounter.Add(ctx, -1)
	case "reconnected":
		r.reconnectionCounter.Add(ctx, 1)
		if connectionTime > 0 {
			r.clientConnectionTime.Record(ctx, connectionTime.Seconds())
		}
	}
}

// RecordKeyExpiration records key expiration events
func (r *RedisMetrics) RecordKeyExpiration(ctx context.Context, cacheType string, count int64) {
	r.keyExpirationCounter.Add(ctx, count, metric.WithAttributes(
		attribute.String("cache_type", cacheType),
	))
}

// RecordEviction records key eviction events
func (r *RedisMetrics) RecordEviction(ctx context.Context, evictionPolicy string, count int64) {
	r.evictionCounter.Add(ctx, count, metric.WithAttributes(
		attribute.String("eviction_policy", evictionPolicy),
	))
}

// RecordMemoryStats records Redis memory and keyspace statistics
func (r *RedisMetrics) RecordMemoryStats(ctx context.Context, memoryUsed int64, keyCount int64) {
	r.memoryUsage.Add(ctx, memoryUsed, metric.WithAttributes(
		attribute.String("memory_type", "used"),
	))

	r.keyspaceSize.Add(ctx, keyCount, metric.WithAttributes(
		attribute.String("keyspace", "db0"),
	))
}

// RecordCacheHitRatio records cache hit ratio over a time window
func (r *RedisMetrics) RecordCacheHitRatio(ctx context.Context, hitRatio float64, timeWindow string) {
	r.cacheHitRatio.Record(ctx, hitRatio, metric.WithAttributes(
		attribute.String("time_window", timeWindow),
	))
}

// CreateOperationMetrics creates a new OperationMetrics instance
func (r *RedisMetrics) CreateOperationMetrics(command, key string) *OperationMetrics {
	return &OperationMetrics{
		StartTime: time.Now(),
		Command:   command,
		Key:       key,
		CacheType: getCacheTypeFromKey(key),
		DataType:  inferDataTypeFromCommand(command),
	}
}

// Helper functions

func getRedisErrorType(err error) string {
	if err == nil {
		return "success"
	}

	errStr := strings.ToLower(err.Error())
	switch {
	case err == redis.Nil:
		return "key_not_found"
	case strings.Contains(errStr, "timeout"):
		return "timeout"
	case strings.Contains(errStr, "connection"):
		return "connection_error"
	case strings.Contains(errStr, "auth"):
		return "authentication_error"
	case strings.Contains(errStr, "readonly"):
		return "readonly_error"
	case strings.Contains(errStr, "memory"):
		return "out_of_memory"
	case strings.Contains(errStr, "syntax"):
		return "syntax_error"
	case strings.Contains(errStr, "wrong"):
		return "wrong_type"
	default:
		return "redis_error"
	}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func getCommandType(command string) string {
	cmd := strings.ToUpper(command)
	switch {
	case strings.HasPrefix(cmd, "GET") || strings.HasPrefix(cmd, "MGET"):
		return "read"
	case strings.HasPrefix(cmd, "SET") || strings.HasPrefix(cmd, "MSET"):
		return "write"
	case strings.HasPrefix(cmd, "DEL"):
		return "delete"
	case strings.HasPrefix(cmd, "INCR") || strings.HasPrefix(cmd, "DECR"):
		return "increment"
	case strings.HasPrefix(cmd, "H"):
		return "hash"
	case strings.HasPrefix(cmd, "L"):
		return "list"
	case strings.HasPrefix(cmd, "S"):
		return "set"
	case strings.HasPrefix(cmd, "Z"):
		return "sorted_set"
	case cmd == "PING" || cmd == "INFO" || cmd == "CONFIG":
		return "admin"
	default:
		return "other"
	}
}

func inferDataTypeFromCommand(command string) string {
	cmd := strings.ToUpper(command)
	switch {
	case strings.HasPrefix(cmd, "H"):
		return "hash"
	case strings.HasPrefix(cmd, "L"):
		return "list"
	case strings.HasPrefix(cmd, "S") && !strings.HasPrefix(cmd, "SET"):
		return "set"
	case strings.HasPrefix(cmd, "Z"):
		return "sorted_set"
	case strings.HasPrefix(cmd, "GET") || strings.HasPrefix(cmd, "SET"):
		return "string"
	default:
		return "unknown"
	}
}

func getDataTypeOperation(command string) string {
	cmd := strings.ToUpper(command)
	switch {
	case strings.Contains(cmd, "GET") || strings.Contains(cmd, "RANGE"):
		return "read"
	case strings.Contains(cmd, "SET") || strings.Contains(cmd, "ADD") || strings.Contains(cmd, "PUSH"):
		return "write"
	case strings.Contains(cmd, "DEL") || strings.Contains(cmd, "REM") || strings.Contains(cmd, "POP"):
		return "delete"
	default:
		return "other"
	}
}

func getCommandDurationBucket(duration time.Duration) string {
	ms := duration.Milliseconds()
	switch {
	case ms <= 1:
		return "1ms"
	case ms <= 5:
		return "5ms"
	case ms <= 10:
		return "10ms"
	case ms <= 25:
		return "25ms"
	case ms <= 50:
		return "50ms"
	case ms <= 100:
		return "100ms"
	default:
		return "100ms+"
	}
}

// Note: getCacheTypeFromKey function is shared with redis.go

// GetRedisMetrics returns a configured Redis metrics instance
func GetRedisMetrics() *RedisMetrics {
	service := GetService()
	if service == nil {
		return nil
	}

	redisMetrics, _ := NewRedisMetrics(service.GetTracer(), service.GetMeter())
	return redisMetrics
}
