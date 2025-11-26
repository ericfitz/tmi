package api

import (
	"net/http"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AuthServiceAdapter adapts the auth package's Handlers to implement our AuthService interface
type AuthServiceAdapter struct {
	handlers *auth.Handlers
}

// NewAuthServiceAdapter creates a new adapter for auth handlers
func NewAuthServiceAdapter(handlers *auth.Handlers) *AuthServiceAdapter {
	return &AuthServiceAdapter{
		handlers: handlers,
	}
}

// GetProviders delegates to auth handlers
func (a *AuthServiceAdapter) GetProviders(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetProviders called - delegating to auth.Handlers")
	a.handlers.GetProviders(c)
}

// GetSAMLProviders delegates to auth handlers
func (a *AuthServiceAdapter) GetSAMLProviders(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetSAMLProviders called - delegating to auth.Handlers")
	a.handlers.GetSAMLProviders(c)
}

// Authorize delegates to auth handlers
func (a *AuthServiceAdapter) Authorize(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Authorize called - delegating to auth.Handlers")
	a.handlers.Authorize(c)
}

// Callback delegates to auth handlers
func (a *AuthServiceAdapter) Callback(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Callback called - delegating to auth.Handlers")
	a.handlers.Callback(c)
}

// Exchange delegates to auth handlers
func (a *AuthServiceAdapter) Exchange(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Exchange called - delegating to auth.Handlers")
	a.handlers.Exchange(c)
}

// Refresh delegates to auth handlers
func (a *AuthServiceAdapter) Refresh(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Refresh called - delegating to auth.Handlers")
	a.handlers.Refresh(c)
}

// Logout delegates to auth handlers
func (a *AuthServiceAdapter) Logout(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Logout called - delegating to auth.Handlers")
	a.handlers.Logout(c)
}

// Me delegates to auth handlers, with fallback user lookup if needed
func (a *AuthServiceAdapter) Me(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Me called - processing user context")

	// First check if this method is actually being called by our OpenAPI integration

	// Check if user is already in context (set by auth middleware)
	if _, exists := c.Get(string(auth.UserContextKey)); exists {
		// User is already in context, delegate directly
		a.handlers.Me(c)
		return
	}

	// User not in context, try to fetch it using the userEmail from JWT middleware
	userEmailInterface, exists := c.Get("userEmail")
	if !exists {
		c.Header("WWW-Authenticate", "Bearer")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "AuthServiceAdapter: User not authenticated - no userEmail in context",
		})
		return
	}

	userEmail, ok := userEmailInterface.(string)
	if !ok || userEmail == "" {
		c.Header("WWW-Authenticate", "Bearer")
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid user context",
		})
		return
	}

	// Get database manager and fetch user
	dbManager := auth.GetDatabaseManager()
	if dbManager == nil {
		slogging.Get().WithContext(c).Error("AuthServiceAdapter: Database manager not available for user lookup (userName: %s)", userEmail)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Database not available",
		})
		return
	}

	// Create auth service to fetch user
	authConfig := auth.ConfigFromUnified(&config.Config{
		Database: config.DatabaseConfig{
			Postgres: config.PostgresConfig{
				Host:     "localhost",
				Port:     "5432",
				User:     "tmi_dev",
				Password: "dev123",
				Database: "tmi_dev",
				SSLMode:  "disable",
			},
		},
	})

	service, err := auth.NewService(dbManager, authConfig)
	if err != nil {
		slogging.Get().WithContext(c).Error("AuthServiceAdapter: Failed to create auth service for user lookup (userName: %s): %v", userEmail, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Auth service unavailable",
		})
		return
	}

	// Fetch user by email
	user, err := service.GetUserByEmail(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "User not found",
		})
		return
	}

	// Set user in context and delegate to handlers
	c.Set(string(auth.UserContextKey), user)
	a.handlers.Me(c)
}

// GetJWKS delegates to auth handlers
func (a *AuthServiceAdapter) GetJWKS(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetJWKS called - delegating to auth.Handlers")
	a.handlers.GetJWKS(c)
}

// GetSAMLMetadata delegates to auth handlers for SAML metadata
func (a *AuthServiceAdapter) GetSAMLMetadata(c *gin.Context, providerID string) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetSAMLMetadata called for provider: %s", providerID)
	a.handlers.GetSAMLMetadata(c, providerID)
}

// InitiateSAMLLogin delegates to auth handlers to start SAML authentication
func (a *AuthServiceAdapter) InitiateSAMLLogin(c *gin.Context, providerID string, clientCallback *string) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] InitiateSAMLLogin called for provider: %s", providerID)
	a.handlers.InitiateSAMLLogin(c, providerID, clientCallback)
}

// ProcessSAMLResponse delegates to auth handlers to process SAML assertion
func (a *AuthServiceAdapter) ProcessSAMLResponse(c *gin.Context, providerID string, samlResponse string, relayState string) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] ProcessSAMLResponse called for provider: %s", providerID)
	a.handlers.ProcessSAMLResponse(c, providerID, samlResponse, relayState)
}

// ProcessSAMLLogout delegates to auth handlers for SAML logout
func (a *AuthServiceAdapter) ProcessSAMLLogout(c *gin.Context, providerID string, samlRequest string) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] ProcessSAMLLogout called for provider: %s", providerID)
	a.handlers.ProcessSAMLLogout(c, providerID, samlRequest)
}

// GetOpenIDConfiguration delegates to auth handlers
func (a *AuthServiceAdapter) GetOpenIDConfiguration(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetOpenIDConfiguration called - delegating to auth.Handlers")
	a.handlers.GetOpenIDConfiguration(c)
}

// GetOAuthAuthorizationServerMetadata delegates to auth handlers
func (a *AuthServiceAdapter) GetOAuthAuthorizationServerMetadata(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetOAuthAuthorizationServerMetadata called - delegating to auth.Handlers")
	a.handlers.GetOAuthAuthorizationServerMetadata(c)
}

// GetOAuthProtectedResourceMetadata delegates to auth handlers
func (a *AuthServiceAdapter) GetOAuthProtectedResourceMetadata(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetOAuthProtectedResourceMetadata called - delegating to auth.Handlers")
	a.handlers.GetOAuthProtectedResourceMetadata(c)
}

// IntrospectToken delegates to auth handlers
func (a *AuthServiceAdapter) IntrospectToken(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] IntrospectToken called - delegating to auth.Handlers")
	a.handlers.IntrospectToken(c)
}

// GetService returns the underlying auth service for advanced operations
func (a *AuthServiceAdapter) GetService() *auth.Service {
	return a.handlers.Service()
}
