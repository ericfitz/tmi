package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// IPRateLimitMiddleware creates middleware for IP-based rate limiting (Tier 1 - public discovery)
func IPRateLimitMiddleware(server *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Skip if no IP rate limiter configured
		if server.ipRateLimiter == nil {
			c.Next()
			return
		}

		// Only apply to public discovery endpoints
		path := c.Request.URL.Path
		if !isPublicEndpoint(path) {
			c.Next()
			return
		}

		// Extract IP address
		ipAddress := extractIPAddress(c)
		if ipAddress == "" {
			logger.Warn("Could not extract IP address for rate limiting")
			c.Next()
			return
		}

		// Check rate limit (10 requests/minute for public endpoints)
		allowed, retryAfter, err := server.ipRateLimiter.CheckRateLimit(c.Request.Context(), ipAddress, 10, 60)
		if err != nil {
			logger.Error("IP rate limit check failed for %s: %v", ipAddress, err)
			// Fail open
			c.Next()
			return
		}

		// Get rate limit info for headers
		remaining, resetAt, _ := server.ipRateLimiter.GetRateLimitInfo(c.Request.Context(), ipAddress, 10, 60)

		// Add rate limit headers
		c.Header("X-RateLimit-Limit", "10")
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))

		if !allowed {
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    "rate_limit_exceeded",
				"message": "IP rate limit exceeded. Please retry after the specified time.",
				"details": gin.H{
					"limit":       10,
					"scope":       "ip",
					"window":      "minute",
					"retry_after": retryAfter,
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AuthFlowRateLimitMiddleware creates middleware for multi-scope auth flow rate limiting (Tier 2)
func AuthFlowRateLimitMiddleware(server *Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Skip if no auth flow rate limiter configured
		if server.authFlowRateLimiter == nil {
			c.Next()
			return
		}

		// Only apply to auth flow endpoints
		path := c.Request.URL.Path
		if !isAuthFlowEndpoint(path) {
			c.Next()
			return
		}

		// Extract identifiers for multi-scope checking
		sessionID := extractSessionID(c)
		ipAddress := extractIPAddress(c)
		userIdentifier := extractUserIdentifier(c)

		// Check rate limits across all scopes
		result, err := server.authFlowRateLimiter.CheckRateLimit(c.Request.Context(), sessionID, ipAddress, userIdentifier)
		if err != nil {
			logger.Error("Auth flow rate limit check failed: %v", err)
			// Fail open
			c.Next()
			return
		}

		// Add rate limit headers (based on most restrictive scope)
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", result.ResetAt))
		if result.BlockedByScope != "" {
			c.Header("X-RateLimit-Scope", result.BlockedByScope)
		}

		if !result.Allowed {
			c.Header("Retry-After", fmt.Sprintf("%d", result.RetryAfter))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    "rate_limit_exceeded",
				"message": fmt.Sprintf("Auth flow rate limit exceeded (%s scope). Please retry after the specified time.", result.BlockedByScope),
				"details": gin.H{
					"limit":       result.Limit,
					"scope":       result.BlockedByScope,
					"retry_after": result.RetryAfter,
				},
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// extractIPAddress extracts the client IP address from the request
func extractIPAddress(c *gin.Context) string {
	// Try X-Forwarded-For first (for proxied requests)
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Try X-Real-IP
	if xri := c.GetHeader("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := c.ClientIP()
	return ip
}

// extractSessionID extracts session identifier for OAuth/SAML flows
func extractSessionID(c *gin.Context) string {
	// For OAuth, use state parameter
	if state := c.Query("state"); state != "" {
		return state
	}

	// For SAML, could use SAMLRequest or RelayState
	if relayState := c.Query("RelayState"); relayState != "" {
		return relayState
	}

	// For token endpoint, use a hash of the authorization code or refresh token
	if code := c.Query("code"); code != "" {
		return code // The code itself is unique per session
	}

	return ""
}

// extractUserIdentifier extracts user identifier for account-based rate limiting
func extractUserIdentifier(c *gin.Context) string {
	// For OAuth, use login_hint if provided
	if loginHint := c.Query("login_hint"); loginHint != "" {
		// Normalize to lowercase
		return strings.ToLower(strings.TrimSpace(loginHint))
	}

	// For SAML or form-based login, could extract username from request body
	// For now, return empty - this would require reading the body which we want to avoid

	return ""
}
