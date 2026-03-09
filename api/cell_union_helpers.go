package api

import "encoding/json"

// SafeFromNode updates a DfdDiagram_Cells_Item with a Node while preserving
// the node's actual shape value. The generated FromNode() method hardcodes
// the shape to a fixed discriminator value (due to oapi-codegen limitation
// where multiple discriminator values map to the same type), which corrupts
// the shape field. This helper bypasses that by marshaling the node directly
// and storing the raw bytes via UnmarshalJSON.
func SafeFromNode(item *DfdDiagram_Cells_Item, node Node) error {
	b, err := json.Marshal(node)
	if err != nil {
		return err
	}
	return item.UnmarshalJSON(b)
}

// SafeFromEdge updates a DfdDiagram_Cells_Item with an Edge while preserving
// the edge's actual shape value. While "flow" is currently the only edge shape,
// this helper provides consistency with SafeFromNode and future-proofs against
// additional edge shapes.
func SafeFromEdge(item *DfdDiagram_Cells_Item, edge Edge) error {
	b, err := json.Marshal(edge)
	if err != nil {
		return err
	}
	return item.UnmarshalJSON(b)
}
