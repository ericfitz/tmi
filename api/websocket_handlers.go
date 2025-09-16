package api

import (
	"encoding/json"
	"runtime/debug"

	"github.com/ericfitz/tmi/internal/slogging"
)

// MessageHandler defines the interface for handling WebSocket messages
type MessageHandler interface {
	HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error
	MessageType() string
}

// MessageRouter handles routing of WebSocket messages to appropriate handlers
type MessageRouter struct {
	handlers map[string]MessageHandler
}

// NewMessageRouter creates a new message router with default handlers
func NewMessageRouter() *MessageRouter {
	router := &MessageRouter{
		handlers: make(map[string]MessageHandler),
	}

	// Register default handlers
	router.RegisterHandler(&DiagramOperationHandler{})
	router.RegisterHandler(&PresenterRequestHandler{})
	router.RegisterHandler(&ChangePresenterHandler{})
	router.RegisterHandler(&RemoveParticipantHandler{})
	router.RegisterHandler(&PresenterDeniedHandler{})
	router.RegisterHandler(&PresenterCursorHandler{})
	router.RegisterHandler(&PresenterSelectionHandler{})
	router.RegisterHandler(&ResyncRequestHandler{})
	router.RegisterHandler(&UndoRequestHandler{})
	router.RegisterHandler(&RedoRequestHandler{})

	return router
}

// RegisterHandler registers a message handler for a specific message type
func (r *MessageRouter) RegisterHandler(handler MessageHandler) {
	r.handlers[handler.MessageType()] = handler
}

// RouteMessage routes a message to the appropriate handler
func (r *MessageRouter) RouteMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	// Add panic recovery for message routing
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in RouteMessage - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	// Log raw incoming message with wsmsg component (sanitized to remove newlines)
	sanitizedMessage := slogging.SanitizeLogMessage(string(message))
	slogging.Get().Debug("[wsmsg] Received WebSocket message - session_id=%s user_id=%s message_size=%d raw_message=%s",
		session.ID, client.UserID, len(message), sanitizedMessage)

	// Parse base message to determine type
	var baseMsg struct {
		MessageType string          `json:"message_type"`
		UserID      string          `json:"user_id"`
		Raw         json.RawMessage `json:"-"`
	}

	if err := json.Unmarshal(message, &baseMsg); err != nil {
		slogging.Get().Error("Failed to parse WebSocket message - Session: %s, User: %s, Error: %v, Message: %s",
			session.ID, client.UserID, err, sanitizedMessage)
		return err
	}

	// Log parsed message details
	slogging.Get().Debug("[wsmsg] Parsed message - session_id=%s message_type=%s user_id=%s",
		session.ID, baseMsg.MessageType, baseMsg.UserID)

	// Handle special client-initiated messages that should be ignored
	switch baseMsg.MessageType {
	case "participant_joined":
		// Client is notifying they've joined - this is handled automatically on connection
		slogging.Get().Debug("Received participant_joined from %s - ignored (join is automatic)", client.UserID)
		return nil
	case "participant_left":
		// Client is notifying they're leaving - this is handled automatically on disconnect
		slogging.Get().Debug("Received participant_left from %s - ignored (leave is automatic)", client.UserID)
		return nil
	case "participants_update":
		// Clients shouldn't send this - server sends it
		slogging.Get().Warn("Client %s sent participants_update - this is a server-only message", client.UserID)
		return nil
	}

	// Route to appropriate handler
	handler, exists := r.handlers[baseMsg.MessageType]
	if !exists {
		slogging.Get().Warn("Unknown message type '%s' from user %s in session %s", baseMsg.MessageType, client.UserID, session.ID)
		return nil
	}

	return handler.HandleMessage(session, client, message)
}
