package slogging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// LogLevel represents logging verbosity (maintained for compatibility)
type LogLevel int

const (
	// LogLevelDebug includes detailed debug information
	LogLevelDebug LogLevel = iota
	// LogLevelInfo includes general request information
	LogLevelInfo
	// LogLevelWarn includes warnings and errors only
	LogLevelWarn
	// LogLevelError includes only errors
	LogLevelError
)

var (
	// For storing the global logger instance
	globalLogger *Logger
	// Default log file location
	defaultLogDir = "logs"
)

// SimpleLogger defines the basic logging interface used across the app (compatibility)
type SimpleLogger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// Logger is the slog-based logging component
type Logger struct {
	slogger                     *slog.Logger
	level                       LogLevel
	isDev                       bool
	fileLogger                  *lumberjack.Logger
	suppressUnauthenticatedLogs bool
}

// Config holds configuration options for the logger (maintained for compatibility)
type Config struct {
	// Level is the minimum log level to output
	Level LogLevel
	// IsDev indicates if this is a development build (includes file/line info)
	IsDev bool
	// LogDir is the directory to store log files
	LogDir string
	// MaxAgeDays is the maximum number of days to retain logs
	MaxAgeDays int
	// MaxSizeMB is the maximum size of a log file in MB before rotation
	MaxSizeMB int
	// MaxBackups is the maximum number of old log files to retain
	MaxBackups int
	// AlsoLogToConsole controls if logs also go to stdout/stderr
	AlsoLogToConsole bool
	// SuppressUnauthenticatedLogs controls whether to log requests without authenticated users
	SuppressUnauthenticatedLogs bool
	// RedactionConfig controls sensitive data redaction (optional, uses defaults if nil)
	RedactionConfig *RedactionConfig
}

// ParseLogLevel converts a string log level to LogLevel
func ParseLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn", "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// toSlogLevel converts our LogLevel to slog.Level
func (l LogLevel) toSlogLevel() slog.Level {
	switch l {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelInfo:
		return slog.LevelInfo
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// customHandler wraps slog handlers to add source information in dev mode
type customHandler struct {
	handler slog.Handler
	isDev   bool
}

func (h *customHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *customHandler) Handle(ctx context.Context, record slog.Record) error {
	// Add source information in dev mode
	if h.isDev {
		if record.PC == 0 {
			// Get caller info if not already set
			_, file, line, ok := runtime.Caller(4) // Skip through slog layers
			if ok {
				record.Add(slog.String("source", fmt.Sprintf("%s:%d", filepath.Base(file), line)))
			}
		} else {
			// Use existing PC to get source info
			frame := runtime.CallersFrames([]uintptr{record.PC})
			f, _ := frame.Next()
			record.Add(slog.String("source", fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)))
		}
	}

	return h.handler.Handle(ctx, record)
}

func (h *customHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &customHandler{
		handler: h.handler.WithAttrs(attrs),
		isDev:   h.isDev,
	}
}

func (h *customHandler) WithGroup(name string) slog.Handler {
	return &customHandler{
		handler: h.handler.WithGroup(name),
		isDev:   h.isDev,
	}
}

// NewLogger creates a new slog-based logger instance
func NewLogger(config Config) (*Logger, error) {
	// Set defaults
	if config.LogDir == "" {
		config.LogDir = defaultLogDir
	}
	if config.MaxAgeDays <= 0 {
		config.MaxAgeDays = 7
	}
	if config.MaxSizeMB <= 0 {
		config.MaxSizeMB = 100
	}
	if config.MaxBackups <= 0 {
		config.MaxBackups = 10
	}

	// Create log directory if it doesn't exist
	if err := os.MkdirAll(config.LogDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Setup log rotation
	fileLogger := &lumberjack.Logger{
		Filename:   filepath.Join(config.LogDir, "tmi.log"),
		MaxSize:    config.MaxSizeMB,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAgeDays,
		Compress:   true,
	}

	// Create writer
	var writer io.Writer
	if config.AlsoLogToConsole {
		writer = io.MultiWriter(os.Stdout, fileLogger)
	} else {
		writer = fileLogger
	}

	// Create slog handler options
	handlerOpts := &slog.HandlerOptions{
		Level: config.Level.toSlogLevel(),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize time format for compatibility
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					return slog.String(slog.TimeKey, t.Format(time.RFC3339))
				}
			}
			return a
		},
	}

	// Create appropriate handler based on environment
	var handler slog.Handler
	if config.IsDev {
		// Text handler for development (easier to read)
		handler = slog.NewTextHandler(writer, handlerOpts)
	} else {
		// JSON handler for production (structured logging)
		handler = slog.NewJSONHandler(writer, handlerOpts)
	}

	// Wrap with redaction handler
	redactionConfig := DefaultRedactionConfig()
	if config.RedactionConfig != nil {
		redactionConfig = *config.RedactionConfig
	}
	redactionHandler, err := NewRedactionHandler(handler, redactionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create redaction handler: %w", err)
	}

	// Wrap with custom handler for source info
	customHandler := &customHandler{
		handler: redactionHandler,
		isDev:   config.IsDev,
	}

	// Create slog logger
	slogger := slog.New(customHandler)

	return &Logger{
		slogger:                     slogger,
		level:                       config.Level,
		isDev:                       config.IsDev,
		fileLogger:                  fileLogger,
		suppressUnauthenticatedLogs: config.SuppressUnauthenticatedLogs,
	}, nil
}

// Initialize sets up the global logger
func Initialize(config Config) error {
	logger, err := NewLogger(config)
	if err != nil {
		return err
	}
	globalLogger = logger

	// Set as default slog logger
	slog.SetDefault(logger.slogger)

	return nil
}

// Get returns the global logger instance, initializing with defaults if needed
func Get() *Logger {
	if globalLogger == nil {
		// Initialize with defaults if not already initialized
		err := Initialize(Config{
			Level:            LogLevelInfo,
			IsDev:            false,
			MaxAgeDays:       7,
			AlsoLogToConsole: true,
		})
		if err != nil {
			// If we failed to initialize, fall back to a simple console logger
			handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			})
			globalLogger = &Logger{
				slogger:                     slog.New(handler),
				level:                       LogLevelInfo,
				isDev:                       false,
				suppressUnauthenticatedLogs: false,
			}
		}
	}
	return globalLogger
}

// Close properly closes the logger
func (l *Logger) Close() error {
	if l.fileLogger != nil {
		return l.fileLogger.Close()
	}
	return nil
}

// Debug logs a debug-level message (compatibility method)
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.level > LogLevelDebug {
		return
	}

	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	l.slogger.Debug(message)
}

// Info logs an info-level message (compatibility method)
func (l *Logger) Info(format string, args ...interface{}) {
	if l.level > LogLevelInfo {
		return
	}

	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	l.slogger.Info(message)
}

// Warn logs a warning-level message (compatibility method)
func (l *Logger) Warn(format string, args ...interface{}) {
	if l.level > LogLevelWarn {
		return
	}

	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	l.slogger.Warn(message)
}

// Error logs an error-level message (compatibility method)
func (l *Logger) Error(format string, args ...interface{}) {
	if l.level > LogLevelError {
		return
	}

	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	} else {
		message = format
	}

	l.slogger.Error(message)
}

// Structured logging methods (new slog-native methods)

// DebugCtx logs a debug message with context and structured attributes
func (l *Logger) DebugCtx(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.slogger.LogAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

// InfoCtx logs an info message with context and structured attributes
func (l *Logger) InfoCtx(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.slogger.LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
}

// WarnCtx logs a warning message with context and structured attributes
func (l *Logger) WarnCtx(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.slogger.LogAttrs(ctx, slog.LevelWarn, msg, attrs...)
}

// ErrorCtx logs an error message with context and structured attributes
func (l *Logger) ErrorCtx(ctx context.Context, msg string, attrs ...slog.Attr) {
	l.slogger.LogAttrs(ctx, slog.LevelError, msg, attrs...)
}

// GetSlogger returns the underlying slog.Logger for advanced usage
func (l *Logger) GetSlogger() *slog.Logger {
	return l.slogger
}
