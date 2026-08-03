package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/ericfitz/tmi/internal/slogging"
)

// sseEventNameRe restricts SSE event names to safe alphanumeric/underscore characters.
var sseEventNameRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// SSEWriter provides helpers for writing Server-Sent Events to a Gin response
// SEM@e2f78925781ef0a3d3eb6ea222688508cec07972: holds a Gin context and flush function for writing Server-Sent Events (pure)
type SSEWriter struct {
	c       *gin.Context
	flusher func()
	// writeErr latches the first failed write. Once set, the client is
	// unreachable and every later send short-circuits instead of retrying a
	// socket that will not recover. Callers observe it through IsClientGone /
	// Err, which is what lets an in-flight LLM stream stop early.
	writeErr error
}

// NewSSEWriter initializes an SSE response stream
// SEM@495d0e707e286e7230e11759448f784ebb220018: initialize an SSE response stream with required headers on the Gin response
func NewSSEWriter(c *gin.Context) *SSEWriter {
	c.Header("Content-Type", "text/event-stream")
	// Do NOT override Cache-Control here. The SecurityHeaders middleware already
	// set "no-store, no-cache, must-revalidate" earlier in the chain, which is
	// both correct for a sensitive stream (no-store) and sufficient to keep SSE
	// uncached (no-cache). Setting a bare "no-cache" here dropped no-store and
	// was the sole finding CATS' CheckSecurityHeaders flagged on this path.
	c.Header("X-Accel-Buffering", "no") // Disable nginx buffering

	// Clear the connection's write deadline for the life of this response.
	//
	// http.Server.WriteTimeout (10s by default here) bounds the ENTIRE response
	// write, and Go arms it when the request header is read — not per write. A
	// stream that outlives it has every subsequent write fail with i/o timeout,
	// silently: the handler keeps producing events into a dead socket and still
	// returns 200, and the abrupt close at handler return is what the browser
	// reports as ERR_HTTP2_PROTOCOL_ERROR. Measured against the AWS deployment,
	// the last byte reached the browser at 10.055s against the 10s timeout while
	// the server logged "200 (24.0s)" with no error anywhere.
	//
	// A zero time.Time means "no deadline", which is the correct posture for an
	// open-ended stream; request duration is bounded by the LLM timeout and the
	// request context instead. Failure is logged rather than fatal: a writer
	// that cannot be unwrapped (some test recorders) still streams correctly
	// under no deadline at all.
	//
	// NOTE: c.Header("Connection", "keep-alive") deliberately removed. It is
	// redundant on HTTP/1.1, where Go manages keep-alive, and it is a
	// connection-specific header that RFC 9113 §8.2.2 forbids in HTTP/2.
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{}); err != nil {
		slogging.Get().WithContext(c).Warn(
			"SSE: could not clear the write deadline (%v); a stream longer than "+
				"the server WriteTimeout will be truncated", err)
	}

	return &SSEWriter{
		c: c,
		flusher: func() {
			c.Writer.Flush()
		},
	}
}

// SendEvent sends a named SSE event with JSON data
// SEM@e2f78925781ef0a3d3eb6ea222688508cec07972: serialize data as JSON and send a named SSE event to the client
func (w *SSEWriter) SendEvent(event string, data any) error {
	// Once a write has failed the connection does not recover, and every
	// further attempt is a syscall that fails the same way. Short-circuit so a
	// long token stream against a dead client costs nothing.
	if w.writeErr != nil {
		return w.writeErr
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		// A marshalling failure is a bug in the payload, not a broken
		// connection: do not latch it, the stream is still usable.
		return fmt.Errorf("failed to marshal SSE data: %w", err)
	}
	safeEvent := sseEventNameRe.ReplaceAllString(event, "")
	writer := w.c.Writer
	if _, err = io.WriteString(writer, "event: "); err != nil {
		return w.failed(err)
	}
	if _, err = io.WriteString(writer, safeEvent); err != nil { // #nosec G705 -- safeEvent sanitized by sseEventNameRe (alphanumeric+underscore only)
		return w.failed(err)
	}
	if _, err = io.WriteString(writer, "\ndata: "); err != nil {
		return w.failed(err)
	}
	if _, err = writer.Write(jsonBytes); err != nil {
		return w.failed(err)
	}
	if _, err = io.WriteString(writer, "\n\n"); err != nil {
		return w.failed(err)
	}
	w.flusher()

	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmySSEEvents.Add(context.Background(), 1, metric.WithAttributes(attribute.String("event_type", safeEvent)))
	}

	return nil
}

// SendToken sends a single token event for LLM streaming
// SEM@4c239c4f250b659952e70e3af2276d2651e420e9: send a streaming LLM token as an SSE token event
func (w *SSEWriter) SendToken(content string) error {
	return w.SendEvent("token", map[string]string{"content": content})
}

// SendError sends an error event
// SEM@4c239c4f250b659952e70e3af2276d2651e420e9: send an error code and message as an SSE error event
func (w *SSEWriter) SendError(code, message string) error {
	return w.SendEvent("error", map[string]string{"code": code, "message": message})
}

// failed latches the first write error and logs it once. Subsequent sends
// short-circuit on the latch rather than logging per event, which matters for a
// token stream that would otherwise emit hundreds of identical lines.
// SEM@e2f78925781ef0a3d3eb6ea222688508cec07972: latch and log the first SSE write failure, returning it wrapped
func (w *SSEWriter) failed(err error) error {
	w.writeErr = fmt.Errorf("failed to write SSE event: %w", err)
	slogging.Get().WithContext(w.c).Warn(
		"SSE: client unreachable, abandoning stream: %v", err)
	return w.writeErr
}

// Err returns the first write failure, or nil while the stream is healthy.
// SEM@e2f78925781ef0a3d3eb6ea222688508cec07972: return the first SSE write failure, or nil if none (pure)
func (w *SSEWriter) Err() error { return w.writeErr }

// IsClientGone reports whether the client can no longer receive events.
//
// Two distinct conditions, and the second is why write errors are latched. A
// client that closes cleanly cancels the request context. A client whose writes
// fail — the server's own WriteTimeout expiring mid-stream being the case that
// motivated this — does NOT cancel the context: from Go's side the request is
// still in flight, so the context check alone reports a healthy client and the
// handler keeps generating LLM tokens nobody will receive. Checking the latched
// write error covers that.
// SEM@e2f78925781ef0a3d3eb6ea222688508cec07972: report whether the SSE client is unreachable, via context cancellation or a latched write failure (pure)
func (w *SSEWriter) IsClientGone() bool {
	if w.writeErr != nil {
		return true
	}
	select {
	case <-w.c.Request.Context().Done():
		return true
	default:
		return false
	}
}
