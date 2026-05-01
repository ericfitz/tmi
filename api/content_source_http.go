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
// Outbound requests go through SafeHTTPClient which pins the validated IP at
// dial time (DNS-rebinding defense). Response bodies are capped at 50 MiB.
type HTTPSource struct {
	client *SafeHTTPClient
}

// NewHTTPSource creates a new HTTPSource with the given SSRF validator.
func NewHTTPSource(ssrfValidator *URIValidator) *HTTPSource {
	return &HTTPSource{
		client: NewSafeHTTPClient(
			ssrfValidator,
			WithTransportWrapper(func(rt http.RoundTripper) http.RoundTripper {
				return otelhttp.NewTransport(rt)
			}),
			WithDefaultTimeouts(60*time.Second, 15*time.Second, httpSourceMaxBody),
		),
	}
}

// Name returns the source name.
func (s *HTTPSource) Name() string { return ProviderHTTP }

// CanHandle returns true for URIs with an http:// or https:// scheme.
func (s *HTTPSource) CanHandle(_ context.Context, uri string) bool {
	return strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://")
}

// Fetch validates the URI against SSRF rules, fetches it via the egress helper
// (DNS-pinned), and returns the raw body bytes along with the Content-Type
// header value. Returns an error for non-2xx responses.
func (s *HTTPSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	resp, err := s.client.FetchStreaming(ctx, uri, SafeFetchOptions{
		MaxBodyBytes: httpSourceMaxBody,
	})
	if err != nil {
		return nil, "", fmt.Errorf("SSRF check failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, uri)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	return data, resp.Header.Get("Content-Type"), nil
}
