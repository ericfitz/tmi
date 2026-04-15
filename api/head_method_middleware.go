package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// RegisterHEADRoutes inspects all currently registered GET routes and adds a
// corresponding HEAD route for each one that is not in headExcludedPaths.
// This must be called after all GET routes have been registered so that:
//   - Gin's HandleMethodNotAllowed=true does not return 405 for HEAD requests
//     on GET-only routes (RFC 9110 §9.3.2 requires HEAD support).
//   - HeadMethodMiddleware converts HEAD→GET so that OpenAPI validation and the
//     actual handler see "GET", while the response body is suppressed.
//
// Excluded paths (OAuth redirects, SAML redirects) are intentionally omitted
// because their redirect behaviour makes HEAD semantically incorrect.
func RegisterHEADRoutes(r *gin.Engine) {
	// Build a set of paths that already have HEAD routes registered
	// (e.g., static files registered via r.StaticFile register both GET and HEAD).
	existingHEAD := make(map[string]bool)
	for _, route := range r.Routes() {
		if route.Method == http.MethodHead {
			existingHEAD[route.Path] = true
		}
	}

	for _, route := range r.Routes() {
		if route.Method != http.MethodGet {
			continue
		}
		// Skip paths that already have HEAD routes
		if existingHEAD[route.Path] {
			continue
		}
		// Skip paths that are excluded from HEAD→GET conversion
		if isExcludedFromHead(route.Path) {
			continue
		}
		// Register HEAD using the same handler function; the global
		// HeadMethodMiddleware will suppress the response body.
		r.HEAD(route.Path, route.HandlerFunc)
	}
}

// headExcludedPaths defines URL path patterns that should NOT have HEAD→GET
// conversion applied. Each pattern is a slice of path segments where "*"
// matches any single segment.
var headExcludedPaths = [][]string{
	{"oauth2", "authorize"},
	{"oauth2", "callback"},
	{"saml", "*", "login"},
	{"saml", "slo"},
}

// isExcludedFromHead returns true if the given path matches any of the
// excluded path patterns. Matching is segment-based: each segment must match
// exactly or the pattern segment must be "*" (which matches any single segment).
func isExcludedFromHead(path string) bool {
	// Trim leading slash and split into segments
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return false
	}
	segments := strings.Split(trimmed, "/")

	for _, pattern := range headExcludedPaths {
		if matchSegments(pattern, segments) {
			return true
		}
	}
	return false
}

// matchSegments returns true if the path segments match the pattern segments.
// A pattern segment of "*" matches any single path segment.
func matchSegments(pattern, segments []string) bool {
	if len(pattern) != len(segments) {
		return false
	}
	for i, p := range pattern {
		if p != "*" && p != segments[i] {
			return false
		}
	}
	return true
}

// headResponseWriter wraps a gin.ResponseWriter to suppress response body
// writes while tracking the number of bytes that would have been written.
// This is used by HeadMethodMiddleware to implement proper HEAD responses.
type headResponseWriter struct {
	gin.ResponseWriter
	bodyBytes int
}

// Write counts the bytes but does not write them to the underlying writer.
func (w *headResponseWriter) Write(b []byte) (int, error) {
	w.bodyBytes += len(b)
	return len(b), nil
}

// WriteString counts the string length but does not write to the underlying writer.
func (w *headResponseWriter) WriteString(s string) (int, error) {
	w.bodyBytes += len(s)
	return len(s), nil
}

// Size returns the number of body bytes that were suppressed.
func (w *headResponseWriter) Size() int {
	return w.bodyBytes
}

// Written returns true if any body bytes were suppressed.
func (w *headResponseWriter) Written() bool {
	return w.bodyBytes > 0
}

// HeadMethodMiddleware returns Gin middleware that converts HEAD requests to
// GET before the handler chain, suppresses the response body, and restores
// the original HEAD method afterward. This allows OpenAPI validation middleware
// to match GET routes for HEAD requests while producing correct HEAD responses.
//
// Certain protocol endpoints (OAuth authorize/callback, SAML login/SLO) are
// excluded from conversion because they involve redirects or other behavior
// that should not be altered.
func HeadMethodMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodHead {
			c.Next()
			return
		}

		if isExcludedFromHead(c.Request.URL.Path) {
			logger := slogging.GetContextLogger(c)
			logger.Debug("HEAD request on excluded path, passing through without conversion: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Save the original writer
		origWriter := c.Writer

		// Convert HEAD to GET so OpenAPI validation and handlers match
		c.Request.Method = http.MethodGet

		// Replace writer with body-suppressing wrapper
		wrapper := &headResponseWriter{ResponseWriter: origWriter}
		c.Writer = wrapper

		c.Next()

		// Restore original method and writer
		c.Writer = origWriter
		c.Request.Method = http.MethodHead

		// Set Content-Length if the handler didn't set it explicitly and body bytes were captured
		if origWriter.Header().Get("Content-Length") == "" && wrapper.bodyBytes > 0 {
			origWriter.Header().Set("Content-Length", strconv.Itoa(wrapper.bodyBytes))
		}
	}
}
