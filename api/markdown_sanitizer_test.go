package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeMarkdownContent(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		shouldContain    []string
		shouldNotContain []string
		description      string
	}{
		// Safe HTML that should be preserved
		{
			name:  "Table HTML preserved",
			input: "<table><thead><tr><th>Header</th></tr></thead><tbody><tr><td>Cell</td></tr></tbody></table>",
			shouldContain: []string{
				"<table>", "<thead>", "<tr>", "<th>", "<td>",
			},
			description: "Table elements should survive sanitization",
		},
		{
			name:  "Heading tags preserved",
			input: "<h1>Title</h1><h2>Subtitle</h2><h3>Section</h3>",
			shouldContain: []string{
				"<h1>Title</h1>", "<h2>Subtitle</h2>", "<h3>Section</h3>",
			},
			description: "Heading elements should survive sanitization",
		},
		{
			name:  "Formatting tags preserved",
			input: "<strong>bold</strong> <em>italic</em> <del>deleted</del> <code>code</code>",
			shouldContain: []string{
				"<strong>bold</strong>", "<em>italic</em>", "<del>deleted</del>", "<code>code</code>",
			},
			description: "Formatting elements should survive sanitization",
		},
		{
			name:  "Structural tags preserved",
			input: "<p>paragraph</p><br><hr><div>block</div><span>inline</span>",
			shouldContain: []string{
				"<p>paragraph</p>", "<div>block</div>", "<span>inline</span>",
			},
			description: "Structural elements should survive sanitization",
		},
		{
			name:  "Link with href preserved",
			input: `<a href="https://example.com">Link</a>`,
			shouldContain: []string{
				`href="https://example.com"`, "Link</a>",
			},
			description: "Links with href should survive sanitization",
		},
		{
			name:  "Image preserved",
			input: `<img src="https://example.com/img.png" alt="photo" title="Photo" width="100" height="50">`,
			shouldContain: []string{
				`src="https://example.com/img.png"`, `alt="photo"`,
			},
			description: "Images with safe attributes should survive sanitization",
		},
		{
			name:  "List elements preserved",
			input: "<ul><li>item 1</li><li>item 2</li></ul><ol><li>first</li></ol>",
			shouldContain: []string{
				"<ul>", "<ol>", "<li>",
			},
			description: "List elements should survive sanitization",
		},
		{
			name:  "Blockquote preserved",
			input: "<blockquote>Quoted text</blockquote>",
			shouldContain: []string{
				"<blockquote>Quoted text</blockquote>",
			},
			description: "Blockquote should survive sanitization",
		},
		{
			name:  "Checkbox input preserved",
			input: `<input type="checkbox" checked disabled>`,
			shouldContain: []string{
				"input", "checkbox",
			},
			description: "Checkbox inputs should survive sanitization",
		},
		{
			name:  "SVG diagram preserved",
			input: `<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg"><circle cx="50" cy="50" r="40" fill="red"></circle></svg>`,
			shouldContain: []string{
				// bluemonday normalizes attribute names to lowercase (standard HTML behavior)
				"<svg", "viewbox", "<circle", `fill="red"`,
			},
			description: "SVG elements should survive sanitization",
		},
		{
			name:  "SVG path preserved",
			input: `<svg><path d="M10 10 L90 90" stroke="black" stroke-width="2"></path></svg>`,
			shouldContain: []string{
				"<path", `d="M10 10 L90 90"`, `stroke="black"`,
			},
			description: "SVG path with d attribute should survive sanitization",
		},
		{
			name:  "Global data attributes preserved",
			input: `<div data-line="5" data-sourcepos="3:1-3:10">text</div>`,
			shouldContain: []string{
				`data-line="5"`, `data-sourcepos="3:1-3:10"`,
			},
			description: "data-line and data-sourcepos attributes should survive sanitization",
		},
		{
			name:  "Class and id attributes preserved",
			input: `<div class="highlight" id="section-1">text</div>`,
			shouldContain: []string{
				`class="highlight"`, `id="section-1"`,
			},
			description: "Class and id attributes should survive sanitization",
		},
		{
			name:  "Pre and code preserved",
			input: "<pre><code>func main() {}</code></pre>",
			shouldContain: []string{
				"<pre>", "<code>",
			},
			description: "Pre and code elements should survive sanitization",
		},

		// Dangerous HTML that should be stripped
		{
			name:  "Script tag stripped",
			input: "Hello <script>alert('xss')</script> World",
			shouldNotContain: []string{
				"<script", "alert",
			},
			shouldContain: []string{
				"Hello", "World",
			},
			description: "Script tags and their content should be stripped",
		},
		{
			name:  "Iframe stripped",
			input: `<iframe src="http://evil.com"></iframe>`,
			shouldNotContain: []string{
				"<iframe", "evil.com",
			},
			description: "Iframe tags should be stripped",
		},
		{
			name:  "Object tag stripped",
			input: `<object data="evil.swf" type="application/x-shockwave-flash"></object>`,
			shouldNotContain: []string{
				"<object", "evil.swf",
			},
			description: "Object tags should be stripped",
		},
		{
			name:  "Embed tag stripped",
			input: `<embed src="evil.swf">`,
			shouldNotContain: []string{
				"<embed", "evil.swf",
			},
			description: "Embed tags should be stripped",
		},
		{
			name:  "Event handler attribute stripped",
			input: `<img src="https://example.com/img.png" onerror="alert(1)">`,
			shouldNotContain: []string{
				"onerror", "alert",
			},
			description: "Event handler attributes should be stripped from otherwise safe elements",
		},
		{
			name:  "Javascript URL in href stripped",
			input: `<a href="javascript:alert(1)">Click</a>`,
			shouldNotContain: []string{
				"javascript:",
			},
			shouldContain: []string{
				"Click",
			},
			description: "javascript: URLs should be stripped, text preserved",
		},
		{
			name:  "Non-checkbox input stripped",
			input: `<input type="text" value="inject">`,
			shouldNotContain: []string{
				`type="text"`,
			},
			description: "Non-checkbox input types should be stripped",
		},
		{
			name:  "Form tag stripped",
			input: `<form action="http://evil.com"><input type="submit"></form>`,
			shouldNotContain: []string{
				"<form", "action",
			},
			description: "Form tags should be stripped",
		},

		// Edge cases
		{
			name:             "Empty content returns empty",
			input:            "",
			shouldContain:    nil,
			shouldNotContain: nil,
			description:      "Empty content should return empty",
		},
		{
			name:  "Plain markdown unchanged",
			input: "# Heading\n\n**bold** text\n\n- list item",
			shouldContain: []string{
				"# Heading", "**bold** text", "- list item",
			},
			description: "Plain markdown without HTML should pass through unchanged",
		},
		{
			name:  "Mixed markdown and safe HTML",
			input: "# Title\n\n<table><tr><td>Data</td></tr></table>\n\n**bold**",
			shouldContain: []string{
				"# Title", "<table>", "<td>Data</td>", "**bold**",
			},
			description: "Mixed markdown and safe HTML should both survive",
		},
		{
			name:  "Mixed safe and dangerous HTML",
			input: "<strong>Safe</strong><script>evil()</script><em>Also safe</em>",
			shouldContain: []string{
				"<strong>Safe</strong>", "<em>Also safe</em>",
			},
			shouldNotContain: []string{
				"<script", "evil",
			},
			description: "Safe HTML preserved, dangerous HTML stripped from mixed content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeMarkdownContent(tt.input)

			for _, expected := range tt.shouldContain {
				assert.Contains(t, result, expected, "%s: should contain %q", tt.description, expected)
			}
			for _, unexpected := range tt.shouldNotContain {
				assert.NotContains(t, result, unexpected, "%s: should not contain %q", tt.description, unexpected)
			}
		})
	}
}

func TestSanitizeMarkdownContent_Empty(t *testing.T) {
	assert.Equal(t, "", SanitizeMarkdownContent(""))
}

func TestSanitizePlainText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"plain text unchanged", "hello world", "hello world"},
		{"arrow notation preserved", "private (0.0.0.0/0 -> NAT, OCI Services -> SGW)", "private (0.0.0.0/0 -> NAT, OCI Services -> SGW)"},
		{"angle brackets in technical content", "10.0.0.0/16 -> gateway", "10.0.0.0/16 -> gateway"},
		{"script tags stripped", "hello <script>alert('xss')</script> world", "hello  world"},
		{"all HTML stripped", "<b>bold</b> and <i>italic</i>", "bold and italic"},
		{"event handlers stripped", `<img onerror="alert(1)">`, ""},
		{"nested tags stripped", "<div><p>text</p></div>", "text"},
		{"ampersands preserved", "A & B", "A & B"},
		{"special chars preserved", "key=value; path=/usr/bin", "key=value; path=/usr/bin"},
		{"entity-encoded script stripped", "&lt;script&gt;alert(1)&lt;/script&gt;", ""},
		{"entity-encoded img stripped", "&lt;img src=x onerror=alert(1)&gt;", ""},
		{"entity-encoded bold stripped", "&lt;b&gt;bold&lt;/b&gt;", "bold"},
		{"double-encoded script stripped", "&amp;lt;script&amp;gt;alert(1)&amp;lt;/script&amp;gt;", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizePlainText(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeMetadataSlice(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		err := SanitizeMetadataSlice(nil)
		assert.NoError(t, err)
	})

	t.Run("clean metadata unchanged", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "component_type", Value: "network"},
			{Key: "cidr_blocks", Value: "10.0.0.0/16"},
		}
		err := SanitizeMetadataSlice(&metadata)
		assert.NoError(t, err)
		assert.Equal(t, "network", metadata[0].Value)
		assert.Equal(t, "10.0.0.0/16", metadata[1].Value)
	})

	t.Run("HTML stripped from values", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "description", Value: "<b>important</b> network"},
		}
		err := SanitizeMetadataSlice(&metadata)
		assert.NoError(t, err)
		assert.Equal(t, "important network", metadata[0].Value)
	})

	t.Run("script tags stripped", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "test", Value: "hello <script>alert(1)</script> world"},
		}
		err := SanitizeMetadataSlice(&metadata)
		assert.NoError(t, err)
		assert.Equal(t, "hello  world", metadata[0].Value)
	})

	t.Run("template injection rejected", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "test", Value: "hello {{ .Token }} world"},
		}
		err := SanitizeMetadataSlice(&metadata)
		assert.Error(t, err)
	})

	t.Run("arrow notation preserved after sanitization", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "routing", Value: "private (0.0.0.0/0 -> NAT)"},
		}
		err := SanitizeMetadataSlice(&metadata)
		assert.NoError(t, err)
		assert.Equal(t, "private (0.0.0.0/0 -> NAT)", metadata[0].Value)
	})
}

func TestSanitizeOptionalString(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		assert.Nil(t, SanitizeOptionalString(nil))
	})

	t.Run("clean text unchanged", func(t *testing.T) {
		s := "A simple description"
		result := SanitizeOptionalString(&s)
		assert.Equal(t, "A simple description", *result)
	})

	t.Run("HTML stripped", func(t *testing.T) {
		s := "<script>alert('xss')</script>clean text"
		result := SanitizeOptionalString(&s)
		assert.Equal(t, "clean text", *result)
	})

	t.Run("entities decoded", func(t *testing.T) {
		s := "A &gt; B"
		result := SanitizeOptionalString(&s)
		assert.Equal(t, "A > B", *result)
	})

	t.Run("idempotent", func(t *testing.T) {
		s := "<b>bold</b> & normal"
		result1 := SanitizeOptionalString(&s)
		result2 := SanitizeOptionalString(result1)
		assert.Equal(t, *result1, *result2)
	})

	t.Run("idempotent with entity-encoded HTML", func(t *testing.T) {
		s := "&lt;script&gt;alert(1)&lt;/script&gt;"
		result1 := SanitizeOptionalString(&s)
		result2 := SanitizeOptionalString(result1)
		assert.Equal(t, *result1, *result2)
		assert.Equal(t, "", *result1)
	})
}

func TestSanitizePatchOperations(t *testing.T) {
	t.Run("sanitizes matching paths", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: string(Replace), Path: "/name", Value: "<b>bold</b> name"},
			{Op: string(Replace), Path: "/description", Value: "<script>xss</script>desc"},
		}
		SanitizePatchOperations(ops, []string{"/name", "/description"})
		assert.Equal(t, "bold name", ops[0].Value)
		assert.Equal(t, "desc", ops[1].Value)
	})

	t.Run("ignores non-matching paths", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: string(Replace), Path: "/id", Value: "some-uuid"},
		}
		SanitizePatchOperations(ops, []string{"/name"})
		assert.Equal(t, "some-uuid", ops[0].Value)
	})

	t.Run("ignores non-string values", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: string(Replace), Path: "/name", Value: 42},
		}
		SanitizePatchOperations(ops, []string{"/name"})
		assert.Equal(t, 42, ops[0].Value)
	})

	t.Run("only sanitizes replace and add ops", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: "remove", Path: "/name", Value: "<b>bold</b>"},
		}
		SanitizePatchOperations(ops, []string{"/name"})
		assert.Equal(t, "<b>bold</b>", ops[0].Value)
	})

	t.Run("handles add operation", func(t *testing.T) {
		ops := []PatchOperation{
			{Op: string(Add), Path: "/name", Value: "<img src=x>clean"},
		}
		SanitizePatchOperations(ops, []string{"/name"})
		assert.Equal(t, "clean", ops[0].Value)
	})
}

func TestValidateTemplateInjectionInMarkdown(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		description string
	}{
		{
			name:        "Clean content",
			content:     "Hello world, this is markdown",
			expectError: false,
			description: "Plain text should pass",
		},
		{
			name:        "Handlebars template expression",
			content:     "Hello {{ user }} world",
			expectError: true,
			description: "Template expressions should be rejected",
		},
		{
			name:        "Closing template expression",
			content:     "Hello result }} here",
			expectError: true,
			description: "Closing template expressions should be rejected",
		},
		{
			name:        "JavaScript template literal",
			content:     "Hello ${ name } world",
			expectError: true,
			description: "Template interpolation should be rejected",
		},
		{
			name:        "GitHub Actions context",
			content:     "Token: ${{ github.token }}",
			expectError: true,
			description: "GitHub Actions expressions should be rejected",
		},
		{
			name:        "JSP/ASP template tag",
			content:     "Hello <% code %> world",
			expectError: true,
			description: "Server template tags should be rejected",
		},
		{
			name:        "Spring EL expression",
			content:     "Hello #{ expr } world",
			expectError: true,
			description: "Expression language should be rejected",
		},
		{
			name:        "Template expression in fenced code block",
			content:     "```\n{{ user }}\n```",
			expectError: false,
			description: "Template expressions in code blocks should be allowed",
		},
		{
			name:        "Template expression in inline code",
			content:     "Use `{{ template }}` syntax",
			expectError: false,
			description: "Template expressions in inline code should be allowed",
		},
		{
			name:        "Template expression in code block with language",
			content:     "```go\nfmt.Println(\"{{ .Name }}\")\n```",
			expectError: false,
			description: "Template expressions in language-tagged code blocks should be allowed",
		},
		{
			name:        "Mixed: template in code block and clean content",
			content:     "# Title\n\n```\n{{ user }}\n```\n\nSafe content here",
			expectError: false,
			description: "Template in code block with clean surrounding content should pass",
		},
		{
			name:        "Empty content",
			content:     "",
			expectError: false,
			description: "Empty content should pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTemplateInjectionInMarkdown(tt.content)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}
