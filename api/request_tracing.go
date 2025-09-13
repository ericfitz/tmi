package api

import (
	"bytes"
	"html"
	"io"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// RequestTracingMiddleware provides comprehensive request tracing
func RequestTracingMiddleware() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		logger := logging.Get()

		// Extract request ID from context
		requestID := "unknown"
		if param.Keys != nil {
			if id, exists := param.Keys["request_id"]; exists {
				if idStr, ok := id.(string); ok {
					requestID = idStr
				}
			}
		}

		logger.Info("REQUEST_TRACE [%s] %s %s %d %s %s",
			requestID,
			param.Method,
			param.Path,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
		)

		return "" // Don't return anything for default logging
	})
}

// DetailedRequestLoggingMiddleware logs request details at each stage
func DetailedRequestLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := generateRequestID()
		c.Set("request_id", requestID)

		logger := logging.GetContextLogger(c)

		// Log incoming request with redacted headers
		redactedHeaders := logging.RedactHeaders(c.Request.Header)
		logger.Info("INCOMING_REQUEST [%s] %s %s - Headers: %v",
			requestID, c.Request.Method, c.Request.URL.Path, redactedHeaders)

		// Read and log request body if present
		if c.Request.Body != nil {
			bodyBytes, _ := io.ReadAll(c.Request.Body)
			if len(bodyBytes) > 0 {
				logger.Debug("REQUEST_BODY [%s] %s", requestID, html.EscapeString(string(bodyBytes)))
				// Restore body for further processing
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		// Process request
		c.Next()

		// Log response
		duration := time.Since(start)
		logger.Info("REQUEST_COMPLETE [%s] %s %s -> %d (%v)",
			requestID, c.Request.Method, c.Request.URL.Path, c.Writer.Status(), duration)

		// Log any errors
		if len(c.Errors) > 0 {
			logger.Error("REQUEST_ERRORS [%s] %v", requestID, c.Errors)
		}
	}
}

// RouteMatchingMiddleware logs which routes are being matched
func RouteMatchingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := getRequestID(c)
		logger := logging.GetContextLogger(c)

		logger.Debug("ROUTE_MATCHING [%s] Attempting to match %s %s",
			requestID, c.Request.Method, c.Request.URL.Path)

		c.Next()

		// Log final route match
		if c.HandlerName() != "" {
			logger.Debug("ROUTE_MATCHED [%s] Handler: %s", requestID, c.HandlerName())
		} else {
			logger.Debug("ROUTE_NO_MATCH [%s] No handler found", requestID)
		}
	}
}

// generateRequestID creates a unique request ID
func generateRequestID() string {
	return time.Now().Format("20060102-150405.000000")
}

// getRequestID extracts request ID from context
func getRequestID(c *gin.Context) string {
	if id, exists := c.Get("request_id"); exists {
		if idStr, ok := id.(string); ok {
			return idStr
		}
	}
	return "unknown"
}
