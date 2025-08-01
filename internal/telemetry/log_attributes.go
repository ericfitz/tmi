package telemetry

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// AttributeExtractor handles extraction and enhancement of log attributes
type AttributeExtractor struct {
	enableUserContext    bool
	enableRequestContext bool
	enableSystemContext  bool
	enablePerformance    bool
	enableStackTrace     bool
}

// NewAttributeExtractor creates a new attribute extractor
func NewAttributeExtractor(enableUserContext, enableRequestContext, enableSystemContext, enablePerformance, enableStackTrace bool) *AttributeExtractor {
	return &AttributeExtractor{
		enableUserContext:    enableUserContext,
		enableRequestContext: enableRequestContext,
		enableSystemContext:  enableSystemContext,
		enablePerformance:    enablePerformance,
		enableStackTrace:     enableStackTrace,
	}
}

// ExtractTraceAttributes extracts OpenTelemetry trace and span attributes
func (ae *AttributeExtractor) ExtractTraceAttributes(ctx context.Context) []attribute.KeyValue {
	var attrs []attribute.KeyValue

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return attrs
	}

	spanContext := span.SpanContext()

	// Add trace correlation
	if spanContext.HasTraceID() {
		attrs = append(attrs, attribute.String("trace_id", spanContext.TraceID().String()))
	}

	if spanContext.HasSpanID() {
		attrs = append(attrs, attribute.String("span_id", spanContext.SpanID().String()))
	}

	// Add span name if available
	// Note: span name is not directly accessible, but we can add it if stored in context
	if spanName := ctx.Value("span_name"); spanName != nil {
		attrs = append(attrs, attribute.String("span_name", fmt.Sprintf("%v", spanName)))
	}

	// Add sampling decision
	if spanContext.IsSampled() {
		attrs = append(attrs, attribute.Bool("sampled", true))
	}

	return attrs
}

// ExtractUserAttributes extracts user-related attributes from context
func (ae *AttributeExtractor) ExtractUserAttributes(ctx context.Context) []attribute.KeyValue {
	if !ae.enableUserContext {
		return nil
	}

	var attrs []attribute.KeyValue

	// Extract user ID
	if userID := ctx.Value("user_id"); userID != nil {
		attrs = append(attrs, attribute.String("user_id", sanitizeUserID(fmt.Sprintf("%v", userID))))
	}

	// Extract user role
	if userRole := ctx.Value("user_role"); userRole != nil {
		attrs = append(attrs, attribute.String("user_role", fmt.Sprintf("%v", userRole)))
	}

	// Extract user type
	if userType := ctx.Value("user_type"); userType != nil {
		attrs = append(attrs, attribute.String("user_type", fmt.Sprintf("%v", userType)))
	}

	// Extract organization ID
	if orgID := ctx.Value("organization_id"); orgID != nil {
		attrs = append(attrs, attribute.String("organization_id", fmt.Sprintf("%v", orgID)))
	}

	// Extract tenant ID
	if tenantID := ctx.Value("tenant_id"); tenantID != nil {
		attrs = append(attrs, attribute.String("tenant_id", fmt.Sprintf("%v", tenantID)))
	}

	return attrs
}

// ExtractRequestAttributes extracts HTTP request attributes from context
func (ae *AttributeExtractor) ExtractRequestAttributes(ctx context.Context) []attribute.KeyValue {
	if !ae.enableRequestContext {
		return nil
	}

	var attrs []attribute.KeyValue

	// Extract request ID
	if requestID := ctx.Value("request_id"); requestID != nil {
		attrs = append(attrs, attribute.String("request_id", fmt.Sprintf("%v", requestID)))
	}

	// Extract correlation ID
	if correlationID := ctx.Value("correlation_id"); correlationID != nil {
		attrs = append(attrs, attribute.String("correlation_id", fmt.Sprintf("%v", correlationID)))
	}

	// Extract session ID
	if sessionID := ctx.Value("session_id"); sessionID != nil {
		attrs = append(attrs, attribute.String("session_id", sanitizeSessionID(fmt.Sprintf("%v", sessionID))))
	}

	// Extract HTTP method
	if method := ctx.Value("http_method"); method != nil {
		attrs = append(attrs, attribute.String("http_method", fmt.Sprintf("%v", method)))
	}

	// Extract HTTP path
	if path := ctx.Value("http_path"); path != nil {
		attrs = append(attrs, attribute.String("http_path", fmt.Sprintf("%v", path)))
	}

	// Extract HTTP user agent
	if userAgent := ctx.Value("http_user_agent"); userAgent != nil {
		attrs = append(attrs, attribute.String("http_user_agent", sanitizeUserAgent(fmt.Sprintf("%v", userAgent))))
	}

	// Extract client IP
	if clientIP := ctx.Value("client_ip"); clientIP != nil {
		attrs = append(attrs, attribute.String("client_ip", fmt.Sprintf("%v", clientIP)))
	}

	// Extract referer
	if referer := ctx.Value("http_referer"); referer != nil {
		attrs = append(attrs, attribute.String("http_referer", fmt.Sprintf("%v", referer)))
	}

	return attrs
}

// ExtractSystemAttributes extracts system-level attributes
func (ae *AttributeExtractor) ExtractSystemAttributes(ctx context.Context) []attribute.KeyValue {
	if !ae.enableSystemContext {
		return nil
	}

	var attrs []attribute.KeyValue

	// Add service information
	attrs = append(attrs,
		attribute.String("service_name", "tmi-api"),
		attribute.String("service_version", getServiceVersion()),
		attribute.String("environment", getEnvironment()),
	)

	// Add runtime information
	attrs = append(attrs,
		attribute.String("go_version", runtime.Version()),
		attribute.Int("go_routines", runtime.NumGoroutine()),
	)

	// Add process information
	if hostname := getHostname(); hostname != "" {
		attrs = append(attrs, attribute.String("hostname", hostname))
	}

	// Add deployment information
	if deploymentID := ctx.Value("deployment_id"); deploymentID != nil {
		attrs = append(attrs, attribute.String("deployment_id", fmt.Sprintf("%v", deploymentID)))
	}

	if instanceID := ctx.Value("instance_id"); instanceID != nil {
		attrs = append(attrs, attribute.String("instance_id", fmt.Sprintf("%v", instanceID)))
	}

	return attrs
}

// ExtractPerformanceAttributes extracts performance-related attributes
func (ae *AttributeExtractor) ExtractPerformanceAttributes(ctx context.Context) []attribute.KeyValue {
	if !ae.enablePerformance {
		return nil
	}

	var attrs []attribute.KeyValue

	// Extract operation start time
	if startTime := ctx.Value("operation_start_time"); startTime != nil {
		if start, ok := startTime.(time.Time); ok {
			duration := time.Since(start)
			attrs = append(attrs, attribute.Int64("operation_duration_ms", duration.Milliseconds()))
		}
	}

	// Extract query count
	if queryCount := ctx.Value("query_count"); queryCount != nil {
		attrs = append(attrs, attribute.Int("query_count", parseIntValue(queryCount)))
	}

	// Extract cache hits/misses
	if cacheHits := ctx.Value("cache_hits"); cacheHits != nil {
		attrs = append(attrs, attribute.Int("cache_hits", parseIntValue(cacheHits)))
	}

	if cacheMisses := ctx.Value("cache_misses"); cacheMisses != nil {
		attrs = append(attrs, attribute.Int("cache_misses", parseIntValue(cacheMisses)))
	}

	// Extract memory allocation
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	attrs = append(attrs,
		attribute.Int64("memory_alloc_bytes", safeUint64ToInt64(m.Alloc)),
		attribute.Int64("memory_sys_bytes", safeUint64ToInt64(m.Sys)),
	)

	return attrs
}

// ExtractErrorAttributes extracts error-related attributes and stack trace
func (ae *AttributeExtractor) ExtractErrorAttributes(ctx context.Context, err error, skipFrames int) []attribute.KeyValue {
	if err == nil {
		return nil
	}

	var attrs []attribute.KeyValue

	// Add error information
	attrs = append(attrs,
		attribute.String("error_type", getLogErrorType(err)),
		attribute.String("error_message", err.Error()),
	)

	// Add stack trace if enabled
	if ae.enableStackTrace {
		stackTrace := captureStackTrace(skipFrames + 2) // +2 to skip this function and caller
		attrs = append(attrs, attribute.String("stack_trace", stackTrace))
	}

	// Add error context if available
	if errorCode := ctx.Value("error_code"); errorCode != nil {
		attrs = append(attrs, attribute.String("error_code", fmt.Sprintf("%v", errorCode)))
	}

	if errorCategory := ctx.Value("error_category"); errorCategory != nil {
		attrs = append(attrs, attribute.String("error_category", fmt.Sprintf("%v", errorCategory)))
	}

	return attrs
}

// ExtractDatabaseAttributes extracts database operation attributes
func (ae *AttributeExtractor) ExtractDatabaseAttributes(ctx context.Context) []attribute.KeyValue {
	var attrs []attribute.KeyValue

	// Extract database operation details
	if dbOperation := ctx.Value("db_operation"); dbOperation != nil {
		attrs = append(attrs, attribute.String("db_operation", fmt.Sprintf("%v", dbOperation)))
	}

	if dbTable := ctx.Value("db_table"); dbTable != nil {
		attrs = append(attrs, attribute.String("db_table", fmt.Sprintf("%v", dbTable)))
	}

	if dbRowsAffected := ctx.Value("db_rows_affected"); dbRowsAffected != nil {
		attrs = append(attrs, attribute.Int64("db_rows_affected", parseInt64Value(dbRowsAffected)))
	}

	if dbQueryDuration := ctx.Value("db_query_duration"); dbQueryDuration != nil {
		if duration, ok := dbQueryDuration.(time.Duration); ok {
			attrs = append(attrs, attribute.Int64("db_query_duration_ms", duration.Milliseconds()))
		}
	}

	return attrs
}

// ExtractCacheAttributes extracts cache operation attributes
func (ae *AttributeExtractor) ExtractCacheAttributes(ctx context.Context) []attribute.KeyValue {
	var attrs []attribute.KeyValue

	// Extract cache operation details
	if cacheOperation := ctx.Value("cache_operation"); cacheOperation != nil {
		attrs = append(attrs, attribute.String("cache_operation", fmt.Sprintf("%v", cacheOperation)))
	}

	if cacheKey := ctx.Value("cache_key"); cacheKey != nil {
		key := fmt.Sprintf("%v", cacheKey)
		attrs = append(attrs, attribute.String("cache_key", sanitizeCacheKey(key)))
	}

	if cacheHit := ctx.Value("cache_hit"); cacheHit != nil {
		attrs = append(attrs, attribute.Bool("cache_hit", parseBoolValue(cacheHit)))
	}

	if cacheTTL := ctx.Value("cache_ttl"); cacheTTL != nil {
		if ttl, ok := cacheTTL.(time.Duration); ok {
			attrs = append(attrs, attribute.Int64("cache_ttl_seconds", int64(ttl.Seconds())))
		}
	}

	return attrs
}

// ExtractBusinessAttributes extracts business logic attributes
func (ae *AttributeExtractor) ExtractBusinessAttributes(ctx context.Context) []attribute.KeyValue {
	var attrs []attribute.KeyValue

	// Extract threat model attributes
	if threatModelID := ctx.Value("threat_model_id"); threatModelID != nil {
		attrs = append(attrs, attribute.String("threat_model_id", fmt.Sprintf("%v", threatModelID)))
	}

	if diagramID := ctx.Value("diagram_id"); diagramID != nil {
		attrs = append(attrs, attribute.String("diagram_id", fmt.Sprintf("%v", diagramID)))
	}

	if cellID := ctx.Value("cell_id"); cellID != nil {
		attrs = append(attrs, attribute.String("cell_id", fmt.Sprintf("%v", cellID)))
	}

	// Extract collaboration attributes
	if collaborationSessionID := ctx.Value("collaboration_session_id"); collaborationSessionID != nil {
		attrs = append(attrs, attribute.String("collaboration_session_id", fmt.Sprintf("%v", collaborationSessionID)))
	}

	if participantCount := ctx.Value("participant_count"); participantCount != nil {
		attrs = append(attrs, attribute.Int("participant_count", parseIntValue(participantCount)))
	}

	// Extract authorization attributes
	if resource := ctx.Value("auth_resource"); resource != nil {
		attrs = append(attrs, attribute.String("auth_resource", fmt.Sprintf("%v", resource)))
	}

	if action := ctx.Value("auth_action"); action != nil {
		attrs = append(attrs, attribute.String("auth_action", fmt.Sprintf("%v", action)))
	}

	return attrs
}

// ExtractAllAttributes extracts all available attributes from context
func (ae *AttributeExtractor) ExtractAllAttributes(ctx context.Context) []attribute.KeyValue {
	var allAttrs []attribute.KeyValue

	// Combine all attribute types
	allAttrs = append(allAttrs, ae.ExtractTraceAttributes(ctx)...)
	allAttrs = append(allAttrs, ae.ExtractUserAttributes(ctx)...)
	allAttrs = append(allAttrs, ae.ExtractRequestAttributes(ctx)...)
	allAttrs = append(allAttrs, ae.ExtractSystemAttributes(ctx)...)
	allAttrs = append(allAttrs, ae.ExtractPerformanceAttributes(ctx)...)
	allAttrs = append(allAttrs, ae.ExtractDatabaseAttributes(ctx)...)
	allAttrs = append(allAttrs, ae.ExtractCacheAttributes(ctx)...)
	allAttrs = append(allAttrs, ae.ExtractBusinessAttributes(ctx)...)

	return allAttrs
}

// EnhanceLogWithAttributes enhances a log entry with extracted attributes
func (ae *AttributeExtractor) EnhanceLogWithAttributes(ctx context.Context, baseAttrs []attribute.KeyValue) []attribute.KeyValue {
	extractedAttrs := ae.ExtractAllAttributes(ctx)

	// Combine base attributes with extracted attributes
	allAttrs := make([]attribute.KeyValue, 0, len(baseAttrs)+len(extractedAttrs))
	allAttrs = append(allAttrs, baseAttrs...)
	allAttrs = append(allAttrs, extractedAttrs...)

	return allAttrs
}

// Helper functions for context value extraction and sanitization

func parseIntValue(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

func parseInt64Value(value interface{}) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func parseBoolValue(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return false
}

func getLogErrorType(err error) string {
	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "not found"):
		return "not_found"
	case strings.Contains(errStr, "unauthorized"):
		return "unauthorized"
	case strings.Contains(errStr, "forbidden"):
		return "forbidden"
	case strings.Contains(errStr, "timeout"):
		return "timeout"
	case strings.Contains(errStr, "connection"):
		return "connection_error"
	case strings.Contains(errStr, "validation"):
		return "validation_error"
	case strings.Contains(errStr, "constraint"):
		return "constraint_violation"
	default:
		return "application_error"
	}
}

func captureStackTrace(skipFrames int) string {
	const maxFrames = 20
	pcs := make([]uintptr, maxFrames)
	n := runtime.Callers(skipFrames, pcs)

	if n == 0 {
		return ""
	}

	frames := runtime.CallersFrames(pcs[:n])
	var stackTrace strings.Builder

	for {
		frame, more := frames.Next()

		// Skip internal Go runtime frames
		if strings.HasPrefix(frame.Function, "runtime.") {
			if !more {
				break
			}
			continue
		}

		if stackTrace.Len() > 0 {
			stackTrace.WriteString("\n")
		}

		stackTrace.WriteString(fmt.Sprintf("%s:%d %s", frame.File, frame.Line, frame.Function))

		if !more {
			break
		}
	}

	return stackTrace.String()
}

func sanitizeUserAgent(userAgent string) string {
	// Remove version information but keep browser/client type
	parts := strings.Fields(userAgent)
	if len(parts) > 0 {
		return parts[0] // Keep first part only
	}
	return userAgent
}

func sanitizeCacheKey(key string) string {
	// Keep prefix and suffix, redact middle for sensitive keys
	if strings.Contains(key, "session") || strings.Contains(key, "token") {
		parts := strings.Split(key, ":")
		if len(parts) > 2 {
			return fmt.Sprintf("%s:***:%s", parts[0], parts[len(parts)-1])
		}
	}
	return key
}

func getServiceVersion() string {
	// This would typically come from build time variables
	return "1.0.0"
}

func getEnvironment() string {
	// This would typically come from environment variables
	return "development"
}

func getHostname() string {
	// This would typically come from system
	return "localhost"
}

// Global attribute extractor instance
var globalAttributeExtractor *AttributeExtractor

// InitializeAttributeExtractor initializes the global attribute extractor
func InitializeAttributeExtractor(enableUserContext, enableRequestContext, enableSystemContext, enablePerformance, enableStackTrace bool) {
	globalAttributeExtractor = NewAttributeExtractor(
		enableUserContext,
		enableRequestContext,
		enableSystemContext,
		enablePerformance,
		enableStackTrace,
	)
}

// GetAttributeExtractor returns the global attribute extractor
func GetAttributeExtractor() *AttributeExtractor {
	if globalAttributeExtractor == nil {
		// Initialize with default settings
		globalAttributeExtractor = NewAttributeExtractor(true, true, true, true, false)
	}
	return globalAttributeExtractor
}
