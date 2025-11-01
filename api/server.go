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

// HandleCollaborationSessions returns all active collaboration sessions that the user has access to
func (s *Server) HandleCollaborationSessions(c *gin.Context) {
	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
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

// AuthService placeholder - we'll need to create this interface to avoid circular deps
type AuthService interface {
	GetProviders(c *gin.Context)
	Authorize(c *gin.Context)
	Callback(c *gin.Context)
	Exchange(c *gin.Context)
	Refresh(c *gin.Context)
	Logout(c *gin.Context)
	Me(c *gin.Context)
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
		HandleRequestError(c, ServerError("Auth service not configured"))
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
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// LogoutUser logs out the current user
func (s *Server) LogoutUser(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] LogoutUser called")
	if s.authService != nil {
		s.authService.Logout(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// GetCurrentUser gets current user information
func (s *Server) GetCurrentUser(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetCurrentUser called - delegating to authService.Me()")
	if s.authService != nil {
		s.authService.Me(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// GetCurrentUserProfile gets current user profile with groups (from /users/me endpoint)
func (s *Server) GetCurrentUserProfile(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetCurrentUserProfile called (GET /users/me) - delegating to authService.Me()")
	if s.authService != nil {
		// The Me() method will be updated to include groups and IdP
		s.authService.Me(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
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
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// GetProviderGroups returns groups available from a specific identity provider
func (s *Server) GetProviderGroups(c *gin.Context, idp string) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetProviderGroups called for IdP: %s", idp)

	// For now, return a placeholder response
	// TODO: Implement actual group fetching from provider or cache
	response := struct {
		IdP    string `json:"idp"`
		Groups []struct {
			Name                 string `json:"name"`
			DisplayName          string `json:"display_name,omitempty"`
			UsedInAuthorizations bool   `json:"used_in_authorizations"`
		} `json:"groups"`
	}{
		IdP: idp,
		Groups: []struct {
			Name                 string `json:"name"`
			DisplayName          string `json:"display_name,omitempty"`
			UsedInAuthorizations bool   `json:"used_in_authorizations"`
		}{},
	}

	c.JSON(http.StatusOK, response)
}

// GetSAMLMetadata returns SAML service provider metadata
func (s *Server) GetSAMLMetadata(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] GetSAMLMetadata called")

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServerError("Authentication service not configured"))
		return
	}

	// Get provider ID from query parameter
	providerID := c.Query("provider")
	if providerID == "" {
		providerID = "default" // Use default provider if not specified
	}

	// Delegate to auth service for SAML metadata
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.GetSAMLMetadata(c, providerID)
	} else {
		HandleRequestError(c, ServerError("SAML not supported by current auth provider"))
	}
}

// InitiateSAMLLogin starts SAML authentication flow
func (s *Server) InitiateSAMLLogin(c *gin.Context, params InitiateSAMLLoginParams) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] InitiateSAMLLogin called")

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServerError("Authentication service not configured"))
		return
	}

	// Get provider ID from query parameter
	providerID := c.Query("provider")
	if providerID == "" {
		providerID = "default" // Use default provider if not specified
	}

	// Delegate to auth service for SAML login
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.InitiateSAMLLogin(c, providerID, params.ClientCallback)
	} else {
		HandleRequestError(c, ServerError("SAML not supported by current auth provider"))
	}
}

// ProcessSAMLResponse handles SAML assertion consumer service
func (s *Server) ProcessSAMLResponse(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] ProcessSAMLResponse called")

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServerError("Authentication service not configured"))
		return
	}

	// Parse form data
	samlResponse := c.PostForm("SAMLResponse")
	relayState := c.PostForm("RelayState")

	if samlResponse == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing SAMLResponse",
		})
		return
	}

	// Get provider ID from relay state or default
	providerID := "default"
	// TODO: Extract provider ID from relay state if encoded there

	// Delegate to auth service for SAML response processing
	if authAdapter, ok := s.authService.(*AuthServiceAdapter); ok {
		authAdapter.ProcessSAMLResponse(c, providerID, samlResponse, relayState)
	} else {
		HandleRequestError(c, ServerError("SAML not supported by current auth provider"))
	}
}

// ProcessSAMLLogout handles SAML single logout (GET)
func (s *Server) ProcessSAMLLogout(c *gin.Context, params ProcessSAMLLogoutParams) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] ProcessSAMLLogout called (GET)")

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServerError("Authentication service not configured"))
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
		HandleRequestError(c, ServerError("SAML not supported by current auth provider"))
	}
}

// ProcessSAMLLogoutPost handles SAML single logout (POST)
func (s *Server) ProcessSAMLLogoutPost(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] ProcessSAMLLogoutPost called (POST)")

	// Check if auth service is configured
	if s.authService == nil {
		HandleRequestError(c, ServerError("Authentication service not configured"))
		return
	}

	// Parse form data
	samlRequest := c.PostForm("SAMLRequest")
	if samlRequest == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing SAMLRequest",
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
		HandleRequestError(c, ServerError("SAML not supported by current auth provider"))
	}
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
			HandleRequestError(c, ServerError("JWKS endpoint not supported"))
		}
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
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
			HandleRequestError(c, ServerError("OAuth metadata endpoint not supported"))
		}
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
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
			HandleRequestError(c, ServerError("OpenID configuration endpoint not supported"))
		}
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
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
			HandleRequestError(c, ServerError("OAuth protected resource metadata endpoint not supported"))
		}
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
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
			HandleRequestError(c, ServerError("Token introspection endpoint not supported"))
		}
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// RefreshToken refreshes JWT token
func (s *Server) RefreshToken(c *gin.Context) {
	logger := slogging.Get()
	logger.Info("[SERVER_INTERFACE] RefreshToken called")
	if s.authService != nil {
		s.authService.Refresh(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
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
		s.authService.Exchange(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// Collaboration Session Methods

// GetCollaborationSessions returns active collaboration sessions (already implemented)
func (s *Server) GetCollaborationSessions(c *gin.Context) {
	s.HandleCollaborationSessions(c) // Delegate to existing implementation
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
