package otel

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// RedactedAttributeValue is the placeholder substituted for sensitive
// span-attribute values before export. Mirrors the slogging convention.
const RedactedAttributeValue = "<redacted>"

// sensitiveAttributeKey reports whether the given attribute key (case-
// insensitive) names a sensitive value that must not leave the process in
// an OTLP span. The catalog mirrors the slogging redaction patterns:
// authorization headers, tokens, cookies, secrets, OAuth client_callback
// (which often carries a session-bearing redirect target).
//
// The check is case-insensitive and considers any key that *contains* one
// of the sensitive substrings — this catches future instrumentation that
// uses keys like `http.request.header.authorization` without having to
// extend the catalog.
func sensitiveAttributeKey(key string) bool {
	k := strings.ToLower(key)
	for _, needle := range []string{
		"authorization",
		"bearer",
		"cookie",
		"set-cookie",
		"password",
		"secret",
		"client_secret",
		"client_callback",
		"id_token",
		"access_token",
		"refresh_token",
		"refresh-token",
		"api_key",
		"api-key",
		"x-auth-token",
		"jwt",
		"token", // catches *_token, _token_*, etc.
	} {
		if strings.Contains(k, needle) {
			return true
		}
	}
	return false
}

// redactedReadOnlySpan wraps a sdktrace.ReadOnlySpan and overrides
// Attributes() to apply the sensitive-key redaction. All other methods
// delegate to the embedded span. This pattern is the only way to filter
// attributes in OTel Go because ReadOnlySpan's private() method makes the
// interface unimplementable from outside the SDK.
type redactedReadOnlySpan struct {
	sdktrace.ReadOnlySpan
	redacted []attribute.KeyValue
}

func (r redactedReadOnlySpan) Attributes() []attribute.KeyValue { return r.redacted }

func redactAttributes(attrs []attribute.KeyValue) []attribute.KeyValue {
	if len(attrs) == 0 {
		return attrs
	}
	out := make([]attribute.KeyValue, len(attrs))
	for i, kv := range attrs {
		if sensitiveAttributeKey(string(kv.Key)) {
			out[i] = attribute.String(string(kv.Key), RedactedAttributeValue)
		} else {
			out[i] = kv
		}
	}
	return out
}

// RedactingSpanExporter wraps an sdktrace.SpanExporter so that span
// attributes matching the sensitive-key catalog are replaced with
// "<redacted>" before they reach the OTLP collector. This is the
// defense-in-depth fix for T23 — even if a future instrumentation path
// sets `attribute.String("authorization", header)` directly, the value
// never leaves the process.
type RedactingSpanExporter struct {
	inner sdktrace.SpanExporter
}

// NewRedactingSpanExporter wraps the given exporter with attribute
// redaction. Returns inner verbatim if it is nil.
func NewRedactingSpanExporter(inner sdktrace.SpanExporter) *RedactingSpanExporter {
	return &RedactingSpanExporter{inner: inner}
}

// ExportSpans redacts sensitive attribute values before forwarding to the
// wrapped exporter.
func (r *RedactingSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if r.inner == nil {
		return nil
	}
	wrapped := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		wrapped[i] = redactedReadOnlySpan{
			ReadOnlySpan: s,
			redacted:     redactAttributes(s.Attributes()),
		}
	}
	return r.inner.ExportSpans(ctx, wrapped)
}

// Shutdown forwards to the wrapped exporter.
func (r *RedactingSpanExporter) Shutdown(ctx context.Context) error {
	if r.inner == nil {
		return nil
	}
	return r.inner.Shutdown(ctx)
}
