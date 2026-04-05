package api

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
)

// SSEWriter provides helpers for writing Server-Sent Events to a Gin response
type SSEWriter struct {
	c       *gin.Context
	flusher func()
}

// NewSSEWriter initializes an SSE response stream
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
func (w *SSEWriter) SendEvent(event string, data any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal SSE data: %w", err)
	}
	if _, err = fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", event, string(jsonBytes)); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}
	w.flusher()
	return nil
}

// SendToken sends a single token event for LLM streaming
func (w *SSEWriter) SendToken(content string) error {
	return w.SendEvent("token", map[string]string{"content": content})
}

// SendError sends an error event
func (w *SSEWriter) SendError(code, message string) error {
	return w.SendEvent("error", map[string]string{"code": code, "message": message})
}

// IsClientGone checks if the client has disconnected
func (w *SSEWriter) IsClientGone() bool {
	select {
	case <-w.c.Request.Context().Done():
		return true
	default:
		return false
	}
}
