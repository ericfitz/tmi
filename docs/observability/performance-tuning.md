# Performance Tuning Guide

This guide provides comprehensive performance tuning strategies for the TMI OpenTelemetry implementation.

## Table of Contents

1. [Overview](#overview)
2. [Performance Monitoring](#performance-monitoring)
3. [Sampling Optimization](#sampling-optimization)
4. [Batch Processing Tuning](#batch-processing-tuning)
5. [Memory Management](#memory-management)
6. [Network Optimization](#network-optimization)
7. [Database Performance](#database-performance)
8. [Cache Optimization](#cache-optimization)
9. [Goroutine Management](#goroutine-management)
10. [Production Tuning](#production-tuning)
11. [Automated Optimization](#automated-optimization)

## Overview

Performance tuning for OpenTelemetry involves optimizing multiple layers:

- Telemetry data collection and processing
- Network communication and serialization
- Memory usage and garbage collection
- CPU utilization and concurrency
- I/O operations and caching

The goal is to maintain comprehensive observability while minimizing performance impact (typically < 5% overhead).

## Performance Monitoring

### Key Performance Indicators

1. **Telemetry Overhead**

   ```promql
   # CPU overhead from telemetry
   rate(process_cpu_seconds_total{job="tmi-api"}[5m]) * 100

   # Memory overhead
   (go_memstats_heap_inuse_bytes / go_memstats_sys_bytes) * 100

   # Goroutine overhead
   go_goroutines
   ```

2. **Telemetry Pipeline Performance**

   ```promql
   # Export latency
   otel_exporter_send_duration_seconds

   # Queue utilization
   otel_processor_queue_size / otel_processor_queue_capacity * 100

   # Drop rate
   rate(otel_processor_dropped_spans_total[5m])
   ```

3. **Application Performance Impact**

   ```promql
   # Request latency with/without telemetry
   histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))

   # Throughput impact
   rate(http_requests_total[5m])
   ```

### Performance Baseline

Establish baseline metrics before optimization:

```bash
# Run performance test without telemetry
OTEL_TRACING_ENABLED=false go run cmd/server/main.go &
./scripts/load-test.sh --duration=300s --output=baseline.json

# Run performance test with full telemetry
OTEL_TRACING_ENABLED=true OTEL_TRACING_SAMPLE_RATE=1.0 go run cmd/server/main.go &
./scripts/load-test.sh --duration=300s --output=full-telemetry.json

# Compare results
./scripts/compare-performance.sh baseline.json full-telemetry.json
```

## Sampling Optimization

### Adaptive Sampling

Configure sampling rates based on environment and traffic patterns:

```go
// Development: Sample everything for debugging
OTEL_TRACING_SAMPLE_RATE=1.0

// Staging: Moderate sampling for testing
OTEL_TRACING_SAMPLE_RATE=0.5

// Production: Conservative sampling
OTEL_TRACING_SAMPLE_RATE=0.1

// High-traffic production: Very conservative
OTEL_TRACING_SAMPLE_RATE=0.01
```

### Intelligent Sampling Configuration

```yaml
# Advanced sampling configuration
sampling:
  default_strategy:
    type: probabilistic
    param: 0.1

  per_service_strategies:
    - service: "tmi-api"
      type: adaptive
      max_traces_per_second: 100
      per_operation_strategies:
        - operation: "GET /"
          type: probabilistic
          param: 0.01
        - operation: "POST /api/v1/threat-models"
          type: probabilistic
          param: 1.0
```

### Dynamic Sampling

Implement dynamic sampling based on system load:

```go
func (s *DynamicSampler) ShouldSample(ctx context.Context) bool {
    cpuUsage := s.resourceMonitor.GetCPUUsage()
    memoryUsage := s.resourceMonitor.GetMemoryUsage()

    // Reduce sampling under high load
    if cpuUsage > 80 || memoryUsage > 80 {
        return rand.Float64() < 0.05 // 5% sampling
    } else if cpuUsage > 60 || memoryUsage > 60 {
        return rand.Float64() < 0.1  // 10% sampling
    }

    return rand.Float64() < 0.2 // 20% sampling
}
```

## Batch Processing Tuning

### Batch Size Optimization

Configure batch sizes based on throughput and latency requirements:

```bash
# High throughput, can tolerate higher latency
OTEL_TRACING_BATCH_SIZE=2048
OTEL_METRICS_BATCH_SIZE=4096

# Low latency requirements
OTEL_TRACING_BATCH_SIZE=256
OTEL_METRICS_BATCH_SIZE=512

# Memory constrained environments
OTEL_TRACING_BATCH_SIZE=128
OTEL_METRICS_BATCH_SIZE=256
```

### Batch Timeout Configuration

Balance between latency and efficiency:

```bash
# Real-time requirements (higher CPU cost)
OTEL_TRACING_BATCH_TIMEOUT=1s
OTEL_METRICS_BATCH_TIMEOUT=5s

# Balanced (recommended)
OTEL_TRACING_BATCH_TIMEOUT=5s
OTEL_METRICS_BATCH_TIMEOUT=10s

# Efficiency optimized (higher latency)
OTEL_TRACING_BATCH_TIMEOUT=30s
OTEL_METRICS_BATCH_TIMEOUT=60s
```

### Queue Size Tuning

Prevent data loss while managing memory:

```bash
# Calculate queue size based on:
# Queue Size = (Peak RPS * Batch Timeout) * Safety Factor

# For 1000 RPS with 5s timeout:
OTEL_TRACING_QUEUE_SIZE=10000  # 1000 * 5 * 2

# Memory usage = Queue Size * Average Span Size
# Approximate span size: 1-5KB
# Memory per queue: ~50MB for 10,000 spans
```

## Memory Management

### Go Runtime Tuning

Optimize Go runtime for telemetry workloads:

```bash
# Adjust garbage collection frequency
GOGC=100  # Default
GOGC=50   # More frequent GC (lower memory usage)
GOGC=200  # Less frequent GC (higher memory usage, better CPU)

# Set memory limit (Go 1.19+)
GOMEMLIMIT=512MiB

# Control max processors
GOMAXPROCS=4
```

### Memory Pool Management

Implement object pooling for frequently allocated objects:

```go
var spanPool = sync.Pool{
    New: func() interface{} {
        return &SpanData{}
    },
}

func GetSpan() *SpanData {
    return spanPool.Get().(*SpanData)
}

func PutSpan(span *SpanData) {
    span.Reset()
    spanPool.Put(span)
}
```

### Memory Profiling

Use Go's built-in profiling tools:

```bash
# Enable profiling endpoint
go run -tags pprof cmd/server/main.go

# Collect memory profile
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Analyze allocations
curl http://localhost:6060/debug/pprof/allocs > allocs.prof
go tool pprof allocs.prof
```

## Network Optimization

### Compression Configuration

Enable compression for network efficiency:

```bash
# OTLP HTTP exporter with compression
OTEL_EXPORTER_OTLP_COMPRESSION=gzip

# Jaeger exporter optimization
JAEGER_REPORTER_MAX_PACKET_SIZE=65000
```

### Connection Pooling

Optimize HTTP client settings:

```go
transport := &http.Transport{
    MaxIdleConns:        100,
    MaxIdleConnsPerHost: 10,
    IdleConnTimeout:     90 * time.Second,
    DisableKeepAlives:   false,
}

client := &http.Client{
    Transport: transport,
    Timeout:   30 * time.Second,
}
```

### Network Monitoring

Monitor network performance:

```promql
# Network latency to exporters
otel_exporter_send_duration_seconds{quantile="0.95"}

# Network errors
rate(otel_exporter_send_failed_total[5m])

# Bandwidth usage
rate(otel_exporter_sent_bytes_total[5m])
```

## Database Performance

### Connection Pool Optimization

Tune database connections for telemetry workloads:

```go
db.SetMaxOpenConns(25)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
db.SetConnMaxIdleTime(10 * time.Minute)
```

### Query Optimization

Optimize database queries used by telemetry:

```sql
-- Create indexes for trace queries
CREATE INDEX idx_traces_timestamp ON traces(timestamp);
CREATE INDEX idx_traces_service ON traces(service_name, timestamp);
CREATE INDEX idx_spans_trace_id ON spans(trace_id);

-- Partition large tables
CREATE TABLE traces_2024 PARTITION OF traces
FOR VALUES FROM ('2024-01-01') TO ('2025-01-01');
```

### Database Monitoring

Monitor database performance impact:

```promql
# Database connection usage
postgres_connections_active / postgres_connections_max * 100

# Query performance
histogram_quantile(0.95, rate(postgres_query_duration_seconds_bucket[5m]))

# Lock contention
postgres_locks_count{mode="exclusive"}
```

## Cache Optimization

### Redis Configuration

Optimize Redis for telemetry caching:

```bash
# Memory optimization
maxmemory 256mb
maxmemory-policy allkeys-lru

# Persistence settings for telemetry cache
save ""  # Disable snapshotting for cache-only usage
appendonly no

# Network optimization
tcp-keepalive 60
tcp-nodelay yes
```

### Cache Strategies

Implement intelligent caching:

```go
// Cache frequently accessed traces
func (c *TraceCache) Get(traceID string) (*Trace, bool) {
    if trace, exists := c.lruCache.Get(traceID); exists {
        c.recordCacheHit()
        return trace.(*Trace), true
    }

    c.recordCacheMiss()
    return nil, false
}

// Cache with TTL for time-sensitive data
func (c *MetricsCache) Set(key string, value interface{}, ttl time.Duration) {
    c.redisClient.Set(ctx, key, value, ttl)
}
```

### Cache Performance Monitoring

```promql
# Cache hit rate
rate(redis_cache_hits_total[5m]) / rate(redis_cache_requests_total[5m]) * 100

# Cache latency
histogram_quantile(0.95, rate(redis_operation_duration_seconds_bucket[5m]))

# Memory usage
redis_memory_used_bytes / redis_memory_max_bytes * 100
```

## Goroutine Management

### Goroutine Pool Implementation

Use worker pools for telemetry processing:

```go
type WorkerPool struct {
    workers    int
    workQueue  chan Work
    workerPool chan chan Work
    quit       chan bool
}

func (p *WorkerPool) Start() {
    for i := 0; i < p.workers; i++ {
        worker := NewWorker(p.workerPool)
        worker.Start()
    }

    go p.dispatch()
}

func (p *WorkerPool) dispatch() {
    for {
        select {
        case work := <-p.workQueue:
            workerChannel := <-p.workerPool
            workerChannel <- work
        case <-p.quit:
            return
        }
    }
}
```

### Goroutine Monitoring

Monitor goroutine usage:

```promql
# Goroutine count
go_goroutines

# Goroutine growth rate
rate(go_goroutines[5m])

# Blocked goroutines
go_sched_goroutines_goroutines{state="blocked"}
```

### Concurrency Limits

Set appropriate concurrency limits:

```go
// Limit concurrent exports
semaphore := make(chan struct{}, 10)

func export(data []byte) error {
    semaphore <- struct{}{}
    defer func() { <-semaphore }()

    return doExport(data)
}
```

## Production Tuning

### Environment-Specific Configurations

#### High-Traffic Production

```bash
# Conservative sampling
OTEL_TRACING_SAMPLE_RATE=0.01
OTEL_METRICS_SAMPLE_RATE=0.1

# Large batches for efficiency
OTEL_TRACING_BATCH_SIZE=4096
OTEL_METRICS_BATCH_SIZE=8192

# Longer timeouts acceptable
OTEL_TRACING_BATCH_TIMEOUT=30s
OTEL_METRICS_BATCH_TIMEOUT=60s

# Performance profile
OTEL_PERFORMANCE_PROFILE=high
```

#### Low-Latency Production

```bash
# Moderate sampling
OTEL_TRACING_SAMPLE_RATE=0.05

# Smaller batches for low latency
OTEL_TRACING_BATCH_SIZE=512
OTEL_METRICS_BATCH_SIZE=1024

# Short timeouts
OTEL_TRACING_BATCH_TIMEOUT=1s
OTEL_METRICS_BATCH_TIMEOUT=5s

# Balanced profile
OTEL_PERFORMANCE_PROFILE=medium
```

#### Resource-Constrained Production

```bash
# Very conservative sampling
OTEL_TRACING_SAMPLE_RATE=0.005

# Small batches
OTEL_TRACING_BATCH_SIZE=128
OTEL_METRICS_BATCH_SIZE=256

# Memory limits
OTEL_MAX_MEMORY_MB=128
GOMEMLIMIT=256MiB

# Low resource profile
OTEL_PERFORMANCE_PROFILE=low
```

### Load Testing

Conduct performance testing under various loads:

```bash
#!/bin/bash
# load-test-suite.sh

# Test different sampling rates
for rate in 0.01 0.05 0.1 0.5 1.0; do
    echo "Testing sampling rate: $rate"
    OTEL_TRACING_SAMPLE_RATE=$rate ./run-load-test.sh
    ./collect-metrics.sh "sampling-$rate"
done

# Test different batch sizes
for size in 128 512 1024 2048 4096; do
    echo "Testing batch size: $size"
    OTEL_TRACING_BATCH_SIZE=$size ./run-load-test.sh
    ./collect-metrics.sh "batch-$size"
done

# Generate performance report
./generate-report.sh
```

## Automated Optimization

### Performance Optimizer Configuration

Enable automated performance optimization:

```go
// Initialize performance optimizer
config := &OptimizerConfig{
    OptimizationInterval:        5 * time.Minute,
    MonitoringInterval:          30 * time.Second,
    MaxCPUUtilization:          80.0,
    MaxMemoryUtilization:       80.0,
    EnableAdaptiveBatching:     true,
    EnableAdaptiveSampling:     true,
    EnableResourceOptimization: true,
}

optimizer, err := NewPerformanceOptimizer(config)
if err != nil {
    log.Fatal(err)
}

// Start optimization
ctx := context.Background()
optimizer.Start(ctx)
```

### Optimization Metrics

Monitor optimization effectiveness:

```promql
# Optimization frequency
rate(telemetry_optimizations_total[1h])

# Performance improvements
telemetry_performance_gain_ratio

# Resource utilization after optimization
avg_over_time(go_memstats_heap_inuse_bytes[1h])
```

### Custom Optimization Rules

Implement custom optimization logic:

```go
func (o *CustomOptimizer) OptimizeForLatency(ctx context.Context) {
    currentLatency := o.getAverageLatency()
    targetLatency := 100 * time.Millisecond

    if currentLatency > targetLatency {
        // Reduce batch sizes
        o.adjustBatchSize(0.8)

        // Increase sampling aggressiveness
        o.adjustSamplingRate(0.7)

        // Prioritize low-latency exporters
        o.switchToFastExporter()
    }
}
```

## Performance Monitoring Dashboard

Create comprehensive performance monitoring:

```json
{
  "dashboard": {
    "title": "OpenTelemetry Performance",
    "panels": [
      {
        "title": "Telemetry Overhead",
        "targets": [
          {
            "expr": "rate(process_cpu_seconds_total{job=\"tmi-api\"}[5m]) * 100",
            "legendFormat": "CPU Usage %"
          },
          {
            "expr": "go_memstats_heap_inuse_bytes / 1024 / 1024",
            "legendFormat": "Heap Memory MB"
          }
        ]
      },
      {
        "title": "Export Performance",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, rate(otel_exporter_send_duration_seconds_bucket[5m]))",
            "legendFormat": "Export Latency P95"
          },
          {
            "expr": "rate(otel_processor_dropped_spans_total[5m])",
            "legendFormat": "Dropped Spans/sec"
          }
        ]
      }
    ]
  }
}
```

## Best Practices Summary

1. **Start Conservative**: Begin with low sampling rates and small batch sizes
2. **Monitor Continuously**: Track performance impact in real-time
3. **Test Thoroughly**: Validate changes in staging before production
4. **Profile Regularly**: Use Go profiling tools to identify bottlenecks
5. **Optimize Iteratively**: Make incremental improvements
6. **Balance Trade-offs**: Consider latency vs. throughput vs. resource usage
7. **Use Automation**: Implement automated optimization for dynamic workloads
8. **Document Changes**: Keep records of optimization decisions and results

## Troubleshooting Performance Issues

### High Memory Usage

```bash
# Check memory usage
go tool pprof http://localhost:6060/debug/pprof/heap

# Reduce batch sizes
OTEL_TRACING_BATCH_SIZE=256
OTEL_METRICS_BATCH_SIZE=512

# Enable more aggressive GC
GOGC=50
```

### High CPU Usage

```bash
# Check CPU hotspots
go tool pprof http://localhost:6060/debug/pprof/profile

# Reduce sampling rate
OTEL_TRACING_SAMPLE_RATE=0.01

# Optimize serialization
OTEL_EXPORTER_OTLP_COMPRESSION=gzip
```

### High Latency

```bash
# Reduce batch timeout
OTEL_TRACING_BATCH_TIMEOUT=1s

# Use asynchronous processing
OTEL_PROCESSOR_TYPE=batch_async

# Optimize network
OTEL_EXPORTER_OTLP_TIMEOUT=5s
```

This performance tuning guide should be regularly updated based on operational experience and performance testing results.
