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
		ipAddress := extractIPAddress(c, server.trustedProxiesConfigured)
		if ipAddress == "" {
			logger.Warn("Could not extract IP address for rate limiting")
			c.Next()
			return
		}

		// Read configured limits
		limit := server.ipRateLimiter.DefaultLimit
		window := server.ipRateLimiter.DefaultWindowSeconds

		// Check rate limit for public endpoints
		allowed, retryAfter, err := server.ipRateLimiter.CheckRateLimit(c.Request.Context(), ipAddress, limit, window)
		if err != nil {
			logger.Error("IP rate limit check failed for %s: %v", ipAddress, err)
			// Fail open
			c.Next()
			return
		}

		// Get rate limit info for headers
		remaining, resetAt, _ := server.ipRateLimiter.GetRateLimitInfo(c.Request.Context(), ipAddress, limit, window)

		// Add rate limit headers
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))

		if !allowed {
			// TODO: emit structured log event with IP, endpoint, and remaining count on rate limit block
			// TODO: emit rate_limit_blocked metric counter with labels {tier: "public-discovery", ip: extractedIP}
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			c.JSON(http.StatusTooManyRequests, Error{
				Error:            "rate_limit_exceeded",
				ErrorDescription: "IP rate limit exceeded. Please retry after the specified time.",
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
		ipAddress := extractIPAddress(c, server.trustedProxiesConfigured)
		userIdentifier := extractUserIdentifier(c)

		// Use stricter IP rate limit for token endpoint to mitigate brute force
		var result *RateLimitResult
		var err error
		if isTokenEndpoint(path) {
			result, err = server.authFlowRateLimiter.CheckRateLimitForTokenEndpoint(c.Request.Context(), sessionID, ipAddress, userIdentifier)
		} else {
			result, err = server.authFlowRateLimiter.CheckRateLimit(c.Request.Context(), sessionID, ipAddress, userIdentifier)
		}
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
			c.JSON(http.StatusTooManyRequests, Error{
				Error:            "rate_limit_exceeded",
				ErrorDescription: fmt.Sprintf("Auth flow rate limit exceeded (%s scope). Please retry after the specified time.", result.BlockedByScope),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// isTokenEndpoint checks if the path is the OAuth token exchange endpoint
func isTokenEndpoint(path string) bool {
	return path == "/oauth2/token"
}

// extractIPAddress extracts the client IP address from the request.
// When trusted proxies are configured, uses Gin's ClientIP() which validates
// the X-Forwarded-For chain. Otherwise, extracts from headers directly.
func extractIPAddress(c *gin.Context, trustedProxiesConfigured bool) string {
	if trustedProxiesConfigured {
		// Gin's ClientIP() validates X-Forwarded-For against trusted proxy list
		return c.ClientIP()
	}

	// Manual extraction when no trusted proxies configured (backward compatible)
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
	return c.ClientIP()
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

	// For token endpoint, check both query params and POST body for code/refresh_token
	// POST /oauth2/token sends these as form-encoded or JSON body params
	if code := c.Query("code"); code != "" {
		return code
	}
	if code := c.PostForm("code"); code != "" {
		return code
	}
	if refreshToken := c.PostForm("refresh_token"); refreshToken != "" {
		return refreshToken
	}

	return ""
}

// extractUserIdentifier extracts user identifier for account-based rate limiting
func extractUserIdentifier(c *gin.Context) string {
	// For OAuth, use login_hint if provided
	if loginHint := c.Query("login_hint"); loginHint != "" {
		return strings.ToLower(strings.TrimSpace(loginHint))
	}

	// For token endpoint, use client_id from POST body (client_credentials grant)
	if clientID := c.PostForm("client_id"); clientID != "" {
		return strings.ToLower(strings.TrimSpace(clientID))
	}

	return ""
}
