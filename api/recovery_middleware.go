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

				// Log the panic internally with full details for debugging
				logger.Error("PANIC recovered: %v\nStack Trace:\n%s", err, stack)

				// Determine if we're in development mode
				isDevelopment := gin.Mode() == gin.DebugMode || strings.ToLower(os.Getenv("LOG_LEVEL")) == "debug"

				// Prepare the error response
				var errorResponse ErrorResponse

				if isDevelopment {
					// In development, provide more details (but still no full stack trace)
					errorResponse = ErrorResponse{
						Error:   "internal_server_error",
						Message: fmt.Sprintf("Server panic: %v", err),
					}
				} else {
					// In production, provide minimal information
					errorResponse = ErrorResponse{
						Error:   "internal_server_error",
						Message: "An unexpected error occurred. Please try again later.",
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
	if gin.Mode() == gin.DebugMode || strings.ToLower(os.Getenv("LOG_LEVEL")) == "debug" {
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
