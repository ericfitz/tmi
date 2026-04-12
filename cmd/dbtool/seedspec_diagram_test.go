package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransformDiagramCells_SimpleNodes(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "actor-1", Type: "actor", Label: "User", X: 100, Y: 200},
		{ID: "process-1", Type: "process", Label: "App", X: 300, Y: 200},
	}

	cells, err := transformDiagramCells(nodes, nil)
	require.NoError(t, err)
	require.Len(t, cells, 2)

	// Actor node
	assert.Equal(t, "actor", cells[0]["shape"])
	assert.Equal(t, float64(100), cells[0]["x"])
	assert.Equal(t, float64(200), cells[0]["y"])
	assert.Equal(t, float64(100), cells[0]["width"]) // default actor width
	assert.Equal(t, float64(60), cells[0]["height"]) // default actor height

	attrs, ok := cells[0]["attrs"].(map[string]any)
	require.True(t, ok)
	text, ok := attrs["text"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "User", text["text"])

	// Process node
	assert.Equal(t, "process", cells[1]["shape"])
	assert.Equal(t, float64(140), cells[1]["width"]) // default process width
	assert.Equal(t, float64(70), cells[1]["height"]) // default process height
}

func TestTransformDiagramCells_Edges(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "n1", Type: "actor", X: 0, Y: 0},
		{ID: "n2", Type: "process", X: 100, Y: 0},
	}
	edges := []SeedSpecEdge{
		{Source: "n1", Target: "n2", Label: "HTTP Request"},
	}

	cells, err := transformDiagramCells(nodes, edges)
	require.NoError(t, err)
	require.Len(t, cells, 3) // 2 nodes + 1 edge

	edge := cells[2]
	assert.Equal(t, "flow", edge["shape"])

	source, ok := edge["source"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, source["cell"])

	target, ok := edge["target"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, target["cell"])

	// Check label
	labels, ok := edge["labels"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, labels, 1)
	labelAttrs := labels[0]["attrs"].(map[string]any)
	labelText := labelAttrs["text"].(map[string]any)
	assert.Equal(t, "HTTP Request", labelText["text"])
	assert.Equal(t, 0.5, labels[0]["position"])
}

func TestTransformDiagramCells_DeterministicUUIDs(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "node-1", Type: "process", X: 0, Y: 0},
	}

	cells1, err := transformDiagramCells(nodes, nil)
	require.NoError(t, err)

	cells2, err := transformDiagramCells(nodes, nil)
	require.NoError(t, err)

	// Same input should produce same UUIDs
	assert.Equal(t, cells1[0]["id"], cells2[0]["id"])
}

func TestTransformDiagramCells_ParentResolution(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "parent", Type: "process", X: 100, Y: 100},
		{ID: "child", Type: "process", X: 120, Y: 120, Parent: "parent"},
	}

	cells, err := transformDiagramCells(nodes, nil)
	require.NoError(t, err)
	require.Len(t, cells, 2)

	parentID := cells[0]["id"]
	childParent := cells[1]["parent"]
	assert.Equal(t, parentID, childParent)
}

func TestTransformDiagramCells_UnknownParentError(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "child", Type: "process", X: 0, Y: 0, Parent: "nonexistent"},
	}

	_, err := transformDiagramCells(nodes, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown parent")
}

func TestTransformDiagramCells_UnknownSourceError(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "n1", Type: "actor", X: 0, Y: 0},
	}
	edges := []SeedSpecEdge{
		{Source: "nonexistent", Target: "n1"},
	}

	_, err := transformDiagramCells(nodes, edges)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown source")
}

func TestTransformDiagramCells_UnknownTargetError(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "n1", Type: "actor", X: 0, Y: 0},
	}
	edges := []SeedSpecEdge{
		{Source: "n1", Target: "nonexistent"},
	}

	_, err := transformDiagramCells(nodes, edges)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown target")
}

func TestTransformDiagramCells_EdgeWithoutLabel(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "n1", Type: "actor", X: 0, Y: 0},
		{ID: "n2", Type: "store", X: 100, Y: 0},
	}
	edges := []SeedSpecEdge{
		{Source: "n1", Target: "n2"},
	}

	cells, err := transformDiagramCells(nodes, edges)
	require.NoError(t, err)

	edge := cells[2]
	_, hasLabels := edge["labels"]
	assert.False(t, hasLabels, "edge without label should not have labels field")
}

func TestMapNodeType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"actor", "actor"},
		{"process", "process"},
		{"store", "store"},
		{"security-boundary", "security-boundary"},
		{"boundary", "security-boundary"},
		{"text-box", "text-box"},
		{"text", "text-box"},
		{"unknown", "process"}, // default
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, mapNodeType(tt.input), "mapNodeType(%q)", tt.input)
	}
}

func TestTransformDiagramCells_StoreDefaultSize(t *testing.T) {
	nodes := []SeedSpecNode{
		{ID: "s1", Type: "store", X: 0, Y: 0},
	}

	cells, err := transformDiagramCells(nodes, nil)
	require.NoError(t, err)

	assert.Equal(t, "store", cells[0]["shape"])
	assert.Equal(t, float64(140), cells[0]["width"])
	assert.Equal(t, float64(50), cells[0]["height"])
}
