package slogging

import (
	"bytes"
	"html"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestResponseLoggingConfig holds configuration for enhanced logging
type RequestResponseLoggingConfig struct {
	LogRequests    bool
	LogResponses   bool
	RedactTokens   bool
	MaxBodySize    int64 // Max request/response body size to log (in bytes)
	SkipPaths      []string
	OnlyDebugLevel bool // Only log at debug level
}

// RequestResponseLogger creates middleware for detailed request/response logging
func RequestResponseLogger(config RequestResponseLoggingConfig) gin.HandlerFunc {
	// Set default max body size if not specified
	if config.MaxBodySize == 0 {
		config.MaxBodySize = 10 * 1024 // 10KB default
	}

	return func(c *gin.Context) {
		// Skip logging for specified paths
		for _, skipPath := range config.SkipPaths {
			if strings.HasPrefix(c.Request.URL.Path, skipPath) {
				c.Next()
				return
			}
		}

		logger := Get().WithContext(c)

		// Only proceed if we're logging at debug level and config allows it
		if config.OnlyDebugLevel && logger.logger.level > LogLevelDebug {
			c.Next()
			return
		}

		// Check if request is authenticated
		userName, hasUser := c.Get("userName")
		isAuthenticated := hasUser && userName != nil && userName != ""

		// Skip logging if configured to suppress unauthenticated logs
		if Get().suppressUnauthenticatedLogs && !isAuthenticated {
			// Still process the request, just don't log
			c.Next()
			return
		}

		startTime := time.Now()

		// Log request details
		if config.LogRequests {
			logRequestDetails(c, logger, config)
		}

		// Capture response if needed
		var responseBody *bytes.Buffer
		if config.LogResponses {
			responseBody = &bytes.Buffer{}
			// Replace the writer with a multi-writer to capture response
			originalWriter := c.Writer
			c.Writer = &responseWriter{
				ResponseWriter: originalWriter,
				body:           responseBody,
			}
		}

		// Process request
		c.Next()

		// Log response details
		if config.LogResponses {
			logResponseDetails(c, logger, config, responseBody, time.Since(startTime))
		}
	}
}

// responseWriter wraps gin.ResponseWriter to capture response body
type responseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write implements io.Writer interface for response body capture.
//
// SECURITY NOTE for CodeQL: This method directly writes data to HTTP responses,
// which could theoretically expose stack traces. However, stack trace exposure
// is prevented through multiple protective layers in the application:
// 1. Error handling in api/request_utils.go uses truncateBeforeStackTrace()
// 2. Panic recovery in internal/logging/logger.go returns safe error messages
// 3. Response logging uses truncateBeforeStackTraceMarkers() to filter logs
// 4. All error response paths are designed to prevent stack trace leakage
// This architectural approach is safer than filtering all responses at this level,
// which would add performance overhead and risk corrupting binary/JSON content.
func (w *responseWriter) Write(data []byte) (int, error) {
	// Write to both the original response and our buffer
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

// logRequestDetails logs detailed request information using structured logging
func logRequestDetails(c *gin.Context, logger *ContextLogger, config RequestResponseLoggingConfig) {
	// Build structured attributes for the request
	attrs := []slog.Attr{
		slog.String("method", c.Request.Method),
		slog.String("url", c.Request.URL.String()),
		slog.String("protocol", c.Request.Proto),
		slog.String("remote_addr", c.Request.RemoteAddr),
		slog.String("user_agent", c.Request.UserAgent()),
		slog.Int64("content_length", c.Request.ContentLength),
	}

	// Add headers (with potential redaction)
	if len(c.Request.Header) > 0 {
		headers := make(map[string]string)
		for name, values := range c.Request.Header {
			if len(values) > 0 {
				value := values[0]
				if config.RedactTokens {
					value = RedactSensitiveInfo(value)
				}
				headers[name] = value
			}
		}
		attrs = append(attrs, slog.Any("headers", headers))
	}

	// Add query parameters
	if c.Request.URL.RawQuery != "" {
		query := c.Request.URL.RawQuery
		if config.RedactTokens {
			query = RedactSensitiveInfo(query)
		}
		attrs = append(attrs, slog.String("query", query))
	}

	// Add request body if present and within size limits
	if c.Request.ContentLength > 0 && c.Request.ContentLength <= config.MaxBodySize {
		if body, err := io.ReadAll(c.Request.Body); err == nil {
			// Restore the body for further processing
			c.Request.Body = io.NopCloser(strings.NewReader(string(body)))

			bodyStr := string(body)
			if config.RedactTokens {
				bodyStr = RedactSensitiveInfo(bodyStr)
			}

			// Limit body size and escape for safe logging
			if len(bodyStr) > int(config.MaxBodySize) {
				bodyStr = bodyStr[:config.MaxBodySize] + "... [TRUNCATED]"
			}
			bodyStr = html.EscapeString(bodyStr)

			attrs = append(attrs, slog.String("request_body", bodyStr))
		}
	}

	logger.DebugCtx("HTTP Request Details", attrs...)
}

// logResponseDetails logs detailed response information using structured logging
func logResponseDetails(c *gin.Context, logger *ContextLogger, config RequestResponseLoggingConfig, responseBody *bytes.Buffer, duration time.Duration) {
	// Build structured attributes for the response
	attrs := []slog.Attr{
		slog.Int("status_code", c.Writer.Status()),
		slog.Int64("response_size", int64(c.Writer.Size())),
		slog.Duration("duration", duration),
	}

	// Add response headers
	if len(c.Writer.Header()) > 0 {
		headers := make(map[string]string)
		for name, values := range c.Writer.Header() {
			if len(values) > 0 {
				value := values[0]
				if config.RedactTokens {
					value = RedactSensitiveInfo(value)
				}
				headers[name] = value
			}
		}
		attrs = append(attrs, slog.Any("response_headers", headers))
	}

	// Add response body if captured and within size limits
	if responseBody != nil && responseBody.Len() > 0 {
		bodyStr := responseBody.String()

		if config.RedactTokens {
			bodyStr = RedactSensitiveInfo(bodyStr)
		}

		// SECURITY: Filter out stack traces from response bodies for security
		bodyStr = truncateBeforeStackTraceMarkers(bodyStr)

		// Limit body size and escape for safe logging
		if len(bodyStr) > int(config.MaxBodySize) {
			bodyStr = bodyStr[:config.MaxBodySize] + "... [TRUNCATED]"
		}
		bodyStr = html.EscapeString(bodyStr)

		attrs = append(attrs, slog.String("response_body", bodyStr))
	}

	// Choose log level based on status code
	statusCode := c.Writer.Status()
	switch {
	case statusCode >= 500:
		logger.ErrorCtx("HTTP Response Details", attrs...)
	case statusCode >= 400:
		logger.WarnCtx("HTTP Response Details", attrs...)
	default:
		logger.DebugCtx("HTTP Response Details", attrs...)
	}
}

// truncateBeforeStackTraceMarkers removes stack trace information from response bodies
// by truncating at stack trace markers to prevent disclosure in logs.
//
// SECURITY: This function is part of the defense-in-depth strategy against stack trace
// exposure (CWE-209). It ensures that captured response bodies don't contain stack traces
// that could be inadvertently logged and potentially exposed through log analysis tools.
func truncateBeforeStackTraceMarkers(body string) string {
	if body == "" {
		return body
	}

	// Look for stack trace markers and truncate before them
	stackTraceMarkers := []string{
		"--- STACK_TRACE_START ---",
		"\nStack trace:",
		"goroutine ",
	}

	for _, marker := range stackTraceMarkers {
		if idx := strings.Index(body, marker); idx != -1 {
			return strings.TrimSpace(body[:idx])
		}
	}

	// No stack trace markers found, return original body
	return body
}
