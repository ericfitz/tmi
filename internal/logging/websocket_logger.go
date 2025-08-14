package logging

import (
	"encoding/json"
	"strings"
	"time"
)

// WebSocketLoggingConfig holds configuration for WebSocket message logging
type WebSocketLoggingConfig struct {
	Enabled        bool
	RedactTokens   bool
	MaxMessageSize int64 // Max message size to log (in bytes)
	OnlyDebugLevel bool  // Only log at debug level
}

// WSMessageDirection indicates the direction of the WebSocket message
type WSMessageDirection string

const (
	WSMessageInbound  WSMessageDirection = "INBOUND"
	WSMessageOutbound WSMessageDirection = "OUTBOUND"
)

// LogWebSocketMessage logs WebSocket messages with optional token redaction
func LogWebSocketMessage(direction WSMessageDirection, sessionID, userID string, messageType string, data []byte, config WebSocketLoggingConfig) {
	if !config.Enabled {
		return
	}

	logger := Get()

	// Only proceed if we're logging at debug level and config allows it
	if config.OnlyDebugLevel && logger.level > LogLevelDebug {
		return
	}

	// Check message size limits
	if config.MaxMessageSize > 0 && int64(len(data)) > config.MaxMessageSize {
		logger.Debug("WebSocket %s message from %s in session %s - Type: %s, Size: %d bytes [TRUNCATED - too large]",
			direction, userID, sessionID, messageType, len(data))
		return
	}

	// Convert data to string for logging
	messageStr := string(data)

	// Apply token redaction if enabled
	if config.RedactTokens {
		messageStr = RedactWebSocketMessage(messageStr)
	}

	// Format and log the message
	timestamp := time.Now().Format(time.RFC3339Nano)
	logger.Debug("WebSocket %s [%s] Session: %s, User: %s, Type: %s\nMessage: %s",
		direction, timestamp, sessionID, userID, messageType, messageStr)
}

// RedactWebSocketMessage redacts sensitive information from WebSocket messages
func RedactWebSocketMessage(message string) string {
	if message == "" {
		return message
	}

	// Try to parse as JSON to redact specific fields
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(message), &jsonData); err == nil {
		// Successfully parsed as JSON, redact sensitive fields
		redactJSONFields(jsonData)

		// Convert back to JSON string
		if redactedBytes, err := json.Marshal(jsonData); err == nil {
			return string(redactedBytes)
		}
	}

	// Fallback to string-based redaction
	return RedactSensitiveInfo(message)
}

// redactJSONFields recursively redacts sensitive fields in JSON data
func redactJSONFields(data interface{}) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			lowerKey := strings.ToLower(key)
			// Check for sensitive field names
			if isSensitiveField(lowerKey) {
				v[key] = "[REDACTED]"
			} else if strValue, ok := value.(string); ok {
				// Check string values for embedded tokens
				v[key] = RedactSensitiveInfo(strValue)
			} else {
				// Recursively process nested objects/arrays
				redactJSONFields(value)
			}
		}
	case []interface{}:
		for i, item := range v {
			if strValue, ok := item.(string); ok {
				v[i] = RedactSensitiveInfo(strValue)
			} else {
				redactJSONFields(item)
			}
		}
	}
}

// isSensitiveField checks if a field name indicates sensitive data
func isSensitiveField(fieldName string) bool {
	sensitiveFields := map[string]bool{
		"token":         true,
		"auth":          true,
		"authorization": true,
		"bearer":        true,
		"password":      true,
		"secret":        true,
		"key":           true,
		"api_key":       true,
		"apikey":        true,
		"access_token":  true,
		"refresh_token": true,
		"jwt":           true,
		"session":       true,
		"sessionid":     true,
		"session_id":    true,
		"csrf":          true,
		"xsrf":          true,
	}

	return sensitiveFields[fieldName]
}

// LogWebSocketConnection logs WebSocket connection events
func LogWebSocketConnection(event string, sessionID, userID, diagramID string, config WebSocketLoggingConfig) {
	if !config.Enabled {
		return
	}

	logger := Get()

	// Only proceed if we're logging at debug level and config allows it
	if config.OnlyDebugLevel && logger.level > LogLevelDebug {
		return
	}

	timestamp := time.Now().Format(time.RFC3339Nano)
	logger.Debug("WebSocket %s [%s] Session: %s, User: %s, Diagram: %s",
		event, timestamp, sessionID, userID, diagramID)
}

// LogWebSocketError logs WebSocket-related errors
func LogWebSocketError(errorType, errorMessage, sessionID, userID string, config WebSocketLoggingConfig) {
	if !config.Enabled {
		return
	}

	logger := Get()

	// Apply redaction to error message if enabled
	if config.RedactTokens {
		errorMessage = RedactSensitiveInfo(errorMessage)
	}

	timestamp := time.Now().Format(time.RFC3339Nano)
	logger.Error("WebSocket ERROR [%s] Type: %s, Session: %s, User: %s, Message: %s",
		timestamp, errorType, sessionID, userID, errorMessage)
}

// DefaultWebSocketConfig returns a sensible default configuration for WebSocket logging
func DefaultWebSocketConfig() WebSocketLoggingConfig {
	return WebSocketLoggingConfig{
		Enabled:        true,
		RedactTokens:   true,
		MaxMessageSize: 5 * 1024, // 5KB
		OnlyDebugLevel: true,
	}
}
