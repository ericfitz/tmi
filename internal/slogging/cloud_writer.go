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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: interface for cloud logging providers supporting structured log delivery and health checks
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
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: structured log entry carrying timestamp, level, message, and attributes for cloud providers
type LogEntry struct {
	Timestamp time.Time
	Level     slog.Level
	Message   string
	Attrs     map[string]any
	Source    string // file:line if available
}

// CloudLogHandler is a slog.Handler that writes to cloud logging providers.
// It wraps another handler for local logging and adds cloud logging on top.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: slog handler that fans out to a local handler and an async cloud logging provider
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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: configuration for CloudLogHandler including local handler, cloud writer, level, and buffer settings
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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: build a CloudLogHandler with async writer goroutine and configurable buffer (mutates shared state)
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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: report whether the handler accepts records at the given log level (pure)
func (h *CloudLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.localHandler.Enabled(ctx, level)
}

// Handle handles the Record.
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: dispatch a log record to local and cloud handlers, buffering cloud delivery asynchronously
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
		Attrs:     make(map[string]any),
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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: return a new handler with additional attributes merged into all subsequent log entries (pure)
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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: return a new handler that namespaces all attribute keys under the given group (pure)
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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: drain the log entry buffer and forward each entry to the cloud writer (mutates shared state)
func (h *CloudLogHandler) asyncWriter() {
	defer h.wg.Done()

	ctx := context.Background()
	for entry := range h.buffer {
		h.writeToCloud(ctx, entry)
	}
}

// writeToCloud sends a log entry to the cloud provider.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: send a single log entry to the cloud writer and record any error (mutates shared state)
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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: flush remaining buffered log entries and shut down the cloud handler (mutates shared state)
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
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: return the most recent cloud write error, or nil if none (pure)
func (h *CloudLogHandler) LastError() error {
	h.lastErrorMu.RLock()
	defer h.lastErrorMu.RUnlock()
	return h.lastError
}

// ErrorCount returns the total number of cloud write errors.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: return the total number of cloud write errors accumulated since start (pure)
func (h *CloudLogHandler) ErrorCount() int64 {
	h.lastErrorMu.RLock()
	defer h.lastErrorMu.RUnlock()
	return h.errorCount
}

// NoopCloudWriter is a no-op implementation for testing or when cloud logging is disabled.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: no-op CloudLogWriter implementation for testing or when cloud logging is disabled
type NoopCloudWriter struct{}

// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: discard bytes and report success for the no-op cloud writer (pure)
func (n *NoopCloudWriter) Write(p []byte) (int, error) { return len(p), nil }

// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: discard a structured log entry for the no-op cloud writer (pure)
func (n *NoopCloudWriter) WriteLog(_ context.Context, _ LogEntry) error { return nil }

// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: no-op flush for the no-op cloud writer (pure)
func (n *NoopCloudWriter) Flush(_ context.Context) error { return nil }

// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: no-op close for the no-op cloud writer (pure)
func (n *NoopCloudWriter) Close() error { return nil }

// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: return the provider name for the no-op cloud writer (pure)
func (n *NoopCloudWriter) Name() string { return "noop" }

// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: report healthy for the no-op cloud writer (pure)
func (n *NoopCloudWriter) IsHealthy(_ context.Context) bool { return true }
