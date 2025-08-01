package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// DatabaseMetrics provides comprehensive database performance metrics
type DatabaseMetrics struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Core database metrics
	queryDuration        metric.Float64Histogram
	queryCounter         metric.Int64Counter
	transactionDuration  metric.Float64Histogram
	transactionCounter   metric.Int64Counter
	connectionPoolActive metric.Int64UpDownCounter
	connectionPoolIdle   metric.Int64UpDownCounter
	connectionWaitTime   metric.Float64Histogram

	// Query performance metrics
	slowQueryCounter metric.Int64Counter
	queryByTable     metric.Int64Counter
	queryByOperation metric.Int64Counter
	rowsAffected     metric.Int64Histogram
	rowsReturned     metric.Int64Histogram

	// Connection metrics
	connectionLifetime metric.Float64Histogram
	connectionErrors   metric.Int64Counter
	connectionRetries  metric.Int64Counter

	// Advanced metrics
	lockWaitTime     metric.Float64Histogram
	deadlockCounter  metric.Int64Counter
	queryPlanChanges metric.Int64Counter
	indexUsage       metric.Int64Counter

	// Migration and schema metrics
	migrationDuration metric.Float64Histogram
	migrationCounter  metric.Int64Counter
	schemaValidation  metric.Int64Counter
}

// NewDatabaseMetrics creates a new database metrics instance
func NewDatabaseMetrics(tracer trace.Tracer, meter metric.Meter) (*DatabaseMetrics, error) {
	d := &DatabaseMetrics{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Core database metrics
	d.queryDuration, err = meter.Float64Histogram(
		"db_query_duration_seconds",
		metric.WithDescription("Duration of database queries"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create query duration histogram: %w", err)
	}

	d.queryCounter, err = meter.Int64Counter(
		"db_queries_total",
		metric.WithDescription("Total number of database queries"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create query counter: %w", err)
	}

	d.transactionDuration, err = meter.Float64Histogram(
		"db_transaction_duration_seconds",
		metric.WithDescription("Duration of database transactions"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction duration histogram: %w", err)
	}

	d.transactionCounter, err = meter.Int64Counter(
		"db_transactions_total",
		metric.WithDescription("Total number of database transactions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction counter: %w", err)
	}

	d.connectionPoolActive, err = meter.Int64UpDownCounter(
		"db_connections_active",
		metric.WithDescription("Number of active database connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create active connections counter: %w", err)
	}

	d.connectionPoolIdle, err = meter.Int64UpDownCounter(
		"db_connections_idle",
		metric.WithDescription("Number of idle database connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create idle connections counter: %w", err)
	}

	d.connectionWaitTime, err = meter.Float64Histogram(
		"db_connection_wait_time_seconds",
		metric.WithDescription("Time spent waiting for database connections"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection wait time histogram: %w", err)
	}

	// Query performance metrics
	d.slowQueryCounter, err = meter.Int64Counter(
		"db_slow_queries_total",
		metric.WithDescription("Total number of slow database queries"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create slow query counter: %w", err)
	}

	d.queryByTable, err = meter.Int64Counter(
		"db_queries_by_table_total",
		metric.WithDescription("Total database queries by table"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create queries by table counter: %w", err)
	}

	d.queryByOperation, err = meter.Int64Counter(
		"db_queries_by_operation_total",
		metric.WithDescription("Total database queries by operation type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create queries by operation counter: %w", err)
	}

	d.rowsAffected, err = meter.Int64Histogram(
		"db_rows_affected",
		metric.WithDescription("Number of rows affected by database operations"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500, 1000, 5000, 10000),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rows affected histogram: %w", err)
	}

	d.rowsReturned, err = meter.Int64Histogram(
		"db_rows_returned",
		metric.WithDescription("Number of rows returned by database queries"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 50, 100, 500, 1000, 5000, 10000),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rows returned histogram: %w", err)
	}

	// Connection metrics
	d.connectionLifetime, err = meter.Float64Histogram(
		"db_connection_lifetime_seconds",
		metric.WithDescription("Lifetime of database connections"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 30, 60, 300, 600, 1800, 3600),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection lifetime histogram: %w", err)
	}

	d.connectionErrors, err = meter.Int64Counter(
		"db_connection_errors_total",
		metric.WithDescription("Total number of database connection errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection errors counter: %w", err)
	}

	d.connectionRetries, err = meter.Int64Counter(
		"db_connection_retries_total",
		metric.WithDescription("Total number of database connection retries"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection retries counter: %w", err)
	}

	// Advanced metrics
	d.lockWaitTime, err = meter.Float64Histogram(
		"db_lock_wait_time_seconds",
		metric.WithDescription("Time spent waiting for database locks"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create lock wait time histogram: %w", err)
	}

	d.deadlockCounter, err = meter.Int64Counter(
		"db_deadlocks_total",
		metric.WithDescription("Total number of database deadlocks"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create deadlock counter: %w", err)
	}

	d.queryPlanChanges, err = meter.Int64Counter(
		"db_query_plan_changes_total",
		metric.WithDescription("Total number of query plan changes"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create query plan changes counter: %w", err)
	}

	d.indexUsage, err = meter.Int64Counter(
		"db_index_usage_total",
		metric.WithDescription("Total database index usage"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create index usage counter: %w", err)
	}

	// Migration and schema metrics
	d.migrationDuration, err = meter.Float64Histogram(
		"db_migration_duration_seconds",
		metric.WithDescription("Duration of database migrations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.5, 1, 5, 10, 30, 60, 300, 600),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration duration histogram: %w", err)
	}

	d.migrationCounter, err = meter.Int64Counter(
		"db_migrations_total",
		metric.WithDescription("Total number of database migrations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration counter: %w", err)
	}

	d.schemaValidation, err = meter.Int64Counter(
		"db_schema_validations_total",
		metric.WithDescription("Total number of schema validations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create schema validation counter: %w", err)
	}

	return d, nil
}

// QueryMetrics holds information about a database query for metrics recording
type QueryMetrics struct {
	StartTime    time.Time
	Operation    string
	Table        string
	Query        string
	RowsAffected int64
	RowsReturned int64
	IsSlowQuery  bool
	LockWaitTime time.Duration
	PlanChanged  bool
	IndexesUsed  []string
}

// RecordQuery records comprehensive metrics for a database query
func (d *DatabaseMetrics) RecordQuery(ctx context.Context, queryMetrics *QueryMetrics, err error) {
	duration := time.Since(queryMetrics.StartTime)

	// Base attributes
	baseAttrs := []attribute.KeyValue{
		attribute.String("operation", queryMetrics.Operation),
		attribute.String("table", queryMetrics.Table),
	}

	// Add status
	status := "success"
	if err != nil {
		status = "error"
		baseAttrs = append(baseAttrs, attribute.String("error_type", getDBErrorType(err)))
	}
	baseAttrs = append(baseAttrs, attribute.String("status", status))

	// Record core metrics
	d.queryCounter.Add(ctx, 1, metric.WithAttributes(baseAttrs...))
	d.queryDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(baseAttrs...))

	// Record query by table and operation
	d.queryByTable.Add(ctx, 1, metric.WithAttributes(
		attribute.String("table", queryMetrics.Table),
		attribute.String("operation", queryMetrics.Operation),
		attribute.String("status", status),
	))

	d.queryByOperation.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation", queryMetrics.Operation),
		attribute.String("status", status),
	))

	// Record slow queries
	if queryMetrics.IsSlowQuery || duration > time.Second {
		d.slowQueryCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", queryMetrics.Operation),
			attribute.String("table", queryMetrics.Table),
			attribute.String("duration_bucket", getQueryDurationBucket(duration)),
		))
	}

	// Record rows affected/returned
	if queryMetrics.RowsAffected > 0 {
		d.rowsAffected.Record(ctx, queryMetrics.RowsAffected, metric.WithAttributes(
			attribute.String("operation", queryMetrics.Operation),
			attribute.String("table", queryMetrics.Table),
		))
	}

	if queryMetrics.RowsReturned > 0 {
		d.rowsReturned.Record(ctx, queryMetrics.RowsReturned, metric.WithAttributes(
			attribute.String("operation", queryMetrics.Operation),
			attribute.String("table", queryMetrics.Table),
		))
	}

	// Record lock wait time
	if queryMetrics.LockWaitTime > 0 {
		d.lockWaitTime.Record(ctx, queryMetrics.LockWaitTime.Seconds(), metric.WithAttributes(
			attribute.String("operation", queryMetrics.Operation),
			attribute.String("table", queryMetrics.Table),
		))
	}

	// Record query plan changes
	if queryMetrics.PlanChanged {
		d.queryPlanChanges.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", queryMetrics.Operation),
			attribute.String("table", queryMetrics.Table),
		))
	}

	// Record index usage
	for _, index := range queryMetrics.IndexesUsed {
		d.indexUsage.Add(ctx, 1, metric.WithAttributes(
			attribute.String("index_name", index),
			attribute.String("table", queryMetrics.Table),
			attribute.String("operation", queryMetrics.Operation),
		))
	}
}

// RecordTransaction records metrics for a database transaction
func (d *DatabaseMetrics) RecordTransaction(ctx context.Context, duration time.Duration, operation string, err error) {
	status := "success"
	if err != nil {
		status = getTransactionStatus(err)
	}

	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("status", status),
	}

	d.transactionCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	d.transactionDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	// Record deadlocks specifically
	if isDeadlock(err) {
		d.deadlockCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", operation),
		))
	}
}

// RecordConnectionPoolStats records connection pool statistics
func (d *DatabaseMetrics) RecordConnectionPoolStats(ctx context.Context, stats sql.DBStats) {
	// Record active and idle connections
	d.connectionPoolActive.Add(ctx, int64(stats.OpenConnections-stats.Idle), metric.WithAttributes(
		attribute.String("pool", "main"),
	))

	d.connectionPoolIdle.Add(ctx, int64(stats.Idle), metric.WithAttributes(
		attribute.String("pool", "main"),
	))

	// Record connection wait time if available
	if stats.WaitCount > 0 && stats.WaitDuration > 0 {
		avgWaitTime := stats.WaitDuration.Seconds() / float64(stats.WaitCount)
		d.connectionWaitTime.Record(ctx, avgWaitTime, metric.WithAttributes(
			attribute.String("pool", "main"),
		))
	}
}

// RecordConnectionError records connection errors
func (d *DatabaseMetrics) RecordConnectionError(ctx context.Context, errorType string, retryCount int) {
	d.connectionErrors.Add(ctx, 1, metric.WithAttributes(
		attribute.String("error_type", errorType),
	))

	if retryCount > 0 {
		d.connectionRetries.Add(ctx, int64(retryCount), metric.WithAttributes(
			attribute.String("error_type", errorType),
		))
	}
}

// RecordConnectionLifetime records the lifetime of a database connection
func (d *DatabaseMetrics) RecordConnectionLifetime(ctx context.Context, lifetime time.Duration, reason string) {
	d.connectionLifetime.Record(ctx, lifetime.Seconds(), metric.WithAttributes(
		attribute.String("close_reason", reason),
	))
}

// RecordMigration records database migration metrics
func (d *DatabaseMetrics) RecordMigration(ctx context.Context, migrationName string, duration time.Duration, direction string, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	attrs := []attribute.KeyValue{
		attribute.String("migration", migrationName),
		attribute.String("direction", direction),
		attribute.String("status", status),
	}

	d.migrationCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	d.migrationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

// RecordSchemaValidation records schema validation events
func (d *DatabaseMetrics) RecordSchemaValidation(ctx context.Context, validationType string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}

	d.schemaValidation.Add(ctx, 1, metric.WithAttributes(
		attribute.String("validation_type", validationType),
		attribute.String("status", status),
	))
}

// CreateQueryMetrics creates a new QueryMetrics instance
func (d *DatabaseMetrics) CreateQueryMetrics(query string) *QueryMetrics {
	operation := extractOperation(query)
	table := extractTable(query)

	return &QueryMetrics{
		StartTime:   time.Now(),
		Operation:   operation,
		Table:       table,
		Query:       query,
		IndexesUsed: make([]string, 0),
	}
}

// Helper functions

// Note: extractOperation and extractTable functions are shared with database.go

func getDBErrorType(err error) string {
	if err == nil {
		return "none"
	}

	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "connection"):
		return "connection_error"
	case strings.Contains(errStr, "timeout"):
		return "timeout"
	case strings.Contains(errStr, "deadlock"):
		return "deadlock"
	case strings.Contains(errStr, "constraint"):
		return "constraint_violation"
	case strings.Contains(errStr, "syntax"):
		return "syntax_error"
	case strings.Contains(errStr, "permission"):
		return "permission_denied"
	case strings.Contains(errStr, "duplicate"):
		return "duplicate_key"
	default:
		return "database_error"
	}
}

func getTransactionStatus(err error) string {
	if err == nil {
		return "committed"
	}

	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "deadlock"):
		return "deadlock"
	case strings.Contains(errStr, "rollback"):
		return "rolled_back"
	case strings.Contains(errStr, "timeout"):
		return "timeout"
	default:
		return "error"
	}
}

func isDeadlock(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "deadlock")
}

func getQueryDurationBucket(duration time.Duration) string {
	seconds := duration.Seconds()
	switch {
	case seconds <= 1:
		return "1s"
	case seconds <= 2:
		return "2s"
	case seconds <= 5:
		return "5s"
	case seconds <= 10:
		return "10s"
	case seconds <= 30:
		return "30s"
	default:
		return "30s+"
	}
}

// GetDatabaseMetrics returns a configured database metrics instance
func GetDatabaseMetrics() *DatabaseMetrics {
	service := GetService()
	if service == nil {
		return nil
	}

	dbMetrics, _ := NewDatabaseMetrics(service.GetTracer(), service.GetMeter())
	return dbMetrics
}
