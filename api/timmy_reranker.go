package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Reranker reorders a list of documents by relevance to a query.
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]RerankResult, error)
}

// RerankResult holds a single document's reranking outcome.
type RerankResult struct {
	Index    int     // original index in the documents slice
	Score    float64 // relevance score (higher = more relevant)
	Document string  // the document text
}

// APIReranker calls an HTTP reranker endpoint compatible with Cohere/Jina/vLLM.
type APIReranker struct {
	httpClient *http.Client
	baseURL    string
	model      string
	apiKey     string
	topK       int
}

// NewAPIReranker creates an APIReranker. httpClient may be nil (uses http.DefaultClient).
func NewAPIReranker(baseURL, model, apiKey string, topK int, httpClient *http.Client) *APIReranker {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &APIReranker{
		httpClient: httpClient,
		baseURL:    baseURL,
		model:      model,
		apiKey:     apiKey,
		topK:       topK,
	}
}

// rerankRequest is the JSON body sent to the rerank endpoint.
type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

// rerankResponseItem is one entry in the API response results array.
type rerankResponseItem struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// rerankResponse is the full API response body.
type rerankResponse struct {
	Results []rerankResponseItem `json:"results"`
}

// Rerank sends documents to the reranker API and returns them ordered by relevance.
// Returns nil, nil when documents is empty.
func (r *APIReranker) Rerank(ctx context.Context, query string, documents []string) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	logger := slogging.Get()

	tracer := otel.Tracer("tmi.timmy")
	ctx, span := tracer.Start(ctx, "timmy.rerank")
	defer span.End()

	span.SetAttributes(
		attribute.String("tmi.timmy.rerank_model", r.model),
		attribute.Int("tmi.timmy.document_count", len(documents)),
	)

	reqBody := rerankRequest{
		Model:     r.model,
		Query:     query,
		Documents: documents,
		TopN:      r.topK,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("reranker: failed to marshal request: %w", err)
	}

	endpoint := r.baseURL + "/rerank"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reranker: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	logger.Debug("reranker: sending request to %s (model=%s, document_count=%d)", endpoint, r.model, len(documents))

	resp, err := r.httpClient.Do(req) //nolint:gosec // G704 - URL is from operator configuration, not user input
	if err != nil {
		return nil, fmt.Errorf("reranker: HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reranker: failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reranker: API returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed rerankResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("reranker: failed to parse response: %w", err)
	}

	results := make([]RerankResult, 0, len(parsed.Results))
	for _, item := range parsed.Results {
		if item.Index < 0 || item.Index >= len(documents) {
			return nil, fmt.Errorf("reranker: response index %d out of bounds (document count: %d)", item.Index, len(documents))
		}
		results = append(results, RerankResult{
			Index:    item.Index,
			Score:    item.RelevanceScore,
			Document: documents[item.Index],
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	span.SetAttributes(attribute.Int("tmi.timmy.result_count", len(results)))
	logger.Debug("reranker: received %d results", len(results))

	return results, nil
}
