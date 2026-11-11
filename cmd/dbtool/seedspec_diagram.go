package main

import (
	"fmt"

	"github.com/google/uuid"
)

// diagramNamespace is a fixed UUID v5 namespace for generating deterministic cell UUIDs.
// This ensures the same seed-spec always produces the same diagram cell IDs.
var diagramNamespace = uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-000000000001")

// Default dimensions per node shape type.
var defaultNodeSize = map[string][2]float64{
	"actor":             {100, 60},
	"process":           {140, 70},
	"store":             {140, 50},
	"security-boundary": {200, 150},
	"text-box":          {120, 40},
}

// transformDiagramCells converts seed-spec nodes/edges into DFD cell maps
// compatible with the DfdDiagramInput cells array.
func transformDiagramCells(nodes []SeedSpecNode, edges []SeedSpecEdge) ([]map[string]any, error) {
	// Build ID mapping: seed-spec node ID -> generated UUID
	idMap := make(map[string]string, len(nodes))
	for _, n := range nodes {
		idMap[n.ID] = uuid.NewSHA1(diagramNamespace, []byte(n.ID)).String()
	}

	cells := make([]map[string]any, 0, len(nodes)+len(edges))

	// Transform nodes
	for _, n := range nodes {
		shape := mapNodeType(n.Type)
		cellID := idMap[n.ID]

		size := defaultNodeSize[shape]
		if size == [2]float64{} {
			size = [2]float64{140, 70} // fallback
		}

		cell := map[string]any{
			"id":     cellID,
			"shape":  shape,
			"x":      n.X,
			"y":      n.Y,
			"width":  size[0],
			"height": size[1],
		}

		if n.Label != "" {
			cell["attrs"] = map[string]any{
				"text": map[string]any{
					"text": n.Label,
				},
			}
		}

		if n.Parent != "" {
			parentID, ok := idMap[n.Parent]
			if !ok {
				return nil, fmt.Errorf("node %q references unknown parent %q", n.ID, n.Parent)
			}
			cell["parent"] = parentID
		}

		cells = append(cells, cell)
	}

	// Transform edges
	for i, e := range edges {
		sourceID, ok := idMap[e.Source]
		if !ok {
			return nil, fmt.Errorf("edge %d references unknown source %q", i, e.Source)
		}
		targetID, ok := idMap[e.Target]
		if !ok {
			return nil, fmt.Errorf("edge %d references unknown target %q", i, e.Target)
		}

		edgeID := uuid.NewSHA1(diagramNamespace, []byte(fmt.Sprintf("edge:%s->%s:%d", e.Source, e.Target, i))).String()

		cell := map[string]any{
			"id":    edgeID,
			"shape": "flow",
			"source": map[string]any{
				"cell": sourceID,
			},
			"target": map[string]any{
				"cell": targetID,
			},
		}

		if e.Label != "" {
			cell["labels"] = []map[string]any{
				{
					"attrs": map[string]any{
						"text": map[string]any{
							"text": e.Label,
						},
					},
					"position": 0.5,
				},
			}
		}

		cells = append(cells, cell)
	}

	return cells, nil
}

const shapeProcess = "process"

// mapNodeType maps seed-spec node types to DFD cell shape values.
func mapNodeType(nodeType string) string {
	switch nodeType {
	case "actor":
		return "actor"
	case shapeProcess:
		return shapeProcess
	case "store":
		return "store"
	case "security-boundary", "boundary":
		return "security-boundary"
	case "text-box", "text":
		return "text-box"
	default:
		return shapeProcess
	}
}
