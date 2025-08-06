package telemetry

import (
	"context"
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

	mb := newMetricBuilder(meter)

	// Build metrics by category
	s.buildGoRuntimeMetrics(mb)
	s.buildProcessMetrics(mb)
	s.buildSystemPerformanceMetrics(mb)
	s.buildApplicationMetrics(mb)
	s.buildHealthMetrics(mb)
	s.buildPerformanceIndicators(mb)

	return s, mb.Error()
}

// buildGoRuntimeMetrics creates Go runtime related metrics
func (s *SystemMetrics) buildGoRuntimeMetrics(mb *metricBuilder) {
	s.goGoroutines = mb.Int64UpDownCounter(
		"go_goroutines",
		"Number of goroutines",
		"1")
	s.goThreads = mb.Int64UpDownCounter(
		"go_threads",
		"Number of OS threads",
		"1")
	s.goGCDuration = mb.Float64Histogram(
		"go_gc_duration_seconds",
		"Time spent in garbage collection",
		"s",
		[]float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5})
	s.goGCCount = mb.Int64Counter(
		"go_gc_cycles_total",
		"Total number of GC cycles",
		"1")
	s.goMemAlloc = mb.Int64UpDownCounter(
		"go_memstats_alloc_bytes",
		"Bytes allocated and in use",
		"By")
	s.goMemSys = mb.Int64UpDownCounter(
		"go_memstats_sys_bytes",
		"Bytes obtained from system",
		"By")
	s.goMemHeapAlloc = mb.Int64UpDownCounter(
		"go_memstats_heap_alloc_bytes",
		"Bytes allocated to heap objects",
		"By")
	s.goMemHeapSys = mb.Int64UpDownCounter(
		"go_memstats_heap_sys_bytes",
		"Bytes obtained from system for heap",
		"By")
	s.goMemHeapIdle = mb.Int64UpDownCounter(
		"go_memstats_heap_idle_bytes",
		"Bytes in idle heap spans",
		"By")
	s.goMemHeapInuse = mb.Int64UpDownCounter(
		"go_memstats_heap_inuse_bytes",
		"Bytes in in-use heap spans",
		"By")
	s.goMemStackInuse = mb.Int64UpDownCounter(
		"go_memstats_stack_inuse_bytes",
		"Bytes used by stack spans",
		"By")
	s.goMemStackSys = mb.Int64UpDownCounter(
		"go_memstats_stack_sys_bytes",
		"Bytes obtained from system for stacks",
		"By")
}

// buildProcessMetrics creates process related metrics
func (s *SystemMetrics) buildProcessMetrics(mb *metricBuilder) {
	s.processCPUTime = mb.Float64Counter(
		"process_cpu_seconds_total",
		"Total user and system CPU time",
		"s")
	s.processMemoryRSS = mb.Int64UpDownCounter(
		"process_resident_memory_bytes",
		"Resident memory size in bytes",
		"By")
	s.processMemoryVMS = mb.Int64UpDownCounter(
		"process_virtual_memory_bytes",
		"Virtual memory size in bytes",
		"By")
	s.processStartTime = mb.Int64UpDownCounter(
		"process_start_time_seconds",
		"Start time of the process since unix epoch",
		"s")
	s.processUptime = mb.Float64UpDownCounter(
		"process_uptime_seconds",
		"Process uptime in seconds",
		"s")
	s.processFileDescriptors = mb.Int64UpDownCounter(
		"process_open_fds",
		"Number of open file descriptors",
		"1")
}

// buildSystemPerformanceMetrics creates system performance related metrics
func (s *SystemMetrics) buildSystemPerformanceMetrics(mb *metricBuilder) {
	utilizationBuckets := []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99}

	s.cpuUtilization = mb.Float64Histogram(
		"system_cpu_utilization_ratio",
		"System CPU utilization ratio",
		"1",
		utilizationBuckets)
	s.memoryUtilization = mb.Float64Histogram(
		"system_memory_utilization_ratio",
		"System memory utilization ratio",
		"1",
		utilizationBuckets)
	s.diskIOOperations = mb.Int64Counter(
		"system_disk_io_operations_total",
		"Total disk I/O operations",
		"1")
	s.diskIOBytes = mb.Int64Counter(
		"system_disk_io_bytes_total",
		"Total disk I/O bytes",
		"By")
	s.networkConnections = mb.Int64UpDownCounter(
		"system_network_connections_active",
		"Number of active network connections",
		"1")
	s.networkIOBytes = mb.Int64Counter(
		"system_network_io_bytes_total",
		"Total network I/O bytes",
		"By")
}

// buildApplicationMetrics creates application specific metrics
func (s *SystemMetrics) buildApplicationMetrics(mb *metricBuilder) {
	s.requestQueueSize = mb.Int64UpDownCounter(
		"application_request_queue_size",
		"Size of request processing queue",
		"1")
	s.connectionPoolUsage = mb.Float64Histogram(
		"application_connection_pool_usage_ratio",
		"Connection pool usage ratio",
		"1",
		[]float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99})
	s.cacheMemoryUsage = mb.Int64UpDownCounter(
		"application_cache_memory_bytes",
		"Memory used by application caches",
		"By")
	s.backgroundTasks = mb.Int64UpDownCounter(
		"application_background_tasks_active",
		"Number of active background tasks",
		"1")
	s.scheduledJobs = mb.Int64Counter(
		"application_scheduled_jobs_total",
		"Total scheduled jobs executed",
		"1")
}

// buildHealthMetrics creates health and error metrics
func (s *SystemMetrics) buildHealthMetrics(mb *metricBuilder) {
	s.systemErrors = mb.Int64Counter(
		"system_errors_total",
		"Total system-level errors",
		"1")
	s.memoryPressure = mb.Int64Counter(
		"system_memory_pressure_events_total",
		"Total memory pressure events",
		"1")
	s.resourceExhaustion = mb.Int64Counter(
		"system_resource_exhaustion_events_total",
		"Total resource exhaustion events",
		"1")
	s.healthCheckResults = mb.Int64Counter(
		"system_health_check_results_total",
		"Total health check results",
		"1")
}

// buildPerformanceIndicators creates performance indicator metrics
func (s *SystemMetrics) buildPerformanceIndicators(mb *metricBuilder) {
	s.responseTimeP99 = mb.Float64Histogram(
		"application_response_time_p99_seconds",
		"99th percentile response time",
		"s",
		[]float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10})
	s.throughputPerSecond = mb.Float64Histogram(
		"application_throughput_per_second",
		"Application throughput per second",
		"1/s",
		[]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500})
	s.errorRate = mb.Float64Histogram(
		"application_error_rate_ratio",
		"Application error rate ratio",
		"1",
		[]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.3, 0.4, 0.5})
	s.availability = mb.Float64Histogram(
		"application_availability_ratio",
		"Application availability ratio",
		"1",
		[]float64{0.90, 0.95, 0.99, 0.995, 0.999, 0.9995, 0.9999, 1.0})
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
