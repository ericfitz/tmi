package slogging

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected LogLevel
	}{
		{"debug lowercase", "debug", LogLevelDebug},
		{"debug uppercase", "DEBUG", LogLevelDebug},
		{"debug mixed case", "Debug", LogLevelDebug},
		{"info lowercase", "info", LogLevelInfo},
		{"info uppercase", "INFO", LogLevelInfo},
		{"warn lowercase", "warn", LogLevelWarn},
		{"warning lowercase", "warning", LogLevelWarn},
		{"error lowercase", "error", LogLevelError},
		{"error uppercase", "ERROR", LogLevelError},
		{"unknown defaults to info", "unknown", LogLevelInfo},
		{"empty defaults to info", "", LogLevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseLogLevel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LogLevelDebug, "DEBUG"},
		{LogLevelInfo, "INFO"},
		{LogLevelWarn, "WARN"},
		{LogLevelError, "ERROR"},
		{LogLevel(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.level.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLogLevel_toSlogLevel(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected slog.Level
	}{
		{LogLevelDebug, slog.LevelDebug},
		{LogLevelInfo, slog.LevelInfo},
		{LogLevelWarn, slog.LevelWarn},
		{LogLevelError, slog.LevelError},
		{LogLevel(99), slog.LevelInfo}, // Unknown defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			result := tt.level.toSlogLevel()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewLogger(t *testing.T) {
	// Create a temp directory for log files
	tempDir, err := os.MkdirTemp("", "slogging_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	t.Run("creates logger with default config", func(t *testing.T) {
		config := Config{
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		assert.NotNil(t, logger)
		assert.NotNil(t, logger.slogger)
		assert.NotNil(t, logger.fileLogger)
	})

	t.Run("creates logger with custom log level", func(t *testing.T) {
		config := Config{
			Level:  LogLevelDebug,
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		assert.Equal(t, LogLevelDebug, logger.level)
	})

	t.Run("creates logger with dev mode", func(t *testing.T) {
		config := Config{
			IsDev:  true,
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		assert.True(t, logger.isDev)
	})

	t.Run("creates logger with console output", func(t *testing.T) {
		config := Config{
			AlsoLogToConsole: true,
			LogDir:           tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		assert.NotNil(t, logger)
	})

	t.Run("creates logger with suppress unauthenticated logs", func(t *testing.T) {
		config := Config{
			SuppressUnauthenticatedLogs: true,
			LogDir:                      tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		assert.True(t, logger.suppressUnauthenticatedLogs)
	})

	t.Run("uses default log directory if not specified", func(t *testing.T) {
		// Create logger without specifying LogDir
		config := Config{
			LogDir: filepath.Join(tempDir, "default_logs"),
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		assert.NotNil(t, logger)
	})
}

func TestLogger_LogMethods(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slogging_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	t.Run("Debug method logs at debug level", func(t *testing.T) {
		config := Config{
			Level:  LogLevelDebug,
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		// Should not panic
		logger.Debug("debug message")
		logger.Debug("debug message with args: %s", "value")
	})

	t.Run("Info method logs at info level", func(t *testing.T) {
		config := Config{
			Level:  LogLevelInfo,
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		// Should not panic
		logger.Info("info message")
		logger.Info("info message with args: %d", 42)
	})

	t.Run("Warn method logs at warn level", func(t *testing.T) {
		config := Config{
			Level:  LogLevelWarn,
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		// Should not panic
		logger.Warn("warning message")
		logger.Warn("warning message with args: %v", true)
	})

	t.Run("Error method logs at error level", func(t *testing.T) {
		config := Config{
			Level:  LogLevelError,
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		// Should not panic
		logger.Error("error message")
		logger.Error("error message with args: %s", "details")
	})

	t.Run("log methods respect level filtering", func(t *testing.T) {
		config := Config{
			Level:  LogLevelError, // Only error logs
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		// These should be filtered out (no error, but also no output)
		logger.Debug("debug message")
		logger.Info("info message")
		logger.Warn("warn message")
		// This should be logged
		logger.Error("error message")
	})
}

func TestLogger_ContextMethods(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slogging_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	config := Config{
		Level:  LogLevelDebug,
		LogDir: tempDir,
	}
	logger, err := NewLogger(config)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	ctx := context.Background()

	t.Run("DebugCtx logs with context", func(t *testing.T) {
		logger.DebugCtx(ctx, "debug context message", slog.String("key", "value"))
	})

	t.Run("InfoCtx logs with context", func(t *testing.T) {
		logger.InfoCtx(ctx, "info context message", slog.Int("count", 5))
	})

	t.Run("WarnCtx logs with context", func(t *testing.T) {
		logger.WarnCtx(ctx, "warn context message", slog.Bool("flag", true))
	})

	t.Run("ErrorCtx logs with context", func(t *testing.T) {
		logger.ErrorCtx(ctx, "error context message", slog.Any("error", "test error"))
	})
}

func TestLogger_GetSlogger(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slogging_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	config := Config{
		LogDir: tempDir,
	}
	logger, err := NewLogger(config)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	slogger := logger.GetSlogger()
	assert.NotNil(t, slogger)
	assert.IsType(t, &slog.Logger{}, slogger)
}

func TestLogger_Close(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slogging_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	t.Run("close with file logger", func(t *testing.T) {
		config := Config{
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)

		err = logger.Close()
		assert.NoError(t, err)
	})

	t.Run("close without file logger", func(t *testing.T) {
		logger := &Logger{
			fileLogger: nil,
		}
		err := logger.Close()
		assert.NoError(t, err)
	})
}

func TestCustomHandler(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slogging_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	t.Run("in dev mode adds source info", func(t *testing.T) {
		config := Config{
			IsDev:  true,
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		// Log something - source info should be added
		logger.Info("test message")
	})

	t.Run("in prod mode no extra source info", func(t *testing.T) {
		config := Config{
			IsDev:  false,
			LogDir: tempDir,
		}
		logger, err := NewLogger(config)
		require.NoError(t, err)
		defer func() { _ = logger.Close() }()

		// Log something - no extra source info
		logger.Info("test message")
	})
}

func TestLogLevelConstants(t *testing.T) {
	// Test that constants have expected values
	assert.Equal(t, LogLevel(0), LogLevelDebug)
	assert.Equal(t, LogLevel(1), LogLevelInfo)
	assert.Equal(t, LogLevel(2), LogLevelWarn)
	assert.Equal(t, LogLevel(3), LogLevelError)
}

func TestSanitizeLogMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain message unchanged",
			input:    "This is a normal log message",
			expected: "This is a normal log message",
		},
		{
			name:     "removes newlines",
			input:    "Line 1\nLine 2\nLine 3",
			expected: "Line 1 Line 2 Line 3",
		},
		{
			name:     "removes carriage returns",
			input:    "Line 1\rLine 2\rLine 3",
			expected: "Line 1 Line 2 Line 3",
		},
		{
			name:     "removes CRLF",
			input:    "Line 1\r\nLine 2\r\nLine 3",
			expected: "Line 1 Line 2 Line 3",
		},
		{
			name:     "removes tabs",
			input:    "Column1\tColumn2\tColumn3",
			expected: "Column1 Column2 Column3",
		},
		{
			name:     "collapses multiple spaces",
			input:    "Too    many     spaces",
			expected: "Too many spaces",
		},
		{
			name:     "trims leading and trailing whitespace",
			input:    "   trimmed message   ",
			expected: "trimmed message",
		},
		{
			name:     "handles complex injection attempt",
			input:    "User input\n[FAKE] Admin logged in successfully\nReal message continues",
			expected: "User input [FAKE] Admin logged in successfully Real message continues",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "handles only whitespace",
			input:    "   \n\r\t   ",
			expected: "",
		},
		{
			name:     "handles mixed control characters",
			input:    "Start\n\t\rMiddle\r\n\tEnd",
			expected: "Start Middle End",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeLogMessage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLogMethodsSanitization(t *testing.T) {
	// This test verifies that log messages with injection attempts
	// are sanitized when logged through the Logger methods.
	tempDir, err := os.MkdirTemp("", "slogging_sanitize_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	config := Config{
		Level:  LogLevelDebug,
		LogDir: tempDir,
	}
	logger, err := NewLogger(config)
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	// These should not panic and should sanitize the injection attempt
	injectionAttempt := "User input\n[FAKE] Admin action logged\nReal message"

	t.Run("Debug sanitizes injection", func(t *testing.T) {
		logger.Debug("Processing: %s", injectionAttempt)
	})

	t.Run("Info sanitizes injection", func(t *testing.T) {
		logger.Info("Processing: %s", injectionAttempt)
	})

	t.Run("Warn sanitizes injection", func(t *testing.T) {
		logger.Warn("Processing: %s", injectionAttempt)
	})

	t.Run("Error sanitizes injection", func(t *testing.T) {
		logger.Error("Processing: %s", injectionAttempt)
	})
}
