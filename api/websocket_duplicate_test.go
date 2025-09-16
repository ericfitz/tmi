package api

import (
	"testing"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDuplicateCellOperationFiltering tests that duplicate cell operations within a single message are filtered out
func TestDuplicateCellOperationFiltering(t *testing.T) {
	// Create a test diagram with initial state
	diagramID := openapi_types.UUID(uuid.New())
	testDiagram := &DfdDiagram{
		Id:    &diagramID,
		Name:  "Test Diagram",
		Type:  "DFD-1.0.0",
		Cells: []DfdDiagram_Cells_Item{},
	}

	// Create a DiagramSession
	session := &DiagramSession{
		ID:                 uuid.New().String(),
		DiagramID:          testDiagram.Id.String(),
		ThreatModelID:      uuid.New().String(),
		Clients:            make(map[*WebSocketClient]bool),
		NextSequenceNumber: 1,
		clientLastSequence: make(map[string]uint64),
		recentCorrections:  make(map[string]int),
	}

	// Build current state map (empty for this test)
	currentState := make(map[string]*Cell)

	t.Run("SingleCellOperation", func(t *testing.T) {
		cellID := uuid.New().String()
		operation := CellPatchOperation{
			Type: "patch",
			Cells: []CellOperation{
				{
					ID:        cellID,
					Operation: "add",
					Data: &Cell{
						Id:    uuid.MustParse(cellID),
						Shape: "process",
						Data: &Cell_Data{
							AdditionalProperties: map[string]interface{}{
								"x":      100.0,
								"y":      150.0,
								"width":  120.0,
								"height": 80.0,
								"label":  "Test Process",
							},
						},
					},
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
		currentState = make(map[string]*Cell)

		cellID := uuid.New().String()
		
		// Create 8 identical cell operations (simulating the bug reported)
		duplicateCells := make([]CellOperation, 8)
		for i := 0; i < 8; i++ {
			duplicateCells[i] = CellOperation{
				ID:        cellID,
				Operation: "add",
				Data: &Cell{
					Id:    uuid.MustParse(cellID),
					Shape: "process",
					Data: &Cell_Data{
						AdditionalProperties: map[string]interface{}{
							"x":      100.0,
							"y":      150.0,
							"width":  120.0,
							"height": 80.0,
							"label":  "Test Process",
						},
					},
				},
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
		currentState = make(map[string]*Cell)

		cellID1 := uuid.New().String()
		cellID2 := uuid.New().String()
		
		operation := CellPatchOperation{
			Type: "patch",
			Cells: []CellOperation{
				// First unique cell
				{
					ID:        cellID1,
					Operation: "add",
					Data: &Cell{
						Id:    uuid.MustParse(cellID1),
						Shape: "process",
						Data: &Cell_Data{
							AdditionalProperties: map[string]interface{}{
								"x": 100.0, "y": 150.0, "label": "Process 1",
							},
						},
					},
				},
				// Duplicate of first cell (should be filtered)
				{
					ID:        cellID1,
					Operation: "add",
					Data: &Cell{
						Id:    uuid.MustParse(cellID1),
						Shape: "process",
						Data: &Cell_Data{
							AdditionalProperties: map[string]interface{}{
								"x": 100.0, "y": 150.0, "label": "Process 1",
							},
						},
					},
				},
				// Second unique cell
				{
					ID:        cellID2,
					Operation: "add",
					Data: &Cell{
						Id:    uuid.MustParse(cellID2),
						Shape: "store",
						Data: &Cell_Data{
							AdditionalProperties: map[string]interface{}{
								"x": 300.0, "y": 150.0, "label": "Store 1",
							},
						},
					},
				},
				// Another duplicate of first cell (should be filtered)
				{
					ID:        cellID1,
					Operation: "add",
					Data: &Cell{
						Id:    uuid.MustParse(cellID1),
						Shape: "process",
						Data: &Cell_Data{
							AdditionalProperties: map[string]interface{}{
								"x": 100.0, "y": 150.0, "label": "Process 1",
							},
						},
					},
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
		cell := &Cell{
			Id:    uuid.MustParse(cellID),
			Shape: "process",
			Data: &Cell_Data{
				AdditionalProperties: map[string]interface{}{
					"x": 100.0, "y": 150.0, "label": "Original",
				},
			},
		}
		
		// Add cell to current state
		currentState[cellID] = cell
		converter := NewCellConverter()
		cellItem, err := converter.ConvertCellToUnionItem(*cell)
		require.NoError(t, err, "Should convert cell successfully")
		testDiagram.Cells = []DfdDiagram_Cells_Item{cellItem}

		// Now send duplicate update operations
		operation := CellPatchOperation{
			Type: "patch",
			Cells: []CellOperation{
				{
					ID:        cellID,
					Operation: "update",
					Data: &Cell{
						Id:    uuid.MustParse(cellID),
						Shape: "process",
						Data: &Cell_Data{
							AdditionalProperties: map[string]interface{}{
								"x": 200.0, "y": 250.0, "label": "Updated",
							},
						},
					},
				},
				// Duplicate update (should be filtered)
				{
					ID:        cellID,
					Operation: "update",
					Data: &Cell{
						Id:    uuid.MustParse(cellID),
						Shape: "process",
						Data: &Cell_Data{
							AdditionalProperties: map[string]interface{}{
								"x": 200.0, "y": 250.0, "label": "Updated",
							},
						},
					},
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