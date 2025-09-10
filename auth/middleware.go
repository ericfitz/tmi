package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
)

// ContextKey is a type for context keys
type ContextKey string

const (
	// UserContextKey is the key for the user in the context
	UserContextKey ContextKey = "user"
	// ClaimsContextKey is the key for the JWT claims in the context
	ClaimsContextKey ContextKey = "claims"
)

// Middleware provides authentication middleware for Gin
type Middleware struct {
	service *Service
}

// NewMiddleware creates a new authentication middleware
func NewMiddleware(service *Service) *Middleware {
	return &Middleware{
		service: service,
	}
}

// AuthRequired is a middleware that requires authentication
func (m *Middleware) AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract the token from the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header is required",
			})
			return
		}

		// Check if the Authorization header has the correct format
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header format must be Bearer {token}",
			})
			return
		}

		// Validate the token
		tokenString := parts[1]
		claims, err := m.service.ValidateToken(tokenString)
		if err != nil {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": fmt.Sprintf("Invalid token: %v", err),
			})
			return
		}

		// Set the claims in the context
		c.Set(string(ClaimsContextKey), claims)

		// Get the user from the database with provider ID
		user, err := m.service.GetUserWithProviderID(c.Request.Context(), claims.Email)
		if err != nil {
			// If the user is not found, we'll still allow the request to proceed
			// but we won't set the user in the context
			c.Next()
			return
		}

		// Set the user in the context
		c.Set(string(UserContextKey), user)
		c.Next()
	}
}

// GetUserFromContext gets the user from the context
func GetUserFromContext(ctx context.Context) (User, error) {
	user, ok := ctx.Value(UserContextKey).(User)
	if !ok {
		return User{}, errors.New("user not found in context")
	}
	return user, nil
}

// GetClaimsFromContext gets the JWT claims from the context
func GetClaimsFromContext(ctx context.Context) (*Claims, error) {
	claims, ok := ctx.Value(ClaimsContextKey).(*Claims)
	if !ok {
		return nil, errors.New("claims not found in context")
	}
	return claims, nil
}

// RequireRole is a middleware that requires a specific role
func (m *Middleware) RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the user from the context
		_, exists := c.Get(string(UserContextKey))
		if !exists {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "User not found in context",
			})
			return
		}

		// TODO: Implement role checking
		// For now, we'll just allow all authenticated users
		c.Next()
	}
}

// RequireOwner is a middleware that requires the user to be the owner of a resource
func (m *Middleware) RequireOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the user from the context
		_, exists := c.Get(string(UserContextKey))
		if !exists {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "User not found in context",
			})
			return
		}

		// TODO: Implement owner checking
		// For now, we'll just allow all authenticated users
		c.Next()
	}
}

// RequireWriter is a middleware that requires the user to be a writer of a resource
func (m *Middleware) RequireWriter() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the user from the context
		_, exists := c.Get(string(UserContextKey))
		if !exists {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "User not found in context",
			})
			return
		}

		// TODO: Implement writer checking
		// For now, we'll just allow all authenticated users
		c.Next()
	}
}

// RequireReader is a middleware that requires the user to be a reader of a resource
func (m *Middleware) RequireReader() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the user from the context
		_, exists := c.Get(string(UserContextKey))
		if !exists {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "User not found in context",
			})
			return
		}

		// TODO: Implement reader checking
		// For now, we'll just allow all authenticated users
		c.Next()
	}
}

// Admin middleware functions

// RequireAdminAuth is a middleware that requires admin authentication
func (m *Middleware) RequireAdminAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if admin interface is enabled
		if !cfg.IsAdminEnabled() {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
				"error": "Admin interface is not enabled",
			})
			return
		}

		// First, require standard authentication
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Redirect(http.StatusFound, "/oauth2/authorize")
			return
		}

		// Check if the Authorization header has the correct format
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.Redirect(http.StatusFound, "/oauth2/authorize")
			return
		}

		// Validate the token
		tokenString := parts[1]
		claims, err := m.service.ValidateToken(tokenString)
		if err != nil {
			c.Redirect(http.StatusFound, "/oauth2/authorize")
			return
		}

		// Check if user is an admin
		if !cfg.IsUserAdmin(claims.Email) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Admin privileges required",
			})
			return
		}

		// Set claims and user in context
		c.Set(string(ClaimsContextKey), claims)

		// Get the user from the database with provider ID
		user, err := m.service.GetUserWithProviderID(c.Request.Context(), claims.Email)
		if err == nil {
			c.Set(string(UserContextKey), user)
		}

		c.Next()
	}
}

// RequireAdminAuthAPI is a middleware for admin API endpoints (returns JSON errors)
func (m *Middleware) RequireAdminAuthAPI(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if admin interface is enabled
		if !cfg.IsAdminEnabled() {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
				"error": "Admin interface is not enabled",
			})
			return
		}

		// Extract the token from the Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header is required",
			})
			return
		}

		// Check if the Authorization header has the correct format
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header format must be Bearer {token}",
			})
			return
		}

		// Validate the token
		tokenString := parts[1]
		claims, err := m.service.ValidateToken(tokenString)
		if err != nil {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": fmt.Sprintf("Invalid token: %v", err),
			})
			return
		}

		// Check if user is an admin
		if !cfg.IsUserAdmin(claims.Email) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Admin privileges required",
			})
			return
		}

		// Set claims and user in context
		c.Set(string(ClaimsContextKey), claims)

		// Get the user from the database with provider ID
		user, err := m.service.GetUserWithProviderID(c.Request.Context(), claims.Email)
		if err == nil {
			c.Set(string(UserContextKey), user)
		}

		c.Next()
	}
}

// CheckIPAllowlist is a middleware that checks if the client IP is in the admin allowlist
func CheckIPAllowlist(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If no IP allowlist is configured, allow all
		if len(cfg.Admin.Security.IPAllowlist) == 0 {
			c.Next()
			return
		}

		clientIP := c.ClientIP()
		allowed := false

		for _, allowedIP := range cfg.Admin.Security.IPAllowlist {
			if isIPAllowed(clientIP, allowedIP) {
				allowed = true
				break
			}
		}

		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "Access denied from this IP address",
			})
			return
		}

		c.Next()
	}
}

// isIPAllowed checks if the client IP is allowed based on the allowlist entry
func isIPAllowed(clientIP, allowedEntry string) bool {
	// Parse client IP
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}

	// Check if allowedEntry is a CIDR
	if strings.Contains(allowedEntry, "/") {
		_, cidr, err := net.ParseCIDR(allowedEntry)
		if err != nil {
			return false
		}
		return cidr.Contains(ip)
	}

	// Direct IP comparison
	allowedIP := net.ParseIP(allowedEntry)
	if allowedIP == nil {
		return false
	}

	return ip.Equal(allowedIP)
}
