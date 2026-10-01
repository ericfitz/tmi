package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSanitizeMarkdownContent_VerbatimText covers #992: markdown text must not
// be entity-encoded by the sanitizer.
func TestSanitizeMarkdownContent_VerbatimText(t *testing.T) {
	verbatim := []string{
		`[Link](https://example.com/ "a title")`,
		"it's a 'test' & more, 1 < 2 > 0, a+b",
		"```go\nfmt.Println(\"hi & bye\") // it's\n```",
		"`x := \"a\" && 'b'`",
		"&lt;script&gt; stays literal text &amp; &#34;",
		"line1\r\n\ttabbed  \"quoted\"\n",
	}
	for _, in := range verbatim {
		assert.Equal(t, in, SanitizeMarkdownContent(in), "input %q", in)
	}
}

func TestSanitizeMarkdownContent_XSSStillNeutralized(t *testing.T) {
	payloads := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`text <a href="javascript:alert(1)" onclick="x()">l</a> "q"`,
		`<<script>script>alert(1)</script>`,
		`<scr<script>ipt>alert(1)</script>`,
		`<style>body{x:y}</style>`,
		`<iframe src="//evil"></iframe>`,
		`<div onmouseover="x()" title="a">hi</div>`,
		`<b onclick="x()">hi`,
		`<img src="x" onerror="alert(1)"`,
	}
	for _, in := range payloads {
		out := SanitizeMarkdownContent(in)
		for _, bad := range []string{"<script", "onerror", "onclick", "onmouseover", "javascript:", "<iframe", "<style", "{x:y}"} {
			assert.NotContains(t, strings.ToLower(out), bad, "input %q -> %q", in, out)
		}
	}
	assert.Equal(t, `"q" ok`, SanitizeMarkdownContent(`<script>x</script>"q" ok`))
}
