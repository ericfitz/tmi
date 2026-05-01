package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestRedactingSpanExporter_RedactsSensitiveAttributes pins T23: any future
// instrumentation that sets `attribute.String("authorization", header)`
// directly on a span must have the value replaced with "<redacted>" in the
// exported span. If a refactor removes the wrapping in otel.Setup, this
// test fails.
func TestRedactingSpanExporter_RedactsSensitiveAttributes(t *testing.T) {
	inner := tracetest.NewInMemoryExporter()
	wrapped := NewRedactingSpanExporter(inner)

	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(wrapped))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-span")
	span.SetAttributes(
		attribute.String("authorization", "Bearer secrettoken"),
		attribute.String("client_callback", "https://evil.example/grab?code=xyz"),
		attribute.String("cookie", "session=abc"),
		attribute.String("Set-Cookie", "session=abc"),
		attribute.String("password", "hunter2"),
		attribute.String("api_key", "sk-12345"),
		attribute.String("X-Auth-Token", "tok"),
		attribute.String("access_token", "at-1"),
		attribute.String("refresh_token", "rt-1"),
		attribute.String("id_token", "id-1"),
		attribute.String("client_secret", "cs-1"),
		attribute.String("http.request.header.authorization", "Bearer xyz"),
		// Non-sensitive attributes pass through unchanged.
		attribute.String("threat_model.id", "abc-123"),
		attribute.Int("http.status_code", 200),
		attribute.String("user.id", "user-42"),
	)
	span.End()

	require.Len(t, inner.GetSpans(), 1, "expected one exported span")
	exported := inner.GetSpans()[0]

	attrs := map[string]string{}
	for _, kv := range exported.Attributes {
		if kv.Value.Type() == attribute.STRING {
			attrs[string(kv.Key)] = kv.Value.AsString()
		}
	}

	sensitive := []string{
		"authorization", "client_callback", "cookie", "Set-Cookie",
		"password", "api_key", "X-Auth-Token", "access_token",
		"refresh_token", "id_token", "client_secret",
		"http.request.header.authorization",
	}
	for _, k := range sensitive {
		v, ok := attrs[k]
		require.True(t, ok, "attribute %q should be present (redacted, not removed)", k)
		assert.Equal(t, RedactedAttributeValue, v, "attribute %q must be redacted", k)
	}

	// Non-sensitive attributes survive unchanged.
	assert.Equal(t, "abc-123", attrs["threat_model.id"])
	assert.Equal(t, "user-42", attrs["user.id"])

	// Verify the int attribute is preserved by inspecting it directly.
	var statusCodeOK bool
	for _, kv := range exported.Attributes {
		if string(kv.Key) == "http.status_code" {
			assert.Equal(t, int64(200), kv.Value.AsInt64())
			statusCodeOK = true
		}
	}
	assert.True(t, statusCodeOK, "http.status_code attribute should be preserved")
}

// TestRedactingSpanExporter_PreservesSpanIdentity confirms that the wrapper
// does not alter the span name, status, kind, or trace context. If a
// refactor breaks this, traces become incoherent in the collector.
func TestRedactingSpanExporter_PreservesSpanIdentity(t *testing.T) {
	inner := tracetest.NewInMemoryExporter()
	wrapped := NewRedactingSpanExporter(inner)

	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(wrapped))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "named-span", trace.WithSpanKind(trace.SpanKindServer))
	span.SetAttributes(attribute.String("authorization", "Bearer x"))
	span.End()

	require.Len(t, inner.GetSpans(), 1)
	exported := inner.GetSpans()[0]
	assert.Equal(t, "named-span", exported.Name)
	assert.Equal(t, trace.SpanKindServer, exported.SpanKind)
}

// TestSensitiveAttributeKey_Catalog covers the case-insensitive substring
// catalog that drives the redactor. If a future change drops a key from
// the catalog, this test catches it.
func TestSensitiveAttributeKey_Catalog(t *testing.T) {
	cases := []struct {
		key      string
		expected bool
	}{
		// Sensitive
		{"authorization", true},
		{"Authorization", true},
		{"http.request.header.authorization", true},
		{"client_callback", true},
		{"cookie", true},
		{"Cookie", true},
		{"set-cookie", true},
		{"password", true},
		{"client_secret", true},
		{"id_token", true},
		{"access_token", true},
		{"refresh_token", true},
		{"any-bearer-thing", true},
		{"http.access_token.lifetime_ms", true},
		{"jwt.iss", true},
		// Not sensitive
		{"threat_model.id", false},
		{"user.id", false},
		{"http.status_code", false},
		{"db.query", false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			assert.Equal(t, tc.expected, sensitiveAttributeKey(tc.key))
		})
	}
}

// TestRedactingSpanExporter_NilInnerSafe confirms the wrapper is robust
// to a nil inner — used during graceful shutdown / disabled telemetry.
func TestRedactingSpanExporter_NilInnerSafe(t *testing.T) {
	r := NewRedactingSpanExporter(nil)
	assert.NoError(t, r.ExportSpans(context.Background(), nil))
	assert.NoError(t, r.Shutdown(context.Background()))
}
