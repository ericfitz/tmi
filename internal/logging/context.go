package logging

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

// FallbackLogger provides a simple logger that writes to gin's output
type FallbackLogger struct{}

// Debug logs debug level messages
func (l *FallbackLogger) Debug(format string, args ...interface{}) {
	gin.DefaultWriter.Write([]byte(fmt.Sprintf("[DEBUG] "+format+"\n", args...)))
}

// Info logs info level messages
func (l *FallbackLogger) Info(format string, args ...interface{}) {
	gin.DefaultWriter.Write([]byte(fmt.Sprintf("[INFO] "+format+"\n", args...)))
}

// Warn logs warning level messages
func (l *FallbackLogger) Warn(format string, args ...interface{}) {
	gin.DefaultWriter.Write([]byte(fmt.Sprintf("[WARN] "+format+"\n", args...)))
}

// Error logs error level messages
func (l *FallbackLogger) Error(format string, args ...interface{}) {
	gin.DefaultErrorWriter.Write([]byte(fmt.Sprintf("[ERROR] "+format+"\n", args...)))
}

// NewFallbackLogger creates a simple logger for fallback use
func NewFallbackLogger() SimpleLogger {
	return &FallbackLogger{}
}

// GinContextLike defines a minimal interface for contexts that can be used with the logger
type GinContextLike interface {
	Get(key string) (interface{}, bool)
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
