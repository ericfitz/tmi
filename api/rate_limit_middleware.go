package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// RateLimitMiddleware creates a middleware that enforces API rate limiting
func RateLimitMiddleware(server *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Skip rate limiting if no rate limiter is configured
		if server.apiRateLimiter == nil {
			c.Next()
			return
		}

		// Skip rate limiting for unauthenticated endpoints (public discovery, auth flows)
		// These have their own rate limiting strategies (IP-based or multi-scope)
		path := c.Request.URL.Path
		if isPublicEndpoint(path) || isAuthFlowEndpoint(path) {
			c.Next()
			return
		}

		// Extract user ID from context (set by JWT middleware)
		userIDValue, exists := c.Get("user_id")
		if !exists {
			// No user ID means not authenticated - skip rate limiting for this request
			// (JWT middleware will handle authentication failures)
			c.Next()
			return
		}

		userID, ok := userIDValue.(string)
		if !ok || userID == "" {
			logger.Warn("Invalid user_id in context for rate limiting")
			c.Next()
			return
		}

		// Check rate limit
		allowed, retryAfter, err := server.apiRateLimiter.CheckRateLimit(c.Request.Context(), userID)
		if err != nil {
			logger.Error("Rate limit check failed for user %s: %v", userID, err)
			// On error, allow the request (fail open)
			c.Next()
			return
		}

		// Get rate limit info for headers
		limit, remaining, resetAt, err := server.apiRateLimiter.GetRateLimitInfo(c.Request.Context(), userID)
		if err != nil {
			logger.Error("Failed to get rate limit info for user %s: %v", userID, err)
			// Set default headers on error
			limit = DefaultMaxRequestsPerMinute
			remaining = DefaultMaxRequestsPerMinute
			resetAt = 0
		}

		// Always add rate limit headers to response
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		if resetAt > 0 {
			c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))
		}

		if !allowed {
			// Rate limit exceeded
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))

			c.JSON(http.StatusTooManyRequests, Error{
				Error:            "rate_limit_exceeded",
				ErrorDescription: "Rate limit exceeded. Please retry after the specified time.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// isPublicEndpoint checks if the path is a public discovery endpoint
func isPublicEndpoint(path string) bool {
	// Exact matches for specific public paths
	exactMatches := []string{
		"/",
	}
	if slices.Contains(exactMatches, path) {
		return true
	}

	// Prefix matches for public path prefixes
	prefixes := []string{
		"/.well-known/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// isAuthFlowEndpoint checks if the path is an OAuth or SAML auth flow endpoint
func isAuthFlowEndpoint(path string) bool {
	authPaths := []string{
		"/oauth2/authorize",
		"/oauth2/callback",
		"/oauth2/token",
		"/oauth2/refresh",
		"/oauth2/introspect",
		"/saml/login",
		"/saml/acs",
		"/saml/slo",
	}

	return slices.Contains(authPaths, path)
}
