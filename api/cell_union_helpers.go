package api

import "encoding/json"

// SafeFromNode updates a DfdDiagram_Cells_Item with a Node while preserving
// the node's actual shape value. The generated FromNode() currently marshals
// the value as given, because oapi-codegen v2.7.1 assigns the discriminator in
// From<Variant>/Merge<Variant> only when the discriminator mapping has exactly
// one entry per oneOf member (union.tmpl). Here cells.items has 2 members and 6
// mapping entries, so nothing is assigned. Collapsing Node's five shape entries
// to one would make the union qualify and hardcode a single shape into every
// node. This helper is the required guard: it marshals the node directly and
// stores the raw bytes via UnmarshalJSON.
// SEM@e0319b46956724d532b5b4f64b9f66b006e3a0a9: update a diagram cell union item with a node while preserving its actual shape discriminator (pure)
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
// SEM@e0319b46956724d532b5b4f64b9f66b006e3a0a9: update a diagram cell union item with an edge while preserving its actual shape discriminator (pure)
func SafeFromEdge(item *DfdDiagram_Cells_Item, edge Edge) error {
	b, err := json.Marshal(edge)
	if err != nil {
		return err
	}
	return item.UnmarshalJSON(b)
}
