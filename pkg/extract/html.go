package extract

import (
	"strings"

	"golang.org/x/net/html"
)

// HTMLExtractor extracts visible text from HTML content.
// It reuses extractTextFromHTML from timmy_content_provider_http.go,
// which strips script and style elements.
type HTMLExtractor struct{}

// NewHTMLExtractor creates a new HTMLExtractor.
func NewHTMLExtractor() *HTMLExtractor { return &HTMLExtractor{} }

// Name returns the extractor name.
func (e *HTMLExtractor) Name() string { return "html" }

// CanHandle returns true for text/html content types.
func (e *HTMLExtractor) CanHandle(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/html")
}

// Extract strips HTML tags and returns the visible text.
// Script and style element content is excluded.
func (e *HTMLExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	text := extractTextFromHTML(string(data))
	return ExtractedContent{
		Text:        text,
		ContentType: contentType,
	}, nil
}

// extractTextFromHTML parses an HTML document and returns the concatenated visible text,
// skipping content inside <script> and <style> elements.
// Self-contained copy for pkg/extract; the monolith keeps its own copy in
// api/timmy_content_provider_http.go for the HTTP provider's use.
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
