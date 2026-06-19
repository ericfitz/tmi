package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// DebugHandlers provides HTTP endpoints for controlling debug logging
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: handler group that wraps the WebSocket debug logger for debug HTTP endpoints
type DebugHandlers struct {
	wsDebugLogger *slogging.WebSocketDebugLogger
}

// NewDebugHandlers creates a new debug handlers instance
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: build a DebugHandlers bound to the global WebSocket debug logger
func NewDebugHandlers() *DebugHandlers {
	return &DebugHandlers{
		wsDebugLogger: slogging.GetWebSocketDebugLogger(),
	}
}

// HandleWebSocketDebugControl handles enabling/disabling WebSocket debug logging for sessions
// POST /debug/websocket/{session_id}?action=enable|disable
// SEM@d86c7a3d58999ec91e9d2a8676d972f89424dad4: handle enable/disable/status debug logging for a specific WebSocket session (mutates shared state)
func (h *DebugHandlers) HandleWebSocketDebugControl(c *gin.Context) {
	sessionID := c.Param("session_id")
	action := c.Query("action")

	if sessionID == "" || sessionID == "/" {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "session_id is required",
		})
		return
	}

	switch action {
	case "enable":
		h.wsDebugLogger.EnableSessionLogging(sessionID)
		c.JSON(http.StatusOK, gin.H{
			"status":     "enabled",
			"session_id": sessionID,
		})
	case "disable":
		h.wsDebugLogger.DisableSessionLogging(sessionID)
		c.JSON(http.StatusOK, gin.H{
			"status":     "disabled",
			"session_id": sessionID,
		})
	case "status":
		enabled := h.wsDebugLogger.IsSessionLoggingEnabled(sessionID)
		c.JSON(http.StatusOK, gin.H{
			"status":     "checked",
			"session_id": sessionID,
			"enabled":    enabled,
		})
	default:
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "action must be 'enable', 'disable', or 'status'",
		})
	}
}

// HandleWebSocketDebugStatus returns status of all debug logging sessions
// GET /debug/websocket/status
// SEM@79bd6821708dbab17a998153f9a0d9ae26399bb5: list all WebSocket sessions that currently have debug logging enabled
func (h *DebugHandlers) HandleWebSocketDebugStatus(c *gin.Context) {
	enabledSessions := h.wsDebugLogger.GetEnabledSessions()

	c.JSON(http.StatusOK, gin.H{
		"enabled_sessions": enabledSessions,
		"count":            len(enabledSessions),
	})
}

// HandleWebSocketDebugClear disables debug logging for all sessions
// DELETE /debug/websocket/sessions
// SEM@79bd6821708dbab17a998153f9a0d9ae26399bb5: disable debug logging for all active WebSocket sessions (mutates shared state)
func (h *DebugHandlers) HandleWebSocketDebugClear(c *gin.Context) {
	h.wsDebugLogger.ClearAllSessions()

	c.JSON(http.StatusOK, gin.H{
		"status":  "cleared",
		"message": "Debug logging disabled for all sessions",
	})
}

// RegisterDebugRoutes registers debug routes with the gin router
// Note: These should only be enabled in development or with proper authentication
// SEM@79bd6821708dbab17a998153f9a0d9ae26399bb5: register authenticated debug routes for WebSocket log control on a Gin engine (mutates shared state)
func RegisterDebugRoutes(r *gin.Engine, requireAuth gin.HandlerFunc) {
	handlers := NewDebugHandlers()

	// Debug routes group - requires authentication
	debug := r.Group("/debug")
	debug.Use(requireAuth) // Ensure debug routes are protected

	// WebSocket debug logging routes
	debug.POST("/websocket/:session_id", handlers.HandleWebSocketDebugControl)
	debug.GET("/websocket/status", handlers.HandleWebSocketDebugStatus)
	debug.DELETE("/websocket/sessions", handlers.HandleWebSocketDebugClear)
}
