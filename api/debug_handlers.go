package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// DebugHandlers provides HTTP endpoints for controlling debug logging
type DebugHandlers struct {
	wsDebugLogger *logging.WebSocketDebugLogger
}

// NewDebugHandlers creates a new debug handlers instance
func NewDebugHandlers() *DebugHandlers {
	return &DebugHandlers{
		wsDebugLogger: logging.GetWebSocketDebugLogger(),
	}
}

// HandleWebSocketDebugControl handles enabling/disabling WebSocket debug logging for sessions
// POST /debug/websocket/{session_id}?action=enable|disable
func (h *DebugHandlers) HandleWebSocketDebugControl(c *gin.Context) {
	sessionID := c.Param("session_id")
	action := c.Query("action")

	if sessionID == "" || sessionID == "/" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "session_id is required",
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
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "action must be 'enable', 'disable', or 'status'",
		})
	}
}

// HandleWebSocketDebugStatus returns status of all debug logging sessions
// GET /debug/websocket/status
func (h *DebugHandlers) HandleWebSocketDebugStatus(c *gin.Context) {
	enabledSessions := h.wsDebugLogger.GetEnabledSessions()

	c.JSON(http.StatusOK, gin.H{
		"enabled_sessions": enabledSessions,
		"count":            len(enabledSessions),
	})
}

// HandleWebSocketDebugClear disables debug logging for all sessions
// DELETE /debug/websocket/sessions
func (h *DebugHandlers) HandleWebSocketDebugClear(c *gin.Context) {
	h.wsDebugLogger.ClearAllSessions()

	c.JSON(http.StatusOK, gin.H{
		"status":  "cleared",
		"message": "Debug logging disabled for all sessions",
	})
}

// RegisterDebugRoutes registers debug routes with the gin router
// Note: These should only be enabled in development or with proper authentication
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
