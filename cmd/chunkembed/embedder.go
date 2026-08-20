package main

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/llm"
	"github.com/ericfitz/tmi/internal/worker"
)

// embedConfig is tmi-chunk-embed's embedding configuration, read from env.
// In Plan 3 / #415 this is replaced by the projected shared-config object so
// the worker and the monolith's Timmy query path cannot diverge.
// SEM@ef969bb79ad525fa5038847af0fb0be1038ae961: embedding model configuration struct holding model name, base URL, and API key (pure)
type embedConfig struct {
	Model   string
	BaseURL string
	APIKey  string
}

// embedConfigFromEnv reads the embedding config. Model and BaseURL come from
// the CR spec.config; APIKey comes from a secretRef-injected env var.
// SEM@ef969bb79ad525fa5038847af0fb0be1038ae961: build embedding configuration from required environment variables (pure)
func embedConfigFromEnv() (embedConfig, error) {
	model, err := worker.MustEnv("TMI_EMBEDDING_MODEL")
	if err != nil {
		return embedConfig{}, err
	}
	baseURL, err := worker.MustEnv("TMI_EMBEDDING_BASE_URL")
	if err != nil {
		return embedConfig{}, err
	}
	apiKey, err := worker.MustEnv("TMI_EMBEDDING_API_KEY")
	if err != nil {
		return embedConfig{}, err
	}
	return embedConfig{Model: model, BaseURL: baseURL, APIKey: apiKey}, nil
}

// newEmbedder builds an OpenAI embedder via internal/llm. chunkembed stays
// OpenAI-only permanently — unlike chat, embeddings have no Anthropic
// equivalent (Anthropic does not offer an embeddings API), so there is no
// phase 2 provider seam to preserve here.
// SEM@0000000000000000000000000000000000000000: build an OpenAI embedder from the given config (pure)
func newEmbedder(cfg embedConfig) (llm.Embedder, error) {
	emb, err := llm.NewEmbedder(llm.Config{
		Provider: llm.ProviderOpenAI,
		Model:    cfg.Model,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("chunkembed: build embedder: %w", err)
	}
	return emb, nil
}

// embedChunks embeds every chunk, returning one vector per chunk in order.
// SEM@0000000000000000000000000000000000000000: compute embedding vectors for a slice of text chunks in order (reads DB)
func embedChunks(ctx context.Context, emb llm.Embedder, chunks []string) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	vectors, err := emb.EmbedDocuments(ctx, chunks)
	if err != nil {
		return nil, fmt.Errorf("chunkembed: embed documents: %w", err)
	}
	return vectors, nil
}
