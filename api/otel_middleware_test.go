package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOTelSpanEnrichment_AddsUserID(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Simulate: start a span (normally otelgin does this), then enrich
	r.Use(func(c *gin.Context) {
		tracer := otel.Tracer("test")
		ctx, span := tracer.Start(c.Request.Context(), "test-request")
		defer span.End()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.Use(OTelSpanEnrichmentMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Set("userID", "alice")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "tmi.user.id" && attr.Value.AsString() == "alice" {
			found = true
		}
	}
	assert.True(t, found, "span should have tmi.user.id=alice attribute")
}

func TestOTelSpanEnrichment_AddsAllContextValues(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.Use(func(c *gin.Context) {
		tracer := otel.Tracer("test")
		ctx, span := tracer.Start(c.Request.Context(), "test-request")
		defer span.End()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.Use(OTelSpanEnrichmentMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Set("userID", "bob")
		c.Set("threatModelID", "tm-uuid-123")
		c.Set("diagramID", "diag-uuid-456")
		c.Set("requestID", "req-uuid-789")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	attrs := make(map[attribute.Key]string)
	for _, attr := range spans[0].Attributes {
		attrs[attr.Key] = attr.Value.AsString()
	}

	assert.Equal(t, "bob", attrs["tmi.user.id"], "span should have tmi.user.id=bob")
	assert.Equal(t, "tm-uuid-123", attrs["tmi.threat_model.id"], "span should have tmi.threat_model.id")
	assert.Equal(t, "diag-uuid-456", attrs["tmi.diagram.id"], "span should have tmi.diagram.id")
	assert.Equal(t, "req-uuid-789", attrs["tmi.request.id"], "span should have tmi.request.id")
}

func TestOTelSpanEnrichment_NoSpan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// No span started — middleware should not panic
	r.Use(OTelSpanEnrichmentMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Set("userID", "alice")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		r.ServeHTTP(w, req)
	})
	assert.Equal(t, http.StatusOK, w.Code)
}
