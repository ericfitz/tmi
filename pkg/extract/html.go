package extract

import (
	"strings"

	"golang.org/x/net/html"
)

// HTMLExtractor extracts visible text from HTML content,
// stripping script and style elements.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: extracts visible text from HTML content, excluding script and style elements (pure)
type HTMLExtractor struct{}

// NewHTMLExtractor creates a new HTMLExtractor.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: build a new HTMLExtractor (pure)
func NewHTMLExtractor() *HTMLExtractor { return &HTMLExtractor{} }

// Name returns the extractor name.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: return the extractor's identifying name (pure)
func (e *HTMLExtractor) Name() string { return "html" }

// CanHandle returns true for text/html content types.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: report whether the content type is text/html (pure)
func (e *HTMLExtractor) CanHandle(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/html")
}

// Extract strips HTML tags and returns the visible text.
// Script and style element content is excluded.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parse HTML bytes and return visible text as extracted content (pure)
func (e *HTMLExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	text := ExtractTextFromHTML(string(data))
	return ExtractedContent{
		Text:        text,
		ContentType: contentType,
	}, nil
}

// ExtractTextFromHTML parses an HTML document and returns the concatenated visible text,
// skipping content inside <script> and <style> elements. This is the single
// source of truth shared by the HTMLExtractor and the api HTTP content provider.
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: parse an HTML document and concatenate visible text, skipping script and style subtrees (pure)
func ExtractTextFromHTML(htmlContent string) string {
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
