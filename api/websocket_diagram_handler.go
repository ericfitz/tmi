package api

import (
	"encoding/json"
	"runtime/debug"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// DiagramOperationHandler handles diagram operation messages
type DiagramOperationHandler struct{}

// MessageType returns the message type this handler processes
func (h *DiagramOperationHandler) MessageType() string {
	return "diagram_operation"
}

// HandleMessage processes diagram operation messages
func (h *DiagramOperationHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in DiagramOperationHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	startTime := time.Now()
	slogging.Get().Debug("Processing diagram operation - Session: %s, User: %s", session.ID, client.UserID)

	var msg DiagramOperationMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Error("Failed to parse diagram operation - Session: %s, User: %s, Error: %v",
			session.ID, client.UserID, err)
		return err
	}

	// Use the existing applyOperation logic from DiagramSession
	if session.applyOperation(client, msg) {
		// Broadcast the operation to all other clients
		session.broadcastToOthers(client, msg)
	}

	processingTime := time.Since(startTime)
	slogging.Get().Debug("Completed diagram operation processing - Session: %s, User: %s, Duration: %v",
		session.ID, client.UserID, processingTime)

	return nil
}
