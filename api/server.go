package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ericfitz/tmi/internal/logging"
)

// Server is the main API server instance
type Server struct {
	// Handlers
	threatModelHandler         *ThreatModelHandler
	documentHandler            *DocumentSubResourceHandler
	sourceHandler              *SourceSubResourceHandler
	threatHandler              *ThreatSubResourceHandler
	batchHandler               *BatchHandler
	documentMetadataHandler    *DocumentMetadataHandler
	sourceMetadataHandler      *SourceMetadataHandler
	threatMetadataHandler      *ThreatMetadataHandler
	threatModelMetadataHandler *ThreatModelMetadataHandler
	// WebSocket hub
	wsHub *WebSocketHub
	// Auth handlers (for delegating auth-related methods)
	authService AuthService // We'll need to add this dependency
}

// NewServer creates a new API server instance
func NewServer(wsLoggingConfig logging.WebSocketLoggingConfig) *Server {
	return &Server{
		threatModelHandler:         NewThreatModelHandler(),
		documentHandler:            NewDocumentSubResourceHandler(GlobalDocumentStore, nil, nil, nil),
		sourceHandler:              NewSourceSubResourceHandler(GlobalSourceStore, nil, nil, nil),
		threatHandler:              NewThreatSubResourceHandler(GlobalThreatStore, nil, nil, nil),
		batchHandler:               NewBatchHandler(GlobalThreatStore, nil, nil, nil),
		documentMetadataHandler:    NewDocumentMetadataHandlerSimple(),
		sourceMetadataHandler:      NewSourceMetadataHandlerSimple(),
		threatMetadataHandler:      NewThreatMetadataHandlerSimple(),
		threatModelMetadataHandler: NewThreatModelMetadataHandlerSimple(),
		wsHub:                      NewWebSocketHub(wsLoggingConfig),
		// authService will be set separately via SetAuthService
	}
}

// NewServerForTests creates a server with default test configuration
func NewServerForTests() *Server {
	return NewServer(logging.WebSocketLoggingConfig{
		Enabled:        false, // Disable logging in tests by default
		RedactTokens:   true,
		MaxMessageSize: 5 * 1024,
		OnlyDebugLevel: true,
	})
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
	logger := logging.Get()
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
	// Pass user name from context to WebSocket handler
	if userID, exists := c.Get("userName"); exists {
		if userName, ok := userID.(string); ok {
			c.Set("user_name", userName)
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
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		// For collaboration endpoints, return empty list if user is not authenticated
		c.JSON(http.StatusOK, []CollaborationSession{})
		return
	}

	// Get filtered sessions based on user permissions
	sessions := s.wsHub.GetActiveSessionsForUser(c, userName)
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

// JoinDiagramCollaborationSession joins an existing collaboration session for a diagram
func (s *Server) JoinDiagramCollaborationSession(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.JoinDiagramCollaborate(c, threatModelId.String(), diagramId.String())
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
	logger := logging.Get()
	logger.Info("[SERVER_INTERFACE] HandleOAuthCallback called")
	if s.authService != nil {
		s.authService.Callback(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// AuthorizeOAuthProvider initiates OAuth flow
func (s *Server) AuthorizeOAuthProvider(c *gin.Context, params AuthorizeOAuthProviderParams) {
	logger := logging.Get()
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
	logger := logging.Get()
	logger.Info("[SERVER_INTERFACE] LogoutUser called")
	if s.authService != nil {
		s.authService.Logout(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// GetCurrentUser gets current user information
func (s *Server) GetCurrentUser(c *gin.Context) {
	logger := logging.Get()
	logger.Info("[SERVER_INTERFACE] GetCurrentUser called - delegating to authService.Me()")
	if s.authService != nil {
		s.authService.Me(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// GetAuthProviders lists OAuth providers
func (s *Server) GetAuthProviders(c *gin.Context) {
	logger := logging.Get()
	logger.Info("[SERVER_INTERFACE] GetAuthProviders called")
	if s.authService != nil {
		s.authService.GetProviders(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// GetJWKS returns the JSON Web Key Set for JWT signature verification
func (s *Server) GetJWKS(c *gin.Context) {
	logger := logging.Get()
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
	logger := logging.Get()
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
	logger := logging.Get()
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

// IntrospectToken handles token introspection requests per RFC 7662
func (s *Server) IntrospectToken(c *gin.Context) {
	logger := logging.Get()
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
	logger := logging.Get()
	logger.Info("[SERVER_INTERFACE] RefreshToken called")
	if s.authService != nil {
		s.authService.Refresh(c)
	} else {
		HandleRequestError(c, ServerError("Auth service not configured"))
	}
}

// ExchangeOAuthCode exchanges auth code for tokens
func (s *Server) ExchangeOAuthCode(c *gin.Context, params ExchangeOAuthCodeParams) {
	logger := logging.Get()
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
	c.Params = append(c.Params, gin.Param{Key: "id", Value: threatModelId.String()})
	s.threatModelHandler.GetThreatModelByID(c)
}

// UpdateThreatModel updates a threat model
func (s *Server) UpdateThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "id", Value: threatModelId.String()})
	s.threatModelHandler.UpdateThreatModel(c)
}

// PatchThreatModel partially updates a threat model
func (s *Server) PatchThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "id", Value: threatModelId.String()})
	s.threatModelHandler.PatchThreatModel(c)
}

// DeleteThreatModel deletes a threat model
func (s *Server) DeleteThreatModel(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "id", Value: threatModelId.String()})
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
	handler := NewDiagramMetadataHandlerSimple()

	// Delegate to existing implementation
	handler.GetThreatModelDiagramMetadata(c)
}

// CreateDiagramMetadata creates diagram metadata
func (s *Server) CreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandlerSimple()

	// Delegate to existing implementation
	handler.CreateThreatModelDiagramMetadata(c)
}

// BulkCreateDiagramMetadata bulk creates diagram metadata
func (s *Server) BulkCreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandlerSimple()

	// Delegate to existing implementation
	handler.BulkCreateThreatModelDiagramMetadata(c)
}

// DeleteDiagramMetadataByKey deletes diagram metadata by key
func (s *Server) DeleteDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandlerSimple()

	// Delegate to existing implementation
	handler.DeleteThreatModelDiagramMetadata(c)
}

// GetDiagramMetadataByKey gets diagram metadata by key
func (s *Server) GetDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandlerSimple()

	// Delegate to existing implementation
	handler.GetThreatModelDiagramMetadataByKey(c)
}

// UpdateDiagramMetadataByKey updates diagram metadata by key
func (s *Server) UpdateDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	// Create diagram metadata handler
	handler := NewDiagramMetadataHandlerSimple()

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

// Source Methods - Placeholder implementations

// GetThreatModelSources lists sources
func (s *Server) GetThreatModelSources(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelSourcesParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.sourceHandler.GetSources(c)
}

// CreateThreatModelSource creates a source
func (s *Server) CreateThreatModelSource(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.sourceHandler.CreateSource(c)
}

// BulkCreateThreatModelSources bulk creates sources
func (s *Server) BulkCreateThreatModelSources(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.sourceHandler.BulkCreateSources(c)
}

// DeleteThreatModelSource deletes a source
func (s *Server) DeleteThreatModelSource(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "source_id", Value: sourceId.String()})
	s.sourceHandler.DeleteSource(c)
}

// GetThreatModelSource gets a source
func (s *Server) GetThreatModelSource(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "source_id", Value: sourceId.String()})
	s.sourceHandler.GetSource(c)
}

// UpdateThreatModelSource updates a source
func (s *Server) UpdateThreatModelSource(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "source_id", Value: sourceId.String()})
	s.sourceHandler.UpdateSource(c)
}

// Source Metadata Methods - Placeholder implementations

// GetSourceMetadata gets source metadata
func (s *Server) GetSourceMetadata(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	s.sourceMetadataHandler.GetSourceMetadata(c)
}

// CreateSourceMetadata creates source metadata
func (s *Server) CreateSourceMetadata(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	s.sourceMetadataHandler.CreateSourceMetadata(c)
}

// BulkCreateSourceMetadata bulk creates source metadata
func (s *Server) BulkCreateSourceMetadata(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	s.sourceMetadataHandler.BulkCreateSourceMetadata(c)
}

// DeleteSourceMetadataByKey deletes source metadata by key
func (s *Server) DeleteSourceMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID, key string) {
	s.sourceMetadataHandler.DeleteSourceMetadata(c)
}

// GetSourceMetadataByKey gets source metadata by key
func (s *Server) GetSourceMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID, key string) {
	s.sourceMetadataHandler.GetSourceMetadataByKey(c)
}

// UpdateSourceMetadataByKey updates source metadata by key
func (s *Server) UpdateSourceMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID, key string) {
	s.sourceMetadataHandler.UpdateSourceMetadata(c)
}

// Threat Methods - Placeholder implementations

// GetThreatModelThreats lists threats
func (s *Server) GetThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelThreatsParams) {
	s.threatHandler.GetThreats(c)
}

// CreateThreatModelThreat creates a threat
func (s *Server) CreateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.CreateThreat(c)
}

// BatchDeleteThreatModelThreats batch deletes threats
func (s *Server) BatchDeleteThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params BatchDeleteThreatModelThreatsParams) {
	s.batchHandler.BatchDeleteThreats(c)
}

// BatchPatchThreatModelThreats batch patches threats
func (s *Server) BatchPatchThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.batchHandler.BatchPatchThreats(c)
}

// BulkCreateThreatModelThreats bulk creates threats
func (s *Server) BulkCreateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkCreateThreats(c)
}

// BulkUpdateThreatModelThreats bulk updates threats
func (s *Server) BulkUpdateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	s.threatHandler.BulkUpdateThreats(c)
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

// Legacy ServerInterface wrapper methods for backward compatibility

// GetThreatModelsThreatModelIdDiagramsDiagramIdCollaborate - wrapper for GetDiagramCollaborationSession
func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")

	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.GetDiagramCollaborate(c, threatModelId, diagramId)
}

// PostThreatModelsThreatModelIdDiagramsDiagramIdCollaborate - wrapper for CreateDiagramCollaborationSession
func (s *Server) PostThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")

	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.CreateDiagramCollaborate(c, threatModelId, diagramId)
}

// PutThreatModelsThreatModelIdDiagramsDiagramIdCollaborate - wrapper for JoinDiagramCollaborationSession
func (s *Server) PutThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")

	// Log the join request for debugging
	logger := logging.Get()
	logger.Info("PUT collaborate wrapper method called - TM: %s, Diagram: %s", threatModelId, diagramId)

	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.JoinDiagramCollaborate(c, threatModelId, diagramId)
}

// DeleteThreatModelsThreatModelIdDiagramsDiagramIdCollaborate - wrapper for EndDiagramCollaborationSession
func (s *Server) DeleteThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")

	// Create handler with websocket hub
	handler := &ThreatModelDiagramHandler{wsHub: s.wsHub}

	// Delegate to existing implementation
	handler.DeleteDiagramCollaborate(c, threatModelId, diagramId)
}
