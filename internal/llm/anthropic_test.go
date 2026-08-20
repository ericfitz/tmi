package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// anthropicSSE is a fake Messages API streaming response: message_start,
// two text deltas, message_delta carrying the final cumulative usage, then
// message_stop. Anthropic's SSE wire format pairs an `event:` line naming
// the event type with a `data:` line carrying its JSON body — unlike
// OpenAI's, which only sends `data:` lines.
const anthropicSSE = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-test\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
	"event: content_block_start\n" +
	"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
	"event: content_block_stop\n" +
	"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\n" +
	"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}\n\n" +
	"event: message_stop\n" +
	"data: {\"type\":\"message_stop\"}\n\n"

// TestAnthropicChatClient_StreamChat_RoutesThroughInjectedDoer proves that a
// streamed chat completion flows through the injected HTTPDoer (the same
// SSRF-safe-adapter contract proven for the OpenAI backend), that the
// system prompt is hoisted to the top-level `system` field rather than sent
// as a message turn, that max_tokens is present in the request body, and
// that deltas and final usage are assembled correctly from a fake SSE
// server.
func TestAnthropicChatClient_StreamChat_RoutesThroughInjectedDoer(t *testing.T) {
	var sawSystemField string
	var sawFirstMessageRole string
	var sawMaxTokens float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		// Anthropic accepts `system` as either a bare string or a list of text
		// blocks; decode as generic JSON so this test can inspect the exact
		// shape the SDK actually sends.
		var raw map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		if sysVal, ok := raw["system"]; ok {
			if blocks, ok := sysVal.([]any); ok && len(blocks) > 0 {
				if block, ok := blocks[0].(map[string]any); ok {
					sawSystemField, _ = block["text"].(string)
				}
			}
		}
		if mt, ok := raw["max_tokens"].(float64); ok {
			sawMaxTokens = mt
		}
		if msgs, ok := raw["messages"].([]any); ok && len(msgs) > 0 {
			if first, ok := msgs[0].(map[string]any); ok {
				sawFirstMessageRole, _ = first["role"].(string)
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(anthropicSSE))
	}))
	defer srv.Close()

	doer := &recordingDoer{inner: srv.Client()}
	client, err := newAnthropicChatClient(Config{
		Model:      "claude-test",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: doer,
		MaxTokens:  2048,
	})
	require.NoError(t, err)

	var got string
	usage, err := client.StreamChat(context.Background(), []Message{
		{Role: RoleSystem, Text: "sys"},
		{Role: RoleUser, Text: "hi"},
	}, func(_ context.Context, chunk []byte) error {
		got += string(chunk)
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, "Hello world", got)
	assert.Equal(t, Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}, usage)
	assert.Greater(t, doer.Calls(), 0, "streamed chat request must go through the injected HTTPDoer")
	assert.Equal(t, "sys", sawSystemField, "system prompt must be hoisted to the top-level system field")
	assert.Equal(t, "user", sawFirstMessageRole, "system message must not appear in the messages array")
	assert.Equal(t, float64(2048), sawMaxTokens, "configured max_tokens must be sent in the request body")
}

// TestAnthropicChatClient_StreamChat_MultipleSystemMessagesConcatenated
// proves multiple RoleSystem messages (not expected from TMI's caller today,
// which sends exactly one, but not assumed by splitSystemAndTurns) are
// joined with a blank line into the single top-level system field.
func TestAnthropicChatClient_StreamChat_MultipleSystemMessagesConcatenated(t *testing.T) {
	var sawSystemField string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		if blocks, ok := raw["system"].([]any); ok && len(blocks) > 0 {
			if block, ok := blocks[0].(map[string]any); ok {
				sawSystemField, _ = block["text"].(string)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(anthropicSSE))
	}))
	defer srv.Close()

	client, err := newAnthropicChatClient(Config{
		Model:      "claude-test",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: &recordingDoer{inner: srv.Client()},
	})
	require.NoError(t, err)

	_, err = client.StreamChat(context.Background(), []Message{
		{Role: RoleSystem, Text: "first"},
		{Role: RoleSystem, Text: "second"},
		{Role: RoleUser, Text: "hi"},
	}, func(_ context.Context, _ []byte) error { return nil })
	require.NoError(t, err)

	assert.Equal(t, "first\n\nsecond", sawSystemField)
}

// TestAnthropicChatClient_StreamChat_DefaultMaxTokens proves that when
// Config.MaxTokens is unset (0), the backend still sends a positive
// max_tokens — Anthropic's Messages API requires it on every request, unlike
// OpenAI's.
func TestAnthropicChatClient_StreamChat_DefaultMaxTokens(t *testing.T) {
	var sawMaxTokens float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		if mt, ok := raw["max_tokens"].(float64); ok {
			sawMaxTokens = mt
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(anthropicSSE))
	}))
	defer srv.Close()

	client, err := newAnthropicChatClient(Config{
		Model:      "claude-test",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: &recordingDoer{inner: srv.Client()},
		// MaxTokens intentionally left unset.
	})
	require.NoError(t, err)

	_, err = client.StreamChat(context.Background(), []Message{
		{Role: RoleUser, Text: "hi"},
	}, func(_ context.Context, _ []byte) error { return nil })
	require.NoError(t, err)

	assert.Equal(t, float64(anthropicDefaultMaxTokens), sawMaxTokens)
}

func TestNewAnthropicChatClient_RequiresModel(t *testing.T) {
	_, err := newAnthropicChatClient(Config{APIKey: "k"})
	assert.Error(t, err)
}

func TestNewAnthropicChatClient_RejectsNegativeMaxTokens(t *testing.T) {
	_, err := newAnthropicChatClient(Config{Model: "claude-test", APIKey: "k", MaxTokens: -1})
	assert.Error(t, err)
}

func TestNewAnthropicChatClient_AppliesDefaultMaxTokens(t *testing.T) {
	client, err := newAnthropicChatClient(Config{Model: "claude-test", APIKey: "k"})
	require.NoError(t, err)
	assert.EqualValues(t, anthropicDefaultMaxTokens, client.maxTokens)
}

func TestNewAnthropicChatClient_HonorsConfiguredMaxTokens(t *testing.T) {
	client, err := newAnthropicChatClient(Config{Model: "claude-test", APIKey: "k", MaxTokens: 8192})
	require.NoError(t, err)
	assert.EqualValues(t, 8192, client.maxTokens)
}

// TestSplitSystemAndTurns_NoSystemMessage proves the system field stays
// empty (and is therefore omitted from the request) when no RoleSystem
// message is present.
func TestSplitSystemAndTurns_NoSystemMessage(t *testing.T) {
	system, turns := splitSystemAndTurns([]Message{
		{Role: RoleUser, Text: "hi"},
		{Role: RoleAssistant, Text: "hello"},
	})
	assert.Empty(t, system)
	require.Len(t, turns, 2)
}
