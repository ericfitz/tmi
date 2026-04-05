package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// JSONContentProvider extracts semantic text from DFD diagram JSON stored in DiagramStore.
type JSONContentProvider struct{}

// NewJSONContentProvider creates a new JSONContentProvider.
func NewJSONContentProvider() *JSONContentProvider {
	return &JSONContentProvider{}
}

// Name returns the provider name for logging.
func (p *JSONContentProvider) Name() string { return "json-dfd" }

// CanHandle returns true when the entity is a diagram with no external URI.
func (p *JSONContentProvider) CanHandle(_ context.Context, ref EntityReference) bool {
	return ref.EntityType == "diagram" && ref.URI == ""
}

// Extract reads the diagram from DiagramStore and converts its cells to human-readable text.
func (p *JSONContentProvider) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	logger := slogging.Get()

	if DiagramStore == nil {
		return ExtractedContent{}, fmt.Errorf("diagram store is not initialized")
	}

	diagram, err := DiagramStore.Get(ref.EntityID)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to get diagram %s: %w", ref.EntityID, err)
	}

	logger.Debug("Extracting text from diagram %s with %d cells", ref.EntityID, len(diagram.Cells))

	// Build a map from cell UUID to node label for resolving edge source/target names
	nodeLabels := make(map[string]string)
	var lines []string

	// First pass: collect node labels
	for _, cellItem := range diagram.Cells {
		disc, err := cellItem.Discriminator()
		if err != nil {
			continue
		}
		if disc == "flow" {
			continue
		}
		node, err := cellItem.AsNode()
		if err != nil {
			continue
		}
		label := nodeLabel(node)
		nodeLabels[node.Id.String()] = label
	}

	// Second pass: emit descriptions
	for _, cellItem := range diagram.Cells {
		disc, err := cellItem.Discriminator()
		if err != nil {
			logger.Debug("Skipping cell with discriminator error: %v", err)
			continue
		}

		switch disc {
		case "flow":
			edge, err := cellItem.AsEdge()
			if err != nil {
				continue
			}
			edgeName := edgeLabel(edge)
			srcName := nodeLabels[edge.Source.Cell.String()]
			tgtName := nodeLabels[edge.Target.Cell.String()]
			if srcName == "" {
				srcName = edge.Source.Cell.String()
			}
			if tgtName == "" {
				tgtName = edge.Target.Cell.String()
			}
			if edgeName != "" {
				lines = append(lines, fmt.Sprintf("Flow: %s (from %s to %s)", edgeName, srcName, tgtName))
			} else {
				lines = append(lines, fmt.Sprintf("Flow: (from %s to %s)", srcName, tgtName))
			}

		default:
			// Node shapes: actor, process, store, security-boundary, text-box
			node, err := cellItem.AsNode()
			if err != nil {
				continue
			}
			label := nodeLabels[node.Id.String()]
			switch node.Shape {
			case NodeShapeProcess:
				if label != "" {
					lines = append(lines, fmt.Sprintf("Process: %s", label))
				} else {
					lines = append(lines, "Process: (unnamed)")
				}
			case NodeShapeStore:
				if label != "" {
					lines = append(lines, fmt.Sprintf("Store: %s", label))
				} else {
					lines = append(lines, "Store: (unnamed)")
				}
			case NodeShapeActor:
				if label != "" {
					lines = append(lines, fmt.Sprintf("Actor: %s", label))
				} else {
					lines = append(lines, "Actor: (unnamed)")
				}
			case NodeShapeSecurityBoundary:
				if label != "" {
					lines = append(lines, fmt.Sprintf("Trust Boundary: %s", label))
				} else {
					lines = append(lines, "Trust Boundary: (unnamed)")
				}
			case NodeShapeTextBox:
				if label != "" {
					lines = append(lines, fmt.Sprintf("Note: %s", label))
				}
				// Skip unlabeled text boxes — they carry no semantic content
			default:
				if label != "" {
					lines = append(lines, fmt.Sprintf("%s: %s", strings.Title(string(node.Shape)), label)) //nolint:staticcheck
				}
			}
		}
	}

	title := diagram.Name
	if ref.Name != "" {
		title = ref.Name
	}

	return ExtractedContent{
		Text:        strings.Join(lines, "\n"),
		Title:       title,
		ContentType: "application/json",
	}, nil
}

// nodeLabel extracts the display label from a Node's Attrs.Text.Text field.
func nodeLabel(node Node) string {
	if node.Attrs == nil {
		return ""
	}
	if node.Attrs.Text == nil {
		return ""
	}
	if node.Attrs.Text.Text == nil {
		return ""
	}
	return strings.TrimSpace(*node.Attrs.Text.Text)
}

// edgeLabel extracts the display label from an Edge's Labels or DefaultLabel field.
func edgeLabel(edge Edge) string {
	// Check Labels array first
	if edge.Labels != nil {
		for _, lbl := range *edge.Labels {
			if lbl.Attrs != nil && lbl.Attrs.Text != nil && lbl.Attrs.Text.Text != nil {
				text := strings.TrimSpace(*lbl.Attrs.Text.Text)
				if text != "" {
					return text
				}
			}
		}
	}
	// Fall back to DefaultLabel
	if edge.DefaultLabel != nil && edge.DefaultLabel.Attrs != nil &&
		edge.DefaultLabel.Attrs.Text != nil && edge.DefaultLabel.Attrs.Text.Text != nil {
		return strings.TrimSpace(*edge.DefaultLabel.Attrs.Text.Text)
	}
	return ""
}
