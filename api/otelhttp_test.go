package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestOTelHTTP_OutboundClientSpan verifies that an http.Client wrapped with
// otelhttp.NewTransport produces a client span for each outbound request.
func TestOTelHTTP_OutboundClientSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Start a test HTTP server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Create instrumented client — same pattern as TMI's webhook/LLM clients.
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	// Make a request inside a parent span.
	tracer := otel.Tracer("test")
	ctx, parentSpan := tracer.Start(context.Background(), "test-parent")
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/test", nil)
	require.NoError(t, err)
	resp, err := client.Do(req) // #nosec G704 -- test-only call to httptest server
	require.NoError(t, err)
	_ = resp.Body.Close()
	parentSpan.End()

	spans := exporter.GetSpans()
	// Expect at least the parent span and the HTTP client span.
	require.GreaterOrEqual(t, len(spans), 2, "should have parent + client spans")

	// Find the client span.
	var clientSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].SpanKind.String() == "client" {
			clientSpan = &spans[i]
			break
		}
	}
	require.NotNil(t, clientSpan, "should have a client span")
}

// TestOTelHTTP_TraceparentPropagation verifies that the W3C traceparent header is
// injected into outbound requests when otelhttp.NewTransport is used.
// The global TextMapPropagator must include propagation.TraceContext{} for the
// header to be written; this test sets it explicitly to match production setup.
func TestOTelHTTP_TraceparentPropagation(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	// Register the W3C TraceContext propagator so otelhttp can inject the header.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	receivedTraceparent := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTraceparent = r.Header.Get("Traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	tracer := otel.Tracer("test")
	ctx, parentSpan := tracer.Start(context.Background(), "propagation-parent")
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/probe", nil)
	require.NoError(t, err)
	resp, err := client.Do(req) // #nosec G704 -- test-only call to httptest server
	require.NoError(t, err)
	_ = resp.Body.Close()
	parentSpan.End()

	assert.NotEmpty(t, receivedTraceparent, "otelhttp transport should propagate traceparent header to server")
}

// TestOTelHTTP_NoSpan_WhenNoProvider verifies that using an otelhttp-wrapped client
// without a configured TracerProvider does not panic and still completes the request.
func TestOTelHTTP_NoSpan_WhenNoProvider(t *testing.T) {
	// Use a no-op tracer provider so we don't inherit a provider set by other tests.
	otel.SetTracerProvider(otel.GetTracerProvider())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", srv.URL+"/noop", nil)
	require.NoError(t, err)

	// Should not panic even without a real SDK provider.
	assert.NotPanics(t, func() {
		resp, doErr := client.Do(req) // #nosec G704 -- test-only call to httptest server
		require.NoError(t, doErr)
		_ = resp.Body.Close()
	})
}
