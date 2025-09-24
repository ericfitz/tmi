package slogging

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// FallbackLogger provides a simple logger that writes to gin's output (compatibility)
type FallbackLogger struct {
	logger *slog.Logger
}

// Debug logs debug level messages
func (l *FallbackLogger) Debug(format string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}
	l.logger.Debug(message)
}

// Info logs info level messages
func (l *FallbackLogger) Info(format string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}
	l.logger.Info(message)
}

// Warn logs warning level messages
func (l *FallbackLogger) Warn(format string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}
	l.logger.Warn(message)
}

// Error logs error level messages
func (l *FallbackLogger) Error(format string, args ...any) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}
	l.logger.Error(message)
}

// NewFallbackLogger creates a simple logger for fallback use
func NewFallbackLogger() SimpleLogger {
	handler := slog.NewTextHandler(gin.DefaultWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return &FallbackLogger{
		logger: slog.New(handler),
	}
}

// GinContextLike defines a minimal interface for contexts that can be used with the logger
type GinContextLike interface {
	Get(key any) (any, bool)
	GetHeader(key string) string
	ClientIP() string
}

// GetContextLogger retrieves a logger from the context or creates a fallback
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

	// Create a logger with context attributes
	contextLogger := l.slogger.With(
		slog.String("request_id", requestID),
		slog.String("client_ip", c.ClientIP()),
		slog.String("user_id", fmt.Sprintf("%v", userID)),
	)

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
type ContextLogger struct {
	logger    *Logger
	slogger   *slog.Logger
	ctx       context.Context
	requestID string
	clientIP  string
	userID    string
}

// formatContextMessage formats a message with request context (compatibility method)
func (cl *ContextLogger) formatContextMessage(msg string) string {
	contextInfo := fmt.Sprintf("[%s]", cl.requestID)

	// Always log user, even if empty
	contextInfo += fmt.Sprintf(" user=%s", cl.userID)

	if cl.clientIP != "" {
		contextInfo += fmt.Sprintf(" ip=%s", cl.clientIP)
	}

	return fmt.Sprintf("%s | %s", contextInfo, msg)
}

// Debug logs a debug-level message with context (compatibility method)
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

	cl.slogger.Debug(message)
}

// Info logs an info-level message with context (compatibility method)
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

	cl.slogger.Info(message)
}

// Warn logs a warning-level message with context (compatibility method)
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

	cl.slogger.Warn(message)
}

// Error logs an error-level message with context (compatibility method)
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

	cl.slogger.Error(message)
}

// Structured logging methods for ContextLogger

// DebugCtx logs a debug message with additional structured attributes
func (cl *ContextLogger) DebugCtx(msg string, attrs ...slog.Attr) {
	cl.slogger.LogAttrs(cl.ctx, slog.LevelDebug, msg, attrs...)
}

// InfoCtx logs an info message with additional structured attributes
func (cl *ContextLogger) InfoCtx(msg string, attrs ...slog.Attr) {
	cl.slogger.LogAttrs(cl.ctx, slog.LevelInfo, msg, attrs...)
}

// WarnCtx logs a warning message with additional structured attributes
func (cl *ContextLogger) WarnCtx(msg string, attrs ...slog.Attr) {
	cl.slogger.LogAttrs(cl.ctx, slog.LevelWarn, msg, attrs...)
}

// ErrorCtx logs an error message with additional structured attributes
func (cl *ContextLogger) ErrorCtx(msg string, attrs ...slog.Attr) {
	cl.slogger.LogAttrs(cl.ctx, slog.LevelError, msg, attrs...)
}

// WithAttrs returns a new ContextLogger with additional attributes
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
func (cl *ContextLogger) GetSlogger() *slog.Logger {
	return cl.slogger
}

// Helper function to convert slog.Attr to any values
func attrsToAny(attrs []slog.Attr) []any {
	result := make([]any, 0, len(attrs)*2)
	for _, attr := range attrs {
		result = append(result, attr.Key, attr.Value)
	}
	return result
}

// Compatibility functions for migrated code
// These need to be added to the main package exports
