package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// HTTPContentProvider fetches and extracts plain text from HTTP/HTTPS URLs.
// It enforces SSRF protection and limits response body reads to 10 MiB.
type HTTPContentProvider struct {
	ssrfValidator *URIValidator
	client        *http.Client
}

// NewHTTPContentProvider creates a new HTTPContentProvider with the given SSRF validator.
func NewHTTPContentProvider(ssrfValidator *URIValidator) *HTTPContentProvider {
	return &HTTPContentProvider{
		ssrfValidator: ssrfValidator,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the provider name for logging.
func (p *HTTPContentProvider) Name() string { return "http-html" }

// CanHandle returns true for entity references with an http:// or https:// URI.
func (p *HTTPContentProvider) CanHandle(_ context.Context, ref EntityReference) bool {
	if ref.URI == "" {
		return false
	}
	return strings.HasPrefix(ref.URI, "http://") || strings.HasPrefix(ref.URI, "https://")
}

// Extract fetches the URL, enforces SSRF protection, and returns extracted plain text.
// HTML responses have tags stripped; other content types are returned as-is.
func (p *HTTPContentProvider) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	if err := p.ssrfValidator.Validate(ref.URI); err != nil {
		return ExtractedContent{}, fmt.Errorf("SSRF check failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.URI, nil)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req) //nolint:gosec // URL is validated by SSRFValidator before reaching this point
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	const maxBodySize = 10 * 1024 * 1024 // 10 MiB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to read response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	var text string
	if strings.Contains(contentType, "text/html") {
		text = extractTextFromHTML(string(body))
	} else {
		text = string(body)
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
	var extractText func(*html.Node)
	extractText = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
		}
		// Skip the children of script and style elements
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractText(c)
		}
	}
	extractText(doc)
	return strings.TrimSpace(sb.String())
}
