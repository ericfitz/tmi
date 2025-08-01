package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// HTTPTracing provides HTTP request tracing middleware
type HTTPTracing struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics instruments
	requestCounter    metric.Int64Counter
	requestDuration   metric.Float64Histogram
	requestSizeHisto  metric.Int64Histogram
	responseSizeHisto metric.Int64Histogram
	requestsInFlight  metric.Int64UpDownCounter
}

// NewHTTPTracing creates a new HTTP tracing middleware
func NewHTTPTracing(tracer trace.Tracer, meter metric.Meter) (*HTTPTracing, error) {
	h := &HTTPTracing{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Create metrics instruments
	h.requestCounter, err = meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request counter: %w", err)
	}

	h.requestDuration, err = meter.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("Duration of HTTP requests"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request duration histogram: %w", err)
	}

	h.requestSizeHisto, err = meter.Int64Histogram(
		"http_request_size_bytes",
		metric.WithDescription("Size of HTTP requests"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(0, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request size histogram: %w", err)
	}

	h.responseSizeHisto, err = meter.Int64Histogram(
		"http_response_size_bytes",
		metric.WithDescription("Size of HTTP responses"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(0, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create response size histogram: %w", err)
	}

	h.requestsInFlight, err = meter.Int64UpDownCounter(
		"http_requests_in_flight",
		metric.WithDescription("Number of HTTP requests currently being processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create requests in flight counter: %w", err)
	}

	return h, nil
}

// GinMiddleware returns a Gin middleware that traces HTTP requests
func (h *HTTPTracing) GinMiddleware() gin.HandlerFunc {
	// Use the otelgin middleware as base, then enhance it
	otelMiddleware := otelgin.Middleware("tmi-api")

	return func(c *gin.Context) {
		startTime := time.Now()

		// Increment in-flight requests
		h.requestsInFlight.Add(c.Request.Context(), 1)
		defer h.requestsInFlight.Add(c.Request.Context(), -1)

		// Get request size
		requestSize := getRequestSize(c.Request)

		// Call the OpenTelemetry Gin middleware first
		otelMiddleware(c)

		// If the OpenTelemetry middleware created a span, enhance it
		span := trace.SpanFromContext(c.Request.Context())
		if span.IsRecording() {
			h.enhanceSpan(span, c, requestSize)
		}

		// Process request
		c.Next()

		// Calculate metrics after request processing
		duration := time.Since(startTime)
		responseSize := int64(c.Writer.Size())
		statusCode := c.Writer.Status()

		// Record metrics
		h.recordMetrics(c.Request.Context(), c, duration, requestSize, responseSize, statusCode)

		// Update span with response information
		if span.IsRecording() {
			h.updateSpanWithResponse(span, c, duration, responseSize, statusCode)
		}
	}
}

// enhanceSpan adds additional attributes to the trace span
func (h *HTTPTracing) enhanceSpan(span trace.Span, c *gin.Context, requestSize int64) {
	// Add request attributes
	span.SetAttributes(
		attribute.String("http.method", c.Request.Method),
		attribute.String("http.url", c.Request.URL.String()),
		attribute.String("http.scheme", c.Request.URL.Scheme),
		attribute.String("http.host", c.Request.Host),
		attribute.String("http.route", c.FullPath()),
		attribute.String("http.user_agent", c.Request.UserAgent()),
		attribute.String("http.remote_addr", c.ClientIP()),
		attribute.Int64("http.request_content_length", requestSize),
	)

	// Add custom TMI attributes
	if userID := getUserFromContext(c); userID != "" {
		span.SetAttributes(attribute.String("tmi.user.id", userID))
	}

	if requestID := c.GetHeader("X-Request-ID"); requestID != "" {
		span.SetAttributes(attribute.String("tmi.request.id", requestID))
	}

	// Add route-specific attributes
	if route := c.Param("threat_model_id"); route != "" {
		span.SetAttributes(attribute.String("tmi.threat_model.id", route))
	}

	if route := c.Param("diagram_id"); route != "" {
		span.SetAttributes(attribute.String("tmi.diagram.id", route))
	}
}

// updateSpanWithResponse updates the span with response information
func (h *HTTPTracing) updateSpanWithResponse(span trace.Span, c *gin.Context, duration time.Duration, responseSize int64, statusCode int) {
	// Add response attributes
	span.SetAttributes(
		attribute.Int("http.status_code", statusCode),
		attribute.Int64("http.response_content_length", responseSize),
		attribute.Float64("http.duration_ms", float64(duration.Nanoseconds())/1e6),
	)

	// Set span status based on HTTP status code
	if statusCode >= 400 {
		span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))

		// Add error details for server errors
		if statusCode >= 500 {
			span.RecordError(fmt.Errorf("HTTP %d: %s", statusCode, getErrorFromContext(c)))
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}

	// Add performance classification
	if duration > 5*time.Second {
		span.SetAttributes(attribute.String("tmi.performance.class", "slow"))
	} else if duration > 1*time.Second {
		span.SetAttributes(attribute.String("tmi.performance.class", "medium"))
	} else {
		span.SetAttributes(attribute.String("tmi.performance.class", "fast"))
	}
}

// recordMetrics records HTTP metrics
func (h *HTTPTracing) recordMetrics(ctx context.Context, c *gin.Context, duration time.Duration, requestSize, responseSize int64, statusCode int) {
	// Create common attributes
	attrs := []attribute.KeyValue{
		attribute.String("method", c.Request.Method),
		attribute.String("route", c.FullPath()),
		attribute.Int("status_code", statusCode),
	}

	// Add user type if available
	if userType := getUserTypeFromContext(c); userType != "" {
		attrs = append(attrs, attribute.String("user_type", userType))
	}

	// Record metrics
	h.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	h.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	h.requestSizeHisto.Record(ctx, requestSize, metric.WithAttributes(attrs...))
	h.responseSizeHisto.Record(ctx, responseSize, metric.WithAttributes(attrs...))
}

// Enhanced tracing middleware that replaces the existing logging middleware
func (h *HTTPTracing) TracingLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// Create or get trace context
		ctx, span := h.tracer.Start(c.Request.Context(),
			fmt.Sprintf("%s %s", c.Request.Method, c.FullPath()),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		// Update request context
		c.Request = c.Request.WithContext(ctx)

		// Store tracing context in Gin context for handlers
		c.Set("trace_id", span.SpanContext().TraceID().String())
		c.Set("span_id", span.SpanContext().SpanID().String())
		c.Set("tracer", h.tracer)

		// Increment in-flight requests
		h.requestsInFlight.Add(ctx, 1)
		defer h.requestsInFlight.Add(ctx, -1)

		// Get request size
		requestSize := getRequestSize(c.Request)

		// Enhance span with request info
		h.enhanceSpan(span, c, requestSize)

		// Process request
		c.Next()

		// Calculate final metrics
		duration := time.Since(startTime)
		responseSize := int64(c.Writer.Size())
		statusCode := c.Writer.Status()

		// Record metrics
		h.recordMetrics(ctx, c, duration, requestSize, responseSize, statusCode)

		// Update span with response info
		h.updateSpanWithResponse(span, c, duration, responseSize, statusCode)
	}
}

// Helper functions

func getRequestSize(req *http.Request) int64 {
	if req.ContentLength > 0 {
		return req.ContentLength
	}
	return 0
}

func getUserFromContext(c *gin.Context) string {
	if user, exists := c.Get("userName"); exists {
		if userStr, ok := user.(string); ok {
			return userStr
		}
	}
	return ""
}

func getUserTypeFromContext(c *gin.Context) string {
	if userType, exists := c.Get("userType"); exists {
		if userTypeStr, ok := userType.(string); ok {
			return userTypeStr
		}
	}
	// Default classification based on authentication
	if getUserFromContext(c) != "" {
		return "authenticated"
	}
	return "anonymous"
}

func getErrorFromContext(c *gin.Context) string {
	if len(c.Errors) > 0 {
		return c.Errors.Last().Error()
	}
	return "Unknown error"
}

// StartSpan creates a new span for manual instrumentation
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	tracer := GetTracer()
	return tracer.Start(ctx, name, opts...)
}

// SpanFromContext returns the current span from context
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddSpanAttributes adds attributes to the current span
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// RecordSpanError records an error on the current span
func RecordSpanError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
