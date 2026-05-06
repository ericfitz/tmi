package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
)

// EntityReference identifies a source entity for content extraction.
// For DB-resident content (notes, assets), URI is empty and the provider
// reads directly from the database using EntityType + EntityID.
// For external content (documents with URLs), URI is the fetch target.
type EntityReference struct {
	EntityType string // "asset", "threat", "document", "note", "diagram", "repository"
	EntityID   string // UUID of the source entity
	URI        string // External URL (empty for DB-resident content)
	Name       string // Display name for progress reporting
}

// ExtractedContent holds the text extracted from a source entity
type ExtractedContent struct {
	Text        string            // Extracted plain text
	Title       string            // Document title if available
	ContentType string            // Original content type (e.g., "application/pdf")
	Metadata    map[string]string // Provider-specific metadata
}

// EmbeddingSource extracts plain text from source entities for embedding
type EmbeddingSource interface {
	// Name returns the provider name for logging
	Name() string
	// CanHandle returns true if this provider can extract content from the given entity
	CanHandle(ctx context.Context, ref EntityReference) bool
	// Extract fetches and returns plain text content
	Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error)
}

// EmbeddingSourceRegistry manages embedding sources in priority order
type EmbeddingSourceRegistry struct {
	providers []EmbeddingSource
}

// NewEmbeddingSourceRegistry creates a new registry
func NewEmbeddingSourceRegistry() *EmbeddingSourceRegistry {
	return &EmbeddingSourceRegistry{}
}

// Register adds a provider to the registry (providers are tried in registration order)
func (r *EmbeddingSourceRegistry) Register(provider EmbeddingSource) {
	r.providers = append(r.providers, provider)
}

// Extract finds the first provider that can handle the entity and extracts its content
func (r *EmbeddingSourceRegistry) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	logger := slogging.Get()
	for _, p := range r.providers {
		if p.CanHandle(ctx, ref) {
			logger.Debug("Using embedding source %s for entity %s/%s", p.Name(), ref.EntityType, ref.EntityID)
			return p.Extract(ctx, ref)
		}
	}
	return ExtractedContent{}, fmt.Errorf("no embedding source can handle entity type=%s id=%s uri=%s", ref.EntityType, ref.EntityID, ref.URI)
}

// PipelineEmbeddingSource adapts the two-layer ContentPipeline to the
// existing EmbeddingSource interface, bridging old and new code.
type PipelineEmbeddingSource struct {
	pipeline *ContentPipeline
}

// NewPipelineEmbeddingSource creates an adapter.
func NewPipelineEmbeddingSource(pipeline *ContentPipeline) *PipelineEmbeddingSource {
	return &PipelineEmbeddingSource{pipeline: pipeline}
}

// Name returns the adapter name.
func (p *PipelineEmbeddingSource) Name() string { return "pipeline" }

// CanHandle returns true for entity references with a URI.
func (p *PipelineEmbeddingSource) CanHandle(_ context.Context, ref EntityReference) bool {
	return ref.URI != ""
}

// Extract delegates to the content pipeline.
func (p *PipelineEmbeddingSource) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	result, err := p.pipeline.Extract(ctx, ref.URI)
	if err != nil {
		return ExtractedContent{}, err
	}
	if result.Title == "" {
		result.Title = ref.Name
	}
	return result, nil
}
