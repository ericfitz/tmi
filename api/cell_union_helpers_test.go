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
	// Use a shape other than "process" since FromNode hardcodes shape to "process"
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

// TestSanitizeDiagramCellMetadata_NoNodeToEdgeCorruption verifies that nodes are never
// corrupted into edges during metadata sanitization. This is a regression test for #170
// where nodes without position data would fail AsNode(), fall through to AsEdge(), and
// get rewritten with edge-specific fields (source/target with nil UUIDs).
func TestSanitizeDiagramCellMetadata_NoNodeToEdgeCorruption(t *testing.T) {
	// Create a node cell with metadata but simulate a scenario where the
	// raw JSON might cause AsNode() to fail. Use a raw JSON node that has
	// metadata but could be tricky for the Node unmarshaler.
	nodeJSON := `{
		"id": "b4aa268c-84a6-4c01-a786-b23a0006c888",
		"shape": "actor",
		"x": 200, "y": 100, "width": 120, "height": 60,
		"attrs": {"body": {"fill": "#e8f4fd", "stroke": "#1f77b4"}, "text": {"text": "Actor", "fontSize": 12}},
		"data": {"_metadata": [{"key": "env", "value": "production"}]}
	}`
	var item DfdDiagram_Cells_Item
	err := item.UnmarshalJSON([]byte(nodeJSON))
	require.NoError(t, err)

	cells := []DfdDiagram_Cells_Item{item}
	err = SanitizeDiagramCellMetadata(cells)
	require.NoError(t, err)

	// Verify the cell is still a valid node, not corrupted to an edge
	extracted, err := cells[0].AsNode()
	require.NoError(t, err)

	// Verify node properties are preserved
	assert.Equal(t, NodeShapeActor, extracted.Shape)
	assert.NotNil(t, extracted.Position, "position should be preserved")
	assert.NotNil(t, extracted.Size, "size should be preserved")
	assert.NotNil(t, extracted.Attrs, "attrs should be preserved")
	assert.NotNil(t, extracted.Attrs.Body, "attrs.body should be preserved")
	assert.NotNil(t, extracted.Attrs.Text, "attrs.text should be preserved")

	// Verify no edge-specific fields leaked in via raw JSON
	disc, err := cells[0].Discriminator()
	require.NoError(t, err)
	assert.Equal(t, "actor", disc, "discriminator should still be 'actor'")

	// Verify AsEdge doesn't produce zero-value source/target
	// (AsEdge would succeed on any JSON, but if the cell was corrupted,
	// the raw JSON would contain source/target with nil UUIDs)
	rawJSON, _ := cells[0].MarshalJSON()
	assert.NotContains(t, string(rawJSON), "00000000-0000-0000-0000-000000000000",
		"cell should not contain nil UUIDs from edge source/target corruption")
}

// TestSanitizeDiagramCellMetadata_AllNodeShapes verifies that all node shapes
// are correctly handled by metadata sanitization without corruption.
func TestSanitizeDiagramCellMetadata_AllNodeShapes(t *testing.T) {
	shapes := []NodeShape{
		NodeShapeActor,
		NodeShapeProcess,
		NodeShapeStore,
		NodeShapeSecurityBoundary,
		NodeShapeTextBox,
	}

	for _, shape := range shapes {
		t.Run(string(shape), func(t *testing.T) {
			node := Node{
				Id:    uuid.New(),
				Shape: shape,
				Position: &struct {
					X float32 `json:"x"`
					Y float32 `json:"y"`
				}{X: 100, Y: 200},
				Size: &struct {
					Height float32 `json:"height"`
					Width  float32 `json:"width"`
				}{Height: 60, Width: 120},
				Attrs: &NodeAttrs{
					Body: &struct {
						Fill            *string  `json:"fill,omitempty"`
						Stroke          *string  `json:"stroke,omitempty"`
						StrokeDasharray *string  `json:"strokeDasharray"`
						StrokeWidth     *float32 `json:"strokeWidth,omitempty"`
					}{},
				},
				Data: &Node_Data{
					Metadata: &[]Metadata{{Key: "test", Value: "value"}},
				},
			}

			var item DfdDiagram_Cells_Item
			err := SafeFromNode(&item, node)
			require.NoError(t, err)

			cells := []DfdDiagram_Cells_Item{item}
			err = SanitizeDiagramCellMetadata(cells)
			require.NoError(t, err)

			extracted, err := cells[0].AsNode()
			require.NoError(t, err)
			assert.Equal(t, shape, extracted.Shape, "shape should be preserved after sanitization")
			assert.NotNil(t, extracted.Attrs, "attrs should be preserved after sanitization")
		})
	}
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
