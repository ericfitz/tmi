package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestDebugHandlersWebSocketControl tests the WebSocket debug control endpoints
func TestDebugHandlersWebSocketControl(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create test router
	r := gin.New()
	handlers := NewDebugHandlers()

	// Set up routes
	r.POST("/debug/websocket/:session_id", handlers.HandleWebSocketDebugControl)
	r.GET("/debug/websocket/status", handlers.HandleWebSocketDebugStatus)
	r.DELETE("/debug/websocket/sessions", handlers.HandleWebSocketDebugClear)

	sessionID := "test-session-123"

	t.Run("Enable WebSocket Debug Logging", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/debug/websocket/"+sessionID+"?action=enable", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Equal(t, "enabled", response["status"])
		assert.Equal(t, sessionID, response["session_id"])
	})

	t.Run("Check Status After Enable", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/debug/websocket/"+sessionID+"?action=status", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Equal(t, "checked", response["status"])
		assert.Equal(t, sessionID, response["session_id"])
		assert.Equal(t, true, response["enabled"])
	})

	t.Run("Get Status of All Sessions", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/debug/websocket/status", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		enabledSessions, ok := response["enabled_sessions"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, enabledSessions, 1)
		assert.Equal(t, sessionID, enabledSessions[0])
		assert.Equal(t, float64(1), response["count"]) // JSON unmarshals numbers as float64
	})

	t.Run("Disable WebSocket Debug Logging", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/debug/websocket/"+sessionID+"?action=disable", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Equal(t, "disabled", response["status"])
		assert.Equal(t, sessionID, response["session_id"])
	})

	t.Run("Clear All Sessions", func(t *testing.T) {
		// First enable a session
		req := httptest.NewRequest("POST", "/debug/websocket/"+sessionID+"?action=enable", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Then clear all sessions
		req = httptest.NewRequest("DELETE", "/debug/websocket/sessions", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Equal(t, "cleared", response["status"])

		// Verify all sessions are cleared
		req = httptest.NewRequest("GET", "/debug/websocket/status", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)

		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		enabledSessions, ok := response["enabled_sessions"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, enabledSessions, 0)
		assert.Equal(t, float64(0), response["count"])
	})
}

// TestDebugHandlersErrorCases tests error handling in debug handlers
func TestDebugHandlersErrorCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create test router
	r := gin.New()
	handlers := NewDebugHandlers()

	// Set up routes
	r.POST("/debug/websocket/:session_id", handlers.HandleWebSocketDebugControl)

	t.Run("Invalid Action", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/debug/websocket/test-session?action=invalid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Contains(t, response["error"], "action must be")
	})

	t.Run("No Action Specified", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/debug/websocket/test-session", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Contains(t, response["error"], "action must be")
	})
}
