package slogging

import (
	"context"
	"encoding/json"
	"log/slog"
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
		logger.slogger.Debug("WebSocket message truncated due to size",
			slog.String("direction", string(direction)),
			slog.String("user_id", userID),
			slog.String("session_id", sessionID),
			slog.String("message_type", messageType),
			slog.Int("size_bytes", len(data)),
			slog.Bool("truncated", true),
			slog.String("reason", "too large"),
		)
		return
	}

	// Convert data to string for logging
	messageStr := string(data)

	// Apply token redaction if enabled
	if config.RedactTokens {
		messageStr = RedactWebSocketMessage(messageStr)
	}

	// Try to parse as JSON for better formatting
	var messageData interface{}
	if json.Unmarshal(data, &messageData) == nil {
		// Successfully parsed as JSON, log with structured data
		logger.slogger.Debug("WebSocket message",
			slog.String("direction", string(direction)),
			slog.String("user_id", userID),
			slog.String("session_id", sessionID),
			slog.String("message_type", messageType),
			slog.Int("size_bytes", len(data)),
			slog.Any("message_data", messageData),
		)
	} else {
		// Not valid JSON, log as string
		logger.slogger.Debug("WebSocket message",
			slog.String("direction", string(direction)),
			slog.String("user_id", userID),
			slog.String("session_id", sessionID),
			slog.String("message_type", messageType),
			slog.Int("size_bytes", len(data)),
			slog.String("message_content", messageStr),
		)
	}
}

// RedactWebSocketMessage applies redaction rules to WebSocket message content
func RedactWebSocketMessage(message string) string {
	if message == "" {
		return message
	}

	// Try to parse as JSON and redact structured data
	var messageData map[string]interface{}
	if err := json.Unmarshal([]byte(message), &messageData); err == nil {
		// Successfully parsed as JSON, apply redaction to fields
		redactedData := redactJSONData(messageData)
		if redactedBytes, err := json.Marshal(redactedData); err == nil {
			return string(redactedBytes)
		}
	}

	// If not JSON or failed to process as JSON, apply string-based redaction
	return RedactSensitiveInfo(message)
}

// redactJSONData recursively applies redaction rules to JSON data
func redactJSONData(data map[string]interface{}) map[string]interface{} {
	config := DefaultRedactionConfig()
	if err := config.CompileRules(); err != nil {
		return data // Return original if compilation fails
	}

	result := make(map[string]interface{})

	for key, value := range data {
		// Check if this field should be redacted
		shouldRedact := false
		var redactionAction RedactionAction

		for _, rule := range config.Rules {
			if rule.compiledPattern != nil && rule.compiledPattern.MatchString(key) {
				shouldRedact = true
				redactionAction = rule.Action
				break
			}
		}

		if shouldRedact {
			switch redactionAction {
			case RedactionOmit:
				// Skip this field entirely
				continue
			case RedactionObfuscate:
				result[key] = "[REDACTED]"
			case RedactionPartial:
				if strValue, ok := value.(string); ok {
					result[key] = partialRedactValue(strValue)
				} else {
					result[key] = "[REDACTED]"
				}
			default:
				result[key] = value
			}
		} else {
			// Recursively process nested objects
			if nestedMap, ok := value.(map[string]interface{}); ok {
				result[key] = redactJSONData(nestedMap)
			} else if nestedArray, ok := value.([]interface{}); ok {
				result[key] = redactJSONArray(nestedArray)
			} else {
				result[key] = value
			}
		}
	}

	return result
}

// redactJSONArray recursively applies redaction rules to JSON arrays
func redactJSONArray(data []interface{}) []interface{} {
	result := make([]interface{}, len(data))

	for i, item := range data {
		if nestedMap, ok := item.(map[string]interface{}); ok {
			result[i] = redactJSONData(nestedMap)
		} else if nestedArray, ok := item.([]interface{}); ok {
			result[i] = redactJSONArray(nestedArray)
		} else {
			result[i] = item
		}
	}

	return result
}

// LogWebSocketConnection logs WebSocket connection events (compatibility with original signature)
func LogWebSocketConnection(event string, sessionID, userID, diagramID string, config WebSocketLoggingConfig) {
	if !config.Enabled {
		return
	}

	logger := Get()

	attrs := []slog.Attr{
		slog.String("event", event),
		slog.String("session_id", sessionID),
		slog.String("user_id", userID),
		slog.String("diagram_id", diagramID),
	}

	logger.slogger.LogAttrs(context.TODO(), slog.LevelInfo, "WebSocket connection event", attrs...)
}

// LogWebSocketError logs WebSocket-related errors (compatibility with original signature)
func LogWebSocketError(errorType, errorMessage, sessionID, userID string, config WebSocketLoggingConfig) {
	if !config.Enabled {
		return
	}

	logger := Get()

	logger.slogger.Error("WebSocket error",
		slog.String("error_type", errorType),
		slog.String("error_message", errorMessage),
		slog.String("session_id", sessionID),
		slog.String("user_id", userID),
	)
}

// LogWebSocketMetrics logs WebSocket performance metrics
func LogWebSocketMetrics(sessionID string, metrics map[string]interface{}) {
	logger := Get()

	attrs := []slog.Attr{
		slog.String("session_id", sessionID),
		slog.String("metric_type", "websocket_performance"),
	}

	// Add metrics as attributes
	for key, value := range metrics {
		attrs = append(attrs, slog.Any(key, value))
	}

	logger.slogger.LogAttrs(context.TODO(), slog.LevelDebug, "WebSocket metrics", attrs...)
}
