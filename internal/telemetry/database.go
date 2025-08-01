package telemetry

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.nhat.io/otelsql"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// DatabaseTracing provides database operation tracing
type DatabaseTracing struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics instruments
	queryCounter        metric.Int64Counter
	queryDuration       metric.Float64Histogram
	connectionCounter   metric.Int64UpDownCounter
	connectionWaitTime  metric.Float64Histogram
	transactionDuration metric.Float64Histogram
	transactionCounter  metric.Int64Counter
}

// NewDatabaseTracing creates a new database tracing instance
func NewDatabaseTracing(tracer trace.Tracer, meter metric.Meter) (*DatabaseTracing, error) {
	d := &DatabaseTracing{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Create metrics instruments
	d.queryCounter, err = meter.Int64Counter(
		"db_queries_total",
		metric.WithDescription("Total number of database queries"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create query counter: %w", err)
	}

	d.queryDuration, err = meter.Float64Histogram(
		"db_query_duration_seconds",
		metric.WithDescription("Duration of database queries"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create query duration histogram: %w", err)
	}

	d.connectionCounter, err = meter.Int64UpDownCounter(
		"db_connections_active",
		metric.WithDescription("Number of active database connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection counter: %w", err)
	}

	d.connectionWaitTime, err = meter.Float64Histogram(
		"db_connection_wait_time_seconds",
		metric.WithDescription("Time spent waiting for a database connection"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection wait time histogram: %w", err)
	}

	d.transactionDuration, err = meter.Float64Histogram(
		"db_transaction_duration_seconds",
		metric.WithDescription("Duration of database transactions"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
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

	return d, nil
}

// WrapDB wraps a database connection with OpenTelemetry instrumentation
func (d *DatabaseTracing) WrapDB(db *sql.DB, driverName, dataSourceName string) *sql.DB {
	// Record database stats
	if err := otelsql.RecordStats(db); err != nil { //nolint:staticcheck
		// Log error but continue (non-fatal)\n\t\t_ = err
	}

	return db
}

// WrapDriver wraps a database driver with OpenTelemetry instrumentation
func (d *DatabaseTracing) WrapDriver(driverName string, driver driver.Driver) (string, error) {
	return otelsql.Register(driverName,
		otelsql.AllowRoot(),
		otelsql.TraceQueryWithoutArgs(),
		otelsql.TraceRowsClose(),
		otelsql.TraceRowsAffected(),
		otelsql.WithDatabaseName("tmi"),
		otelsql.WithSystem(attribute.String("db.system", "postgresql")),
	)
}

// InstrumentedQuery executes a query with tracing
func (d *DatabaseTracing) InstrumentedQuery(ctx context.Context, db *sql.DB, query string, args ...interface{}) (*sql.Rows, error) {
	startTime := time.Now()

	// Create span for query
	ctx, span := d.tracer.Start(ctx, "db.query",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	// Extract operation and table from query
	operation := extractOperation(query)
	table := extractTable(query)

	// Add span attributes
	span.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", operation),
		attribute.String("db.statement", sanitizeQuery(query)),
	)

	if table != "" {
		span.SetAttributes(attribute.String("db.table", table))
	}

	// Execute query
	rows, err := db.QueryContext(ctx, query, args...)

	duration := time.Since(startTime)

	// Record metrics
	status := "success"
	if err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("table", table),
		attribute.String("status", status),
	}

	d.queryCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	d.queryDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	// Add timing attributes to span
	span.SetAttributes(
		attribute.Float64("db.duration_ms", float64(duration.Nanoseconds())/1e6),
		attribute.String("db.status", status),
	)

	return rows, err
}

// InstrumentedExec executes a statement with tracing
func (d *DatabaseTracing) InstrumentedExec(ctx context.Context, db *sql.DB, query string, args ...interface{}) (sql.Result, error) {
	startTime := time.Now()

	// Create span for exec
	ctx, span := d.tracer.Start(ctx, "db.exec",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	// Extract operation and table from query
	operation := extractOperation(query)
	table := extractTable(query)

	// Add span attributes
	span.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", operation),
		attribute.String("db.statement", sanitizeQuery(query)),
	)

	if table != "" {
		span.SetAttributes(attribute.String("db.table", table))
	}

	// Execute statement
	result, err := db.ExecContext(ctx, query, args...)

	duration := time.Since(startTime)

	// Record metrics
	status := "success"
	if err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	// Get affected rows if available
	if result != nil && err == nil {
		if rowsAffected, rowsErr := result.RowsAffected(); rowsErr == nil {
			span.SetAttributes(attribute.Int64("db.rows_affected", rowsAffected))
		}
	}

	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("table", table),
		attribute.String("status", status),
	}

	d.queryCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	d.queryDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	// Add timing attributes to span
	span.SetAttributes(
		attribute.Float64("db.duration_ms", float64(duration.Nanoseconds())/1e6),
		attribute.String("db.status", status),
	)

	return result, err
}

// TracedTransaction wraps a transaction with tracing
type TracedTransaction struct {
	*sql.Tx
	ctx       context.Context
	span      trace.Span
	startTime time.Time
	tracer    *DatabaseTracing
}

// BeginTx starts a traced transaction
func (d *DatabaseTracing) BeginTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions) (*TracedTransaction, error) {
	startTime := time.Now()

	// Create span for transaction
	ctx, span := d.tracer.Start(ctx, "db.transaction",
		trace.WithSpanKind(trace.SpanKindClient),
	)

	span.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", "begin"),
	)

	// Begin transaction
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, err
	}

	// Record transaction start
	d.transactionCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", "started"),
	))

	return &TracedTransaction{
		Tx:        tx,
		ctx:       ctx,
		span:      span,
		startTime: startTime,
		tracer:    d,
	}, nil
}

// Commit commits the transaction with tracing
func (tt *TracedTransaction) Commit() error {
	defer tt.span.End()

	err := tt.Tx.Commit()
	duration := time.Since(tt.startTime)

	status := "committed"
	if err != nil {
		status = "commit_failed"
		tt.span.RecordError(err)
		tt.span.SetStatus(codes.Error, err.Error())
	}

	// Record metrics
	attrs := []attribute.KeyValue{
		attribute.String("status", status),
	}

	tt.tracer.transactionCounter.Add(tt.ctx, 1, metric.WithAttributes(attrs...))
	tt.tracer.transactionDuration.Record(tt.ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	// Add span attributes
	tt.span.SetAttributes(
		attribute.String("db.transaction.status", status),
		attribute.Float64("db.transaction.duration_ms", float64(duration.Nanoseconds())/1e6),
	)

	return err
}

// Rollback rolls back the transaction with tracing
func (tt *TracedTransaction) Rollback() error {
	defer tt.span.End()

	err := tt.Tx.Rollback()
	duration := time.Since(tt.startTime)

	status := "rolled_back"
	if err != nil {
		status = "rollback_failed"
		tt.span.RecordError(err)
		tt.span.SetStatus(codes.Error, err.Error())
	}

	// Record metrics
	attrs := []attribute.KeyValue{
		attribute.String("status", status),
	}

	tt.tracer.transactionCounter.Add(tt.ctx, 1, metric.WithAttributes(attrs...))
	tt.tracer.transactionDuration.Record(tt.ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	// Add span attributes
	tt.span.SetAttributes(
		attribute.String("db.transaction.status", status),
		attribute.Float64("db.transaction.duration_ms", float64(duration.Nanoseconds())/1e6),
	)

	return err
}

// Helper functions for query analysis

var (
	operationRegex = regexp.MustCompile(`^\s*(SELECT|INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|BEGIN|COMMIT|ROLLBACK)\s+`)
	tableRegex     = regexp.MustCompile(`(?i)(?:FROM|INTO|UPDATE|TABLE)\s+([a-zA-Z_][a-zA-Z0-9_]*)\b`)
	sensitiveWords = []string{"password", "token", "secret", "key", "credential"}
)

// extractOperation extracts the SQL operation from a query
func extractOperation(query string) string {
	matches := operationRegex.FindStringSubmatch(strings.TrimSpace(query))
	if len(matches) > 1 {
		return strings.ToUpper(matches[1])
	}
	return "UNKNOWN"
}

// extractTable extracts the primary table from a query
func extractTable(query string) string {
	matches := tableRegex.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// sanitizeQuery removes sensitive information from queries for logging
func sanitizeQuery(query string) string {
	// Remove actual parameter values
	sanitized := regexp.MustCompile(`\$\d+`).ReplaceAllString(query, "?")

	// Remove potential sensitive data
	for _, word := range sensitiveWords {
		pattern := fmt.Sprintf(`(?i)\b%s\s*=\s*'[^']*'`, word)
		sanitized = regexp.MustCompile(pattern).ReplaceAllString(sanitized, fmt.Sprintf("%s = '[REDACTED]'", word))

		pattern = fmt.Sprintf(`(?i)\b%s\s*=\s*"[^"]*"`, word)
		sanitized = regexp.MustCompile(pattern).ReplaceAllString(sanitized, fmt.Sprintf("%s = \"[REDACTED]\"", word))
	}

	// Limit query length for span attributes
	if len(sanitized) > 500 {
		sanitized = sanitized[:497] + "..."
	}

	return sanitized
}

// extractDBName extracts database name from PostgreSQL connection string
func extractDBName(dsn string) string {
	// Simple extraction from common DSN formats
	if strings.Contains(dsn, "dbname=") {
		parts := strings.Split(dsn, "dbname=")
		if len(parts) > 1 {
			dbName := strings.Split(parts[1], " ")[0]
			return dbName
		}
	}

	// Default database name
	return "tmi"
}

// RecordConnectionStats records connection pool statistics
func (d *DatabaseTracing) RecordConnectionStats(ctx context.Context, stats sql.DBStats) {
	d.connectionCounter.Add(ctx, int64(stats.OpenConnections), metric.WithAttributes(
		attribute.String("state", "open"),
	))

	// Record other connection pool metrics as gauges would be better,
	// but we'll use the available instruments
	if stats.WaitCount > 0 {
		avgWaitTime := stats.WaitDuration.Seconds() / float64(stats.WaitCount)
		d.connectionWaitTime.Record(ctx, avgWaitTime)
	}
}
