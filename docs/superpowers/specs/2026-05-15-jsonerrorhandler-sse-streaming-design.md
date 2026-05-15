# Design: Fix JSONErrorHandler SSE Buffering (#409)

**Date:** 2026-05-15
**Issue:** [#409](https://github.com/ericfitz/tmi/issues/409) — `fix: JSONErrorHandler middleware buffers SSE responses, breaking real-time streaming`
**Branch:** `dev/1.4.0`

## Problem

`JSONErrorHandler` (`api/middleware.go`) is a global middleware that wraps every
response with `bufferedResponseWriter` so it can transform plain-text Gin
framework errors into the JSON `Error` envelope. `bufferedResponseWriter.Write`
writes only to an in-memory buffer; the buffered bytes reach the wire after
`c.Next()` returns.

For non-streaming handlers this is invisible. For **Server-Sent Events (SSE)**
handlers — the Timmy chat endpoints (`HandleCreateTimmySession`,
`HandleSendTimmyMessage`) — it disables real-time streaming entirely. Every
`status`/`message_start`/`token` event the handler emits during the LLM call is
held in memory and arrives in a single TCP burst when the handler returns.
Client evidence in #409 shows ~9.4s of dead status bubble during the LLM call,
which is exactly the latency the status events were designed to mask.

`SSEWriter.SendEvent` calls `c.Writer.Flush()`, but the flush flushes the
*underlying* writer, which has received no data — all bytes are still in
`bufferedResponseWriter.body`.

## Approach

**Option C — response-driven pass-through.** The buffered writer detects when the
handler sets `Content-Type: text/event-stream` on its header map and, from the
first body write onward, forwards writes directly to the inner writer instead of
buffering.

This was chosen over the two alternatives in #409 because it is the only option
that is both **deterministic** and **self-maintaining**:

- *Skip-by-Accept-header* depends on the client always sending
  `Accept: text/event-stream`; if it doesn't, streaming silently breaks again.
- *Skip-by-path* requires a hand-maintained list of SSE endpoint prefixes that
  must be kept in sync as endpoints are added.
- *Pass-through-on-stream (Option C)* keys off what the handler actually does.
  `NewSSEWriter` always sets `Content-Type: text/event-stream`, so any current
  or future SSE endpoint is covered with zero configuration.

## Components

### 1. `bufferedResponseWriter` — add streaming mode (`api/middleware.go`)

Add a `streaming bool` field.

Add an idempotent helper `maybeSwitchToStreaming()`:

1. If `w.streaming` is already true, return.
2. Read `w.Header().Get("Content-Type")`. If it does **not** contain
   `text/event-stream`, return — the writer stays in buffered mode.
3. Otherwise flip to streaming mode:
   - Set `w.streaming = true`.
   - Commit the buffered status to the inner writer:
     `w.ResponseWriter.WriteHeader(w.statusCode)`.
   - If `w.body.Len() > 0`, flush already-buffered bytes to the inner writer
     (`w.ResponseWriter.Write(w.body.Bytes())`) and reset the buffer.

The helper is called at the top of every write/flush entry point so that
whichever the handler hits first triggers the flip (per the approved design,
the flip must be robust regardless of handler call order):

- `Write` — after the helper, dispatch to `w.ResponseWriter` if `streaming`,
  else to `w.body`.
- `WriteString` — same dispatch.
- `WriteHeaderNow` — after the helper; if `streaming`, the header has already
  been committed by the flip, so guard against a second `WriteHeader` call;
  otherwise keep the existing behavior.
- `Flush` — a **new explicit method** on `bufferedResponseWriter`. Currently
  `Flush` is inherited from the embedded `gin.ResponseWriter`, so a `Flush`
  before any `Write` would not trigger the flip. The new method calls the
  helper then `w.ResponseWriter.Flush()`. (Current SSE code always writes
  before flushing, but the explicit method makes the flip entry-point complete.)

### 2. `JSONErrorHandler` — early return for streamed responses (`api/middleware.go`)

After `c.Next()` returns, add an early check at the top of the post-processing
block:

```go
if blw.streaming {
    return
}
```

When the response was streamed, the headers and body are already on the wire and
the buffer is empty. The existing plain-text→JSON conversion and pass-through
writes must be skipped — re-running them would double-write headers and body.

## Data Flow

### SSE request (Timmy chat)

1. `JSONErrorHandler` installs `bufferedResponseWriter` (`streaming = false`).
2. Handler calls `NewSSEWriter(c)`, which sets
   `Content-Type: text/event-stream` (and `Cache-Control`, `Connection`,
   `X-Accel-Buffering`) on the buffered writer's header map.
3. Handler's first `SendEvent` → `Write` → `maybeSwitchToStreaming()` sees the
   SSE Content-Type → commits headers + status, flips to `streaming`, forwards
   the bytes. `SendEvent`'s trailing `Flush()` now reaches the real
   `http.Flusher`. **The event is on the wire immediately.**
4. All later events forward directly to the inner writer.
5. `c.Next()` returns. `JSONErrorHandler` sees `blw.streaming == true` and
   returns without touching the response.

### Non-SSE request (unchanged)

Content-Type never becomes `text/event-stream`, so `maybeSwitchToStreaming` is a
no-op on every call. Writes stay buffered. The existing error-conversion and
pass-through logic runs exactly as today, including the `#289` status-code
behavior (`Status()`, `WriteHeader`, `WriteHeaderNow` overrides).

## Error Handling

- The mid-write flip commits headers exactly once; the `streaming` guard makes
  `maybeSwitchToStreaming` idempotent.
- If `WriteHeader`/`WriteHeaderNow` is called after the flip, the `streaming`
  flag prevents a second header commit to the inner writer.
- A handler that sets the SSE Content-Type but writes nothing and returns:
  `maybeSwitchToStreaming` never fires (no write/flush) → the writer stays
  buffered → it is handled as a normal (empty) response. This is acceptable;
  real SSE handlers always write at least one event.

## Testing

Tests extend the existing `TestJSONErrorHandler` in
`api/security_headers_test.go`, which already has five sub-tests including two
`#289` regression tests for `bufferedResponseWriter`.

`httptest.NewRecorder` does not implement `http.Flusher` and does not record
write timing. A small test `ResponseWriter` (`flushRecorder`) is added: it wraps
a buffer, implements `http.Flusher`, and records each `Write`/`Flush` so a test
can assert bytes reached the writer *before* the handler returned.

New sub-tests:

- **SSE response streams incrementally (#409 regression)** — handler sets
  `Content-Type: text/event-stream`, writes event A, signals a channel, blocks,
  then writes event B. Assert event A is observable on the writer *before* the
  handler unblocks.
- **streaming response not transformed** — an SSE handler that produces a
  non-2xx status still has its raw body passed through, not wrapped in the
  `Error` JSON envelope.
- **no bytes lost across the flip** — handler sets the SSE Content-Type then
  writes; assert all written bytes are present on the inner writer.
- **`maybeSwitchToStreaming` idempotency** — direct unit test of
  `bufferedResponseWriter`: repeated calls flip once, commit headers once.

The existing five sub-tests (non-SSE behavior plus both `#289` regressions) must
continue to pass unchanged.

## Out of Scope

- The Timmy client-side "Ready!" timer (tmi-ux#690), in flight separately.
- Any change to `SSEWriter` itself — it is correct; the bug is entirely in the
  buffering middleware.

## Compliance Notes

- **No database changes** — `oracle-db-admin` review not required.
- **Security-adjacent middleware** — `security-regression` check required before
  commit (`JSONErrorHandler` is the error-envelope path).
