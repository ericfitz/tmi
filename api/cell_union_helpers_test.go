package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeFromNode_PreservesAllShapes(t *testing.T) {
	shapes := []NodeShape{
		NodeShapeActor,
		NodeShapeProcess,
		NodeShapeStore,
		NodeShapeSecurityBoundary,
		NodeShapeTextBox,
	}

	for _, shape := range shapes {
		t.Run(string(shape), func(t *testing.T) {
			id := uuid.New()
			node := Node{
				Id:    id,
				Shape: shape,
				Position: &struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 100, Y: 200},
				Size: &struct {
					Height float32 `json:"height"`
					Width  float32 `json:"width"`
				}{Height: 60, Width: 120},
			}

			var item DfdDiagram_Cells_Item
			err := SafeFromNode(&item, node)
			require.NoError(t, err)

			// Round-trip: extract node back and verify shape is preserved
			extracted, err := item.AsNode()
			require.NoError(t, err)
			assert.Equal(t, shape, extracted.Shape, "shape should be preserved through SafeFromNode round-trip")
			assert.Equal(t, id, extracted.Id, "id should be preserved")
		})
	}
}

func TestSafeFromNode_VsFromNode_ShapeCorruption(t *testing.T) {
	// Demonstrate that the generated FromNode corrupts shape
	id := uuid.New()
	node := Node{
		Id:    id,
		Shape: NodeShapeActor,
		Position: &struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		}{X: 100, Y: 200},
		Size: &struct {
			Height float32 `json:"height"`
			Width  float32 `json:"width"`
		}{Height: 60, Width: 120},
	}

	// Generated FromNode corrupts shape
	var corruptedItem DfdDiagram_Cells_Item
	err := corruptedItem.FromNode(node)
	require.NoError(t, err)

	corrupted, err := corruptedItem.AsNode()
	require.NoError(t, err)
	assert.NotEqual(t, NodeShapeActor, corrupted.Shape,
		"generated FromNode should corrupt shape (this test documents the bug)")

	// SafeFromNode preserves shape
	var safeItem DfdDiagram_Cells_Item
	err = SafeFromNode(&safeItem, node)
	require.NoError(t, err)

	safe, err := safeItem.AsNode()
	require.NoError(t, err)
	assert.Equal(t, NodeShapeActor, safe.Shape,
		"SafeFromNode should preserve the original shape")
}

func TestSafeFromEdge_PreservesShape(t *testing.T) {
	id := uuid.New()
	sourceID := uuid.New()
	targetID := uuid.New()

	edge := Edge{
		Id:     id,
		Shape:  EdgeShapeFlow,
		Source: EdgeTerminal{Cell: sourceID},
		Target: EdgeTerminal{Cell: targetID},
	}

	var item DfdDiagram_Cells_Item
	err := SafeFromEdge(&item, edge)
	require.NoError(t, err)

	extracted, err := item.AsEdge()
	require.NoError(t, err)
	assert.Equal(t, EdgeShapeFlow, extracted.Shape, "edge shape should be preserved")
	assert.Equal(t, id, extracted.Id, "edge id should be preserved")
	assert.Equal(t, sourceID, extracted.Source.Cell, "source should be preserved")
	assert.Equal(t, targetID, extracted.Target.Cell, "target should be preserved")
}

func TestSafeFromNode_PreservesAllNodeFields(t *testing.T) {
	id := uuid.New()
	parentID := uuid.New()
	fillColor := "#ff0000"
	strokeColor := "#000000"
	labelText := "My Actor"

	node := Node{
		Id:    id,
		Shape: NodeShapeActor,
		Position: &struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		}{X: -500, Y: -180},
		Size: &struct {
			Height float32 `json:"height"`
			Width  float32 `json:"width"`
		}{Height: 60, Width: 120},
		Parent: &parentID,
		Attrs: &NodeAttrs{
			Body: &struct {
				Fill            *string  `json:"fill,omitempty"`
				Stroke          *string  `json:"stroke,omitempty"`
				StrokeDasharray *string  `json:"strokeDasharray"`
				StrokeWidth     *float32 `json:"strokeWidth,omitempty"`
			}{
				Fill:   &fillColor,
				Stroke: &strokeColor,
			},
			Text: &struct {
				Fill               *string                          `json:"fill,omitempty"`
				FontFamily         *string                          `json:"fontFamily,omitempty"`
				FontSize           *float32                         `json:"fontSize,omitempty"`
				RefDx              *float32                         `json:"refDx,omitempty"`
				RefDy              *float32                         `json:"refDy,omitempty"`
				RefX               *float32                         `json:"refX,omitempty"`
				RefY               *float32                         `json:"refY,omitempty"`
				Text               *string                          `json:"text,omitempty"`
				TextAnchor         *NodeAttrsTextTextAnchor         `json:"textAnchor,omitempty"`
				TextVerticalAnchor *NodeAttrsTextTextVerticalAnchor `json:"textVerticalAnchor,omitempty"`
			}{
				Text: &labelText,
			},
		},
	}

	var item DfdDiagram_Cells_Item
	err := SafeFromNode(&item, node)
	require.NoError(t, err)

	extracted, err := item.AsNode()
	require.NoError(t, err)

	assert.Equal(t, NodeShapeActor, extracted.Shape)
	assert.Equal(t, id, extracted.Id)
	assert.Equal(t, &parentID, extracted.Parent)
	require.NotNil(t, extracted.Attrs)
	require.NotNil(t, extracted.Attrs.Body)
	assert.Equal(t, &fillColor, extracted.Attrs.Body.Fill)
	assert.Equal(t, &strokeColor, extracted.Attrs.Body.Stroke)
	require.NotNil(t, extracted.Attrs.Text)
	assert.Equal(t, &labelText, extracted.Attrs.Text.Text)
}

func TestNormalizeDiagramCells_PreservesShape(t *testing.T) {
	// Create cells with different shapes
	shapes := []NodeShape{
		NodeShapeActor,
		NodeShapeProcess,
		NodeShapeStore,
		NodeShapeSecurityBoundary,
	}

	cells := make([]DfdDiagram_Cells_Item, len(shapes))
	for i, shape := range shapes {
		node := Node{
			Id:    uuid.New(),
			Shape: shape,
			Position: &struct {
				X float32 `json:"x"`
				Y float32 `json:"y"`
			}{X: float32(i * 100), Y: float32(i * 100)},
			Size: &struct {
				Height float32 `json:"height"`
				Width  float32 `json:"width"`
			}{Height: 60, Width: 120},
		}
		err := SafeFromNode(&cells[i], node)
		require.NoError(t, err)
	}

	// Normalize should not corrupt shapes
	NormalizeDiagramCells(cells)

	for i, shape := range shapes {
		extracted, err := cells[i].AsNode()
		require.NoError(t, err)
		assert.Equal(t, shape, extracted.Shape,
			"NormalizeDiagramCells should preserve shape %q", string(shape))
	}
}

func TestSanitizeDiagramCellMetadata_PreservesShape(t *testing.T) {
	// Create an actor node with metadata
	id := uuid.New()
	metadata := &[]Metadata{
		{Key: "env", Value: "production"},
	}
	node := Node{
		Id:    id,
		Shape: NodeShapeActor,
		Position: &struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		}{X: 100, Y: 200},
		Size: &struct {
			Height float32 `json:"height"`
			Width  float32 `json:"width"`
		}{Height: 60, Width: 120},
		Data: &Node_Data{
			Metadata: metadata,
		},
	}

	var item DfdDiagram_Cells_Item
	err := SafeFromNode(&item, node)
	require.NoError(t, err)

	cells := []DfdDiagram_Cells_Item{item}

	// Sanitize metadata should not corrupt shape
	err = SanitizeDiagramCellMetadata(cells)
	require.NoError(t, err)

	extracted, err := cells[0].AsNode()
	require.NoError(t, err)
	assert.Equal(t, NodeShapeActor, extracted.Shape,
		"SanitizeDiagramCellMetadata should preserve actor shape")
}

func TestCreateNode_PreservesShape(t *testing.T) {
	// Test the test fixture helper preserves shape correctly
	shapes := []NodeShape{
		NodeShapeActor,
		NodeShapeProcess,
		NodeShapeStore,
		NodeShapeSecurityBoundary,
		NodeShapeTextBox,
	}

	for _, shape := range shapes {
		t.Run(string(shape), func(t *testing.T) {
			item, err := CreateNode(uuid.New().String(), shape, 100, 200, 120, 60)
			require.NoError(t, err)

			extracted, err := item.AsNode()
			require.NoError(t, err)
			assert.Equal(t, shape, extracted.Shape,
				"CreateNode should produce nodes with correct shape")
		})
	}
}
