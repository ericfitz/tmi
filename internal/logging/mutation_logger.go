package logging

import (
	"encoding/json"
)

// MutationLogger provides detailed logging for mutation operations
type MutationLogger struct {
	logger *Logger
}

// NewMutationLogger creates a new mutation logger
func NewMutationLogger(logger *Logger) *MutationLogger {
	return &MutationLogger{logger: logger}
}

// MutationResult represents the result of a mutation operation
type MutationResult struct {
	Success          bool
	StateChanged     bool
	ConflictDetected bool
	Error            error
	CellsModified    []string
	SequenceNumber   uint64
}

// MutationContext provides context for mutation operations
type MutationContext struct {
	UserID        string
	UserRole      string
	SessionID     string
	DiagramID     string
	ThreatModelID string
	CausedBy      string // "websocket", "undo", "redo", "conflict_resolution"
}

// OperationDetails contains the specific operation information
type OperationDetails struct {
	OperationID   string
	OperationType string
	AffectedCells []string
	OperationData interface{} // The actual operation data
}

// StateChange represents changes made to diagram state
type StateChange struct {
	Added    []string
	Removed  []string
	Modified []string
}

// LogMutationAttempt logs when a mutation operation is attempted
func (ml *MutationLogger) LogMutationAttempt(ctx MutationContext, op OperationDetails) {
	// Convert affected cells to JSON for structured logging
	cellsJSON, _ := json.Marshal(op.AffectedCells)

	ml.logger.Info("Mutation operation attempt - user_id=%s operation_id=%s operation_type=%s affected_cells=%s session_id=%s diagram_id=%s threat_model_id=%s caused_by=%s",
		ctx.UserID,
		op.OperationID,
		op.OperationType,
		string(cellsJSON),
		ctx.SessionID,
		ctx.DiagramID,
		ctx.ThreatModelID,
		ctx.CausedBy,
	)
}

// LogMutationResult logs the result of a mutation operation
func (ml *MutationLogger) LogMutationResult(operationID string, result MutationResult) {
	// Convert cells modified to JSON for structured logging
	cellsJSON, _ := json.Marshal(result.CellsModified)

	// Create error string
	errorStr := ""
	if result.Error != nil {
		errorStr = result.Error.Error()
	}

	ml.logger.Info("Mutation operation result - operation_id=%s success=%t state_changed=%t conflict_detected=%t error=%s cells_modified=%s sequence_number=%d",
		operationID,
		result.Success,
		result.StateChanged,
		result.ConflictDetected,
		errorStr,
		string(cellsJSON),
		result.SequenceNumber,
	)
}

// LogStateChange logs detailed state changes applied
func (ml *MutationLogger) LogStateChange(operationID string, change StateChange) {
	addedJSON, _ := json.Marshal(change.Added)
	removedJSON, _ := json.Marshal(change.Removed)
	modifiedJSON, _ := json.Marshal(change.Modified)

	ml.logger.Info("State changes applied - operation_id=%s cells_added=%s cells_removed=%s cells_modified=%s",
		operationID,
		string(addedJSON),
		string(removedJSON),
		string(modifiedJSON),
	)
}

// LogValidationFailure logs when operation validation fails
func (ml *MutationLogger) LogValidationFailure(ctx MutationContext, op OperationDetails, reason string) {
	ml.logger.Warn("Mutation validation failed - user_id=%s operation_id=%s operation_type=%s reason=%s session_id=%s diagram_id=%s",
		ctx.UserID,
		op.OperationID,
		op.OperationType,
		reason,
		ctx.SessionID,
		ctx.DiagramID,
	)
}

// LogAuthorizationFailure logs when user lacks permissions for mutation
func (ml *MutationLogger) LogAuthorizationFailure(ctx MutationContext, op OperationDetails, reason string) {
	ml.logger.Warn("Mutation authorization failed - user_id=%s user_role=%s operation_id=%s operation_type=%s reason=%s session_id=%s diagram_id=%s threat_model_id=%s",
		ctx.UserID,
		ctx.UserRole,
		op.OperationID,
		op.OperationType,
		reason,
		ctx.SessionID,
		ctx.DiagramID,
		ctx.ThreatModelID,
	)
}

// LogConflictDetected logs when operation conflicts with current state
func (ml *MutationLogger) LogConflictDetected(ctx MutationContext, op OperationDetails, conflictType string, details string) {
	ml.logger.Info("Mutation conflict detected - user_id=%s operation_id=%s operation_type=%s conflict_type=%s details=%s session_id=%s diagram_id=%s",
		ctx.UserID,
		op.OperationID,
		op.OperationType,
		conflictType,
		details,
		ctx.SessionID,
		ctx.DiagramID,
	)
}

// GetMutationLogger returns a global mutation logger instance
func GetMutationLogger() *MutationLogger {
	return NewMutationLogger(Get())
}
