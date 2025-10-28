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
	slogging.Get().Debug("Processing diagram operation - Session: %s, User: %s, Client pointer: %p", session.ID, client.UserID, client)

	var msg DiagramOperationMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		slogging.Get().Error("Failed to parse diagram operation - Session: %s, User: %s, Error: %v",
			session.ID, client.UserID, err)
		return err
	}

	// Note: DiagramOperationMessage doesn't currently include user identity field,
	// so we rely on the authenticated client context from the WebSocket connection.
	// If user attribution is added in the future, validate with validateAndEnforceUserIdentity()

	// Assign sequence number for operation tracking
	session.mu.Lock()
	sequenceNumber := session.NextSequenceNumber
	session.NextSequenceNumber++
	session.mu.Unlock()

	msg.SequenceNumber = &sequenceNumber

	slogging.Get().Debug("Assigned sequence number - Session: %s, User: %s, OperationID: %s, SequenceNumber: %d",
		session.ID, client.UserID, msg.OperationID, sequenceNumber)

	// Use the existing applyOperation logic from DiagramSession
	applied := session.applyOperation(client, msg)

	session.mu.RLock()
	totalClients := len(session.Clients)
	session.mu.RUnlock()

	slogging.Get().Info("Diagram operation validation result - Session: %s, User: %s, OperationID: %s, Applied: %v, Total clients in session: %d",
		session.ID, client.UserID, msg.OperationID, applied, totalClients)

	if applied {
		// Broadcast the operation to all other clients
		slogging.Get().Info("Broadcasting diagram operation - Session: %s, Sender: %s (%p), OperationID: %s, Recipients: %d",
			session.ID, client.UserID, client, msg.OperationID, totalClients-1)
		session.broadcastToOthers(client, msg)
	} else {
		slogging.Get().Warn("Diagram operation NOT broadcasted - Session: %s, User: %s, OperationID: %s, Reason: applyOperation returned false (check validation logs)",
			session.ID, client.UserID, msg.OperationID)
	}

	processingTime := time.Since(startTime)
	slogging.Get().Debug("Completed diagram operation processing - Session: %s, User: %s, Duration: %v",
		session.ID, client.UserID, processingTime)

	return nil
}
