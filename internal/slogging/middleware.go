package slogging

import (
	"log/slog"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// LoggerMiddleware returns a Gin middleware for logging requests using slog
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger
		logger := Get().WithContext(c)

		// Store logger in context for handlers to use
		c.Set("logger", logger)

		// Check if request is authenticated
		userName, hasUser := c.Get("userName")
		isAuthenticated := hasUser && userName != nil && userName != ""

		// Skip logging if configured to suppress unauthenticated logs
		if Get().suppressUnauthenticatedLogs && !isAuthenticated {
			// Still process the request, just don't log
			c.Next()
			return
		}

		// Log request start
		logger.DebugCtx("Request started",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.String("user_agent", c.GetHeader("User-Agent")),
		)

		// Process request
		start := time.Now()
		c.Next()

		// Calculate duration
		latency := time.Since(start)

		// Get status from gin context
		var statusCode int
		if w, ok := c.Writer.(interface{ Status() int }); ok {
			statusCode = w.Status()
		} else {
			statusCode = 0 // Unknown
		}

		// Log request completion based on status code
		logAttrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status_code", statusCode),
			slog.Duration("duration", latency),
			slog.Int64("response_size", int64(c.Writer.Size())),
		}

		switch {
		case statusCode >= 500:
			logger.ErrorCtx("Request completed with server error", logAttrs...)
		case statusCode >= 400:
			logger.WarnCtx("Request completed with client error", logAttrs...)
		default:
			logger.InfoCtx("Request completed successfully", logAttrs...)
		}
	}
}

// Recoverer creates middleware for recovering from panics using slog
func Recoverer() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Get logger from context or create one
				var logger *ContextLogger
				loggerInterface, exists := c.Get("logger")
				if exists {
					logger = loggerInterface.(*ContextLogger)
				} else {
					logger = Get().WithContext(c)
				}

				// Get stack trace
				buf := make([]byte, 2048)
				n := runtime.Stack(buf, false)
				stackTrace := string(buf[:n])

				// Log error with stack trace
				logger.ErrorCtx("Panic recovered",
					slog.Any("panic_value", err),
					slog.String("stack_trace", stackTrace),
					slog.String("method", c.Request.Method),
					slog.String("path", c.Request.URL.Path),
				)

				// Return error to client
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	}
}

// RequestTracingMiddleware adds detailed request tracing for debugging
func RequestTracingMiddleware(enableBodyLogging bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := Get().WithContext(c)

		// Log detailed request information
		if logger.logger.level <= LogLevelDebug {
			attrs := []slog.Attr{
				slog.String("method", c.Request.Method),
				slog.String("url", c.Request.URL.String()),
				slog.String("remote_addr", c.Request.RemoteAddr),
				slog.String("user_agent", c.GetHeader("User-Agent")),
				slog.Int64("content_length", c.Request.ContentLength),
			}

			// Add headers (with redaction)
			headers := make(map[string]string)
			for name, values := range c.Request.Header {
				if len(values) > 0 {
					headers[name] = values[0] // Log first value only
				}
			}
			attrs = append(attrs, slog.Any("headers", headers))

			// Log query parameters
			if len(c.Request.URL.RawQuery) > 0 {
				attrs = append(attrs, slog.String("query", c.Request.URL.RawQuery))
			}

			logger.DebugCtx("Detailed request trace", attrs...)
		}

		c.Next()
	}
}

// PerformanceMiddleware logs performance metrics for requests
func PerformanceMiddleware(slowRequestThreshold time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start)

		// Log slow requests
		if duration > slowRequestThreshold {
			logger := Get().WithContext(c)

			statusCode := c.Writer.Status()

			logger.WarnCtx("Slow request detected",
				slog.String("method", c.Request.Method),
				slog.String("path", c.Request.URL.Path),
				slog.Duration("duration", duration),
				slog.Duration("threshold", slowRequestThreshold),
				slog.Int("status_code", statusCode),
				slog.Int64("response_size", int64(c.Writer.Size())),
			)
		}
	}
}

// StructuredLogHandler provides a Gin handler for structured logging endpoints
func StructuredLogHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var logRequest struct {
			Level   string                 `json:"level"`
			Message string                 `json:"message"`
			Data    map[string]interface{} `json:"data,omitempty"`
		}

		if err := c.ShouldBindJSON(&logRequest); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON format"})
			return
		}

		logger := Get().WithContext(c)

		// Convert data to slog attributes
		attrs := make([]slog.Attr, 0, len(logRequest.Data))
		for key, value := range logRequest.Data {
			attrs = append(attrs, slog.Any(key, value))
		}

		// Log at the specified level
		switch logRequest.Level {
		case "debug":
			logger.DebugCtx(logRequest.Message, attrs...)
		case "info":
			logger.InfoCtx(logRequest.Message, attrs...)
		case "warn":
			logger.WarnCtx(logRequest.Message, attrs...)
		case "error":
			logger.ErrorCtx(logRequest.Message, attrs...)
		default:
			logger.InfoCtx(logRequest.Message, attrs...)
		}

		c.JSON(200, gin.H{"status": "logged"})
	}
}
