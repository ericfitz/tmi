package api

import (
	"context"
	
	"github.com/gin-gonic/gin"
)

// Server is the main API server instance
type Server struct {
	// Handlers
	threatModelHandler *ThreatModelHandler
	diagramHandler     *DiagramHandler
	// WebSocket hub
	wsHub              *WebSocketHub
}

// NewServer creates a new API server instance
func NewServer() *Server {
	return &Server{
		threatModelHandler: NewThreatModelHandler(),
		diagramHandler:     NewDiagramHandler(),
		wsHub:              NewWebSocketHub(),
	}
}

// RegisterHandlers registers custom API handlers with the router
func (s *Server) RegisterHandlers(r *gin.Engine) {
	// Register WebSocket handler - it needs a custom route because it's not part of the OpenAPI spec
	r.GET("/ws/diagrams/:id", s.HandleWebSocket)
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