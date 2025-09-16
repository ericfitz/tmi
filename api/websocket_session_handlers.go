package api

import (
	"runtime/debug"

	"github.com/ericfitz/tmi/internal/slogging"
)

// ResyncRequestHandler handles resync request messages
type ResyncRequestHandler struct{}

func (h *ResyncRequestHandler) MessageType() string {
	return "resync_request"
}

func (h *ResyncRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in ResyncRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processResyncRequest(client, message)
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
