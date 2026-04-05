package api

// Timmy chat endpoint handlers.
// These implement the ServerInterface methods generated from the OpenAPI spec.

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CreateTimmyChatSession creates a new chat session and streams preparation progress via SSE.
func (s *Server) CreateTimmyChatSession(c *gin.Context, threatModelId ThreatModelId) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":             "not_implemented",
		"error_description": "Timmy chat is not yet implemented",
	})
}

// ListTimmyChatSessions lists the current user's sessions for a threat model.
func (s *Server) ListTimmyChatSessions(c *gin.Context, threatModelId ThreatModelId, params ListTimmyChatSessionsParams) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":             "not_implemented",
		"error_description": "Timmy chat is not yet implemented",
	})
}

// GetTimmyChatSession retrieves a specific session.
func (s *Server) GetTimmyChatSession(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":             "not_implemented",
		"error_description": "Timmy chat is not yet implemented",
	})
}

// DeleteTimmyChatSession soft-deletes a session.
func (s *Server) DeleteTimmyChatSession(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":             "not_implemented",
		"error_description": "Timmy chat is not yet implemented",
	})
}

// CreateTimmyChatMessage sends a message and streams the assistant's response via SSE.
func (s *Server) CreateTimmyChatMessage(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":             "not_implemented",
		"error_description": "Timmy chat is not yet implemented",
	})
}

// ListTimmyChatMessages lists message history for a session.
func (s *Server) ListTimmyChatMessages(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId, params ListTimmyChatMessagesParams) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":             "not_implemented",
		"error_description": "Timmy chat is not yet implemented",
	})
}

// GetTimmyUsage returns aggregated usage statistics (admin only).
func (s *Server) GetTimmyUsage(c *gin.Context, params GetTimmyUsageParams) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":             "not_implemented",
		"error_description": "Timmy chat is not yet implemented",
	})
}

// GetTimmyStatus returns current memory and index status (admin only).
func (s *Server) GetTimmyStatus(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":             "not_implemented",
		"error_description": "Timmy chat is not yet implemented",
	})
}
