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
	router.RegisterHandler(&DiagramOperationRequestHandler{})
	router.RegisterHandler(&PresenterRequestHandler{})
	router.RegisterHandler(&ChangePresenterRequestHandler{})
	router.RegisterHandler(&RemoveParticipantRequestHandler{})
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

	// Handle server-only message types that clients shouldn't send
	switch baseMsg.MessageType {
	case "participants_update", "diagram_operation_event":
		slogging.Get().Warn("Client %s sent server-only message type '%s' - protocol violation", client.UserID, baseMsg.MessageType)
		session.sendErrorMessage(client, "invalid_message_type", "Message type '"+baseMsg.MessageType+"' is server-only and cannot be sent by clients")
		return nil
	}

	// Route to appropriate handler
	handler, exists := r.handlers[baseMsg.MessageType]
	if !exists {
		slogging.Get().Warn("Unsupported message type '%s' from user %s in session %s", baseMsg.MessageType, client.UserID, session.ID)
		session.sendErrorMessage(client, "unsupported_message_type", "Message type '"+baseMsg.MessageType+"' is not supported")
		return nil
	}

	slogging.Get().Debug("[TRACE-BROADCAST] Routing to handler for message_type=%s, handler type: %T", baseMsg.MessageType, handler)
	err := handler.HandleMessage(session, client, message)
	slogging.Get().Debug("[TRACE-BROADCAST] Handler returned - message_type=%s, error=%v", baseMsg.MessageType, err)
	return err
}
