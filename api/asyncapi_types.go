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
	hasUserId := u.ProviderId != ""
	hasEmail := u.Email != ""

	if !hasUserId && !hasEmail {
		return fmt.Errorf("user must have either user_id or email")
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
	MessageTypeDiagramStateSync    MessageType = "diagram_state_sync"
	MessageTypeResyncRequest       MessageType = "resync_request"
	MessageTypeResyncResponse      MessageType = "resync_response"
	MessageTypeHistoryOperation    MessageType = "history_operation"
	MessageTypeUndoRequest         MessageType = "undo_request"
	MessageTypeRedoRequest         MessageType = "redo_request"

	// New Request/Event pattern message types (Client→Server requests, Server→Client events)
	MessageTypeDiagramOperationRequest  MessageType = "diagram_operation_request"
	MessageTypeDiagramOperationEvent    MessageType = "diagram_operation_event"
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
	case "add", "update":
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
	case "remove":
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
	SequenceNumber *uint64            `json:"sequence_number,omitempty"`
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

type PresenterDeniedMessage struct {
	MessageType      MessageType `json:"message_type"`
	CurrentPresenter User        `json:"current_presenter"`
}

func (m PresenterDeniedMessage) GetMessageType() MessageType { return m.MessageType }

func (m PresenterDeniedMessage) Validate() error {
	if m.MessageType != MessageTypePresenterDenied {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypePresenterDenied, m.MessageType)
	}
	if m.CurrentPresenter.ProviderId == "" {
		return fmt.Errorf("current_presenter.user_id is required")
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
		return fmt.Errorf("initiating_user.user_id is required")
	}
	if m.NewPresenter.ProviderId == "" {
		return fmt.Errorf("new_presenter.user_id is required")
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
		return fmt.Errorf("removed_user.user_id is required")
	}
	return nil
}

type CurrentPresenterMessage struct {
	MessageType      MessageType `json:"message_type"`
	InitiatingUser   User        `json:"initiating_user"`
	CurrentPresenter User        `json:"current_presenter"`
}

func (m CurrentPresenterMessage) GetMessageType() MessageType { return m.MessageType }

func (m CurrentPresenterMessage) Validate() error {
	if m.MessageType != MessageTypeCurrentPresenter {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeCurrentPresenter, m.MessageType)
	}
	if err := ValidateUserIdentity(m.InitiatingUser); err != nil {
		return fmt.Errorf("initiating_user: %w", err)
	}
	if err := ValidateUserIdentity(m.CurrentPresenter); err != nil {
		return fmt.Errorf("current_presenter: %w", err)
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
	MessageType  MessageType `json:"message_type"`
	UpdateVector *int64      `json:"update_vector"`
}

func (m StateCorrectionMessage) GetMessageType() MessageType { return m.MessageType }

func (m StateCorrectionMessage) Validate() error {
	if m.MessageType != MessageTypeStateCorrection {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeStateCorrection, m.MessageType)
	}
	if m.UpdateVector == nil {
		return fmt.Errorf("update_vector is required for state correction")
	}
	if *m.UpdateVector < 0 {
		return fmt.Errorf("update_vector must be non-negative")
	}
	return nil
}

// DiagramStateSyncMessage is sent to clients immediately upon connection to synchronize state
type DiagramStateSyncMessage struct {
	MessageType  MessageType             `json:"message_type"`
	DiagramID    string                  `json:"diagram_id"`
	UpdateVector *int64                  `json:"update_vector"`
	Cells        []DfdDiagram_Cells_Item `json:"cells"`
}

func (m DiagramStateSyncMessage) GetMessageType() MessageType { return m.MessageType }

func (m DiagramStateSyncMessage) Validate() error {
	if m.MessageType != MessageTypeDiagramStateSync {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeDiagramStateSync, m.MessageType)
	}
	if m.DiagramID == "" {
		return fmt.Errorf("diagram_id is required for diagram state sync")
	}
	if _, err := uuid.Parse(m.DiagramID); err != nil {
		return fmt.Errorf("diagram_id must be a valid UUID: %w", err)
	}
	if m.Cells == nil {
		return fmt.Errorf("cells array is required (may be empty for new diagrams)")
	}
	if m.UpdateVector != nil && *m.UpdateVector < 0 {
		return fmt.Errorf("update_vector must be non-negative")
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
	Method        string      `json:"method"`
	DiagramID     string      `json:"diagram_id"`
	ThreatModelID string      `json:"threat_model_id,omitempty"`
}

func (m ResyncResponseMessage) GetMessageType() MessageType { return m.MessageType }

func (m ResyncResponseMessage) Validate() error {
	if m.MessageType != MessageTypeResyncResponse {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeResyncResponse, m.MessageType)
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
	MessageType    MessageType `json:"message_type"`
	InitiatingUser User        `json:"initiating_user"`
}

func (m UndoRequestMessage) GetMessageType() MessageType { return m.MessageType }

func (m UndoRequestMessage) Validate() error {
	if m.MessageType != MessageTypeUndoRequest {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeUndoRequest, m.MessageType)
	}
	if m.InitiatingUser.ProviderId == "" {
		return fmt.Errorf("initiating_user.user_id is required")
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
		return fmt.Errorf("initiating_user.user_id is required")
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
	InitiatingUser   *User              `json:"initiating_user,omitempty"`
	Participants     []AsyncParticipant `json:"participants"`
	Host             string             `json:"host"`
	CurrentPresenter string             `json:"current_presenter"`
}

func (m ParticipantsUpdateMessage) GetMessageType() MessageType { return m.MessageType }

func (m ParticipantsUpdateMessage) Validate() error {
	if m.MessageType != MessageTypeParticipantsUpdate {
		return fmt.Errorf("invalid message_type: expected %s, got %s", MessageTypeParticipantsUpdate, m.MessageType)
	}
	// Validate initiating_user if present (optional for system events)
	if m.InitiatingUser != nil {
		if err := ValidateUserIdentity(*m.InitiatingUser); err != nil {
			return fmt.Errorf("initiating_user: %w", err)
		}
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
	case MessageTypeChangePresenter:
		return parseAndValidate[ChangePresenterMessage](data, "change presenter")
	case MessageTypeRemoveParticipant:
		return parseAndValidate[RemoveParticipantMessage](data, "remove participant")
	case MessageTypePresenterCursor:
		return parseAndValidate[PresenterCursorMessage](data, "presenter cursor")
	case MessageTypePresenterSelection:
		return parseAndValidate[PresenterSelectionMessage](data, "presenter selection")
	case MessageTypeResyncRequest:
		return parseAndValidate[ResyncRequestMessage](data, "resync request")
	case MessageTypeResyncResponse:
		return parseAndValidate[ResyncResponseMessage](data, "resync response")
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
