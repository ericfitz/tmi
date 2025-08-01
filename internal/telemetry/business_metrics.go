package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// BusinessMetrics provides application-specific business logic metrics
type BusinessMetrics struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Threat Model metrics
	threatModelsTotal     metric.Int64UpDownCounter
	threatModelOperations metric.Int64Counter
	threatModelDuration   metric.Float64Histogram
	threatModelSize       metric.Int64Histogram

	// Diagram metrics
	diagramsTotal         metric.Int64UpDownCounter
	diagramOperations     metric.Int64Counter
	diagramCellOperations metric.Int64Counter
	cellModifications     metric.Int64Counter
	diagramComplexity     metric.Int64Histogram

	// Collaboration metrics
	collaborationSessions metric.Int64UpDownCounter
	sessionDuration       metric.Float64Histogram
	activeUsers           metric.Int64UpDownCounter
	concurrentEditors     metric.Int64UpDownCounter

	// WebSocket metrics
	websocketConnections metric.Int64UpDownCounter
	messagesSent         metric.Int64Counter
	messagesReceived     metric.Int64Counter
	broadcastEvents      metric.Int64Counter

	// Authorization metrics
	authorizationChecks metric.Int64Counter
	roleBasedAccess     metric.Int64Counter
	permissionDenials   metric.Int64Counter

	// API usage metrics
	apiEndpointUsage metric.Int64Counter
	apiResponseTimes metric.Float64Histogram
	apiErrorRates    metric.Int64Counter

	// User activity metrics
	userSessions   metric.Int64UpDownCounter
	userActions    metric.Int64Counter
	featureUsage   metric.Int64Counter
	userEngagement metric.Float64Histogram

	// Content metrics
	documentsTotal     metric.Int64UpDownCounter
	sourceCodeTotal    metric.Int64UpDownCounter
	threatsTotal       metric.Int64UpDownCounter
	metadataOperations metric.Int64Counter

	// Performance metrics
	operationLatency    metric.Float64Histogram
	resourceUtilization metric.Float64Histogram
	errorPatterns       metric.Int64Counter
}

// NewBusinessMetrics creates a new business metrics instance
func NewBusinessMetrics(tracer trace.Tracer, meter metric.Meter) (*BusinessMetrics, error) {
	b := &BusinessMetrics{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Threat Model metrics
	b.threatModelsTotal, err = meter.Int64UpDownCounter(
		"threat_models_total",
		metric.WithDescription("Total number of threat models"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create threat models total counter: %w", err)
	}

	b.threatModelOperations, err = meter.Int64Counter(
		"threat_model_operations_total",
		metric.WithDescription("Total threat model operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create threat model operations counter: %w", err)
	}

	b.threatModelDuration, err = meter.Float64Histogram(
		"threat_model_operation_duration_seconds",
		metric.WithDescription("Duration of threat model operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create threat model duration histogram: %w", err)
	}

	b.threatModelSize, err = meter.Int64Histogram(
		"threat_model_size_bytes",
		metric.WithDescription("Size of threat models in bytes"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(1000, 10000, 100000, 1000000, 10000000),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create threat model size histogram: %w", err)
	}

	// Diagram metrics
	b.diagramsTotal, err = meter.Int64UpDownCounter(
		"diagrams_total",
		metric.WithDescription("Total number of diagrams"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create diagrams total counter: %w", err)
	}

	b.diagramOperations, err = meter.Int64Counter(
		"diagram_operations_total",
		metric.WithDescription("Total diagram operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create diagram operations counter: %w", err)
	}

	b.diagramCellOperations, err = meter.Int64Counter(
		"diagram_cell_operations_total",
		metric.WithDescription("Total diagram cell operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create diagram cell operations counter: %w", err)
	}

	b.cellModifications, err = meter.Int64Counter(
		"diagram_cells_modified_total",
		metric.WithDescription("Total number of diagram cell modifications"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cell modifications counter: %w", err)
	}

	b.diagramComplexity, err = meter.Int64Histogram(
		"diagram_complexity",
		metric.WithDescription("Complexity of diagrams measured by cell count"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create diagram complexity histogram: %w", err)
	}

	// Collaboration metrics
	b.collaborationSessions, err = meter.Int64UpDownCounter(
		"collaboration_sessions_active",
		metric.WithDescription("Number of active collaboration sessions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create collaboration sessions counter: %w", err)
	}

	b.sessionDuration, err = meter.Float64Histogram(
		"collaboration_session_duration_seconds",
		metric.WithDescription("Duration of collaboration sessions"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(10, 30, 60, 300, 600, 1800, 3600, 7200),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session duration histogram: %w", err)
	}

	b.activeUsers, err = meter.Int64UpDownCounter(
		"active_users",
		metric.WithDescription("Number of active users"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create active users counter: %w", err)
	}

	b.concurrentEditors, err = meter.Int64UpDownCounter(
		"concurrent_editors",
		metric.WithDescription("Number of concurrent editors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create concurrent editors counter: %w", err)
	}

	// WebSocket metrics
	b.websocketConnections, err = meter.Int64UpDownCounter(
		"websocket_connections_active",
		metric.WithDescription("Number of active WebSocket connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create websocket connections counter: %w", err)
	}

	b.messagesSent, err = meter.Int64Counter(
		"websocket_messages_sent_total",
		metric.WithDescription("Total WebSocket messages sent"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create messages sent counter: %w", err)
	}

	b.messagesReceived, err = meter.Int64Counter(
		"websocket_messages_received_total",
		metric.WithDescription("Total WebSocket messages received"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create messages received counter: %w", err)
	}

	b.broadcastEvents, err = meter.Int64Counter(
		"websocket_broadcast_events_total",
		metric.WithDescription("Total WebSocket broadcast events"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create broadcast events counter: %w", err)
	}

	// Authorization metrics
	b.authorizationChecks, err = meter.Int64Counter(
		"authorization_checks_total",
		metric.WithDescription("Total authorization checks"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create authorization checks counter: %w", err)
	}

	b.roleBasedAccess, err = meter.Int64Counter(
		"role_based_access_total",
		metric.WithDescription("Total role-based access checks"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create role based access counter: %w", err)
	}

	b.permissionDenials, err = meter.Int64Counter(
		"permission_denials_total",
		metric.WithDescription("Total permission denials"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create permission denials counter: %w", err)
	}

	// API usage metrics
	b.apiEndpointUsage, err = meter.Int64Counter(
		"api_endpoint_usage_total",
		metric.WithDescription("Total API endpoint usage"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create API endpoint usage counter: %w", err)
	}

	b.apiResponseTimes, err = meter.Float64Histogram(
		"api_response_time_seconds",
		metric.WithDescription("API response times"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create API response times histogram: %w", err)
	}

	b.apiErrorRates, err = meter.Int64Counter(
		"api_errors_total",
		metric.WithDescription("Total API errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create API errors counter: %w", err)
	}

	// User activity metrics
	b.userSessions, err = meter.Int64UpDownCounter(
		"user_sessions_active",
		metric.WithDescription("Number of active user sessions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user sessions counter: %w", err)
	}

	b.userActions, err = meter.Int64Counter(
		"user_actions_total",
		metric.WithDescription("Total user actions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user actions counter: %w", err)
	}

	b.featureUsage, err = meter.Int64Counter(
		"feature_usage_total",
		metric.WithDescription("Total feature usage"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create feature usage counter: %w", err)
	}

	b.userEngagement, err = meter.Float64Histogram(
		"user_engagement_duration_seconds",
		metric.WithDescription("User engagement duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(30, 60, 300, 600, 1800, 3600, 7200, 14400),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user engagement histogram: %w", err)
	}

	// Content metrics
	b.documentsTotal, err = meter.Int64UpDownCounter(
		"documents_total",
		metric.WithDescription("Total number of documents"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create documents total counter: %w", err)
	}

	b.sourceCodeTotal, err = meter.Int64UpDownCounter(
		"source_code_total",
		metric.WithDescription("Total number of source code items"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create source code total counter: %w", err)
	}

	b.threatsTotal, err = meter.Int64UpDownCounter(
		"threats_total",
		metric.WithDescription("Total number of threats"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create threats total counter: %w", err)
	}

	b.metadataOperations, err = meter.Int64Counter(
		"metadata_operations_total",
		metric.WithDescription("Total metadata operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata operations counter: %w", err)
	}

	// Performance metrics
	b.operationLatency, err = meter.Float64Histogram(
		"business_operation_latency_seconds",
		metric.WithDescription("Latency of business operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create operation latency histogram: %w", err)
	}

	b.resourceUtilization, err = meter.Float64Histogram(
		"resource_utilization_ratio",
		metric.WithDescription("Resource utilization ratios"),
		metric.WithUnit("1"),
		metric.WithExplicitBucketBoundaries(0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource utilization histogram: %w", err)
	}

	b.errorPatterns, err = meter.Int64Counter(
		"error_patterns_total",
		metric.WithDescription("Total error patterns"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create error patterns counter: %w", err)
	}

	return b, nil
}

// RecordThreatModelOperation records threat model business operations
func (b *BusinessMetrics) RecordThreatModelOperation(ctx context.Context, operation string, duration time.Duration, size int64, userRole string, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("user_role", userRole),
		attribute.String("status", status),
	}

	b.threatModelOperations.Add(ctx, 1, metric.WithAttributes(attrs...))
	b.threatModelDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	if size > 0 {
		b.threatModelSize.Record(ctx, size, metric.WithAttributes(
			attribute.String("operation", operation),
		))
	}

	if operation == "create" && err == nil {
		b.threatModelsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
	} else if operation == "delete" && err == nil {
		b.threatModelsTotal.Add(ctx, -1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
	}

	b.operationLatency.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("operation_type", "threat_model"),
		attribute.String("operation", operation),
	))
}

// RecordDiagramOperation records diagram business operations
func (b *BusinessMetrics) RecordDiagramOperation(ctx context.Context, operation string, diagramID string, cellCount int, userRole string, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("user_role", userRole),
		attribute.String("status", status),
	}

	b.diagramOperations.Add(ctx, 1, metric.WithAttributes(attrs...))

	if cellCount > 0 {
		b.diagramComplexity.Record(ctx, int64(cellCount), metric.WithAttributes(
			attribute.String("operation", operation),
		))
	}

	if operation == "create" && err == nil {
		b.diagramsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
	} else if operation == "delete" && err == nil {
		b.diagramsTotal.Add(ctx, -1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
	}
}

// RecordCellOperation records diagram cell operations
func (b *BusinessMetrics) RecordCellOperation(ctx context.Context, operation string, cellType string, diagramID string, userRole string, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("cell_type", cellType),
		attribute.String("user_role", userRole),
		attribute.String("status", status),
	}

	b.diagramCellOperations.Add(ctx, 1, metric.WithAttributes(attrs...))

	if operation == "modify" || operation == "update" {
		b.cellModifications.Add(ctx, 1, metric.WithAttributes(
			attribute.String("cell_type", cellType),
			attribute.String("user_role", userRole),
		))
	}
}

// RecordCollaborationSession records collaboration session metrics
func (b *BusinessMetrics) RecordCollaborationSession(ctx context.Context, sessionID string, participants int, started bool) {
	if started {
		b.collaborationSessions.Add(ctx, 1, metric.WithAttributes(
			attribute.Int("initial_participants", participants),
		))
	} else {
		b.collaborationSessions.Add(ctx, -1)
	}
}

// RecordCollaborationSessionEnd records the end of a collaboration session
func (b *BusinessMetrics) RecordCollaborationSessionEnd(ctx context.Context, duration time.Duration, totalParticipants int, messagesExchanged int) {
	b.sessionDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.Int("total_participants", totalParticipants),
		attribute.String("duration_category", getSessionDurationCategory(duration)),
	))
}

// RecordUserActivity records user activity metrics
func (b *BusinessMetrics) RecordUserActivity(ctx context.Context, userID, action, feature, userRole string) {
	b.userActions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("action", action),
		attribute.String("user_role", userRole),
	))

	b.featureUsage.Add(ctx, 1, metric.WithAttributes(
		attribute.String("feature", feature),
		attribute.String("user_role", userRole),
		attribute.String("action", action),
	))
}

// RecordUserSession records user session events
func (b *BusinessMetrics) RecordUserSession(ctx context.Context, userID, userRole string, started bool, duration time.Duration) {
	if started {
		b.userSessions.Add(ctx, 1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
		b.activeUsers.Add(ctx, 1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
	} else {
		b.userSessions.Add(ctx, -1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
		b.activeUsers.Add(ctx, -1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))

		if duration > 0 {
			b.userEngagement.Record(ctx, duration.Seconds(), metric.WithAttributes(
				attribute.String("user_role", userRole),
				attribute.String("engagement_level", getEngagementLevel(duration)),
			))
		}
	}
}

// RecordWebSocketActivity records WebSocket-related business metrics
func (b *BusinessMetrics) RecordWebSocketActivity(ctx context.Context, activity string, diagramID string, messageType string, userRole string) {
	switch activity {
	case "connect":
		b.websocketConnections.Add(ctx, 1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
	case "disconnect":
		b.websocketConnections.Add(ctx, -1, metric.WithAttributes(
			attribute.String("user_role", userRole),
		))
	case "message_sent":
		b.messagesSent.Add(ctx, 1, metric.WithAttributes(
			attribute.String("message_type", messageType),
			attribute.String("user_role", userRole),
		))
	case "message_received":
		b.messagesReceived.Add(ctx, 1, metric.WithAttributes(
			attribute.String("message_type", messageType),
			attribute.String("user_role", userRole),
		))
	case "broadcast":
		b.broadcastEvents.Add(ctx, 1, metric.WithAttributes(
			attribute.String("message_type", messageType),
		))
	}
}

// RecordAuthorizationCheck records authorization and access control metrics
func (b *BusinessMetrics) RecordAuthorizationCheck(ctx context.Context, resource, action, userRole string, allowed bool) {
	b.authorizationChecks.Add(ctx, 1, metric.WithAttributes(
		attribute.String("resource", resource),
		attribute.String("action", action),
		attribute.String("user_role", userRole),
		attribute.Bool("allowed", allowed),
	))

	b.roleBasedAccess.Add(ctx, 1, metric.WithAttributes(
		attribute.String("user_role", userRole),
		attribute.String("resource_type", getResourceType(resource)),
		attribute.Bool("granted", allowed),
	))

	if !allowed {
		b.permissionDenials.Add(ctx, 1, metric.WithAttributes(
			attribute.String("resource", resource),
			attribute.String("action", action),
			attribute.String("user_role", userRole),
			attribute.String("denial_reason", "insufficient_privileges"),
		))
	}
}

// RecordAPIUsage records API endpoint usage metrics
func (b *BusinessMetrics) RecordAPIUsage(ctx context.Context, endpoint, method string, responseTime time.Duration, statusCode int, userRole string) {
	b.apiEndpointUsage.Add(ctx, 1, metric.WithAttributes(
		attribute.String("endpoint", endpoint),
		attribute.String("method", method),
		attribute.String("user_role", userRole),
		attribute.Int("status_code", statusCode),
	))

	b.apiResponseTimes.Record(ctx, responseTime.Seconds(), metric.WithAttributes(
		attribute.String("endpoint", endpoint),
		attribute.String("method", method),
	))

	if statusCode >= 400 {
		b.apiErrorRates.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", endpoint),
			attribute.String("method", method),
			attribute.String("error_type", getAPIErrorType(statusCode)),
		))
	}
}

// RecordContentOperation records operations on content types
func (b *BusinessMetrics) RecordContentOperation(ctx context.Context, contentType, operation string, userRole string, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	b.metadataOperations.Add(ctx, 1, metric.WithAttributes(
		attribute.String("content_type", contentType),
		attribute.String("operation", operation),
		attribute.String("user_role", userRole),
		attribute.String("status", status),
	))

	delta := int64(0)
	if operation == "create" && err == nil {
		delta = 1
	} else if operation == "delete" && err == nil {
		delta = -1
	}

	if delta != 0 {
		switch contentType {
		case "document":
			b.documentsTotal.Add(ctx, delta, metric.WithAttributes(
				attribute.String("user_role", userRole),
			))
		case "source_code":
			b.sourceCodeTotal.Add(ctx, delta, metric.WithAttributes(
				attribute.String("user_role", userRole),
			))
		case "threat":
			b.threatsTotal.Add(ctx, delta, metric.WithAttributes(
				attribute.String("user_role", userRole),
			))
		}
	}
}

// RecordResourceUtilization records resource utilization metrics
func (b *BusinessMetrics) RecordResourceUtilization(ctx context.Context, resourceType string, utilizationRatio float64) {
	b.resourceUtilization.Record(ctx, utilizationRatio, metric.WithAttributes(
		attribute.String("resource_type", resourceType),
		attribute.String("utilization_level", getUtilizationLevel(utilizationRatio)),
	))
}

// RecordErrorPattern records application error patterns
func (b *BusinessMetrics) RecordErrorPattern(ctx context.Context, errorType, component, userRole string) {
	b.errorPatterns.Add(ctx, 1, metric.WithAttributes(
		attribute.String("error_type", errorType),
		attribute.String("component", component),
		attribute.String("user_role", userRole),
	))
}

// Helper functions

func getSessionDurationCategory(duration time.Duration) string {
	minutes := duration.Minutes()
	switch {
	case minutes < 1:
		return "very_short"
	case minutes < 5:
		return "short"
	case minutes < 30:
		return "medium"
	case minutes < 120:
		return "long"
	default:
		return "very_long"
	}
}

func getEngagementLevel(duration time.Duration) string {
	minutes := duration.Minutes()
	switch {
	case minutes < 2:
		return "low"
	case minutes < 10:
		return "medium"
	case minutes < 60:
		return "high"
	default:
		return "very_high"
	}
}

func getResourceType(resource string) string {
	parts := strings.Split(resource, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return "unknown"
}

func getAPIErrorType(statusCode int) string {
	switch {
	case statusCode >= 400 && statusCode < 500:
		return "client_error"
	case statusCode >= 500:
		return "server_error"
	default:
		return "unknown_error"
	}
}

func getUtilizationLevel(ratio float64) string {
	switch {
	case ratio < 0.3:
		return "low"
	case ratio < 0.7:
		return "medium"
	case ratio < 0.9:
		return "high"
	default:
		return "critical"
	}
}

// GetBusinessMetrics returns a configured business metrics instance
func GetBusinessMetrics() *BusinessMetrics {
	service := GetService()
	if service == nil {
		return nil
	}

	businessMetrics, _ := NewBusinessMetrics(service.GetTracer(), service.GetMeter())
	return businessMetrics
}
