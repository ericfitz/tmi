package api

import (
	"errors"
	"html"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Markdown link and image destinations (#1013).
//
// The HTML pass in SanitizeMarkdownContent never looks at markdown syntax, so
// `[x](javascript:alert(1))` (and entity-encoded variants) would be stored
// verbatim and executed by any consumer that renders the note without its own
// sanitizer. This pass finds every inline link/image destination and every
// reference definition destination and neutralizes the unsafe ones.
//
// Neutralization policy: an unsafe destination (including an angle-bracket
// one) is replaced by the inert fragment "#". The link text, image alt text and
// title stay, and so does every other byte of the note. Code spans and fenced
// blocks are deliberately not special-cased: a destination-looking construct
// with an unsafe scheme is rewritten there too, because guessing where a given
// renderer sees code is exactly how filters get bypassed.
//
// This scanner is best effort: it does not model block containers (block
// quotes, list items) that a real CommonMark parser strips from continuation
// lines. markdownHasUnsafeLink is therefore the authoritative gate: it parses
// the final text with goldmark and reports any link, image or autolink whose
// destination is still unsafe, and SanitizeRequiredMarkdownContent rejects such
// content with a 400. Autolinks (<javascript:...>) are removed by the HTML pass
// as unknown tags, and are also covered by the gate.

// safeLinkSchemes is the allowlist of URL schemes for link and image
// destinations. Scheme-less (relative, fragment, query, protocol-relative)
// destinations are always allowed.
var safeLinkSchemes = map[string]bool{"http": true, "https": true, "mailto": true}

// refDefRe matches the start of a reference definition ("[label]: ") up to the
// start of its destination, which may sit on the following line. Block-quote
// and list-item prefixes are tolerated.
var refDefRe = regexp.MustCompile(`(?m)^[ \t>]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+)*\[(?:[^\]\\]|\\[\s\S])+\]:[ \t]*(?:\r?\n)?[ \t]*`)

// urlSchemeRe matches a leading URL scheme.
var urlSchemeRe = regexp.MustCompile(`^([a-z][a-z0-9+.-]*):`)

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: neutralize markdown link, image and reference-definition destinations whose scheme is not allowlisted (pure)
func neutralizeMarkdownLinkDestinations(content string) string {
	content = neutralizeInlineDestinations(content)
	return neutralizeRefDefinitions(content)
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: replace unsafe destinations of inline links and images with a fragment (pure)
func neutralizeInlineDestinations(content string) string {
	var out strings.Builder
	last := 0
	for i := 0; i+1 < len(content); i++ {
		if content[i] != ']' || content[i+1] != '(' || isBackslashEscaped(content, i) {
			continue
		}
		start := skipMarkdownSpace(content, i+2)
		end, ok := scanDestination(content, start)
		if !ok {
			continue
		}
		if !isSafeLinkDestination(content[start:end]) {
			out.WriteString(content[last:start])
			out.WriteString("#")
			last = end
		}
		i = end - 1
	}
	if last == 0 {
		return content
	}
	out.WriteString(content[last:])
	return out.String()
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: replace unsafe destinations of markdown reference definitions with a fragment (pure)
func neutralizeRefDefinitions(content string) string {
	var out strings.Builder
	last := 0
	for _, m := range refDefRe.FindAllStringIndex(content, -1) {
		start := m[1]
		if start < last {
			continue
		}
		end, ok := scanDestination(content, start)
		if !ok || isSafeLinkDestination(content[start:end]) {
			continue
		}
		out.WriteString(content[last:start])
		out.WriteString("#")
		last = end
	}
	if last == 0 {
		return content
	}
	out.WriteString(content[last:])
	return out.String()
}

// isBackslashEscaped reports whether the byte at i is preceded by an odd
// number of backslashes.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether a byte is escaped by an odd run of preceding backslashes (pure)
func isBackslashEscaped(s string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: advance past markdown whitespace (pure)
func skipMarkdownSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

// scanDestination returns the end offset of the link destination starting at
// start, following CommonMark: either <...> (no newline, no unescaped angle
// brackets) or a raw run without spaces or control characters in which
// parentheses balance. ok is false when no destination starts there, including
// the empty destination, which is always safe.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: find the end of a markdown link destination, angle-bracketed or raw (pure)
func scanDestination(s string, start int) (end int, ok bool) {
	if start >= len(s) {
		return 0, false
	}
	if s[start] == '<' {
		for i := start + 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				if i+1 < len(s) && isASCIIPunct(s[i+1]) {
					i++
				}
			case '\n', '<':
				return 0, false
			case '>':
				return i + 1, true
			}
		}
		return 0, false
	}
	depth := 0
	i := start
scan:
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]):
			i++
		case c <= ' ' || c == 0x7f:
			break scan
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 {
				break scan
			}
			depth--
		}
	}
	return i, i > start
}

// isSafeLinkDestination reports whether a destination as written in markdown
// (raw or <angle> form) resolves to a relative URL or an allowlisted scheme.
// The destination is normalized the way a renderer and then a browser would
// before looking at the scheme: angle brackets dropped, backslash escapes and
// HTML entities decoded (repeatedly, so double encoding cannot hide a scheme),
// tabs/newlines removed anywhere, and leading/trailing control characters and
// spaces trimmed (WHATWG URL parsing).
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: decide whether a markdown link destination is relative or uses an allowlisted scheme (pure)
func isSafeLinkDestination(dest string) bool {
	if strings.HasPrefix(dest, "<") && strings.HasSuffix(dest, ">") {
		dest = dest[1 : len(dest)-1]
	}
	dest = unescapeMarkdownBackslashes(dest)
	for i := 0; i < 10; i++ {
		next := html.UnescapeString(dest)
		if next == dest {
			break
		}
		dest = next
	}
	dest = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, dest)
	dest = strings.TrimFunc(dest, func(r rune) bool { return r <= ' ' || r == 0x7f })
	m := urlSchemeRe.FindStringSubmatch(strings.ToLower(dest))
	if m == nil {
		return true
	}
	return safeLinkSchemes[m[1]]
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: remove markdown backslash escapes before ASCII punctuation (pure)
func unescapeMarkdownBackslashes(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether a byte is ASCII punctuation, the only bytes a markdown backslash escapes (pure)
func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// markdownGateParser parses just enough CommonMark to find link, image and
// autolink destinations: the default block parsers and paragraph transformers
// (reference definitions), and inline parsers for code spans, links/images,
// autolinks and raw HTML. The emphasis parser is left out because goldmark's
// delimiter processing is quadratic on inputs like "*a" repeated, and links
// take precedence over emphasis so destinations are unaffected. GFM is left
// out too: its bare-text autolinks only ever produce http, https, ftp, www and
// email links, never script-bearing schemes, so they are prose here. It is only
// used to parse, never to render.
var markdownGateParser = parser.NewParser(
	parser.WithBlockParsers(parser.DefaultBlockParsers()...),
	parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
	parser.WithInlineParsers(
		util.Prioritized(parser.NewCodeSpanParser(), 100),
		util.Prioritized(parser.NewLinkParser(), 200),
		util.Prioritized(parser.NewAutoLinkParser(), 300),
		util.Prioritized(parser.NewRawHTMLParser(), 400),
	),
)

// maxMarkdownContainerDepth bounds how many block quote markers and list
// markers may prefix a single line. goldmark's block-quote matching is
// quadratic in this depth; nobody writes notes nested 32 deep.
const maxMarkdownContainerDepth = 32

// listMarkerRe matches a list marker (bullet or ordered) at the start of s.
var listMarkerRe = regexp.MustCompile(`^(?:[-*+]|[0-9]{1,9}[.)])(?:[ \t]|$)`)

// markdownNestsTooDeeply reports whether any line's leading container prefix
// (block quote markers and list markers, ignoring spaces) is deeper than
// maxMarkdownContainerDepth.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether any line nests block quotes or list markers deeper than the limit (pure)
func markdownNestsTooDeeply(content string) bool {
	for len(content) > 0 {
		line := content
		if i := strings.IndexByte(content, '\n'); i >= 0 {
			line, content = content[:i], content[i+1:]
		} else {
			content = ""
		}
		if lineContainerDepth(line) > maxMarkdownContainerDepth {
			return true
		}
	}
	return false
}

// lineContainerDepth counts the block quote and list markers prefixing a line,
// stopping early once the limit is exceeded.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: count block quote and list markers prefixing a line, stopping past the limit (pure)
func lineContainerDepth(line string) int {
	depth := 0
	for depth <= maxMarkdownContainerDepth {
		line = strings.TrimLeft(line, " \t")
		switch {
		case strings.HasPrefix(line, ">"):
			line = line[1:]
		case listMarkerRe.MatchString(line):
			n := strings.IndexAny(line, " \t")
			if n < 0 {
				n = len(line)
			}
			line = line[n:]
		default:
			return depth
		}
		depth++
	}
	return depth
}

// anySchemeRe finds scheme-like tokens followed by a colon anywhere in text.
var anySchemeRe = regexp.MustCompile(`([a-z][a-z0-9+.-]*):`)

// markdownMayHaveUnsafeLink is a cheap prefilter for the parse: it reports
// whether the text, normalized the way isSafeLinkDestination does (entities
// decoded, control characters removed, lowercased), contains any "scheme:"
// token whose scheme is not allowlisted. Notes without one cannot hold an
// unsafe destination and skip the parse. It over-approximates (any "word:"
// matches) and never under-approximates. Because of that, ordinary prose
// ("Note:", "TODO:", "Step 1:") passes it, so most real notes do reach the
// gate: the gate's cost limit (markdownGateCheck) protects every note write,
// not just hostile ones.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether text contains a colon-terminated token with a non-allowlisted scheme (pure)
func markdownMayHaveUnsafeLink(content string) bool {
	norm := content
	for i := 0; i < 10 && strings.Contains(norm, "&"); i++ {
		next := html.UnescapeString(norm)
		if next == norm {
			break
		}
		norm = next
	}
	norm = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, norm)
	norm = strings.ToLower(unescapeMarkdownBackslashes(norm))
	for _, m := range anySchemeRe.FindAllStringSubmatch(norm, -1) {
		if !safeLinkSchemes[m[1]] {
			return true
		}
	}
	return false
}

// markdownHasUnsafeLink parses content as CommonMark (see markdownGateParser)
// and reports whether any link, image or autolink has a destination that is
// not relative or allowlisted. It has no time limit; request paths go through
// markdownGateCheck.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether parsed markdown contains a link, image or autolink with a disallowed destination (pure)
func markdownHasUnsafeLink(content string) bool {
	return markdownHasUnsafeLinkBefore(content, time.Time{})
}

// markdownHasUnsafeLinkBefore is markdownHasUnsafeLink with a deadline (zero
// means none): past it, the parse panics with errMarkdownGateDeadline (see
// gateDeadlineContext).
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether parsed markdown contains a disallowed link destination, aborting the parse at a deadline (pure)
func markdownHasUnsafeLinkBefore(content string, deadline time.Time) bool {
	src := []byte(content)
	var opts []parser.ParseOption
	if !deadline.IsZero() {
		opts = append(opts, parser.WithContext(&gateDeadlineContext{Context: parser.NewContext(), deadline: deadline}))
	}
	doc := markdownGateParser.Parse(text.NewReader(src), opts...)
	unsafe := false
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Link:
			unsafe = unsafe || !isSafeLinkDestination(string(v.Destination))
		case *ast.Image:
			unsafe = unsafe || !isSafeLinkDestination(string(v.Destination))
		case *ast.AutoLink:
			unsafe = unsafe || !isSafeLinkDestination(string(v.URL(src)))
		}
		if unsafe {
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return unsafe
}

// markdownGateBudget bounds the wall-clock time one gate check may take.
// goldmark has inputs on which parsing is quadratic (a run of "[a](" with no
// whitespace takes ~10 s at the 256 KiB schema limit) and more may exist, so
// rather than chase each one the gate stops the parse at the deadline and
// rejects the note as too complex. A normal 256 KiB note parses in ~20 ms. It
// is a variable so tests can shorten it.
var markdownGateBudget = 500 * time.Millisecond

// markdownGateInFlight counts gate parses still running, including ones whose
// caller already gave up; tests use it to check that cancellation is prompt.
var markdownGateInFlight atomic.Int64

// markdownGateScan is the parse the gate runs; tests replace it to control
// timing. It must give up (panic with errMarkdownGateDeadline) soon after the
// deadline.
var markdownGateScan = markdownHasUnsafeLinkBefore

// errMarkdownGateDeadline is the panic value that aborts a gate parse.
var errMarkdownGateDeadline = errors.New("markdown gate parse deadline exceeded")

// gateDeadlineContext is the parser.Context for a gate parse. goldmark threads
// one Context through every phase, including the inline phase, which reads
// from its own internal block reader (so wrapping the input reader would not
// reach the quadratic link-destination scan). Every few calls into the
// context it checks the clock and panics with errMarkdownGateDeadline once
// the deadline has passed. Between two context calls goldmark does at most
// one linear scan of a line, so a parse stops within milliseconds of the
// deadline. A parse is single-goroutine, so the counter needs no locking.
type gateDeadlineContext struct {
	parser.Context
	deadline time.Time
	calls    uint32
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: abort the gate parse with a sentinel panic once its deadline has passed, checking the clock every few calls
func (c *gateDeadlineContext) tick() {
	c.calls++
	if c.calls&7 == 0 && time.Now().After(c.deadline) {
		panic(errMarkdownGateDeadline)
	}
}

// The overrides below are the Context methods goldmark's block parsers,
// inline parsers (links, code spans, autolinks, raw HTML) and the
// link-reference paragraph transformer call as they work.

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: read a parse-context value after a deadline check
func (c *gateDeadlineContext) Get(k parser.ContextKey) any { c.tick(); return c.Context.Get(k) }

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: compute or read a parse-context value after a deadline check
func (c *gateDeadlineContext) ComputeIfAbsent(k parser.ContextKey, f func() any) any {
	c.tick()
	return c.Context.ComputeIfAbsent(k, f)
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: store a parse-context value after a deadline check
func (c *gateDeadlineContext) Set(k parser.ContextKey, v any) { c.tick(); c.Context.Set(k, v) }

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: record a link reference definition after a deadline check
func (c *gateDeadlineContext) AddReference(r parser.Reference) { c.tick(); c.Context.AddReference(r) }

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: look up a link reference definition after a deadline check
func (c *gateDeadlineContext) Reference(label string) (parser.Reference, bool) {
	c.tick()
	return c.Context.Reference(label)
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: read the block offset after a deadline check
func (c *gateDeadlineContext) BlockOffset() int { c.tick(); return c.Context.BlockOffset() }

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: read the block indent after a deadline check
func (c *gateDeadlineContext) BlockIndent() int { c.tick(); return c.Context.BlockIndent() }

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: read the open block stack after a deadline check
func (c *gateDeadlineContext) OpenedBlocks() []parser.Block {
	c.tick()
	return c.Context.OpenedBlocks()
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: replace the open block stack after a deadline check
func (c *gateDeadlineContext) SetOpenedBlocks(b []parser.Block) {
	c.tick()
	c.Context.SetOpenedBlocks(b)
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: read the innermost open block after a deadline check
func (c *gateDeadlineContext) LastOpenedBlock() parser.Block {
	c.tick()
	return c.Context.LastOpenedBlock()
}

// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: report whether the parser is inside a link label after a deadline check
func (c *gateDeadlineContext) IsInLinkLabel() bool { c.tick(); return c.Context.IsInLinkLabel() }

// markdownGateResult is the outcome of markdownGateCheck.
type markdownGateResult int

const (
	markdownGateSafe markdownGateResult = iota
	markdownGateUnsafe
	markdownGateTooComplex
)

// markdownGateCheck runs markdownGateScan on content with a deadline of
// markdownGateBudget. It reports markdownGateTooComplex when the parse does
// not finish in time or panics (it runs on its own goroutine, where a panic
// would otherwise take down the process). The parse itself stops at the
// deadline, so abandoned parses cannot accumulate; there is deliberately no
// concurrency cap, which would turn a few slow requests into rejections for
// every other user (#1013 review).
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: run the markdown link gate under a cancelling deadline, reporting too-complex on overrun or panic
func markdownGateCheck(content string) markdownGateResult {
	deadline := time.Now().Add(markdownGateBudget)
	timer := time.NewTimer(markdownGateBudget)
	defer timer.Stop()
	scan := markdownGateScan // read here, not on the goroutine, which may outlive a test's override
	done := make(chan markdownGateResult, 1)
	markdownGateInFlight.Add(1)
	go func() {
		defer markdownGateInFlight.Add(-1)
		defer func() {
			if r := recover(); r != nil {
				if r != errMarkdownGateDeadline { //nolint:errorlint // sentinel panic value, compared by identity
					slogging.Get().Error("markdown link gate: parse panicked: %v", r)
				}
				done <- markdownGateTooComplex
			}
		}()
		if scan(content, deadline) {
			done <- markdownGateUnsafe
		} else {
			done <- markdownGateSafe
		}
	}()
	select {
	case result := <-done:
		return result
	case <-timer.C:
		return markdownGateTooComplex
	}
}
