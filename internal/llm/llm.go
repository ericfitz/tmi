// Package llm is TMI's provider-agnostic seam over chat-completion and
// embedding backends. No caller outside this package (and its
// provider-specific files) should ever import an SDK type from a concrete
// provider — api/timmy_llm_service.go, api/timmy_session_manager.go, and
// cmd/chunkembed/* all speak only Message/Role/ChatClient/Embedder.
//
// Phase 1 (#754) implements the OpenAI backend on github.com/openai/openai-go.
// Phase 2 adds an Anthropic backend on the same interface; NewChatClient
// already recognizes ProviderAnthropic and rejects it with a clear
// not-yet-implemented error so config can be staged ahead of that work.
package llm

import (
	"context"
	"fmt"
	"net/http"
)

// Role identifies the speaker of a chat Message.
// SEM@0000000000000000000000000000000000000000: identifies the speaker role of a chat message (pure)
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn in a chat conversation. It is the only message shape
// that may cross the internal/llm boundary — no provider SDK type may be
// substituted for it anywhere else in the codebase.
// SEM@0000000000000000000000000000000000000000: holds one chat turn's role and text (pure)
type Message struct {
	Role Role
	Text string
}

// Usage reports token accounting for a chat completion, when the provider
// exposes it. A zero value means the provider did not report usage (e.g. the
// stream was cancelled before the final chunk); callers should treat it as
// "unknown" rather than "zero tokens used."
// SEM@0000000000000000000000000000000000000000: holds prompt/completion/total token counts for a chat completion (pure)
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ChatClient streams a chat completion, invoking onDelta once per text
// chunk as it arrives. It returns the provider's reported token usage for
// the completion (zero value if unavailable) and any error. Implementations
// MUST route every request — including the streamed request itself —
// through the Config.HTTPClient supplied at construction time.
// SEM@0000000000000000000000000000000000000000: interface for streaming a chat completion via a delta callback (pure)
type ChatClient interface {
	StreamChat(ctx context.Context, messages []Message, onDelta func(ctx context.Context, chunk []byte) error) (Usage, error)
}

// Embedder computes embedding vectors for a batch of texts. Implementations
// MUST preserve index alignment: result[i] is always the embedding of
// texts[i], regardless of internal batching or provider response order.
// SEM@0000000000000000000000000000000000000000: interface for computing index-aligned embedding vectors for a batch of texts (pure)
type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
}

// HTTPDoer is the minimal HTTP contract accepted by provider constructors —
// identical in shape to *http.Client and to openai-go's option.HTTPClient
// (and to anthropic-sdk-go's, once phase 2 lands). Callers inject an
// SSRF-safe adapter here (see api.safeHTTPDoer) so all provider traffic,
// chat and embeddings, streamed and non-streamed, flows through the same
// egress control. No SDK type is referenced by this interface.
// SEM@0000000000000000000000000000000000000000: minimal HTTP client contract accepted by provider constructors (pure)
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider identifies which backend a Config targets.
// SEM@0000000000000000000000000000000000000000: identifies which LLM backend a Config targets (pure)
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
)

// Config carries the connection settings for one provider endpoint. TMI
// configures chat, text embeddings, and code embeddings as independent
// model/key/base-URL triples, so callers construct one Config per endpoint
// (see api.NewTimmyLLMService) rather than one Config for the whole service.
// SEM@0000000000000000000000000000000000000000: connection settings for one provider chat or embedding endpoint (pure)
type Config struct {
	Provider Provider
	// Model is the chat or embedding model name, depending on which
	// constructor consumes this Config.
	Model      string
	APIKey     string
	BaseURL    string
	HTTPClient HTTPDoer
}

// NewChatClient builds a ChatClient for cfg.Provider. Only ProviderOpenAI is
// implemented in phase 1; ProviderAnthropic returns a clear not-yet-
// implemented error rather than silently falling back, so operators who
// stage anthropic in config get an actionable failure instead of talking to
// the wrong provider. TODO(#754 phase 2): implement the Anthropic backend.
// SEM@0000000000000000000000000000000000000000: build a ChatClient for the configured provider, rejecting unimplemented ones (pure)
func NewChatClient(cfg Config) (ChatClient, error) {
	switch cfg.Provider {
	case ProviderOpenAI:
		return newOpenAIChatClient(cfg)
	case ProviderAnthropic:
		return nil, fmt.Errorf("llm: provider %q chat backend is not yet implemented (TODO #754 phase 2)", cfg.Provider)
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", cfg.Provider)
	}
}

// NewEmbedder builds an Embedder for cfg.Provider. Only ProviderOpenAI is
// implemented — and, per #754, OpenAI is the only provider this ever
// implements for embeddings: Anthropic has no embeddings API, so phase 2
// does not add an Anthropic embedder.
// SEM@0000000000000000000000000000000000000000: build an Embedder for the configured provider, rejecting unimplemented ones (pure)
func NewEmbedder(cfg Config) (Embedder, error) {
	switch cfg.Provider {
	case ProviderOpenAI:
		return newOpenAIEmbedder(cfg)
	case ProviderAnthropic:
		return nil, fmt.Errorf("llm: provider %q has no embeddings API", cfg.Provider)
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", cfg.Provider)
	}
}
