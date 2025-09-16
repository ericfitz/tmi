package api

import (
	"runtime/debug"

	"github.com/ericfitz/tmi/internal/slogging"
)

// PresenterRequestHandler handles presenter request messages
type PresenterRequestHandler struct{}

func (h *PresenterRequestHandler) MessageType() string {
	return "presenter_request"
}

func (h *PresenterRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in PresenterRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processPresenterRequest(client, message)
	return nil
}

// ChangePresenterHandler handles change presenter messages
type ChangePresenterHandler struct{}

func (h *ChangePresenterHandler) MessageType() string {
	return "change_presenter"
}

func (h *ChangePresenterHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in ChangePresenterHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processChangePresenter(client, message)
	return nil
}

// RemoveParticipantHandler handles remove participant messages
type RemoveParticipantHandler struct{}

func (h *RemoveParticipantHandler) MessageType() string {
	return "remove_participant"
}

func (h *RemoveParticipantHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in RemoveParticipantHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processRemoveParticipant(client, message)
	return nil
}

// PresenterDeniedHandler handles presenter denied messages
type PresenterDeniedHandler struct{}

func (h *PresenterDeniedHandler) MessageType() string {
	return "presenter_denied"
}

func (h *PresenterDeniedHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in PresenterDeniedHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processPresenterDenied(client, message)
	return nil
}

// PresenterCursorHandler handles presenter cursor messages
type PresenterCursorHandler struct{}

func (h *PresenterCursorHandler) MessageType() string {
	return "presenter_cursor"
}

func (h *PresenterCursorHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in PresenterCursorHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processPresenterCursor(client, message)
	return nil
}

// PresenterSelectionHandler handles presenter selection messages
type PresenterSelectionHandler struct{}

func (h *PresenterSelectionHandler) MessageType() string {
	return "presenter_selection"
}

func (h *PresenterSelectionHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in PresenterSelectionHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processPresenterSelection(client, message)
	return nil
}
