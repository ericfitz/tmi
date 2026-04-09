package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const httpSourceMaxBody = 50 * 1024 * 1024 // 50 MiB

// HTTPSource implements ContentSource for HTTP and HTTPS URIs.
// It enforces SSRF protection via URIValidator and limits response bodies to 50 MiB.
type HTTPSource struct {
	ssrfValidator *URIValidator
	client        *http.Client
}

// NewHTTPSource creates a new HTTPSource with the given SSRF validator.
func NewHTTPSource(ssrfValidator *URIValidator) *HTTPSource {
	return &HTTPSource{
		ssrfValidator: ssrfValidator,
		client: &http.Client{
			Timeout:   60 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// Name returns the source name.
func (s *HTTPSource) Name() string { return "http" }

// CanHandle returns true for URIs with an http:// or https:// scheme.
func (s *HTTPSource) CanHandle(_ context.Context, uri string) bool {
	return strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://")
}

// Fetch validates the URI against SSRF rules, fetches it, and returns the raw body bytes
// along with the Content-Type header value. Returns an error for non-2xx responses.
func (s *HTTPSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	if err := s.ssrfValidator.Validate(uri); err != nil {
		return nil, "", fmt.Errorf("SSRF check failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req) //nolint:gosec // URL validated by SSRFValidator
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, uri)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, httpSourceMaxBody))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	return data, resp.Header.Get("Content-Type"), nil
}
