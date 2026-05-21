package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/internal/config"
)

func TestTimmyConfigProvider_Current_AssemblesFromSettings(t *testing.T) {
	ms := NewMockSettingsService()
	ms.AddSetting("timmy.enabled", "true", "bool")
	ms.AddSetting("timmy.llm_provider", "openai", "string")
	ms.AddSetting("timmy.llm_model", "gpt-5.5", "string")
	ms.AddSetting("timmy.llm_base_url", "https://api.openai.com/v1", "string")
	ms.AddSetting("timmy.llm_api_key", "sk-test", "string")
	ms.AddSetting("timmy.text_embedding_provider", "openai", "string")
	ms.AddSetting("timmy.text_embedding_model", "text-embedding-3-large", "string")
	ms.AddSetting("timmy.text_embedding_base_url", "https://api.openai.com/v1", "string")
	ms.AddSetting("timmy.text_embedding_api_key", "sk-embed", "string")
	ms.AddSetting("timmy.embedding_dimension", "3072", "int")
	ms.AddSetting("timmy.text_retrieval_top_k", "7", "int")
	ms.AddSetting("timmy.embedding_cleanup_interval_minutes", "120", "int")

	p := NewTimmyConfigProvider(ms)
	cfg := p.Current(context.Background())

	assert.True(t, cfg.Enabled)
	assert.Equal(t, "openai", cfg.LLMProvider)
	assert.Equal(t, "gpt-5.5", cfg.LLMModel)
	assert.Equal(t, "sk-test", cfg.LLMAPIKey)
	assert.Equal(t, "text-embedding-3-large", cfg.TextEmbeddingModel)
	assert.Equal(t, "sk-embed", cfg.TextEmbeddingAPIKey)
	assert.Equal(t, 3072, cfg.EmbeddingDimension)
	assert.Equal(t, 7, cfg.TextRetrievalTopK)
	assert.Equal(t, 120, cfg.EmbeddingCleanupIntervalMinutes)
	assert.Equal(t, 50, cfg.MaxConversationHistory)
	require.True(t, cfg.IsConfigured())
}

func TestTimmyConfigProvider_Current_DisabledByDefault(t *testing.T) {
	ms := NewMockSettingsService()
	p := NewTimmyConfigProvider(ms)
	cfg := p.Current(context.Background())
	assert.False(t, cfg.Enabled)
	assert.False(t, cfg.IsConfigured())
}

func TestTimmyConfigProvider_Current_NilSettings(t *testing.T) {
	p := NewTimmyConfigProvider(nil)
	cfg := p.Current(context.Background())
	assert.False(t, cfg.Enabled)
	assert.Equal(t, 50, cfg.MaxConversationHistory)
	assert.False(t, cfg.IsConfigured())
}

func TestTimmyConfigProvider_WiringHash_StableForKnobs(t *testing.T) {
	p := NewTimmyConfigProvider(NewMockSettingsService())
	base := config.DefaultTimmyConfig()
	base.LLMProvider = "openai"
	base.LLMModel = "gpt-5.5"
	base.LLMAPIKey = "sk-a"
	base.TextEmbeddingProvider = "openai"
	base.TextEmbeddingModel = "text-embedding-3-large"
	base.EmbeddingDimension = 3072

	h1 := p.WiringHash(base)

	// Changing a tuning knob does NOT change the hash.
	knob := base
	knob.MaxConversationHistory = 99
	knob.TextRetrievalTopK = 1
	assert.Equal(t, h1, p.WiringHash(knob), "tuning-knob change must not change wiring hash")

	// Changing the api key DOES change the hash.
	rekey := base
	rekey.LLMAPIKey = "sk-b"
	assert.NotEqual(t, h1, p.WiringHash(rekey), "api key change must change wiring hash")

	// Changing the model DOES change the hash.
	remodel := base
	remodel.LLMModel = "gpt-6"
	assert.NotEqual(t, h1, p.WiringHash(remodel), "model change must change wiring hash")
}
