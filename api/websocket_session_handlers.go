package api

import (
	"runtime/debug"

	"github.com/ericfitz/tmi/internal/slogging"
)

// SyncStatusRequestHandler handles sync status request messages
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: WebSocket handler that dispatches sync_status_request messages to the diagram session
type SyncStatusRequestHandler struct{}

// SEM@d791c9a859555ac908a93f4bd6d49574103f13b9: return the sync_status_request message type identifier (pure)
func (h *SyncStatusRequestHandler) MessageType() string {
	return "sync_status_request"
}

// SEM@d791c9a859555ac908a93f4bd6d49574103f13b9: dispatch a sync_status_request to the session with panic recovery
func (h *SyncStatusRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in SyncStatusRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processSyncStatusRequest(client, message)
	return nil
}

// SyncRequestHandler handles sync request messages
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: WebSocket handler that dispatches sync_request messages to the diagram session
type SyncRequestHandler struct{}

// SEM@d791c9a859555ac908a93f4bd6d49574103f13b9: return the sync_request message type identifier (pure)
func (h *SyncRequestHandler) MessageType() string {
	return "sync_request"
}

// SEM@d791c9a859555ac908a93f4bd6d49574103f13b9: dispatch a sync_request to the session with panic recovery
func (h *SyncRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in SyncRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processSyncRequest(client, message)
	return nil
}

// UndoRequestHandler handles undo request messages
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: WebSocket handler that dispatches undo_request messages to the diagram session
type UndoRequestHandler struct{}

// SEM@d791c9a859555ac908a93f4bd6d49574103f13b9: return the undo_request message type identifier (pure)
func (h *UndoRequestHandler) MessageType() string {
	return "undo_request"
}

// SEM@d791c9a859555ac908a93f4bd6d49574103f13b9: dispatch an undo_request to the session with panic recovery
func (h *UndoRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in UndoRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processUndoRequest(client, message)
	return nil
}

// RedoRequestHandler handles redo request messages
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: WebSocket handler that dispatches redo_request messages to the diagram session
type RedoRequestHandler struct{}

// SEM@d791c9a859555ac908a93f4bd6d49574103f13b9: return the redo_request message type identifier (pure)
func (h *RedoRequestHandler) MessageType() string {
	return "redo_request"
}

// SEM@d791c9a859555ac908a93f4bd6d49574103f13b9: dispatch a redo_request to the session with panic recovery
func (h *RedoRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in RedoRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processRedoRequest(client, message)
	return nil
}
