package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimmyConfig_IsConfigured(t *testing.T) {
	cfg := TimmyConfig{
		LLMProvider:           "openai",
		LLMModel:              "gpt-4",
		TextEmbeddingProvider: "openai",
		TextEmbeddingModel:    "text-embedding-3-small",
	}
	assert.True(t, cfg.IsConfigured(), "should be configured with LLM + text embedding")

	empty := TimmyConfig{}
	assert.False(t, empty.IsConfigured(), "should not be configured with empty fields")

	noTextEmbed := TimmyConfig{
		LLMProvider: "openai",
		LLMModel:    "gpt-4",
	}
	assert.False(t, noTextEmbed.IsConfigured(), "should not be configured without text embedding")
}

func TestTimmyConfig_IsCodeIndexConfigured(t *testing.T) {
	cfg := TimmyConfig{
		CodeEmbeddingProvider: "openai",
		CodeEmbeddingModel:    "text-embedding-3-small",
	}
	assert.True(t, cfg.IsCodeIndexConfigured(), "should be configured with provider + model")

	noModel := TimmyConfig{
		CodeEmbeddingProvider: "openai",
	}
	assert.False(t, noModel.IsCodeIndexConfigured(), "should not be configured without model")

	empty := TimmyConfig{}
	assert.False(t, empty.IsCodeIndexConfigured(), "should not be configured when empty")
}

func TestTimmyConfig_BaseURLFields(t *testing.T) {
	cfg := DefaultTimmyConfig()
	assert.Empty(t, cfg.LLMBaseURL, "default LLMBaseURL should be empty")
	assert.Empty(t, cfg.TextEmbeddingBaseURL, "default TextEmbeddingBaseURL should be empty")
	assert.Empty(t, cfg.CodeEmbeddingBaseURL, "default CodeEmbeddingBaseURL should be empty")

	cfg.LLMBaseURL = "http://localhost:1234/v1"
	cfg.TextEmbeddingBaseURL = "http://localhost:5678/v1"
	cfg.CodeEmbeddingBaseURL = "http://localhost:9012/v1"
	assert.Equal(t, "http://localhost:1234/v1", cfg.LLMBaseURL)
	assert.Equal(t, "http://localhost:5678/v1", cfg.TextEmbeddingBaseURL)
	assert.Equal(t, "http://localhost:9012/v1", cfg.CodeEmbeddingBaseURL)
}

func TestTimmyConfig_Defaults(t *testing.T) {
	cfg := DefaultTimmyConfig()
	assert.Equal(t, 10, cfg.TextRetrievalTopK)
	assert.Equal(t, 10, cfg.CodeRetrievalTopK)
	assert.Equal(t, 512, cfg.ChunkSize)
	assert.Equal(t, 50, cfg.ChunkOverlap)
	assert.Equal(t, 256, cfg.MaxMemoryMB)
	assert.Equal(t, 120, cfg.LLMTimeoutSeconds)
}
