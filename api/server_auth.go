package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Complete ServerInterface Implementation - OpenAPI Generated Methods

// API Info Methods

// GetApiInfo returns API information
func (s *Server) GetApiInfo(c *gin.Context) {
	// Delegate to ApiInfoHandler for proper OpenAPI-compliant response
	handler := NewApiInfoHandler(s)
	handler.GetApiInfo(c)
}

// Authentication Methods (delegate to auth service)

// HandleOAuthCallback handles OAuth callback
func (s *Server) HandleOAuthCallback(c *gin.Context, params HandleOAuthCallbackParams) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] HandleOAuthCallback called")
	if s.authService != nil {
		s.authService.Callback(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// AuthorizeOAuthProvider initiates OAuth flow
func (s *Server) AuthorizeOAuthProvider(c *gin.Context, params AuthorizeOAuthProviderParams) {
	logger := slogging.Get()
	var providerStr string
	if params.Idp != nil {
		providerStr = *params.Idp
	} else {
		providerStr = "<default>"
	}
	logger.Debug("[SERVER_INTERFACE] AuthorizeOAuthProvider called for provider: %s", providerStr)
	logger.Debug("[SERVER_INTERFACE] Request URL: %s", c.Request.URL.String())
	logger.Debug("[SERVER_INTERFACE] Auth service configured: %t", s.authService != nil)

	if s.authService != nil {
		logger.Debug("[SERVER_INTERFACE] Delegating to auth service")
		s.authService.Authorize(c)
	} else {
		logger.Debug("[SERVER_INTERFACE] Auth service not configured, returning error")
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// RevokeToken revokes a token per RFC 7009 (POST /oauth2/revoke)
func (s *Server) RevokeToken(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] RevokeToken called")
	if s.authService != nil {
		s.authService.RevokeToken(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// LogoutCurrentUser logs out the current user (POST /me/logout)
func (s *Server) LogoutCurrentUser(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] LogoutCurrentUser called")
	if s.authService != nil {
		s.authService.MeLogout(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// GetCurrentUser gets current user information
func (s *Server) GetCurrentUser(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetCurrentUser called - delegating to authService.Me()")
	if s.authService != nil {
		// Use OIDC-compliant response format for /oauth2/userinfo
		c.Set("oidc_response_format", true)
		s.authService.Me(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// GetCurrentUserProfile gets current user profile with groups and admin status (from /me endpoint)
func (s *Server) GetCurrentUserProfile(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetCurrentUserProfile called (GET /me)")

	if s.authService == nil {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
		return
	}

	// Set a flag to indicate we want to add admin status
	c.Set("add_admin_status", true)

	// Delegate to auth service Me() which handles the user retrieval and groups
	s.authService.Me(c)
}

// DeleteUserAccount handles user account deletion (two-step challenge-response)
func (s *Server) DeleteUserAccount(c *gin.Context, params DeleteUserAccountParams) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] DeleteUserAccount called")

	if s.userDeletionHandler == nil {
		HandleRequestError(c, ServerError("User deletion service not configured"))
		return
	}

	// Convert params to query parameter for handler
	if params.Challenge != nil {
		c.Request.URL.RawQuery = fmt.Sprintf("challenge=%s", *params.Challenge)
	}

	s.userDeletionHandler.DeleteUserAccount(c)
}

// TransferCurrentUserOwnership handles POST /me/transfer
func (s *Server) TransferCurrentUserOwnership(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] TransferCurrentUserOwnership called")

	if s.ownershipTransferHandler == nil {
		HandleRequestError(c, ServerError("Ownership transfer service not configured"))
		return
	}

	s.ownershipTransferHandler.TransferCurrentUserOwnership(c)
}

// TransferAdminUserOwnership handles POST /admin/users/{internal_uuid}/transfer
func (s *Server) TransferAdminUserOwnership(c *gin.Context, internalUuid InternalUuidPathParam) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] TransferAdminUserOwnership called")

	if s.ownershipTransferHandler == nil {
		HandleRequestError(c, ServerError("Ownership transfer service not configured"))
		return
	}

	s.ownershipTransferHandler.TransferAdminUserOwnership(c, internalUuid)
}

// GetAuthProviders lists OAuth providers
func (s *Server) GetAuthProviders(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetAuthProviders called")
	if s.authService != nil {
		s.authService.GetProviders(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// GetProviderGroups returns groups available from a specific identity provider
func (s *Server) GetProviderGroups(c *gin.Context, idp string) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetProviderGroups called for IdP: %s", idp)

	// Validate that the provider exists
	if !s.authService.IsValidProvider(idp) {
		logger.Debug("[SERVER_INTERFACE] Provider %s not found", idp)
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "OAuth provider not found",
		})
		return
	}

	// Get groups from the provider by querying all cached user groups for this IdP
	// Note: This returns groups seen in recent sessions, not all groups from the IdP
	// For complete group lists, the IdP would need a dedicated groups API
	ctx := c.Request.Context()
	groups, err := s.authService.GetProviderGroupsFromCache(ctx, idp)
	if err != nil {
		logger.Error("[SERVER_INTERFACE] Failed to get groups for provider %s: %v", idp, err)
		// Return empty list on error rather than failing
		groups = []string{}
	}

	// Check which groups are used in authorizations
	usedGroups := s.getGroupsUsedInAuthorizations(ctx)

	// Build response
	type GroupInfo struct {
		Name                 string `json:"name"`
		DisplayName          string `json:"display_name,omitempty"`
		UsedInAuthorizations bool   `json:"used_in_authorizations"`
	}

	groupInfos := make([]GroupInfo, 0, len(groups))
	for _, group := range groups {
		groupInfos = append(groupInfos, GroupInfo{
			Name:                 group,
			DisplayName:          group, // Use name as display name unless we have better metadata
			UsedInAuthorizations: slices.Contains(usedGroups, group),
		})
	}

	response := struct {
		IdP    string      `json:"idp"`
		Groups []GroupInfo `json:"groups"`
	}{
		IdP:    idp,
		Groups: groupInfos,
	}

	c.JSON(http.StatusOK, response)
}

// getGroupsUsedInAuthorizations returns a list of groups that are used in threat model authorizations
func (s *Server) getGroupsUsedInAuthorizations(_ context.Context) []string {
	// Query the database for all unique groups used in authorizations
	// For now, return empty list - this would require querying all Authorization objects
	// and extracting unique group names
	return []string{}
}

// GetSAMLMetadata returns SAML service provider metadata
func (s *Server) GetSAMLMetadata(c *gin.Context, provider string) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetSAMLMetadata called for provider: %s", provider)

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
		return
	}

	// Delegate to auth service for SAML metadata
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.GetSAMLMetadata(c, provider)
	} else {
		HandleRequestError(c, NotImplementedError("SAML not supported by current auth provider"))
	}
}

// InitiateSAMLLogin starts SAML authentication flow
func (s *Server) InitiateSAMLLogin(c *gin.Context, provider string, params InitiateSAMLLoginParams) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] InitiateSAMLLogin called for provider: %s", provider)

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
		return
	}

	// Delegate to auth service for SAML login
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.InitiateSAMLLogin(c, provider, params.ClientCallback)
	} else {
		HandleRequestError(c, NotImplementedError("SAML not supported by current auth provider"))
	}
}

// ProcessSAMLResponse handles SAML assertion consumer service
func (s *Server) ProcessSAMLResponse(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] ProcessSAMLResponse called")

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
		return
	}

	// Parse form data
	samlResponse := c.PostForm("SAMLResponse")
	relayState := c.PostForm("RelayState")

	if samlResponse == "" {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "Missing SAMLResponse",
		})
		return
	}

	// Delegate to auth service for SAML response processing
	// The provider ID will be retrieved from the relay state by the auth handler
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.ProcessSAMLResponse(c, "", samlResponse, relayState)
	} else {
		HandleRequestError(c, NotImplementedError("SAML not supported by current auth provider"))
	}
}

// ProcessSAMLLogout handles SAML single logout (GET)
func (s *Server) ProcessSAMLLogout(c *gin.Context, params ProcessSAMLLogoutParams) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] ProcessSAMLLogout called (GET)")

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
		return
	}

	// Get provider ID from query parameter
	providerID := c.Query("provider")
	if providerID == "" {
		providerID = "default"
	}

	// Delegate to auth service for SAML logout
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.ProcessSAMLLogout(c, providerID, params.SAMLRequest)
	} else {
		HandleRequestError(c, NotImplementedError("SAML not supported by current auth provider"))
	}
}

// ProcessSAMLLogoutPost handles SAML single logout (POST)
func (s *Server) ProcessSAMLLogoutPost(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] ProcessSAMLLogoutPost called (POST)")

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
		return
	}

	// Parse form data
	samlRequest := c.PostForm("SAMLRequest")
	if samlRequest == "" {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "Missing SAMLRequest",
		})
		return
	}

	// Get provider ID from form or default
	providerID := c.PostForm("provider")
	if providerID == "" {
		providerID = "default"
	}

	// Delegate to auth service for SAML logout
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.ProcessSAMLLogout(c, providerID, samlRequest)
	} else {
		HandleRequestError(c, NotImplementedError("SAML not supported by current auth provider"))
	}
}

// GetSAMLProviders implements ServerInterface
func (s *Server) GetSAMLProviders(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetSAMLProviders called")

	if s.authService == nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Auth service not configured",
		})
		return
	}

	s.authService.GetSAMLProviders(c)
}

// GetJWKS returns the JSON Web Key Set for JWT signature verification
func (s *Server) GetJWKS(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetJWKS called")
	if s.authService != nil {
		// Delegate to auth service (assuming it has a GetJWKS method)
		if jwksHandler, ok := s.authService.(interface{ GetJWKS(c *gin.Context) }); ok {
			jwksHandler.GetJWKS(c)
		} else {
			HandleRequestError(c, NotImplementedError("JWKS endpoint not supported by current auth provider"))
		}
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// GetOAuthAuthorizationServerMetadata returns OAuth 2.0 Authorization Server Metadata
func (s *Server) GetOAuthAuthorizationServerMetadata(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetOAuthAuthorizationServerMetadata called")
	if s.authService != nil {
		// Delegate to auth service (assuming it has this method)
		if metaHandler, ok := s.authService.(interface{ GetOAuthAuthorizationServerMetadata(c *gin.Context) }); ok {
			metaHandler.GetOAuthAuthorizationServerMetadata(c)
		} else {
			HandleRequestError(c, NotImplementedError("OAuth metadata endpoint not supported by current auth provider"))
		}
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// GetOpenIDConfiguration returns OpenID Connect configuration
func (s *Server) GetOpenIDConfiguration(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetOpenIDConfiguration called")
	if s.authService != nil {
		// Delegate to auth service (assuming it has this method)
		if oidcHandler, ok := s.authService.(interface{ GetOpenIDConfiguration(c *gin.Context) }); ok {
			oidcHandler.GetOpenIDConfiguration(c)
		} else {
			HandleRequestError(c, NotImplementedError("OpenID configuration endpoint not supported by current auth provider"))
		}
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// GetOAuthProtectedResourceMetadata returns OAuth 2.0 protected resource metadata as per RFC 9728
func (s *Server) GetOAuthProtectedResourceMetadata(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetOAuthProtectedResourceMetadata called")
	if s.authService != nil {
		// Delegate to auth service (assuming it has this method)
		if metaHandler, ok := s.authService.(interface{ GetOAuthProtectedResourceMetadata(c *gin.Context) }); ok {
			metaHandler.GetOAuthProtectedResourceMetadata(c)
		} else {
			HandleRequestError(c, NotImplementedError("OAuth protected resource metadata endpoint not supported by current auth provider"))
		}
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// IntrospectToken handles token introspection requests per RFC 7662
func (s *Server) IntrospectToken(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] IntrospectToken called")
	if s.authService != nil {
		// Delegate to auth service (assuming it has an IntrospectToken method)
		if introspectHandler, ok := s.authService.(interface{ IntrospectToken(c *gin.Context) }); ok {
			introspectHandler.IntrospectToken(c)
		} else {
			HandleRequestError(c, NotImplementedError("Token introspection endpoint not supported by current auth provider"))
		}
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// RefreshToken refreshes JWT token
func (s *Server) RefreshToken(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] RefreshToken called")
	if s.authService != nil {
		s.authService.Refresh(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// ExchangeOAuthCode exchanges auth code for tokens
func (s *Server) ExchangeOAuthCode(c *gin.Context, params ExchangeOAuthCodeParams) {
	logger := slogging.Get()
	var providerStr string
	if params.Idp != nil {
		providerStr = *params.Idp
	} else {
		providerStr = "<default>"
	}
	logger.Info("[SERVER_INTERFACE] ExchangeOAuthCode called for provider: %s", providerStr)
	if s.authService != nil {
		// Use Token handler which supports all grant types (authorization_code, client_credentials, refresh_token)
		// and both JSON and form-urlencoded content types
		s.authService.Token(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}
