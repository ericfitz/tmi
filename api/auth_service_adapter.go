package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuthServiceAdapter adapts the auth package's Handlers to implement our AuthService interface
// SEM@0f62047e453da0091b22aafd9f8f959f3d083927: adapter bridging the auth package's Handlers to the API's AuthService interface (pure)
type AuthServiceAdapter struct {
	handlers *auth.Handlers
	service  *auth.Service
}

// NewAuthServiceAdapter creates a new adapter for auth handlers
// SEM@0f62047e453da0091b22aafd9f8f959f3d083927: build an AuthServiceAdapter wrapping the given auth Handlers (pure)
func NewAuthServiceAdapter(handlers *auth.Handlers) *AuthServiceAdapter {
	return &AuthServiceAdapter{
		handlers: handlers,
		service:  handlers.Service(),
	}
}

// GetProviders delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: delegate the list-OAuth-providers request to auth.Handlers
func (a *AuthServiceAdapter) GetProviders(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetProviders called - delegating to auth.Handlers")
	a.handlers.GetProviders(c)
}

// GetSAMLProviders delegates to auth handlers
// SEM@f2053af9d1a8c6b42c543c9406c5fb607c9c7d69: delegate the list-SAML-providers request to auth.Handlers
func (a *AuthServiceAdapter) GetSAMLProviders(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetSAMLProviders called - delegating to auth.Handlers")
	a.handlers.GetSAMLProviders(c)
}

// Authorize delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: delegate the OAuth authorization request to auth.Handlers
func (a *AuthServiceAdapter) Authorize(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Authorize called - delegating to auth.Handlers")
	a.handlers.Authorize(c)
}

// StepUp delegates to auth.Handlers.StepUp (#397).
// SEM@3b3ce007aac967644943c133123d85a9a1525644: delegate a step-up authentication request to auth.Handlers
func (a *AuthServiceAdapter) StepUp(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] StepUp called - delegating to auth.Handlers")
	a.handlers.StepUp(c)
}

// Callback delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: delegate the OAuth callback request to auth.Handlers
func (a *AuthServiceAdapter) Callback(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Callback called - delegating to auth.Handlers")
	a.handlers.Callback(c)
}

// Exchange delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: delegate the authorization code exchange request to auth.Handlers
func (a *AuthServiceAdapter) Exchange(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Exchange called - delegating to auth.Handlers")
	a.handlers.Exchange(c)
}

// Token delegates to auth handlers (supports all grant types and content types)
// SEM@9af9f8a9f8a6ebe7f9c56c6b1de45cebf7fbd5b1: delegate a token request supporting all grant types to auth.Handlers
func (a *AuthServiceAdapter) Token(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Token called - delegating to auth.Handlers")
	a.handlers.Token(c)
}

// Refresh delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: delegate a token refresh request to auth.Handlers
func (a *AuthServiceAdapter) Refresh(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Refresh called - delegating to auth.Handlers")
	a.handlers.Refresh(c)
}

// Logout delegates to auth handlers (deprecated - use RevokeToken or MeLogout)
// SEM@e01a24e0b115a4483cccea13af361c6ede9d62a5: delegate a logout request to auth.Handlers.MeLogout (deprecated endpoint)
func (a *AuthServiceAdapter) Logout(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] Logout called - delegating to auth.Handlers.MeLogout")
	a.handlers.MeLogout(c)
}

// RevokeToken delegates to auth handlers for RFC 7009 token revocation
// SEM@e01a24e0b115a4483cccea13af361c6ede9d62a5: delegate an RFC 7009 token revocation request to auth.Handlers
func (a *AuthServiceAdapter) RevokeToken(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] RevokeToken called - delegating to auth.Handlers")
	a.handlers.RevokeToken(c)
}

// MeLogout delegates to auth handlers for self-logout
// SEM@e01a24e0b115a4483cccea13af361c6ede9d62a5: delegate a self-logout request to auth.Handlers
func (a *AuthServiceAdapter) MeLogout(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] MeLogout called - delegating to auth.Handlers")
	a.handlers.MeLogout(c)
}

// Me delegates to auth handlers, with fallback user lookup if needed
// SEM@23ecc252aaf49b08f2030803b81a91d5727c7d25: fetch the authenticated user from context or by provider ID and return user profile
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

	// User not in context, try to fetch using provider + provider_user_id from JWT middleware
	providerInterface, exists := c.Get("userProvider")
	if !exists {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Authentication required")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated - no provider in context",
		})
		return
	}
	provider, ok := providerInterface.(string)
	if !ok || provider == "" {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication token")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid provider context",
		})
		return
	}

	providerUserIDInterface, exists := c.Get("userID")
	if !exists {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Authentication required")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated - no provider user ID in context",
		})
		return
	}
	providerUserID, ok := providerUserIDInterface.(string)
	if !ok || providerUserID == "" {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication token")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid user context",
		})
		return
	}

	// Use the existing auth service to fetch user
	if a.service == nil {
		slogging.Get().WithContext(c).Error("AuthServiceAdapter: Auth service not available for user lookup (provider: %s, provider_user_id: %s)", provider, providerUserID)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Auth service unavailable",
		})
		return
	}

	// Fetch user by provider + provider_user_id (unambiguous lookup)
	user, err := a.service.GetUserByProviderID(c.Request.Context(), provider, providerUserID)
	if err != nil {
		slogging.Get().WithContext(c).Warn("AuthServiceAdapter: User not found by provider ID (provider: %s, provider_user_id: %s): %v", provider, providerUserID, err)
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "User not found",
		})
		return
	}

	// Set user in context and delegate to handlers
	c.Set(string(auth.UserContextKey), user)
	a.handlers.Me(c)
}

// GetJWKS delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: fetch the public key set (JWKS) for JWT verification (pure)
func (a *AuthServiceAdapter) GetJWKS(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetJWKS called - delegating to auth.Handlers")
	a.handlers.GetJWKS(c)
}

// GetSAMLMetadata delegates to auth handlers for SAML metadata
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: fetch SAML SP metadata XML for a given provider
func (a *AuthServiceAdapter) GetSAMLMetadata(c *gin.Context, providerID string) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetSAMLMetadata called for provider: %s", providerID)
	a.handlers.GetSAMLMetadata(c, providerID)
}

// InitiateSAMLLogin delegates to auth handlers to start SAML authentication
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: dispatch a SAML authentication redirect to the identity provider
func (a *AuthServiceAdapter) InitiateSAMLLogin(c *gin.Context, providerID string, clientCallback *string) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] InitiateSAMLLogin called for provider: %s", providerID)
	a.handlers.InitiateSAMLLogin(c, providerID, clientCallback)
}

// ProcessSAMLResponse delegates to auth handlers to process SAML assertion
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: validate a SAML assertion and issue a token pair for the authenticated user
func (a *AuthServiceAdapter) ProcessSAMLResponse(c *gin.Context, providerID string, samlResponse string, relayState string) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] ProcessSAMLResponse called for provider: %s", providerID)
	a.handlers.ProcessSAMLResponse(c, providerID, samlResponse, relayState)
}

// ProcessSAMLLogout delegates to auth handlers for SAML logout
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: validate a SAML logout request and invalidate the user's sessions
func (a *AuthServiceAdapter) ProcessSAMLLogout(c *gin.Context, providerID string, samlRequest string) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] ProcessSAMLLogout called for provider: %s", providerID)
	a.handlers.ProcessSAMLLogout(c, providerID, samlRequest)
}

// GetOpenIDConfiguration delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: fetch the OIDC discovery document for this server (pure)
func (a *AuthServiceAdapter) GetOpenIDConfiguration(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetOpenIDConfiguration called - delegating to auth.Handlers")
	a.handlers.GetOpenIDConfiguration(c)
}

// GetOAuthAuthorizationServerMetadata delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: fetch the RFC 8414 authorization server metadata document (pure)
func (a *AuthServiceAdapter) GetOAuthAuthorizationServerMetadata(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetOAuthAuthorizationServerMetadata called - delegating to auth.Handlers")
	a.handlers.GetOAuthAuthorizationServerMetadata(c)
}

// GetOAuthProtectedResourceMetadata delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: fetch the RFC 9728 protected resource metadata document (pure)
func (a *AuthServiceAdapter) GetOAuthProtectedResourceMetadata(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] GetOAuthProtectedResourceMetadata called - delegating to auth.Handlers")
	a.handlers.GetOAuthProtectedResourceMetadata(c)
}

// IntrospectToken delegates to auth handlers
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: validate a bearer token and return its active status and claims (reads DB)
func (a *AuthServiceAdapter) IntrospectToken(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[AUTH_SERVICE_ADAPTER] IntrospectToken called - delegating to auth.Handlers")
	a.handlers.IntrospectToken(c)
}

// GetService returns the underlying auth service for advanced operations
// SEM@bd740ab90ce24a669adc1fa8b8153efbd33bac10: fetch the underlying auth service instance (pure)
func (a *AuthServiceAdapter) GetService() *auth.Service {
	return a.handlers.Service()
}

// IsValidProvider checks if the given provider ID is configured and enabled
// SEM@0eb4bf778ed84abb8fa3d433bf42cc7928258257: validate that an OAuth provider ID is configured and enabled (pure)
func (a *AuthServiceAdapter) IsValidProvider(idp string) bool {
	// Check OAuth providers
	config := a.handlers.Config()
	if providerConfig, exists := config.OAuth.Providers[idp]; exists {
		return providerConfig.Enabled
	}

	// Provider not found or not enabled
	return false
}

// GetProviderGroupsFromCache retrieves all unique groups for a provider from cached user sessions
// SEM@d510ee7a8017fc630e79a21b9480e4f975482b47: fetch all unique cached group names for an identity provider (reads DB)
func (a *AuthServiceAdapter) GetProviderGroupsFromCache(ctx context.Context, idp string) ([]string, error) {
	logger := slogging.Get()
	service := a.GetService()

	// Get the Redis client to scan for user_groups keys
	dbManager := db.GetGlobalManager()
	if dbManager == nil {
		logger.Warn("Database manager not available for group fetching")
		return []string{}, nil
	}

	redisDB := dbManager.Redis()
	if redisDB == nil {
		logger.Warn("Redis not available for group fetching")
		return []string{}, nil
	}

	client := redisDB.GetClient()

	// Scan for all user_groups:* keys
	pattern := "user_groups:*"
	keys, err := client.Keys(ctx, pattern).Result()
	if err != nil {
		logger.Error("Failed to scan for user group keys: %v", err)
		return nil, err
	}

	// Collect all unique groups for this IdP
	groupSet := make(map[string]bool)
	for _, key := range keys {
		// Get the cached groups for this user
		cachedIdP, groups, err := service.GetCachedGroups(ctx, key[len("user_groups:"):])
		if err != nil {
			// Skip this user if we can't read their groups
			continue
		}

		// Only include groups from the requested IdP
		if cachedIdP == idp {
			for _, group := range groups {
				groupSet[group] = true
			}
		}
	}

	// Convert map to slice
	uniqueGroups := make([]string, 0, len(groupSet))
	for group := range groupSet {
		uniqueGroups = append(uniqueGroups, group)
	}

	logger.Debug("Found %d unique groups for provider %s", len(uniqueGroups), idp)
	return uniqueGroups, nil
}

// IssueForInvocation implements DelegationTokenIssuer (api/delegation_token_issuer.go)
// by loading the invoker from the auth user store and asking auth.Service
// to mint a scoped delegation JWT (auth/delegation_token.go). Used by the
// webhook delivery worker to attach a per-attempt token to every
// addon.invoked delivery (T18, #358).
// SEM@e6be8a8f816c564356a656ac18f3693ac7f10369: issue a delegated addon invocation token for the given invoking user
func (a *AuthServiceAdapter) IssueForInvocation(
	ctx context.Context,
	invokerInternalUUID string,
	addonID, deliveryID, threatModelID uuid.UUID,
) (string, error) {
	if a == nil || a.service == nil {
		return "", fmt.Errorf("auth service unavailable")
	}
	user, err := a.service.GetUserByID(ctx, invokerInternalUUID)
	if err != nil {
		return "", fmt.Errorf("load invoker %s: %w", invokerInternalUUID, err)
	}
	return a.service.IssueAddonDelegationToken(ctx, &user, addonID, deliveryID, threatModelID)
}
