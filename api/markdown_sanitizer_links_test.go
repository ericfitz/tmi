package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Unsafe markdown link and image destinations are replaced with "#" (#1013).
func TestSanitizeMarkdownContent_UnsafeLinkDestinations(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"javascript inline link", `[x](javascript:alert(1))`, `[x](#)`},
		{"mixed case scheme", `[x](JaVaScRiPt:alert(1))`, `[x](#)`},
		{"decimal entity", `[x](&#106;avascript:alert(1))`, `[x](#)`},
		{"hex entity", `[x](&#x6A;avascript:alert(1))`, `[x](#)`},
		{"entity tab inside scheme", `[x](java&#x09;script:alert(1))`, `[x](#)`},
		{"entity newline inside scheme", `[x](java&#10;script:alert(1))`, `[x](#)`},
		{"named entity tab", `[x](java&Tab;script:alert(1))`, `[x](#)`},
		{"angle dest with entity scheme", `[x](<&#106;avascript:alert(1)>)`, `[x](#)`},
		{"angle dest with entity tab", "[x](<&#106;ava&#9;script:alert(1)> \"T\")", `[x](# "T")`},
		{"entity leading control char", `[x](&#1;javascript:alert(1))`, `[x](#)`},
		{"entity leading space", `[x](&#32;javascript:alert(1))`, `[x](#)`},
		{"backslash escape in scheme", `[x](javascript\:alert(1))`, `[x](#)`},
		{"entity colon", `[x](javascript&#58;alert(1))`, `[x](#)`},
		{"vbscript", `[x](vbscript:msgbox(1))`, `[x](#)`},
		{"data link", `[x](data:text/html;base64,PHNjcmlwdD4=)`, `[x](#)`},
		{"data image", `![x](data:image/png;base64,AAAA)`, `![x](#)`},
		{"javascript image", `![x](javascript:alert(1))`, `![x](#)`},
		{"file scheme", `[x](file:///etc/passwd)`, `[x](#)`},
		{"title preserved", `[x](javascript:alert(1) "My title")`, `[x](# "My title")`},
		{"whitespace before destination", `[x](   javascript:alert(1))`, `[x](   #)`},
		{"text around", "see [a](javascript:x) and [b](https://ok.example/p)", "see [a](#) and [b](https://ok.example/p)"},
		{"two bad links on a line", `[a](javascript:x) [b](vbscript:y)`, `[a](#) [b](#)`},
		{"reference definition", "[ref]: javascript:alert(1)\n\n[x][ref]", "[ref]: #\n\n[x][ref]"},
		{"reference definition angle and title", `[ref]: <&#106;avascript:alert(1)> "T"`, `[ref]: # "T"`},
		{"reference definition indented", `   [ref]: javascript:alert(1)`, `   [ref]: #`},
		{"reference definition in blockquote", `> [ref]: javascript:alert(1)`, `> [ref]: #`},
		{"reference definition destination on next line", "[ref]:\n  javascript:alert(1)", "[ref]:\n  #"},
		{"reference definition entity", `[ref]: &#106;avascript:alert(1)`, `[ref]: #`},
		{"autolink", `<javascript:alert(1)>`, ``},
		{"autolink data", `<data:text/html,x>`, ``},
		{"nested parens in dest", `[x](javascript:alert((1)))`, `[x](#)`},
		{"unrelated text after bad link", `[x](javascript:alert(1)) tail (paren)`, `[x](#) tail (paren)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeMarkdownContent(tt.in)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, got, SanitizeMarkdownContent(got), "must be idempotent")
		})
	}
}

// Safe destinations, and text that merely looks link-like, stay byte-identical.
func TestSanitizeMarkdownContent_SafeLinkDestinationsUnchanged(t *testing.T) {
	inputs := []string{
		`[x](http://example.com)`,
		`[x](https://example.com/a?b=c#d)`,
		`[x](HTTPS://EXAMPLE.COM)`,
		`[x](mailto:a@example.com)`,
		`[x](relative/path.md)`,
		`[x](./a.md)`,
		`[x](../a.md)`,
		`[x](/abs/path)`,
		`[x](#anchor)`,
		`[x](?q=1)`,
		`[x]()`,
		`[x](//cdn.example.com/a.png)`,
		`[x](javascript%3Aalert(1))`,
		`[x](dir/file:with-colon)`,
		`[x](https://example.com "a title")`,
		`[x](<./my file.md>)`,
		`[x](<../a b: c.md> "t")`,
		`![img](https://example.com/a.png)`,
		`![img](images/a.png "t")`,
		"[ref]: https://example.com\n\n[x][ref]",
		`[ref]: /relative "t"`,
		`[ref]: #frag`,
		`[x](https://example.com/a_(b))`,
		`not a link: javascript:alert(1) in prose`,
		`[x] (javascript:alert(1)) with a space is not a link`,
		`array[i](j) stays`,
	}
	for _, in := range inputs {
		assert.Equal(t, in, SanitizeMarkdownContent(in), "input %q", in)
	}
}
