package api

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gopkg.in/yaml.v3"

	"github.com/ericfitz/tmi/internal/slogging"
)

// buildMinimalDiagramModel transforms full threat model and diagram into minimal model representation.
// Extracts threat model context and transforms cells to minimal format without visual properties.
// Fetches referenced assets from the asset store to include in the model.
// SEM@f7d829c2058f4f0be9f76648be2cbcfc3501f485: build a minimal diagram model by combining threat model metadata, cells, and resolved assets (reads DB)
func buildMinimalDiagramModel(ctx context.Context, tm ThreatModel, diagram DfdDiagram, assetStore AssetRepository) MinimalDiagramModel {
	// Flatten threat model metadata from array to map
	tmMetadata := flattenMetadata(tm.Metadata)

	// Get description (may be nil)
	description := ""
	if tm.Description != nil {
		description = *tm.Description
	}

	// Transform diagram cells to minimal representation
	minimalCells := transformCellsToMinimal(diagram.Cells)

	// Collect and fetch referenced assets
	assets := collectReferencedAssets(ctx, minimalCells, assetStore)

	return MinimalDiagramModel{
		Id:          *tm.Id,
		Name:        tm.Name,
		Description: description,
		Metadata:    tmMetadata,
		Cells:       minimalCells,
		Assets:      assets,
	}
}

// collectReferencedAssets extracts unique asset IDs from cells and fetches the corresponding Asset objects.
// Returns only the assets that are successfully retrieved; missing assets are logged but not included.
// SEM@f7d829c2058f4f0be9f76648be2cbcfc3501f485: fetch all unique assets referenced by diagram cells from the asset store (reads DB)
func collectReferencedAssets(ctx context.Context, cells []MinimalCell, assetStore AssetRepository) []Asset {
	if assetStore == nil {
		return []Asset{}
	}

	// Collect unique asset IDs from all cells
	assetIDSet := make(map[string]struct{})
	for _, cell := range cells {
		// Try to get dataAssetIds from either MinimalNode or MinimalEdge
		var dataAssetIds *[]openapi_types.UUID

		// Check if it's a MinimalNode
		if node, err := cell.AsMinimalNode(); err == nil && node.DataAssetIds != nil {
			dataAssetIds = node.DataAssetIds
		}
		// Check if it's a MinimalEdge
		if edge, err := cell.AsMinimalEdge(); err == nil && edge.DataAssetIds != nil {
			dataAssetIds = edge.DataAssetIds
		}

		if dataAssetIds != nil {
			for _, assetID := range *dataAssetIds {
				assetIDSet[assetID.String()] = struct{}{}
			}
		}
	}

	// Fetch each unique asset
	assets := make([]Asset, 0, len(assetIDSet))
	for assetID := range assetIDSet {
		asset, err := assetStore.Get(ctx, assetID)
		if err != nil {
			slogging.Get().Warn("Failed to fetch referenced asset: assetId=%s, error=%v", assetID, err)
			continue
		}
		if asset != nil {
			assets = append(assets, *asset)
		}
	}

	return assets
}

// flattenMetadata converts []Metadata array format to map[string]string.
// Used for both threat model metadata and cell metadata.
//
// Example: [{"key": "env", "value": "prod"}] → {"env": "prod"}
// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: convert a metadata key-value array to a flat string map (pure)
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
// SEM@30604730ee54403d31d30e94debd8c9646ab3356: convert full diagram cells to their minimal node and edge representations (pure)
func transformCellsToMinimal(cells []DfdDiagram_Cells_Item) []MinimalCell {
	// Initialize as empty slice (not nil) to ensure JSON serialization produces []
	// instead of null, which is required by the OpenAPI schema
	minimalCells := make([]MinimalCell, 0)

	// Return empty array if no cells
	if cells == nil {
		return minimalCells
	}

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
// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: build a parent-to-children ID index from a list of diagram cells (pure)
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
// SEM@7bac1ed632ff8929eff543daec4372c53d51283a: convert a full diagram node to its minimal representation with labels and metadata (pure)
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

	// Extract data_assets from node.Data.DataAssets if present
	// Note: text-box and security-boundary shapes should not have data_assets in minimal models
	var dataAssetIds *[]openapi_types.UUID
	if node.Shape != NodeShapeTextBox && node.Shape != NodeShapeSecurityBoundary {
		if node.Data != nil && node.Data.DataAssets != nil && len(*node.Data.DataAssets) > 0 {
			dataAssetIds = node.Data.DataAssets
		}
	}

	// Extract and flatten metadata from node.Data._metadata
	var metadata map[string]string
	if node.Data != nil && node.Data.UnderscoreMetadata != nil {
		metadata = flattenMetadata(node.Data.UnderscoreMetadata)
	} else {
		metadata = make(map[string]string)
	}

	// Determine security_boundary: true if shape is "security-boundary" OR if explicitly set in cell data
	securityBoundary := node.Shape == NodeShapeSecurityBoundary
	if !securityBoundary && node.Data != nil && node.Data.SecurityBoundary != nil && *node.Data.SecurityBoundary {
		securityBoundary = true
	}

	// Convert shape to MinimalNodeShape
	shapeStr := string(node.Shape)

	return MinimalNode{
		Id:               node.Id,
		Shape:            MinimalNodeShape(shapeStr),
		Parent:           node.Parent,
		Children:         children,
		Labels:           labels,
		DataAssetIds:     dataAssetIds,
		Metadata:         metadata,
		SecurityBoundary: securityBoundary,
	}
}

// extractNodeLabels extracts text from node.Attrs.Text.Text.
//
// Returns:
//   - Single-element array with text if present
//   - Empty array if attrs, text, or text value is nil
//
// Gracefully handles missing fields (no panics).
// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: extract visible text labels from a diagram node's attributes (pure)
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
// SEM@cdbe48c974fb76e1161972733b30bb0d1c02c3b1: extract text labels from text-box child cells of a parent node (pure)
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
				if node.Id == childID && node.Shape == NodeShapeTextBox {
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
// SEM@7bac1ed632ff8929eff543daec4372c53d51283a: convert a full diagram edge to its minimal representation with labels and metadata (pure)
func transformEdgeToMinimal(edge Edge) MinimalEdge {
	// Extract labels from edge's labels array
	labels := extractEdgeLabels(edge)

	// Extract data_assets from edge.Data.DataAssets if present
	var dataAssetIds *[]openapi_types.UUID
	if edge.Data != nil && edge.Data.DataAssets != nil && len(*edge.Data.DataAssets) > 0 {
		dataAssetIds = edge.Data.DataAssets
	}

	// Extract and flatten metadata from edge.Data._metadata
	var metadata map[string]string
	if edge.Data != nil && edge.Data.UnderscoreMetadata != nil {
		metadata = flattenMetadata(edge.Data.UnderscoreMetadata)
	} else {
		metadata = make(map[string]string)
	}

	return MinimalEdge{
		Id:           edge.Id,
		Shape:        MinimalEdgeShapeFlow,
		Source:       edge.Source,
		Target:       edge.Target,
		Labels:       labels,
		DataAssetIds: dataAssetIds,
		Metadata:     metadata,
	}
}

// extractEdgeLabels extracts text from edge.Labels[].Attrs.Text.
//
// Returns:
//   - Array of text strings from all labels
//   - Empty array if labels is nil or no text found
//
// Gracefully handles nil fields at each level.
// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: extract visible text labels from a diagram edge's labels array (pure)
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
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: serialize a minimal diagram model to YAML via JSON intermediate form (pure)
func serializeAsYAML(model MinimalDiagramModel) ([]byte, error) {
	// First marshal to JSON to properly handle the MinimalCell union types
	// (MinimalCell uses json.RawMessage internally which yaml.Marshal doesn't handle)
	jsonBytes, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal model to JSON: %w", err)
	}

	// Unmarshal JSON into a generic interface for YAML conversion
	var generic any
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
// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: serialize a minimal diagram model to GraphML XML format (pure)
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

// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: XML root element struct for a GraphML document (pure)
type GraphML struct {
	XMLName        xml.Name     `xml:"graphml"`
	XMLNS          string       `xml:"xmlns,attr"`
	XMLNSXSI       string       `xml:"xmlns:xsi,attr"`
	SchemaLocation string       `xml:"xsi:schemaLocation,attr"`
	Keys           []GraphKey   `xml:"key"`
	Graph          GraphMLGraph `xml:"graph"`
}

// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: GraphML key declaration describing an attribute schema for nodes, edges, or graphs (pure)
type GraphKey struct {
	ID       string `xml:"id,attr"`
	For      string `xml:"for,attr"`
	AttrName string `xml:"attr.name,attr"`
	AttrType string `xml:"attr.type,attr"`
}

// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: GraphML graph element containing nodes, edges, and graph-level data attributes (pure)
type GraphMLGraph struct {
	ID          string        `xml:"id,attr"`
	EdgeDefault string        `xml:"edgedefault,attr"`
	Data        []GraphData   `xml:"data"`
	Nodes       []GraphMLNode `xml:"node"`
	Edges       []GraphMLEdge `xml:"edge"`
}

// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: GraphML node element with id and associated data attributes (pure)
type GraphMLNode struct {
	ID   string      `xml:"id,attr"`
	Data []GraphData `xml:"data"`
}

// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: GraphML edge element with source, target, and associated data attributes (pure)
type GraphMLEdge struct {
	ID     string      `xml:"id,attr"`
	Source string      `xml:"source,attr"`
	Target string      `xml:"target,attr"`
	Data   []GraphData `xml:"data"`
}

// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: GraphML data element holding a keyed string value for a graph, node, or edge (pure)
type GraphData struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

// buildGraphMLKeys defines the schema for custom attributes in GraphML
// SEM@91af4833f48bf86029c557aceb77e3ad9fe3b2d8: build the standard GraphML key declarations for threat model, node, and edge attributes (pure)
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
		{ID: "securityBoundary", For: "node", AttrName: "securityBoundary", AttrType: "boolean"},

		// Edge-level keys
		{ID: "labels_edge", For: "edge", AttrName: "labels", AttrType: "string"},
		{ID: "dataAssetId_edge", For: "edge", AttrName: "dataAssetId", AttrType: "string"},
		{ID: "metadata_edge", For: "edge", AttrName: "metadata", AttrType: "string"},
	}
}

// buildGraphMLGraph constructs the graph element with threat model context and all cells
// SEM@06614a5c21526df142f90d2848fa7ba794a8f8d2: build a GraphML graph element from a minimal diagram model (pure)
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
// SEM@6e3a086017c90a6a018fd799308443eb877dd8a5: build graph-level GraphML data elements for threat model identity and metadata (pure)
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
// SEM@015620d2448a93ae93203eb25a08967e816b1a74: build a GraphML node element from a minimal node with shape, labels, and metadata (pure)
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

	// Add data_asset_ids if present
	if node.DataAssetIds != nil && len(*node.DataAssetIds) > 0 {
		assetIdsJSON, _ := json.Marshal(node.DataAssetIds)
		data = append(data, GraphData{Key: "data_asset_ids", Value: string(assetIdsJSON)})
	}

	// Add metadata as JSON object
	if len(node.Metadata) > 0 {
		metaJSON, _ := json.Marshal(node.Metadata)
		data = append(data, GraphData{Key: "metadata", Value: string(metaJSON)})
	}

	// Add security boundary flag (derived from shape)
	if node.Shape == MinimalNodeShapeSecurityBoundary {
		data = append(data, GraphData{Key: "securityBoundary", Value: "true"})
	} else {
		data = append(data, GraphData{Key: "securityBoundary", Value: "false"})
	}

	return GraphMLNode{
		ID:   node.Id.String(),
		Data: data,
	}
}

// buildGraphMLEdge converts MinimalEdge to GraphML edge element
// SEM@015620d2448a93ae93203eb25a08967e816b1a74: build a GraphML edge element from a minimal edge with labels and metadata (pure)
func buildGraphMLEdge(edge MinimalEdge) GraphMLEdge {
	data := []GraphData{}

	// Add labels as JSON array
	if len(edge.Labels) > 0 {
		labelsJSON, _ := json.Marshal(edge.Labels)
		data = append(data, GraphData{Key: "labels_edge", Value: string(labelsJSON)})
	}

	// Add data_asset_ids if present
	if edge.DataAssetIds != nil && len(*edge.DataAssetIds) > 0 {
		assetIdsJSON, _ := json.Marshal(edge.DataAssetIds)
		data = append(data, GraphData{Key: "data_asset_ids_edge", Value: string(assetIdsJSON)})
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

// Diagram-model response formats and their canonical (modern) media types.
const (
	diagramFormatJSON    = "json"
	diagramFormatYAML    = "yaml"
	diagramFormatGraphML = "graphml"

	diagramMediaTypeJSON    = "application/json"
	diagramMediaTypeYAML    = "application/yaml"        // RFC 9512
	diagramMediaTypeGraphML = "application/graphml+xml" // GraphML over XML
)

// negotiateFormat resolves the diagram-model response format from the request's
// Accept header. Offered media types, in server-preference order:
// application/json (default), application/yaml, application/graphml+xml.
// Returns the normalized format ("json", "yaml", or "graphml"), or an error
// (the caller maps it to 406 Not Acceptable) when the Accept header matches
// none of the offered types.
func negotiateFormat(c *gin.Context) (string, error) {
	chosen, ok := negotiateContentType(
		c.GetHeader("Accept"),
		[]string{diagramMediaTypeJSON, diagramMediaTypeYAML, diagramMediaTypeGraphML},
	)
	if !ok {
		return "", fmt.Errorf(
			"no acceptable response media type; offered: %s, %s, %s",
			diagramMediaTypeJSON, diagramMediaTypeYAML, diagramMediaTypeGraphML,
		)
	}
	switch chosen {
	case diagramMediaTypeYAML:
		return diagramFormatYAML, nil
	case diagramMediaTypeGraphML:
		return diagramFormatGraphML, nil
	default:
		return diagramFormatJSON, nil
	}
}
