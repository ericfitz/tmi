package api

import (
	"bytes"
	"html"
	"io"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// RequestTracingMiddleware provides comprehensive request tracing
// SEM@dff4dd105825de9e0bddf30a1b4cfc72f3acc18d: log method, path, status, latency, and request ID for every HTTP request
func RequestTracingMiddleware() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		logger := slogging.Get()

		// Extract request ID from context
		requestID := string(ComponentHealthStatusUnknown)
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
// SEM@053baa340d412aa135be32953dfcb6133af89b4d: log incoming request headers, body, and completion status with a unique request ID
func DetailedRequestLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := generateRequestID()
		c.Set("request_id", requestID)

		logger := slogging.GetContextLogger(c)

		// Log incoming request with redacted headers and path.
		// Redact the token segment from pending-link paths to avoid logging high-entropy
		// one-time tokens that are equivalent to credentials.
		redactedHeaders := slogging.RedactHeaders(c.Request.Header)
		logPath := redactPendingLinkPath(c.Request.URL.Path)
		logger.Info("INCOMING_REQUEST [%s] %s %s - Headers: %v",
			requestID, c.Request.Method, logPath, redactedHeaders)

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
			requestID, c.Request.Method, logPath, c.Writer.Status(), duration)

		// Log any errors
		if len(c.Errors) > 0 {
			logger.Error("REQUEST_ERRORS [%s] %v", requestID, c.Errors)
		}
	}
}

// RouteMatchingMiddleware logs which routes are being matched
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: log which route handler is matched for each incoming request
func RouteMatchingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := getRequestID(c)
		logger := slogging.GetContextLogger(c)

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

// redactPendingLinkPath replaces the token segment in
// /me/identities/link/pending/{token} with "(redacted)" so that
// high-entropy one-time link tokens never appear in log output.
// SEM@053baa340d412aa135be32953dfcb6133af89b4d: replace the one-time token segment in a pending-link URL path with a redacted placeholder (pure)
func redactPendingLinkPath(path string) string {
	const prefix = "/me/identities/link/pending/"
	if strings.HasPrefix(path, prefix) {
		return prefix + "(redacted)"
	}
	return path
}

// generateRequestID creates a unique request ID
// SEM@19582bedf42bd97ae1b96ae801dffb9ef13e920d: build a timestamp-based unique request ID string (pure)
func generateRequestID() string {
	return time.Now().Format("20060102-150405.000000")
}

// getRequestID extracts request ID from context
// SEM@dff4dd105825de9e0bddf30a1b4cfc72f3acc18d: extract the request ID string from the Gin context (pure)
func getRequestID(c *gin.Context) string {
	if id, exists := c.Get("request_id"); exists {
		if idStr, ok := id.(string); ok {
			return idStr
		}
	}
	return string(ComponentHealthStatusUnknown)
}
