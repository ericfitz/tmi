package api

import (
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// CustomRecoveryMiddleware returns a Gin middleware that recovers from panics
// and returns appropriate error responses without exposing sensitive information
func CustomRecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Get the stack trace for internal logging
				stack := debug.Stack()

				// Get logger with context
				logger := slogging.GetContextLogger(c)

				// Capture CATS fuzzer information and request context for debugging
				fuzzer := c.GetHeader("X-CATS-Fuzzer")
				requestID := c.GetHeader("X-Request-ID")
				path := c.Request.URL.Path
				method := c.Request.Method

				// Log the panic internally with full details for debugging
				if fuzzer != "" {
					logger.Error("PANIC recovered: %v\nFuzzer: %s\nRequest ID: %s\nPath: %s %s\nStack Trace:\n%s",
						err, fuzzer, requestID, method, path, stack)
				} else {
					logger.Error("PANIC recovered: %v\nRequest ID: %s\nPath: %s %s\nStack Trace:\n%s",
						err, requestID, method, path, stack)
				}

				// Determine if we're in development mode
				isDevelopment := gin.Mode() == gin.DebugMode || strings.ToLower(os.Getenv("LOG_LEVEL")) == LogLevelDebugStr

				// Prepare the error response
				var errorResponse Error

				if isDevelopment {
					// In development, provide more details (but still no full stack trace)
					errorResponse = Error{
						Error:            "internal_server_error",
						ErrorDescription: fmt.Sprintf("Server panic: %v", err),
					}
				} else {
					// In production, provide minimal information
					errorResponse = Error{
						Error:            "internal_server_error",
						ErrorDescription: "An unexpected error occurred. Please try again later.",
					}
				}

				// Abort the request chain
				c.AbortWithStatusJSON(http.StatusInternalServerError, errorResponse)
			}
		}()

		// Continue processing
		c.Next()
	}
}

// FilterStackTraceFromBody filters out stack trace information from response bodies
// This is used by the request logger to prevent stack traces from being logged
func FilterStackTraceFromBody(body string) string {
	if body == "" {
		return body
	}

	// Common stack trace indicators
	stackTraceIndicators := []string{
		"goroutine",
		"runtime/debug.Stack",
		"runtime.gopanic",
		".go:",
		"panic(",
		"PANIC",
		"/usr/local/go/src/",
		"github.com/",
	}

	// Check if this looks like it contains a stack trace
	hasStackTrace := false
	for _, indicator := range stackTraceIndicators {
		if strings.Contains(body, indicator) {
			hasStackTrace = true
			break
		}
	}

	if !hasStackTrace {
		return body
	}

	// If in development mode, show partial stack trace
	if gin.Mode() == gin.DebugMode || strings.ToLower(os.Getenv("LOG_LEVEL")) == LogLevelDebugStr {
		// Truncate stack traces but keep some info for debugging
		lines := strings.Split(body, "\n")
		filtered := []string{}
		stackLineCount := 0
		maxStackLines := 5
		truncated := false

		inStackTrace := false
		for _, line := range lines {
			// Check if this line looks like a stack trace line
			isStackLine := false
			for _, indicator := range stackTraceIndicators {
				if strings.Contains(line, indicator) {
					isStackLine = true
					inStackTrace = true
					break
				}
			}

			// If we're in a stack trace, consider indented lines as part of it too
			if inStackTrace && strings.HasPrefix(line, "\t") {
				isStackLine = true
			}

			if isStackLine {
				if stackLineCount < maxStackLines {
					filtered = append(filtered, line)
					stackLineCount++
				} else if !truncated {
					filtered = append(filtered, "... [stack trace truncated for security] ...")
					truncated = true
				}
				// Skip remaining stack lines
			} else {
				// Reset if we hit a non-stack line
				if line != "" && !strings.HasPrefix(line, "\t") {
					inStackTrace = false
				}
				// Include non-stack lines
				if !inStackTrace || stackLineCount == 0 {
					filtered = append(filtered, line)
				}
			}
		}

		return strings.Join(filtered, "\n")
	}

	// In production, completely remove stack trace information
	return "[Error details redacted for security. Check server logs for full information.]"
}
