package api

import (
	"strings"
	"sync"
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

// forceGateParse is a bare token with a non-allowlisted scheme. It is prose, not
// a link, but it defeats the markdownMayHaveUnsafeLink prefilter, as an attacker
// can, so the goldmark parse really runs.
const forceGateParse = " vbscript:x"

// gateOverrunSize is big enough for the "[a](" run to overrun the default
// budget even unraced, and small enough that abandoned parses finish quickly.
const gateOverrunSize = 64 * 1024

// gateTestSize is the pathological input size: the schema maximum, or 64 KiB
// under -short so parses abandoned on timeout finish (and free their slots)
// quickly even under -race.
func gateTestSize() int {
	if testing.Short() {
		return 64 * 1024
	}
	return markdownMaxLen
}

// waitForGateSlotsDrained fails the test unless every gate parse slot frees up
// in time, i.e. abandoned parses really finish and release their slot, and
// keeps leftover parses from turning later gate tests into spurious 400s.
func waitForGateSlotsDrained(t *testing.T) {
	t.Helper()
	assert.Eventually(t, func() bool { return len(markdownGateSlots) == 0 },
		2*time.Minute, 10*time.Millisecond, "markdown gate parse slots leaked")
}

// TestSanitizeRequiredMarkdownContent_BoundedCost guards the sanitizer plus the
// link gate against pathological, schema-valid inputs that make a CommonMark
// parser slow. Every case carries forceGateParse so the gate's parse runs.
// Each case must return within the gate budget plus slack for the sanitizer's
// other passes, either with its normal outcome (want: "" for success, else a
// substring of the 400 message) or with the too-complex 400. Not skipped under
// -race.
func TestSanitizeRequiredMarkdownContent_BoundedCost(t *testing.T) {
	t.Cleanup(func() { waitForGateSlotsDrained(t) })
	n := gateTestSize() - len(forceGateParse)
	cases := map[string]struct{ in, want string }{
		"blockquote":        {repeatTo(">", n), "nests block quotes or lists too deeply"},
		"emphasis":          {repeatTo("*a", n), ""},
		"underscore":        {repeatTo("_a", n), ""},
		"refdefs":           {repeatTo("[a]: /x\n", n), ""},
		"open brackets":     {repeatTo("[", n), ""},
		"open angle":        {repeatTo("<", n), ""},
		"backticks":         {repeatTo("`", n), ""},
		"bracket paren":     {repeatTo("[a](", n), ""},
		"empty bracket":     {repeatTo("[](", n), ""},
		"image paren":       {repeatTo("![a](", n), ""},
		"brackets balanced": {repeatTo("[", n/2) + repeatTo("]", n/2), ""},
		"nested lists":      {repeatTo("- ", n), "nests block quotes or lists too deeply"},
		"mixed containers":  {repeatTo("> - ", n), "nests block quotes or lists too deeply"},
		"indented lists":    {repeatTo("- a\n  ", 2000) + "- b", ""},
		"unsafe link last":  {repeatTo("plain text with: colons\n", n-40) + "[x](javascript:alert(1))", ""},
		"safe links":        {repeatTo("[a](https://example.com/x) ", n), ""},
		"bare javascript:s": {repeatTo("javascript:x ", n), ""},
	}
	bound := markdownGateBudget + time.Second
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := tc.in + forceGateParse
			start := time.Now()
			out, err := SanitizeRequiredMarkdownContent("content", in)
			elapsed := time.Since(start)
			t.Logf("%s: %d bytes, %v, err=%v", name, len(in), elapsed, err)
			assert.Less(t, elapsed, bound, "input of %d bytes took too long", len(in))
			switch {
			case err != nil && strings.Contains(err.Message, "too complex to validate"):
				assert.Equal(t, 400, err.Status)
			case tc.want == "":
				if assert.Nil(t, err) {
					assert.NotContains(t, strings.ToLower(out), "](javascript:")
				}
			default:
				if assert.NotNil(t, err) {
					assert.Equal(t, 400, err.Status)
					assert.Contains(t, err.Message, tc.want)
				}
			}
		})
	}
}

// slowGateScan replaces markdownGateScan with a parse that blocks until release
// is closed, then reports the content safe. It restores the real scan on
// cleanup (after release, so the blocked goroutines can finish).
func slowGateScan(t *testing.T) (release func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(ch) }) }
	orig := markdownGateScan
	markdownGateScan = func(string) bool { <-ch; return false }
	t.Cleanup(func() {
		release()
		waitForGateSlotsDrained(t)
		markdownGateScan = orig
	})
	return release
}

// shortGateBudget sets markdownGateBudget for one test. Tests that use it must
// not run in parallel.
func shortGateBudget(t *testing.T, d time.Duration) {
	t.Helper()
	orig := markdownGateBudget
	markdownGateBudget = d
	t.Cleanup(func() { markdownGateBudget = orig })
}

func assertTooComplex(t *testing.T, err *RequestError) {
	t.Helper()
	if assert.NotNil(t, err) {
		assert.Equal(t, 400, err.Status)
		assert.Equal(t, "invalid_input", err.Code)
		assert.Contains(t, err.Message, "too complex to validate")
	}
}

// A parse that overruns the budget yields the too-complex 400 on time, and its
// slot is released once the parse itself finishes, not before.
func TestMarkdownGate_TimeoutReturnsTooComplexAndReleasesSlot(t *testing.T) {
	shortGateBudget(t, 50*time.Millisecond)
	release := slowGateScan(t)

	start := time.Now()
	_, err := SanitizeRequiredMarkdownContent("content", "hello"+forceGateParse)
	elapsed := time.Since(start)
	assertTooComplex(t, err)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	assert.Less(t, elapsed, time.Second)
	assert.Equal(t, 1, len(markdownGateSlots), "abandoned parse must keep its slot while running")

	release()
	waitForGateSlotsDrained(t)
}

// The real parser on a pathological input also hits the budget.
func TestMarkdownGate_RealPathologicalParseTimesOut(t *testing.T) {
	t.Cleanup(func() { waitForGateSlotsDrained(t) })
	shortGateBudget(t, 20*time.Millisecond)
	in := repeatTo("[a](", gateOverrunSize) + forceGateParse
	start := time.Now()
	_, err := SanitizeRequiredMarkdownContent("content", in)
	assertTooComplex(t, err)
	assert.Less(t, time.Since(start), time.Second)
}

// With every slot held by an abandoned parse, a new check waits at most the
// budget for a slot and then returns the too-complex 400 without parsing.
func TestMarkdownGate_NoFreeSlotReturnsTooComplex(t *testing.T) {
	shortGateBudget(t, 50*time.Millisecond)
	slowGateScan(t)
	for i := 0; i < cap(markdownGateSlots); i++ {
		_, err := SanitizeRequiredMarkdownContent("content", "hello"+forceGateParse)
		assertTooComplex(t, err)
	}
	assert.Equal(t, cap(markdownGateSlots), len(markdownGateSlots))

	start := time.Now()
	_, err := SanitizeRequiredMarkdownContent("content", "hello"+forceGateParse)
	assertTooComplex(t, err)
	assert.Less(t, time.Since(start), time.Second)
}

// More parallel pathological requests than there are slots all return within
// the bound: none queues behind another's abandoned parse for longer than the
// budget.
func TestMarkdownGate_ConcurrentPathologicalRequestsBounded(t *testing.T) {
	t.Cleanup(func() { waitForGateSlotsDrained(t) })
	in := repeatTo("[a](", gateOverrunSize) + forceGateParse
	const requests = 12
	bound := markdownGateBudget + 2*time.Second
	var wg sync.WaitGroup
	errs := make([]*RequestError, requests)
	durations := make([]time.Duration, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := time.Now()
			_, errs[i] = SanitizeRequiredMarkdownContent("content", in)
			durations[i] = time.Since(start)
		}(i)
	}
	wg.Wait()
	for i := 0; i < requests; i++ {
		assert.Less(t, durations[i], bound, "request %d", i)
		if errs[i] != nil {
			assertTooComplex(t, errs[i])
		}
	}
}

// A panic in the parse goroutine becomes the too-complex 400, not a crash, and
// frees the slot.
func TestMarkdownGate_PanicIsTooComplex(t *testing.T) {
	orig := markdownGateScan
	markdownGateScan = func(string) bool { panic("boom") }
	t.Cleanup(func() { markdownGateScan = orig })
	_, err := SanitizeRequiredMarkdownContent("content", "hello"+forceGateParse)
	assertTooComplex(t, err)
	waitForGateSlotsDrained(t)
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
