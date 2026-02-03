package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Server is the main API server instance
type Server struct {
	// Handlers
	threatModelHandler         *ThreatModelHandler
	documentHandler            *DocumentSubResourceHandler
	noteHandler                *NoteSubResourceHandler
	repositoryHandler          *RepositorySubResourceHandler
	assetHandler               *AssetSubResourceHandler
	threatHandler              *ThreatSubResourceHandler
	documentMetadataHandler    *DocumentMetadataHandler
	noteMetadataHandler        *NoteMetadataHandler
	repositoryMetadataHandler  *RepositoryMetadataHandler
	assetMetadataHandler       *AssetMetadataHandler
	threatMetadataHandler      *ThreatMetadataHandler
	threatModelMetadataHandler *ThreatModelMetadataHandler
	userDeletionHandler        *UserDeletionHandler
	// WebSocket hub
	wsHub *WebSocketHub
	// Auth handlers (for delegating auth-related methods)
	authService AuthService // We'll need to add this dependency
	// Rate limiters
	apiRateLimiter      *APIRateLimiter
	webhookRateLimiter  *WebhookRateLimiter
	ipRateLimiter       *IPRateLimiter
	authFlowRateLimiter *AuthFlowRateLimiter
	// Settings service for database-stored configuration
	settingsService *SettingsService
	// Config provider for settings migration
	configProvider ConfigProvider
}

// ConfigProvider provides access to migratable settings from configuration
type ConfigProvider interface {
	GetMigratableSettings() []MigratableSetting
}

// MigratableSetting represents a setting that can be migrated from config to database
type MigratableSetting struct {
	Key         string
	Value       string
	Type        string
	Description string
}

// NewServer creates a new API server instance
func NewServer(wsLoggingConfig slogging.WebSocketLoggingConfig, inactivityTimeout time.Duration) *Server {
	wsHub := NewWebSocketHub(wsLoggingConfig, inactivityTimeout)
	return &Server{
		threatModelHandler:         NewThreatModelHandler(wsHub),
		documentHandler:            NewDocumentSubResourceHandler(GlobalDocumentStore, nil, nil, nil),
		noteHandler:                NewNoteSubResourceHandler(GlobalNoteStore, nil, nil, nil),
		repositoryHandler:          NewRepositorySubResourceHandler(GlobalRepositoryStore, nil, nil, nil),
		assetHandler:               NewAssetSubResourceHandler(GlobalAssetStore, nil, nil, nil),
		threatHandler:              NewThreatSubResourceHandler(GlobalThreatStore, nil, nil, nil),
		documentMetadataHandler:    NewDocumentMetadataHandler(GlobalMetadataStore, nil, nil, nil),
		noteMetadataHandler:        NewNoteMetadataHandler(GlobalMetadataStore, nil, nil, nil),
		repositoryMetadataHandler:  NewRepositoryMetadataHandler(GlobalMetadataStore, nil, nil, nil),
		assetMetadataHandler:       NewAssetMetadataHandler(GlobalMetadataStore, nil, nil, nil),
		threatMetadataHandler:      NewThreatMetadataHandler(GlobalMetadataStore, nil, nil, nil),
		threatModelMetadataHandler: NewThreatModelMetadataHandler(GlobalMetadataStore, nil, nil, nil),
		wsHub:                      wsHub,
		// authService will be set separately via SetAuthService
	}
}

// NewServerForTests creates a server with default test configuration
func NewServerForTests() *Server {
	return NewServer(slogging.WebSocketLoggingConfig{
		Enabled:        false, // Disable logging in tests by default
		RedactTokens:   true,
		MaxMessageSize: 5 * 1024,
		OnlyDebugLevel: true,
	}, 30*time.Second) // Short timeout for tests
}

// ServerInfo provides information about the server configuration
type ServerInfo struct {
	// Whether TLS is enabled
	TLSEnabled bool `json:"tls_enabled"`
	// Subject name for TLS certificate
	TLSSubjectName string `json:"tls_subject_name,omitempty"`
	// WebSocket base URL
	WebSocketBaseURL string `json:"websocket_base_url"`
}

// RegisterHandlers registers custom API handlers with the router
func (s *Server) RegisterHandlers(r *gin.Engine) {
	logger := slogging.Get()
	logger.Info("[API_SERVER] Starting custom route registration")

	// Register WebSocket handler - it needs a custom route because it's not part of the OpenAPI spec
	logger.Info("[API_SERVER] Registering WebSocket route: GET /threat_models/:threat_model_id/diagrams/:diagram_id/ws")
	r.GET("/threat_models/:threat_model_id/diagrams/:diagram_id/ws", s.HandleWebSocket)

	// Register notification WebSocket handler
	logger.Info("[API_SERVER] Registering notification WebSocket route: GET /ws/notifications")
	r.GET("/ws/notifications", s.HandleNotificationWebSocket)

	// Register server info endpoint
	logger.Info("[API_SERVER] Registering custom route: GET /api/server-info")
	r.GET("/api/server-info", s.HandleServerInfo)

	logger.Info("[API_SERVER] Custom route registration completed")
}

// HandleWebSocket handles WebSocket connections
func (s *Server) HandleWebSocket(c *gin.Context) {
	// Pass user ID from context to WebSocket handler
	if userID, exists := c.Get("userID"); exists {
		if id, ok := userID.(string); ok {
			c.Set("user_id", id)
		}
	}

	// Pass user display name from context to WebSocket handler
	if displayName, exists := c.Get("userDisplayName"); exists {
		if name, ok := displayName.(string); ok {
			c.Set("user_name", name)
		}
	}

	// Pass user email from context to WebSocket handler
	if userEmail, exists := c.Get("userEmail"); exists {
		if email, ok := userEmail.(string); ok {
			c.Set("userEmail", email)
		}
	}

	// Handle WebSocket connection
	s.wsHub.HandleWS(c)
}

// StartWebSocketHub starts the WebSocket hub cleanup timer
func (s *Server) StartWebSocketHub(ctx context.Context) {
	// Clean up any existing sessions from previous server runs
	s.wsHub.CleanupAllSessions()

	// Start the periodic cleanup timer
	go s.wsHub.StartCleanupTimer(ctx)

	// Initialize the notification hub
	InitNotificationHub()
}

// GetWebSocketHub returns the WebSocket hub instance
func (s *Server) GetWebSocketHub() *WebSocketHub {
	return s.wsHub
}

// GetCurrentUserSessions returns all active collaboration sessions that the user has access to
func (s *Server) GetCurrentUserSessions(c *gin.Context) {
	// Get username from JWT claim
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		// For collaboration endpoints, return empty list if user is not authenticated
		c.JSON(http.StatusOK, []CollaborationSession{})
		return
	}

	// Get filtered sessions based on user permissions
	sessions := s.wsHub.GetActiveSessionsForUser(c, userEmail)
	c.JSON(http.StatusOK, sessions)
}

// buildWebSocketURL constructs the WebSocket base URL from request context
func (s *Server) buildWebSocketURL(c *gin.Context) string {
	// Get config information from the context
	tlsEnabled := false
	tlsSubjectName := ""
	serverPort := "8080"

	// Try to extract from request context
	if val, exists := c.Get("tlsEnabled"); exists {
		if enabled, ok := val.(bool); ok {
			tlsEnabled = enabled
		}
	}

	if val, exists := c.Get("tlsSubjectName"); exists {
		if name, ok := val.(string); ok {
			tlsSubjectName = name
		}
	}

	if val, exists := c.Get("serverPort"); exists {
		if port, ok := val.(string); ok {
			serverPort = port
		}
	}

	// Determine websocket protocol
	scheme := "ws"
	if tlsEnabled {
		scheme = "wss"
	}

	// Determine host
	host := c.Request.Host
	if tlsSubjectName != "" && tlsEnabled {
		// Use configured subject name if available
		host = tlsSubjectName
		// Add port if not the default HTTPS port
		if serverPort != "443" {
			host = fmt.Sprintf("%s:%s", host, serverPort)
		}
	}

	// Build WebSocket URL
	return fmt.Sprintf("%s://%s/ws", scheme, host)
}

// HandleServerInfo provides server configuration information to clients
func (s *Server) HandleServerInfo(c *gin.Context) {
	// Get config information from the context
	tlsEnabled := false
	tlsSubjectName := ""

	// Try to extract from request context
	if val, exists := c.Get("tlsEnabled"); exists {
		if enabled, ok := val.(bool); ok {
			tlsEnabled = enabled
		}
	}

	if val, exists := c.Get("tlsSubjectName"); exists {
		if name, ok := val.(string); ok {
			tlsSubjectName = name
		}
	}

	// Build WebSocket URL using helper
	wsURL := s.buildWebSocketURL(c)

	// Return server info
	c.JSON(http.StatusOK, ServerInfo{
		TLSEnabled:       tlsEnabled,
		TLSSubjectName:   tlsSubjectName,
		WebSocketBaseURL: wsURL,
	})
}

// Collaboration Session API Methods - implementing GinServerInterface

// GetDiagramCollaborationSession retrieves the current collaboration session for a diagram
func (s *Server) GetDiagramCollaborationSession(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.GetDiagramCollaborate(c, threatModelId.String(), diagramId.String())
}

// CreateDiagramCollaborationSession creates a new collaboration session for a diagram
func (s *Server) CreateDiagramCollaborationSession(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.CreateDiagramCollaborate(c, threatModelId.String(), diagramId.String())
}

// EndDiagramCollaborationSession ends a collaboration session for a diagram
func (s *Server) EndDiagramCollaborationSession(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.DeleteDiagramCollaborate(c, threatModelId.String(), diagramId.String())
}

// SetAuthService sets the auth service for delegating auth-related methods
func (s *Server) SetAuthService(authService AuthService) {
	s.authService = authService

	// Initialize user deletion handler with auth service
	if authAdapter, ok := authService.(*AuthServiceAdapter); ok {
		s.userDeletionHandler = NewUserDeletionHandler(authAdapter.GetService())
	}
}

// SetAPIRateLimiter sets the API rate limiter
func (s *Server) SetAPIRateLimiter(rateLimiter *APIRateLimiter) {
	s.apiRateLimiter = rateLimiter
}

// SetWebhookRateLimiter sets the webhook rate limiter
func (s *Server) SetWebhookRateLimiter(rateLimiter *WebhookRateLimiter) {
	s.webhookRateLimiter = rateLimiter
}

// SetIPRateLimiter sets the IP rate limiter
func (s *Server) SetIPRateLimiter(rateLimiter *IPRateLimiter) {
	s.ipRateLimiter = rateLimiter
}

// SetAuthFlowRateLimiter sets the auth flow rate limiter
func (s *Server) SetAuthFlowRateLimiter(rateLimiter *AuthFlowRateLimiter) {
	s.authFlowRateLimiter = rateLimiter
}

// SetSettingsService sets the settings service for database-stored configuration
func (s *Server) SetSettingsService(settingsService *SettingsService) {
	s.settingsService = settingsService
}

// SetConfigProvider sets the config provider for settings migration
func (s *Server) SetConfigProvider(provider ConfigProvider) {
	s.configProvider = provider
}

// AuthService placeholder - we'll need to create this interface to avoid circular deps
type AuthService interface {
	GetProviders(c *gin.Context)
	GetSAMLProviders(c *gin.Context)
	Authorize(c *gin.Context)
	Callback(c *gin.Context)
	Exchange(c *gin.Context)
	Token(c *gin.Context)
	Refresh(c *gin.Context)
	Logout(c *gin.Context)
	RevokeToken(c *gin.Context)
	MeLogout(c *gin.Context)
	Me(c *gin.Context)
	IsValidProvider(idp string) bool
	GetProviderGroupsFromCache(ctx context.Context, idp string) ([]string, error)
}

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
			UsedInAuthorizations: contains(usedGroups, group),
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

// contains checks if a string slice contains a specific value
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
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

// Threat Model Methods (delegate to ThreatModelHandler)

// ListThreatModels lists threat models
func (s *Server) ListThreatModels(c *gin.Context, params ListThreatModelsParams) {
	s.threatModelHandler.GetThreatModels(c)
}

// CreateThreatModel creates a new threat model
func (s *Server) CreateThreatModel(c *gin.Context) {
	s.threatModelHandler.CreateThreatModel(c)
}

// GetThreatModel gets a specific threat model
func (s *Server) GetThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	// Set path parameter for handler
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.GetThreatModelByID(c)
}

// UpdateThreatModel updates a threat model
func (s *Server) UpdateThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.UpdateThreatModel(c)
}

// PatchThreatModel partially updates a threat model
func (s *Server) PatchThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.PatchThreatModel(c)
}

// DeleteThreatModel deletes a threat model
func (s *Server) DeleteThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.threatModelHandler.DeleteThreatModel(c)
}

// Threat Model Diagram Methods

// GetThreatModelDiagrams lists diagrams for a threat model
func (s *Server) GetThreatModelDiagrams(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelDiagramsParams) {
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}
	handler.GetDiagrams(c, threatModelId.String())
}

// CreateThreatModelDiagram creates a new diagram
func (s *Server) CreateThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.CreateDiagram(c, threatModelId.String())
}

// GetThreatModelDiagram gets a specific diagram
func (s *Server) GetThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.GetDiagramByID(c, threatModelId.String(), diagramId.String())
}

// UpdateThreatModelDiagram updates a diagram
func (s *Server) UpdateThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.UpdateDiagram(c, threatModelId.String(), diagramId.String())
}

// PatchThreatModelDiagram partially updates a diagram
func (s *Server) PatchThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.PatchDiagram(c, threatModelId.String(), diagramId.String())
}

// DeleteThreatModelDiagram deletes a diagram
func (s *Server) DeleteThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.DeleteDiagram(c, threatModelId.String(), diagramId.String())
}

// Diagram Collaboration Methods (already partially implemented above)

// GetDiagramModel gets minimal diagram model for automated analysis
func (s *Server) GetDiagramModel(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, params GetDiagramModelParams) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.GetDiagramModel(c, threatModelId, diagramId, params)
}

// Diagram Metadata Methods

// GetDiagramMetadata gets diagram metadata
func (s *Server) GetDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandler(GlobalMetadataStore, nil, nil, nil)

	// Delegate to existing implementation
	handler.GetThreatModelDiagramMetadata(c)
}

// CreateDiagramMetadata creates diagram metadata
func (s *Server) CreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandler(GlobalMetadataStore, nil, nil, nil)

	// Delegate to existing implementation
	handler.CreateThreatModelDiagramMetadata(c)
}

// BulkCreateDiagramMetadata bulk creates diagram metadata
func (s *Server) BulkCreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandler(GlobalMetadataStore, nil, nil, nil)

	// Delegate to existing implementation
	handler.BulkCreateThreatModelDiagramMetadata(c)
}

// BulkUpsertDiagramMetadata bulk upserts diagram metadata
func (s *Server) BulkUpsertDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandler(GlobalMetadataStore, nil, nil, nil)

	// Delegate to existing implementation
	handler.BulkUpdateThreatModelDiagramMetadata(c)
}

// DeleteDiagramMetadataByKey deletes diagram metadata by key
func (s *Server) DeleteDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandler(GlobalMetadataStore, nil, nil, nil)

	// Delegate to existing implementation
	handler.DeleteThreatModelDiagramMetadata(c)
}

// GetDiagramMetadataByKey gets diagram metadata by key
func (s *Server) GetDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandler(GlobalMetadataStore, nil, nil, nil)

	// Delegate to existing implementation
	handler.GetThreatModelDiagramMetadataByKey(c)
}

// UpdateDiagramMetadataByKey updates diagram metadata by key
func (s *Server) UpdateDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandler(GlobalMetadataStore, nil, nil, nil)

	// Delegate to existing implementation
	handler.UpdateThreatModelDiagramMetadata(c)
}

// Document Methods - Placeholder implementations (not yet implemented)

// GetThreatModelDocuments lists documents
func (s *Server) GetThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelDocumentsParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.GetDocuments(c)
}

// CreateThreatModelDocument creates a document
func (s *Server) CreateThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.CreateDocument(c)
}

// BulkCreateThreatModelDocuments bulk creates documents
func (s *Server) BulkCreateThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.BulkCreateDocuments(c)
}

// BulkUpsertThreatModelDocuments bulk upserts documents
func (s *Server) BulkUpsertThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.documentHandler.BulkUpdateDocuments(c)
}

// DeleteThreatModelDocument deletes a document
func (s *Server) DeleteThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.DeleteDocument(c)
}

// GetThreatModelDocument gets a document
func (s *Server) GetThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.GetDocument(c)
}

// UpdateThreatModelDocument updates a document
func (s *Server) UpdateThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "document_id", Value: documentId.String()})
	s.documentHandler.UpdateDocument(c)
}

// PatchThreatModelDocument patches a document
func (s *Server) PatchThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentHandler.PatchDocument(c)
}

// Document Metadata Methods - Placeholder implementations

// GetDocumentMetadata gets document metadata
func (s *Server) GetDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadataHandler.GetDocumentMetadata(c)
}

// CreateDocumentMetadata creates document metadata
func (s *Server) CreateDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadataHandler.CreateDocumentMetadata(c)
}

// BulkCreateDocumentMetadata bulk creates document metadata
func (s *Server) BulkCreateDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadataHandler.BulkCreateDocumentMetadata(c)
}

// BulkUpsertDocumentMetadata bulk upserts document metadata
func (s *Server) BulkUpsertDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	s.documentMetadataHandler.BulkUpdateDocumentMetadata(c)
}

// DeleteDocumentMetadataByKey deletes document metadata by key
func (s *Server) DeleteDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadataHandler.DeleteDocumentMetadata(c)
}

// GetDocumentMetadataByKey gets document metadata by key
func (s *Server) GetDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadataHandler.GetDocumentMetadataByKey(c)
}

// UpdateDocumentMetadataByKey updates document metadata by key
func (s *Server) UpdateDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	s.documentMetadataHandler.UpdateDocumentMetadata(c)
}

// Note Methods - Implementations

// GetThreatModelNotes lists notes
func (s *Server) GetThreatModelNotes(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelNotesParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.noteHandler.GetNotes(c)
}

// CreateThreatModelNote creates a note
func (s *Server) CreateThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.noteHandler.CreateNote(c)
}

// DeleteThreatModelNote deletes a note
func (s *Server) DeleteThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.DeleteNote(c)
}

// GetThreatModelNote gets a note
func (s *Server) GetThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.GetNote(c)
}

// UpdateThreatModelNote updates a note
func (s *Server) UpdateThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "note_id", Value: noteId.String()})
	s.noteHandler.UpdateNote(c)
}

// PatchThreatModelNote patches a note
func (s *Server) PatchThreatModelNote(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteHandler.PatchNote(c)
}

// Note Metadata Methods - Implementations

// GetNoteMetadata gets note metadata
func (s *Server) GetNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadataHandler.GetNoteMetadata(c)
}

// CreateNoteMetadata creates note metadata
func (s *Server) CreateNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadataHandler.CreateNoteMetadata(c)
}

// BulkCreateNoteMetadata bulk creates note metadata
func (s *Server) BulkCreateNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadataHandler.BulkCreateNoteMetadata(c)
}

// BulkUpdateNoteMetadata bulk updates note metadata
func (s *Server) BulkUpdateNoteMetadata(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID) {
	s.noteMetadataHandler.BulkUpdateNoteMetadata(c)
}

// DeleteNoteMetadataByKey deletes note metadata by key
func (s *Server) DeleteNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadataHandler.DeleteNoteMetadata(c)
}

// GetNoteMetadataByKey gets note metadata by key
func (s *Server) GetNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadataHandler.GetNoteMetadataByKey(c)
}

// UpdateNoteMetadataByKey updates note metadata by key
func (s *Server) UpdateNoteMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, noteId openapi_types.UUID, key string) {
	s.noteMetadataHandler.UpdateNoteMetadata(c)
}

// Threat Model Metadata Methods - Placeholder implementations

// GetThreatModelMetadata gets threat model metadata
func (s *Server) GetThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadataHandler.GetThreatModelMetadata(c)
}

// CreateThreatModelMetadata creates threat model metadata
func (s *Server) CreateThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadataHandler.CreateThreatModelMetadata(c)
}

// BulkCreateThreatModelMetadata bulk creates threat model metadata
func (s *Server) BulkCreateThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadataHandler.BulkCreateThreatModelMetadata(c)
}

// BulkUpsertThreatModelMetadata bulk upserts threat model metadata
func (s *Server) BulkUpsertThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatModelMetadataHandler.BulkUpdateThreatModelMetadata(c)
}

// DeleteThreatModelMetadataByKey deletes threat model metadata by key
func (s *Server) DeleteThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadataHandler.DeleteThreatModelMetadata(c)
}

// GetThreatModelMetadataByKey gets threat model metadata by key
func (s *Server) GetThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadataHandler.GetThreatModelMetadataByKey(c)
}

// UpdateThreatModelMetadataByKey updates threat model metadata by key
func (s *Server) UpdateThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	s.threatModelMetadataHandler.UpdateThreatModelMetadata(c)
}

// Repository Methods

// GetThreatModelRepositories lists repositories
func (s *Server) GetThreatModelRepositories(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelRepositoriesParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.GetRepositorys(c)
}

// CreateThreatModelRepository creates a repository
func (s *Server) CreateThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.CreateRepository(c)
}

// BulkCreateThreatModelRepositories bulk creates repositories
func (s *Server) BulkCreateThreatModelRepositories(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.BulkCreateRepositorys(c)
}

// BulkUpsertThreatModelRepositories bulk upserts repositories
func (s *Server) BulkUpsertThreatModelRepositories(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.repositoryHandler.BulkUpdateRepositorys(c)
}

// DeleteThreatModelRepository deletes a repository
func (s *Server) DeleteThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.DeleteRepository(c)
}

// GetThreatModelRepository gets a repository
func (s *Server) GetThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.GetRepository(c)
}

// UpdateThreatModelRepository updates a repository
func (s *Server) UpdateThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "repository_id", Value: repositoryId.String()})
	s.repositoryHandler.UpdateRepository(c)
}

// PatchThreatModelRepository patches a repository
func (s *Server) PatchThreatModelRepository(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryHandler.PatchRepository(c)
}

// Repository Metadata Methods

// GetRepositoryMetadata gets repository metadata
func (s *Server) GetRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadataHandler.GetRepositoryMetadata(c)
}

// CreateRepositoryMetadata creates repository metadata
func (s *Server) CreateRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadataHandler.CreateRepositoryMetadata(c)
}

// BulkCreateRepositoryMetadata bulk creates repository metadata
func (s *Server) BulkCreateRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadataHandler.BulkCreateRepositoryMetadata(c)
}

// BulkUpsertRepositoryMetadata bulk upserts repository metadata
func (s *Server) BulkUpsertRepositoryMetadata(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID) {
	s.repositoryMetadataHandler.BulkUpdateRepositoryMetadata(c)
}

// DeleteRepositoryMetadataByKey deletes repository metadata by key
func (s *Server) DeleteRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadataHandler.DeleteRepositoryMetadata(c)
}

// GetRepositoryMetadataByKey gets repository metadata by key
func (s *Server) GetRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadataHandler.GetRepositoryMetadataByKey(c)
}

// UpdateRepositoryMetadataByKey updates repository metadata by key
func (s *Server) UpdateRepositoryMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, repositoryId openapi_types.UUID, key string) {
	s.repositoryMetadataHandler.UpdateRepositoryMetadata(c)
}

// Asset Methods

// GetThreatModelAssets lists assets
func (s *Server) GetThreatModelAssets(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelAssetsParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.GetAssets(c)
}

// CreateThreatModelAsset creates an asset
func (s *Server) CreateThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.CreateAsset(c)
}

// BulkCreateThreatModelAssets bulk creates assets
func (s *Server) BulkCreateThreatModelAssets(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.BulkCreateAssets(c)
}

// BulkUpsertThreatModelAssets bulk upserts assets
func (s *Server) BulkUpsertThreatModelAssets(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.assetHandler.BulkUpdateAssets(c)
}

// DeleteThreatModelAsset deletes an asset
func (s *Server) DeleteThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.DeleteAsset(c)
}

// GetThreatModelAsset gets an asset
func (s *Server) GetThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.GetAsset(c)
}

// UpdateThreatModelAsset updates an asset
func (s *Server) UpdateThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "asset_id", Value: assetId.String()})
	s.assetHandler.UpdateAsset(c)
}

// PatchThreatModelAsset patches an asset
func (s *Server) PatchThreatModelAsset(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetHandler.PatchAsset(c)
}

// Asset Metadata Methods

// GetThreatModelAssetMetadata gets asset metadata
func (s *Server) GetThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadataHandler.GetAssetMetadata(c)
}

// CreateThreatModelAssetMetadata creates asset metadata
func (s *Server) CreateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadataHandler.CreateAssetMetadata(c)
}

// BulkCreateThreatModelAssetMetadata bulk creates asset metadata
func (s *Server) BulkCreateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadataHandler.BulkCreateAssetMetadata(c)
}

// BulkUpsertThreatModelAssetMetadata creates or updates multiple asset metadata entries
func (s *Server) BulkUpsertThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID) {
	s.assetMetadataHandler.BulkUpdateAssetMetadata(c)
}

// DeleteThreatModelAssetMetadata deletes asset metadata by key
func (s *Server) DeleteThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadataHandler.DeleteAssetMetadata(c)
}

// GetThreatModelAssetMetadataByKey gets asset metadata by key
func (s *Server) GetThreatModelAssetMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadataHandler.GetAssetMetadataByKey(c)
}

// UpdateThreatModelAssetMetadata updates asset metadata by key
func (s *Server) UpdateThreatModelAssetMetadata(c *gin.Context, threatModelId openapi_types.UUID, assetId openapi_types.UUID, key string) {
	s.assetMetadataHandler.UpdateAssetMetadata(c)
}

// Threat Methods - Placeholder implementations

// GetThreatModelThreats lists threats
func (s *Server) GetThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelThreatsParams) {
	s.threatHandler.GetThreatsWithFilters(c, params)
}

// CreateThreatModelThreat creates a threat
func (s *Server) CreateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.CreateThreat(c)
}

// BulkCreateThreatModelThreats bulk creates threats
func (s *Server) BulkCreateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkCreateThreats(c)
}

// BulkUpdateThreatModelThreats bulk updates threats
func (s *Server) BulkUpdateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkUpdateThreats(c)
}

// BulkPatchThreatModelThreats bulk patches threats
func (s *Server) BulkPatchThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkPatchThreats(c)
}

// BulkDeleteThreatModelThreats bulk deletes threats
func (s *Server) BulkDeleteThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params BulkDeleteThreatModelThreatsParams) {
	s.threatHandler.BulkDeleteThreats(c)
}

// DeleteThreatModelThreat deletes a threat
func (s *Server) DeleteThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.DeleteThreat(c)
}

// GetThreatModelThreat gets a threat
func (s *Server) GetThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.GetThreat(c)
}

// PatchThreatModelThreat patches a threat
func (s *Server) PatchThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.PatchThreat(c)
}

// UpdateThreatModelThreat updates a threat
func (s *Server) UpdateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatHandler.UpdateThreat(c)
}

// Threat Metadata Methods - Placeholder implementations

// GetThreatMetadata gets threat metadata
func (s *Server) GetThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadataHandler.GetThreatMetadata(c)
}

// CreateThreatMetadata creates threat metadata
func (s *Server) CreateThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadataHandler.CreateThreatMetadata(c)
}

// BulkCreateThreatMetadata bulk creates threat metadata
func (s *Server) BulkCreateThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadataHandler.BulkCreateThreatMetadata(c)
}

// BulkUpsertThreatMetadata bulk upserts threat metadata
func (s *Server) BulkUpsertThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	s.threatMetadataHandler.BulkUpdateThreatMetadata(c)
}

// DeleteThreatMetadataByKey deletes threat metadata by key
func (s *Server) DeleteThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadataHandler.DeleteThreatMetadata(c)
}

// GetThreatMetadataByKey gets threat metadata by key
func (s *Server) GetThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadataHandler.GetThreatMetadataByKey(c)
}

// UpdateThreatMetadataByKey updates threat metadata by key
func (s *Server) UpdateThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	s.threatMetadataHandler.UpdateThreatMetadata(c)
}

// Addon Methods - Complete ServerInterface Implementation

// CreateAddon creates a new add-on (admin only)
func (s *Server) CreateAddon(c *gin.Context) {
	// Delegate to existing standalone handler
	CreateAddon(c)
}

// ListAddons lists all add-ons
func (s *Server) ListAddons(c *gin.Context, params ListAddonsParams) {
	// The standalone handler reads query params directly from context
	// which is already set by the OpenAPI middleware
	ListAddons(c)
}

// GetAddon gets a single add-on by ID
func (s *Server) GetAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	GetAddon(c)
}

// DeleteAddon deletes an add-on (admin only)
func (s *Server) DeleteAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	DeleteAddon(c)
}

// InvokeAddon invokes an add-on
func (s *Server) InvokeAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	InvokeAddon(c)
}

// ListInvocations lists invocations (user sees own, admin sees all)
func (s *Server) ListInvocations(c *gin.Context, params ListInvocationsParams) {
	// The standalone handler reads query params directly from context
	ListInvocations(c)
}

// GetInvocation gets a single invocation by ID
func (s *Server) GetInvocation(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	GetInvocation(c)
}

// UpdateInvocationStatus updates invocation status (webhook callback with HMAC auth)
func (s *Server) UpdateInvocationStatus(c *gin.Context, id openapi_types.UUID, params UpdateInvocationStatusParams) {
	// The standalone handler reads the HMAC signature from headers directly
	UpdateInvocationStatus(c)
}
