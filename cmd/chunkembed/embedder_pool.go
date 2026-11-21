package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/ericfitz/tmi/internal/llm"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

// allowedHostsEnv names the env var holding the comma-separated hostnames the
// worker may send embedding requests (and its API key) to when a job envelope
// supplies its own endpoint. The host of the env base URL
// (TMI_EMBEDDING_BASE_URL) is always allowed, so leaving this empty means
// "only the env base URL's host".
const allowedHostsEnv = "TMI_EMBEDDING_ALLOWED_HOSTS"

// reasonEndpointNotAllowed is the failed-result reason code for a stamped
// endpoint whose host is not on the allowlist. Syntactically valid per
// jobenvelope.ValidateResult (lowercase letters and underscores).
const reasonEndpointNotAllowed = "embedding_endpoint_not_allowed"

// errEndpointNotAllowed marks a terminal, non-retryable job failure: the
// envelope asked the worker to send its API key to a host it does not trust.
var errEndpointNotAllowed = errors.New("embedding endpoint host not allowed")

// embedderPool resolves the embedder for a job and keeps one embedder per
// (model, base URL). The API key always comes from the worker's own env, never
// from the envelope.
type embedderPool struct {
	env     embedConfig
	allowed map[string]struct{}
	build   func(embedConfig) (llm.Embedder, error)

	mu    sync.Mutex
	cache map[embedderKey]llm.Embedder
}

type embedderKey struct{ model, baseURL string }

// newEmbedderPool builds the pool. allowedHosts is the raw comma-separated
// allowlist (may be empty). The env base URL must itself parse to a host.
func newEmbedderPool(env embedConfig, allowedHosts string,
	build func(embedConfig) (llm.Embedder, error)) (*embedderPool, error) {
	envHost, err := hostOf(env.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("chunkembed: TMI_EMBEDDING_BASE_URL: %w", err)
	}
	allowed := parseAllowedHosts(allowedHosts)
	allowed[envHost] = struct{}{}
	return &embedderPool{
		env:     env,
		allowed: allowed,
		build:   build,
		cache:   make(map[embedderKey]llm.Embedder),
	}, nil
}

// parseAllowedHosts splits a comma-separated hostname list into a lowercase set.
func parseAllowedHosts(csv string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, h := range strings.Split(csv, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			out[h] = struct{}{}
		}
	}
	return out
}

// hostOf returns the lowercase hostname of an http(s) URL. Errors never echo
// the URL, which may carry credentials.
func hostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("unparseable URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("URL scheme must be http or https")
	}
	h := strings.ToLower(u.Hostname())
	if h == "" {
		return "", errors.New("URL has no host")
	}
	return h, nil
}

// forJob returns the embedder for the job and the stamped dimension (0 when
// the envelope carries none). A stamped endpoint is followed only when its
// host is allowed; otherwise it returns errEndpointNotAllowed. An absent
// field falls back to the env configuration.
func (p *embedderPool) forJob(job jobenvelope.Job) (llm.Embedder, int, error) {
	prof := (*jobenvelope.EmbeddingProfile)(nil)
	if job.Config != nil {
		prof = job.Config.Embedding
	}
	if prof == nil {
		emb, err := p.get(p.env.Model, p.env.BaseURL)
		return emb, 0, err
	}
	host, err := hostOf(prof.Endpoint)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %s", errEndpointNotAllowed, err.Error())
	}
	if _, ok := p.allowed[host]; !ok {
		return nil, 0, fmt.Errorf("%w: host %q", errEndpointNotAllowed, host)
	}
	emb, err := p.get(prof.Model, prof.Endpoint)
	return emb, prof.Dimension, err
}

func (p *embedderPool) get(model, baseURL string) (llm.Embedder, error) {
	key := embedderKey{model: model, baseURL: baseURL}
	p.mu.Lock()
	defer p.mu.Unlock()
	if emb, ok := p.cache[key]; ok {
		return emb, nil
	}
	cfg := p.env
	cfg.Model, cfg.BaseURL = model, baseURL
	emb, err := p.build(cfg)
	if err != nil {
		return nil, err
	}
	p.cache[key] = emb
	return emb, nil
}

// checkDimension warns, without failing the job, when the returned vector
// length differs from the stamped dimension. It reports whether the vectors
// are consistent with it. want <= 0 (not stamped) and no vectors never warn.
func checkDimension(jobID string, want int, vectors [][]float32) bool {
	if want <= 0 || len(vectors) == 0 {
		return true
	}
	if got := len(vectors[0]); got != want {
		slogging.Get().Warn("tmi-chunk-embed: job %s: embedding length %d differs from stamped dimension %d",
			jobID, got, want)
		return false
	}
	return true
}
