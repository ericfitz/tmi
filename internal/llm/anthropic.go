package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// anthropicDefaultMaxTokens is applied when Config.MaxTokens is unset (<=0).
// Anthropic's Messages API requires max_tokens on every request — unlike
// OpenAI, there is no "let the provider decide" mode — so operators who
// don't set timmy.llm_max_tokens still get a working request instead of a
// 400 from the provider. 4096 is a conservative middle ground: enough for a
// substantive chat turn without inviting runaway generation cost.
const anthropicDefaultMaxTokens = 4096

// anthropicRequestOptions builds the anthropic-sdk-go client options shared
// by backend constructors: API key, optional base URL override, and —
// critically — the caller-supplied HTTPDoer. anthropic-sdk-go's
// option.WithHTTPClient accepts the same `Do(*http.Request)
// (*http.Response, error)` shape as openai-go's, so cfg.HTTPClient (TMI's
// SSRF-safe adapter, see api.safeHTTPDoer) routes every request — streamed
// or not — through the same egress control as the OpenAI backend.
// SEM@0000000000000000000000000000000000000000: build shared anthropic-sdk-go request options including the injected HTTP doer (pure)
func anthropicRequestOptions(cfg Config) []option.RequestOption {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	return opts
}

// anthropicChatClient implements ChatClient on top of anthropic-sdk-go's
// Messages API. Only the plain chat surface (system + user/assistant text
// turns) is used; tool use is deliberately not wired up here (T13 Part 2 /
// #384 is a separate, deferred effort), but nothing about this struct's
// shape forecloses adding it — a future tool-use extension can add fields
// and branch in StreamChat without changing the ChatClient contract.
// SEM@0000000000000000000000000000000000000000: ChatClient backend for Anthropic Messages API chat completions (pure)
type anthropicChatClient struct {
	client    anthropic.Client
	model     string
	maxTokens int64
}

// newAnthropicChatClient builds the Anthropic ChatClient backend. maxTokens
// defaults to anthropicDefaultMaxTokens when cfg.MaxTokens is unset; a
// negative value is rejected rather than silently clamped, since it almost
// certainly indicates a misconfiguration upstream (internal/config also
// validates this ahead of time, but the backend does not trust that alone).
// SEM@0000000000000000000000000000000000000000: build an Anthropic-backed ChatClient from provider config (pure)
func newAnthropicChatClient(cfg Config) (*anthropicChatClient, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("llm: anthropic chat client requires a model")
	}
	if cfg.MaxTokens < 0 {
		return nil, fmt.Errorf("llm: anthropic chat client max tokens must be non-negative, got %d", cfg.MaxTokens)
	}
	maxTokens := int64(cfg.MaxTokens)
	if maxTokens == 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	return &anthropicChatClient{
		client:    anthropic.NewClient(anthropicRequestOptions(cfg)...),
		model:     cfg.Model,
		maxTokens: maxTokens,
	}, nil
}

// StreamChat streams a chat completion via the Messages API, feeding each
// non-empty text delta to onDelta and returning the provider-reported token
// usage from the last usage-bearing stream event. Anthropic takes the
// system prompt as a top-level request field rather than a message turn —
// splitSystemAndTurns extracts and concatenates any RoleSystem messages
// (TMI's caller sends at most one, but this is not assumed) and the
// remainder becomes the user/assistant turn sequence.
// SEM@0000000000000000000000000000000000000000: stream an Anthropic chat completion via delta callback, returning reported token usage
func (c *anthropicChatClient) StreamChat(
	ctx context.Context,
	messages []Message,
	onDelta func(ctx context.Context, chunk []byte) error,
) (Usage, error) {
	system, turns := splitSystemAndTurns(messages)

	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		Messages:  turns,
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}

	stream := c.client.Messages.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()

	var usage Usage
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "content_block_delta":
			if event.Delta.Type != "text_delta" || event.Delta.Text == "" {
				continue
			}
			if err := onDelta(ctx, []byte(event.Delta.Text)); err != nil {
				return usage, err
			}
		case "message_delta":
			// MessageDeltaUsage's InputTokens/OutputTokens are cumulative for
			// the whole response, so the last message_delta event (there is
			// ordinarily exactly one, immediately before message_stop) carries
			// the final totals — no need to also read message_start's usage.
			usage = Usage{
				PromptTokens:     int(event.Usage.InputTokens),
				CompletionTokens: int(event.Usage.OutputTokens),
				TotalTokens:      int(event.Usage.InputTokens) + int(event.Usage.OutputTokens),
			}
		}
	}
	if err := stream.Err(); err != nil {
		return usage, fmt.Errorf("llm: anthropic chat stream failed: %w", err)
	}
	return usage, nil
}

// splitSystemAndTurns separates RoleSystem messages from the ordered
// user/assistant turn sequence, since Anthropic's Messages API takes the
// system prompt as a top-level parameter rather than a message in the
// array. Multiple system messages (not expected from TMI's caller today,
// which sends exactly one) are concatenated with a blank line, matching how
// a caller would join them if they appeared as consecutive system turns.
// SEM@0000000000000000000000000000000000000000: split internal chat messages into a top-level system prompt and Anthropic message turns (pure)
func splitSystemAndTurns(messages []Message) (string, []anthropic.MessageParam) {
	var systemParts []string
	turns := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			systemParts = append(systemParts, m.Text)
		case RoleAssistant:
			turns = append(turns, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Text)))
		default:
			turns = append(turns, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Text)))
		}
	}
	return strings.Join(systemParts, "\n\n"), turns
}
