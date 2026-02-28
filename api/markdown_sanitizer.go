package api

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// markdownPolicy is the singleton bluemonday policy for markdown content fields.
// It matches the client-side DOMPurify allowlist to ensure consistent HTML handling.
// bluemonday policies are safe for concurrent use after creation.
var markdownPolicy *bluemonday.Policy

func init() {
	markdownPolicy = createMarkdownSanitizationPolicy()
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
