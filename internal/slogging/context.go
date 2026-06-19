package slogging

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

// FallbackLogger provides a simple logger that writes to gin's output (compatibility)
// All messages are sanitized to prevent log injection attacks (CWE-117)
// SEM@fd65443f98d69fa4f22b8f982c98ebf8eb89c515: simple slog-backed logger used when no request-scoped logger is available (pure)
type FallbackLogger struct {
	logger *slog.Logger
}

// Debug logs debug level messages
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a sanitized debug message via the fallback logger (pure)
func (l *FallbackLogger) Debug(format string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}
	l.logger.Debug(SanitizeLogMessage(message))
}

// Info logs info level messages
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a sanitized info message via the fallback logger (pure)
func (l *FallbackLogger) Info(format string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}
	l.logger.Info(SanitizeLogMessage(message))
}

// Warn logs warning level messages
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a sanitized warning message via the fallback logger (pure)
func (l *FallbackLogger) Warn(format string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}
	l.logger.Warn(SanitizeLogMessage(message))
}

// Error logs error level messages
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a sanitized error message via the fallback logger (pure)
func (l *FallbackLogger) Error(format string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}
	l.logger.Error(SanitizeLogMessage(message))
}

// NewFallbackLogger creates a simple logger for fallback use
// SEM@fd65443f98d69fa4f22b8f982c98ebf8eb89c515: build a fallback SimpleLogger writing sanitized text to gin's default output (pure)
func NewFallbackLogger() SimpleLogger {
	handler := slog.NewTextHandler(gin.DefaultWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return &FallbackLogger{
		logger: slog.New(handler),
	}
}

// GinContextLike defines a minimal interface for contexts that can be used with the logger
// SEM@c94efddda17fc5e306e3f4ec21ff2fe092472d98: minimal interface for contexts providing header, IP, and key lookup for logger attachment (pure)
type GinContextLike interface {
	Get(key any) (any, bool)
	GetHeader(key string) string
	ClientIP() string
}

// GetContextLogger retrieves a logger from the context or creates a fallback
// SEM@fd65443f98d69fa4f22b8f982c98ebf8eb89c515: fetch the request-scoped logger from a context or return a fallback logger (pure)
func GetContextLogger(c GinContextLike) SimpleLogger {
	// Get logger from context
	loggerInterface, exists := c.Get("logger")
	if exists {
		if logger, ok := loggerInterface.(SimpleLogger); ok {
			return logger
		}
	}

	// Return fallback logger
	return NewFallbackLogger()
}

// WithContext returns a context-aware logger that includes request information
// SEM@3da74faa5e66fc9ead55d176f928fb36713b5ec0: build a ContextLogger bound to request metadata including request ID, client IP, user, and OTel trace (pure)
func (l *Logger) WithContext(c GinContextLike) *ContextLogger {
	// Get or generate request ID
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
		// Only set header if context supports it
		if setter, ok := c.(interface{ Header(string, string) }); ok {
			setter.Header("X-Request-ID", requestID)
		}
	}

	// Get user info if available
	userID, _ := c.Get("userName")

	// Create context with request attributes
	ctx := context.Background()

	// Build logger attributes, starting with request context fields
	logAttrs := []any{
		slog.String("request_id", requestID),
		slog.String("client_ip", c.ClientIP()),
		slog.String("user_id", fmt.Sprintf("%v", userID)),
	}

	// Add OTel trace ID to log entries for trace-log correlation
	if goCtx, ok := c.(context.Context); ok {
		spanCtx := trace.SpanContextFromContext(goCtx)
		if spanCtx.IsValid() {
			logAttrs = append(logAttrs, slog.String("trace_id", spanCtx.TraceID().String()))
			logAttrs = append(logAttrs, slog.String("span_id", spanCtx.SpanID().String()))
		}
	}

	// Create a logger with context attributes
	contextLogger := l.slogger.With(logAttrs...)

	return &ContextLogger{
		logger:    l,
		slogger:   contextLogger,
		ctx:       ctx,
		requestID: requestID,
		clientIP:  c.ClientIP(),
		userID:    fmt.Sprintf("%v", userID),
	}
}

// ContextLogger adds request context to log messages
// All messages are sanitized to prevent log injection attacks (CWE-117)
// SEM@fd65443f98d69fa4f22b8f982c98ebf8eb89c515: request-scoped structured logger enriched with request ID, client IP, user ID, and OTel trace context (pure)
type ContextLogger struct {
	logger    *Logger
	slogger   *slog.Logger
	ctx       context.Context
	requestID string
	clientIP  string
	userID    string
}

// Debug logs a debug-level message with context (compatibility method)
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a sanitized debug message with request context (pure)
func (cl *ContextLogger) Debug(format string, args ...any) {
	if cl.logger.level > LogLevelDebug {
		return
	}

	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	cl.slogger.Debug(SanitizeLogMessage(message))
}

// Info logs an info-level message with context (compatibility method)
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a sanitized info message with request context (pure)
func (cl *ContextLogger) Info(format string, args ...any) {
	if cl.logger.level > LogLevelInfo {
		return
	}

	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	cl.slogger.Info(SanitizeLogMessage(message))
}

// Warn logs a warning-level message with context (compatibility method)
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a sanitized warning message with request context (pure)
func (cl *ContextLogger) Warn(format string, args ...any) {
	if cl.logger.level > LogLevelWarn {
		return
	}

	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	cl.slogger.Warn(SanitizeLogMessage(message))
}

// Error logs an error-level message with context (compatibility method)
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a sanitized error message with request context (pure)
func (cl *ContextLogger) Error(format string, args ...any) {
	if cl.logger.level > LogLevelError {
		return
	}

	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	cl.slogger.Error(SanitizeLogMessage(message))
}

// Structured logging methods for ContextLogger

// DebugCtx logs a debug message with additional structured attributes
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a debug message with structured slog attributes via the request context (pure)
func (cl *ContextLogger) DebugCtx(msg string, attrs ...slog.Attr) {
	cl.slogger.LogAttrs(cl.ctx, slog.LevelDebug, SanitizeLogMessage(msg), attrs...)
}

// InfoCtx logs an info message with additional structured attributes
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log an info message with structured slog attributes via the request context (pure)
func (cl *ContextLogger) InfoCtx(msg string, attrs ...slog.Attr) {
	cl.slogger.LogAttrs(cl.ctx, slog.LevelInfo, SanitizeLogMessage(msg), attrs...)
}

// WarnCtx logs a warning message with additional structured attributes
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log a warning message with structured slog attributes via the request context (pure)
func (cl *ContextLogger) WarnCtx(msg string, attrs ...slog.Attr) {
	cl.slogger.LogAttrs(cl.ctx, slog.LevelWarn, SanitizeLogMessage(msg), attrs...)
}

// ErrorCtx logs an error message with additional structured attributes
// SEM@b29bc09af9d85dba2a37f84f1ec25c440fb77c3f: log an error message with structured slog attributes via the request context (pure)
func (cl *ContextLogger) ErrorCtx(msg string, attrs ...slog.Attr) {
	cl.slogger.LogAttrs(cl.ctx, slog.LevelError, SanitizeLogMessage(msg), attrs...)
}

// WithAttrs returns a new ContextLogger with additional attributes
// SEM@fd65443f98d69fa4f22b8f982c98ebf8eb89c515: build a new ContextLogger with additional structured attributes attached (pure)
func (cl *ContextLogger) WithAttrs(attrs ...slog.Attr) *ContextLogger {
	return &ContextLogger{
		logger:    cl.logger,
		slogger:   cl.slogger.With(attrsToAny(attrs)...),
		ctx:       cl.ctx,
		requestID: cl.requestID,
		clientIP:  cl.clientIP,
		userID:    cl.userID,
	}
}

// WithGroup returns a new ContextLogger with a group name
// SEM@fd65443f98d69fa4f22b8f982c98ebf8eb89c515: build a new ContextLogger with all subsequent attributes nested under a group name (pure)
func (cl *ContextLogger) WithGroup(name string) *ContextLogger {
	return &ContextLogger{
		logger:    cl.logger,
		slogger:   cl.slogger.WithGroup(name),
		ctx:       cl.ctx,
		requestID: cl.requestID,
		clientIP:  cl.clientIP,
		userID:    cl.userID,
	}
}

// GetSlogger returns the underlying slog.Logger for this context
// SEM@fd65443f98d69fa4f22b8f982c98ebf8eb89c515: return the underlying slog.Logger for direct structured logging (pure)
func (cl *ContextLogger) GetSlogger() *slog.Logger {
	return cl.slogger
}

// Helper function to convert slog.Attr to any values
// SEM@fd65443f98d69fa4f22b8f982c98ebf8eb89c515: convert a slice of slog.Attr to alternating key-value pairs for slog With calls (pure)
func attrsToAny(attrs []slog.Attr) []any {
	result := make([]any, 0, len(attrs)*2)
	for _, attr := range attrs {
		result = append(result, attr.Key, attr.Value)
	}
	return result
}

// Compatibility functions for migrated code
// These need to be added to the main package exports
