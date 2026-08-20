package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewChatClient_OpenAI(t *testing.T) {
	client, err := NewChatClient(Config{Provider: ProviderOpenAI, Model: "gpt-test", APIKey: "k"})
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewChatClient_Anthropic(t *testing.T) {
	client, err := NewChatClient(Config{Provider: ProviderAnthropic, Model: "claude-test", APIKey: "k"})
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewChatClient_UnknownProvider(t *testing.T) {
	_, err := NewChatClient(Config{Provider: "made-up", Model: "x", APIKey: "k"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestNewEmbedder_OpenAI(t *testing.T) {
	embedder, err := NewEmbedder(Config{Provider: ProviderOpenAI, Model: "text-embedding-test", APIKey: "k"})
	assert.NoError(t, err)
	assert.NotNil(t, embedder)
}

func TestNewEmbedder_AnthropicHasNoEmbeddingsAPI(t *testing.T) {
	_, err := NewEmbedder(Config{Provider: ProviderAnthropic, Model: "x", APIKey: "k"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no embeddings API")
}

func TestNewEmbedder_UnknownProvider(t *testing.T) {
	_, err := NewEmbedder(Config{Provider: "made-up", Model: "x", APIKey: "k"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}
