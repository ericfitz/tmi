package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/internal/llm"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

type stubEmbedder struct{ id string }

func (s *stubEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1}
	}
	return out, nil
}
func (s *stubEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{1}, nil
}

func newTestPool(t *testing.T, allowed string) (*embedderPool, *[]embedConfig) {
	t.Helper()
	var built []embedConfig
	p, err := newEmbedderPool(
		embedConfig{Model: "env-model", BaseURL: "https://env.example.com/v1", APIKey: "k"},
		allowed,
		func(c embedConfig) (llm.Embedder, error) {
			built = append(built, c)
			return &stubEmbedder{id: c.Model + "@" + c.BaseURL}, nil
		})
	if err != nil {
		t.Fatalf("newEmbedderPool: %v", err)
	}
	return p, &built
}

func jobWithProfile(model, endpoint string, dim int) jobenvelope.Job {
	return jobenvelope.Job{JobID: "j1", Config: &jobenvelope.Config{
		Embedding: &jobenvelope.EmbeddingProfile{Model: model, Endpoint: endpoint, Dimension: dim},
	}}
}

func TestPoolFallsBackToEnvWhenFieldAbsent(t *testing.T) {
	p, built := newTestPool(t, "")
	for _, j := range []jobenvelope.Job{{JobID: "a"}, {JobID: "b", Config: &jobenvelope.Config{}}} {
		emb, dim, err := p.forJob(j)
		if err != nil {
			t.Fatalf("forJob: %v", err)
		}
		if dim != 0 {
			t.Fatalf("expected no expected dimension, got %d", dim)
		}
		if got := emb.(*stubEmbedder).id; got != "env-model@https://env.example.com/v1" {
			t.Fatalf("expected env embedder, got %s", got)
		}
	}
	if len(*built) != 1 {
		t.Fatalf("expected one cached embedder, built %d", len(*built))
	}
}

func TestPoolFollowsAllowlistedEndpoint(t *testing.T) {
	p, built := newTestPool(t, " Embed.Example.org , other.example.org ")
	emb, dim, err := p.forJob(jobWithProfile("new-model", "https://embed.example.org/v1", 1024))
	if err != nil {
		t.Fatalf("forJob: %v", err)
	}
	if dim != 1024 {
		t.Fatalf("dim = %d, want 1024", dim)
	}
	if got := emb.(*stubEmbedder).id; got != "new-model@https://embed.example.org/v1" {
		t.Fatalf("expected stamped embedder, got %s", got)
	}
	// The API key always comes from the worker env.
	if (*built)[0].APIKey != "k" {
		t.Fatalf("stamped embedder must use the env API key")
	}
	// Same (model, URL) reuses the embedder; a different model builds another.
	emb2, _, _ := p.forJob(jobWithProfile("new-model", "https://embed.example.org/v1", 1024))
	if emb2 != emb {
		t.Fatalf("expected cached embedder to be reused")
	}
	if _, _, err := p.forJob(jobWithProfile("model-2", "https://embed.example.org/v1", 1024)); err != nil {
		t.Fatalf("forJob: %v", err)
	}
	if len(*built) != 2 {
		t.Fatalf("expected 2 embedders, built %d", len(*built))
	}
}

func TestPoolEnvHostAllowedByDefault(t *testing.T) {
	p, _ := newTestPool(t, "")
	emb, _, err := p.forJob(jobWithProfile("stamped-model", "https://env.example.com/other/path", 8))
	if err != nil {
		t.Fatalf("env base URL host must be allowed by default: %v", err)
	}
	if got := emb.(*stubEmbedder).id; got != "stamped-model@https://env.example.com/other/path" {
		t.Fatalf("got %s", got)
	}
}

func TestPoolRejectsNonAllowlistedEndpoint(t *testing.T) {
	p, built := newTestPool(t, "embed.example.org")
	bad := []string{
		"https://evil.example.net/v1",
		"https://embed.example.org.evil.net/v1", // suffix trick
		"https://evil.net/v1?h=embed.example.org",
		"https://embed.example.org@evil.net/v1", // userinfo trick
		"ftp://embed.example.org/v1",            // scheme
		"//embed.example.org/v1",                // no scheme
		"not a url",
		"https:///v1",
	}
	for _, ep := range bad {
		_, _, err := p.forJob(jobWithProfile("m", ep, 1))
		if !errors.Is(err, errEndpointNotAllowed) {
			t.Errorf("endpoint %q: want errEndpointNotAllowed, got %v", ep, err)
		}
	}
	if len(*built) != 0 {
		t.Fatalf("no embedder may be built for a rejected endpoint")
	}
}

func TestPoolErrorNamesHostNotSecrets(t *testing.T) {
	p, _ := newTestPool(t, "")
	_, _, err := p.forJob(jobWithProfile("m", "https://user:pw@evil.example.net/v1?key=sekrit", 1))
	if !errors.Is(err, errEndpointNotAllowed) {
		t.Fatalf("got %v", err)
	}
	msg := err.Error()
	for _, leak := range []string{"pw", "sekrit", "user:"} {
		if containsStr(msg, leak) {
			t.Fatalf("error leaks %q: %s", leak, msg)
		}
	}
	if !containsStr(msg, "evil.example.net") {
		t.Fatalf("error should name the host: %s", msg)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCheckDimension(t *testing.T) {
	vecs := [][]float32{{1, 2, 3}, {1, 2, 3}}
	if !checkDimension("j", 3, vecs) {
		t.Fatal("matching dimension reported as mismatch")
	}
	if !checkDimension("j", 0, vecs) {
		t.Fatal("unset expected dimension must not warn")
	}
	if checkDimension("j", 4, vecs) {
		t.Fatal("mismatch not detected")
	}
	if !checkDimension("j", 4, nil) {
		t.Fatal("no vectors, nothing to compare")
	}
}

func TestParseAllowedHosts(t *testing.T) {
	got := parseAllowedHosts(" A.example.com,, b.example.com ,")
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if _, ok := got["a.example.com"]; !ok {
		t.Fatalf("expected lowercase trimmed entry: %v", got)
	}
}
