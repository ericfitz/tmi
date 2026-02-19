package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ValidateUserIdentity validates that a User struct contains at least one valid identifier
func ValidateUserIdentity(u User) error {
	hasProviderId := u.ProviderId != ""
	hasEmail := u.Email != ""

	if !hasProviderId && !hasEmail {
		return fmt.Errorf("user must have either provider_id or email")
	}

	if hasEmail {
		// Basic email format check
		emailStr := string(u.Email)
		if !strings.Contains(emailStr, "@") || !strings.Contains(emailStr, ".") {
			return fmt.Errorf("invalid email format")
		}
	}

	return nil
}

// AsyncAPI Message Types
// These types are manually implemented based on our AsyncAPI v3.0 specification
// in tmi-asyncapi.yml to provide type safety and validation for WebSocket messages

// MessageType represents the type of WebSocket message
type MessageType string

const (
	// Collaborative editing message types
	MessageTypeDiagramOperation       MessageType = "diagram_operation"
	MessageTypePresenterRequest       MessageType = "presenter_request"
	MessageTypePresenterDeniedRequest MessageType = "presenter_denied_request"
	MessageTypePresenterDeniedEvent   MessageType = "presenter_denied_event"
	MessageTypeChangePresenter        MessageType = "change_presenter"
	MessageTypeRemoveParticipant      MessageType = "remove_participant"
	MessageTypePresenterCursor        MessageType = "presenter_cursor"
	MessageTypePresenterSelection     MessageType = "presenter_selection"
	MessageTypeAuthorizationDenied    MessageType = "authorization_denied"
	MessageTypeHistoryOperation       MessageType = "history_operation"
	MessageTypeUndoRequest            MessageType = "undo_request"
	MessageTypeRedoRequest            MessageType = "redo_request"

	// Sync message types (new protocol)
	MessageTypeSyncStatusRequest  MessageType = "sync_status_request"
	MessageTypeSyncStatusResponse MessageType = "sync_status_response"
	MessageTypeSyncRequest        MessageType = "sync_request"
	MessageTypeDiagramState       MessageType = "diagram_state"

	// Request/Event pattern message types (Client→Server requests, Server→Client events)
	MessageTypeDiagramOperationRequest  MessageType = "diagram_operation_request"
	MessageTypeDiagramOperationEvent    MessageType = "diagram_operation_event"
	MessageTypePresenterRequestEvent    MessageType = "presenter_request_event"
	MessageTypeChangePresenterRequest   MessageType = "change_presenter_request"
	MessageTypeRemoveParticipantRequest MessageType = "remove_participant_request"

	// Session management message types
	MessageTypeParticipantsUpdate MessageType = "participants_update"
	MessageTypeError              MessageType = "error"
	MessageTypeOperationRejected  MessageType = "operation_rejected"
)

// AsyncMessage is the base interface for all WebSocket messages
type AsyncMessage interface {
	GetMessageType() MessageType
	Validate() error
}

// DiagramOperationMessage represents enhanced collaborative editing operations
type DiagramOperationMessage struct {
	MessageType    MessageType        `json:"message_type"`
	InitiatingUser User               `json:"initiating_user"`
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
	ID        string                 `json:"id"`
	Operation string                 `json:"operation"`
	Data      *DfdDiagram_Cells_Item `json:"data,omitempty"` // Union type: Node | Edge
}

func (op CellOperation) Validate() error {
	if op.ID == "" {
		return fmt.Errorf("cell id is required")
	}
	if _, err := uuid.Parse(op.ID); err != nil {
		return fmt.Errorf("cell id must be a valid UUID: %w", err)
	}

	switch op.Operation {
	case string(Add), "update":
		if op.Data == nil {
			return fmt.Errorf("%s operation requires cell data", op.Operation)
		}
		// Extract ID from the union type (Node or Edge)
		var cellID string
		if node, err := op.Data.AsNode(); err == nil {
			cellID = node.Id.String()
		} else if edge, err := op.Data.AsEdge(); err == nil {
			cellID = edge.Id.String()
		} else {
			return fmt.Errorf("cell data must be either a Node or Edge")
		}

		if cellID != op.ID {
			return fmt.Errorf("cell data ID (%s) must match operation ID (%s)", cellID, op.ID)
		}
	case string(Remove):
		if op.Data != nil {
			return fmt.Errorf("remove operation should not include cell data")
		}
	default:
		return fmt.Errorf("invalid operation type: %s (must be add, update, or remove)", op.Operation)
	}

	return nil
}

// New Request Message Types (Client→Server, no initiating_user)

// DiagramOperationRequest is sent by client to perform a diagram operation
type DiagramOperationRequest struct {
	MessageType    MessageType        `json:"message_type"`
	OperationID    string             `json:"operation_id"`
	BaseVector     *int64             `json:"base_vector,omitempty"`     // Client's state when operation was created
	SequenceNumber *uint64            `json:"sequence_number,omitempty"` // Server-assigned
	Operation      CellPatchOperation `json:"operation"`
}

func (m DiagramOperationRequest) GetMessageType() MessageType { return m.MessageType }

func (m DiagramOperationRequest) Validate() error {
	if m.MessageType != MessageTypeDiagramOperationRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s",
			MessageTypeDiagramOperationRequest, m.MessageType)
	}
	if m.OperationID == "" {
		return fmt.Errorf("operation_id is required")
	}
	if _, err := uuid.Parse(m.OperationID); err != nil {
		return fmt.Errorf("operation_id must be a valid UUID: %w", err)
	}
	if err := m.Operation.Validate(); err != nil {
		return fmt.Errorf("operation: %w", err)
	}
	return nil
}

// ChangePresenterRequest is sent by client to change presenter
type ChangePresenterRequest struct {
	MessageType  MessageType `json:"message_type"`
	NewPresenter User        `json:"new_presenter"`
}

func (m ChangePresenterRequest) GetMessageType() MessageType { return m.MessageType }

func (m ChangePresenterRequest) Validate() error {
	if m.MessageType != MessageTypeChangePresenterRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s",
			MessageTypeChangePresenterRequest, m.MessageType)
	}
	if err := ValidateUserIdentity(m.NewPresenter); err != nil {
		return fmt.Errorf("new_presenter: %w", err)
	}
	return nil
}

// RemoveParticipantRequest is sent by client to remove a participant
type RemoveParticipantRequest struct {
	MessageType MessageType `json:"message_type"`
	RemovedUser User        `json:"removed_user"`
}

func (m RemoveParticipantRequest) GetMessageType() MessageType { return m.MessageType }

func (m RemoveParticipantRequest) Validate() error {
	if m.MessageType != MessageTypeRemoveParticipantRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s",
			MessageTypeRemoveParticipantRequest, m.MessageType)
	}
	if err := ValidateUserIdentity(m.RemovedUser); err != nil {
		return fmt.Errorf("removed_user: %w", err)
	}
	return nil
}

// New Event Message Types (Server→Client, with initiating_user)

// DiagramOperationEvent is broadcast by server when a diagram operation occurs
type DiagramOperationEvent struct {
	MessageType    MessageType        `json:"message_type"`
	InitiatingUser User               `json:"initiating_user"`
	OperationID    string             `json:"operation_id"`
	SequenceNumber *uint64            `json:"sequence_number,omitempty"`
	UpdateVector   int64              `json:"update_vector"` // Server's update vector after operation
	Operation      CellPatchOperation `json:"operation"`
}

func (m DiagramOperationEvent) GetMessageType() MessageType { return m.MessageType }

func (m DiagramOperationEvent) Validate() error {
	if m.MessageType != MessageTypeDiagramOperationEvent {
		return fmt.Errorf("invalid message_type: expected %s, got %s",
			MessageTypeDiagramOperationEvent, m.MessageType)
	}
	if err := ValidateUserIdentity(m.InitiatingUser); err != nil {
		return fmt.Errorf("initiating_user: %w", err)
	}
	if m.OperationID == "" {
		return fmt.Errorf("operation_id is required")
	}
	if _, err := uuid.Parse(m.OperationID); err != nil {
		return fmt.Errorf("operation_id must be a valid UUID: %w", err)
	}
	if err := m.Operation.Validate(); err != nil {
		return fmt.Errorf("operation: %w", err)
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

// PresenterRequestEvent is sent by server to host when a participant requests presenter
type PresenterRequestEvent struct {
	MessageType    MessageType `json:"message_type"`
	RequestingUser User        `json:"requesting_user"`
}

func (m PresenterRequestEvent) GetMessageType() MessageType { return m.MessageType }

func (m PresenterRequestEvent) Validate() error {
	if m.MessageType != MessageTypePresenterRequestEvent {
		return fmt.Errorf("invalid message_type: expected %s, got %s",
			MessageTypePresenterRequestEvent, m.MessageType)
	}
	if err := ValidateUserIdentity(m.RequestingUser); err != nil {
		return fmt.Errorf("requesting_user: %w", err)
	}
	return nil
}

// PresenterDeniedRequest is sent by host to server to deny a presenter request
type PresenterDeniedRequest struct {
	MessageType MessageType `json:"message_type"`
	DeniedUser  User        `json:"denied_user"`
}

func (m PresenterDeniedRequest) GetMessageType() MessageType { return m.MessageType }

func (m PresenterDeniedRequest) Validate() error {
	if m.MessageType != MessageTypePresenterDeniedRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypePresenterDeniedRequest, m.MessageType)
	}
	if err := ValidateUserIdentity(m.DeniedUser); err != nil {
		return fmt.Errorf("denied_user: %w", err)
	}
	return nil
}

// PresenterDeniedEvent is sent by server to the denied user
type PresenterDeniedEvent struct {
	MessageType MessageType `json:"message_type"`
	DeniedUser  User        `json:"denied_user"`
}

func (m PresenterDeniedEvent) GetMessageType() MessageType { return m.MessageType }

func (m PresenterDeniedEvent) Validate() error {
	if m.MessageType != MessageTypePresenterDeniedEvent {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypePresenterDeniedEvent, m.MessageType)
	}
	if err := ValidateUserIdentity(m.DeniedUser); err != nil {
		return fmt.Errorf("denied_user: %w", err)
	}
	return nil
}

type ChangePresenterMessage struct {
	MessageType    MessageType `json:"message_type"`
	InitiatingUser User        `json:"initiating_user"`
	NewPresenter   User        `json:"new_presenter"`
}

func (m ChangePresenterMessage) GetMessageType() MessageType { return m.MessageType }

func (m ChangePresenterMessage) Validate() error {
	if m.MessageType != MessageTypeChangePresenter {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeChangePresenter, m.MessageType)
	}
	if m.InitiatingUser.ProviderId == "" {
		return fmt.Errorf("initiating_user.provider_id is required")
	}
	if m.NewPresenter.ProviderId == "" {
		return fmt.Errorf("new_presenter.provider_id is required")
	}
	return nil
}

type RemoveParticipantMessage struct {
	MessageType MessageType `json:"message_type"`
	RemovedUser User        `json:"removed_user"`
}

func (m RemoveParticipantMessage) GetMessageType() MessageType { return m.MessageType }

func (m RemoveParticipantMessage) Validate() error {
	if m.MessageType != MessageTypeRemoveParticipant {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeRemoveParticipant, m.MessageType)
	}
	if m.RemovedUser.ProviderId == "" {
		return fmt.Errorf("removed_user.provider_id is required")
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

// Sync Protocol Messages

// SyncStatusRequestMessage is sent by client to check server's current update vector
type SyncStatusRequestMessage struct {
	MessageType MessageType `json:"message_type"`
}

func (m SyncStatusRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m SyncStatusRequestMessage) Validate() error {
	if m.MessageType != MessageTypeSyncStatusRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeSyncStatusRequest, m.MessageType)
	}
	return nil
}

// SyncStatusResponseMessage is sent by server with current update vector
type SyncStatusResponseMessage struct {
	MessageType  MessageType `json:"message_type"`
	UpdateVector int64       `json:"update_vector"`
}

func (m SyncStatusResponseMessage) GetMessageType() MessageType { return m.MessageType }

func (m SyncStatusResponseMessage) Validate() error {
	if m.MessageType != MessageTypeSyncStatusResponse {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeSyncStatusResponse, m.MessageType)
	}
	if m.UpdateVector < 0 {
		return fmt.Errorf("update_vector must be non-negative")
	}
	return nil
}

// SyncRequestMessage is sent by client to request full state if stale
type SyncRequestMessage struct {
	MessageType  MessageType `json:"message_type"`
	UpdateVector *int64      `json:"update_vector,omitempty"` // Client's current vector, nil means "send everything"
}

func (m SyncRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m SyncRequestMessage) Validate() error {
	if m.MessageType != MessageTypeSyncRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeSyncRequest, m.MessageType)
	}
	if m.UpdateVector != nil && *m.UpdateVector < 0 {
		return fmt.Errorf("update_vector must be non-negative")
	}
	return nil
}

// DiagramStateMessage is sent by server with full diagram state
type DiagramStateMessage struct {
	MessageType  MessageType             `json:"message_type"`
	DiagramID    string                  `json:"diagram_id"`
	UpdateVector int64                   `json:"update_vector"`
	Cells        []DfdDiagram_Cells_Item `json:"cells"`
}

func (m DiagramStateMessage) GetMessageType() MessageType { return m.MessageType }

func (m DiagramStateMessage) Validate() error {
	if m.MessageType != MessageTypeDiagramState {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeDiagramState, m.MessageType)
	}
	if m.DiagramID == "" {
		return fmt.Errorf("diagram_id is required")
	}
	if _, err := uuid.Parse(m.DiagramID); err != nil {
		return fmt.Errorf("diagram_id must be a valid UUID: %w", err)
	}
	if m.Cells == nil {
		return fmt.Errorf("cells array is required (may be empty for new diagrams)")
	}
	if m.UpdateVector < 0 {
		return fmt.Errorf("update_vector must be non-negative")
	}
	return nil
}

// History Messages

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
	MessageType    MessageType `json:"message_type"`
	InitiatingUser User        `json:"initiating_user"`
}

func (m UndoRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m UndoRequestMessage) Validate() error {
	if m.MessageType != MessageTypeUndoRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeUndoRequest, m.MessageType)
	}
	if m.InitiatingUser.ProviderId == "" {
		return fmt.Errorf("initiating_user.provider_id is required")
	}
	return nil
}

type RedoRequestMessage struct {
	MessageType    MessageType `json:"message_type"`
	InitiatingUser User        `json:"initiating_user"`
}

func (m RedoRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m RedoRequestMessage) Validate() error {
	if m.MessageType != MessageTypeRedoRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeRedoRequest, m.MessageType)
	}
	if m.InitiatingUser.ProviderId == "" {
		return fmt.Errorf("initiating_user.provider_id is required")
	}
	return nil
}

// AsyncParticipant represents a participant in the AsyncAPI format
type AsyncParticipant struct {
	User         User      `json:"user"`
	Permissions  string    `json:"permissions"`
	LastActivity time.Time `json:"last_activity"`
}

// ParticipantsUpdateMessage provides complete participant list with roles
type ParticipantsUpdateMessage struct {
	MessageType      MessageType        `json:"message_type"`
	Participants     []AsyncParticipant `json:"participants"`
	Host             User               `json:"host"`
	CurrentPresenter *User              `json:"current_presenter"`
}

func (m ParticipantsUpdateMessage) GetMessageType() MessageType { return m.MessageType }

func (m ParticipantsUpdateMessage) Validate() error {
	if m.MessageType != MessageTypeParticipantsUpdate {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeParticipantsUpdate, m.MessageType)
	}
	// Validate host (required)
	if err := ValidateUserIdentity(m.Host); err != nil {
		return fmt.Errorf("host: %w", err)
	}
	// Validate current_presenter if present (can be nil when no presenter)
	if m.CurrentPresenter != nil {
		if err := ValidateUserIdentity(*m.CurrentPresenter); err != nil {
			return fmt.Errorf("current_presenter: %w", err)
		}
	}
	// Validate participants
	for i, p := range m.Participants {
		if p.User.ProviderId == "" {
			return fmt.Errorf("participant[%d].user.provider_id is required", i)
		}
		if p.User.DisplayName == "" {
			return fmt.Errorf("participant[%d].user.display_name is required", i)
		}
		if p.User.Email == "" {
			return fmt.Errorf("participant[%d].user.email is required", i)
		}
		if p.Permissions != string(AuthorizationRoleReader) && p.Permissions != string(AuthorizationRoleWriter) {
			return fmt.Errorf("participant[%d].permissions must be 'reader' or 'writer', got '%s'", i, p.Permissions)
		}
		if p.LastActivity.IsZero() {
			return fmt.Errorf("participant[%d].last_activity is required", i)
		}
	}
	return nil
}

// parseAndValidate is a helper that unmarshals data into a message and validates it
func parseAndValidate[T AsyncMessage](data []byte, msgType string) (AsyncMessage, error) {
	var msg T
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse %s message: %w", msgType, err)
	}
	return msg, msg.Validate()
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
		return parseAndValidate[DiagramOperationMessage](data, "diagram operation")
	case MessageTypeDiagramOperationRequest:
		return parseAndValidate[DiagramOperationRequest](data, "diagram operation request")
	case MessageTypeChangePresenterRequest:
		return parseAndValidate[ChangePresenterRequest](data, "change presenter request")
	case MessageTypeRemoveParticipantRequest:
		return parseAndValidate[RemoveParticipantRequest](data, "remove participant request")
	case MessageTypePresenterRequest:
		return parseAndValidate[PresenterRequestMessage](data, "presenter request")
	case MessageTypePresenterRequestEvent:
		return parseAndValidate[PresenterRequestEvent](data, "presenter request event")
	case MessageTypeChangePresenter:
		return parseAndValidate[ChangePresenterMessage](data, "change presenter")
	case MessageTypeRemoveParticipant:
		return parseAndValidate[RemoveParticipantMessage](data, "remove participant")
	case MessageTypePresenterCursor:
		return parseAndValidate[PresenterCursorMessage](data, "presenter cursor")
	case MessageTypePresenterSelection:
		return parseAndValidate[PresenterSelectionMessage](data, "presenter selection")
	case MessageTypeSyncStatusRequest:
		return parseAndValidate[SyncStatusRequestMessage](data, "sync status request")
	case MessageTypeSyncStatusResponse:
		return parseAndValidate[SyncStatusResponseMessage](data, "sync status response")
	case MessageTypeSyncRequest:
		return parseAndValidate[SyncRequestMessage](data, "sync request")
	case MessageTypeDiagramState:
		return parseAndValidate[DiagramStateMessage](data, "diagram state")
	case MessageTypeUndoRequest:
		return parseAndValidate[UndoRequestMessage](data, "undo request")
	case MessageTypeRedoRequest:
		return parseAndValidate[RedoRequestMessage](data, "redo request")
	case MessageTypeParticipantsUpdate:
		return parseAndValidate[ParticipantsUpdateMessage](data, "participants update")
	case MessageTypeError:
		return parseAndValidate[ErrorMessage](data, "error")
	case MessageTypeOperationRejected:
		return parseAndValidate[OperationRejectedMessage](data, "operation_rejected")
	default:
		return nil, fmt.Errorf("unsupported message type: %s", base.MessageType)
	}
}

// ErrorMessage represents an error response
type ErrorMessage struct {
	MessageType MessageType            `json:"message_type"`
	Error       string                 `json:"error"`
	Message     string                 `json:"message"`
	Code        *string                `json:"code,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
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

// OperationRejectedMessage represents a notification sent exclusively to the
// operation originator when their diagram operation is rejected
type OperationRejectedMessage struct {
	MessageType    MessageType `json:"message_type"`
	OperationID    string      `json:"operation_id"`
	SequenceNumber *uint64     `json:"sequence_number,omitempty"` // May be assigned before rejection
	UpdateVector   int64       `json:"update_vector"`             // Current server update vector
	Reason         string      `json:"reason"`                    // Structured reason code
	Message        string      `json:"message"`                   // Human-readable description
	Details        *string     `json:"details,omitempty"`         // Optional technical details
	AffectedCells  []string    `json:"affected_cells,omitempty"`  // Cell IDs affected
	RequiresResync bool        `json:"requires_resync"`           // Whether client should resync
	Timestamp      time.Time   `json:"timestamp"`
}

func (m OperationRejectedMessage) GetMessageType() MessageType { return m.MessageType }

func (m OperationRejectedMessage) Validate() error {
	if m.MessageType != MessageTypeOperationRejected {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeOperationRejected, m.MessageType)
	}
	if m.OperationID == "" {
		return fmt.Errorf("operation_id is required")
	}
	if _, err := uuid.Parse(m.OperationID); err != nil {
		return fmt.Errorf("operation_id must be a valid UUID: %w", err)
	}
	if m.Reason == "" {
		return fmt.Errorf("reason is required")
	}
	// Validate reason code against known values
	validReasons := map[string]bool{
		"validation_failed":      true,
		"conflict_detected":      true,
		"no_state_change":        true,
		"diagram_not_found":      true,
		"permission_denied":      true,
		"invalid_operation_type": true,
		"empty_operation":        true,
	}
	if !validReasons[m.Reason] {
		return fmt.Errorf("invalid reason code: %s", m.Reason)
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
