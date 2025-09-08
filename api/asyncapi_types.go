package api

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AsyncAPI Message Types
// These types are manually implemented based on our AsyncAPI v3.0 specification
// in tmi-asyncapi.yml to provide type safety and validation for WebSocket messages

// MessageType represents the type of WebSocket message
type MessageType string

const (
	// Collaborative editing message types
	MessageTypeDiagramOperation    MessageType = "diagram_operation"
	MessageTypePresenterRequest    MessageType = "presenter_request"
	MessageTypePresenterDenied     MessageType = "presenter_denied"
	MessageTypeChangePresenter     MessageType = "change_presenter"
	MessageTypeRemoveParticipant   MessageType = "remove_participant"
	MessageTypeCurrentPresenter    MessageType = "current_presenter"
	MessageTypePresenterCursor     MessageType = "presenter_cursor"
	MessageTypePresenterSelection  MessageType = "presenter_selection"
	MessageTypeAuthorizationDenied MessageType = "authorization_denied"
	MessageTypeStateCorrection     MessageType = "state_correction"
	MessageTypeResyncRequest       MessageType = "resync_request"
	MessageTypeResyncResponse      MessageType = "resync_response"
	MessageTypeHistoryOperation    MessageType = "history_operation"
	MessageTypeUndoRequest         MessageType = "undo_request"
	MessageTypeRedoRequest         MessageType = "redo_request"

	// Session management message types
	MessageTypeParticipantJoined  MessageType = "participant_joined"
	MessageTypeParticipantLeft    MessageType = "participant_left"
	MessageTypeParticipantsUpdate MessageType = "participants_update"
	MessageTypeError              MessageType = "error"
)

// AsyncMessage is the base interface for all WebSocket messages
type AsyncMessage interface {
	GetMessageType() MessageType
	Validate() error
}

// DiagramOperationMessage represents enhanced collaborative editing operations
type DiagramOperationMessage struct {
	MessageType    MessageType        `json:"message_type"`
	OperationID    string             `json:"operation_id"`
	SequenceNumber *uint64            `json:"sequence_number,omitempty"` // Server-assigned
	Operation      CellPatchOperation `json:"operation"`
}

func (m DiagramOperationMessage) GetMessageType() MessageType { return m.MessageType }

func (m DiagramOperationMessage) Validate() error {
	if m.MessageType != MessageTypeDiagramOperation {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeDiagramOperation, m.MessageType)
	}
	if m.OperationID == "" {
		return fmt.Errorf("operation_id is required")
	}
	if _, err := uuid.Parse(m.OperationID); err != nil {
		return fmt.Errorf("operation_id must be a valid UUID: %w", err)
	}
	return m.Operation.Validate()
}

// CellPatchOperation mirrors REST PATCH operations for cells with batch support
type CellPatchOperation struct {
	Type  string          `json:"type"`
	Cells []CellOperation `json:"cells"`
}

func (op CellPatchOperation) Validate() error {
	if op.Type != "patch" {
		return fmt.Errorf("operation type must be 'patch', got: %s", op.Type)
	}
	if len(op.Cells) == 0 {
		return fmt.Errorf("at least one cell operation is required")
	}
	for i, cellOp := range op.Cells {
		if err := cellOp.Validate(); err != nil {
			return fmt.Errorf("cell operation %d invalid: %w", i, err)
		}
	}
	return nil
}

// CellOperation represents a single cell operation (add/update/remove)
type CellOperation struct {
	ID        string `json:"id"`
	Operation string `json:"operation"`
	Data      *Cell  `json:"data,omitempty"`
}

func (op CellOperation) Validate() error {
	if op.ID == "" {
		return fmt.Errorf("cell id is required")
	}
	if _, err := uuid.Parse(op.ID); err != nil {
		return fmt.Errorf("cell id must be a valid UUID: %w", err)
	}

	switch op.Operation {
	case "add", "update":
		if op.Data == nil {
			return fmt.Errorf("%s operation requires cell data", op.Operation)
		}
		if op.Data.Id.String() != op.ID {
			return fmt.Errorf("cell data ID (%s) must match operation ID (%s)", op.Data.Id.String(), op.ID)
		}
	case "remove":
		if op.Data != nil {
			return fmt.Errorf("remove operation should not include cell data")
		}
	default:
		return fmt.Errorf("invalid operation type: %s (must be add, update, or remove)", op.Operation)
	}

	return nil
}

// Presenter Mode Messages

type PresenterRequestMessage struct {
	MessageType MessageType `json:"message_type"`
}

func (m PresenterRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m PresenterRequestMessage) Validate() error {
	if m.MessageType != MessageTypePresenterRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypePresenterRequest, m.MessageType)
	}
	return nil
}

type PresenterDeniedMessage struct {
	MessageType MessageType `json:"message_type"`
	User        User        `json:"user"`
	TargetUser  string      `json:"target_user"`
}

func (m PresenterDeniedMessage) GetMessageType() MessageType { return m.MessageType }

func (m PresenterDeniedMessage) Validate() error {
	if m.MessageType != MessageTypePresenterDenied {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypePresenterDenied, m.MessageType)
	}
	if m.User.UserId == "" {
		return fmt.Errorf("user.user_id is required")
	}
	if m.TargetUser == "" {
		return fmt.Errorf("target_user is required")
	}
	return nil
}

type ChangePresenterMessage struct {
	MessageType  MessageType `json:"message_type"`
	User         User        `json:"user"`
	NewPresenter string      `json:"new_presenter"`
}

func (m ChangePresenterMessage) GetMessageType() MessageType { return m.MessageType }

func (m ChangePresenterMessage) Validate() error {
	if m.MessageType != MessageTypeChangePresenter {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeChangePresenter, m.MessageType)
	}
	if m.User.UserId == "" {
		return fmt.Errorf("user.user_id is required")
	}
	if m.NewPresenter == "" {
		return fmt.Errorf("new_presenter is required")
	}
	return nil
}

type RemoveParticipantMessage struct {
	MessageType MessageType `json:"message_type"`
	User        User        `json:"user"`
	TargetUser  string      `json:"target_user"`
}

func (m RemoveParticipantMessage) GetMessageType() MessageType { return m.MessageType }

func (m RemoveParticipantMessage) Validate() error {
	if m.MessageType != MessageTypeRemoveParticipant {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeRemoveParticipant, m.MessageType)
	}
	if m.User.UserId == "" {
		return fmt.Errorf("user.user_id is required")
	}
	if m.TargetUser == "" {
		return fmt.Errorf("target_user is required")
	}
	return nil
}

type CurrentPresenterMessage struct {
	MessageType      MessageType `json:"message_type"`
	CurrentPresenter string      `json:"current_presenter"`
}

func (m CurrentPresenterMessage) GetMessageType() MessageType { return m.MessageType }

func (m CurrentPresenterMessage) Validate() error {
	if m.MessageType != MessageTypeCurrentPresenter {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeCurrentPresenter, m.MessageType)
	}
	if m.CurrentPresenter == "" {
		return fmt.Errorf("current_presenter is required")
	}
	return nil
}

// CursorPosition represents cursor coordinates
type CursorPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type PresenterCursorMessage struct {
	MessageType    MessageType    `json:"message_type"`
	CursorPosition CursorPosition `json:"cursor_position"`
}

func (m PresenterCursorMessage) GetMessageType() MessageType { return m.MessageType }

func (m PresenterCursorMessage) Validate() error {
	if m.MessageType != MessageTypePresenterCursor {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypePresenterCursor, m.MessageType)
	}
	return nil
}

type PresenterSelectionMessage struct {
	MessageType   MessageType `json:"message_type"`
	SelectedCells []string    `json:"selected_cells"`
}

func (m PresenterSelectionMessage) GetMessageType() MessageType { return m.MessageType }

func (m PresenterSelectionMessage) Validate() error {
	if m.MessageType != MessageTypePresenterSelection {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypePresenterSelection, m.MessageType)
	}
	// Validate that selected cells are valid UUIDs
	for i, cellID := range m.SelectedCells {
		if _, err := uuid.Parse(cellID); err != nil {
			return fmt.Errorf("selected_cells[%d] must be a valid UUID: %w", i, err)
		}
	}
	return nil
}

// Authorization and State Messages

type AuthorizationDeniedMessage struct {
	MessageType         MessageType `json:"message_type"`
	OriginalOperationID string      `json:"original_operation_id"`
	Reason              string      `json:"reason"`
}

func (m AuthorizationDeniedMessage) GetMessageType() MessageType { return m.MessageType }

func (m AuthorizationDeniedMessage) Validate() error {
	if m.MessageType != MessageTypeAuthorizationDenied {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeAuthorizationDenied, m.MessageType)
	}
	if m.OriginalOperationID == "" {
		return fmt.Errorf("original_operation_id is required")
	}
	if _, err := uuid.Parse(m.OriginalOperationID); err != nil {
		return fmt.Errorf("original_operation_id must be a valid UUID: %w", err)
	}
	if m.Reason == "" {
		return fmt.Errorf("reason is required")
	}
	return nil
}

type StateCorrectionMessage struct {
	MessageType MessageType `json:"message_type"`
	Cells       []Cell      `json:"cells"`
}

func (m StateCorrectionMessage) GetMessageType() MessageType { return m.MessageType }

func (m StateCorrectionMessage) Validate() error {
	if m.MessageType != MessageTypeStateCorrection {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeStateCorrection, m.MessageType)
	}
	if len(m.Cells) == 0 {
		return fmt.Errorf("at least one cell is required for state correction")
	}
	return nil
}

// History and Sync Messages

type ResyncRequestMessage struct {
	MessageType MessageType `json:"message_type"`
}

func (m ResyncRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m ResyncRequestMessage) Validate() error {
	if m.MessageType != MessageTypeResyncRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeResyncRequest, m.MessageType)
	}
	return nil
}

type ResyncResponseMessage struct {
	MessageType   MessageType `json:"message_type"`
	User          User        `json:"user"`
	TargetUser    string      `json:"target_user"`
	Method        string      `json:"method"`
	DiagramID     string      `json:"diagram_id"`
	ThreatModelID string      `json:"threat_model_id,omitempty"`
}

func (m ResyncResponseMessage) GetMessageType() MessageType { return m.MessageType }

func (m ResyncResponseMessage) Validate() error {
	if m.MessageType != MessageTypeResyncResponse {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeResyncResponse, m.MessageType)
	}
	if m.User.UserId == "" {
		return fmt.Errorf("user.user_id is required")
	}
	if m.TargetUser == "" {
		return fmt.Errorf("target_user is required")
	}
	if m.Method == "" {
		return fmt.Errorf("method is required")
	}
	if m.DiagramID == "" {
		return fmt.Errorf("diagram_id is required")
	}
	return nil
}

type HistoryOperationMessage struct {
	MessageType   MessageType `json:"message_type"`
	OperationType string      `json:"operation_type"`
	Message       string      `json:"message"`
}

func (m HistoryOperationMessage) GetMessageType() MessageType { return m.MessageType }

func (m HistoryOperationMessage) Validate() error {
	if m.MessageType != MessageTypeHistoryOperation {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeHistoryOperation, m.MessageType)
	}
	if m.OperationType != "undo" && m.OperationType != "redo" {
		return fmt.Errorf("operation_type must be 'undo' or 'redo', got: %s", m.OperationType)
	}
	return nil
}

type UndoRequestMessage struct {
	MessageType MessageType `json:"message_type"`
}

func (m UndoRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m UndoRequestMessage) Validate() error {
	if m.MessageType != MessageTypeUndoRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeUndoRequest, m.MessageType)
	}
	return nil
}

type RedoRequestMessage struct {
	MessageType MessageType `json:"message_type"`
}

func (m RedoRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m RedoRequestMessage) Validate() error {
	if m.MessageType != MessageTypeRedoRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeRedoRequest, m.MessageType)
	}
	return nil
}

// AsyncParticipant represents a participant in the AsyncAPI format
type AsyncParticipant struct {
	User         AsyncUser `json:"user"`
	Permissions  string    `json:"permissions"`
	LastActivity time.Time `json:"last_activity"`
}

// AsyncUser represents user information in AsyncAPI format
type AsyncUser struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

// ParticipantsUpdateMessage provides complete participant list with roles
type ParticipantsUpdateMessage struct {
	MessageType      MessageType        `json:"message_type"`
	Participants     []AsyncParticipant `json:"participants"`
	Host             string             `json:"host"`
	CurrentPresenter string             `json:"current_presenter"`
}

func (m ParticipantsUpdateMessage) GetMessageType() MessageType { return m.MessageType }

func (m ParticipantsUpdateMessage) Validate() error {
	if m.MessageType != MessageTypeParticipantsUpdate {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeParticipantsUpdate, m.MessageType)
	}
	if m.Host == "" {
		return fmt.Errorf("host is required")
	}
	// Current presenter can be empty
	// Validate participants
	for i, p := range m.Participants {
		if p.User.UserID == "" {
			return fmt.Errorf("participant[%d].user.user_id is required", i)
		}
		if p.User.Name == "" {
			return fmt.Errorf("participant[%d].user.name is required", i)
		}
		if p.User.Email == "" {
			return fmt.Errorf("participant[%d].user.email is required", i)
		}
		if p.Permissions != "reader" && p.Permissions != "writer" {
			return fmt.Errorf("participant[%d].permissions must be 'reader' or 'writer', got '%s'", i, p.Permissions)
		}
		if p.LastActivity.IsZero() {
			return fmt.Errorf("participant[%d].last_activity is required", i)
		}
	}
	return nil
}

// Message Parser utility to parse incoming WebSocket messages
func ParseAsyncMessage(data []byte) (AsyncMessage, error) {
	// First, parse to determine message type
	var base struct {
		MessageType MessageType `json:"message_type"`
	}

	if err := json.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("failed to parse base message: %w", err)
	}

	// Parse into specific message type
	switch base.MessageType {
	case MessageTypeDiagramOperation:
		var msg DiagramOperationMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse diagram operation message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypePresenterRequest:
		var msg PresenterRequestMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse presenter request message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeChangePresenter:
		var msg ChangePresenterMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse change presenter message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeRemoveParticipant:
		var msg RemoveParticipantMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse remove participant message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypePresenterCursor:
		var msg PresenterCursorMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse presenter cursor message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypePresenterSelection:
		var msg PresenterSelectionMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse presenter selection message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeResyncRequest:
		var msg ResyncRequestMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse resync request message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeResyncResponse:
		var msg ResyncResponseMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse resync response message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeUndoRequest:
		var msg UndoRequestMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse undo request message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeRedoRequest:
		var msg RedoRequestMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse redo request message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeParticipantsUpdate:
		var msg ParticipantsUpdateMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse participants update message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeParticipantJoined:
		var msg ParticipantJoinedMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse participant joined message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeParticipantLeft:
		var msg ParticipantLeftMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse participant left message: %w", err)
		}
		return msg, msg.Validate()

	case MessageTypeError:
		var msg ErrorMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse error message: %w", err)
		}
		return msg, msg.Validate()

	default:
		return nil, fmt.Errorf("unsupported message type: %s", base.MessageType)
	}
}

// ParticipantJoinedMessage notifies when a participant joins a session
type ParticipantJoinedMessage struct {
	MessageType MessageType `json:"message_type"`
	User        User        `json:"user"`
	Timestamp   time.Time   `json:"timestamp"`
}

func (m ParticipantJoinedMessage) GetMessageType() MessageType { return m.MessageType }

func (m ParticipantJoinedMessage) Validate() error {
	if m.MessageType != MessageTypeParticipantJoined {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeParticipantJoined, m.MessageType)
	}
	if m.User.UserId == "" {
		return fmt.Errorf("user.user_id is required")
	}
	return nil
}

// ParticipantLeftMessage notifies when a participant leaves a session
type ParticipantLeftMessage struct {
	MessageType MessageType `json:"message_type"`
	User        User        `json:"user"`
	Timestamp   time.Time   `json:"timestamp"`
}

func (m ParticipantLeftMessage) GetMessageType() MessageType { return m.MessageType }

func (m ParticipantLeftMessage) Validate() error {
	if m.MessageType != MessageTypeParticipantLeft {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeParticipantLeft, m.MessageType)
	}
	if m.User.UserId == "" {
		return fmt.Errorf("user.user_id is required")
	}
	return nil
}

// ErrorMessage represents an error response
type ErrorMessage struct {
	MessageType      MessageType `json:"message_type"`
	Error            string      `json:"error"`
	Message          string      `json:"message"`
	ErrorDescription *string     `json:"error_description,omitempty"`
	Timestamp        time.Time   `json:"timestamp"`
}

func (m ErrorMessage) GetMessageType() MessageType { return m.MessageType }

func (m ErrorMessage) Validate() error {
	if m.MessageType != MessageTypeError {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeError, m.MessageType)
	}
	if m.Error == "" {
		return fmt.Errorf("error is required")
	}
	if m.Message == "" {
		return fmt.Errorf("message is required")
	}
	return nil
}

// Helper function to marshal AsyncMessage to JSON
func MarshalAsyncMessage(msg AsyncMessage) ([]byte, error) {
	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("message validation failed: %w", err)
	}
	return json.Marshal(msg)
}
