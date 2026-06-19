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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route API info request to the API info handler
func (s *Server) GetApiInfo(c *gin.Context) {
	// Delegate to ApiInfoHandler for proper OpenAPI-compliant response
	handler := NewApiInfoHandler(s)
	handler.GetApiInfo(c)
}

// Authentication Methods (delegate to auth service)

// HandleOAuthCallback handles OAuth callback
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route OAuth callback to the auth service for token exchange
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route OAuth authorization request to the auth service to initiate an OAuth flow
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

// StepUpAuthenticate handles GET /oauth2/step_up — fresh-prompt step-up
// re-authentication. Delegates to the auth service. #397.
// SEM@3b3ce007aac967644943c133123d85a9a1525644: route step-up re-authentication request to the auth service
func (s *Server) StepUpAuthenticate(c *gin.Context, params StepUpAuthenticateParams) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] StepUpAuthenticate called")
	_ = params // params are read from c.Request.URL.Query() by the underlying handler
	if s.authService != nil {
		s.authService.StepUp(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// RevokeToken revokes a token per RFC 7009 (POST /oauth2/revoke)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route token revocation request to the auth service per RFC 7009
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route logout request to the auth service for the current user's session
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route OIDC userinfo request to the auth service for the current user
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route /me profile request to the auth service, including admin status
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route account deletion request to the user deletion handler via challenge-response
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route ownership transfer request for the current user to the transfer handler
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin ownership transfer for a target user to the transfer handler
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route request listing configured OAuth providers to the auth service
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch cached groups for a given identity provider and annotate which are used in authorizations (reads DB)
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
	// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: hold group name, display name, and authorization-usage flag for a provider group (pure)
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list groups referenced in any threat model authorization (reads DB)
func (s *Server) getGroupsUsedInAuthorizations(_ context.Context) []string {
	// Query the database for all unique groups used in authorizations
	// For now, return empty list - this would require querying all Authorization objects
	// and extracting unique group names
	return []string{}
}

// GetSAMLMetadata returns SAML service provider metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route SAML SP metadata request to the auth adapter for a given SAML provider
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route SAML login initiation to the auth adapter for a given provider
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route SAML assertion consumer service POST to the auth adapter for session establishment
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route SAML single logout GET to the auth adapter for session termination
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route SAML single logout POST to the auth adapter for session termination
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route request listing configured SAML providers to the auth service
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route JWKS request to the auth service for JWT signature verification
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route OAuth 2.0 authorization server metadata request to the auth service
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route OpenID Connect discovery document request to the auth service
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route OAuth 2.0 protected resource metadata request to the auth service per RFC 9728
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route token introspection request to the auth service per RFC 7662
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route token refresh request to the auth service
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
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route OAuth token exchange request to the auth service supporting multiple grant types
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

// StartIdentityLink handles POST /me/identities/link/start (#383).
// Delegates to the auth.Handlers.StartIdentityLink method via the AuthServiceAdapter.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: route identity link initiation request to the auth handler
func (s *Server) StartIdentityLink(c *gin.Context, params StartIdentityLinkParams) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] StartIdentityLink called")
	_ = params // query params are read directly from c.Request.URL.Query() by the handler
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.handlers.StartIdentityLink(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// GetPendingIdentityLink handles GET /me/identities/link/pending/{link_id} (#383).
// SEM@053baa340d412aa135be32953dfcb6133af89b4d: route fetch-pending-identity-link request to the auth handler by link ID
func (s *Server) GetPendingIdentityLink(c *gin.Context, linkId string) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetPendingIdentityLink called")
	// Set the path param so the handler can read it via c.Param("link_id").
	// The OpenAPI middleware passes it as a function argument; we adapt here.
	c.Params = append(c.Params, gin.Param{Key: "link_id", Value: linkId})
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.handlers.GetPendingIdentityLink(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}

// ConfirmIdentityLink handles POST /me/identities/link/confirm (#383).
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: route identity link confirmation request to the auth handler
func (s *Server) ConfirmIdentityLink(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] ConfirmIdentityLink called")
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.handlers.ConfirmIdentityLink(c)
	} else {
		HandleRequestError(c, ServiceUnavailableError("Authentication service not configured"))
	}
}
