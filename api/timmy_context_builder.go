package api

import (
	"fmt"
	"strings"
)

// ContextBuilder constructs LLM context from structured data and vector search results
type ContextBuilder struct{}

// NewContextBuilder creates a new ContextBuilder
func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{}
}

// BuildTier1Context creates a structured overview of the threat model.
// This is a placeholder that formats entity names and descriptions.
// The full implementation will read from stores to get all entity details.
func (cb *ContextBuilder) BuildTier1Context(entitySummaries []EntitySummary) string {
	if len(entitySummaries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Threat Model Overview\n\n")

	// Group by entity type
	groups := make(map[string][]EntitySummary)
	for _, es := range entitySummaries {
		groups[es.EntityType] = append(groups[es.EntityType], es)
	}

	typeOrder := []string{"asset", "threat", "diagram", "document", "note", "repository"}
	typeLabels := map[string]string{
		"asset":      "Assets",
		"threat":     "Threats",
		"diagram":    "Diagrams",
		"document":   "Documents",
		"note":       "Notes",
		"repository": "Repositories",
	}

	for _, et := range typeOrder {
		entities, ok := groups[et]
		if !ok || len(entities) == 0 {
			continue
		}
		label := typeLabels[et]
		if label == "" {
			label = et
		}
		fmt.Fprintf(&sb, "### %s\n\n", label)
		for _, e := range entities {
			fmt.Fprintf(&sb, "- **%s**", e.Name)
			if e.Description != "" {
				fmt.Fprintf(&sb, ": %s", e.Description)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildTier2Context performs vector search and formats results with source attribution
func (cb *ContextBuilder) BuildTier2Context(index *VectorIndex, queryVector []float32, topK int) string {
	if index == nil || topK <= 0 {
		return ""
	}

	results := index.Search(queryVector, topK)
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Relevant Source Material\n\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "### Source %d (relevance: %.2f)\n", i+1, r.Similarity)
		sb.WriteString(r.ChunkText)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// BuildTier2ContextFromResults formats pre-searched (and optionally reranked) vector search results
// into tier 2 context for the LLM prompt.
func (cb *ContextBuilder) BuildTier2ContextFromResults(results []VectorSearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Relevant Source Material\n\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "### Source %d (relevance: %.2f)\n", i+1, r.Similarity)
		sb.WriteString(r.ChunkText)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// BuildFullContext assembles the complete system prompt with context
func (cb *ContextBuilder) BuildFullContext(basePrompt, tier1, tier2 string) string {
	var sb strings.Builder
	sb.WriteString(basePrompt)

	if tier1 != "" {
		sb.WriteString("\n\n---\n\n")
		sb.WriteString(tier1)
	}

	if tier2 != "" {
		sb.WriteString("\n\n---\n\n")
		sb.WriteString(tier2)
	}

	return sb.String()
}

// EntitySummary holds a brief summary of an entity for Tier 1 context
type EntitySummary struct {
	EntityType  string
	EntityID    string
	Name        string
	Description string
	// Additional fields for specific entity types
	Extra map[string]string // e.g., "severity" for threats, "type" for assets
}
