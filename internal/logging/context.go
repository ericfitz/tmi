package logging

import (
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
)

// FallbackLogger provides a simple logger that writes to gin's output
type FallbackLogger struct{}

// Debug logs debug level messages
func (l *FallbackLogger) Debug(format string, args ...interface{}) {
	_, err := fmt.Fprintf(gin.DefaultWriter, "[DEBUG] "+format+"\n", args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing debug log: %v\n", err)
	}
}

// Info logs info level messages
func (l *FallbackLogger) Info(format string, args ...interface{}) {
	_, err := fmt.Fprintf(gin.DefaultWriter, "[INFO] "+format+"\n", args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing info log: %v\n", err)
	}
}

// Warn logs warning level messages
func (l *FallbackLogger) Warn(format string, args ...interface{}) {
	_, err := fmt.Fprintf(gin.DefaultWriter, "[WARN] "+format+"\n", args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing warning log: %v\n", err)
	}
}

// Error logs error level messages
func (l *FallbackLogger) Error(format string, args ...interface{}) {
	_, err := fmt.Fprintf(gin.DefaultErrorWriter, "[ERROR] "+format+"\n", args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing error log: %v\n", err)
	}
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
