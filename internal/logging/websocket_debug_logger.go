package logging

import (
	"sync"
	"time"
)

// WebSocketDebugLogger provides toggleable WebSocket message logging for debugging
type WebSocketDebugLogger struct {
	enabled map[string]bool // sessionID -> enabled
	logger  *Logger
	mutex   sync.RWMutex
}

// NewWebSocketDebugLogger creates a new WebSocket debug logger
func NewWebSocketDebugLogger(logger *Logger) *WebSocketDebugLogger {
	return &WebSocketDebugLogger{
		enabled: make(map[string]bool),
		logger:  logger,
	}
}

// EnableSessionLogging enables debug logging for a specific session
func (wsl *WebSocketDebugLogger) EnableSessionLogging(sessionID string) {
	wsl.mutex.Lock()
	defer wsl.mutex.Unlock()
	wsl.enabled[sessionID] = true
	wsl.logger.Info("WebSocket debug logging enabled - session_id=%s", sessionID)
}

// DisableSessionLogging disables debug logging for a specific session
func (wsl *WebSocketDebugLogger) DisableSessionLogging(sessionID string) {
	wsl.mutex.Lock()
	defer wsl.mutex.Unlock()
	delete(wsl.enabled, sessionID)
	wsl.logger.Info("WebSocket debug logging disabled - session_id=%s", sessionID)
}

// IsSessionLoggingEnabled checks if logging is enabled for a session
func (wsl *WebSocketDebugLogger) IsSessionLoggingEnabled(sessionID string) bool {
	wsl.mutex.RLock()
	defer wsl.mutex.RUnlock()
	return wsl.enabled[sessionID]
}

// LogMessage logs a WebSocket message if debug logging is enabled for the session
func (wsl *WebSocketDebugLogger) LogMessage(sessionID, userID, direction string, message []byte) {
	wsl.mutex.RLock()
	enabled := wsl.enabled[sessionID]
	wsl.mutex.RUnlock()

	if !enabled {
		return
	}

	timestamp := time.Now().UTC()
	wsl.logger.Debug("WebSocket message - session_id=%s user_id=%s direction=%s message_size=%d message_content=%s timestamp=%s",
		sessionID,
		userID,
		direction, // "inbound" or "outbound"
		len(message),
		string(message),
		timestamp.Format(time.RFC3339),
	)
}

// LogConnection logs WebSocket connection events
func (wsl *WebSocketDebugLogger) LogConnection(sessionID, userID, event string, details map[string]interface{}) {
	wsl.mutex.RLock()
	enabled := wsl.enabled[sessionID]
	wsl.mutex.RUnlock()

	if !enabled {
		return
	}

	detailsStr := ""
	// Convert details to string representation
	for key, value := range details {
		if detailsStr != "" {
			detailsStr += " "
		}
		detailsStr += key + "=" + formatValue(value)
	}

	wsl.logger.Debug("WebSocket connection event - session_id=%s user_id=%s event=%s details=%s",
		sessionID,
		userID,
		event, // "connect", "disconnect", "error", "timeout"
		detailsStr,
	)
}

// LogOperationProcessing logs operation processing details
func (wsl *WebSocketDebugLogger) LogOperationProcessing(sessionID, userID, operationID string, stage string, details string) {
	wsl.mutex.RLock()
	enabled := wsl.enabled[sessionID]
	wsl.mutex.RUnlock()

	if !enabled {
		return
	}

	wsl.logger.Debug("WebSocket operation processing - session_id=%s user_id=%s operation_id=%s stage=%s details=%s",
		sessionID,
		userID,
		operationID,
		stage, // "received", "validating", "validated", "applying", "applied", "broadcasting", "broadcast"
		details,
	)
}

// LogBroadcast logs message broadcast details
func (wsl *WebSocketDebugLogger) LogBroadcast(sessionID string, messageType string, recipientCount int, messageSize int) {
	// Check if any sessions have logging enabled for broadcast logging
	wsl.mutex.RLock()
	hasEnabledSessions := len(wsl.enabled) > 0
	sessionEnabled := wsl.enabled[sessionID]
	wsl.mutex.RUnlock()

	if !hasEnabledSessions && !sessionEnabled {
		return
	}

	wsl.logger.Debug("WebSocket broadcast - session_id=%s message_type=%s recipient_count=%d message_size=%d",
		sessionID,
		messageType,
		recipientCount,
		messageSize,
	)
}

// LogError logs WebSocket-related errors
func (wsl *WebSocketDebugLogger) LogError(sessionID, userID string, errorType string, error error, context string) {
	wsl.mutex.RLock()
	enabled := wsl.enabled[sessionID]
	wsl.mutex.RUnlock()

	if !enabled {
		return
	}

	errorStr := ""
	if error != nil {
		errorStr = error.Error()
	}

	wsl.logger.Error("WebSocket error - session_id=%s user_id=%s error_type=%s error=%s context=%s",
		sessionID,
		userID,
		errorType, // "read_error", "write_error", "parse_error", "validation_error"
		errorStr,
		context,
	)
}

// GetEnabledSessions returns a list of session IDs with debug logging enabled
func (wsl *WebSocketDebugLogger) GetEnabledSessions() []string {
	wsl.mutex.RLock()
	defer wsl.mutex.RUnlock()

	sessions := make([]string, 0, len(wsl.enabled))
	for sessionID := range wsl.enabled {
		sessions = append(sessions, sessionID)
	}
	return sessions
}

// ClearAllSessions disables debug logging for all sessions
func (wsl *WebSocketDebugLogger) ClearAllSessions() {
	wsl.mutex.Lock()
	defer wsl.mutex.Unlock()

	sessionCount := len(wsl.enabled)
	wsl.enabled = make(map[string]bool)
	wsl.logger.Info("WebSocket debug logging cleared for all sessions - disabled_count=%d", sessionCount)
}

// formatValue converts interface{} to string for logging
func formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case int, int32, int64:
		return "numeric"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return "unknown"
	}
}

// GetWebSocketDebugLogger returns a global WebSocket debug logger instance
func GetWebSocketDebugLogger() *WebSocketDebugLogger {
	return NewWebSocketDebugLogger(Get())
}
