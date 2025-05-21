package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header is required",
			})
			return
		}

		// Check if the Authorization header has the correct format
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header format must be Bearer {token}",
			})
			return
		}

		// Validate the token
		tokenString := parts[1]
		claims, err := m.service.ValidateToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": fmt.Sprintf("Invalid token: %v", err),
			})
			return
		}

		// Set the claims in the context
		c.Set(string(ClaimsContextKey), claims)

		// Get the user from the database
		user, err := m.service.GetUserByEmail(c.Request.Context(), claims.Email)
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
