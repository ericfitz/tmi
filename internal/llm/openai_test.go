package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingDoer wraps a real *http.Client and counts every Do() call. It
// proves the HTTPDoer injected via Config.HTTPClient is the transport
// actually used for every request the openai-go client issues — chat AND
// embeddings, streamed AND non-streamed. api.safeHTTPDoer (TMI's SSRF
// defense) must satisfy this exact contract; these tests exercise the
// generic plumbing that guarantees it gets used, independent of
// SafeHTTPClient's own tests in the api package.
type recordingDoer struct {
	inner *http.Client
	calls int32
}

func (d *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&d.calls, 1)
	return d.inner.Do(req) // #nosec G704 -- test-only call to httptest server
}

func (d *recordingDoer) Calls() int {
	return int(atomic.LoadInt32(&d.calls))
}

// TestOpenAIChatClient_StreamChat_RoutesThroughInjectedDoer proves that a
// streamed chat completion — SSE response included — flows through the
// injected HTTPDoer, and that deltas and final usage are assembled
// correctly from a fake SSE server.
func TestOpenAIChatClient_StreamChat_RoutesThroughInjectedDoer(t *testing.T) {
	const sse = "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-test\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"

	var sawSystemMessage bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		var body struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if len(body.Messages) > 0 && body.Messages[0].Role == "system" {
			sawSystemMessage = true
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	doer := &recordingDoer{inner: srv.Client()}
	client, err := newOpenAIChatClient(Config{
		Model:      "gpt-test",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: doer,
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
	assert.Equal(t, Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7}, usage)
	assert.Greater(t, doer.Calls(), 0, "streamed chat request must go through the injected HTTPDoer")
	assert.True(t, sawSystemMessage, "system prompt must be sent as the first message")
}

// TestOpenAIEmbedder_EmbedDocuments_RoutesThroughInjectedDoer proves
// embedding requests also flow through the injected HTTPDoer.
func TestOpenAIEmbedder_EmbedDocuments_RoutesThroughInjectedDoer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/embeddings", r.URL.Path)
		var req struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{
				"object":    "embedding",
				"index":     i,
				"embedding": []float64{float64(i), float64(i) + 0.5},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "text-embedding-test",
			"data":   data,
			"usage":  map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer srv.Close()

	doer := &recordingDoer{inner: srv.Client()}
	embedder, err := newOpenAIEmbedder(Config{
		Model:      "text-embedding-test",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: doer,
	})
	require.NoError(t, err)

	vectors, err := embedder.EmbedDocuments(context.Background(), []string{"a", "b", "c"})
	require.NoError(t, err)
	require.Len(t, vectors, 3)
	for i, v := range vectors {
		assert.Equal(t, []float32{float32(i), float32(i) + 0.5}, v)
	}
	assert.Greater(t, doer.Calls(), 0, "embedding request must go through the injected HTTPDoer")
}

// TestOpenAIEmbedder_EmbedDocuments_IndexAlignment pins the
// correctness-critical guarantee called out in #754: result[i] must be the
// embedding of texts[i] even when the provider returns items out of order
// within a batch. A regression here silently corrupts every downstream
// vector search.
func TestOpenAIEmbedder_EmbedDocuments_IndexAlignment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		// Return items in REVERSE order to prove assembly uses each item's
		// Index field, not its position in the response array.
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			reversed := len(req.Input) - 1 - i
			data[i] = map[string]any{
				"object":    "embedding",
				"index":     reversed,
				"embedding": []float64{float64(reversed)},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "text-embedding-test",
			"data":   data,
			"usage":  map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer srv.Close()

	embedder, err := newOpenAIEmbedder(Config{
		Model:      "text-embedding-test",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: &recordingDoer{inner: srv.Client()},
	})
	require.NoError(t, err)

	texts := []string{"zero", "one", "two", "three"}
	vectors, err := embedder.EmbedDocuments(context.Background(), texts)
	require.NoError(t, err)
	require.Len(t, vectors, len(texts))
	for i := range texts {
		assert.Equal(t, []float32{float32(i)}, vectors[i], "vectors[%d] must be the embedding of texts[%d]", i, i)
	}
}

// TestOpenAIEmbedder_EmbedDocuments_BatchesLargeInputs pins the batching
// boundary (embedBatchSize) and proves index alignment holds ACROSS batches,
// not just within one.
func TestOpenAIEmbedder_EmbedDocuments_BatchesLargeInputs(t *testing.T) {
	var reqCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		var req struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		data := make([]map[string]any, len(req.Input))
		for i, txt := range req.Input {
			var n int
			_, err := fmt.Sscanf(txt, "text-%d", &n)
			require.NoError(t, err)
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": []float64{float64(n)}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "text-embedding-test",
			"data":   data,
			"usage":  map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer srv.Close()

	embedder, err := newOpenAIEmbedder(Config{
		Model:      "text-embedding-test",
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: &recordingDoer{inner: srv.Client()},
	})
	require.NoError(t, err)

	texts := make([]string, embedBatchSize+37)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}
	vectors, err := embedder.EmbedDocuments(context.Background(), texts)
	require.NoError(t, err)
	require.Len(t, vectors, len(texts))
	for i := range texts {
		assert.Equal(t, []float32{float32(i)}, vectors[i], "vectors[%d] must be the embedding of texts[%d]", i, i)
	}
	assert.Equal(t, int32(2), atomic.LoadInt32(&reqCount), "texts exceeding the batch size must be split into multiple requests")
}

func TestOpenAIEmbedder_EmbedDocuments_EmptyInput(t *testing.T) {
	embedder, err := newOpenAIEmbedder(Config{Model: "text-embedding-test", APIKey: "k"})
	require.NoError(t, err)
	vectors, err := embedder.EmbedDocuments(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, vectors)
}

func TestNewOpenAIChatClient_RequiresModel(t *testing.T) {
	_, err := newOpenAIChatClient(Config{APIKey: "k"})
	assert.Error(t, err)
}

func TestNewOpenAIEmbedder_RequiresModel(t *testing.T) {
	_, err := newOpenAIEmbedder(Config{APIKey: "k"})
	assert.Error(t, err)
}
