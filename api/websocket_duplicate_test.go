package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestDuplicateCellOperationFiltering tests that duplicate cell operations within a single message are filtered out
func TestDuplicateCellOperationFiltering(t *testing.T) {
	// Create a test diagram with initial state
	diagramID := uuid.New()
	testDiagram := &DfdDiagram{
		Id:    &diagramID,
		Name:  "Test Diagram",
		Type:  "DFD-1.0.0",
		Cells: []DfdDiagram_Cells_Item{},
	}

	// Initialize test fixtures and stores
	InitTestFixtures()

	// Create a WebSocketHub for the test
	hub := NewWebSocketHubForTests()

	// NodeShapeStore the test diagram
	_, err := DiagramStore.Create(*testDiagram, func(d DfdDiagram, _ string) DfdDiagram {
		d.Id = &diagramID
		return d
	})
	if err != nil {
		t.Fatalf("Failed to create test diagram: %v", err)
	}

	// Create a DiagramSession
	session := &DiagramSession{
		ID:                 uuid.New().String(),
		DiagramID:          testDiagram.Id.String(),
		ThreatModelID:      uuid.New().String(),
		Clients:            make(map[*WebSocketClient]bool),
		NextSequenceNumber: 1,
		clientLastSequence: make(map[string]uint64),
		Hub:                hub,
	}

	// Build current state map (empty for this test)
	currentState := make(map[string]*DfdDiagram_Cells_Item)

	t.Run("SingleCellOperation", func(t *testing.T) {
		cellID := uuid.New().String()
		singleNode, _ := CreateNode(cellID, NodeShapeProcess, 100, 150, 120, 80)
		operation := CellPatchOperation{
			Type: "patch",
			Cells: []CellOperation{
				{
					ID:        cellID,
					Operation: "add",
					Data:      &singleNode,
				},
			},
		}

		result := session.processAndValidateCellOperations(testDiagram, currentState, operation)
		assert.True(t, result.Valid, "Single cell operation should be valid")
		assert.True(t, result.StateChanged, "Single add operation should change state")
		assert.Len(t, result.CellsModified, 1, "Should have one modified cell")
		assert.Equal(t, cellID, result.CellsModified[0], "Modified cell ID should match")
	})

	t.Run("DuplicateCellOperations", func(t *testing.T) {
		// Reset diagram to empty state
		testDiagram.Cells = []DfdDiagram_Cells_Item{}
		currentState = make(map[string]*DfdDiagram_Cells_Item)

		cellID := uuid.New().String()
		dupNode, _ := CreateNode(cellID, NodeShapeProcess, 100, 150, 120, 80)

		// Create 8 identical cell operations (simulating the bug reported)
		duplicateCells := make([]CellOperation, 8)
		for i := 0; i < 8; i++ {
			duplicateCells[i] = CellOperation{
				ID:        cellID,
				Operation: "add",
				Data:      &dupNode,
			}
		}

		operation := CellPatchOperation{
			Type:  "patch",
			Cells: duplicateCells,
		}

		result := session.processAndValidateCellOperations(testDiagram, currentState, operation)

		// The operation should be valid because duplicates are filtered out
		assert.True(t, result.Valid, "Duplicate cell operations should be filtered and result should be valid")
		assert.True(t, result.StateChanged, "Filtered operation should still change state")
		assert.Len(t, result.CellsModified, 1, "Should have only one modified cell after deduplication")
		assert.Equal(t, cellID, result.CellsModified[0], "Modified cell ID should match")

		// Verify the cell was actually added to the diagram
		assert.Len(t, testDiagram.Cells, 1, "Should have exactly one cell in the diagram after deduplication")
	})

	t.Run("MixedDuplicateAndUniqueCells", func(t *testing.T) {
		// Reset diagram to empty state
		testDiagram.Cells = []DfdDiagram_Cells_Item{}
		currentState = make(map[string]*DfdDiagram_Cells_Item)

		cellID1 := uuid.New().String()
		cellID2 := uuid.New().String()
		mixedNode1, _ := CreateNode(cellID1, NodeShapeProcess, 100, 150, 80, 40)
		mixedNode2, _ := CreateNode(cellID2, NodeShapeStore, 300, 150, 80, 40)

		operation := CellPatchOperation{
			Type: "patch",
			Cells: []CellOperation{
				// First unique cell
				{
					ID:        cellID1,
					Operation: "add",
					Data:      &mixedNode1,
				},
				// Duplicate of first cell (should be filtered)
				{
					ID:        cellID1,
					Operation: "add",
					Data:      &mixedNode1,
				},
				// Second unique cell
				{
					ID:        cellID2,
					Operation: "add",
					Data:      &mixedNode2,
				},
				// Another duplicate of first cell (should be filtered)
				{
					ID:        cellID1,
					Operation: "add",
					Data:      &mixedNode1,
				},
			},
		}

		result := session.processAndValidateCellOperations(testDiagram, currentState, operation)

		assert.True(t, result.Valid, "Mixed duplicate and unique operations should be valid after filtering")
		assert.True(t, result.StateChanged, "Mixed operation should change state")
		assert.Len(t, result.CellsModified, 2, "Should have two modified cells after deduplication")

		// Verify both unique cells were added
		assert.Len(t, testDiagram.Cells, 2, "Should have exactly two cells in the diagram after deduplication")

		// Verify the correct cell IDs were processed
		assert.Contains(t, result.CellsModified, cellID1, "First cell ID should be in modified list")
		assert.Contains(t, result.CellsModified, cellID2, "Second cell ID should be in modified list")
	})

	t.Run("DuplicateUpdateOperations", func(t *testing.T) {
		// First add a cell that we can update
		cellID := uuid.New().String()
		originalNode, _ := CreateNode(cellID, NodeShapeProcess, 100, 150, 80, 40)

		// Add cell to current state
		currentState[cellID] = &originalNode
		testDiagram.Cells = []DfdDiagram_Cells_Item{originalNode}

		// Now send duplicate update operations
		updatedNode, _ := CreateNode(cellID, NodeShapeProcess, 200, 250, 80, 40)
		operation := CellPatchOperation{
			Type: "patch",
			Cells: []CellOperation{
				{
					ID:        cellID,
					Operation: "update",
					Data:      &updatedNode,
				},
				// Duplicate update (should be filtered)
				{
					ID:        cellID,
					Operation: "update",
					Data:      &updatedNode,
				},
			},
		}

		result := session.processAndValidateCellOperations(testDiagram, currentState, operation)

		assert.True(t, result.Valid, "Duplicate update operations should be valid after filtering")
		assert.True(t, result.StateChanged, "Update operation should change state")
		assert.Len(t, result.CellsModified, 1, "Should have one modified cell after deduplication")
		assert.Equal(t, cellID, result.CellsModified[0], "Modified cell ID should match")
	})
}
