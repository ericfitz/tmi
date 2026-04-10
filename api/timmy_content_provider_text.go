package api

import (
	"context"
	"fmt"
	"strings"
)

// DirectTextProvider extracts text from DB-resident entities (notes, assets, threats, repositories)
type DirectTextProvider struct{}

// NewDirectTextProvider creates a new DirectTextProvider
func NewDirectTextProvider() *DirectTextProvider {
	return &DirectTextProvider{}
}

// Name returns the provider name for logging
func (p *DirectTextProvider) Name() string {
	return "direct-text"
}

// CanHandle returns true for DB-resident entities that don't have an external URI
func (p *DirectTextProvider) CanHandle(_ context.Context, ref EntityReference) bool {
	if ref.URI != "" {
		return false
	}
	switch ref.EntityType {
	case "note",
		string(AuditEntryObjectTypeAsset),
		string(AuditEntryObjectTypeThreat),
		string(AuditEntryObjectTypeRepository):
		return true
	default:
		return false
	}
}

// Extract fetches and returns plain text content from the entity
func (p *DirectTextProvider) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	var text string
	var title string

	switch ref.EntityType {
	case "note":
		note, err := GlobalNoteStore.Get(ctx, ref.EntityID)
		if err != nil {
			return ExtractedContent{}, fmt.Errorf("failed to get note %s: %w", ref.EntityID, err)
		}
		title = note.Name
		var parts []string
		parts = append(parts, fmt.Sprintf("Note: %s", note.Name))
		if note.Description != nil {
			parts = append(parts, *note.Description)
		}
		parts = append(parts, note.Content)
		text = strings.Join(parts, "\n\n")

	case "asset":
		asset, err := GlobalAssetStore.Get(ctx, ref.EntityID)
		if err != nil {
			return ExtractedContent{}, fmt.Errorf("failed to get asset %s: %w", ref.EntityID, err)
		}
		title = asset.Name
		var parts []string
		parts = append(parts, fmt.Sprintf("Asset: %s (type: %s)", asset.Name, asset.Type))
		if asset.Description != nil {
			parts = append(parts, *asset.Description)
		}
		if asset.Criticality != nil {
			parts = append(parts, fmt.Sprintf("Criticality: %s", *asset.Criticality))
		}
		text = strings.Join(parts, "\n")

	case "threat":
		threat, err := GlobalThreatStore.Get(ctx, ref.EntityID)
		if err != nil {
			return ExtractedContent{}, fmt.Errorf("failed to get threat %s: %w", ref.EntityID, err)
		}
		title = threat.Name
		var parts []string
		parts = append(parts, fmt.Sprintf("Threat: %s", threat.Name))
		if threat.Description != nil {
			parts = append(parts, *threat.Description)
		}
		if threat.Severity != nil {
			parts = append(parts, fmt.Sprintf("Severity: %s", *threat.Severity))
		}
		if threat.Mitigation != nil {
			parts = append(parts, fmt.Sprintf("Mitigation: %s", *threat.Mitigation))
		}
		text = strings.Join(parts, "\n")

	case "repository":
		repo, err := GlobalRepositoryStore.Get(ctx, ref.EntityID)
		if err != nil {
			return ExtractedContent{}, fmt.Errorf("failed to get repository %s: %w", ref.EntityID, err)
		}
		if repo.Name != nil {
			title = *repo.Name
		}
		var parts []string
		if repo.Name != nil {
			parts = append(parts, fmt.Sprintf("Repository: %s", *repo.Name))
		} else {
			parts = append(parts, "Repository")
		}
		if repo.Description != nil {
			parts = append(parts, *repo.Description)
		}
		if repo.Uri != "" {
			parts = append(parts, fmt.Sprintf("URI: %s", repo.Uri))
		}
		text = strings.Join(parts, "\n")

	default:
		return ExtractedContent{}, fmt.Errorf("unsupported entity type for direct text: %s", ref.EntityType)
	}

	return ExtractedContent{
		Text:        text,
		Title:       title,
		ContentType: "text/plain",
	}, nil
}
