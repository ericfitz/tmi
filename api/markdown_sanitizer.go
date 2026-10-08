package api

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/microcosm-cc/bluemonday"
	xhtml "golang.org/x/net/html"
)

// markdownPolicy is the singleton bluemonday policy for markdown content fields.
// It matches the client-side DOMPurify allowlist to ensure consistent HTML handling.
// bluemonday policies are safe for concurrent use after creation.
var markdownPolicy *bluemonday.Policy

// strictPolicy is the singleton bluemonday policy for plain-text fields.
// It strips ALL HTML tags, leaving only text content. Used for fields like
// metadata values that should never contain HTML.
var strictPolicy *bluemonday.Policy

// SEM@6c2ed16a87725c4d8e764cde607ac1e06704e3ac: initialize singleton markdown and strict HTML sanitization policies at package load (mutates shared state)
func init() {
	markdownPolicy = createMarkdownSanitizationPolicy()
	strictPolicy = bluemonday.StrictPolicy()
}

// createMarkdownSanitizationPolicy builds a bluemonday policy matching the client's
// DOMPurify ALLOWED_TAGS and ALLOWED_ATTR configuration.
// SEM@d6557548645ee87e8fe5910b447499fc633fbe6b: build a bluemonday allowlist policy matching the client DOMPurify configuration (pure)
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

// SanitizeMarkdownContent strips dangerous HTML from a markdown string while
// leaving all non-markup text byte-for-byte intact (#992).
//
// Markdown is not HTML, so the source is tokenized and only tag-like tokens
// are run through the bluemonday allowlist; text tokens are copied verbatim
// rather than re-serialized, which would entity-encode quotes, apostrophes and
// ampersands. Content of script/style and similar elements is dropped, as
// bluemonday does. Stripping a tag can splice neighbouring text into a new tag
// (e.g. "<<script>script>"), so the pass repeats until stable; if it does not
// settle, fall back to bluemonday's fully escaped output.
//
// Each pass also neutralizes markdown link, image and reference-definition
// destinations whose scheme is not http, https or mailto (#1013); see
// markdown_link_destinations.go.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: sanitize markdown by stripping disallowed HTML and unsafe link destinations while keeping text verbatim (pure)
func SanitizeMarkdownContent(content string) string {
	if content == "" {
		return content
	}
	cur := content
	for i := 0; i < 10; i++ {
		next := neutralizeMarkdownLinkDestinations(stripMarkdownHTML(cur))
		if next == cur {
			return cur
		}
		cur = next
	}
	// The fallback re-serializes the text, which can reassemble a destination
	// that the pass above had just neutralized; neutralize once more.
	return neutralizeMarkdownLinkDestinations(markdownPolicy.Sanitize(cur))
}

// markdownSkipContent lists elements whose entire content is dropped with the tag.
// Void or rarely-closed elements (embed, applet, frame) are left out: skipping
// their "content" would swallow the rest of the note; bluemonday drops the tag.
var markdownSkipContent = map[string]bool{
	"frameset": true, "iframe": true,
	"object": true, "script": true, "style": true, "noscript": true,
}

// SEM@9924c9a931e361fced9cd376fbfd52220c9b0bc5: remove disallowed HTML markup in one pass, copying text tokens unchanged (pure)
func stripMarkdownHTML(content string) string {
	var out strings.Builder
	z := xhtml.NewTokenizer(strings.NewReader(content))
	skip := "" // element whose content is currently being dropped
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			// Any unterminated trailing tag is dropped.
			return out.String()
		}
		raw := string(z.Raw())
		if skip != "" {
			if tt == xhtml.EndTagToken {
				if name, _ := z.TagName(); string(name) == skip {
					skip = ""
				}
			}
			continue
		}
		switch tt {
		case xhtml.TextToken:
			out.WriteString(raw)
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, _ := z.TagName()
			if tt == xhtml.StartTagToken && markdownSkipContent[string(name)] {
				skip = string(name)
				continue
			}
			out.WriteString(markdownPolicy.Sanitize(raw))
		case xhtml.EndTagToken:
			out.WriteString(markdownPolicy.Sanitize(raw))
		default: // comments and doctype are dropped
		}
	}
}

// SanitizeRequiredMarkdownContent sanitizes a markdown field the schema
// declares required with minLength 1, and reports a 400 when sanitization
// emptied it.
//
// The schema's own `pattern` for these fields only excludes control
// characters, so a body like `{"content": "<script>alert(1)</script>"}` passes
// OpenAPI validation, reaches the handler, and is then reduced to "" by the
// policy. Every caller previously handed that empty string straight to its
// store, whose non-empty check failed and surfaced as a 500 — a client input
// problem reported as a server fault, and a Zero-500 policy violation (#605).
// (The sibling `name` field is unaffected because its pattern already rejects
// `<` and `>` at the validation layer.)
//
// This lives here rather than in each handler so the create, update and patch
// paths across all four note resources cannot drift on it — the update paths
// had no such check at all and silently persisted the empty value.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: sanitize a required markdown field, returning a 400 when it empties, nests too deeply, keeps an unsafe link or is too complex to check
func SanitizeRequiredMarkdownContent(field, content string) (string, *RequestError) {
	sanitized := SanitizeMarkdownContent(content)
	// Authoritative gate (#1013): the destination scanner is best effort, so
	// reject whatever a real CommonMark parse still sees as an unsafe link.
	if markdownNestsTooDeeply(sanitized) {
		return "", InvalidInputError(fmt.Sprintf(
			"%s nests block quotes or lists too deeply",
			field,
		))
	}
	if markdownMayHaveUnsafeLink(sanitized) {
		switch markdownGateCheck(sanitized) {
		case markdownGateSafe:
		case markdownGateUnsafe:
			return "", InvalidInputError(fmt.Sprintf(
				"%s contains a link or image with a disallowed URL scheme",
				field,
			))
		default:
			if m := tmiotel.GlobalMetrics; m != nil {
				m.MarkdownGateTooComplex.Add(context.Background(), 1)
			}
			reqErr := InvalidInputError(fmt.Sprintf(
				"%s is too complex to validate",
				field,
			))
			reqErr.markdownTooComplexBytes = len(sanitized)
			return "", reqErr
		}
	}
	if strings.TrimSpace(sanitized) == "" && strings.TrimSpace(content) != "" {
		return "", InvalidInputError(fmt.Sprintf(
			"%s is empty after sanitization; it consisted entirely of markup that is not permitted",
			field,
		))
	}
	return sanitized, nil
}

// SanitizePlainText strips ALL HTML tags from a string, leaving only text content.
// Use this for plain-text fields (e.g., metadata values) that should never contain HTML.
// Unlike SanitizeMarkdownContent, this does not preserve any HTML elements.
// HTML entities are decoded first so that entity-encoded tags (e.g., &lt;script&gt;)
// become real tags before sanitization strips them. The result is then unescaped
// again so that legitimate text like "0.0.0.0/0 -> NAT" is stored verbatim rather
// than as "0.0.0.0/0 -&gt; NAT".
// SEM@b8c2e635d7ace355cadeed6d3b0b853b2c950f75: strip all HTML from a plain-text field, decoding entities first to prevent bypass (pure)
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
// SEM@6c2ed16a87725c4d8e764cde607ac1e06704e3ac: sanitize all values in a metadata slice and validate against HTML injection (pure)
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
// SEM@7bac1ed632ff8929eff543daec4372c53d51283a: sanitize metadata values in all diagram cells using the shape discriminator to avoid type corruption (pure)
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
				if edge.Data != nil && edge.Data.UnderscoreMetadata != nil {
					if sanitizeErr := SanitizeMetadataSlice(edge.Data.UnderscoreMetadata); sanitizeErr != nil {
						return sanitizeErr
					}
					_ = SafeFromEdge(&cells[i], edge)
				}
			}
		} else {
			// Node cell (actor, process, store, security-boundary, text-box)
			if node, err := cells[i].AsNode(); err == nil {
				if node.Data != nil && node.Data.UnderscoreMetadata != nil {
					if sanitizeErr := SanitizeMetadataSlice(node.Data.UnderscoreMetadata); sanitizeErr != nil {
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
// SEM@0c74197df2eb6b51a91fa4592766476fdf09f984: sanitize an optional string field, returning nil if input is nil (pure)
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
// SEM@0c74197df2eb6b51a91fa4592766476fdf09f984: sanitize string values in JSON Patch replace and add operations at specified paths (pure)
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

// noteText points at the text fields of a threat-model, team or project note.
type noteText struct {
	content     *string
	name        *string
	description **string
}

// sanitizePatchedNoteText enforces note sanitization on the entity a JSON
// Patch produced, rather than on individual operations: RFC 6902 copy and move
// can carry a value between fields (e.g. copy /name to /content), so only the
// result can be checked. Note stores call it (through the check functions
// below) on the very entity they are about to persist, so a concurrent write
// between the handler's read and the store's cannot slip a value past it.
// Each field the patch changed is sanitized in place as the create/update path
// does: content as required markdown (a 400 if it empties, keeps an unsafe
// link or is too complex to check), name and description as plain text when
// includePlainText is set (an emptied description becomes nil). A changed
// name is held to the schema's rules on every note type. Unchanged fields are not re-checked, so a legacy
// value cannot block an unrelated edit.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: sanitize the note text fields a JSON Patch changed, in place on the patched entity, rejecting emptied required fields (pure)
func sanitizePatchedNoteText(before, after noteText, includePlainText bool) *RequestError {
	if *after.content != *before.content {
		sanitized, reqErr := SanitizeRequiredMarkdownContent("content", *after.content)
		if reqErr != nil {
			return reqErr
		}
		*after.content = sanitized
	}
	if *after.name != *before.name {
		if includePlainText {
			*after.name = SanitizePlainText(*after.name)
		}
		if reqErr := validatePatchedNoteName(*after.name); reqErr != nil {
			return reqErr
		}
	}
	if !includePlainText {
		return nil
	}
	if desc := *after.description; desc != nil && (*before.description == nil || **before.description != *desc) {
		sanitized := SanitizePlainText(*desc)
		if sanitized == "" {
			// Oracle stores '' as NULL; store nil on every database.
			*after.description = nil
		} else {
			*after.description = &sanitized
		}
	}
	return nil
}

// noteNamePattern is the schema pattern for note names (NoteBase,
// TeamProjectNoteBase); maxNoteNameLength is their maxLength.
var noteNamePattern = regexp.MustCompile(`^[^<>"'&]*$`)

// validatePatchedNoteName holds a name produced by a JSON Patch to the rules
// request validation applies to a name sent directly: copy and move can carry
// any field's value (e.g. content) into it. An empty name would also be NULL on
// Oracle (NOT NULL column, a store error) but saved on PostgreSQL, and an
// over-long one would fail the VARCHAR2(256) column.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: validate a patched note name against the schema's non-empty, length and character rules (pure)
func validatePatchedNoteName(name string) *RequestError {
	switch {
	case strings.TrimSpace(name) == "":
		return InvalidInputError("name is empty after sanitization")
	case utf8.RuneCountInString(name) > maxNoteNameLength:
		return InvalidInputError(fmt.Sprintf("name must be at most %d characters", maxNoteNameLength))
	case !noteNamePattern.MatchString(name):
		return InvalidInputError(`name must not contain <, >, ", ' or &`)
	}
	return nil
}

// checkPatchedNote is the threat-model note store's patch check. It sanitizes
// content only, as the threat-model note create and update paths do, and
// validates a changed name.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: sanitize the content a JSON Patch changed on a threat-model note (pure)
func checkPatchedNote(before, after *Note) error {
	if reqErr := sanitizePatchedNoteText(
		noteText{&before.Content, &before.Name, &before.Description},
		noteText{&after.Content, &after.Name, &after.Description},
		false,
	); reqErr != nil {
		return reqErr
	}
	return nil
}

// checkPatchedTeamNote is the team note store's patch check.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: sanitize the content, name and description a JSON Patch changed on a team note (pure)
func checkPatchedTeamNote(before, after *TeamNote) error {
	if reqErr := sanitizePatchedNoteText(
		noteText{&before.Content, &before.Name, &before.Description},
		noteText{&after.Content, &after.Name, &after.Description},
		true,
	); reqErr != nil {
		return reqErr
	}
	return nil
}

// checkPatchedProjectNote is the project note store's patch check.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: sanitize the content, name and description a JSON Patch changed on a project note (pure)
func checkPatchedProjectNote(before, after *ProjectNote) error {
	if reqErr := sanitizePatchedNoteText(
		noteText{&before.Content, &before.Name, &before.Description},
		noteText{&after.Content, &after.Name, &after.Description},
		true,
	); reqErr != nil {
		return reqErr
	}
	return nil
}
