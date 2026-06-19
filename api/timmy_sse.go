package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	tmiotel "github.com/ericfitz/tmi/internal/otel"
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
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // Disable nginx buffering

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
