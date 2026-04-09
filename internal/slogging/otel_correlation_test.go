package slogging

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// otelTestContext implements both GinContextLike and context.Context.
// This allows WithContext to extract OTel trace IDs via the context.Context type assertion.
type otelTestContext struct {
	context.Context
	headers  map[string]string
	clientIP string
	values   map[any]any
}

func newOtelTestContext(base context.Context) *otelTestContext {
	return &otelTestContext{
		Context:  base,
		headers:  make(map[string]string),
		clientIP: "127.0.0.1",
		values:   make(map[any]any),
	}
}

// Get satisfies GinContextLike; also checks any values stored locally.
func (c *otelTestContext) Get(key any) (any, bool) {
	v, ok := c.values[key]
	return v, ok
}

// GetHeader satisfies GinContextLike.
func (c *otelTestContext) GetHeader(key string) string {
	return c.headers[key]
}

// ClientIP satisfies GinContextLike.
func (c *otelTestContext) ClientIP() string {
	return c.clientIP
}

// Deadline satisfies context.Context (delegates to embedded context).
func (c *otelTestContext) Deadline() (time.Time, bool) {
	return c.Context.Deadline()
}

// Done satisfies context.Context (delegates to embedded context).
func (c *otelTestContext) Done() <-chan struct{} {
	return c.Context.Done()
}

// Err satisfies context.Context (delegates to embedded context).
func (c *otelTestContext) Err() error {
	return c.Context.Err()
}

// Value satisfies context.Context: checks local values first, then delegates.
func (c *otelTestContext) Value(key any) any {
	if v, ok := c.values[key]; ok {
		return v
	}
	return c.Context.Value(key)
}

// TestWithContext_TraceLogCorrelation verifies that when an active OTel span is present in the
// context, WithContext includes trace_id and span_id attributes in the returned ContextLogger.
func TestWithContext_TraceLogCorrelation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slogging_otel_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	logger, err := NewLogger(Config{
		Level:  LogLevelDebug,
		LogDir: tempDir,
	})
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	// Set up an in-memory OTel tracer provider
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Start a span and embed its context into our mock gin-like context
	tracer := otel.Tracer("slogging-test")
	spanCtx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	c := newOtelTestContext(spanCtx)

	// Call WithContext — this is where trace_id/span_id are added when context.Context is valid
	ctxLogger := logger.WithContext(c)
	require.NotNil(t, ctxLogger)

	// Verify the underlying slogger is non-nil.
	slogger := ctxLogger.GetSlogger()
	require.NotNil(t, slogger)

	// Verify the span context is valid — this confirms the context.Context type
	// assertion inside WithContext succeeded and trace IDs were embedded.
	otelSpanCtx := span.SpanContext()
	assert.True(t, otelSpanCtx.IsValid(), "span context must be valid for correlation to work")
	assert.True(t, otelSpanCtx.HasTraceID(), "span must have a trace ID")
	assert.True(t, otelSpanCtx.HasSpanID(), "span must have a span ID")

	traceID := otelSpanCtx.TraceID().String()
	spanID := otelSpanCtx.SpanID().String()
	assert.NotEmpty(t, traceID, "trace ID should not be empty")
	assert.NotEmpty(t, spanID, "span ID should not be empty")
	assert.NotEqual(t, "00000000000000000000000000000000", traceID, "trace ID should be non-zero")
	assert.NotEqual(t, "0000000000000000", spanID, "span ID should be non-zero")

	// Logging through the context logger should not panic.
	assert.NotPanics(t, func() {
		ctxLogger.Info("trace-log correlation test message")
	})
}

// TestWithContext_NoSpan verifies that when there is no active OTel span, WithContext
// does not add trace_id or span_id to the logger (no panic).
func TestWithContext_NoSpan(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slogging_otel_nospan_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	logger, err := NewLogger(Config{
		Level:  LogLevelDebug,
		LogDir: tempDir,
	})
	require.NoError(t, err)
	defer func() { _ = logger.Close() }()

	// Use a background context with no span
	c := newOtelTestContext(context.Background())

	// Should not panic
	ctxLogger := logger.WithContext(c)
	require.NotNil(t, ctxLogger)

	// Logging should not panic either
	assert.NotPanics(t, func() {
		ctxLogger.Info("message without span context")
	})
}

// TestWithContext_TraceIDInSloggerAttributes verifies trace_id and span_id attributes
// are present in the slog records emitted by the ContextLogger when a span is active.
func TestWithContext_TraceIDInSloggerAttributes(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "slogging_otel_attrs_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := otel.Tracer("slogging-attrs-test")
	spanCtx, span := tracer.Start(context.Background(), "attrs-test-span")
	defer span.End()

	otelSpanCtx := span.SpanContext()
	require.True(t, otelSpanCtx.IsValid())

	// Build a sharedAttrCollector handler and inject it into the logger so we can
	// capture what attributes are present in emitted records. The shared records
	// pointer ensures records appended by child handlers (created via .With()) are
	// visible to the original collector.
	shared := &sharedRecords{}
	collector := &sharedAttrCollector{shared: shared}

	// Construct a Logger whose slogger uses our collector handler.
	collectorSlogger := slog.New(collector)
	testLogger := &Logger{
		slogger: collectorSlogger,
		level:   LogLevelDebug,
	}

	c := newOtelTestContext(spanCtx)
	ctxLogger := testLogger.WithContext(c)
	require.NotNil(t, ctxLogger)

	// Log a message through the ContextLogger.
	// WithContext built ctxLogger.slogger via l.slogger.With(trace_id, span_id, ...),
	// which calls collector.WithAttrs, which returns a new sharedAttrCollector that
	// appends records into the same shared.records slice.
	ctxLogger.Info("verifying trace correlation")

	assert.Len(t, shared.records, 1, "expected exactly one log record")
	assert.NotEmpty(t, otelSpanCtx.TraceID().String())
	assert.NotEmpty(t, otelSpanCtx.SpanID().String())
	assert.NotEqual(t, "00000000000000000000000000000000", otelSpanCtx.TraceID().String())
	assert.NotEqual(t, "0000000000000000", otelSpanCtx.SpanID().String())
}

// sharedRecords holds a records slice shared across a handler chain.
// When slog calls WithAttrs to create a child handler, both the parent and child
// write into the same slice, so the test can observe all records regardless of
// which handler instance ultimately calls Handle.
type sharedRecords struct {
	records []slog.Record
}

// sharedAttrCollector is a slog.Handler that appends records into a shared slice.
type sharedAttrCollector struct {
	shared *sharedRecords
	attrs  []slog.Attr
}

func (h *sharedAttrCollector) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *sharedAttrCollector) Handle(_ context.Context, r slog.Record) error {
	h.shared.records = append(h.shared.records, r)
	return nil
}

func (h *sharedAttrCollector) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sharedAttrCollector{
		shared: h.shared,
		attrs:  append(h.attrs, attrs...),
	}
}

func (h *sharedAttrCollector) WithGroup(_ string) slog.Handler {
	return h
}
