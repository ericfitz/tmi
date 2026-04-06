package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimmyConfig_IsConfigured(t *testing.T) {
	cfg := TimmyConfig{
		LLMProvider:       "openai",
		LLMModel:          "gpt-4",
		EmbeddingProvider: "openai",
		EmbeddingModel:    "text-embedding-ada-002",
	}
	assert.True(t, cfg.IsConfigured(), "should be configured with all required fields")

	empty := TimmyConfig{}
	assert.False(t, empty.IsConfigured(), "should not be configured with empty fields")
}

func TestTimmyConfig_BaseURLFields(t *testing.T) {
	cfg := DefaultTimmyConfig()
	assert.Empty(t, cfg.LLMBaseURL, "default LLMBaseURL should be empty")
	assert.Empty(t, cfg.EmbeddingBaseURL, "default EmbeddingBaseURL should be empty")

	cfg.LLMBaseURL = "http://localhost:1234/v1"
	cfg.EmbeddingBaseURL = "http://localhost:1234/v1"
	assert.Equal(t, "http://localhost:1234/v1", cfg.LLMBaseURL)
	assert.Equal(t, "http://localhost:1234/v1", cfg.EmbeddingBaseURL)
}
