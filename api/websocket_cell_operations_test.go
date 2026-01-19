package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCellOperationProcessor_IdempotentAdd tests that duplicate add operations
// are treated as updates (idempotent behavior)
func TestCellOperationProcessor_IdempotentAdd(t *testing.T) {
	processor := &CellOperationProcessor{}

	// Create initial diagram with one node
	initialNodeID := uuid.New().String()
	initialNode, err := CreateNode(initialNodeID, NodeShapeProcess, 100, 150, 120, 80)
	require.NoError(t, err, "Should create initial node")

	diagramID := NewUUID()
	diagram := &DfdDiagram{
		Id:    &diagramID,
		Cells: []DfdDiagram_Cells_Item{initialNode},
	}

	// Build current state map
	currentState := make(map[string]*DfdDiagram_Cells_Item)
	for i := range diagram.Cells {
		cellItem := &diagram.Cells[i]
		var itemID string
		if node, err := cellItem.AsNode(); err == nil {
			itemID = node.Id.String()
		} else if edge, err := cellItem.AsEdge(); err == nil {
			itemID = edge.Id.String()
		}
		currentState[itemID] = cellItem
	}

	t.Run("IdempotentAdd_NodeUpdate", func(t *testing.T) {
		// Attempt to "add" the same node again with different position (should act as update)
		updatedNode, err := CreateNode(initialNodeID, NodeShapeProcess, 200, 250, 120, 80)
		require.NoError(t, err, "Should create updated node")

		addOp := CellOperation{
			ID:        initialNodeID,
			Operation: "add",
			Data:      &updatedNode,
		}

		// Validate the add operation - should succeed (idempotent)
		result := processor.validateAddOperation(diagram, currentState, addOp)

		// Verify operation succeeded
		assert.True(t, result.Valid, "Idempotent add should succeed")
		assert.True(t, result.StateChanged, "State should be changed")
		assert.False(t, result.ConflictDetected, "No conflict should be detected")
		assert.Empty(t, result.Reason, "No error reason")

		// Verify the node was updated, not duplicated
		assert.Len(t, diagram.Cells, 1, "Should still have only 1 cell")

		// Verify the position was updated
		updatedCellItem := &diagram.Cells[0]
		node, err := updatedCellItem.AsNode()
		require.NoError(t, err, "Cell should be a node")
		require.NotNil(t, node.Position, "Position should be set")
		assert.Equal(t, float32(200), node.Position.X, "X position should be updated")
		assert.Equal(t, float32(250), node.Position.Y, "Y position should be updated")
	})

	t.Run("IdempotentAdd_EdgeUpdate", func(t *testing.T) {
		// Create diagram with nodes and edge
		sourceNodeID := uuid.New().String()
		targetNodeID := uuid.New().String()
		edgeID := uuid.New().String()

		sourceNode, err := CreateNode(sourceNodeID, NodeShapeActor, 100, 100, 120, 60)
		require.NoError(t, err)

		targetNode, err := CreateNode(targetNodeID, NodeShapeProcess, 300, 100, 140, 60)
		require.NoError(t, err)

		// Create initial edge
		initialEdge, err := CreateEdge(edgeID, EdgeShapeFlow, sourceNodeID, targetNodeID)
		require.NoError(t, err)

		edgeDiagramID := NewUUID()
		edgeDiagram := &DfdDiagram{
			Id:    &edgeDiagramID,
			Cells: []DfdDiagram_Cells_Item{sourceNode, targetNode, initialEdge},
		}

		// Build current state
		edgeState := make(map[string]*DfdDiagram_Cells_Item)
		for i := range edgeDiagram.Cells {
			cellItem := &edgeDiagram.Cells[i]
			var itemID string
			if node, err := cellItem.AsNode(); err == nil {
				itemID = node.Id.String()
			} else if edge, err := cellItem.AsEdge(); err == nil {
				itemID = edge.Id.String()
			}
			edgeState[itemID] = cellItem
		}

		// Now "add" the same edge again (simulating client duplicate add)
		updatedEdge, err := CreateEdge(edgeID, EdgeShapeFlow, sourceNodeID, targetNodeID)
		require.NoError(t, err)

		addOp := CellOperation{
			ID:        edgeID,
			Operation: "add",
			Data:      &updatedEdge,
		}

		// Validate the add operation - should succeed (idempotent)
		result := processor.validateAddOperation(edgeDiagram, edgeState, addOp)

		// Verify operation succeeded
		assert.True(t, result.Valid, "Idempotent add should succeed for edge")
		assert.True(t, result.StateChanged, "State should be changed")
		assert.False(t, result.ConflictDetected, "No conflict should be detected")
		assert.Empty(t, result.Reason, "No error reason")

		// Verify the edge was updated, not duplicated
		assert.Len(t, edgeDiagram.Cells, 3, "Should still have 3 cells (2 nodes + 1 edge)")

		// Verify the edge exists
		var foundEdge *DfdDiagram_Cells_Item
		for i := range edgeDiagram.Cells {
			cellItem := &edgeDiagram.Cells[i]
			if edge, err := cellItem.AsEdge(); err == nil {
				if edge.Id.String() == edgeID {
					foundEdge = cellItem
					break
				}
			}
		}

		require.NotNil(t, foundEdge, "Should find the edge")
		edge, err := foundEdge.AsEdge()
		require.NoError(t, err)
		assert.Equal(t, edgeID, edge.Id.String(), "Edge ID should match")
	})

	t.Run("NormalAdd_NewCell", func(t *testing.T) {
		// Test that adding a truly new cell still works normally
		newNodeID := uuid.New().String()
		newNode, err := CreateNode(newNodeID, NodeShapeStore, 300, 300, 150, 90)
		require.NoError(t, err)

		addOp := CellOperation{
			ID:        newNodeID,
			Operation: "add",
			Data:      &newNode,
		}

		initialCellCount := len(diagram.Cells)

		// Rebuild current state for this test
		currentState := make(map[string]*DfdDiagram_Cells_Item)
		for i := range diagram.Cells {
			cellItem := &diagram.Cells[i]
			var itemID string
			if node, err := cellItem.AsNode(); err == nil {
				itemID = node.Id.String()
			} else if edge, err := cellItem.AsEdge(); err == nil {
				itemID = edge.Id.String()
			}
			currentState[itemID] = cellItem
		}

		// Validate the add operation
		result := processor.validateAddOperation(diagram, currentState, addOp)

		// Verify operation succeeded
		assert.True(t, result.Valid, "Normal add should succeed")
		assert.True(t, result.StateChanged, "State should be changed")
		assert.False(t, result.ConflictDetected, "No conflict")
		assert.Empty(t, result.Reason, "No error reason")

		// Verify the cell was added
		assert.Len(t, diagram.Cells, initialCellCount+1, "Should have one more cell")
	})

	t.Run("Add_RequiresData", func(t *testing.T) {
		// Test that add operation without data fails
		addOp := CellOperation{
			ID:        uuid.New().String(),
			Operation: "add",
			Data:      nil, // No data
		}

		result := processor.validateAddOperation(diagram, currentState, addOp)

		// Verify operation failed
		assert.False(t, result.Valid, "Add without data should fail")
		assert.Equal(t, "add_requires_cell_data", result.Reason)
	})
}
