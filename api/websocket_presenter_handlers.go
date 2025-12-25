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

// ChangePresenterRequestHandler handles change presenter request messages
type ChangePresenterRequestHandler struct{}

func (h *ChangePresenterRequestHandler) MessageType() string {
	return "change_presenter_request"
}

func (h *ChangePresenterRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in ChangePresenterRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processChangePresenter(client, message)
	return nil
}

// RemoveParticipantRequestHandler handles remove participant request messages
type RemoveParticipantRequestHandler struct{}

func (h *RemoveParticipantRequestHandler) MessageType() string {
	return "remove_participant_request"
}

func (h *RemoveParticipantRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in RemoveParticipantRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	session.processRemoveParticipant(client, message)
	return nil
}

// PresenterDeniedRequestHandler handles presenter denied request messages from host
type PresenterDeniedRequestHandler struct{}

func (h *PresenterDeniedRequestHandler) MessageType() string {
	return "presenter_denied_request"
}

func (h *PresenterDeniedRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
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
