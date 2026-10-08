package api

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/errcode"
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

// waitForGateParsesDone fails the test unless every gate parse, including
// abandoned ones, has stopped within a short time, and keeps leftover parses
// from skewing later tests.
func waitForGateParsesDone(t *testing.T) {
	t.Helper()
	assert.Eventually(t, func() bool { return markdownGateInFlight.Load() == 0 },
		5*time.Second, time.Millisecond, "markdown gate parses kept running past their deadline")
}

// gatePathologicalInputs are the known quadratic shapes, each forcing the parse.
func gatePathologicalInputs(n int) map[string]string {
	n -= len(forceGateParse)
	return map[string]string{
		"[a](":         repeatTo("[a](", n) + forceGateParse,
		"[](":          repeatTo("[](", n) + forceGateParse,
		"![a](":        repeatTo("![a](", n) + forceGateParse,
		"[ x n, ] x n": repeatTo("[", n/2) + repeatTo("]", n/2) + forceGateParse,
		"ref defs":     repeatTo("[a]: /x\n", n) + forceGateParse,
	}
}

// TestSanitizeRequiredMarkdownContent_BoundedCost guards the sanitizer plus the
// link gate against pathological, schema-valid inputs that make a CommonMark
// parser slow. Every case carries forceGateParse so the gate's parse runs.
// Each case must return within the gate budget plus slack for the sanitizer's
// other passes, either with its normal outcome (want: "" for success, else a
// substring of the 400 message) or with the too-complex 400. Not skipped under
// -race.
func TestSanitizeRequiredMarkdownContent_BoundedCost(t *testing.T) {
	t.Cleanup(func() { waitForGateParsesDone(t) })
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

// shortGateBudget sets markdownGateBudget for one test. Tests that use it must
// not run in parallel.
func shortGateBudget(t *testing.T, d time.Duration) {
	t.Helper()
	orig := markdownGateBudget
	markdownGateBudget = d
	t.Cleanup(func() { markdownGateBudget = orig })
}

// stubGateScan replaces markdownGateScan for one test.
func stubGateScan(t *testing.T, scan func(string, time.Time) bool) {
	t.Helper()
	orig := markdownGateScan
	markdownGateScan = scan
	t.Cleanup(func() {
		waitForGateParsesDone(t)
		markdownGateScan = orig
	})
}

func assertTooComplex(t *testing.T, err *RequestError) {
	t.Helper()
	if assert.NotNil(t, err) {
		assert.Equal(t, 400, err.Status)
		assert.Equal(t, errcode.InvalidInput, err.Code)
		assert.Contains(t, err.Message, "too complex to validate")
		assert.Positive(t, err.markdownTooComplexBytes, "rejection must be tagged for logging")
	}
}

// The real parse stops at its deadline: for every known quadratic shape the
// check returns the too-complex 400 on time and the parse goroutine exits
// within a few milliseconds of the deadline, so abandoned work cannot pile up.
func TestMarkdownGate_ParseCancelledAtDeadline(t *testing.T) {
	t.Cleanup(func() { waitForGateParsesDone(t) })
	shortGateBudget(t, 100*time.Millisecond)
	for name, in := range gatePathologicalInputs(gateTestSize()) {
		t.Run(name, func(t *testing.T) {
			waitForGateParsesDone(t)
			start := time.Now()
			deadline := start.Add(markdownGateBudget)
			result := markdownGateCheck(in)
			returned := time.Now()
			for markdownGateInFlight.Load() != 0 && time.Since(returned) < 5*time.Second {
				time.Sleep(100 * time.Microsecond)
			}
			stopped := time.Now()
			t.Logf("%s: %d bytes, result=%d, returned %v after deadline, parse stopped %v after deadline",
				name, len(in), result, returned.Sub(deadline), stopped.Sub(deadline))
			if result != markdownGateTooComplex {
				// Fast enough to finish within the budget: nothing to cancel.
				assert.Equal(t, markdownGateSafe, result)
				return
			}
			assert.Less(t, stopped.Sub(deadline), 100*time.Millisecond, "parse kept running after its deadline")
		})
	}
}

// A scan that ignores the deadline still cannot hold the caller past the
// budget (the timer is the backstop).
func TestMarkdownGate_TimerBackstopsStuckScan(t *testing.T) {
	shortGateBudget(t, 50*time.Millisecond)
	release := make(chan struct{})
	stubGateScan(t, func(string, time.Time) bool { <-release; return false })
	defer close(release)

	start := time.Now()
	_, err := SanitizeRequiredMarkdownContent("content", "hello"+forceGateParse)
	elapsed := time.Since(start)
	assertTooComplex(t, err)
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	assert.Less(t, elapsed, time.Second)
}

// Starvation regression (#1013 review): pathological requests in flight must
// not make anyone else's ordinary note fail. A small prose note and a large
// link-heavy note sent mid-attack both succeed.
func TestMarkdownGate_AttackDoesNotStarveOtherNotes(t *testing.T) {
	t.Cleanup(func() { waitForGateParsesDone(t) })
	attack := repeatTo("[a](", gateTestSize()) + forceGateParse
	const attackers = 8
	var wg sync.WaitGroup
	for i := 0; i < attackers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := SanitizeRequiredMarkdownContent("content", attack)
			if err != nil {
				assertTooComplex(t, err)
			}
		}()
	}
	assert.Eventually(t, func() bool { return markdownGateInFlight.Load() >= attackers },
		time.Second, time.Millisecond, "attack parses did not start")

	// Starvation shows up as a too-complex rejection of the legitimate notes,
	// which the Nil assertions below catch. The wall-clock bound only guards
	// against a hang: sanitizing a 256 KiB note outside the gate (bluemonday,
	// the scanner) is not covered by the gate's budget, and under -race on a
	// 2-CPU CI runner with 8 attackers it alone took over 1.2 s.
	legitStart := time.Now()
	small := "Note: hello"
	out, err := SanitizeRequiredMarkdownContent("content", small)
	assert.Nil(t, err, "small note rejected during attack")
	assert.Equal(t, small, out)

	large := repeatTo("Step: see [a](https://example.com/x) ", markdownMaxLen)
	_, err = SanitizeRequiredMarkdownContent("content", large)
	assert.Nil(t, err, "large legitimate note rejected during attack")

	assert.Less(t, time.Since(legitStart), 10*time.Second,
		"legitimate notes must not hang behind the attack's parses")
	wg.Wait()
}

// More parallel pathological requests than CPUs all return within the bound,
// and their parses stop promptly.
func TestMarkdownGate_ConcurrentPathologicalRequestsBounded(t *testing.T) {
	in := repeatTo("[a](", gateOverrunSize) + forceGateParse
	requests := 2*runtime.GOMAXPROCS(0) + 4
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
	waitForGateParsesDone(t)
}

// A panic in the parse goroutine becomes the too-complex 400, not a crash.
func TestMarkdownGate_PanicIsTooComplex(t *testing.T) {
	stubGateScan(t, func(string, time.Time) bool { panic("boom") })
	_, err := SanitizeRequiredMarkdownContent("content", "hello"+forceGateParse)
	assertTooComplex(t, err)
}

// The deadline context lets an unhurried parse finish normally.
func TestMarkdownHasUnsafeLinkBefore_FinishesWithinDeadline(t *testing.T) {
	far := time.Now().Add(time.Minute)
	assert.True(t, markdownHasUnsafeLinkBefore("> [x](\n> javascript:alert(1))", far))
	assert.False(t, markdownHasUnsafeLinkBefore("[x](https://example.com) Note: ok", far))
	assert.Panics(t, func() {
		markdownHasUnsafeLinkBefore(repeatTo("[a](", 64*1024), time.Now().Add(-time.Second))
	})
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
