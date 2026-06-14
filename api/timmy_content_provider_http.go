package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/net/html"
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
		text = extractTextFromHTML(body)
	} else {
		text = body
	}

	return ExtractedContent{
		Text:        text,
		Title:       ref.Name,
		ContentType: contentType,
	}, nil
}

// extractTextFromHTML parses an HTML document and returns the concatenated visible text,
// skipping content inside <script> and <style> elements.
func extractTextFromHTML(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		// Fall back to raw content if parsing fails
		return htmlContent
	}
	var sb strings.Builder
	// Iterative pre-order traversal using the tree's own parent/sibling
	// links (O(1) extra memory). A recursive walk here is remotely
	// triggerable stack exhaustion: nesting depth is attacker-controlled
	// and a Go stack overflow is an unrecoverable fatal error.
	for n := doc; n != nil; {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
		}
		// Skip the children of script and style elements
		skipChildren := n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style")
		if !skipChildren && n.FirstChild != nil {
			n = n.FirstChild
			continue
		}
		// Subtree finished: advance to the next sibling, climbing back
		// toward the root as ancestors complete. Never ascend above doc.
		for n != doc && n.NextSibling == nil {
			n = n.Parent
		}
		if n == doc {
			break
		}
		n = n.NextSibling
	}
	return strings.TrimSpace(sb.String())
}
