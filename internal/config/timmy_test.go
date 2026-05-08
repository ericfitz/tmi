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

func TestTimmyConfig_IsRerankConfigured(t *testing.T) {
	cfg := TimmyConfig{
		RerankProvider: "jina",
		RerankModel:    "jina-reranker-v3",
	}
	assert.True(t, cfg.IsRerankConfigured(), "should be configured with provider + model")

	noModel := TimmyConfig{
		RerankProvider: "jina",
	}
	assert.False(t, noModel.IsRerankConfigured(), "should not be configured without model")

	empty := TimmyConfig{}
	assert.False(t, empty.IsRerankConfigured(), "should not be configured when empty")
}

func TestTimmyConfig_DecompositionAndRerankDefaults(t *testing.T) {
	cfg := DefaultTimmyConfig()
	assert.False(t, cfg.QueryDecompositionEnabled, "decomposition should be off by default")
	assert.Equal(t, 10, cfg.RerankTopK, "rerank top-k should default to 10")
	assert.Empty(t, cfg.RerankProvider)
	assert.Empty(t, cfg.RerankModel)
}

// TestTimmyConfig_DumpExtractedTextToNote_DefaultsOff ensures the dev-only
// dump flag does not silently activate.
func TestTimmyConfig_DumpExtractedTextToNote_DefaultsOff(t *testing.T) {
	cfg := DefaultTimmyConfig()
	assert.False(t, cfg.DumpExtractedTextToNote, "dump flag must default to false")
}

// TestValidateTimmy_DumpExtractedTextToNote covers the production refusal
// rule. Production builds with the flag enabled must fail validation; any
// other combination passes.
func TestValidateTimmy_DumpExtractedTextToNote(t *testing.T) {
	tests := []struct {
		name      string
		buildMode string
		dumpOn    bool
		wantErr   bool
	}{
		{name: "dev_off", buildMode: "dev", dumpOn: false, wantErr: false},
		{name: "dev_on", buildMode: "dev", dumpOn: true, wantErr: false},
		{name: "test_on", buildMode: "test", dumpOn: true, wantErr: false},
		{name: "production_off", buildMode: "production", dumpOn: false, wantErr: false},
		{name: "production_on", buildMode: "production", dumpOn: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Auth:  AuthConfig{BuildMode: tt.buildMode},
				Timmy: TimmyConfig{DumpExtractedTextToNote: tt.dumpOn},
			}
			err := c.validateTimmy()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "dump_extracted_text_to_note")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
