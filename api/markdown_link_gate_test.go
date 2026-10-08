package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// assertNoUnsafeLinkStored asserts the #1013 invariant for one input: either
// SanitizeRequiredMarkdownContent rejects it with a 400, or the stored text
// parses (CommonMark + GFM) to no link, image or autolink with an unsafe
// destination.
func assertNoUnsafeLinkStored(t *testing.T, in string) {
	t.Helper()
	stored, reqErr := SanitizeRequiredMarkdownContent("content", in)
	if reqErr != nil {
		assert.Equal(t, 400, reqErr.Status, "input %q", in)
		assert.Equal(t, "invalid_input", reqErr.Code, "input %q", in)
		return
	}
	assert.False(t, markdownHasUnsafeLink(stored), "input %q stored as %q still parses to an unsafe link", in, stored)
}

// Reviewer-found bypasses of the destination scanner (#1013): each must be
// neutralized or rejected.
var reviewerBypassInputs = []string{
	"[x](java<<<<<<<<<<<b>b>b>b>b>b>b>b>b>b>b>script:alert(1))",
	"[a](https://x\\ [b](javascript:alert(1))",
	"[a](https://x\\\t[b](javascript:alert(1))",
	"[a](https://x\\\n[b](javascript:alert(1))",
	"> [x](\n> javascript:alert(1))",
	"> [ref]:\n> javascript:alert(1)\n\n[x][ref]",
	"[ref\nref]: javascript:alert(1)\n\n[x][ref ref]",
	"- > [ref]: javascript:alert(1)\n\n[x][ref]",
	"[ref]:\r\njavascript:alert(1)\r\n\r\n[x][ref]",
	"[ref\r\nref]: javascript:alert(1)\r\n\r\n[x][ref ref]",
	"www.example.com and <javascript:alert(1)>",
}

var baseUnsafeInputs = []string{
	`[x](javascript:alert(1))`,
	`[x](JaVaScRiPt:alert(1))`,
	`[x](&#106;avascript:alert(1))`,
	`[x](&#x6A;avascript:alert(1))`,
	`[x](java&#x09;script:alert(1))`,
	`[x](vbscript:msgbox(1))`,
	`[x](data:text/html;base64,PHNjcmlwdD4=)`,
	`![x](data:image/png;base64,AAAA)`,
	`[x](file:///etc/passwd)`,
	`[x](javascript\:alert(1))`,
	`[x](<&#106;avascript:alert(1)>)`,
	`[x](javascript:alert(1) "t")`,
	"[x](\njavascript:alert(1))",
	`<javascript:alert(1)>`,
	"[ref]: javascript:alert(1)\n\n[x][ref]",
	"[ref]:\n  javascript:alert(1)\n\n[x][ref]",
}

func TestSanitizeMarkdownContent_ReviewerBypasses(t *testing.T) {
	for _, in := range reviewerBypassInputs {
		assertNoUnsafeLinkStored(t, in)
	}
}

func TestSanitizeMarkdownContent_UnsafeTableSatisfiesInvariant(t *testing.T) {
	for _, in := range baseUnsafeInputs {
		assertNoUnsafeLinkStored(t, in)
	}
}

// Every unsafe case, wrapped in block containers and CRLF line endings, must
// be neutralized or rejected.
func TestSanitizeMarkdownContent_UnsafeCasesInContainers(t *testing.T) {
	cases := append(append([]string{}, reviewerBypassInputs...), baseUnsafeInputs...)
	prefixes := []string{"", "> ", "- ", "- > ", "1. ", "> > ", "   ", "* - ", "> - > "}
	for _, base := range cases {
		lines := strings.Split(strings.ReplaceAll(base, "\r\n", "\n"), "\n")
		for _, eol := range []string{"\n", "\r\n"} {
			for _, p := range prefixes {
				wrapped := make([]string, len(lines))
				for i, l := range lines {
					switch {
					case i == 0 || strings.HasPrefix(p, ">") || p == "   ":
						wrapped[i] = p + l
					default:
						wrapped[i] = strings.Repeat(" ", len(p)) + l
					}
				}
				assertNoUnsafeLinkStored(t, strings.Join(wrapped, eol))
			}
		}
	}
}

func TestMarkdownHasUnsafeLink(t *testing.T) {
	assert.True(t, markdownHasUnsafeLink("[x](javascript:alert(1))"))
	assert.True(t, markdownHasUnsafeLink("> [x](\n> javascript:alert(1))"))
	assert.True(t, markdownHasUnsafeLink("![x](data:image/png;base64,AA)"))
	assert.False(t, markdownHasUnsafeLink("[x](https://example.com) ![i](a.png) <https://e.com> www.example.com"))
}

func TestSanitizeRequiredMarkdownContent_RejectsUnsafeLink(t *testing.T) {
	// Content the scanner cannot neutralize (destination hidden behind a
	// container prefix it does not model) must be a 400, not stored.
	_, err := SanitizeRequiredMarkdownContent("content", "> [x](\n> javascript:alert(1))")
	if assert.NotNil(t, err) {
		assert.Equal(t, 400, err.Status)
		assert.Contains(t, err.Message, "disallowed URL scheme")
	}
}
