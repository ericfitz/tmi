package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeNodeCellItem(id uuid.UUID) DfdDiagram_Cells_Item {
	var item DfdDiagram_Cells_Item
	node := Node{Id: id}
	_ = item.FromNode(node)
	return item
}

func makeEdgeCellItem(id uuid.UUID) DfdDiagram_Cells_Item {
	var item DfdDiagram_Cells_Item
	edge := Edge{Id: id}
	_ = item.FromEdge(edge)
	return item
}

func TestExtractCellID(t *testing.T) {
	nodeID := uuid.New()
	edgeID := uuid.New()

	tests := []struct {
		name     string
		item     DfdDiagram_Cells_Item
		expected string
	}{
		{"node cell", makeNodeCellItem(nodeID), nodeID.String()},
		{"edge cell", makeEdgeCellItem(edgeID), edgeID.String()},
		{"empty cell item", DfdDiagram_Cells_Item{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCellID(&tt.item)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDeduplicateCellOperations(t *testing.T) {
	tests := []struct {
		name        string
		cells       []CellOperation
		logContext  string
		expectedLen int
		expectedIDs []string
	}{
		{
			name:        "no duplicates",
			cells:       []CellOperation{{ID: "a", Operation: "add"}, {ID: "b", Operation: "add"}},
			logContext:  "",
			expectedLen: 2,
			expectedIDs: []string{"a", "b"},
		},
		{
			name:        "with duplicates",
			cells:       []CellOperation{{ID: "a", Operation: "add"}, {ID: "a", Operation: "update"}, {ID: "b", Operation: "add"}},
			logContext:  "Session: test-123",
			expectedLen: 2,
			expectedIDs: []string{"a", "b"},
		},
		{
			name:        "all duplicates of same ID",
			cells:       []CellOperation{{ID: "a", Operation: "add"}, {ID: "a", Operation: "update"}, {ID: "a", Operation: "remove"}},
			logContext:  "",
			expectedLen: 1,
			expectedIDs: []string{"a"},
		},
		{
			name:        "empty input",
			cells:       []CellOperation{},
			logContext:  "",
			expectedLen: 0,
			expectedIDs: []string{},
		},
		{
			name:        "single element",
			cells:       []CellOperation{{ID: "a", Operation: "add"}},
			logContext:  "",
			expectedLen: 1,
			expectedIDs: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deduplicateCellOperations(tt.cells, tt.logContext)
			assert.Equal(t, tt.expectedLen, len(result))
			for i, expectedID := range tt.expectedIDs {
				assert.Equal(t, expectedID, result[i].ID)
			}
		})
	}
}

func TestDeduplicateCellOperationsKeepsFirstOccurrence(t *testing.T) {
	cells := []CellOperation{
		{ID: "a", Operation: "add"},
		{ID: "a", Operation: "update"},
	}
	result := deduplicateCellOperations(cells, "")
	require.Len(t, result, 1)
	assert.Equal(t, "add", result[0].Operation)
}

func TestFindAndReplaceCellInDiagram(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	id3 := uuid.New()

	diagram := &DfdDiagram{
		Cells: []DfdDiagram_Cells_Item{
			makeNodeCellItem(id1),
			makeEdgeCellItem(id2),
		},
	}

	// Replace existing node cell
	newItem := makeNodeCellItem(id1)
	found := findAndReplaceCellInDiagram(diagram, id1.String(), newItem)
	assert.True(t, found)

	// Try to replace non-existent cell
	found = findAndReplaceCellInDiagram(diagram, id3.String(), newItem)
	assert.False(t, found)

	// Verify diagram still has 2 cells
	assert.Len(t, diagram.Cells, 2)
}

func TestRemoveCellFromDiagram(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	id3 := uuid.New()

	t.Run("remove existing cell", func(t *testing.T) {
		diagram := &DfdDiagram{
			Cells: []DfdDiagram_Cells_Item{
				makeNodeCellItem(id1),
				makeEdgeCellItem(id2),
			},
		}
		found := removeCellFromDiagram(diagram, id1.String())
		assert.True(t, found)
		assert.Len(t, diagram.Cells, 1)
	})

	t.Run("remove non-existent cell", func(t *testing.T) {
		diagram := &DfdDiagram{
			Cells: []DfdDiagram_Cells_Item{
				makeNodeCellItem(id1),
			},
		}
		found := removeCellFromDiagram(diagram, id3.String())
		assert.False(t, found)
		assert.Len(t, diagram.Cells, 1)
	})

	t.Run("remove last cell", func(t *testing.T) {
		diagram := &DfdDiagram{
			Cells: []DfdDiagram_Cells_Item{
				makeNodeCellItem(id1),
			},
		}
		found := removeCellFromDiagram(diagram, id1.String())
		assert.True(t, found)
		assert.Empty(t, diagram.Cells)
	})

	t.Run("remove from empty diagram", func(t *testing.T) {
		diagram := &DfdDiagram{
			Cells: []DfdDiagram_Cells_Item{},
		}
		found := removeCellFromDiagram(diagram, id1.String())
		assert.False(t, found)
	})
}

func TestBuildCellState(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()

	t.Run("builds state from cells", func(t *testing.T) {
		cells := []DfdDiagram_Cells_Item{
			makeNodeCellItem(id1),
			makeEdgeCellItem(id2),
		}
		state := buildCellState(cells)
		assert.Len(t, state, 2)
		assert.NotNil(t, state[id1.String()])
		assert.NotNil(t, state[id2.String()])
	})

	t.Run("empty cells", func(t *testing.T) {
		state := buildCellState([]DfdDiagram_Cells_Item{})
		assert.Empty(t, state)
	})

	t.Run("skips cells with no extractable ID", func(t *testing.T) {
		cells := []DfdDiagram_Cells_Item{
			makeNodeCellItem(id1),
			{}, // empty item - no node or edge
		}
		state := buildCellState(cells)
		assert.Len(t, state, 1)
		assert.NotNil(t, state[id1.String()])
	})
}

func TestCopyPreviousState(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()

	item1 := makeNodeCellItem(id1)
	item2 := makeEdgeCellItem(id2)

	original := map[string]*DfdDiagram_Cells_Item{
		id1.String(): &item1,
		id2.String(): &item2,
	}

	copied := copyPreviousState(original)

	// Same keys
	assert.Len(t, copied, 2)
	assert.NotNil(t, copied[id1.String()])
	assert.NotNil(t, copied[id2.String()])

	// Different pointers (deep copy)
	assert.NotSame(t, original[id1.String()], copied[id1.String()])
	assert.NotSame(t, original[id2.String()], copied[id2.String()])

	t.Run("empty state", func(t *testing.T) {
		empty := copyPreviousState(map[string]*DfdDiagram_Cells_Item{})
		assert.Empty(t, empty)
	})
}
