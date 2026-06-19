package api

import (
	"runtime/debug"

	"github.com/ericfitz/tmi/internal/slogging"
)

// PresenterRequestHandler handles presenter request messages
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: handle a presenter_request WebSocket message by dispatching to the session (pure)
type PresenterRequestHandler struct{}

// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: return the presenter_request message type discriminator (pure)
func (h *PresenterRequestHandler) MessageType() string {
	return "presenter_request"
}

// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: dispatch a presenter request WebSocket message to the diagram session (mutates shared state)
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
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: handle a change_presenter_request WebSocket message for presenter role transfer (pure)
type ChangePresenterRequestHandler struct{}

// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: return the change_presenter_request message type discriminator (pure)
func (h *ChangePresenterRequestHandler) MessageType() string {
	return "change_presenter_request"
}

// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: dispatch a change presenter WebSocket message to the diagram session (mutates shared state)
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
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: handle a remove_participant_request WebSocket message from the session host (pure)
type RemoveParticipantRequestHandler struct{}

// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: return the remove_participant_request message type discriminator (pure)
func (h *RemoveParticipantRequestHandler) MessageType() string {
	return "remove_participant_request"
}

// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: dispatch a remove participant WebSocket message to the diagram session (mutates shared state)
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
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: handle a presenter_denied_request WebSocket message from the host (pure)
type PresenterDeniedRequestHandler struct{}

// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: return the presenter_denied_request message type discriminator (pure)
func (h *PresenterDeniedRequestHandler) MessageType() string {
	return "presenter_denied_request"
}

// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: dispatch a presenter denied WebSocket message to the diagram session (mutates shared state)
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
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: handle a presenter_cursor WebSocket message for cursor position broadcast (pure)
type PresenterCursorHandler struct{}

// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: return the presenter_cursor message type discriminator (pure)
func (h *PresenterCursorHandler) MessageType() string {
	return "presenter_cursor"
}

// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: dispatch a presenter cursor WebSocket message to the diagram session (mutates shared state)
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
// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: handle a presenter_selection WebSocket message for selection state broadcast (pure)
type PresenterSelectionHandler struct{}

// SEM@90b176688ca38f0b04e4e70a233b332f1c28218e: return the presenter_selection message type discriminator (pure)
func (h *PresenterSelectionHandler) MessageType() string {
	return "presenter_selection"
}

// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: dispatch a presenter selection WebSocket message to the diagram session (mutates shared state)
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
