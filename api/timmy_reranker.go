package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Reranker reorders a list of documents by relevance to a query.
// SEM@907448abd6162d78125a4d628a7b26110fe7939d: interface for reordering documents by relevance to a query (pure)
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]RerankResult, error)
}

// RerankResult holds a single document's reranking outcome.
// SEM@907448abd6162d78125a4d628a7b26110fe7939d: holds a document's original index, relevance score, and text after reranking (pure)
type RerankResult struct {
	Index    int     // original index in the documents slice
	Score    float64 // relevance score (higher = more relevant)
	Document string  // the document text
}

// APIReranker calls an HTTP reranker endpoint compatible with Cohere/Jina/vLLM.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: HTTP reranker client compatible with Cohere/Jina/vLLM rerank endpoints (pure)
type APIReranker struct {
	client  *SafeHTTPClient
	baseURL string
	model   string
	apiKey  string
	topK    int
	timeout time.Duration
}

// NewAPIReranker creates an APIReranker. validator MUST be non-nil and is used
// to validate the reranker endpoint URL against scheme/SSRF allowlist rules
// before each call. timeout sets the per-request overall timeout (0 → 120s).
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: build an APIReranker with SSRF-safe HTTP client and configurable timeout (pure)
func NewAPIReranker(baseURL, model, apiKey string, topK int, validator *URIValidator, timeout time.Duration) *APIReranker {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &APIReranker{
		client: NewSafeHTTPClient(
			validator,
			WithDefaultTimeouts(timeout, 30*time.Second, 10*1024*1024),
		),
		baseURL: baseURL,
		model:   model,
		apiKey:  apiKey,
		topK:    topK,
		timeout: timeout,
	}
}

// rerankRequest is the JSON body sent to the rerank endpoint.
// SEM@907448abd6162d78125a4d628a7b26110fe7939d: JSON request body sent to the external rerank API endpoint (pure)
type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

// rerankResponseItem is one entry in the API response results array.
// SEM@907448abd6162d78125a4d628a7b26110fe7939d: single entry in the rerank API response results array (pure)
type rerankResponseItem struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// rerankResponse is the full API response body.
// SEM@907448abd6162d78125a4d628a7b26110fe7939d: full JSON response body from the external rerank API (pure)
type rerankResponse struct {
	Results []rerankResponseItem `json:"results"`
}

// Rerank sends documents to the reranker API and returns them ordered by relevance.
// Returns nil, nil when documents is empty.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: fetch document relevance scores from the reranker API and return results sorted by score descending
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
	logger.Debug("reranker: sending request to %s (model=%s, document_count=%d)", endpoint, r.model, len(documents))

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		headers.Set("Authorization", "Bearer "+r.apiKey)
	}

	result, err := r.client.Fetch(ctx, endpoint, SafeFetchOptions{
		Method:       http.MethodPost,
		Body:         bytes.NewReader(bodyBytes),
		Headers:      headers,
		Timeout:      r.timeout,
		MaxBodyBytes: 10 * 1024 * 1024, // 10 MiB cap on rerank response (defensive)
	})
	if err != nil {
		return nil, fmt.Errorf("reranker: HTTP request failed: %w", err)
	}

	if result.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reranker: API returned status %d: %s", result.StatusCode, string(result.Body))
	}

	var parsed rerankResponse
	if err := json.Unmarshal(result.Body, &parsed); err != nil {
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
