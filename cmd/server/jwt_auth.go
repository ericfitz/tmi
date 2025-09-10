package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// TokenExtractor handles extracting JWT tokens from requests
type TokenExtractor struct{}

// ExtractToken extracts the JWT token from the request
func (t *TokenExtractor) ExtractToken(c *gin.Context) (string, error) {
	logger := logging.GetContextLogger(c)

	// For WebSocket connections, use query parameter authentication
	if strings.HasPrefix(c.Request.URL.Path, "/ws/") || strings.HasSuffix(c.Request.URL.Path, "/ws") {
		tokenStr := c.Query("token")
		if tokenStr == "" {
			logger.Warn("Authentication failed: Missing token query parameter for WebSocket path: %s", c.Request.URL.Path)
			return "", fmt.Errorf("missing token query parameter")
		}
		return tokenStr, nil
	}

	// For regular API calls, use Authorization header
	logger.Debug("[JWT_MIDDLEWARE] Checking for Authorization header")
	authHeader := c.GetHeader("Authorization")
	logger.Debug("[JWT_MIDDLEWARE] Authorization header value: '%s' (empty: %t)", logging.RedactSensitiveInfo(authHeader), authHeader == "")

	if authHeader == "" {
		logger.Warn("[JWT_MIDDLEWARE] 🚫 Authentication failed: Missing Authorization header for path: %s", c.Request.URL.Path)
		return "", fmt.Errorf("missing Authorization header")
	}

	// Parse the header format
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		logger.Warn("Authentication failed: Invalid Authorization header format for path: %s", c.Request.URL.Path)
		return "", fmt.Errorf("invalid Authorization header format")
	}

	return parts[1], nil
}

// TokenValidator handles JWT token validation
type TokenValidator struct {
	jwtSecret []byte
}

// NewTokenValidator creates a new token validator
func NewTokenValidator(cfg *config.Config) *TokenValidator {
	return &TokenValidator{
		jwtSecret: []byte(cfg.Auth.JWT.Secret),
	}
}

// ValidateToken validates a JWT token and returns the parsed token
func (v *TokenValidator) ValidateToken(c *gin.Context, tokenStr string) (*jwt.Token, error) {
	logger := logging.GetContextLogger(c)

	// Validate the token
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		logger.Warn("Authentication failed: Invalid or expired token - %v", err)
		return nil, fmt.Errorf("invalid or expired token")
	}

	return token, nil
}

// TokenBlacklistChecker handles checking if a token is blacklisted
type TokenBlacklistChecker struct {
	tokenBlacklist *auth.TokenBlacklist
}

// NewTokenBlacklistChecker creates a new blacklist checker
func NewTokenBlacklistChecker(tokenBlacklist *auth.TokenBlacklist) *TokenBlacklistChecker {
	return &TokenBlacklistChecker{
		tokenBlacklist: tokenBlacklist,
	}
}

// CheckBlacklist checks if a token is blacklisted
func (b *TokenBlacklistChecker) CheckBlacklist(ctx context.Context, tokenStr string) error {
	if b.tokenBlacklist == nil {
		return nil
	}

	isBlacklisted, err := b.tokenBlacklist.IsTokenBlacklisted(ctx, tokenStr)
	if err != nil {
		return fmt.Errorf("failed to check token blacklist: %w", err)
	}

	if isBlacklisted {
		return fmt.Errorf("token has been revoked")
	}

	return nil
}

// ClaimsExtractor handles extracting and setting claims in the context
type ClaimsExtractor struct {
	authHandlers *auth.Handlers
	config       *config.Config
}

// NewClaimsExtractor creates a new claims extractor
func NewClaimsExtractor(authHandlers *auth.Handlers, cfg *config.Config) *ClaimsExtractor {
	return &ClaimsExtractor{
		authHandlers: authHandlers,
		config:       cfg,
	}
}

// ExtractAndSetClaims extracts claims from a valid token and sets them in the context
func (e *ClaimsExtractor) ExtractAndSetClaims(c *gin.Context, token *jwt.Token) error {
	logger := logging.GetContextLogger(c)

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return fmt.Errorf("invalid token claims")
	}

	// Extract user ID (sub claim)
	if sub, ok := claims["sub"].(string); ok {
		logger.Debug("Authenticated user ID: %s", sub)
		c.Set("userID", sub)

		// Extract role if present
		if roleValue, hasRole := claims["role"]; hasRole {
			if role, ok := roleValue.(string); ok {
				logger.Debug("User role from token: %s", role)
				c.Set("userTokenRole", role)
			}
		}

		// Extract display name if present
		if nameValue, hasName := claims["name"]; hasName {
			if name, ok := nameValue.(string); ok {
				logger.Debug("User display name from token: %s", name)
				c.Set("userDisplayName", name)
			}
		}

		// Extract email if present
		if emailValue, hasEmail := claims["email"]; hasEmail {
			if email, ok := emailValue.(string); ok {
				logger.Debug("User email from token: %s", email)
				c.Set("userEmail", email)
			}
		}

		// Fetch full user object if auth handlers are available
		if err := e.fetchAndSetUserObject(c); err != nil {
			logger.Debug("Failed to fetch full user object: %v", err)
			// Continue execution even if we can't fetch the full user object
		}
	}

	return nil
}

// fetchAndSetUserObject fetches the full user object and sets it in context
func (e *ClaimsExtractor) fetchAndSetUserObject(c *gin.Context) error {
	if e.authHandlers == nil {
		return fmt.Errorf("auth handlers not available")
	}

	logger := logging.GetContextLogger(c)

	// Get the auth service from the handlers to fetch user by ID
	dbManager := auth.GetDatabaseManager()
	if dbManager == nil {
		return fmt.Errorf("database manager not available")
	}

	service, err := auth.NewService(dbManager, auth.ConfigFromUnified(e.config))
	if err != nil {
		return fmt.Errorf("failed to create auth service for user lookup: %w", err)
	}

	// If we have email from claims, use it. Otherwise try to get user by ID
	if email := c.GetString("userEmail"); email != "" {
		user, err := service.GetUserByEmail(c.Request.Context(), email)
		if err != nil {
			return fmt.Errorf("failed to fetch user by email %s: %w", email, err)
		}

		// Set the full user object in context using auth package's expected key
		c.Set(string(auth.UserContextKey), user)
		logger.Debug("Full user object set in context for user: %s", user.Email)
	}
	// TODO: Add GetUserByID method to service

	return nil
}

// JWTAuthenticator orchestrates the JWT authentication process
type JWTAuthenticator struct {
	tokenExtractor   *TokenExtractor
	tokenValidator   *TokenValidator
	blacklistChecker *TokenBlacklistChecker
	claimsExtractor  *ClaimsExtractor
}

// NewJWTAuthenticator creates a new JWT authenticator
func NewJWTAuthenticator(cfg *config.Config, tokenBlacklist *auth.TokenBlacklist, authHandlers *auth.Handlers) *JWTAuthenticator {
	return &JWTAuthenticator{
		tokenExtractor:   &TokenExtractor{},
		tokenValidator:   NewTokenValidator(cfg),
		blacklistChecker: NewTokenBlacklistChecker(tokenBlacklist),
		claimsExtractor:  NewClaimsExtractor(authHandlers, cfg),
	}
}

// AuthenticateRequest performs the complete JWT authentication process
func (a *JWTAuthenticator) AuthenticateRequest(c *gin.Context) error {
	logger := logging.GetContextLogger(c)

	// Extract token from request
	tokenStr, err := a.tokenExtractor.ExtractToken(c)
	if err != nil {
		return &AuthError{
			Code:        "unauthorized",
			Description: err.Error(),
			StatusCode:  http.StatusUnauthorized,
		}
	}

	// Validate token
	token, err := a.tokenValidator.ValidateToken(c, tokenStr)
	if err != nil {
		return &AuthError{
			Code:        "unauthorized",
			Description: err.Error(),
			StatusCode:  http.StatusUnauthorized,
		}
	}

	// Check if token is blacklisted
	if err := a.blacklistChecker.CheckBlacklist(c.Request.Context(), tokenStr); err != nil {
		if strings.Contains(err.Error(), "revoked") {
			return &AuthError{
				Code:        "unauthorized",
				Description: "Token has been revoked",
				StatusCode:  http.StatusUnauthorized,
			}
		}
		logger.Error("Failed to check token blacklist: %v", err)
		return &AuthError{
			Code:        "server_error",
			Description: "Authentication service error",
			StatusCode:  http.StatusInternalServerError,
		}
	}

	// Extract claims and set in context
	if err := a.claimsExtractor.ExtractAndSetClaims(c, token); err != nil {
		logger.Error("Failed to extract claims: %v", err)
		return &AuthError{
			Code:        "server_error",
			Description: "Authentication processing error",
			StatusCode:  http.StatusInternalServerError,
		}
	}

	return nil
}

// AuthError represents an authentication error
type AuthError struct {
	Code        string
	Description string
	StatusCode  int
}

// Error implements the error interface
func (e *AuthError) Error() string {
	return fmt.Sprintf("auth error: %s - %s", e.Code, e.Description)
}

// PublicPathChecker handles checking if a path is public
type PublicPathChecker struct{}

// IsPublicPath checks if the current request is for a public path
func (p *PublicPathChecker) IsPublicPath(c *gin.Context) bool {
	logger := logging.GetContextLogger(c)

	// Check if isPublicPath is set in context
	isPublic, exists := c.Get("isPublicPath")
	logger.Debug("[JWT_MIDDLEWARE] Context check - isPublicPath exists: %t, value: %v", exists, isPublic)

	// Skip authentication for public paths
	if exists && isPublic.(bool) {
		logger.Debug("[JWT_MIDDLEWARE] ✅ Skipping authentication for public path: %s", c.Request.URL.Path)
		// Set a dummy user for context consistency if needed
		c.Set("userEmail", "anonymous")
		logger.Debug("[JWT_MIDDLEWARE] Set userEmail=anonymous for public path")
		return true
	}

	logger.Debug("[JWT_MIDDLEWARE] ❌ Authentication required for private path: %s", c.Request.URL.Path)
	return false
}
