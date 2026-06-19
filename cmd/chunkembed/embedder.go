package main

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms/openai"
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

// newEmbedder builds an OpenAI-compatible langchaingo embedder.
// SEM@ef969bb79ad525fa5038847af0fb0be1038ae961: build an OpenAI-compatible langchaingo embedder from the given config (pure)
func newEmbedder(cfg embedConfig) (embeddings.Embedder, error) {
	llm, err := openai.New(
		openai.WithModel(cfg.Model),
		openai.WithEmbeddingModel(cfg.Model),
		openai.WithBaseURL(cfg.BaseURL),
		openai.WithToken(cfg.APIKey),
	)
	if err != nil {
		return nil, fmt.Errorf("chunkembed: build embedding LLM: %w", err)
	}
	emb, err := embeddings.NewEmbedder(llm)
	if err != nil {
		return nil, fmt.Errorf("chunkembed: build embedder: %w", err)
	}
	return emb, nil
}

// embedChunks embeds every chunk, returning one vector per chunk in order.
// SEM@ef969bb79ad525fa5038847af0fb0be1038ae961: compute embedding vectors for a slice of text chunks in order (reads DB)
func embedChunks(ctx context.Context, emb embeddings.Embedder, chunks []string) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	vectors, err := emb.EmbedDocuments(ctx, chunks)
	if err != nil {
		return nil, fmt.Errorf("chunkembed: embed documents: %w", err)
	}
	return vectors, nil
}
