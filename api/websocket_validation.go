package api

import (
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// UserInfoExtractor handles extracting user information from the request context
type UserInfoExtractor struct{}

// UserInfo represents extracted user information
type UserInfo struct {
	UserID    string
	UserName  string
	UserEmail string
}

// ExtractUserInfo extracts user information from the gin context
func (u *UserInfoExtractor) ExtractUserInfo(c *gin.Context) (*UserInfo, error) {
	// Get user ID from context (required)
	userIDStr := ""
	if userID, exists := c.Get("userID"); exists {
		if id, ok := userID.(string); ok && id != "" {
			userIDStr = id
		}
	}
	if userIDStr == "" {
		return nil, fmt.Errorf("user ID not found in context")
	}

	// Get user display name from context (optional, will use email as fallback)
	userNameStr := ""
	if userName, exists := c.Get("user_name"); exists {
		if name, ok := userName.(string); ok && name != "" {
			userNameStr = name
		}
	}

	// Get user email from context
	userEmailStr := ""
	if userEmail, exists := c.Get("userEmail"); exists {
		if email, ok := userEmail.(string); ok && email != "" {
			userEmailStr = email
		}
	}

	// If no display name, use email as fallback
	if userNameStr == "" && userEmailStr != "" {
		userNameStr = userEmailStr
	}

	// If still no display name, use user ID as last resort
	if userNameStr == "" {
		userNameStr = userIDStr
	}

	return &UserInfo{
		UserID:    userIDStr,
		UserName:  userNameStr,
		UserEmail: userEmailStr,
	}, nil
}

// SessionValidator handles session validation logic
type SessionValidator struct{}

// ValidateSessionAccess validates that a user can access a diagram session
func (v *SessionValidator) ValidateSessionAccess(hub *WebSocketHub, userInfo *UserInfo, threatModelID, diagramID string) error {
	// For backwards compatibility, use email for validation if userID lookup fails
	validationID := userInfo.UserID
	if userInfo.UserEmail != "" {
		// TODO: Update validateWebSocketDiagramAccessDirect to use user ID instead of email
		validationID = userInfo.UserEmail
	}

	if !hub.validateWebSocketDiagramAccessDirect(validationID, threatModelID, diagramID) {
		return fmt.Errorf("insufficient permissions to collaborate on diagram %s", diagramID)
	}

	return nil
}

// ValidateSessionState validates the session is in the correct state for connection
func (v *SessionValidator) ValidateSessionState(session *DiagramSession) error {
	session.mu.RLock()
	sessionState := session.State
	session.mu.RUnlock()

	if sessionState != SessionStateActive {
		return fmt.Errorf("session %s is not active (state: %s)", session.ID, sessionState)
	}

	return nil
}

// ValidateSessionID validates that the provided session ID matches the actual session
func (v *SessionValidator) ValidateSessionID(session *DiagramSession, providedSessionID string) error {
	if providedSessionID == "" {
		return nil // No session ID provided, which is acceptable
	}

	session.mu.RLock()
	actualSessionID := session.ID
	session.mu.RUnlock()

	if providedSessionID != actualSessionID {
		return fmt.Errorf("session ID mismatch - provided: %s, actual: %s", providedSessionID, actualSessionID)
	}

	return nil
}

// WebSocketConnectionManager handles WebSocket connection setup and error handling
type WebSocketConnectionManager struct{}

// SendErrorAndClose sends an error message to the WebSocket connection and closes it
func (m *WebSocketConnectionManager) SendErrorAndClose(conn *websocket.Conn, errorCode, errorMessage string) {
	errorMsg := ErrorMessage{
		MessageType: MessageTypeError,
		Error:       errorCode,
		Message:     errorMessage,
		Timestamp:   time.Now().UTC(),
	}

	if msgBytes, err := MarshalAsyncMessage(errorMsg); err == nil {
		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			logging.Get().Debug("Failed to send error message: %v", err)
		}
	}

	// Close the connection
	if err := conn.Close(); err != nil {
		logging.Get().Debug("Failed to close connection: %v", err)
	}
}

// SendCloseAndClose sends a close message to the WebSocket connection and closes it
func (m *WebSocketConnectionManager) SendCloseAndClose(conn *websocket.Conn, closeCode int, closeText string) {
	closeMsg := websocket.FormatCloseMessage(closeCode, closeText)
	if err := conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second)); err != nil {
		logging.Get().Debug("Failed to send close message: %v", err)
	}

	// Close the connection
	if err := conn.Close(); err != nil {
		logging.Get().Debug("Failed to close connection: %v", err)
	}
}

// RegisterClientWithTimeout registers a client with the session with a timeout to prevent blocking
func (m *WebSocketConnectionManager) RegisterClientWithTimeout(session *DiagramSession, client *WebSocketClient, timeoutDuration time.Duration) error {
	select {
	case session.Register <- client:
		logging.Get().Debug("Successfully sent client to Register channel - User: %s, Session: %s", client.UserID, session.ID)
		return nil
	case <-time.After(timeoutDuration):
		logging.Get().Error("Timeout registering WebSocket client - User: %s, Session: %s (session may be blocked or dead)",
			client.UserID, session.ID)
		return fmt.Errorf("timeout registering client after %v", timeoutDuration)
	}
}
