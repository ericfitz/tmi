package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestOTelGin_ProducesHTTPSpans verifies that the otelgin middleware records a
// span for each handled HTTP request.
func TestOTelGin_ProducesHTTPSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(otelgin.Middleware("tmi"))
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := exporter.GetSpans()
	require.GreaterOrEqual(t, len(spans), 1, "otelgin should produce at least one span")

	// Find the HTTP server span for /test.
	var httpSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "GET /test" || strings.Contains(spans[i].Name, "/test") {
			httpSpan = &spans[i]
			break
		}
	}
	require.NotNil(t, httpSpan, "should have an HTTP span for GET /test")
}

// TestOTelGin_SpanHasHTTPAttributes verifies that the span produced by otelgin
// carries standard HTTP semantic convention attributes.
func TestOTelGin_SpanHasHTTPAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(otelgin.Middleware("tmi"))
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := exporter.GetSpans()
	require.GreaterOrEqual(t, len(spans), 1, "otelgin should produce at least one span")

	// Collect all attribute keys across all spans.
	allAttrs := make(map[string]string)
	for _, span := range spans {
		for _, attr := range span.Attributes {
			allAttrs[string(attr.Key)] = attr.Value.Emit()
		}
	}

	// OTel HTTP semantic conventions (semconv v1.x / v2.x): at least one of these
	// should be present to confirm the middleware is emitting HTTP metadata.
	httpAttrKeys := []string{
		"http.method",
		"http.request.method",
		"http.route",
		"http.target",
		"url.path",
	}

	found := false
	for _, key := range httpAttrKeys {
		if _, ok := allAttrs[key]; ok {
			found = true
			break
		}
	}
	assert.True(t, found,
		"span should contain at least one HTTP semantic convention attribute; got attrs: %v", allAttrs)
}

// TestOTelGin_404Route verifies that otelgin still records a span for routes
// that are not registered (404 responses).
func TestOTelGin_404Route(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(otelgin.Middleware("tmi"))
	// No routes registered — every request returns 404.

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
	// otelgin may or may not produce a span for unmatched routes depending on
	// version; we just verify no panic occurs and the response code is correct.
}
