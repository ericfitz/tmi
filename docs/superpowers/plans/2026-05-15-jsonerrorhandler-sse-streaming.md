# JSONErrorHandler SSE Streaming Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `JSONErrorHandler` middleware stream SSE responses in real time instead of buffering every event until the handler returns.

**Architecture:** The `bufferedResponseWriter` gains a "streaming" mode. The first time the handler writes/flushes after setting `Content-Type: text/event-stream`, the writer commits headers + any buffered bytes to the underlying writer and forwards all subsequent writes directly. `JSONErrorHandler` skips its post-`c.Next()` transform/pass-through logic when the response was streamed.

**Tech Stack:** Go, Gin web framework, `httptest`, `testify/assert`.

---

## Background

`api/middleware.go` defines `bufferedResponseWriter` (a `gin.ResponseWriter` wrapper) and `JSONErrorHandler` (a global middleware). `bufferedResponseWriter.Write` writes only to an in-memory `bytes.Buffer`; bytes reach the wire after `c.Next()` returns. SSE handlers (`api/timmy_sse.go` → `NewSSEWriter`, used by Timmy chat handlers) set `Content-Type: text/event-stream` and call `c.Writer.Flush()` per event — but the flush flushes the underlying writer, which never received any data. Result: every event is buffered and delivered in one burst when the handler returns. See issue #409 and `docs/superpowers/specs/2026-05-15-jsonerrorhandler-sse-streaming-design.md`.

The fix is **Option C — response-driven pass-through**: detect the SSE Content-Type the handler itself sets, and flip to direct forwarding.

## Current code reference

`bufferedResponseWriter` and `JSONErrorHandler` are in `api/middleware.go` (struct around line 859-894, `JSONErrorHandler` around line 896-953). Current relevant code:

```go
type bufferedResponseWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *bufferedResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *bufferedResponseWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

func (w *bufferedResponseWriter) Status() int {
	return w.statusCode
}

func (w *bufferedResponseWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeader(w.statusCode)
	w.ResponseWriter.WriteHeaderNow()
}
```

`JSONErrorHandler`'s post-processing block (after `c.Next()`):

```go
	// Process the request
	c.Next()

	// Get the response details
	statusCode := blw.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	contentType := blw.Header().Get("Content-Type")
	bodyContent := blw.body.String()

	// Check if this is a plain text error response that needs conversion
	if statusCode >= 400 && (contentType == "" || strings.Contains(contentType, "text/plain")) {
		... // transform to JSON
	} else {
		// Pass through the original response unchanged
		blw.ResponseWriter.WriteHeader(statusCode)
		_, _ = blw.ResponseWriter.Write(blw.body.Bytes())
	}
```

## File Structure

- **Modify** `api/middleware.go` — add `streaming` field + `maybeSwitchToStreaming` helper to `bufferedResponseWriter`; route `Write`/`WriteString`/`WriteHeaderNow` through it; add explicit `Flush`; add `if blw.streaming { return }` early-return in `JSONErrorHandler`.
- **Modify** `api/security_headers_test.go` — add `flushRecorder` test helper + new sub-tests inside `TestJSONErrorHandler`. Add `bytes` and `sync` to the import block.

No other files change. `api/timmy_sse.go` and the Timmy handlers are already correct.

---

## Task 1: Add streaming mode to `bufferedResponseWriter`

**Files:**
- Modify: `api/middleware.go` (struct + methods, ~line 859-894)
- Test: `api/security_headers_test.go` (new `flushRecorder` helper + new sub-tests in `TestJSONErrorHandler`)

- [ ] **Step 1: Add the `flushRecorder` test helper**

`httptest.NewRecorder` does not implement `http.Flusher` and does not record write timing. Add this helper at the **end** of `api/security_headers_test.go` (after the closing brace of `TestJSONErrorHandler`):

```go
// flushRecorder is a test http.ResponseWriter that implements http.Flusher and
// records every Write so tests can assert when bytes reached the writer.
type flushRecorder struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	code    int
	flushes int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: make(http.Header), code: http.StatusOK}
}

func (f *flushRecorder) Header() http.Header { return f.header }

func (f *flushRecorder) WriteHeader(code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.code = code
}

func (f *flushRecorder) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body.Write(b)
}

func (f *flushRecorder) Flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
}

// snapshot returns the body bytes written so far. Safe for concurrent use.
func (f *flushRecorder) snapshot() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body.String()
}
```

Update the import block at the top of `api/security_headers_test.go` from:

```go
import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)
```

to:

```go
import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)
```

- [ ] **Step 2: Write the failing streaming sub-test**

Add this sub-test inside `TestJSONErrorHandler` in `api/security_headers_test.go`, immediately before the closing `}` of the function:

```go
	// Regression test for #409: an SSE handler must stream events to the wire
	// as it writes them, not have them buffered until the handler returns.
	t.Run("SSE response streams incrementally", func(t *testing.T) {
		release := make(chan struct{})
		handlerDone := make(chan struct{})

		// Build the writer chain manually so we can observe the underlying
		// writer mid-handler. JSONErrorHandler wraps c.Writer; we install our
		// flushRecorder as the gin ResponseWriter underneath.
		rec := newFlushRecorder()
		gw := &bufferedResponseWriter{
			ResponseWriter: &testGinWriter{rec: rec},
			body:           bytes.NewBufferString(""),
			statusCode:     http.StatusOK,
		}

		// Simulate the handler: set the SSE content type, write event A,
		// signal, block on release, then write event B.
		go func() {
			defer close(handlerDone)
			gw.Header().Set("Content-Type", "text/event-stream")
			_, _ = gw.WriteString("event: status\ndata: {\"a\":1}\n\n")
			gw.Flush()
			<-release
			_, _ = gw.WriteString("event: status\ndata: {\"b\":2}\n\n")
			gw.Flush()
		}()

		// Event A must be visible on the underlying writer before release.
		assert.Eventually(t, func() bool {
			return strings.Contains(rec.snapshot(), `{"a":1}`)
		}, time.Second, 5*time.Millisecond,
			"event A must reach the underlying writer before the handler unblocks")
		assert.NotContains(t, rec.snapshot(), `{"b":2}`,
			"event B must not appear before the handler unblocks")

		close(release)
		<-handlerDone
		assert.Contains(t, rec.snapshot(), `{"b":2}`, "event B must arrive after release")
		assert.True(t, gw.streaming, "writer should be in streaming mode")
	})
```

This test references `testGinWriter` (defined in Step 3), the `streaming` field, and routes through `WriteString`/`Flush` — none of which exist yet, so it will not compile.

- [ ] **Step 3: Add the `testGinWriter` shim**

`bufferedResponseWriter` embeds `gin.ResponseWriter` (an interface wider than `http.ResponseWriter`). For the unit test we need a minimal `gin.ResponseWriter` that delegates to `flushRecorder`. Add this helper at the end of `api/security_headers_test.go`, after `flushRecorder`:

```go
// testGinWriter adapts a flushRecorder to the gin.ResponseWriter interface for
// unit-testing bufferedResponseWriter directly (without a full gin engine).
type testGinWriter struct {
	gin.ResponseWriter // embedded nil interface; only the methods below are called
	rec                *flushRecorder
}

func (t *testGinWriter) Header() http.Header        { return t.rec.Header() }
func (t *testGinWriter) Write(b []byte) (int, error) { return t.rec.Write(b) }
func (t *testGinWriter) WriteHeader(code int)        { t.rec.WriteHeader(code) }
func (t *testGinWriter) WriteHeaderNow()             {}
func (t *testGinWriter) Flush()                      { t.rec.Flush() }
func (t *testGinWriter) Status() int                 { return t.rec.code }
func (t *testGinWriter) Written() bool               { return t.rec.body.Len() > 0 }
```

Add `"time"` to the test file import block (the new sub-test uses `time.Second`):

```go
import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `make test-unit name=TestJSONErrorHandler`
Expected: COMPILE FAILURE — `gw.streaming undefined`, plus `WriteString`/`Flush` may not flip to streaming. The test must not pass yet.

- [ ] **Step 5: Add the `streaming` field and `maybeSwitchToStreaming` helper**

In `api/middleware.go`, change the struct (around line 859):

```go
// bufferedResponseWriter wraps gin.ResponseWriter to buffer responses
// This allows us to intercept and transform plain text error responses to JSON.
// When the handler sets Content-Type: text/event-stream, the writer flips to
// streaming mode and forwards writes directly to the underlying writer so that
// Server-Sent Events reach the client in real time (see issue #409).
type bufferedResponseWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
	streaming  bool
}

// maybeSwitchToStreaming flips the writer into streaming (pass-through) mode the
// first time it observes a text/event-stream Content-Type. It is idempotent:
// once streaming, subsequent calls are no-ops. On the flip it commits the
// buffered status code and any already-buffered bytes to the underlying writer.
func (w *bufferedResponseWriter) maybeSwitchToStreaming() {
	if w.streaming {
		return
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		return
	}
	w.streaming = true
	w.ResponseWriter.WriteHeader(w.statusCode)
	if w.body.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.body.Bytes())
		w.body.Reset()
	}
}
```

- [ ] **Step 6: Route `Write`, `WriteString`, `WriteHeaderNow` through the helper and add `Flush`**

In `api/middleware.go`, replace the `Write`, `WriteString`, and `WriteHeaderNow` methods and add a new `Flush` method:

```go
func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	w.maybeSwitchToStreaming()
	if w.streaming {
		return w.ResponseWriter.Write(b)
	}
	return w.body.Write(b)
}

func (w *bufferedResponseWriter) WriteString(s string) (int, error) {
	w.maybeSwitchToStreaming()
	if w.streaming {
		return w.ResponseWriter.Write([]byte(s))
	}
	return w.body.WriteString(s)
}

// WriteHeaderNow forces the buffered status onto the underlying writer.
// Without this override, calls to WriteHeaderNow would fall through to the
// embedded gin.ResponseWriter and commit its default status (200) instead of
// the status the handler asked for via c.Status / c.JSON.
func (w *bufferedResponseWriter) WriteHeaderNow() {
	w.maybeSwitchToStreaming()
	if w.streaming {
		// Header already committed by the streaming flip; do not write it again.
		return
	}
	w.ResponseWriter.WriteHeader(w.statusCode)
	w.ResponseWriter.WriteHeaderNow()
}

// Flush forwards a flush to the underlying writer. It first gives
// maybeSwitchToStreaming a chance to flip — covering the (uncommon) case of a
// handler that flushes before its first body write.
func (w *bufferedResponseWriter) Flush() {
	w.maybeSwitchToStreaming()
	if w.streaming {
		w.ResponseWriter.Flush()
	}
}
```

Leave `WriteHeader` and `Status` unchanged.

- [ ] **Step 7: Run the test to verify it passes**

Run: `make test-unit name=TestJSONErrorHandler`
Expected: PASS — including the new `SSE response streams incrementally` sub-test and all five pre-existing sub-tests.

- [ ] **Step 8: Commit**

```bash
git add api/middleware.go api/security_headers_test.go
git commit -m "fix(api): bufferedResponseWriter streams SSE responses (#409)

Add a streaming mode to bufferedResponseWriter. When the handler sets
Content-Type: text/event-stream, the writer commits headers and any
buffered bytes to the underlying writer and forwards subsequent writes
directly, so Server-Sent Events reach the client in real time.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Make `JSONErrorHandler` skip transform for streamed responses

**Files:**
- Modify: `api/middleware.go` (`JSONErrorHandler`, ~line 910-912)
- Test: `api/security_headers_test.go` (new sub-test in `TestJSONErrorHandler`)

- [ ] **Step 1: Write the failing sub-test**

Add this sub-test inside `TestJSONErrorHandler` in `api/security_headers_test.go`, before the closing `}` of the function. It runs a full gin engine with `JSONErrorHandler` installed and an SSE handler that returns a non-2xx-looking body, verifying the body is passed through verbatim (not wrapped in the `Error` JSON envelope) and not duplicated:

```go
	// Regression test for #409: when the response was streamed, JSONErrorHandler
	// must not re-run its transform/pass-through logic — doing so would
	// double-write the body or wrap a streamed body in the Error envelope.
	t.Run("streamed SSE response is not transformed or duplicated", func(t *testing.T) {
		router := gin.New()
		router.Use(JSONErrorHandler())
		router.GET("/sse", func(c *gin.Context) {
			c.Header("Content-Type", "text/event-stream")
			c.Status(http.StatusOK)
			_, _ = c.Writer.WriteString("event: status\ndata: {\"phase\":\"x\"}\n\n")
			c.Writer.Flush()
			_, _ = c.Writer.WriteString("event: token\ndata: {\"content\":\"hi\"}\n\n")
			c.Writer.Flush()
		})

		req := httptest.NewRequest("GET", "/sse", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		body := w.Body.String()
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
		assert.Contains(t, body, `{"phase":"x"}`)
		assert.Contains(t, body, `{"content":"hi"}`)
		// The body must not be wrapped in the JSON error envelope...
		assert.NotContains(t, body, `"error_description"`)
		// ...and each event must appear exactly once (no end-of-handler re-flush).
		assert.Equal(t, 1, strings.Count(body, `{"phase":"x"}`),
			"streamed event must not be duplicated by JSONErrorHandler")
	})
```

Note: `httptest.NewRecorder` *does* implement `http.Flusher` as of Go 1.x via `httptest.ResponseRecorder.Flush()` (it sets the `Flushed` flag). The streaming flip in Task 1 still occurs because the flip is triggered by `Write`/`WriteString` reaching `maybeSwitchToStreaming`, independent of whether the recorder's `Flush` does anything. This sub-test verifies end-to-end behavior through a real gin engine.

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test-unit name=TestJSONErrorHandler`
Expected: FAIL on `streamed SSE response is not transformed or duplicated` — the streamed events appear once (written directly), then `JSONErrorHandler`'s post-`c.Next()` pass-through branch writes `blw.body.Bytes()` again. Since the buffer was reset on the flip, `blw.body` is empty, so duplication may not occur — but `blw.ResponseWriter.WriteHeader(statusCode)` is still called a second time after the stream completed, and on some writers that is a misuse. The assertion most likely to fail first is the duplicate-count or a superfluous-WriteHeader warning; if the test happens to pass without the fix, Step 3 is still required for correctness (the early return is the contract). Treat a passing test here as a signal to inspect: confirm `blw.streaming` is true at end of `c.Next()` and that the early return is what guarantees no second `WriteHeader`.

- [ ] **Step 3: Add the early return in `JSONErrorHandler`**

In `api/middleware.go`, in `JSONErrorHandler`, immediately after `c.Next()` and before `// Get the response details`, insert the early return:

```go
	// Process the request
	c.Next()

	// If the response was streamed (e.g. Server-Sent Events), it has already
	// been written to the wire and the buffer was drained on the streaming
	// flip. Re-running the transform/pass-through logic below would write the
	// header a second time. See issue #409.
	if blw.streaming {
		return
	}

	// Get the response details
	statusCode := blw.statusCode
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test-unit name=TestJSONErrorHandler`
Expected: PASS — all sub-tests including `streamed SSE response is not transformed or duplicated`.

- [ ] **Step 5: Commit**

```bash
git add api/middleware.go api/security_headers_test.go
git commit -m "fix(api): JSONErrorHandler skips transform for streamed responses (#409)

After c.Next(), return early when the response was streamed. The body
is already on the wire and the buffer was drained on the streaming
flip; re-running the transform/pass-through would write the header a
second time.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Direct unit tests for `bufferedResponseWriter` streaming behavior

**Files:**
- Test: `api/security_headers_test.go` (new `TestBufferedResponseWriterStreaming` function)

- [ ] **Step 1: Write the unit test for `maybeSwitchToStreaming`**

Add this **new top-level test function** to `api/security_headers_test.go`, after `TestJSONErrorHandler` and before the `flushRecorder` helper:

```go
// TestBufferedResponseWriterStreaming exercises the streaming-mode flip of
// bufferedResponseWriter directly (issue #409).
func TestBufferedResponseWriterStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newWriter := func() (*bufferedResponseWriter, *flushRecorder) {
		rec := newFlushRecorder()
		return &bufferedResponseWriter{
			ResponseWriter: &testGinWriter{rec: rec},
			body:           bytes.NewBufferString(""),
			statusCode:     http.StatusOK,
		}, rec
	}

	t.Run("stays buffered without SSE content type", func(t *testing.T) {
		w, rec := newWriter()
		_, _ = w.Write([]byte("hello"))
		assert.False(t, w.streaming, "must not flip without text/event-stream")
		assert.Empty(t, rec.snapshot(), "bytes must stay buffered")
		assert.Equal(t, "hello", w.body.String())
	})

	t.Run("flips on SSE content type and forwards writes", func(t *testing.T) {
		w, rec := newWriter()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("first"))
		assert.True(t, w.streaming, "must flip when Content-Type is text/event-stream")
		assert.Equal(t, "first", rec.snapshot(), "byte must reach the underlying writer")
		_, _ = w.Write([]byte("-second"))
		assert.Equal(t, "first-second", rec.snapshot(), "subsequent writes forwarded")
	})

	t.Run("flushes buffered bytes on flip", func(t *testing.T) {
		w, rec := newWriter()
		// Bytes written before the content type is SSE stay buffered...
		_, _ = w.Write([]byte("pre"))
		assert.Empty(t, rec.snapshot())
		// ...then the content type becomes SSE and the next write triggers the
		// flip, draining the buffer first.
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("post"))
		assert.Equal(t, "prepost", rec.snapshot(),
			"buffered bytes must be flushed ahead of the triggering write")
		assert.Equal(t, 0, w.body.Len(), "buffer must be reset after the flip")
	})

	t.Run("maybeSwitchToStreaming is idempotent", func(t *testing.T) {
		w, rec := newWriter()
		w.statusCode = http.StatusOK
		w.Header().Set("Content-Type", "text/event-stream")
		w.maybeSwitchToStreaming()
		w.maybeSwitchToStreaming()
		w.maybeSwitchToStreaming()
		assert.True(t, w.streaming)
		// Three calls, but the header commit and buffer drain happen once.
		// No body was written, so the recorder body stays empty and the flip
		// did not error or panic.
		assert.Empty(t, rec.snapshot())
	})

	t.Run("WriteString forwards in streaming mode", func(t *testing.T) {
		w, rec := newWriter()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.WriteString("via-writestring")
		assert.True(t, w.streaming)
		assert.Equal(t, "via-writestring", rec.snapshot())
	})

	t.Run("Flush before any write triggers the flip", func(t *testing.T) {
		w, rec := newWriter()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Flush()
		assert.True(t, w.streaming, "Flush must be a flip entry point")
		assert.Equal(t, 1, rec.flushes, "underlying Flush must be called")
	})
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `make test-unit name=TestBufferedResponseWriterStreaming`
Expected: PASS — all six sub-tests. (These exercise code already implemented in Tasks 1-2; this task is pure test coverage hardening, so the tests should pass on first run. If any fail, the failure is a real defect in Task 1's implementation — fix `api/middleware.go`, do not weaken the test.)

- [ ] **Step 3: Commit**

```bash
git add api/security_headers_test.go
git commit -m "test(api): direct unit tests for bufferedResponseWriter streaming (#409)

Cover the streaming flip: stays buffered without the SSE content type,
flips and forwards on text/event-stream, drains buffered bytes on flip,
idempotent maybeSwitchToStreaming, and Flush as a flip entry point.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Verification gates

**Files:** none (verification only)

- [ ] **Step 1: Lint**

Run: `make lint`
Expected: no new findings. Fix any introduced by Tasks 1-3 (e.g. unused imports). Note `api/api.go` ST1005 warnings are pre-existing and expected.

- [ ] **Step 2: Build**

Run: `make build-server`
Expected: `bin/tmiserver` builds with no errors.

- [ ] **Step 3: Full unit test suite**

Run: `make test-unit`
Expected: all tests pass. Pay attention to anything else in `api/` that constructs `bufferedResponseWriter` or asserts on `JSONErrorHandler` output.

- [ ] **Step 4: Integration tests**

Run: `make test-integration`
Expected: all pass. This exercises real API endpoints through the middleware chain; confirms non-SSE error transformation still works end-to-end.

- [ ] **Step 5: Manual SSE smoke test (optional but recommended)**

Start the dev environment and a Timmy chat SSE request with `curl -N`, confirming `status` events arrive *before* the LLM call completes rather than in a single burst:

```bash
make start-dev
make start-oauth-stub
curl -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid": "alice"}'
# retrieve token, create a Timmy session, then POST a message with:
#   -H 'Accept: text/event-stream' -N
# observe: status: building_context arrives seconds before the first token.
```

Expected: the four pre-token `status` events arrive spread out over the server-side work, not in one burst at handler return.

- [ ] **Step 6: Security regression check**

`JSONErrorHandler` is the error-envelope path (security-adjacent). Before the final commit, run the `security-regression` skill over the branch changes. Expected: no reintroduced vulnerabilities (verbose-error 500s, etc.). The fix does not change error-response content for non-SSE paths, so this should be clean.

---

## Self-Review Notes

- **Spec coverage:** Task 1 implements the `streaming` field + `maybeSwitchToStreaming` + routed `Write`/`WriteString`/`WriteHeaderNow`/`Flush` (design §Components.1). Task 2 implements the `JSONErrorHandler` early return (design §Components.2). Task 3 + the sub-tests in Tasks 1-2 implement the design §Testing list: incremental streaming regression, streamed-response-not-transformed, no-bytes-lost-on-flip, idempotency. Task 4 covers the compliance notes (no Oracle review needed; security-regression required).
- **Type consistency:** `streaming` (field), `maybeSwitchToStreaming` (method), `flushRecorder`/`newFlushRecorder`/`snapshot`/`testGinWriter` (test helpers) are named identically everywhere they appear.
- **Out of scope confirmed:** no change to `api/timmy_sse.go`, Timmy handlers, or the tmi-ux client.
