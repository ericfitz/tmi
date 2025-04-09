package logging

import (
	"bytes"
	"strings"
	"testing"
)

// TestLoggerLevels tests that log levels work correctly
func TestLoggerLevels(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	
	// Create test logger with the buffer as output
	logger := &Logger{
		level:  LogLevelInfo, // Set level to INFO
		isDev:  false,
		writer: &buf,
	}
	
	// Test messages
	logger.Debug("This debug message should not appear")
	logger.Info("This info message should appear")
	logger.Warn("This warning message should appear")
	logger.Error("This error message should appear")
	
	// Get output
	output := buf.String()
	
	// Check logs
	if strings.Contains(output, "This debug message should not appear") {
		t.Error("Debug message should not appear when log level is INFO")
	}
	if !strings.Contains(output, "This info message should appear") {
		t.Error("Info message should appear when log level is INFO")
	}
	if !strings.Contains(output, "This warning message should appear") {
		t.Error("Warning message should appear when log level is INFO")
	}
	if !strings.Contains(output, "This error message should appear") {
		t.Error("Error message should appear when log level is INFO")
	}
}

// TestLoggerFormat tests formatting of log messages
func TestLoggerFormat(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	
	// Create test logger with the buffer as output
	logger := &Logger{
		level:  LogLevelDebug,
		isDev:  false,
		writer: &buf,
	}
	
	// Test formatted message
	logger.Info("User %s logged in from %s", "john", "192.168.1.1")
	
	// Get output
	output := buf.String()
	
	// Check formatting
	if !strings.Contains(output, "User john logged in from 192.168.1.1") {
		t.Error("Formatted message not found in output")
	}
}

// TestLoggerRotation tests log rotation functionality (basic test)
func TestLoggerConfig(t *testing.T) {
	// Test valid configuration
	_, err := NewLogger(Config{
		Level:          LogLevelDebug,
		IsDev:          true,
		LogDir:         t.TempDir(), // Use temp dir for testing
		MaxAgeDays:     7,
		MaxSizeMB:      10,
		MaxBackups:     3,
		AlsoLogToConsole: false,
	})
	
	if err != nil {
		t.Errorf("Failed to create logger with valid config: %v", err)
	}
}

// Test initialization
func TestLoggerInitialization(t *testing.T) {
	// Reset global logger
	globalLogger = nil
	
	// Initialize with custom config
	err := Initialize(Config{
		Level:          LogLevelDebug,
		IsDev:          true,
		LogDir:         t.TempDir(),
		MaxAgeDays:     7,
		MaxSizeMB:      10,
		MaxBackups:     3,
		AlsoLogToConsole: false,
	})
	
	if err != nil {
		t.Errorf("Failed to initialize logger: %v", err)
	}
	
	// Get logger
	logger := Get()
	if logger == nil {
		t.Error("Failed to get global logger")
	}
	
	// Should be able to log without errors
	logger.Info("Test message")
}

// Test context logger
type mockGinContext struct {
	headers map[string]string
	values  map[string]interface{}
}

func newMockGinContext() *mockGinContext {
	return &mockGinContext{
		headers: make(map[string]string),
		values:  make(map[string]interface{}),
	}
}

func (c *mockGinContext) GetHeader(key string) string {
	return c.headers[key]
}

func (c *mockGinContext) Header(key, value string) {
	c.headers[key] = value
}

func (c *mockGinContext) Get(key string) (interface{}, bool) {
	v, ok := c.values[key]
	return v, ok
}

func (c *mockGinContext) ClientIP() string {
	return "127.0.0.1"
}

func (c *mockGinContext) Set(key string, value interface{}) {
	c.values[key] = value
}

func (c *mockGinContext) Next() {
	// No-op for testing
}

// These additional methods help with testing but aren't used directly by our code
func (c *mockGinContext) Status() int {
	return 200
}

// We don't need the request but the tests expect it
func (c *mockGinContext) Request() interface{} {
	return nil
}

// TestContextLogger tests context logging
func TestContextLogger(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	
	// Create test logger with the buffer as output
	logger := &Logger{
		level:  LogLevelDebug,
		isDev:  false,
		writer: &buf,
	}
	
	// Create mock context
	ctx := newMockGinContext()
	ctx.headers["X-Request-ID"] = "test-request-id"
	ctx.values["userName"] = "testuser"
	
	// Create context logger
	ctxLogger := logger.WithContext(ctx)
	
	// Test log
	ctxLogger.Info("Test message")
	
	// Get output
	output := buf.String()
	
	// Check output
	if !strings.Contains(output, "test-request-id") {
		t.Error("Request ID not found in log output")
	}
	if !strings.Contains(output, "Test message") {
		t.Error("Message not found in log output")
	}
}

// TestGetContextLogger tests the GetContextLogger function
func TestGetContextLogger(t *testing.T) {
	// Create mock context
	ctx := newMockGinContext()
	
	// Test get context logger
	logger := GetContextLogger(ctx)
	
	// Verify logger is not nil
	if logger == nil {
		t.Error("GetContextLogger returned nil")
	}
	
	// Test with logger in context
	mockLogger := NewFallbackLogger()
	ctx.Set("logger", mockLogger)
	
	logger2 := GetContextLogger(ctx)
	
	// Verify returns same logger
	if logger2 != mockLogger {
		t.Error("GetContextLogger did not return context logger")
	}
}