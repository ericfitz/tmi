package api

import "strings"

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
