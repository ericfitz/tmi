package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"gopkg.in/yaml.v3"
)

// buildMinimalDiagramModel transforms full threat model and diagram into minimal model representation.
// Extracts threat model context and transforms cells to minimal format without visual properties.
func buildMinimalDiagramModel(tm ThreatModel, diagram DfdDiagram) MinimalDiagramModel {
	// Flatten threat model metadata from array to map
	tmMetadata := flattenMetadata(tm.Metadata)

	// Get description (may be nil)
	description := ""
	if tm.Description != nil {
		description = *tm.Description
	}

	// Transform diagram cells to minimal representation
	minimalCells := transformCellsToMinimal(diagram.Cells)

	return MinimalDiagramModel{
		Id:          *tm.Id,
		Name:        tm.Name,
		Description: description,
		Metadata:    tmMetadata,
		Cells:       minimalCells,
	}
}

// flattenMetadata converts []Metadata array format to map[string]string.
// Used for both threat model metadata and cell metadata.
//
// Example: [{"key": "env", "value": "prod"}] → {"env": "prod"}
func flattenMetadata(metadata *[]Metadata) map[string]string {
	result := make(map[string]string)
	if metadata == nil {
		return result
	}

	for _, m := range *metadata {
		result[m.Key] = m.Value
	}
	return result
}

// transformCellsToMinimal converts full diagram cells to minimal representation.
//
// Algorithm:
//  1. First pass: Build parent → children map for all nodes
//  2. Second pass: Transform each cell (node or edge) to minimal format
//  3. For nodes: Extract labels from attrs and text-box children
//  4. For edges: Extract labels from labels array
//
// Returns slice of MinimalCell union types (can be MinimalNode or MinimalEdge).
func transformCellsToMinimal(cells []DfdDiagram_Cells_Item) []MinimalCell {
	var minimalCells []MinimalCell

	// Pass 1: Build parent → children relationship map
	childrenMap := buildChildrenMap(cells)

	// Pass 2: Transform each cell to minimal format
	for _, cellUnion := range cells {
		// Attempt to decode as Node
		if node, err := cellUnion.AsNode(); err == nil {
			minimalNode := transformNodeToMinimal(node, childrenMap, cells)
			// Wrap in MinimalCell - set the Node variant
			cellBytes, _ := json.Marshal(minimalNode)
			var minCell MinimalCell
			_ = json.Unmarshal(cellBytes, &minCell)
			minimalCells = append(minimalCells, minCell)
			continue
		}

		// Attempt to decode as Edge
		if edge, err := cellUnion.AsEdge(); err == nil {
			minimalEdge := transformEdgeToMinimal(edge)
			// Wrap in MinimalCell - set the Edge variant
			cellBytes, _ := json.Marshal(minimalEdge)
			var minCell MinimalCell
			_ = json.Unmarshal(cellBytes, &minCell)
			minimalCells = append(minimalCells, minCell)
			continue
		}
	}

	return minimalCells
}

// buildChildrenMap creates mapping from parent ID to list of child IDs.
//
// Algorithm:
//   - Iterate all cells
//   - For each node with a parent field: append node.Id to childrenMap[parent]
//
// Returns: map[parentId][]childIds
//
// Note: This builds the reverse direction of the parent relationship.
// Example: If node A has parent=B, this adds A to childrenMap[B].
func buildChildrenMap(cells []DfdDiagram_Cells_Item) map[string][]openapi_types.UUID {
	childrenMap := make(map[string][]openapi_types.UUID)

	for _, cellUnion := range cells {
		// Only nodes have parent relationships
		if node, err := cellUnion.AsNode(); err == nil && node.Parent != nil {
			parentID := node.Parent.String()
			childrenMap[parentID] = append(childrenMap[parentID], node.Id)
		}
	}

	return childrenMap
}

// transformNodeToMinimal converts Node to MinimalNode.
//
// Extracts:
//   - Basic properties: id, shape, parent
//   - Children: from childrenMap (computed bidirectional relationship)
//   - Labels: from node.Attrs.Text.Text + text-box children
//   - DataAssetId: from node.Data.AdditionalProperties["dataAssetId"]
//   - Metadata: from node.Data._metadata (flattened to map)
//
// All visual properties (attrs styling, ports, markup, position, size, etc.) are excluded.
func transformNodeToMinimal(node Node, childrenMap map[string][]openapi_types.UUID, cells []DfdDiagram_Cells_Item) MinimalNode {
	// Extract labels from node's own text attribute
	labels := extractNodeLabels(node)

	// Add labels from embedded text-box children
	textBoxLabels := extractTextBoxChildLabels(node.Id.String(), childrenMap, cells)
	labels = append(labels, textBoxLabels...)

	// Get computed children array for this node
	children := childrenMap[node.Id.String()]
	if children == nil {
		children = []openapi_types.UUID{} // Empty array if no children
	}

	// Extract dataAssetId from node.Data.AdditionalProperties if present
	var dataAssetId *openapi_types.UUID
	if node.Data != nil && node.Data.AdditionalProperties != nil {
		if assetIdVal, ok := node.Data.AdditionalProperties["dataAssetId"]; ok {
			if assetIdStr, ok := assetIdVal.(string); ok {
				if uuid, err := ParseUUID(assetIdStr); err == nil {
					dataAssetId = &uuid
				}
			}
		}
	}

	// Extract and flatten metadata from node.Data._metadata
	var metadata map[string]string
	if node.Data != nil && node.Data.Metadata != nil {
		metadata = flattenMetadata(node.Data.Metadata)
	} else {
		metadata = make(map[string]string)
	}

	// Convert shape to MinimalNodeShape
	shapeStr := string(node.Shape)

	return MinimalNode{
		Id:          node.Id,
		Shape:       MinimalNodeShape(shapeStr),
		Parent:      node.Parent,
		Children:    children,
		Labels:      labels,
		DataAssetId: dataAssetId,
		Metadata:    metadata,
	}
}

// extractNodeLabels extracts text from node.Attrs.Text.Text.
//
// Returns:
//   - Single-element array with text if present
//   - Empty array if attrs, text, or text value is nil
//
// Gracefully handles missing fields (no panics).
func extractNodeLabels(node Node) []string {
	labels := []string{}

	if node.Attrs != nil && node.Attrs.Text != nil && node.Attrs.Text.Text != nil {
		text := *node.Attrs.Text.Text
		if text != "" {
			labels = append(labels, text)
		}
	}

	return labels
}

// extractTextBoxChildLabels finds text-box children of a node and extracts their text.
//
// Algorithm:
//  1. Get child IDs for this parent from childrenMap
//  2. For each child ID, find the corresponding cell in cells array
//  3. Check if cell is a node with shape="text-box"
//  4. Extract text from text-box node's attrs
//
// Returns: Array of text strings from text-box children (empty if no text-boxes found).
//
// Performance: O(children * cells) - acceptable for typical diagrams (<1000 cells).
func extractTextBoxChildLabels(parentID string, childrenMap map[string][]openapi_types.UUID, cells []DfdDiagram_Cells_Item) []string {
	labels := []string{}

	// Get children IDs for this parent
	childIDs := childrenMap[parentID]
	if childIDs == nil {
		return labels
	}

	// Find each child cell and check if it's a text-box
	for _, cellUnion := range cells {
		if node, err := cellUnion.AsNode(); err == nil {
			// Check if this node is a child of our parent
			for _, childID := range childIDs {
				if node.Id == childID && node.Shape == "text-box" {
					// Extract text from text-box
					if node.Attrs != nil && node.Attrs.Text != nil && node.Attrs.Text.Text != nil {
						text := *node.Attrs.Text.Text
						if text != "" {
							labels = append(labels, text)
						}
					}
				}
			}
		}
	}

	return labels
}

// transformEdgeToMinimal converts Edge to MinimalEdge.
//
// Extracts:
//   - Basic properties: id, shape
//   - Connection: source, target (EdgeTerminal structs)
//   - Labels: from edge.Labels[].Attrs.Text
//   - DataAssetId: from edge.Data.AdditionalProperties["dataAssetId"]
//   - Metadata: from edge.Data._metadata (flattened to map)
//
// All visual properties (attrs styling, router, connector, vertices, etc.) are excluded.
func transformEdgeToMinimal(edge Edge) MinimalEdge {
	// Extract labels from edge's labels array
	labels := extractEdgeLabels(edge)

	// Extract dataAssetId from edge.Data.AdditionalProperties if present
	var dataAssetId *openapi_types.UUID
	if edge.Data != nil && edge.Data.AdditionalProperties != nil {
		if assetIdVal, ok := edge.Data.AdditionalProperties["dataAssetId"]; ok {
			if assetIdStr, ok := assetIdVal.(string); ok {
				if uuid, err := ParseUUID(assetIdStr); err == nil {
					dataAssetId = &uuid
				}
			}
		}
	}

	// Extract and flatten metadata from edge.Data._metadata
	var metadata map[string]string
	if edge.Data != nil && edge.Data.Metadata != nil {
		metadata = flattenMetadata(edge.Data.Metadata)
	} else {
		metadata = make(map[string]string)
	}

	return MinimalEdge{
		Id:          edge.Id,
		Shape:       MinimalEdgeShapeFlow,
		Source:      edge.Source,
		Target:      edge.Target,
		Labels:      labels,
		DataAssetId: dataAssetId,
		Metadata:    metadata,
	}
}

// extractEdgeLabels extracts text from edge.Labels[].Attrs.Text.
//
// Returns:
//   - Array of text strings from all labels
//   - Empty array if labels is nil or no text found
//
// Gracefully handles nil fields at each level.
func extractEdgeLabels(edge Edge) []string {
	labels := []string{}

	if edge.Labels != nil {
		for _, label := range *edge.Labels {
			if label.Attrs != nil && label.Attrs.Text != nil && label.Attrs.Text.Text != nil {
				text := *label.Attrs.Text.Text
				if text != "" {
					labels = append(labels, text)
				}
			}
		}
	}

	return labels
}

// serializeAsYAML converts MinimalDiagramModel to YAML format.
// First serializes to JSON to properly handle union types, then converts to YAML.
// Returns YAML bytes and any error encountered during marshaling.
func serializeAsYAML(model MinimalDiagramModel) ([]byte, error) {
	// First marshal to JSON to properly handle the MinimalCell union types
	// (MinimalCell uses json.RawMessage internally which yaml.Marshal doesn't handle)
	jsonBytes, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal model to JSON: %w", err)
	}

	// Unmarshal JSON into a generic interface for YAML conversion
	var generic interface{}
	if err := json.Unmarshal(jsonBytes, &generic); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON for YAML conversion: %w", err)
	}

	// Marshal the generic structure to YAML
	return yaml.Marshal(generic)
}

// serializeAsGraphML converts MinimalDiagramModel to GraphML XML format.
// GraphML is a standard graph format supported by tools like yEd, Gephi, and NetworkX.
//
// Returns GraphML XML bytes and any error encountered during generation.
func serializeAsGraphML(model MinimalDiagramModel) ([]byte, error) {
	// Build GraphML structure
	graphml := GraphML{
		XMLName:  xml.Name{Space: "http://graphml.graphdrawing.org/xmlns", Local: "graphml"},
		XMLNS:    "http://graphml.graphdrawing.org/xmlns",
		XMLNSXSI: "http://www.w3.org/2001/XMLSchema-instance",
		SchemaLocation: "http://graphml.graphdrawing.org/xmlns " +
			"http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd",
		Keys:  buildGraphMLKeys(),
		Graph: buildGraphMLGraph(model),
	}

	// Marshal to XML with proper formatting
	output, err := xml.MarshalIndent(graphml, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphML: %w", err)
	}

	// Add XML declaration
	result := []byte(xml.Header + string(output))
	return result, nil
}

// GraphML structure definitions for XML marshaling

type GraphML struct {
	XMLName        xml.Name     `xml:"graphml"`
	XMLNS          string       `xml:"xmlns,attr"`
	XMLNSXSI       string       `xml:"xmlns:xsi,attr"`
	SchemaLocation string       `xml:"xsi:schemaLocation,attr"`
	Keys           []GraphKey   `xml:"key"`
	Graph          GraphMLGraph `xml:"graph"`
}

type GraphKey struct {
	ID       string `xml:"id,attr"`
	For      string `xml:"for,attr"`
	AttrName string `xml:"attr.name,attr"`
	AttrType string `xml:"attr.type,attr"`
}

type GraphMLGraph struct {
	ID          string        `xml:"id,attr"`
	EdgeDefault string        `xml:"edgedefault,attr"`
	Data        []GraphData   `xml:"data"`
	Nodes       []GraphMLNode `xml:"node"`
	Edges       []GraphMLEdge `xml:"edge"`
}

type GraphMLNode struct {
	ID   string      `xml:"id,attr"`
	Data []GraphData `xml:"data"`
}

type GraphMLEdge struct {
	ID     string      `xml:"id,attr"`
	Source string      `xml:"source,attr"`
	Target string      `xml:"target,attr"`
	Data   []GraphData `xml:"data"`
}

type GraphData struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

// buildGraphMLKeys defines the schema for custom attributes in GraphML
func buildGraphMLKeys() []GraphKey {
	return []GraphKey{
		// Threat model graph-level keys
		{ID: "tm_id", For: "graph", AttrName: "threat_model_id", AttrType: "string"},
		{ID: "tm_name", For: "graph", AttrName: "threat_model_name", AttrType: "string"},
		{ID: "tm_desc", For: "graph", AttrName: "threat_model_description", AttrType: "string"},
		{ID: "tm_meta", For: "graph", AttrName: "threat_model_metadata", AttrType: "string"},

		// Node-level keys
		{ID: "shape", For: "node", AttrName: "shape", AttrType: "string"},
		{ID: "parent", For: "node", AttrName: "parent", AttrType: "string"},
		{ID: "children", For: "node", AttrName: "children", AttrType: "string"},
		{ID: "labels", For: "node", AttrName: "labels", AttrType: "string"},
		{ID: "dataAssetId", For: "node", AttrName: "dataAssetId", AttrType: "string"},
		{ID: "metadata", For: "node", AttrName: "metadata", AttrType: "string"},

		// Edge-level keys
		{ID: "labels_edge", For: "edge", AttrName: "labels", AttrType: "string"},
		{ID: "dataAssetId_edge", For: "edge", AttrName: "dataAssetId", AttrType: "string"},
		{ID: "metadata_edge", For: "edge", AttrName: "metadata", AttrType: "string"},
	}
}

// buildGraphMLGraph constructs the graph element with threat model context and all cells
func buildGraphMLGraph(model MinimalDiagramModel) GraphMLGraph {
	graph := GraphMLGraph{
		ID:          "G",
		EdgeDefault: "directed",
		Data:        buildThreatModelData(model),
		Nodes:       []GraphMLNode{},
		Edges:       []GraphMLEdge{},
	}

	// Add all cells (nodes and edges)
	for _, cellUnion := range model.Cells {
		// Use the discriminator to determine the cell type
		discriminator, err := cellUnion.Discriminator()
		if err != nil {
			continue // Skip cells we can't identify
		}

		// "edge" is the only edge discriminator value; all others are node shapes
		if discriminator == "edge" {
			edge, err := cellUnion.AsMinimalEdge()
			if err == nil {
				graph.Edges = append(graph.Edges, buildGraphMLEdge(edge))
			}
		} else {
			// Node shapes: actor, process, store, security-boundary, text-box
			node, err := cellUnion.AsMinimalNode()
			if err == nil {
				graph.Nodes = append(graph.Nodes, buildGraphMLNode(node))
			}
		}
	}

	return graph
}

// buildThreatModelData creates graph-level data elements for threat model context
func buildThreatModelData(model MinimalDiagramModel) []GraphData {
	metaJSON, _ := json.Marshal(model.Metadata)
	return []GraphData{
		{Key: "tm_id", Value: model.Id.String()},
		{Key: "tm_name", Value: model.Name},
		{Key: "tm_desc", Value: model.Description},
		{Key: "tm_meta", Value: string(metaJSON)},
	}
}

// buildGraphMLNode converts MinimalNode to GraphML node element
func buildGraphMLNode(node MinimalNode) GraphMLNode {
	data := []GraphData{
		{Key: "shape", Value: string(node.Shape)},
	}

	// Add parent if present
	if node.Parent != nil {
		data = append(data, GraphData{Key: "parent", Value: node.Parent.String()})
	}

	// Add children as JSON array
	if len(node.Children) > 0 {
		childrenJSON, _ := json.Marshal(node.Children)
		data = append(data, GraphData{Key: "children", Value: string(childrenJSON)})
	}

	// Add labels as JSON array
	if len(node.Labels) > 0 {
		labelsJSON, _ := json.Marshal(node.Labels)
		data = append(data, GraphData{Key: "labels", Value: string(labelsJSON)})
	}

	// Add dataAssetId if present
	if node.DataAssetId != nil {
		data = append(data, GraphData{Key: "dataAssetId", Value: node.DataAssetId.String()})
	}

	// Add metadata as JSON object
	if len(node.Metadata) > 0 {
		metaJSON, _ := json.Marshal(node.Metadata)
		data = append(data, GraphData{Key: "metadata", Value: string(metaJSON)})
	}

	return GraphMLNode{
		ID:   node.Id.String(),
		Data: data,
	}
}

// buildGraphMLEdge converts MinimalEdge to GraphML edge element
func buildGraphMLEdge(edge MinimalEdge) GraphMLEdge {
	data := []GraphData{}

	// Add labels as JSON array
	if len(edge.Labels) > 0 {
		labelsJSON, _ := json.Marshal(edge.Labels)
		data = append(data, GraphData{Key: "labels_edge", Value: string(labelsJSON)})
	}

	// Add dataAssetId if present
	if edge.DataAssetId != nil {
		data = append(data, GraphData{Key: "dataAssetId_edge", Value: edge.DataAssetId.String()})
	}

	// Add metadata as JSON object
	if len(edge.Metadata) > 0 {
		metaJSON, _ := json.Marshal(edge.Metadata)
		data = append(data, GraphData{Key: "metadata_edge", Value: string(metaJSON)})
	}

	// Get source and target cell IDs from EdgeTerminal
	sourceCell := edge.Source.Cell.String()
	targetCell := edge.Target.Cell.String()

	return GraphMLEdge{
		ID:     edge.Id.String(),
		Source: sourceCell,
		Target: targetCell,
		Data:   data,
	}
}

// parseFormat converts format parameter to lowercase for case-insensitive matching.
// Returns normalized format string and validation error if invalid.
func parseFormat(format *GetDiagramModelParamsFormat) (string, error) {
	if format == nil {
		return "json", nil
	}

	// Convert to lowercase for case-insensitive comparison
	normalized := strings.ToLower(string(*format))

	// Validate against allowed values
	switch normalized {
	case "json", "yaml", "graphml":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid format parameter: must be json, yaml, or graphml")
	}
}
