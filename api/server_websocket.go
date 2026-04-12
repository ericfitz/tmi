package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ericfitz/tmi/internal/slogging"
)

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
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		// For collaboration endpoints, return empty list if user is not authenticated
		c.JSON(http.StatusOK, []CollaborationSession{})
		return
	}

	// Get filtered sessions based on user permissions
	sessions := s.wsHub.GetActiveSessionsForUser(c, user.Email)
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
		scheme = SchemeWSS
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
