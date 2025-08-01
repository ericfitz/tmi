package telemetry

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// RedisTracing provides Redis operation tracing
type RedisTracing struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics instruments
	operationCounter  metric.Int64Counter
	operationDuration metric.Float64Histogram
	cacheHitCounter   metric.Int64Counter
	cacheMissCounter  metric.Int64Counter
	memoryUsageGauge  metric.Int64ObservableGauge
	connectionCounter metric.Int64UpDownCounter
	keySpaceCounter   metric.Int64Counter
}

// NewRedisTracing creates a new Redis tracing instance
func NewRedisTracing(tracer trace.Tracer, meter metric.Meter) (*RedisTracing, error) {
	r := &RedisTracing{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Create metrics instruments
	r.operationCounter, err = meter.Int64Counter(
		"redis_operations_total",
		metric.WithDescription("Total number of Redis operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create operation counter: %w", err)
	}

	r.operationDuration, err = meter.Float64Histogram(
		"redis_operation_duration_seconds",
		metric.WithDescription("Duration of Redis operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create operation duration histogram: %w", err)
	}

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

	r.memoryUsageGauge, err = meter.Int64ObservableGauge(
		"redis_memory_usage_bytes",
		metric.WithDescription("Redis memory usage in bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory usage gauge: %w", err)
	}

	r.connectionCounter, err = meter.Int64UpDownCounter(
		"redis_connections_active",
		metric.WithDescription("Number of active Redis connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection counter: %w", err)
	}

	r.keySpaceCounter, err = meter.Int64Counter(
		"redis_keyspace_operations_total",
		metric.WithDescription("Total number of keyspace operations by type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create keyspace counter: %w", err)
	}

	return r, nil
}

// WrapClient wraps a Redis client with OpenTelemetry instrumentation
func (r *RedisTracing) WrapClient(client redis.UniversalClient) error {
	// Add OpenTelemetry tracing instrumentation
	if err := redisotel.InstrumentTracing(client); err != nil {
		return fmt.Errorf("failed to instrument Redis tracing: %w", err)
	}

	// Add OpenTelemetry metrics instrumentation
	if err := redisotel.InstrumentMetrics(client); err != nil {
		return fmt.Errorf("failed to instrument Redis metrics: %w", err)
	}

	return nil
}

// WrapClusterClient wraps a Redis cluster client with OpenTelemetry instrumentation
func (r *RedisTracing) WrapClusterClient(client redis.UniversalClient) error {
	// Add OpenTelemetry tracing instrumentation
	if err := redisotel.InstrumentTracing(client); err != nil {
		return fmt.Errorf("failed to instrument Redis cluster tracing: %w", err)
	}

	// Add OpenTelemetry metrics instrumentation
	if err := redisotel.InstrumentMetrics(client); err != nil {
		return fmt.Errorf("failed to instrument Redis cluster metrics: %w", err)
	}

	return nil
}

// Note: Custom metrics hooks removed - using official redisotel instrumentation

// InstrumentedGet performs a GET operation with additional tracing context
func (r *RedisTracing) InstrumentedGet(ctx context.Context, client redis.UniversalClient, key string) (string, error) {
	ctx, span := r.tracer.Start(ctx, "redis.get",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	// Add span attributes
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.operation", "GET"),
		attribute.String("db.redis.key", sanitizeKey(key)),
		attribute.String("tmi.cache.type", getCacheTypeFromKey(key)),
	)

	startTime := time.Now()
	result, err := client.Get(ctx, key).Result()
	duration := time.Since(startTime)

	// Update span with results
	switch err {
	case nil:
		span.SetStatus(codes.Ok, "")
		span.SetAttributes(
			attribute.String("db.redis.hit_status", "hit"),
			attribute.Int("db.redis.value_size", len(result)),
		)
	case redis.Nil:
		span.SetStatus(codes.Ok, "key not found")
		span.SetAttributes(attribute.String("db.redis.hit_status", "miss"))
	default:
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	span.SetAttributes(
		attribute.Float64("db.duration_ms", float64(duration.Nanoseconds())/1e6),
	)

	return result, err
}

// InstrumentedSet performs a SET operation with additional tracing context
func (r *RedisTracing) InstrumentedSet(ctx context.Context, client redis.UniversalClient, key string, value interface{}, expiration time.Duration) error {
	ctx, span := r.tracer.Start(ctx, "redis.set",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	// Add span attributes
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.operation", "SET"),
		attribute.String("db.redis.key", sanitizeKey(key)),
		attribute.String("tmi.cache.type", getCacheTypeFromKey(key)),
	)

	if expiration > 0 {
		span.SetAttributes(attribute.Int64("db.redis.expiration_seconds", int64(expiration.Seconds())))
	}

	startTime := time.Now()
	err := client.Set(ctx, key, value, expiration).Err()
	duration := time.Since(startTime)

	// Update span with results
	if err == nil {
		span.SetStatus(codes.Ok, "")
	} else {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	span.SetAttributes(
		attribute.Float64("db.duration_ms", float64(duration.Nanoseconds())/1e6),
	)

	return err
}

// RecordMemoryStats records Redis memory and connection statistics
func (r *RedisTracing) RecordMemoryStats(ctx context.Context, client redis.UniversalClient) error {
	// Get Redis INFO
	info, err := client.Info(ctx, "memory", "stats").Result()
	if err != nil {
		return fmt.Errorf("failed to get Redis info: %w", err)
	}

	// Parse memory usage
	memoryUsage := parseMemoryUsage(info)
	if memoryUsage > 0 {
		// Note: Observable gauges need to be registered with a callback
		// This is a simplified approach - in practice you'd register the callback once
		_, _ = r.meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
			o.ObserveInt64(r.memoryUsageGauge, memoryUsage)
			return nil
		}, r.memoryUsageGauge)
	}

	// Record connection stats
	poolStats := client.PoolStats()
	r.connectionCounter.Add(ctx, int64(poolStats.TotalConns), metric.WithAttributes(
		attribute.String("state", "total"),
	))

	return nil
}

// Helper functions

func isReadOperation(operation string) bool {
	readOps := map[string]bool{
		"GET": true, "MGET": true, "HGET": true, "HGETALL": true, "HMGET": true,
		"LGET": true, "LINDEX": true, "LRANGE": true,
		"SGET": true, "SMEMBERS": true, "SISMEMBER": true,
		"ZGET": true, "ZRANGE": true, "ZRANGEBYSCORE": true,
		"EXISTS": true, "TTL": true, "PTTL": true,
	}
	return readOps[operation]
}

func isEmptyResult(cmd redis.Cmder) bool {
	switch cmd := cmd.(type) {
	case *redis.StringCmd:
		return cmd.Val() == ""
	case *redis.SliceCmd:
		return len(cmd.Val()) == 0
	case *redis.StringSliceCmd:
		return len(cmd.Val()) == 0
	case *redis.IntCmd:
		return cmd.Val() == 0
	default:
		return false
	}
}

func isKeyspaceOperation(operation string) bool {
	keyspaceOps := map[string]bool{
		"DEL": true, "EXPIRE": true, "EXPIREAT": true, "PERSIST": true,
		"RENAME": true, "RENAMENX": true, "FLUSHDB": true, "FLUSHALL": true,
	}
	return keyspaceOps[operation]
}

func getCacheType(args []interface{}) string {
	if len(args) == 0 {
		return ""
	}

	key, ok := args[0].(string)
	if !ok {
		return ""
	}

	return getCacheTypeFromKey(key)
}

func getCacheTypeFromKey(key string) string {
	// Extract cache type from key pattern
	if strings.HasPrefix(key, "cache:threat_model:") {
		return "threat_model"
	}
	if strings.HasPrefix(key, "cache:diagram:") {
		return "diagram"
	}
	if strings.HasPrefix(key, "cache:threat:") {
		return "threat"
	}
	if strings.HasPrefix(key, "cache:document:") {
		return "document"
	}
	if strings.HasPrefix(key, "cache:source:") {
		return "source"
	}
	if strings.HasPrefix(key, "cache:metadata:") {
		return "metadata"
	}
	if strings.HasPrefix(key, "cache:auth:") {
		return "authorization"
	}
	if strings.HasPrefix(key, "cache:list:") {
		return "list"
	}
	if strings.HasPrefix(key, "session:") {
		return "session"
	}
	if strings.HasPrefix(key, "auth:") {
		return "auth"
	}
	return "unknown"
}

func sanitizeKey(key string) string {
	// Remove sensitive information from keys
	if strings.Contains(key, "token") || strings.Contains(key, "session") {
		parts := strings.Split(key, ":")
		if len(parts) > 2 {
			// Keep prefix and suffix, redact middle parts
			return fmt.Sprintf("%s:[REDACTED]:%s", parts[0], parts[len(parts)-1])
		}
	}
	return key
}

func parseMemoryUsage(info string) int64 {
	lines := strings.Split(info, "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "used_memory:") {
			memStr := strings.TrimPrefix(line, "used_memory:")
			if mem, err := strconv.ParseInt(memStr, 10, 64); err == nil {
				return mem
			}
		}
	}
	return 0
}
