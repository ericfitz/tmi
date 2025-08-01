package telemetry

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// SystemMetrics provides system-level performance and resource metrics
type SystemMetrics struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Go runtime metrics
	goGoroutines    metric.Int64UpDownCounter
	goThreads       metric.Int64UpDownCounter
	goGCDuration    metric.Float64Histogram
	goGCCount       metric.Int64Counter
	goMemAlloc      metric.Int64UpDownCounter
	goMemSys        metric.Int64UpDownCounter
	goMemHeapAlloc  metric.Int64UpDownCounter
	goMemHeapSys    metric.Int64UpDownCounter
	goMemHeapIdle   metric.Int64UpDownCounter
	goMemHeapInuse  metric.Int64UpDownCounter
	goMemStackInuse metric.Int64UpDownCounter
	goMemStackSys   metric.Int64UpDownCounter

	// Process metrics
	processCPUTime         metric.Float64Counter
	processMemoryRSS       metric.Int64UpDownCounter
	processMemoryVMS       metric.Int64UpDownCounter
	processStartTime       metric.Int64UpDownCounter
	processUptime          metric.Float64UpDownCounter
	processFileDescriptors metric.Int64UpDownCounter

	// System performance metrics
	cpuUtilization     metric.Float64Histogram
	memoryUtilization  metric.Float64Histogram
	diskIOOperations   metric.Int64Counter
	diskIOBytes        metric.Int64Counter
	networkConnections metric.Int64UpDownCounter
	networkIOBytes     metric.Int64Counter

	// Application-specific metrics
	requestQueueSize    metric.Int64UpDownCounter
	connectionPoolUsage metric.Float64Histogram
	cacheMemoryUsage    metric.Int64UpDownCounter
	backgroundTasks     metric.Int64UpDownCounter
	scheduledJobs       metric.Int64Counter

	// Error and health metrics
	systemErrors       metric.Int64Counter
	memoryPressure     metric.Int64Counter
	resourceExhaustion metric.Int64Counter
	healthCheckResults metric.Int64Counter

	// Performance indicators
	responseTimeP99     metric.Float64Histogram
	throughputPerSecond metric.Float64Histogram
	errorRate           metric.Float64Histogram
	availability        metric.Float64Histogram
}

// NewSystemMetrics creates a new system metrics instance
func NewSystemMetrics(tracer trace.Tracer, meter metric.Meter) (*SystemMetrics, error) {
	s := &SystemMetrics{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Go runtime metrics
	s.goGoroutines, err = meter.Int64UpDownCounter(
		"go_goroutines",
		metric.WithDescription("Number of goroutines"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create goroutines counter: %w", err)
	}

	s.goThreads, err = meter.Int64UpDownCounter(
		"go_threads",
		metric.WithDescription("Number of OS threads"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create threads counter: %w", err)
	}

	s.goGCDuration, err = meter.Float64Histogram(
		"go_gc_duration_seconds",
		metric.WithDescription("Time spent in garbage collection"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GC duration histogram: %w", err)
	}

	s.goGCCount, err = meter.Int64Counter(
		"go_gc_cycles_total",
		metric.WithDescription("Total number of GC cycles"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GC count counter: %w", err)
	}

	s.goMemAlloc, err = meter.Int64UpDownCounter(
		"go_memstats_alloc_bytes",
		metric.WithDescription("Bytes allocated and in use"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create mem alloc counter: %w", err)
	}

	s.goMemSys, err = meter.Int64UpDownCounter(
		"go_memstats_sys_bytes",
		metric.WithDescription("Bytes obtained from system"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create mem sys counter: %w", err)
	}

	s.goMemHeapAlloc, err = meter.Int64UpDownCounter(
		"go_memstats_heap_alloc_bytes",
		metric.WithDescription("Bytes allocated to heap objects"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create heap alloc counter: %w", err)
	}

	s.goMemHeapSys, err = meter.Int64UpDownCounter(
		"go_memstats_heap_sys_bytes",
		metric.WithDescription("Bytes obtained from system for heap"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create heap sys counter: %w", err)
	}

	s.goMemHeapIdle, err = meter.Int64UpDownCounter(
		"go_memstats_heap_idle_bytes",
		metric.WithDescription("Bytes in idle heap spans"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create heap idle counter: %w", err)
	}

	s.goMemHeapInuse, err = meter.Int64UpDownCounter(
		"go_memstats_heap_inuse_bytes",
		metric.WithDescription("Bytes in in-use heap spans"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create heap inuse counter: %w", err)
	}

	s.goMemStackInuse, err = meter.Int64UpDownCounter(
		"go_memstats_stack_inuse_bytes",
		metric.WithDescription("Bytes used by stack spans"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create stack inuse counter: %w", err)
	}

	s.goMemStackSys, err = meter.Int64UpDownCounter(
		"go_memstats_stack_sys_bytes",
		metric.WithDescription("Bytes obtained from system for stacks"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create stack sys counter: %w", err)
	}

	// Process metrics
	s.processCPUTime, err = meter.Float64Counter(
		"process_cpu_seconds_total",
		metric.WithDescription("Total user and system CPU time"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process CPU time counter: %w", err)
	}

	s.processMemoryRSS, err = meter.Int64UpDownCounter(
		"process_resident_memory_bytes",
		metric.WithDescription("Resident memory size in bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process memory RSS counter: %w", err)
	}

	s.processMemoryVMS, err = meter.Int64UpDownCounter(
		"process_virtual_memory_bytes",
		metric.WithDescription("Virtual memory size in bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process memory VMS counter: %w", err)
	}

	s.processStartTime, err = meter.Int64UpDownCounter(
		"process_start_time_seconds",
		metric.WithDescription("Start time of the process since unix epoch"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process start time counter: %w", err)
	}

	s.processUptime, err = meter.Float64UpDownCounter(
		"process_uptime_seconds",
		metric.WithDescription("Process uptime in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process uptime counter: %w", err)
	}

	s.processFileDescriptors, err = meter.Int64UpDownCounter(
		"process_open_fds",
		metric.WithDescription("Number of open file descriptors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create process file descriptors counter: %w", err)
	}

	// System performance metrics
	s.cpuUtilization, err = meter.Float64Histogram(
		"system_cpu_utilization_ratio",
		metric.WithDescription("System CPU utilization ratio"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CPU utilization histogram: %w", err)
	}

	s.memoryUtilization, err = meter.Float64Histogram(
		"system_memory_utilization_ratio",
		metric.WithDescription("System memory utilization ratio"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory utilization histogram: %w", err)
	}

	s.diskIOOperations, err = meter.Int64Counter(
		"system_disk_io_operations_total",
		metric.WithDescription("Total disk I/O operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk I/O operations counter: %w", err)
	}

	s.diskIOBytes, err = meter.Int64Counter(
		"system_disk_io_bytes_total",
		metric.WithDescription("Total disk I/O bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk I/O bytes counter: %w", err)
	}

	s.networkConnections, err = meter.Int64UpDownCounter(
		"system_network_connections_active",
		metric.WithDescription("Number of active network connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create network connections counter: %w", err)
	}

	s.networkIOBytes, err = meter.Int64Counter(
		"system_network_io_bytes_total",
		metric.WithDescription("Total network I/O bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create network I/O bytes counter: %w", err)
	}

	// Application-specific metrics
	s.requestQueueSize, err = meter.Int64UpDownCounter(
		"application_request_queue_size",
		metric.WithDescription("Size of request processing queue"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request queue size counter: %w", err)
	}

	s.connectionPoolUsage, err = meter.Float64Histogram(
		"application_connection_pool_usage_ratio",
		metric.WithDescription("Connection pool usage ratio"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool usage histogram: %w", err)
	}

	s.cacheMemoryUsage, err = meter.Int64UpDownCounter(
		"application_cache_memory_bytes",
		metric.WithDescription("Memory used by application caches"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache memory usage counter: %w", err)
	}

	s.backgroundTasks, err = meter.Int64UpDownCounter(
		"application_background_tasks_active",
		metric.WithDescription("Number of active background tasks"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create background tasks counter: %w", err)
	}

	s.scheduledJobs, err = meter.Int64Counter(
		"application_scheduled_jobs_total",
		metric.WithDescription("Total scheduled jobs executed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduled jobs counter: %w", err)
	}

	// Error and health metrics
	s.systemErrors, err = meter.Int64Counter(
		"system_errors_total",
		metric.WithDescription("Total system-level errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create system errors counter: %w", err)
	}

	s.memoryPressure, err = meter.Int64Counter(
		"system_memory_pressure_events_total",
		metric.WithDescription("Total memory pressure events"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory pressure counter: %w", err)
	}

	s.resourceExhaustion, err = meter.Int64Counter(
		"system_resource_exhaustion_events_total",
		metric.WithDescription("Total resource exhaustion events"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource exhaustion counter: %w", err)
	}

	s.healthCheckResults, err = meter.Int64Counter(
		"system_health_check_results_total",
		metric.WithDescription("Total health check results"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create health check results counter: %w", err)
	}

	// Performance indicators
	s.responseTimeP99, err = meter.Float64Histogram(
		"application_response_time_p99_seconds",
		metric.WithDescription("99th percentile response time"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create response time P99 histogram: %w", err)
	}

	s.throughputPerSecond, err = meter.Float64Histogram(
		"application_throughput_per_second",
		metric.WithDescription("Application throughput per second"),
		metric.WithUnit("1/s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create throughput histogram: %w", err)
	}

	s.errorRate, err = meter.Float64Histogram(
		"application_error_rate_ratio",
		metric.WithDescription("Application error rate ratio"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.3, 0.4, 0.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create error rate histogram: %w", err)
	}

	s.availability, err = meter.Float64Histogram(
		"application_availability_ratio",
		metric.WithDescription("Application availability ratio"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.90, 0.95, 0.99, 0.995, 0.999, 0.9995, 0.9999, 1.0),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create availability histogram: %w", err)
	}

	return s, nil
}

// CollectRuntimeMetrics collects Go runtime metrics
func (s *SystemMetrics) CollectRuntimeMetrics(ctx context.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Go runtime metrics
	s.goGoroutines.Add(ctx, int64(runtime.NumGoroutine()))

	// Memory metrics
	s.goMemAlloc.Add(ctx, safeUint64ToInt64(m.Alloc))
	s.goMemSys.Add(ctx, safeUint64ToInt64(m.Sys))
	s.goMemHeapAlloc.Add(ctx, safeUint64ToInt64(m.HeapAlloc))
	s.goMemHeapSys.Add(ctx, safeUint64ToInt64(m.HeapSys))
	s.goMemHeapIdle.Add(ctx, safeUint64ToInt64(m.HeapIdle))
	s.goMemHeapInuse.Add(ctx, safeUint64ToInt64(m.HeapInuse))
	s.goMemStackInuse.Add(ctx, safeUint64ToInt64(m.StackInuse))
	s.goMemStackSys.Add(ctx, safeUint64ToInt64(m.StackSys))

	// GC metrics
	s.goGCCount.Add(ctx, int64(m.NumGC))

	// Check for memory pressure
	if m.HeapInuse > m.HeapSys*9/10 { // 90% heap utilization
		s.memoryPressure.Add(ctx, 1, metric.WithAttributes(
			attribute.String("pressure_type", "heap_pressure"),
		))
	}
}

// CollectGCMetrics collects garbage collection metrics
func (s *SystemMetrics) CollectGCMetrics(ctx context.Context, gcDuration time.Duration) {
	s.goGCDuration.Record(ctx, gcDuration.Seconds(), metric.WithAttributes(
		attribute.String("gc_type", "mark_and_sweep"),
	))
}

// RecordProcessMetrics records process-level metrics
func (s *SystemMetrics) RecordProcessMetrics(ctx context.Context, cpuTime time.Duration, rssBytes, vmsBytes int64, openFDs int) {
	s.processCPUTime.Add(ctx, cpuTime.Seconds())
	s.processMemoryRSS.Add(ctx, rssBytes)
	s.processMemoryVMS.Add(ctx, vmsBytes)
	s.processFileDescriptors.Add(ctx, int64(openFDs))
}

// RecordSystemUtilization records system resource utilization
func (s *SystemMetrics) RecordSystemUtilization(ctx context.Context, cpuRatio, memoryRatio float64) {
	s.cpuUtilization.Record(ctx, cpuRatio, metric.WithAttributes(
		attribute.String("utilization_level", getSystemUtilizationLevel(cpuRatio)),
	))

	s.memoryUtilization.Record(ctx, memoryRatio, metric.WithAttributes(
		attribute.String("utilization_level", getSystemUtilizationLevel(memoryRatio)),
	))

	// Record high utilization events
	if cpuRatio > 0.9 {
		s.resourceExhaustion.Add(ctx, 1, metric.WithAttributes(
			attribute.String("resource_type", "cpu"),
			attribute.String("severity", "critical"),
		))
	}

	if memoryRatio > 0.9 {
		s.resourceExhaustion.Add(ctx, 1, metric.WithAttributes(
			attribute.String("resource_type", "memory"),
			attribute.String("severity", "critical"),
		))
	}
}

// RecordDiskIO records disk I/O metrics
func (s *SystemMetrics) RecordDiskIO(ctx context.Context, readOps, writeOps int64, readBytes, writeBytes int64) {
	s.diskIOOperations.Add(ctx, readOps, metric.WithAttributes(
		attribute.String("operation", "read"),
	))
	s.diskIOOperations.Add(ctx, writeOps, metric.WithAttributes(
		attribute.String("operation", "write"),
	))

	s.diskIOBytes.Add(ctx, readBytes, metric.WithAttributes(
		attribute.String("operation", "read"),
	))
	s.diskIOBytes.Add(ctx, writeBytes, metric.WithAttributes(
		attribute.String("operation", "write"),
	))
}

// RecordNetworkIO records network I/O metrics
func (s *SystemMetrics) RecordNetworkIO(ctx context.Context, activeConnections int, rxBytes, txBytes int64) {
	s.networkConnections.Add(ctx, int64(activeConnections))
	s.networkIOBytes.Add(ctx, rxBytes, metric.WithAttributes(
		attribute.String("direction", "rx"),
	))
	s.networkIOBytes.Add(ctx, txBytes, metric.WithAttributes(
		attribute.String("direction", "tx"),
	))
}

// RecordApplicationMetrics records application-specific metrics
func (s *SystemMetrics) RecordApplicationMetrics(ctx context.Context, queueSize int, poolUsageRatio float64, cacheMemory int64, backgroundTasks int) {
	s.requestQueueSize.Add(ctx, int64(queueSize))
	s.connectionPoolUsage.Record(ctx, poolUsageRatio, metric.WithAttributes(
		attribute.String("pool_type", "database"),
	))
	s.cacheMemoryUsage.Add(ctx, cacheMemory)
	s.backgroundTasks.Add(ctx, int64(backgroundTasks))
}

// RecordScheduledJob records scheduled job execution
func (s *SystemMetrics) RecordScheduledJob(ctx context.Context, jobType string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failure"
	}

	s.scheduledJobs.Add(ctx, 1, metric.WithAttributes(
		attribute.String("job_type", jobType),
		attribute.String("status", status),
	))

	if !success {
		s.systemErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "scheduled_job_failure"),
			attribute.String("job_type", jobType),
		))
	}
}

// RecordHealthCheck records health check results
func (s *SystemMetrics) RecordHealthCheck(ctx context.Context, component string, healthy bool, responseTime time.Duration) {
	status := "healthy"
	if !healthy {
		status = "unhealthy"
	}

	s.healthCheckResults.Add(ctx, 1, metric.WithAttributes(
		attribute.String("component", component),
		attribute.String("status", status),
	))

	if !healthy {
		s.systemErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "health_check_failure"),
			attribute.String("component", component),
		))
	}
}

// RecordPerformanceIndicators records high-level performance indicators
func (s *SystemMetrics) RecordPerformanceIndicators(ctx context.Context, responseTimeP99 time.Duration, throughput float64, errorRate float64, availability float64) {
	s.responseTimeP99.Record(ctx, responseTimeP99.Seconds(), metric.WithAttributes(
		attribute.String("performance_tier", getPerformanceTier(responseTimeP99)),
	))

	s.throughputPerSecond.Record(ctx, throughput, metric.WithAttributes(
		attribute.String("throughput_level", getThroughputLevel(throughput)),
	))

	s.errorRate.Record(ctx, errorRate, metric.WithAttributes(
		attribute.String("error_level", getErrorLevel(errorRate)),
	))

	s.availability.Record(ctx, availability, metric.WithAttributes(
		attribute.String("availability_tier", getAvailabilityTier(availability)),
	))
}

// RecordSystemError records system-level errors
func (s *SystemMetrics) RecordSystemError(ctx context.Context, errorType, component string, severity string) {
	s.systemErrors.Add(ctx, 1, metric.WithAttributes(
		attribute.String("error_type", errorType),
		attribute.String("component", component),
		attribute.String("severity", severity),
	))
}

// StartProcessMetricsCollection starts periodic collection of process and system metrics
func (s *SystemMetrics) StartProcessMetricsCollection(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	startTime := time.Now()

	// Record process start time
	s.processStartTime.Add(ctx, startTime.Unix())

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CollectRuntimeMetrics(ctx)

				// Record uptime
				uptime := time.Since(startTime)
				s.processUptime.Add(ctx, uptime.Seconds())

				// Check for resource exhaustion
				var m runtime.MemStats
				runtime.ReadMemStats(&m)

				// Check if GC is running too frequently (more than 10% of time)
				if m.GCCPUFraction > 0.1 {
					s.resourceExhaustion.Add(ctx, 1, metric.WithAttributes(
						attribute.String("resource_type", "gc_pressure"),
						attribute.String("severity", "warning"),
					))
				}
			}
		}
	}()
}

// Helper functions

func getSystemUtilizationLevel(ratio float64) string {
	switch {
	case ratio < 0.3:
		return "low"
	case ratio < 0.7:
		return "medium"
	case ratio < 0.9:
		return "high"
	default:
		return "critical"
	}
}

func getPerformanceTier(responseTime time.Duration) string {
	ms := responseTime.Milliseconds()
	switch {
	case ms < 100:
		return "excellent"
	case ms < 500:
		return "good"
	case ms < 1000:
		return "acceptable"
	case ms < 5000:
		return "slow"
	default:
		return "critical"
	}
}

func getThroughputLevel(throughput float64) string {
	switch {
	case throughput < 10:
		return "low"
	case throughput < 100:
		return "medium"
	case throughput < 1000:
		return "high"
	default:
		return "very_high"
	}
}

func getErrorLevel(errorRate float64) string {
	switch {
	case errorRate < 0.001:
		return "excellent"
	case errorRate < 0.01:
		return "good"
	case errorRate < 0.05:
		return "acceptable"
	case errorRate < 0.1:
		return "concerning"
	default:
		return "critical"
	}
}

func getAvailabilityTier(availability float64) string {
	switch {
	case availability >= 0.999:
		return "three_nines"
	case availability >= 0.99:
		return "two_nines"
	case availability >= 0.95:
		return "one_nine"
	default:
		return "below_sla"
	}
}

// GetBuildInfo returns build information for metrics
func GetBuildInfo() map[string]string {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return map[string]string{
			"version":    "unknown",
			"go_version": runtime.Version(),
		}
	}

	info := map[string]string{
		"version":    buildInfo.Main.Version,
		"go_version": runtime.Version(),
		"path":       buildInfo.Path,
	}

	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			info["git_commit"] = setting.Value
		case "vcs.time":
			info["build_time"] = setting.Value
		case "vcs.modified":
			info["git_dirty"] = setting.Value
		}
	}

	return info
}

// GetSystemMetrics returns a configured system metrics instance
func GetSystemMetrics() *SystemMetrics {
	service := GetService()
	if service == nil {
		return nil
	}

	systemMetrics, _ := NewSystemMetrics(service.GetTracer(), service.GetMeter())
	return systemMetrics
}
