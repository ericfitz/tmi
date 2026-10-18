package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/ericfitz/tmi/internal/errcode"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// RateLimitMiddleware creates a middleware that enforces API rate limiting
// SEM@db94db3c8118cffb5220bf9a44c2fde0ada20427: enforce per-user API rate limits and set rate limit response headers, skipping public and auth endpoints
func RateLimitMiddleware(server *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Skip all rate limiting when disabled via config (dev/test mode)
		if server.rateLimitingDisabled {
			c.Next()
			return
		}

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

		// Key on the authenticated user set by the JWT middleware
		userID, authenticated := rateLimitUserKey(c)
		if !authenticated {
			// No user means not authenticated - skip rate limiting for this request
			// (JWT middleware will handle authentication failures)
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
				Error:            ErrorError(errcode.RateLimitExceeded),
				ErrorDescription: "Rate limit exceeded. Please retry after the specified time.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// rateLimitUserKey returns the per-user rate limit key from the JWT middleware's context
// values: the internal UUID (the quota store key), else the provider user ID.
// SEM@0000000: resolve the authenticated user's rate limit key from request context (pure)
func rateLimitUserKey(c *gin.Context) (string, bool) {
	for _, key := range []string{"userInternalUUID", "userID"} {
		if v, ok := c.Get(key); ok {
			if id, ok := v.(string); ok && id != "" {
				return id, true
			}
		}
	}
	return "", false
}

// isPublicEndpoint checks if the path is a public discovery endpoint.
// Note: "/" (health/info) is intentionally excluded — it is hit by ALB health
// checks, kubelet probes, and frontend status polls, so IP-based rate limiting
// on it causes spurious 429s in cloud deployments.
// SEM@ea92ee787f92d4cc95d248683abb5f294c035012: report whether a request path is a public discovery endpoint exempt from rate limiting (pure)
func isPublicEndpoint(path string) bool {
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
// SEM@c0b50b4b57f4dcf67acd03dc8524fccfc907c40c: report whether a request path is an OAuth or SAML auth flow endpoint governed by the auth-flow limiter, not the per-user limiter (pure)
func isAuthFlowEndpoint(path string) bool {
	authPaths := []string{
		"/oauth2/authorize",
		"/oauth2/callback",
		"/oauth2/token",
		"/oauth2/refresh",
		"/oauth2/introspect",
		"/oauth2/step_up",
		"/saml/login",
		"/saml/acs",
		"/saml/slo",
	}

	return slices.Contains(authPaths, path)
}
