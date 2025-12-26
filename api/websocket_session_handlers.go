package api

import (
	"runtime/debug"

	"github.com/ericfitz/tmi/internal/slogging"
)

// SyncStatusRequestHandler handles sync status request messages
type SyncStatusRequestHandler struct{}

func (h *SyncStatusRequestHandler) MessageType() string {
	return "sync_status_request"
}

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
type SyncRequestHandler struct{}

func (h *SyncRequestHandler) MessageType() string {
	return "sync_request"
}

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
type UndoRequestHandler struct{}

func (h *UndoRequestHandler) MessageType() string {
	return "undo_request"
}

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
type RedoRequestHandler struct{}

func (h *RedoRequestHandler) MessageType() string {
	return "redo_request"
}

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
