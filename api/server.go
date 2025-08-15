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
	threatModelHandler *ThreatModelHandler
	// diagramHandler     *DiagramHandler  // Disabled - diagram endpoints removed
	// WebSocket hub
	wsHub *WebSocketHub
	// Auth handlers (for delegating auth-related methods)
	authService AuthService // We'll need to add this dependency
}

// NewServer creates a new API server instance
func NewServer(wsLoggingConfig logging.WebSocketLoggingConfig) *Server {
	return &Server{
		threatModelHandler: NewThreatModelHandler(),
		// diagramHandler:     NewDiagramHandler(),  // Disabled - diagram endpoints removed
		wsHub: NewWebSocketHub(wsLoggingConfig),
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
	go s.wsHub.StartCleanupTimer(ctx)
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
	c.JSON(http.StatusOK, gin.H{
		"name":        "TMI API",
		"version":     "1.0",
		"description": "Threat Modeling Interface API",
	})
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
func (s *Server) AuthorizeOAuthProvider(c *gin.Context, provider AuthorizeOAuthProviderParamsProvider, params AuthorizeOAuthProviderParams) {
	logger := logging.Get()
	logger.Info("[SERVER_INTERFACE] AuthorizeOAuthProvider called for provider: %s", provider)
	if s.authService != nil {
		s.authService.Authorize(c)
	} else {
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
func (s *Server) ExchangeOAuthCode(c *gin.Context, provider ExchangeOAuthCodeParamsProvider) {
	logger := logging.Get()
	logger.Info("[SERVER_INTERFACE] ExchangeOAuthCode called for provider: %s", provider)
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
	HandleRequestError(c, ServerError("GetThreatModel not yet implemented")) // Placeholder implementation
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
	// handler := &ThreatModelDiagramHandler{wsHub: s.wsHub} // Unused
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	HandleRequestError(c, ServerError("GetThreatModelDiagrams not yet implemented"))
}

// CreateThreatModelDiagram creates a new diagram
func (s *Server) CreateThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID) {
	// handler := &ThreatModelDiagramHandler{wsHub: s.wsHub} // Unused
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	HandleRequestError(c, ServerError("CreateThreatModelDiagram not yet implemented"))
}

// GetThreatModelDiagram gets a specific diagram
func (s *Server) GetThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// handler := &ThreatModelDiagramHandler{wsHub: s.wsHub} // Unused
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "diagram_id", Value: diagramId.String()})
	HandleRequestError(c, ServerError("GetThreatModelDiagram not yet implemented"))
}

// UpdateThreatModelDiagram updates a diagram
func (s *Server) UpdateThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// handler := &ThreatModelDiagramHandler{wsHub: s.wsHub} // Unused
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "diagram_id", Value: diagramId.String()})
	HandleRequestError(c, ServerError("UpdateThreatModelDiagram not yet implemented"))
}

// PatchThreatModelDiagram partially updates a diagram
func (s *Server) PatchThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// handler := &ThreatModelDiagramHandler{wsHub: s.wsHub} // Unused
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "diagram_id", Value: diagramId.String()})
	HandleRequestError(c, ServerError("PatchThreatModelDiagram not yet implemented"))
}

// DeleteThreatModelDiagram deletes a diagram
func (s *Server) DeleteThreatModelDiagram(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	// handler := &ThreatModelDiagramHandler{wsHub: s.wsHub} // Unused
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	c.Params = append(c.Params, gin.Param{Key: "diagram_id", Value: diagramId.String()})
	HandleRequestError(c, ServerError("DeleteThreatModelDiagram not yet implemented"))
}

// Diagram Collaboration Methods (already partially implemented above)

// Diagram Metadata Methods

// GetDiagramMetadata gets diagram metadata
func (s *Server) GetDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Diagram metadata not yet implemented"))
}

// CreateDiagramMetadata creates diagram metadata
func (s *Server) CreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Diagram metadata not yet implemented"))
}

// BulkCreateDiagramMetadata bulk creates diagram metadata
func (s *Server) BulkCreateDiagramMetadata(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Diagram metadata not yet implemented"))
}

// DeleteDiagramMetadataByKey deletes diagram metadata by key
func (s *Server) DeleteDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Diagram metadata not yet implemented"))
}

// GetDiagramMetadataByKey gets diagram metadata by key
func (s *Server) GetDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Diagram metadata not yet implemented"))
}

// UpdateDiagramMetadataByKey updates diagram metadata by key
func (s *Server) UpdateDiagramMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, diagramId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Diagram metadata not yet implemented"))
}

// Document Methods - Placeholder implementations (not yet implemented)

// GetThreatModelDocuments lists documents
func (s *Server) GetThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelDocumentsParams) {
	HandleRequestError(c, ServerError("Documents not yet implemented"))
}

// CreateThreatModelDocument creates a document
func (s *Server) CreateThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Documents not yet implemented"))
}

// BulkCreateThreatModelDocuments bulk creates documents
func (s *Server) BulkCreateThreatModelDocuments(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Documents not yet implemented"))
}

// DeleteThreatModelDocument deletes a document
func (s *Server) DeleteThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Documents not yet implemented"))
}

// GetThreatModelDocument gets a document
func (s *Server) GetThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Documents not yet implemented"))
}

// UpdateThreatModelDocument updates a document
func (s *Server) UpdateThreatModelDocument(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Documents not yet implemented"))
}

// Document Metadata Methods - Placeholder implementations

// GetDocumentMetadata gets document metadata
func (s *Server) GetDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Document metadata not yet implemented"))
}

// CreateDocumentMetadata creates document metadata
func (s *Server) CreateDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Document metadata not yet implemented"))
}

// BulkCreateDocumentMetadata bulk creates document metadata
func (s *Server) BulkCreateDocumentMetadata(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Document metadata not yet implemented"))
}

// DeleteDocumentMetadataByKey deletes document metadata by key
func (s *Server) DeleteDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Document metadata not yet implemented"))
}

// GetDocumentMetadataByKey gets document metadata by key
func (s *Server) GetDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Document metadata not yet implemented"))
}

// UpdateDocumentMetadataByKey updates document metadata by key
func (s *Server) UpdateDocumentMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, documentId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Document metadata not yet implemented"))
}

// Threat Model Metadata Methods - Placeholder implementations

// GetThreatModelMetadata gets threat model metadata
func (s *Server) GetThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threat model metadata not yet implemented"))
}

// CreateThreatModelMetadata creates threat model metadata
func (s *Server) CreateThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threat model metadata not yet implemented"))
}

// BulkCreateThreatModelMetadata bulk creates threat model metadata
func (s *Server) BulkCreateThreatModelMetadata(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threat model metadata not yet implemented"))
}

// DeleteThreatModelMetadataByKey deletes threat model metadata by key
func (s *Server) DeleteThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Threat model metadata not yet implemented"))
}

// GetThreatModelMetadataByKey gets threat model metadata by key
func (s *Server) GetThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Threat model metadata not yet implemented"))
}

// UpdateThreatModelMetadataByKey updates threat model metadata by key
func (s *Server) UpdateThreatModelMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Threat model metadata not yet implemented"))
}

// Source Methods - Placeholder implementations

// GetThreatModelSources lists sources
func (s *Server) GetThreatModelSources(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelSourcesParams) {
	HandleRequestError(c, ServerError("Sources not yet implemented"))
}

// CreateThreatModelSource creates a source
func (s *Server) CreateThreatModelSource(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Sources not yet implemented"))
}

// BulkCreateThreatModelSources bulk creates sources
func (s *Server) BulkCreateThreatModelSources(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Sources not yet implemented"))
}

// DeleteThreatModelSource deletes a source
func (s *Server) DeleteThreatModelSource(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Sources not yet implemented"))
}

// GetThreatModelSource gets a source
func (s *Server) GetThreatModelSource(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Sources not yet implemented"))
}

// UpdateThreatModelSource updates a source
func (s *Server) UpdateThreatModelSource(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Sources not yet implemented"))
}

// Source Metadata Methods - Placeholder implementations

// GetSourceMetadata gets source metadata
func (s *Server) GetSourceMetadata(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Source metadata not yet implemented"))
}

// CreateSourceMetadata creates source metadata
func (s *Server) CreateSourceMetadata(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Source metadata not yet implemented"))
}

// BulkCreateSourceMetadata bulk creates source metadata
func (s *Server) BulkCreateSourceMetadata(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Source metadata not yet implemented"))
}

// DeleteSourceMetadataByKey deletes source metadata by key
func (s *Server) DeleteSourceMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Source metadata not yet implemented"))
}

// GetSourceMetadataByKey gets source metadata by key
func (s *Server) GetSourceMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Source metadata not yet implemented"))
}

// UpdateSourceMetadataByKey updates source metadata by key
func (s *Server) UpdateSourceMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, sourceId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Source metadata not yet implemented"))
}

// Threat Methods - Placeholder implementations

// GetThreatModelThreats lists threats
func (s *Server) GetThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params GetThreatModelThreatsParams) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// CreateThreatModelThreat creates a threat
func (s *Server) CreateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// BatchDeleteThreatModelThreats batch deletes threats
func (s *Server) BatchDeleteThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID, params BatchDeleteThreatModelThreatsParams) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// BatchPatchThreatModelThreats batch patches threats
func (s *Server) BatchPatchThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// BulkCreateThreatModelThreats bulk creates threats
func (s *Server) BulkCreateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// BulkUpdateThreatModelThreats bulk updates threats
func (s *Server) BulkUpdateThreatModelThreats(c *gin.Context, threatModelId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// DeleteThreatModelThreat deletes a threat
func (s *Server) DeleteThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// GetThreatModelThreat gets a threat
func (s *Server) GetThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// PatchThreatModelThreat patches a threat
func (s *Server) PatchThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// UpdateThreatModelThreat updates a threat
func (s *Server) UpdateThreatModelThreat(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threats not yet implemented"))
}

// Threat Metadata Methods - Placeholder implementations

// GetThreatMetadata gets threat metadata
func (s *Server) GetThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threat metadata not yet implemented"))
}

// CreateThreatMetadata creates threat metadata
func (s *Server) CreateThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threat metadata not yet implemented"))
}

// BulkCreateThreatMetadata bulk creates threat metadata
func (s *Server) BulkCreateThreatMetadata(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID) {
	HandleRequestError(c, ServerError("Threat metadata not yet implemented"))
}

// DeleteThreatMetadataByKey deletes threat metadata by key
func (s *Server) DeleteThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Threat metadata not yet implemented"))
}

// GetThreatMetadataByKey gets threat metadata by key
func (s *Server) GetThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Threat metadata not yet implemented"))
}

// UpdateThreatMetadataByKey updates threat metadata by key
func (s *Server) UpdateThreatMetadataByKey(c *gin.Context, threatModelId openapi_types.UUID, threatId openapi_types.UUID, key string) {
	HandleRequestError(c, ServerError("Threat metadata not yet implemented"))
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
