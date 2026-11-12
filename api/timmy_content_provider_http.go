package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/ericfitz/tmi/pkg/extract"
)

// HTTPEmbeddingSource fetches and extracts plain text from HTTP/HTTPS URLs.
// It enforces SSRF protection (including DNS-pin defense) via SafeHTTPClient
// and limits response body reads to 10 MiB.
type HTTPEmbeddingSource struct {
	client *SafeHTTPClient
}

// NewHTTPEmbeddingSource creates a new HTTPEmbeddingSource with the given SSRF validator.
func NewHTTPEmbeddingSource(ssrfValidator *URIValidator) *HTTPEmbeddingSource {
	return &HTTPEmbeddingSource{
		client: NewSafeHTTPClient(
			ssrfValidator,
			WithTransportWrapper(func(rt http.RoundTripper) http.RoundTripper {
				return otelhttp.NewTransport(rt)
			}),
			WithDefaultTimeouts(30*time.Second, 10*time.Second, 10*1024*1024),
		),
	}
}

// Name returns the provider name for logging.
func (p *HTTPEmbeddingSource) Name() string { return "http-html" }

// CanHandle returns true for entity references with an http:// or https:// URI.
func (p *HTTPEmbeddingSource) CanHandle(_ context.Context, ref EntityReference) bool {
	if ref.URI == "" {
		return false
	}
	return strings.HasPrefix(ref.URI, "http://") || strings.HasPrefix(ref.URI, "https://")
}

// Extract fetches the URL via the egress helper (DNS-pinned, SSRF-checked) and
// returns extracted plain text. HTML responses have tags stripped; other content
// types are returned as-is.
func (p *HTTPEmbeddingSource) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	result, err := p.client.Fetch(ctx, ref.URI, SafeFetchOptions{
		MaxBodyBytes: 10 * 1024 * 1024,
	})
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("SSRF check failed: %w", err)
	}

	contentType := result.Header.Get("Content-Type")
	body := string(result.Body)
	var text string
	if strings.Contains(contentType, "text/html") {
		text = extract.ExtractTextFromHTML(body)
	} else {
		text = body
	}

	return ExtractedContent{
		Text:        text,
		Title:       ref.Name,
		ContentType: contentType,
	}, nil
}
