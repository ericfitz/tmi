package logging

import (
	"bytes"
	"fmt"
	"io"
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

func (w *responseWriter) Write(data []byte) (int, error) {
	// Write to both the original response and our buffer
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

func logRequestDetails(c *gin.Context, logger *ContextLogger, config RequestResponseLoggingConfig) {
	req := c.Request

	// Build request info
	requestInfo := fmt.Sprintf("REQUEST %s %s", req.Method, req.URL.Path)
	if req.URL.RawQuery != "" {
		if config.RedactTokens {
			requestInfo += "?" + RedactSensitiveInfo(req.URL.RawQuery)
		} else {
			requestInfo += "?" + req.URL.RawQuery
		}
	}

	// Log basic request info
	logger.Debug("%s", requestInfo)

	// Log headers
	if len(req.Header) > 0 {
		headers := req.Header
		if config.RedactTokens {
			headers = RedactHeaders(req.Header)
		}

		// Log headers as a single line map
		logger.Debug("REQUEST Headers: %v", headers)
	}

	// Log request body if present and not too large
	if req.Body != nil && req.ContentLength > 0 && req.ContentLength <= config.MaxBodySize {
		bodyBytes, err := io.ReadAll(req.Body)
		if err == nil && len(bodyBytes) > 0 {
			// Restore the body for the actual handler
			req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			bodyStr := string(bodyBytes)
			if config.RedactTokens {
				bodyStr = RedactSensitiveInfo(bodyStr)
			}

			logger.Debug("REQUEST Body: %s", bodyStr)
		}
	}
}

func logResponseDetails(c *gin.Context, logger *ContextLogger, config RequestResponseLoggingConfig, responseBody *bytes.Buffer, duration time.Duration) {
	status := c.Writer.Status()

	// Log basic response info
	logger.Debug("RESPONSE %s %s - %d (%v)", c.Request.Method, c.Request.URL.Path, status, duration)

	// Log response headers
	headers := c.Writer.Header()
	if len(headers) > 0 {
		if config.RedactTokens {
			headers = RedactHeaders(headers)
		}

		// Log headers as a single line map
		logger.Debug("RESPONSE Headers: %v", headers)
	}

	// Log response body if captured and not too large
	if responseBody != nil && responseBody.Len() > 0 && int64(responseBody.Len()) <= config.MaxBodySize {
		bodyStr := responseBody.String()
		if config.RedactTokens {
			bodyStr = RedactSensitiveInfo(bodyStr)
		}

		logger.Debug("RESPONSE Body: %s", bodyStr)
	}
}

// DefaultRequestResponseConfig returns a sensible default configuration
func DefaultRequestResponseConfig() RequestResponseLoggingConfig {
	return RequestResponseLoggingConfig{
		LogRequests:    true,
		LogResponses:   true,
		RedactTokens:   true,
		MaxBodySize:    10 * 1024, // 10KB
		OnlyDebugLevel: true,
		SkipPaths: []string{
			"/metrics",
			"/favicon.ico",
		},
	}
}
