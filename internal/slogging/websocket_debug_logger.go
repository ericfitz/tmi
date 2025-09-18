package slogging

import (
	"log/slog"
	"sync"
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
	wsl.logger.slogger.Info("WebSocket debug logging enabled",
		slog.String("session_id", sessionID))
}

// DisableSessionLogging disables debug logging for a specific session
func (wsl *WebSocketDebugLogger) DisableSessionLogging(sessionID string) {
	wsl.mutex.Lock()
	defer wsl.mutex.Unlock()
	delete(wsl.enabled, sessionID)
	wsl.logger.slogger.Info("WebSocket debug logging disabled",
		slog.String("session_id", sessionID))
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

	// Use structured logging for WebSocket debug messages
	wsl.logger.slogger.Debug("WebSocket Debug Message",
		slog.String("session_id", sessionID),
		slog.String("user_id", userID),
		slog.String("direction", direction),
		slog.Int("message_size", len(message)),
		slog.String("message_content", string(message)),
	)
}

// GetEnabledSessions returns a list of sessions with debug logging enabled
func (wsl *WebSocketDebugLogger) GetEnabledSessions() []string {
	wsl.mutex.RLock()
	defer wsl.mutex.RUnlock()

	sessions := make([]string, 0, len(wsl.enabled))
	for sessionID := range wsl.enabled {
		sessions = append(sessions, sessionID)
	}
	return sessions
}

// DisableAllSessions disables debug logging for all sessions
func (wsl *WebSocketDebugLogger) DisableAllSessions() {
	wsl.mutex.Lock()
	defer wsl.mutex.Unlock()

	sessionCount := len(wsl.enabled)
	wsl.enabled = make(map[string]bool)

	wsl.logger.slogger.Info("WebSocket debug logging disabled for all sessions",
		slog.Int("disabled_sessions_count", sessionCount))
}

// ClearAllSessions is an alias for DisableAllSessions (compatibility)
func (wsl *WebSocketDebugLogger) ClearAllSessions() {
	wsl.DisableAllSessions()
}

// Global WebSocket debug logger instance
var globalWebSocketDebugLogger *WebSocketDebugLogger

// GetWebSocketDebugLogger returns the global WebSocket debug logger
func GetWebSocketDebugLogger() *WebSocketDebugLogger {
	if globalWebSocketDebugLogger == nil {
		globalWebSocketDebugLogger = NewWebSocketDebugLogger(Get())
	}
	return globalWebSocketDebugLogger
}
