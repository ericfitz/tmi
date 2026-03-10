package api

import (
	"html"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// markdownPolicy is the singleton bluemonday policy for markdown content fields.
// It matches the client-side DOMPurify allowlist to ensure consistent HTML handling.
// bluemonday policies are safe for concurrent use after creation.
var markdownPolicy *bluemonday.Policy

// strictPolicy is the singleton bluemonday policy for plain-text fields.
// It strips ALL HTML tags, leaving only text content. Used for fields like
// metadata values that should never contain HTML.
var strictPolicy *bluemonday.Policy

func init() {
	markdownPolicy = createMarkdownSanitizationPolicy()
	strictPolicy = bluemonday.StrictPolicy()
}

// createMarkdownSanitizationPolicy builds a bluemonday policy matching the client's
// DOMPurify ALLOWED_TAGS and ALLOWED_ATTR configuration.
func createMarkdownSanitizationPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// Headings
	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6")

	// Structural
	p.AllowElements("p", "br", "hr", "span", "div")

	// Formatting
	p.AllowElements("strong", "em", "del", "code", "pre")

	// Lists & quotes
	p.AllowElements("ul", "ol", "li", "blockquote")

	// Links
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("target").Matching(regexp.MustCompile(`^_(blank|self|parent|top)$`)).OnElements("a")
	p.AllowAttrs("rel").Matching(regexp.MustCompile(`^[a-zA-Z\s-]+$`)).OnElements("a")
	p.RequireNoFollowOnLinks(true)

	// Images
	p.AllowAttrs("src", "alt", "title", "width", "height").OnElements("img")
	p.AllowImages()

	// Tables
	p.AllowElements("table", "colgroup", "col", "thead", "tbody", "tr", "th", "td")
	p.AllowTables()

	// Checkboxes (task lists)
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")

	// SVG elements
	p.AllowElements("svg", "path", "g", "rect", "circle", "line", "polygon", "text", "tspan")

	// SVG attributes on svg element
	p.AllowAttrs("viewBox", "xmlns", "width", "height").OnElements("svg")

	// SVG presentation attributes on shape/text elements
	svgElements := []string{"svg", "path", "g", "rect", "circle", "line", "polygon", "text", "tspan"}
	p.AllowAttrs("fill", "stroke", "stroke-width").OnElements(svgElements...)
	p.AllowAttrs("transform").OnElements(svgElements...)

	// SVG geometry attributes
	p.AllowAttrs("d").OnElements("path")
	p.AllowAttrs("x", "y", "width", "height").OnElements("rect")
	p.AllowAttrs("cx", "cy", "r").OnElements("circle")
	p.AllowAttrs("x1", "y1", "x2", "y2").OnElements("line")
	p.AllowAttrs("points").OnElements("polygon")
	p.AllowAttrs("x", "y").OnElements("text", "tspan")

	// Global attributes from DOMPurify ALLOWED_ATTR
	p.AllowAttrs("class", "id", "title").Globally()
	p.AllowAttrs("style").Globally()
	p.AllowAttrs("data-line", "data-sourcepos").Globally()

	return p
}

// SanitizeMarkdownContent applies the bluemonday HTML sanitization policy
// to a markdown content string. Safe HTML (per the allowlist) is preserved;
// dangerous elements and attributes are stripped.
func SanitizeMarkdownContent(content string) string {
	if content == "" {
		return content
	}
	return markdownPolicy.Sanitize(content)
}

// codeBlockRegex matches fenced code blocks (```...```)
var codeBlockRegex = regexp.MustCompile("(?s)```[^`]*```")

// inlineCodeRegex matches inline code (`...`)
var inlineCodeRegex = regexp.MustCompile("`[^`]+`")

// SanitizePlainText strips ALL HTML tags from a string, leaving only text content.
// Use this for plain-text fields (e.g., metadata values) that should never contain HTML.
// Unlike SanitizeMarkdownContent, this does not preserve any HTML elements.
// HTML entities are decoded first so that entity-encoded tags (e.g., &lt;script&gt;)
// become real tags before sanitization strips them. The result is then unescaped
// again so that legitimate text like "0.0.0.0/0 -> NAT" is stored verbatim rather
// than as "0.0.0.0/0 -&gt; NAT".
func SanitizePlainText(s string) string {
	if s == "" {
		return s
	}
	// Fully decode HTML entities before sanitization so that entity-encoded
	// tags (e.g., &lt;script&gt; or multi-layer &amp;lt;script&amp;gt;)
	// become real tags that bluemonday can strip. This prevents a
	// double-decode bypass where entities survive sanitization and are
	// later decoded into live HTML.
	decoded := s
	for i := 0; i < 10; i++ {
		next := html.UnescapeString(decoded)
		if next == decoded {
			break
		}
		decoded = next
	}
	stripped := strictPolicy.Sanitize(decoded)
	// Unescape again so that bluemonday's entity-encoding of legitimate
	// characters (e.g., & → &amp;, > in "->") is reversed for storage.
	return html.UnescapeString(stripped)
}

// SanitizeMetadataSlice sanitizes all values in a metadata slice using SanitizePlainText.
// Returns an error if any value fails template injection validation after sanitization.
func SanitizeMetadataSlice(metadata *[]Metadata) error {
	if metadata == nil {
		return nil
	}
	for i := range *metadata {
		sanitized := SanitizePlainText((*metadata)[i].Value)
		if err := CheckHTMLInjection(sanitized, "value"); err != nil {
			return err
		}
		(*metadata)[i].Value = sanitized
	}
	return nil
}

// SanitizeDiagramCellMetadata sanitizes metadata values in all cells of a diagram.
// Processes both Node and Edge cell types. Returns an error if any value fails validation.
// Uses the shape discriminator to determine cell type, preventing node corruption
// that can occur when AsNode() fails (e.g., position validation) and the code
// falls through to AsEdge(), which rewrites nodes with edge-specific fields.
func SanitizeDiagramCellMetadata(cells []DfdDiagram_Cells_Item) error {
	for i := range cells {
		// Use discriminator to determine cell type rather than try-and-fallthrough
		disc, err := cells[i].Discriminator()
		if err != nil {
			continue
		}

		if disc == string(EdgeShapeFlow) {
			// Edge cell
			if edge, err := cells[i].AsEdge(); err == nil {
				if edge.Data != nil && edge.Data.Metadata != nil {
					if sanitizeErr := SanitizeMetadataSlice(edge.Data.Metadata); sanitizeErr != nil {
						return sanitizeErr
					}
					_ = SafeFromEdge(&cells[i], edge)
				}
			}
		} else {
			// Node cell (actor, process, store, security-boundary, text-box)
			if node, err := cells[i].AsNode(); err == nil {
				if node.Data != nil && node.Data.Metadata != nil {
					if sanitizeErr := SanitizeMetadataSlice(node.Data.Metadata); sanitizeErr != nil {
						return sanitizeErr
					}
					_ = SafeFromNode(&cells[i], node)
				}
			}
		}
	}
	return nil
}

// SanitizeOptionalString sanitizes an optional string field using SanitizePlainText.
// Returns nil if input is nil. Use for *string fields like Description, IssueUri, Mitigation.
func SanitizeOptionalString(s *string) *string {
	if s == nil {
		return nil
	}
	sanitized := SanitizePlainText(*s)
	return &sanitized
}

// SanitizePatchOperations sanitizes string values in JSON Patch operations
// for the specified field paths using SanitizePlainText.
// Only "replace" and "add" operations are sanitized.
func SanitizePatchOperations(operations []PatchOperation, paths []string) {
	pathSet := make(map[string]bool, len(paths))
	for _, p := range paths {
		pathSet[p] = true
	}
	for i, op := range operations {
		if (op.Op == string(Replace) || op.Op == string(Add)) && pathSet[op.Path] {
			if content, ok := op.Value.(string); ok {
				operations[i].Value = SanitizePlainText(content)
			}
		}
	}
}

// validateTemplateInjectionInMarkdown checks for server-side template injection
// patterns in markdown content. Code blocks are stripped first to avoid false
// positives from code examples. This covers patterns that bluemonday does not
// handle (template expressions are not HTML).
func validateTemplateInjectionInMarkdown(content string) error {
	// Remove code blocks to avoid false positives
	contentWithoutCodeBlocks := codeBlockRegex.ReplaceAllString(content, "")
	contentWithoutCode := inlineCodeRegex.ReplaceAllString(contentWithoutCodeBlocks, "")

	// Check template injection patterns (reuses patterns from html_injection_checker.go)
	for _, tp := range templateInjectionPatterns {
		if strings.Contains(contentWithoutCode, tp.pattern) {
			return InvalidInputError(
				"Field 'content' contains potentially unsafe " + tp.desc +
					" pattern (" + tp.pattern + ")")
		}
	}
	return nil
}
