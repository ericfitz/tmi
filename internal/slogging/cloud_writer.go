package slogging

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

// CloudLogWriter defines the interface for cloud logging providers.
// Implementations should handle buffering, batching, and retry logic internally.
type CloudLogWriter interface {
	io.Writer

	// WriteLog sends a structured log entry to the cloud provider.
	// This is the preferred method for cloud logging as it preserves structure.
	WriteLog(ctx context.Context, entry LogEntry) error

	// Flush forces any buffered logs to be sent immediately.
	Flush(ctx context.Context) error

	// Close releases resources and flushes any remaining logs.
	Close() error

	// Name returns the provider name for identification.
	Name() string

	// IsHealthy returns true if the cloud provider is reachable.
	IsHealthy(ctx context.Context) bool
}

// LogEntry represents a structured log entry for cloud providers.
type LogEntry struct {
	Timestamp time.Time
	Level     slog.Level
	Message   string
	Attrs     map[string]interface{}
	Source    string // file:line if available
}

// CloudLogHandler is a slog.Handler that writes to cloud logging providers.
// It wraps another handler for local logging and adds cloud logging on top.
type CloudLogHandler struct {
	localHandler slog.Handler
	cloudWriter  CloudLogWriter
	level        slog.Level
	attrs        []slog.Attr
	group        string

	// Buffer for async writes
	buffer     chan LogEntry
	bufferSize int
	wg         sync.WaitGroup
	closed     bool
	closeMu    sync.Mutex

	// Error handling
	lastError   error
	lastErrorMu sync.RWMutex
	errorCount  int64
}

// CloudLogHandlerConfig configures the CloudLogHandler.
type CloudLogHandlerConfig struct {
	// LocalHandler is the handler for local logging (required)
	LocalHandler slog.Handler

	// CloudWriter is the cloud logging provider (required)
	CloudWriter CloudLogWriter

	// Level is the minimum log level for cloud logging
	Level slog.Level

	// BufferSize is the size of the async write buffer (default: 1000)
	BufferSize int

	// AsyncWrites enables asynchronous cloud writes (default: true)
	AsyncWrites bool
}

// NewCloudLogHandler creates a new handler that writes to both local and cloud.
func NewCloudLogHandler(config CloudLogHandlerConfig) *CloudLogHandler {
	if config.BufferSize <= 0 {
		config.BufferSize = 1000
	}

	h := &CloudLogHandler{
		localHandler: config.LocalHandler,
		cloudWriter:  config.CloudWriter,
		level:        config.Level,
		bufferSize:   config.BufferSize,
		buffer:       make(chan LogEntry, config.BufferSize),
	}

	// Start async writer goroutine
	if config.AsyncWrites {
		h.wg.Add(1)
		go h.asyncWriter()
	}

	return h
}

// Enabled reports whether the handler handles records at the given level.
func (h *CloudLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.localHandler.Enabled(ctx, level)
}

// Handle handles the Record.
func (h *CloudLogHandler) Handle(ctx context.Context, record slog.Record) error {
	// Always write to local handler first
	if err := h.localHandler.Handle(ctx, record); err != nil {
		return err
	}

	// Check if cloud logging is enabled for this level
	if record.Level < h.level {
		return nil
	}

	// Build LogEntry from record
	entry := LogEntry{
		Timestamp: record.Time,
		Level:     record.Level,
		Message:   record.Message,
		Attrs:     make(map[string]interface{}),
	}

	// Add group prefix if set
	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}

	// Add handler-level attrs
	for _, attr := range h.attrs {
		entry.Attrs[prefix+attr.Key] = attr.Value.Any()
	}

	// Add record attrs
	record.Attrs(func(attr slog.Attr) bool {
		entry.Attrs[prefix+attr.Key] = attr.Value.Any()
		return true
	})

	// Send to cloud (async or sync)
	h.closeMu.Lock()
	if h.closed {
		h.closeMu.Unlock()
		return nil
	}
	h.closeMu.Unlock()

	select {
	case h.buffer <- entry:
		// Buffered successfully
	default:
		// Buffer full - try sync write
		h.writeToCloud(ctx, entry)
	}

	return nil
}

// WithAttrs returns a new Handler with the given attributes added.
func (h *CloudLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CloudLogHandler{
		localHandler: h.localHandler.WithAttrs(attrs),
		cloudWriter:  h.cloudWriter,
		level:        h.level,
		attrs:        append(h.attrs, attrs...),
		group:        h.group,
		buffer:       h.buffer,
		bufferSize:   h.bufferSize,
	}
}

// WithGroup returns a new Handler with the given group name.
func (h *CloudLogHandler) WithGroup(name string) slog.Handler {
	newGroup := name
	if h.group != "" {
		newGroup = h.group + "." + name
	}

	return &CloudLogHandler{
		localHandler: h.localHandler.WithGroup(name),
		cloudWriter:  h.cloudWriter,
		level:        h.level,
		attrs:        h.attrs,
		group:        newGroup,
		buffer:       h.buffer,
		bufferSize:   h.bufferSize,
	}
}

// asyncWriter processes buffered log entries asynchronously.
func (h *CloudLogHandler) asyncWriter() {
	defer h.wg.Done()

	ctx := context.Background()
	for entry := range h.buffer {
		h.writeToCloud(ctx, entry)
	}
}

// writeToCloud sends a log entry to the cloud provider.
func (h *CloudLogHandler) writeToCloud(ctx context.Context, entry LogEntry) {
	if h.cloudWriter == nil {
		return
	}

	if err := h.cloudWriter.WriteLog(ctx, entry); err != nil {
		h.lastErrorMu.Lock()
		h.lastError = err
		h.errorCount++
		h.lastErrorMu.Unlock()
	}
}

// Close shuts down the handler and flushes remaining logs.
func (h *CloudLogHandler) Close() error {
	h.closeMu.Lock()
	if h.closed {
		h.closeMu.Unlock()
		return nil
	}
	h.closed = true
	h.closeMu.Unlock()

	// Close buffer channel and wait for async writer
	close(h.buffer)
	h.wg.Wait()

	// Flush and close cloud writer
	if h.cloudWriter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := h.cloudWriter.Flush(ctx); err != nil {
			return err
		}
		return h.cloudWriter.Close()
	}

	return nil
}

// LastError returns the last cloud write error, if any.
func (h *CloudLogHandler) LastError() error {
	h.lastErrorMu.RLock()
	defer h.lastErrorMu.RUnlock()
	return h.lastError
}

// ErrorCount returns the total number of cloud write errors.
func (h *CloudLogHandler) ErrorCount() int64 {
	h.lastErrorMu.RLock()
	defer h.lastErrorMu.RUnlock()
	return h.errorCount
}

// NoopCloudWriter is a no-op implementation for testing or when cloud logging is disabled.
type NoopCloudWriter struct{}

func (n *NoopCloudWriter) Write(p []byte) (int, error)                  { return len(p), nil }
func (n *NoopCloudWriter) WriteLog(_ context.Context, _ LogEntry) error { return nil }
func (n *NoopCloudWriter) Flush(_ context.Context) error                { return nil }
func (n *NoopCloudWriter) Close() error                                 { return nil }
func (n *NoopCloudWriter) Name() string                                 { return "noop" }
func (n *NoopCloudWriter) IsHealthy(_ context.Context) bool             { return true }
