package api

import (
	"fmt"
)

// CellConverter provides conversion utilities between Cell struct and union types
type CellConverter struct{}

// NewCellConverter creates a new cell converter instance
func NewCellConverter() *CellConverter {
	return &CellConverter{}
}

// ConvertCellToUnionItem converts a Cell struct to DfdDiagram_Cells_Item union type
func (c *CellConverter) ConvertCellToUnionItem(cell Cell) (DfdDiagram_Cells_Item, error) {
	var item DfdDiagram_Cells_Item

	// Determine if this is a node or edge based on the shape field
	// Edges typically have shapes like "edge", "connector", etc.
	// Nodes have shapes like "rectangle", "ellipse", "process", etc.
	if isEdgeShape(cell.Shape) {
		// Convert to Edge
		edge := Edge{
			Id:    cell.Id,
			Shape: EdgeShape(cell.Shape),
			Data:  cell.Data,
		}

		// Set optional fields if they exist
		if cell.Visible != nil {
			edge.Visible = cell.Visible
		}
		if cell.ZIndex != nil {
			edge.ZIndex = cell.ZIndex
		}

		if err := item.FromEdge(edge); err != nil {
			return item, fmt.Errorf("failed to convert cell to edge: %w", err)
		}
	} else {
		// Convert to Node - we need to extract geometry information
		// This is a best-effort conversion since Cell doesn't have all Node fields
		node := Node{
			Id:     cell.Id,
			Shape:  NodeShape(cell.Shape),
			Data:   cell.Data,
			Height: 40.0, // Default values since Cell doesn't have these
			Width:  80.0,
			X:      0.0,
			Y:      0.0,
		}

		// Set optional fields if they exist
		if cell.Visible != nil {
			node.Visible = cell.Visible
		}
		if cell.ZIndex != nil {
			node.ZIndex = cell.ZIndex
		}

		if err := item.FromNode(node); err != nil {
			return item, fmt.Errorf("failed to convert cell to node: %w", err)
		}
	}

	return item, nil
}

// ConvertUnionItemToCell converts a DfdDiagram_Cells_Item union type to Cell struct
func (c *CellConverter) ConvertUnionItemToCell(item DfdDiagram_Cells_Item) (Cell, error) {
	// Try to convert as Node first
	if node, err := item.AsNode(); err == nil {
		return c.nodeToCell(node), nil
	}

	// Try to convert as Edge
	if edge, err := item.AsEdge(); err == nil {
		return c.edgeToCell(edge), nil
	}

	return Cell{}, fmt.Errorf("union item is neither Node nor Edge")
}

// nodeToCell converts a Node to Cell struct
func (c *CellConverter) nodeToCell(node Node) Cell {
	return Cell{
		Id:      node.Id,
		Shape:   string(node.Shape),
		Data:    node.Data,
		Visible: node.Visible,
		ZIndex:  node.ZIndex,
	}
}

// edgeToCell converts an Edge to Cell struct
func (c *CellConverter) edgeToCell(edge Edge) Cell {
	return Cell{
		Id:      edge.Id,
		Shape:   string(edge.Shape),
		Data:    edge.Data,
		Visible: edge.Visible,
		ZIndex:  edge.ZIndex,
	}
}

// isEdgeShape determines if a shape string represents an edge
func isEdgeShape(shape string) bool {
	edgeShapes := []string{
		"edge",
		"connector",
		"line",
		"arrow",
		"dashed-line",
		"dotted-line",
	}

	for _, edgeShape := range edgeShapes {
		if shape == edgeShape {
			return true
		}
	}

	return false
}

// ConvertCellSliceToUnionItems converts a slice of Cell structs to DfdDiagram_Cells_Item slice
func (c *CellConverter) ConvertCellSliceToUnionItems(cells []Cell) ([]DfdDiagram_Cells_Item, error) {
	items := make([]DfdDiagram_Cells_Item, len(cells))

	for i, cell := range cells {
		item, err := c.ConvertCellToUnionItem(cell)
		if err != nil {
			return nil, fmt.Errorf("failed to convert cell at index %d: %w", i, err)
		}
		items[i] = item
	}

	return items, nil
}

// ConvertUnionItemsToCellSlice converts a slice of DfdDiagram_Cells_Item to Cell structs
func (c *CellConverter) ConvertUnionItemsToCellSlice(items []DfdDiagram_Cells_Item) ([]Cell, error) {
	cells := make([]Cell, len(items))

	for i, item := range items {
		cell, err := c.ConvertUnionItemToCell(item)
		if err != nil {
			return nil, fmt.Errorf("failed to convert union item at index %d: %w", i, err)
		}
		cells[i] = cell
	}

	return cells, nil
}

// Helper function to create a Node from basic parameters
func CreateNode(id string, shape NodeShape, x, y, width, height float32) (DfdDiagram_Cells_Item, error) {
	var item DfdDiagram_Cells_Item

	uuid, err := ParseUUID(id)
	if err != nil {
		return item, fmt.Errorf("invalid UUID: %w", err)
	}

	node := Node{
		Id:     uuid,
		Shape:  shape,
		X:      x,
		Y:      y,
		Width:  width,
		Height: height,
	}

	if err := item.FromNode(node); err != nil {
		return item, fmt.Errorf("failed to create node: %w", err)
	}

	return item, nil
}

// Helper function to create an Edge from basic parameters
func CreateEdge(id string, shape EdgeShape, sourceId, targetId string) (DfdDiagram_Cells_Item, error) {
	var item DfdDiagram_Cells_Item

	uuid, err := ParseUUID(id)
	if err != nil {
		return item, fmt.Errorf("invalid UUID: %w", err)
	}

	sourceUUID, err := ParseUUID(sourceId)
	if err != nil {
		return item, fmt.Errorf("invalid source UUID: %w", err)
	}

	targetUUID, err := ParseUUID(targetId)
	if err != nil {
		return item, fmt.Errorf("invalid target UUID: %w", err)
	}

	edge := Edge{
		Id:     uuid,
		Shape:  shape,
		Source: EdgeTerminal{Cell: sourceUUID},
		Target: EdgeTerminal{Cell: targetUUID},
	}

	if err := item.FromEdge(edge); err != nil {
		return item, fmt.Errorf("failed to create edge: %w", err)
	}

	return item, nil
}
