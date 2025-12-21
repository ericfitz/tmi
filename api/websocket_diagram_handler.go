package api

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// DiagramOperationRequestHandler handles diagram operation request messages
type DiagramOperationRequestHandler struct{}

// MessageType returns the message type this handler processes
func (h *DiagramOperationRequestHandler) MessageType() string {
	return "diagram_operation_request"
}

// HandleMessage processes diagram operation request messages
func (h *DiagramOperationRequestHandler) HandleMessage(session *DiagramSession, client *WebSocketClient, message []byte) error {
	defer func() {
		if r := recover(); r != nil {
			slogging.Get().Error("PANIC in DiagramOperationRequestHandler - Session: %s, User: %s, Error: %v, Stack: %s",
				session.ID, client.UserID, r, debug.Stack())
		}
	}()

	startTime := time.Now()
	slogging.Get().Debug("[TRACE-BROADCAST] DiagramOperationRequestHandler.HandleMessage ENTRY - Session: %s, User: %s, Client pointer: %p, Message size: %d bytes",
		session.ID, client.UserID, client, len(message))

	var req DiagramOperationRequest
	if err := json.Unmarshal(message, &req); err != nil {
		slogging.Get().Error("Failed to parse diagram operation request - Session: %s, User: %s, Error: %v",
			session.ID, client.UserID, err)
		return err
	}

	slogging.Get().Debug("[TRACE-BROADCAST] Parsed diagram operation request - Session: %s, User: %s, OperationID: %s, OperationType: %s, CellCount: %d",
		session.ID, client.UserID, req.OperationID, req.Operation.Type, len(req.Operation.Cells))

	// Log details of each cell operation in the message
	for i, cellOp := range req.Operation.Cells {
		slogging.Get().Debug("[TRACE-BROADCAST] Cell operation %d - CellID: %s, Operation: '%s', HasData: %v",
			i, cellOp.ID, cellOp.Operation, cellOp.Data != nil)
	}

	// Use authenticated client context for initiating_user (client cannot spoof identity)

	// Assign sequence number for operation tracking
	session.mu.Lock()
	sequenceNumber := session.NextSequenceNumber
	session.NextSequenceNumber++
	session.mu.Unlock()

	req.SequenceNumber = &sequenceNumber

	slogging.Get().Debug("Assigned sequence number - Session: %s, User: %s, OperationID: %s, SequenceNumber: %d",
		session.ID, client.UserID, req.OperationID, sequenceNumber)

	// Get current diagram state from the session cache if available, otherwise from database
	// This prevents race conditions where concurrent operations read stale state from DB
	session.mu.Lock()
	var diagram DfdDiagram
	var currentState map[string]*DfdDiagram_Cells_Item
	var err error

	// Check if we have cached state in operation history
	if session.OperationHistory != nil && len(session.OperationHistory.CurrentState) > 0 {
		// Use cached state from operation history
		currentState = make(map[string]*DfdDiagram_Cells_Item)
		for k, v := range session.OperationHistory.CurrentState {
			cellCopy := *v
			currentState[k] = &cellCopy
		}

		// Reconstruct diagram from current state
		diagram, err = DiagramStore.Get(session.DiagramID)
		if err != nil {
			session.mu.Unlock()
			slogging.Get().Error("Failed to get diagram before operation validation - Session: %s, User: %s, OperationID: %s, Error: %v",
				session.ID, client.UserID, req.OperationID, err)
			session.sendOperationRejected(client, req.OperationID, req.SequenceNumber, "diagram_not_found",
				"Target diagram could not be retrieved", nil, nil, true)
			return err
		}

		// Replace diagram cells with current state from history
		diagram.Cells = make([]DfdDiagram_Cells_Item, 0, len(currentState))
		for _, cellItem := range currentState {
			diagram.Cells = append(diagram.Cells, *cellItem)
		}

		slogging.Get().Debug("Using cached diagram state from operation history - Session: %s, CellCount: %d",
			session.ID, len(currentState))
	} else {
		// First operation in session - load from database
		diagram, err = DiagramStore.Get(session.DiagramID)
		if err != nil {
			session.mu.Unlock()
			slogging.Get().Error("Failed to get diagram before operation validation - Session: %s, User: %s, OperationID: %s, Error: %v",
				session.ID, client.UserID, req.OperationID, err)
			session.sendOperationRejected(client, req.OperationID, req.SequenceNumber, "diagram_not_found",
				"Target diagram could not be retrieved", nil, nil, true)
			return err
		}

		// Build current state map for detailed rejection feedback
		currentState = make(map[string]*DfdDiagram_Cells_Item)
		for i := range diagram.Cells {
			cellItem := &diagram.Cells[i]
			var itemID string
			if node, err := cellItem.AsNode(); err == nil {
				itemID = node.Id.String()
			} else if edge, err := cellItem.AsEdge(); err == nil {
				itemID = edge.Id.String()
			}
			if itemID != "" {
				currentState[itemID] = cellItem
			}
		}

		slogging.Get().Debug("Loaded diagram state from database - Session: %s, CellCount: %d",
			session.ID, len(currentState))
	}
	session.mu.Unlock()

	// Process and validate cell operations to get detailed rejection reason
	validationResult := session.processAndValidateCellOperations(&diagram, currentState, req.Operation)

	session.mu.RLock()
	totalClients := len(session.Clients)
	session.mu.RUnlock()

	// Determine if operation should be applied
	applied := validationResult.Valid && validationResult.StateChanged

	slogging.Get().Info("[TRACE-BROADCAST] Diagram operation validation result - Session: %s, User: %s, OperationID: %s, Valid: %v, StateChanged: %v, Applied: %v, Total clients: %d",
		session.ID, client.UserID, req.OperationID, validationResult.Valid, validationResult.StateChanged, applied, totalClients)

	if applied {
		// Save the modified diagram to the database
		if err := DiagramStore.Update(session.DiagramID, diagram); err != nil {
			slogging.Get().Error("Failed to save diagram after operation - Session: %s, OperationID: %s, Error: %v",
				session.ID, req.OperationID, err)
			session.sendOperationRejected(client, req.OperationID, req.SequenceNumber, "save_failed",
				"Failed to persist diagram changes", nil, nil, true)
			return fmt.Errorf("failed to save diagram: %w", err)
		}

		// Rebuild current state map after validation (diagram was modified in place)
		newCurrentState := make(map[string]*DfdDiagram_Cells_Item)
		for i := range diagram.Cells {
			cellItem := &diagram.Cells[i]
			var itemID string
			if node, err := cellItem.AsNode(); err == nil {
				itemID = node.Id.String()
			} else if edge, err := cellItem.AsEdge(); err == nil {
				itemID = edge.Id.String()
			}
			if itemID != "" {
				newCurrentState[itemID] = cellItem
			}
		}

		// Create DiagramOperationEvent for broadcasting (includes initiating_user from auth context)
		event := DiagramOperationEvent{
			MessageType:    MessageTypeDiagramOperationEvent,
			InitiatingUser: client.toUser(),
			OperationID:    req.OperationID,
			SequenceNumber: req.SequenceNumber,
			Operation:      req.Operation,
		}

		// Update operation history with new state
		session.addToHistory(event, client.UserID, validationResult.PreviousState, newCurrentState)

		// Broadcast the operation event to all other clients
		slogging.Get().Info("[TRACE-BROADCAST] *** CALLING broadcastToOthers *** - Session: %s, Sender: %s (%p), OperationID: %s, Total clients: %d, Expected recipients: %d",
			session.ID, client.UserID, client, req.OperationID, totalClients, totalClients-1)
		session.broadcastToOthers(client, event)
		slogging.Get().Info("[TRACE-BROADCAST] *** RETURNED from broadcastToOthers *** - Session: %s, OperationID: %s",
			session.ID, req.OperationID)
	} else {
		// Send rejection notification to originator
		var rejectionReason, rejectionMessage string
		var detailsPtr *string
		requiresResync := false

		if !validationResult.Valid {
			rejectionReason = validationResult.Reason
			rejectionMessage = fmt.Sprintf("Operation validation failed: %s", validationResult.Reason)

			// Add more specific messages based on reason
			switch validationResult.Reason {
			case "conflict_detected":
				rejectionMessage = fmt.Sprintf("Operation conflicts with current diagram state for cells: %v", validationResult.CellsModified)
				requiresResync = true
			case "invalid_operation_type":
				details := fmt.Sprintf("Operation type must be 'patch', got: %s", req.Operation.Type)
				detailsPtr = &details
				rejectionMessage = "Invalid operation type"
			case "empty_operation":
				rejectionMessage = "Operation contains no cell operations"
			case "validation_failed":
				if len(validationResult.CellsModified) > 0 {
					rejectionMessage = fmt.Sprintf("Cell validation failed for: %v", validationResult.CellsModified)
				}
			}

			if validationResult.ConflictDetected {
				requiresResync = true
			}
		} else if !validationResult.StateChanged {
			rejectionReason = "no_state_change"
			rejectionMessage = "Operation resulted in no state changes (idempotent or no-op)"
			requiresResync = false
		}

		slogging.Get().Warn("Diagram operation REJECTED - Session: %s, User: %s, OperationID: %s, Reason: %s, RequiresResync: %v, AffectedCells: %v",
			session.ID, client.UserID, req.OperationID, rejectionReason, requiresResync, validationResult.CellsModified)

		session.sendOperationRejected(client, req.OperationID, req.SequenceNumber, rejectionReason,
			rejectionMessage, detailsPtr, validationResult.CellsModified, requiresResync)

		// Still send state correction if needed (for conflicts)
		if validationResult.CorrectionNeeded {
			slogging.Get().Info("Sending additional state correction to %s for cells: %v", client.UserID, validationResult.CellsModified)
			session.sendStateCorrection(client, validationResult.CellsModified)
		}
	}

	processingTime := time.Since(startTime)
	slogging.Get().Debug("Completed diagram operation processing - Session: %s, User: %s, Duration: %v",
		session.ID, client.UserID, processingTime)

	return nil
}
