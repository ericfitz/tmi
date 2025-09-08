package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagramOperationMessage(t *testing.T) {
	cellID := uuid.New().String()
	operationID := uuid.New().String()

	t.Run("Valid Message", func(t *testing.T) {
		msg := DiagramOperationMessage{
			MessageType: MessageTypeDiagramOperation,
			OperationID: operationID,
			Operation: CellPatchOperation{
				Type: "patch",
				Cells: []CellOperation{
					{
						ID:        cellID,
						Operation: "add",
						Data: &Cell{
							Id:    uuid.MustParse(cellID),
							Shape: "process",
						},
					},
				},
			},
		}

		err := msg.Validate()
		assert.NoError(t, err)
		assert.Equal(t, MessageTypeDiagramOperation, msg.GetMessageType())
	})

	t.Run("Invalid Message Type", func(t *testing.T) {
		msg := DiagramOperationMessage{
			MessageType: "invalid",
			OperationID: operationID,
		}

		err := msg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid message_type")
	})

	t.Run("Invalid Operation ID", func(t *testing.T) {
		msg := DiagramOperationMessage{
			MessageType: MessageTypeDiagramOperation,
			OperationID: "invalid-uuid",
		}

		err := msg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "operation_id must be a valid UUID")
	})
}

func TestCellPatchOperation(t *testing.T) {
	cellID := uuid.New().String()

	t.Run("Valid Patch Operation", func(t *testing.T) {
		op := CellPatchOperation{
			Type: "patch",
			Cells: []CellOperation{
				{
					ID:        cellID,
					Operation: "add",
					Data: &Cell{
						Id:    uuid.MustParse(cellID),
						Shape: "process",
					},
				},
			},
		}

		err := op.Validate()
		assert.NoError(t, err)
	})

	t.Run("Invalid Operation Type", func(t *testing.T) {
		op := CellPatchOperation{
			Type:  "invalid",
			Cells: []CellOperation{},
		}

		err := op.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "operation type must be 'patch'")
	})

	t.Run("Empty Cells Array", func(t *testing.T) {
		op := CellPatchOperation{
			Type:  "patch",
			Cells: []CellOperation{},
		}

		err := op.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one cell operation is required")
	})
}

func TestCellOperation(t *testing.T) {
	cellID := uuid.New().String()

	t.Run("Valid Add Operation", func(t *testing.T) {
		op := CellOperation{
			ID:        cellID,
			Operation: "add",
			Data: &Cell{
				Id:    uuid.MustParse(cellID),
				Shape: "process",
			},
		}

		err := op.Validate()
		assert.NoError(t, err)
	})

	t.Run("Valid Remove Operation", func(t *testing.T) {
		op := CellOperation{
			ID:        cellID,
			Operation: "remove",
			Data:      nil,
		}

		err := op.Validate()
		assert.NoError(t, err)
	})

	t.Run("Add Operation Missing Data", func(t *testing.T) {
		op := CellOperation{
			ID:        cellID,
			Operation: "add",
			Data:      nil,
		}

		err := op.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "add operation requires cell data")
	})

	t.Run("Remove Operation With Data", func(t *testing.T) {
		op := CellOperation{
			ID:        cellID,
			Operation: "remove",
			Data: &Cell{
				Id: uuid.MustParse(cellID),
			},
		}

		err := op.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "remove operation should not include cell data")
	})

	t.Run("Mismatched Cell ID", func(t *testing.T) {
		differentID := uuid.New().String()

		op := CellOperation{
			ID:        cellID,
			Operation: "update",
			Data: &Cell{
				Id: uuid.MustParse(differentID),
			},
		}

		err := op.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cell data ID")
		assert.Contains(t, err.Error(), "must match operation ID")
	})
}

func TestPresenterMessages(t *testing.T) {
	t.Run("Presenter Request", func(t *testing.T) {
		msg := PresenterRequestMessage{
			MessageType: MessageTypePresenterRequest,
		}

		err := msg.Validate()
		assert.NoError(t, err)
		assert.Equal(t, MessageTypePresenterRequest, msg.GetMessageType())
	})

	t.Run("Presenter Cursor", func(t *testing.T) {
		msg := PresenterCursorMessage{
			MessageType: MessageTypePresenterCursor,
			CursorPosition: CursorPosition{
				X: 100.5,
				Y: 200.5,
			},
		}

		err := msg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Presenter Selection", func(t *testing.T) {
		cellID1 := uuid.New().String()
		cellID2 := uuid.New().String()

		msg := PresenterSelectionMessage{
			MessageType:   MessageTypePresenterSelection,
			SelectedCells: []string{cellID1, cellID2},
		}

		err := msg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Presenter Selection Invalid UUID", func(t *testing.T) {
		msg := PresenterSelectionMessage{
			MessageType:   MessageTypePresenterSelection,
			SelectedCells: []string{"invalid-uuid"},
		}

		err := msg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be a valid UUID")
	})
}

func TestMessageParser(t *testing.T) {
	t.Run("Parse Diagram Operation", func(t *testing.T) {
		cellID := uuid.New().String()
		operationID := uuid.New().String()

		originalMsg := DiagramOperationMessage{
			MessageType: MessageTypeDiagramOperation,
			OperationID: operationID,
			Operation: CellPatchOperation{
				Type: "patch",
				Cells: []CellOperation{
					{
						ID:        cellID,
						Operation: "add",
						Data: &Cell{
							Id:    uuid.MustParse(cellID),
							Shape: "process",
						},
					},
				},
			},
		}

		// Marshal to JSON
		data, err := json.Marshal(originalMsg)
		require.NoError(t, err)

		// Parse back
		parsedMsg, err := ParseAsyncMessage(data)
		require.NoError(t, err)

		diagMsg, ok := parsedMsg.(DiagramOperationMessage)
		require.True(t, ok)

		assert.Equal(t, originalMsg.MessageType, diagMsg.MessageType)
		assert.Equal(t, originalMsg.OperationID, diagMsg.OperationID)
		assert.Len(t, diagMsg.Operation.Cells, 1)
	})

	t.Run("Parse Presenter Request", func(t *testing.T) {
		originalMsg := PresenterRequestMessage{
			MessageType: MessageTypePresenterRequest,
		}

		// Marshal to JSON
		data, err := json.Marshal(originalMsg)
		require.NoError(t, err)

		// Parse back
		parsedMsg, err := ParseAsyncMessage(data)
		require.NoError(t, err)

		presMsg, ok := parsedMsg.(PresenterRequestMessage)
		require.True(t, ok)

		assert.Equal(t, originalMsg.MessageType, presMsg.MessageType)
	})

	t.Run("Parse Invalid JSON", func(t *testing.T) {
		data := []byte(`{"invalid": "json"`)

		_, err := ParseAsyncMessage(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse base message")
	})

	t.Run("Parse Unsupported Message Type", func(t *testing.T) {
		data := []byte(`{"message_type": "unsupported_type"}`)

		_, err := ParseAsyncMessage(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported message type")
	})

	t.Run("Parse Invalid Message Content", func(t *testing.T) {
		data := []byte(`{"message_type": "diagram_operation", "operation_id": "invalid-uuid"}`)

		_, err := ParseAsyncMessage(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "operation_id must be a valid UUID")
	})
}

func TestMarshalAsyncMessage(t *testing.T) {
	t.Run("Marshal Valid Message", func(t *testing.T) {
		msg := PresenterRequestMessage{
			MessageType: MessageTypePresenterRequest,
		}

		data, err := MarshalAsyncMessage(msg)
		assert.NoError(t, err)

		// Verify it can be parsed back
		var parsed map[string]interface{}
		err = json.Unmarshal(data, &parsed)
		assert.NoError(t, err)
		assert.Equal(t, string(MessageTypePresenterRequest), parsed["message_type"])
	})

	t.Run("Marshal Invalid Message Type", func(t *testing.T) {
		msg := PresenterRequestMessage{
			MessageType: "invalid_type",
		}

		_, err := MarshalAsyncMessage(msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "message validation failed")
	})
}

func TestAuthorizationDeniedMessage(t *testing.T) {
	operationID := uuid.New().String()

	t.Run("Valid Message", func(t *testing.T) {
		msg := AuthorizationDeniedMessage{
			MessageType:         MessageTypeAuthorizationDenied,
			OriginalOperationID: operationID,
			Reason:              "insufficient_permissions",
		}

		err := msg.Validate()
		assert.NoError(t, err)
	})

	t.Run("Invalid Operation ID", func(t *testing.T) {
		msg := AuthorizationDeniedMessage{
			MessageType:         MessageTypeAuthorizationDenied,
			OriginalOperationID: "invalid-uuid",
			Reason:              "insufficient_permissions",
		}

		err := msg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be a valid UUID")
	})
}
