package telemetry

import (
	"context"
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

	mb := newMetricBuilder(meter)

	// Build metrics by category
	b.buildThreatModelMetrics(mb)
	b.buildDiagramMetrics(mb)
	b.buildCollaborationMetrics(mb)
	b.buildWebSocketMetrics(mb)
	b.buildAuthorizationMetrics(mb)
	b.buildAPIMetrics(mb)
	b.buildUserActivityMetrics(mb)
	b.buildContentMetrics(mb)
	b.buildPerformanceMetrics(mb)

	return b, mb.Error()
}

// buildThreatModelMetrics creates threat model related metrics
func (b *BusinessMetrics) buildThreatModelMetrics(mb *metricBuilder) {
	b.threatModelsTotal = mb.Int64UpDownCounter(
		"threat_models_total",
		"Total number of threat models",
		"1")
	b.threatModelOperations = mb.Int64Counter(
		"threat_model_operations_total",
		"Total threat model operations",
		"1")
	b.threatModelDuration = mb.Float64Histogram(
		"threat_model_operation_duration_seconds",
		"Duration of threat model operations",
		"s",
		[]float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60})
	b.threatModelSize = mb.Int64Histogram(
		"threat_model_size_bytes",
		"Size of threat models in bytes",
		"By",
		[]float64{1000, 10000, 100000, 1000000, 10000000})
}

// buildDiagramMetrics creates diagram related metrics
func (b *BusinessMetrics) buildDiagramMetrics(mb *metricBuilder) {
	b.diagramsTotal = mb.Int64UpDownCounter(
		"diagrams_total",
		"Total number of diagrams",
		"1")
	b.diagramOperations = mb.Int64Counter(
		"diagram_operations_total",
		"Total diagram operations",
		"1")
	b.diagramCellOperations = mb.Int64Counter(
		"diagram_cell_operations_total",
		"Total diagram cell operations",
		"1")
	b.cellModifications = mb.Int64Counter(
		"diagram_cells_modified_total",
		"Total number of diagram cell modifications",
		"1")
	b.diagramComplexity = mb.Int64Histogram(
		"diagram_complexity",
		"Complexity of diagrams measured by cell count",
		"1",
		[]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000})
}

// buildCollaborationMetrics creates collaboration related metrics
func (b *BusinessMetrics) buildCollaborationMetrics(mb *metricBuilder) {
	b.collaborationSessions = mb.Int64UpDownCounter(
		"collaboration_sessions_active",
		"Number of active collaboration sessions",
		"1")
	b.sessionDuration = mb.Float64Histogram(
		"collaboration_session_duration_seconds",
		"Duration of collaboration sessions",
		"s",
		[]float64{10, 30, 60, 300, 600, 1800, 3600, 7200})
	b.activeUsers = mb.Int64UpDownCounter(
		"active_users",
		"Number of active users",
		"1")
	b.concurrentEditors = mb.Int64UpDownCounter(
		"concurrent_editors",
		"Number of concurrent editors",
		"1")
}

// buildWebSocketMetrics creates WebSocket related metrics
func (b *BusinessMetrics) buildWebSocketMetrics(mb *metricBuilder) {
	b.websocketConnections = mb.Int64UpDownCounter(
		"websocket_connections_active",
		"Number of active WebSocket connections",
		"1")
	b.messagesSent = mb.Int64Counter(
		"websocket_messages_sent_total",
		"Total WebSocket messages sent",
		"1")
	b.messagesReceived = mb.Int64Counter(
		"websocket_messages_received_total",
		"Total WebSocket messages received",
		"1")
	b.broadcastEvents = mb.Int64Counter(
		"websocket_broadcast_events_total",
		"Total WebSocket broadcast events",
		"1")
}

// buildAuthorizationMetrics creates authorization related metrics
func (b *BusinessMetrics) buildAuthorizationMetrics(mb *metricBuilder) {
	b.authorizationChecks = mb.Int64Counter(
		"authorization_checks_total",
		"Total authorization checks",
		"1")
	b.roleBasedAccess = mb.Int64Counter(
		"role_based_access_total",
		"Total role-based access checks",
		"1")
	b.permissionDenials = mb.Int64Counter(
		"permission_denials_total",
		"Total permission denials",
		"1")
}

// buildAPIMetrics creates API usage related metrics
func (b *BusinessMetrics) buildAPIMetrics(mb *metricBuilder) {
	b.apiEndpointUsage = mb.Int64Counter(
		"api_endpoint_usage_total",
		"Total API endpoint usage",
		"1")
	b.apiResponseTimes = mb.Float64Histogram(
		"api_response_time_seconds",
		"API response times",
		"s",
		[]float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5})
	b.apiErrorRates = mb.Int64Counter(
		"api_errors_total",
		"Total API errors",
		"1")
}

// buildUserActivityMetrics creates user activity related metrics
func (b *BusinessMetrics) buildUserActivityMetrics(mb *metricBuilder) {
	b.userSessions = mb.Int64UpDownCounter(
		"user_sessions_active",
		"Number of active user sessions",
		"1")
	b.userActions = mb.Int64Counter(
		"user_actions_total",
		"Total user actions",
		"1")
	b.featureUsage = mb.Int64Counter(
		"feature_usage_total",
		"Total feature usage",
		"1")
	b.userEngagement = mb.Float64Histogram(
		"user_engagement_duration_seconds",
		"User engagement duration",
		"s",
		[]float64{30, 60, 300, 600, 1800, 3600, 7200, 14400})
}

// buildContentMetrics creates content related metrics
func (b *BusinessMetrics) buildContentMetrics(mb *metricBuilder) {
	b.documentsTotal = mb.Int64UpDownCounter(
		"documents_total",
		"Total number of documents",
		"1")
	b.sourceCodeTotal = mb.Int64UpDownCounter(
		"source_code_total",
		"Total number of source code items",
		"1")
	b.threatsTotal = mb.Int64UpDownCounter(
		"threats_total",
		"Total number of threats",
		"1")
	b.metadataOperations = mb.Int64Counter(
		"metadata_operations_total",
		"Total metadata operations",
		"1")
}

// buildPerformanceMetrics creates performance related metrics
func (b *BusinessMetrics) buildPerformanceMetrics(mb *metricBuilder) {
	b.operationLatency = mb.Float64Histogram(
		"business_operation_latency_seconds",
		"Latency of business operations",
		"s",
		[]float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10})
	b.resourceUtilization = mb.Float64Histogram(
		"resource_utilization_ratio",
		"Resource utilization ratios",
		"1",
		[]float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99})
	b.errorPatterns = mb.Int64Counter(
		"error_patterns_total",
		"Total error patterns",
		"1")
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
