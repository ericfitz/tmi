package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIReranker_Rerank verifies that valid responses are parsed correctly,
// results are sorted by score descending, the request body is correct, and
// the Authorization header is set when an API key is provided.
func TestAPIReranker_Rerank(t *testing.T) {
	var capturedBody rerankRequest
	var capturedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuthHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		resp := rerankResponse{
			Results: []rerankResponseItem{
				{Index: 2, RelevanceScore: 0.95},
				{Index: 0, RelevanceScore: 0.72},
				{Index: 1, RelevanceScore: 0.31},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	docs := []string{"apple", "banana", "cherry"}
	reranker := NewAPIReranker(server.URL, "rerank-model", "test-key", 3, server.Client())

	results, err := reranker.Rerank(context.Background(), "fruit query", docs)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Results should be sorted by score descending
	assert.Equal(t, 2, results[0].Index)
	assert.InDelta(t, 0.95, results[0].Score, 1e-9)
	assert.Equal(t, "cherry", results[0].Document)

	assert.Equal(t, 0, results[1].Index)
	assert.InDelta(t, 0.72, results[1].Score, 1e-9)
	assert.Equal(t, "apple", results[1].Document)

	assert.Equal(t, 1, results[2].Index)
	assert.InDelta(t, 0.31, results[2].Score, 1e-9)
	assert.Equal(t, "banana", results[2].Document)

	// Verify request body
	assert.Equal(t, "rerank-model", capturedBody.Model)
	assert.Equal(t, "fruit query", capturedBody.Query)
	assert.Equal(t, docs, capturedBody.Documents)

	// Verify auth header
	assert.Equal(t, "Bearer test-key", capturedAuthHeader)
}

// TestAPIReranker_Rerank_TopN verifies that top_n is correctly sent in the request body.
func TestAPIReranker_Rerank_TopN(t *testing.T) {
	var capturedBody rerankRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		resp := rerankResponse{
			Results: []rerankResponseItem{
				{Index: 0, RelevanceScore: 0.9},
				{Index: 1, RelevanceScore: 0.5},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	docs := []string{"doc-a", "doc-b", "doc-c", "doc-d", "doc-e"}
	reranker := NewAPIReranker(server.URL, "my-model", "", 2, server.Client())

	_, err := reranker.Rerank(context.Background(), "query", docs)
	require.NoError(t, err)

	assert.Equal(t, 2, capturedBody.TopN, "top_n should match the configured topK")
}

// TestAPIReranker_Rerank_HTTPError verifies that a non-200 response produces an error.
func TestAPIReranker_Rerank_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	reranker := NewAPIReranker(server.URL, "model", "key", 5, server.Client())
	_, err := reranker.Rerank(context.Background(), "query", []string{"doc1"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// TestAPIReranker_Rerank_EmptyDocuments verifies that nil documents return nil/nil
// without making any HTTP call.
func TestAPIReranker_Rerank_EmptyDocuments(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reranker := NewAPIReranker(server.URL, "model", "key", 5, server.Client())

	results, err := reranker.Rerank(context.Background(), "query", nil)
	assert.NoError(t, err)
	assert.Nil(t, results)
	assert.Equal(t, 0, callCount, "no HTTP call should be made for empty documents")

	results, err = reranker.Rerank(context.Background(), "query", []string{})
	assert.NoError(t, err)
	assert.Nil(t, results)
	assert.Equal(t, 0, callCount, "no HTTP call should be made for empty documents slice")
}

// TestAPIReranker_Rerank_MalformedJSON verifies that an invalid JSON response produces an error.
func TestAPIReranker_Rerank_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	reranker := NewAPIReranker(server.URL, "model", "key", 5, server.Client())
	_, err := reranker.Rerank(context.Background(), "query", []string{"doc1"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse response")
}
