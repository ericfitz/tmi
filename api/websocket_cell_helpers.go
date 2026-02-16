package api

import (
	"github.com/ericfitz/tmi/internal/slogging"
)

// extractCellID extracts the ID string from a DfdDiagram_Cells_Item union type.
// Returns the Node or Edge ID as a string, or empty string if neither can be extracted.
func extractCellID(cellItem *DfdDiagram_Cells_Item) string {
	if node, err := cellItem.AsNode(); err == nil {
		return node.Id.String()
	}
	if edge, err := cellItem.AsEdge(); err == nil {
		return edge.Id.String()
	}
	return ""
}

// deduplicateCellOperations filters duplicate cell operations from a slice,
// keeping only the first operation for each cell ID. The logContext parameter
// is included in warning/info log messages (e.g., a session ID).
func deduplicateCellOperations(cells []CellOperation, logContext string) []CellOperation {
	seenCellIDs := make(map[string]bool)
	deduplicatedCells := make([]CellOperation, 0, len(cells))

	for _, cellOp := range cells {
		if seenCellIDs[cellOp.ID] {
			if logContext != "" {
				slogging.Get().Warn("Duplicate cell operation detected in single message - %s, CellID: %s, Operation: %s",
					logContext, cellOp.ID, cellOp.Operation)
			} else {
				slogging.Get().Warn("Duplicate cell operation detected in single message - CellID: %s, Operation: %s",
					cellOp.ID, cellOp.Operation)
			}
			continue
		}
		seenCellIDs[cellOp.ID] = true
		deduplicatedCells = append(deduplicatedCells, cellOp)
	}

	if len(deduplicatedCells) != len(cells) {
		if logContext != "" {
			slogging.Get().Info("Filtered %d duplicate cell operations from message - %s",
				len(cells)-len(deduplicatedCells), logContext)
		} else {
			slogging.Get().Info("Filtered %d duplicate cell operations from message",
				len(cells)-len(deduplicatedCells))
		}
	}

	return deduplicatedCells
}

// findAndReplaceCellInDiagram finds a cell by ID in the diagram and replaces it with newData.
// Returns true if the cell was found and replaced, false otherwise.
func findAndReplaceCellInDiagram(diagram *DfdDiagram, cellID string, newData DfdDiagram_Cells_Item) bool {
	for i := range diagram.Cells {
		if extractCellID(&diagram.Cells[i]) == cellID {
			diagram.Cells[i] = newData
			return true
		}
	}
	return false
}

// removeCellFromDiagram removes a cell by ID from the diagram using swap-remove.
// Returns true if the cell was found and removed, false otherwise.
func removeCellFromDiagram(diagram *DfdDiagram, cellID string) bool {
	for i := range diagram.Cells {
		if extractCellID(&diagram.Cells[i]) == cellID {
			lastIndex := len(diagram.Cells) - 1
			if i != lastIndex {
				diagram.Cells[i] = diagram.Cells[lastIndex]
			}
			diagram.Cells = diagram.Cells[:lastIndex]
			return true
		}
	}
	return false
}

// buildCellState builds a map of cell ID to cell item pointer for conflict detection.
func buildCellState(cells []DfdDiagram_Cells_Item) map[string]*DfdDiagram_Cells_Item {
	state := make(map[string]*DfdDiagram_Cells_Item)
	for i := range cells {
		cellItem := &cells[i]
		if itemID := extractCellID(cellItem); itemID != "" {
			state[itemID] = cellItem
		}
	}
	return state
}

// copyPreviousState creates a deep copy of a cell state map for storing as previous state.
func copyPreviousState(currentState map[string]*DfdDiagram_Cells_Item) map[string]*DfdDiagram_Cells_Item {
	previousState := make(map[string]*DfdDiagram_Cells_Item, len(currentState))
	for k, v := range currentState {
		cellCopy := *v
		previousState[k] = &cellCopy
	}
	return previousState
}
