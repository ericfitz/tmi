package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LogLevel represents logging levels
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// LoggingBridge provides backward compatibility with existing logging while adding OpenTelemetry correlation
type LoggingBridge struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	config         *LoggingConfig
	securityFilter *SecurityFilter
	sampler        *LogSampler
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level             LogLevel
	OutputFile        string
	MaxSize           int // megabytes
	MaxBackups        int
	MaxAge            int // days
	Compress          bool
	EnableConsole     bool
	EnableTraceID     bool
	EnableSpanID      bool
	EnableStructured  bool
	SamplingEnabled   bool
	SamplingRate      float64
	SecurityFiltering bool
}

// LogEntry represents a structured log entry with OpenTelemetry correlation
type LogEntry struct {
	Timestamp  time.Time              `json:"timestamp"`
	Level      string                 `json:"level"`
	Message    string                 `json:"message"`
	TraceID    string                 `json:"trace_id,omitempty"`
	SpanID     string                 `json:"span_id,omitempty"`
	UserID     string                 `json:"user_id,omitempty"`
	RequestID  string                 `json:"request_id,omitempty"`
	Component  string                 `json:"component,omitempty"`
	Operation  string                 `json:"operation,omitempty"`
	Duration   int64                  `json:"duration_ms,omitempty"`
	Error      string                 `json:"error,omitempty"`
	StackTrace string                 `json:"stack_trace,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// NewLoggingBridge creates a new logging bridge with OpenTelemetry integration
func NewLoggingBridge(config *LoggingConfig) (*LoggingBridge, error) {
	service := GetService()
	if service == nil {
		return nil, fmt.Errorf("telemetry service not initialized")
	}

	// Set up log output
	var logHandler slog.Handler

	if config.OutputFile != "" {
		// File logging with rotation
		logWriter := &lumberjack.Logger{
			Filename:   config.OutputFile,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}

		if config.EnableStructured {
			logHandler = slog.NewJSONHandler(logWriter, &slog.HandlerOptions{
				Level: getSlogLevel(config.Level),
			})
		} else {
			logHandler = slog.NewTextHandler(logWriter, &slog.HandlerOptions{
				Level: getSlogLevel(config.Level),
			})
		}
	} else if config.EnableConsole {
		// Console logging
		if config.EnableStructured {
			logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
				Level: getSlogLevel(config.Level),
			})
		} else {
			logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: getSlogLevel(config.Level),
			})
		}
	} else {
		return nil, fmt.Errorf("no log output configured")
	}

	logger := slog.New(logHandler)

	// Initialize security filter
	securityFilter, err := NewSecurityFilter()
	if err != nil {
		return nil, fmt.Errorf("failed to create security filter: %w", err)
	}

	// Initialize sampler
	sampler := NewLogSampler(config.SamplingRate)

	bridge := &LoggingBridge{
		logger:         logger,
		tracer:         service.GetTracer(),
		config:         config,
		securityFilter: securityFilter,
		sampler:        sampler,
	}

	return bridge, nil
}

// Debug logs a debug message with OpenTelemetry correlation
func (lb *LoggingBridge) Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	if lb.config.Level > LevelDebug {
		return
	}

	// Check sampling for debug logs
	if lb.config.SamplingEnabled && !lb.sampler.ShouldLog(LevelDebug) {
		return
	}

	lb.logWithContext(ctx, LevelDebug, msg, attrs...)
}

// Info logs an info message with OpenTelemetry correlation
func (lb *LoggingBridge) Info(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	if lb.config.Level > LevelInfo {
		return
	}

	lb.logWithContext(ctx, LevelInfo, msg, attrs...)
}

// Warn logs a warning message with OpenTelemetry correlation
func (lb *LoggingBridge) Warn(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	if lb.config.Level > LevelWarn {
		return
	}

	lb.logWithContext(ctx, LevelWarn, msg, attrs...)
}

// Error logs an error message with OpenTelemetry correlation
func (lb *LoggingBridge) Error(ctx context.Context, msg string, err error, attrs ...attribute.KeyValue) {
	if lb.config.Level > LevelError {
		return
	}

	// Always log errors regardless of sampling
	allAttrs := make([]attribute.KeyValue, len(attrs))
	copy(allAttrs, attrs)

	if err != nil {
		allAttrs = append(allAttrs, attribute.String("error", err.Error()))

		// Add error to current span if available
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}

	lb.logWithContext(ctx, LevelError, msg, allAttrs...)
}

// Fatal logs a fatal message and exits (use sparingly)
func (lb *LoggingBridge) Fatal(ctx context.Context, msg string, err error, attrs ...attribute.KeyValue) {
	allAttrs := make([]attribute.KeyValue, len(attrs))
	copy(allAttrs, attrs)

	if err != nil {
		allAttrs = append(allAttrs, attribute.String("error", err.Error()))
	}

	lb.logWithContext(ctx, LevelFatal, msg, allAttrs...)
	os.Exit(1)
}

// LogOperation logs the start and completion of an operation
func (lb *LoggingBridge) LogOperation(ctx context.Context, operation string, attrs ...attribute.KeyValue) func(error) {
	startTime := time.Now()

	// Log operation start
	startAttrs := make([]attribute.KeyValue, len(attrs)+1)
	copy(startAttrs, attrs)
	startAttrs[len(attrs)] = attribute.String("operation_phase", "start")

	lb.Debug(ctx, fmt.Sprintf("Starting operation: %s", operation), startAttrs...)

	return func(err error) {
		duration := time.Since(startTime)

		// Log operation completion
		endAttrs := make([]attribute.KeyValue, len(attrs)+2)
		copy(endAttrs, attrs)
		endAttrs[len(attrs)] = attribute.String("operation_phase", "complete")
		endAttrs[len(attrs)+1] = attribute.Int64("duration_ms", duration.Milliseconds())

		if err != nil {
			lb.Error(ctx, fmt.Sprintf("Operation failed: %s", operation), err, endAttrs...)
		} else {
			lb.Info(ctx, fmt.Sprintf("Operation completed: %s", operation), endAttrs...)
		}
	}
}

// LogRequest logs HTTP request details
func (lb *LoggingBridge) LogRequest(ctx context.Context, method, path string, statusCode int, duration time.Duration, attrs ...attribute.KeyValue) {
	level := LevelInfo
	if statusCode >= 400 {
		level = LevelError
	}

	allAttrs := make([]attribute.KeyValue, len(attrs)+4)
	copy(allAttrs, attrs)
	allAttrs[len(attrs)] = attribute.String("http_method", method)
	allAttrs[len(attrs)+1] = attribute.String("http_path", path)
	allAttrs[len(attrs)+2] = attribute.Int("http_status", statusCode)
	allAttrs[len(attrs)+3] = attribute.Int64("http_duration_ms", duration.Milliseconds())

	msg := fmt.Sprintf("%s %s - %d (%dms)", method, path, statusCode, duration.Milliseconds())

	if level == LevelError {
		lb.Error(ctx, msg, nil, allAttrs...)
	} else {
		lb.Info(ctx, msg, allAttrs...)
	}
}

// LogDatabase logs database operation details
func (lb *LoggingBridge) LogDatabase(ctx context.Context, operation, table string, duration time.Duration, rowsAffected int64, err error, attrs ...attribute.KeyValue) {
	allAttrs := make([]attribute.KeyValue, len(attrs)+4)
	copy(allAttrs, attrs)
	allAttrs[len(attrs)] = attribute.String("db_operation", operation)
	allAttrs[len(attrs)+1] = attribute.String("db_table", table)
	allAttrs[len(attrs)+2] = attribute.Int64("db_duration_ms", duration.Milliseconds())
	allAttrs[len(attrs)+3] = attribute.Int64("db_rows_affected", rowsAffected)

	msg := fmt.Sprintf("DB %s on %s (%dms, %d rows)", operation, table, duration.Milliseconds(), rowsAffected)

	if err != nil {
		lb.Error(ctx, msg, err, allAttrs...)
	} else {
		lb.Debug(ctx, msg, allAttrs...)
	}
}

// LogCache logs cache operation details
func (lb *LoggingBridge) LogCache(ctx context.Context, operation, key string, hit bool, duration time.Duration, attrs ...attribute.KeyValue) {
	allAttrs := make([]attribute.KeyValue, len(attrs)+4)
	copy(allAttrs, attrs)
	allAttrs[len(attrs)] = attribute.String("cache_operation", operation)
	allAttrs[len(attrs)+1] = attribute.String("cache_key", lb.securityFilter.SanitizeKey(key))
	allAttrs[len(attrs)+2] = attribute.Bool("cache_hit", hit)
	allAttrs[len(attrs)+3] = attribute.Int64("cache_duration_ms", duration.Milliseconds())

	hitStatus := "MISS"
	if hit {
		hitStatus = "HIT"
	}

	msg := fmt.Sprintf("Cache %s %s (%dms)", operation, hitStatus, duration.Milliseconds())
	lb.Debug(ctx, msg, allAttrs...)
}

// logWithContext is the core logging method that adds OpenTelemetry correlation
func (lb *LoggingBridge) logWithContext(ctx context.Context, level LogLevel, msg string, attrs ...attribute.KeyValue) {
	entry := &LogEntry{
		Timestamp:  time.Now(),
		Level:      level.String(),
		Message:    msg,
		Attributes: make(map[string]interface{}),
	}

	// Add OpenTelemetry correlation
	if lb.config.EnableTraceID || lb.config.EnableSpanID {
		span := trace.SpanFromContext(ctx)
		if span.IsRecording() {
			spanContext := span.SpanContext()

			if lb.config.EnableTraceID && spanContext.HasTraceID() {
				entry.TraceID = spanContext.TraceID().String()
			}

			if lb.config.EnableSpanID && spanContext.HasSpanID() {
				entry.SpanID = spanContext.SpanID().String()
			}
		}
	}

	// Process attributes
	for _, attr := range attrs {
		key := string(attr.Key)
		value := attr.Value.AsInterface()

		// Apply security filtering
		if lb.config.SecurityFiltering {
			key, value = lb.securityFilter.FilterAttribute(key, value)
		}

		// Handle special attributes
		switch key {
		case "user_id":
			entry.UserID = fmt.Sprintf("%v", value)
		case "request_id":
			entry.RequestID = fmt.Sprintf("%v", value)
		case "component":
			entry.Component = fmt.Sprintf("%v", value)
		case "operation":
			entry.Operation = fmt.Sprintf("%v", value)
		case "duration_ms":
			if duration, ok := value.(int64); ok {
				entry.Duration = duration
			}
		case "error":
			entry.Error = fmt.Sprintf("%v", value)
		case "stack_trace":
			entry.StackTrace = fmt.Sprintf("%v", value)
		default:
			entry.Attributes[key] = value
		}
	}

	// Apply final message filtering
	if lb.config.SecurityFiltering {
		entry.Message = lb.securityFilter.SanitizeMessage(entry.Message)
	}

	// Log using structured or text format
	if lb.config.EnableStructured {
		lb.logStructured(entry)
	} else {
		lb.logText(entry)
	}
}

// logStructured logs in JSON format
func (lb *LoggingBridge) logStructured(entry *LogEntry) {
	slogAttrs := make([]slog.Attr, 0)

	if entry.TraceID != "" {
		slogAttrs = append(slogAttrs, slog.String("trace_id", entry.TraceID))
	}
	if entry.SpanID != "" {
		slogAttrs = append(slogAttrs, slog.String("span_id", entry.SpanID))
	}
	if entry.UserID != "" {
		slogAttrs = append(slogAttrs, slog.String("user_id", entry.UserID))
	}
	if entry.RequestID != "" {
		slogAttrs = append(slogAttrs, slog.String("request_id", entry.RequestID))
	}
	if entry.Component != "" {
		slogAttrs = append(slogAttrs, slog.String("component", entry.Component))
	}
	if entry.Operation != "" {
		slogAttrs = append(slogAttrs, slog.String("operation", entry.Operation))
	}
	if entry.Duration > 0 {
		slogAttrs = append(slogAttrs, slog.Int64("duration_ms", entry.Duration))
	}
	if entry.Error != "" {
		slogAttrs = append(slogAttrs, slog.String("error", entry.Error))
	}
	if entry.StackTrace != "" {
		slogAttrs = append(slogAttrs, slog.String("stack_trace", entry.StackTrace))
	}

	// Add custom attributes
	for key, value := range entry.Attributes {
		slogAttrs = append(slogAttrs, slog.Any(key, value))
	}

	lb.logger.LogAttrs(context.Background(), getSlogLevel(LogLevel(0)), entry.Message, slogAttrs...)
}

// logText logs in text format
func (lb *LoggingBridge) logText(entry *LogEntry) {
	msg := entry.Message

	// Add correlation IDs to message
	if entry.TraceID != "" {
		msg += fmt.Sprintf(" [trace_id=%s]", entry.TraceID)
	}
	if entry.SpanID != "" {
		msg += fmt.Sprintf(" [span_id=%s]", entry.SpanID)
	}
	if entry.UserID != "" {
		msg += fmt.Sprintf(" [user_id=%s]", entry.UserID)
	}
	if entry.RequestID != "" {
		msg += fmt.Sprintf(" [request_id=%s]", entry.RequestID)
	}

	lb.logger.Info(msg)
}

// Helper functions

func getSlogLevel(level LogLevel) slog.Level {
	switch level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	case LevelFatal:
		return slog.LevelError // slog doesn't have FATAL, use ERROR
	default:
		return slog.LevelInfo
	}
}

// GetDefaultLoggingConfig returns a default logging configuration
func GetDefaultLoggingConfig() *LoggingConfig {
	return &LoggingConfig{
		Level:             LevelInfo,
		OutputFile:        "",
		MaxSize:           100,
		MaxBackups:        3,
		MaxAge:            28,
		Compress:          true,
		EnableConsole:     true,
		EnableTraceID:     true,
		EnableSpanID:      true,
		EnableStructured:  true,
		SamplingEnabled:   true,
		SamplingRate:      0.1, // 10% sampling for debug logs
		SecurityFiltering: true,
	}
}

// Global logging bridge instance
var globalLoggingBridge *LoggingBridge

// InitializeLoggingBridge initializes the global logging bridge
func InitializeLoggingBridge(config *LoggingConfig) error {
	bridge, err := NewLoggingBridge(config)
	if err != nil {
		return err
	}

	globalLoggingBridge = bridge
	return nil
}

// GetLoggingBridge returns the global logging bridge instance
func GetLoggingBridge() *LoggingBridge {
	return globalLoggingBridge
}

// Convenience functions for global logging
func Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	if globalLoggingBridge != nil {
		globalLoggingBridge.Debug(ctx, msg, attrs...)
	}
}

func Info(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	if globalLoggingBridge != nil {
		globalLoggingBridge.Info(ctx, msg, attrs...)
	}
}

func Warn(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	if globalLoggingBridge != nil {
		globalLoggingBridge.Warn(ctx, msg, attrs...)
	}
}

func Error(ctx context.Context, msg string, err error, attrs ...attribute.KeyValue) {
	if globalLoggingBridge != nil {
		globalLoggingBridge.Error(ctx, msg, err, attrs...)
	}
}

func Fatal(ctx context.Context, msg string, err error, attrs ...attribute.KeyValue) {
	if globalLoggingBridge != nil {
		globalLoggingBridge.Fatal(ctx, msg, err, attrs...)
	}
}
