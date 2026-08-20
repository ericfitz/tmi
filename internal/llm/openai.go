package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// embedBatchSize mirrors the batch size langchaingo's embeddings.NewEmbedder
// used by default (embeddings.defaultBatchSize) so migrating off it does not
// silently change request shape against the configured embedding endpoint.
const embedBatchSize = 512

// openAIRequestOptions builds the openai-go client options shared by the
// chat and embedding constructors: API key, optional base URL override, and
// — critically — the caller-supplied HTTPDoer. Every request the resulting
// client makes, streamed or not, chat or embeddings, goes through
// cfg.HTTPClient when set; callers use this to route traffic through
// TMI's SSRF-safe transport (see api.safeHTTPDoer).
// SEM@0000000000000000000000000000000000000000: build shared openai-go request options including the injected HTTP doer (pure)
func openAIRequestOptions(cfg Config) []option.RequestOption {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	return opts
}

// openAIChatClient implements ChatClient on top of openai-go's chat
// completions streaming API.
// SEM@0000000000000000000000000000000000000000: ChatClient backend for OpenAI chat completions (pure)
type openAIChatClient struct {
	client openai.Client
	model  string
}

// newOpenAIChatClient builds the OpenAI ChatClient backend.
// SEM@0000000000000000000000000000000000000000: build an OpenAI-backed ChatClient from provider config (pure)
func newOpenAIChatClient(cfg Config) (*openAIChatClient, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("llm: openai chat client requires a model")
	}
	return &openAIChatClient{
		client: openai.NewClient(openAIRequestOptions(cfg)...),
		model:  cfg.Model,
	}, nil
}

// StreamChat streams a chat completion, feeding each non-empty text delta to
// onDelta and returning the provider-reported token usage from the final
// chunk (stream_options.include_usage). Usage is the zero value if the
// stream ends without a usage-bearing chunk (e.g. cancellation).
// SEM@0000000000000000000000000000000000000000: stream an OpenAI chat completion via delta callback, returning reported token usage
func (c *openAIChatClient) StreamChat(
	ctx context.Context,
	messages []Message,
	onDelta func(ctx context.Context, chunk []byte) error,
) (Usage, error) {
	params := openai.ChatCompletionNewParams{
		Model:    c.model,
		Messages: toOpenAIMessages(messages),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()

	var usage Usage
	for stream.Next() {
		chunk := stream.Current()
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			if err := onDelta(ctx, []byte(choice.Delta.Content)); err != nil {
				return usage, err
			}
		}
		// The usage-bearing chunk has empty Choices per the OpenAI streaming
		// contract (stream_options.include_usage); TotalTokens > 0 is the
		// signal it carries real data rather than the zero value.
		if chunk.Usage.TotalTokens > 0 {
			usage = Usage{
				PromptTokens:     int(chunk.Usage.PromptTokens),
				CompletionTokens: int(chunk.Usage.CompletionTokens),
				TotalTokens:      int(chunk.Usage.TotalTokens),
			}
		}
	}
	if err := stream.Err(); err != nil {
		return usage, fmt.Errorf("llm: openai chat stream failed: %w", err)
	}
	return usage, nil
}

// toOpenAIMessages converts TMI's provider-agnostic messages into
// openai-go's message union type. Unrecognized roles fall back to user —
// today the only caller (TimmyLLMService) only ever emits RoleSystem,
// RoleUser, and RoleAssistant.
// SEM@0000000000000000000000000000000000000000: convert internal chat messages to the OpenAI message union type (pure)
func toOpenAIMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			out = append(out, openai.SystemMessage(m.Text))
		case RoleAssistant:
			out = append(out, openai.AssistantMessage(m.Text))
		default:
			out = append(out, openai.UserMessage(m.Text))
		}
	}
	return out
}

// openAIEmbedder implements Embedder on top of openai-go's embeddings API.
// SEM@0000000000000000000000000000000000000000: Embedder backend for OpenAI embeddings, index-aligned and batched (pure)
type openAIEmbedder struct {
	client openai.Client
	model  string
}

// newOpenAIEmbedder builds the OpenAI Embedder backend.
// SEM@0000000000000000000000000000000000000000: build an OpenAI-backed Embedder from provider config (pure)
func newOpenAIEmbedder(cfg Config) (*openAIEmbedder, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("llm: openai embedder requires a model")
	}
	return &openAIEmbedder{
		client: openai.NewClient(openAIRequestOptions(cfg)...),
		model:  cfg.Model,
	}, nil
}

// EmbedDocuments embeds texts in batches of embedBatchSize, always returning
// a slice the same length as texts. Each batch response is assembled by the
// provider's per-item Index field rather than by response array order, so
// result[i] is guaranteed to be the embedding of texts[i] even if a provider
// ever returns items out of order — this index alignment is
// correctness-critical for the vector store (#754).
// SEM@0000000000000000000000000000000000000000: compute index-aligned embedding vectors for texts in batches (reads network)
func (e *openAIEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	result := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]

		// Strip newlines per-input, matching langchaingo's default
		// (StripNewLines: true) so migrating backends does not change the
		// text actually sent to the embedding model.
		prepped := make([]string, len(batch))
		for i, t := range batch {
			prepped[i] = strings.ReplaceAll(t, "\n", " ")
		}

		resp, err := e.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
			Model: e.model,
			Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: prepped},
		})
		if err != nil {
			return nil, fmt.Errorf("llm: openai embed batch [%d:%d]: %w", start, end, err)
		}
		if len(resp.Data) != len(batch) {
			return nil, fmt.Errorf("llm: openai embed batch [%d:%d]: got %d embeddings for %d inputs",
				start, end, len(resp.Data), len(batch))
		}

		for _, item := range resp.Data {
			if item.Index < 0 || int(item.Index) >= len(batch) {
				return nil, fmt.Errorf("llm: openai embed batch [%d:%d]: response index %d out of range",
					start, end, item.Index)
			}
			vec := make([]float32, len(item.Embedding))
			for j, f := range item.Embedding {
				vec[j] = float32(f)
			}
			result[start+int(item.Index)] = vec
		}
	}

	return result, nil
}
