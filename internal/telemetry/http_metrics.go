package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMetrics provides comprehensive HTTP request metrics
type HTTPMetrics struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Core HTTP metrics
	requestDuration  metric.Float64Histogram
	requestCounter   metric.Int64Counter
	requestSize      metric.Int64Histogram
	responseSize     metric.Int64Histogram
	requestsInFlight metric.Int64UpDownCounter

	// Advanced HTTP metrics
	requestsByEndpoint metric.Int64Counter
	errorsByType       metric.Int64Counter
	slowRequests       metric.Int64Counter
	rateLimitCounter   metric.Int64Counter
	authenticationRate metric.Int64Counter

	// Performance metrics
	queueTime        metric.Float64Histogram
	firstByteTime    metric.Float64Histogram
	totalRequestTime metric.Float64Histogram

	// User-specific metrics
	requestsByUserType metric.Int64Counter
	requestsByRole     metric.Int64Counter
}

// NewHTTPMetrics creates a new HTTP metrics instance
func NewHTTPMetrics(tracer trace.Tracer, meter metric.Meter) (*HTTPMetrics, error) {
	h := &HTTPMetrics{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Core HTTP metrics
	h.requestDuration, err = meter.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("Duration of HTTP requests"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request duration histogram: %w", err)
	}

	h.requestCounter, err = meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request counter: %w", err)
	}

	h.requestSize, err = meter.Int64Histogram(
		"http_request_size_bytes",
		metric.WithDescription("Size of HTTP request payloads"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(100, 1000, 10000, 100000, 1000000, 10000000),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request size histogram: %w", err)
	}

	h.responseSize, err = meter.Int64Histogram(
		"http_response_size_bytes",
		metric.WithDescription("Size of HTTP response payloads"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(100, 1000, 10000, 100000, 1000000, 10000000),
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

	// Advanced HTTP metrics
	h.requestsByEndpoint, err = meter.Int64Counter(
		"http_requests_by_endpoint_total",
		metric.WithDescription("Total HTTP requests by endpoint"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create requests by endpoint counter: %w", err)
	}

	h.errorsByType, err = meter.Int64Counter(
		"http_errors_by_type_total",
		metric.WithDescription("Total HTTP errors by type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create errors by type counter: %w", err)
	}

	h.slowRequests, err = meter.Int64Counter(
		"http_slow_requests_total",
		metric.WithDescription("Total number of slow HTTP requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create slow requests counter: %w", err)
	}

	h.rateLimitCounter, err = meter.Int64Counter(
		"http_rate_limit_hits_total",
		metric.WithDescription("Total number of rate limit hits"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create rate limit counter: %w", err)
	}

	h.authenticationRate, err = meter.Int64Counter(
		"http_authentication_attempts_total",
		metric.WithDescription("Total number of authentication attempts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create authentication rate counter: %w", err)
	}

	// Performance metrics
	h.queueTime, err = meter.Float64Histogram(
		"http_request_queue_time_seconds",
		metric.WithDescription("Time requests spend in queue before processing"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create queue time histogram: %w", err)
	}

	h.firstByteTime, err = meter.Float64Histogram(
		"http_first_byte_time_seconds",
		metric.WithDescription("Time to first byte for HTTP responses"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create first byte time histogram: %w", err)
	}

	h.totalRequestTime, err = meter.Float64Histogram(
		"http_total_request_time_seconds",
		metric.WithDescription("Total time for HTTP request processing including queuing"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create total request time histogram: %w", err)
	}

	// User-specific metrics
	h.requestsByUserType, err = meter.Int64Counter(
		"http_requests_by_user_type_total",
		metric.WithDescription("Total HTTP requests by user type"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create requests by user type counter: %w", err)
	}

	h.requestsByRole, err = meter.Int64Counter(
		"http_requests_by_role_total",
		metric.WithDescription("Total HTTP requests by user role"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create requests by role counter: %w", err)
	}

	return h, nil
}

// HTTPRequestMetrics holds timing and context information for a request
type HTTPRequestMetrics struct {
	StartTime     time.Time
	QueueTime     time.Duration
	FirstByteTime time.Time
	Method        string
	Route         string
	UserAgent     string
	UserID        string
	UserRole      string
	UserType      string
	RequestSize   int64
}

// RecordHTTPRequest records comprehensive metrics for an HTTP request
func (h *HTTPMetrics) RecordHTTPRequest(c *gin.Context, requestMetrics *HTTPRequestMetrics, err error) {
	duration := time.Since(requestMetrics.StartTime)
	statusCode := c.Writer.Status()
	responseSize := int64(c.Writer.Size())

	// Get context for metrics recording
	ctx := c.Request.Context()

	// Base attributes
	baseAttrs := []attribute.KeyValue{
		attribute.String("method", requestMetrics.Method),
		attribute.String("route", normalizeRoute(requestMetrics.Route)),
		attribute.String("status_code", strconv.Itoa(statusCode)),
		attribute.String("status_class", getStatusClass(statusCode)),
	}

	// Add user context if available
	if requestMetrics.UserType != "" {
		baseAttrs = append(baseAttrs, attribute.String("user_type", requestMetrics.UserType))
	}
	if requestMetrics.UserRole != "" {
		baseAttrs = append(baseAttrs, attribute.String("user_role", requestMetrics.UserRole))
	}

	// Record core metrics
	h.requestCounter.Add(ctx, 1, metric.WithAttributes(baseAttrs...))
	h.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(baseAttrs...))

	// Record request/response sizes
	if requestMetrics.RequestSize > 0 {
		h.requestSize.Record(ctx, requestMetrics.RequestSize, metric.WithAttributes(
			attribute.String("method", requestMetrics.Method),
			attribute.String("route", normalizeRoute(requestMetrics.Route)),
		))
	}

	if responseSize > 0 {
		h.responseSize.Record(ctx, responseSize, metric.WithAttributes(
			attribute.String("method", requestMetrics.Method),
			attribute.String("route", normalizeRoute(requestMetrics.Route)),
			attribute.String("status_code", strconv.Itoa(statusCode)),
		))
	}

	// Record endpoint-specific metrics
	h.requestsByEndpoint.Add(ctx, 1, metric.WithAttributes(
		attribute.String("endpoint", fmt.Sprintf("%s %s", requestMetrics.Method, normalizeRoute(requestMetrics.Route))),
		attribute.String("status_code", strconv.Itoa(statusCode)),
	))

	// Record error metrics
	if statusCode >= 400 {
		errorType := getErrorType(statusCode)
		h.errorsByType.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", errorType),
			attribute.String("method", requestMetrics.Method),
			attribute.String("route", normalizeRoute(requestMetrics.Route)),
			attribute.String("status_code", strconv.Itoa(statusCode)),
		))
	}

	// Record slow request metrics (>1 second)
	if duration > time.Second {
		h.slowRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("method", requestMetrics.Method),
			attribute.String("route", normalizeRoute(requestMetrics.Route)),
			attribute.String("duration_bucket", getDurationBucket(duration)),
		))
	}

	// Record performance timing metrics
	if requestMetrics.QueueTime > 0 {
		h.queueTime.Record(ctx, requestMetrics.QueueTime.Seconds(), metric.WithAttributes(
			attribute.String("route", normalizeRoute(requestMetrics.Route)),
		))
	}

	if !requestMetrics.FirstByteTime.IsZero() {
		firstByteTime := requestMetrics.FirstByteTime.Sub(requestMetrics.StartTime)
		h.firstByteTime.Record(ctx, firstByteTime.Seconds(), metric.WithAttributes(
			attribute.String("method", requestMetrics.Method),
			attribute.String("route", normalizeRoute(requestMetrics.Route)),
		))
	}

	totalTime := duration
	if requestMetrics.QueueTime > 0 {
		totalTime += requestMetrics.QueueTime
	}
	h.totalRequestTime.Record(ctx, totalTime.Seconds(), metric.WithAttributes(baseAttrs...))

	// Record user-specific metrics
	if requestMetrics.UserType != "" {
		h.requestsByUserType.Add(ctx, 1, metric.WithAttributes(
			attribute.String("user_type", requestMetrics.UserType),
			attribute.String("method", requestMetrics.Method),
			attribute.String("status_class", getStatusClass(statusCode)),
		))
	}

	if requestMetrics.UserRole != "" {
		h.requestsByRole.Add(ctx, 1, metric.WithAttributes(
			attribute.String("user_role", requestMetrics.UserRole),
			attribute.String("method", requestMetrics.Method),
			attribute.String("status_class", getStatusClass(statusCode)),
		))
	}
}

// RecordRequestInFlight increments/decrements the in-flight request counter
func (h *HTTPMetrics) RecordRequestInFlight(ctx context.Context, route string, delta int64) {
	h.requestsInFlight.Add(ctx, delta, metric.WithAttributes(
		attribute.String("route", normalizeRoute(route)),
	))
}

// RecordRateLimit records rate limiting events
func (h *HTTPMetrics) RecordRateLimit(ctx context.Context, limitType, userID string) {
	h.rateLimitCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("limit_type", limitType),
		attribute.String("user_id", sanitizeUserID(userID)),
	))
}

// RecordAuthentication records authentication attempts
func (h *HTTPMetrics) RecordAuthentication(ctx context.Context, method, result string) {
	h.authenticationRate.Add(ctx, 1, metric.WithAttributes(
		attribute.String("auth_method", method),
		attribute.String("result", result),
	))
}

// CreateRequestMetrics creates a new HTTPRequestMetrics instance
func (h *HTTPMetrics) CreateRequestMetrics(c *gin.Context) *HTTPRequestMetrics {
	return &HTTPRequestMetrics{
		StartTime:   time.Now(),
		Method:      c.Request.Method,
		Route:       c.FullPath(),
		UserAgent:   c.GetHeader("User-Agent"),
		RequestSize: c.Request.ContentLength,
	}
}

// SetUserContext sets user-specific context for request metrics
func (rm *HTTPRequestMetrics) SetUserContext(userID, userRole, userType string) {
	rm.UserID = userID
	rm.UserRole = userRole
	rm.UserType = userType
}

// MarkFirstByte marks the time when the first byte of response is sent
func (rm *HTTPRequestMetrics) MarkFirstByte() {
	rm.FirstByteTime = time.Now()
}

// SetQueueTime sets the time the request spent in queue
func (rm *HTTPRequestMetrics) SetQueueTime(queueTime time.Duration) {
	rm.QueueTime = queueTime
}

// Helper functions

func normalizeRoute(route string) string {
	if route == "" {
		return "unknown"
	}

	// Remove query parameters and fragments
	if idx := strings.Index(route, "?"); idx != -1 {
		route = route[:idx]
	}
	if idx := strings.Index(route, "#"); idx != -1 {
		route = route[:idx]
	}

	// Normalize path parameters
	route = strings.ReplaceAll(route, ":id", ":id")
	route = strings.ReplaceAll(route, ":uuid", ":id")

	return route
}

func getStatusClass(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "2xx"
	case statusCode >= 300 && statusCode < 400:
		return "3xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	case statusCode >= 500:
		return "5xx"
	default:
		return "unknown"
	}
}

func getErrorType(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusRequestTimeout:
		return "timeout"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusInternalServerError:
		return "internal_error"
	case http.StatusBadGateway:
		return "bad_gateway"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	case http.StatusGatewayTimeout:
		return "gateway_timeout"
	default:
		if statusCode >= 400 && statusCode < 500 {
			return "client_error"
		} else if statusCode >= 500 {
			return "server_error"
		}
		return "unknown_error"
	}
}

func getDurationBucket(duration time.Duration) string {
	seconds := duration.Seconds()
	switch {
	case seconds <= 1:
		return "1s"
	case seconds <= 2:
		return "2s"
	case seconds <= 5:
		return "5s"
	case seconds <= 10:
		return "10s"
	case seconds <= 30:
		return "30s"
	default:
		return "30s+"
	}
}

// GetHTTPMetrics returns a configured HTTP metrics instance
func GetHTTPMetrics() *HTTPMetrics {
	service := GetService()
	if service == nil {
		return nil
	}

	httpMetrics, _ := NewHTTPMetrics(service.GetTracer(), service.GetMeter())
	return httpMetrics
}
