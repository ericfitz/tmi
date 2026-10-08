package api

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// markdownMaxLen is the schema's maxLength for note content.
const markdownMaxLen = 262144

// repeatTo returns unit repeated until it reaches n bytes.
func repeatTo(unit string, n int) string {
	return strings.Repeat(unit, n/len(unit))
}

// TestSanitizeRequiredMarkdownContent_BoundedCost guards the sanitizer plus the
// link gate against pathological, schema-valid (256 KiB) inputs that make a
// CommonMark parser quadratic. Each case must finish within the bound. The
// bound is generous for CI; it is not skipped under -race.
func TestSanitizeRequiredMarkdownContent_BoundedCost(t *testing.T) {
	cases := map[string]string{
		"blockquote":        repeatTo(">", markdownMaxLen),
		"emphasis":          repeatTo("*a", markdownMaxLen),
		"underscore":        repeatTo("_a", markdownMaxLen),
		"refdefs":           repeatTo("[a]: /x\n", markdownMaxLen),
		"open brackets":     repeatTo("[", markdownMaxLen),
		"open angle":        repeatTo("<", markdownMaxLen),
		"backticks":         repeatTo("`", markdownMaxLen),
		"bracket paren":     repeatTo("[a](", markdownMaxLen),
		"nested lists":      repeatTo("- ", markdownMaxLen),
		"mixed containers":  repeatTo("> - ", markdownMaxLen),
		"indented lists":    repeatTo("- a\n  ", 2000) + "- b",
		"unsafe link last":  repeatTo("plain text with: colons\n", markdownMaxLen-40) + "[x](javascript:alert(1))",
		"safe links":        repeatTo("[a](https://example.com/x) ", markdownMaxLen),
		"bare javascript:s": repeatTo("javascript:x ", markdownMaxLen),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			out, err := SanitizeRequiredMarkdownContent("content", in)
			elapsed := time.Since(start)
			t.Logf("%s: %v (err=%v)", name, elapsed, err != nil)
			assert.Less(t, elapsed, time.Second, "input of %d bytes took too long", len(in))
			if err == nil {
				assert.NotContains(t, strings.ToLower(out), "](javascript:")
			}
		})
	}
}

// A very large note whose only unsafe construct is the last link must still be
// neutralized or rejected.
func TestSanitizeRequiredMarkdownContent_LargeNoteUnsafeLinkCaught(t *testing.T) {
	in := repeatTo("plain text line\n", markdownMaxLen-40) + "[x](javascript:alert(1))"
	out, err := SanitizeRequiredMarkdownContent("content", in)
	if err == nil {
		assert.NotContains(t, out, "javascript:")
	}
}

func TestSanitizeRequiredMarkdownContent_RejectsDeepNesting(t *testing.T) {
	for _, in := range []string{
		strings.Repeat("> ", 40) + "x",
		strings.Repeat(">", 40) + "x",
		strings.Repeat("- ", 40) + "x",
		strings.Repeat("> - ", 20) + "x",
	} {
		_, err := SanitizeRequiredMarkdownContent("content", in)
		if assert.NotNil(t, err, "input %q", in) {
			assert.Equal(t, 400, err.Status)
			assert.Contains(t, err.Message, "nests block quotes or lists too deeply")
		}
	}
	// Reasonable nesting is fine.
	for _, in := range []string{strings.Repeat("> ", 5) + "x", "- a\n  - b\n    - c", "1. a\n   > q"} {
		_, err := SanitizeRequiredMarkdownContent("content", in)
		assert.Nil(t, err, "input %q", in)
	}
}

// Bare-text URLs for other schemes are prose, not links; CommonMark autolinks
// get the same treatment as inline links (rewritten, not rejected).
func TestSanitizeMarkdownContent_FtpProse(t *testing.T) {
	prose := "see ftp://files.example.com/x for the dump"
	out, err := SanitizeRequiredMarkdownContent("content", prose)
	assert.Nil(t, err)
	assert.Equal(t, prose, out)

	// [x](ftp://...) is rewritten to #; <ftp://x> is stripped by the HTML pass
	// like every other autolink, and neither is stored as a live ftp link.
	out, err = SanitizeRequiredMarkdownContent("content", "[x](ftp://files.example.com/x)")
	assert.Nil(t, err)
	assert.Equal(t, "[x](#)", out)
	out, err = SanitizeRequiredMarkdownContent("content", "a <ftp://x> b")
	assert.Nil(t, err)
	assert.NotContains(t, out, "ftp:")
}
