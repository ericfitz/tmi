package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Server is the main API server instance
type Server struct {
	// Handlers
	threatModelHandler *ThreatModelHandler
	// diagramHandler     *DiagramHandler  // Disabled - diagram endpoints removed
	// WebSocket hub
	wsHub *WebSocketHub
}

// NewServer creates a new API server instance
func NewServer() *Server {
	return &Server{
		threatModelHandler: NewThreatModelHandler(),
		// diagramHandler:     NewDiagramHandler(),  // Disabled - diagram endpoints removed
		wsHub: NewWebSocketHub(),
	}
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
	// Register WebSocket handler - it needs a custom route because it's not part of the OpenAPI spec
	r.GET("/ws/diagrams/:id", s.HandleWebSocket)

	// Register collaboration sessions endpoint
	r.GET("/collaboration/sessions", s.HandleCollaborationSessions)

	// Register server info endpoint
	r.GET("/api/server-info", s.HandleServerInfo)
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

// HandleCollaborationSessions returns all active collaboration sessions
func (s *Server) HandleCollaborationSessions(c *gin.Context) {
	sessions := s.wsHub.GetActiveSessions()
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
