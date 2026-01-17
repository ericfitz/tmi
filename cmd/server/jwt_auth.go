package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenExtractor handles extracting JWT tokens from requests
type TokenExtractor struct{}

// ExtractToken extracts the JWT token from the request
func (t *TokenExtractor) ExtractToken(c *gin.Context) (string, error) {
	logger := slogging.GetContextLogger(c)

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
	logger.Debug("[JWT_MIDDLEWARE] Authorization header value: '%s' (empty: %t)", slogging.RedactSensitiveInfo(authHeader), authHeader == "")

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
	authHandlers *auth.Handlers
}

// NewTokenValidator creates a new token validator
func NewTokenValidator(authHandlers *auth.Handlers) *TokenValidator {
	return &TokenValidator{
		authHandlers: authHandlers,
	}
}

// ValidateToken validates a JWT token and returns the parsed token
func (v *TokenValidator) ValidateToken(c *gin.Context, tokenStr string) (*jwt.Token, error) {
	logger := slogging.GetContextLogger(c)

	if v.authHandlers == nil {
		logger.Error("Auth handlers not available for token validation")
		return nil, fmt.Errorf("auth handlers not available")
	}

	// Use the centralized JWT verification
	claims := jwt.MapClaims{}
	token, err := v.authHandlers.Service().GetKeyManager().VerifyToken(tokenStr, claims)
	if err != nil {
		logger.Warn("Authentication failed: Invalid or expired token - %v", err)
		return nil, fmt.Errorf("invalid or expired token: %w", err)
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
	logger := slogging.GetContextLogger(c)

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return fmt.Errorf("invalid token claims")
	}

	// Extract provider user ID (sub claim contains provider's user ID, NOT internal_uuid)
	// For service accounts, sub format is: "sa:{credential_id}:{owner_provider_user_id}"
	if sub, ok := claims["sub"].(string); ok {
		// Check if this is a service account token
		if strings.HasPrefix(sub, "sa:") {
			// Parse service account subject: "sa:{credential_id}:{owner_provider_user_id}"
			parts := strings.SplitN(sub, ":", 3)
			if len(parts) == 3 {
				credentialID := parts[1]
				ownerProviderUserID := parts[2]

				// Set service account context
				c.Set("isServiceAccount", true)
				c.Set("serviceAccountCredentialID", credentialID)
				c.Set("userID", ownerProviderUserID) // Owner's provider user ID

				logger.Debug("Service account authenticated: credential_id=%s, owner=%s", credentialID, ownerProviderUserID)
			} else {
				logger.Warn("Invalid service account subject format: %s", sub)
				c.Set("isServiceAccount", false)
				c.Set("userID", sub)
			}
		} else {
			// Regular user token
			c.Set("isServiceAccount", false)
			c.Set("userID", sub) // For backward compatibility, this contains provider_user_id (from JWT sub)
			logger.Debug("Authenticated provider user ID: %s", sub)
		}

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

		// Extract IdP (provider) if present
		if idpValue, hasIdP := claims["idp"]; hasIdP {
			if idp, ok := idpValue.(string); ok {
				logger.Debug("User IdP from token: %s", idp)
				c.Set("userIdP", idp)
				c.Set("userProvider", idp) // Set both for compatibility
			}
		}

		// Extract groups if present
		if groupsValue, hasGroups := claims["groups"]; hasGroups {
			if groupsArray, ok := groupsValue.([]interface{}); ok {
				groups := make([]string, 0, len(groupsArray))
				for _, g := range groupsArray {
					if groupStr, ok := g.(string); ok {
						groups = append(groups, groupStr)
					}
				}
				logger.Debug("User groups from token: %v", groups)
				c.Set("userGroups", groups)
			}
		}

		// Fetch full user object using provider + provider_user_id
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

	logger := slogging.GetContextLogger(c)

	// Get the auth service from the handlers to fetch user by provider + provider_user_id
	dbManager := auth.GetDatabaseManager()
	if dbManager == nil {
		return fmt.Errorf("database manager not available")
	}

	service, err := auth.NewService(dbManager, auth.ConfigFromUnified(e.config))
	if err != nil {
		return fmt.Errorf("failed to create auth service for user lookup: %w", err)
	}

	// Get provider and provider_user_id from context
	provider := c.GetString("userProvider")
	providerUserID := c.GetString("userID") // This contains provider_user_id from JWT sub claim

	if provider != "" && providerUserID != "" {
		// Look up user by provider + provider_user_id (uses cache if available)
		user, err := service.GetUserByProviderID(c.Request.Context(), provider, providerUserID)
		if err != nil {
			return fmt.Errorf("failed to fetch user by provider %s:%s: %w", provider, providerUserID, err)
		}

		// Set the full user object in context using auth package's expected key
		c.Set(string(auth.UserContextKey), user)
		// Also set the internal UUID for handlers that need it
		c.Set("userInternalUUID", user.InternalUUID)
		logger.Debug("Full user object set in context for user: %s (internal_uuid: %s)", user.Email, user.InternalUUID)
		return nil
	}

	// Fallback: If we have email from claims, use it
	if email := c.GetString("userEmail"); email != "" {
		user, err := service.GetUserByEmail(c.Request.Context(), email)
		if err != nil {
			return fmt.Errorf("failed to fetch user by email %s: %w", email, err)
		}

		// Set the full user object in context using auth package's expected key
		c.Set(string(auth.UserContextKey), user)
		// Also set the internal UUID for handlers that need it
		c.Set("userInternalUUID", user.InternalUUID)
		logger.Debug("Full user object set in context for user: %s (internal_uuid: %s)", user.Email, user.InternalUUID)
		return nil
	}

	return fmt.Errorf("insufficient claims to fetch user object")
}

// JWTAuthenticator orchestrates the JWT authentication process
type JWTAuthenticator struct {
	config           *config.Config
	tokenExtractor   *TokenExtractor
	tokenValidator   *TokenValidator
	blacklistChecker *TokenBlacklistChecker
	claimsExtractor  *ClaimsExtractor
}

// NewJWTAuthenticator creates a new JWT authenticator
func NewJWTAuthenticator(cfg *config.Config, tokenBlacklist *auth.TokenBlacklist, authHandlers *auth.Handlers) *JWTAuthenticator {
	return &JWTAuthenticator{
		config:           cfg,
		tokenExtractor:   &TokenExtractor{},
		tokenValidator:   NewTokenValidator(authHandlers),
		blacklistChecker: NewTokenBlacklistChecker(tokenBlacklist),
		claimsExtractor:  NewClaimsExtractor(authHandlers, cfg),
	}
}

// AuthenticateRequest performs the complete JWT authentication process
func (a *JWTAuthenticator) AuthenticateRequest(c *gin.Context) error {
	logger := slogging.GetContextLogger(c)

	// Extract token from request
	tokenStr, err := a.tokenExtractor.ExtractToken(c)
	if err != nil {
		// Use generic error message to avoid leaking implementation details
		return &AuthError{
			Code:        "unauthorized",
			Description: "Authentication required",
			StatusCode:  http.StatusUnauthorized,
		}
	}

	// Validate token
	token, err := a.tokenValidator.ValidateToken(c, tokenStr)
	if err != nil {
		// Use generic error message to avoid leaking implementation details
		return &AuthError{
			Code:        "unauthorized",
			Description: "Authentication required",
			StatusCode:  http.StatusUnauthorized,
		}
	}

	// Check if token is blacklisted
	if err := a.blacklistChecker.CheckBlacklist(c.Request.Context(), tokenStr); err != nil {
		if strings.Contains(err.Error(), "revoked") {
			// Use generic error message to avoid leaking implementation details
			return &AuthError{
				Code:        "unauthorized",
				Description: "Authentication required",
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

	// Auto-promotion: If enabled and no administrators exist, promote first user
	if a.config.Auth.AutoPromoteFirstUser {
		if err := a.autoPromoteFirstUser(c, logger); err != nil {
			logger.Warn("Auto-promotion check failed (non-fatal): %v", err)
			// Don't fail authentication if auto-promotion fails - this is a best-effort feature
		}
	}

	return nil
}

// autoPromoteFirstUser checks if any administrators exist and promotes the current user if none exist
func (a *JWTAuthenticator) autoPromoteFirstUser(c *gin.Context, logger slogging.SimpleLogger) error {
	// Only check if GlobalAdministratorStore is initialized
	if api.GlobalAdministratorStore == nil {
		return fmt.Errorf("GlobalAdministratorStore not initialized")
	}

	// Check if this is a GORM store (required for HasAnyAdministrators)
	dbStore, ok := api.GlobalAdministratorStore.(*api.GormAdministratorStore)
	if !ok {
		return fmt.Errorf("GlobalAdministratorStore is not a GORM store")
	}

	// Check if any administrators exist
	hasAdmins, err := dbStore.HasAnyAdministrators(c.Request.Context())
	if err != nil {
		return fmt.Errorf("failed to check for existing administrators: %w", err)
	}

	// If administrators already exist, no auto-promotion needed
	if hasAdmins {
		logger.Debug("Auto-promotion skipped: administrators already exist")
		return nil
	}

	// Get current user information from context
	userInternalUUID := c.GetString("userInternalUUID")
	userEmail := c.GetString("userEmail")
	provider := c.GetString("userProvider")

	if userInternalUUID == "" || provider == "" {
		return fmt.Errorf("missing user context (userInternalUUID or provider)")
	}

	logger.Info("Auto-promoting first user to administrator: email=%s, provider=%s", userEmail, provider)

	// Parse user UUID
	userUUID, err := uuid.Parse(userInternalUUID)
	if err != nil {
		return fmt.Errorf("invalid user UUID: %w", err)
	}

	// Create administrator grant for first user
	admin := api.DBAdministrator{
		ID:               uuid.New(),
		UserInternalUUID: &userUUID,
		SubjectType:      "user",
		Provider:         provider,
		Notes:            "Auto-promoted as first administrator",
	}

	// Create the administrator grant
	if err := api.GlobalAdministratorStore.Create(c.Request.Context(), admin); err != nil {
		logger.Error("Failed to auto-promote first user to administrator: email=%s, provider=%s, error=%v",
			userEmail, provider, err)
		return fmt.Errorf("failed to create administrator grant: %w", err)
	}

	// AUDIT LOG: Log auto-promotion success
	logger.Info("[AUDIT] Successfully auto-promoted first user to administrator: grant_id=%s, user_id=%s, email=%s, provider=%s",
		admin.ID, userInternalUUID, userEmail, provider)

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
	logger := slogging.GetContextLogger(c)

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
