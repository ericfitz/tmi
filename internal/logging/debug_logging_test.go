package logging

import (
	"bytes"
	"strings"
	"testing"
)

// TestMutationLoggerBasicFunctionality tests the basic mutation logging functionality
func TestMutationLoggerBasicFunctionality(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger that writes to our buffer
	logger := &Logger{
		level:  LogLevelInfo,
		isDev:  true,
		writer: &buf,
	}

	mutationLogger := NewMutationLogger(logger)

	// Test mutation attempt logging
	ctx := MutationContext{
		UserID:        "test@example.com",
		UserRole:      "writer",
		SessionID:     "session-123",
		DiagramID:     "diagram-456",
		ThreatModelID: "tm-789",
		CausedBy:      "websocket",
	}

	op := OperationDetails{
		OperationID:   "op-abc",
		OperationType: "add",
		AffectedCells: []string{"cell-1", "cell-2"},
	}

	mutationLogger.LogMutationAttempt(ctx, op)

	// Verify the log output contains expected information
	output := buf.String()
	if !strings.Contains(output, "Mutation operation attempt") {
		t.Error("Expected 'Mutation operation attempt' in log output")
	}
	if !strings.Contains(output, "test@example.com") {
		t.Error("Expected user ID in log output")
	}
	if !strings.Contains(output, "op-abc") {
		t.Error("Expected operation ID in log output")
	}
	if !strings.Contains(output, "add") {
		t.Error("Expected operation type in log output")
	}

	// Test mutation result logging
	buf.Reset()
	result := MutationResult{
		Success:          true,
		StateChanged:     true,
		ConflictDetected: false,
		CellsModified:    []string{"cell-1"},
		SequenceNumber:   12345,
	}

	mutationLogger.LogMutationResult("op-abc", result)

	output = buf.String()
	if !strings.Contains(output, "Mutation operation result") {
		t.Error("Expected 'Mutation operation result' in log output")
	}
	if !strings.Contains(output, "success=true") {
		t.Error("Expected success=true in log output")
	}
	if !strings.Contains(output, "sequence_number=12345") {
		t.Error("Expected sequence number in log output")
	}
}

// TestWebSocketDebugLoggerToggling tests enabling and disabling WebSocket debug logging
func TestWebSocketDebugLoggerToggling(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger that writes to our buffer
	logger := &Logger{
		level:  LogLevelDebug,
		isDev:  true,
		writer: &buf,
	}

	wsLogger := NewWebSocketDebugLogger(logger)

	sessionID := "test-session-123"

	// Initially, logging should be disabled
	if wsLogger.IsSessionLoggingEnabled(sessionID) {
		t.Error("Expected logging to be initially disabled")
	}

	// Enable logging
	wsLogger.EnableSessionLogging(sessionID)

	// Check that logging is now enabled
	if !wsLogger.IsSessionLoggingEnabled(sessionID) {
		t.Error("Expected logging to be enabled after EnableSessionLogging")
	}

	// Verify enable message was logged
	output := buf.String()
	if !strings.Contains(output, "WebSocket debug logging enabled") {
		t.Error("Expected enable message in log output")
	}
	if !strings.Contains(output, sessionID) {
		t.Error("Expected session ID in enable message")
	}

	// Test message logging when enabled
	buf.Reset()
	testMessage := []byte(`{"message_type": "diagram_operation", "user_id": "test@example.com"}`)
	wsLogger.LogMessage(sessionID, "test@example.com", "inbound", testMessage)

	output = buf.String()
	if !strings.Contains(output, "WebSocket message") {
		t.Error("Expected 'WebSocket message' in log output when logging is enabled")
	}
	if !strings.Contains(output, "inbound") {
		t.Error("Expected direction in log output")
	}
	if !strings.Contains(output, "diagram_operation") {
		t.Error("Expected message content in log output")
	}

	// Disable logging
	buf.Reset()
	wsLogger.DisableSessionLogging(sessionID)

	// Check that logging is now disabled
	if wsLogger.IsSessionLoggingEnabled(sessionID) {
		t.Error("Expected logging to be disabled after DisableSessionLogging")
	}

	// Verify disable message was logged
	output = buf.String()
	if !strings.Contains(output, "WebSocket debug logging disabled") {
		t.Error("Expected disable message in log output")
	}

	// Test that messages are not logged when disabled
	buf.Reset()
	wsLogger.LogMessage(sessionID, "test@example.com", "outbound", testMessage)

	output = buf.String()
	if strings.Contains(output, "WebSocket message") {
		t.Error("Expected no message logging when logging is disabled")
	}
}

// TestWebSocketDebugLoggerMultipleSessions tests handling multiple sessions
func TestWebSocketDebugLoggerMultipleSessions(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger that writes to our buffer
	logger := &Logger{
		level:  LogLevelDebug,
		isDev:  true,
		writer: &buf,
	}

	wsLogger := NewWebSocketDebugLogger(logger)

	session1 := "session-1"
	session2 := "session-2"
	session3 := "session-3"

	// Enable logging for sessions 1 and 3
	wsLogger.EnableSessionLogging(session1)
	wsLogger.EnableSessionLogging(session3)

	// Check enabled sessions
	enabledSessions := wsLogger.GetEnabledSessions()
	if len(enabledSessions) != 2 {
		t.Errorf("Expected 2 enabled sessions, got %d", len(enabledSessions))
	}

	// Verify correct sessions are enabled
	if !contains(enabledSessions, session1) {
		t.Error("Expected session1 to be enabled")
	}
	if contains(enabledSessions, session2) {
		t.Error("Expected session2 to be disabled")
	}
	if !contains(enabledSessions, session3) {
		t.Error("Expected session3 to be enabled")
	}

	// Clear all sessions
	wsLogger.ClearAllSessions()

	// Verify all sessions are disabled
	if wsLogger.IsSessionLoggingEnabled(session1) {
		t.Error("Expected session1 to be disabled after clear")
	}
	if wsLogger.IsSessionLoggingEnabled(session3) {
		t.Error("Expected session3 to be disabled after clear")
	}

	enabledSessions = wsLogger.GetEnabledSessions()
	if len(enabledSessions) != 0 {
		t.Errorf("Expected 0 enabled sessions after clear, got %d", len(enabledSessions))
	}
}

// TestMutationLoggerErrorHandling tests error handling in mutation logging
func TestMutationLoggerErrorHandling(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger that writes to our buffer
	logger := &Logger{
		level:  LogLevelWarn,
		isDev:  true,
		writer: &buf,
	}

	mutationLogger := NewMutationLogger(logger)

	// Test validation failure logging
	ctx := MutationContext{
		UserID:    "test@example.com",
		SessionID: "session-123",
		DiagramID: "diagram-456",
	}

	op := OperationDetails{
		OperationID:   "op-invalid",
		OperationType: "invalid",
	}

	mutationLogger.LogValidationFailure(ctx, op, "invalid operation type")

	output := buf.String()
	if !strings.Contains(output, "Mutation validation failed") {
		t.Error("Expected 'Mutation validation failed' in log output")
	}
	if !strings.Contains(output, "invalid operation type") {
		t.Error("Expected failure reason in log output")
	}

	// Test authorization failure logging
	buf.Reset()
	mutationLogger.LogAuthorizationFailure(ctx, op, "insufficient permissions")

	output = buf.String()
	if !strings.Contains(output, "Mutation authorization failed") {
		t.Error("Expected 'Mutation authorization failed' in log output")
	}
	if !strings.Contains(output, "insufficient permissions") {
		t.Error("Expected failure reason in log output")
	}
}

// TestLogLevelFiltering tests that logs are filtered by level
func TestLogLevelFiltering(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger with WARN level (should filter out DEBUG)
	logger := &Logger{
		level:  LogLevelWarn,
		isDev:  true,
		writer: &buf,
	}

	wsLogger := NewWebSocketDebugLogger(logger)
	sessionID := "test-session"

	// Enable debug logging for session
	wsLogger.EnableSessionLogging(sessionID)

	// Try to log a debug message (should be filtered out due to WARN level)
	buf.Reset()
	wsLogger.LogMessage(sessionID, "test@example.com", "inbound", []byte("test message"))

	output := buf.String()
	if strings.Contains(output, "WebSocket message") {
		t.Error("Expected debug message to be filtered out at WARN level")
	}

	// Try to log an error (should appear at WARN level)
	wsLogger.LogError(sessionID, "test@example.com", "test_error", nil, "test context")

	output = buf.String()
	if !strings.Contains(output, "WebSocket error") {
		t.Error("Expected error message to appear at WARN level")
	}
}

// Helper function to check if slice contains string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// TestLoggingIntegration tests integration with the existing logging system
func TestLoggingIntegration(t *testing.T) {
	// Test that GetMutationLogger and GetWebSocketDebugLogger work
	mutationLogger := GetMutationLogger()
	if mutationLogger == nil {
		t.Fatal("Expected GetMutationLogger to return non-nil logger")
	}

	wsLogger := GetWebSocketDebugLogger()
	if wsLogger == nil {
		t.Fatal("Expected GetWebSocketDebugLogger to return non-nil logger")
	}

	// Test that they use the global logger
	globalLogger := Get()
	if mutationLogger.logger != globalLogger {
		t.Error("Expected mutation logger to use global logger")
	}
	if wsLogger.logger != globalLogger {
		t.Error("Expected WebSocket debug logger to use global logger")
	}
}
