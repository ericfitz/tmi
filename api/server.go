package api

import (
	"github.com/gin-gonic/gin"
)

// Server is the main API server instance
type Server struct {
	// Handlers
	threatModelHandler *ThreatModelHandler
	// Add other handlers as needed
}

// NewServer creates a new API server instance
func NewServer() *Server {
	return &Server{
		threatModelHandler: NewThreatModelHandler(),
	}
}

// RegisterHandlers registers custom API handlers with the router
func (s *Server) RegisterHandlers(r *gin.Engine) {
	// We'll rely on the generated routes instead of custom ones
	// This method is kept as a placeholder for future custom routes if needed
}