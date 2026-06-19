package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// EmbeddingSource extracts plain text from source entities for embedding
// SEM@0551ca168efde46f1a10164b714a260d65a9d52a: interface for extracting plain text from an entity reference for embedding (pure)
type EmbeddingSource interface {
	// Name returns the provider name for logging
	Name() string
	// CanHandle returns true if this provider can extract content from the given entity
	CanHandle(ctx context.Context, ref EntityReference) bool
	// Extract fetches and returns plain text content
	Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error)
}

// EmbeddingSourceRegistry manages embedding sources in priority order
// SEM@80346558ce851de593c85a2d5660f92a649b1686: ordered registry of embedding sources tried in registration priority (pure)
type EmbeddingSourceRegistry struct {
	providers []EmbeddingSource
}

// NewEmbeddingSourceRegistry creates a new registry
// SEM@80346558ce851de593c85a2d5660f92a649b1686: build an empty EmbeddingSourceRegistry (pure)
func NewEmbeddingSourceRegistry() *EmbeddingSourceRegistry {
	return &EmbeddingSourceRegistry{}
}

// Register adds a provider to the registry (providers are tried in registration order)
// SEM@80346558ce851de593c85a2d5660f92a649b1686: append an embedding source provider to the registry in priority order (mutates shared state)
func (r *EmbeddingSourceRegistry) Register(provider EmbeddingSource) {
	r.providers = append(r.providers, provider)
}

// Extract finds the first provider that can handle the entity and extracts its content
// SEM@80346558ce851de593c85a2d5660f92a649b1686: dispatch to the first capable provider and extract text content from an entity reference (reads DB)
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
// SEM@dc7c0088604cc1a0974d51f13e10814917275372: adapter wrapping a fixed ContentPipeline as an EmbeddingSource (pure)
type PipelineEmbeddingSource struct {
	pipeline *ContentPipeline
}

// NewPipelineEmbeddingSource creates an adapter.
// SEM@80346558ce851de593c85a2d5660f92a649b1686: build a PipelineEmbeddingSource adapter over a given ContentPipeline (pure)
func NewPipelineEmbeddingSource(pipeline *ContentPipeline) *PipelineEmbeddingSource {
	return &PipelineEmbeddingSource{pipeline: pipeline}
}

// LivePipelineEmbeddingSource is like PipelineEmbeddingSource but resolves the
// content pipeline from a ContentSourceHolder on each call, so the embedding
// source stays current when the content-source registry is rebuilt at runtime.
// This avoids stale-pipeline reads when content sources are toggled without a
// restart.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: embedding source that resolves its content pipeline from a holder on each call to stay current (pure)
type LivePipelineEmbeddingSource struct {
	holder *ContentSourceHolder
}

// NewLivePipelineEmbeddingSource creates an embedding source that resolves its
// pipeline from holder.Get on each call.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: build an embedding source that resolves its content pipeline live on each call (pure)
func NewLivePipelineEmbeddingSource(holder *ContentSourceHolder) *LivePipelineEmbeddingSource {
	return &LivePipelineEmbeddingSource{holder: holder}
}

// Name returns the adapter name.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: return the provider name for the live pipeline embedding source (pure)
func (l *LivePipelineEmbeddingSource) Name() string { return "pipeline" }

// CanHandle returns true for entity references with a URI.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: report whether an entity reference has a URI and can be extracted (pure)
func (l *LivePipelineEmbeddingSource) CanHandle(_ context.Context, ref EntityReference) bool {
	return ref.URI != ""
}

// Extract resolves the live pipeline from the holder and delegates extraction.
// If the holder has no bundle or the bundle has no pipeline, returns an error.
// SEM@80346558ce851de593c85a2d5660f92a649b1686: fetch and extract text content via the live content pipeline for a given entity reference
func (l *LivePipelineEmbeddingSource) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	bundle, err := l.holder.Get(ctx)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("content source holder unavailable: %w", err)
	}
	if bundle == nil || bundle.Pipeline == nil {
		return ExtractedContent{}, fmt.Errorf("content pipeline not available")
	}
	p := bundle.Pipeline
	var (
		result ExtractedContent
		extErr error
	)
	if ref.EntityType == "document" && ref.EntityID != "" {
		docID, parseErr := uuid.Parse(ref.EntityID)
		if parseErr != nil {
			result, extErr = p.Extract(ctx, ref.URI)
		} else {
			doc := Document{Id: &docID, Name: ref.Name, Uri: ref.URI}
			result, extErr = p.ExtractForDocument(ctx, doc)
		}
	} else {
		result, extErr = p.Extract(ctx, ref.URI)
	}
	if extErr != nil {
		return ExtractedContent{}, extErr
	}
	if result.Title == "" {
		result.Title = ref.Name
	}
	return result, nil
}

// Name returns the adapter name.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: return the provider name for the static pipeline embedding source (pure)
func (p *PipelineEmbeddingSource) Name() string { return "pipeline" }

// CanHandle returns true for entity references with a URI.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: report whether an entity reference has a URI and can be extracted (pure)
func (p *PipelineEmbeddingSource) CanHandle(_ context.Context, ref EntityReference) bool {
	return ref.URI != ""
}

// Extract delegates to the content pipeline. For document entities the
// document-aware variant is used so the dev/test-only extracted-text dump
// hook (#337) fires; for non-document URI-bearing entities the plain
// Extract path is sufficient.
// SEM@80346558ce851de593c85a2d5660f92a649b1686: fetch and extract text content via the content pipeline for a given entity reference
func (p *PipelineEmbeddingSource) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	var (
		result ExtractedContent
		err    error
	)
	if ref.EntityType == "document" && ref.EntityID != "" {
		docID, parseErr := uuid.Parse(ref.EntityID)
		if parseErr != nil {
			// Fall through to plain Extract; the dump hook is best-effort.
			result, err = p.pipeline.Extract(ctx, ref.URI)
		} else {
			doc := Document{Id: &docID, Name: ref.Name, Uri: ref.URI}
			result, err = p.pipeline.ExtractForDocument(ctx, doc)
		}
	} else {
		result, err = p.pipeline.Extract(ctx, ref.URI)
	}
	if err != nil {
		return ExtractedContent{}, err
	}
	if result.Title == "" {
		result.Title = ref.Name
	}
	return result, nil
}
