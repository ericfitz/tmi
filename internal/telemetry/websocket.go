package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// WebSocketTracing provides WebSocket collaboration tracing
type WebSocketTracing struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics instruments
	connectionCounter     metric.Int64UpDownCounter
	connectionDuration    metric.Float64Histogram
	messageCounter        metric.Int64Counter
	messageDuration       metric.Float64Histogram
	messageSize           metric.Int64Histogram
	collaborationSessions metric.Int64UpDownCounter
	sessionDuration       metric.Float64Histogram
	broadcastCounter      metric.Int64Counter
	broadcastDuration     metric.Float64Histogram
	errorCounter          metric.Int64Counter
}

// NewWebSocketTracing creates a new WebSocket tracing instance
func NewWebSocketTracing(tracer trace.Tracer, meter metric.Meter) (*WebSocketTracing, error) {
	w := &WebSocketTracing{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Create metrics instruments
	w.connectionCounter, err = meter.Int64UpDownCounter(
		"websocket_connections_active",
		metric.WithDescription("Number of active WebSocket connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection counter: %w", err)
	}

	w.connectionDuration, err = meter.Float64Histogram(
		"websocket_connection_duration_seconds",
		metric.WithDescription("Duration of WebSocket connections"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 30, 60, 300, 600, 1800, 3600),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection duration histogram: %w", err)
	}

	w.messageCounter, err = meter.Int64Counter(
		"websocket_messages_total",
		metric.WithDescription("Total number of WebSocket messages"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create message counter: %w", err)
	}

	w.messageDuration, err = meter.Float64Histogram(
		"websocket_message_duration_seconds",
		metric.WithDescription("Duration of WebSocket message processing"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create message duration histogram: %w", err)
	}

	w.messageSize, err = meter.Int64Histogram(
		"websocket_message_size_bytes",
		metric.WithDescription("Size of WebSocket messages in bytes"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(100, 500, 1000, 5000, 10000, 50000, 100000),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create message size histogram: %w", err)
	}

	w.collaborationSessions, err = meter.Int64UpDownCounter(
		"websocket_collaboration_sessions_active",
		metric.WithDescription("Number of active collaboration sessions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create collaboration sessions counter: %w", err)
	}

	w.sessionDuration, err = meter.Float64Histogram(
		"websocket_collaboration_session_duration_seconds",
		metric.WithDescription("Duration of collaboration sessions"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(10, 30, 60, 300, 600, 1800, 3600, 7200),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session duration histogram: %w", err)
	}

	w.broadcastCounter, err = meter.Int64Counter(
		"websocket_broadcasts_total",
		metric.WithDescription("Total number of WebSocket broadcasts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create broadcast counter: %w", err)
	}

	w.broadcastDuration, err = meter.Float64Histogram(
		"websocket_broadcast_duration_seconds",
		metric.WithDescription("Duration of WebSocket broadcast operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create broadcast duration histogram: %w", err)
	}

	w.errorCounter, err = meter.Int64Counter(
		"websocket_errors_total",
		metric.WithDescription("Total number of WebSocket errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create error counter: %w", err)
	}

	return w, nil
}

// TraceConnection traces WebSocket connection lifecycle
func (w *WebSocketTracing) TraceConnection(ctx context.Context, diagramID, userID string) (context.Context, func(err error)) {
	startTime := time.Now()

	ctx, span := w.tracer.Start(ctx, "websocket.connection",
		trace.WithSpanKind(trace.SpanKindServer),
	)

	// Add span attributes (sanitize sensitive data)
	span.SetAttributes(
		attribute.String("websocket.diagram_id", diagramID),
		attribute.String("websocket.user_id", sanitizeUserID(userID)),
		attribute.String("websocket.event", "connection_start"),
	)

	// Increment active connections
	w.connectionCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("diagram_id", diagramID),
		attribute.String("event", "connected"),
	))

	return ctx, func(err error) {
		duration := time.Since(startTime)
		defer span.End()

		// Decrement active connections
		w.connectionCounter.Add(ctx, -1, metric.WithAttributes(
			attribute.String("diagram_id", diagramID),
			attribute.String("event", "disconnected"),
		))

		status := "success"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			w.errorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "connection_error"),
				attribute.String("diagram_id", diagramID),
			))
		} else {
			span.SetStatus(codes.Ok, "Connection closed successfully")
		}

		span.SetAttributes(
			attribute.String("websocket.status", status),
			attribute.Float64("websocket.duration_seconds", duration.Seconds()),
		)

		// Record connection duration
		w.connectionDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attribute.String("diagram_id", diagramID),
			attribute.String("status", status),
		))
	}
}

// TraceMessage traces WebSocket message processing
func (w *WebSocketTracing) TraceMessage(ctx context.Context, messageType, diagramID string, messageSize int) (context.Context, func(success bool, recipientCount int, err error)) {
	startTime := time.Now()

	ctx, span := w.tracer.Start(ctx, "websocket.message",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	span.SetAttributes(
		attribute.String("websocket.message_type", messageType),
		attribute.String("websocket.diagram_id", diagramID),
		attribute.Int("websocket.message_size", messageSize),
	)

	return ctx, func(success bool, recipientCount int, err error) {
		duration := time.Since(startTime)
		defer span.End()

		status := "success"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			w.errorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "message_error"),
				attribute.String("message_type", messageType),
			))
		} else if !success {
			status = "failure"
			span.SetStatus(codes.Error, "Message processing failed")
		} else {
			span.SetStatus(codes.Ok, "Message processed successfully")
		}

		span.SetAttributes(
			attribute.String("websocket.status", status),
			attribute.Int("websocket.recipient_count", recipientCount),
			attribute.Float64("websocket.duration_ms", float64(duration.Nanoseconds())/1e6),
		)

		// Record metrics
		attrs := []attribute.KeyValue{
			attribute.String("message_type", messageType),
			attribute.String("diagram_id", diagramID),
			attribute.String("status", status),
		}

		w.messageCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		w.messageDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

		if messageSize > 0 {
			w.messageSize.Record(ctx, int64(messageSize), metric.WithAttributes(
				attribute.String("message_type", messageType),
			))
		}
	}
}

// TraceBroadcast traces WebSocket broadcast operations
func (w *WebSocketTracing) TraceBroadcast(ctx context.Context, broadcastType, diagramID string, targetCount int) (context.Context, func(successCount, failureCount int, err error)) {
	startTime := time.Now()

	ctx, span := w.tracer.Start(ctx, "websocket.broadcast",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	span.SetAttributes(
		attribute.String("websocket.broadcast_type", broadcastType),
		attribute.String("websocket.diagram_id", diagramID),
		attribute.Int("websocket.target_count", targetCount),
	)

	return ctx, func(successCount, failureCount int, err error) {
		duration := time.Since(startTime)
		defer span.End()

		status := "success"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if failureCount > 0 {
			status = "partial"
			span.SetStatus(codes.Error, fmt.Sprintf("Broadcast partially failed: %d failures", failureCount))
		} else {
			span.SetStatus(codes.Ok, "Broadcast completed successfully")
		}

		span.SetAttributes(
			attribute.String("websocket.status", status),
			attribute.Int("websocket.success_count", successCount),
			attribute.Int("websocket.failure_count", failureCount),
			attribute.Float64("websocket.duration_ms", float64(duration.Nanoseconds())/1e6),
		)

		// Record metrics
		attrs := []attribute.KeyValue{
			attribute.String("broadcast_type", broadcastType),
			attribute.String("diagram_id", diagramID),
			attribute.String("status", status),
		}

		w.broadcastCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		w.broadcastDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

		if failureCount > 0 {
			w.errorCounter.Add(ctx, int64(failureCount), metric.WithAttributes(
				attribute.String("error_type", "broadcast_failure"),
				attribute.String("broadcast_type", broadcastType),
			))
		}
	}
}

// TraceCollaborationSession traces collaboration session lifecycle
func (w *WebSocketTracing) TraceCollaborationSession(ctx context.Context, diagramID string, participantCount int) (context.Context, func(totalMessages int, err error)) {
	startTime := time.Now()

	ctx, span := w.tracer.Start(ctx, "websocket.collaboration_session",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	span.SetAttributes(
		attribute.String("websocket.diagram_id", diagramID),
		attribute.Int("websocket.initial_participant_count", participantCount),
		attribute.String("websocket.session_event", "started"),
	)

	// Increment active collaboration sessions
	w.collaborationSessions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("diagram_id", diagramID),
	))

	return ctx, func(totalMessages int, err error) {
		duration := time.Since(startTime)
		defer span.End()

		// Decrement active collaboration sessions
		w.collaborationSessions.Add(ctx, -1, metric.WithAttributes(
			attribute.String("diagram_id", diagramID),
		))

		status := "completed"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			w.errorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "session_error"),
				attribute.String("diagram_id", diagramID),
			))
		} else {
			span.SetStatus(codes.Ok, "Collaboration session completed")
		}

		span.SetAttributes(
			attribute.String("websocket.session_status", status),
			attribute.Int("websocket.total_messages", totalMessages),
			attribute.Float64("websocket.session_duration_seconds", duration.Seconds()),
			attribute.String("websocket.session_event", "ended"),
		)

		// Record session duration
		w.sessionDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attribute.String("diagram_id", diagramID),
			attribute.String("status", status),
		))
	}
}

// TraceRealTimeUpdate traces real-time collaboration updates
func (w *WebSocketTracing) TraceRealTimeUpdate(ctx context.Context, updateType, diagramID, cellID string) (context.Context, func(propagated bool, latency time.Duration, err error)) {
	startTime := time.Now()

	ctx, span := w.tracer.Start(ctx, "websocket.realtime_update",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	span.SetAttributes(
		attribute.String("websocket.update_type", updateType),
		attribute.String("websocket.diagram_id", diagramID),
		attribute.String("websocket.cell_id", cellID),
	)

	return ctx, func(propagated bool, latency time.Duration, err error) {
		duration := time.Since(startTime)
		defer span.End()

		status := "propagated"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			w.errorCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "update_error"),
				attribute.String("update_type", updateType),
			))
		} else if !propagated {
			status = "failed"
			span.SetStatus(codes.Error, "Update propagation failed")
		} else {
			span.SetStatus(codes.Ok, "Update propagated successfully")
		}

		span.SetAttributes(
			attribute.String("websocket.propagation_status", status),
			attribute.Float64("websocket.processing_duration_ms", float64(duration.Nanoseconds())/1e6),
			attribute.Float64("websocket.propagation_latency_ms", float64(latency.Nanoseconds())/1e6),
		)

		// Record metrics
		w.messageCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("message_type", "realtime_update"),
			attribute.String("update_type", updateType),
			attribute.String("status", status),
		))

		w.messageDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attribute.String("message_type", "realtime_update"),
			attribute.String("update_type", updateType),
		))
	}
}

// RecordConnectionHealth records WebSocket connection health metrics
func (w *WebSocketTracing) RecordConnectionHealth(ctx context.Context, diagramID string, activeConnections, healthyConnections int) {
	w.connectionCounter.Add(ctx, int64(activeConnections), metric.WithAttributes(
		attribute.String("diagram_id", diagramID),
		attribute.String("health_status", "total"),
	))

	if healthyConnections < activeConnections {
		unhealthyCount := activeConnections - healthyConnections
		w.errorCounter.Add(ctx, int64(unhealthyCount), metric.WithAttributes(
			attribute.String("error_type", "unhealthy_connection"),
			attribute.String("diagram_id", diagramID),
		))
	}
}

// GetWebSocketTracing returns a configured WebSocket tracing instance
func GetWebSocketTracing() *WebSocketTracing {
	service := GetService()
	if service == nil {
		return nil
	}

	wsTracing, _ := NewWebSocketTracing(service.GetTracer(), service.GetMeter())
	return wsTracing
}
