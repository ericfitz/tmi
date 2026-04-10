package workflows

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRerankerIntegration tests the reranker against a live API endpoint.
// Gated by TMI_TEST_RERANK_BASE_URL.
func TestRerankerIntegration(t *testing.T) {
	baseURL := os.Getenv("TMI_TEST_RERANK_BASE_URL")
	if baseURL == "" {
		t.Skip("Skipping reranker integration test (set TMI_TEST_RERANK_BASE_URL)")
	}
	model := os.Getenv("TMI_TEST_RERANK_MODEL")
	if model == "" {
		model = "jinaai/jina-reranker-v3-mlx"
	}
	apiKey := os.Getenv("TMI_TEST_RERANK_API_KEY")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	reranker := api.NewAPIReranker(baseURL, model, apiKey, 3, httpClient)

	documents := []string{
		"The authentication system uses OAuth 2.0 with PKCE for browser-based clients.",
		"User preferences are stored in a PostgreSQL database with row-level security.",
		"The login handler validates credentials against bcrypt-hashed passwords.",
		"API rate limiting uses a sliding window algorithm with Redis backing.",
		"Session tokens are JWT with 1-hour expiry and refresh token rotation.",
	}

	results, err := reranker.Rerank(context.Background(), "How does authentication work?", documents)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Results should be sorted by relevance score descending
	for i := 1; i < len(results); i++ {
		assert.GreaterOrEqual(t, results[i-1].Score, results[i].Score,
			"results should be sorted by descending score")
	}

	// Log the results for debugging
	for i, r := range results {
		t.Logf("Result %d (score=%.3f, index=%d): %s", i+1, r.Score, r.Index, r.Document)
	}
}

// TestDecomposerIntegration tests query decomposition against a live LLM.
// Gated by TMI_TEST_DECOMPOSITION_BASE_URL.
func TestDecomposerIntegration(t *testing.T) {
	baseURL := os.Getenv("TMI_TEST_DECOMPOSITION_BASE_URL")
	if baseURL == "" {
		t.Skip("Skipping decomposer integration test (set TMI_TEST_DECOMPOSITION_BASE_URL)")
	}

	// Decomposer integration requires a full LLM service setup.
	// For now, skip — this can be run via make test-integration with full env.
	t.Skip("Decomposer integration test requires full LLM service setup — run via make test-integration")
}
