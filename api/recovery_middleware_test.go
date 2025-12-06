package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCustomRecoveryMiddleware(t *testing.T) {
	// Test in both debug and production modes
	testCases := []struct {
		name          string
		mode          string
		logLevel      string
		expectDetails bool
	}{
		{
			name:          "Production Mode",
			mode:          gin.ReleaseMode,
			logLevel:      "info",
			expectDetails: false,
		},
		{
			name:          "Debug Mode",
			mode:          gin.DebugMode,
			logLevel:      "debug",
			expectDetails: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up environment
			originalMode := gin.Mode()
			originalLogLevel := os.Getenv("LOG_LEVEL")
			gin.SetMode(tc.mode)
			_ = os.Setenv("LOG_LEVEL", tc.logLevel)
			defer func() {
				gin.SetMode(originalMode)
				_ = os.Setenv("LOG_LEVEL", originalLogLevel)
			}()

			// Create router with recovery middleware
			router := gin.New()
			router.Use(CustomRecoveryMiddleware())

			// Add a route that panics
			router.GET("/panic", func(c *gin.Context) {
				panic("test panic: this should not expose stack trace")
			})

			// Make request
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/panic", nil)
			router.ServeHTTP(w, req)

			// Check response
			assert.Equal(t, http.StatusInternalServerError, w.Code)

			var response Error
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, "internal_server_error", response.Error)

			// Check that stack trace is not exposed
			bodyStr := w.Body.String()
			assert.NotContains(t, bodyStr, "runtime/debug.Stack")
			assert.NotContains(t, bodyStr, "goroutine")
			assert.NotContains(t, bodyStr, ".go:")

			if tc.expectDetails {
				// In debug mode, should have more specific error message
				assert.Contains(t, response.ErrorDescription, "Server panic")
			} else {
				// In production, should have generic message
				assert.Equal(t, "An unexpected error occurred. Please try again later.", response.ErrorDescription)
			}
		})
	}
}

func TestFilterStackTraceFromBody(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		logLevel string
		expected string
	}{
		{
			name:     "No stack trace",
			input:    `{"error": "validation_error", "error_description": "Invalid input"}`,
			logLevel: "info",
			expected: `{"error": "validation_error", "error_description": "Invalid input"}`,
		},
		{
			name: "Stack trace in production",
			input: `panic: test error
goroutine 1 [running]:
runtime/debug.Stack()
	/usr/local/go/src/runtime/debug/stack.go:24 +0x65
github.com/ericfitz/tmi/api.TestFunction()
	/path/to/file.go:123 +0x123`,
			logLevel: "info",
			expected: "[Error details redacted for security. Check server logs for full information.]",
		},
		{
			name: "Stack trace in debug mode",
			input: `panic: test error
goroutine 1 [running]:
runtime/debug.Stack()
	/usr/local/go/src/runtime/debug/stack.go:24 +0x65
github.com/ericfitz/tmi/api.TestFunction()
	/path/to/file.go:123 +0x123
github.com/ericfitz/tmi/api.AnotherFunction()
	/path/to/another.go:456 +0x789
runtime.gopanic()
	/usr/local/go/src/runtime/panic.go:890 +0xabc`,
			logLevel: "debug",
			expected: `panic: test error
goroutine 1 [running]:
runtime/debug.Stack()
	/usr/local/go/src/runtime/debug/stack.go:24 +0x65
github.com/ericfitz/tmi/api.TestFunction()
... [stack trace truncated for security] ...`,
		},
		{
			name:     "Empty body",
			input:    "",
			logLevel: "info",
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set log level and gin mode
			originalLogLevel := os.Getenv("LOG_LEVEL")
			originalMode := gin.Mode()
			_ = os.Setenv("LOG_LEVEL", tc.logLevel)
			if tc.logLevel == "debug" {
				gin.SetMode(gin.DebugMode)
			} else {
				gin.SetMode(gin.ReleaseMode)
			}
			defer func() {
				_ = os.Setenv("LOG_LEVEL", originalLogLevel)
				gin.SetMode(originalMode)
			}()

			// Test filtering
			result := FilterStackTraceFromBody(tc.input)

			// For debug mode with truncation, just check key parts
			if tc.logLevel == "debug" && strings.Contains(tc.input, "goroutine") {
				assert.Contains(t, result, "panic: test error")
				assert.Contains(t, result, "... [stack trace truncated for security] ...")
				// Should not contain the later stack trace lines
				assert.NotContains(t, result, "runtime.gopanic")
				assert.NotContains(t, result, "AnotherFunction")
			} else {
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
