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
// SEM@4c239c4f250b659952e70e3af2276d2651e420e9: holds a Gin context and flush function for writing Server-Sent Events (pure)
type SSEWriter struct {
	c       *gin.Context
	flusher func()
}

// NewSSEWriter initializes an SSE response stream
// SEM@4c239c4f250b659952e70e3af2276d2651e420e9: initialize an SSE response stream with required headers on the Gin response
func NewSSEWriter(c *gin.Context) *SSEWriter {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
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
// SEM@de94ca8de4d9f1541750217c9a701b38bf923214: serialize data as JSON and send a named SSE event to the client
func (w *SSEWriter) SendEvent(event string, data any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal SSE data: %w", err)
	}
	safeEvent := sseEventNameRe.ReplaceAllString(event, "")
	writer := w.c.Writer
	if _, err = io.WriteString(writer, "event: "); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}
	if _, err = io.WriteString(writer, safeEvent); err != nil { // #nosec G705 -- safeEvent sanitized by sseEventNameRe (alphanumeric+underscore only)
		return fmt.Errorf("failed to write SSE event: %w", err)
	}
	if _, err = io.WriteString(writer, "\ndata: "); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}
	if _, err = writer.Write(jsonBytes); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}
	if _, err = io.WriteString(writer, "\n\n"); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
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

// IsClientGone checks if the client has disconnected
// SEM@4c239c4f250b659952e70e3af2276d2651e420e9: report whether the SSE client has disconnected by checking request context cancellation (pure)
func (w *SSEWriter) IsClientGone() bool {
	select {
	case <-w.c.Request.Context().Done():
		return true
	default:
		return false
	}
}
