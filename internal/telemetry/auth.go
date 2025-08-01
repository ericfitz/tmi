package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// AuthTracing provides authorization and authentication tracing
type AuthTracing struct {
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics instruments
	authAttemptCounter    metric.Int64Counter
	authSuccessCounter    metric.Int64Counter
	authFailureCounter    metric.Int64Counter
	authDuration          metric.Float64Histogram
	jwtValidationCounter  metric.Int64Counter
	jwtValidationDuration metric.Float64Histogram
	oauthFlowCounter      metric.Int64Counter
	oauthFlowDuration     metric.Float64Histogram
	authorizationCounter  metric.Int64Counter
	roleCheckCounter      metric.Int64Counter
}

// NewAuthTracing creates a new authorization tracing instance
func NewAuthTracing(tracer trace.Tracer, meter metric.Meter) (*AuthTracing, error) {
	a := &AuthTracing{
		tracer: tracer,
		meter:  meter,
	}

	var err error

	// Create metrics instruments
	a.authAttemptCounter, err = meter.Int64Counter(
		"auth_attempts_total",
		metric.WithDescription("Total number of authentication attempts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth attempt counter: %w", err)
	}

	a.authSuccessCounter, err = meter.Int64Counter(
		"auth_success_total",
		metric.WithDescription("Total number of successful authentications"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth success counter: %w", err)
	}

	a.authFailureCounter, err = meter.Int64Counter(
		"auth_failures_total",
		metric.WithDescription("Total number of authentication failures"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth failure counter: %w", err)
	}

	a.authDuration, err = meter.Float64Histogram(
		"auth_duration_seconds",
		metric.WithDescription("Duration of authentication operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth duration histogram: %w", err)
	}

	a.jwtValidationCounter, err = meter.Int64Counter(
		"jwt_validations_total",
		metric.WithDescription("Total number of JWT validation operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT validation counter: %w", err)
	}

	a.jwtValidationDuration, err = meter.Float64Histogram(
		"jwt_validation_duration_seconds",
		metric.WithDescription("Duration of JWT validation operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT validation duration histogram: %w", err)
	}

	a.oauthFlowCounter, err = meter.Int64Counter(
		"oauth_flows_total",
		metric.WithDescription("Total number of OAuth flow operations"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OAuth flow counter: %w", err)
	}

	a.oauthFlowDuration, err = meter.Float64Histogram(
		"oauth_flow_duration_seconds",
		metric.WithDescription("Duration of OAuth flow operations"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 25.0),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OAuth flow duration histogram: %w", err)
	}

	a.authorizationCounter, err = meter.Int64Counter(
		"authorization_checks_total",
		metric.WithDescription("Total number of authorization checks"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create authorization counter: %w", err)
	}

	a.roleCheckCounter, err = meter.Int64Counter(
		"role_checks_total",
		metric.WithDescription("Total number of role-based access checks"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create role check counter: %w", err)
	}

	return a, nil
}

// TraceOAuthFlow traces OAuth authentication flow
func (a *AuthTracing) TraceOAuthFlow(ctx context.Context, provider, flowType string) (context.Context, func(success bool, userID string, err error)) {
	startTime := time.Now()

	ctx, span := a.tracer.Start(ctx, "auth.oauth_flow",
		trace.WithSpanKind(trace.SpanKindClient),
	)

	// Add span attributes (excluding sensitive data)
	span.SetAttributes(
		attribute.String("auth.provider", provider),
		attribute.String("auth.flow_type", flowType),
		attribute.String("auth.method", "oauth"),
	)

	// Record OAuth flow attempt
	a.oauthFlowCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("flow_type", flowType),
		attribute.String("status", "started"),
	))

	return ctx, func(success bool, userID string, err error) {
		duration := time.Since(startTime)
		defer span.End()

		status := "success"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if !success {
			status = "failure"
			span.SetStatus(codes.Error, "OAuth flow failed")
		}

		// Add completion attributes (sanitize user ID)
		span.SetAttributes(
			attribute.String("auth.status", status),
			attribute.Float64("auth.duration_ms", float64(duration.Nanoseconds())/1e6),
		)

		if success && userID != "" {
			span.SetAttributes(attribute.String("auth.user_id", sanitizeUserID(userID)))
		}

		// Record metrics
		attrs := []attribute.KeyValue{
			attribute.String("provider", provider),
			attribute.String("flow_type", flowType),
			attribute.String("status", status),
		}

		a.oauthFlowCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		a.oauthFlowDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

		if success {
			a.authSuccessCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "oauth"),
				attribute.String("provider", provider),
			))
		} else {
			a.authFailureCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "oauth"),
				attribute.String("provider", provider),
				attribute.String("reason", getFailureReason(err)),
			))
		}
	}
}

// TraceJWTValidation traces JWT token validation
func (a *AuthTracing) TraceJWTValidation(ctx context.Context, tokenType string) (context.Context, func(valid bool, userID string, err error)) {
	startTime := time.Now()

	ctx, span := a.tracer.Start(ctx, "auth.jwt_validation",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	span.SetAttributes(
		attribute.String("auth.token_type", tokenType),
		attribute.String("auth.method", "jwt"),
	)

	return ctx, func(valid bool, userID string, err error) {
		duration := time.Since(startTime)
		defer span.End()

		status := "valid"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if !valid {
			status = "invalid"
			span.SetStatus(codes.Error, "Invalid JWT token")
		}

		span.SetAttributes(
			attribute.String("auth.validation_status", status),
			attribute.Float64("auth.duration_ms", float64(duration.Nanoseconds())/1e6),
		)

		if valid && userID != "" {
			span.SetAttributes(attribute.String("auth.user_id", sanitizeUserID(userID)))
			span.SetStatus(codes.Ok, "JWT validation successful")
		}

		// Record metrics
		attrs := []attribute.KeyValue{
			attribute.String("token_type", tokenType),
			attribute.String("status", status),
		}

		a.jwtValidationCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		a.jwtValidationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	}
}

// TraceAuthorizationCheck traces authorization checks
func (a *AuthTracing) TraceAuthorizationCheck(ctx context.Context, resource, action string) (context.Context, func(allowed bool, userRole string, err error)) {
	startTime := time.Now()

	ctx, span := a.tracer.Start(ctx, "auth.authorization_check",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	span.SetAttributes(
		attribute.String("auth.resource", resource),
		attribute.String("auth.action", action),
	)

	return ctx, func(allowed bool, userRole string, err error) {
		duration := time.Since(startTime)
		defer span.End()

		status := "allowed"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if !allowed {
			status = "denied"
			span.SetStatus(codes.Error, "Authorization denied")
		} else {
			span.SetStatus(codes.Ok, "Authorization granted")
		}

		span.SetAttributes(
			attribute.String("auth.decision", status),
			attribute.Float64("auth.duration_ms", float64(duration.Nanoseconds())/1e6),
		)

		if userRole != "" {
			span.SetAttributes(attribute.String("auth.user_role", userRole))
		}

		// Record metrics
		attrs := []attribute.KeyValue{
			attribute.String("resource", resource),
			attribute.String("action", action),
			attribute.String("decision", status),
		}

		if userRole != "" {
			attrs = append(attrs, attribute.String("user_role", userRole))
		}

		a.authorizationCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		a.authDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	}
}

// TraceRoleCheck traces role-based access control checks
func (a *AuthTracing) TraceRoleCheck(ctx context.Context, requiredRole, userRole string) (context.Context, func(hasRole bool, err error)) {
	startTime := time.Now()

	ctx, span := a.tracer.Start(ctx, "auth.role_check",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	span.SetAttributes(
		attribute.String("auth.required_role", requiredRole),
		attribute.String("auth.user_role", userRole),
	)

	return ctx, func(hasRole bool, err error) {
		duration := time.Since(startTime)
		defer span.End()

		status := "granted"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if !hasRole {
			status = "insufficient"
			span.SetStatus(codes.Error, "Insufficient privileges")
		} else {
			span.SetStatus(codes.Ok, "Role check passed")
		}

		span.SetAttributes(
			attribute.String("auth.role_status", status),
			attribute.Float64("auth.duration_ms", float64(duration.Nanoseconds())/1e6),
		)

		// Record metrics
		attrs := []attribute.KeyValue{
			attribute.String("required_role", requiredRole),
			attribute.String("user_role", userRole),
			attribute.String("status", status),
		}

		a.roleCheckCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
		a.authDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	}
}

// TraceUserSession traces user session operations
func (a *AuthTracing) TraceUserSession(ctx context.Context, operation string) (context.Context, func(success bool, sessionID string, err error)) {
	startTime := time.Now()

	ctx, span := a.tracer.Start(ctx, "auth.session_operation",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	span.SetAttributes(
		attribute.String("auth.session_operation", operation),
	)

	return ctx, func(success bool, sessionID string, err error) {
		duration := time.Since(startTime)
		defer span.End()

		status := "success"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if !success {
			status = "failure"
		} else {
			span.SetStatus(codes.Ok, "Session operation successful")
		}

		span.SetAttributes(
			attribute.String("auth.session_status", status),
			attribute.Float64("auth.duration_ms", float64(duration.Nanoseconds())/1e6),
		)

		if success && sessionID != "" {
			span.SetAttributes(attribute.String("auth.session_id", sanitizeSessionID(sessionID)))
		}

		// Record metrics
		a.authAttemptCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("status", status),
		))
		a.authDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("status", status),
		))
	}
}

// Helper functions for data sanitization and security

func sanitizeUserID(userID string) string {
	if len(userID) <= 8 {
		return "[REDACTED]"
	}
	// Show first 4 and last 4 characters, redact middle
	return userID[:4] + "***" + userID[len(userID)-4:]
}

func sanitizeSessionID(sessionID string) string {
	if len(sessionID) <= 12 {
		return "[REDACTED]"
	}
	// Show first 4 and last 4 characters, redact middle
	return sessionID[:4] + "***" + sessionID[len(sessionID)-4:]
}

func getFailureReason(err error) string {
	if err == nil {
		return "unknown"
	}

	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "invalid"):
		return "invalid_credentials"
	case strings.Contains(errStr, "expired"):
		return "token_expired"
	case strings.Contains(errStr, "timeout"):
		return "timeout"
	case strings.Contains(errStr, "network"):
		return "network_error"
	case strings.Contains(errStr, "permission"):
		return "permission_denied"
	default:
		return "auth_error"
	}
}

// GetAuthTracing returns a configured auth tracing instance
func GetAuthTracing() *AuthTracing {
	service := GetService()
	if service == nil {
		return nil
	}

	authTracing, _ := NewAuthTracing(service.GetTracer(), service.GetMeter())
	return authTracing
}
